package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// ttyColors reports whether w is a terminal; Bash colors only then (`[[ -t 1 ]]`).
func ttyColors(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(st, null) {
		return false
	}
	return true
}

// echoE returns what `echo -e` prints for s (see tui.EchoE).
func echoE(s string) string { return tui.EchoE(s) }

// echoPlain returns what `echo "$s"` prints: a value that is only echo
// options (-n, -e, -E, in any combination) is swallowed by the builtin.
func echoPlain(s string) string {
	if len(s) > 1 && s[0] == '-' && strings.Trim(s[1:], "neE") == "" {
		if strings.Contains(s, "n") {
			return ""
		}
		return "\n"
	}
	return s + "\n"
}

const (
	ansiGreen = "\033[0;32m"
	ansiCyan  = "\033[0;36m"
	ansiRed   = "\033[0;31m"
	ansiBold  = "\033[1m"
	ansiReset = "\033[0m"
)

// errCancelled is secretCmd's result when the user aborts a prompt.
var errCancelled = errors.New("cancelled")

// SecretIn runs `ctx secret set|get|list|delete|migrate`, mirroring cmd_secret in
// lib/cmd_ops.sh. Output goes to t.Out, errors and prompts to t.Err; the result
// is the exit code. A cancelled prompt exits 130 (see tui.Term.Cancelled).
func SecretIn(t tui.Term, args []string) int {
	err := secretCmd(t, args)
	if err == nil {
		return 0
	}
	if errors.Is(err, errCancelled) {
		return t.Cancelled()
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		fmt.Fprintln(t.Err, ee.Msg)
		return ee.Code
	}
	fmt.Fprintf(t.Err, "\u2717 %v\n", err)
	return 1
}

func secretCmd(t tui.Term, args []string) error {
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	sub, prof, key := arg(0), arg(1), arg(2)
	color := func(c string) string {
		if t.Colors {
			return c
		}
		return ""
	}
	say := func(mark, c, msg string) error {
		_, err := io.WriteString(t.Out, echoE(color(c)+mark+color(ansiReset)+" "+msg))
		return err
	}
	success := func(msg string) error { return say("\u2713", ansiGreen, msg) }
	warn := func(msg string) error { return say("!", ansiYellow, msg) }
	// die prints like Bash's error: red mark on stderr, colored by stdout's TTY.
	die := func(msg string) error {
		return &ExitError{Code: 1, Msg: strings.TrimSuffix(echoE(color(ansiRed)+"\u2717"+color(ansiReset)+" "+msg), "\n")}
	}
	switch sub {
	case "set":
		if prof == "" || key == "" {
			return die("Usage: ctx secret set <profile> <KEY>")
		}
		if !profileExists(prof) {
			return die(fmt.Sprintf("Profile '%s' not found.", prof))
		}
		val, err := t.Prompter().AskSecret("Value for " + key)
		if err != nil {
			return errCancelled
		}
		if !t.Rich {
			// Bash's ask_secret ends in `echo "$var"`: a value that is only echo
			// options vanishes, and $(...) drops trailing newlines.
			val = strings.TrimRight(echoPlain(val), "\n")
		}
		label := secret.StoreLabel()
		st, err := secret.Open(secret.EffectiveProvider())
		if err == nil {
			err = st.Set(prof, key, val)
		}
		if err != nil {
			return die(fmt.Sprintf("Failed to store secret using provider '%s'. Check provider config and required CLI tools.", label))
		}
		return success(key + " stored (" + label + ") for " + prof)
	case "migrate":
		return secretMigrate(t, arg(1), arg(2), arg(3), die, success, warn)
	case "get":
		if prof == "" || key == "" {
			return die("Usage: ctx secret get <profile> <KEY>")
		}
		val, err := secret.ProviderGetter{}.Get(prof, key)
		if err != nil {
			return die(fmt.Sprintf("No secret '%s' for profile '%s'", key, prof))
		}
		_, err = io.WriteString(t.Out, echoPlain(val))
		return err
	case "list":
		if prof == "" {
			return die("Usage: ctx secret list <profile>")
		}
		if !profileExists(prof) {
			return die(fmt.Sprintf("Profile '%s' not found.", prof))
		}
		var b strings.Builder
		b.WriteString(echoE(color(ansiBold) + "\n  Secrets for: " + prof + "\n" + color(ansiReset)))
		var keys []string
		if st, err := secret.Open(secret.EffectiveProvider()); err == nil {
			keys, _ = st.List(prof)
		}
		var shown []string
		for _, k := range keys {
			if k != "" {
				shown = append(shown, k)
			}
		}
		if len(shown) == 0 {
			b.WriteString(echoE(color(ansiCyan) + "→" + color(ansiReset) + " No secrets stored for this profile."))
		}
		for _, k := range shown {
			b.WriteString(echoE("  " + color(ansiGreen) + "•" + color(ansiReset) + " " + k))
		}
		b.WriteString("\n")
		_, err := io.WriteString(t.Out, b.String())
		return err
	case "delete":
		if prof == "" || key == "" {
			return die("Usage: ctx secret delete <profile> <KEY>")
		}
		label := secret.StoreLabel()
		st, err := secret.Open(secret.EffectiveProvider())
		if err == nil {
			err = st.Delete(prof, key)
		}
		if err != nil {
			return die(fmt.Sprintf("Failed to delete secret using provider '%s'. Check provider config and required CLI tools.", label))
		}
		return success("Deleted " + key + " from " + label)
	}
	return fmt.Errorf("secret: unsupported subcommand %q", sub)
}

