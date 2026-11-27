// Package render turns a run output into a message with the agent's response template.
//
// Templates are Go text/template files shipped in the agent package. The template gets:
//
//	.output   the validated model output (decoded JSON)
//	.input    the run input (decoded JSON)
//	.run      id, short_id, agent, version, scenario, connection, autonomy, status, error, seconds
//	.action   for act scenarios after the action: input, result, state (done, rejected), by
//
// A template may define a block named "failed": it renders the message for a run that
// did not produce a valid answer (status failed or invalid). With an approval, the main template is
// shown with the buttons; block "done" renders the outcome after the action, block "rejected" after
// a rejection or an expired approval.
package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"unicode/utf8"
)

// Template blocks.
const (
	FailedBlock   = "failed"   // a run without a valid answer
	DoneBlock     = "done"     // after an approved action ran
	RejectedBlock = "rejected" // after a rejection or an expired approval
)

// Run is the run metadata a template sees as .run.
type Run struct {
	ID         string  `json:"id"`
	ShortID    string  `json:"short_id"`
	Agent      string  `json:"agent"`
	Version    string  `json:"version"`
	Scenario   string  `json:"scenario"`
	Connection string  `json:"connection"`
	Autonomy   string  `json:"autonomy"`
	Status     string  `json:"status"`
	Error      string  `json:"error"`
	Seconds    float64 `json:"seconds"`
	// Params are the scenario parameters of the run, seen as .params.
	Params map[string]any `json:"-"`
}

// Parse compiles a template and checks it, so a broken template fails at package load.
func Parse(name, text string) (*template.Template, error) {
	return template.New(name).Funcs(funcs).Option("missingkey=zero").Parse(text)
}

// Output renders the answer of a successful run.
func Output(text string, run Run, input, output json.RawMessage) (string, error) {
	t, err := Parse("response", text)
	if err != nil {
		return "", err
	}
	return exec(t, "", run, input, output, nil)
}

// OutputWith renders the answer with .action set (an act scenario without approval).
func OutputWith(text string, run Run, input, output, action json.RawMessage) (string, error) {
	t, err := Parse("response", text)
	if err != nil {
		return "", err
	}
	return exec(t, "", run, input, output, action)
}

// Block renders a named block with .action. ok is false when the template has no such block.
func Block(text, block string, run Run, input, output, action json.RawMessage) (msg string, ok bool, err error) {
	t, err := Parse("response", text)
	if err != nil {
		return "", false, err
	}
	if t.Lookup(block) == nil {
		return "", false, nil
	}
	msg, err = exec(t, block, run, input, output, action)
	return msg, err == nil, err
}

// Failed renders the failure message. ok is false when the template has no "failed" block.
func Failed(text string, run Run, input json.RawMessage) (msg string, ok bool, err error) {
	t, err := Parse("response", text)
	if err != nil {
		return "", false, err
	}
	if t.Lookup(FailedBlock) == nil {
		return "", false, nil
	}
	msg, err = exec(t, FailedBlock, run, input, nil, nil)
	return msg, err == nil, err
}

func exec(t *template.Template, block string, run Run, input, output, action json.RawMessage) (string, error) {
	data := map[string]any{"run": toMap(run), "input": decode(input), "output": decode(output), "action": decode(action),
		"params": run.Params}
	var buf bytes.Buffer
	var err error
	if block == "" {
		err = t.Execute(&buf, data)
	} else {
		err = t.ExecuteTemplate(&buf, block, data)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func decode(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil
	}
	return v
}

func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

var funcs = template.FuncMap{
	"clip":     clip,
	"oneline":  oneline,
	"take":     take,
	"sortBy":   sortBy,
	"limitPer": limitPer,
	"byID":     byID,
	"dict":     dict,
	"list":     func(v ...any) []any { return v },
	"get":      get,
	"default": func(def, v any) any {
		if empty(v) {
			return def
		}
		return v
	},
	"join":    join,
	"replace": func(old, repl, s string) string { return strings.ReplaceAll(s, old, repl) },
	"printf":  fmt.Sprintf,
	"short": func(s string, n int) string {
		if len(s) > n {
			return s[:n]
		}
		return s
	},
	"add":   func(a, b int) int { return a + b },
	"len":   length,
	"round": func(f float64) string { return fmt.Sprintf("%.0f", f) },
}

// oneline collapses whitespace into single spaces.
func oneline(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip shortens text to n characters on a word boundary and adds an ellipsis.
func clip(n int, v any) string {
	s := oneline(fmt.Sprint(nz(v)))
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)[:n]
	cut := string(r)
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, ",;:·–- ") + "…"
}

func nz(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func asList(v any) []any {
	switch l := v.(type) {
	case []any:
		return l
	case nil:
		return nil
	}
	return nil
}

// take returns the first n items of a list.
func take(n int, v any) []any {
	l := asList(v)
	if len(l) > n {
		return l[:n]
	}
	return l
}

// sortBy orders objects by a field, following the order of values given; unknown values go last.
// Stable: equal items keep their order.
func sortBy(field string, order []any, v any) []any {
	rank := map[string]int{}
	for i, o := range order {
		rank[fmt.Sprint(o)] = i
	}
	l := append([]any(nil), asList(v)...)
	pos := func(x any) int {
		if r, ok := rank[fmt.Sprint(get(x, field))]; ok {
			return r
		}
		return len(order)
	}
	sort.SliceStable(l, func(i, j int) bool { return pos(l[i]) < pos(l[j]) })
	return l
}

// limitPer keeps at most limits[value] items per value of a field; values without a limit are kept.
func limitPer(field string, limits map[string]any, v any) []any {
	seen := map[string]int{}
	var out []any
	for _, x := range asList(v) {
		k := fmt.Sprint(get(x, field))
		seen[k]++
		if lim, ok := limits[k]; ok {
			if n, ok := toInt(lim); ok && seen[k] > n {
				continue
			}
		}
		out = append(out, x)
	}
	return out
}

// byID indexes a list of objects by their "id" field.
func byID(v any) map[string]any {
	m := map[string]any{}
	for _, x := range asList(v) {
		if id := get(x, "id"); id != nil {
			m[fmt.Sprint(id)] = x
		}
	}
	return m
}

func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, errors.New("dict: odd number of arguments")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		m[fmt.Sprint(kv[i])] = kv[i+1]
	}
	return m, nil
}

// get reads a key of a map, nil when absent.
func get(v any, key string) any {
	switch m := v.(type) {
	case map[string]any:
		return m[key]
	}
	return nil
}

func join(sep string, v any) string {
	var parts []string
	for _, x := range asList(v) {
		parts = append(parts, fmt.Sprint(x))
	}
	return strings.Join(parts, sep)
}

func length(v any) int {
	switch x := v.(type) {
	case []any:
		return len(x)
	case map[string]any:
		return len(x)
	case string:
		return utf8.RuneCountInString(x)
	}
	return 0
}

func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	case bool:
		return !x
	}
	return false
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}
