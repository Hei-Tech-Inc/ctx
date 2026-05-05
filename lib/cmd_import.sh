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
  dim "  Use your real email (e.g. you@company.com), not your GitHub username."
  while :; do
    GIT_EMAIL=$(ask "Git email for this client" "$current_email")
    [[ -z "$GIT_EMAIL" ]] && break
    if is_sensible_git_email "$GIT_EMAIL"; then
      break
    fi
    warn "That doesn't look like an email (expected user@domain.tld)."
  done
  echo ""

  # ── Working directory ───────────────────────────────────────────────────
  hr
  bold "  Working directory"
  echo ""
  local WORK_DIR work_root work_slug
  work_root="$(ctx_work_root)"
  info "Profiles default under: $work_root"
  work_slug=$(ask "Client folder name (not full path)" "$PROFILE_NAME")
  work_slug="${work_slug//\//-}"
  work_slug="$(echo "$work_slug" | sed 's/[^a-zA-Z0-9._-]/-/g')"
  [[ -z "$work_slug" ]] && die "Client folder name required."
  WORK_DIR="${work_root}/${work_slug}"
  echo ""

  # ── SSH key ─────────────────────────────────────────────────────────────
  hr
  bold "  SSH / GitHub"
  echo ""

  local SSH_KEY_PATH=""
  local SSH_KEY_GENERATED=0

  if [[ ${#ssh_keys[@]} -gt 0 ]]; then
    local key_choices=() picked choice_label
    local idx=1
    for key_path in "${ssh_keys[@]}"; do
      local fp=""
      if command -v ssh-keygen &>/dev/null; then
        fp=$(ssh-keygen -lf "${key_path}.pub" 2>/dev/null | awk '{print $2}')
      fi
      if [[ -n "$fp" ]]; then
        key_choices+=("[$idx] $(basename "$key_path")  ($fp)")
      else
        key_choices+=("[$idx] $(basename "$key_path")")
      fi
      idx=$((idx + 1))
    done

    choice_label=$(pick_one "Choose SSH key (recommended: Generate new key)" "Generate new key" "${key_choices[@]}")
    picked=""
    if [[ "$choice_label" != "Generate new key" && -n "$choice_label" ]]; then
      local picked_idx
      picked_idx=$(echo "$choice_label" | sed -n 's/^\[\([0-9]\+\)\].*/\1/p')
      if [[ "$picked_idx" =~ ^[0-9]+$ ]] && (( picked_idx >= 1 && picked_idx <= ${#ssh_keys[@]} )); then
        picked="${ssh_keys[$((picked_idx - 1))]}"
      fi
    fi

    if [[ "$picked" == "Generate new key" || -z "$picked" ]]; then
      SSH_KEY_PATH="$HOME/.ssh/ctx_${PROFILE_NAME}"
      _gen_ssh_key "$SSH_KEY_PATH" "$GIT_EMAIL"
      SSH_KEY_GENERATED=1
    else
      SSH_KEY_PATH="$picked"
      success "Using existing key: $(basename "$SSH_KEY_PATH")"
    fi
  else
    if ask_yn "No SSH keys found. Generate one for this profile?" "y"; then
      SSH_KEY_PATH="$HOME/.ssh/ctx_${PROFILE_NAME}"
      _gen_ssh_key "$SSH_KEY_PATH" "$GIT_EMAIL"
      SSH_KEY_GENERATED=1
    fi
  fi

  # ── GitHub account ──────────────────────────────────────────────────────
  local GITHUB_USER=""
  if has gh && ask_yn "Need to login/switch GitHub account now?" "n"; then
    info "Tip: choose 'Skip' for SSH key upload here; ctx manages per-profile keys."
    gh auth login || warn "gh auth login did not complete; continuing setup."
    gh auth switch 2>/dev/null || true
    gh_accounts=()
    while IFS= read -r a; do [[ -n "$a" ]] && gh_accounts+=("$a"); done < <(detect_gh_accounts)
  fi

  if [[ ${#gh_accounts[@]} -gt 0 ]]; then
    GITHUB_USER=$(pick_one "Which GitHub account for '$PROFILE_NAME'?" "None / skip" "${gh_accounts[@]}")
    [[ "$GITHUB_USER" == "None / skip" ]] && GITHUB_USER=""
    [[ -n "$GITHUB_USER" ]] && success "GitHub account: $GITHUB_USER"
  fi

  if [[ -z "$GITHUB_USER" ]]; then
    GITHUB_USER=$(ask "GitHub username for this client (leave blank to skip)")
  fi
  if [[ -n "$GITHUB_USER" && "$SSH_KEY_GENERATED" == "1" && -f "${SSH_KEY_PATH}.pub" ]]; then
    if has gh && ask_yn "Upload this new SSH key to GitHub account '$GITHUB_USER' now?" "y"; then
      gh auth switch -u "$GITHUB_USER" 2>/dev/null || true
      local key_title
      key_title="ctx-${PROFILE_NAME}-$(hostname)-$(date +%Y%m%d)"
      if gh ssh-key add "${SSH_KEY_PATH}.pub" --title "$key_title" 2>/dev/null; then
        success "SSH key uploaded to GitHub ($GITHUB_USER)"
      else
        warn "Could not upload key automatically. Add it manually at https://github.com/settings/keys"
      fi
    fi
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

  # ── Secrets ─────────────────────────────────────────────────────────────
  hr
  if [[ "$(uname -s)" == "Darwin" ]]; then
    bold "  Secrets (macOS Keychain)"
  else
    bold "  Secrets (~/.ctx/secrets, file per key — use full-disk encryption)"
  fi
  echo ""
  if [[ "$(uname -s)" == "Darwin" ]]; then
    dim "  Values you enter here go into Keychain."
  else
    dim "  Values you enter here go into ~/.ctx/secrets/<profile>/<KEY> (0600)."
  fi
  dim "  They are exported by ctx use and by mise hooks.enter (when mise runs)."
  echo ""

  local SECRET_KEYS=()

  _maybe_secret() {
    local key="$1" prompt="$2"
    local where="secrets"
    [[ "$(uname -s)" == "Darwin" ]] && where="Keychain"
    is_valid_env_key "$key" || { warn "Skipping invalid key '$key'"; return 1; }
    if ask_yn "$prompt?" "n"; then
      local val
      val=$(ask_secret "Value for $key")
      if [[ -n "$val" ]]; then
        keychain_set "$PROFILE_NAME" "$key" "$val" \
          && success "$key → $where" \
          || warn "Could not store $key ($where)"
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
    if ! is_valid_env_key "$ckey"; then
      warn "Invalid key '$ckey'. Use [A-Za-z_][A-Za-z0-9_]*."
      continue
    fi
    local cval
    cval=$(ask_secret "Value for $ckey")
    if [[ -n "$cval" ]]; then
      local where="secrets"
      [[ "$(uname -s)" == "Darwin" ]] && where="Keychain"
      keychain_set "$PROFILE_NAME" "$ckey" "$cval" \
        && success "$ckey → $where"
      SECRET_KEYS+=("$ckey")
    fi
  done
  echo ""

  # ── Non-secret env vars ─────────────────────────────────────────────────
  local EXTRA_ENVS=""
  if ask_yn "Add non-secret env vars (e.g. NODE_ENV=development)?" "n"; then
    while :; do
      local env_key env_val
      env_key=$(ask "Env key (leave blank to stop)")
      [[ -z "$env_key" ]] && break
      if ! is_valid_env_key "$env_key"; then
        warn "Invalid key '$env_key'. Use [A-Za-z_][A-Za-z0-9_]*."
        continue
      fi
      env_val=$(ask "Value for $env_key")
      EXTRA_ENVS+="${env_key}=${env_val}"$'\n'
    done
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
  backup_file "$HOME/.config/git/ctx-${PROFILE_NAME}"
  backup_file "$CTX_SSH_CONFIG"

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
      "$(dim "Check:    ctx status")" \
      "$(dim "Clone:    git clone git@github-${PROFILE_NAME}:OWNER/REPO.git")" \
      "$(dim "Never:    git@github.com — uses the wrong SSH key for this profile")"
  else
    success "Profile '${BOLD}${PROFILE_NAME}${RESET}${GREEN}' created."
    info "Activate: ctx use $PROFILE_NAME"
    info "Check:    ctx status"
    info "Clone:    git clone git@github-${PROFILE_NAME}:OWNER/REPO.git"
    dim "  Avoid git@github.com here — it won't use this profile's key."
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

  local passphrase=""
  if ask_yn "Protect this SSH key with a passphrase?" "y"; then
    passphrase=$(ask_secret "Passphrase (leave blank to use none)")
  fi
  ssh-keygen -t ed25519 -C "$email" -f "$key_path" -N "$passphrase" -q
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
  {
    printf '# ctx profile: %s\n' "$profile"
    printf '# Generated: %s\n' "$(date)"
    printf '# Secrets: macOS Keychain or ~/.ctx/secrets. Env vars are in mise.toml.\n\n'
    printf 'PROFILE_NAME=%q\n' "$profile"
    printf 'GIT_NAME=%q\n' "$git_name"
    printf 'GIT_EMAIL=%q\n' "$git_email"
    printf 'WORK_DIR=%q\n' "$work_dir"
    printf 'GITHUB_USER=%q\n' "$github_user"
    printf 'SSH_KEY_PATH=%q\n' "$ssh_key"
    printf 'AWS_PROFILE_NAME=%q\n' "$aws"
    printf 'AZURE_SUBSCRIPTION=%q\n' "$azure_sub"
    printf 'AZURE_TENANT=%q\n' "$azure_tenant"
    printf 'GCP_PROJECT=%q\n' "$gcp"
    printf 'GCP_ACCOUNT=%q\n' "$gcp_acct"
    printf 'KUBE_CONTEXT=%q\n' "$kube"
    printf 'SECRET_KEYS=%q\n' "$secret_keys"
    printf 'EXTRA_ENVS=%q\n' "$extra_envs"
  } > "$CTX_PROFILES_DIR/${profile}.conf"
  chmod 600 "$CTX_PROFILES_DIR/${profile}.conf"
  success "Profile config written"

  # 2. Git identity file (used by includeIf)
  mkdir -p "$HOME/.config/git"
  local git_identity_file="$HOME/.config/git/ctx-${profile}"
  : > "$git_identity_file"
  git config --file "$git_identity_file" user.name "$git_name"
  if is_sensible_git_email "$git_email"; then
    git config --file "$git_identity_file" user.email "$git_email"
    success "Git identity file: ~/.config/git/ctx-${profile}"
  else
    warn "Skipped git email in identity file (invalid): $git_email"
    success "Git identity file: ~/.config/git/ctx-${profile} (name only — fix email and re-run setup)"
  fi

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
