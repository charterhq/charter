package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/telemetry"
)

type accepted struct {
	Scenario  string `json:"scenario"`
	RunID     string `json:"run_id"`
	Duplicate bool   `json:"duplicate"`
}

// hook takes an event of a team: the connection says which scenarios the source launches and in
// which format the source sends (receiver). No flow per agent is needed in front of the core.
//
//	POST /v1/hooks/{agent}/{connection}/{source}
func (s *Server) hook(w http.ResponseWriter, r *http.Request) {
	agent, connection, source := chi.URLParam(r, "agent"), chi.URLParam(r, "connection"), chi.URLParam(r, "source")
	scenarios, ct, err := s.Engine.Route(r.Context(), agent, connection, "event", source)
	if err != nil {
		writeErr(w, acceptStatus(err), err.Error())
		return
	}
	if len(scenarios) == 0 {
		writeErr(w, http.StatusNotFound, "connection "+connection+" binds no event source "+source)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	receiver := ct.Receiver
	if receiver == "" {
		receiver = "raw"
	}
	envs, err := normalizers[receiver](r, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out := []accepted{}
	for _, scenario := range scenarios {
		for _, env := range envs {
			env.Trigger = run.Trigger{Type: "event", Source: source}
			env.Connection = connection
			env.Trace = telemetry.New(r.Header.Get("traceparent"))
			if env.Initiator.Kind == "" {
				env.Initiator = run.Initiator{Kind: "system", ID: caller(r.Context())}
			}
			if len(scenarios) > 1 {
				env.DeliveryID = scenario + "/" + env.DeliveryID // one delivery, a run per scenario
			}
			id, dup, err := s.Engine.Accept(r.Context(), agent, scenario, env)
			if err != nil {
				writeErr(w, acceptStatus(err), err.Error())
				return
			}
			out = append(out, accepted{Scenario: scenario, RunID: id, Duplicate: dup})
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"runs": out})
}

// call launches a scenario with a call trigger. The body is the run input. With ?wait=<duration>
// (at most 5m) the answer carries the finished run's status and output.
//
//	POST /v1/agents/{name}/scenarios/{scenario}/call?connection=&wait=
//	headers as for raw launches: X-Charter-Delivery-Id, X-Charter-Object-Key, X-Charter-Reply-Channel, X-Charter-Reply-Thread
func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	agent, scenario := chi.URLParam(r, "name"), chi.URLParam(r, "scenario")
	q := r.URL.Query()
	connection := q.Get("connection")
	if connection == "" {
		writeErr(w, http.StatusBadRequest, "connection is required")
		return
	}
	var wait time.Duration
	if v := q.Get("wait"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 || d > 5*time.Minute {
			writeErr(w, http.StatusBadRequest, "wait is a duration up to 5m")
			return
		}
		wait = d
	}
	source, err := s.Engine.CallSource(r.Context(), agent, scenario)
	if err != nil {
		writeErr(w, acceptStatus(err), err.Error())
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	envs, err := raw(r, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	env := envs[0]
	if r.Header.Get("X-Charter-Delivery-Id") == "" {
		env.DeliveryID = uuid.NewString() // calls repeat on purpose; a caller that retries sends its own ID
	}
	env.Trigger = run.Trigger{Type: "call", Source: source}
	env.Connection = connection
	env.Trace = telemetry.New(r.Header.Get("traceparent"))
	env.Initiator = run.Initiator{Kind: "system", ID: caller(r.Context())}
	id, dup, err := s.Engine.Accept(r.Context(), agent, scenario, env)
	if err != nil {
		writeErr(w, acceptStatus(err), err.Error())
		return
	}
	res := map[string]any{"run_id": id, "duplicate": dup}
	if wait == 0 {
		writeJSON(w, http.StatusAccepted, res)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), wait)
	defer cancel()
	status, output, err := s.waitRun(ctx, id)
	if errors.Is(err, context.DeadlineExceeded) {
		writeJSON(w, http.StatusAccepted, res) // still running: poll GET /v1/runs/{id}
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	res["status"], res["output"] = status, output
	writeJSON(w, http.StatusOK, res)
}

// waitRun polls a run until it is done.
func (s *Server) waitRun(ctx context.Context, id string) (string, json.RawMessage, error) {
	for delay := 100 * time.Millisecond; ; delay = min(delay*2, 2*time.Second) {
		var state, status string
		var output []byte
		if err := s.Pool.QueryRow(ctx, `SELECT state, status, output FROM runs WHERE id = $1`, id).
			Scan(&state, &status, &output); err != nil {
			return "", nil, err
		}
		if state == string(run.Done) {
			return status, output, nil
		}
		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}
