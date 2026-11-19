package telemetry

import (
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewContinuesCallerTrace(t *testing.T) {
	tr := New("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if tr.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || tr.ParentSpanID != "00f067aa0ba902b7" || len(tr.RootSpanID) != 16 {
		t.Fatalf("%+v", tr)
	}
	if got := tr.Traceparent(); got != "00-4bf92f3577b34da6a3ce929d0e0e4736-"+tr.RootSpanID+"-01" {
		t.Fatalf("traceparent %q", got)
	}
	for _, bad := range []string{"", "garbage", "00-00000000000000000000000000000000-00f067aa0ba902b7-01"} {
		if n := New(bad); n.ParentSpanID != "" || len(n.TraceID) != 32 {
			t.Fatalf("%q -> %+v", bad, n)
		}
	}
}

func TestExportRootWithPresetIDs(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	e := NewWithExporter(mem)
	tr := New("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	t0 := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)
	e.Export(Run{Trace: tr, Name: "invoke_workflow alert-triage", Started: t0, Finished: t0.Add(200 * time.Second),
		Steps: []Step{
			{Name: "accepted", Outcome: "filtering", Attempt: 1, Started: t0, Finished: t0.Add(time.Millisecond)},
			{Name: "chat reasoning-light", Outcome: "validating", Attempt: 1, Started: t0.Add(time.Second), Finished: t0.Add(199 * time.Second)},
		}})
	spans := mem.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("spans %d", len(spans))
	}
	root := spans[len(spans)-1]
	if root.Name != "invoke_workflow alert-triage" || root.SpanContext.TraceID().String() != tr.TraceID ||
		root.SpanContext.SpanID().String() != tr.RootSpanID || root.Parent.SpanID().String() != tr.ParentSpanID {
		t.Fatalf("root %s trace=%s span=%s parent=%s", root.Name, root.SpanContext.TraceID(), root.SpanContext.SpanID(), root.Parent.SpanID())
	}
	for _, s := range spans[:2] {
		if s.Parent.SpanID() != root.SpanContext.SpanID() || s.SpanContext.SpanID() == root.SpanContext.SpanID() {
			t.Fatalf("step %s not a child of root", s.Name)
		}
	}
	if d := spans[1].EndTime.Sub(spans[1].StartTime); d != 198*time.Second {
		t.Fatalf("model step duration %v", d)
	}
}
