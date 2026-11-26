// Package mcp calls agent tools through an MCP gateway (Streamable HTTP).
//
// A package names a tool "ref.function": ref is the MCP server, function is its tool.
// The gateway exposes server tools as "<ref><Sep><function>" (Bifrost uses "-").
// Each agent connects with its own gateway key, so the gateway decides what the agent may see and call.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/charterhq/charter/internal/driver"
)

// Config of the driver.
type Config struct {
	URL string
	// Keys maps an agent to its gateway key; "*" is the key for agents without their own.
	Keys map[string]string
	// Sep joins server and tool in gateway tool names; "-" when empty.
	Sep string
	// ListTTL is how long a tool list is reused; 5 minutes when zero.
	ListTTL time.Duration
	// CallTimeout bounds one tool call; 2 minutes when zero.
	CallTimeout time.Duration
	// MaxResultBytes cuts a tool result before it goes back to the model; 64 KiB when zero.
	MaxResultBytes int
	HTTP           *http.Client
	Version        string
}

// Tools is a driver.Tools backed by an MCP gateway.
type Tools struct {
	cfg Config

	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	cs     *sdk.ClientSession
	tools  []*sdk.Tool
	listed time.Time
}

// New returns the driver. Sessions open lazily, one per agent.
func New(cfg Config) *Tools {
	if cfg.Sep == "" {
		cfg.Sep = "-"
	}
	if cfg.ListTTL == 0 {
		cfg.ListTTL = 5 * time.Minute
	}
	if cfg.CallTimeout == 0 {
		cfg.CallTimeout = 2 * time.Minute
	}
	if cfg.MaxResultBytes == 0 {
		cfg.MaxResultBytes = 64 << 10
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{}
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	return &Tools{cfg: cfg, sessions: map[string]*session{}}
}

// ErrNoKey means the agent has no gateway key and there is no default one.
var ErrNoKey = errors.New("mcp: no gateway key for agent")

func (t *Tools) key(agent string) (string, error) {
	if k, ok := t.cfg.Keys[agent]; ok && k != "" {
		return k, nil
	}
	if k, ok := t.cfg.Keys["*"]; ok && k != "" {
		return k, nil
	}
	return "", fmt.Errorf("%w %q", ErrNoKey, agent)
}

type bearer struct {
	key  string
	base http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.key)
	return b.base.RoundTrip(r)
}

// session returns the agent's open session, connecting if needed. Caller holds no lock.
func (t *Tools) session(ctx context.Context, agent string) (*session, error) {
	t.mu.Lock()
	s := t.sessions[agent]
	t.mu.Unlock()
	if s != nil {
		return s, nil
	}
	key, err := t.key(agent)
	if err != nil {
		return nil, err
	}
	base := t.cfg.HTTP.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	hc := *t.cfg.HTTP
	hc.Transport = bearer{key: key, base: base}
	client := sdk.NewClient(&sdk.Implementation{Name: "charter", Version: t.cfg.Version}, nil)
	cs, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: t.cfg.URL, HTTPClient: &hc, MaxRetries: 1, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: connect for %s: %w", agent, err)
	}
	s = &session{cs: cs}
	t.mu.Lock()
	if old := t.sessions[agent]; old != nil {
		t.mu.Unlock()
		_ = cs.Close()
		return old, nil
	}
	t.sessions[agent] = s
	t.mu.Unlock()
	return s, nil
}

// drop forgets a broken session so the next call reconnects.
func (t *Tools) drop(agent string, s *session) {
	t.mu.Lock()
	if t.sessions[agent] == s {
		delete(t.sessions, agent)
	}
	t.mu.Unlock()
	_ = s.cs.Close()
}

// retry runs f on the agent's session and once more on a fresh one if the first attempt
// failed at the transport level (gateway restart, expired session).
func (t *Tools) retry(ctx context.Context, agent string, f func(*session) error) error {
	s, err := t.session(ctx, agent)
	if err != nil {
		return err
	}
	if err = f(s); err == nil || ctx.Err() != nil || !transportError(err) {
		return err
	}
	t.drop(agent, s)
	if s, err = t.session(ctx, agent); err != nil {
		return err
	}
	return f(s)
}

// transportError says the session is gone and a fresh one may succeed.
func transportError(err error) bool {
	return errors.Is(err, sdk.ErrConnectionClosed) || errors.Is(err, sdk.ErrSessionMissing)
}

