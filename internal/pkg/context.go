package pkg

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/charterhq/charter/internal/run"
	"github.com/charterhq/charter/internal/schema"
)

// MaxKnowledgeBytes limits the knowledge files an agent carries into the model context, card and
// connections together. Larger reference data belongs to a tool the agent searches.
const MaxKnowledgeBytes = 256 << 10

// loadKnowledge reads the knowledge files the card and the connections name. Files live in the package
// under knowledge/; sources in other repositories are not read by the core (it does not read Git).
func loadKnowledge(fsys fs.FS, p *Package, ps *Problems) {
	p.Knowledge = map[string]string{}
	total := 0
	read := func(file string, k Knowledge) {
		if k.Repo != "" {
			ps.add(file, "knowledge %s: sources in other repositories are not supported by this core: put the file into knowledge/ or give the agent a tool that searches it", k.Path)
			return
		}
		clean := path.Clean(k.Path)
		if !strings.HasPrefix(clean, "knowledge/") {
			ps.add(file, "knowledge %s: a knowledge file lives under knowledge/", k.Path)
			return
		}
		if _, done := p.Knowledge[clean]; done {
			return
		}
		b, err := fs.ReadFile(fsys, clean)
		if err != nil {
			ps.add(file, "knowledge %s is missing", k.Path)
			return
		}
		if !utf8.Valid(b) {
			ps.add(clean, "knowledge file is not UTF-8 text")
			return
		}
		total += len(b)
		p.Knowledge[clean] = string(b)
	}
	for _, k := range p.Agent.Spec.Knowledge {
		read("agent.yml", k)
	}
	for _, name := range sortedKeys(p.Connections) {
		c := p.Connections[name]
		for _, k := range c.Spec.Knowledge {
			read(c.File, k)
		}
	}
	if total > MaxKnowledgeBytes {
		ps.add("knowledge", "knowledge files take %d KiB, more than %d KiB: give the agent a tool that searches them", total>>10, MaxKnowledgeBytes>>10)
	}
	if entries, err := fs.ReadDir(fsys, "knowledge"); err == nil {
		for _, e := range entries {
			if f := "knowledge/" + e.Name(); !e.IsDir() && p.Knowledge[f] == "" && !named(p, f) {
				ps.add(f, "knowledge file is not named in agent.yml or a connection")
			}
		}
	}
}

func named(p *Package, file string) bool {
	for _, k := range p.Agent.Spec.Knowledge {
		if path.Clean(k.Path) == file {
			return true
		}
	}
	for _, c := range p.Connections {
		for _, k := range c.Spec.Knowledge {
			if path.Clean(k.Path) == file {
				return true
			}
		}
	}
	return false
}

// checkParams: parameter schemas compile and their defaults fit them; connection values fit the schemas
// of the scenarios the connection enables, name a declared parameter and cover parameters without defaults.
func checkParams(p *Package, ps *Problems) {
	for _, name := range sortedKeys(p.Scenarios) {
		s := p.Scenarios[name]
		file := "scenarios/" + name + "/scenario.yml"
		for _, pn := range sortedKeys(s.Spec.Params) {
			raw := s.Spec.Params[pn]
			if _, err := schema.Compile(raw); err != nil {
				ps.add(file, "params.%s is not a valid JSON Schema: %v", pn, err)
				continue
			}
			if def, ok := paramDefault(raw); ok {
				for _, msg := range schema.Check(raw, def) {
					ps.add(file, "params.%s: default does not match its schema: %s", pn, msg)
				}
			}
		}
	}
	for _, cname := range sortedKeys(p.Connections) {
		c := p.Connections[cname]
		declared := map[string]bool{}
		for _, sn := range c.Spec.Scenarios {
			s := p.Scenarios[sn]
			if s == nil {
				continue
			}
			for pn, raw := range s.Spec.Params {
				declared[pn] = true
				v, set := c.Spec.Params[pn]
				_, hasDefault := paramDefault(raw)
				switch {
				case !set && !hasDefault:
					ps.add(c.File, "params.%s is required by scenario %s and has no default", pn, sn)
				case set:
					b, _ := json.Marshal(v)
					for _, msg := range schema.Check(raw, b) {
						ps.add(c.File, "params.%s does not match the schema of scenario %s: %s", pn, sn, msg)
					}
				}
			}
		}
		for _, pn := range sortedKeys(c.Spec.Params) {
			if !declared[pn] {
				ps.add(c.File, "params.%s is not a parameter of the scenarios this connection enables", pn)
			}
		}
	}
}

// paramDefault returns the default of a parameter schema.
func paramDefault(raw json.RawMessage) (json.RawMessage, bool) {
	var s struct {
		Default json.RawMessage `json:"default"`
	}
	if json.Unmarshal(raw, &s) != nil || len(s.Default) == 0 {
		return nil, false
	}
	return s.Default, true
}

// ParamDefaults are the defaults of a scenario's parameters.
func ParamDefaults(params map[string]json.RawMessage) map[string]any {
	out := map[string]any{}
	for pn, raw := range params {
		if def, ok := paramDefault(raw); ok {
			var v any
			if json.Unmarshal(def, &v) == nil {
				out[pn] = v
			}
		}
	}
	return out
}

func knowledgeDocs(p *Package, ks []Knowledge) []run.Doc {
	var out []run.Doc
	seen := map[string]bool{}
	for _, k := range ks {
		f := path.Clean(k.Path)
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, run.Doc{Path: f, Text: p.Knowledge[f]})
	}
	return out
}
