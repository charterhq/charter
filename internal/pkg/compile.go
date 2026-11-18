package pkg

import (
	"encoding/json"
	"fmt"
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
			Tools:        s.Spec.Tools,
			MaxSteps:     p.Agent.Spec.Budgets.MaxSteps,
			Autonomy:     s.Spec.Autonomy,
			Idempotency:  s.Spec.Idempotency.OnRepeat,
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
