package run

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// History limits: the newest messages that fit go into the input, older ones are left out.
const (
	historyMessages = 40
	historyChars    = 60000
)

// Turn is a past message of a conversation as the model sees it.
type Turn struct {
	Role   string    `json:"role"` // user, agent
	Author string    `json:"author,omitempty"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
}

// MessageInput is the run input of a message trigger. The scenario input schema describes this shape.
type MessageInput struct {
	Conversation string `json:"conversation"`
	Message      struct {
		Author string `json:"author"`
		Text   string `json:"text"`
	} `json:"message"`
	// History holds earlier messages of the conversation, oldest first; only with memory: thread.
	History []Turn `json:"history"`
}

// saveUserMessage records the incoming message with the run that answers it.
func saveUserMessage(ctx context.Context, tx pgx.Tx, agent string, env Envelope, runID uuid.UUID) error {
	c := env.Conversation
	_, err := tx.Exec(ctx, `
		INSERT INTO conversation_messages (agent, connection, conversation, role, author, text, run_id)
		VALUES ($1, $2, $3, 'user', $4, $5, $6)`, agent, env.Connection, c.ID, c.Author, c.Text, runID)
	return err
}

// conversationInput waits for earlier messages of the same conversation to be answered and builds
// the input: one conversation is answered in order, different conversations in parallel.
func (e *Engine) conversationInput(ctx context.Context, r runRow) (json.RawMessage, error) {
	c := r.Env.Conversation
	var mySeq int64
	if err := e.pool.QueryRow(ctx, `SELECT seq FROM conversation_messages WHERE run_id = $1 AND role = 'user'`,
		r.ID).Scan(&mySeq); err != nil {
		return nil, err
	}
	var busy bool
	if err := e.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM conversation_messages m JOIN runs x ON x.id = m.run_id
		               WHERE m.agent = $1 AND m.connection = $2 AND m.conversation = $3
		                 AND m.role = 'user' AND m.seq < $4 AND x.state <> 'done')`,
		r.Agent, r.Connection, c.ID, mySeq).Scan(&busy); err != nil {
		return nil, err
	}
	if busy {
		return nil, river.JobSnooze(time.Second)
	}

	in := MessageInput{Conversation: c.ID, History: []Turn{}}
	in.Message.Author, in.Message.Text = c.Author, c.Text
	if r.Scn.Memory == "thread" {
		rows, err := e.pool.Query(ctx, `
			SELECT role, author, text, created_at FROM conversation_messages
			WHERE agent = $1 AND connection = $2 AND conversation = $3
			  AND (seq < $4 OR role = 'agent') -- turns are answered in order, so every agent reply is earlier than this turn
			ORDER BY seq DESC LIMIT $5`, r.Agent, r.Connection, c.ID, mySeq, historyMessages)
		if err != nil {
			return nil, err
		}
		total := 0
		for rows.Next() {
			var t Turn
			if err := rows.Scan(&t.Role, &t.Author, &t.Text, &t.At); err != nil {
				rows.Close()
				return nil, err
			}
			if total += len(t.Text); total > historyChars {
				break
			}
			in.History = append(in.History, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for i, j := 0, len(in.History)-1; i < j; i, j = i+1, j-1 {
			in.History[i], in.History[j] = in.History[j], in.History[i]
		}
	}
	return json.Marshal(in)
}

// saveAgentMessage records the answer people saw, in the transaction that ends step 9.
func saveAgentMessage(ctx context.Context, tx pgx.Tx, r runRow, text string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO conversation_messages (agent, connection, conversation, role, author, text, run_id)
		VALUES ($1, $2, $3, 'agent', $1, $4, $5) ON CONFLICT DO NOTHING`,
		r.Agent, r.Connection, r.Env.Conversation.ID, text, r.ID)
	return err
}

// answerText is what goes into the history: the rendered message, else output.reply, else the output.
func answerText(m string, output json.RawMessage) string {
	if m != "" {
		return m
	}
	var o struct {
		Reply string `json:"reply"`
	}
	if json.Unmarshal(output, &o) == nil && o.Reply != "" {
		return o.Reply
	}
	return string(output)
}
