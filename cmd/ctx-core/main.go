// Command ctx-core is the Go companion to the ctx Bash CLI. It hosts the
// MCP server and the knowledge store; bin/ctx dispatches to it.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hei-Tech-Inc/ctx/internal/cli"
	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/mcpserver"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: ctx-core <version|mcp|knowledge|list|status|workdir-prompt|env|run|guard> [args]\n"

// implemented is the single table of commands ctx-core runs instead of Bash.
// Each entry reports whether it handles the given first argument (the
// subcommand or flag; "" when absent). Later tasks flip entries to true.
// list and status count only with --json; guard only with check.
var implemented = map[string]func(sub string) bool{
	"list":           func(sub string) bool { return sub == "--json" },
	"status":         func(sub string) bool { return sub == "--json" },
	"workdir-prompt": func(string) bool { return true },
	"env":            func(string) bool { return false },
	"run":            func(string) bool { return false },
	"guard":          func(sub string) bool { return false && sub == "check" },
}

func implements(cmd string, rest []string) bool {
	f, ok := implemented[cmd]
	if !ok {
		return false
	}
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	return f(sub)
}

// ctxVersion is the ctx version bin/ctx passes in CTX_VERSION, else the build version.
func ctxVersion() string {
	if v := os.Getenv("CTX_VERSION"); v != "" {
		return v
	}
	return version
}

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
	case "list", "status":
		return runListStatus(args, stdout, stderr)
	case "workdir-prompt":
		pwd, err := os.Getwd()
		if err == nil {
			err = cli.WorkdirPrompt(stdout, pwd)
		}
		if err != nil {
			fmt.Fprintf(stderr, "ctx-core: %v\n", err)
		}
		return 0 // the Bash command never fails
	case "env", "run", "guard":
		// Not ported yet. bin/ctx asks `implements` first, so this is unreachable via forwarding.
		fmt.Fprintln(stderr, "not implemented")
		return 2
	case "implements":
		if len(args) >= 2 && implements(args[1], args[2:]) {
			return 0
		}
		return 1
	case "knowledge":
		return runKnowledge(args[1:], stdout, stderr)
	case "mcp":
		if len(args) > 1 && args[1] == "install" {
			return runMCPInstall(args[2:], stdout, stderr)
		}
		return runMCP(args[1:], stderr)
	default:
		fmt.Fprintf(stderr, "ctx-core: unknown command %q\n%s", args[0], usage)
		return 2
	}
}

const mcpUsage = "usage: ctx-core mcp [--profile NAME] | mcp install <claude|cursor>\n"

// runMCP serves MCP over stdin/stdout until the client disconnects.
func runMCP(args []string, stderr io.Writer) int {
	opts := mcpserver.Options{}
	switch {
	case len(args) == 0:
	case len(args) == 2 && args[0] == "--profile" && args[1] != "":
		opts.Profile = args[1]
	case len(args) == 1 && strings.HasPrefix(args[0], "--profile=") && len(args[0]) > len("--profile="):
		opts.Profile = strings.TrimPrefix(args[0], "--profile=")
	default:
		fmt.Fprint(stderr, mcpUsage)
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "ctx-core: %v\n", err)
		return 1
	}
	opts.Cwd = cwd
	d := mcpserver.Deps{ProfilesDir: profile.Dir(), Getter: secret.CLIGetter{}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := mcpserver.Run(ctx, d, opts, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		fmt.Fprintf(stderr, "ctx-core mcp: %v\n", err)
		return 1
	}
	return 0
}

const knowledgeUsage = "usage: ctx-core knowledge init [profile]\n"

func runKnowledge(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "init" || len(args) > 2 {
		fmt.Fprint(stderr, knowledgeUsage)
		return 2
	}
	name := ""
	if len(args) == 2 {
		name = args[1]
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
			fmt.Fprintf(stderr, "ctx-core: invalid profile name %q\n", name)
			return 1
		}
	}
	dir := profile.Dir()
	if name == "" {
		pwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "ctx-core: %v\n", err)
			return 1
		}
		name, _, err = resolve.Path(pwd, dir)
		if err != nil {
			fmt.Fprintf(stderr, "ctx-core: %v\n", err)
			return 1
		}
		if name == "" {
			fmt.Fprintf(stderr, "ctx-core: no profile matches %s; pass a profile name\n", pwd)
			return 1
		}
	}
	if _, err := profile.Load(dir, name); err != nil {
		fmt.Fprintf(stderr, "ctx-core: %v\n", err)
		return 1
	}
	root := knowledge.Root(name)
	created, err := knowledge.Init(root)
	if err != nil {
		fmt.Fprintf(stderr, "ctx-core: %v\n", err)
		return 1
	}
	if len(created) == 0 {
		fmt.Fprintf(stdout, "%s already initialised; nothing created\n", root)
		return 0
	}
	for _, p := range created {
		fmt.Fprintln(stdout, p)
	}
	return 0
}

// runListStatus serves `list --json` and `status --json`. Any other form is not
// ported (the text screens stay in Bash) and exits 127 so a caller can fall back.
func runListStatus(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[1] != "--json" {
		fmt.Fprintf(stderr, "ctx-core: %s supports only --json\n", args[0])
		return 127
	}
	var err error
	if args[0] == "list" {
		err = cli.ListJSON(stdout, ctxVersion())
	} else {
		var pwd string
		if pwd, err = os.Getwd(); err == nil {
			err = cli.StatusJSON(stdout, ctxVersion(), pwd)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "\u2717 %v\n", err)
		return 1
	}
	return 0
}
