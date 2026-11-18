// Command agentctl is the Charter CLI.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("agentctl", version)
		return
	}
	fmt.Fprintln(os.Stderr, "usage: agentctl version")
	os.Exit(2)
}
