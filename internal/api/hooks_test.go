package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/run"
)

// republish applies a second version with connections over the one setup applied.
func republish(t *testing.T, e env, p run.Package) {
	t.Helper()
	b, _ := json.Marshal(p)
	if _, err := registry.Apply(context.Background(), e.pool, registry.Agent{Name: "alert-triage", Team: "sre", Owner: "o"}, "1.1.0", "sha2", b); err != nil {
		t.Fatal(err)
	}
}

func TestHookByConnection(t *testing.T) {
	e := setup(t, advise, `{"verdict":"noise"}`)
	scn := advise
	scn.Triggers = []run.Trigger{{Type: "event", Source: "alerts"}, {Type: "event", Source: "tickets"}}
	republish(t, e, run.Package{
		Scenarios: map[string]run.Scenario{"triage": scn},
		Connections: map[string]run.Connection{"sre": {Name: "sre", Scenarios: []string{"triage"}, Status: run.ConnActive,
			Channels: map[string]string{"reply": "chan-sre"},
			Triggers: map[string]run.ConnectionTrigger{"alerts": {Receiver: "alertmanager"}}}},
	})

	code, out := call(t, e, "POST", "/v1/hooks/alert-triage/sre/alerts", "kestra-token", json.RawMessage(webhook))
	if code != http.StatusAccepted {
		t.Fatalf("hook: %d %v", code, out)
	}
	runs := out["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("one firing alert, one run: %v", runs)
	}
	id := runs[0].(map[string]any)["run_id"].(string)
	if status := waitStatus(t, e, id); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
	if p, _ := e.ch.LastPost(); p.Target.Channel != "chan-sre" {
		t.Fatalf("answer target: %+v", p.Target)
	}
	var trig, conn string
	_ = e.pool.QueryRow(context.Background(), `SELECT envelope -> 'trigger' ->> 'source', connection FROM runs WHERE id = $1`, id).Scan(&trig, &conn)
	if trig != "alerts" || conn != "sre" {
		t.Fatalf("envelope source %q, connection %q", trig, conn)
	}

	// the scenario knows "tickets", but this team did not bind it
	if code, out := call(t, e, "POST", "/v1/hooks/alert-triage/sre/tickets", "kestra-token", map[string]any{"labels": map[string]any{}}); code != http.StatusNotFound {
		t.Fatalf("unbound source: %d %v", code, out)
	}
	if code, _ := call(t, e, "POST", "/v1/hooks/alert-triage/nope/alerts", "kestra-token", json.RawMessage(webhook)); code != http.StatusNotFound {
		t.Fatalf("unknown connection: %d", code)
	}
	if code, _ := call(t, e, "POST", "/v1/hooks/alert-triage/sre/alerts", "", json.RawMessage(webhook)); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
}

func TestCallWaitsForOutput(t *testing.T) {
	e := setup(t, advise, `{"verdict":"real"}`)
	callable := advise
	callable.Triggers = []run.Trigger{{Type: "call", Source: "agents"}}
	republish(t, e, run.Package{
		Scenarios:   map[string]run.Scenario{"triage": callable, "quiet": advise},
		Connections: map[string]run.Connection{"sre": {Name: "sre", Scenarios: []string{"triage", "quiet"}, Status: run.ConnActive}},
	})
	code, out := call(t, e, "POST", "/v1/agents/alert-triage/scenarios/triage/call?connection=sre&wait=30s", "cli-token",
		map[string]any{"labels": map[string]any{"alertname": "DiskFull"}})
	if code != http.StatusOK || out["status"] != run.StatusOK || out["output"].(map[string]any)["verdict"] != "real" {
		t.Fatalf("call: %d %v", code, out)
	}
	if code, out := call(t, e, "POST", "/v1/agents/alert-triage/scenarios/quiet/call?connection=sre", "cli-token",
		map[string]any{"labels": map[string]any{}}); code != http.StatusConflict {
		t.Fatalf("scenario without a call trigger: %d %v", code, out)
	}
	if code, _ := call(t, e, "POST", "/v1/agents/alert-triage/scenarios/triage/call", "cli-token", map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("no connection: %d", code)
	}
}

// The old launch path checks the connection once the package declares connections.
func TestLaunchChecksConnection(t *testing.T) {
	e := setup(t, advise, `{"verdict":"noise"}`)
	republish(t, e, run.Package{
		Scenarios:   map[string]run.Scenario{"triage": advise},
		Connections: map[string]run.Connection{"sre": {Name: "sre", Scenarios: []string{"triage"}, Status: run.ConnPaused}},
	})
	if code, out := launch(t, e, "kestra-token"); code != http.StatusConflict {
		t.Fatalf("paused connection: %d %v", code, out)
	}
}

func TestConversationAccess(t *testing.T) {
	e := setup(t, advise, `{"reply":"hi"}`)
	talk := run.Scenario{Name: "talk", Level: 2, ModelClass: "c", Autonomy: "advise", Memory: "thread",
		Triggers: []run.Trigger{{Type: "message", Source: "chat"}}}
	republish(t, e, run.Package{
		Scenarios: map[string]run.Scenario{"talk": talk},
		Connections: map[string]run.Connection{"sre": {Name: "sre", Scenarios: []string{"talk"}, Status: run.ConnActive,
			Triggers: map[string]run.ConnectionTrigger{"chat": {Users: []string{"jane.doe"}}}}},
	})
	msg := func(author string) map[string]any {
		return map[string]any{"conversation": "th1", "message_id": "p-" + author, "author": author, "text": "hello",
			"reply": map[string]string{"channel": "c1", "thread": "th1"}}
	}
	if code, out := call(t, e, "POST", "/v1/conversations/alert-triage/sre/chat", "kestra-token", msg("stranger")); code != http.StatusForbidden {
		t.Fatalf("stranger: %d %v", code, out)
	}
	code, out := call(t, e, "POST", "/v1/conversations/alert-triage/sre/chat", "kestra-token", msg("jane.doe"))
	if code != http.StatusAccepted {
		t.Fatalf("allowed author: %d %v", code, out)
	}
	if status := waitStatus(t, e, out["run_id"].(string)); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
	if p, _ := e.ch.LastPost(); p.Target.Thread != "th1" {
		t.Fatalf("answer target: %+v", p.Target)
	}
	if code, _ := call(t, e, "POST", "/v1/conversations/alert-triage/sre/chat", "kestra-token", map[string]any{"text": "x"}); code != http.StatusBadRequest {
		t.Fatalf("missing fields: %d", code)
	}
}
