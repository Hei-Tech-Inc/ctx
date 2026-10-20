package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/setup"
)

const (
	ansiYellow = "\033[0;33m"
	ansiDim    = "\033[2m"
)

// ui prints like the Bash helpers info/success/warn/bold/dim (stdout) and
// error/die (stderr); every message goes through echo -e semantics.
// setupOut adapts ui to setup.Out with the same bytes as the Bash helpers.
type setupOut struct{ u ui }

func (o setupOut) Info(m string)    { o.u.info(m) }
func (o setupOut) Success(m string) { o.u.success(m) }
func (o setupOut) Warn(m string)    { o.u.warn(m) }
func (o setupOut) Dim(m string)     { o.u.dim(m) }
func (o setupOut) Bold(m string)    { o.u.bold(m) }

// setupOut returns u as a setup.Out.
func (u ui) setupOut() setup.Out { return setupOut{u} }

type ui struct {
	out, err io.Writer
	colors   bool
}

func (u ui) c(code string) string {
	if u.colors {
		return code
	}
	return ""
}

func (u ui) line(w io.Writer, s string) { _, _ = io.WriteString(w, echoE(s)) }

func (u ui) info(msg string)    { u.line(u.out, u.c(ansiCyan)+"→"+u.c(ansiReset)+" "+msg) }
func (u ui) success(msg string) { u.line(u.out, u.c(ansiGreen)+"✓"+u.c(ansiReset)+" "+msg) }
func (u ui) warn(msg string)    { u.line(u.out, u.c(ansiYellow)+"!"+u.c(ansiReset)+" "+msg) }
func (u ui) bold(msg string)    { u.line(u.out, u.c(ansiBold)+msg+u.c(ansiReset)) }
func (u ui) dim(msg string)     { u.line(u.out, u.c(ansiDim)+msg+u.c(ansiReset)) }
func (u ui) error(msg string)   { u.line(u.err, u.c(ansiRed)+"✗"+u.c(ansiReset)+" "+msg) }
func (u ui) say(s string)       { _, _ = io.WriteString(u.out, s+"\n") }

// die prints the error and returns the exit status 1.
func (u ui) die(msg string) int {
	u.error(msg)
	return 1
}

