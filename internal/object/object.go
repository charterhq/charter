// Package object holds the rules of a business object state (spec.memory: object): the fields of
// its schema, the two checks of the schema, the operations a model may apply and the schema
// evolution allowed between package versions. The package knows no database and no agent.
//
// The state schema is a JSON Schema of an object. Its top-level properties are the fields people
// see: title is the label, description is the question an empty field asks, required and minItems
// say when the state is complete. Two extensions:
//
//	x-charter-readonly: model    the model may not change the field
//	x-charter-collection: true   an array of objects with an id, changed one item at a time
package object

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/charterhq/charter/internal/schema"
)

// Field is a top-level property of the state schema.
type Field struct {
	Name     string `json:"name"`
	Title    string `json:"title,omitempty"`
	Question string `json:"question,omitempty"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
	// Collection: an array of objects with an id; items are changed one at a time.
	Collection bool `json:"collection,omitempty"`
	MinItems   int  `json:"min_items,omitempty"`
	// ReadonlyModel: the model may not change the field.
	ReadonlyModel bool `json:"readonly_model,omitempty"`
}

type propDoc struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Type        any             `json:"type"`
	MinItems    int             `json:"minItems"`
	Readonly    string          `json:"x-charter-readonly"`
	Collection  bool            `json:"x-charter-collection"`
	Items       json.RawMessage `json:"items"`
}

// Fields lists the top-level properties of the schema in the order they are written.
func Fields(schemaJSON json.RawMessage) ([]Field, error) {
	var doc struct {
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	if err := json.Unmarshal(schemaJSON, &doc); err != nil {
		return nil, fmt.Errorf("state schema: %w", err)
	}
	names, err := orderedKeys(doc.Properties)
	if err != nil {
		return nil, fmt.Errorf("state schema properties: %w", err)
	}
	var props map[string]propDoc
	if len(doc.Properties) > 0 {
		if err := json.Unmarshal(doc.Properties, &props); err != nil {
			return nil, fmt.Errorf("state schema properties: %w", err)
		}
	}
	required := map[string]bool{}
	for _, r := range doc.Required {
		required[r] = true
	}
	out := make([]Field, 0, len(names))
	for _, n := range names {
		p := props[n]
		t, _ := p.Type.(string)
		out = append(out, Field{Name: n, Title: p.Title, Question: p.Description, Type: t, Required: required[n],
			Collection: p.Collection, MinItems: p.MinItems, ReadonlyModel: p.Readonly == "model"})
	}
	return out, nil
}

// orderedKeys returns the keys of a JSON object in document order.
func orderedKeys(obj json.RawMessage) ([]string, error) {
	if len(bytes.TrimSpace(obj)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(obj))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not an object")
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		keys = append(keys, t.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// CheckSchema reports what makes a JSON Schema unfit as a state schema.
func CheckSchema(schemaJSON json.RawMessage) []string {
	if _, err := schema.Compile(schemaJSON); err != nil {
		return []string{"not a valid JSON Schema: " + err.Error()}
	}
	var doc struct {
		Type     string   `json:"type"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schemaJSON, &doc); err != nil {
		return []string{err.Error()}
	}
	var ps []string
	if doc.Type != "object" {
		ps = append(ps, "the state schema must be of type object")
	}
	fields, err := Fields(schemaJSON)
	if err != nil {
		return append(ps, err.Error())
	}
	if len(fields) == 0 {
		ps = append(ps, "the state schema has no properties")
	}
	known := map[string]bool{}
	for _, f := range fields {
		known[f.Name] = true
	}
	for _, r := range doc.Required {
		if !known[r] {
			ps = append(ps, fmt.Sprintf("required field %q is not a property", r))
		}
	}
	var props map[string]propDoc
	_ = json.Unmarshal(mustProps(schemaJSON), &props)
	for _, f := range fields {
		p := props[f.Name]
		if p.Readonly != "" && p.Readonly != "model" {
			ps = append(ps, fmt.Sprintf("field %s: x-charter-readonly must be model", f.Name))
		}
		if !f.Collection {
			continue
		}
		var items struct {
			Type       string                     `json:"type"`
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		_ = json.Unmarshal(p.Items, &items)
		if f.Type != "array" || items.Type != "object" {
			ps = append(ps, fmt.Sprintf("field %s: a collection is an array of objects", f.Name))
			continue
		}
		if _, ok := items.Properties["id"]; !ok {
			ps = append(ps, fmt.Sprintf("field %s: collection items need an id property", f.Name))
		}
	}
	return ps
}

