package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charterhq/charter/internal/pkg"
)

type evalReport struct {
	Agent    string            `json:"agent"`
	Version  string            `json:"version"`
	Scenario string            `json:"scenario"`
	Case     string            `json:"case"`
	Pass     bool              `json:"pass"`
	Problem  string            `json:"problem,omitempty"`
	Action   bool              `json:"action"`
	Output   json.RawMessage   `json:"output,omitempty"`
	Checks   []pkg.CheckResult `json:"checks"`
	Seconds  float64           `json:"seconds"`
	Usage    struct {
		InputTokens  int     `json:"input_tokens"`
		OutputTokens int     `json:"output_tokens"`
		Cost         float64 `json:"cost"`
	} `json:"usage"`
	Error string `json:"error,omitempty"` // the call failed: no verdict
}

// evalCmd runs the evaluation set of a package through the core and reports every case.
func evalCmd(args []string) int {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	scenario := fs.String("scenario", "", "only this scenario")
	only := fs.String("case", "", "only this case id")
	conn := fs.String("connection", "", "run with this connection's parameters and knowledge")
	asJSON := fs.Bool("json", false, "print reports as JSON lines")
	jobs := fs.Int("j", 2, "cases run in parallel")
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: agentctl eval <package-dir> [--scenario s] [--case id] [--connection c] [-j N] [--json]")
		return 2
	}
	dir := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	p, problems := pkg.Load(os.DirFS(dir))
	if problems != nil {
		fmt.Fprintf(os.Stderr, "package has problems:\n%v\n", problems)
		return 1
	}
	base, token := strings.TrimRight(os.Getenv("CHARTER_URL"), "/"), os.Getenv("CHARTER_TOKEN")
	if base == "" || token == "" {
		fmt.Fprintln(os.Stderr, "CHARTER_URL and CHARTER_TOKEN must be set")
		return 2
	}
	body, err := pkg.Pack(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	type job struct{ scenario, id string }
	var todo []job
	names := make([]string, 0, len(p.Scenarios))
	for n := range p.Scenarios {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if *scenario != "" && n != *scenario {
			continue
		}
		for _, c := range p.Scenarios[n].Cases {
			if *only == "" || c.ID == *only {
				todo = append(todo, job{n, c.ID})
			}
		}
	}
	if len(todo) == 0 {
		fmt.Fprintln(os.Stderr, "no cases match")
		return 2
	}

	client := &http.Client{Timeout: 15 * time.Minute}
	reports := make([]evalReport, len(todo))
	sem := make(chan struct{}, max(*jobs, 1))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, j := range todo {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rep := callEval(client, base, token, body, j.scenario, j.id, *conn)
			reports[i] = rep
			mu.Lock()
			printReport(rep, *asJSON)
			mu.Unlock()
		}()
	}
	wg.Wait()

	passed, cost := 0, 0.0
	for _, r := range reports {
		if r.Pass {
			passed++
		}
		cost += r.Usage.Cost
	}
	if !*asJSON {
		fmt.Printf("%s %s: %d/%d cases passed, cost %.4f\n", p.Agent.Metadata.Name, p.Agent.Metadata.Version, passed, len(reports), cost)
	}
	if passed != len(reports) {
		return 1
	}
	return 0
}

func callEval(client *http.Client, base, token string, body []byte, scenario, id, conn string) evalReport {
	rep := evalReport{Scenario: scenario, Case: id}
	q := url.Values{"scenario": {scenario}, "case": {id}}
	if conn != "" {
		q.Set("connection", conn)
	}
	req, err := http.NewRequest(http.MethodPost, base+"/v1/eval?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/gzip")
	resp, err := client.Do(req)
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		rep.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		return rep
	}
	if err := json.Unmarshal(raw, &rep); err != nil {
		rep.Error = "decode: " + err.Error()
	}
	return rep
}

func printReport(r evalReport, asJSON bool) {
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(r)
		return
	}
	mark := "PASS"
	switch {
	case r.Error != "":
		mark = "ERROR"
	case !r.Pass:
		mark = "FAIL"
	}
	fmt.Printf("%-5s %s/%s  %.0fs  %d+%d tokens\n", mark, r.Scenario, r.Case, r.Seconds, r.Usage.InputTokens, r.Usage.OutputTokens)
	if r.Error != "" {
		fmt.Printf("      %s\n", r.Error)
	}
	if r.Problem != "" {
		fmt.Printf("      invalid output: %s\n", r.Problem)
	}
	for _, c := range r.Checks {
		if !c.Pass {
			fmt.Printf("      ✗ %s: %s\n", c.Check, c.Detail)
		}
	}
}
