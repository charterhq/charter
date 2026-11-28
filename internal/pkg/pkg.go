// Package pkg loads and validates agent packages in the Charter v1 format.
//
// A package is a directory:
//
//	agent.yml                       agent card (kind: Agent)
//	persona.md                      role
//	scenarios/<name>/scenario.yml   scenario (kind: Scenario)
//	scenarios/<name>/instructions.md
//	scenarios/<name>/input.schema.json
//	scenarios/<name>/output.schema.json
//	scenarios/<name>/eval/cases/*.yml
//	knowledge/*                     knowledge files named in agent.yml and connections
//	connections/<team>.yml          team connections (kind: Connection)
//	brief.md                        the request in plain words; not part of a version (see Brief)
package pkg

import (
	"encoding/json"
	"fmt"
	"strings"
)

// APIVersion is the package format version this code reads.
const APIVersion = "charter.dev/v1"

// Package is a loaded and validated agent package.
type Package struct {
	Agent       Agent
	Persona     string
	Scenarios   map[string]*Scenario
	Connections map[string]*Connection
	// Warnings are required fields this core does not enforce; they do not stop the package.
	Warnings Problems
	// Knowledge are the knowledge files of the package (knowledge/...) by path.
	Knowledge map[string]string
	// ObjectSchema and ObjectOpener are the files spec.object names, read.
	ObjectSchema json.RawMessage
	ObjectOpener string
}

