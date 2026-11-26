package mattermost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/charterhq/charter/internal/driver"
)

// fakeMM serves users, posts, reactions and a websocket that sends what the test pushes.
type fakeMM struct {
	mu        sync.Mutex
	events    chan string
	posts     []post
	reactions []string
	dials     int
	drop      chan struct{} // closing a connection forces a reconnect
	deaf      bool          // the first connection never reads: pings get no pong, like a half-open socket
}

func (f *fakeMM) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/users/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bot-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(user{ID: "bot-id", Username: "sre-manager", IsBot: true})
	})
	mux.HandleFunc("GET /api/v4/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		_ = json.NewEncoder(w).Encode(user{ID: id, Username: strings.TrimPrefix(id, "u-"), IsBot: id == "u-alertbot"})
	})
	mux.HandleFunc("POST /api/v4/posts", func(w http.ResponseWriter, r *http.Request) {
		var p post
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.mu.Lock()
		f.posts = append(f.posts, p)
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"new"}`))
	})
	mux.HandleFunc("POST /api/v4/reactions", func(w http.ResponseWriter, r *http.Request) {
		var rc map[string]string
		_ = json.NewDecoder(r.Body).Decode(&rc)
		f.mu.Lock()
		f.reactions = append(f.reactions, rc["post_id"]+":"+rc["emoji_name"])
		f.mu.Unlock()
	})
	mux.HandleFunc("GET /api/v4/websocket", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bot-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = c.CloseNow() }()
		f.mu.Lock()
		f.dials++
		drop := f.drop
		f.mu.Unlock()
		if f.deaf && f.dials == 1 {
			<-r.Context().Done() // holds the connection open and silent
			return
		}
		ctx := c.CloseRead(r.Context()) // answers pings
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"event":"hello","data":{}}`))
		for {
			select {
			case <-ctx.Done():
				return
			case <-drop:
				return
			case ev := <-f.events:
				if err := c.Write(ctx, websocket.MessageText, []byte(ev)); err != nil {
					return
				}
			}
		}
	})
	return mux
}

func posted(channelType string, p wsPost) string {
	pj, _ := json.Marshal(p)
	ev, _ := json.Marshal(map[string]any{"event": "posted", "data": map[string]string{"channel_type": channelType, "post": string(pj)}})
	return string(ev)
}

type fakeInbox struct {
	mu  sync.Mutex
	got []driver.Incoming
}

func (i *fakeInbox) Listeners(context.Context) ([]driver.Listener, error) {
	return []driver.Listener{
		{System: System, Account: "sre-manager", Agent: "sre-manager", Connection: "sre", Source: "chat"},
		{System: System, Account: "sre-manager", Agent: "sre-manager", Connection: "qa", Source: "chat", Places: []string{"qa-chan"}},
		{System: "slack", Account: "other", Agent: "x", Connection: "y", Source: "z"},
	}, nil
}

func (i *fakeInbox) Converse(_ context.Context, m driver.Incoming) (string, bool, error) {
	if m.Author == "stranger" {
		return "", false, driver.ErrForbidden
	}
	i.mu.Lock()
	i.got = append(i.got, m)
	i.mu.Unlock()
	return "run-" + m.MessageID, false, nil
}

func (i *fakeInbox) count() int { i.mu.Lock(); defer i.mu.Unlock(); return len(i.got) }

func (i *fakeInbox) last() driver.Incoming {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.got[len(i.got)-1]
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timeout: %s", what)
}

