// Package driver defines the interfaces through which the core uses external tools.
//
// Interfaces speak the language of the Charter contract, not of any tool:
// a driver translates them for Kestra, Bifrost, Mattermost and so on.
package driver

import (
	"context"
	"encoding/json"
	"time"
)

// StepRequest is one model step of a run.
type StepRequest struct {
	RunID        string
	Agent        string
	Scenario     string
	ModelClass   string
	Instructions string
	Input        json.RawMessage
	OutputSchema json.RawMessage
	Tools        []string
	MaxSteps     int
	// Feedback carries validation errors of the previous attempt, empty on the first one.
	Feedback []string
}

// Usage is what a step consumed.
type Usage struct {
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	CachedTokens    int     `json:"cached_tokens"`
	ReasoningTokens int     `json:"reasoning_tokens"`
	ToolCalls       int     `json:"tool_calls"`
	ToolErrors      int     `json:"tool_errors"`
	Cost            float64 `json:"cost"`
	Model           string  `json:"model,omitempty"`
	Provider        string  `json:"provider,omitempty"`
	FinishReason    string  `json:"finish_reason,omitempty"`
}

// StepResult is the raw model output and its usage.
type StepResult struct {
	Output json.RawMessage
	Usage  Usage
}

// ModelStep runs one model call or a model loop with tools within the step budget.
type ModelStep interface {
	Run(ctx context.Context, req StepRequest) (StepResult, error)
}

// Tools calls a tool on behalf of an agent.
type Tools interface {
	Call(ctx context.Context, agent, tool string, args json.RawMessage) (json.RawMessage, error)
}

// Target says where a message goes: a channel and, if any, a thread in it.
type Target struct {
	System  string `json:"system,omitempty"`
	Channel string `json:"channel,omitempty"`
	Thread  string `json:"thread,omitempty"`
}

// Message is what an agent says to people.
type Message struct {
	RunID      string
	Agent      string
	Connection string
	ObjectKey  string
	Target     Target
	Text       string
	Data       json.RawMessage
}

// MessageRef points at a delivered message.
type MessageRef struct {
	System string
	ID     string
}

// ApprovalRequest shows a draft with exact parameters and a deadline.
type ApprovalRequest struct {
	Message
	WaitID      string
	DraftID     string
	Fingerprint string
	Action      json.RawMessage
	Deadline    time.Time
}

// Channel talks to people.
type Channel interface {
	Post(ctx context.Context, m Message) (MessageRef, error)
	AskApproval(ctx context.Context, r ApprovalRequest) (MessageRef, error)
}

// Journal stores run records.
type Journal interface {
	Write(ctx context.Context, records []json.RawMessage) error
}

// Set is the set of drivers the core runs with.
type Set struct {
	Model   ModelStep
	Tools   Tools
	Channel Channel
	Journal Journal
}
