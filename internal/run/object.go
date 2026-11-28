package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/object"
)

// Errors of object calls; the API maps them to codes.
var (
	ErrObjectNotFound       = errors.New("object not found")
	ErrConversationNotFound = errors.New("conversation not found")
	ErrNoObjectSpec         = errors.New("agent has no spec.object")
	ErrNoWebSource          = errors.New("connection binds no message source with receiver web")
	ErrArchived             = errors.New("object is archived")
)

// FieldConflictError means people changed a field after the version the edit was based on.
type FieldConflictError struct {
	Field   string `json:"field"`
	Rev     int64  `json:"rev"`
	Current any    `json:"current"`
}

func (e *FieldConflictError) Error() string {
	return fmt.Sprintf("field %s changed since the edit started (now at rev %d)", e.Field, e.Rev)
}

// InvalidError is an edit the object schema refuses.
type InvalidError struct{ Problems []string }

func (e *InvalidError) Error() string { return "invalid: " + strings.Join(e.Problems, "; ") }

// NotifyChannel is the PostgreSQL channel that wakes event streams: the payload is "<object id>:<seq>".
const NotifyChannel = "charter_objects"

// FieldSource says where the current value of a state path came from.
type FieldSource struct {
	Rev            int64     `json:"rev"`
	Source         string    `json:"source"` // conversation, person
	ConversationID string    `json:"conversation_id,omitempty"`
	RunID          string    `json:"run_id,omitempty"`
	By             string    `json:"by"`
	At             time.Time `json:"at"`
}

// Member is a role of a person on an object.
type Member struct {
	Person string `json:"person"`
	Role   string `json:"role"` // owner, member
}

// ConversationView is a conversation about an object, without its messages.
type ConversationView struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	OpenedBy   string     `json:"opened_by"`
	CreatedAt  time.Time  `json:"created_at"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

// MessageView is a message of a conversation.
type MessageView struct {
	Seq          int64     `json:"seq"`
	MessageID    string    `json:"message_id,omitempty"`
	Conversation string    `json:"conversation"`
	Role         string    `json:"role"` // user, agent
	Author       string    `json:"author"`
	Text         string    `json:"text"`
	RunID        string    `json:"run_id,omitempty"`
	Fields       []string  `json:"fields"`
	At           time.Time `json:"at"`
}

// ArtifactView is something an agent created for the object in an external system.
type ArtifactView struct {
	System    string    `json:"system"`
	Ref       string    `json:"ref"`
	RunID     string    `json:"run_id"`
	CreatedAt time.Time `json:"created_at"`
}

// WaitView is an open approval of a run about the object.
type WaitView struct {
	ID          string          `json:"id"`
	RunID       string          `json:"run_id"`
	Action      json.RawMessage `json:"action"`
	Fingerprint string          `json:"fingerprint"`
	Deadline    time.Time       `json:"deadline"`
}

// ObjectView is the snapshot of an object people and the UI see.
type ObjectView struct {
	ID         string                 `json:"id"`
	Agent      string                 `json:"agent"`
	Connection string                 `json:"connection"`
	Key        string                 `json:"key"`
	Title      string                 `json:"title"`
	Attrs      map[string]any         `json:"attrs"`
	State      map[string]any         `json:"state"`
	Rev        int64                  `json:"rev"`
	Fields     []object.Field         `json:"fields"`
	Schema     json.RawMessage        `json:"schema"`
	Missing    []string               `json:"missing"`
	Sources    map[string]FieldSource `json:"sources"`
	Stage      string                 `json:"stage"`
	Stages     []Stage                `json:"stages"`
	Members    []Member               `json:"members"`
	Artifacts  []ArtifactView         `json:"artifacts"`
	Waits      []WaitView             `json:"waits"`
	// Conversations are the open ones, newest first.
	Conversations []ConversationView `json:"conversations"`
	LastSeq       int64              `json:"last_seq"`
	CreatedBy     string             `json:"created_by"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
	ArchivedAt    *time.Time         `json:"archived_at,omitempty"`
}

// ObjectSummary is a line of an object list.
type ObjectSummary struct {
	ID         string     `json:"id"`
	Agent      string     `json:"agent"`
	Connection string     `json:"connection"`
	Title      string     `json:"title"`
	Stage      string     `json:"stage"`
	Missing    int        `json:"missing"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

// Event is an entry of an object's event log.
type Event struct {
	Seq  int64           `json:"seq"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
	At   time.Time       `json:"at"`
}

// ObjectInput is the object as a conversation run sees it, under "object" of the input.
type ObjectInput struct {
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Stage   string         `json:"stage"`
	Rev     int64          `json:"rev"`
	State   map[string]any `json:"state"`
	Missing []string       `json:"missing"`
}

