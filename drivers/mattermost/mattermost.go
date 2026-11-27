// Package mattermost talks to people through Mattermost as a bot.
package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/signing"
)

// Config of the driver.
type Config struct {
	URL   string
	Token string
	// CallbackURL is the public base URL of the core for button callbacks.
	CallbackURL string
	// Secret signs button decisions.
	Secret []byte
	// DefaultChannel is used when a message has no target channel.
	DefaultChannel string
	HTTP           *http.Client
	// System is the logical system of spec.identities and targets this driver serves (chat).
	System string
	// Bots are agent bot tokens by account (spec.identities of System). An agent with its
	// own bot posts and listens as it; others post as Token.
	Bots map[string]string
	// Refusal is the answer to an author the connection does not let talk to the agent.
	Refusal string
	// PingInterval is how often the websocket is pinged; a missing pong within it reconnects (30s).
	PingInterval time.Duration
	// RefreshInterval is how often the listeners are re-read from the core (1m).
	RefreshInterval time.Duration
	Log             *slog.Logger
	// Labels of the approval card.
	ApproveLabel, RejectLabel, ApprovalNote string
	// Texts shown to the person who pressed a button; "%s" is their name where it fits.
	ApprovedText, RejectedText, ClosedText, ChangedText, SameApproverText, SuspendedText string
	// TypingInterval is how often "typing" is repeated while an agent works on a message (4s);
	// TypingLimit stops it if no answer comes (10m).
	TypingInterval, TypingLimit time.Duration
}

// Channel is a driver.Channel backed by Mattermost.
type Channel struct {
	cfg    Config
	typing sync.Map // channel/thread -> context.CancelFunc
}

// New returns a Mattermost channel driver.
func New(cfg Config) *Channel {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 30 * time.Second
	}
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = time.Minute
	}
	if cfg.ApproveLabel == "" {
		cfg.ApproveLabel = "Approve"
	}
	if cfg.RejectLabel == "" {
		cfg.RejectLabel = "Reject"
	}
	if cfg.ApprovalNote == "" {
		cfg.ApprovalNote = "Approve runs the action with exactly the parameters above. Deadline:"
	}
	if cfg.System == "" {
		cfg.System = DefaultSystem
	}
	defaults := []struct {
		v   *string
		def string
	}{
		{&cfg.ApprovedText, "Approved by @%s. The action runs with the shown parameters."},
		{&cfg.RejectedText, "Rejected by @%s."},
		{&cfg.ClosedText, "This request is already resolved or expired."},
		{&cfg.ChangedText, "Parameters changed since the draft was shown. Nothing was done."},
		{&cfg.SameApproverText, "The initiator cannot approve this action. Ask someone else."},
		{&cfg.SuspendedText, "The agent is switched off, nothing was done."},
	}
	for _, d := range defaults {
		if *d.v == "" {
			*d.v = d.def
		}
	}
	if cfg.TypingInterval == 0 {
		cfg.TypingInterval = 4 * time.Second
	}
	if cfg.TypingLimit == 0 {
		cfg.TypingLimit = 10 * time.Minute
	}
	if cfg.Refusal == "" {
		cfg.Refusal = "this agent does not take requests from you here."
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	cfg.CallbackURL = strings.TrimRight(cfg.CallbackURL, "/")
	return &Channel{cfg: cfg}
}

type post struct {
	ChannelID string         `json:"channel_id"`
	RootID    string         `json:"root_id,omitempty"`
	Message   string         `json:"message"`
	Props     map[string]any `json:"props,omitempty"`
}

// Post publishes a message to the target channel or thread.
func (c *Channel) Post(ctx context.Context, m driver.Message) (driver.MessageRef, error) {
	p, err := c.basePost(m)
	if err != nil {
		return driver.MessageRef{}, err
	}
	p.Message = render(m)
	c.stopTyping(m.Target)
	return c.createAs(ctx, c.token(m.Identities), p)
}

// token is the agent's own bot token, or the core's bot token.
func (c *Channel) token(ids map[string]string) string {
	if t := c.cfg.Bots[ids[c.cfg.System]]; t != "" {
		return t
	}
	return c.cfg.Token
}

// AskApproval shows the exact parameters, the deadline and two buttons.
func (c *Channel) AskApproval(ctx context.Context, r driver.ApprovalRequest) (driver.MessageRef, error) {
	p, err := c.basePost(r.Message)
	if err != nil {
		return driver.MessageRef{}, err
	}
	deadline := r.Deadline.UTC().Format("2006-01-02 15:04 UTC")
	if r.Text != "" {
		// the agent's template shows the draft; the fingerprint binds the buttons to exactly that draft
		p.Message = r.Text
	} else {
		params, _ := json.MarshalIndent(json.RawMessage(r.Action), "", "  ")
		p.Message = fmt.Sprintf("**%s** asks for approval. The action runs with exactly these parameters:\n```json\n%s\n```",
			r.Agent, params)
	}
	button := func(id, name, decision string) map[string]any {
		return map[string]any{
			"id": id, "name": name,
			"integration": map[string]any{
				"url": c.cfg.CallbackURL + "/v1/drivers/" + Name + "/actions/" + r.WaitID,
				"context": map[string]string{
					"wait_id": r.WaitID, "fingerprint": r.Fingerprint, "decision": decision,
					"sig": signing.Sign(c.cfg.Secret, r.WaitID, r.Fingerprint, decision),
				},
			},
		}
	}
	p.Props = map[string]any{"attachments": []map[string]any{{
		"text":    c.cfg.ApprovalNote + " " + deadline,
		"actions": []map[string]any{button("approve", c.cfg.ApproveLabel, "approve"), button("reject", c.cfg.RejectLabel, "reject")},
	}}}
	c.stopTyping(r.Target)
	return c.createAs(ctx, c.token(r.Identities), p)
}

func (c *Channel) basePost(m driver.Message) (post, error) {
	ch := m.Target.Channel
	if ch == "" {
		ch = c.cfg.DefaultChannel
	}
	if ch == "" {
		return post{}, fmt.Errorf("mattermost: run %s has no target channel", m.RunID)
	}
	return post{ChannelID: ch, RootID: m.Target.Thread}, nil
}

func (c *Channel) createAs(ctx context.Context, token string, p post) (driver.MessageRef, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return driver.MessageRef{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL+"/api/v4/posts", bytes.NewReader(body))
	if err != nil {
		return driver.MessageRef{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return driver.MessageRef{}, fmt.Errorf("mattermost: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return driver.MessageRef{}, fmt.Errorf("mattermost: HTTP %d: %.200s", resp.StatusCode, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return driver.MessageRef{}, fmt.Errorf("mattermost: decode post: %w", err)
	}
	return driver.MessageRef{System: c.cfg.System, ID: created.ID}, nil
}

// render is the generic presentation of an agent result. Agent-specific templates come later.
func render(m driver.Message) string {
	if m.Text != "" {
		return m.Text
	}
	pretty, err := json.MarshalIndent(json.RawMessage(m.Data), "", "  ")
	if err != nil {
		pretty = m.Data
	}
	return fmt.Sprintf("**%s**\n```json\n%s\n```", m.Agent, pretty)
}
