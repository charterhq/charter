package pkg_test

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/charterhq/charter/internal/pkg"
	"github.com/charterhq/charter/schemas"
)

// TestFieldsCoverFormat: every property of the format schemas has a row in the support matrix,
// so a new field cannot reach the format without saying what the core does with it.
func TestFieldsCoverFormat(t *testing.T) {
	rows := map[string]bool{}
	for _, f := range pkg.Fields {
		path, _, _ := strings.Cut(f.Path, "=")
		rows[f.Kind+" "+path] = true
	}
	for kind, file := range map[string]string{"Agent": "agent.schema.json", "Scenario": "scenario.schema.json", "Connection": "connection.schema.json"} {
		b, err := schemas.FS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		defs, _ := doc["$defs"].(map[string]any)
		for _, path := range leaves(doc, defs, "") {
			if path == "apiVersion" || path == "kind" {
				continue
			}
			if !rows[kind+" "+path] && !covered(rows, kind, path) {
				t.Errorf("%s %s has no row in pkg.Fields", kind, path)
			}
		}
	}
}

// covered: a row for a parent object (spec.triggers) covers its fields.
func covered(rows map[string]bool, kind, path string) bool {
	for p := path; strings.Contains(p, "."); {
		p = p[:strings.LastIndex(p, ".")]
		if rows[kind+" "+p] {
			return true
		}
	}
	return false
}

// leaves lists the property paths of a JSON Schema, through local $refs and array items.
func leaves(s map[string]any, defs map[string]any, prefix string) []string {
	if ref, ok := s["$ref"].(string); ok {
		if d, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any); ok {
			return leaves(d, defs, prefix)
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		return leaves(items, defs, prefix)
	}
	props, ok := s["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return []string{prefix}
	}
	var out []string
	for name, sub := range props {
		p := name
		if prefix != "" {
			p = prefix + "." + name
		}
		m, _ := sub.(map[string]any)
		out = append(out, leaves(m, defs, p)...)
	}
	return out
}

func TestUnsupportedFields(t *testing.T) {
	const scn = "scenarios/classify/scenario.yml"
	add := func(file, after, extra string) func(fstest.MapFS) {
		return func(m fstest.MapFS) { replace(m, file, after, after+extra) }
	}
	cases := []struct {
		name string
		edit func(m fstest.MapFS)
		want string
	}{
		{"knowledge from another repo", add("agent.yml", "  memory: none\n", "  knowledge:\n    - {repo: docs, path: kb/}\n"), "agent.yml: knowledge kb/: sources in other repositories are not supported"},
		{"skills", add("agent.yml", "  memory: none\n", "  skills:\n    - {ref: tone, version: 1.0.0}\n"), "spec.skills is not supported"},
		{"persona includes", add("agent.yml", "  persona: persona.md\n", "  persona_includes: [common.md]\n"), "spec.persona_includes"},
		{"cost budget", func(m fstest.MapFS) {
			replace(m, "agent.yml", "concurrency: 5}", "concurrency: 5, max_cost_per_run: 0.5}")
		}, "spec.budgets.max_cost_per_run"},
		{"object memory without an object", func(m fstest.MapFS) { replace(m, "agent.yml", "  memory: none\n", "  memory: object\n") }, "memory: object needs spec.object"},
		{"object transitions", func(m fstest.MapFS) {
			withObject(m)
			replace(m, "agent.yml", "    schema: object/schema.json\n", "    schema: object/schema.json\n    transitions: [{from: a, to: b}]\n")
		}, "spec.object.transitions"},
		{"input_change trigger", add(scn, "    - {type: event, source: tickets}\n", "    - {type: input_change, source: other}\n"), "spec.triggers.type=input_change"},
		{"inputs_from", add(scn, "  level: 1\n", "  inputs_from:\n    - {agent: a, scenario: s, field: f}\n"), "spec.inputs_from"},
		{"checks", add(scn, "  level: 1\n", "  checks: [links-in-input]\n"), "spec.checks is not supported"},
		{"artifacts", add(scn, "  level: 1\n", "  artifacts:\n    - {system: tracker, on_close: close}\n"), "spec.artifacts"},
		{"on_failure owner", func(m fstest.MapFS) { replace(m, scn, "on_failure: requester", "on_failure: owner") }, "spec.on_failure=owner"},
		{"feedback", add(scn, "  level: 1\n", "  feedback: {unit: response}\n"), "spec.feedback"},
		{"approvers", func(m fstest.MapFS) {
			replace(m, scn, "  autonomy: advise\n", "  autonomy: act\n  action: tracker.add_comment\n  approval: {required: true, approvers: [lead]}\n")
			replace(m, "agent.yml", "  tools: []\n", "  tools:\n    - {ref: tracker}\n")
		}, "spec.approval.approvers"},
		{"approval fallback", func(m fstest.MapFS) {
			replace(m, scn, "  autonomy: advise\n", "  autonomy: act\n  action: tracker.add_comment\n  approval: {required: true, on_timeout: fallback}\n")
			replace(m, "agent.yml", "  tools: []\n", "  tools:\n    - {ref: tracker}\n")
		}, "spec.approval.on_timeout=fallback"},
		{"connection knowledge from another repo", add("connections/support.yml", "  status: shadow\n", "  knowledge: [{repo: x, path: y.md}]\n"), "connections/support.yml: knowledge y.md: sources in other repositories"},
		{"connection budgets", add("connections/support.yml", "  status: shadow\n", "  budgets: {max_runs_per_day: 10}\n"), "connections/support.yml: spec.budgets"},
		{"shadow card with active connection", func(m fstest.MapFS) {
			replace(m, "connections/support.yml", "  status: shadow\n", "  status: active\n")
		}, "metadata.status is shadow but connection support is active"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := example(t)
			c.edit(m)
			_, problems := pkg.Load(m)
			if !strings.Contains(problems.Error(), c.want) {
				t.Fatalf("want problem containing %q, got:\n%v", c.want, problems)
			}
		})
	}
}

func TestUnenforcedFieldsWarn(t *testing.T) {
	p, problems := pkg.Load(example(t))
	if problems != nil {
		t.Fatal(problems)
	}
	got := p.Warnings.Error()
	for _, want := range []string{"spec.data_classes", "spec.budgets.timeout", "spec.budgets.max_runs_per_day", "spec.idempotency.key"} {
		if !strings.Contains(got, want) {
			t.Errorf("no warning for %s in:\n%s", want, got)
		}
	}
}