// Config runs `ctx config ...`, mirroring cmd_config in lib/cmd_ops.sh, and
// returns the exit code.
func Config(stdout, stderr io.Writer, args []string) int {
	return configCmd(ui{out: stdout, err: stderr, colors: ttyColors(stdout)}, args)
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// echoLower is `$(echo "$s" | tr '[:upper:]' '[:lower:]')`.
func echoLower(s string) string {
	return asciiLower(strings.TrimRight(echoPlain(s), "\n"))
}

func expandTilde(v string) string {
	if strings.HasPrefix(v, "~") {
		return os.Getenv("HOME") + v[1:]
	}
	return v
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func lastValue(key string) string {
	cfg, err := ctxconfig.Load()
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.Last[key]
}

func secretProviderLine() string {
	return secret.ConfiguredProvider() + " (effective: " + secret.EffectiveProvider() + ")"
}

func promptDepthText() string {
	cfg, err := ctxconfig.Load()
	if err != nil || cfg == nil {
		return "2"
	}
	return cfg.PromptWorkdirDepthText
}

// promptExtraPaths is `$(ctx_config_prompt_extra_paths_or_default)`.
func promptExtraPaths() string {
	if v := strings.TrimRight(lastValue("prompt_extra_paths"), "\n"); v != "" {
		return v
	}
	return "(none)"
}

// autoswitchNotify is ctx_config_autoswitch_notify: true when notifications are on.
func autoswitchNotify() bool {
	var v string
	if env := os.Getenv("CTX_AUTOSWITCH_NOTIFY"); env != "" {
		v = echoLower(env)
	} else {
		v = echoLower(lastValue("autoswitch_notify"))
	}
	v = strings.ReplaceAll(strings.ReplaceAll(v, "\r", ""), " ", "")
	switch v {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// upsert writes key=value and reports a failure on stderr like the Bash helper
// (which prints and returns 1); it returns whether the write succeeded.
func (u ui) upsert(key, value string) bool {
	err := ctxconfig.Upsert(key, value)
	if err == nil {
		return true
	}
	u.error(err.Error())
	return false
}

func configCmd(u ui, args []string) int {
	_ = ctxconfig.InitDirs()
	key, value := "", ""
	if len(args) > 0 {
		key = args[0]
	}
	if len(args) > 1 {
		value = args[1]
	}
	switch key {
	case "", "show":
		u.bold(`\n  ctx config\n`)
		u.say("  work_root: " + ctxconfig.WorkRoot())
		u.say("  secret_provider: " + secretProviderLine())
		u.say("  prompt_workdir_max_depth: " + promptDepthText() + " (prompt: CTX_PROMPT_* under each profile WORK_DIR)")
		u.say("  prompt_extra_paths: " + promptExtraPaths())
		if autoswitchNotify() {
			u.say("  autoswitch_notify: on ([ctx] ←/→ messages on profile change)")
		} else {
			u.say("  autoswitch_notify: off (default — silent autoswitch; screen-share safe)")
		}
		u.say("")
		u.info("Set with: ctx config work-root <path>")
		u.info("      or: ctx config secret-provider <auto|keychain|file|pass>")
		u.info("      or: ctx config prompt-workdir-depth <n>   # max path segments under WORK_DIR for CTX_PROMPT_SHOW (default 2)")
		u.info("      or: ctx config prompt-extra-paths '<p1>:<p2>'   # optional extra absolute prefixes (colon-separated)")
		u.info("      or: ctx config autoswitch-notify <on|off>   # [ctx] ←/→ lines (default off)")
		u.info("      or: ctx config export <dir> / ctx config import <dir>")
	case "work-root":
		if value == "" {
			u.say("  work_root: " + ctxconfig.WorkRoot())
			u.say("")
			u.info("Usage: ctx config work-root <path>")
			return 0
		}
		value = expandTilde(value)
		if err := os.MkdirAll(value, 0o777); err != nil {
			u.toolErr("mkdir", err)
			return u.die("Could not create directory: " + value)
		}
		if !u.upsert("work_root", value) {
			return u.die("Could not update " + ctxconfig.Path())
		}
		u.success("work_root set to: " + value)
	case "secret-provider":
		if value == "" {
			u.say("  secret_provider: " + secretProviderLine())
			u.say("")
			u.info("Usage: ctx config secret-provider <auto|keychain|file|pass>")
			return 0
		}
		value = echoLower(value)
		switch value {
		case "auto", "keychain", "file", "pass":
		default:
			return u.die("Invalid secret provider '" + value + "'. Use: auto, keychain, file, pass")
		}
		if !u.upsert("secret_provider", value) {
			return u.die("Could not update " + ctxconfig.Path())
		}
		u.success("secret_provider set to: " + value + " (effective: " + secret.EffectiveProvider() + ")")
	case "prompt-workdir-depth":
		if value == "" {
			u.say("  prompt_workdir_max_depth: " + promptDepthText())
			u.say("")
			u.info("Usage: ctx config prompt-workdir-depth <n>  (non-negative integer; 0 = only exact WORK_DIR)")
			return 0
		}
		if !isDigits(value) {
			return u.die("prompt-workdir-depth must be a non-negative integer")
		}
		u.upsert("prompt_workdir_max_depth", value)
		u.success("prompt_workdir_max_depth set to: " + value)
	case "prompt-extra-paths":
		if value == "" {
			u.say("  prompt_extra_paths: " + promptExtraPaths())
			u.say("")
			u.info("Usage: ctx config prompt-extra-paths '<abs1>:<abs2>:…'  (colon-separated), or: ctx config prompt-extra-paths clear")
			return 0
		}
		if value == "clear" {
			_ = ctxconfig.Remove("prompt_extra_paths")
			u.success("prompt_extra_paths cleared.")
		} else {
			u.upsert("prompt_extra_paths", value)
			u.success("prompt_extra_paths set.")
		}
	case "autoswitch-notify":
		if value == "" {
			if autoswitchNotify() {
				u.say("  autoswitch_notify: on")
			} else {
				u.say("  autoswitch_notify: off")
			}
			u.say("")
			u.info("Usage: ctx config autoswitch-notify <on|off>")
			u.dim("  Default is off so new tabs / cd do not print client profile names (screen-share safe).")
			u.dim("  Session override: CTX_AUTOSWITCH_NOTIFY=1")
			return 0
		}
		switch echoLower(value) {
		case "on", "1", "true", "yes":
			u.upsert("autoswitch_notify", "on")
			u.success("autoswitch_notify: on — [ctx] ←/→ messages enabled")
		case "off", "0", "false", "no":
			u.upsert("autoswitch_notify", "off")
			u.success("autoswitch_notify: off — silent autoswitch (default)")
		default:
			return u.die("Usage: ctx config autoswitch-notify <on|off>")
		}
	case "export":
		if value == "" {
			return u.die("Usage: ctx config export <directory>")
		}
		return configExport(u, expandTilde(value))
	case "import":
		if value == "" {
			return u.die("Usage: ctx config import <directory>")
		}
		return configImport(u, expandTilde(value))
	default:
		return u.die("Usage: ctx config [show|work-root <path>|secret-provider <auto|keychain|file|pass>|prompt-workdir-depth <n>|prompt-extra-paths '<paths>'|autoswitch-notify <on|off>|export <dir>|import <dir>]")
	}
	return 0
}

// copyGlob copies the regular files in srcDir whose names match (and do not
// start with a dot, like a shell glob) into dstDir; failures are ignored, as
// in `cp dir/pattern dst 2>/dev/null || true`.
func copyGlob(srcDir string, match func(string) bool, dstDir string) {
	ents, err := os.ReadDir(srcDir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !match(name) {
			continue
		}
		_ = ctxconfig.CopyFile(filepath.Join(srcDir, name), filepath.Join(dstDir, name))
	}
}

// toolErr prints a failure the way the coreutils diagnostic would: "cp: path: reason".
func (u ui) toolErr(tool string, err error) {
	var pe *os.PathError
	if errors.As(err, &pe) {
		_, _ = fmt.Fprintf(u.err, "%s: %s: %v\n", tool, pe.Path, pe.Err)
		return
	}
	_, _ = fmt.Fprintf(u.err, "%s: %v\n", tool, err)
}

func (u ui) cpFailed(err error) { u.toolErr("cp", err) }

func configExport(u ui, outdir string) int {
	home := os.Getenv("HOME")
	if err := os.MkdirAll(outdir, 0o777); err != nil {
		u.toolErr("mkdir", err)
		return u.die("Could not create export directory: " + outdir)
	}
	if os.MkdirAll(filepath.Join(outdir, "profiles"), 0o777) != nil || os.MkdirAll(filepath.Join(outdir, "git-identities"), 0o777) != nil {
		return u.die("Could not prepare export structure")
	}
	if isRegular(ctxconfig.Path()) {
		if err := ctxconfig.CopyFile(ctxconfig.Path(), filepath.Join(outdir, "config")); err != nil {
			u.cpFailed(err)
		}
	}
	copyGlob(filepath.Join(ctxconfig.Dir(), "profiles"), func(n string) bool { return strings.HasSuffix(n, ".conf") }, filepath.Join(outdir, "profiles"))
	copyGlob(filepath.Join(home, ".config", "git"), func(n string) bool { return strings.HasPrefix(n, "ctx-") }, filepath.Join(outdir, "git-identities"))
	if isRegular(sshConfigPath()) {
		if err := ctxconfig.CopyFile(sshConfigPath(), filepath.Join(outdir, "ctx_ssh_config")); err != nil {
			u.cpFailed(err)
		}
	}
	host, _ := os.Hostname()
	manifest := "ctx_export_version=1\n" +
		"created_at=" + time.Now().UTC().Format("2006-01-02T15:04:05Z") + "\n" +
		"created_host=" + host + "\n" +
		"includes_secrets=no\n" +
		"notes=Secrets are intentionally excluded. Use secret backend migration or manual re-entry.\n"
	if err := os.WriteFile(filepath.Join(outdir, "manifest.env"), []byte(manifest), 0o666); err != nil {
		u.error(err.Error())
	}
	u.success("Config exported to: " + outdir)
	u.warn("Secrets were NOT exported.")
	u.info("Move this directory to the new laptop securely (encrypted transfer).")
	return 0
}

func isRegular(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func configImport(u ui, indir string) int {
	home := os.Getenv("HOME")
	if st, err := os.Stat(indir); err != nil || !st.IsDir() {
		return u.die("Import directory not found: " + indir)
	}
	if !isRegular(filepath.Join(indir, "manifest.env")) {
		u.warn("No manifest found in import directory (continuing).")
	}

	_ = ctxconfig.BackupFile(ctxconfig.Path())
	_ = ctxconfig.BackupFile(sshConfigPath())
	_ = ctxconfig.BackupFile(filepath.Join(home, ".gitconfig"))

	if src := filepath.Join(indir, "config"); isRegular(src) {
		err := ctxconfig.WithLock(func() error { return ctxconfig.CopyFile(src, ctxconfig.Path()) })
		if err != nil {
			u.cpFailed(err)
		}
	}
	profilesDir := filepath.Join(ctxconfig.Dir(), "profiles")
	gitDir := filepath.Join(home, ".config", "git")
	_ = os.MkdirAll(profilesDir, 0o777)
	_ = os.MkdirAll(gitDir, 0o777)
	copyGlob(filepath.Join(indir, "profiles"), func(n string) bool { return strings.HasSuffix(n, ".conf") }, profilesDir)
	copyGlob(filepath.Join(indir, "git-identities"), func(n string) bool { return strings.HasPrefix(n, "ctx-") }, gitDir)
	if src := filepath.Join(indir, "ctx_ssh_config"); isRegular(src) {
		if err := ctxconfig.CopyFile(src, sshConfigPath()); err != nil {
			u.cpFailed(err)
		}
	}

	ensureSSHInclude()

	ents, _ := os.ReadDir(profilesDir)
	var confs []string
	for _, e := range ents {
		if n := e.Name(); strings.HasSuffix(n, ".conf") && !strings.HasPrefix(n, ".") {
			confs = append(confs, n)
		}
	}
	sort.Strings(confs)
	for _, n := range confs {
		conf := filepath.Join(profilesDir, n)
		if st, err := os.Stat(conf); err != nil || !st.Mode().IsRegular() {
			continue
		}
		identity := filepath.Join(gitDir, "ctx-"+strings.TrimSuffix(n, ".conf"))
		workDir := sourcedWorkDir(conf)
		if workDir != "" && isRegular(identity) {
			gitconfigAddInclude(workDir, identity)
		}
	}

	u.success("Config imported from: " + indir)
	u.warn("Secrets are not included in exports; rehydrate via your provider (keychain/file/pass) or 'ctx secret'.")
	return 0
}
