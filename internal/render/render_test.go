package render

import (
	"encoding/json"
	"testing"
)

func TestHelpers(t *testing.T) {
	out := json.RawMessage(`{"summary":"a  long   text — with dash","items":[
		{"id":"e1","status":"refuted"},{"id":"e2","status":"cause"},{"id":"e3","status":"refuted"},
		{"id":"e4","status":"refuted"},{"id":"e5","status":"symptom"}]}`)
	tpl := `{{ .output.summary | replace "—" "–" | clip 12 }}|` +
		`{{ range .output.items | sortBy "status" (list "cause" "symptom" "refuted") | limitPer "status" (dict "refuted" 2) | take 3 }}{{ .id }},{{ end }}|` +
		`{{ (get (byID .output.items) "e5").status }}|{{ .missing | default "none" }}|{{ round .run.seconds }}`
	got, err := Output(tpl, Run{Seconds: 41.6}, nil, out)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a long text…|e2,e5,e1,|symptom|none|42"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFailedBlock(t *testing.T) {
	msg, ok, err := Failed(`ok{{ define "failed" }}fail: {{ .run.error }}{{ end }}`, Run{Error: "boom"}, nil)
	if err != nil || !ok || msg != "fail: boom" {
		t.Fatalf("msg=%q ok=%v err=%v", msg, ok, err)
	}
	if _, ok, _ := Failed(`ok`, Run{}, nil); ok {
		t.Fatal("no failed block expected")
	}
}

func TestParseRejectsUnknownFunc(t *testing.T) {
	if _, err := Parse("x", `{{ nosuch }}`); err == nil {
		t.Fatal("want parse error")
	}
}
