package run_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/run"
)

var talk = run.Scenario{Name: "talk", Level: 2, ModelClass: "c", Autonomy: "advise", Memory: "thread",
	OutputSchema: json.RawMessage(`{"type":"object","required":["reply"]}`),
	Triggers:     []run.Trigger{{Type: "message", Source: "chat"}}}

func message(conv, id, author, text string) run.Envelope {
	return run.Envelope{
		DeliveryID: id, Trigger: run.Trigger{Type: "message", Source: "chat"}, Connection: "sre",
		Object: run.Object{Key: conv}, Initiator: run.Initiator{Kind: "human", ID: author},
		Conversation: &run.Conversation{ID: conv, MessageID: id, Author: author, Text: text},
		Payload:      json.RawMessage(`{}`),
	}
}

// Messages of one conversation are answered in order, each sees the earlier turns.
func TestConversationHistoryInOrder(t *testing.T) {
	pool := newDB(t)
	applyPackage(t, pool, "1.0.0", run.Package{
		Scenarios:   map[string]run.Scenario{"talk": talk},
		Connections: map[string]run.Connection{"sre": {Name: "sre", Scenarios: []string{"talk"}, Status: run.ConnActive}},
	})
	model := &stub.Model{Outputs: []string{`{"reply":"first answer"}`, `{"reply":"second answer"}`, `{"reply":"other"}`}}
	eng, _, ch, _ := startEngine(t, pool, model)
	ctx := context.Background()

	var ids []string
	for i, text := range []string{"hello", "and then?"} {
		id, _, err := eng.Accept(ctx, "alert-triage", "talk", message("thread-1", fmt.Sprintf("m%d", i), "alice", text))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if status := waitDone(t, pool, id); status != run.StatusOK {
			t.Fatalf("status %s", status)
		}
	}
	var second run.MessageInput
	if err := json.Unmarshal(model.InputAt(1), &second); err != nil {
		t.Fatal(err)
	}
	h := second.History
	if second.Message.Text != "and then?" || len(h) != 2 || h[0].Role != "user" || h[0].Text != "hello" ||
		h[1].Role != "agent" || h[1].Text != "first answer" {
		t.Fatalf("second turn input: %+v", second)
	}

	// another conversation starts empty
	id, _, err := eng.Accept(ctx, "alert-triage", "talk", message("thread-2", "m9", "bob", "hi"))
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, pool, id)
	var other run.MessageInput
	_ = json.Unmarshal(model.InputAt(2), &other)
	if other.Conversation != "thread-2" || len(other.History) != 0 {
		t.Fatalf("other conversation: %+v", other)
	}
	if ch.PostCount() != 3 {
		t.Fatalf("posts %d", ch.PostCount())
	}
}

// Without memory: thread the model gets only the message.
func TestConversationWithoutMemory(t *testing.T) {
	pool := newDB(t)
	scn := talk
	scn.Memory = "none"
	applyPackage(t, pool, "1.0.0", run.Package{
		Scenarios:   map[string]run.Scenario{"talk": scn},
		Connections: map[string]run.Connection{"sre": {Name: "sre", Scenarios: []string{"talk"}, Status: run.ConnActive}},
	})
	model := &stub.Model{Outputs: []string{`{"reply":"ok"}`}}
	eng, _, _, _ := startEngine(t, pool, model)
	for i := 0; i < 2; i++ {
		id, _, err := eng.Accept(context.Background(), "alert-triage", "talk", message("t", fmt.Sprintf("m%d", i), "alice", "x"))
		if err != nil {
			t.Fatal(err)
		}
		waitDone(t, pool, id)
	}
	var in run.MessageInput
	_ = json.Unmarshal(model.InputAt(1), &in)
	if len(in.History) != 0 {
		t.Fatalf("history without memory: %+v", in.History)
	}
}

func TestListeners(t *testing.T) {
	pool := newDB(t)
	p := run.Package{
		Scenarios: map[string]run.Scenario{"talk": talk, "triage": withTriggers(advise, alertsTrigger)},
		Connections: map[string]run.Connection{
			"sre": {Name: "sre", Scenarios: []string{"talk", "triage"}, Status: run.ConnActive,
				Triggers: map[string]run.ConnectionTrigger{"chat": {Channels: []string{"c1"}}, "alerts": {}}},
			"off": {Name: "off", Scenarios: []string{"talk"}, Status: run.ConnPaused,
				Triggers: map[string]run.ConnectionTrigger{"chat": {}}},
		},
	}
	p.Agent.Spec.Identities = []run.Identity{{System: "mattermost", Account: "sre-manager"}}
	applyPackage(t, pool, "1.0.0", p)
	eng, _, _, _ := startEngine(t, pool, &stub.Model{})
	ls, err := eng.Listeners(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 1 || ls[0].Account != "sre-manager" || ls[0].Connection != "sre" || ls[0].Source != "chat" ||
		len(ls[0].Places) != 1 || ls[0].Places[0] != "c1" {
		t.Fatalf("listeners: %+v", ls)
	}
}
