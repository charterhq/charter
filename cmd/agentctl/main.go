// Command agentctl is the Charter CLI.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/charterhq/charter/internal/pkg"
)

var version = "dev"

const usage = `usage:
  agentctl validate <package-dir> [--json]   check an agent package
  agentctl version`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Println("agentctl", version)
		return 0
	case "validate":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
		asJSON := len(args) > 2 && args[2] == "--json"
		p, problems := pkg.Load(os.DirFS(args[1]))
		if asJSON {
			out := map[string]any{"valid": problems == nil, "problems": problems}
			if p != nil {
				out["agent"] = p.Agent.Metadata
			}
			_ = json.NewEncoder(os.Stdout).Encode(out)
		} else if problems != nil {
			for _, pr := range problems {
				fmt.Println(pr)
			}
		} else {
			fmt.Printf("%s %s: ok, %d scenario(s), %d connection(s)\n",
				p.Agent.Metadata.Name, p.Agent.Metadata.Version, len(p.Scenarios), len(p.Connections))
		}
		if problems != nil {
			return 1
		}
		return 0
	default:
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
}
