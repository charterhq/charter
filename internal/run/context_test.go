package run_test

import (
	"context"
	"strings"
	"testing"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/run"
)

func TestRunSeesConnectionParamsAndKnowledge(t *testing.T) {
	pool := newDB(t)
	scn := withTriggers(advise, alertsTrigger)
	scn.Instructions = "You triage."
	scn.Params = map[string]any{"project": "DEF", "queues": []any{"l1"}}
	scn.Knowledge = []run.Doc{{Path: "knowledge/runbook.md", Text: "restart the pod"}}
	scn.Template = "{{ .params.project }}: {{ .output.verdict }}"
	applyPackage(t, pool, "1.0.0", run.Package{
		Scenarios: map[string]run.Scenario{"triage": scn},
		Connections: map[string]run.Connection{
			"sre": {Name: "sre", Scenarios: []string{"triage"}, Status: run.ConnActive,
				Channels:       map[string]string{"reply": "chan-sre"},
				Triggers:       map[string]run.ConnectionTrigger{"alerts": {}},
				Knowledge:      []run.Doc{{Path: "knowledge/sre-people.yml", Text: "lead: jane.doe"}},
				ScenarioParams: map[string]map[string]any{"triage": {"project": "SRE", "queues": []any{"l1"}}}},
		},
	})
	model := &stub.Model{Outputs: []string{`{"verdict":"ok"}`}}
	eng, _, ch, _ := startEngine(t, pool, model)
	id, _, err := eng.Accept(context.Background(), "alert-triage", "triage",
		run.Envelope{Trigger: alertsTrigger, Connection: "sre", Payload: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
	got := model.Instructions[0]
	for _, want := range []string{"You triage.", "### knowledge/runbook.md", "restart the pod", "### knowledge/sre-people.yml", `"project": "SRE"`} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions lack %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "runbook.md") > strings.Index(got, "sre-people.yml") || strings.Index(got, "sre-people.yml") > strings.Index(got, "## Parameters") {
		t.Errorf("order: agent knowledge, team knowledge, parameters:\n%s", got)
	}
	if p, _ := ch.LastPost(); p.Text != "SRE: ok" {
		t.Fatalf("template params: %q", p.Text)
	}
}

func TestPromptWithoutContext(t *testing.T) {
	if got := run.Prompt("plain", nil, nil); got != "plain" {
		t.Fatalf("prompt without knowledge and params changed: %q", got)
	}
}
