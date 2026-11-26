package pkg_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/charterhq/charter/internal/pkg"
)

func example(t *testing.T) fstest.MapFS {
	t.Helper()
	root := "../../examples/ticket-triage"
	m := fstest.MapFS{}
	err := fsWalk(root, func(rel string, b []byte) { m[rel] = &fstest.MapFile{Data: b} })
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func fsWalk(root string, fn func(rel string, b []byte)) error {
	return walk(root, "", fn)
}

func walk(root, rel string, fn func(string, []byte)) error {
	entries, err := os.ReadDir(root + "/" + rel)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := e.Name()
		if rel != "" {
			p = rel + "/" + e.Name()
		}
		if e.IsDir() {
			if err := walk(root, p, fn); err != nil {
				return err
			}
			continue
		}
		b, err := os.ReadFile(root + "/" + p)
		if err != nil {
			return err
		}
		fn(p, b)
	}
	return nil
}

func TestExampleIsValid(t *testing.T) {
	p, problems := pkg.Load(example(t))
	if problems != nil {
		t.Fatalf("example has problems:\n%v", problems)
	}
	if p.Agent.Metadata.Name != "ticket-triage" || len(p.Scenarios) != 1 || len(p.Connections) != 1 {
		t.Fatalf("unexpected package: %+v", p.Agent.Metadata)
	}
	raw, err := pkg.Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	var st pkg.Stored
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	c := st.Connections["support"]
	if c.Status != "shadow" || c.Channels["shadow"] != "support-triage-shadow" || c.Triggers["tickets"].Receiver != "raw" {
		t.Fatalf("compiled connection: %+v", c)
	}
	s := st.Scenarios["classify"]
	if s.ModelClass != "fast" || s.Level != 1 || len(s.Triggers) != 1 || len(s.OutputSchema) == 0 || !strings.Contains(s.Instructions, "support triage") {
		t.Fatalf("compiled scenario: %+v", s)
	}
}

func TestProblems(t *testing.T) {
	cases := []struct {
		name string
		edit func(m fstest.MapFS)
		want string
	}{
		{"missing card", func(m fstest.MapFS) { delete(m, "agent.yml") }, "agent.yml: file is missing"},
		{"bad api version", func(m fstest.MapFS) { replace(m, "agent.yml", "charter.dev/v1", "charter.dev/v0") }, "/apiVersion"},
		{"missing duties", func(m fstest.MapFS) {
			replace(m, "agent.yml", "  duties:\n    - reads one ticket and returns a category, a queue and a short reason\n", "")
		}, "duties"},
		{"tools on level 1", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "  level: 1\n", "  level: 1\n  tools: [jira]\n")
		}, "level 1"},
		{"undeclared tool", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "  level: 1\n", "  level: 3\n  tools: [jira]\n")
		}, `tool "jira" is not declared`},
		{"unlisted scenario dir", func(m fstest.MapFS) {
			m["scenarios/extra/scenario.yml"] = &fstest.MapFile{Data: []byte("x")}
		}, "not listed"},
		{"broken output schema", func(m fstest.MapFS) {
			m["scenarios/classify/output.schema.json"] = &fstest.MapFile{Data: []byte(`{"type": 5}`)}
		}, "not a valid JSON Schema"},
		{"no eval cases", func(m fstest.MapFS) { delete(m, "scenarios/classify/eval/cases/refund.yml") }, "no cases"},
		{"connection for other agent", func(m fstest.MapFS) {
			replace(m, "connections/support.yml", "name: ticket-triage", "name: other")
		}, "connection is for agent"},
		{"missing response template", func(m fstest.MapFS) {
			delete(m, "scenarios/classify/response.md.tmpl")
		}, "response template is missing"},
		{"broken response template", func(m fstest.MapFS) {
			m["scenarios/classify/response.md.tmpl"] = &fstest.MapFile{Data: []byte("{{ .output.x | nosuch }}")}
		}, "response template does not parse"},
		{"approval on advise", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "  autonomy: advise\n", "  autonomy: advise\n  approval: {required: true}\n")
		}, "approval is only meaningful"},
		{"act without action", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "  autonomy: advise\n", "  autonomy: act\n")
		}, "autonomy act needs spec.action"},
		{"action on advise", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "  autonomy: advise\n", "  autonomy: advise\n  action: tracker.add_comment\n")
		}, "only meaningful for autonomy act"},
		{"schedule trigger without cron", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "    - {type: event, source: tickets}\n",
				"    - {type: event, source: tickets}\n    - {type: schedule, source: digest}\n")
		}, "type schedule needs a schedule"},
		{"bad cron", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "    - {type: event, source: tickets}\n",
				"    - {type: event, source: tickets}\n    - {type: schedule, source: digest, schedule: \"61 * * * *\"}\n")
		}, `schedule "61 * * * *"`},
		{"connection binds unknown source", func(m fstest.MapFS) {
			replace(m, "connections/support.yml", "    tickets: {receiver: raw}\n", "    tickets: {receiver: raw}\n    mail: {}\n")
		}, `trigger "mail" is not a source`},
		{"schedule on event source", func(m fstest.MapFS) {
			replace(m, "connections/support.yml", "tickets: {receiver: raw}", "tickets: {schedule: \"0 9 * * *\"}")
		}, "only meaningful for schedule sources"},
		{"unknown receiver", func(m fstest.MapFS) {
			replace(m, "connections/support.yml", "receiver: raw", "receiver: jira")
		}, "receiver"},
		{"users on event source", func(m fstest.MapFS) {
			replace(m, "connections/support.yml", "receiver: raw", "receiver: raw, users: [jane]")
		}, "users is only meaningful for message sources"},
		{"action with undeclared tool", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "  autonomy: advise\n", "  autonomy: act\n  action: wiki.edit\n")
		}, "is not declared in agent.yml"},
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

func replace(m fstest.MapFS, file, old, new string) {
	s := string(m[file].Data)
	if !strings.Contains(s, old) {
		panic(file + ": no " + old)
	}
	m[file] = &fstest.MapFile{Data: []byte(strings.Replace(s, old, new, 1))}
}
