package run_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/run"
)

func TestEvaluateWithholdsAction(t *testing.T) {
	model := &stub.Model{Outputs: []string{`{"answer": "a", "draft": {"summary": "s"}}`}}
	drv, _, _, _ := stub.Set(model)
	eng, err := run.New(newDB(t), drv, run.Config{})
	if err != nil {
		t.Fatal(err)
	}
	scn := run.Scenario{Name: "intake", Level: 3, ModelClass: "c", Autonomy: "act", Action: "jira.create_issue", ActionInput: "draft",
		Tools: []string{"jira.search", "jira.create_issue"}, OutputSchema: json.RawMessage(`{"type": "object"}`)}
	res, err := eng.Evaluate(context.Background(), "a", scn, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Action || string(res.ActionArgs) != `{"summary": "s"}` {
		t.Fatalf("action: %+v", res)
	}
	if got := model.Tools[0]; !slices.Equal(got, []string{"jira.search"}) {
		t.Fatalf("tools offered: %v", got)
	}
	if scn.Tools[1] != "jira.create_issue" {
		t.Fatal("scenario tools changed")
	}
}
