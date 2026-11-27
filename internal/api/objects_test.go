package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/api"
	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/testdb"
)

const briefSchema = `{"type":"object","additionalProperties":false,"required":["work","trigger"],"properties":{
  "work":{"type":"string","title":"Work","description":"What job does the agent do?"},
  "trigger":{"type":"string","title":"Trigger","description":"What starts it?"}}}`

func setupObjects(t *testing.T, outputs ...string) (env, *api.Server) {
	t.Helper()
	pool := testdb.New(t)
	p := run.Package{
		Scenarios: map[string]run.Scenario{"intake": {Name: "intake", Level: 2, ModelClass: "c", Autonomy: "advise", Memory: "object",
			OutputSchema: json.RawMessage(`{"type":"object","required":["reply"]}`), Template: `{{.output.reply}}`,
			Triggers: []run.Trigger{{Type: "message", Source: "requests"}}}},
		Object: &run.ObjectSpec{Schema: briefSchema, Stages: []run.Stage{{ID: "request", Title: "Request"}}},
		Connections: map[string]run.Connection{"platform": {Name: "platform", Scenarios: []string{"intake"}, Status: run.ConnActive,
			Triggers: map[string]run.ConnectionTrigger{"requests": {Receiver: run.ReceiverWeb}}}},
	}
	b, _ := json.Marshal(p)
	if _, err := registry.Apply(context.Background(), pool, registry.Agent{Name: "builder", Team: "platform", Owner: "o"}, "1.0.0", "sha", b); err != nil {
		t.Fatal(err)
	}
	drv, _, ch, _ := stub.Set(&stub.Model{Outputs: outputs})
	eng, err := run.New(pool, drv, run.Config{FetchPollInterval: 50 * time.Millisecond, FetchCooldown: 10 * time.Millisecond, ScheduleInterval: -1})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.StopAndCancel(context.Background()) })
	s := &api.Server{Pool: pool, Engine: eng, Tokens: map[string]string{"alice-token": "alice", "bob-token": "bob", "platform-token": "ops"},
		Platform: map[string]bool{"ops": true}, Log: slog.Default()}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { s.StopStreams(); srv.Close() })
	return env{srv: srv, pool: pool, ch: ch}, s
}

type sse struct {
	id, event, data string
}

// stream reads server-sent events into a channel until the stream ends.
func stream(t *testing.T, e env, path, token string, header map[string]string) <-chan sse {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	out := make(chan sse, 100)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		var cur sse
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if cur.event != "" {
					out <- cur
				}
				cur = sse{}
			case strings.HasPrefix(line, "id: "):
				cur.id = line[4:]
			case strings.HasPrefix(line, "event: "):
				cur.event = line[7:]
			case strings.HasPrefix(line, "data: "):
				cur.data = line[6:]
			}
		}
	}()
	return out
}

// until reads events until one of the type arrives and returns the types seen.
func until(t *testing.T, c <-chan sse, typ string, match func(sse) bool) []sse {
	t.Helper()
	var seen []sse
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev, ok := <-c:
			if !ok {
				t.Fatalf("stream ended waiting for %s, seen %v", typ, seen)
			}
			seen = append(seen, ev)
			if ev.event == typ && (match == nil || match(ev)) {
				return seen
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %s, seen %v", typ, seen)
		}
	}
}

