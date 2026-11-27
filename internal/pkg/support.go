package pkg

import (
	"fmt"
	"sort"
	"strings"
)

// Support says what the core does with a field of the v1 format.
//
// The format (STD-0003) is ahead of the core: a field the core does not execute must not pass
// validation silently, or an agent behaves differently from its card. Fields are therefore one of:
type Support string

const (
	// Executed: the core does what the field says.
	Executed Support = "executed"
	// Described: the field is for people and review, the core keeps it in the card and acts on nothing.
	Described Support = "described"
	// Unenforced: the format requires the field, the core does not enforce it yet. Validation warns.
	Unenforced Support = "unenforced"
	// Unsupported: the core does not execute the field. A package that sets it is not applied.
	Unsupported Support = "unsupported"
)

// FieldSupport is one row of the support matrix.
type FieldSupport struct {
	Kind    string  // Agent, Scenario, Connection
	Path    string  // field path in the document, values after "=" (memory=object)
	Support Support // what the core does
	Note    string
}

// Fields is the support matrix of this core. A new field of the format is added here with its support;
// TestFieldsCoverFormat fails while a field of the package types is missing.
var Fields = []FieldSupport{
	{"Agent", "metadata.name", Executed, ""},
	{"Agent", "metadata.team", Executed, "journal"},
	{"Agent", "metadata.owner", Executed, "journal"},
	{"Agent", "metadata.version", Executed, "a published version is immutable by its source files"},
	{"Agent", "metadata.status", Described, "connection status decides how runs go; draft, shadow or retired with an active connection is a problem"},
	{"Agent", "spec.description", Described, ""},
	{"Agent", "spec.goals", Described, ""},
	{"Agent", "spec.duties", Described, ""},
	{"Agent", "spec.not_duties", Described, ""},
	{"Agent", "spec.persona", Executed, "goes to the model before the scenario instructions"},
	{"Agent", "spec.persona_includes", Unsupported, "not read"},
	{"Agent", "spec.model.class", Executed, ""},
	{"Agent", "spec.model.params", Executed, ""},
	{"Agent", "spec.data_classes", Unenforced, "no model provider choice or masking by data class"},
	{"Agent", "spec.identities", Executed, ""},
	{"Agent", "spec.tools", Executed, "MCP gateway, functions limit what the model and the action may call"},
	{"Agent", "spec.skills", Unsupported, "not given to the model"},
	{"Agent", "spec.knowledge", Unsupported, "not given to the model"},
	{"Agent", "spec.budgets.timeout", Unenforced, "a step is limited by the core step timeout, not by the run budget"},
	{"Agent", "spec.budgets.max_steps", Executed, "model turns per run"},
	{"Agent", "spec.budgets.max_tool_calls", Executed, ""},
	{"Agent", "spec.budgets.max_tokens_per_run", Unenforced, "tokens are logged, a run is not stopped by them"},
	{"Agent", "spec.budgets.max_cost_per_run", Unsupported, "cost is logged, a run is not stopped by it"},
	{"Agent", "spec.budgets.max_runs_per_day", Unenforced, "not counted"},
	{"Agent", "spec.budgets.concurrency", Unenforced, "not limited"},
	{"Agent", "spec.memory=none", Executed, ""},
	{"Agent", "spec.memory=thread", Executed, "conversation history goes into the input"},
	{"Agent", "spec.memory=object", Unsupported, "no object state"},
	{"Agent", "spec.scenarios.name", Executed, ""},
	{"Agent", "spec.scenarios.from", Unsupported, "no scenario templates"},
	{"Agent", "spec.scenarios.with", Unsupported, "no scenario templates"},
	{"Agent", "spec.fallback", Described, ""},
	{"Agent", "spec.review_every_days", Described, ""},
	{"Agent", "spec.exceptions", Described, ""},

	{"Scenario", "metadata.name", Executed, ""},
	{"Scenario", "metadata.description", Described, ""},
	{"Scenario", "spec.triggers.type=event", Executed, "team webhook of the connection"},
	{"Scenario", "spec.triggers.type=schedule", Executed, "Moscow time"},
	{"Scenario", "spec.triggers.type=message", Executed, "channel driver, conversation per thread"},
	{"Scenario", "spec.triggers.type=call", Executed, ""},
	{"Scenario", "spec.triggers.type=input_change", Unsupported, "no launch on a change of another run's output"},
	{"Scenario", "spec.triggers.source", Executed, "bound by the connection"},
	{"Scenario", "spec.triggers.filter", Described, "the source filters events"},
	{"Scenario", "spec.triggers.schedule", Executed, ""},
	{"Scenario", "spec.level", Executed, "level 1 cannot use tools"},
	{"Scenario", "spec.input_schema", Executed, "the input is checked before the model"},
	{"Scenario", "spec.output_schema", Executed, "one retry with the validation error"},
	{"Scenario", "spec.inputs_from", Unsupported, "no inputs from other scenarios"},
	{"Scenario", "spec.instructions", Executed, ""},
	{"Scenario", "spec.tools", Executed, ""},
	{"Scenario", "spec.autonomy", Executed, "advise never runs an action"},
	{"Scenario", "spec.action", Executed, "runs once, after checks and approval"},
	{"Scenario", "spec.action_input", Executed, "no field in the output means an answer without an action"},
	{"Scenario", "spec.approval.required", Executed, ""},
	{"Scenario", "spec.approval.approvers", Unsupported, "anyone the channel lets press the button approves"},
	{"Scenario", "spec.approval.timeout", Executed, ""},
	{"Scenario", "spec.approval.on_timeout=cancel", Executed, ""},
	{"Scenario", "spec.approval.on_timeout=fallback", Unsupported, "a timeout always cancels"},
	{"Scenario", "spec.approval.distinct_approver", Executed, ""},
	{"Scenario", "spec.checks", Unsupported, "step 6 only picks the road"},
	{"Scenario", "spec.idempotency.key", Unenforced, "the core keys by object, scenario and input fingerprint"},
	{"Scenario", "spec.idempotency.on_repeat", Executed, ""},
	{"Scenario", "spec.artifacts", Unsupported, "actions are recorded, nothing is done on object close"},
	{"Scenario", "spec.on_failure=requester", Executed, ""},
	{"Scenario", "spec.on_failure=owner", Unsupported, "failures go to the place the run answers in"},
	{"Scenario", "spec.eval", Executed, "agentctl eval runs the cases through the core steps, without the action and the answer"},
	{"Scenario", "spec.feedback", Unsupported, "no feedback collection"},
	{"Scenario", "spec.params", Unsupported, "not given to the run"},
	{"Scenario", "spec.response", Executed, ""},

	{"Connection", "metadata.name", Executed, ""},
	{"Connection", "metadata.team", Executed, "journal"},
	{"Connection", "metadata.owner", Described, ""},
	{"Connection", "spec.agent.name", Executed, ""},
	{"Connection", "spec.agent.versions", Described, "the connection ships in the package and binds its version"},
	{"Connection", "spec.scenarios", Executed, ""},
	{"Connection", "spec.triggers", Executed, "receiver, schedule, input, users, channels"},
	{"Connection", "spec.channels", Executed, "reply and shadow"},
	{"Connection", "spec.params", Unsupported, "not given to the run"},
	{"Connection", "spec.knowledge", Unsupported, "not given to the model"},
	{"Connection", "spec.budgets", Unsupported, "no per-connection limits"},
	{"Connection", "spec.status", Executed, ""},
}

