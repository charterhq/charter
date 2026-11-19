package bifrost_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/charterhq/charter/drivers/bifrost"
	"github.com/charterhq/charter/internal/driver"
)

func TestRunAgainstFakeGateway(t *testing.T) {
	var got map[string]any
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"model":"m-real","choices":[{"message":{"content":"` + "```json\\n{\\\"verdict\\\":\\\"noise\\\"}\\n```" + `"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":3}},
			"extra_fields":{"provider":"sail"}}`))
	}))
	defer srv.Close()

	m := bifrost.New(bifrost.Config{URL: srv.URL, Key: "k", Classes: map[string]string{"fast": "alias-fast"},
		Params: map[string]map[string]any{"fast": {"max_tokens": 100}}})
	res, err := m.Run(context.Background(), driver.StepRequest{
		RunID: "r1", Agent: "a", Scenario: "s", ModelClass: "fast", Instructions: "do it",
		Params: map[string]any{"reasoning": "low", "max_output_tokens": 500, "response_format": "json"},
		Input:  json.RawMessage(`{"x":1}`), OutputSchema: json.RawMessage(`{"type":"object"}`),
		Feedback: []string{"missing verdict"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Output) != `{"verdict":"noise"}` {
		t.Fatalf("output %s", res.Output)
	}
	u := res.Usage
	if u.InputTokens != 10 || u.OutputTokens != 5 || u.CachedTokens != 2 || u.ReasoningTokens != 3 ||
		u.Model != "m-real" || u.Provider != "sail" || u.FinishReason != "stop" {
		t.Fatalf("usage %+v", u)
	}
	if got["model"] != "alias-fast" || got["max_tokens"] != float64(500) || got["reasoning_effort"] != "low" {
		t.Fatalf("request %v", got)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 || !strings.Contains(msgs[2].(map[string]any)["content"].(string), "missing verdict") {
		t.Fatalf("feedback not sent: %v", msgs)
	}
	if hdr.Get("Authorization") != "Bearer k" || hdr.Get("x-bf-session-id") != "r1" {
		t.Fatalf("headers %v", hdr)
	}
}

func TestErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()
	m := bifrost.New(bifrost.Config{URL: srv.URL, Classes: map[string]string{"fast": "x"}})

	if _, err := m.Run(context.Background(), driver.StepRequest{ModelClass: "fast"}); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("err %v", err)
	}
	if _, err := m.Run(context.Background(), driver.StepRequest{ModelClass: "unknown"}); err == nil {
		t.Fatal("unknown class accepted")
	}
	if _, err := m.Run(context.Background(), driver.StepRequest{ModelClass: "fast", Tools: []string{"t"}}); !errors.Is(err, bifrost.ErrToolsUnsupported) {
		t.Fatalf("err %v", err)
	}
}

// Live call to a real gateway: CHARTER_BIFROST_URL, CHARTER_BIFROST_KEY, CHARTER_BIFROST_MODEL.
func TestLive(t *testing.T) {
	url, key, model := os.Getenv("CHARTER_BIFROST_URL"), os.Getenv("CHARTER_BIFROST_KEY"), os.Getenv("CHARTER_BIFROST_MODEL")
	if url == "" || key == "" || model == "" {
		t.Skip("live gateway is not configured")
	}
	m := bifrost.New(bifrost.Config{URL: url, Key: key, Classes: map[string]string{"c": model},
		Params: map[string]map[string]any{"c": {"max_tokens": 2000}}})
	res, err := m.Run(context.Background(), driver.StepRequest{
		RunID: "charter-live-test", Agent: "charter-test", Scenario: "live", ModelClass: "c",
		Instructions: "Classify the ticket.",
		Input:        json.RawMessage(`{"ticket":{"subject":"Refund","body":"I was charged twice"}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["category"],"properties":{"category":{"enum":["billing","bug","other"]}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(res.Output, &out); err != nil || out["category"] == nil {
		t.Fatalf("output %s", res.Output)
	}
	if res.Usage.InputTokens == 0 || res.Usage.Model == "" {
		t.Fatalf("usage %+v", res.Usage)
	}
	t.Logf("category=%v model=%s provider=%s tokens=%d/%d", out["category"], res.Usage.Model, res.Usage.Provider,
		res.Usage.InputTokens, res.Usage.OutputTokens)
}

func TestCost(t *testing.T) {
	cases := []struct {
		name, usage string
		want        float64
	}{
		{"gateway number", `"cost":0.5`, 0.5},
		{"gateway object", `"cost":{"total_cost":0.25}`, 0.25},
		{"price by provider and model", ``, (800*0.30 + 200*0.006 + 500*1.20) / 1e6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			extra := ""
			if c.usage != "" {
				extra = "," + c.usage
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"model":"deepseek-v4.1-flash","choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],
					"usage":{"prompt_tokens":1000,"completion_tokens":500,"prompt_tokens_details":{"cached_tokens":200}` + extra + `},
					"extra_fields":{"provider":"opencode-zen"}}`))
			}))
			defer srv.Close()
			m := bifrost.New(bifrost.Config{URL: srv.URL, Key: "k", Classes: map[string]string{"c": "a"},
				Prices: map[string]bifrost.Price{
					"opencode-zen/deepseek-v4.1-flash": {Input: 0.30, CachedInput: 0.006, Output: 1.20},
					"opencode-zen":                     {Input: 9, Output: 9},
				}})
			res, err := m.Run(context.Background(), driver.StepRequest{ModelClass: "c", Input: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			if d := res.Usage.Cost - c.want; d > 1e-12 || d < -1e-12 {
				t.Fatalf("cost %v want %v", res.Usage.Cost, c.want)
			}
		})
	}
}
