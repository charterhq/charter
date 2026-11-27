package pkg_test

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/charterhq/charter/internal/pkg"
)

func loadCase(t *testing.T, yml string) pkg.Case {
	t.Helper()
	m := example(t)
	m["scenarios/classify/eval/cases/refund.yml"] = &fstest.MapFile{Data: []byte(yml)}
	p, problems := pkg.Load(m)
	if problems != nil {
		t.Fatal(problems)
	}
	return p.Scenarios["classify"].Cases[0]
}

func TestCaseAssert(t *testing.T) {
	c := loadCase(t, `
id: a
input: {ticket: {id: "1", subject: s, body: b}}
expected: {category: billing, meta: {lang: en}}
checks:
  - {path: queue, equals: billing}
  - {path: reason, matches: "(?i)refund"}
  - {path: tags, contains: urgent}
  - {path: items.0.n, equals: 2}
  - {path: draft, exists: false}
  - {action: false}
  - judged in words
`)
	out := json.RawMessage(`{"category": "billing", "queue": "billing", "reason": "Refund please", "tags": ["urgent"],
		"items": [{"n": 2}], "meta": {"lang": "en", "extra": 1}}`)
	for _, r := range c.Assert(out, false) {
		if !r.Pass {
			t.Errorf("%s: %s", r.Check, r.Detail)
		}
	}
	if got := c.JudgeCriteria(); len(got) != 1 || got[0] != "judged in words" {
		t.Fatalf("judge criteria %v", got)
	}
	bad := json.RawMessage(`{"category": "bug", "queue": "l1", "reason": "x", "tags": [], "items": [], "draft": {}, "meta": {}}`)
	for _, r := range c.Assert(bad, true) {
		if r.Pass {
			t.Errorf("%s passed on a bad output", r.Check)
		}
	}
}

func TestCaseProblems(t *testing.T) {
	cases := []struct{ name, yml, want string }{
		{"no input", "id: a\n", "case has no input"},
		{"input against schema", "id: a\ninput: {ticket: 5}\n", "input does not match input_schema"},
		{"missing input file", "id: a\ninput_file: nope.json\n", "input_file nope.json is missing"},
		{"two assertions", "id: a\ninput: {ticket: {id: '1', subject: s, body: b}}\nchecks:\n  - {path: q, equals: 1, exists: true}\n", "exactly one"},
		{"no path", "id: a\ninput: {ticket: {id: '1', subject: s, body: b}}\nchecks:\n  - {equals: 1}\n", "needs a path"},
		{"bad regexp", "id: a\ninput: {ticket: {id: '1', subject: s, body: b}}\nchecks:\n  - {path: q, matches: '('}\n", "matches"},
		{"unknown key", "id: a\ninput: {ticket: {id: '1', subject: s, body: b}}\nexpect: {}\n", "unknown field"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := example(t)
			m["scenarios/classify/eval/cases/refund.yml"] = &fstest.MapFile{Data: []byte(c.yml)}
			_, problems := pkg.Load(m)
			if !strings.Contains(problems.Error(), c.want) {
				t.Fatalf("want %q, got:\n%v", c.want, problems)
			}
		})
	}
}
