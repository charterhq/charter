package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/charterhq/charter/internal/driver"
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
	in := driver.Incoming{Agent: agent, Connection: connection, Source: source, Conversation: m.Conversation,
		MessageID: m.MessageID, Author: m.Author, Text: m.Text}
	if m.Reply != nil {
		in.Reply = *m.Reply
	}
	id, dup, err := s.Engine.Converse(r.Context(), in)
	switch {
	case errors.Is(err, driver.ErrForbidden):
		writeErr(w, http.StatusForbidden, err.Error())
		return
	case err != nil:
		writeErr(w, acceptStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"run_id": id, "duplicate": dup})
}
