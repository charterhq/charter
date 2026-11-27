package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/render"
)

// actionArgs returns the arguments of the scenario action from the output. With action_input the
// arguments are that field; a missing or null field means the model proposes no action this time.
func actionArgs(r runRow) (json.RawMessage, bool) {
	if r.Scn.ActionInput == "" {
		return r.Output, true
	}
	var o map[string]json.RawMessage
	if json.Unmarshal(r.Output, &o) != nil {
		return nil, false
	}
	v, ok := o[r.Scn.ActionInput]
	if !ok || string(v) == "null" {
		return nil, false
	}
	return v, true
}

// draftRow is the action of a run: what was proposed, who decided, what the action returned.
type draftRow struct {
	ID       uuid.UUID
	Action   json.RawMessage
	Result   json.RawMessage
	By       string
	Approval bool       // false: the scenario acts without approval
	CalledAt *time.Time // the action call started; set before the call, never cleared after it ran
}

func (e *Engine) loadDraft(ctx context.Context, runID uuid.UUID) (draftRow, bool, error) {
	var d draftRow
	err := e.pool.QueryRow(ctx, `
		SELECT d.id, d.action, d.result, d.approved_by,
		       EXISTS (SELECT 1 FROM waits w WHERE w.draft_id = d.id), d.called_at
		FROM drafts d WHERE d.run_id = $1 ORDER BY d.created_at DESC LIMIT 1`, runID).
		Scan(&d.ID, &d.Action, &d.Result, &d.By, &d.Approval, &d.CalledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, false, nil
	}
	return d, err == nil, err
}

// actionData is what a template sees as .action.
func actionData(d draftRow, state, by string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"input": d.Action, "result": d.Result, "state": state, "by": by})
	return b
}

// outcome is the message after an act scenario: with an approval the answer was already shown with
// the buttons, so people get the outcome (block done or rejected); without one, the answer itself
// with .action filled in.
func (e *Engine) outcomeMessage(ctx context.Context, r runRow) (driver.Message, bool, error) {
	d, ok, err := e.loadDraft(ctx, r.ID)
	if err != nil || !ok {
		return driver.Message{}, false, err
	}
	m := driver.Message{RunID: r.ID.String(), Agent: r.Agent, Connection: r.Connection, ObjectKey: r.ObjectKey,
		Identities: r.Identities, Target: r.target(), Data: r.Output}
	rr := e.renderRun(r, StatusOK)
	if !d.Approval {
		if r.Scn.Template != "" {
			if text, err := render.OutputWith(r.Scn.Template, rr, r.Input, r.Output, actionData(d, "done", "")); err == nil {
				m.Text = text
				return m, true, nil
			}
		}
		m.Text = answerText("", r.Output)
		return m, true, nil
	}
	state, block := "done", render.DoneBlock
	var resolvedBy string
	_ = e.pool.QueryRow(ctx, `SELECT resolved_by FROM waits WHERE draft_id = $1 ORDER BY created_at DESC LIMIT 1`, d.ID).Scan(&resolvedBy)
	if r.Status == StatusCancelled {
		state, block = "rejected", render.RejectedBlock
	}
	if r.Scn.Template != "" {
		if text, ok, err := render.Block(r.Scn.Template, block, rr, r.Input, r.Output, actionData(d, state, resolvedBy)); err != nil {
			e.log.Warn("outcome template failed", "run_id", r.ID, "block", block, "err", err)
		} else if ok {
			m.Text = text
			return m, true, nil
		}
	}
	switch {
	case state == "done":
		m.Text = fmt.Sprintf("**%s**: done (`%s`).", r.Agent, r.Scn.Action)
	case resolvedBy != "":
		m.Text = fmt.Sprintf("**%s**: rejected by @%s, nothing was done.", r.Agent, resolvedBy)
	default:
		m.Text = fmt.Sprintf("**%s**: %s, nothing was done.", r.Agent, strings.TrimPrefix(r.Error, validationPrefix))
	}
	return m, true, nil
}

// supersede closes a pending approval of an earlier turn of the conversation: the newer message
// changes the request, the old draft must not be approved any more. The old run ends quietly.
func (e *Engine) supersede(ctx context.Context, r runRow, mySeq int64) error {
	rows, err := e.pool.Query(ctx, `
		SELECT w.id, w.run_id FROM waits w
		JOIN conversation_messages m ON m.run_id = w.run_id AND m.role = 'user'
		WHERE m.agent = $1 AND m.connection = $2 AND m.conversation = $3 AND m.seq < $4
		  AND w.state = 'open' AND w.kind = 'approval'`, r.Agent, r.Connection, r.Env.Conversation.ID, mySeq)
	if err != nil {
		return err
	}
	type pending struct{ wait, run uuid.UUID }
	var ps []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.wait, &p.run); err != nil {
			rows.Close()
			return err
		}
		ps = append(ps, p)
	}
	rows.Close()
	for _, p := range ps {
		tx, err := e.pool.Begin(ctx)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE waits SET state = 'rejected', resolved_by = 'superseded', resolved_at = now()
			WHERE id = $1 AND state = 'open'`, p.wait)
		if err == nil && tag.RowsAffected() == 1 {
			err = e.transition(ctx, tx, p.run, Approving, Recording, StatusCancelled, "superseded by a newer message", e.now(), 1)
		}
		if err != nil && !errors.Is(err, errStale) {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
