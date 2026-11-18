// Package mattermost talks to people through Mattermost as a bot.
package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
}

// Channel is a driver.Channel backed by Mattermost.
type Channel struct {
	cfg Config
}

// New returns a Mattermost channel driver.
func New(cfg Config) *Channel {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
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
	return c.create(ctx, p)
}

// AskApproval shows the exact parameters, the deadline and two buttons.
func (c *Channel) AskApproval(ctx context.Context, r driver.ApprovalRequest) (driver.MessageRef, error) {
	p, err := c.basePost(r.Message)
	if err != nil {
		return driver.MessageRef{}, err
	}
	params, _ := json.MarshalIndent(json.RawMessage(r.Action), "", "  ")
	p.Message = fmt.Sprintf("**%s** asks for approval. The action runs with exactly these parameters:\n```json\n%s\n```\nDeadline: %s",
		r.Agent, params, r.Deadline.UTC().Format("2006-01-02 15:04 UTC"))
	button := func(id, name, decision string) map[string]any {
		return map[string]any{
			"id": id, "name": name,
			"integration": map[string]any{
				"url": c.cfg.CallbackURL + "/v1/waits/" + r.WaitID + "/resolve",
				"context": map[string]string{
					"wait_id": r.WaitID, "fingerprint": r.Fingerprint, "decision": decision,
					"sig": signing.Sign(c.cfg.Secret, r.WaitID, r.Fingerprint, decision),
				},
			},
		}
	}
	p.Props = map[string]any{"attachments": []map[string]any{{
		"actions": []map[string]any{button("approve", "Approve", "approve"), button("reject", "Reject", "reject")},
	}}}
	return c.create(ctx, p)
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

func (c *Channel) create(ctx context.Context, p post) (driver.MessageRef, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return driver.MessageRef{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL+"/api/v4/posts", bytes.NewReader(body))
	if err != nil {
		return driver.MessageRef{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
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
	return driver.MessageRef{System: "mattermost", ID: created.ID}, nil
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
