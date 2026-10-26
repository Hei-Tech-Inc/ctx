package cli

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/gitssh"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

const (
	autoswitchBegin = "# BEGIN ctx profile autoswitch"
	autoswitchEnd   = "# END ctx profile autoswitch"
)

// InstallHook runs `ctx install-hook [RC]`, mirroring cmd_install_hook in
// lib/cmd_ops.sh (R2, R1). RC defaults by $SHELL (*/zsh, */bash, */fish). It
// adds the mise activation (when mise is on PATH and RC has none; the line
// follows RC's name: config.fish → fish, *zsh* → zsh, else bash) and the ctx
// autoswitch block, which sources ctx_autoswitch.* from $CTX_LIB, then makes
// sure ~/.ssh/config includes ctx_config.
//
// Every check comes before any change, in Bash's order: the autoswitch lib in
// CTX_LIB (unset: refused, no guessing), RC not a directory, readable,
// writable (or new, in an existing dir), and every BEGIN line with an END
// (none nested). Historic full pastes are removed by their exact header (R19).
// RC is backed up, then rewritten once through writeThroughMode (symlinks and
// mode kept; a new file gets the umask default). Kept lines keep their line
// endings, CRLF too; the appended lines end in LF. --dry-run (CTX_DRY_RUN=1)
// prints what it would do and changes nothing. Ctrl+C prints the cancel
// line and exits 130 (OnInterrupt), removing a temp file beside the rc.
func InstallHook(t tui.Term, args []string) int {
	defer OnInterrupt(t, nil)()
	return installHook(t, args)
}

// installHook is InstallHook without its interrupt handler, for init (which
// has its own).
func installHook(t tui.Term, args []string) int {
	u := termUI(t)
	home := os.Getenv("HOME")
	rc := ""
	if len(args) > 0 {
		rc = args[0]
	}
	if rc == "" {
		sh := os.Getenv("SHELL")
		switch {
		case strings.HasSuffix(sh, "/zsh"):
			rc = home + "/.zshrc"
		case strings.HasSuffix(sh, "/bash"):
			rc = home + "/.bashrc"
		case strings.HasSuffix(sh, "/fish"):
			rc = home + "/.config/fish/config.fish"
		default:
			return u.die("Unknown shell. Pass rc file: ctx install-hook ~/.zshrc")
		}
	}

	kind := rcShell(rc)
	lib := os.Getenv("CTX_LIB")
	if lib == "" {
		return u.die("CTX_LIB is not set, so the ctx lib dir is unknown; run this through ctx: ctx install-hook")
	}
	libfile := "ctx_autoswitch.bash"
	if kind == "fish" {
		libfile = "ctx_autoswitch.fish"
	}
	if !isRegular(lib + "/" + libfile) {
		return u.die("Missing " + lib + "/" + libfile + " — reinstall ctx.")
	}

	if isDir(rc) {
		return u.die(rc + " is a directory")
	}
	exists := false
	if _, err := os.Stat(rc); err == nil {
		exists = true
		if !isRegular(rc) || syscall.Access(rc, 4) != nil { // R_OK
			return u.die("Cannot read " + rc)
		}
	}
	target := followLinks(rc)
	if st, err := os.Lstat(target); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return u.die("Cannot write " + rc) // a link loop
	}
	if _, err := os.Stat(target); err == nil {
		if syscall.Access(target, 2) != nil { // W_OK
			return u.die("Cannot write " + rc)
		}
	} else if !isDir(bashDirname(target)) {
		return u.die("Cannot write " + rc)
	}

	var data []byte
	var buf bytes.Buffer
	if exists {
		var err error
		if data, err = os.ReadFile(rc); err != nil {
			return u.die("Cannot read " + rc)
		}
		kept, ok := stripAutoswitch(data)
		if !ok {
			return u.die("Unterminated ctx block in " + rc + "; add the line '" + autoswitchEnd + "' or remove the '" + autoswitchBegin + "' line")
		}
		buf.Write(kept)
	}

	miseLine := ""
	if !bytes.Contains(data, []byte("mise activate")) {
		if _, err := exec.LookPath("mise"); err == nil {
			switch kind {
			case "zsh":
				miseLine = `eval "$(mise activate zsh)"`
			case "fish":
				miseLine = "mise activate fish | source"
			default:
				miseLine = `eval "$(mise activate bash)"`
			}
		}
	}

	sshConf := sshConfigPath()
	if FlagsFromEnv().DryRun {
		if exists {
			u.dim("  [dry-run] would back up " + rc)
		}
		if miseLine != "" {
			u.dim("  [dry-run] would add mise activation to " + rc)
		}
		u.dim("  [dry-run] would refresh the ctx profile autoswitch hook in " + rc)
		if b, _ := os.ReadFile(home + "/.ssh/config"); !bytes.Contains(b, []byte("Include "+sshConf)) {
			u.dim("  [dry-run] would add Include " + sshConf + " to " + home + "/.ssh/config")
		}
		return 0
	}

	if ctxconfig.BackupFile(rc) != nil {
		u.warnErr("Could not back up " + rc)
	}
	if miseLine != "" {
		buf.WriteString("\n# mise-en-place — added by ctx\n" + miseLine + "\n")
	}
	buf.WriteString(autoswitchBlock(kind, lib))
	if err := writeThroughMode(target, buf.Bytes(), 0o666&^currentUmask()); err != nil {
		return u.die("Cannot write " + rc)
	}
	if miseLine != "" {
		u.success("mise activation added to " + rc)
	}
	u.success("ctx profile autoswitch hook refreshed in " + rc + " (sources lib)")

	if gitssh.EnsureInclude(u.warnErr) {
		u.success("SSH config isolated to " + sshConf)
	}
	u.info("Reload your shell: source " + rc)
	return 0
}

