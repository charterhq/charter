package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/api"
	"github.com/charterhq/charter/internal/pkg"
	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/testdb"
)

const (
	refundOK    = `{"category": "billing", "queue": "billing", "reason": "charged twice, wants a refund"}`
	refundWrong = `{"category": "question", "queue": "l1", "reason": "asks about a refund"}`
	judgePass   = `{"verdicts": [{"criterion": "x", "pass": true, "reason": "says charged twice"}]}`
)

type evalOut struct {
	Pass     bool              `json:"pass"`
	Problem  string            `json:"problem"`
	Attempts int               `json:"attempts"`
	Checks   []pkg.CheckResult `json:"checks"`
}

func evalServer(t *testing.T, outputs ...string) (*httptest.Server, *stub.Model) {
	t.Helper()
	pool := testdb.New(t)
	model := &stub.Model{Outputs: outputs}
	drv, _, _, _ := stub.Set(model)
	eng, err := run.New(pool, drv, run.Config{FetchPollInterval: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	s := &api.Server{Pool: pool, Engine: eng, Tokens: map[string]string{"ci-token": "gitlab-ci", "cli-token": "lead"},
		Publishers: map[string]bool{"gitlab-ci": true}, Log: slog.Default()}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, model
}

func postEval(t *testing.T, srv *httptest.Server, token, query string) (int, evalOut) {
	t.Helper()
	archive, err := pkg.Pack("../../examples/ticket-triage")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/eval?"+query, bytes.NewReader(archive))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out evalOut
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestEvalPasses(t *testing.T) {
	srv, model := evalServer(t, refundOK, judgePass)
	code, out := postEval(t, srv, "ci-token", "scenario=classify&case=refund")
	if code != http.StatusOK || !out.Pass || out.Attempts != 1 {
		t.Fatalf("eval: %d %+v", code, out)
	}
	if len(out.Checks) != 4 { // expected category, expected queue, matches, judge
		t.Fatalf("checks: %+v", out.Checks)
	}
	if model.CallCount() != 2 {
		t.Fatalf("model calls %d, want case + judge", model.CallCount())
	}
	var judged map[string]any
	_ = json.Unmarshal(model.InputAt(1), &judged)
	if judged["criteria"].([]any)[0] != "the reason mentions that the customer was charged twice" || judged["output"] == nil {
		t.Fatalf("judge input: %s", model.InputAt(1))
	}
}

func TestEvalFailsOnExpected(t *testing.T) {
	srv, _ := evalServer(t, refundWrong, judgePass)
	_, out := postEval(t, srv, "ci-token", "scenario=classify&case=refund")
	if out.Pass || out.Checks[0].Pass || out.Checks[0].Detail != `got "question"` {
		t.Fatalf("eval: %+v", out)
	}
}

func TestEvalRetriesInvalidOutput(t *testing.T) {
	srv, _ := evalServer(t, `{"category": "nope"}`, refundOK, judgePass)
	_, out := postEval(t, srv, "ci-token", "scenario=classify&case=refund")
	if !out.Pass || out.Attempts != 2 {
		t.Fatalf("eval: %+v", out)
	}
	srv, _ = evalServer(t, `{"category": "nope"}`)
	_, out = postEval(t, srv, "ci-token", "scenario=classify&case=refund")
	if out.Pass || out.Problem == "" || len(out.Checks) != 0 {
		t.Fatalf("invalid twice: %+v", out)
	}
}

func TestEvalAccess(t *testing.T) {
	srv, _ := evalServer(t, refundOK)
	if code, _ := postEval(t, srv, "cli-token", "scenario=classify&case=refund"); code != http.StatusForbidden {
		t.Fatalf("non-publisher: %d", code)
	}
	if code, _ := postEval(t, srv, "ci-token", "scenario=classify&case=nope"); code != http.StatusNotFound {
		t.Fatalf("unknown case: %d", code)
	}
}

func TestEvalWithConnectionContext(t *testing.T) {
	dir := copyExample(t)
	edit := func(rel, old, new string) {
		f := filepath.Join(dir, rel)
		b, _ := os.ReadFile(f)
		if !strings.Contains(string(b), old) {
			t.Fatalf("%s: no %q", rel, old)
		}
		_ = os.WriteFile(f, []byte(strings.Replace(string(b), old, new, 1)), 0o644)
	}
	edit("scenarios/classify/scenario.yml", "  level: 1\n", "  level: 1\n  params:\n    team: {type: string, default: anyone}\n    tone: {type: string, default: plain}\n")
	edit("connections/support.yml", "  status: shadow\n", "  status: shadow\n  params: {team: support}\n  knowledge:\n    - {path: knowledge/queues.md}\n")
	edit("scenarios/classify/eval/cases/refund.yml", "expected:", "params: {tone: polite}\nexpected:")
	_ = os.MkdirAll(filepath.Join(dir, "knowledge"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "knowledge", "queues.md"), []byte("billing queue takes refunds"), 0o644)
	archive, err := pkg.Pack(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv, model := evalServer(t, refundOK, judgePass)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/eval?scenario=classify&case=refund&connection=support", bytes.NewReader(archive))
	req.Header.Set("Authorization", "Bearer ci-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("eval: %d", resp.StatusCode)
	}
	got := model.Instructions[0]
	for _, want := range []string{`"team": "support"`, `"tone": "polite"`, "billing queue takes refunds"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions lack %s:\n%s", want, got)
		}
	}
}
