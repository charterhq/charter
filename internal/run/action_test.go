package run_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/run"
)

// actNow acts without approval: the action runs right after the output is checked.
func actNow() run.Scenario {
	scn := act
	scn.Approval = nil
	return scn
}

// replied is an event that says where to answer, so a failure notice has a place to go.
func replied(delivery, object string) run.Envelope {
	env := envelope(delivery, object)
	env.Reply = &driver.Target{System: "chat", Channel: "ops", Thread: "t1"}
	return env
}

func runError(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var e string
	_ = pool.QueryRow(context.Background(), `SELECT error FROM runs WHERE id = $1`, id).Scan(&e)
	return e
}

func TestActionLostAnswerIsNotRepeated(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, actNow())
	eng, tools, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"summary":"Disk full"}`}})
	tools.Failures = map[string][]error{"jira.create_issue": {errors.New("connection reset after the request was sent")}}

	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", replied("d1", "fp1"))
	if status := waitDone(t, pool, id); status != run.StatusFailed {
		t.Fatalf("status %q", status)
	}
	if tools.CallCount() != 1 {
		t.Fatalf("action called %d times, want once", tools.CallCount())
	}
	if e := runError(t, pool, id); !strings.Contains(e, "outcome unknown") {
		t.Fatalf("error %q", e)
	}
	if ch.PostCount() != 1 {
		t.Fatalf("people not told: posts %d", ch.PostCount())
	}
}

func TestActionNotSentIsRepeated(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, actNow())
	eng, tools, _, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"summary":"Disk full"}`}})
	tools.Failures = map[string][]error{"jira.create_issue": {fmt.Errorf("%w: gateway down", driver.ErrNotSent)}}

	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", replied("d1", "fp1"))
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %q", status)
	}
	if tools.CallCount() != 2 {
		t.Fatalf("tool calls %d, want the unsent one and the real one", tools.CallCount())
	}
	var artifacts int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM artifacts WHERE run_id = $1`, id).Scan(&artifacts)
	if artifacts != 1 {
		t.Fatalf("artifacts %d", artifacts)
	}
}

func TestActionToolRefusalIsNotRepeated(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, actNow())
	eng, tools, _, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"summary":"Disk full"}`}})
	tools.Failures = map[string][]error{"jira.create_issue": {&driver.ToolError{Text: "project does not exist"}}}

	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", replied("d1", "fp1"))
	if status := waitDone(t, pool, id); status != run.StatusFailed {
		t.Fatalf("status %q", status)
	}
	if tools.CallCount() != 1 {
		t.Fatalf("tool calls %d", tools.CallCount())
	}
	if e := runError(t, pool, id); !strings.Contains(e, "project does not exist") {
		t.Fatalf("error %q", e)
	}
}

// waitApproval runs an act scenario up to its approval request.
func waitApproval(t *testing.T, eng *run.Engine, ch *stub.Channel) (id, wait, fp string) {
	t.Helper()
	id, _, _ = eng.Accept(context.Background(), "alert-triage", "triage", replied("d1", "fp1"))
	waitFor(t, "approval request", func() bool {
		r, ok := ch.LastApproval()
		wait, fp = r.WaitID, r.Fingerprint
		return ok
	})
	return id, wait, fp
}

// A step that died after marking the call, for example with the core, must not call again.
func TestActionInterruptedAttemptIsNotRepeated(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, act)
	eng, tools, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"summary":"Disk full"}`}})
	ctx := context.Background()
	id, wait, fp := waitApproval(t, eng, ch)

	if _, err := pool.Exec(ctx, `UPDATE drafts SET called_at = now() WHERE run_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := eng.Resolve(ctx, wait, "lead", fp, true); err != nil {
		t.Fatal(err)
	}
	if status := waitDone(t, pool, id); status != run.StatusFailed {
		t.Fatalf("status %q", status)
	}
	if tools.CallCount() != 0 {
		t.Fatalf("interrupted action called again: %d", tools.CallCount())
	}
	if e := runError(t, pool, id); !strings.Contains(e, "outcome unknown") {
		t.Fatalf("error %q", e)
	}
}

func TestApprovalAfterSuspendDoesNothing(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, act)
	eng, tools, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"summary":"Disk full"}`}})
	ctx := context.Background()
	id, wait, fp := waitApproval(t, eng, ch)

	if err := registry.Suspend(ctx, pool, "alert-triage", "", "owner", "wrong drafts", false); err != nil {
		t.Fatal(err)
	}
	if err := eng.Resolve(ctx, wait, "lead", fp, true); !errors.Is(err, run.ErrSuspended) {
		t.Fatalf("approve after suspend: %v", err)
	}
	if status := waitDone(t, pool, id); status != run.StatusSuspended {
		t.Fatalf("status %q", status)
	}
	if tools.CallCount() != 0 {
		t.Fatalf("action ran after suspend: %d", tools.CallCount())
	}
	if ch.PostCount() != 1 {
		t.Fatalf("people not told: posts %d", ch.PostCount())
	}
}

func TestSuspendCancelsOpenWaits(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, act)
	eng, tools, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"summary":"Disk full"}`}})
	ctx := context.Background()
	id, wait, fp := waitApproval(t, eng, ch)

	if err := registry.Suspend(ctx, pool, "alert-triage", "sre", "owner", "wrong drafts", false); err != nil {
		t.Fatal(err)
	}
	if n, err := eng.CancelWaits(ctx, "alert-triage", "sre", "owner"); err != nil || n != 1 {
		t.Fatalf("cancel waits: %d %v", n, err)
	}
	if status := waitDone(t, pool, id); status != run.StatusSuspended {
		t.Fatalf("status %q", status)
	}
	if err := eng.Resolve(ctx, wait, "lead", fp, true); !errors.Is(err, run.ErrWaitClosed) {
		t.Fatalf("approve a cancelled wait: %v", err)
	}
	if tools.CallCount() != 0 || ch.PostCount() != 1 {
		t.Fatalf("tool calls %d, posts %d", tools.CallCount(), ch.PostCount())
	}
}
