package pkg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	yaml "go.yaml.in/yaml/v3"

	"github.com/charterhq/charter/internal/render"
	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/schema"
	"github.com/charterhq/charter/schemas"
)

var (
	docSchemasOnce sync.Once
	docSchemas     map[string]*jsonschema.Schema
	docSchemasErr  error
)

// formatSchemas compiles the embedded schemas of agent, scenario and connection files.
func formatSchemas() (map[string]*jsonschema.Schema, error) {
	docSchemasOnce.Do(func() {
		c := jsonschema.NewCompiler()
		names := map[string]string{
			"Agent":      "agent.schema.json",
			"Scenario":   "scenario.schema.json",
			"Connection": "connection.schema.json",
		}
		docSchemas = map[string]*jsonschema.Schema{}
		for kind, file := range names {
			b, err := schemas.FS.ReadFile(file)
			if err != nil {
				docSchemasErr = err
				return
			}
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
			if err != nil {
				docSchemasErr = fmt.Errorf("%s: %w", file, err)
				return
			}
			url := "https://charter.dev/schemas/v1/" + file
			if err := c.AddResource(url, doc); err != nil {
				docSchemasErr = err
				return
			}
			s, err := c.Compile(url)
			if err != nil {
				docSchemasErr = fmt.Errorf("%s: %w", file, err)
				return
			}
			docSchemas[kind] = s
		}
	})
	return docSchemas, docSchemasErr
}

// Load reads a package directory, validates it and returns it with all problems found.
// A non-empty Problems means the package must not be applied. Fields the core does not execute are
// problems, required fields it does not enforce are p.Warnings (see Fields).
func Load(fsys fs.FS) (*Package, Problems) {
	var ps Problems
	p := &Package{Scenarios: map[string]*Scenario{}, Connections: map[string]*Connection{}}

	if !readDoc(fsys, "agent.yml", "Agent", &p.Agent, &ps) {
		return nil, ps
	}
	a := p.Agent.Spec

	if b, err := fs.ReadFile(fsys, a.Persona); err != nil {
		ps.add(a.Persona, "persona file is missing")
	} else if strings.TrimSpace(string(b)) == "" {
		ps.add(a.Persona, "persona file is empty")
	} else {
		p.Persona = string(b)
	}

	for _, id := range a.Identities {
		if toolNames[strings.ToLower(id.System)] {
			ps.add("agent.yml", "identities: system %q is a tool name; name the logical system (chat, tracker), the installation maps it to a driver", id.System)
		}
	}

	agentTools := map[string][]string{}
	for _, t := range a.Tools {
		agentTools[t.Ref] = t.Functions
	}

	listed := map[string]bool{}
	for _, ref := range a.Scenarios {
		listed[ref.Name] = true
		if ref.From != "" {
			ps.add("agent.yml", "scenario %s: templates (from) are not supported yet", ref.Name)
			continue
		}
		s := loadScenario(fsys, ref.Name, agentTools, &ps)
		if s != nil {
			p.Scenarios[ref.Name] = s
		}
	}
	if entries, err := fs.ReadDir(fsys, "scenarios"); err == nil {
		for _, e := range entries {
			if e.IsDir() && !listed[e.Name()] {
				ps.add("scenarios/"+e.Name(), "scenario directory is not listed in agent.yml spec.scenarios")
			}
		}
	}

	if entries, err := fs.ReadDir(fsys, "connections"); err == nil {
		for _, e := range entries {
			if e.IsDir() || path.Ext(e.Name()) != ".yml" {
				continue
			}
			file := "connections/" + e.Name()
			var c Connection
			if !readDoc(fsys, file, "Connection", &c, &ps) {
				continue
			}
			if c.Spec.Agent.Name != p.Agent.Metadata.Name {
				ps.add(file, "connection is for agent %q, package is %q", c.Spec.Agent.Name, p.Agent.Metadata.Name)
			}
			for _, sc := range c.Spec.Scenarios {
				if !listed[sc] {
					ps.add(file, "scenario %q is not in the agent", sc)
				}
			}
			checkConnectionTriggers(file, &c, p.Scenarios, &ps)
			if _, dup := p.Connections[c.Metadata.Name]; dup {
				ps.add(file, "duplicate connection name %q", c.Metadata.Name)
			}
			c.File = file
			p.Connections[c.Metadata.Name] = &c
		}
	}

	checkSupport(p, &ps, &p.Warnings)

	sort.SliceStable(ps, func(i, j int) bool { return ps[i].File < ps[j].File })
	if len(ps) > 0 {
		return p, ps
	}
	return p, nil
}

