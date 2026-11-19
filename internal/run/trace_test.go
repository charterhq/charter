package run_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/telemetry"
)

func TestRunTrace(t *testing.T) {
	pool := newDB(t)
	applyAgent(t, pool, advise)
	mem := tracetest.NewInMemoryExporter()
	drv, _, _, _ := stub.Set(&stub.Model{Outputs: []string{`{"verdict":"noise"}`}})
	cfg := testConfig()
	cfg.Telemetry = telemetry.NewWithExporter(mem)
	eng, err := run.New(pool, drv, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = eng.StopAndCancel(ctx)
	})
	env := envelope("d1", "fp1")
	env.Trace = telemetry.New("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	id, _, _ := eng.Accept(context.Background(), "alert-triage", "triage", env)
	waitDone(t, pool, id)
	waitFor(t, "spans", func() bool { return len(mem.GetSpans()) > 0 })
	spans := mem.GetSpans()
	root := spans[len(spans)-1]
	if root.Name != "invoke_workflow alert-triage" || root.SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" ||
		root.SpanContext.SpanID().String() != env.Trace.RootSpanID {
		t.Fatalf("root %s %s %s", root.Name, root.SpanContext.TraceID(), root.SpanContext.SpanID())
	}
	if len(spans) != 8 { // root + 7 recorded steps before recording
		names := []string{}
		for _, s := range spans {
			names = append(names, s.Name)
		}
		t.Fatalf("spans %d: %v", len(spans), names)
	}
}
