package run_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/run"
)

const intakeSchema = `{"type":"object","additionalProperties":false,"required":["work","trigger"],"properties":{
  "work":{"type":"string","title":"Work","description":"What job does the agent do?"},
  "trigger":{"type":"string","title":"Trigger","description":"What starts it?"},
  "owner":{"type":"string","x-charter-readonly":"model"},
  "cases":{"type":"array","minItems":2,"x-charter-collection":true,
    "items":{"type":"object","required":["id","came"],"properties":{"id":{"type":"string"},"came":{"type":"string"}}}}}}`

var intake = run.Scenario{Name: "intake", Level: 2, ModelClass: "c", Autonomy: "advise", Memory: "object",
	OutputSchema: json.RawMessage(`{"type":"object","required":["reply"]}`),
	Template:     `{{.output.reply}}`,
	Triggers:     []run.Trigger{{Type: "message", Source: "requests"}}}

func builderPackage() run.Package {
	return run.Package{
		Scenarios: map[string]run.Scenario{"intake": intake},
		Object: &run.ObjectSpec{Schema: intakeSchema, Opener: "{{.Filled}} of {{.Total}}: {{(index .Missing 0).Question}}",
			Stages: []run.Stage{{ID: "request", Title: "Request"}, {ID: "review", Title: "Review"}}},
		Connections: map[string]run.Connection{"platform": {Name: "platform", Scenarios: []string{"intake"}, Status: run.ConnActive,
			Triggers: map[string]run.ConnectionTrigger{"requests": {Receiver: run.ReceiverWeb}}}},
	}
}

