#!/usr/bin/env bash
# ctx/lib/core.sh — v3: adds mise.toml support, safe isolated SSH config

CTX_VERSION="3.0.0"
CTX_DIR="${CTX_DIR:-$HOME/.ctx}"
CTX_CONFIG="${CTX_DIR}/config"
CTX_PROFILES_DIR="${CTX_DIR}/profiles"
CTX_LOG="${CTX_DIR}/ctx.log"
# CTX_SSH_CONFIG is derived dynamically at call time (see ensure_ssh_include / ctx_ssh_add_host)

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
command -v gum &>/dev/null && HAS_GUM=true

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
          --cursor.foreground 99 \
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
          --cursor.foreground 99 \
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

# ─── macOS Keychain ───────────────────────────────────────────────────────────
keychain_set() {
  local profile="$1" key="$2" value="$3"
  local svc="ctx-${profile}-${key}"
  security delete-generic-password -a "$USER" -s "$svc" &>/dev/null || true
  security add-generic-password -a "$USER" -s "$svc" -w "$value" -T "" -U 2>/dev/null
}

keychain_get() {
  local profile="$1" key="$2"
  security find-generic-password -a "$USER" -s "ctx-${profile}-${key}" -w 2>/dev/null || echo ""
}

keychain_delete() {
  security delete-generic-password -a "$USER" -s "ctx-${1}-${2}" &>/dev/null || true
}

keychain_list_keys() {
  security dump-keychain 2>/dev/null \
    | grep -o "\"ctx-${1}-[^\"]*\"" \
    | sed "s/\"ctx-${1}-//;s/\"//" | sort
}

# ─── Safe SSH config (writes ONLY to ~/.ssh/ctx_config, never directly) ───────
# One-time: add a single Include line to ~/.ssh/config pointing to our file
ensure_ssh_include() {
  local CTX_SSH_CONFIG="$HOME/.ssh/ctx_config"
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
  local CTX_SSH_CONFIG="$HOME/.ssh/ctx_config"

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
  local CTX_SSH_CONFIG="$HOME/.ssh/ctx_config"
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
    echo "# Secrets are loaded from macOS Keychain by the [hooks.enter] block."
    echo ""

    # Environment variables
    echo "[env]"
    [[ -n "$aws_profile"  ]] && echo "AWS_PROFILE = \"$aws_profile\""
    [[ -n "$azure_sub"    ]] && echo "AZURE_SUBSCRIPTION = \"$azure_sub\""
    [[ -n "$gcp_project"  ]] && echo "GCLOUD_PROJECT = \"$gcp_project\""
    [[ -n "$kube_ctx"     ]] && echo "CTX_KUBE_CONTEXT = \"$kube_ctx\""

    if [[ -n "$extra_envs" ]]; then
      for pair in $extra_envs; do
        local k="${pair%%=*}" v="${pair#*=}"
        echo "$k = \"$v\""
      done
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
    echo "  git config user.name  \"$git_name\""
    echo "  git config user.email \"$git_email\""
    echo ""

    # Cloud context switches
    [[ -n "$azure_sub"  ]] && echo "  az account set --subscription \"$azure_sub\" 2>/dev/null || true"
    [[ -n "$gcp_project" ]] && echo "  gcloud config set project \"$gcp_project\" --quiet 2>/dev/null || true"
    [[ -n "$kube_ctx"   ]] && echo "  kubectl config use-context \"$kube_ctx\" 2>/dev/null || true"

    # Keychain secret injection
    if [[ -n "$secret_keys" ]]; then
      echo ""
      echo "  # Load secrets from macOS Keychain"
      for key in $secret_keys; do
        echo "  _val=\$(security find-generic-password -a \"\$USER\" -s \"ctx-${profile}-${key}\" -w 2>/dev/null || true)"
        echo "  [[ -n \"\$_val\" ]] && export ${key}=\"\$_val\""
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
  az account list --query "[].name" -o tsv 2>/dev/null || true
}

detect_gcp_projects() {
  command -v gcloud &>/dev/null || return 0
  gcloud projects list --format="value(projectId)" 2>/dev/null || true
}

# ─── Tool check ───────────────────────────────────────────────────────────────
has() { command -v "$1" &>/dev/null; }
