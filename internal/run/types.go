// Package run drives every agent run through the ten steps of STD-0006.
//
// A run is a fixed state machine. Each transition is one transaction that changes
// the run state, records the step and enqueues the next job. After a restart the
// queue picks up unfinished jobs and the run continues from the step it was on.
package run

import (
	"encoding/json"
	"time"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/telemetry"
)

// State is a step of the run state machine.
type State string

const (
	Accepted   State = "accepted"   // 1. accept
	Filtering  State = "filtering"  // 2. filter duplicates, kill switch
	Preparing  State = "preparing"  // 3. assemble input
	Modeling   State = "model"      // 4. call the model
	Validating State = "validating" // 5. validate output
	Checking   State = "checking"   // 6. checks before action
	Approving  State = "approving"  // 7. wait for approval
	Executing  State = "executing"  // 8. execute action
	Responding State = "responding" // 9. respond
	Recording  State = "recording"  // 10. write run record
	Done       State = "done"
)

// Final run statuses (STD-0005, section 4).
const (
	StatusOK             = "ok"
	StatusInvalid        = "invalid"
	StatusFailed         = "failed"
	StatusSkipped        = "skipped"
	StatusBudgetExceeded = "budget_exceeded"
	StatusSuspended      = "suspended"
	StatusCancelled      = "cancelled"
)

// Envelope is the launch envelope (STD-0006, section 4).
type Envelope struct {
	DeliveryID string     `json:"delivery_id"`
	Trigger    Trigger    `json:"trigger"`
	Connection string     `json:"connection"`
	Object     Object     `json:"object"`
	Initiator  Initiator  `json:"initiator"`
	ReceivedAt time.Time  `json:"received_at"`
	Deadline   *time.Time `json:"deadline,omitempty"`
	// Reply is where the answer goes when the launch has a place to answer in (a thread, a chat).
	Reply *driver.Target `json:"reply,omitempty"`
	// Shadow: the connection is in shadow, the answer goes only to its shadow channel, if any.
	Shadow bool `json:"shadow,omitempty"`
	// Conversation is set for message triggers: the message and the conversation it belongs to.
	Conversation *Conversation   `json:"conversation,omitempty"`
	Payload      json.RawMessage `json:"payload"`
	// Trace is fixed at accept time; see package telemetry.
	Trace telemetry.Trace `json:"trace"`
}

type Trigger struct {
	Type   string `json:"type"` // event, schedule, message, call, input_change
	Source string `json:"source"`
	// Schedule is the cron of a schedule trigger in a package scenario; empty in an envelope.
	Schedule string `json:"schedule,omitempty"`
}

// Conversation is a message in a conversation, as a channel driver hands it to the core.
// The core does not know channels: a conversation ID is whatever the driver keeps stable,
// a Mattermost thread, a chat, a ticket.
type Conversation struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
	Author    string `json:"author"`
	Text      string `json:"text"`
}

type Object struct {
	Key string `json:"key,omitempty"`
	// ID is set when the object is a business object of the core (memory: object).
	ID string `json:"id,omitempty"`
}

type Initiator struct {
	Kind string `json:"kind"` // human, system, agent
	ID   string `json:"id,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

// Scenario is the part of a package scenario the run engine needs.
// The full v1 package format is loaded by the registry.
type Scenario struct {
	Name         string          `json:"name"`
	Level        int             `json:"level"`
	ModelClass   string          `json:"model_class"`
	ModelParams  map[string]any  `json:"model_params,omitempty"`
	Instructions string          `json:"instructions"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	Tools        []string        `json:"tools,omitempty"`
	MaxSteps     int             `json:"max_steps,omitempty"`
	MaxToolCalls int             `json:"max_tool_calls,omitempty"`
	Autonomy     string          `json:"autonomy"` // advise, act
	Action       string          `json:"action,omitempty"`
	// ActionInput is the output field with the action arguments; empty means the whole output.
	ActionInput string    `json:"action_input,omitempty"`
	Approval    *Approval `json:"approval,omitempty"`
	Idempotency string    `json:"idempotency,omitempty"` // update, skip
	// Template renders the answer for people (Go text/template, see package render).
	Template string `json:"template,omitempty"`
	// Triggers are the logical launch sources of the scenario; a connection binds them to real events.
	Triggers []Trigger `json:"triggers,omitempty"`
	// Memory is the agent's memory kind: none, thread (conversation history goes into the input), object.
	Memory string `json:"memory,omitempty"`
	// Params are the parameter defaults; a run sees them merged with its connection's values.
	Params map[string]any `json:"params,omitempty"`
	// Knowledge are the agent's knowledge files; a run adds its connection's files.
	Knowledge []Doc `json:"knowledge,omitempty"`
}

