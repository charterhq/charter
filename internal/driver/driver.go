// Package driver defines the interfaces through which the core uses external tools.
//
// Interfaces speak the language of the Charter contract, not of any tool:
// a driver translates them for Kestra, Bifrost, Mattermost and so on.
// The core never names a tool: packages name logical systems (chat, tracker), the installation
// maps them to drivers, and tool formats (webhooks, button callbacks) stay inside drivers.
package driver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

// Decision errors: the core refuses a decision on an approval for one of these reasons.
var (
	// ErrWaitClosed means the wait is resolved, expired or does not exist.
	ErrWaitClosed = errors.New("wait is not open")
	// ErrFingerprint means the approval does not match the draft parameters.
	ErrFingerprint = errors.New("fingerprint does not match the draft")
	// ErrSameApprover means the initiator tried to approve their own irreversible action.
	ErrSameApprover = errors.New("approver must differ from initiator")
)

// Approvals is the core side of deciding on an approval request; the run engine implements it.
// A channel driver calls it when a person presses a button.
type Approvals interface {
	Resolve(ctx context.Context, waitID, by, fingerprint string, approve bool) error
}

// Callbacks is a driver that takes requests from its tool over HTTP (chat button presses and the like).
// The core serves the handler under /v1/drivers/<Name>/ without a bearer token: the driver checks
// its own signatures.
type Callbacks interface {
	Name() string
	Handler(a Approvals) http.Handler
}

// Event is one launch an event source delivered, in the core's terms.
type Event struct {
	// DeliveryID is unique per delivery: a repeated delivery of the same event is not run twice.
	DeliveryID string
	// Source names what sent the event, for the journal.
	Source string
	// ObjectKey is the business object the event is about: runs on one object share memory and idempotency.
	ObjectKey string
	// Initiator is the sender and a reference on its side (a group of alerts, an issue).
	Initiator, InitiatorRef string
	Payload                 json.RawMessage
	// Reply is where the answer goes, when the source knows it.
	Reply *Target
}

// Format splits a webhook of one tool into events. The connection names the format of an event source
// (connections/*.yml, triggers.<source>.receiver).
type Format interface {
	Name() string
	Split(r *http.Request, body []byte) ([]Event, error)
}

// Set is the set of drivers the core runs with.
type Set struct {
	Model ModelStep
	Tools Tools
	// Channels are the drivers that talk to people, by the logical system agents name in
	// spec.identities and that targets carry (chat).
	Channels map[string]Channel
	Journal  Journal
	// Formats are the webhook formats of event sources besides the built-in raw.
	Formats map[string]Format
}

// Channel returns the driver for the first system that has one, or the only channel driver when
// none of the systems is known (a run without identities in an installation with one chat).
func (s Set) Channel(systems ...string) (Channel, string) {
	for _, sys := range systems {
		if c, ok := s.Channels[sys]; ok && sys != "" {
			return c, sys
		}
	}
	if len(s.Channels) == 1 {
		for sys, c := range s.Channels {
			return c, sys
		}
	}
	return nil, ""
}
