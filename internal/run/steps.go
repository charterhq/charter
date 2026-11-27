package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/render"
	"github.com/charterhq/charter/internal/schema"
)

const (
	stepMaxAttempts        = 5
	defaultApprovalTimeout = 24 * time.Hour
	validationPrefix       = "validation: "
)

type stepArgs struct {
	RunID string `json:"run_id"`
}

func (stepArgs) Kind() string { return "charter_run_step" }

func (stepArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: stepMaxAttempts} }

type deadlineArgs struct {
	WaitID string `json:"wait_id"`
}

func (deadlineArgs) Kind() string { return "charter_wait_deadline" }

type shipArgs struct{}

func (shipArgs) Kind() string { return "charter_journal_ship" }

// runRow is a run with its scenario, as a step sees it.
type runRow struct {
	ID         uuid.UUID
	Agent      string
	Version    string
	Scenario   string
	Connection string
	State      State
	Status     string
	Env        Envelope
	ObjectKey  string
	IdemKey    string
	Input      json.RawMessage
	Output     json.RawMessage
	Usage      driver.Usage
	Error      string
	ReceivedAt time.Time
	StartedAt  *time.Time
	Scn        Scenario
	Identities map[string]string
	// Params and Docs are what the run knows beyond its input: the scenario parameters for its
	// connection and the knowledge files of the agent and the connection.
	Params map[string]any
	Docs   []Doc
}

func (e *Engine) load(ctx context.Context, id uuid.UUID) (runRow, error) {
	var r runRow
	var env, scn, usage, ids, conn []byte
	err := e.pool.QueryRow(ctx, `
		SELECT r.id, r.agent, v.version, r.scenario, r.connection, r.state, r.status, r.envelope,
		       r.object_key, r.idempotency_key, r.input, r.output, r.usage, r.error,
		       r.received_at, r.started_at, v.package -> 'scenarios' -> r.scenario,
		       coalesce(v.package -> 'agent' -> 'spec' -> 'identities', '[]'::jsonb),
		       coalesce(v.package -> 'connections' -> r.connection, 'null'::jsonb)
		FROM runs r JOIN agent_versions v ON v.id = r.agent_version_id
		WHERE r.id = $1`, id).Scan(&r.ID, &r.Agent, &r.Version, &r.Scenario, &r.Connection, &r.State, &r.Status,
		&env, &r.ObjectKey, &r.IdemKey, &r.Input, &r.Output, &usage, &r.Error, &r.ReceivedAt, &r.StartedAt, &scn, &ids, &conn)
	if err != nil {
		return r, err
	}
	var idents []Identity
	if err := json.Unmarshal(ids, &idents); err != nil {
		return r, fmt.Errorf("decode identities: %w", err)
	}
	for _, i := range idents {
		if r.Identities == nil {
			r.Identities = map[string]string{}
		}
		r.Identities[i.System] = i.Account
	}
	if err := json.Unmarshal(env, &r.Env); err != nil {
		return r, fmt.Errorf("decode envelope: %w", err)
	}
	if err := json.Unmarshal(usage, &r.Usage); err != nil {
		return r, fmt.Errorf("decode usage: %w", err)
	}
	if err := json.Unmarshal(scn, &r.Scn); err != nil {
		return r, fmt.Errorf("decode scenario: %w", err)
	}
	var c *Connection
	if err := json.Unmarshal(conn, &c); err != nil {
		return r, fmt.Errorf("decode connection: %w", err)
	}
	r.Params, r.Docs = runContext(r.Scn, c)
	return r, nil
}

// runContext: the parameters a scenario runs with for a connection and the knowledge it sees,
// the agent's files first, then the team's.
func runContext(s Scenario, c *Connection) (map[string]any, []Doc) {
	params, docs := s.Params, slices.Clone(s.Knowledge)
	if c != nil {
		if p, ok := c.ScenarioParams[s.Name]; ok {
			params = p
		}
		docs = append(docs, c.Knowledge...)
	}
	return params, docs
}

