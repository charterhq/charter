package object

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const brief = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["work", "trigger", "limits"],
  "properties": {
    "work": {"type": "string", "title": "Work", "description": "What job does the agent do?", "minLength": 3},
    "trigger": {"type": "string", "title": "Trigger"},
    "limits": {"type": "object", "properties": {"self": {"type": "string"}, "never": {"type": "string"}}},
    "owner": {"type": "string", "x-charter-readonly": "model"},
    "cases": {"type": "array", "minItems": 2, "x-charter-collection": true,
      "items": {"type": "object", "required": ["id", "came"], "properties": {"id": {"type": "string"}, "came": {"type": "string"}, "answer": {"type": "string"}}}}
  }
}`

func fields(t *testing.T) []Field {
	t.Helper()
	fs, err := Fields(json.RawMessage(brief))
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestFieldsInDocumentOrder(t *testing.T) {
	var names []string
	for _, f := range fields(t) {
		names = append(names, f.Name)
	}
	if want := []string{"work", "trigger", "limits", "owner", "cases"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("order %v", names)
	}
	f := fields(t)
	if !f[0].Required || f[0].Title != "Work" || f[0].Question != "What job does the agent do?" || !f[3].ReadonlyModel ||
		!f[4].Collection || f[4].MinItems != 2 {
		t.Fatalf("fields %+v", f)
	}
}

func TestCheckSchema(t *testing.T) {
	if ps := CheckSchema(json.RawMessage(brief)); len(ps) != 0 {
		t.Fatalf("brief: %v", ps)
	}
	bad := `{"type":"object","required":["x"],"properties":{"c":{"type":"array","x-charter-collection":true,"items":{"type":"object","properties":{"n":{"type":"string"}}}}}}`
	ps := CheckSchema(json.RawMessage(bad))
	if len(ps) != 2 || !strings.Contains(strings.Join(ps, ";"), "need an id") {
		t.Fatalf("bad: %v", ps)
	}
}

// A partial state passes the form check, a state of the wrong shape does not.
func TestFormLeavesCompletenessOut(t *testing.T) {
	if ps := CheckForm(json.RawMessage(brief), json.RawMessage(`{"work":"x"}`)); len(ps) != 0 {
		t.Fatalf("partial: %v", ps)
	}
	if ps := CheckForm(json.RawMessage(brief), json.RawMessage(`{"cases":[{"id":"a"}]}`)); len(ps) != 0 {
		t.Fatalf("item without came: %v", ps)
	}
	if ps := CheckForm(json.RawMessage(brief), json.RawMessage(`{"work":1}`)); len(ps) == 0 {
		t.Fatal("wrong type passed")
	}
	if ps := CheckForm(json.RawMessage(brief), json.RawMessage(`{"other":"x"}`)); len(ps) == 0 {
		t.Fatal("unknown field passed")
	}
}

func TestMissing(t *testing.T) {
	st, _ := Decode([]byte(`{"work":"  ","limits":{"self":"","never":""},"cases":[{"id":"a","came":"x"},{"id":"b","came":""}]}`))
	if got := Missing(fields(t), st); !reflect.DeepEqual(got, []string{"work", "trigger", "limits", "cases"}) {
		t.Fatalf("missing %v", got)
	}
	st, _ = Decode([]byte(`{"work":"triage","trigger":"alert","limits":{"never":"restart"},"cases":[{"id":"a","came":"x"},{"id":"b","came":"y"}]}`))
	if got := Missing(fields(t), st); len(got) != 0 {
		t.Fatalf("complete state misses %v", got)
	}
}

func ops(t *testing.T, s string) []Op {
	t.Helper()
	var o []Op
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestApply(t *testing.T) {
	st, _ := Decode([]byte(`{"work":"old","trigger":"t","cases":[{"id":"a","came":"x"}]}`))
	res, ps := Apply(fields(t), st, ops(t, `[
		{"op":"set","field":"work","value":"triage alerts"},
		{"op":"clear","field":"trigger"},
		{"op":"put","field":"cases","id":"b","value":{"came":"y"}},
		{"op":"put","field":"cases","id":"a","value":{"came":"x2"}}
	]`), nil)
	if len(ps) != 0 {
		t.Fatal(ps)
	}
	got, _ := json.Marshal(res.State)
	if string(got) != `{"cases":[{"came":"x2","id":"a"},{"came":"y","id":"b"}],"work":"triage alerts"}` {
		t.Fatalf("state %s", got)
	}
	if !reflect.DeepEqual(res.Changed, []string{"work", "trigger", "cases/b", "cases/a"}) {
		t.Fatalf("changed %v", res.Changed)
	}
	if st["work"] != "old" {
		t.Fatal("input state changed")
	}

	res, _ = Apply(fields(t), res.State, ops(t, `[{"op":"remove","field":"cases","id":"a"}]`), nil)
	if items := res.State["cases"].([]any); len(items) != 1 {
		t.Fatalf("after remove %v", items)
	}
}

func TestApplyProblems(t *testing.T) {
	_, ps := Apply(fields(t), map[string]any{}, ops(t, `[
		{"op":"set","field":"nope","value":"x"},
		{"op":"set","field":"owner","value":"mallory"},
		{"op":"set","field":"work","value":null},
		{"op":"set","field":"cases","value":[]},
		{"op":"put","field":"work","id":"a","value":{}},
		{"op":"put","field":"cases","value":{}},
		{"op":"merge","field":"work"}
	]`), nil)
	if len(ps) != 7 {
		t.Fatalf("problems %v", ps)
	}
}

// A path people changed after the model read the state is not overwritten.
func TestApplyPeopleWin(t *testing.T) {
	held := func(p string) bool { return p == "work" || p == "cases" }
	res, ps := Apply(fields(t), map[string]any{"work": "by person"}, ops(t, `[
		{"op":"set","field":"work","value":"by model"},
		{"op":"put","field":"cases","id":"a","value":{"came":"x"}},
		{"op":"set","field":"trigger","value":"t"}
	]`), held)
	if len(ps) != 0 || res.State["work"] != "by person" || res.State["cases"] != nil ||
		!reflect.DeepEqual(res.Skipped, []string{"work", "cases/a"}) || !reflect.DeepEqual(res.Changed, []string{"trigger"}) {
		t.Fatalf("res %+v %v", res, ps)
	}
}

func TestCompatible(t *testing.T) {
	added := strings.Replace(brief, `"owner":`, `"extra": {"type":"string"}, "owner":`, 1)
	if ps := Compatible(json.RawMessage(brief), json.RawMessage(added)); len(ps) != 0 {
		t.Fatalf("adding a field: %v", ps)
	}
	retitled := strings.Replace(brief, `"title": "Work"`, `"title": "Job"`, 1)
	if ps := Compatible(json.RawMessage(brief), json.RawMessage(retitled)); len(ps) != 0 {
		t.Fatalf("retitle: %v", ps)
	}
	broken := strings.Replace(strings.Replace(brief, `"trigger": {"type": "string", "title": "Trigger"},`, "", 1),
		`"work": {"type": "string"`, `"work": {"type": "array"`, 1)
	if ps := Compatible(json.RawMessage(brief), json.RawMessage(broken)); len(ps) != 2 {
		t.Fatalf("broken: %v", ps)
	}
}

func TestOpener(t *testing.T) {
	st, _ := Decode([]byte(`{"work":"triage"}`))
	d := NewOpenerData("alert-triage", fields(t), st)
	if d.Filled != 1 || d.Total != 4 || len(d.Missing) != 3 {
		t.Fatalf("data %+v", d)
	}
	text, err := Opener("{{.Title}}: {{.Filled}}/{{.Total}}.{{range .Missing}} {{lower .Title}}{{end}}", d)
	// limits and cases have no title; the rendered text is trimmed
	if err != nil || text != "alert-triage: 1/4. trigger" {
		t.Fatalf("opener %q %v", text, err)
	}
	plain, err := Opener("", d)
	if err != nil || !strings.Contains(plain, "1 of 4 filled, missing: Trigger, limits, cases") {
		t.Fatalf("plain %q %v", plain, err)
	}
}
