#!/usr/bin/env bash
# ctx/lib/core.sh — v3: adds mise.toml support, safe isolated SSH config

CTX_VERSION="3.1.1"
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
# Usage: ask "Prompt text" [default]
ask() {
  local prompt="$1" default="${2:-}"
  if $HAS_GUM; then
    local result
    result=$(gum input \
      --placeholder "${default:-$prompt}" \
      --prompt "> " \
      --prompt.foreground 99 \
      --width 60)
    echo "${result:-$default}"
  else
    if [[ -n "$default" ]]; then
      echo -e "${CYAN}?${RESET} $prompt ${DIM}[$default]${RESET}: \c" >&2
    else
      echo -e "${CYAN}?${RESET} $prompt: \c" >&2
    fi
    local var
    if [[ -t 0 ]]; then
      read -r var
    else
      read -r var < /dev/tty 2>/dev/null || var=""
    fi
    echo "${var:-$default}"
  fi
}

# Secret/password input — hidden
ask_secret() {
  local prompt="$1"
  if $HAS_GUM; then
    gum input --password \
      --placeholder "$prompt" \
      --prompt "> " \
      --prompt.foreground 99 \
      --width 60
  else
    echo -e "${CYAN}?${RESET} $prompt ${DIM}(hidden)${RESET}: \c"
    local var; read -rs var; echo ""
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
      "$prompt"
    return $?
  else
    local hint; [[ "$default" == "y" ]] && hint="Y/n" || hint="y/N"
    echo -e "${CYAN}?${RESET} $prompt ${DIM}[$hint]${RESET}: \c"
    local answer; read -r answer
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
    result=$(printf '%s\n' "${items[@]}" \
      | gum choose \
          --header "$prompt" \
          --header.foreground 99 \
          --selected.foreground 99 \
          2>/dev/null) || true
    echo "$result"
  else
    echo ""
    for i in "${!items[@]}"; do
      echo -e "  ${DIM}$((i+1))${RESET}  ${items[$i]}"
    done
    echo ""
    echo -e "${CYAN}?${RESET} $prompt (1-${#items[@]}, 0 to skip): \c"
    local choice; read -r choice
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
    printf '%s\n' "${items[@]}" \
      | gum choose --no-limit \
          --header "$prompt (space to select, enter to confirm)" \
          --header.foreground 99 \
          --selected.foreground 99 \
          2>/dev/null || true
  else
    echo ""
    for i in "${!items[@]}"; do
      echo -e "  ${DIM}$((i+1))${RESET}  ${items[$i]}"
    done
    echo ""
    echo -e "${CYAN}?${RESET} $prompt (comma-separated numbers, e.g. 1,3): \c"
    local input; read -r input
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
  unset KUBE_CONTEXT SECRET_KEYS EXTRA_ENVS
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
  if grep -q "^active=" "$CTX_CONFIG" 2>/dev/null; then
    sed -i.bak "s/^active=.*/active=$name/" "$CTX_CONFIG"
    rm -f "$CTX_CONFIG.bak"
  else
    echo "active=$name" >> "$CTX_CONFIG"
  fi
  log "Switched to profile: $name"
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
backup_file() {
  local src="$1"
  [[ -f "$src" ]] || return 0
  local stamp; stamp=$(date '+%Y%m%d-%H%M%S')
  local bdir="${CTX_DIR}/backups/$stamp"
  mkdir -p "$bdir"
  cp "$src" "$bdir/$(basename "$src")"
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
  mkdir -p "$(dirname "$f")" || return 1
  chmod 700 "${CTX_DIR}/secrets" "${CTX_DIR}/secrets/${profile}" 2>/dev/null || true
  printf '%s' "$value" > "$f" || return 1
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

# Write a host block to our ctx_config file, never to ~/.ssh/config
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
  } >> "$CTX_SSH_CONFIG"
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
gitconfig_add_include() {
  local work_dir="$1" identity_file="$2"
  local gitconfig="$HOME/.gitconfig"

  grep -qF "$identity_file" "$gitconfig" 2>/dev/null && return 1  # already present

  backup_file "$gitconfig"
  {
    echo ""
    echo "[includeIf \"gitdir:${work_dir}/\"]"
    echo "  path = ${identity_file}"
  } >> "$gitconfig"
  log "Added includeIf for $work_dir to ~/.gitconfig"
  return 0
}

gitconfig_remove_include() {
  local identity_file="$1"
  local gitconfig="$HOME/.gitconfig"
  [[ -f "$gitconfig" ]] || return 0
  local tmp; tmp=$(mktemp)
  grep -v "$identity_file" "$gitconfig" \
    | grep -v "\[includeIf.*$(basename "$identity_file")" \
    > "$tmp" || true
  mv "$tmp" "$gitconfig"
}

# ─── mise.toml generation ─────────────────────────────────────────────────────
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
    echo "# mise.toml — ctx managed environment for: $profile"
    echo "# Edit freely. This file is safe to commit (no secrets here)."
    echo "# Secrets load in [hooks.enter]: macOS Keychain, or ~/.ctx/secrets on other OSes."
    echo ""

    # Environment variables
    echo "[env]"
    [[ -n "$aws_profile"  ]] && echo "AWS_PROFILE = \"$aws_profile\""
    [[ -n "$azure_sub"    ]] && echo "AZURE_SUBSCRIPTION = \"$azure_sub\""
    [[ -n "$gcp_project"  ]] && echo "GCLOUD_PROJECT = \"$gcp_project\""
    [[ -n "$kube_ctx"     ]] && echo "CTX_KUBE_CONTEXT = \"$kube_ctx\""

    if [[ -n "$extra_envs" ]]; then
      while IFS= read -r pair; do
        [[ -z "$pair" ]] && continue
        local k="${pair%%=*}" v="${pair#*=}"
        is_valid_env_key "$k" || continue
        echo "$k = \"$v\""
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
      echo "  git config user.name  \"$git_name\""
      echo "  git config user.email \"$git_email\""
    else
      echo "  # Skipped invalid git email (looks like a username, not an address): \"$git_email\""
      echo "  # Fix GIT_EMAIL in ~/.ctx/profiles/${profile}.conf and re-run setup, or edit mise.toml."
    fi
    echo ""

    # Cloud context switches
    [[ -n "$azure_sub"  ]] && echo "  az account set --subscription \"$azure_sub\" 2>/dev/null || true"
    [[ -n "$gcp_project" ]] && echo "  gcloud config set project \"$gcp_project\" --quiet 2>/dev/null || true"
    [[ -n "$kube_ctx"   ]] && echo "  kubectl config use-context \"$kube_ctx\" 2>/dev/null || true"

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

  } > "$toml_file"

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
  gh auth status 2>&1 \
    | grep "Logged in to github.com account" \
    | awk '{print $NF}' | tr -d '()'
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