func (r runRow) target() driver.Target {
	if r.Env.Reply != nil {
		return *r.Env.Reply
	}
	return driver.Target{}
}

// outcome of a step: where the run goes next and what to write with the transition.
type outcome struct {
	next    State
	status  string
	errText string
	apply   func(ctx context.Context, tx pgx.Tx) error
	stay    bool // the step parked the run itself (approval wait)
}

type stepWorker struct {
	river.WorkerDefaults[stepArgs]
	e *Engine
}

func (w *stepWorker) Work(ctx context.Context, job *river.Job[stepArgs]) error {
	e := w.e
	id, err := uuid.Parse(job.Args.RunID)
	if err != nil {
		return river.JobCancel(err)
	}
	r, err := e.load(ctx, id)
	if err != nil {
		return err
	}
	if r.State == Done {
		return nil
	}
	started := e.now()
	out, stepErr := e.step(ctx, r)
	var snooze *river.JobSnoozeError
	if errors.As(stepErr, &snooze) {
		return stepErr // waiting its turn is not a failed attempt
	}
	if stepErr != nil {
		if job.Attempt < job.MaxAttempts || r.State == Recording {
			return stepErr
		}
		// Out of attempts: the run fails and still reaches the journal.
		out = outcome{next: Recording, status: StatusFailed, errText: fmt.Sprintf("%s: %v", r.State, stepErr)}
	}
	if out.stay {
		return nil
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if out.apply != nil {
		if err := out.apply(ctx, tx); err != nil {
			return err
		}
	}
	if err := e.transition(ctx, tx, r.ID, r.State, out.next, out.status, out.errText, started, job.Attempt); err != nil {
		if errors.Is(err, errStale) {
			return nil
		}
		return err
	}
	return tx.Commit(ctx)
}

func (e *Engine) step(ctx context.Context, r runRow) (outcome, error) {
	switch r.State {
	case Accepted:
		return outcome{next: Filtering, apply: func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE runs SET started_at = coalesce(started_at, now()) WHERE id = $1`, r.ID)
			return err
		}}, nil
	case Filtering:
		return e.filter(ctx, r)
	case Preparing:
		input := r.Env.Payload
		if r.Env.Conversation != nil {
			var err error
			if input, err = e.conversationInput(ctx, r); err != nil {
				return outcome{}, err
			}
		}
		if len(r.Scn.InputSchema) > 0 {
			if problems := schema.Check(r.Scn.InputSchema, input); len(problems) > 0 {
				return outcome{next: Recording, status: StatusFailed,
					errText: "input does not match input_schema: " + strings.Join(problems, "; ")}, nil
			}
		}
		return outcome{next: Modeling, apply: func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE runs SET input = $2 WHERE id = $1`, r.ID, []byte(input))
			return err
		}}, nil
	case Modeling:
		return e.model(ctx, r)
	case Validating:
		return e.validate(ctx, r), nil
	case Checking:
		switch {
		case r.Env.Shadow:
			return outcome{next: Responding}, nil // a connection in shadow never acts, people see the draft
		case r.Scn.Autonomy == "act" && r.Scn.Action != "":
			if _, ok := actionArgs(r); !ok {
				return outcome{next: Responding}, nil // no draft in the output: an answer, no action
			}
			if r.Scn.Approval != nil {
				return outcome{next: Approving}, nil
			}
			return outcome{next: Executing}, nil
		case r.Scn.Approval != nil:
			return outcome{next: Approving}, nil
		default:
			return outcome{next: Responding}, nil
		}
	case Approving:
		return e.approve(ctx, r)
	case Executing:
		return e.execute(ctx, r)
	case Responding:
		m := e.answer(r)
		if om, ok, err := e.outcomeMessage(ctx, r); err != nil {
			return outcome{}, err
		} else if ok {
			m = om
		}
		out := outcome{next: Recording}
		if r.Env.Conversation != nil {
			text := answerText(m.Text, r.Output)
			out.apply = func(ctx context.Context, tx pgx.Tx) error { return saveAgentMessage(ctx, tx, r, text) }
		}
		if r.Env.Shadow && r.Env.Reply == nil {
			return out, nil // shadow without a shadow channel: journal only
		}
		if r.Env.Object.ID != "" && r.Env.Reply == nil {
			return out, nil // a conversation about an object: people read the answer from the object's stream
		}
		if _, err := e.post(ctx, m); err != nil {
			return outcome{}, err
		}
		return out, nil
	case Recording:
		return e.record(ctx, r)
	}
	return outcome{}, fmt.Errorf("unknown state %q", r.State)
}

