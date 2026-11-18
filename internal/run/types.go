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
	Reply   *driver.Target  `json:"reply,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

type Trigger struct {
	Type   string `json:"type"` // event, schedule, message, call, input_change
	Source string `json:"source"`
}

type Object struct {
	Key string `json:"key,omitempty"`
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
	Instructions string          `json:"instructions"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	Tools        []string        `json:"tools,omitempty"`
	MaxSteps     int             `json:"max_steps,omitempty"`
	Autonomy     string          `json:"autonomy"` // advise, act
	Action       string          `json:"action,omitempty"`
	Approval     *Approval       `json:"approval,omitempty"`
	Idempotency  string          `json:"idempotency,omitempty"` // update, skip
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