func TestReceive(t *testing.T) {
	f := &fakeMM{events: make(chan string, 16), drop: make(chan struct{})}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	c := New(Config{URL: srv.URL, Token: "core-token", Bots: map[string]string{"sre-manager": "bot-token"},
		Refusal: "не могу", PingInterval: 200 * time.Millisecond, RefreshInterval: time.Hour})
	inbox := &fakeInbox{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Receive(ctx, inbox) }()

	// not addressed: plain post, @channel, the bot itself, another bot – all ignored
	f.events <- posted("O", wsPost{ID: "p0", UserID: "u-alice", ChannelID: "c1", Message: "hello all"})
	f.events <- posted("O", wsPost{ID: "p1", UserID: "u-alice", ChannelID: "c1", Message: "@channel deploy"})
	f.events <- posted("O", wsPost{ID: "p2", UserID: "bot-id", ChannelID: "c1", Message: "@sre-manager loop"})
	f.events <- posted("O", wsPost{ID: "p3", UserID: "u-alertbot", ChannelID: "c1", Message: "@sre-manager fire"})
	// addressed in a channel: a new conversation rooted at the post
	f.events <- posted("O", wsPost{ID: "p4", UserID: "u-alice", ChannelID: "c1", Message: "@sre-manager заведи задачу"})
	waitUntil(t, "first message", func() bool { return inbox.count() == 1 })
	m := inbox.last()
	if m.Text != "заведи задачу" || m.Conversation != "p4" || m.Reply.Thread != "p4" || m.Author != "alice" ||
		m.Connection != "sre" || m.MessageID != "p4" {
		t.Fatalf("channel message: %+v", m)
	}
	// a reply in the thread continues the conversation
	f.events <- posted("O", wsPost{ID: "p5", UserID: "u-alice", ChannelID: "c1", RootID: "p4", Message: "@SRE-Manager, да"})
	waitUntil(t, "thread reply", func() bool { return inbox.count() == 2 })
	if m := inbox.last(); m.Conversation != "p4" || m.Reply.Thread != "p4" || m.Text != "да" {
		t.Fatalf("thread reply: %+v", m)
	}
	// a direct message needs no mention; the channel is the conversation
	f.events <- posted("D", wsPost{ID: "p6", UserID: "u-alice", ChannelID: "dm1", Message: "привет"})
	waitUntil(t, "dm", func() bool { return inbox.count() == 3 })
	if m := inbox.last(); m.Conversation != "dm1" || m.Reply.Thread != "" || m.Reply.Channel != "dm1" {
		t.Fatalf("dm: %+v", m)
	}
	// a channel with its own connection
	f.events <- posted("O", wsPost{ID: "p7", UserID: "u-alice", ChannelID: "qa-chan", Message: "@sre-manager test"})
	waitUntil(t, "qa", func() bool { return inbox.count() == 4 })
	if m := inbox.last(); m.Connection != "qa" {
		t.Fatalf("routing: %+v", m)
	}
	// a stranger gets the refusal in the thread
	f.events <- posted("O", wsPost{ID: "p8", UserID: "u-stranger", ChannelID: "c1", Message: "@sre-manager дай доступ"})
	waitUntil(t, "refusal", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.posts) == 1 })
	f.mu.Lock()
	if p := f.posts[0]; p.RootID != "p8" || p.Message != "@stranger не могу" {
		t.Fatalf("refusal: %+v", p)
	}
	if len(f.reactions) != 4 || f.reactions[0] != "p4:eyes" {
		t.Fatalf("reactions: %v", f.reactions)
	}
	// the server drops the connection: the bot reconnects and keeps listening
	close(f.drop)
	f.drop = make(chan struct{})
	f.mu.Unlock()
	waitUntil(t, "reconnect", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.dials >= 2 })
	f.events <- posted("O", wsPost{ID: "p9", UserID: "u-alice", ChannelID: "c1", Message: "@sre-manager снова"})
	waitUntil(t, "after reconnect", func() bool { return inbox.count() == 5 })
}

func TestPostAsAgentBot(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL, Token: "core-token", Bots: map[string]string{"sre-manager": "bot-token"}})
	ctx := context.Background()
	target := driver.Target{Channel: "c1"}
	_, _ = c.Post(ctx, driver.Message{Agent: "sre-manager", Target: target, Text: "a", Identities: map[string]string{System: "sre-manager"}})
	_, _ = c.Post(ctx, driver.Message{Agent: "alert-triage", Target: target, Text: "b"})
	if len(auth) != 2 || auth[0] != "Bearer bot-token" || auth[1] != "Bearer core-token" {
		t.Fatalf("tokens: %v", auth)
	}
}

func TestStripMention(t *testing.T) {
	for _, c := range []struct {
		in, out string
		named   bool
	}{
		{"@sre-manager hi", "hi", true},
		{"hi @sre-manager", "hi", true},
		{"hi @sre-manager-bot", "hi @sre-manager-bot", false},
		{"mail me@sre-manager.ru", "mail me@sre-manager.ru", false},
		{"@here @sre-manager ok", "@here ok", true},
		{"@sre-manager", "", true},
	} {
		got, named := stripMention(c.in, "sre-manager")
		if got != c.out || named != c.named {
			t.Errorf("%q: got %q %v", c.in, got, named)
		}
	}
}

// A connection that stays open but answers nothing is dropped by the ping and replaced.
func TestHalfOpenSocketReconnects(t *testing.T) {
	f := &fakeMM{events: make(chan string, 4), drop: make(chan struct{}), deaf: true}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	c := New(Config{URL: srv.URL, Token: "core-token", Bots: map[string]string{"sre-manager": "bot-token"},
		PingInterval: 100 * time.Millisecond, RefreshInterval: time.Hour})
	inbox := &fakeInbox{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Receive(ctx, inbox) }()
	waitUntil(t, "second dial", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.dials >= 2 })
	f.events <- posted("O", wsPost{ID: "p1", UserID: "u-alice", ChannelID: "c1", Message: "@sre-manager жив?"})
	waitUntil(t, "message after reconnect", func() bool { return inbox.count() == 1 })
}