// switchedOff returns the state of the switch that stops the agent or its connection, or "".
func switchedOff(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, agent, connection string) (string, error) {
	var off string
	err := q.QueryRow(ctx, `
		SELECT state FROM switches
		WHERE agent = $1 AND connection IN ('', $2) AND state <> 'on' LIMIT 1`, agent, connection).Scan(&off)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return off, err
}

// suspended ends a run of a switched-off agent; the switch is checked again before steps 7 and 8.
func (e *Engine) suspended(ctx context.Context, r runRow, when string) (outcome, bool, error) {
	off, err := switchedOff(ctx, e.pool, r.Agent, r.Connection)
	if err != nil || off == "" {
		return outcome{}, false, err
	}
	return outcome{next: Recording, status: StatusSuspended, errText: "agent switched off" + when + ": " + off}, true, nil
}

func (e *Engine) filter(ctx context.Context, r runRow) (outcome, error) {
	if out, off, err := e.suspended(ctx, r, ""); err != nil || off {
		return out, err
	}
	if r.IdemKey != "" {
		if _, err := e.pool.Exec(ctx, `INSERT INTO idempotency (key, run_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			r.IdemKey, r.ID); err != nil {
			return outcome{}, err
		}
		var owner uuid.UUID
		if err := e.pool.QueryRow(ctx, `SELECT run_id FROM idempotency WHERE key = $1`, r.IdemKey).Scan(&owner); err != nil {
			return outcome{}, err
		}
		if owner != r.ID && r.Scn.Idempotency != "update" {
			return outcome{next: Recording, status: StatusSkipped, errText: "duplicate of run " + owner.String()}, nil
		}
	}
	return outcome{next: Preparing}, nil
}

func (e *Engine) model(ctx context.Context, r runRow) (outcome, error) {
	req := driver.StepRequest{
		RunID: r.ID.String(), Agent: r.Agent, Scenario: r.Scenario, ModelClass: r.Scn.ModelClass, Params: r.Scn.ModelParams,
		Instructions: Prompt(r.Scn.Instructions, r.Docs, r.Params), Input: r.Input, OutputSchema: r.Scn.OutputSchema,
		Tools: r.Scn.Tools, MaxSteps: r.Scn.MaxSteps, MaxToolCalls: r.Scn.MaxToolCalls, Traceparent: r.Env.Trace.Traceparent(),
	}
	if len(r.Error) > len(validationPrefix) && r.Error[:len(validationPrefix)] == validationPrefix {
		req.Feedback = []string{r.Error[len(validationPrefix):]}
	}
	res, err := e.drv.Model.Run(ctx, req)
	if err != nil {
		return outcome{}, err
	}
	usage := addUsage(r.Usage, res.Usage)
	usageJSON, err := json.Marshal(usage)
	if err != nil {
		return outcome{}, err
	}
	return outcome{next: Validating, apply: func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE runs SET output = $2, usage = $3 WHERE id = $1`, r.ID, []byte(res.Output), usageJSON)
		return err
	}}, nil
}