func loadScenario(fsys fs.FS, name string, agentTools map[string][]string, ps *Problems) *Scenario {
	dir := "scenarios/" + name
	file := dir + "/scenario.yml"
	var s Scenario
	if !readDoc(fsys, file, "Scenario", &s, ps) {
		return nil
	}
	if s.Metadata.Name != name {
		ps.add(file, "metadata.name %q does not match the directory %q", s.Metadata.Name, name)
	}
	sp := s.Spec

	if b, err := fs.ReadFile(fsys, path.Join(dir, sp.Instructions)); err != nil {
		ps.add(path.Join(dir, sp.Instructions), "instructions file is missing")
	} else if strings.TrimSpace(string(b)) == "" {
		ps.add(path.Join(dir, sp.Instructions), "instructions file is empty")
	} else {
		s.Instructions = string(b)
	}
	s.InputSchema = readSchema(fsys, path.Join(dir, sp.InputSchema), ps)
	s.OutputSchema = readSchema(fsys, path.Join(dir, sp.OutputSchema), ps)
	if sp.Response != nil {
		tf := path.Join(dir, sp.Response.Template)
		if b, err := fs.ReadFile(fsys, tf); err != nil {
			ps.add(tf, "response template is missing")
		} else if _, err := render.Parse(tf, string(b)); err != nil {
			ps.add(tf, "response template does not parse: %v", err)
		} else {
			s.Template = string(b)
		}
	}

	for _, t := range sp.Triggers {
		if toolNames[strings.ToLower(t.Source)] {
			ps.add(file, "trigger source %q is a tool name; name the logical source (alerts, tickets), the connection binds it", t.Source)
		}
		switch {
		case t.Type == "schedule" && t.Schedule == "":
			ps.add(file, "trigger %s: type schedule needs a schedule (cron)", t.Source)
		case t.Type != "schedule" && t.Schedule != "":
			ps.add(file, "trigger %s: schedule is only meaningful for type schedule", t.Source)
		case t.Schedule != "":
			if _, err := run.ParseSchedule(t.Schedule, time.UTC); err != nil {
				ps.add(file, "trigger %s: schedule %q: %v", t.Source, t.Schedule, err)
			}
		}
	}

	if sp.Level == 1 && len(sp.Tools) > 0 {
		ps.add(file, "level 1 is a single model call over prepared input and cannot use tools")
	}
	for _, t := range sp.Tools {
		ref, fn, _ := strings.Cut(t, ".")
		fns, ok := agentTools[ref]
		if !ok {
			ps.add(file, "tool %q is not declared in agent.yml spec.tools", t)
			continue
		}
		if fn != "" && len(fns) > 0 && !slices.Contains(fns, fn) {
			ps.add(file, "function %q of tool %q is not allowed in agent.yml", fn, ref)
		}
	}
	switch {
	case sp.Autonomy == "act" && sp.Action == "":
		ps.add(file, "autonomy act needs spec.action: the tool function that performs the action")
	case sp.Autonomy != "act" && sp.Action != "":
		ps.add(file, "spec.action is only meaningful for autonomy act")
	case sp.Action != "":
		ref, fn, _ := strings.Cut(sp.Action, ".")
		if fns, ok := agentTools[ref]; !ok {
			ps.add(file, "action %q: tool %q is not declared in agent.yml spec.tools", sp.Action, ref)
		} else if len(fns) > 0 && !slices.Contains(fns, fn) {
			ps.add(file, "action %q: function %q of tool %q is not allowed in agent.yml", sp.Action, fn, ref)
		}
	}
	if sp.ActionInput != "" {
		if sp.Action == "" {
			ps.add(file, "action_input is only meaningful with spec.action")
		} else if !schemaHasProperty(s.OutputSchema, sp.ActionInput) {
			ps.add(file, "action_input %q is not a property of the output schema", sp.ActionInput)
		}
	}
	if sp.Approval != nil && sp.Approval.Required && sp.Autonomy != "act" {
		ps.add(file, "approval is only meaningful for autonomy act")
	}

	s.Cases = loadCases(fsys, path.Join(dir, sp.Eval), s.InputSchema, ps)
	if len(s.Cases) == 0 {
		ps.add(path.Join(dir, sp.Eval), "evaluation set has no cases (cases/*.yml)")
	}
	s.EvalCases = len(s.Cases)
	return &s
}

