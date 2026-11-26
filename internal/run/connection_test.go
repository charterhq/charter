package run_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/run"
)

func applyPackage(t *testing.T, pool *pgxpool.Pool, version string, p run.Package) {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Apply(context.Background(), pool,
		registry.Agent{Name: "alert-triage", Team: "sre", Owner: "owner"}, version, "abc123", b); err != nil {
		t.Fatal(err)
	}
}

func withTriggers(s run.Scenario, ts ...run.Trigger) run.Scenario {
	s.Triggers = ts
	return s
}

var (
	alertsTrigger = run.Trigger{Type: "event", Source: "alerts"}
	smokeTrigger  = run.Trigger{Type: "schedule", Source: "smoke", Schedule: "30 10 * * *"}
)

func TestScheduleTick(t *testing.T) {
	pool := newDB(t)
	scn := withTriggers(advise, alertsTrigger, smokeTrigger)
	applyPackage(t, pool, "1.0.0", run.Package{
		Scenarios: map[string]run.Scenario{"triage": scn},
		Connections: map[string]run.Connection{
			"sre": {Name: "sre", Scenarios: []string{"triage"}, Status: run.ConnActive,
				Channels: map[string]string{"reply": "chan-sre"},
				Triggers: map[string]run.ConnectionTrigger{"smoke": {Input: json.RawMessage(`{"probe":true}`)}}},
			// does not bind smoke: the schedule does not launch for this team
			"ops": {Name: "ops", Scenarios: []string{"triage"}, Status: run.ConnActive,
				Triggers: map[string]run.ConnectionTrigger{"alerts": {}}},
			"qa": {Name: "qa", Scenarios: []string{"triage"}, Status: run.ConnPaused,
				Triggers: map[string]run.ConnectionTrigger{"smoke": {}}},
		},
	})
	eng, _, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"verdict":"ok"}`}})
	at := time.Date(2026, 11, 26, 10, 34, 0, 0, time.UTC)

	due, err := eng.TickAt(context.Background(), at)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Connection != "sre" || due[0].Duplicate || !due[0].At.Equal(at.Add(-4*time.Minute)) {
		t.Fatalf("due: %+v", due)
	}
	if status := waitDone(t, pool, due[0].RunID); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
	var env run.Envelope
	var envJSON []byte
	if err := pool.QueryRow(context.Background(), `SELECT envelope FROM runs WHERE id = $1`, due[0].RunID).Scan(&envJSON); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(envJSON, &env)
	var payload map[string]any
	_ = json.Unmarshal(env.Payload, &payload)
	if env.Trigger.Type != "schedule" || env.Trigger.Source != "smoke" || payload["probe"] != true {
		t.Fatalf("envelope: %+v", env)
	}
	if p, _ := ch.LastPost(); p.Target.Channel != "chan-sre" {
		t.Fatalf("answer must go to the reply channel: %+v", p.Target)
	}

	// a second core or a repeated tick launches nothing new
	again, err := eng.TickAt(context.Background(), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || !again[0].Duplicate || again[0].RunID != due[0].RunID {
		t.Fatalf("repeated tick: %+v", again)
	}
	// outside the lookback window the occurrence is gone
	if late, _ := eng.TickAt(context.Background(), at.Add(time.Hour)); len(late) != 0 {
		t.Fatalf("late tick: %+v", late)
	}
}

func TestScheduleOverrideAndZone(t *testing.T) {
	pool := newDB(t)
	scn := withTriggers(advise, smokeTrigger)
	applyPackage(t, pool, "1.0.0", run.Package{
		Scenarios: map[string]run.Scenario{"triage": scn},
		Connections: map[string]run.Connection{
			"sre": {Name: "sre", Scenarios: []string{"triage"}, Status: run.ConnShadow,
				Triggers: map[string]run.ConnectionTrigger{"smoke": {Schedule: "CRON_TZ=Europe/Moscow 0 9 * * *"}}},
		},
	})
	eng, _, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"verdict":"ok"}`}})
	// 09:00 Moscow = 06:00 UTC; the package cron 10:30 UTC is overridden
	due, err := eng.TickAt(context.Background(), time.Date(2026, 11, 26, 6, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || !due[0].At.Equal(time.Date(2026, 11, 26, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("due: %+v", due)
	}
	if status := waitDone(t, pool, due[0].RunID); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
	if ch.PostCount() != 0 {
		t.Fatal("shadow without a shadow channel must not post")
	}
}

func TestAcceptChecksConnection(t *testing.T) {
	pool := newDB(t)
	applyPackage(t, pool, "1.0.0", run.Package{
		Scenarios: map[string]run.Scenario{"triage": withTriggers(advise, alertsTrigger), "other": advise},
		Connections: map[string]run.Connection{
			"sre":    {Name: "sre", Scenarios: []string{"triage"}, Status: run.ConnActive},
			"paused": {Name: "paused", Scenarios: []string{"triage"}, Status: run.ConnPaused},
		},
	})
	eng, _, _, _ := startEngine(t, pool, &stub.Model{})
	ctx := context.Background()
	for _, tc := range []struct {
		conn, scenario string
		want           error
	}{
		{"nope", "triage", run.ErrNoConnection},
		{"sre", "other", run.ErrNotEnabled},
		{"paused", "triage", run.ErrPaused},
	} {
		env := envelope("d-"+tc.conn+tc.scenario, "")
		env.Connection = tc.conn
		if _, _, err := eng.Accept(ctx, "alert-triage", tc.scenario, env); !errors.Is(err, tc.want) {
			t.Errorf("%s/%s: %v, want %v", tc.conn, tc.scenario, err, tc.want)
		}
	}
}

// Shadow: the answer goes to the shadow channel instead of the launch's thread, and an act scenario
// does not act.
func TestShadowNeverActs(t *testing.T) {
	pool := newDB(t)
	act := run.Scenario{Name: "fix", Level: 2, ModelClass: "c", Autonomy: "act", Action: "jira.create_issue",
		OutputSchema: json.RawMessage(`{"type":"object"}`)}
	applyPackage(t, pool, "1.0.0", run.Package{
		Scenarios: map[string]run.Scenario{"fix": act},
		Connections: map[string]run.Connection{
			"sre": {Name: "sre", Scenarios: []string{"fix"}, Status: run.ConnShadow, Channels: map[string]string{"shadow": "chan-shadow"}},
		},
	})
	eng, tools, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"summary":"x"}`}})
	env := envelope("d-1", "")
	env.Reply = &driver.Target{Channel: "alert-chan", Thread: "root"}
	id, _, err := eng.Accept(context.Background(), "alert-triage", "fix", env)
	if err != nil {
		t.Fatal(err)
	}
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
	if tools.CallCount() != 0 {
		t.Fatal("shadow must not execute the action")
	}
	if p, _ := ch.LastPost(); p.Target.Channel != "chan-shadow" || p.Target.Thread != "" {
		t.Fatalf("shadow answer target: %+v", p.Target)
	}
}

// A package from before connections were stored takes any connection name.
func TestLegacyPackageTakesAnyConnection(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, advise)
	eng, _, _, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"verdict":"ok"}`}})
	env := envelope("d-1", "")
	env.Connection = "whatever"
	if _, _, err := eng.Accept(context.Background(), "alert-triage", "triage", env); err != nil {
		t.Fatal(err)
	}
	if due, err := eng.Tick(context.Background()); err != nil || len(due) != 0 {
		t.Fatalf("legacy package must not be scheduled: %v %v", due, err)
	}
}