func TestObjectsOverHTTP(t *testing.T) {
	e, _ := setupObjects(t, `{"reply":"Noted. What starts it?","state_ops":[{"op":"set","field":"work","value":"triage alerts"}]}`)

	code, obj := call(t, e, http.MethodPost, "/v1/agents/builder/objects", "alice-token",
		map[string]any{"connection": "platform", "title": "New agent", "attrs": map[string]any{"team": "sre"}})
	if code != http.StatusCreated || obj["stage"] != "request" {
		t.Fatalf("create: %d %v", code, obj)
	}
	id := obj["id"].(string)
	fields := obj["fields"].([]any)
	if fields[0].(map[string]any)["question"] != "What job does the agent do?" {
		t.Fatalf("fields %v", fields)
	}

	// bob has no role: the object does not exist for him; the platform sees it
	if code, out := call(t, e, http.MethodGet, "/v1/objects/"+id, "bob-token", nil); code != http.StatusNotFound || out["code"] != "not_found" {
		t.Fatalf("bob: %d %v", code, out)
	}
	if code, _ := call(t, e, http.MethodGet, "/v1/objects/"+id, "platform-token", nil); code != http.StatusOK {
		t.Fatalf("platform: %d", code)
	}
	if code, out := call(t, e, http.MethodGet, "/v1/agents/builder/objects", "bob-token", nil); code != http.StatusOK || len(out["objects"].([]any)) != 0 {
		t.Fatalf("bob's list: %d %v", code, out)
	}

	events := stream(t, e, fmt.Sprintf("/v1/objects/%s/events?after=%v", id, obj["last_seq"]), "alice-token", nil)

	code, opened := call(t, e, http.MethodPost, "/v1/objects/"+id+"/conversations", "alice-token", map[string]any{"title": "start"})
	if code != http.StatusCreated {
		t.Fatalf("open: %d %v", code, opened)
	}
	conv := opened["conversation"].(map[string]any)["id"].(string)
	if text := opened["message"].(map[string]any)["text"].(string); !strings.Contains(text, "What job does the agent do?") {
		t.Fatalf("opener %q", text)
	}

	code, said := call(t, e, http.MethodPost, "/v1/objects/"+id+"/conversations/"+conv+"/messages", "alice-token",
		map[string]any{"message_id": "m1", "text": "it triages alerts"})
	if code != http.StatusAccepted || said["duplicate"] != false {
		t.Fatalf("say: %d %v", code, said)
	}

	seen := until(t, events, "run", func(ev sse) bool { return strings.Contains(ev.data, `"done"`) })
	var types []string
	for _, ev := range seen {
		types = append(types, ev.event)
	}
	want := "conversation message message run run state message run"
	if strings.Join(types, " ") != want {
		t.Fatalf("events %v", types)
	}
	var state struct {
		Changed []string       `json:"changed"`
		Values  map[string]any `json:"values"`
		Missing []string       `json:"missing"`
	}
	for _, ev := range seen {
		if ev.event == "state" {
			_ = json.Unmarshal([]byte(ev.data), &state)
		}
	}
	if state.Values["work"] != "triage alerts" || len(state.Missing) != 1 || state.Missing[0] != "trigger" {
		t.Fatalf("state event %+v", state)
	}

	code, msgs := call(t, e, http.MethodGet, "/v1/objects/"+id+"/conversations/"+conv+"/messages", "alice-token", nil)
	if list := msgs["messages"].([]any); code != http.StatusOK || len(list) != 3 || list[2].(map[string]any)["text"] != "Noted. What starts it?" {
		t.Fatalf("messages: %d %v", code, msgs)
	}

	// an edit based on an old rev of the same field conflicts
	code, out := call(t, e, http.MethodPatch, "/v1/objects/"+id+"/state", "alice-token",
		map[string]any{"fields": map[string]any{"work": map[string]any{"value": "mine", "rev": 0}}})
	if code != http.StatusConflict || out["code"] != "field_conflict" || out["current"] != "triage alerts" {
		t.Fatalf("conflict: %d %v", code, out)
	}
	code, out = call(t, e, http.MethodPatch, "/v1/objects/"+id+"/state", "alice-token",
		map[string]any{"fields": map[string]any{"trigger": map[string]any{"value": 1}}})
	if code != http.StatusUnprocessableEntity || out["code"] != "invalid" {
		t.Fatalf("invalid: %d %v", code, out)
	}

	// only an owner changes roles
	members := []map[string]any{{"person": "alice", "role": "owner"}, {"person": "bob", "role": "member"}}
	if code, _ := call(t, e, http.MethodPut, "/v1/objects/"+id+"/members", "alice-token", members); code != http.StatusOK {
		t.Fatalf("members: %d", code)
	}
	if code, _ := call(t, e, http.MethodPut, "/v1/objects/"+id+"/members", "bob-token", members); code != http.StatusForbidden {
		t.Fatalf("bob changes roles: %d", code)
	}
	if code, _ := call(t, e, http.MethodDelete, "/v1/objects/"+id, "alice-token", nil); code != http.StatusOK {
		t.Fatalf("archive: %d", code)
	}
	if code, out := call(t, e, http.MethodPost, "/v1/objects/"+id+"/conversations", "alice-token", nil); code != http.StatusConflict || out["code"] != "archived" {
		t.Fatalf("conversation on archived: %d %v", code, out)
	}
}

// A reconnecting client continues after Last-Event-ID; one that needs events no longer kept gets reset.
func TestObjectEventsResume(t *testing.T) {
	e, _ := setupObjects(t)
	_, obj := call(t, e, http.MethodPost, "/v1/agents/builder/objects", "alice-token", map[string]any{"connection": "platform"})
	id := obj["id"].(string)
	call(t, e, http.MethodPatch, "/v1/objects/"+id, "alice-token", map[string]any{"title": "renamed"})

	evs := stream(t, e, "/v1/objects/"+id+"/events?after=0", "alice-token", map[string]string{"Last-Event-ID": "1"})
	got := until(t, evs, "object", nil)
	if got[0].id != "2" || !strings.Contains(got[0].data, "renamed") {
		t.Fatalf("resume: %+v", got)
	}

	if _, err := e.pool.Exec(context.Background(), `DELETE FROM object_events WHERE object_id = $1 AND seq = 1`, id); err != nil {
		t.Fatal(err)
	}
	evs = stream(t, e, "/v1/objects/"+id+"/events?after=0", "alice-token", nil)
	if got := until(t, evs, "reset", nil); len(got) != 1 {
		t.Fatalf("reset: %+v", got)
	}
}
