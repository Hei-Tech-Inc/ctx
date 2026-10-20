#!/usr/bin/env bash
# ctx/lib/core.sh — v3: adds mise.toml support, safe isolated SSH config

CTX_VERSION="3.2.0"
CTX_DIR="${CTX_DIR:-$HOME/.ctx}"
CTX_CONFIG="${CTX_DIR}/config"
CTX_PROFILES_DIR="${CTX_DIR}/profiles"
CTX_LOG="${CTX_DIR}/ctx.log"
CTX_SSH_CONFIG="${HOME}/.ssh/ctx_config"

# ─── Colors ───────────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
  RED='\033[0;31m' GREEN='\033[0;32m' YELLOW='\033[0;33m'
  CYAN='\033[0;36m' DIM='\033[2m' BOLD='\033[1m' RESET='\033[0m'
else
  RED='' GREEN='' YELLOW='' CYAN='' DIM='' BOLD='' RESET=''
fi

info()    { echo -e "${CYAN}→${RESET} $*"; }
success() { echo -e "${GREEN}✓${RESET} $*"; }
warn()    { echo -e "${YELLOW}!${RESET} $*"; }
error()   { echo -e "${RED}✗${RESET} $*" >&2; }
dim()     { echo -e "${DIM}$*${RESET}"; }
bold()    { echo -e "${BOLD}$*${RESET}"; }
die()     { error "$*"; exit 1; }
hr()      { echo -e "${DIM}────────────────────────────────────────${RESET}"; }
log()     { mkdir -p "$CTX_DIR"; echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" >> "$CTX_LOG"; }
user_cancelled() {
  # Print to stderr so command substitutions never capture this as field input.
  echo -e "${YELLOW}!${RESET} Cancelled by user." >&2
  # If we're inside command substitution, exit only aborts the subshell.
  # Signal the main ctx shell ($$ is its PID even in a subshell; bin/ctx traps
  # INT and exits 130). $PPID would be ctx's caller.
  if [[ ${BASH_SUBSHELL:-0} -gt 0 ]]; then
    kill -INT "$$" 2>/dev/null || true
  fi
  exit 130
}

# ─── gum or plain fallback ────────────────────────────────────────────────────
# All prompts go through these wrappers.
# If gum is installed: beautiful TUI. If not: plain read/echo.

HAS_GUM=false
if command -v gum &>/dev/null && [[ -t 0 && -t 1 ]]; then
  HAS_GUM=true
fi

# Styled header — uses gum style if available
ctx_header() {
  if $HAS_GUM; then
    gum style \
      --border rounded --border-foreground 99 \
      --padding "0 2" --margin "1 0" \
      --bold "ctx v${CTX_VERSION} — $*"
  else
    bold "\n  ctx v${CTX_VERSION} — $*\n"
  fi
}

# Text input — gum input or read
# Usage: ask "Prompt text" [default] [placeholder_when_empty]
# With gum: default becomes visible --value; characters show as you type. TUI draws on stderr — never hide stderr.
ask() {
  local prompt="$1" default="${2:-}" empty_hint="${3:-}"
  if $HAS_GUM; then
    local result
    echo -e "${DIM}  Your text appears after > as you type. Enter confirms, Ctrl+C cancels. (? for gum shortcuts)${RESET}" >&2
    local -a _gin=(
      gum input
      --header "$prompt"
      --header.foreground 99
      --prompt "> "
      --prompt.foreground 99
      --placeholder.foreground 245
      --cursor.foreground 99
      --cursor.mode blink
      --width 72
      --show-help
    )
    if [[ -n "$default" ]]; then
      _gin+=(--value "$default" --placeholder "${empty_hint:-Press Enter to keep, or edit}")
    else
      _gin+=(--placeholder "${empty_hint:-Type here, then Enter}")
    fi
    result=$("${_gin[@]}" < /dev/tty) || user_cancelled
    echo "${result:-$default}"
  else
    if [[ -n "$default" ]]; then
      echo -e "${CYAN}?${RESET} $prompt ${DIM}[$default]${RESET}: \c" >&2
    else
      echo -e "${CYAN}?${RESET} $prompt: \c" >&2
    fi
    local var
    read -r var || user_cancelled
    echo "${var:-$default}"
  fi
}

# Secret/password input — hidden
ask_secret() {
  local prompt="$1"
  if $HAS_GUM; then
    echo -e "${DIM}  Waiting for secret input — press Enter to confirm (Ctrl+C to cancel).${RESET}" >&2
    gum input --password \
      --placeholder "$prompt" \
      --prompt "> " \
      --prompt.foreground 99 \
      --cursor.foreground 99 \
      --width 60 \
      < /dev/tty || user_cancelled
  else
    echo -e "${CYAN}?${RESET} $prompt ${DIM}(hidden)${RESET}: \c" >&2
    local var
    read -rs var || user_cancelled
    echo "" >&2
    echo "$var"
  fi
}

# Yes/no confirm
# Usage: ask_yn "Question?" [y|n]  — returns 0 for yes, 1 for no
ask_yn() {
  local prompt="$1" default="${2:-y}"
  if $HAS_GUM; then
    local affirm="Yes" deny="No"
    [[ "$default" == "n" ]] && affirm="No" && deny="Yes"
    gum confirm \
      --affirmative "$affirm" --negative "$deny" \
      --prompt.foreground 99 \
      "$prompt (Ctrl+C to cancel)"
    local rc=$?
    [[ $rc -eq 130 ]] && user_cancelled
    return $rc
  else
    local hint; [[ "$default" == "y" ]] && hint="Y/n" || hint="y/N"
    echo -e "${CYAN}?${RESET} $prompt ${DIM}[$hint]${RESET}: \c" >&2
    local answer
    read -r answer || user_cancelled
    answer="${answer:-$default}"
    [[ "$answer" =~ ^[Yy]$ ]]
  fi
}

# Pick one item from a list — gum choose or numbered menu
# Usage: pick_one "prompt" item1 item2 item3
# Prints selected item to stdout, empty string if skipped
pick_one() {
  local prompt="$1"; shift
  local items=("$@")
  [[ ${#items[@]} -eq 0 ]] && echo "" && return 0

  if $HAS_GUM; then
    local result
    # Pass items as argv instead of stdin piping; some terminals can render a blank
    # chooser when fed via pipe, making it look stuck.
    # gum draws its list on stderr; do not redirect stderr or the menu is invisible when stdout is captured.
    result=$(gum choose \
      --height 10 \
      --show-help \
      --header "$prompt (↑/↓ move, Enter picks, ? shortcuts, Ctrl+C cancel)" \
      --header.foreground 99 \
      --cursor.foreground 99 \
      --selected.foreground 99 \
      "${items[@]}" \
      < /dev/tty) || user_cancelled
    echo "$result"
  else
    echo "" >&2
    for i in "${!items[@]}"; do
      echo -e "  ${DIM}$((i+1))${RESET}  ${items[$i]}" >&2
    done
    echo "" >&2
    echo -e "${CYAN}?${RESET} $prompt (1-${#items[@]}, 0 to skip): \c" >&2
    local choice
    read -r choice || user_cancelled
    if [[ "$choice" =~ ^[0-9]+$ ]] \
       && [[ "$choice" -ge 1 ]] \
       && [[ "$choice" -le ${#items[@]} ]]; then
      echo "${items[$((choice-1))]}"
    else
      echo ""
    fi
  fi
}

# Multi-select — gum choose --no-limit or numbered checkboxes
# Usage: pick_many "prompt" item1 item2 ...
# Prints newline-separated selected items
pick_many() {
  local prompt="$1"; shift
  local items=("$@")
  [[ ${#items[@]} -eq 0 ]] && return 0

  if $HAS_GUM; then
    gum choose --no-limit \
      --height 10 \
      --show-help \
      --header "$prompt (Space toggles, Enter confirms, ? shortcuts, Ctrl+C cancel)" \
      --header.foreground 99 \
      --cursor.foreground 99 \
      --selected.foreground 99 \
      "${items[@]}" \
      < /dev/tty || user_cancelled
  else
    echo "" >&2
    for i in "${!items[@]}"; do
      echo -e "  ${DIM}$((i+1))${RESET}  ${items[$i]}" >&2
    done
    echo "" >&2
    echo -e "${CYAN}?${RESET} $prompt (comma-separated numbers, e.g. 1,3): \c" >&2
    local input
    read -r input || user_cancelled
    for n in $(echo "$input" | tr ',' ' '); do
      n=$(echo "$n" | tr -d ' ')
      [[ "$n" =~ ^[0-9]+$ ]] \
        && [[ "$n" -ge 1 ]] \
        && [[ "$n" -le ${#items[@]} ]] \
        && echo "${items[$((n-1))]}"
    done
  fi
}

# Spinner while a command runs
# Usage: spin "message" command args...
spin() {
  local msg="$1"; shift
  if $HAS_GUM; then
    gum spin --title "$msg" --spinner dot -- "$@"
  else
    info "$msg"
    "$@"
  fi
}

# ─── ctx setup wizard (steps + review + optional --config file) ─────────────
# KEY=value config lines; # comments; last KEY= wins. No bash assoc arrays (macOS bash 3 safe).

ctx_setup_cfg_get() {
  local file="$1" key="$2"
  local default="${3:-}"
  [[ -f "$file" ]] || { printf '%s' "$default"; return 0; }
  local line val
  line="$(grep -E "^[[:space:]]*${key}=" "$file" 2>/dev/null | tail -1)" || true
  [[ -z "$line" ]] && { printf '%s' "$default"; return 0; }
  val="${line#*=}"
  val="${val#"${val%%[![:space:]]*}"}"
  val="${val%"${val##*[![:space:]]}"}"
  val="${val%$'\r'}"
  if [[ "${#val}" -ge 2 && "${val:0:1}" == '"' && "${val: -1}" == '"' ]]; then
    val="${val:1:${#val}-2}"
  elif [[ "${#val}" -ge 2 && "${val:0:1}" == "'" && "${val: -1}" == "'" ]]; then
    val="${val:1:${#val}-2}"
  fi
  printf '%s' "${val:-$default}"
}

# Normalise common truthy strings to 1 or 0.
ctx_setup_cfg_truthy() {
  local v
  v=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
  case "$v" in
    1|true|yes|y|on) printf '1' ;;
    *) printf '0' ;;
  esac
}

# Banner for interactive setup (step 1-based).
ctx_setup_step() {
  local step="$1" total="$2" title="$3"
  local why="${4-}"
  hr
  if $HAS_GUM; then
    gum style \
      --border normal --border-foreground 240 \
      --padding "0 1" --margin "0 0" \
      "$(gum style --bold "Step ${step} / ${total} — ${title}")"
  else
    bold "  Step ${step} / ${total} — ${title}"
  fi
  [[ -n "$why" ]] && dim "  ${why}"
  echo ""
}

# Review table + confirm. Sets exit 0 to proceed, 1 to abort (caller calls die).
# Args: auto_yes(0|1) profile git_name git_email work_dir import_existing(0|1) ssh_key gh_user aws az_sub az_ten gcp_proj gcp_acct kube
ctx_setup_review_confirm() {
  local auto_yes="${1:-0}"; shift
  local profile="$1" git_name="$2" git_email="$3" work_dir="$4"
  local import_existing="$5" ssh_key="$6" gh_user="$7"
  local aws_p="$8" az_sub="$9" az_ten="${10}" gcp_proj="${11}" gcp_acct="${12}" kube="${13}"
  if [[ "$(ctx_setup_cfg_truthy "$auto_yes")" == "1" ]]; then
    info "Confirming automatically (--yes or AUTO_CONFIRM in config)."
    return 0
  fi
  local imp_l="Import detected references"
  [[ "$import_existing" == "1" ]] || imp_l="Manual entry only"
  local ssh_d="${ssh_key:-—}"
  [[ -z "$ssh_key" ]] && ssh_d="(none)"

  hr
  bold "  Step 8 / 8 — Review"
  dim "  Nothing is written until you confirm. No = exit without changes."
  echo ""

  if $HAS_GUM; then
    {
      printf '%s\t%s\n' "Profile" "$profile"
      printf '%s\t%s\n' "Git name" "$git_name"
      printf '%s\t%s\n' "Git email" "${git_email:-—}"
      printf '%s\t%s\n' "Work directory" "$work_dir"
      printf '%s\t%s\n' "Config import" "$imp_l"
      printf '%s\t%s\n' "SSH private key" "$ssh_d"
      printf '%s\t%s\n' "GitHub user" "${gh_user:-—}"
      printf '%s\t%s\n' "AWS profile" "${aws_p:-—}"
      printf '%s\t%s\n' "Azure subscription" "${az_sub:-—}"
      printf '%s\t%s\n' "Azure tenant" "${az_ten:-—}"
      printf '%s\t%s\n' "GCP project" "${gcp_proj:-—}"
      printf '%s\t%s\n' "GCP account" "${gcp_acct:-—}"
      printf '%s\t%s\n' "kubectl context" "${kube:-—}"
    } | gum table --print -c "Setting,Value" -w 20,58 --border rounded
  else
    printf '  %-16s %s\n' "Profile" "$profile"
    printf '  %-16s %s\n' "Git name" "$git_name"
    printf '  %-16s %s\n' "Git email" "${git_email:-—}"
    printf '  %-16s %s\n' "Work directory" "$work_dir"
    printf '  %-16s %s\n' "Config import" "$imp_l"
    printf '  %-16s %s\n' "SSH key" "$ssh_d"
    printf '  %-16s %s\n' "GitHub user" "${gh_user:-—}"
    printf '  %-16s %s\n' "AWS profile" "${aws_p:-—}"
    printf '  %-16s %s\n' "Azure sub" "${az_sub:-—}"
    printf '  %-16s %s\n' "Azure tenant" "${az_ten:-—}"
    printf '  %-16s %s\n' "GCP project" "${gcp_proj:-—}"
    printf '  %-16s %s\n' "GCP account" "${gcp_acct:-—}"
    printf '  %-16s %s\n' "kubectl" "${kube:-—}"
  fi
  echo ""
  ask_yn "Write this profile to disk now?" "y"
}

# Shown after setup errors so users know where to diagnose tools / hooks.
ctx_setup_hint_doctor() {
  dim "  Tools, hooks, SSH layout: run ${BOLD}ctx doctor${RESET}${DIM}.${RESET}" >&2
}

# ─── Directory init ────────────────────────────────────────────────────────────
ctx_init_dirs() {
  mkdir -p "$CTX_DIR" "$CTX_PROFILES_DIR" "${CTX_DIR}/backups"
  touch "$CTX_CONFIG"
  chmod 700 "$CTX_DIR" "$CTX_PROFILES_DIR"
}

# ─── Profile helpers ──────────────────────────────────────────────────────────
profile_exists()  { [[ -f "$CTX_PROFILES_DIR/$1.conf" ]]; }

load_profile() {
  local name="$1"
  profile_exists "$name" || die "Profile '$name' not found. Run: ctx list"
  unset PROFILE_NAME GIT_NAME GIT_EMAIL WORK_DIR GITHUB_USER SSH_KEY_PATH
  unset AWS_PROFILE_NAME AZURE_SUBSCRIPTION AZURE_TENANT GCP_PROJECT GCP_ACCOUNT
  unset KUBE_CONTEXT SECRET_KEYS EXTRA_ENVS SIGNING_FORMAT SIGNING_KEY
  # shellcheck source=/dev/null
  source "$CTX_PROFILES_DIR/$name.conf"
}

profile_list() {
  for f in "$CTX_PROFILES_DIR"/*.conf; do
    [[ -e "$f" ]] && basename "$f" .conf
  done
}

active_profile() {
  grep "^active=" "$CTX_CONFIG" 2>/dev/null | cut -d= -f2 || echo ""
}

set_active_profile() {
  local name="$1"
  _ctx_config_upsert_line active "$name"
  log "Switched to profile: $name"
}

# Remove active profile marker and auto-switch manual-lock metadata (~/.ctx/config).
clear_active_profile_activation() {
  ctx_init_dirs
  [[ -f "$CTX_CONFIG" ]] || return 0
  local tmp
  tmp="$(mktemp)" || return 1
  grep -vE '^(active|active_source|manual_pwd)=' "$CTX_CONFIG" > "$tmp" 2>/dev/null || true
  mv "$tmp" "$CTX_CONFIG"
}

# Read WORK_DIR from a profile .conf (safe: file is assignment-only metadata).
ctx_profile_read_work_dir() {
  local conf="$1" wd
  [[ -f "$conf" ]] || return 1
  wd="$(bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "$conf")"
  wd="${wd/#\~/$HOME}"
  [[ "$wd" == "/" ]] || wd="${wd%/}"
  [[ -n "$wd" ]] || return 1
  printf '%s\n' "$wd"
}

# Escape a string for use inside JSON double quotes (no surrounding quotes).
ctx_json_escape() {
  local s="$1" out="" i c
  local -i i
  for ((i = 0; i < ${#s}; i++)); do
    c="${s:i:1}"
    case "$c" in
      \\) out+='\\' ;;
      \") out+='\"' ;;
      $'\n') out+='\n' ;;
      $'\r') out+='\r' ;;
      $'\t') out+='\t' ;;
      *) out+="$c" ;;
    esac
  done
  printf '%s' "$out"
}

# Longest WORK_DIR prefix of pwd under profiles_dir; optional sibling profile by
# first path segment under that prefix; then nearest .ctx (walk pwd → git root).
# Prints: "<profile_name><TAB><display WORK_DIR>" (WORK_DIR may be empty before sync).
ctx_resolve_path_with_root() {
  local pwd="${1:?pwd required}" pdir="${2:?profiles dir required}"
  pwd="${pwd/#\~/$HOME}"
  [[ -d "$pdir" ]] || return 1
  local best_pname="" best_len=0 best_wd="" conf work_dir len
  for conf in "$pdir"/*.conf; do
    [[ -e "$conf" ]] || continue
    work_dir="$(bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "$conf")"
    work_dir="${work_dir/#\~/$HOME}"
    [[ "$work_dir" == "/" ]] || work_dir="${work_dir%/}"
    [[ -z "$work_dir" ]] && continue
    if [[ "$pwd" == "$work_dir" || "$pwd" == "$work_dir/"* ]]; then
      len=${#work_dir}
      if [[ $len -gt $best_len ]]; then
        best_len=$len
        best_pname="$(basename "$conf" .conf)"
        best_wd="$work_dir"
      fi
    fi
  done
  local path_profile="$best_pname" best_work_dir="$best_wd"
  if [[ -n "$best_pname" && -n "$best_work_dir" && "$pwd" == "${best_work_dir}/"* ]]; then
    local rel="${pwd#"${best_work_dir}"/}"
    local first="${rel%%/*}"
    if [[ -n "$first" && -f "${pdir}/${first}.conf" ]]; then
      local wdc
      wdc="$(bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "${pdir}/${first}.conf")"
      wdc="${wdc/#\~/$HOME}"
      [[ "$wdc" == "/" ]] || wdc="${wdc%/}"
      [[ -z "$wdc" ]] && wdc="${best_work_dir%/}/$first"
      if [[ -d "$wdc" && ( "$pwd" == "$wdc" || "$pwd" == "$wdc/"* ) ]]; then
        local cand_len=${#wdc}
        if [[ $cand_len -gt ${#best_work_dir} ]]; then
          path_profile="$first"
          best_work_dir="$wdc"
        fi
      fi
    fi
  fi
  if [[ -n "$path_profile" && -d "$pwd" ]]; then
    local repo_root d parent ov
    repo_root="$(git -C "$pwd" rev-parse --show-toplevel 2>/dev/null || echo "")"
    repo_root="${repo_root%/}"
    if [[ -n "$repo_root" ]]; then
      d="${pwd%/}"
      while true; do
        if [[ -f "$d/.ctx" ]]; then
          ov="$(grep "^profile=" "$d/.ctx" 2>/dev/null | tail -1 | cut -d= -f2-)"
          ov="${ov//$'\r'/}"
          ov="$(printf '%s' "$ov" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
          if [[ -n "$ov" && -f "${pdir}/${ov}.conf" ]]; then
            path_profile="$ov"
            break
          fi
        fi
        if [[ "$d" == "$repo_root" ]]; then
          break
        fi
        parent="${d%/*}"
        [[ "$parent" == "$d" ]] && break
        d="$parent"
      done
    fi
  fi
  local display_wd=""
  if [[ -n "$path_profile" ]]; then
    display_wd="$(bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "${pdir}/${path_profile}.conf")"
    display_wd="${display_wd/#\~/$HOME}"
    [[ "$display_wd" == "/" ]] || display_wd="${display_wd%/}"
    [[ -n "$display_wd" ]] && best_work_dir="$display_wd"
    [[ -z "$display_wd" && -n "$best_work_dir" ]] && display_wd="$best_work_dir"
  fi
  printf '%s\t%s' "${path_profile:-}" "${display_wd:-}"
}

# Prints profile basename (no .conf) or empty. pwd and profile paths are expanded (~ → HOME).
ctx_resolve_path_profile() {
  local line name
  line="$(ctx_resolve_path_with_root "$1" "$2")" || return 1
  name="${line%%$'\t'*}"
  printf '%s' "$name"
}

# Path segments under work_dir (0 = exact match). Prints -1 if pwd is not under work_dir.
ctx_rel_path_depth_under() {
  local p="${1:?pwd}" r="${2:?work dir}"
  p="${p/#\~/$HOME}"
  r="${r/#\~/$HOME}"
  p="${p%/}"
  r="${r%/}"
  [[ "$p" == "$r" ]] && { printf '%s' 0; return 0; }
  [[ "$p" == "$r/"* ]] || { printf '%s' -1; return 0; }
  local x="${p#"${r}"/}"
  [[ -z "$x" ]] && { printf '%s' 0; return 0; }
  if [[ "$x" != */* ]]; then
    printf '%s' 1
    return 0
  fi
  printf '%s' "$(printf '%s\n' "$x" | awk -F/ '{print NF}')"
}

# Default 2: show prompt helpers for WORK_DIR plus up to two path segments below it.
ctx_config_prompt_workdir_max_depth() {
  local v="2"
  ctx_init_dirs
  if [[ -f "$CTX_CONFIG" ]]; then
    v="$(grep "^prompt_workdir_max_depth=" "$CTX_CONFIG" 2>/dev/null | tail -1 | cut -d= -f2-)"
  fi
  v="${v//$'\r'/}"
  v="${v// /}"
  [[ "$v" =~ ^[0-9]+$ ]] || v=2
  printf '%s' "$v"
}

# Colon-separated extra absolute path prefixes for prompt scope (optional).
ctx_config_prompt_extra_paths_or_default() {
  ctx_init_dirs
  local v=""
  if [[ -f "$CTX_CONFIG" ]]; then
    v="$(grep "^prompt_extra_paths=" "$CTX_CONFIG" 2>/dev/null | tail -1 | cut -d= -f2-)"
  fi
  [[ -n "$v" ]] && printf '%s' "$v" || printf '%s' "(none)"
}

# Print 1 if autoswitch should echo [ctx] ←/→ lines; else 0.
# Default off (privacy: screen share / new tabs). Override: CTX_AUTOSWITCH_NOTIFY=1|0
# or ~/.ctx/config autoswitch_notify=on|off|1|0|true|false.
ctx_config_autoswitch_notify() {
  local v=""
  if [[ -n "${CTX_AUTOSWITCH_NOTIFY:-}" ]]; then
    v="$(echo "$CTX_AUTOSWITCH_NOTIFY" | tr '[:upper:]' '[:lower:]')"
  else
    ctx_init_dirs
    if [[ -f "$CTX_CONFIG" ]]; then
      v="$(grep "^autoswitch_notify=" "$CTX_CONFIG" 2>/dev/null | tail -1 | cut -d= -f2-)"
    fi
    v="$(echo "${v:-}" | tr '[:upper:]' '[:lower:]')"
  fi
  v="${v//$'\r'/}"
  v="${v// /}"
  case "$v" in
    1|true|yes|on) printf '%s' 1 ;;
    *) printf '%s' 0 ;;
  esac
}

ctx_activation_get_source() {
  local v
  v="$(grep "^active_source=" "$CTX_CONFIG" 2>/dev/null | tail -1 | cut -d= -f2-)"
  v="$(echo "${v:-auto}" | tr '[:upper:]' '[:lower:]')"
  [[ "$v" == "manual" ]] && echo manual || echo auto
}

# First base64 flag spelling that produces output wins (GNU -d, BSD -D, --decode).
# GNU base64 prints the decoded text and then exits 1 on stray padding, so an
# `a || b` chain would print it twice; never concatenate attempts.
ctx_base64_decode() {
  local f out
  for f in -d -D --decode; do
    out="$(printf '%s' "$1" | base64 "$f" 2>/dev/null)"
    [[ -n "$out" ]] && { printf '%s' "$out"; return 0; }
  done
  return 1
}

# Decoded manual anchor PWD (when active_source=manual), or empty.
ctx_activation_get_manual_pwd() {
  local raw dec
  raw="$(grep "^manual_pwd=" "$CTX_CONFIG" 2>/dev/null | tail -1 | cut -d= -f2-)"
  raw="${raw//$'\r'/}"
  [[ -z "$raw" ]] && return 1
  dec="$(ctx_base64_decode "$raw")"
  [[ -n "$dec" ]] && printf '%s\n' "$dec" && return 0
  printf '%s\n' "$raw"
}

# Set key=val in ~/.ctx/config. Values are written literally (no sed: & | \ are safe);
# duplicate key lines collapse into one; the file is replaced atomically.
_ctx_config_upsert_line() {
  local key="$1" val="$2" tmp line found=0
  [[ "$val" == *$'\n'* ]] && { error "config value for '$key' must be a single line"; return 1; }
  ctx_init_dirs
  [[ -f "$CTX_CONFIG" ]] || : >"$CTX_CONFIG"
  tmp="$(mktemp "${CTX_CONFIG}.XXXXXX")" || return 1
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" == "${key}="* ]]; then
      [[ $found -eq 0 ]] && printf '%s=%s\n' "$key" "$val"
      found=1
      continue
    fi
    printf '%s\n' "$line"
  done <"$CTX_CONFIG" >"$tmp"
  [[ $found -eq 1 ]] || printf '%s=%s\n' "$key" "$val" >>"$tmp"
  mv "$tmp" "$CTX_CONFIG"
}

_ctx_config_rm_key() {
  local key="$1"
  [[ -f "$CTX_CONFIG" ]] || return 0
  local tmp
  tmp="$(mktemp)" || return 1
  grep -vE "^${key}=" "$CTX_CONFIG" > "$tmp" 2>/dev/null || true
  mv "$tmp" "$CTX_CONFIG"
}

# After explicit ctx use (not from directory hook): lock auto-switch until user cds.
ctx_activation_set_manual() {
  local pwd="$1" b64
  b64="$(printf '%s' "$pwd" | base64 | tr -d '\n')"
  _ctx_config_upsert_line active_source manual
  _ctx_config_upsert_line manual_pwd "$b64"
}

# After hook-driven ctx use: follow directory inference on next cd.
ctx_activation_set_auto() {
  _ctx_config_rm_key active_source
  _ctx_config_rm_key manual_pwd
}

ctx_work_root() {
  local configured=""
  configured="${CTX_WORK_ROOT:-}"
  if [[ -z "$configured" && -f "$CTX_CONFIG" ]]; then
    configured="$(grep "^work_root=" "$CTX_CONFIG" 2>/dev/null | tail -1 | cut -d= -f2-)"
  fi
  [[ -z "$configured" ]] && configured="$HOME/clients"
  configured="${configured/#\~/$HOME}"
  echo "$configured"
}

ctx_secret_provider() {
  local configured=""
  configured="${CTX_SECRET_PROVIDER:-}"
  if [[ -z "$configured" && -f "$CTX_CONFIG" ]]; then
    configured="$(grep "^secret_provider=" "$CTX_CONFIG" 2>/dev/null | tail -1 | cut -d= -f2-)"
  fi
  [[ -z "$configured" ]] && configured="auto"
  configured="$(echo "$configured" | tr '[:upper:]' '[:lower:]')"
  case "$configured" in
    auto|keychain|file|pass) ;;
    *) configured="auto" ;;
  esac
  echo "$configured"
}

ctx_effective_secret_provider() {
  local selected
  selected="$(ctx_secret_provider)"
  case "$selected" in
    keychain) echo "keychain" ;;
    file) echo "file" ;;
    pass) echo "pass" ;;
    auto)
      if [[ "$(uname -s)" == "Darwin" ]]; then
        echo "keychain"
      else
        echo "file"
      fi
      ;;
  esac
}

ctx_secret_store_label() {
  case "$(ctx_effective_secret_provider)" in
    keychain) echo "Keychain" ;;
    pass) echo "pass password store" ;;
    *) echo "file store" ;;
  esac
}

