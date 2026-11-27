package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/telemetry"
)

// Config tunes the engine. Zero values mean production defaults.
type Config struct {
	Workers           int
	FetchPollInterval time.Duration
	FetchCooldown     time.Duration
	StepTimeout       time.Duration
	ShipInterval      time.Duration
	// RescueAfter returns jobs of a crashed process to the queue.
	RescueAfter time.Duration
	Logger      *slog.Logger
	// Telemetry exports run traces; nil switches them off.
	Telemetry *telemetry.Exporter
	// ScheduleInterval is how often schedule triggers are checked (1m); negative switches the scheduler off.
	ScheduleInterval time.Duration
	// ScheduleLookback is how far back a tick looks for missed occurrences (10m): a restart within it loses nothing.
	ScheduleLookback time.Duration
	// ScheduleLocation is the time zone of crons without CRON_TZ (UTC).
	ScheduleLocation *time.Location
}

// Engine owns runs: it accepts launches, drives the state machine and resolves waits.
type Engine struct {
	pool   *pgxpool.Pool
	drv    driver.Set
	client *river.Client[pgx.Tx]
	log    *slog.Logger
	tel    *telemetry.Exporter
	now    func() time.Time

	lookback time.Duration
	loc      *time.Location
}

// ErrNoVersion means the agent has no applied package version.
var ErrNoVersion = errors.New("agent has no applied version")

// Decision errors are shared with channel drivers, which turn them into words for people.
var (
	ErrWaitClosed   = driver.ErrWaitClosed
	ErrFingerprint  = driver.ErrFingerprint
	ErrSameApprover = driver.ErrSameApprover
)

// ErrNoConnection means the agent's current version does not declare the connection.
var ErrNoConnection = errors.New("connection is not declared in the agent package")

// ErrNotEnabled means the connection does not run the scenario.
var ErrNotEnabled = errors.New("scenario is not enabled in the connection")

// ErrPaused means the connection is paused and takes no launches.
var ErrPaused = errors.New("connection is paused")

// errStale means another worker already moved the run; the job has nothing to do.
var errStale = errors.New("run state changed concurrently")

// New builds an engine with a River client on the same database.
func New(pool *pgxpool.Pool, drv driver.Set, cfg Config) (*Engine, error) {
	if cfg.Workers == 0 {
		cfg.Workers = 20
	}
	if cfg.StepTimeout == 0 {
		cfg.StepTimeout = 10 * time.Minute
	}
	if cfg.RescueAfter == 0 {
		cfg.RescueAfter = cfg.StepTimeout + 5*time.Minute
	}
	if cfg.ShipInterval == 0 {
		cfg.ShipInterval = 10 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.ScheduleInterval == 0 {
		cfg.ScheduleInterval = time.Minute
	}
	if cfg.ScheduleLookback == 0 {
		cfg.ScheduleLookback = 10 * time.Minute
	}
	if cfg.ScheduleLocation == nil {
		cfg.ScheduleLocation = time.UTC
	}
	e := &Engine{pool: pool, drv: drv, log: cfg.Logger, now: time.Now, tel: cfg.Telemetry,
		lookback: cfg.ScheduleLookback, loc: cfg.ScheduleLocation}

	workers := river.NewWorkers()
	river.AddWorker(workers, &stepWorker{e: e})
	river.AddWorker(workers, &deadlineWorker{e: e})
	river.AddWorker(workers, &shipWorker{e: e})
	river.AddWorker(workers, &scheduleWorker{e: e})

	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(cfg.ShipInterval),
			func() (river.JobArgs, *river.InsertOpts) { return shipArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true}),
	}
	// Periodic jobs run on the River leader only: with several cores one of them schedules.
	if cfg.ScheduleInterval > 0 {
		periodic = append(periodic, river.NewPeriodicJob(river.PeriodicInterval(cfg.ScheduleInterval),
			func() (river.JobArgs, *river.InsertOpts) { return scheduleArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true}))
	}

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:               map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: cfg.Workers}},
		Workers:              workers,
		FetchPollInterval:    cfg.FetchPollInterval,
		FetchCooldown:        cfg.FetchCooldown,
		JobTimeout:           cfg.StepTimeout,
		RescueStuckJobsAfter: cfg.RescueAfter,
		// River reports every poll at INFO; the core keeps only its warnings and errors.
		Logger:       slog.New(minLevel{h: cfg.Logger.Handler(), min: slog.LevelWarn}),
		PeriodicJobs: periodic,
	})
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	e.client = client
	return e, nil
}