// rcShell is _ctx_rc_shell: the shell an rc file is for, from its name.
func rcShell(rc string) string {
	switch {
	case rc == "config.fish" || strings.HasSuffix(rc, "/config.fish"):
		return "fish"
	case strings.Contains(bashBasename(rc), "zsh"):
		return "zsh"
	}
	return "bash"
}

// The headers of the full hook pastes ctx made from 970aa1a to a65d9a9 (the
// whole ctx_autoswitch.bash or .fish appended to the rc), and of the pastes
// before them (62d7522 to 970aa1a: install-hook's and install.sh's), exactly
// as ctx wrote them; git history has no other.
const (
	pasteHeaderBash   = "# ctx profile autoswitch — appended to shell rc by install.sh / ctx install-hook"
	pasteHeaderFish   = "# ctx profile autoswitch — fish (see lib/ctx_autoswitch.bash for behavior)"
	legacyHeaderHook  = "# ── ctx auto-switch ───────────────────────────────────────────────────────────"
	legacyHeaderInst  = "# ── ctx auto-switch — added by ctx installer ─────────────────────────────────"
	pasteLastLineFish = "_ctx_profile_autoswitch"
)

// The line two above a bash paste's final "fi" in every historic version
// (970aa1a has the second, the others the first).
const (
	pastePromptBash    = `  PROMPT_COMMAND="_ctx_profile_autoswitch${PROMPT_COMMAND:+;$PROMPT_COMMAND}"`
	pastePromptBashOld = `  PROMPT_COMMAND="${PROMPT_COMMAND//_ctx_auto_switch;/}"`
)

// pasteEnd is _ctx_rc_paste_end: which full paste (bash or fish) ends at line
// l, given the two lines before it (p1 right before), or "". Every historic
// bash paste ends with one of the PROMPT_COMMAND lines above,
// "  _ctx_profile_autoswitch", then "fi"; every fish paste ends "end", maybe
// a blank line, then "_ctx_profile_autoswitch". A user's own `fi` elsewhere
// is not such an end.
func pasteEnd(p2, p1, l string) string {
	switch {
	case l == "fi" && p1 == "  _ctx_profile_autoswitch" && (p2 == pastePromptBash || p2 == pastePromptBashOld):
		return "bash"
	case l == pasteLastLineFish && (p1 == "end" || p1 == "" && p2 == "end"):
		return "fish"
	}
	return ""
}

// stripAutoswitch is _ctx_rc_strip_autoswitch: data's lines without
//   - the BEGIN..END blocks (a lone END line is dropped too);
//   - a full hook paste (exact header above) through its end (pasteEnd),
//     when such an end comes later in the file;
//   - other legacy pastes (one of the exact headers above) up to their
//     closing "# ─…" rule of 25 or more, or up to, not including, the next
//     blank line, whichever comes first (with neither, to the end of the file).
//
// Lines are matched without a trailing CR and kept as they were; every kept
// line ends in a newline. ok is false when a BEGIN has no END or a BEGIN
// comes inside an open BEGIN block.
func stripAutoswitch(data []byte) (out []byte, ok bool) {
	s := strings.TrimSuffix(string(data), "\n")
	var lines []string
	if len(data) > 0 {
		lines = strings.Split(s, "\n")
	}
	// Lookahead: the last line where a full paste of each kind could end.
	lastEnd := map[string]int{"bash": -1, "fish": -1}
	p1, p2 := "", ""
	for i, line := range lines {
		l := strings.TrimSuffix(line, "\r")
		if k := pasteEnd(p2, p1, l); k != "" {
			lastEnd[k] = i
		}
		p2, p1 = p1, l
	}
	var b bytes.Buffer
	state, pend := "", ""
	p1, p2 = "", ""
	for i, line := range lines {
		l := strings.TrimSuffix(line, "\r")
		prev2, prev1 := p2, p1
		p2, p1 = p1, l
		switch state {
		case "begin":
			if strings.HasPrefix(l, autoswitchBegin) {
				return nil, false
			}
			if strings.HasPrefix(l, autoswitchEnd) {
				state = ""
			}
			continue
		case "paste":
			if pasteEnd(prev2, prev1, l) == pend {
				state = ""
			}
			continue
		case "legacy":
			switch {
			case strings.HasPrefix(l, autoswitchBegin):
				state = "begin"
			case strings.HasPrefix(l, autoswitchEnd):
				state = ""
			case l == "":
				state = ""
				b.WriteString(line + "\n")
			case isRcRule(l):
				state = ""
			}
			continue
		}
		switch {
		case l == pasteHeaderBash && lastEnd["bash"] > i:
			state, pend = "paste", "bash"
		case l == pasteHeaderFish && lastEnd["fish"] > i:
			state, pend = "paste", "fish"
		case strings.HasPrefix(l, autoswitchBegin):
			state = "begin"
		case strings.HasPrefix(l, autoswitchEnd):
		case l == pasteHeaderBash, l == pasteHeaderFish, l == legacyHeaderHook, l == legacyHeaderInst:
			state = "legacy"
		default:
			b.WriteString(line + "\n")
		}
	}
	return b.Bytes(), state != "begin"
}