// appendEvent writes an event of the object in tx: the number is issued under the object row lock,
// so the events of one object are numbered without gaps in commit order. Streams wake on commit.
func appendEvent(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, typ string, data any) (int64, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	var seq int64
	if err := tx.QueryRow(ctx, `UPDATE objects SET last_seq = last_seq + 1, updated_at = now() WHERE id = $1 RETURNING last_seq`,
		objectID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("event %s: %w", typ, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO object_events (object_id, seq, type, data) VALUES ($1, $2, $3, $4)`,
		objectID, seq, typ, b); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, NotifyChannel, fmt.Sprintf("%s:%d", objectID, seq)); err != nil {
		return 0, err
	}
	return seq, nil
}

// objectRow is an object with the schema of its agent's current version.
type objectRow struct {
	ID                            uuid.UUID
	Agent, Connection, Key, Title string
	Attrs, State                  map[string]any
	Rev, LastSeq                  int64
	Stage, CreatedBy              string
	CreatedAt, UpdatedAt          time.Time
	ArchivedAt                    *time.Time
	Spec                          ObjectSpec
	Fields                        []object.Field
}

// loadObject reads an object; forUpdate locks its row in tx. The schema is the current version's:
// schema changes between versions only add fields (validate refuses the rest).
func (e *Engine) loadObject(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string, forUpdate bool) (objectRow, error) {
	var o objectRow
	oid, err := uuid.Parse(id)
	if err != nil {
		return o, ErrObjectNotFound
	}
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE OF o"
	}
	var attrs, state, spec []byte
	err = q.QueryRow(ctx, `
		SELECT o.id, o.agent, o.connection, o.key, o.title, o.attrs, o.state, o.rev, o.last_seq, o.stage, o.created_by,
		       o.created_at, o.updated_at, o.archived_at,
		       coalesce((SELECT v.package -> 'object' FROM agent_versions v
		                 WHERE v.agent = o.agent AND v.applied_at IS NOT NULL ORDER BY v.applied_at DESC LIMIT 1), 'null'::jsonb)
		FROM objects o WHERE o.id = $1`+lock, oid).Scan(&o.ID, &o.Agent, &o.Connection, &o.Key, &o.Title, &attrs, &state,
		&o.Rev, &o.LastSeq, &o.Stage, &o.CreatedBy, &o.CreatedAt, &o.UpdatedAt, &o.ArchivedAt, &spec)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrObjectNotFound
	}
	if err != nil {
		return o, err
	}
	if o.Attrs, err = object.Decode(attrs); err != nil {
		return o, err
	}
	if o.State, err = object.Decode(state); err != nil {
		return o, err
	}
	var sp *ObjectSpec
	if err := json.Unmarshal(spec, &sp); err != nil {
		return o, fmt.Errorf("decode object spec: %w", err)
	}
	if sp == nil {
		return o, fmt.Errorf("%s: %w", o.Agent, ErrNoObjectSpec)
	}
	o.Spec = *sp
	if o.Fields, err = object.Fields(json.RawMessage(sp.Schema)); err != nil {
		return o, err
	}
	return o, nil
}

// CreateObject starts a business object of an agent with memory: object. The creator becomes its owner.
func (e *Engine) CreateObject(ctx context.Context, agent, connection, title string, attrs map[string]any, by string) (ObjectView, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return ObjectView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var versionID uuid.UUID
	var pkgJSON []byte
	err = tx.QueryRow(ctx, `
		SELECT id, package FROM agent_versions WHERE agent = $1 AND applied_at IS NOT NULL
		ORDER BY applied_at DESC LIMIT 1`, agent).Scan(&versionID, &pkgJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return ObjectView{}, fmt.Errorf("%s: %w", agent, ErrNoVersion)
	}
	if err != nil {
		return ObjectView{}, err
	}
	var pkg Package
	if err := json.Unmarshal(pkgJSON, &pkg); err != nil {
		return ObjectView{}, fmt.Errorf("decode package: %w", err)
	}
	if pkg.Object == nil {
		return ObjectView{}, fmt.Errorf("%s: %w", agent, ErrNoObjectSpec)
	}
	if _, ok := pkg.Connections[connection]; !ok {
		return ObjectView{}, fmt.Errorf("%s/%s: %w", agent, connection, ErrNoConnection)
	}
	if attrs == nil {
		attrs = map[string]any{}
	}
	attrsJSON, err := json.Marshal(attrs)
	if err != nil {
		return ObjectView{}, err
	}
	stage := ""
	if len(pkg.Object.Stages) > 0 {
		stage = pkg.Object.Stages[0].ID
	}
	id := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO objects (id, agent, connection, key, title, attrs, agent_version_id, stage, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, agent, connection, id.String(), title, attrsJSON, versionID, stage, by); err != nil {
		return ObjectView{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO object_members (object_id, person, role, added_by) VALUES ($1, $2, 'owner', $2)`,
		id, by); err != nil {
		return ObjectView{}, err
	}
	if _, err := appendEvent(ctx, tx, id, "object", map[string]any{"title": title, "attrs": attrs, "created_by": by}); err != nil {
		return ObjectView{}, err
	}
	if err := audit(ctx, tx, by, "object.create", id.String(), map[string]any{"agent": agent, "connection": connection}); err != nil {
		return ObjectView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ObjectView{}, err
	}
	return e.GetObject(ctx, id.String())
}

func audit(ctx context.Context, tx pgx.Tx, by, action, target string, details any) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit (actor, action, target, details) VALUES ($1, $2, $3, $4)`, by, action, target, b)
	return err
}

// GetObject returns the snapshot of an object; LastSeq is the event the snapshot includes.
func (e *Engine) GetObject(ctx context.Context, id string) (ObjectView, error) {
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ObjectView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	o, err := e.loadObject(ctx, tx, id, false)
	if err != nil {
		return ObjectView{}, err
	}
	v := ObjectView{ID: o.ID.String(), Agent: o.Agent, Connection: o.Connection, Key: o.Key, Title: o.Title, Attrs: o.Attrs,
		State: o.State, Rev: o.Rev, Fields: o.Fields, Schema: json.RawMessage(o.Spec.Schema), Missing: object.Missing(o.Fields, o.State),
		Sources: map[string]FieldSource{}, Stage: o.Stage, Stages: o.Spec.Stages, Members: []Member{},
		Artifacts: []ArtifactView{}, Waits: []WaitView{}, Conversations: []ConversationView{},
		LastSeq: o.LastSeq, CreatedBy: o.CreatedBy, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt, ArchivedAt: o.ArchivedAt}
	if v.Stages == nil {
		v.Stages = []Stage{}
	}

	rows, err := tx.Query(ctx, `SELECT path, rev, source, coalesce(conversation_id::text, ''), coalesce(run_id::text, ''), by, at
		FROM object_fields WHERE object_id = $1`, o.ID)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var path string
		var s FieldSource
		if err := rows.Scan(&path, &s.Rev, &s.Source, &s.ConversationID, &s.RunID, &s.By, &s.At); err != nil {
			rows.Close()
			return v, err
		}
		v.Sources[path] = s
	}
	rows.Close()

	if v.Members, err = collect(ctx, tx, `SELECT person, role FROM object_members WHERE object_id = $1 ORDER BY added_at, person`,
		func(r pgx.Rows) (Member, error) {
			var m Member
			return m, r.Scan(&m.Person, &m.Role)
		}, o.ID); err != nil {
		return v, err
	}
	if v.Conversations, err = collect(ctx, tx, `SELECT id::text, title, opened_by, created_at, archived_at FROM conversations
		WHERE object_id = $1 AND archived_at IS NULL ORDER BY created_at DESC`,
		func(r pgx.Rows) (ConversationView, error) {
			var c ConversationView
			return c, r.Scan(&c.ID, &c.Title, &c.OpenedBy, &c.CreatedAt, &c.ArchivedAt)
		}, o.ID); err != nil {
		return v, err
	}
	if v.Artifacts, err = collect(ctx, tx, `SELECT a.system, a.ref, a.run_id::text, a.created_at FROM artifacts a
		JOIN runs r ON r.id = a.run_id WHERE r.object_id = $1 ORDER BY a.created_at`,
		func(r pgx.Rows) (ArtifactView, error) {
			var a ArtifactView
			return a, r.Scan(&a.System, &a.Ref, &a.RunID, &a.CreatedAt)
		}, o.ID); err != nil {
		return v, err
	}
	if v.Waits, err = collect(ctx, tx, `SELECT w.id::text, w.run_id::text, d.action, d.fingerprint, w.deadline FROM waits w
		JOIN runs r ON r.id = w.run_id JOIN drafts d ON d.id = w.draft_id
		WHERE r.object_id = $1 AND w.state = 'open' ORDER BY w.created_at`,
		func(r pgx.Rows) (WaitView, error) {
			var w WaitView
			return w, r.Scan(&w.ID, &w.RunID, &w.Action, &w.Fingerprint, &w.Deadline)
		}, o.ID); err != nil {
		return v, err
	}
	return v, nil
}

func collect[T any](ctx context.Context, tx pgx.Tx, sql string, scan func(pgx.Rows) (T, error), args ...any) ([]T, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ObjectFilter selects objects of an agent.
type ObjectFilter struct {
	Agent, Connection string
	// Person limits the list to objects where the person has a role.
	Person   string
	Archived bool
	// Before is the cursor: the updated_at and id of the last object of the previous page.
	Before *ObjectCursor
	Limit  int
}

// ObjectCursor points into an object list.
type ObjectCursor struct {
	UpdatedAt time.Time
	ID        string
}

// ListObjects lists objects, most recently updated first.
func (e *Engine) ListObjects(ctx context.Context, f ObjectFilter) ([]ObjectSummary, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	var beforeAt *time.Time
	beforeID := ""
	if f.Before != nil {
		beforeAt, beforeID = &f.Before.UpdatedAt, f.Before.ID
	}
	rows, err := e.pool.Query(ctx, `
		SELECT o.id::text, o.agent, o.connection, o.title, o.stage, o.state, o.updated_at, o.archived_at
		FROM objects o
		WHERE o.agent = $1 AND ($2 = '' OR o.connection = $2)
		  AND ($3 = '' OR EXISTS (SELECT 1 FROM object_members m WHERE m.object_id = o.id AND m.person = $3))
		  AND (o.archived_at IS NOT NULL) = $4
		  AND ($5::timestamptz IS NULL OR (o.updated_at, o.id::text) < ($5, $6))
		ORDER BY o.updated_at DESC, o.id::text DESC LIMIT $7`,
		f.Agent, f.Connection, f.Person, f.Archived, beforeAt, beforeID, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fields []object.Field
	if pkg, err := e.current(ctx, f.Agent); err == nil && pkg.Object != nil {
		fields, _ = object.Fields(json.RawMessage(pkg.Object.Schema))
	}
	out := []ObjectSummary{}
	for rows.Next() {
		var s ObjectSummary
		var state []byte
		if err := rows.Scan(&s.ID, &s.Agent, &s.Connection, &s.Title, &s.Stage, &state, &s.UpdatedAt, &s.ArchivedAt); err != nil {
			return nil, err
		}
		st, err := object.Decode(state)
		if err != nil {
			return nil, err
		}
		s.Missing = len(object.Missing(fields, st))
		out = append(out, s)
	}
	return out, rows.Err()
}

// ObjectUpdate changes what people set on an object; nil leaves a part alone.
type ObjectUpdate struct {
	Title    *string
	Attrs    map[string]any
	Archived *bool
}

// UpdateObject renames, sets attributes or archives an object.
func (e *Engine) UpdateObject(ctx context.Context, id string, u ObjectUpdate, by string) (ObjectView, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return ObjectView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	o, err := e.loadObject(ctx, tx, id, true)
	if err != nil {
		return ObjectView{}, err
	}
	data := map[string]any{"by": by}
	if u.Title != nil {
		if _, err := tx.Exec(ctx, `UPDATE objects SET title = $2 WHERE id = $1`, o.ID, *u.Title); err != nil {
			return ObjectView{}, err
		}
		data["title"] = *u.Title
	}
	if u.Attrs != nil {
		merged := o.Attrs
		for k, v := range u.Attrs {
			if v == nil {
				delete(merged, k)
			} else {
				merged[k] = v
			}
		}
		b, err := json.Marshal(merged)
		if err != nil {
			return ObjectView{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE objects SET attrs = $2 WHERE id = $1`, o.ID, b); err != nil {
			return ObjectView{}, err
		}
		data["attrs"] = merged
	}
	if u.Archived != nil {
		if _, err := tx.Exec(ctx, `UPDATE objects SET archived_at = CASE WHEN $2 THEN coalesce(archived_at, now()) END WHERE id = $1`,
			o.ID, *u.Archived); err != nil {
			return ObjectView{}, err
		}
		data["archived"] = *u.Archived
	}
	if _, err := appendEvent(ctx, tx, o.ID, "object", data); err != nil {
		return ObjectView{}, err
	}
	if err := audit(ctx, tx, by, "object.update", o.ID.String(), data); err != nil {
		return ObjectView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ObjectView{}, err
	}
	return e.GetObject(ctx, id)
}