func (t *Tools) gatewayTools(ctx context.Context, agent string) ([]*sdk.Tool, error) {
	var out []*sdk.Tool
	err := t.retry(ctx, agent, func(s *session) error {
		t.mu.Lock()
		fresh := s.tools != nil && time.Since(s.listed) < t.cfg.ListTTL
		cached := s.tools
		t.mu.Unlock()
		if fresh {
			out = cached
			return nil
		}
		var all []*sdk.Tool
		params := &sdk.ListToolsParams{}
		for {
			res, err := s.cs.ListTools(ctx, params)
			if err != nil {
				return err
			}
			all = append(all, res.Tools...)
			if res.NextCursor == "" {
				break
			}
			params = &sdk.ListToolsParams{Cursor: res.NextCursor}
		}
		t.mu.Lock()
		s.tools, s.listed = all, time.Now()
		t.mu.Unlock()
		out = all
		return nil
	})
	return out, err
}

// gatewayName maps "ref.function" to the gateway tool name.
func (t *Tools) gatewayName(name string) string {
	ref, fn, _ := strings.Cut(name, ".")
	return ref + t.cfg.Sep + fn
}

// List returns the tools the agent may call: every requested "ref.function" the gateway shows
// to the agent's key, and every function of a server requested as "ref.*".
// A requested function the gateway does not show is an error: the package and the key disagree.
func (t *Tools) List(ctx context.Context, agent string, names []string) ([]driver.ToolDef, error) {
	gw, err := t.gatewayTools(ctx, agent)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*sdk.Tool, len(gw))
	for _, tl := range gw {
		byName[tl.Name] = tl
	}
	var out []driver.ToolDef
	var missing []string
	seen := map[string]bool{}
	add := func(pkgName string, tl *sdk.Tool) {
		if seen[pkgName] {
			return
		}
		seen[pkgName] = true
		schema, _ := json.Marshal(tl.InputSchema)
		if len(schema) == 0 || string(schema) == "null" {
			schema = []byte(`{"type":"object"}`)
		}
		out = append(out, driver.ToolDef{Name: pkgName, Description: tl.Description, InputSchema: schema})
	}
	for _, n := range names {
		ref, fn, _ := strings.Cut(n, ".")
		if fn == "*" {
			prefix := ref + t.cfg.Sep
			found := false
			for _, tl := range gw {
				if rest, ok := strings.CutPrefix(tl.Name, prefix); ok {
					add(ref+"."+rest, tl)
					found = true
				}
			}
			if !found {
				missing = append(missing, n)
			}
			continue
		}
		if tl, ok := byName[t.gatewayName(n)]; ok {
			add(n, tl)
		} else {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return out, fmt.Errorf("mcp: gateway does not show these tools to agent %s: %s", agent, strings.Join(missing, ", "))
	}
	return out, nil
}

// ToolError is a tool that ran and reported a failure; the model sees it and may try otherwise.
type ToolError struct{ Text string }

func (e *ToolError) Error() string { return "tool error: " + e.Text }

// Call runs a tool and returns its result as JSON: structured content when the tool gives it,
// otherwise its text joined, as a JSON string.
func (t *Tools) Call(ctx context.Context, agent, tool string, args json.RawMessage) (json.RawMessage, error) {
	var a map[string]any
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, &ToolError{Text: "arguments are not a JSON object: " + err.Error()}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, t.cfg.CallTimeout)
	defer cancel()
	var res *sdk.CallToolResult
	err := t.retry(ctx, agent, func(s *session) error {
		var err error
		res, err = s.cs.CallTool(ctx, &sdk.CallToolParams{Name: t.gatewayName(tool), Arguments: a})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: %w", tool, err)
	}
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if res.IsError {
		return nil, &ToolError{Text: clip(text, 2000)}
	}
	if res.StructuredContent != nil {
		if b, err := json.Marshal(res.StructuredContent); err == nil && len(b) <= t.cfg.MaxResultBytes {
			return b, nil
		}
	}
	b, _ := json.Marshal(clip(text, t.cfg.MaxResultBytes))
	return b, nil
}

// Close ends all sessions.
func (t *Tools) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for agent, s := range t.sessions {
		_ = s.cs.Close()
		delete(t.sessions, agent)
	}
	return nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n[cut: %d of %d bytes]", cut, len(s))
}