func mustProps(schemaJSON json.RawMessage) json.RawMessage {
	var doc struct {
		Properties json.RawMessage `json:"properties"`
	}
	_ = json.Unmarshal(schemaJSON, &doc)
	return doc.Properties
}

// completeness keywords: the form check leaves them out, the completeness check reads them.
var completeness = map[string]bool{"required": true, "minItems": true, "minLength": true, "minProperties": true}

// Form returns the schema without completeness keywords: a partial state passes it, a state of the
// wrong shape does not.
func Form(schemaJSON json.RawMessage) (json.RawMessage, error) {
	var v any
	if err := json.Unmarshal(schemaJSON, &v); err != nil {
		return nil, err
	}
	return json.Marshal(stripCompleteness(v, ""))
}

func stripCompleteness(v any, parent string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			// under properties keys are field names, not keywords
			if parent != "properties" && completeness[k] {
				continue
			}
			out[k] = stripCompleteness(val, k)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = stripCompleteness(val, "")
		}
		return out
	}
	return v
}

// CheckForm validates the shape of a state; nil means it fits.
func CheckForm(schemaJSON, state json.RawMessage) []string {
	form, err := Form(schemaJSON)
	if err != nil {
		return []string{"state schema: " + err.Error()}
	}
	return schema.Check(form, state)
}

// Missing lists the fields that keep the state from being complete, in schema order: a required
// field that is empty, a collection shorter than its minItems.
func Missing(fields []Field, state map[string]any) []string {
	out := []string{}
	for _, f := range fields {
		v, ok := state[f.Name]
		switch {
		case f.MinItems > 0:
			items, _ := v.([]any)
			if len(nonEmpty(items)) < f.MinItems {
				out = append(out, f.Name)
			}
		case f.Required && (!ok || Empty(v)):
			out = append(out, f.Name)
		}
	}
	return out
}

func nonEmpty(items []any) []any {
	var out []any
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			rest := make(map[string]any, len(m))
			for k, v := range m {
				if k != "id" {
					rest[k] = v
				}
			}
			if Empty(rest) {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

// Empty says whether a value carries nothing: null, a blank string, an empty list, an object whose
// values are all empty.
func Empty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		for _, val := range t {
			if !Empty(val) {
				return false
			}
		}
		return true
	}
	return false
}

