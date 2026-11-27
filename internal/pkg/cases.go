package pkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/charterhq/charter/internal/schema"
)

// Case is one case of a scenario evaluation set: eval/cases/<id>.yml.
//
//	id: refund
//	input: {...}                     the run input, or
//	input_file: packages/refund.json a JSON file next to cases/, relative to the eval directory
//	params: {queues: [l1]}           scenario parameters for this case, over the defaults or the connection
//	expected: {category: billing}    output fields that must be equal; objects match as subsets
//	checks:
//	  - every finding cites evidence from the input    a criterion for the judge model
//	  - {path: queue, equals: billing}                 code checks on the output
//	  - {path: reason, matches: "(?i)refund"}
//	  - {path: labels, contains: urgent}
//	  - {path: draft, exists: false}
//	  - {action: false}                                whether the run would perform the action
type Case struct {
	ID        string          `json:"id"`
	Input     json.RawMessage `json:"input,omitempty"`
	InputFile string          `json:"input_file,omitempty"`
	Params    map[string]any  `json:"params,omitempty"`
	Expected  map[string]any  `json:"expected,omitempty"`
	Checks    []Check         `json:"checks,omitempty"`

	File string `json:"-"` // path in the package
}

// Check is one check of a case: a criterion for the judge model, or one code assertion on the output.
type Check struct {
	Judge    string `json:"judge,omitempty"`
	Path     string `json:"path,omitempty"`
	Equals   any    `json:"equals,omitempty"`
	Matches  string `json:"matches,omitempty"`
	Contains any    `json:"contains,omitempty"`
	Exists   *bool  `json:"exists,omitempty"`
	Action   *bool  `json:"action,omitempty"`

	hasEquals bool
}

// UnmarshalJSON reads a plain string as a judge criterion.
func (c *Check) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*c = Check{Judge: s}
		return nil
	}
	type plain Check
	var p plain
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return err
	}
	*c = Check(p)
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(b, &keys)
	_, c.hasEquals = keys["equals"]
	return nil
}

// MarshalJSON writes a judge criterion back as a plain string.
func (c Check) MarshalJSON() ([]byte, error) {
	if c.Judge != "" {
		return json.Marshal(c.Judge)
	}
	type plain Check
	return json.Marshal(plain(c))
}

// String is a short label of the check for reports.
func (c Check) String() string {
	switch {
	case c.Judge != "":
		return c.Judge
	case c.Action != nil:
		return fmt.Sprintf("action %v", *c.Action)
	case c.hasEquals:
		return fmt.Sprintf("%s equals %s", c.Path, compact(c.Equals))
	case c.Matches != "":
		return fmt.Sprintf("%s matches %s", c.Path, c.Matches)
	case c.Contains != nil:
		return fmt.Sprintf("%s contains %s", c.Path, compact(c.Contains))
	case c.Exists != nil:
		return fmt.Sprintf("%s exists %v", c.Path, *c.Exists)
	}
	return "?"
}

func (c Check) validate() error {
	n := 0
	for _, set := range []bool{c.Judge != "", c.hasEquals, c.Matches != "", c.Contains != nil, c.Exists != nil, c.Action != nil} {
		if set {
			n++
		}
	}
	switch {
	case n != 1:
		return fmt.Errorf("a check is a judge criterion or exactly one of equals, matches, contains, exists, action")
	case c.Judge == "" && c.Action == nil && c.Path == "":
		return fmt.Errorf("check %s needs a path", c)
	case c.Action != nil && c.Path != "":
		return fmt.Errorf("check action takes no path")
	case c.Matches != "":
		if _, err := regexp.Compile(c.Matches); err != nil {
			return fmt.Errorf("matches: %v", err)
		}
	}
	return nil
}