// FieldEdit is a field people set by hand: Value null clears it, Rev is the field's rev the edit is based on.
type FieldEdit struct {
	Value json.RawMessage `json:"value"`
	Rev   int64           `json:"rev"`
}

// EditState applies people's edits to the state. An edit conflicts only when the same field changed
// after its rev: edits of other fields, by the model or by people, do not get in the way.
func (e *Engine) EditState(ctx context.Context, id string, edits map[string]FieldEdit, by string) (ObjectView, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return ObjectView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	o, err := e.loadObject(ctx, tx, id, true)
	if err != nil {
		return ObjectView{}, err
	}
	if o.ArchivedAt != nil {
		return ObjectView{}, ErrArchived
	}
	known := map[string]bool{}
	for _, f := range o.Fields {
		known[f.Name] = true
	}
	names := make([]string, 0, len(edits))
	for n := range edits {
		names = append(names, n)
	}
	slices.Sort(names)
	state := o.State
	var problems []string
	for _, n := range names {
		ed := edits[n]
		if !known[n] {
			problems = append(problems, fmt.Sprintf("%q is not a field of the state", n))
			continue
		}
		var cur int64
		err := tx.QueryRow(ctx, `SELECT coalesce(max(rev), 0) FROM object_fields WHERE object_id = $1 AND (path = $2 OR starts_with(path, $3))`,
			o.ID, n, n+"/").Scan(&cur)
		if err != nil {
			return ObjectView{}, err
		}
		if cur > ed.Rev {
			return ObjectView{}, &FieldConflictError{Field: n, Rev: cur, Current: state[n]}
		}
		var v any
		if len(ed.Value) > 0 {
			if err := json.Unmarshal(ed.Value, &v); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", n, err))
				continue
			}
		}
		if v == nil {
			delete(state, n)
		} else {
			state[n] = v
		}
	}
	if len(problems) > 0 {
		return ObjectView{}, &InvalidError{Problems: problems}
	}
	if err := e.saveState(ctx, tx, o, state, names, FieldSource{Source: "person", By: by}, nil); err != nil {
		return ObjectView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ObjectView{}, err
	}
	return e.GetObject(ctx, id)
}

