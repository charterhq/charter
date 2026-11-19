// Package telemetry exports run traces over OTLP.
//
// Steps of a run execute in different jobs, possibly on different core instances, so the
// trace is assembled when the run is recorded: a root span for the whole run and a child
// span per step, with the real step timestamps from the database. The trace id and the root
// span id are fixed when the run is accepted, so outgoing calls during the run (the model
// gateway) can already carry a traceparent that points at the root span.
//
// Without OTEL_EXPORTER_OTLP_ENDPOINT the exporter is off and spans go nowhere.
package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Trace is the trace identity of a run, kept in its envelope.
type Trace struct {
	TraceID      string `json:"trace_id"`
	RootSpanID   string `json:"root_span_id"`
	ParentSpanID string `json:"parent_span_id,omitempty"` // the caller's span, from an incoming traceparent
}

// New fixes the trace of a run: it continues the caller's trace when traceparent is valid.
func New(traceparent string) Trace {
	t := Trace{RootSpanID: randHex(8)}
	if tid, sid, ok := parseTraceparent(traceparent); ok {
		t.TraceID, t.ParentSpanID = tid, sid
	} else {
		t.TraceID = randHex(16)
	}
	return t
}

// Traceparent is the W3C header for calls made during the run, pointing at the run root span.
func (t Trace) Traceparent() string {
	if t.TraceID == "" || t.RootSpanID == "" {
		return ""
	}
	return "00-" + t.TraceID + "-" + t.RootSpanID + "-01"
}

func parseTraceparent(h string) (traceID, spanID string, ok bool) {
	p := strings.Split(strings.TrimSpace(h), "-")
	if len(p) != 4 || len(p[1]) != 32 || len(p[2]) != 16 {
		return "", "", false
	}
	if _, err := hex.DecodeString(p[1] + p[2]); err != nil || strings.Trim(p[1], "0") == "" || strings.Trim(p[2], "0") == "" {
		return "", "", false
	}
	return p[1], p[2], true
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Exporter builds traces of finished runs.
type Exporter struct {
	tp     trace.TracerProvider
	tracer trace.Tracer
	sdk    *sdktrace.TracerProvider
}

// Setup starts the OTLP exporter from the standard OTEL_* environment. Off without an endpoint.
func Setup(ctx context.Context, service, version string) (*Exporter, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		tp := noop.NewTracerProvider()
		return &Exporter{tp: tp, tracer: tp.Tracer("charter")}, nil
	}
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", service), attribute.String("service.version", version)))
	if err != nil {
		return nil, err
	}
	sdk := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res),
		sdktrace.WithIDGenerator(presetIDs{}), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	return &Exporter{tp: sdk, tracer: sdk.Tracer("charter"), sdk: sdk}, nil
}

// NewWithExporter is an exporter over a given span exporter, synchronous; for tests.
func NewWithExporter(exp sdktrace.SpanExporter) *Exporter {
	sdk := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithIDGenerator(presetIDs{}),
		sdktrace.WithSampler(sdktrace.AlwaysSample()))
	return &Exporter{tp: sdk, tracer: sdk.Tracer("charter"), sdk: sdk}
}

// Shutdown flushes pending spans.
func (e *Exporter) Shutdown(ctx context.Context) error {
	if e == nil || e.sdk == nil {
		return nil
	}
	return e.sdk.Shutdown(ctx)
}

// Step is one step of a run as recorded in the database.
type Step struct {
	Name     string
	Outcome  string
	Attempt  int
	Started  time.Time
	Finished time.Time
	Attrs    []attribute.KeyValue
}

// Run is a finished run to export.
type Run struct {
	Trace    Trace
	Name     string // span name of the root, e.g. "invoke_workflow alert-triage"
	Started  time.Time
	Finished time.Time
	Failed   bool
	Error    string
	Attrs    []attribute.KeyValue
	Steps    []Step
}

// Export emits the run trace. Safe on a nil exporter.
func (e *Exporter) Export(r Run) {
	if e == nil || e.sdk == nil {
		return
	}
	tid, err1 := trace.TraceIDFromHex(r.Trace.TraceID)
	rid, err2 := trace.SpanIDFromHex(r.Trace.RootSpanID)
	if err1 != nil || err2 != nil {
		return
	}
	ctx := context.Background()
	if pid, err := trace.SpanIDFromHex(r.Trace.ParentSpanID); err == nil {
		ctx = trace.ContextWithRemoteSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: tid, SpanID: pid, TraceFlags: trace.FlagsSampled, Remote: true}))
	}
	ctx = context.WithValue(ctx, presetKey{}, preset{trace: tid, span: rid})
	ctx, root := e.tracer.Start(ctx, r.Name, trace.WithTimestamp(r.Started), trace.WithAttributes(r.Attrs...),
		trace.WithSpanKind(trace.SpanKindInternal))
	ctx = context.WithValue(ctx, presetKey{}, nil)
	for _, s := range r.Steps {
		_, sp := e.tracer.Start(ctx, s.Name, trace.WithTimestamp(s.Started), trace.WithAttributes(
			append([]attribute.KeyValue{attribute.String("agentic.step.outcome", s.Outcome),
				attribute.Int("agentic.step.attempt", s.Attempt)}, s.Attrs...)...))
		sp.End(trace.WithTimestamp(s.Finished))
	}
	if r.Failed {
		root.SetStatus(codes.Error, r.Error)
	}
	root.End(trace.WithTimestamp(r.Finished))
}

type presetKey struct{}

type preset struct {
	trace trace.TraceID
	span  trace.SpanID
}

// presetIDs gives the root span the ids fixed at accept time; other spans get random ids.
type presetIDs struct{}

func (presetIDs) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	if p, ok := ctx.Value(presetKey{}).(preset); ok {
		return p.trace, p.span
	}
	var t trace.TraceID
	_, _ = rand.Read(t[:])
	return t, newSpanID()
}

func (presetIDs) NewSpanID(ctx context.Context, _ trace.TraceID) trace.SpanID {
	if p, ok := ctx.Value(presetKey{}).(preset); ok {
		return p.span
	}
	return newSpanID()
}

func newSpanID() trace.SpanID {
	var s trace.SpanID
	_, _ = rand.Read(s[:])
	return s
}
