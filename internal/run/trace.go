package run

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/charterhq/charter/internal/telemetry"
)

// exportTrace emits the run trace from the recorded steps. Best effort: tracing never fails a run.
func (e *Engine) exportTrace(ctx context.Context, r runRow, status string, finished time.Time) {
	if e.tel == nil || r.Env.Trace.TraceID == "" {
		return
	}
	rows, err := e.pool.Query(ctx, `
		SELECT state, outcome, attempt, started_at, finished_at FROM run_steps WHERE run_id = $1 ORDER BY seq`, r.ID)
	if err != nil {
		e.log.Warn("trace: read steps", "run_id", r.ID, "err", err)
		return
	}
	defer rows.Close()
	var steps []telemetry.Step
	for rows.Next() {
		var s telemetry.Step
		if err := rows.Scan(&s.Name, &s.Outcome, &s.Attempt, &s.Started, &s.Finished); err != nil {
			e.log.Warn("trace: scan step", "run_id", r.ID, "err", err)
			return
		}
		if State(s.Name) == Modeling {
			s.Name = "chat " + r.Scn.ModelClass
			s.Attrs = []attribute.KeyValue{
				attribute.String("gen_ai.operation.name", "chat"),
				attribute.String("agentic.model.class", r.Scn.ModelClass),
				attribute.String("gen_ai.response.model", r.Usage.Model),
				attribute.String("gen_ai.provider.name", r.Usage.Provider),
			}
		}
		steps = append(steps, s)
	}
	if rows.Err() != nil {
		return
	}
	op := "invoke_workflow"
	if r.Scn.Level == 3 {
		op = "invoke_agent"
	}
	started := r.ReceivedAt
	e.tel.Export(telemetry.Run{
		Trace: r.Env.Trace, Name: op + " " + r.Agent, Started: started, Finished: finished,
		Failed: status == StatusFailed || status == StatusInvalid, Error: r.Error,
		Steps: steps,
		Attrs: []attribute.KeyValue{
			attribute.String("gen_ai.operation.name", op),
			attribute.String("gen_ai.agent.name", r.Agent),
			attribute.String("gen_ai.agent.version", r.Version),
			attribute.String("agentic.run.id", r.ID.String()),
			attribute.String("agentic.scenario", r.Scenario),
			attribute.String("agentic.connection", r.Connection),
			attribute.String("agentic.run.status", status),
			attribute.String("agentic.object.key", r.ObjectKey),
			attribute.String("agentic.trigger.source", r.Env.Trigger.Source),
			attribute.String("gen_ai.response.model", r.Usage.Model),
			attribute.String("gen_ai.provider.name", r.Usage.Provider),
			attribute.Int("gen_ai.usage.input_tokens", r.Usage.InputTokens),
			attribute.Int("gen_ai.usage.output_tokens", r.Usage.OutputTokens),
			attribute.Int("agentic.usage.reasoning_tokens", r.Usage.ReasoningTokens),
			attribute.Float64("agentic.usage.cost", r.Usage.Cost),
		},
	})
}