// Doc is a knowledge file that goes into the model context.
type Doc struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// Approval configures step 7.
type Approval struct {
	Timeout   Duration `json:"timeout"`
	OnTimeout string   `json:"on_timeout"` // cancel, fallback
	// DistinctApprover requires the approver to differ from the initiator (STD-0006, I6).
	DistinctApprover bool `json:"distinct_approver,omitempty"`
}

// Package is the stored agent package as the engine reads it.
type Package struct {
	Scenarios map[string]Scenario `json:"scenarios"`
	// Object is the business object of an agent with memory: object.
	Object *ObjectSpec `json:"object,omitempty"`
	// Connections are the teams that use the agent. A package without connections is from before
	// connections were stored: launches name any connection and nothing is scheduled.
	Connections map[string]Connection `json:"connections,omitempty"`
	// Agent is the card as written; the engine reads its identities.
	Agent struct {
		Spec struct {
			Identities []Identity `json:"identities,omitempty"`
		} `json:"spec"`
	} `json:"agent"`
}

// Identity is an account of the agent in a system.
type Identity struct {
	System  string `json:"system"`
	Account string `json:"account"`
}

// Connection is a team's use of the agent (STD-0003, section 7) as the engine applies it.
type Connection struct {
	Name      string   `json:"name"`
	Team      string   `json:"team"`
	Owner     string   `json:"owner"`
	Scenarios []string `json:"scenarios"`
	// Status: shadow (answers go to the shadow channel only), active, paused (no launches).
	Status string `json:"status"`
	// Triggers bind logical sources of the scenarios to this team's events. A source the connection
	// does not list does not launch the agent for this team.
	Triggers map[string]ConnectionTrigger `json:"triggers,omitempty"`
	// Channels name where the agent writes: "reply" when the launch has no place to answer in,
	// "shadow" while the connection is in shadow. Values are channel IDs of the channel driver.
	Channels map[string]string `json:"channels,omitempty"`
	Params   map[string]any    `json:"params,omitempty"`
	// Knowledge are the team's knowledge files for this connection.
	Knowledge []Doc `json:"knowledge,omitempty"`
	// ScenarioParams are the parameters each enabled scenario runs with for this team:
	// the scenario defaults with the connection's values over them.
	ScenarioParams map[string]map[string]any `json:"scenario_params,omitempty"`
}

// ConnectionTrigger is the team side of a logical source.
type ConnectionTrigger struct {
	// Receiver is the webhook format of an event source: raw (default) or a format driver of the installation.
	// For a message source, ReceiverWeb means the core's own API receives the messages (conversations
	// about objects); empty means the channel drivers of the agent identities.
	Receiver string `json:"receiver,omitempty"`
	// Schedule overrides the cron of a schedule source.
	Schedule string `json:"schedule,omitempty"`
	// Input is the run input of a schedule source.
	Input json.RawMessage `json:"input,omitempty"`
	// Users may talk to the agent through a message source; empty means anyone the channel lets in.
	Users []string `json:"users,omitempty"`
	// Channels limit a message source to places of the channel driver (channel IDs); empty means everywhere.
	Channels []string `json:"channels,omitempty"`
}

// ReceiverWeb is the receiver of a message source served by the core API, not by a channel driver.
const ReceiverWeb = "web"

// ObjectSpec is the business object of an agent: the schema of its state, the opener template of a
// conversation and the stages people see.
type ObjectSpec struct {
	// Schema is the JSON Schema of the state, kept as text: the stored package is jsonb, which
	// reorders keys, and the order of the fields is the order people see them in.
	Schema string  `json:"schema"`
	Opener string  `json:"opener,omitempty"`
	Stages []Stage `json:"stages,omitempty"`
}

// Stage is a stage of an object and the artifacts it produces.
type Stage struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Artifacts []ArtifactSlot `json:"artifacts,omitempty"`
}

// ArtifactSlot is an artifact a stage produces.
type ArtifactSlot struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

// Connection statuses.
const (
	ConnShadow = "shadow"
	ConnActive = "active"
	ConnPaused = "paused"
)

// enables reports whether the connection runs the scenario.
func (c Connection) enables(scenario string) bool {
	for _, s := range c.Scenarios {
		if s == scenario {
			return true
		}
	}
	return false
}

// Duration is a time.Duration encoded as a Go duration string ("1h30m").
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}