// Op is one change of the state the model asks for.
type Op struct {
	Op    string          `json:"op"` // set, clear, put, remove
	Field string          `json:"field"`
	ID    string          `json:"id,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

// Path is where an operation lands: the field, or the item of a collection.
func (o Op) Path() string {
	if o.ID != "" {
		return o.Field + "/" + o.ID
	}
	return o.Field
}

// Result is a state after operations.
type Result struct {
	State map[string]any
	// Changed are the paths the operations changed, Skipped the paths left alone because people
	// changed them after the model read the state.
	Changed, Skipped []string
}

// Apply applies the model's operations to a copy of the state. A problem means the output is
// invalid: the model asked for something the schema does not allow. held says whether a path was
// changed by people after the model read the state; such operations are skipped, people win.
func Apply(fields []Field, state map[string]any, ops []Op, held func(path string) bool) (Result, []string) {
	byName := map[string]Field{}
	for _, f := range fields {
		byName[f.Name] = f
	}
	res := Result{State: clone(state)}
	var problems []string
	seen := map[string]bool{}
	for i, op := range ops {
		f, ok := byName[op.Field]
		where := fmt.Sprintf("state_ops[%d]", i)
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s: %q is not a field of the state", where, op.Field))
			continue
		case f.ReadonlyModel:
			problems = append(problems, fmt.Sprintf("%s: field %s is not changed by the model", where, op.Field))
			continue
		}
		var value any
		if op.Op == "set" || op.Op == "put" {
			if len(op.Value) == 0 {
				problems = append(problems, fmt.Sprintf("%s: %s needs a value", where, op.Op))
				continue
			}
			if err := decode(op.Value, &value); err != nil {
				problems = append(problems, fmt.Sprintf("%s: value: %v", where, err))
				continue
			}
		}
		switch op.Op {
		case "set":
			if f.Collection {
				problems = append(problems, fmt.Sprintf("%s: %s is a collection, change it with put and remove", where, op.Field))
				continue
			}
			if value == nil {
				problems = append(problems, fmt.Sprintf("%s: null does not clear a field, use clear", where))
				continue
			}
		case "clear":
		case "put", "remove":
			if !f.Collection {
				problems = append(problems, fmt.Sprintf("%s: %s is not a collection, use set and clear", where, op.Field))
				continue
			}
			if op.ID == "" {
				problems = append(problems, fmt.Sprintf("%s: %s needs the item id", where, op.Op))
				continue
			}
			if m, ok := value.(map[string]any); op.Op == "put" && !ok {
				problems = append(problems, fmt.Sprintf("%s: a collection item is an object", where))
				continue
			} else if ok {
				m["id"] = op.ID
			}
		default:
			problems = append(problems, fmt.Sprintf("%s: unknown op %q (set, clear, put, remove)", where, op.Op))
			continue
		}
		path := op.Path()
		if held != nil && (held(path) || op.ID != "" && held(op.Field)) {
			res.Skipped = append(res.Skipped, path)
			continue
		}
		switch op.Op {
		case "set":
			res.State[op.Field] = value
		case "clear":
			delete(res.State, op.Field)
		case "put":
			res.State[op.Field] = putItem(res.State[op.Field], op.ID, value)
		case "remove":
			res.State[op.Field] = removeItem(res.State[op.Field], op.ID)
		}
		if !seen[path] {
			seen[path] = true
			res.Changed = append(res.Changed, path)
		}
	}
	return res, problems
}

func putItem(list any, id string, item any) []any {
	items, _ := list.([]any)
	out := make([]any, 0, len(items)+1)
	replaced := false
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && fmt.Sprint(m["id"]) == id {
			out = append(out, item)
			replaced = true
			continue
		}
		out = append(out, it)
	}
	if !replaced {
		out = append(out, item)
	}
	return out
}

func removeItem(list any, id string) []any {
	items, _ := list.([]any)
	out := make([]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && fmt.Sprint(m["id"]) == id {
			continue
		}
		out = append(out, it)
	}
	return out
}

func clone(state map[string]any) map[string]any {
	b, _ := json.Marshal(state)
	out := map[string]any{}
	_ = decode(b, &out)
	return out
}

func decode(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return dec.Decode(v)
}

// Decode reads a stored state.
func Decode(b []byte) (map[string]any, error) {
	out := map[string]any{}
	if len(b) == 0 {
		return out, nil
	}
	err := decode(b, &out)
	return out, err
}

// Compatible reports changes from the old state schema to the new one that would break objects
// created with the old one: a field removed, its type changed, a collection that stopped being one.
// Adding fields and changing titles and questions is allowed.
func Compatible(oldSchema, newSchema json.RawMessage) []string {
	if len(oldSchema) == 0 {
		return nil
	}
	oldF, err := Fields(oldSchema)
	if err != nil {
		return nil // an unreadable old schema has nothing to keep
	}
	newF, err := Fields(newSchema)
	if err != nil {
		return []string{err.Error()}
	}
	byName := map[string]Field{}
	for _, f := range newF {
		byName[f.Name] = f
	}
	var ps []string
	for _, o := range oldF {
		n, ok := byName[o.Name]
		switch {
		case !ok:
			ps = append(ps, fmt.Sprintf("field %s is removed: objects keep their state, add fields instead", o.Name))
		case n.Type != o.Type:
			ps = append(ps, fmt.Sprintf("field %s changes type from %q to %q", o.Name, o.Type, n.Type))
		case n.Collection != o.Collection:
			ps = append(ps, fmt.Sprintf("field %s changes whether it is a collection", o.Name))
		}
	}
	sort.Strings(ps)
	return ps
}
