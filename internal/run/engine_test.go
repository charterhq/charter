package run_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/db"
	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/run"
)

// newDB creates an isolated database per test. Tests skip without CHARTER_TEST_DATABASE_URL.
func newDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("CHARTER_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("CHARTER_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := db.Open(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("charter_test_%d", rand.Uint64())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := db.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		admin.Close()
	})
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func testConfig() run.Config {
	return run.Config{FetchPollInterval: 50 * time.Millisecond, FetchCooldown: 10 * time.Millisecond, ShipInterval: 200 * time.Millisecond}
}

func applyAgent(t *testing.T, pool *pgxpool.Pool, scn run.Scenario) {
	t.Helper()
	pkg, err := json.Marshal(run.Package{Scenarios: map[string]run.Scenario{scn.Name: scn}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Apply(context.Background(), pool,
		registry.Agent{Name: "alert-triage", Team: "sre", Owner: "owner"}, "1.0.0", "abc123", pkg); err != nil {
		t.Fatal(err)
	}
}

func startEngine(t *testing.T, pool *pgxpool.Pool, model *stub.Model) (*run.Engine, *stub.Tools, *stub.Channel, *stub.Journal) {
	t.Helper()
	drv, tools, ch, j := stub.Set(model)
	eng, err := run.New(pool, drv, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.StopAndCancel(ctx)
	})
	return eng, tools, ch, j
}

func envelope(delivery, object string) run.Envelope {
	return run.Envelope{
		DeliveryID: delivery,
		Trigger:    run.Trigger{Type: "event", Source: "alerts"},
		Connection: "sre",
		Object:     run.Object{Key: object},
		Initiator:  run.Initiator{Kind: "system", ID: "alertmanager"},
		Payload:    json.RawMessage(`{"alertname":"DiskFull","instance":"db01"}`),
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func runState(t *testing.T, pool *pgxpool.Pool, id string) (state, status string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `SELECT state, status FROM runs WHERE id = $1`, id).Scan(&state, &status); err != nil {
		t.Fatal(err)
	}
	return state, status
}

func waitDone(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var status string
	waitFor(t, "run done", func() bool {
		var state string
		state, status = runState(t, pool, id)
		return state == string(run.Done)
	})
	return status
}

var advise = run.Scenario{
	Name: "triage", Level: 2, ModelClass: "balanced", Autonomy: "advise",
	OutputSchema: json.RawMessage(`{"type":"object","required":["verdict"]}`),
}

func TestHappyPath(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, advise)
	eng, _, ch, j := startEngine(t, pool, &stub.Model{Outputs: []string{`{"verdict":"noise"}`}})

	id, dup, err := eng.Accept(context.Background(), "alert-triage", "triage", envelope("d1", "fp1"))
	if err != nil || dup {
		t.Fatalf("accept: %v dup=%v", err, dup)
	}
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %q", status)
	}
	if ch.PostCount() != 1 {
		t.Fatalf("posts %d", ch.PostCount())
	}
	var steps int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM run_steps WHERE run_id = $1`, id).Scan(&steps)
	if steps != 8 { // accepted..responding, recording -> done; no approval, no execution
		t.Fatalf("steps %d", steps)
	}
	waitFor(t, "journal shipped", func() bool { return j.Count() == 1 })
}

func TestDuplicateDelivery(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, advise)
	eng, _, _, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"verdict":"noise"}`}})
	ctx := context.Background()

	first, _, err := eng.Accept(ctx, "alert-triage", "triage", envelope("same", "fp1"))
	if err != nil {
		t.Fatal(err)
	}
	second, dup, err := eng.Accept(ctx, "alert-triage", "triage", envelope("same", "fp1"))
	if err != nil {
		t.Fatal(err)
	}
	if !dup || second != first {
		t.Fatalf("duplicate delivery created a new run: dup=%v %s != %s", dup, second, first)
	}
	var runs int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM runs`).Scan(&runs)
	if runs != 1 {
		t.Fatalf("runs %d", runs)
	}
}

func TestIdempotencySkip(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, advise)
	model := &stub.Model{Outputs: []string{`{"verdict":"noise"}`}}
	eng, _, _, _ := startEngine(t, pool, model)
	ctx := context.Background()

	a, _, _ := eng.Accept(ctx, "alert-triage", "triage", envelope("d1", "fp1"))
	waitDone(t, pool, a)
	b, _, _ := eng.Accept(ctx, "alert-triage", "triage", envelope("d2", "fp1"))
	if status := waitDone(t, pool, b); status != run.StatusSkipped {
		t.Fatalf("second run status %q", status)
	}
	if model.CallCount() != 1 {
		t.Fatalf("model calls %d", model.CallCount())
	}
}

func TestRestartMidStep(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, advise)
	blocking := &stub.Model{Outputs: []string{`{"verdict":"noise"}`}, Block: true}
	drv, _, _, _ := stub.Set(blocking)
	first, err := run.New(pool, drv, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := first.Start(ctx); err != nil {
		t.Fatal(err)
	}
	id, _, err := first.Accept(ctx, "alert-triage", "triage", envelope("d1", "fp1"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "model step started", func() bool { return blocking.CallCount() == 1 })

	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := first.StopAndCancel(stopCtx); err != nil {
		t.Fatal(err)
	}
	cancel()
	if state, _ := runState(t, pool, id); state != string(run.Modeling) {
		t.Fatalf("run moved past the model step without a model result: %s", state)
	}

	model := &stub.Model{Outputs: []string{`{"verdict":"noise"}`}}
	startEngine(t, pool, model)
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status after restart %q", status)
	}
	if model.CallCount() != 1 {
		t.Fatalf("second engine model calls %d", model.CallCount())
	}
}

func TestKillSwitch(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, advise)
	model := &stub.Model{Outputs: []string{`{"verdict":"noise"}`}}
	eng, _, ch, _ := startEngine(t, pool, model)
	ctx := context.Background()

	if err := registry.Suspend(ctx, pool, "alert-triage", "", "owner", "noisy", false); err != nil {
		t.Fatal(err)
	}
	id, _, _ := eng.Accept(ctx, "alert-triage", "triage", envelope("d1", "fp1"))
	if status := waitDone(t, pool, id); status != run.StatusSuspended {
		t.Fatalf("status %q", status)
	}
	if model.CallCount() != 0 || ch.PostCount() != 0 {
		t.Fatalf("switched-off agent did work: model=%d posts=%d", model.CallCount(), ch.PostCount())
	}

	if err := registry.Resume(ctx, pool, "alert-triage", "", "owner", "fixed"); err != nil {
		t.Fatal(err)
	}
	id2, _, _ := eng.Accept(ctx, "alert-triage", "triage", envelope("d2", "fp2"))
	if status := waitDone(t, pool, id2); status != run.StatusOK {
		t.Fatalf("status after resume %q", status)
	}
}

func TestValidationRetry(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, advise)
	model := &stub.Model{Outputs: []string{`not json`, `{"verdict":"real"}`}}
	eng, _, _, _ := startEngine(t, pool, model)

	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", envelope("d1", "fp1"))
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %q", status)
	}
	if model.CallCount() != 2 {
		t.Fatalf("model calls %d", model.CallCount())
	}
}

func TestValidationInvalid(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, advise)
	model := &stub.Model{Outputs: []string{`{"other":1}`}}
	eng, _, ch, _ := startEngine(t, pool, model)

	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", envelope("d1", "fp1"))
	if status := waitDone(t, pool, id); status != run.StatusInvalid {
		t.Fatalf("status %q", status)
	}
	if model.CallCount() != 2 || ch.PostCount() != 0 {
		t.Fatalf("model=%d posts=%d", model.CallCount(), ch.PostCount())
	}
}

var act = run.Scenario{
	Name: "triage", Level: 2, ModelClass: "balanced", Autonomy: "act", Action: "jira.create_issue",
	OutputSchema: json.RawMessage(`{"type":"object","required":["summary"]}`),
	Approval:     &run.Approval{Timeout: run.Duration(time.Hour), OnTimeout: "cancel", DistinctApprover: true},
}

func TestApprovalByFingerprint(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, act)
	eng, tools, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"summary":"Disk full on db01"}`}})
	ctx := context.Background()

	id, _, _ := eng.Accept(ctx, "alert-triage", "triage", envelope("d1", "fp1"))
	var req struct{ wait, fp string }
	waitFor(t, "approval request", func() bool {
		r, ok := ch.LastApproval()
		req.wait, req.fp = r.WaitID, r.Fingerprint
		return ok
	})
	if state, _ := runState(t, pool, id); state != string(run.Approving) {
		t.Fatalf("state %s", state)
	}
	if err := eng.Resolve(ctx, req.wait, "lead", "tampered", true); !errors.Is(err, run.ErrFingerprint) {
		t.Fatalf("tampered approval: %v", err)
	}
	if err := eng.Resolve(ctx, req.wait, "alertmanager", req.fp, true); !errors.Is(err, run.ErrSameApprover) {
		t.Fatalf("self approval: %v", err)
	}
	if err := eng.Resolve(ctx, req.wait, "lead", req.fp, true); err != nil {
		t.Fatal(err)
	}
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %q", status)
	}
	if tools.CallCount() != 1 {
		t.Fatalf("tool calls %d", tools.CallCount())
	}
	var artifacts int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM artifacts WHERE run_id = $1`, id).Scan(&artifacts)
	if artifacts != 1 {
		t.Fatalf("artifacts %d", artifacts)
	}
	if err := eng.Resolve(ctx, req.wait, "lead", req.fp, true); !errors.Is(err, run.ErrWaitClosed) {
		t.Fatalf("second resolve: %v", err)
	}
}

func TestApprovalExpires(t *testing.T) {
	pool := newDB(t)
	scn := act
	scn.Approval = &run.Approval{Timeout: run.Duration(time.Second), OnTimeout: "cancel"}
	applyAgent(t, pool, scn)
	eng, tools, _, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"summary":"Disk full"}`}})

	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", envelope("d1", "fp1"))
	if status := waitDone(t, pool, id); status != run.StatusCancelled {
		t.Fatalf("status %q", status)
	}
	if tools.CallCount() != 0 {
		t.Fatalf("action executed without approval: %d", tools.CallCount())
	}
}
