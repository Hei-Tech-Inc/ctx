// Command ctx-core is the Go companion to the ctx Bash CLI. It hosts the
// MCP server and the knowledge store; bin/ctx dispatches to it.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: ctx-core <version|mcp|knowledge> [args]\n"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "ctx-core %s\n", version)
		return 0
	case "mcp", "knowledge":
		fmt.Fprintf(stderr, "ctx-core %s: not implemented yet\n", args[0])
		return 2
	default:
		fmt.Fprintf(stderr, "ctx-core: unknown command %q\n%s", args[0], usage)
		return 2
	}
}
