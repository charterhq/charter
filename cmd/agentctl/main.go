// Command agentctl is the Charter CLI.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/charterhq/charter/internal/pkg"
	"github.com/charterhq/charter/internal/render"
)

var version = "dev"

const usage = `usage:
  agentctl validate <package-dir> [--json]          check an agent package
  agentctl fields                                    what this core does with each field of the format
  agentctl publish <package-dir> [git-sha]           validate and publish a package to the core
  agentctl render <package-dir> <scenario> <output.json> [--failed error]
                                                     show the message a run would post
  agentctl suspend <agent> <reason> [connection]     switch an agent off
  agentctl resume <agent> <reason> [connection]      switch an agent back on
  agentctl agents                                    list agents and switches
  agentctl runs [agent] [status]                     list recent runs
  agentctl run <id>                                  show a run with its steps
  agentctl approve|reject <wait-id> <fingerprint>    answer an approval
  agentctl say <agent> <connection> <source> <conversation> <text...>
                                                     send a message to an agent as the token's caller
  agentctl call <agent> <scenario> <connection> <input.json|-> [wait]
                                                     launch a scenario with a call trigger, wait up to 5m for the output
  agentctl version

API commands use CHARTER_URL and CHARTER_TOKEN.`

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	switch cmd := args[0]; cmd {
	case "version":
		fmt.Println("agentctl", version)
		return 0
	case "fields":
		fmt.Print(pkg.SupportTable())
		return 0
	case "validate":
		if len(args) < 2 {
			break
		}
		return validate(args[1], len(args) > 2 && args[2] == "--json")
	case "publish":
		if len(args) < 2 {
			break
		}
		sha := ""
		if len(args) > 2 {
			sha = args[2]
		}
		return publish(args[1], sha)
	case "render":
		if len(args) < 4 {
			break
		}
		failed := ""
		if len(args) > 5 && args[4] == "--failed" {
			failed = args[5]
		}
		return renderCmd(args[1], args[2], args[3], failed)
	case "suspend", "resume":
		if len(args) < 3 {
			break
		}
		conn := ""
		if len(args) > 3 {
			conn = args[3]
		}
		return apiCall("POST", "/v1/agents/"+args[1]+"/"+cmd, map[string]string{"reason": args[2], "connection": conn})
	case "agents":
		return apiCall("GET", "/v1/agents", nil)
	case "runs":
		q := "?limit=50"
		if len(args) > 1 {
			q += "&agent=" + args[1]
		}
		if len(args) > 2 {
			q += "&status=" + args[2]
		}
		return apiCall("GET", "/v1/runs"+q, nil)
	case "run":
		if len(args) < 2 {
			break
		}
		return apiCall("GET", "/v1/runs/"+args[1], nil)
	case "say":
		if len(args) < 6 {
			break
		}
		who := os.Getenv("USER")
		if who == "" {
			who = "agentctl"
		}
		return apiCall("POST", "/v1/conversations/"+args[1]+"/"+args[2]+"/"+args[3], map[string]string{
			"conversation": args[4], "message_id": fmt.Sprintf("agentctl-%d", time.Now().UnixNano()),
			"author": who, "text": strings.Join(args[5:], " ")})
	case "call":
		if len(args) < 5 {
			break
		}
		var in []byte
		var err error
		if args[4] == "-" {
			in, err = io.ReadAll(os.Stdin)
		} else {
			in, err = os.ReadFile(args[4])
		}
		if err != nil || !json.Valid(in) {
			fmt.Fprintln(os.Stderr, "input must be a JSON file or - for stdin")
			return 2
		}
		q := "?connection=" + url.QueryEscape(args[3])
		if len(args) > 5 {
			q += "&wait=" + url.QueryEscape(args[5])
		}
		return apiCall("POST", "/v1/agents/"+args[1]+"/scenarios/"+args[2]+"/call"+q, json.RawMessage(in))
	case "approve", "reject":
		if len(args) < 3 {
			break
		}
		return apiCall("POST", "/v1/waits/"+args[1]+"/resolve", map[string]string{"decision": cmd, "fingerprint": args[2]})
	}
	fmt.Fprintln(os.Stderr, usage)
	return 2
}