// validate checks the output against the scenario schema; one retry with feedback (STD-0006, step 5).
// With an object, the model's state_ops must fit its schema too, and they are saved here: a run with
// an approval or one that is cancelled never reaches step 9, the state must not wait for it.
func (e *Engine) validate(ctx context.Context, r runRow) outcome {
	problem := checkOutput(r.Output, r.Scn.OutputSchema)
	withObject := problem == "" && r.Scn.Memory == "object" && r.Env.Object.ID != ""
	if withObject {
		problem = e.checkStateOps(ctx, r)
	}
	if problem != "" && r.Usage.FinishReason == "length" {
		problem += fmt.Sprintf(" (model stopped at the output limit; reasoning tokens so far: %d)", r.Usage.ReasoningTokens)
	}
	if problem == "" {
		return outcome{next: Checking, apply: func(ctx context.Context, tx pgx.Tx) error {
			if withObject {
				if err := e.applyStateOps(ctx, tx, r); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `UPDATE runs SET error = '' WHERE id = $1`, r.ID)
			return err
		}}
	}
	if len(r.Error) >= len(validationPrefix) && r.Error[:len(validationPrefix)] == validationPrefix {
		return outcome{next: Recording, status: StatusInvalid, errText: validationPrefix + problem}
	}
	return outcome{next: Modeling, errText: validationPrefix + problem}
}

// checkOutput validates the output against the scenario output schema.
func checkOutput(output, outputSchema json.RawMessage) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(output, &obj); err != nil {
		return "output is not a JSON object"
	}
	if len(outputSchema) == 0 {
		return ""
	}
	if problems := schema.Check(outputSchema, output); len(problems) > 0 {
		return strings.Join(problems, "; ")
	}
	return ""
}