ctx_secret_provider_available() {
  case "$(ctx_effective_secret_provider)" in
    keychain) [[ "$(uname -s)" == "Darwin" ]] && command -v security &>/dev/null ;;
    pass) command -v pass &>/dev/null ;;
    file) return 0 ;;
  esac
}

# ─── Backup ────────────────────────────────────────────────────────────────────
# Returns 1 when the copy could not be made (a missing src is not an error).
backup_file() {
  local src="$1"
  [[ -f "$src" ]] || return 0
  local stamp; stamp=$(date '+%Y%m%d-%H%M%S')
  local bdir="${CTX_DIR}/backups/$stamp"
  { mkdir -p "$bdir" && cp -f "$src" "$bdir/$(basename "$src")"; } 2>/dev/null || return 1
  echo "$src" >> "$bdir/manifest.txt"
  log "Backed up $src → $bdir"
}

latest_backup() {
  ls -1t "${CTX_DIR}/backups" 2>/dev/null | head -1
}

# ─── Secrets storage ─────────────────────────────────────────────────────────
# macOS: Keychain (preferred)
# Other Unix: ~/.ctx/secrets/<profile>/<KEY> (0600) — not as strong as Keychain; keep disk encrypted.

_secret_file() {
  printf '%s/secrets/%s/%s' "$CTX_DIR" "$1" "$2"
}