// saveState checks the form of a new state and stores it with the sources of the changed paths and
// a state event. The object row must be locked in tx.
func (e *Engine) saveState(ctx context.Context, tx pgx.Tx, o objectRow, state map[string]any, changed []string,
	src FieldSource, skipped []string) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if ps := object.CheckForm(json.RawMessage(o.Spec.Schema), b); len(ps) > 0 {
		return &InvalidError{Problems: ps}
	}
	if len(changed) == 0 && len(skipped) == 0 {
		return nil
	}
	rev := o.Rev
	if len(changed) > 0 {
		rev++
		if _, err := tx.Exec(ctx, `UPDATE objects SET state = $2, rev = $3 WHERE id = $1`, o.ID, b, rev); err != nil {
			return err
		}
	}
	var conv, runID *uuid.UUID
	if src.ConversationID != "" {
		c := uuid.MustParse(src.ConversationID)
		conv = &c
	}
	if src.RunID != "" {
		r := uuid.MustParse(src.RunID)
		runID = &r
	}
	values := map[string]any{}
	for _, p := range changed {
		field, _, _ := strings.Cut(p, "/")
		if src.Source == "person" {
			// a field set by hand is one path: its collection items lose their own sources
			if _, err := tx.Exec(ctx, `DELETE FROM object_fields WHERE object_id = $1 AND starts_with(path, $2)`, o.ID, field+"/"); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO object_fields (object_id, path, rev, source, conversation_id, run_id, by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (object_id, path) DO UPDATE SET rev = excluded.rev, source = excluded.source,
			    conversation_id = excluded.conversation_id, run_id = excluded.run_id, by = excluded.by, at = now()`,
			o.ID, p, rev, src.Source, conv, runID, src.By); err != nil {
			return err
		}
		values[field] = state[field]
	}
	data := map[string]any{"rev": rev, "changed": nonNil(changed), "values": values, "source": src.Source, "by": src.By,
		"missing": object.Missing(o.Fields, state)}
	if src.ConversationID != "" {
		data["conversation"] = src.ConversationID
	}
	if src.RunID != "" {
		data["run_id"] = src.RunID
	}
	if len(skipped) > 0 {
		data["skipped"] = skipped
	}
	_, err = appendEvent(ctx, tx, o.ID, "state", data)
	return err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// SetMembers replaces the roles on an object. An object keeps at least one owner.
func (e *Engine) SetMembers(ctx context.Context, id string, members []Member, by string) (ObjectView, error) {
	owners := 0
	for _, m := range members {
		switch {
		case strings.TrimSpace(m.Person) == "":
			return ObjectView{}, &InvalidError{Problems: []string{"person is required"}}
		case m.Role == "owner":
			owners++
		case m.Role != "member":
			return ObjectView{}, &InvalidError{Problems: []string{fmt.Sprintf("role %q: owner or member", m.Role)}}
		}
	}
	if owners == 0 {
		return ObjectView{}, &InvalidError{Problems: []string{"an object needs an owner"}}
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return ObjectView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	o, err := e.loadObject(ctx, tx, id, true)
	if err != nil {
		return ObjectView{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM object_members WHERE object_id = $1`, o.ID); err != nil {
		return ObjectView{}, err
	}
	for _, m := range members {
		if _, err := tx.Exec(ctx, `INSERT INTO object_members (object_id, person, role, added_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT (object_id, person) DO UPDATE SET role = excluded.role`, o.ID, m.Person, m.Role, by); err != nil {
			return ObjectView{}, err
		}
	}
	if _, err := appendEvent(ctx, tx, o.ID, "members", map[string]any{"members": members, "by": by}); err != nil {
		return ObjectView{}, err
	}
	if err := audit(ctx, tx, by, "object.members", o.ID.String(), members); err != nil {
		return ObjectView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ObjectView{}, err
	}
	return e.GetObject(ctx, id)
}

// HasRole says whether a person has a role on an object.
func (e *Engine) HasRole(ctx context.Context, id, person string) (bool, error) {
	oid, err := uuid.Parse(id)
	if err != nil {
		return false, nil
	}
	var ok bool
	err = e.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM object_members WHERE object_id = $1 AND person = $2)`,
		oid, person).Scan(&ok)
	return ok, err
}

// OpenConversation starts a conversation about an object. Its first message is the opener template
// of the package, rendered without a model from what the state lacks.
func (e *Engine) OpenConversation(ctx context.Context, objectID, title, by string) (ConversationView, MessageView, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return ConversationView{}, MessageView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	o, err := e.loadObject(ctx, tx, objectID, true)
	if err != nil {
		return ConversationView{}, MessageView{}, err
	}
	if o.ArchivedAt != nil {
		return ConversationView{}, MessageView{}, ErrArchived
	}
	name := o.Title
	if name == "" {
		name = o.Agent
	}
	text, err := object.Opener(o.Spec.Opener, object.NewOpenerData(name, o.Fields, o.State))
	if err != nil {
		e.log.Warn("opener template failed, using the plain one", "object", o.ID, "err", err)
		text, _ = object.Opener("", object.NewOpenerData(name, o.Fields, o.State))
	}
	c := ConversationView{ID: uuid.New().String(), Title: title, OpenedBy: by, CreatedAt: e.now()}
	if _, err := tx.Exec(ctx, `INSERT INTO conversations (id, object_id, title, opened_by, created_at) VALUES ($1, $2, $3, $4, $5)`,
		c.ID, o.ID, c.Title, by, c.CreatedAt); err != nil {
		return ConversationView{}, MessageView{}, err
	}
	if _, err := appendEvent(ctx, tx, o.ID, "conversation", c); err != nil {
		return ConversationView{}, MessageView{}, err
	}
	m := MessageView{Conversation: c.ID, Role: "agent", Author: o.Agent, Text: text, Fields: []string{}}
	if err := tx.QueryRow(ctx, `
		INSERT INTO conversation_messages (agent, connection, conversation, role, author, text)
		VALUES ($1, $2, $3, 'agent', $1, $4) RETURNING seq, created_at`, o.Agent, o.Connection, c.ID, text).Scan(&m.Seq, &m.At); err != nil {
		return ConversationView{}, MessageView{}, err
	}
	if _, err := appendEvent(ctx, tx, o.ID, "message", m); err != nil {
		return ConversationView{}, MessageView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ConversationView{}, MessageView{}, err
	}
	return c, m, nil
}

// conversationOf checks that a conversation belongs to an object.
func conversationOf(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, objectID uuid.UUID, convID string) (ConversationView, error) {
	var c ConversationView
	cid, err := uuid.Parse(convID)
	if err != nil {
		return c, ErrConversationNotFound
	}
	err = q.QueryRow(ctx, `SELECT id::text, title, opened_by, created_at, archived_at FROM conversations WHERE id = $1 AND object_id = $2`,
		cid, objectID).Scan(&c.ID, &c.Title, &c.OpenedBy, &c.CreatedAt, &c.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrConversationNotFound
	}
	return c, err
}

// UpdateConversation renames or archives a conversation.
func (e *Engine) UpdateConversation(ctx context.Context, objectID, convID string, title *string, archived *bool, by string) (ConversationView, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return ConversationView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	o, err := e.loadObject(ctx, tx, objectID, true)
	if err != nil {
		return ConversationView{}, err
	}
	if _, err := conversationOf(ctx, tx, o.ID, convID); err != nil {
		return ConversationView{}, err
	}
	if title != nil {
		if _, err := tx.Exec(ctx, `UPDATE conversations SET title = $2 WHERE id = $1`, convID, *title); err != nil {
			return ConversationView{}, err
		}
	}
	if archived != nil {
		if _, err := tx.Exec(ctx, `UPDATE conversations SET archived_at = CASE WHEN $2 THEN coalesce(archived_at, now()) END WHERE id = $1`,
			convID, *archived); err != nil {
			return ConversationView{}, err
		}
	}
	c, err := conversationOf(ctx, tx, o.ID, convID)
	if err != nil {
		return ConversationView{}, err
	}
	if _, err := appendEvent(ctx, tx, o.ID, "conversation", c); err != nil {
		return ConversationView{}, err
	}
	return c, tx.Commit(ctx)
}

// MessagePage selects messages of a conversation: after a seq (oldest first), before a seq (newest
// page, returned oldest first), or the newest page.
type MessagePage struct {
	Before, After int64
	Limit         int
}

// Messages returns a page of a conversation's messages, oldest first.
func (e *Engine) Messages(ctx context.Context, objectID, convID string, p MessagePage) ([]MessageView, error) {
	o, err := e.loadObject(ctx, e.pool, objectID, false)
	if err != nil {
		return nil, err
	}
	if _, err := conversationOf(ctx, e.pool, o.ID, convID); err != nil {
		return nil, err
	}
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	order := "DESC"
	if p.After > 0 {
		order = "ASC"
	}
	rows, err := e.pool.Query(ctx, `
		SELECT seq, message_id, conversation, role, author, text, coalesce(run_id::text, ''), fields, created_at
		FROM conversation_messages
		WHERE agent = $1 AND connection = $2 AND conversation = $3
		  AND ($4 = 0 OR seq > $4) AND ($5 = 0 OR seq < $5)
		ORDER BY seq `+order+` LIMIT $6`, o.Agent, o.Connection, convID, p.After, p.Before, p.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MessageView{}
	for rows.Next() {
		var m MessageView
		var fields []byte
		if err := rows.Scan(&m.Seq, &m.MessageID, &m.Conversation, &m.Role, &m.Author, &m.Text, &m.RunID, &fields, &m.At); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(fields, &m.Fields); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if order == "DESC" {
		slices.Reverse(out)
	}
	return out, nil
}

// Say takes a person's message in a conversation about an object and starts the run that answers it.
// A repeated message_id in the same conversation returns the run of the first one.
func (e *Engine) Say(ctx context.Context, objectID, convID, messageID, author string, groups []string, text string) (runID string, seq int64, dup bool, err error) {
	o, err := e.loadObject(ctx, e.pool, objectID, false)
	if err != nil {
		return "", 0, false, err
	}
	if o.ArchivedAt != nil {
		return "", 0, false, ErrArchived
	}
	c, err := conversationOf(ctx, e.pool, o.ID, convID)
	if err != nil {
		return "", 0, false, err
	}
	if c.ArchivedAt != nil {
		return "", 0, false, ErrArchived
	}
	pkg, err := e.current(ctx, o.Agent)
	if err != nil {
		return "", 0, false, err
	}
	conn, ok := pkg.Connections[o.Connection]
	if !ok {
		return "", 0, false, fmt.Errorf("%s/%s: %w", o.Agent, o.Connection, ErrNoConnection)
	}
	source := ""
	for src, ct := range conn.Triggers {
		if ct.Receiver == ReceiverWeb && hasTrigger(pkg, conn, "message", src) {
			source = src
			break
		}
	}
	if source == "" {
		return "", 0, false, fmt.Errorf("%s/%s: %w", o.Agent, o.Connection, ErrNoWebSource)
	}
	id, dup, err := e.converse(ctx, driver.Incoming{Agent: o.Agent, Connection: o.Connection, Source: source,
		Conversation: c.ID, MessageID: messageID, Author: author, Groups: groups, Text: text}, Object{Key: o.Key, ID: o.ID.String()})
	if err != nil {
		return "", 0, false, err
	}
	err = e.pool.QueryRow(ctx, `SELECT seq FROM conversation_messages WHERE run_id = $1 AND role = 'user'`, id).Scan(&seq)
	return id, seq, dup, err
}

// Events returns events of an object after a seq, oldest first, and the oldest seq still kept:
// a stream that asks for events older than that has lost some and must read the snapshot again.
func (e *Engine) Events(ctx context.Context, objectID string, after int64, limit int) ([]Event, int64, error) {
	oid, err := uuid.Parse(objectID)
	if err != nil {
		return nil, 0, ErrObjectNotFound
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var oldest int64
	// with no events kept, the next one is the oldest a stream can get
	err = e.pool.QueryRow(ctx, `SELECT coalesce((SELECT min(seq) FROM object_events WHERE object_id = $1), last_seq + 1)
		FROM objects WHERE id = $1`, oid).Scan(&oldest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, ErrObjectNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	rows, err := e.pool.Query(ctx, `SELECT seq, type, data, at FROM object_events WHERE object_id = $1 AND seq > $2
		ORDER BY seq LIMIT $3`, oid, after, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var ev Event
		if err := rows.Scan(&ev.Seq, &ev.Type, &ev.Data, &ev.At); err != nil {
			return nil, 0, err
		}
		out = append(out, ev)
	}
	return out, oldest, rows.Err()
}

// Retention of objects. Defaults until STD-0005 sets them: events are kept 30 days (snapshots are the
// source of truth), an object without activity for 30 days goes to the archive, the conversations of
// an object archived 180 days ago are deleted.
const (
	EventRetention        = 30 * 24 * time.Hour
	IdleArchive           = 30 * 24 * time.Hour
	ConversationRetention = 180 * 24 * time.Hour
)

type objectCleanupArgs struct{}

func (objectCleanupArgs) Kind() string { return "charter_object_cleanup" }

type objectCleanupWorker struct {
	river.WorkerDefaults[objectCleanupArgs]
	e *Engine
}

func (w *objectCleanupWorker) Work(ctx context.Context, _ *river.Job[objectCleanupArgs]) error {
	_, err := w.e.CleanupObjects(ctx)
	return err
}

// Cleanup is what CleanupObjects did.
type Cleanup struct {
	Events, Archived, Messages, Sessions int64
}

// CleanupObjects applies the retention of objects and drops expired login sessions.
func (e *Engine) CleanupObjects(ctx context.Context) (Cleanup, error) {
	var c Cleanup
	now := e.now()
	tag, err := e.pool.Exec(ctx, `DELETE FROM object_events WHERE at < $1`, now.Add(-EventRetention))
	if err != nil {
		return c, err
	}
	c.Events = tag.RowsAffected()
	rows, err := e.pool.Query(ctx, `SELECT id::text FROM objects WHERE archived_at IS NULL AND updated_at < $1`, now.Add(-IdleArchive))
	if err != nil {
		return c, err
	}
	idle, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return c, err
	}
	archived := true
	for _, id := range idle {
		if _, err := e.UpdateObject(ctx, id, ObjectUpdate{Archived: &archived}, "charter"); err != nil {
			return c, err
		}
		c.Archived++
	}
	tag, err = e.pool.Exec(ctx, `
		DELETE FROM conversation_messages m USING conversations c, objects o
		WHERE m.conversation = c.id::text AND c.object_id = o.id AND o.agent = m.agent
		  AND o.archived_at < $1`, now.Add(-ConversationRetention))
	if err != nil {
		return c, err
	}
	c.Messages = tag.RowsAffected()
	if tag, err = e.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < $1`, now); err != nil {
		return c, err
	}
	c.Sessions = tag.RowsAffected()
	if _, err = e.pool.Exec(ctx, `DELETE FROM auth_logins WHERE created_at < $1`, now.Add(-time.Hour)); err != nil {
		return c, err
	}
	return c, nil
}

