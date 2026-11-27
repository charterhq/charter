// Package api is the HTTP API of the core.
package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/registry"
	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/signing"
	"github.com/charterhq/charter/internal/telemetry"
)

// Server serves /v1.
type Server struct {
	Pool   *pgxpool.Pool
	Engine *run.Engine
	// Tokens maps a bearer token to the caller name: drivers, CLI users. People log in via OIDC later.
	Tokens map[string]string
	// ApprovalSecret verifies chat button callbacks.
	ApprovalSecret []byte
	// Publishers are the callers allowed to publish packages (CI of the package repositories).
	Publishers map[string]bool
	// JudgeClass is the model class that judges evaluation criteria; empty means the scenario's class.
	JudgeClass string
	Log        *slog.Logger
}

// Handler returns the router.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Pool.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	// Chat button callbacks carry their own HMAC instead of a bearer token.
	r.Post("/v1/waits/{id}/resolve", s.resolve)
	r.Group(func(r chi.Router) {
		r.Use(s.auth)
		r.Post("/v1/launches/{source}", s.launch)
		r.Post("/v1/hooks/{agent}/{connection}/{source}", s.hook)
		r.Post("/v1/conversations/{agent}/{connection}/{source}", s.converse)
		r.Post("/v1/agents/{name}/scenarios/{scenario}/call", s.call)
		r.Get("/v1/runs", s.listRuns)
		r.Get("/v1/runs/{id}", s.getRun)
		r.Get("/v1/agents", s.listAgents)
		r.Post("/v1/packages", s.publish)
		r.Post("/v1/eval", s.evalCase)
		r.Post("/v1/agents/{name}/suspend", s.setSwitch(true))
		r.Post("/v1/agents/{name}/resume", s.setSwitch(false))
	})
	return r
}

type ctxKey struct{}

func caller(ctx context.Context) string { v, _ := ctx.Value(ctxKey{}).(string); return v }

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeErr(w, http.StatusUnauthorized, "bearer token required")
			return
		}
		for t, name := range s.Tokens {
			if subtle.ConstantTimeCompare([]byte(t), []byte(tok)) == 1 {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, name)))
				return
			}
		}
		if s.Log != nil {
			s.Log.Warn("unknown API token", "path", r.URL.Path, "remote", r.RemoteAddr, "forwarded_for", r.Header.Get("X-Forwarded-For"))
		}
		writeErr(w, http.StatusUnauthorized, "unknown token")
	})
}

// launch accepts an event from a source, splits it into envelopes and creates runs.
//
//	POST /v1/launches/{source}?agent=&scenario=&connection=
func (s *Server) launch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	agent, scenario, connection := q.Get("agent"), q.Get("scenario"), q.Get("connection")
	if agent == "" || scenario == "" || connection == "" {
		writeErr(w, http.StatusBadRequest, "agent, scenario and connection are required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	source := chi.URLParam(r, "source")
	norm, ok := normalizers[source]
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown source "+source)
		return
	}
	envs, err := norm(r, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	type accepted struct {
		RunID     string `json:"run_id"`
		Duplicate bool   `json:"duplicate"`
	}
	out := []accepted{}
	for _, env := range envs {
		env.Connection = connection
		env.Trace = telemetry.New(r.Header.Get("traceparent"))
		if env.Initiator.Kind == "" {
			env.Initiator = run.Initiator{Kind: "system", ID: caller(r.Context())}
		}
		id, dup, err := s.Engine.Accept(r.Context(), agent, scenario, env)
		if err != nil {
			writeErr(w, acceptStatus(err), err.Error())
			return
		}
		out = append(out, accepted{RunID: id, Duplicate: dup})
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"runs": out})
}

