package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/run"
)

// Objects are business objects of agents with memory: object (for the builder – a draft agent and its
// brief). The endpoints are experimental until a second agent uses them: they may change without
// keeping compatibility.

// objectErr maps an object call error to a status and a code.
func (s *Server) objectErr(w http.ResponseWriter, err error) {
	var conflict *run.FieldConflictError
	var invalid *run.InvalidError
	switch {
	case errors.Is(err, run.ErrObjectNotFound), errors.Is(err, run.ErrConversationNotFound):
		writeErrCode(w, http.StatusNotFound, "not_found", err.Error())
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "code": "field_conflict",
			"field": conflict.Field, "rev": conflict.Rev, "current": conflict.Current})
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "the state schema refuses the edit", "code": "invalid",
			"problems": invalid.Problems})
	case errors.Is(err, run.ErrArchived):
		writeErrCode(w, http.StatusConflict, "archived", err.Error())
	case errors.Is(err, run.ErrNoObjectSpec), errors.Is(err, run.ErrNoWebSource):
		writeErrCode(w, http.StatusConflict, "not_enabled", err.Error())
	case errors.Is(err, driver.ErrForbidden):
		writeErrCode(w, http.StatusForbidden, "forbidden", err.Error())
	default:
		if st := acceptStatus(err); st != http.StatusInternalServerError {
			writeErr(w, st, err.Error())
			return
		}
		s.Log.Error("object call", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

// visible says whether the caller may see the object: a role on it, or the platform. Others get 404,
// the same as for an object that does not exist.
func (s *Server) visible(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, who := chi.URLParam(r, "id"), caller(r.Context())
	if s.Platform[who] {
		return id, true
	}
	ok, err := s.Engine.HasRole(r.Context(), id, who)
	if err != nil {
		s.objectErr(w, err)
		return "", false
	}
	if !ok {
		writeErrCode(w, http.StatusNotFound, "not_found", run.ErrObjectNotFound.Error())
		return "", false
	}
	return id, true
}

// owner says whether the caller may change roles: an owner of the object, or the platform.
func (s *Server) owner(r *http.Request, v run.ObjectView) bool {
	who := caller(r.Context())
	if s.Platform[who] {
		return true
	}
	for _, m := range v.Members {
		if m.Person == who && m.Role == "owner" {
			return true
		}
	}
	return false
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "body is not JSON: "+err.Error())
		return false
	}
	return true
}