// objectInput is the object part of a conversation run's input.
func (e *Engine) objectInput(ctx context.Context, id string) (*ObjectInput, error) {
	o, err := e.loadObject(ctx, e.pool, id, false)
	if err != nil {
		return nil, err
	}
	return &ObjectInput{ID: o.ID.String(), Title: o.Title, Stage: o.Stage, Rev: o.Rev, State: o.State,
		Missing: object.Missing(o.Fields, o.State)}, nil
}

// stateOps reads the model's operations on the object state from the output.
func stateOps(output json.RawMessage) ([]object.Op, error) {
	var o struct {
		StateOps []object.Op `json:"state_ops"`
	}
	if err := json.Unmarshal(output, &o); err != nil {
		return nil, fmt.Errorf("state_ops: %w", err)
	}
	return o.StateOps, nil
}

// baseRev is the object rev the run's input was built from.
func baseRev(input json.RawMessage) int64 {
	var in struct {
		Object *ObjectInput `json:"object"`
	}
	if json.Unmarshal(input, &in) != nil || in.Object == nil {
		return 0
	}
	return in.Object.Rev
}

// checkStateOps applies the model's operations to the current state without saving: a problem makes
// the output invalid, and the model gets one more try with it.
func (e *Engine) checkStateOps(ctx context.Context, r runRow) string {
	ops, err := stateOps(r.Output)
	if err != nil {
		return err.Error()
	}
	if len(ops) == 0 {
		return ""
	}
	o, err := e.loadObject(ctx, e.pool, r.Env.Object.ID, false)
	if err != nil {
		return "object: " + err.Error()
	}
	res, problems := object.Apply(o.Fields, o.State, ops, nil)
	if len(problems) > 0 {
		return strings.Join(problems, "; ")
	}
	b, _ := json.Marshal(res.State)
	if ps := object.CheckForm(json.RawMessage(o.Spec.Schema), b); len(ps) > 0 {
		return "state after state_ops: " + strings.Join(ps, "; ")
	}
	return ""
}

