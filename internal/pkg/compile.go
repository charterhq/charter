package pkg

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charterhq/charter/internal/run"
)

// Stored is what the registry keeps for an agent version: the card as written
// and the scenarios in the form the run engine executes.
type Stored struct {
	Agent       Agent                     `json:"agent"`
	Persona     string                    `json:"persona"`
	Scenarios   map[string]run.Scenario   `json:"scenarios"`
	Connections map[string]run.Connection `json:"connections,omitempty"`
	// Object is the business object of an agent with memory: object.
	Object *run.ObjectSpec `json:"object,omitempty"`
}

// Compile turns a validated package into the stored form.
func Compile(p *Package) (json.RawMessage, error) {
	st := Stored{Agent: p.Agent, Persona: p.Persona, Scenarios: map[string]run.Scenario{}}
	if o := p.Agent.Spec.Object; o != nil {
		st.Object = &run.ObjectSpec{Schema: string(p.ObjectSchema), Opener: p.ObjectOpener}
		for _, s := range o.Stages {
			rs := run.Stage{ID: s.ID, Title: s.Title}
			for _, a := range s.Artifacts {
				rs.Artifacts = append(rs.Artifacts, run.ArtifactSlot{Kind: a.Kind, Title: a.Title})
			}
			st.Object.Stages = append(st.Object.Stages, rs)
		}
	}
	for name, s := range p.Scenarios {
		rs := run.Scenario{
			Name:         name,
			Level:        s.Spec.Level,
			ModelClass:   p.Agent.Spec.Model.Class,
			ModelParams:  p.Agent.Spec.Model.Params,
			Instructions: p.Persona + "\n\n" + s.Instructions,
			InputSchema:  s.InputSchema,
			OutputSchema: s.OutputSchema,
			Tools:        expandTools(p.Agent.Spec.Tools, s.Spec.Tools),
			MaxSteps:     p.Agent.Spec.Budgets.MaxSteps,
			MaxToolCalls: p.Agent.Spec.Budgets.MaxToolCalls,
			Autonomy:     s.Spec.Autonomy,
			Action:       s.Spec.Action,
			ActionInput:  s.Spec.ActionInput,
			Idempotency:  s.Spec.Idempotency.OnRepeat,
			Template:     s.Template,
			Memory:       p.Agent.Spec.Memory,
			Knowledge:    knowledgeDocs(p, p.Agent.Spec.Knowledge),
		}
		if len(s.Spec.Params) > 0 {
			rs.Params = ParamDefaults(s.Spec.Params)
		}
		for _, t := range s.Spec.Triggers {
			rs.Triggers = append(rs.Triggers, run.Trigger{Type: t.Type, Source: t.Source, Schedule: t.Schedule})
		}
		if a := s.Spec.Approval; a != nil && a.Required {
			ra := &run.Approval{OnTimeout: a.OnTimeout, DistinctApprover: a.DistinctApprover}
			if a.Timeout != "" {
				d, err := time.ParseDuration(a.Timeout)
				if err != nil {
					return nil, fmt.Errorf("scenario %s: approval timeout: %w", name, err)
				}
				ra.Timeout = run.Duration(d)
			}
			rs.Approval = ra
		}
		st.Scenarios[name] = rs
	}
	for name, c := range p.Connections {
		rc := run.Connection{Name: name, Team: c.Metadata.Team, Owner: c.Metadata.Owner, Scenarios: c.Spec.Scenarios,
			Status: c.Spec.Status, Channels: c.Spec.Channels, Params: c.Spec.Params,
			Knowledge: knowledgeDocs(p, c.Spec.Knowledge)}
		for _, sn := range c.Spec.Scenarios {
			s := p.Scenarios[sn]
			if s == nil || len(s.Spec.Params) == 0 {
				continue
			}
			vals := ParamDefaults(s.Spec.Params)
			for pn := range s.Spec.Params {
				if v, ok := c.Spec.Params[pn]; ok {
					vals[pn] = v
				}
			}
			if rc.ScenarioParams == nil {
				rc.ScenarioParams = map[string]map[string]any{}
			}
			rc.ScenarioParams[sn] = vals
		}
		for src, t := range c.Spec.Triggers {
			if rc.Triggers == nil {
				rc.Triggers = map[string]run.ConnectionTrigger{}
			}
			ct := run.ConnectionTrigger{Receiver: t.Receiver, Schedule: t.Schedule, Users: t.Users, Groups: t.Groups, Channels: t.Channels}
			if t.Input != nil {
				b, err := json.Marshal(t.Input)
				if err != nil {
					return nil, fmt.Errorf("connection %s, trigger %s: input: %w", name, src, err)
				}
				ct.Input = b
			}
			rc.Triggers[src] = ct
		}
		if st.Connections == nil {
			st.Connections = map[string]run.Connection{}
		}
		st.Connections[name] = rc
	}
	return json.Marshal(st)
}

// expandTools resolves scenario tools to "ref.function" names. A bare ref stands for the functions
// agent.yml allows for it, or for every function of the server when agent.yml lists none ("ref.*").
func expandTools(agent []Tool, scenario []string) []string {
	allowed := map[string][]string{}
	for _, t := range agent {
		allowed[t.Ref] = t.Functions
	}
	var out []string
	for _, t := range scenario {
		ref, fn, ok := strings.Cut(t, ".")
		switch {
		case ok && fn != "":
			out = append(out, t)
		case len(allowed[ref]) > 0:
			for _, f := range allowed[ref] {
				out = append(out, ref+"."+f)
			}
		default:
			out = append(out, ref+".*")
		}
	}
	return out
}