// createObject: POST /v1/agents/{agent}/objects {"connection", "title", "attrs"}
func (s *Server) createObject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Connection string         `json:"connection"`
		Title      string         `json:"title"`
		Attrs      map[string]any `json:"attrs"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Connection == "" {
		writeErr(w, http.StatusBadRequest, "connection is required")
		return
	}
	v, err := s.Engine.CreateObject(r.Context(), chi.URLParam(r, "agent"), req.Connection, req.Title, req.Attrs, caller(r.Context()))
	if err != nil {
		s.objectErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// listObjects: GET /v1/agents/{agent}/objects?connection=&archived=&cursor=&limit=
// A caller sees the objects where they have a role; the platform sees all.
func (s *Server) listObjects(w http.ResponseWriter, r *http.Request) {
	q, who := r.URL.Query(), caller(r.Context())
	f := run.ObjectFilter{Agent: chi.URLParam(r, "agent"), Connection: q.Get("connection"), Archived: q.Get("archived") == "true"}
	if !s.Platform[who] || q.Get("mine") == "1" {
		f.Person = who
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if c := q.Get("cursor"); c != "" {
		cur, err := decodeCursor(c)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad cursor")
			return
		}
		f.Before = &cur
	}
	list, err := s.Engine.ListObjects(r.Context(), f)
	if err != nil {
		s.objectErr(w, err)
		return
	}
	out := map[string]any{"objects": list}
	if n := len(list); n > 0 && (f.Limit <= 0 && n == 50 || n == f.Limit) {
		out["next_cursor"] = encodeCursor(run.ObjectCursor{UpdatedAt: list[n-1].UpdatedAt, ID: list[n-1].ID})
	}
	writeJSON(w, http.StatusOK, out)
}

func encodeCursor(c run.ObjectCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.UpdatedAt.Format(time.RFC3339Nano) + "|" + c.ID))
}

func decodeCursor(s string) (run.ObjectCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return run.ObjectCursor{}, err
	}
	at, id, ok := strings.Cut(string(b), "|")
	if !ok {
		return run.ObjectCursor{}, errors.New("bad cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	return run.ObjectCursor{UpdatedAt: t, ID: id}, err
}

// getObject: GET /v1/objects/{id} – the snapshot; last_seq is where its event stream continues.
func (s *Server) getObject(w http.ResponseWriter, r *http.Request) {
	id, ok := s.visible(w, r)
	if !ok {
		return
	}
	v, err := s.Engine.GetObject(r.Context(), id)
	if err != nil {
		s.objectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// updateObject: PATCH /v1/objects/{id} {"title"?, "attrs"?}; attrs are merged, null removes a key.
func (s *Server) updateObject(w http.ResponseWriter, r *http.Request) {
	id, ok := s.visible(w, r)
	if !ok {
		return
	}
	var req struct {
		Title *string        `json:"title"`
		Attrs map[string]any `json:"attrs"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	v, err := s.Engine.UpdateObject(r.Context(), id, run.ObjectUpdate{Title: req.Title, Attrs: req.Attrs}, caller(r.Context()))
	if err != nil {
		s.objectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// archiveObject: DELETE /v1/objects/{id} – the object goes to the archive, nothing is deleted.
func (s *Server) archiveObject(w http.ResponseWriter, r *http.Request) {
	id, ok := s.visible(w, r)
	if !ok {
		return
	}
	archived := true
	v, err := s.Engine.UpdateObject(r.Context(), id, run.ObjectUpdate{Archived: &archived}, caller(r.Context()))
	if err != nil {
		s.objectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// editState: PATCH /v1/objects/{id}/state {"fields": {"<name>": {"value": ..., "rev": N}}}
// value null clears the field; rev is the field's rev the edit is based on.
func (s *Server) editState(w http.ResponseWriter, r *http.Request) {
	id, ok := s.visible(w, r)
	if !ok {
		return
	}
	var req struct {
		Fields map[string]run.FieldEdit `json:"fields"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Fields) == 0 {
		writeErr(w, http.StatusBadRequest, "fields are required")
		return
	}
	v, err := s.Engine.EditState(r.Context(), id, req.Fields, caller(r.Context()))
	if err != nil {
		s.objectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// setMembers: PUT /v1/objects/{id}/members [{"person", "role"}] – owners and the platform only.
func (s *Server) setMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := s.visible(w, r)
	if !ok {
		return
	}
	v, err := s.Engine.GetObject(r.Context(), id)
	if err != nil {
		s.objectErr(w, err)
		return
	}
	if !s.owner(r, v) {
		writeErrCode(w, http.StatusForbidden, "forbidden", "only an owner changes roles")
		return
	}
	var members []run.Member
	if !readJSON(w, r, &members) {
		return
	}
	v, err = s.Engine.SetMembers(r.Context(), id, members, caller(r.Context()))
	if err != nil {
		s.objectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// openConversation: POST /v1/objects/{id}/conversations {"title"?} → the conversation and its first message.
func (s *Server) openConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := s.visible(w, r)
	if !ok {
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if r.ContentLength != 0 && !readJSON(w, r, &req) {
		return
	}
	c, m, err := s.Engine.OpenConversation(r.Context(), id, req.Title, caller(r.Context()))
	if err != nil {
		s.objectErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"conversation": c, "message": m})
}

// updateConversation: PATCH /v1/objects/{id}/conversations/{cid} {"title"?, "archived"?}
func (s *Server) updateConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := s.visible(w, r)
	if !ok {
		return
	}
	var req struct {
		Title    *string `json:"title"`
		Archived *bool   `json:"archived"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	c, err := s.Engine.UpdateConversation(r.Context(), id, chi.URLParam(r, "cid"), req.Title, req.Archived, caller(r.Context()))
	if err != nil {
		s.objectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// listMessages: GET /v1/objects/{id}/conversations/{cid}/messages?before=&after=&limit=
func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	id, ok := s.visible(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var p run.MessagePage
	p.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	p.After, _ = strconv.ParseInt(q.Get("after"), 10, 64)
	p.Limit, _ = strconv.Atoi(q.Get("limit"))
	msgs, err := s.Engine.Messages(r.Context(), id, chi.URLParam(r, "cid"), p)
	if err != nil {
		s.objectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

// say: POST /v1/objects/{id}/conversations/{cid}/messages {"message_id", "text"} → 202 {"run_id", "seq", "duplicate"}.
// The author is the caller; message_id is the client's id, unique within the conversation.
func (s *Server) say(w http.ResponseWriter, r *http.Request) {
	id, ok := s.visible(w, r)
	if !ok {
		return
	}
	var req struct {
		MessageID string `json:"message_id"`
		Text      string `json:"text"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.MessageID == "" || strings.TrimSpace(req.Text) == "" {
		writeErr(w, http.StatusBadRequest, "message_id and text are required")
		return
	}
	runID, seq, dup, err := s.Engine.Say(r.Context(), id, chi.URLParam(r, "cid"), req.MessageID, caller(r.Context()), req.Text)
	if err != nil {
		s.objectErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"run_id": runID, "seq": seq, "duplicate": dup})
}
