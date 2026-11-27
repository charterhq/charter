package pkg_test

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/charterhq/charter/internal/pkg"
)

const objectSchema = `{"type":"object","required":["work"],"properties":{
  "work":{"type":"string","title":"Work","description":"What job does the agent do?"},
  "cases":{"type":"array","minItems":5,"x-charter-collection":true,"items":{"type":"object","properties":{"id":{"type":"string"},"came":{"type":"string"}}}}}}`

// withObject turns the example into an agent with memory: object.
func withObject(m fstest.MapFS) {
	replace(m, "agent.yml", "  memory: none\n", `  memory: object
  object:
    schema: object/schema.json
    opener: object/opener.md.tmpl
    stages:
      - {id: request, title: Request, artifacts: [{kind: brief, title: Brief}]}
      - {id: review, title: Review}
`)
	m["object/schema.json"] = &fstest.MapFile{Data: []byte(objectSchema)}
	m["object/opener.md.tmpl"] = &fstest.MapFile{Data: []byte("{{.Filled}} of {{.Total}}{{range .Missing}}, {{.Title}}{{end}}")}
}

func TestObjectPackage(t *testing.T) {
	m := example(t)
	withObject(m)
	p, problems := pkg.Load(m)
	if problems != nil {
		t.Fatalf("problems:\n%v", problems)
	}
	b, err := pkg.Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	var st pkg.Stored
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	o := st.Object
	if o == nil || !strings.Contains(o.Schema, `"x-charter-collection":true`) || !strings.HasPrefix(o.Opener, "{{.Filled}}") ||
		len(o.Stages) != 2 || o.Stages[0].Artifacts[0].Title != "Brief" || st.Scenarios["classify"].Memory != "object" {
		t.Fatalf("stored object: %+v", o)
	}
}

func TestObjectProblems(t *testing.T) {
	cases := []struct {
		name string
		edit func(m fstest.MapFS)
		want string
	}{
		{"object without memory object", func(m fstest.MapFS) {
			withObject(m)
			replace(m, "agent.yml", "  memory: object\n", "  memory: thread\n")
		}, "spec.object is only meaningful with memory: object"},
		{"missing schema", func(m fstest.MapFS) { withObject(m); delete(m, "object/schema.json") }, "object/schema.json: object schema file is missing"},
		{"schema not an object", func(m fstest.MapFS) {
			withObject(m)
			m["object/schema.json"] = &fstest.MapFile{Data: []byte(`{"type":"array"}`)}
		}, "must be of type object"},
		{"collection without id", func(m fstest.MapFS) {
			withObject(m)
			m["object/schema.json"] = &fstest.MapFile{Data: []byte(strings.Replace(objectSchema, `"id":{"type":"string"},`, "", 1))}
		}, "collection items need an id"},
		{"broken opener", func(m fstest.MapFS) {
			withObject(m)
			m["object/opener.md.tmpl"] = &fstest.MapFile{Data: []byte("{{.Filled")}
		}, "opener template does not parse"},
		{"duplicate stage", func(m fstest.MapFS) {
			withObject(m)
			replace(m, "agent.yml", "{id: review, title: Review}", "{id: request, title: Again}")
		}, "duplicate stage"},
		{"message receiver other than web", func(m fstest.MapFS) {
			replace(m, "scenarios/classify/scenario.yml", "    - {type: event, source: tickets}\n", "    - {type: message, source: tickets}\n")
			replace(m, "connections/support.yml", "tickets: {receiver: raw}", "tickets: {receiver: chat}")
		}, "a message source takes receiver web"},
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

// A message source the core API serves passes validation.
func TestWebMessageSource(t *testing.T) {
	m := example(t)
	replace(m, "scenarios/classify/scenario.yml", "    - {type: event, source: tickets}\n", "    - {type: message, source: tickets}\n")
	replace(m, "connections/support.yml", "tickets: {receiver: raw}", "tickets: {receiver: web}")
	if _, problems := pkg.Load(m); problems != nil {
		t.Fatalf("problems:\n%v", problems)
	}
}
