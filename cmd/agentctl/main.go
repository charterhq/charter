// Command agentctl is the Charter CLI.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charterhq/charter/internal/pkg"
)

var version = "dev"

const usage = `usage:
  agentctl validate <package-dir> [--json]          check an agent package
  agentctl suspend <agent> <reason> [connection]     switch an agent off
  agentctl resume <agent> <reason> [connection]      switch an agent back on
  agentctl agents                                    list agents and switches
  agentctl runs [agent] [status]                     list recent runs
  agentctl run <id>                                  show a run with its steps
  agentctl approve|reject <wait-id> <fingerprint>    answer an approval
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
	case "validate":
		if len(args) < 2 {
			break
		}
		return validate(args[1], len(args) > 2 && args[2] == "--json")
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
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
	case problems != nil:
		for _, pr := range problems {
			fmt.Println(pr)
		}
	default:
		fmt.Printf("%s %s: ok, %d scenario(s), %d connection(s)\n",
			p.Agent.Metadata.Name, p.Agent.Metadata.Version, len(p.Scenarios), len(p.Connections))
	}
	if problems != nil {
		return 1
	}
	return 0
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
