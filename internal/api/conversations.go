package api

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/telemetry"
)

// message is what a channel driver sends for each message addressed to an agent.
type message struct {
	Conversation string `json:"conversation"`
	MessageID    string `json:"message_id"`
	Author       string `json:"author"`
	Text         string `json:"text"`
	// Reply is where the answer goes; without it the connection's reply channel is used.
	Reply *driver.Target `json:"reply,omitempty"`
}

// converse takes a message in a conversation with an agent. The core keeps the history and answers
// messages of one conversation in order; the driver only maps its channel to this call.
//
//	POST /v1/conversations/{agent}/{connection}/{source}
//	{"conversation": "...", "message_id": "...", "author": "...", "text": "...", "reply": {"channel": "...", "thread": "..."}}
//
// 403 means the author may not talk to the agent here: the driver tells the author so.
func (s *Server) converse(w http.ResponseWriter, r *http.Request) {
	agent, connection, source := chi.URLParam(r, "agent"), chi.URLParam(r, "connection"), chi.URLParam(r, "source")
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		writeErr(w, http.StatusBadRequest, "body is not JSON")
		return
	}
	if m.Conversation == "" || m.MessageID == "" || m.Author == "" || strings.TrimSpace(m.Text) == "" {
		writeErr(w, http.StatusBadRequest, "conversation, message_id, author and text are required")
		return
	}
	scenarios, ct, err := s.Engine.Route(r.Context(), agent, connection, "message", source)
	if err != nil {
		writeErr(w, acceptStatus(err), err.Error())
		return
	}
	switch {
	case len(scenarios) == 0:
		writeErr(w, http.StatusNotFound, "connection "+connection+" binds no message source "+source)
		return
	case len(scenarios) > 1:
		writeErr(w, http.StatusConflict, "message source "+source+" starts several scenarios")
		return
	case len(ct.Users) > 0 && !slices.Contains(ct.Users, m.Author):
		writeErr(w, http.StatusForbidden, "author may not talk to the agent through this connection")
		return
	}
	env := run.Envelope{
		DeliveryID:   m.MessageID,
		Trigger:      run.Trigger{Type: "message", Source: source},
		Connection:   connection,
		Object:       run.Object{Key: m.Conversation},
		Initiator:    run.Initiator{Kind: "human", ID: m.Author, Ref: caller(r.Context())},
		Reply:        m.Reply,
		Conversation: &run.Conversation{ID: m.Conversation, MessageID: m.MessageID, Author: m.Author, Text: m.Text},
		Payload:      body,
		Trace:        telemetry.New(r.Header.Get("traceparent")),
	}
	id, dup, err := s.Engine.Accept(r.Context(), agent, scenarios[0], env)
	if err != nil {
		writeErr(w, acceptStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, accepted{Scenario: scenarios[0], RunID: id, Duplicate: dup})
}
