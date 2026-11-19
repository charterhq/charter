package run_test

import (
	"context"
	"strings"
	"testing"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/run"
)

const tmpl = `verdict: {{ .output.verdict }} · {{ .run.agent }} v{{ .run.version }} · {{ .run.short_id }}
{{- define "failed" }}no answer: {{ .run.status }} – {{ .run.error }}{{ end }}`

func replyEnvelope(delivery string) run.Envelope {
	env := envelope(delivery, "fp-"+delivery)
	env.Reply = &driver.Target{Channel: "chan1", Thread: "post1"}
	return env
}

func TestResponseTemplate(t *testing.T) {
	pool := newDB(t)
	scn := advise
	scn.Template = tmpl
	applyAgent(t, pool, scn)
	eng, _, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"verdict":"noise"}`}})

	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", replyEnvelope("d1"))
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %q", status)
	}
	m, ok := ch.LastPost()
	want := "verdict: noise · alert-triage v1.0.0 · " + id[:8]
	if !ok || m.Text != want || m.Target.Thread != "post1" {
		t.Fatalf("post %+v, want text %q", m, want)
	}
}

func TestBrokenTemplateFallsBackToRaw(t *testing.T) {
	pool := newDB(t)
	scn := advise
	scn.Template = `{{ .output.verdict | nosuchfunc }}`
	applyAgent(t, pool, scn)
	eng, _, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"verdict":"noise"}`}})

	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", replyEnvelope("d1"))
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %q", status)
	}
	if m, ok := ch.LastPost(); !ok || m.Text != "" || len(m.Data) == 0 {
		t.Fatalf("want raw output post, got %+v", m)
	}
}

func TestFailureNotice(t *testing.T) {
	pool := newDB(t)
	scn := advise
	scn.Template = tmpl
	applyAgent(t, pool, scn)
	eng, _, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"other":1}`}})

	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", replyEnvelope("d1"))
	if status := waitDone(t, pool, id); status != run.StatusInvalid {
		t.Fatalf("status %q", status)
	}
	m, ok := ch.LastPost()
	if !ok || ch.PostCount() != 1 || !strings.HasPrefix(m.Text, "no answer: invalid – ") || strings.Contains(m.Text, "validation: ") {
		t.Fatalf("posts=%d last %+v", ch.PostCount(), m)
	}
}

func TestFailureNoticeWithoutTemplate(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, advise)
	eng, _, ch, _ := startEngine(t, pool, &stub.Model{Outputs: []string{`{"other":1}`}})

	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", replyEnvelope("d1"))
	waitDone(t, pool, id)
	if m, ok := ch.LastPost(); !ok || !strings.Contains(m.Text, "status `invalid`") {
		t.Fatalf("last %+v", m)
	}
}
