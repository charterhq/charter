package mattermost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/charterhq/charter/internal/driver"
)

// System is the name agents use for Mattermost in spec.identities.
const System = "mattermost"

// Receive listens as every agent bot the core knows (driver.Inbox.Listeners) and hands messages
// addressed to a bot to the core: a direct message, or a post that names the bot as @name.
// A thread is a conversation; in direct messages the channel is. Blocks until ctx is done.
func (c *Channel) Receive(ctx context.Context, inbox driver.Inbox) error {
	bots := map[string]*bot{}
	warned := map[string]bool{}
	refresh := func() {
		ls, err := inbox.Listeners(ctx)
		if err != nil {
			c.log().Warn("mattermost: listeners", "err", err)
			return
		}
		routes := map[string][]driver.Listener{}
		for _, l := range ls {
			if l.System == System {
				routes[l.Account] = append(routes[l.Account], l)
			}
		}
		for acc, rs := range routes {
			b := bots[acc]
			if b == nil {
				token := c.cfg.Bots[acc]
				if token == "" {
					if !warned[acc] {
						c.log().Warn("mattermost: no token for agent bot, its messages are not received", "account", acc)
						warned[acc] = true
					}
					continue
				}
				b = &bot{c: c, account: acc, token: token, inbox: inbox, users: map[string]user{}}
				bots[acc] = b
				go b.run(ctx)
			}
			b.setRoutes(rs)
		}
		for acc, b := range bots {
			if _, ok := routes[acc]; !ok {
				b.setRoutes(nil)
			}
		}
	}
	refresh()
	t := time.NewTicker(c.cfg.RefreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			refresh()
		}
	}
}

func (c *Channel) log() *slog.Logger {
	if c.cfg.Log != nil {
		return c.cfg.Log
	}
	return slog.Default()
}

type user struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	IsBot    bool   `json:"is_bot"`
}

// bot is one agent account with its websocket.
type bot struct {
	c       *Channel
	account string
	token   string
	inbox   driver.Inbox

	mu     sync.Mutex
	routes []driver.Listener
	me     user
	users  map[string]user
}

func (b *bot) setRoutes(rs []driver.Listener) {
	b.mu.Lock()
	b.routes = rs
	b.mu.Unlock()
}

// route picks the listener for a channel: one that names the channel, else one without places.
func (b *bot) route(channel string) (driver.Listener, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var any *driver.Listener
	for i, l := range b.routes {
		if len(l.Places) == 0 && any == nil {
			any = &b.routes[i]
		}
		for _, p := range l.Places {
			if p == channel {
				return l, true
			}
		}
	}
	if any != nil {
		return *any, true
	}
	return driver.Listener{}, false
}

// run keeps the websocket up: a session ends on any error, the next one starts after a backoff.
func (b *bot) run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := b.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		b.c.log().Warn("mattermost: websocket closed, reconnecting", "account", b.account, "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// session is one websocket connection. A ping without a pong in time ends it: a half-open
// connection otherwise waits for frames forever and the bot goes deaf.
func (b *bot) session(ctx context.Context) error {
	if b.me.ID == "" {
		var me user
		if err := b.c.get(ctx, b.token, "/api/v4/users/me", &me); err != nil {
			return fmt.Errorf("users/me: %w", err)
		}
		b.me = me
	}
	url := "ws" + strings.TrimPrefix(b.c.cfg.URL, "http") + "/api/v4/websocket"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPClient: b.c.cfg.HTTP,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + b.token}},
	})
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(4 << 20)
	b.c.log().Info("mattermost: listening", "account", b.account)

	sctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go func() {
		t := time.NewTicker(b.c.cfg.PingInterval)
		defer t.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(sctx, b.c.cfg.PingInterval)
				err := conn.Ping(pctx)
				pcancel()
				if err != nil {
					cancel(fmt.Errorf("ping: %w", err))
					return
				}
			}
		}
	}()
	for {
		_, data, err := conn.Read(sctx)
		if err != nil {
			if cause := context.Cause(sctx); cause != nil && !errors.Is(cause, context.Canceled) {
				return cause
			}
			return err
		}
		b.handle(ctx, data)
	}
}

type event struct {
	Event string `json:"event"`
	Data  struct {
		ChannelType string `json:"channel_type"`
		Post        string `json:"post"`
	} `json:"data"`
}

