// Package e2e runs real agent packages through the core with real drivers.
package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charterhq/charter/drivers/bifrost"
	"github.com/charterhq/charter/drivers/clickhouse"
	"github.com/charterhq/charter/drivers/stub"
	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/pkg"
	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/testdb"
)

// TestLivePackage runs one scenario of a real package on a real input through the real model gateway
// and the journal store. The channel is a stub: chat is reachable only from inside the network.
//
//	CHARTER_TEST_DATABASE_URL, CHARTER_TEST_CLICKHOUSE_URL, CHARTER_BIFROST_URL, CHARTER_BIFROST_KEY,
//	CHARTER_LIVE_PACKAGE (dir), CHARTER_LIVE_SCENARIO, CHARTER_LIVE_INPUT (json file), CHARTER_LIVE_CLASSES (json map)
func TestLivePackage(t *testing.T) {
	need := []string{"CHARTER_TEST_DATABASE_URL", "CHARTER_TEST_CLICKHOUSE_URL", "CHARTER_BIFROST_URL", "CHARTER_BIFROST_KEY",
		"CHARTER_LIVE_PACKAGE", "CHARTER_LIVE_SCENARIO", "CHARTER_LIVE_INPUT", "CHARTER_LIVE_CLASSES"}
	for _, k := range need {
		if os.Getenv(k) == "" {
			t.Skip(k + " is not set")
		}
	}
	ctx := context.Background()
	pool := testdb.New(t)

	p, problems := pkg.Load(os.DirFS(os.Getenv("CHARTER_LIVE_PACKAGE")))
	if problems != nil {
		t.Fatal(problems)
	}
	stored, err := pkg.Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	m := p.Agent.Metadata
	if _, err := registry.Apply(ctx, pool, registry.Agent{Name: m.Name, Team: m.Team, Owner: m.Owner}, m.Version, "live", stored); err != nil {
		t.Fatal(err)
	}

	var classes map[string]string
	if err := json.Unmarshal([]byte(os.Getenv("CHARTER_LIVE_CLASSES")), &classes); err != nil {
		t.Fatal(err)
	}
	params := map[string]map[string]any{}
	for class := range classes {
		params[class] = map[string]any{"max_tokens": 16384, "reasoning_effort": "low", "temperature": 0.2}
	}
	journal := clickhouse.New(clickhouse.Config{URL: os.Getenv("CHARTER_TEST_CLICKHOUSE_URL"), User: "charter", Password: "charter", Database: "default"})
	if err := journal.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	ch := &stub.Channel{}
	drv := driver.Set{
		Model:   bifrost.New(bifrost.Config{URL: os.Getenv("CHARTER_BIFROST_URL"), Key: os.Getenv("CHARTER_BIFROST_KEY"), Classes: classes, Params: params}),
		Tools:   &stub.Tools{},
		Channel: ch,
		Journal: journal,
	}
	eng, err := run.New(pool, drv, run.Config{FetchPollInterval: 100 * time.Millisecond, FetchCooldown: 20 * time.Millisecond, ShipInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng.Stop(context.Background()) }()

	input, err := os.ReadFile(os.Getenv("CHARTER_LIVE_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := eng.Accept(ctx, m.Name, os.Getenv("CHARTER_LIVE_SCENARIO"), run.Envelope{
		DeliveryID: "live-" + filepath.Base(os.Getenv("CHARTER_LIVE_INPUT")),
		Trigger:    run.Trigger{Type: "event", Source: "live-test"},
		Connection: "live",
		Initiator:  run.Initiator{Kind: "system", ID: "e2e"},
		Payload:    input,
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(9 * time.Minute)
	var state, status, errText string
	var output, usage []byte
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(ctx, `SELECT state, status, error, coalesce(output, 'null'), usage FROM runs WHERE id = $1`, id).
			Scan(&state, &status, &errText, &output, &usage); err != nil {
			t.Fatal(err)
		}
		if state == string(run.Done) {
			break
		}
		time.Sleep(time.Second)
	}
	t.Logf("state=%s status=%s error=%q usage=%s", state, status, errText, usage)
	t.Logf("output=%.600s", output)
	if status != run.StatusOK {
		t.Fatalf("run did not finish ok")
	}
	if ch.PostCount() != 1 {
		t.Fatalf("posts %d", ch.PostCount())
	}
	time.Sleep(2 * time.Second) // journal shipping interval
}
