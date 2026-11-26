// Package driver defines the interfaces through which the core uses external tools.
//
// Interfaces speak the language of the Charter contract, not of any tool:
// a driver translates them for Kestra, Bifrost, Mattermost and so on.
package driver

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// StepRequest is one model step of a run.
type StepRequest struct {
	RunID      string
	Agent      string
	Scenario   string
	ModelClass string
	// Params are the agent's model parameters from the package (temperature, reasoning, output limit).
	Params       map[string]any
	Instructions string
	Input        json.RawMessage
	OutputSchema json.RawMessage
	// Tools the step may call, as "ref.function" from the package; empty for a single model call.
	Tools        []string
	MaxSteps     int
	MaxToolCalls int
	// Feedback carries validation errors of the previous attempt, empty on the first one.
	Feedback []string
	// Traceparent links calls made by the driver to the run trace (W3C header value).
	Traceparent string
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

// ToolDef describes a tool to the model: the name the model calls it by and its argument schema.
type ToolDef struct {
	// Name is the package name "ref.function".
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// Tools lists and calls tools on behalf of an agent. Names are package names "ref.function";
// the driver maps them to its gateway and calls them with the agent's own credentials.
type Tools interface {
	List(ctx context.Context, agent string, names []string) ([]ToolDef, error)
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
	// Identities are the agent's accounts by system (spec.identities): a chat driver posts as the
	// agent's own bot when it has its credentials.
	Identities map[string]string
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

// Incoming is a message a channel driver received for an agent.
type Incoming struct {
	Agent, Connection, Source string
	Conversation              string
	MessageID                 string
	Author                    string
	Text                      string
	Reply                     Target
}

// Listener says that an agent hears messages through an account of a system: a channel driver
// listens as Account and hands messages to Agent under Connection and Source.
type Listener struct {
	System, Account           string
	Agent, Connection, Source string
	// Places limits the listener to channels of the driver; empty means every place the account is in.
	Places []string
}

// ErrForbidden means the author may not talk to the agent through this connection.
var ErrForbidden = errors.New("author may not talk to the agent through this connection")

// Inbox is the core side of receiving messages; the run engine implements it.
type Inbox interface {
	Listeners(ctx context.Context) ([]Listener, error)
	Converse(ctx context.Context, m Incoming) (runID string, duplicate bool, err error)
}

// Receiver is a channel driver that also receives messages. Receive blocks until ctx is done.
type Receiver interface {
	Receive(ctx context.Context, inbox Inbox) error
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
