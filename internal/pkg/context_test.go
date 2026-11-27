package pkg_test

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/charterhq/charter/internal/pkg"
)

// withContext adds a parameter, a knowledge file of the agent and one of the connection to the example.
func withContext(t *testing.T) fstest.MapFS {
	t.Helper()
	m := example(t)
	replace(m, "scenarios/classify/scenario.yml", "  level: 1\n",
		"  level: 1\n  params:\n    queues: {type: array, items: {type: string}, default: [l1, l2]}\n    team: {type: string}\n")
	replace(m, "agent.yml", "  memory: none\n", "  memory: none\n  knowledge:\n    - {path: knowledge/categories.md}\n")
	replace(m, "connections/support.yml", "  status: shadow\n",
		"  status: shadow\n  params: {team: support}\n  knowledge:\n    - {path: knowledge/support-people.yml}\n")
	m["knowledge/categories.md"] = &fstest.MapFile{Data: []byte("billing: money questions")}
	m["knowledge/support-people.yml"] = &fstest.MapFile{Data: []byte("lead: jane.doe")}
	return m
}

func TestParamsAndKnowledgeCompile(t *testing.T) {
	p, problems := pkg.Load(withContext(t))
	if problems != nil {
		t.Fatal(problems)
	}
	raw, err := pkg.Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	var st pkg.Stored
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	s, c := st.Scenarios["classify"], st.Connections["support"]
	if q, _ := s.Params["queues"].([]any); len(q) != 2 || s.Params["team"] != nil {
		t.Fatalf("scenario param defaults: %v", s.Params)
	}
	if len(s.Knowledge) != 1 || s.Knowledge[0].Path != "knowledge/categories.md" || !strings.Contains(s.Knowledge[0].Text, "billing") {
		t.Fatalf("agent knowledge: %+v", s.Knowledge)
	}
	if c.Params["team"] != "support" || len(c.Knowledge) != 1 || c.Knowledge[0].Text != "lead: jane.doe" {
		t.Fatalf("connection: %+v", c)
	}
}

func TestParamsAndKnowledgeProblems(t *testing.T) {
	cases := []struct {
		name string
		edit func(m fstest.MapFS)
		want string
	}{
		{"required param missing", func(m fstest.MapFS) { replace(m, "connections/support.yml", "  params: {team: support}\n", "") },
			"params.team is required by scenario classify"},
		{"param value against schema", func(m fstest.MapFS) {
			replace(m, "connections/support.yml", "params: {team: support}", "params: {team: support, queues: l1}")
		}, "params.queues does not match the schema"},
		{"unknown param", func(m fstest.MapFS) {
			replace(m, "connections/support.yml", "params: {team: support}", "params: {team: support, project: SUP}")
		}, "params.project is not a parameter"},
		{"default against schema", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "default: [l1, l2]", "default: l1")
		}, "params.queues: default does not match"},
		{"bad param schema", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "team: {type: string}", "team: {type: 5}")
		}, "params.team is not a valid JSON Schema"},
		{"missing knowledge file", func(m fstest.MapFS) { delete(m, "knowledge/categories.md") }, "knowledge knowledge/categories.md is missing"},
		{"knowledge outside knowledge/", func(m fstest.MapFS) {
			replace(m, "agent.yml", "path: knowledge/categories.md", "path: persona.md")
			delete(m, "knowledge/categories.md")
		}, "a knowledge file lives under knowledge/"},
		{"unnamed knowledge file", func(m fstest.MapFS) { m["knowledge/extra.md"] = &fstest.MapFile{Data: []byte("x")} },
			"knowledge/extra.md: knowledge file is not named"},
		{"knowledge too large", func(m fstest.MapFS) {
			m["knowledge/categories.md"] = &fstest.MapFile{Data: []byte(strings.Repeat("x", pkg.MaxKnowledgeBytes+1))}
		}, "knowledge files take"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := withContext(t)
			c.edit(m)
			_, problems := pkg.Load(m)
			if !strings.Contains(problems.Error(), c.want) {
				t.Fatalf("want %q, got:\n%v", c.want, problems)
			}
		})
	}
}