func support(kind, path string) FieldSupport {
	for _, f := range Fields {
		if f.Kind == kind && f.Path == path {
			return f
		}
	}
	panic("pkg: no support row for " + kind + " " + path)
}

// checkSupport reports fields the core does not execute: set unsupported fields are problems,
// required fields the core does not enforce are warnings.
func checkSupport(p *Package, ps, ws *Problems) {
	unsupported := func(file, kind, path string, set bool) {
		if set {
			f := support(kind, path)
			ps.add(file, "%s is not supported by this core: %s", path, f.Note)
		}
	}
	unenforced := func(file, kind, path string, set bool) {
		if set {
			f := support(kind, path)
			ws.add(file, "%s is not enforced by this core: %s", path, f.Note)
		}
	}

	a, file := p.Agent.Spec, "agent.yml"
	unsupported(file, "Agent", "spec.persona_includes", len(a.PersonaIncludes) > 0)
	unsupported(file, "Agent", "spec.skills", len(a.Skills) > 0)
	unsupported(file, "Agent", "spec.knowledge", len(a.Knowledge) > 0)
	unsupported(file, "Agent", "spec.budgets.max_cost_per_run", a.Budgets.MaxCostPerRun > 0)
	unsupported(file, "Agent", "spec.memory=object", a.Memory == "object")
	unenforced(file, "Agent", "spec.data_classes", len(a.DataClasses) > 0)
	unenforced(file, "Agent", "spec.budgets.timeout", a.Budgets.Timeout != "")
	unenforced(file, "Agent", "spec.budgets.max_tokens_per_run", a.Budgets.MaxTokensPerRun > 0)
	unenforced(file, "Agent", "spec.budgets.max_runs_per_day", a.Budgets.MaxRunsPerDay > 0)
	unenforced(file, "Agent", "spec.budgets.concurrency", a.Budgets.Concurrency > 0)
	for _, ref := range a.Scenarios {
		unsupported(file, "Agent", "spec.scenarios.with", len(ref.With) > 0)
	}
	if st := p.Agent.Metadata.Status; st != "" && st != "active" {
		for _, name := range sortedKeys(p.Connections) {
			if p.Connections[name].Spec.Status == "active" {
				ps.add(file, "metadata.status is %s but connection %s is active: the core runs by connection status", st, name)
			}
		}
	}

	for _, name := range sortedKeys(p.Scenarios) {
		s, file := p.Scenarios[name].Spec, "scenarios/"+name+"/scenario.yml"
		for _, t := range s.Triggers {
			unsupported(file, "Scenario", "spec.triggers.type=input_change", t.Type == "input_change")
		}
		unsupported(file, "Scenario", "spec.inputs_from", len(s.InputsFrom) > 0)
		if s.Approval != nil {
			unsupported(file, "Scenario", "spec.approval.approvers", len(s.Approval.Approvers) > 0)
			unsupported(file, "Scenario", "spec.approval.on_timeout=fallback", s.Approval.OnTimeout == "fallback")
		}
		unsupported(file, "Scenario", "spec.checks", len(s.Checks) > 0)
		unsupported(file, "Scenario", "spec.artifacts", len(s.Artifacts) > 0)
		unsupported(file, "Scenario", "spec.on_failure=owner", s.OnFailure == "owner")
		unsupported(file, "Scenario", "spec.feedback", s.Feedback != nil)
		unsupported(file, "Scenario", "spec.params", len(s.Params) > 0)
		unenforced(file, "Scenario", "spec.idempotency.key", len(s.Idempotency.Key) > 0)
	}

	for _, name := range sortedKeys(p.Connections) {
		c, file := p.Connections[name].Spec, p.Connections[name].File
		unsupported(file, "Connection", "spec.params", len(c.Params) > 0)
		unsupported(file, "Connection", "spec.knowledge", len(c.Knowledge) > 0)
		unsupported(file, "Connection", "spec.budgets", len(c.Budgets) > 0)
	}
}

// SupportTable renders the support matrix as a Markdown table.
func SupportTable() string {
	var b strings.Builder
	b.WriteString("| Kind | Field | Support | Note |\n|---|---|---|---|\n")
	for _, f := range Fields {
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n", f.Kind, f.Path, f.Support, f.Note)
	}
	return b.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