func events(t *testing.T, eng *run.Engine, id string) []run.Event {
	t.Helper()
	evs, _, err := eng.Events(context.Background(), id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func eventTypes(evs []run.Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Type
		if e.Type == "run" {
			var d struct {
				State string `json:"state"`
			}
			_ = json.Unmarshal(e.Data, &d)
			out[i] += ":" + d.State
		}
	}
	return out
}

// A conversation about an object: the opener comes from the template, the model sees the whole
// state and what it lacks, its state_ops land in the state with their sources, and every step is
// an event of the object, numbered without gaps.
func TestObjectConversation(t *testing.T) {
	pool := newDB(t)
	applyPackage(t, pool, "1.0.0", builderPackage())
	model := &stub.Model{Outputs: []string{`{"reply":"Got it. What starts it?","state_ops":[
		{"op":"set","field":"work","value":"triage alerts"},
		{"op":"put","field":"cases","id":"c1","value":{"came":"disk full"}}]}`}}
	eng, _, ch, _ := startEngine(t, pool, model)
	ctx := context.Background()

	obj, err := eng.CreateObject(ctx, "alert-triage", "platform", "New agent", map[string]any{"team": "sre"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if obj.Stage != "request" || len(obj.Stages) != 2 || !reflect.DeepEqual(obj.Members, []run.Member{{Person: "alice", Role: "owner"}}) ||
		!reflect.DeepEqual(obj.Missing, []string{"work", "trigger", "cases"}) || obj.Fields[0].Question != "What job does the agent do?" {
		t.Fatalf("object %+v", obj)
	}
	conv, opener, err := eng.OpenConversation(ctx, obj.ID, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if opener.Text != "0 of 3: What job does the agent do?" || opener.Role != "agent" {
		t.Fatalf("opener %+v", opener)
	}

	runID, seq, dup, err := eng.Say(ctx, obj.ID, conv.ID, "m1", "alice", nil, "it should triage alerts, like the disk full one")
	if err != nil || dup || seq == 0 {
		t.Fatalf("say: %v dup=%v seq=%d", err, dup, seq)
	}
	if status := waitDone(t, pool, runID); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}

	var in run.MessageInput
	if err := json.Unmarshal(model.InputAt(0), &in); err != nil {
		t.Fatal(err)
	}
	if in.Object == nil || in.Object.ID != obj.ID || in.Object.Rev != 0 || len(in.Object.Missing) != 3 ||
		len(in.History) != 1 || in.History[0].Role != "agent" || in.Message.Text != "it should triage alerts, like the disk full one" {
		t.Fatalf("input %+v", in)
	}

	got, err := eng.GetObject(ctx, obj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rev != 1 || got.State["work"] != "triage alerts" || !reflect.DeepEqual(got.Missing, []string{"trigger", "cases"}) {
		t.Fatalf("after the run: %+v", got)
	}
	if s := got.Sources["cases/c1"]; s.Source != "conversation" || s.ConversationID != conv.ID || s.RunID != runID || s.Rev != 1 {
		t.Fatalf("source %+v", s)
	}

	msgs, err := eng.Messages(ctx, obj.ID, conv.ID, run.MessagePage{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 || msgs[1].MessageID != "m1" || msgs[2].Text != "Got it. What starts it?" ||
		!reflect.DeepEqual(msgs[2].Fields, []string{"work", "cases/c1"}) {
		t.Fatalf("messages %+v", msgs)
	}
	if ch.PostCount() != 0 {
		t.Fatal("an object conversation must not post to a chat")
	}

	evs := events(t, eng, obj.ID)
	want := []string{"object", "conversation", "message", "message", "run:accepted", "run:running", "state", "message", "run:done"}
	if types := eventTypes(evs); !reflect.DeepEqual(types, want) {
		t.Fatalf("events %v", types)
	}
	for i, e := range evs {
		if e.Seq != int64(i+1) {
			t.Fatalf("event %d has seq %d", i, e.Seq)
		}
	}
	if got.LastSeq != int64(len(evs)) {
		t.Fatalf("last_seq %d, events %d", got.LastSeq, len(evs))
	}

	// the same message again in the same conversation is the same run
	again, _, dup, err := eng.Say(ctx, obj.ID, conv.ID, "m1", "alice", nil, "it should triage alerts, like the disk full one")
	if err != nil || !dup || again != runID {
		t.Fatalf("repeat: %s dup=%v %v", again, dup, err)
	}
}

// state_ops the schema does not allow make the output invalid: the model gets one more try.
func TestObjectStateOpsInvalid(t *testing.T) {
	pool := newDB(t)
	applyPackage(t, pool, "1.0.0", builderPackage())
	model := &stub.Model{Outputs: []string{
		`{"reply":"x","state_ops":[{"op":"set","field":"owner","value":"mallory"}]}`,
		`{"reply":"ok","state_ops":[{"op":"set","field":"trigger","value":"alert"}]}`,
	}}
	eng, _, _, _ := startEngine(t, pool, model)
	ctx := context.Background()
	obj, _ := eng.CreateObject(ctx, "alert-triage", "platform", "a", nil, "alice")
	conv, _, _ := eng.OpenConversation(ctx, obj.ID, "", "alice")
	id, _, _, err := eng.Say(ctx, obj.ID, conv.ID, "m1", "alice", nil, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
	got, _ := eng.GetObject(ctx, obj.ID)
	if got.State["owner"] != nil || got.State["trigger"] != "alert" || model.Calls != 2 {
		t.Fatalf("state %+v calls %d", got.State, model.Calls)
	}
}

type funcModel func(ctx context.Context, req driver.StepRequest) (driver.StepResult, error)

func (f funcModel) Run(ctx context.Context, req driver.StepRequest) (driver.StepResult, error) {
	return f(ctx, req)
}

// A field people changed after the model read the state is not overwritten by the model.
func TestObjectPeopleWin(t *testing.T) {
	pool := newDB(t)
	applyPackage(t, pool, "1.0.0", builderPackage())
	drv, _, _, _ := stub.Set(&stub.Model{})
	var eng *run.Engine
	var objID string
	drv.Model = funcModel(func(ctx context.Context, req driver.StepRequest) (driver.StepResult, error) {
		// while the model thinks, a person edits the work field by hand
		if _, err := eng.EditState(ctx, objID, map[string]run.FieldEdit{"work": {Value: json.RawMessage(`"by person"`)}}, "alice"); err != nil {
			return driver.StepResult{}, err
		}
		return driver.StepResult{Output: json.RawMessage(`{"reply":"ok","state_ops":[
			{"op":"set","field":"work","value":"by model"},{"op":"set","field":"trigger","value":"alert"}]}`)}, nil
	})
	var err error
	eng, err = run.New(pool, drv, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.StopAndCancel(context.Background()) })

	obj, _ := eng.CreateObject(ctx, "alert-triage", "platform", "a", nil, "alice")
	objID = obj.ID
	conv, _, _ := eng.OpenConversation(ctx, obj.ID, "", "alice")
	id, _, _, _ := eng.Say(ctx, obj.ID, conv.ID, "m1", "alice", nil, "hi")
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
	got, _ := eng.GetObject(ctx, obj.ID)
	if got.State["work"] != "by person" || got.State["trigger"] != "alert" || got.Sources["work"].Source != "person" {
		t.Fatalf("state %+v sources %+v", got.State, got.Sources)
	}
	var skipped bool
	for _, e := range events(t, eng, obj.ID) {
		var d struct {
			Skipped []string `json:"skipped"`
		}
		_ = json.Unmarshal(e.Data, &d)
		skipped = skipped || e.Type == "state" && reflect.DeepEqual(d.Skipped, []string{"work"})
	}
	if !skipped {
		t.Fatal("no state event reports the skipped field")
	}
}

// An edit by hand conflicts only with a change of the same field after its rev.
func TestObjectEditConflict(t *testing.T) {
	pool := newDB(t)
	applyPackage(t, pool, "1.0.0", builderPackage())
	eng, _, _, _ := startEngine(t, pool, &stub.Model{})
	ctx := context.Background()
	obj, _ := eng.CreateObject(ctx, "alert-triage", "platform", "a", nil, "alice")

	v, err := eng.EditState(ctx, obj.ID, map[string]run.FieldEdit{"work": {Value: json.RawMessage(`"one"`)}}, "alice")
	if err != nil || v.Rev != 1 || v.Sources["work"].Rev != 1 {
		t.Fatalf("first edit: %v %+v", err, v.Sources)
	}
	// bob still has the field at rev 0
	_, err = eng.EditState(ctx, obj.ID, map[string]run.FieldEdit{"work": {Value: json.RawMessage(`"two"`), Rev: 0}}, "bob")
	var conflict *run.FieldConflictError
	if !errors.As(err, &conflict) || conflict.Field != "work" || conflict.Current != "one" {
		t.Fatalf("conflict: %v", err)
	}
	// another field at rev 0 is fine
	if _, err := eng.EditState(ctx, obj.ID, map[string]run.FieldEdit{"trigger": {Value: json.RawMessage(`"alert"`)}}, "bob"); err != nil {
		t.Fatal(err)
	}
	// a value of the wrong shape is refused
	_, err = eng.EditState(ctx, obj.ID, map[string]run.FieldEdit{"trigger": {Value: json.RawMessage(`5`), Rev: 3}}, "bob")
	var invalid *run.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("invalid: %v", err)
	}
	// clearing
	v, err = eng.EditState(ctx, obj.ID, map[string]run.FieldEdit{"work": {Value: json.RawMessage(`null`), Rev: 1}}, "alice")
	if err != nil || v.State["work"] != nil {
		t.Fatalf("clear: %v %+v", err, v.State)
	}
}

func TestObjectMembersAndList(t *testing.T) {
	pool := newDB(t)
	applyPackage(t, pool, "1.0.0", builderPackage())
	eng, _, _, _ := startEngine(t, pool, &stub.Model{})
	ctx := context.Background()
	a, _ := eng.CreateObject(ctx, "alert-triage", "platform", "a", nil, "alice")
	if _, err := eng.CreateObject(ctx, "alert-triage", "platform", "b", nil, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.SetMembers(ctx, a.ID, []run.Member{{Person: "bob", Role: "member"}}, "alice"); err == nil {
		t.Fatal("an object without an owner was accepted")
	}
	if _, err := eng.SetMembers(ctx, a.ID, []run.Member{{Person: "alice", Role: "owner"}, {Person: "bob", Role: "member"}}, "alice"); err != nil {
		t.Fatal(err)
	}
	mine, err := eng.ListObjects(ctx, run.ObjectFilter{Agent: "alert-triage", Person: "alice"})
	if err != nil || len(mine) != 1 || mine[0].ID != a.ID || mine[0].Missing != 3 {
		t.Fatalf("alice: %+v %v", mine, err)
	}
	bobs, _ := eng.ListObjects(ctx, run.ObjectFilter{Agent: "alert-triage", Person: "bob"})
	if len(bobs) != 2 {
		t.Fatalf("bob: %+v", bobs)
	}
	page, _ := eng.ListObjects(ctx, run.ObjectFilter{Agent: "alert-triage", Limit: 1})
	next, _ := eng.ListObjects(ctx, run.ObjectFilter{Agent: "alert-triage", Limit: 1,
		Before: &run.ObjectCursor{UpdatedAt: page[0].UpdatedAt, ID: page[0].ID}})
	if len(page) != 1 || len(next) != 1 || page[0].ID == next[0].ID {
		t.Fatalf("pages %+v %+v", page, next)
	}
	archived := true
	if _, err := eng.UpdateObject(ctx, a.ID, run.ObjectUpdate{Archived: &archived}, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := eng.OpenConversation(ctx, a.ID, "", "alice"); !errors.Is(err, run.ErrArchived) {
		t.Fatalf("conversation on an archived object: %v", err)
	}
	if ok, _ := eng.HasRole(ctx, a.ID, "bob"); !ok {
		t.Fatal("bob has no role")
	}
}

// Without a web message source the connection takes no messages about objects.
func TestObjectWithoutWebSource(t *testing.T) {
	pool := newDB(t)
	p := builderPackage()
	c := p.Connections["platform"]
	c.Triggers = map[string]run.ConnectionTrigger{"requests": {}}
	p.Connections["platform"] = c
	applyPackage(t, pool, "1.0.0", p)
	eng, _, _, _ := startEngine(t, pool, &stub.Model{})
	ctx := context.Background()
	obj, _ := eng.CreateObject(ctx, "alert-triage", "platform", "a", nil, "alice")
	conv, _, _ := eng.OpenConversation(ctx, obj.ID, "", "alice")
	if _, _, _, err := eng.Say(ctx, obj.ID, conv.ID, "m1", "alice", nil, "hi"); !errors.Is(err, run.ErrNoWebSource) {
		t.Fatalf("say: %v", err)
	}
}

// Channel drivers do not listen for a message source the core API serves.
func TestWebSourceHasNoListener(t *testing.T) {
	pool := newDB(t)
	p := builderPackage()
	p.Agent.Spec.Identities = []run.Identity{{System: "chat", Account: "builder-bot"}}
	applyPackage(t, pool, "1.0.0", p)
	eng, _, _, _ := startEngine(t, pool, &stub.Model{})
	ls, err := eng.Listeners(context.Background())
	if err != nil || len(ls) != 0 {
		t.Fatalf("listeners %+v %v", ls, err)
	}
}
