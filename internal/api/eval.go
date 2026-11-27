package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/pkg"
	"github.com/charterhq/charter/internal/run"
)

// evalCase runs one case of a package evaluation set through the model part of the run steps.
// The package is not published: it comes in the body like for publish, nothing is recorded.
//
//	POST /v1/eval?scenario=<name>&case=<id>[&connection=<name>]   body: tar.gz of the package directory
//
// With a connection the case runs with that team's parameters and knowledge.
func (s *Server) evalCase(w http.ResponseWriter, r *http.Request) {
	who := caller(r.Context())
	if !s.Publishers[who] {
		writeErr(w, http.StatusForbidden, "caller "+who+" may not evaluate packages")
		return
	}
	scenario, caseID := r.URL.Query().Get("scenario"), r.URL.Query().Get("case")
	fsys, err := pkg.Unpack(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, problems := pkg.Load(fsys)
	if problems != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "package has problems", "problems": problems})
		return
	}
	src := p.Scenarios[scenario]
	if src == nil {
		writeErr(w, http.StatusNotFound, "no scenario "+scenario)
		return
	}
	var c *pkg.Case
	for i := range src.Cases {
		if src.Cases[i].ID == caseID {
			c = &src.Cases[i]
		}
	}
	if c == nil {
		writeErr(w, http.StatusNotFound, "no case "+caseID+" in scenario "+scenario)
		return
	}
	raw, err := pkg.Compile(p)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var st struct {
		Scenarios   map[string]run.Scenario   `json:"scenarios"`
		Connections map[string]run.Connection `json:"connections"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	scn, agent := st.Scenarios[scenario], p.Agent.Metadata.Name
	var conn *run.Connection
	if name := r.URL.Query().Get("connection"); name != "" {
		c, ok := st.Connections[name]
		if !ok {
			writeErr(w, http.StatusNotFound, "no connection "+name)
			return
		}
		conn = &c
	}

	started := time.Now()
	res, err := s.Engine.Evaluate(r.Context(), agent, scn, conn, c.Params, c.Input)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "model step: "+err.Error())
		return
	}
	out := struct {
		Agent      string            `json:"agent"`
		Version    string            `json:"version"`
		Scenario   string            `json:"scenario"`
		Case       string            `json:"case"`
		Pass       bool              `json:"pass"`
		Checks     []pkg.CheckResult `json:"checks"`
		JudgeUsage *driver.Usage     `json:"judge_usage,omitempty"`
		Seconds    float64           `json:"seconds"`
		run.EvalResult
	}{Agent: agent, Version: p.Agent.Metadata.Version, Scenario: scenario, Case: caseID, EvalResult: res, Checks: []pkg.CheckResult{}}

	if res.Problem == "" {
		out.Checks = c.Assert(res.Output, res.Action)
		if criteria := c.JudgeCriteria(); len(criteria) > 0 {
			class := s.JudgeClass
			if class == "" {
				class = scn.ModelClass
			}
			verdicts, usage, err := s.Engine.Judge(r.Context(), agent, class, criteria, c.Input, res.Output)
			out.JudgeUsage = &usage
			if err != nil {
				out.Checks = append(out.Checks, pkg.CheckResult{Check: "judge", Detail: err.Error()})
			}
			for _, v := range verdicts {
				out.Checks = append(out.Checks, pkg.CheckResult{Check: v.Criterion, Pass: v.Pass, Detail: v.Reason})
			}
		}
	}
	out.Pass = res.Problem == ""
	for _, ch := range out.Checks {
		out.Pass = out.Pass && ch.Pass
	}
	out.Seconds = time.Since(started).Seconds()
	if s.Log != nil {
		s.Log.Info("eval case", "agent", agent, "version", out.Version, "scenario", scenario, "case", caseID, "pass", out.Pass, "by", who)
	}
	writeJSON(w, http.StatusOK, out)
}