func (e *Engine) approve(ctx context.Context, r runRow) (outcome, error) {
	var waitID, draftID uuid.UUID
	var fp string
	var deadline time.Time
	if out, off, err := e.suspended(ctx, r, " before approval"); err != nil || off {
		return out, err
	}
	err := e.pool.QueryRow(ctx, `
		SELECT w.id, w.draft_id, d.fingerprint, w.deadline
		FROM waits w JOIN drafts d ON d.id = w.draft_id
		WHERE w.run_id = $1 AND w.state = 'open'`, r.ID).Scan(&waitID, &draftID, &fp, &deadline)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		args, _ := actionArgs(r)
		fp, err = Fingerprint(args)
		if err != nil {
			return outcome{}, err
		}
		timeout := time.Duration(r.Scn.Approval.Timeout)
		if timeout == 0 {
			timeout = defaultApprovalTimeout
		}
		onTimeout := r.Scn.Approval.OnTimeout
		if onTimeout == "" {
			onTimeout = "cancel"
		}
		waitID, draftID, deadline = uuid.New(), uuid.New(), e.now().Add(timeout)
		tx, err := e.pool.Begin(ctx)
		if err != nil {
			return outcome{}, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `INSERT INTO drafts (id, run_id, action, fingerprint, initiator) VALUES ($1, $2, $3, $4, $5)`,
			draftID, r.ID, []byte(args), fp, r.Env.Initiator.ID); err != nil {
			return outcome{}, err
		}
		if r.Env.Conversation != nil {
			// the draft is the agent's turn: the next message is read against it
			if err := saveAgentMessage(ctx, tx, r, answerText(e.answer(r).Text, r.Output)); err != nil {
				return outcome{}, err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO waits (id, run_id, kind, draft_id, deadline, on_timeout, state)
			VALUES ($1, $2, 'approval', $3, $4, $5, 'open')`, waitID, r.ID, draftID, deadline, onTimeout); err != nil {
			return outcome{}, err
		}
		if _, err := e.client.InsertTx(ctx, tx, deadlineArgs{WaitID: waitID.String()},
			&river.InsertOpts{ScheduledAt: deadline}); err != nil {
			return outcome{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return outcome{}, err
		}
	case err != nil:
		return outcome{}, err
	}
	var action json.RawMessage
	if err := e.pool.QueryRow(ctx, `SELECT action FROM drafts WHERE id = $1`, draftID).Scan(&action); err != nil {
		return outcome{}, err
	}
	if _, err := e.askApproval(ctx, driver.ApprovalRequest{
		Message: driver.Message{RunID: r.ID.String(), Agent: r.Agent, Connection: r.Connection, ObjectKey: r.ObjectKey, Identities: r.Identities,
			Target: r.target(), Text: e.answer(r).Text},
		WaitID: waitID.String(), DraftID: draftID.String(), Fingerprint: fp, Action: action, Deadline: deadline,
	}); err != nil {
		return outcome{}, err
	}
	return outcome{stay: true}, nil
}

// execute calls the action at most once (STD-0006, I4). The call is marked in the database before
// it starts; a repeated step that finds the mark does not call again: the outcome is unknown and
// goes to people. Only a call the driver reports as not sent is repeated.
func (e *Engine) execute(ctx context.Context, r runRow) (outcome, error) {
	if out, off, err := e.suspended(ctx, r, " before the action"); err != nil || off {
		return out, err
	}
	// with an approval the action runs with the approved draft, exactly; without one, with the output
	d, hasDraft, err := e.loadDraft(ctx, r.ID)
	if err != nil {
		return outcome{}, err
	}
	if !hasDraft {
		args, _ := actionArgs(r)
		fp, err := Fingerprint(args)
		if err != nil {
			return outcome{}, err
		}
		d = draftRow{ID: uuid.New(), Action: args}
		if _, err := e.pool.Exec(ctx, `INSERT INTO drafts (id, run_id, action, fingerprint, initiator, called_at)
			VALUES ($1, $2, $3, $4, $5, now())`, d.ID, r.ID, []byte(args), fp, r.Env.Initiator.ID); err != nil {
			return outcome{}, err
		}
	} else {
		tag, err := e.pool.Exec(ctx, `UPDATE drafts SET called_at = now() WHERE id = $1 AND called_at IS NULL`, d.ID)
		if err != nil {
			return outcome{}, err
		}
		if tag.RowsAffected() == 0 {
			return unknownOutcome("the previous attempt was interrupted"), nil
		}
	}
	res, err := e.drv.Tools.Call(driver.AtMostOnce(ctx), r.Agent, r.Scn.Action, d.Action)
	var te *driver.ToolError
	switch {
	case err == nil:
	case errors.Is(err, driver.ErrNotSent):
		if _, uerr := e.pool.Exec(ctx, `UPDATE drafts SET called_at = NULL WHERE id = $1`, d.ID); uerr != nil {
			return outcome{}, uerr
		}
		return outcome{}, err // nothing left the core: the step may run again
	case errors.As(err, &te):
		return outcome{next: Recording, status: StatusFailed, errText: "action failed, the tool answered: " + te.Text}, nil
	default:
		return unknownOutcome(err.Error()), nil
	}
	var ref struct {
		Ref string `json:"ref"`
	}
	_ = json.Unmarshal(res, &ref)
	if ref.Ref == "" {
		ref.Ref, _ = Fingerprint(res)
	}
	return outcome{next: Responding, apply: func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE drafts SET result = $2 WHERE id = $1`, d.ID, []byte(res)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO artifacts (id, run_id, agent, object_key, system, ref) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT DO NOTHING`, uuid.New(), r.ID, r.Agent, r.ObjectKey, "tool:"+r.Scn.Action, ref.Ref)
		return err
	}}, nil
}

// unknownOutcome ends a run whose action may or may not have happened: people check by hand.
func unknownOutcome(why string) outcome {
	return outcome{next: Recording, status: StatusFailed,
		errText: "action outcome unknown, it may have been done: check by hand before repeating (" + why + ")"}
}

func (e *Engine) record(ctx context.Context, r runRow) (outcome, error) {
	status := r.Status
	if status == "" {
		status = StatusOK
	}
	if status == StatusFailed || status == StatusInvalid || status == StatusSuspended && e.awaited(ctx, r) {
		e.notifyFailure(ctx, r, status)
	}
	finished := e.now()
	rec, err := json.Marshal(Record(r, status, finished))
	if err != nil {
		return outcome{}, err
	}
	e.exportTrace(ctx, r, status, finished)
	return outcome{next: Done, status: status, apply: func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO journal_outbox (run_id, record) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			r.ID, rec); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE runs SET finished_at = now() WHERE id = $1`, r.ID)
		return err
	}}, nil
}

