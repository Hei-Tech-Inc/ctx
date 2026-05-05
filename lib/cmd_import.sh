#!/usr/bin/env bash
# ctx/lib/cmd_import.sh — v3: gum TUI + mise.toml generation + isolated SSH config

cmd_import() {
  ctx_init_dirs
  ctx_header "Add a client profile"

  # ── Scan machine ────────────────────────────────────────────────────────
  local ssh_keys=() gh_accounts=() aws_profiles=() kube_contexts=()
  local azure_subs=() gcp_projects=()

  if $HAS_GUM; then
    spin "Scanning your machine for existing credentials..." \
      bash -c '
        command -v gh &>/dev/null && gh auth status &>/dev/null || true
        ls ~/.ssh/*.pub &>/dev/null || true
        ls ~/.aws/credentials &>/dev/null || true
      ' &>/dev/null || true
  fi

  while IFS= read -r k; do [[ -n "$k" ]] && ssh_keys+=("$k");       done < <(detect_ssh_keys)
  while IFS= read -r a; do [[ -n "$a" ]] && gh_accounts+=("$a");     done < <(detect_gh_accounts)
  while IFS= read -r p; do [[ -n "$p" ]] && aws_profiles+=("$p");    done < <(detect_aws_profiles)
  while IFS= read -r c; do [[ -n "$c" ]] && kube_contexts+=("$c");   done < <(detect_kube_contexts)
  while IFS= read -r s; do [[ -n "$s" ]] && azure_subs+=("$s");      done < <(detect_azure_subs)
  while IFS= read -r p; do [[ -n "$p" ]] && gcp_projects+=("$p");    done < <(detect_gcp_projects)

  # Summary
  echo ""
  [[ ${#ssh_keys[@]}     -gt 0 ]] && success "Found ${#ssh_keys[@]} SSH key(s)"     || dim "  No SSH keys in ~/.ssh/"
  [[ ${#gh_accounts[@]}  -gt 0 ]] && success "Found ${#gh_accounts[@]} gh account(s)" || dim "  No gh accounts (run: gh auth login)"
  [[ ${#aws_profiles[@]} -gt 0 ]] && success "Found ${#aws_profiles[@]} AWS profile(s)"
  [[ ${#kube_contexts[@]} -gt 0 ]] && success "Found ${#kube_contexts[@]} kubectl context(s)"
  [[ ${#azure_subs[@]}   -gt 0 ]] && success "Found ${#azure_subs[@]} Azure subscription(s)"
  [[ ${#gcp_projects[@]} -gt 0 ]] && success "Found ${#gcp_projects[@]} GCP project(s)"
  echo ""

  # ── Profile name ────────────────────────────────────────────────────────
  hr
  bold "  Profile identity"
  echo ""
  local PROFILE_NAME
  PROFILE_NAME=$(ask "Profile name (e.g. hubtel, aifi, scalecap)")
  [[ -z "$PROFILE_NAME" ]] && die "Profile name required."
  PROFILE_NAME=$(echo "$PROFILE_NAME" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]/-/g')

  if profile_exists "$PROFILE_NAME"; then
    ask_yn "Profile '$PROFILE_NAME' already exists. Overwrite?" "n" || die "Aborted."
  fi

  # ── Git identity ────────────────────────────────────────────────────────
  local current_name current_email
  current_name=$(git config --global user.name  2>/dev/null || echo "")
  current_email=$(git config --global user.email 2>/dev/null || echo "")

  local GIT_NAME GIT_EMAIL
  GIT_NAME=$(ask  "Git name for this client"  "$current_name")
  GIT_EMAIL=$(ask "Git email for this client" "$current_email")
  echo ""

  # ── Working directory ───────────────────────────────────────────────────
  hr
  bold "  Working directory"
  echo ""
  local WORK_DIR
  WORK_DIR=$(ask "Client directory (e.g. ~/clients/${PROFILE_NAME})" "~/clients/${PROFILE_NAME}")
  WORK_DIR="${WORK_DIR/#\~/$HOME}"
  echo ""

  # ── SSH key ─────────────────────────────────────────────────────────────
  hr
  bold "  SSH / GitHub"
  echo ""

  local SSH_KEY_PATH=""

  if [[ ${#ssh_keys[@]} -gt 0 ]]; then
    local picked
    picked=$(pick_one "Use an existing SSH key for this profile?" "Generate new key" "${ssh_keys[@]}")

    if [[ "$picked" == "Generate new key" || -z "$picked" ]]; then
      SSH_KEY_PATH="$HOME/.ssh/ctx_${PROFILE_NAME}"
      _gen_ssh_key "$SSH_KEY_PATH" "$GIT_EMAIL"
    else
      SSH_KEY_PATH="$picked"
      success "Using existing key: $(basename "$SSH_KEY_PATH")"
    fi
  else
    if ask_yn "No SSH keys found. Generate one for this profile?" "y"; then
      SSH_KEY_PATH="$HOME/.ssh/ctx_${PROFILE_NAME}"
      _gen_ssh_key "$SSH_KEY_PATH" "$GIT_EMAIL"
    fi
  fi

  # ── GitHub account ──────────────────────────────────────────────────────
  local GITHUB_USER=""

  if [[ ${#gh_accounts[@]} -gt 0 ]]; then
    GITHUB_USER=$(pick_one "Which GitHub account for '$PROFILE_NAME'?" "None / skip" "${gh_accounts[@]}")
    [[ "$GITHUB_USER" == "None / skip" ]] && GITHUB_USER=""
    [[ -n "$GITHUB_USER" ]] && success "GitHub account: $GITHUB_USER"
  fi

  if [[ -z "$GITHUB_USER" ]]; then
    GITHUB_USER=$(ask "GitHub username for this client (leave blank to skip)")
  fi
  echo ""

  # ── AWS ─────────────────────────────────────────────────────────────────
  hr
  bold "  Cloud credentials"
  echo ""

  local AWS_PROFILE_NAME=""
  if [[ ${#aws_profiles[@]} -gt 0 ]]; then
    AWS_PROFILE_NAME=$(pick_one "AWS profile for '$PROFILE_NAME'?" "None / skip" "${aws_profiles[@]}")
    [[ "$AWS_PROFILE_NAME" == "None / skip" ]] && AWS_PROFILE_NAME=""
    [[ -n "$AWS_PROFILE_NAME" ]] && success "AWS profile: $AWS_PROFILE_NAME"
  fi
  [[ -z "$AWS_PROFILE_NAME" ]] && AWS_PROFILE_NAME=$(ask "AWS profile name (leave blank to skip)")

  # ── Azure ───────────────────────────────────────────────────────────────
  local AZURE_SUBSCRIPTION="" AZURE_TENANT=""
  if [[ ${#azure_subs[@]} -gt 0 ]]; then
    AZURE_SUBSCRIPTION=$(pick_one "Azure subscription for '$PROFILE_NAME'?" "None / skip" "${azure_subs[@]}")
    [[ "$AZURE_SUBSCRIPTION" == "None / skip" ]] && AZURE_SUBSCRIPTION=""
    [[ -n "$AZURE_SUBSCRIPTION" ]] && AZURE_TENANT=$(ask "Azure tenant ID (leave blank to skip)")
  else
    AZURE_SUBSCRIPTION=$(ask "Azure subscription ID (leave blank to skip)")
    [[ -n "$AZURE_SUBSCRIPTION" ]] && AZURE_TENANT=$(ask "Azure tenant ID (leave blank to skip)")
  fi

  # ── GCP ─────────────────────────────────────────────────────────────────
  local GCP_PROJECT="" GCP_ACCOUNT=""
  if [[ ${#gcp_projects[@]} -gt 0 ]]; then
    GCP_PROJECT=$(pick_one "GCP project for '$PROFILE_NAME'?" "None / skip" "${gcp_projects[@]}")
    [[ "$GCP_PROJECT" == "None / skip" ]] && GCP_PROJECT=""
    [[ -n "$GCP_PROJECT" ]] && GCP_ACCOUNT=$(ask "GCP account email (leave blank to skip)")
  else
    GCP_PROJECT=$(ask "GCP project ID (leave blank to skip)")
    [[ -n "$GCP_PROJECT" ]] && GCP_ACCOUNT=$(ask "GCP account email (leave blank to skip)")
  fi

  # ── kubectl ─────────────────────────────────────────────────────────────
  local KUBE_CONTEXT=""
  if [[ ${#kube_contexts[@]} -gt 0 ]]; then
    KUBE_CONTEXT=$(pick_one "kubectl context for '$PROFILE_NAME'?" "None / skip" "${kube_contexts[@]}")
    [[ "$KUBE_CONTEXT" == "None / skip" ]] && KUBE_CONTEXT=""
    [[ -n "$KUBE_CONTEXT" ]] && success "kubectl context: $KUBE_CONTEXT"
  else
    KUBE_CONTEXT=$(ask "kubectl context (leave blank to skip)")
  fi
  echo ""

  # ── Secrets → Keychain ──────────────────────────────────────────────────
  hr
  bold "  Secrets (stored in macOS Keychain — never on disk)"
  echo ""
  dim "  Values you enter here go directly into Keychain."
  dim "  They are exported to your shell by mise's hooks.enter."
  echo ""

  local SECRET_KEYS=()

  _maybe_secret() {
    local key="$1" prompt="$2"
    if ask_yn "$prompt?" "n"; then
      local val
      val=$(ask_secret "Value for $key")
      if [[ -n "$val" ]]; then
        keychain_set "$PROFILE_NAME" "$key" "$val" \
          && success "$key → Keychain" \
          || warn "Could not store $key in Keychain"
        SECRET_KEYS+=("$key")
      fi
    fi
  }

  _maybe_secret "GH_TOKEN"      "Store a GitHub personal access token"
  _maybe_secret "NPM_TOKEN"     "Store an npm registry token"
  _maybe_secret "DOCKER_TOKEN"  "Store a Docker Hub token"

  while ask_yn "Add another secret key?" "n"; do
    local ckey
    ckey=$(ask "Key name (e.g. STRIPE_SECRET_KEY)")
    [[ -z "$ckey" ]] && break
    local cval
    cval=$(ask_secret "Value for $ckey")
    if [[ -n "$cval" ]]; then
      keychain_set "$PROFILE_NAME" "$ckey" "$cval" \
        && success "$ckey → Keychain"
      SECRET_KEYS+=("$ckey")
    fi
  done
  echo ""

  # ── Non-secret env vars ─────────────────────────────────────────────────
  local EXTRA_ENVS=""
  if ask_yn "Add non-secret env vars (e.g. NODE_ENV=development)?" "n"; then
    EXTRA_ENVS=$(ask "Space-separated KEY=VALUE pairs")
  fi
  echo ""

  # ── Dry-run preview ─────────────────────────────────────────────────────
  if [[ "${CTX_DRY_RUN:-0}" == "1" ]]; then
    bold "  [DRY RUN] — nothing written\n"
    echo "  Would create:"
    dim "    $CTX_PROFILES_DIR/${PROFILE_NAME}.conf"
    dim "    $HOME/.config/git/ctx-${PROFILE_NAME}"
    dim "    $WORK_DIR/mise.toml"
    dim "    $CTX_SSH_CONFIG  (host block: github-${PROFILE_NAME})"
    dim "    ~/.gitconfig  (includeIf block)"
    echo ""
    return 0
  fi

  # ── Write everything ────────────────────────────────────────────────────
  backup_file "$HOME/.gitconfig"

  _write_all \
    "$PROFILE_NAME" "$GIT_NAME" "$GIT_EMAIL" "$WORK_DIR" \
    "$GITHUB_USER"  "$SSH_KEY_PATH" \
    "$AWS_PROFILE_NAME" "$AZURE_SUBSCRIPTION" "$AZURE_TENANT" \
    "$GCP_PROJECT" "$GCP_ACCOUNT" "$KUBE_CONTEXT" \
    "${SECRET_KEYS[*]}" "$EXTRA_ENVS"

  # ── Final summary ────────────────────────────────────────────────────────
  echo ""
  if $HAS_GUM; then
    gum style \
      --border rounded --border-foreground 78 \
      --padding "0 2" --margin "1 0" \
      "Profile '${PROFILE_NAME}' created" \
      "$(dim "Activate: ctx use ${PROFILE_NAME}")" \
      "$(dim "Check:    ctx status")"
  else
    success "Profile '${BOLD}${PROFILE_NAME}${RESET}${GREEN}' created."
    info "Activate: ctx use $PROFILE_NAME"
    info "Check:    ctx status"
  fi
  echo ""
}

cmd_add() { cmd_import "$@"; }

# ─── Generate SSH key, pause for GitHub ────────────────────────────────────
_gen_ssh_key() {
  local key_path="$1" email="$2"

  if [[ -f "$key_path" ]]; then
    warn "Key exists at $key_path — skipping generation."
    return 0
  fi

  ssh-keygen -t ed25519 -C "$email" -f "$key_path" -N "" -q
  chmod 600 "$key_path"
  success "SSH key generated: $key_path"
  echo ""
  bold "  Add this public key to GitHub:"
  echo ""
  cat "${key_path}.pub"
  echo ""
  info "Open: https://github.com/settings/ssh/new"
  ask "Press Enter after adding the key to GitHub..."
}

# ─── Write everything to disk ─────────────────────────────────────────────
_write_all() {
  local profile="$1"   git_name="$2"  git_email="$3"  work_dir="$4"
  local github_user="$5" ssh_key="$6"
  local aws="$7"  azure_sub="$8"  azure_tenant="$9"
  local gcp="${10}" gcp_acct="${11}" kube="${12}"
  local secret_keys="${13}" extra_envs="${14}"

  # 1. Profile .conf — metadata only, no secrets
  mkdir -p "$CTX_PROFILES_DIR"
  cat > "$CTX_PROFILES_DIR/${profile}.conf" <<EOF
# ctx profile: $profile
# Generated: $(date)
# Secrets are in macOS Keychain. Env vars are in mise.toml.

PROFILE_NAME="$profile"
GIT_NAME="$git_name"
GIT_EMAIL="$git_email"
WORK_DIR="$work_dir"
GITHUB_USER="$github_user"
SSH_KEY_PATH="$ssh_key"
AWS_PROFILE_NAME="$aws"
AZURE_SUBSCRIPTION="$azure_sub"
AZURE_TENANT="$azure_tenant"
GCP_PROJECT="$gcp"
GCP_ACCOUNT="$gcp_acct"
KUBE_CONTEXT="$kube"
SECRET_KEYS="$secret_keys"
EXTRA_ENVS="$extra_envs"
EOF
  chmod 600 "$CTX_PROFILES_DIR/${profile}.conf"
  success "Profile config written"

  # 2. Git identity file (used by includeIf)
  mkdir -p "$HOME/.config/git"
  cat > "$HOME/.config/git/ctx-${profile}" <<EOF
[user]
  name  = $git_name
  email = $git_email
EOF
  success "Git identity file: ~/.config/git/ctx-${profile}"

  # 3. ~/.gitconfig includeIf — only one line added, deduped
  if [[ -n "$work_dir" ]]; then
    if gitconfig_add_include "$work_dir" "$HOME/.config/git/ctx-${profile}"; then
      success "gitconfig includeIf added for $work_dir"
    else
      dim  "  gitconfig includeIf already present"
    fi
  fi

  # 4. SSH host block → ~/.ssh/ctx_config (not ~/.ssh/config directly)
  if [[ -n "$ssh_key" && -f "$ssh_key" ]]; then
    local ssh_host="github-${profile}"
    if ctx_ssh_add_host "$ssh_host" "$ssh_key"; then
      success "SSH host '$ssh_host' → ~/.ssh/ctx_config"
      info "  Clone repos: git clone git@github-${profile}:org/repo.git"
    else
      dim "  SSH host '$ssh_host' already in ctx_config"
    fi
  fi

  # 5. mise.toml in work dir
  if [[ -n "$work_dir" ]]; then
    mkdir -p "$work_dir"
    if generate_mise_toml \
        "$profile" "$work_dir" "$git_name" "$git_email" \
        "$aws" "$azure_sub" "$gcp" "$kube" \
        "$secret_keys" "$extra_envs"
    then
      success "mise.toml generated: $work_dir/mise.toml"
      if command -v mise &>/dev/null; then
        info "  mise will auto-activate when you cd into $work_dir"
      else
        warn "  mise not installed — run 'ctx doctor' for install help"
      fi
    fi
  fi
}
