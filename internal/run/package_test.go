package run_test

import (
	"context"
	"os"
	"testing"

	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/pkg"
	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/run"
)

// A v1 package goes from a directory through the registry into a run.
func TestPackageToRun(t *testing.T) {
	pool := newDB(t)
	p, problems := pkg.Load(os.DirFS("../../examples/ticket-triage"))
	if problems != nil {
		t.Fatal(problems)
	}
	stored, err := pkg.Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	m := p.Agent.Metadata
	if _, err := registry.Apply(context.Background(), pool, registry.Agent{Name: m.Name, Team: m.Team, Owner: m.Owner},
		m.Version, "deadbeef", stored); err != nil {
		t.Fatal(err)
	}
	model := &stub.Model{Outputs: []string{
		`{"category":"billing","queue":"l9","reason":"charged twice"}`, // queue not in enum
		`{"category":"billing","queue":"billing","reason":"charged twice"}`,
	}}
	eng, _, ch, _ := startEngine(t, pool, model)
	env := envelope("t-1", "T-1")
	env.Trigger.Source = "tickets"
	env.Payload = []byte(`{"ticket":{"id":"T-1","subject":"Refund","body":"charged twice"}}`)
	id, _, err := eng.Accept(context.Background(), "ticket-triage", "classify", env)
	if err != nil {
		t.Fatal(err)
	}
	if status := waitDone(t, pool, id); status != run.StatusOK {
		t.Fatalf("status %q", status)
	}
	if model.CallCount() != 2 || ch.PostCount() != 1 {
		t.Fatalf("schema retry did not happen: model=%d posts=%d", model.CallCount(), ch.PostCount())
	}
}