// Record builds the run record with STD-0005 field names.
func Record(r runRow, status string, finished time.Time) map[string]any {
	op := "invoke_workflow"
	if r.Scn.Level == 3 {
		op = "invoke_agent"
	}
	rec := map[string]any{
		"agentic.run.id":                       r.ID.String(),
		"gen_ai.agent.name":                    r.Agent,
		"gen_ai.agent.version":                 r.Version,
		"agentic.scenario":                     r.Scenario,
		"agentic.connection":                   r.Connection,
		"gen_ai.operation.name":                op,
		"agentic.trigger.type":                 r.Env.Trigger.Type,
		"agentic.trigger.source":               r.Env.Trigger.Source,
		"agentic.initiator":                    r.Env.Initiator.Kind,
		"agentic.initiator.ref":                r.Env.Initiator.Ref,
		"agentic.object.key":                   r.ObjectKey,
		"agentic.idempotency.key":              r.IdemKey,
		"agentic.run.received_at":              r.ReceivedAt,
		"agentic.run.started_at":               r.StartedAt,
		"agentic.run.finished_at":              finished,
		"agentic.run.status":                   status,
		"agentic.output":                       r.Output,
		"agentic.model.class":                  r.Scn.ModelClass,
		"gen_ai.response.model":                r.Usage.Model,
		"gen_ai.provider.name":                 r.Usage.Provider,
		"gen_ai.response.finish_reasons":       r.Usage.FinishReason,
		"gen_ai.usage.input_tokens":            r.Usage.InputTokens,
		"gen_ai.usage.output_tokens":           r.Usage.OutputTokens,
		"gen_ai.usage.cache_read.input_tokens": r.Usage.CachedTokens,
		"agentic.usage.reasoning_tokens":       r.Usage.ReasoningTokens,
		"agentic.usage.cost":                   r.Usage.Cost,
		"agentic.run.tool_calls":               r.Usage.ToolCalls,
		"agentic.run.tool_errors":              r.Usage.ToolErrors,
		"agentic.error":                        r.Error,
		"trace_id":                             r.Env.Trace.TraceID,
	}
	return rec
}

func addUsage(a, b driver.Usage) driver.Usage {
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.CachedTokens += b.CachedTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.ToolCalls += b.ToolCalls
	a.ToolErrors += b.ToolErrors
	a.Cost += b.Cost
	if b.Model != "" {
		a.Model, a.Provider, a.FinishReason = b.Model, b.Provider, b.FinishReason
	}
	return a
}

type deadlineWorker struct {
	river.WorkerDefaults[deadlineArgs]
	e *Engine
}

