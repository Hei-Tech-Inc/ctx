// Package kube runs kubectl scoped to one environment's kubeconfig,
// context and namespace, and performs cluster logins.
package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
)

// Target is where a kubectl call goes. Every call carries all three.
// NeedsContext is set for a kubeconfig-type login: such a target must name
// its Context, or the Runner refuses it rather than fall back to the file's
// current-context.
type Target struct {
	Kubeconfig, Context, Namespace string
	NeedsContext                   bool
}

// KubeDir returns $CTX_DIR/kube/<profile>, with CTX_DIR defaulting to ~/.ctx.
func KubeDir(profileName string) string {
	base := os.Getenv("CTX_DIR")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".ctx")
	}
	return filepath.Join(base, "kube", profileName)
}

// TargetFor returns the target for env. A command login uses the ctx-owned
// file $CTX_DIR/kube/<profile>/<cluster>.yaml; a kubeconfig login uses its
// path and context.
func TargetFor(profileName string, s *knowledge.Store, env knowledge.Env) Target {
	login := s.Clusters[env.Cluster].Login
	if login.Type == "kubeconfig" {
		return Target{Kubeconfig: expandHome(login.Path), Context: login.Context, Namespace: env.Namespace, NeedsContext: true}
	}
	return Target{Kubeconfig: filepath.Join(KubeDir(profileName), env.Cluster+".yaml"), Namespace: env.Namespace}
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		return filepath.Join(os.Getenv("HOME"), p[1:])
	}
	return p
}

// Runner runs kubectl. Kubectl defaults to "kubectl" on PATH. All output and
// errors pass through Redact. Observe, when set, is called with the redacted
// argv (kubectl binary first) of every call just before it runs; it may be
// called from several goroutines at once.
type Runner struct {
	Kubectl string
	Redact  *secret.Redactor
	Observe func(argv []string)
}

func (r *Runner) bin() string {
	if r.Kubectl == "" {
		return "kubectl"
	}
	return r.Kubectl
}

// Args returns the scoping flags followed by args. -n is left out only when
// args ask for all namespaces.
func (r *Runner) Args(t Target, args ...string) []string {
	out := []string{"--kubeconfig", t.Kubeconfig}
	if t.Context != "" {
		out = append(out, "--context", t.Context)
	}
	if !allNamespaces(args) {
		out = append(out, "-n", t.Namespace)
	}
	return append(out, args...)
}

// Argv is the full command line a call with args runs: the kubectl binary
// followed by Args.
func (r *Runner) Argv(t Target, args ...string) []string {
	return append([]string{r.bin()}, r.Args(t, args...)...)
}

// allNamespaces reports whether the kubectl flags (args before "--") turn on
// all namespaces. Args after "--" belong to a remote command ("exec pod --
// ls -A") and do not count.
func allNamespaces(args []string) bool {
	if i := indexOf(args, "--"); i >= 0 {
		args = args[:i]
	}
	for _, a := range args {
		if a == "-A" || a == "--all-namespaces" || a == "--all-namespaces=true" {
			return true
		}
	}
	return false
}

// RunError is a failed kubectl call. Argv (including the kubectl binary)
// and Stderr are redacted. Exit is -1 when kubectl did not run.
type RunError struct {
	Argv   []string
	Exit   int
	Stderr string

	missingKubeconfig bool
}

func (e *RunError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	return fmt.Sprintf("%s: exit %d: %s", strings.Join(e.Argv, " "), e.Exit, msg)
}

// ErrNotLoggedIn means the cluster needs login(env) before kubectl works.
var ErrNotLoggedIn = errors.New("not logged in")

type authError struct{ env string }

func (e *authError) Error() string {
	return fmt.Sprintf("not logged in to %s: call login(env=%q)", e.env, e.env)
}

func (e *authError) Unwrap() error { return ErrNotLoggedIn }

var authMarkers = []string{"unauthorized", "you must be logged in", "provide credentials", "token has expired"}

// AsAuthError turns a *RunError caused by missing or expired credentials
// (or a missing kubeconfig file) into an ErrNotLoggedIn error naming env.
// Any other error is returned unchanged.
func AsAuthError(err error, env string) error {
	var re *RunError
	if !errors.As(err, &re) {
		return err
	}
	if re.missingKubeconfig {
		return &authError{env: env}
	}
	low := strings.ToLower(re.Stderr)
	for _, m := range authMarkers {
		if strings.Contains(low, m) {
			return &authError{env: env}
		}
	}
	return err
}

// childEnv is the current environment without KUBECONFIG, plus KUBECONFIG=kc
// and extra (KEY=value). Variables named in extra are replaced, not duplicated.
func childEnv(kc string, extra ...string) []string {
	drop := map[string]bool{"KUBECONFIG": true}
	for _, kv := range extra {
		drop[strings.SplitN(kv, "=", 2)[0]] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		if !drop[strings.SplitN(kv, "=", 2)[0]] {
			env = append(env, kv)
		}
	}
	env = append(env, "KUBECONFIG="+kc)
	return append(env, extra...)
}