func (e *Engine) Start(ctx context.Context) error { return e.client.Start(ctx) }

// Stop waits for running steps to finish.
func (e *Engine) Stop(ctx context.Context) error { return e.client.Stop(ctx) }

// StopAndCancel cancels running steps; they are retried by the next engine.
func (e *Engine) StopAndCancel(ctx context.Context) error { return e.client.StopAndCancel(ctx) }

// Accept creates a run for a launch. A repeated delivery returns the existing run.
func (e *Engine) Accept(ctx context.Context, agent, scenario string, env Envelope) (runID string, duplicate bool, err error) {
	if env.ReceivedAt.IsZero() {
		env.ReceivedAt = e.now()
	}
	if env.Trace.TraceID == "" {
		env.Trace = telemetry.New("")
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var versionID uuid.UUID
	var pkgJSON []byte
	err = tx.QueryRow(ctx, `
		SELECT id, package FROM agent_versions
		WHERE agent = $1 AND applied_at IS NOT NULL
		ORDER BY applied_at DESC LIMIT 1`, agent).Scan(&versionID, &pkgJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("%s: %w", agent, ErrNoVersion)
	}
	if err != nil {
		return "", false, err
	}
	var pkg Package
	if err := json.Unmarshal(pkgJSON, &pkg); err != nil {
		return "", false, fmt.Errorf("decode package: %w", err)
	}
	if _, ok := pkg.Scenarios[scenario]; !ok {
		return "", false, fmt.Errorf("%s has no scenario %q", agent, scenario)
	}
	if err := applyConnection(pkg, agent, scenario, &env); err != nil {
		return "", false, err
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return "", false, err
	}

	id := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO runs (id, agent_version_id, agent, scenario, connection, envelope, object_key,
		                  idempotency_key, state, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, versionID, agent, scenario, env.Connection, envJSON, env.Object.Key,
		idempotencyKey(agent, scenario, env), Accepted, env.ReceivedAt); err != nil {
		return "", false, err
	}
	if env.DeliveryID != "" {
		tag, err := tx.Exec(ctx, `
			INSERT INTO deliveries (source, delivery_id, run_id) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, env.Trigger.Source, env.DeliveryID, id)
		if err != nil {
			return "", false, err
		}
		if tag.RowsAffected() == 0 {
			_ = tx.Rollback(ctx)
			var existing uuid.UUID
			err := e.pool.QueryRow(ctx, `SELECT run_id FROM deliveries WHERE source = $1 AND delivery_id = $2`,
				env.Trigger.Source, env.DeliveryID).Scan(&existing)
			return existing.String(), true, err
		}
	}
	if env.Conversation != nil {
		if err := saveUserMessage(ctx, tx, agent, env, id); err != nil {
			return "", false, err
		}
	}
	if _, err := e.client.InsertTx(ctx, tx, stepArgs{RunID: id.String()}, nil); err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return id.String(), false, nil
}

// applyConnection checks that the connection takes this launch and sets where the answer goes:
// the shadow channel while in shadow, else the place of the launch, else the connection's reply channel.
func applyConnection(pkg Package, agent, scenario string, env *Envelope) error {
	if len(pkg.Connections) == 0 {
		return nil // package from before connections were stored
	}
	c, ok := pkg.Connections[env.Connection]
	switch {
	case !ok:
		return fmt.Errorf("%s/%s: %w", agent, env.Connection, ErrNoConnection)
	case !c.enables(scenario):
		return fmt.Errorf("%s/%s, scenario %s: %w", agent, env.Connection, scenario, ErrNotEnabled)
	case c.Status == ConnPaused:
		return fmt.Errorf("%s/%s: %w", agent, env.Connection, ErrPaused)
	case c.Status == ConnShadow:
		env.Shadow, env.Reply = true, nil
		if ch := c.Channels["shadow"]; ch != "" {
			env.Reply = &driver.Target{Channel: ch}
		}
	case env.Reply == nil && c.Channels["reply"] != "":
		env.Reply = &driver.Target{Channel: c.Channels["reply"]}
	}
	return nil
}

// current returns the package of the agent's current version.
func (e *Engine) current(ctx context.Context, agent string) (Package, error) {
	var pkg Package
	var pkgJSON []byte
	err := e.pool.QueryRow(ctx, `
		SELECT package FROM agent_versions
		WHERE agent = $1 AND applied_at IS NOT NULL
		ORDER BY applied_at DESC LIMIT 1`, agent).Scan(&pkgJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return pkg, fmt.Errorf("%s: %w", agent, ErrNoVersion)
	}
	if err != nil {
		return pkg, err
	}
	if err := json.Unmarshal(pkgJSON, &pkg); err != nil {
		return pkg, fmt.Errorf("decode package: %w", err)
	}
	return pkg, nil
}

// Route lists the scenarios of a connection that a source of the given trigger type launches,
// with the connection's settings for the source. A source the connection does not bind launches nothing.
func (e *Engine) Route(ctx context.Context, agent, connection, triggerType, source string) ([]string, ConnectionTrigger, error) {
	pkg, err := e.current(ctx, agent)
	if err != nil {
		return nil, ConnectionTrigger{}, err
	}
	c, ok := pkg.Connections[connection]
	if !ok {
		return nil, ConnectionTrigger{}, fmt.Errorf("%s/%s: %w", agent, connection, ErrNoConnection)
	}
	ct, bound := c.Triggers[source]
	if !bound {
		return nil, ct, nil
	}
	var out []string
	for _, s := range c.Scenarios {
		for _, t := range pkg.Scenarios[s].Triggers {
			if t.Type == triggerType && t.Source == source {
				out = append(out, s)
				break
			}
		}
	}
	return out, ct, nil
}

// Listeners lists who hears messages: for every agent identity, the connections that bind a message
// source of an enabled scenario. Channel drivers listen as these accounts.
func (e *Engine) Listeners(ctx context.Context) ([]driver.Listener, error) {
	agents, err := e.currentPackages(ctx)
	if err != nil {
		return nil, err
	}
	var out []driver.Listener
	for _, a := range agents {
		for cname, c := range a.pkg.Connections {
			if c.Status == ConnPaused {
				continue
			}
			for src, ct := range c.Triggers {
				if !hasTrigger(a.pkg, c, "message", src) {
					continue
				}
				for _, id := range a.pkg.Agent.Spec.Identities {
					out = append(out, driver.Listener{System: id.System, Account: id.Account,
						Agent: a.agent, Connection: cname, Source: src, Places: ct.Channels})
				}
			}
		}
	}
	return out, nil
}

func hasTrigger(p Package, c Connection, typ, source string) bool {
	for _, s := range c.Scenarios {
		for _, t := range p.Scenarios[s].Triggers {
			if t.Type == typ && t.Source == source {
				return true
			}
		}
	}
	return false
}

// Converse takes a message for an agent: the connection must bind the source, the author must be
// allowed, the source must start exactly one scenario.
func (e *Engine) Converse(ctx context.Context, m driver.Incoming) (string, bool, error) {
	scenarios, ct, err := e.Route(ctx, m.Agent, m.Connection, "message", m.Source)
	if err != nil {
		return "", false, err
	}
	switch {
	case len(scenarios) == 0:
		return "", false, fmt.Errorf("%s/%s binds no message source %s: %w", m.Agent, m.Connection, m.Source, ErrNotEnabled)
	case len(scenarios) > 1:
		return "", false, fmt.Errorf("message source %s starts several scenarios: %w", m.Source, ErrNotEnabled)
	case len(ct.Users) > 0 && !slices.Contains(ct.Users, m.Author):
		return "", false, driver.ErrForbidden
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return "", false, err
	}
	env := Envelope{
		DeliveryID:   m.MessageID,
		Trigger:      Trigger{Type: "message", Source: m.Source},
		Connection:   m.Connection,
		Object:       Object{Key: m.Conversation},
		Initiator:    Initiator{Kind: "human", ID: m.Author},
		Conversation: &Conversation{ID: m.Conversation, MessageID: m.MessageID, Author: m.Author, Text: m.Text},
		Payload:      payload,
		Trace:        telemetry.New(""),
	}
	if m.Reply.Channel != "" {
		reply := m.Reply
		env.Reply = &reply
	}
	return e.Accept(ctx, m.Agent, scenarios[0], env)
}

// ErrNotCallable means the scenario has no call trigger.
var ErrNotCallable = errors.New("scenario has no call trigger")

// CallSource returns the source of the scenario's call trigger. A call names the scenario itself,
// so the connection does not bind it; Accept still checks that the connection enables the scenario.
func (e *Engine) CallSource(ctx context.Context, agent, scenario string) (string, error) {
	pkg, err := e.current(ctx, agent)
	if err != nil {
		return "", err
	}
	scn, ok := pkg.Scenarios[scenario]
	if !ok {
		return "", fmt.Errorf("%s has no scenario %q", agent, scenario)
	}
	for _, t := range scn.Triggers {
		if t.Type == "call" {
			return t.Source, nil
		}
	}
	return "", fmt.Errorf("%s/%s: %w", agent, scenario, ErrNotCallable)
}

// idempotencyKey: object, scenario and input fingerprint (STD-0003, spec.idempotency).
// Messages are deduplicated by message ID only: the same words twice in a conversation are two turns.
func idempotencyKey(agent, scenario string, env Envelope) string {
	if env.Object.Key == "" || env.Conversation != nil {
		return ""
	}
	h := sha256.Sum256(env.Payload)
	return agent + "/" + scenario + "/" + env.Object.Key + "/" + hex.EncodeToString(h[:8])
}

// Fingerprint is the SHA-256 of an action's canonical JSON parameters.
func Fingerprint(action json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(action, &v); err != nil {
		return "", err
	}
	canon, err := json.Marshal(v) // encoding/json sorts map keys
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(canon)
	return hex.EncodeToString(h[:]), nil
}

// Resolve answers an approval wait. approve=false rejects the draft.
func (e *Engine) Resolve(ctx context.Context, waitID, by, fingerprint string, approve bool) error {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var runID uuid.UUID
	var draftFP, initiator string
	var distinct bool
	err = tx.QueryRow(ctx, `
		SELECT w.run_id, d.fingerprint, d.initiator,
		       coalesce((v.package -> 'scenarios' -> r.scenario -> 'approval' ->> 'distinct_approver')::bool, false)
		FROM waits w
		JOIN drafts d ON d.id = w.draft_id
		JOIN runs r ON r.id = w.run_id
		JOIN agent_versions v ON v.id = r.agent_version_id
		WHERE w.id = $1 AND w.state = 'open'
		FOR UPDATE OF w`, waitID).Scan(&runID, &draftFP, &initiator, &distinct)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWaitClosed
	}
	if err != nil {
		return err
	}
	if approve && fingerprint != draftFP {
		return ErrFingerprint
	}
	if approve && distinct && initiator != "" && by == initiator {
		return ErrSameApprover
	}

	state, next, status := "rejected", Responding, StatusCancelled
	if approve {
		state, next, status = "approved", Executing, ""
	}
	if _, err := tx.Exec(ctx, `UPDATE waits SET state = $2, resolved_by = $3, resolved_at = now() WHERE id = $1`,
		waitID, state, by); err != nil {
		return err
	}
	if approve {
		if _, err := tx.Exec(ctx, `UPDATE drafts d SET approved_by = $2 FROM waits w WHERE w.id = $1 AND d.id = w.draft_id`,
			waitID, by); err != nil {
			return err
		}
	}
	if err := e.transition(ctx, tx, runID, Approving, next, status, "", e.now(), 1); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// transition moves a run from one state to the next in tx and enqueues the next step.
func (e *Engine) transition(ctx context.Context, tx pgx.Tx, runID uuid.UUID, from, to State,
	status, errText string, started time.Time, attempt int) error {
	tag, err := tx.Exec(ctx, `
		UPDATE runs
		SET state = $3,
		    status = CASE WHEN $4 <> '' THEN $4 ELSE status END,
		    error = CASE WHEN $5 <> '' THEN $5 ELSE error END,
		    updated_at = now()
		WHERE id = $1 AND state = $2`, runID, from, to, status, errText)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errStale
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO run_steps (run_id, state, attempt, outcome, started_at) VALUES ($1, $2, $3, $4, $5)`,
		runID, from, attempt, string(to), started); err != nil {
		return err
	}
	if to == Done {
		return nil
	}
	_, err = e.client.InsertTx(ctx, tx, stepArgs{RunID: runID.String()}, nil)
	return err
}

// minLevel drops records below a level.
type minLevel struct {
	h   slog.Handler
	min slog.Level
}

func (m minLevel) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= m.min && m.h.Enabled(ctx, l)
}
func (m minLevel) Handle(ctx context.Context, r slog.Record) error { return m.h.Handle(ctx, r) }
func (m minLevel) WithAttrs(a []slog.Attr) slog.Handler {
	return minLevel{h: m.h.WithAttrs(a), min: m.min}
}
func (m minLevel) WithGroup(n string) slog.Handler { return minLevel{h: m.h.WithGroup(n), min: m.min} }
