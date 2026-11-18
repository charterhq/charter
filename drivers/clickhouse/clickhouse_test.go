package clickhouse_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charterhq/charter/drivers/clickhouse"
)

// Needs a ClickHouse at CHARTER_TEST_CLICKHOUSE_URL (see deploy/compose/dev.yml).
func TestWrite(t *testing.T) {
	base := os.Getenv("CHARTER_TEST_CLICKHOUSE_URL")
	if base == "" {
		t.Skip("CHARTER_TEST_CLICKHOUSE_URL is not set")
	}
	j := clickhouse.New(clickhouse.Config{URL: base, User: "charter", Password: "charter", Database: "default"})
	ctx := context.Background()
	if err := j.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	id := "8f1c6f7e-1111-4a2b-9c3d-" + time.Now().Format("150405000000")[:12]
	rec, _ := json.Marshal(map[string]any{
		"agentic.run.id": id, "gen_ai.agent.name": "alert-triage", "gen_ai.agent.version": "1.0.0",
		"agentic.scenario": "triage", "agentic.connection": "sre", "agentic.run.status": "ok",
		"agentic.run.received_at": time.Now().UTC(), "agentic.run.finished_at": time.Now().UTC(),
		"gen_ai.usage.input_tokens": 100, "gen_ai.usage.output_tokens": 20, "agentic.usage.cost": 0.001,
	})
	if err := j.Write(ctx, []json.RawMessage{rec}); err != nil {
		t.Fatal(err)
	}
	q := url.Values{"query": {"SELECT status, input_tokens, JSONExtractString(record, 'gen_ai.agent.name') FROM charter_runs WHERE run_id = '" + id + "' FORMAT TSV"},
		"user": {"charter"}, "password": {"charter"}}
	resp, err := http.Get(base + "/?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(b)) != "ok\t100\talert-triage" {
		t.Fatalf("row: %q", b)
	}
}
