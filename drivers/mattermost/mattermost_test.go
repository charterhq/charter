package mattermost_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charterhq/charter/drivers/mattermost"
	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/signing"
)

func fakeMM(t *testing.T, got *[]map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/posts" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var p map[string]any
		_ = json.Unmarshal(b, &p)
		*got = append(*got, p)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"post1"}`))
	}))
}

func TestPostToThread(t *testing.T) {
	var got []map[string]any
	srv := fakeMM(t, &got)
	defer srv.Close()
	c := mattermost.New(mattermost.Config{URL: srv.URL, Token: "tok"})

	ref, err := c.Post(context.Background(), driver.Message{
		RunID: "r1", Agent: "alert-triage", Target: driver.Target{Channel: "ch1", Thread: "root1"},
		Data: json.RawMessage(`{"verdict":"noise"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "post1" || got[0]["channel_id"] != "ch1" || got[0]["root_id"] != "root1" ||
		!strings.Contains(got[0]["message"].(string), `"verdict": "noise"`) {
		t.Fatalf("ref %+v post %v", ref, got)
	}
	if _, err := c.Post(context.Background(), driver.Message{RunID: "r2"}); err == nil {
		t.Fatal("post without channel accepted")
	}
}

func TestApprovalButtonsAreSigned(t *testing.T) {
	var got []map[string]any
	srv := fakeMM(t, &got)
	defer srv.Close()
	secret := []byte("s3cret")
	c := mattermost.New(mattermost.Config{URL: srv.URL, Token: "tok", CallbackURL: "https://core.example", Secret: secret, DefaultChannel: "ch"})

	_, err := c.AskApproval(context.Background(), driver.ApprovalRequest{
		Message: driver.Message{RunID: "r1", Agent: "a"}, WaitID: "w1", Fingerprint: "fp1",
		Action: json.RawMessage(`{"summary":"x"}`), Deadline: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	actions := got[0]["props"].(map[string]any)["attachments"].([]any)[0].(map[string]any)["actions"].([]any)
	approve := actions[0].(map[string]any)["integration"].(map[string]any)
	ctx := approve["context"].(map[string]any)
	if approve["url"] != "https://core.example/v1/waits/w1/resolve" {
		t.Fatalf("url %v", approve["url"])
	}
	if !signing.Verify(secret, "w1", "fp1", "approve", ctx["sig"].(string)) {
		t.Fatal("approve button signature does not verify")
	}
	if signing.Verify(secret, "w1", "fp1", "reject", ctx["sig"].(string)) {
		t.Fatal("signature is not bound to the decision")
	}
}