// Work applies the timeout outcome if the wait is still open (STD-0006, I5).
func (w *deadlineWorker) Work(ctx context.Context, job *river.Job[deadlineArgs]) error {
	e := w.e
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var runID uuid.UUID
	var state string
	err = tx.QueryRow(ctx, `SELECT run_id, state FROM waits WHERE id = $1 FOR UPDATE`, job.Args.WaitID).Scan(&runID, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if state != "open" {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE waits SET state = 'expired', resolved_at = now() WHERE id = $1`, job.Args.WaitID); err != nil {
		return err
	}
	if err := e.transition(ctx, tx, runID, Approving, Responding, StatusCancelled, "approval expired", e.now(), job.Attempt); err != nil {
		if errors.Is(err, errStale) {
			return nil
		}
		return err
	}
	return tx.Commit(ctx)
}

type shipWorker struct {
	river.WorkerDefaults[shipArgs]
	e *Engine
}

// Work ships pending run records to the journal store.
func (w *shipWorker) Work(ctx context.Context, _ *river.Job[shipArgs]) error {
	e := w.e
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT run_id, record FROM journal_outbox WHERE shipped_at IS NULL
		ORDER BY created_at LIMIT 500 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	var recs []json.RawMessage
	for rows.Next() {
		var id uuid.UUID
		var rec []byte
		if err := rows.Scan(&id, &rec); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
		recs = append(recs, rec)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if err := e.drv.Journal.Write(ctx, recs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE journal_outbox SET shipped_at = now() WHERE run_id = ANY($1)`, ids); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// renderRun is the run metadata a response template sees.
func (e *Engine) renderRun(r runRow, status string) render.Run {
	rr := render.Run{
		ID: r.ID.String(), ShortID: r.ID.String()[:8], Agent: r.Agent, Version: r.Version, Scenario: r.Scenario,
		Connection: r.Connection, Autonomy: r.Scn.Autonomy, Status: status, Error: strings.TrimPrefix(r.Error, validationPrefix),
		Params: r.Params,
	}
	if r.StartedAt != nil {
		rr.Seconds = e.now().Sub(*r.StartedAt).Seconds()
	}
	return rr
}

// answer builds the message with the scenario's response template. Without a template, or when it
// fails, the driver shows the raw output: the answer still reaches people.
func (e *Engine) answer(r runRow) driver.Message {
	m := driver.Message{
		RunID: r.ID.String(), Agent: r.Agent, Connection: r.Connection, ObjectKey: r.ObjectKey, Identities: r.Identities,
		Target: r.target(), Data: r.Output,
	}
	if r.Scn.Template == "" {
		return m
	}
	text, err := render.Output(r.Scn.Template, e.renderRun(r, StatusOK), r.Input, r.Output)
	if err != nil {
		e.log.Warn("response template failed, posting raw output", "run_id", r.ID, "agent", r.Agent, "err", err)
		return m
	}
	m.Text = text
	return m
}

// awaited says whether people wait for this run: a conversation turn or a drafted action.
// A switched-off agent stays silent on events nobody asked it about.
func (e *Engine) awaited(ctx context.Context, r runRow) bool {
	if r.Env.Conversation != nil {
		return true
	}
	_, ok, err := e.loadDraft(ctx, r.ID)
	return err == nil && ok
}

// notifyFailure tells the place the run would answer in that there is no answer (on_failure).
// Best effort: a channel error must not keep the run out of the journal.
func (e *Engine) notifyFailure(ctx context.Context, r runRow, status string) {
	t := r.target()
	if t.Channel == "" {
		return
	}
	text := ""
	if r.Scn.Template != "" {
		if msg, ok, err := render.Failed(r.Scn.Template, e.renderRun(r, status), r.Input); err != nil {
			e.log.Warn("failure template failed", "run_id", r.ID, "err", err)
		} else if ok {
			text = msg
		}
	}
	if text == "" {
		text = fmt.Sprintf("**%s**: run `%s` ended with status `%s`, no answer – a human is needed.\n%s",
			r.Agent, r.ID.String()[:8], status, strings.TrimPrefix(r.Error, validationPrefix))
	}
	if _, err := e.post(ctx, driver.Message{
		RunID: r.ID.String(), Agent: r.Agent, Connection: r.Connection, ObjectKey: r.ObjectKey, Identities: r.Identities, Target: t, Text: text,
	}); err != nil {
		e.log.Warn("failure notice not delivered", "run_id", r.ID, "err", err)
	}
}
