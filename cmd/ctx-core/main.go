// Command ctx-core is the Go companion to the ctx Bash CLI. It hosts the
// MCP server and the knowledge store; bin/ctx dispatches to it.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hei-Tech-Inc/ctx/internal/cli"
	"github.com/Hei-Tech-Inc/ctx/internal/guard"
	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/mcpserver"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: ctx-core <version|help|completion|mcp|knowledge|list|status|workdir-prompt|env|run|secret|config|deactivate|use|remove|setup|guard|agent-rules|edit|undo|uninstall|verify|clone|doctor> [args]\n"

// implemented is the single table of commands ctx-core runs instead of Bash.
// Each entry gets the arguments after the command and reports whether
// ctx-core handles that shape. Most look only at the first argument (the
// subcommand or flag; "" when absent). Later tasks flip entries to true.
// list and status take no subcommand besides --json; guard for every subcommand.
var implemented = map[string]func(args []string) bool{
	"list":           func(a []string) bool { return first(a) == "" || first(a) == "--json" },
	"status":         func(a []string) bool { return first(a) == "" || first(a) == "--json" },
	"workdir-prompt": func([]string) bool { return true },
	"env":            func([]string) bool { return true },
	"run":            func([]string) bool { return true },
	"guard":          func(a []string) bool { return guardSubs[first(a)] },
	"secret":         func(a []string) bool { return secretSubs[first(a)] },
	"config":         func(a []string) bool { return configSubs[first(a)] },
	"deactivate":     func([]string) bool { return true },
	"use":            func([]string) bool { return true },
	"remove":         func([]string) bool { return true },
	// setup and its aliases: every shape (the wizard, --extend, help,
	// --config and the argument errors).
	"setup":  func([]string) bool { return true },
	"import": func([]string) bool { return true },
	"add":    func([]string) bool { return true },
	// ctx-version is the user-facing `ctx version|-v|--version` line
	// ("ctx version X"); bin/ctx forwards it under this name so that
	// `ctx-core version` keeps printing "ctx-core X" (install and doctor
	// parse that).
	"ctx-version": func([]string) bool { return true },
	"help":        func([]string) bool { return true },
	"completion":  func([]string) bool { return true },
	"agent-rules": func([]string) bool { return true },
	"edit":        func([]string) bool { return true },
	"undo":        func([]string) bool { return true },
	"uninstall":   func([]string) bool { return true },
	"verify":      func([]string) bool { return true },
	"clone":       func([]string) bool { return true },
	"doctor":      func([]string) bool { return true },
}

// first is args[0], or "" when there are no arguments.
func first(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// secretSubs are the `secret` subcommands ctx-core runs.
var secretSubs = map[string]bool{"get": true, "list": true, "delete": true, "set": true, "migrate": true}

// guardSubs are the `guard` subcommands ctx-core runs ("" is status).
var guardSubs = map[string]bool{"": true, "check": true, "install": true, "uninstall": true, "remove": true, "status": true}

// configSubs are the `config` subcommands ctx-core runs ("" is show).
var configSubs = map[string]bool{
	"": true, "show": true, "work-root": true, "secret-provider": true, "prompt-workdir-depth": true,
	"prompt-extra-paths": true, "autoswitch-notify": true, "export": true, "import": true,
}

func implements(cmd string, rest []string) bool {
	f, ok := implemented[cmd]
	if !ok {
		return false
	}
	return f(rest)
}

// ctxVersion is the ctx version bin/ctx passes in CTX_VERSION, else the build version.
func ctxVersion() string {
	if v := os.Getenv("CTX_VERSION"); v != "" {
		return v
	}
	return version
}

// setCtxVersionEnv makes CTX_VERSION the version to print (bin/ctx's, else the build's).
func setCtxVersionEnv() { _ = os.Setenv("CTX_VERSION", ctxVersion()) }

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
	case "env", "run":
		return runEnvRun(args, stdout, stderr)
	case "secret":
		if len(args) < 2 || !implements("secret", args[1:]) {
			fmt.Fprintln(stderr, "ctx-core: secret supports only get, list, delete, set and migrate")
			return 127
		}
		term := osTerm(stdout, stderr)
		return cli.SecretIn(term, args[1:])
	case "config":
		return cli.Config(stdout, stderr, args[1:])
	case "deactivate":
		return cli.Deactivate(stdout, stderr, args[1:])
	case "use":
		term := osTerm(stdout, stderr)
		return cli.UseIn(term, args[1:])
	case "remove":
		term := osTerm(stdout, stderr)
		return cli.Remove(term, args[1:])
	case "setup", "import", "add":
		term := osTerm(stdout, stderr)
		return cli.SetupIn(term, args[1:])
	case "agent-rules":
		return cli.AgentRules(osTerm(stdout, stderr), args[1:])
	case "edit":
		return cli.Edit(osTerm(stdout, stderr), args[1:])
	case "undo":
		return cli.Undo(osTerm(stdout, stderr), args[1:])
	case "uninstall":
		return cli.Uninstall(osTerm(stdout, stderr), args[1:])
	case "verify":
		return cli.Verify(osTerm(stdout, stderr), args[1:])
	case "clone":
		return cli.Clone(osTerm(stdout, stderr), args[1:])
	case "doctor":
		cli.CoreVersion = version
		return cli.Doctor(osTerm(stdout, stderr), args[1:])
	case "guard":
		sub := ""
		if len(args) > 1 {
			sub = args[1]
		}
		switch sub {
		case "", "status":
			return guard.Status(stdout, stderr)
		case "install":
			return guard.Install(stdout, stderr)
		case "uninstall", "remove":
			return guard.Uninstall(stdout, stderr)
		case "check":
			hook, rest := "", []string(nil)
			if len(args) > 2 {
				hook, rest = args[2], args[3:]
			}
			return guard.Check(hook, rest, stderr)
		}
		fmt.Fprintln(stderr, "ctx-core: guard supports only status, install, uninstall and check")
		return 127
	case "ctx-version":
		setCtxVersionEnv()
		return cli.Version(stdout)
	case "help":
		setCtxVersionEnv()
		return cli.Help(osTerm(stdout, stderr), args[1:])
	case "completion":
		return cli.Completion(stdout, stderr, args[1:])
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
	d := mcpserver.Deps{ProfilesDir: profile.Dir(), Getter: secret.ProviderGetter{}}
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

// runListStatus serves `list`, `status` and their --json forms. Any other form
// exits 127.
func runListStatus(args []string, stdout, stderr io.Writer) int {
	if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
		fmt.Fprintf(stderr, "ctx-core: %s takes no arguments other than --json\n", args[0])
		return 127
	}
	if len(args) == 1 {
		term := osTerm(stdout, stderr)
		if args[0] == "list" {
			return cli.ListText(term)
		}
		return cli.StatusText(term)
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

// runEnvRun serves `env` and `run`. run replaces the process and returns only on failure.
func runEnvRun(args []string, stdout, stderr io.Writer) int {
	var err error
	if args[0] == "env" {
		err = cli.Env(stdout, args[1:])
	} else {
		err = cli.Run(args[1:])
	}
	if err == nil {
		return 0
	}
	var ee *cli.ExitError
	if errors.As(err, &ee) {
		fmt.Fprintln(stderr, ee.Msg)
		return ee.Code
	}
	fmt.Fprintf(stderr, "\u2717 %v\n", err)
	return 1
}

// osTerm is the process terminal with its output streams replaced.
func osTerm(stdout, stderr io.Writer) tui.Term {
	term := tui.FromOS()
	term.Out, term.Err = stdout, stderr
	return term
}
