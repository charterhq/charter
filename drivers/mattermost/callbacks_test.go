package mattermost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/signing"
)

type fakeApprovals struct {
	err     error
	waitID  string
	by, fp  string
	approve bool
	calls   int
}

func (f *fakeApprovals) Resolve(_ context.Context, waitID, by, fp string, approve bool) error {
	f.calls++
	f.waitID, f.by, f.fp, f.approve = waitID, by, fp, approve
	return f.err
}

func press(t *testing.T, h http.Handler, path string, ctx map[string]string) (int, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"user_id": "u1", "user_name": "lead", "context": ctx})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b))))
	var out struct {
		Text string `json:"ephemeral_text"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out.Text
}

func TestButtonCallback(t *testing.T) {
	secret := []byte("s")
	c := New(Config{URL: "http://mm", Token: "t", Secret: secret, ApprovedText: "Подтвердил @%s"})
	signed := func(decision, signedAs string) map[string]string {
		return map[string]string{"wait_id": "w1", "fingerprint": "fp", "decision": decision, "sig": signing.Sign(secret, "w1", "fp", signedAs)}
	}

	a := &fakeApprovals{}
	if code, _ := press(t, c.Handler(a), "/actions/w1", signed("approve", "reject")); code != http.StatusForbidden || a.calls != 0 {
		t.Fatalf("forged decision: %d, calls %d", code, a.calls)
	}
	if code, _ := press(t, c.Handler(a), "/actions/w2", signed("approve", "approve")); code != http.StatusForbidden {
		t.Fatalf("other wait: %d", code)
	}
	code, text := press(t, c.Handler(a), "/actions/w1", signed("approve", "approve"))
	if code != http.StatusOK || text != "Подтвердил @lead" || !a.approve || a.by != "lead" || a.fp != "fp" || a.waitID != "w1" {
		t.Fatalf("approve: %d %q %+v", code, text, a)
	}

	a = &fakeApprovals{err: driver.ErrWaitClosed}
	if _, text := press(t, c.Handler(a), "/actions/w1", signed("reject", "reject")); !strings.Contains(text, "already resolved") || a.approve {
		t.Fatalf("closed: %q", text)
	}
	a = &fakeApprovals{err: driver.ErrFingerprint}
	if _, text := press(t, c.Handler(a), "/actions/w1", signed("approve", "approve")); !strings.Contains(text, "Parameters changed") {
		t.Fatalf("changed: %q", text)
	}
	rec := httptest.NewRecorder()
	c.Handler(a).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/actions/w1", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET: %d", rec.Code)
	}
}