_secret_keychain_set() {
  local profile="$1" key="$2" value="$3"
  local svc="ctx-${profile}-${key}"
  security delete-generic-password -a "$USER" -s "$svc" &>/dev/null || true
  security add-generic-password -a "$USER" -s "$svc" -w "$value" -T "" -U 2>/dev/null
  return $?
}

_secret_keychain_get() {
  local profile="$1" key="$2"
  security find-generic-password -a "$USER" -s "ctx-${profile}-${key}" -w 2>/dev/null || echo ""
}

_secret_keychain_delete() {
  security delete-generic-password -a "$USER" -s "ctx-${1}-${2}" &>/dev/null || true
}

_secret_keychain_list_keys() {
  security dump-keychain 2>/dev/null \
    | grep -o "\"ctx-${1}-[^\"]*\"" \
    | sed "s/\"ctx-${1}-//;s/\"//" | sort
}

_secret_file_set() {
  local profile="$1" key="$2" value="$3"
  local f
  f="$(_secret_file "$profile" "$key")"
  # umask 077: dirs and the file are created private from the start.
  ( umask 077; mkdir -p "$(dirname "$f")" ) || return 1
  chmod 700 "${CTX_DIR}/secrets" "${CTX_DIR}/secrets/${profile}" 2>/dev/null || true
  ( umask 077; printf '%s' "$value" > "$f" ) || return 1
  chmod 600 "$f" 2>/dev/null || true
  return 0
}