// secretMigrate mirrors the `migrate` branch of cmd_secret: copy every secret of
// the profile from one provider to another, then offer to delete the originals.
func secretMigrate(t tui.Term, prof, to, from string, die func(string) error, success, warn func(string) error) error {
	const usage = "Usage: ctx secret migrate <profile> <to-provider> [from-provider]"
	if prof == "" {
		return die(usage)
	}
	if !profileExists(prof) {
		return die(fmt.Sprintf("Profile '%s' not found.", prof))
	}
	if to == "" {
		return die(usage)
	}
	if from == "" {
		from = secret.EffectiveProvider()
	}
	to, from = secret.AsciiLower(to), secret.AsciiLower(from)
	valid := func(p string) bool {
		return p == secret.ProviderKeychain || p == secret.ProviderFile || p == secret.ProviderPass
	}
	if !valid(to) {
		return die("Invalid to-provider: " + to)
	}
	if !valid(from) {
		return die("Invalid from-provider: " + from)
	}
	if to == from {
		return die("Source and destination providers are the same: " + to)
	}

	src, _ := secret.Open(from)
	var keys []string
	if all, err := src.List(prof); err == nil {
		for _, k := range all {
			if k != "" {
				keys = append(keys, k)
			}
		}
	}
	if len(keys) == 0 {
		return die(fmt.Sprintf("No secrets found for '%s' in provider '%s'.", prof, from))
	}
	dst, dstErr := secret.OpenWith(to)
	var copiedKeys []string
	missing := 0
	for _, k := range keys {
		v, _ := src.Get(prof, k)
		if v == "" {
			missing++
			continue
		}
		if dstErr == nil && dst.Set(prof, k, v) == nil {
			copiedKeys = append(copiedKeys, k)
		}
	}
	copied := len(copiedKeys)
	if err := success(fmt.Sprintf("Migrated %d secret(s) for '%s' from %s to %s.", copied, prof, from, to)); err != nil {
		return err
	}
	if missing > 0 {
		if err := warn(fmt.Sprintf("%d key(s) were empty/unreadable and skipped.", missing)); err != nil {
			return err
		}
	}
	if copied == 0 {
		return nil // nothing was copied, so there is nothing to offer to delete
	}
	del, err := t.Prompter().AskYN(fmt.Sprintf("Delete migrated keys from source provider '%s'?", from), false)
	if err != nil {
		return errCancelled
	}
	if del {
		for _, k := range copiedKeys {
			_ = src.Delete(prof, k)
		}
		return success(fmt.Sprintf("Removed source keys from '%s'.", from))
	}
	return nil
}

func profileExists(name string) bool {
	st, err := os.Stat(profile.Dir() + "/" + name + ".conf")
	return err == nil && st.Mode().IsRegular()
}

// EchoE is echoE for packages that print Bash-style messages.
func EchoE(s string) string { return echoE(s) }

// TTYColors is ttyColors for packages that print Bash-style messages.
func TTYColors(w io.Writer) bool { return ttyColors(w) }
