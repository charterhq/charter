package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/api"
	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/testdb"
)

type env struct {
	srv  *httptest.Server
	pool *pgxpool.Pool
	ch   *stub.Channel
	s    *api.Server
}

func setup(t *testing.T, scn run.Scenario, outputs ...string) env {
	t.Helper()
	pool := testdb.New(t)
	pkg, _ := json.Marshal(run.Package{Scenarios: map[string]run.Scenario{scn.Name: scn}})
	if _, err := registry.Apply(context.Background(), pool, registry.Agent{Name: "alert-triage", Team: "sre", Owner: "o"}, "1.0.0", "sha", pkg); err != nil {
		t.Fatal(err)
	}
	drv, _, ch, _ := stub.Set(&stub.Model{Outputs: outputs})
	eng, err := run.New(pool, drv, run.Config{FetchPollInterval: 50 * time.Millisecond, FetchCooldown: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.StopAndCancel(context.Background()) })
	s := &api.Server{Pool: pool, Engine: eng, Tokens: map[string]string{"kestra-token": "kestra", "cli-token": "lead"},
		Formats: drv.Formats, Log: slog.Default()}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return env{srv: srv, pool: pool, ch: ch, s: s}
}

func call(t *testing.T, e env, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func waitStatus(t *testing.T, e env, runID string) string {
	t.Helper()
	for i := 0; i < 200; i++ {
		var state, status string
		_ = e.pool.QueryRow(context.Background(), `SELECT state, status FROM runs WHERE id = $1`, runID).Scan(&state, &status)
		if state == "done" {
			return status
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("run not done")
	return ""
}

var advise = run.Scenario{Name: "triage", Level: 1, ModelClass: "c", Autonomy: "advise",
	InputSchema:  json.RawMessage(`{"type":"object","required":["labels"]}`),
	OutputSchema: json.RawMessage(`{"type":"object","required":["verdict"]}`)}

const webhook = `{"groupKey":"g1","alerts":[
 {"status":"firing","fingerprint":"fp1","startsAt":"2026-11-18T10:00:00Z","labels":{"alertname":"DiskFull"}},
 {"status":"resolved","fingerprint":"fp2","startsAt":"2026-11-18T09:00:00Z","labels":{"alertname":"Old"}}]}`

func launch(t *testing.T, e env, token string) (int, map[string]any) {
	t.Helper()
	return call(t, e, "POST", "/v1/launches/alertmanager?agent=alert-triage&scenario=triage&connection=sre", token, json.RawMessage(webhook))
}

func TestLaunchAlertmanager(t *testing.T) {
	e := setup(t, advise, `{"verdict":"noise"}`)
	if code, _ := launch(t, e, ""); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	code, out := launch(t, e, "kestra-token")
	if code != http.StatusAccepted {
		t.Fatalf("launch: %d %v", code, out)
	}
	runs := out["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("resolved alert must not start a run: %v", runs)
	}
	id := runs[0].(map[string]any)["run_id"].(string)
	if status := waitStatus(t, e, id); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
	_, again := launch(t, e, "kestra-token")
	if dup := again["runs"].([]any)[0].(map[string]any); dup["run_id"] != id || dup["duplicate"] != true {
		t.Fatalf("repeated webhook: %v", dup)
	}
	code, doc := call(t, e, "GET", "/v1/runs/"+id, "cli-token", nil)
	if code != http.StatusOK || len(doc["steps"].([]any)) == 0 || doc["version"] != "1.0.0" {
		t.Fatalf("get run: %d %v", code, doc)
	}
}

func TestInputSchemaRejectsBadInput(t *testing.T) {
	e := setup(t, advise, `{"verdict":"noise"}`)
	code, out := call(t, e, "POST", "/v1/launches/raw?agent=alert-triage&scenario=triage&connection=sre", "kestra-token",
		map[string]any{"no_labels": true})
	if code != http.StatusAccepted {
		t.Fatalf("%d %v", code, out)
	}
	id := out["runs"].([]any)[0].(map[string]any)["run_id"].(string)
	if status := waitStatus(t, e, id); status != run.StatusFailed {
		t.Fatalf("status %s", status)
	}
}

func TestSuspendViaAPI(t *testing.T) {
	e := setup(t, advise, `{"verdict":"noise"}`)
	if code, _ := call(t, e, "POST", "/v1/agents/alert-triage/suspend", "cli-token", map[string]string{}); code != http.StatusBadRequest {
		t.Fatalf("suspend without reason: %d", code)
	}
	if code, out := call(t, e, "POST", "/v1/agents/alert-triage/suspend", "cli-token", map[string]string{"reason": "noisy"}); code != http.StatusOK {
		t.Fatalf("suspend: %d %v", code, out)
	}
	_, out := launch(t, e, "kestra-token")
	id := out["runs"].([]any)[0].(map[string]any)["run_id"].(string)
	if status := waitStatus(t, e, id); status != run.StatusSuspended {
		t.Fatalf("status %s", status)
	}
	_, agents := call(t, e, "GET", "/v1/agents", "cli-token", nil)
	sw := agents["agents"].([]any)[0].(map[string]any)["switches"].([]any)[0].(map[string]any)
	if sw["state"] != "off_owner" || sw["changed_by"] != "lead" {
		t.Fatalf("switch %v", sw)
	}
}

func TestApprovalViaAPI(t *testing.T) {
	scn := run.Scenario{Name: "triage", Level: 1, ModelClass: "c", Autonomy: "act", Action: "jira.create_issue",
		Approval: &run.Approval{Timeout: run.Duration(time.Hour), OnTimeout: "cancel"}}
	e := setup(t, scn, `{"summary":"x"}`)
	_, out := launch(t, e, "kestra-token")
	id := out["runs"].([]any)[0].(map[string]any)["run_id"].(string)

	var req struct{ wait, fp string }
	for i := 0; i < 200 && req.wait == ""; i++ {
		if r, ok := e.ch.LastApproval(); ok {
			req.wait, req.fp = r.WaitID, r.Fingerprint
		}
		time.Sleep(50 * time.Millisecond)
	}
	path := "/v1/waits/" + req.wait + "/resolve"
	body := map[string]any{"decision": "approve", "fingerprint": req.fp}
	if code, _ := call(t, e, "POST", path, "", body); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	if code, _ := call(t, e, "POST", path, "cli-token", map[string]any{"decision": "approve", "fingerprint": "other"}); code != http.StatusConflict {
		t.Fatalf("wrong fingerprint: %d", code)
	}
	code, resp := call(t, e, "POST", path, "cli-token", body)
	if code != http.StatusOK || resp["by"] != "lead" {
		t.Fatalf("approve: %d %v", code, resp)
	}
	if status := waitStatus(t, e, id); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
	if code, _ := call(t, e, "POST", path, "cli-token", body); code != http.StatusConflict {
		t.Fatalf("second decision: %d", code)
	}
}

type fakeCallbacks struct{ got chan string }

func (f fakeCallbacks) Name() string { return "fake" }
func (f fakeCallbacks) Handler(a driver.Approvals) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.got <- r.URL.Path
		_ = a.Resolve(r.Context(), "nope", "x", "fp", true)
		w.WriteHeader(http.StatusTeapot)
	})
}

func TestDriverCallbacksMounted(t *testing.T) {
	pool := testdb.New(t)
	drv, _, _, _ := stub.Set(&stub.Model{})
	eng, err := run.New(pool, drv, run.Config{})
	if err != nil {
		t.Fatal(err)
	}
	cb := fakeCallbacks{got: make(chan string, 1)}
	srv := httptest.NewServer((&api.Server{Pool: pool, Engine: eng, Callbacks: []driver.Callbacks{cb}, Log: slog.Default()}).Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/v1/drivers/fake/actions/w1", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot || <-cb.got != "/actions/w1" {
		t.Fatalf("callback not served without a token under the driver prefix: %d", resp.StatusCode)
	}
}