func (r *Runner) command(ctx context.Context, t Target, args []string) (*exec.Cmd, []string, error) {
	if err := ValidateUserArgs(args); err != nil {
		return nil, nil, err
	}
	if t.NeedsContext && t.Context == "" {
		return nil, nil, fmt.Errorf("refusing to run kubectl: kubeconfig login for %s has no context set (ctx never uses the current-context)", t.Kubeconfig)
	}
	argv := r.Argv(t, args...)
	redArgv := make([]string, len(argv))
	for i, a := range argv {
		redArgv[i] = r.Redact.String(a)
	}
	if _, err := os.Stat(t.Kubeconfig); err != nil {
		return nil, redArgv, &RunError{Argv: redArgv, Exit: -1,
			Stderr: r.Redact.String(fmt.Sprintf("kubeconfig %s not found", t.Kubeconfig)), missingKubeconfig: true}
	}
	if r.Observe != nil {
		r.Observe(redArgv)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = childEnv("")
	return cmd, redArgv, nil
}

func (r *Runner) runErr(argv []string, err error, stderr []byte) error {
	if err == nil {
		return nil
	}
	exit := -1
	var ee *exec.ExitError
	msg := string(stderr)
	if errors.As(err, &ee) {
		exit = ee.ExitCode()
	} else if msg == "" {
		msg = err.Error()
	}
	return &RunError{Argv: argv, Exit: exit, Stderr: r.Redact.String(msg)}
}

// Run runs kubectl scoped to t with KUBECONFIG cleared, returning redacted
// stdout. args that fail ValidateUserArgs are refused without running
// kubectl; any other failure is a *RunError.
func (r *Runner) Run(ctx context.Context, t Target, args ...string) ([]byte, error) {
	return r.RunStdin(ctx, t, nil, args...)
}

// RunStdin is Run with stdin fed to kubectl (nil means no input).
func (r *Runner) RunStdin(ctx context.Context, t Target, stdin io.Reader, args ...string) ([]byte, error) {
	cmd, argv, err := r.command(ctx, t, args)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, &stdout, &stderr
	err = cmd.Run()
	return r.Redact.Bytes(stdout.Bytes()), r.runErr(argv, err, stderr.Bytes())
}

// Stream is Run with stdout redacted line by line into w.
func (r *Runner) Stream(ctx context.Context, t Target, w io.Writer, args ...string) error {
	cmd, argv, err := r.command(ctx, t, args)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	lw := &lineRedactor{w: w, red: r.Redact}
	cmd.Stdout, cmd.Stderr = lw, &stderr
	err = cmd.Run()
	if ferr := lw.flush(); err == nil && ferr != nil {
		return ferr
	}
	return r.runErr(argv, err, stderr.Bytes())
}

// Process is a kubectl call running in the background (see Start).
type Process struct {
	cmd *exec.Cmd
	out *lineRedactor
}

// Start runs kubectl scoped to t in the background, like Run but without
// waiting: stdout and stderr go to w, redacted line by line. The process is
// not tied to any context; the caller must Kill and Wait it. w must be safe
// to read while the process writes to it.
func (r *Runner) Start(t Target, w io.Writer, args ...string) (*Process, error) {
	cmd, argv, err := r.command(context.Background(), t, args)
	if err != nil {
		return nil, err
	}
	lw := &lineRedactor{w: w, red: r.Redact}
	cmd.Stdout, cmd.Stderr = lw, lw // the same writer: exec uses one pipe
	if err := cmd.Start(); err != nil {
		return nil, &RunError{Argv: argv, Exit: -1, Stderr: r.Redact.String(err.Error())}
	}
	return &Process{cmd: cmd, out: lw}, nil
}

// Pid is the process id.
func (p *Process) Pid() int { return p.cmd.Process.Pid }

// Kill stops the process.
func (p *Process) Kill() error { return p.cmd.Process.Kill() }

// Wait waits for the process to exit and writes any unfinished last line.
func (p *Process) Wait() error {
	err := p.cmd.Wait()
	if ferr := p.out.flush(); err == nil {
		err = ferr
	}
	return err
}

// lineRedactor buffers partial lines so a secret split across writes is
// still redacted.
type lineRedactor struct {
	w   io.Writer
	red *secret.Redactor
	buf []byte
	err error
}

func (l *lineRedactor) Write(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	l.buf = append(l.buf, p...)
	if i := bytes.LastIndexByte(l.buf, '\n'); i >= 0 {
		_, l.err = l.w.Write(l.red.Bytes(l.buf[:i+1]))
		l.buf = append(l.buf[:0], l.buf[i+1:]...)
	}
	if l.err != nil {
		return 0, l.err
	}
	return len(p), nil
}

func (l *lineRedactor) flush() error {
	if l.err != nil || len(l.buf) == 0 {
		return l.err
	}
	_, l.err = l.w.Write(l.red.Bytes(l.buf))
	l.buf = nil
	return l.err
}
