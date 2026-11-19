// Package stub provides in-memory drivers for tests and local runs without external tools.
package stub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/charterhq/charter/internal/driver"
)

// Model returns outputs from a script, one per call; the last one repeats.
type Model struct {
	mu      sync.Mutex
	Outputs []string
	// Block, when set, makes the first call wait until the context is cancelled.
	Block bool
	Calls int
}

func (m *Model) Run(ctx context.Context, req driver.StepRequest) (driver.StepResult, error) {
	m.mu.Lock()
	m.Calls++
	n := m.Calls
	block := m.Block && n == 1
	out := "{}"
	if len(m.Outputs) > 0 {
		out = m.Outputs[min(n, len(m.Outputs))-1]
	}
	m.mu.Unlock()
	if block {
		<-ctx.Done()
		return driver.StepResult{}, ctx.Err()
	}
	return driver.StepResult{
		Output: json.RawMessage(out),
		Usage:  driver.Usage{InputTokens: 100, OutputTokens: 20, Model: "stub", Provider: "stub", FinishReason: "stop"},
	}, nil
}

// CallCount returns how many times the model was called.
func (m *Model) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Calls
}

// Tools records calls and returns a fixed reference.
type Tools struct {
	mu    sync.Mutex
	Calls []string
}

func (t *Tools) Call(_ context.Context, agent, tool string, _ json.RawMessage) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Calls = append(t.Calls, agent+"/"+tool)
	return json.RawMessage(fmt.Sprintf(`{"ref":"stub-%d"}`, len(t.Calls))), nil
}

// Channel records posts and approval requests.
type Channel struct {
	mu        sync.Mutex
	Posts     []driver.Message
	Approvals []driver.ApprovalRequest
}

func (c *Channel) Post(_ context.Context, m driver.Message) (driver.MessageRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Posts = append(c.Posts, m)
	return driver.MessageRef{System: "stub", ID: fmt.Sprintf("post-%d", len(c.Posts))}, nil
}

func (c *Channel) AskApproval(_ context.Context, r driver.ApprovalRequest) (driver.MessageRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Approvals = append(c.Approvals, r)
	return driver.MessageRef{System: "stub", ID: fmt.Sprintf("approval-%d", len(c.Approvals))}, nil
}

// LastApproval returns the latest approval request, if any.
func (c *Channel) LastApproval() (driver.ApprovalRequest, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.Approvals) == 0 {
		return driver.ApprovalRequest{}, false
	}
	return c.Approvals[len(c.Approvals)-1], true
}

// PostCount returns how many messages were posted.
func (c *Channel) PostCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.Posts)
}

// Journal keeps records in memory.
type Journal struct {
	mu      sync.Mutex
	Records []json.RawMessage
}

func (j *Journal) Write(_ context.Context, records []json.RawMessage) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Records = append(j.Records, records...)
	return nil
}

// Set returns a full set of stub drivers.
func Set(m *Model) (driver.Set, *Tools, *Channel, *Journal) {
	t, c, j := &Tools{}, &Channel{}, &Journal{}
	return driver.Set{Model: m, Tools: t, Channel: c, Journal: j}, t, c, j
}

// Count returns how many records were written.
func (j *Journal) Count() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.Records)
}

// CallCount returns how many tool calls were made.
func (t *Tools) CallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.Calls)
}

// LastPost returns the latest posted message, if any.
func (c *Channel) LastPost() (driver.Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.Posts) == 0 {
		return driver.Message{}, false
	}
	return c.Posts[len(c.Posts)-1], true
}