// applyStateOps saves the model's operations in tx under the object lock. A path people changed
// after the rev the model read is left alone: people win.
func (e *Engine) applyStateOps(ctx context.Context, tx pgx.Tx, r runRow) error {
	ops, err := stateOps(r.Output)
	if err != nil || len(ops) == 0 {
		return err
	}
	o, err := e.loadObject(ctx, tx, r.Env.Object.ID, true)
	if err != nil {
		return err
	}
	base := baseRev(r.Input)
	rows, err := tx.Query(ctx, `SELECT path FROM object_fields WHERE object_id = $1 AND source = 'person' AND rev > $2`, o.ID, base)
	if err != nil {
		return err
	}
	held := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		held[p] = true
	}
	rows.Close()
	res, problems := object.Apply(o.Fields, o.State, ops, func(p string) bool { return held[p] })
	if len(problems) > 0 {
		return &InvalidError{Problems: problems} // checked at step 5; the state changed shape meanwhile
	}
	conv := ""
	if r.Env.Conversation != nil {
		conv = r.Env.Conversation.ID
	}
	return e.saveState(ctx, tx, o, res.State, res.Changed,
		FieldSource{Source: "conversation", ConversationID: conv, RunID: r.ID.String(), By: r.Agent}, res.Skipped)
}

// runFields lists the state paths a run changed, in the order of its operations, for its answer.
func runFields(ctx context.Context, tx pgx.Tx, objectID string, runID uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT x.p FROM object_events e, jsonb_array_elements_text(e.data -> 'changed') WITH ORDINALITY AS x(p, n)
		WHERE e.object_id = $1 AND e.type = 'state' AND e.data ->> 'run_id' = $2
		ORDER BY e.seq, x.n`, objectID, runID.String())
	if err != nil {
		return nil, err
	}
	paths, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, p := range paths {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// runEvent writes the state of a run about an object to the object's log, for "typing…" and its outcome.
func runEvent(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, runID uuid.UUID, conversation, state, status, errText string) error {
	data := map[string]any{"run_id": runID.String(), "state": state}
	if conversation != "" {
		data["conversation"] = conversation
	}
	if status != "" {
		data["status"] = status
	}
	if errText != "" {
		data["error"] = strings.TrimPrefix(errText, validationPrefix)
	}
	_, err := appendEvent(ctx, tx, objectID, "run", data)
	return err
}
