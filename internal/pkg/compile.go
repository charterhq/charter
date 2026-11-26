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
	Agent     Agent                   `json:"agent"`
	Persona   string                  `json:"persona"`
	Scenarios map[string]run.Scenario `json:"scenarios"`
}

// Compile turns a validated package into the stored form.
func Compile(p *Package) (json.RawMessage, error) {
	st := Stored{Agent: p.Agent, Persona: p.Persona, Scenarios: map[string]run.Scenario{}}
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
			Idempotency:  s.Spec.Idempotency.OnRepeat,
			Template:     s.Template,
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