// isRcRule is _ctx_rc_is_rule: "# " and 25 or more ─, nothing else.
func isRcRule(l string) bool {
	r, ok := strings.CutPrefix(l, "# ")
	return ok && strings.HasPrefix(r, strings.Repeat("─", 25)) && strings.ReplaceAll(r, "─", "") == ""
}

// autoswitchForms reports the ctx autoswitch hook forms in rc text: activate,
// an `eval "$(ctx activate <shell>)"` (or ctx-core path) line; lib, the
// install-hook block or an uncommented line mentioning lib/ctx_autoswitch.*
// (taken as sourcing it, which with ctx-core installed runs ctx activate);
// paste, without lib, an old inline paste of the lib (no shim: always the
// legacy hook).
// Bash twin: _ctx_rc_autoswitch_forms.
func autoswitchForms(text string) (activate, lib, paste bool) {
	activate = strings.Contains(text, "ctx activate ") || strings.Contains(text, "ctx-core activate ")
	lib = strings.Contains(text, autoswitchBegin)
	for _, l := range strings.Split(text, "\n") {
		// grep -E '^[[:space:]]*([^#[:space:]].*)?ctx_autoswitch\.'
		if l = strings.TrimLeft(l, " \t\v\f\r"); !strings.HasPrefix(l, "#") && strings.Contains(l, "ctx_autoswitch.") {
			lib = true
		}
	}
	paste = !lib && strings.Contains(text, "_ctx_profile_autoswitch")
	return activate, lib, paste
}

// autoswitchBlock is _ctx_autoswitch_block: the block install-hook appends,
// byte for byte, sourcing ctx_autoswitch.* from lib (or a standard lib dir).
func autoswitchBlock(kind, lib string) string {
	var b strings.Builder
	b.WriteString("\n" + autoswitchBegin + "\n")
	b.WriteString("# Sourced from ctx lib so upgrades (notify mute, etc.) apply without editing this file.\n")
	if kind == "fish" {
		b.WriteString("set -l _ctx_as_lib ''\n" +
			"for _ctx_as_cand in '" + lib + "' $HOME/.local/lib/ctx /usr/local/lib/ctx /opt/homebrew/lib/ctx\n" +
			"  if test -f \"$_ctx_as_cand/ctx_autoswitch.fish\"\n" +
			"    set _ctx_as_lib $_ctx_as_cand\n" +
			"    break\n" +
			"  end\n" +
			"end\n" +
			"if test -n \"$_ctx_as_lib\"\n" +
			"  source \"$_ctx_as_lib/ctx_autoswitch.fish\"\n" +
			"end\n")
	} else {
		b.WriteString("_ctx_as_lib=\"\"\n" +
			"for _ctx_as_cand in \"" + lib + "\" \"${HOME}/.local/lib/ctx\" \"/usr/local/lib/ctx\" \"/opt/homebrew/lib/ctx\"; do\n" +
			"  [[ -f \"${_ctx_as_cand}/ctx_autoswitch.bash\" ]] && { _ctx_as_lib=\"${_ctx_as_cand}\"; break; }\n" +
			"done\n" +
			"if [[ -n \"${_ctx_as_lib}\" ]]; then\n" +
			"  # shellcheck disable=SC1090\n" +
			"  source \"${_ctx_as_lib}/ctx_autoswitch.bash\"\n" +
			"fi\n" +
			"unset _ctx_as_lib _ctx_as_cand 2>/dev/null || true\n")
	}
	b.WriteString(autoswitchEnd + "\n")
	return b.String()
}