type wsPost struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
	RootID    string `json:"root_id"`
	Message   string `json:"message"`
	Type      string `json:"type"`
}

// handle passes a post on when it is addressed to the bot. Posts are handled in the order they
// arrive, so a conversation reaches the core in order.
func (b *bot) handle(ctx context.Context, data []byte) {
	var ev event
	if json.Unmarshal(data, &ev) != nil || ev.Event != "posted" {
		return
	}
	var p wsPost
	if json.Unmarshal([]byte(ev.Data.Post), &p) != nil || p.UserID == b.me.ID || p.Type != "" {
		return
	}
	dm := ev.Data.ChannelType == "D"
	text, named := stripMention(p.Message, b.me.Username)
	if (!dm && !named) || text == "" {
		return
	}
	author, err := b.user(ctx, p.UserID)
	if err != nil {
		b.c.log().Warn("mattermost: author", "account", b.account, "post", p.ID, "err", err)
		return
	}
	if author.IsBot {
		return
	}
	l, ok := b.route(p.ChannelID)
	if !ok {
		return // the bot is in a channel no connection binds
	}
	conv, thread := p.RootID, p.RootID
	switch {
	case conv == "" && dm:
		conv = p.ChannelID
	case conv == "":
		conv, thread = p.ID, p.ID
	}
	_, dup, err := b.inbox.Converse(ctx, driver.Incoming{
		Agent: l.Agent, Connection: l.Connection, Source: l.Source,
		Conversation: conv, MessageID: p.ID, Author: author.Username, Text: text,
		Reply: driver.Target{System: System, Channel: p.ChannelID, Thread: thread},
	})
	switch {
	case errors.Is(err, driver.ErrForbidden):
		if _, perr := b.c.createAs(ctx, b.token, post{ChannelID: p.ChannelID, RootID: thread,
			Message: "@" + author.Username + " " + b.c.cfg.Refusal}); perr != nil {
			b.c.log().Warn("mattermost: refusal not posted", "account", b.account, "err", perr)
		}
	case err != nil:
		b.c.log().Warn("mattermost: message not accepted", "account", b.account, "agent", l.Agent, "post", p.ID, "err", err)
	case !dup:
		b.react(ctx, p.ID, "eyes")
	}
}

func (b *bot) user(ctx context.Context, id string) (user, error) {
	b.mu.Lock()
	u, ok := b.users[id]
	b.mu.Unlock()
	if ok {
		return u, nil
	}
	if err := b.c.get(ctx, b.token, "/api/v4/users/"+id, &u); err != nil {
		return u, err
	}
	b.mu.Lock()
	b.users[id] = u
	b.mu.Unlock()
	return u, nil
}

// react marks the post as taken; best effort.
func (b *bot) react(ctx context.Context, postID, emoji string) {
	body, _ := json.Marshal(map[string]string{"user_id": b.me.ID, "post_id": postID, "emoji_name": emoji})
	if err := b.c.do(ctx, b.token, http.MethodPost, "/api/v4/reactions", body, nil); err != nil {
		b.c.log().Warn("mattermost: reaction", "account", b.account, "err", err)
	}
}

const nameChars = `\p{L}\p{N}_.\-`

// stripMention removes @name from the text and reports whether it was there. @channel, @all and
// @here do not name the bot.
func stripMention(text, name string) (string, bool) {
	if name == "" {
		return strings.TrimSpace(text), false
	}
	re := regexp.MustCompile(`(?i)(^|[^` + nameChars + `])@` + regexp.QuoteMeta(name) + `($|[^` + nameChars + `])`)
	if !re.MatchString(text) {
		return strings.TrimSpace(text), false
	}
	out := strings.Join(strings.Fields(re.ReplaceAllString(text, "$1$2")), " ")
	return strings.TrimSpace(strings.TrimLeft(out, ",:; ")), true // "@bot, да" is "да"
}

func (c *Channel) get(ctx context.Context, token, path string, out any) error {
	return c.do(ctx, token, http.MethodGet, path, nil, out)
}

func (c *Channel) do(ctx context.Context, token, method, path string, body []byte, out any) error {
	var rd io.Reader
	if body != nil {
		rd = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.URL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %.200s", resp.StatusCode, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
