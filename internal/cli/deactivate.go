package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// Deactivate runs `ctx deactivate [--eval [bash|fish]]`, mirroring
// cmd_deactivate in lib/cmd_ops.sh, and returns the exit code. stdout carries
// only shell code for the caller to eval.
func Deactivate(stdout, stderr io.Writer, args []string) int {
	u := ui{out: stderr, err: stderr, colors: ttyColors(stdout)}
	shell, wantEval := "bash", false
	for i := 0; i < len(args); i++ {
		if args[i] != "--eval" {
			continue
		}
		wantEval = true
		if i+1 < len(args) && args[i+1] != "" && !strings.HasPrefix(args[i+1], "--") {
			shell = args[i+1]
			i++
		}
	}

	_ = ctxconfig.InitDirs()
	agentDropAddedKey()
	quiet := os.Getenv("CTX_QUIET")
	if quiet == "" {
		quiet = "0"
	}
	cfg, err := ctxconfig.Load()
	if err != nil {
		return u.die(err.Error())
	}
	current := cfg.Active
	if current == "" {
		if quiet == "0" {
			u.dim("No active profile.")
		}
		return 0
	}

	dir := profile.Dir()
	if st, err := os.Stat(filepath.Join(dir, current+".conf")); err != nil || !st.Mode().IsRegular() {
		return u.die(fmt.Sprintf("Profile '%s' not found. Run: ctx list", current))
	}
	p, err := profile.Load(dir, current)
	if err != nil {
		return u.die(err.Error())
	}
	wd := ""
	if p.WorkDir != "" && strings.HasPrefix(currentPWD(), p.WorkDir) {
		wd = p.WorkDir
	}

	_ = ctxconfig.ClearActivation()

	fish := echoLower(shell) == "fish"
	_, _ = io.WriteString(stdout, deactivateUnsets(p, fish, wd))

	if !wantEval && quiet == "0" {
		u.dim("Tip (fish): ctx deactivate --eval fish | source")
		u.dim("Tip (bash/zsh): eval \"$(ctx deactivate)\"")
	}
	return 0
}

// currentPWD is Bash's $PWD: the inherited PWD when it names the current
// directory, else the physical working directory.
func currentPWD() string {
	cwd, err := os.Getwd()
	if env := os.Getenv("PWD"); filepath.IsAbs(env) {
		a, aerr := os.Stat(env)
		b, berr := os.Stat(".")
		if aerr == nil && berr == nil && os.SameFile(a, b) {
			return env
		}
	}
	if err != nil {
		return ""
	}
	return cwd
}

// deactivateUnsets mirrors _ctx_deactivate_emit_unsets.
func deactivateUnsets(p *profile.Profile, fish bool, wd string) string {
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	isSep := func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }
	unset := func(k string) {
		if fish {
			line("set -e %s", k)
		} else {
			line("unset %s 2>/dev/null || true", k)
		}
	}
	for _, k := range strings.FieldsFunc(p.SecretKeysRaw, isSep) {
		if isValidEnvKey(k) {
			unset(k)
		}
	}
	if fish {
		line("set -e AWS_PROFILE AZURE_SUBSCRIPTION AZURE_TENANT GCLOUD_PROJECT GCP_PROJECT GCP_ACCOUNT")
		line("set -e CTX_ACTIVE_PROFILE CTX_ACTIVATION_TRIGGER")
		line("set -e CTX_PROMPT_SHOW CTX_PROMPT_PROFILE CTX_PROMPT_WORK_DIR")
		if wd != "" {
			line("set -l __ctx_wd '%s'", strings.ReplaceAll(wd, "'", `'\''`))
			line(`if string match "$__ctx_wd/*" "$PWD" >/dev/null 2>&1; or test "$PWD" = "$__ctx_wd"`)
			line("  git config --unset user.name 2>/dev/null; or true")
			line("  git config --unset user.email 2>/dev/null; or true")
			line("end")
		}
	} else {
		line("unset AWS_PROFILE AZURE_SUBSCRIPTION AZURE_TENANT GCLOUD_PROJECT GCP_PROJECT GCP_ACCOUNT 2>/dev/null || true")
		line("unset CTX_ACTIVE_PROFILE CTX_ACTIVATION_TRIGGER 2>/dev/null || true")
		line("unset CTX_PROMPT_SHOW CTX_PROMPT_PROFILE CTX_PROMPT_WORK_DIR 2>/dev/null || true")
		if wd != "" {
			line(`if [[ "$PWD" == %s* ]]; then git config --unset user.name 2>/dev/null || true; git config --unset user.email 2>/dev/null || true; fi`, bashQuote(wd))
		}
	}
	for _, pair := range strings.Split(p.ExtraEnvsRaw, "\n") {
		if pair == "" {
			continue
		}
		k, _, _ := strings.Cut(pair, "=")
		if isValidEnvKey(k) {
			unset(k)
		}
	}
	return b.String()
}