// loadCases reads the cases of a scenario and checks each input against the input schema.
func loadCases(fsys fs.FS, evalDir string, inputSchema json.RawMessage, params map[string]json.RawMessage, ps *Problems) []Case {
	files, _ := fs.Glob(fsys, path.Join(evalDir, "cases", "*.yml"))
	var out []Case
	seen := map[string]bool{}
	for _, file := range files {
		b, err := fs.ReadFile(fsys, file)
		if err != nil {
			ps.add(file, "cannot read: %v", err)
			continue
		}
		var raw any
		if err := yaml.Unmarshal(b, &raw); err != nil {
			ps.add(file, "not valid YAML: %v", err)
			continue
		}
		j, err := json.Marshal(raw)
		if err != nil {
			ps.add(file, "cannot convert to JSON: %v", err)
			continue
		}
		var c Case
		dec := json.NewDecoder(bytes.NewReader(j))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			ps.add(file, "case: %v", err)
			continue
		}
		c.File = file
		if c.ID == "" {
			ps.add(file, "case has no id")
		} else if seen[c.ID] {
			ps.add(file, "duplicate case id %q", c.ID)
		}
		seen[c.ID] = true
		switch {
		case len(c.Input) > 0 && c.InputFile != "":
			ps.add(file, "case has both input and input_file")
		case c.InputFile != "":
			f := path.Join(evalDir, c.InputFile)
			b, err := fs.ReadFile(fsys, f)
			if err != nil {
				ps.add(file, "input_file %s is missing", c.InputFile)
				continue
			}
			if !json.Valid(b) {
				ps.add(f, "not valid JSON")
				continue
			}
			c.Input = json.RawMessage(b)
		case len(c.Input) == 0:
			ps.add(file, "case has no input")
		}
		if len(c.Input) > 0 && len(inputSchema) > 0 {
			for _, p := range schema.Check(inputSchema, c.Input) {
				ps.add(file, "input does not match input_schema: %s", p)
			}
		}
		for _, pn := range sortedKeys(c.Params) {
			raw, ok := params[pn]
			if !ok {
				ps.add(file, "params.%s is not a parameter of the scenario", pn)
				continue
			}
			b, _ := json.Marshal(c.Params[pn])
			for _, msg := range schema.Check(raw, b) {
				ps.add(file, "params.%s does not match its schema: %s", pn, msg)
			}
		}
		for i, ch := range c.Checks {
			if err := ch.validate(); err != nil {
				ps.add(file, "checks[%d]: %v", i, err)
			}
		}
		out = append(out, c)
	}
	return out
}

// CheckResult is the outcome of one check.
type CheckResult struct {
	Check  string `json:"check"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

// Assert runs the code checks and the expected fields of the case on an output.
// action says whether the run would perform the scenario action. Judge criteria are left to the caller.
func (c Case) Assert(output json.RawMessage, action bool) []CheckResult {
	var out any
	_ = json.Unmarshal(output, &out)
	var res []CheckResult
	for _, k := range sortedKeys(c.Expected) {
		got, ok := lookup(out, k)
		r := CheckResult{Check: fmt.Sprintf("expected %s = %s", k, compact(c.Expected[k]))}
		r.Pass = ok && subset(normalize(c.Expected[k]), got)
		if !r.Pass {
			r.Detail = "got " + describe(got, ok)
		}
		res = append(res, r)
	}
	for _, ch := range c.Checks {
		if ch.Judge != "" {
			continue
		}
		r := CheckResult{Check: ch.String()}
		got, ok := lookup(out, ch.Path)
		switch {
		case ch.Action != nil:
			r.Pass = action == *ch.Action
			r.Detail = fmt.Sprintf("action would run: %v", action)
		case ch.hasEquals:
			r.Pass = ok && reflect.DeepEqual(normalize(ch.Equals), got)
		case ch.Matches != "":
			s, isStr := got.(string)
			if !isStr {
				s = compact(got)
			}
			r.Pass = ok && regexp.MustCompile(ch.Matches).MatchString(s)
		case ch.Contains != nil:
			r.Pass = ok && contains(got, normalize(ch.Contains))
		case ch.Exists != nil:
			r.Pass = ok == *ch.Exists
		}
		if !r.Pass && r.Detail == "" {
			r.Detail = "got " + describe(got, ok)
		}
		res = append(res, r)
	}
	return res
}

// JudgeCriteria are the checks left to the judge model.
func (c Case) JudgeCriteria() []string {
	var out []string
	for _, ch := range c.Checks {
		if ch.Judge != "" {
			out = append(out, ch.Judge)
		}
	}
	return out
}

// lookup walks a dot path; numeric segments index arrays. An empty path is the whole value.
func lookup(v any, p string) (any, bool) {
	if p == "" {
		return v, true
	}
	for _, seg := range strings.Split(p, ".") {
		switch x := v.(type) {
		case map[string]any:
			n, ok := x[seg]
			if !ok {
				return nil, false
			}
			v = n
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// subset: objects match when every expected key matches, other values must be equal.
func subset(want, got any) bool {
	wm, ok := want.(map[string]any)
	if !ok {
		return reflect.DeepEqual(want, got)
	}
	gm, ok := got.(map[string]any)
	if !ok {
		return false
	}
	for k, w := range wm {
		g, ok := gm[k]
		if !ok || !subset(w, g) {
			return false
		}
	}
	return true
}

func contains(got, want any) bool {
	switch g := got.(type) {
	case string:
		w, ok := want.(string)
		return ok && strings.Contains(g, w)
	case []any:
		for _, e := range g {
			if subset(want, e) {
				return true
			}
		}
	}
	return false
}

// normalize turns a YAML-decoded value into the shape json.Unmarshal gives (float64 numbers).
func normalize(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func compact(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

func describe(v any, ok bool) string {
	if !ok {
		return "nothing"
	}
	return compact(v)
}