_secret_file_get() {
  local profile="$1" key="$2"
  local f
  f="$(_secret_file "$profile" "$key")"
  [[ -f "$f" ]] && cat "$f" || echo ""
}

_secret_file_delete() {
  local f
  f="$(_secret_file "$1" "$2")"
  rm -f "$f" 2>/dev/null || true
}

_secret_file_list_keys() {
  local d="${CTX_DIR}/secrets/${1}"
  [[ -d "$d" ]] || return 0
  local f
  for f in "$d"/*; do
    [[ -e "$f" ]] || continue
    basename "$f"
  done | sort
}

_secret_pass_path() {
  printf 'ctx/%s/%s' "$1" "$2"
}

_secret_pass_set() {
  local profile="$1" key="$2" value="$3"
  local p
  p="$(_secret_pass_path "$profile" "$key")"
  printf '%s\n' "$value" | pass insert -m -f "$p" >/dev/null 2>&1
}

_secret_pass_get() {
  local profile="$1" key="$2"
  local p out
  p="$(_secret_pass_path "$profile" "$key")"
  out="$(pass show "$p" 2>/dev/null || true)"
  [[ -n "$out" ]] && printf '%s\n' "$out" | sed -n '1p' || echo ""
}

_secret_pass_delete() {
  local p
  p="$(_secret_pass_path "$1" "$2")"
  pass rm -f "$p" >/dev/null 2>&1 || true
}

_secret_pass_list_keys() {
  local base="${PASSWORD_STORE_DIR:-$HOME/.password-store}/ctx/${1}"
  [[ -d "$base" ]] || return 0
  local f
  for f in "$base"/*.gpg; do
    [[ -e "$f" ]] || continue
    basename "${f%.gpg}"
  done | sort
}

keychain_set() {
  ctx_secret_provider_available || return 1
  local provider
  provider="$(ctx_effective_secret_provider)"
  case "$provider" in
    keychain) _secret_keychain_set "$@" ;;
    file) _secret_file_set "$@" ;;
    pass) _secret_pass_set "$@" ;;
  esac
}

keychain_get() {
  ctx_secret_provider_available || { echo ""; return 0; }
  local provider
  provider="$(ctx_effective_secret_provider)"
  case "$provider" in
    keychain) _secret_keychain_get "$@" ;;
    file) _secret_file_get "$@" ;;
    pass) _secret_pass_get "$@" ;;
  esac
}

keychain_delete() {
  ctx_secret_provider_available || return 1
  local provider
  provider="$(ctx_effective_secret_provider)"
  case "$provider" in
    keychain) _secret_keychain_delete "$@" ;;
    file) _secret_file_delete "$@" ;;
    pass) _secret_pass_delete "$@" ;;
  esac
}

keychain_list_keys() {
  ctx_secret_provider_available || return 0
  local provider
  provider="$(ctx_effective_secret_provider)"
  case "$provider" in
    keychain) _secret_keychain_list_keys "$@" ;;
    file) _secret_file_list_keys "$@" ;;
    pass) _secret_pass_list_keys "$@" ;;
  esac
}

secret_set_with_provider() {
  local provider="$1"; shift
  case "$provider" in
    keychain)
      [[ "$(uname -s)" == "Darwin" ]] || return 1
      command -v security &>/dev/null || return 1
      _secret_keychain_set "$@"
      ;;
    file)
      _secret_file_set "$@"
      ;;
    pass)
      command -v pass &>/dev/null || return 1
      _secret_pass_set "$@"
      ;;
    *)
      return 1
      ;;
  esac
}

secret_get_with_provider() {
  local provider="$1"; shift
  case "$provider" in
    keychain)
      [[ "$(uname -s)" == "Darwin" ]] || { echo ""; return 0; }
      command -v security &>/dev/null || { echo ""; return 0; }
      _secret_keychain_get "$@"
      ;;
    file)
      _secret_file_get "$@"
      ;;
    pass)
      command -v pass &>/dev/null || { echo ""; return 0; }
      _secret_pass_get "$@"
      ;;
    *)
      echo ""
      ;;
  esac
}

secret_delete_with_provider() {
  local provider="$1"; shift
  case "$provider" in
    keychain)
      [[ "$(uname -s)" == "Darwin" ]] || return 1
      command -v security &>/dev/null || return 1
      _secret_keychain_delete "$@"
      ;;
    file)
      _secret_file_delete "$@"
      ;;
    pass)
      command -v pass &>/dev/null || return 1
      _secret_pass_delete "$@"
      ;;
    *)
      return 1
      ;;
  esac
}

secret_list_with_provider() {
  local provider="$1"; shift
  case "$provider" in
    keychain)
      [[ "$(uname -s)" == "Darwin" ]] || return 0
      command -v security &>/dev/null || return 0
      _secret_keychain_list_keys "$@"
      ;;
    file)
      _secret_file_list_keys "$@"
      ;;
    pass)
      command -v pass &>/dev/null || return 0
      _secret_pass_list_keys "$@"
      ;;
    *)
      return 0
      ;;
  esac
}

# Read CTX_VERSION= from a lib/core.sh payload (handles CRLF).
parse_ctx_version_from_core_sh_file() {
  local f="$1" line val
  [[ -s "$f" ]] || { echo ""; return 0; }
  line="$(tr -d '\r' < "$f" | grep -m1 '^CTX_VERSION=' || true)"
  [[ -n "$line" ]] || { echo ""; return 0; }
  val="${line#CTX_VERSION=}"
  val="${val//\"/}"
  printf '%s\n' "$val"
}

# Rewrite GitHub clone URLs to use Host github-<profile> (see ~/.ssh/ctx_config).
# Args: profile, url, https_as_ssh (y|n) — for https://github.com/... only.
github_clone_url_for_profile() {
  local prof="$1" clone_url="$2" https_as_ssh="${3:-n}"
  local host="github-${prof}"

  if [[ "$clone_url" == git@github.com:* ]]; then
    printf '%s\n' "git@${host}:${clone_url#git@github.com:}"
  elif [[ "$clone_url" == ssh://git@github.com/* ]]; then
    local gh_path="${clone_url#ssh://git@github.com/}"
    gh_path="${gh_path#/}"
    gh_path="${gh_path%.git}.git"
    printf '%s\n' "git@${host}:${gh_path}"
  elif [[ "$clone_url" == https://github.com/* && "$https_as_ssh" == "y" ]]; then
    local path="${clone_url#https://github.com/}"
    path="${path#/}"
    path="${path%.git}.git"
    printf '%s\n' "git@${host}:${path}"
  else
    printf '%s\n' "$clone_url"
  fi
}

# Make plain git@github.com: / ssh://git@github.com/ remotes use Host github-<profile>
# for every repo under the profile's WORK_DIR (the identity file is loaded via includeIf;
# git clone re-reads it once the new repo exists). No-op without that SSH host. Idempotent.
ctx_git_identity_set_rewrite() {
  local profile="$1" host="github-$1"
  local file="$HOME/.config/git/ctx-${profile}" key="url.git@github-${profile}:.insteadOf"
  [[ -f "$file" ]] || return 0
  grep -q "^Host ${host}$" "$CTX_SSH_CONFIG" 2>/dev/null || return 0
  git config --file "$file" --get-all "$key" 2>/dev/null | grep -qx 'git@github.com:' && return 0
  git config --file "$file" --unset-all "$key" 2>/dev/null || true
  git config --file "$file" --add "$key" 'git@github.com:'
  git config --file "$file" --add "$key" 'ssh://git@github.com/'
  log "Added github.com → $host rewrite to $file"
}

# Commit and tag signing in a git identity file. format: ssh | openpgp; empty format or key turns it off.
ctx_git_identity_set_signing() {
  local file="$1" fmt="$2" key="$3" k
  for k in gpg.format user.signingkey commit.gpgsign tag.gpgsign; do
    git config --file "$file" --unset-all "$k" 2>/dev/null || true
  done
  [[ -n "$fmt" && -n "$key" ]] || return 0
  git config --file "$file" gpg.format "$fmt"
  git config --file "$file" user.signingkey "$key"
  git config --file "$file" commit.gpgsign true
  git config --file "$file" tag.gpgsign true
}

# Normalise SIGNING_FORMAT input: ssh | openpgp | "" (off). Returns 1 for unknown values.
ctx_signing_format_normalize() {
  case "$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')" in
    ssh) printf 'ssh' ;;
    gpg|openpgp|pgp) printf 'openpgp' ;;
    ""|none|off|no) printf '' ;;
    *) return 1 ;;
  esac
}

# ─── Safe SSH config (writes ONLY to ~/.ssh/ctx_config, never directly) ───────
# One-time: add a single Include line to ~/.ssh/config pointing to our file
ensure_ssh_include() {
  local ssh_main="$HOME/.ssh/config"
  mkdir -p "$HOME/.ssh"
  chmod 700 "$HOME/.ssh"
  touch "$CTX_SSH_CONFIG"
  chmod 600 "$CTX_SSH_CONFIG"

  if grep -qF "Include $CTX_SSH_CONFIG" "$ssh_main" 2>/dev/null; then
    return 0  # Already present
  fi

  # Back up before touching
  backup_file "$ssh_main"

  # Include must be FIRST line(s) in ssh/config to take effect
  local tmp; tmp=$(mktemp)
  {
    echo "# ctx managed hosts — added by ctx installer"
    echo "Include $CTX_SSH_CONFIG"
    echo ""
    cat "$ssh_main" 2>/dev/null || true
  } > "$tmp"
  mv "$tmp" "$ssh_main"
  chmod 600 "$ssh_main"
  log "Added Include $CTX_SSH_CONFIG to $ssh_main"
}

# Write a host block to our ctx_config file, never to ~/.ssh/config.
# Returns 0 added, 1 already present, 2 the file could not be written.
ctx_ssh_add_host() {
  local host="$1" key_path="$2"

  ensure_ssh_include

  if grep -q "^Host $host$" "$CTX_SSH_CONFIG" 2>/dev/null; then
    return 1  # already present
  fi

  {
    echo ""
    echo "Host $host"
    echo "  HostName github.com"
    echo "  User git"
    echo "  IdentityFile $key_path"
    echo "  IdentitiesOnly yes"
  } 2>/dev/null >>"$CTX_SSH_CONFIG" || return 2
  chmod 600 "$CTX_SSH_CONFIG"
  log "Added SSH host $host to $CTX_SSH_CONFIG"
  return 0
}

ctx_ssh_remove_host() {
  local host="$1"
  [[ -f "$CTX_SSH_CONFIG" ]] || return 0
  # Remove the 5-line block for this host
  local tmp; tmp=$(mktemp)
  awk "
    /^Host $host\$/ { skip=5; next }
    skip > 0 { skip--; next }
    { print }
  " "$CTX_SSH_CONFIG" > "$tmp"
  mv "$tmp" "$CTX_SSH_CONFIG"
  chmod 600 "$CTX_SSH_CONFIG"
}

# ─── Safe .gitconfig (writes includeIf, deduped) ─────────────────────────────
# Returns 0 added, 1 already present, 2 ~/.gitconfig could not be written.
gitconfig_add_include() {
  local work_dir="$1" identity_file="$2"
  local gitconfig="$HOME/.gitconfig"

  grep -qF "$identity_file" "$gitconfig" 2>/dev/null && return 1  # already present

  backup_file "$gitconfig"
  {
    echo ""
    echo "[includeIf \"gitdir:${work_dir}/\"]"
    echo "  path = ${identity_file}"
  } 2>/dev/null >>"$gitconfig" || return 2
  log "Added includeIf for $work_dir to ~/.gitconfig"
  return 0
}

# Remove what gitconfig_add_include wrote for one identity file: the exact
# "  path = <file>" line, the [includeIf …] header directly above it, and the
# blank separator line directly above that header. Other profiles' includes
# (e.g. ctx-acme-old when removing ctx-acme) and user sections are kept.
gitconfig_remove_include() {
  local identity_file="$1"
  local gitconfig="$HOME/.gitconfig"
  [[ -f "$gitconfig" ]] || return 0
  local tmp rc=0; tmp=$(mktemp)
  ID="  path = ${identity_file}" awk '
    { l[NR] = $0 }
    END {
      for (i = 2; i <= NR; i++) {
        if (l[i] == ENVIRON["ID"] && l[i-1] ~ /^\[includeIf/) {
          del[i] = 1; del[i-1] = 1; hit = 1
          if (i > 2 && l[i-2] == "") del[i-2] = 1
        }
      }
      for (i = 1; i <= NR; i++) if (!del[i]) print l[i]
      exit(hit ? 10 : 0)
    }
  ' "$gitconfig" > "$tmp" || rc=$?
  # Rewrite in place (keeps a symlink and the file's mode) only if a line was dropped.
  if [[ $rc -eq 10 ]]; then
    backup_file "$gitconfig"
    cat "$tmp" > "$gitconfig"
  fi
  rm -f "$tmp"
}

# ─── mise.toml generation ─────────────────────────────────────────────────────
# Body of a TOML basic string: \ → \\, " → \", control chars (0x00-0x1F, 0x7F) → \uXXXX.
# Byte-wise under LC_ALL=C, so UTF-8 passes through unchanged.
_ctx_toml_escape() {
  local LC_ALL=C s="$1" out="" c i
  if [[ "$s" != *[\\\"[:cntrl:]]* ]]; then
    printf '%s' "$s"
    return 0
  fi
  for ((i = 0; i < ${#s}; i++)); do
    c="${s:i:1}"
    case "$c" in
      \\) out+='\\' ;;
      \") out+='\"' ;;
      [[:cntrl:]]) out+="$(printf '\\u%04X' "'$c")" ;;
      *) out+="$c" ;;
    esac
  done
  printf '%s' "$out"
}

# Text for a comment line: control characters (newline, CR, tab, …) become a
# space, so a value cannot end the comment and start a command. Then TOML-escaped.
_ctx_toml_comment_text() {
  local LC_ALL=C s="$1"
  _ctx_toml_escape "${s//[[:cntrl:]]/ }"
}

# A value for inside "…" in a hook script, which is itself inside a TOML """ string:
# \ " $ ` get a shell backslash first, then the result is TOML-escaped.
_ctx_toml_sh_dq() {
  local LC_ALL=C s="$1" out="" c i
  for ((i = 0; i < ${#s}; i++)); do
    c="${s:i:1}"
    case "$c" in
      \\|\"|\$|\`) out+="\\$c" ;;
      *) out+="$c" ;;
    esac
  done
  _ctx_toml_escape "$out"
}

# Returns 2 when the file could not be written.
generate_mise_toml() {
  local profile="$1"
  local work_dir="$2"
  local git_name="$3"
  local git_email="$4"
  local aws_profile="${5:-}"
  local azure_sub="${6:-}"
  local gcp_project="${7:-}"
  local kube_ctx="${8:-}"
  local secret_keys="${9:-}"
  local extra_envs="${10:-}"

  local toml_file="$work_dir/mise.toml"
  [[ -f "$toml_file" ]] && return 0  # Don't overwrite existing

  mkdir -p "$work_dir"

  {
    echo "# mise.toml — ctx managed environment for: $(_ctx_toml_comment_text "$profile")"
    echo "# Edit freely. This file is safe to commit (no secrets here)."
    echo "# Secrets load in [hooks.enter]: macOS Keychain, or ~/.ctx/secrets on other OSes."
    echo ""

    # Environment variables
    echo "[env]"
    [[ -n "$aws_profile"  ]] && echo "AWS_PROFILE = \"$(_ctx_toml_escape "$aws_profile")\""
    [[ -n "$azure_sub"    ]] && echo "AZURE_SUBSCRIPTION = \"$(_ctx_toml_escape "$azure_sub")\""
    [[ -n "$gcp_project"  ]] && echo "GCLOUD_PROJECT = \"$(_ctx_toml_escape "$gcp_project")\""
    [[ -n "$kube_ctx"     ]] && echo "CTX_KUBE_CONTEXT = \"$(_ctx_toml_escape "$kube_ctx")\""

    if [[ -n "$extra_envs" ]]; then
      while IFS= read -r pair; do
        [[ -z "$pair" ]] && continue
        local k="${pair%%=*}" v="${pair#*=}"
        is_valid_env_key "$k" || continue
        echo "$k = \"$(_ctx_toml_escape "$v")\""
      done <<< "$extra_envs"
    fi

    # Load secrets file if it exists (gitignored)
    echo ""
    echo "# Uncomment to load a local secrets file (add it to .gitignore)"
    echo "# _.file = \".env.local\""
    echo ""

    # Enter hook — sets git identity + loads Keychain secrets
    echo "[hooks.enter]"
    echo "shell = \"bash\""
    echo "script = \"\"\""

    # Git identity
    echo "  # Set git identity for this directory"
    if is_sensible_git_email "$git_email"; then
      echo "  git config user.name  \"$(_ctx_toml_sh_dq "$git_name")\""
      echo "  git config user.email \"$(_ctx_toml_sh_dq "$git_email")\""
    else
      echo "  # Skipped invalid git email (looks like a username, not an address): \"$(_ctx_toml_comment_text "$git_email")\""
      echo "  # Fix GIT_EMAIL in ~/.ctx/profiles/$(_ctx_toml_comment_text "$profile").conf and re-run setup, or edit mise.toml."
    fi
    echo ""

    # Cloud context switches
    [[ -n "$azure_sub"  ]] && echo "  az account set --subscription \"$(_ctx_toml_sh_dq "$azure_sub")\" 2>/dev/null || true"
    [[ -n "$gcp_project" ]] && echo "  gcloud config set project \"$(_ctx_toml_sh_dq "$gcp_project")\" --quiet 2>/dev/null || true"
    [[ -n "$kube_ctx"   ]] && echo "  kubectl config use-context \"$(_ctx_toml_sh_dq "$kube_ctx")\" 2>/dev/null || true"

    # Secret injection (provider-aware: keychain/file/pass)
    if [[ -n "$secret_keys" ]]; then
      echo ""
      echo "  # Load profile secrets"
      echo "  _ctx_sp=\"\${CTX_SECRET_PROVIDER:-}\""
      echo "  if [[ -z \"\$_ctx_sp\" && -f \"\${CTX_DIR:-\$HOME/.ctx}/config\" ]]; then"
      echo "    _ctx_sp=\$(grep '^secret_provider=' \"\${CTX_DIR:-\$HOME/.ctx}/config\" 2>/dev/null | tail -1 | cut -d= -f2-)"
      echo "  fi"
      echo "  [[ -z \"\$_ctx_sp\" ]] && _ctx_sp=\"auto\""
      echo "  if [[ \"\$_ctx_sp\" == \"auto\" ]]; then"
      echo "    [[ \"\$(uname -s)\" == \"Darwin\" ]] && _ctx_sp=\"keychain\" || _ctx_sp=\"file\""
      echo "  fi"
      for key in $secret_keys; do
        is_valid_env_key "$key" || continue
        echo "  if [[ \"\$_ctx_sp\" == \"keychain\" ]]; then"
        echo "    _val=\$(security find-generic-password -a \"\$USER\" -s \"ctx-${profile}-${key}\" -w 2>/dev/null || true)"
        echo "  elif [[ \"\$_ctx_sp\" == \"pass\" ]]; then"
        echo "    _val=\$(pass show \"ctx/${profile}/${key}\" 2>/dev/null | sed -n '1p' || true)"
        echo "  else"
        echo "    _f=\"\${CTX_DIR:-\$HOME/.ctx}/secrets/${profile}/${key}\""
        echo "    [[ -f \"\$_f\" ]] && _val=\$(cat \"\$_f\") || _val=\"\""
        echo "  fi"
        echo "  [[ -n \"\$_val\" ]] && declare -x ${key}=\"\$_val\""
      done
    fi

    echo "\"\"\""
    echo ""

    # Leave hook — unset git identity and secrets
    echo "[hooks.leave]"
    echo "shell = \"bash\""
    echo "script = \"\"\""
    echo "  git config --unset user.name  2>/dev/null || true"
    echo "  git config --unset user.email 2>/dev/null || true"
    if [[ -n "$secret_keys" ]]; then
      for key in $secret_keys; do
        is_valid_env_key "$key" || continue
        echo "  unset $key 2>/dev/null || true"
      done
    fi
    echo "\"\"\""

  } 2>/dev/null >"$toml_file" || return 2

  # Trust the file so mise doesn't prompt
  if command -v mise &>/dev/null; then
    mise trust "$toml_file" &>/dev/null 2>&1 || true
  fi

  log "Generated mise.toml at $toml_file"
}

# ─── Detection helpers ────────────────────────────────────────────────────────
detect_ssh_keys() {
  for pub in "$HOME"/.ssh/*.pub; do
    [[ -f "$pub" ]] || continue
    local priv="${pub%.pub}"
    [[ -f "$priv" ]] && echo "$priv"
  done
}

detect_gh_accounts() {
  command -v gh &>/dev/null || return 0
  # gh prints "Logged in to github.com account USER (…)" (newer) or "… as USER (…)" (older).
  # "Failed to log in to github.com account USER" (invalid token) is not a login.
  run_with_timeout 8 gh auth status 2>&1 \
    | awk '/Logged in to github\.com (account|as) / {
        for (i = 1; i + 2 <= NF; i++)
          if ($i == "github.com" && ($(i+1) == "account" || $(i+1) == "as") && $(i-1) == "to") {
            print $(i+2); break
          }
      }'
}

detect_aws_profiles() {
  local creds="$HOME/.aws/credentials" cfg="$HOME/.aws/config"
  {
    [[ -f "$creds" ]] && grep '^\[' "$creds" | tr -d '[]'
    [[ -f "$cfg"   ]] && grep '^\[profile ' "$cfg" | sed 's/\[profile //;s/\]//'
  } | sort -u
}

detect_kube_contexts() {
  command -v kubectl &>/dev/null || return 0
  kubectl config get-contexts -o name 2>/dev/null || true
}

detect_azure_subs() {
  command -v az &>/dev/null || return 0
  run_with_timeout 8 az account list --query "[].name" -o tsv 2>/dev/null || true
}

detect_gcp_projects() {
  command -v gcloud &>/dev/null || return 0
  run_with_timeout 8 gcloud projects list --format="value(projectId)" 2>/dev/null || true
}

# ─── Tool check ───────────────────────────────────────────────────────────────
has() { command -v "$1" &>/dev/null; }

run_with_timeout() {
  local seconds="$1"
  shift

  if command -v timeout &>/dev/null; then
    timeout "$seconds" "$@"
    return $?
  fi
  if command -v gtimeout &>/dev/null; then
    gtimeout "$seconds" "$@"
    return $?
  fi

  "$@" &
  local pid=$!
  local waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if (( waited >= seconds )); then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      return 124
    fi
    sleep 1
    waited=$((waited + 1))
  done
  wait "$pid"
}

is_valid_env_key() {
  [[ "$1" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]
}

# Reject obvious mistakes like pasting a GitHub username into the email field.
is_sensible_git_email() {
  [[ "$1" =~ @ ]] && [[ "$1" != *..* ]] && [[ "$1" =~ ^[^[:space:]]+@[^[:space:]]+\.[^[:space:]]+ ]]
}