func validate(dir string, asJSON bool) int {
	p, problems := pkg.Load(os.DirFS(dir))
	switch {
	case asJSON:
		out := map[string]any{"valid": problems == nil, "problems": problems}
		if p != nil {
			out["agent"] = p.Agent.Metadata
			out["warnings"] = p.Warnings
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
	case problems != nil:
		for _, pr := range problems {
			fmt.Println(pr)
		}
		if p != nil {
			printWarnings(os.Stdout, p.Warnings)
		}
	default:
		printWarnings(os.Stdout, p.Warnings)
		fmt.Printf("%s %s: ok, %d scenario(s), %d connection(s), %d warning(s)\n",
			p.Agent.Metadata.Name, p.Agent.Metadata.Version, len(p.Scenarios), len(p.Connections), len(p.Warnings))
	}
	if problems != nil {
		return 1
	}
	return 0
}

func printWarnings(w io.Writer, ws pkg.Problems) {
	for _, pr := range ws {
		fmt.Fprintln(w, "warning:", pr)
	}
}

func apiCall(method, path string, body any) int {
	base, token := strings.TrimRight(os.Getenv("CHARTER_URL"), "/"), os.Getenv("CHARTER_TOKEN")
	if base == "" || token == "" {
		fmt.Fprintln(os.Stderr, "CHARTER_URL and CHARTER_TOKEN must be set")
		return 2
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var pretty bytes.Buffer
	if json.Indent(&pretty, raw, "", "  ") == nil {
		raw = pretty.Bytes()
	}
	fmt.Println(string(raw))
	if resp.StatusCode >= 300 {
		return 1
	}
	return 0
}

// renderCmd renders a scenario's response template over a saved output, without a run.
func renderCmd(dir, scenario, outputFile, failed string) int {
	p, problems := pkg.Load(os.DirFS(dir))
	if problems != nil {
		for _, pr := range problems {
			fmt.Fprintln(os.Stderr, pr)
		}
		return 1
	}
	s, ok := p.Scenarios[scenario]
	if !ok || s.Template == "" {
		fmt.Fprintf(os.Stderr, "scenario %q has no response template\n", scenario)
		return 1
	}
	out, err := os.ReadFile(outputFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	r := render.Run{ID: "00000000-0000-0000-0000-000000000000", ShortID: "00000000", Agent: p.Agent.Metadata.Name,
		Version: p.Agent.Metadata.Version, Scenario: scenario, Autonomy: s.Spec.Autonomy, Status: "ok", Seconds: 42}
	var msg string
	if failed != "" {
		r.Status, r.Error = "failed", failed
		var has bool
		msg, has, err = render.Failed(s.Template, r, nil)
		if err == nil && !has {
			err = fmt.Errorf("template has no %q block", render.FailedBlock)
		}
	} else {
		msg, err = render.Output(s.Template, r, nil, out)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(msg)
	return 0
}

// publish checks the package locally first (same loader as the core), then sends it as tar.gz.
func publish(dir, sha string) int {
	p, problems := pkg.Load(os.DirFS(dir))
	if problems != nil {
		fmt.Fprintf(os.Stderr, "package has problems:\n%v\n", problems)
		return 1
	}
	printWarnings(os.Stderr, p.Warnings)
	b, err := pkg.Pack(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	base, token := strings.TrimRight(os.Getenv("CHARTER_URL"), "/"), os.Getenv("CHARTER_TOKEN")
	if base == "" || token == "" {
		fmt.Fprintln(os.Stderr, "CHARTER_URL and CHARTER_TOKEN must be set")
		return 2
	}
	req, err := http.NewRequest(http.MethodPost, base+"/v1/packages?git_sha="+url.QueryEscape(sha), bytes.NewReader(b))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/gzip")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	fmt.Println(strings.TrimSpace(string(raw)))
	if resp.StatusCode >= 300 {
		return 1
	}
	return 0
}
