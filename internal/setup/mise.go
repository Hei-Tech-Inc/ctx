package setup

import (
	"fmt"
	"strings"
)

// tomlEscape is _ctx_toml_escape: the body of a TOML basic string. Byte-wise
// (LC_ALL=C): \ and " get a backslash, bytes 0x00-0x1F and 0x7F become \uXXXX
// (uppercase hex), everything else (UTF-8 included) passes through.
func tomlEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			b.WriteString(`\\`)
		case c == '"':
			b.WriteString(`\"`)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// tomlComment is _ctx_toml_comment_text (R15): control bytes become a space,
// so a value cannot end a comment line; then tomlEscape.
func tomlComment(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c < 0x20 || c == 0x7f {
			b[i] = ' '
		}
	}
	return tomlEscape(string(b))
}

// tomlShDQ is _ctx_toml_sh_dq: a value for inside "…" in a hook script (which
// is itself in a TOML """ string): \ " $ ` get a shell backslash, then tomlEscape.
func tomlShDQ(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '\\' || c == '"' || c == '$' || c == '`' {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return tomlEscape(b.String())
}

// ifsFields splits like an unquoted $var with the default IFS (space, tab, newline).
func ifsFields(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' })
}

// MiseTOML is the file generate_mise_toml writes, byte for byte, for its
// arguments (profile work_dir git_name git_email aws azure_sub gcp_project
// kube_ctx secret_keys extra_envs, without work_dir). secretKeys is
// space-separated; extraEnvs is KEY=value lines.
func MiseTOML(profile, gitName, gitEmail, aws, azureSub, gcp, kube, secretKeys, extraEnvs string) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	line("# mise.toml — ctx managed environment for: " + tomlComment(profile))
	line("# Edit freely. This file is safe to commit (no secrets here).")
	line("# Secrets load in [hooks.enter]: macOS Keychain, or ~/.ctx/secrets on other OSes.")
	line("")
	line("[env]")
	for _, kv := range [][2]string{{"AWS_PROFILE", aws}, {"AZURE_SUBSCRIPTION", azureSub}, {"GCLOUD_PROJECT", gcp}, {"CTX_KUBE_CONTEXT", kube}} {
		if kv[1] != "" {
			line(kv[0] + ` = "` + tomlEscape(kv[1]) + `"`)
		}
	}
	if extraEnvs != "" {
		// while IFS= read -r pair; do … done <<< "$extra_envs"
		for _, pair := range strings.Split(extraEnvs, "\n") {
			if pair == "" {
				continue
			}
			k, v := pair, pair // ${pair%%=*} and ${pair#*=} leave a pair without '=' unchanged
			if i := strings.IndexByte(pair, '='); i >= 0 {
				k, v = pair[:i], pair[i+1:]
			}
			if !ValidEnvKey(k) {
				continue
			}
			line(k + ` = "` + tomlEscape(v) + `"`)
		}
	}
	line("")
	line("# Uncomment to load a local secrets file (add it to .gitignore)")
	line(`# _.file = ".env.local"`)
	line("")
	line("[hooks.enter]")
	line(`shell = "bash"`)
	line(`script = """`)
	line("  # Set git identity for this directory")
	if SensibleEmail(gitEmail) {
		line(`  git config user.name  "` + tomlShDQ(gitName) + `"`)
		line(`  git config user.email "` + tomlShDQ(gitEmail) + `"`)
	} else {
		line(`  # Skipped invalid git email (looks like a username, not an address): "` + tomlComment(gitEmail) + `"`)
		line("  # Fix GIT_EMAIL in ~/.ctx/profiles/" + tomlComment(profile) + ".conf and re-run setup, or edit mise.toml.")
	}
	line("")
	if azureSub != "" {
		line(`  az account set --subscription "` + tomlShDQ(azureSub) + `" 2>/dev/null || true`)
	}
	if gcp != "" {
		line(`  gcloud config set project "` + tomlShDQ(gcp) + `" --quiet 2>/dev/null || true`)
	}
	if kube != "" {
		line(`  kubectl config use-context "` + tomlShDQ(kube) + `" 2>/dev/null || true`)
	}
	keys := ifsFields(secretKeys)
	if secretKeys != "" {
		line("")
		line("  # Load profile secrets")
		line(`  _ctx_sp="${CTX_SECRET_PROVIDER:-}"`)
		line(`  if [[ -z "$_ctx_sp" && -f "${CTX_DIR:-$HOME/.ctx}/config" ]]; then`)
		line(`    _ctx_sp=$(grep '^secret_provider=' "${CTX_DIR:-$HOME/.ctx}/config" 2>/dev/null | tail -1 | cut -d= -f2-)`)
		line("  fi")
		line(`  [[ -z "$_ctx_sp" ]] && _ctx_sp="auto"`)
		line(`  if [[ "$_ctx_sp" == "auto" ]]; then`)
		line(`    [[ "$(uname -s)" == "Darwin" ]] && _ctx_sp="keychain" || _ctx_sp="file"`)
		line("  fi")
		for _, key := range keys {
			if !ValidEnvKey(key) {
				continue
			}
			line(`  if [[ "$_ctx_sp" == "keychain" ]]; then`)
			line(`    _val=$(security find-generic-password -a "$USER" -s "ctx-` + profile + "-" + key + `" -w 2>/dev/null || true)`)
			line(`  elif [[ "$_ctx_sp" == "pass" ]]; then`)
			line(`    _val=$(pass show "ctx/` + profile + "/" + key + `" 2>/dev/null | sed -n '1p' || true)`)
			line("  else")
			line(`    _f="${CTX_DIR:-$HOME/.ctx}/secrets/` + profile + "/" + key + `"`)
			line(`    [[ -f "$_f" ]] && _val=$(cat "$_f") || _val=""`)
			line("  fi")
			line(`  [[ -n "$_val" ]] && declare -x ` + key + `="$_val"`)
		}
	}
	line(`"""`)
	line("")
	line("[hooks.leave]")
	line(`shell = "bash"`)
	line(`script = """`)
	line("  git config --unset user.name  2>/dev/null || true")
	line("  git config --unset user.email 2>/dev/null || true")
	for _, key := range keys {
		if ValidEnvKey(key) {
			line("  unset " + key + " 2>/dev/null || true")
		}
	}
	line(`"""`)
	return b.String()
}