// acceptStatus maps an Accept error to an HTTP status.
func acceptStatus(err error) int {
	switch {
	case errors.Is(err, run.ErrNoVersion), errors.Is(err, run.ErrNoConnection), strings.Contains(err.Error(), "has no scenario"):
		return http.StatusNotFound
	case errors.Is(err, run.ErrNotEnabled), errors.Is(err, run.ErrPaused), errors.Is(err, run.ErrNotCallable):
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

type normalizer func(r *http.Request, body []byte) ([]run.Envelope, error)

var normalizers = map[string]normalizer{
	"alertmanager": alertmanager,
	"raw":          raw,
}

// alertmanager splits an Alertmanager webhook into one envelope per firing alert.
func alertmanager(_ *http.Request, body []byte) ([]run.Envelope, error) {
	var wh struct {
		GroupKey string            `json:"groupKey"`
		Alerts   []json.RawMessage `json:"alerts"`
	}
	if err := json.Unmarshal(body, &wh); err != nil {
		return nil, fmt.Errorf("not an Alertmanager webhook: %w", err)
	}
	var envs []run.Envelope
	for _, a := range wh.Alerts {
		var al struct {
			Status      string `json:"status"`
			Fingerprint string `json:"fingerprint"`
			StartsAt    string `json:"startsAt"`
		}
		if err := json.Unmarshal(a, &al); err != nil {
			return nil, err
		}
		if al.Status != "firing" {
			continue
		}
		envs = append(envs, run.Envelope{
			DeliveryID: al.Fingerprint + "@" + al.StartsAt,
			Trigger:    run.Trigger{Type: "event", Source: "alertmanager"},
			Object:     run.Object{Key: al.Fingerprint},
			Initiator:  run.Initiator{Kind: "system", ID: "alertmanager", Ref: wh.GroupKey},
			Payload:    a,
		})
	}
	return envs, nil
}

// raw takes the body as the run input. Headers carry what the source knows:
// X-Charter-Delivery-Id, X-Charter-Object-Key, X-Charter-Reply-Channel, X-Charter-Reply-Thread.
func raw(r *http.Request, body []byte) ([]run.Envelope, error) {
	if !json.Valid(body) {
		return nil, errors.New("body is not JSON")
	}
	delivery := r.Header.Get("X-Charter-Delivery-Id")
	if delivery == "" {
		sum := sha256.Sum256(body)
		delivery = hex.EncodeToString(sum[:16])
	}
	env := run.Envelope{
		DeliveryID: delivery,
		Trigger:    run.Trigger{Type: "event", Source: "raw:" + r.URL.Query().Get("source_name")},
		Object:     run.Object{Key: r.Header.Get("X-Charter-Object-Key")},
		Payload:    body,
	}
	if ch := r.Header.Get("X-Charter-Reply-Channel"); ch != "" {
		env.Reply = &driver.Target{Channel: ch, Thread: r.Header.Get("X-Charter-Reply-Thread")}
	}
	return []run.Envelope{env}, nil
}

// resolve answers an approval wait. Two callers:
//   - a chat button: Mattermost integration request with a signed context;
//   - CLI or API: bearer token and {"decision", "fingerprint"}.
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	waitID := chi.URLParam(r, "id")
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var req struct {
		// Mattermost integration request.
		UserID   string            `json:"user_id"`
		UserName string            `json:"user_name"`
		Context  map[string]string `json:"context"`
		// API request.
		Decision    string `json:"decision"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body is not JSON")
		return
	}

	var by, decision, fp string
	chat := req.Context != nil
	if chat {
		c := req.Context
		if c["wait_id"] != waitID || !signing.Verify(s.ApprovalSecret, waitID, c["fingerprint"], c["decision"], c["sig"]) {
			writeErr(w, http.StatusForbidden, "bad signature")
			return
		}
		by, decision, fp = req.UserName, c["decision"], c["fingerprint"]
		if by == "" {
			by = req.UserID
		}
	} else {
		tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		name := ""
		for t, n := range s.Tokens {
			if subtle.ConstantTimeCompare([]byte(t), []byte(tok)) == 1 {
				name = n
			}
		}
		if name == "" {
			writeErr(w, http.StatusUnauthorized, "bearer token or signed context required")
			return
		}
		by, decision, fp = name, req.Decision, req.Fingerprint
	}
	if decision != "approve" && decision != "reject" {
		writeErr(w, http.StatusBadRequest, "decision must be approve or reject")
		return
	}

	err = s.Engine.Resolve(r.Context(), waitID, by, fp, decision == "approve")
	text := ""
	switch {
	case err == nil && decision == "approve":
		text = "Approved by @" + by + ". The action runs with the shown parameters."
	case err == nil:
		text = "Rejected by @" + by + "."
	case errors.Is(err, run.ErrWaitClosed):
		text = "This request is already resolved or expired."
	case errors.Is(err, run.ErrFingerprint):
		text = "Parameters changed since the draft was shown. Nothing was done."
	case errors.Is(err, run.ErrSameApprover):
		text = "The initiator cannot approve this action. Ask someone else."
	default:
		s.Log.Error("resolve wait", "wait", waitID, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if chat {
		// Mattermost shows ephemeral_text to the clicking user only.
		writeJSON(w, http.StatusOK, map[string]any{"ephemeral_text": text})
		return
	}
	status := http.StatusOK
	if err != nil {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]any{"result": text})
}

func (s *Server) setSwitch(off bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Connection string `json:"connection"`
			Reason     string `json:"reason"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
		if strings.TrimSpace(req.Reason) == "" {
			writeErr(w, http.StatusBadRequest, "reason is required")
			return
		}
		name, by := chi.URLParam(r, "name"), caller(r.Context())
		var err error
		if off {
			err = registry.Suspend(r.Context(), s.Pool, name, req.Connection, by, req.Reason, false)
		} else {
			err = registry.Resume(r.Context(), s.Pool, name, req.Connection, by, req.Reason)
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"agent": name, "connection": req.Connection, "off": off})
	}
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, agent, scenario, connection, state, status, object_key, error, received_at, finished_at
		FROM runs WHERE ($1 = '' OR agent = $1) AND ($2 = '' OR status = $2)
		ORDER BY received_at DESC LIMIT $3`, q.Get("agent"), q.Get("status"), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type item struct {
		ID         string     `json:"id"`
		Agent      string     `json:"agent"`
		Scenario   string     `json:"scenario"`
		Connection string     `json:"connection"`
		State      string     `json:"state"`
		Status     string     `json:"status"`
		ObjectKey  string     `json:"object_key"`
		Error      string     `json:"error,omitempty"`
		ReceivedAt time.Time  `json:"received_at"`
		FinishedAt *time.Time `json:"finished_at,omitempty"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Agent, &it.Scenario, &it.Connection, &it.State, &it.Status, &it.ObjectKey,
			&it.Error, &it.ReceivedAt, &it.FinishedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var doc []byte
	err := s.Pool.QueryRow(r.Context(), `
		SELECT jsonb_build_object(
			'id', r.id, 'agent', r.agent, 'version', v.version, 'scenario', r.scenario, 'connection', r.connection,
			'state', r.state, 'status', r.status, 'envelope', r.envelope, 'output', r.output, 'usage', r.usage,
			'error', r.error, 'received_at', r.received_at, 'started_at', r.started_at, 'finished_at', r.finished_at,
			'steps', coalesce((SELECT jsonb_agg(jsonb_build_object('state', s.state, 'attempt', s.attempt,
				'outcome', s.outcome, 'started_at', s.started_at, 'finished_at', s.finished_at) ORDER BY s.seq)
				FROM run_steps s WHERE s.run_id = r.id), '[]'::jsonb),
			'waits', coalesce((SELECT jsonb_agg(jsonb_build_object('id', w.id, 'kind', w.kind, 'state', w.state,
				'deadline', w.deadline, 'resolved_by', w.resolved_by)) FROM waits w WHERE w.run_id = r.id), '[]'::jsonb))
		FROM runs r JOIN agent_versions v ON v.id = r.agent_version_id
		WHERE r.id::text = $1`, id).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	var doc []byte
	err := s.Pool.QueryRow(r.Context(), `
		SELECT coalesce(jsonb_agg(jsonb_build_object(
			'name', a.name, 'team', a.team, 'owner', a.owner,
			'version', (SELECT version FROM agent_versions v WHERE v.agent = a.name AND applied_at IS NOT NULL
			            ORDER BY applied_at DESC LIMIT 1),
			'switches', coalesce((SELECT jsonb_agg(jsonb_build_object('connection', s.connection, 'state', s.state,
				'reason', s.reason, 'changed_by', s.changed_by, 'changed_at', s.changed_at))
				FROM switches s WHERE s.agent = a.name), '[]'::jsonb)) ORDER BY a.name), '[]'::jsonb)
		FROM agents a`).Scan(&doc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"agents":` + string(doc) + `}`))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
