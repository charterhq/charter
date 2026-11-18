// Package clickhouse writes run records to ClickHouse over HTTP.
package clickhouse

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

//go:embed schema.sql
var Schema string

// Config of the driver. Database holds the charter_runs table.
type Config struct {
	URL      string
	User     string
	Password string
	Database string
	HTTP     *http.Client
}

// Journal is a driver.Journal backed by ClickHouse.
type Journal struct {
	cfg Config
}

// New returns a journal driver.
func New(cfg Config) *Journal {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return &Journal{cfg: cfg}
}

// EnsureSchema creates the table if it does not exist.
func (j *Journal) EnsureSchema(ctx context.Context) error {
	return j.exec(ctx, Schema, nil)
}

type row struct {
	RunID        string  `json:"run_id"`
	Agent        string  `json:"agent"`
	Version      string  `json:"version"`
	Scenario     string  `json:"scenario"`
	Connection   string  `json:"connection"`
	Status       string  `json:"status"`
	ReceivedAt   string  `json:"received_at"`
	FinishedAt   string  `json:"finished_at"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
	RecordText   string  `json:"record"`
}

// Write inserts records in one request.
func (j *Journal) Write(ctx context.Context, records []json.RawMessage) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, rec := range records {
		var f struct {
			RunID        string    `json:"agentic.run.id"`
			Agent        string    `json:"gen_ai.agent.name"`
			Version      string    `json:"gen_ai.agent.version"`
			Scenario     string    `json:"agentic.scenario"`
			Connection   string    `json:"agentic.connection"`
			Status       string    `json:"agentic.run.status"`
			ReceivedAt   time.Time `json:"agentic.run.received_at"`
			FinishedAt   time.Time `json:"agentic.run.finished_at"`
			InputTokens  int       `json:"gen_ai.usage.input_tokens"`
			OutputTokens int       `json:"gen_ai.usage.output_tokens"`
			Cost         float64   `json:"agentic.usage.cost"`
		}
		if err := json.Unmarshal(rec, &f); err != nil {
			return fmt.Errorf("clickhouse: decode record: %w", err)
		}
		const ts = "2006-01-02 15:04:05.000"
		if err := enc.Encode(row{
			RunID: f.RunID, Agent: f.Agent, Version: f.Version, Scenario: f.Scenario, Connection: f.Connection,
			Status: f.Status, ReceivedAt: f.ReceivedAt.UTC().Format(ts), FinishedAt: f.FinishedAt.UTC().Format(ts),
			InputTokens: f.InputTokens, OutputTokens: f.OutputTokens, Cost: f.Cost, RecordText: string(rec),
		}); err != nil {
			return err
		}
	}
	return j.exec(ctx, "INSERT INTO charter_runs FORMAT JSONEachRow", &buf)
}

func (j *Journal) exec(ctx context.Context, query string, body io.Reader) error {
	q := url.Values{"database": {j.cfg.Database}}
	if body == nil {
		body = strings.NewReader(query)
	} else {
		q.Set("query", query)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.cfg.URL+"/?"+q.Encode(), body)
	if err != nil {
		return err
	}
	if j.cfg.User != "" {
		req.SetBasicAuth(j.cfg.User, j.cfg.Password)
	}
	resp, err := j.cfg.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("clickhouse: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("clickhouse: HTTP %d: %.300s", resp.StatusCode, raw)
	}
	return nil
}