// checkConnectionTriggers: every trigger of a connection binds a source of a scenario the connection
// enables, receiver goes with event sources, schedule and input with schedule sources.
func checkConnectionTriggers(file string, c *Connection, scenarios map[string]*Scenario, ps *Problems) {
	kinds := map[string]map[string]bool{}      // source -> trigger types
	byType := map[string]map[string][]string{} // source -> type -> scenarios
	for _, sn := range c.Spec.Scenarios {
		s := scenarios[sn]
		if s == nil {
			continue
		}
		for _, t := range s.Spec.Triggers {
			if kinds[t.Source] == nil {
				kinds[t.Source] = map[string]bool{}
				byType[t.Source] = map[string][]string{}
			}
			kinds[t.Source][t.Type] = true
			byType[t.Source][t.Type] = append(byType[t.Source][t.Type], sn)
		}
	}
	srcs := make([]string, 0, len(c.Spec.Triggers))
	for src := range c.Spec.Triggers {
		srcs = append(srcs, src)
	}
	sort.Strings(srcs)
	for _, src := range srcs {
		t := c.Spec.Triggers[src]
		k := kinds[src]
		if k == nil {
			ps.add(file, "trigger %q is not a source of the scenarios this connection enables", src)
			continue
		}
		if t.Receiver != "" && !k["event"] {
			ps.add(file, "trigger %q: receiver is only meaningful for event sources", src)
		}
		if (t.Schedule != "" || t.Input != nil) && !k["schedule"] {
			ps.add(file, "trigger %q: schedule and input are only meaningful for schedule sources", src)
		}
		if t.Schedule != "" {
			if _, err := run.ParseSchedule(t.Schedule, time.UTC); err != nil {
				ps.add(file, "trigger %q: schedule %q: %v", src, t.Schedule, err)
			}
		}
		if (len(t.Users) > 0 || len(t.Channels) > 0) && !k["message"] {
			ps.add(file, "trigger %q: users and channels are only meaningful for message sources", src)
		}
		if k["message"] && len(byType[src]["message"]) > 1 {
			ps.add(file, "trigger %q: a message source must start one scenario, it starts %v", src, byType[src]["message"])
		}
	}
}

// schemaHasProperty reports whether an object schema declares the property at the top level.
func schemaHasProperty(schema json.RawMessage, name string) bool {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &s) != nil {
		return false
	}
	_, ok := s.Properties[name]
	return ok
}

// readDoc parses a YAML document, validates it against the format schema and decodes it into out.
func readDoc(fsys fs.FS, file, kind string, out any, ps *Problems) bool {
	b, err := fs.ReadFile(fsys, file)
	if err != nil {
		ps.add(file, "file is missing")
		return false
	}
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		ps.add(file, "not valid YAML: %v", err)
		return false
	}
	j, err := json.Marshal(raw)
	if err != nil {
		ps.add(file, "cannot convert to JSON: %v", err)
		return false
	}
	sch, err := formatSchemas()
	if err != nil {
		ps.add(file, "format schemas: %v", err)
		return false
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		ps.add(file, "%v", err)
		return false
	}
	if err := sch[kind].Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			for _, line := range schema.Leaves(ve) {
				ps.add(file, "%s", line)
			}
		} else {
			ps.add(file, "%v", err)
		}
		return false
	}
	if err := json.Unmarshal(j, out); err != nil {
		ps.add(file, "decode: %v", err)
		return false
	}
	return true
}

// readSchema reads a JSON Schema file of a scenario and checks that it compiles.
func readSchema(fsys fs.FS, file string, ps *Problems) json.RawMessage {
	b, err := fs.ReadFile(fsys, file)
	if err != nil {
		ps.add(file, "schema file is missing")
		return nil
	}
	if _, err := schema.Compile(b); err != nil {
		ps.add(file, "not a valid JSON Schema: %v", err)
		return nil
	}
	return json.RawMessage(b)
}

// toolNames are tools a package must not name (STD-0003, F1): it says what, the installation says with what.
var toolNames = map[string]bool{
	"mattermost": true, "slack": true, "telegram": true, "teams": true, "discord": true,
	"jira": true, "youtrack": true, "gitlab": true, "github": true, "confluence": true,
	"alertmanager": true, "prometheus": true, "grafana": true, "sentry": true,
	"kestra": true, "airflow": true, "n8n": true, "bifrost": true, "librefang": true,
}