type Metadata struct {
	Name    string `json:"name"`
	Team    string `json:"team,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Version string `json:"version,omitempty"`
	Status  string `json:"status,omitempty"`
}

type Agent struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Metadata   Metadata  `json:"metadata"`
	Spec       AgentSpec `json:"spec"`
}

type AgentSpec struct {
	Description     string         `json:"description"`
	Goals           []string       `json:"goals,omitempty"`
	Duties          []string       `json:"duties"`
	NotDuties       []string       `json:"not_duties"`
	Persona         string         `json:"persona"`
	PersonaIncludes []string       `json:"persona_includes,omitempty"`
	Model           Model          `json:"model"`
	DataClasses     []string       `json:"data_classes"`
	Identities      []Identity     `json:"identities,omitempty"`
	Tools           []Tool         `json:"tools,omitempty"`
	Skills          []Skill        `json:"skills,omitempty"`
	Knowledge       []Knowledge    `json:"knowledge,omitempty"`
	Budgets         Budgets        `json:"budgets"`
	Memory          string         `json:"memory"`
	Object          *ObjectSpec    `json:"object,omitempty"`
	Scenarios       []ScenarioRef  `json:"scenarios"`
	Fallback        string         `json:"fallback"`
	ReviewEveryDays int            `json:"review_every_days"`
	Exceptions      []Exception    `json:"exceptions,omitempty"`
	Extra           map[string]any `json:"-"`
}

// ObjectSpec describes the business object of an agent with memory: object: the schema of its
// state, the first message of a conversation about it and the stages people see.
type ObjectSpec struct {
	Schema      string           `json:"schema"`
	Opener      string           `json:"opener,omitempty"`
	Stages      []Stage          `json:"stages,omitempty"`
	Transitions []map[string]any `json:"transitions,omitempty"`
}

// Stage is a stage of the object and the artifacts it produces.
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

type Model struct {
	Class  string         `json:"class"`
	Params map[string]any `json:"params,omitempty"`
}

type Identity struct {
	System  string `json:"system"`
	Account string `json:"account"`
}

type Tool struct {
	Ref       string   `json:"ref"`
	Functions []string `json:"functions,omitempty"`
}

type Skill struct {
	Ref     string `json:"ref"`
	Version string `json:"version"`
}

type Knowledge struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	Ref  string `json:"ref,omitempty"`
}

type Budgets struct {
	Timeout         string  `json:"timeout"`
	MaxSteps        int     `json:"max_steps"`
	MaxToolCalls    int     `json:"max_tool_calls,omitempty"`
	MaxTokensPerRun int     `json:"max_tokens_per_run"`
	MaxCostPerRun   float64 `json:"max_cost_per_run,omitempty"`
	MaxRunsPerDay   int     `json:"max_runs_per_day"`
	Concurrency     int     `json:"concurrency"`
}

type ScenarioRef struct {
	Name string         `json:"name"`
	From string         `json:"from,omitempty"`
	With map[string]any `json:"with,omitempty"`
}

type Exception struct {
	Requirement string `json:"requirement"`
	Reason      string `json:"reason"`
	Until       string `json:"until"`
	Owner       string `json:"owner"`
}

type Scenario struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Metadata   Metadata     `json:"metadata"`
	Spec       ScenarioSpec `json:"spec"`

	// Resolved file contents.
	Instructions string          `json:"-"`
	InputSchema  json.RawMessage `json:"-"`
	OutputSchema json.RawMessage `json:"-"`
	Template     string          `json:"-"`
	EvalCases    int             `json:"-"`
	Cases        []Case          `json:"-"`
}

type ScenarioSpec struct {
	Triggers     []Trigger   `json:"triggers"`
	Level        int         `json:"level"`
	InputSchema  string      `json:"input_schema"`
	OutputSchema string      `json:"output_schema"`
	InputsFrom   []InputFrom `json:"inputs_from,omitempty"`
	Instructions string      `json:"instructions"`
	Tools        []string    `json:"tools,omitempty"`
	Autonomy     string      `json:"autonomy"`
	Action       string      `json:"action,omitempty"`
	// ActionInput names the output field that holds the action arguments; without the field in the
	// output there is no action, only the answer. Empty means the whole output is the arguments.
	ActionInput string      `json:"action_input,omitempty"`
	Approval    *Approval   `json:"approval,omitempty"`
	Checks      []string    `json:"checks,omitempty"`
	Idempotency Idempotency `json:"idempotency"`
	Artifacts   []Artifact  `json:"artifacts,omitempty"`
	OnFailure   string      `json:"on_failure"`
	Eval        string      `json:"eval"`
	Feedback    *Feedback   `json:"feedback,omitempty"`
	// Params are the scenario parameters: name -> JSON Schema with an optional default.
	// A parameter without a default must be set by every connection that enables the scenario.
	Params   map[string]json.RawMessage `json:"params,omitempty"`
	Response *Response                  `json:"response,omitempty"`
}

// Response says how the answer is shown to people.
type Response struct {
	Template string `json:"template"`
}

type Trigger struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Filter   string `json:"filter,omitempty"`
	Schedule string `json:"schedule,omitempty"`
}

type InputFrom struct {
	Agent    string `json:"agent"`
	Scenario string `json:"scenario"`
	Field    string `json:"field"`
}

type Approval struct {
	Required         bool     `json:"required"`
	Approvers        []string `json:"approvers,omitempty"`
	Timeout          string   `json:"timeout,omitempty"`
	OnTimeout        string   `json:"on_timeout,omitempty"`
	DistinctApprover bool     `json:"distinct_approver,omitempty"`
}

type Idempotency struct {
	Key      []string `json:"key"`
	OnRepeat string   `json:"on_repeat"`
}

type Artifact struct {
	System  string `json:"system"`
	OnClose string `json:"on_close"`
}

type Feedback struct {
	Unit      string `json:"unit"`
	ItemsPath string `json:"items_path,omitempty"`
}

type Connection struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   Metadata       `json:"metadata"`
	Spec       ConnectionSpec `json:"spec"`

	File string `json:"-"` // path in the package
}

type ConnectionSpec struct {
	Agent     ConnectionAgent              `json:"agent"`
	Scenarios []string                     `json:"scenarios"`
	Triggers  map[string]ConnectionTrigger `json:"triggers,omitempty"`
	Channels  map[string]string            `json:"channels,omitempty"`
	Params    map[string]any               `json:"params,omitempty"`
	Knowledge []Knowledge                  `json:"knowledge,omitempty"`
	Budgets   map[string]any               `json:"budgets,omitempty"`
	Status    string                       `json:"status"`
}

// ConnectionTrigger binds a logical source of the scenarios to this team's events.
// Keys the core does not know stay for drivers that read the package.
type ConnectionTrigger struct {
	// Receiver is the webhook format of an event source: raw (default) or a format driver of the installation.
	Receiver string `json:"receiver,omitempty"`
	// Schedule overrides the cron of a schedule source.
	Schedule string `json:"schedule,omitempty"`
	// Input is the run input of a schedule source.
	Input map[string]any `json:"input,omitempty"`
	// Users may talk to the agent through a message source; with Groups empty too, anyone the channel lets in.
	Users []string `json:"users,omitempty"`
	// Groups are OIDC groups of people who may talk through a message source with receiver web
	// (people of the UI); a person passes if named in Users or a member of one of Groups.
	Groups []string `json:"groups,omitempty"`
	// Channels limit a message source to places of the channel driver (channel IDs); empty means everywhere.
	Channels []string `json:"channels,omitempty"`
}

type ConnectionAgent struct {
	Name     string `json:"name"`
	Versions string `json:"versions"`
}

// Problem is one validation finding, tied to a file of the package.
type Problem struct {
	File string `json:"file"`
	Msg  string `json:"message"`
}

func (p Problem) String() string { return p.File + ": " + p.Msg }

// Problems is a list of findings; a package with problems is not applied.
type Problems []Problem

func (ps Problems) Error() string {
	lines := make([]string, len(ps))
	for i, p := range ps {
		lines[i] = p.String()
	}
	return strings.Join(lines, "\n")
}

func (ps *Problems) add(file, format string, args ...any) {
	*ps = append(*ps, Problem{File: file, Msg: fmt.Sprintf(format, args...)})
}
