#!/usr/bin/env bash
# ctx/lib/cmd_ops.sh — v3: use, status, list, remove, undo, secret, doctor

# ─── use ─────────────────────────────────────────────────────────────────────
# ctx only removes agent keys it added itself (recorded as ssh_agent_added in ~/.ctx/config),
# so keys the user loaded stay put and plain github.com outside client folders keeps working.
_ctx_agent_added_key() {
  grep '^ssh_agent_added=' "$CTX_CONFIG" 2>/dev/null | tail -1 | cut -d= -f2-
}

_ctx_agent_has_key() {
  local fp
  [[ -f "$1.pub" ]] || return 1
  fp="$(ssh-keygen -lf "$1.pub" 2>/dev/null | awk '{print $2}')"
  [[ -n "$fp" ]] && ssh-add -l 2>/dev/null | awk '{print $2}' | grep -qxF "$fp"
}

_ctx_agent_drop_added_key() {
  local key
  key="$(_ctx_agent_added_key)"
  [[ -n "$key" ]] || return 0
  ssh-add -d "$key" >/dev/null 2>&1 || true
  _ctx_config_rm_key ssh_agent_added
}

# One shell-code line that sets NAME=VALUE in the caller's shell (bash/zsh or fish).
_ctx_emit_export() {
  local shell_kind="$1" k="$2" v="$3"
  if [[ "$shell_kind" == fish ]]; then
    printf "set -gx %s '%s'\n" "$k" "$(printf '%s' "$v" | sed "s/[\\\\']/\\\\&/g")"
  else
    printf 'export %s=%q\n' "$k" "$v"
  fi
}

# Variables ctx use wants in the caller's shell. ctx is a child process, so these only reach
# the shell through `ctx use --eval` (the ctx shell function and autoswitch hook eval it).
# Keep in sync with _ctx_deactivate_emit_unsets.
_ctx_use_export() {
  export "$1=$2"
  _ctx_use_k+=("$1")
  _ctx_use_v+=("$2")
}

# ctx use <profile> [--eval bash|fish]
# With --eval: messages go to stderr, stdout is only shell code for the caller to eval.
cmd_use() {
  local name="" eval_shell="" i rc
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --eval)
        eval_shell="bash"
        if [[ -n "${2:-}" && ( "$2" == bash || "$2" == zsh || "$2" == fish ) ]]; then
          eval_shell="$2"
          shift
        fi
        ;;
      --eval=*) eval_shell="${1#--eval=}" ;;
      -*) die "Usage: ctx use <profile> [--eval bash|fish]" ;;
      *) [[ -z "$name" ]] || die "Usage: ctx use <profile> [--eval bash|fish]"; name="$1" ;;
    esac
    shift
  done
  [[ -z "$name" ]] && die "Usage: ctx use <profile> [--eval bash|fish]"

  _ctx_use_k=() _ctx_use_v=()
  if [[ -n "$eval_shell" ]]; then
    [[ "$eval_shell" == fish ]] || eval_shell=bash
    _ctx_use_apply "$name" >&2
    rc=$?
    for ((i = 0; i < ${#_ctx_use_k[@]}; i++)); do
      _ctx_emit_export "$eval_shell" "${_ctx_use_k[i]}" "${_ctx_use_v[i]}"
    done
    return "$rc"
  fi

  _ctx_use_apply "$name"
  rc=$?
  # CTX_ACTIVE_PROFILE is always recorded; warn only when the profile sets something real.
  if [[ "${CTX_QUIET:-0}" == "0" && ${#_ctx_use_k[@]} -gt 1 ]]; then
    local names=""
    for ((i = 0; i < ${#_ctx_use_k[@]}; i++)); do
      [[ "${_ctx_use_k[i]}" == CTX_ACTIVE_PROFILE ]] || names+="${names:+, }${_ctx_use_k[i]}"
    done
    warn "These variables were not applied to this shell: ${names}. Run ctx install-hook and open a new shell, or run: eval \"\$(ctx use ${name} --eval bash)\"  (fish: ctx use ${name} --eval fish | source)" >&2
  fi
  return "$rc"
}

_ctx_use_apply() {
  local name="$1" quiet="${CTX_QUIET:-0}"
  load_profile "$name"

  [[ "$quiet" == "0" ]] && info "Switching to: ${BOLD}${name}${RESET}"

  local errors=0

  # 1. GitHub — the autoswitch hook sets GH_TOKEN per terminal instead of this machine-wide switch
  if [[ -n "${GITHUB_USER:-}" && "${CTX_AUTO_SWITCH:-0}" != "1" ]] && has gh; then
    if gh auth switch -u "$GITHUB_USER" 2>/dev/null; then
      [[ "$quiet" == "0" ]] && success "GitHub → $GITHUB_USER"
    else
      warn "gh switch failed for '$GITHUB_USER' — run: gh auth login"
      errors=$((errors+1))
    fi
  fi

  # 2. SSH key — add this profile's key; keep the user's own agent keys
  if [[ -n "${SSH_KEY_PATH:-}" && -f "$SSH_KEY_PATH" ]]; then
    [[ "$(_ctx_agent_added_key)" == "$SSH_KEY_PATH" ]] || _ctx_agent_drop_added_key
    if _ctx_agent_has_key "$SSH_KEY_PATH"; then
      [[ "$quiet" == "0" ]] && success "SSH key → $(basename "$SSH_KEY_PATH") (already in agent)"
    elif ssh-add "$SSH_KEY_PATH" 2>/dev/null; then
      _ctx_config_upsert_line ssh_agent_added "$SSH_KEY_PATH"
      [[ "$quiet" == "0" ]] && success "SSH key → $(basename "$SSH_KEY_PATH")"
    else
      warn "SSH key not loaded into the agent — github-${name} still reads it from disk"
    fi
  else
    _ctx_agent_drop_added_key
  fi
  ctx_git_identity_set_rewrite "$name" || true

  # 3. AWS
  if [[ -n "${AWS_PROFILE_NAME:-}" ]]; then
    _ctx_use_export AWS_PROFILE "$AWS_PROFILE_NAME"
    [[ "$quiet" == "0" ]] && success "AWS_PROFILE → $AWS_PROFILE_NAME"
  fi

  # 4. Azure
  if [[ -n "${AZURE_SUBSCRIPTION:-}" ]]; then
    if has az; then
      if az account set --subscription "$AZURE_SUBSCRIPTION" &>/dev/null; then
        [[ "$quiet" == "0" ]] && success "Azure → $AZURE_SUBSCRIPTION"
      else
        warn "Azure subscription switch failed"
        errors=$((errors+1))
      fi
    else
      _ctx_use_export AZURE_SUBSCRIPTION "$AZURE_SUBSCRIPTION"
      [[ -n "${AZURE_TENANT:-}" ]] && _ctx_use_export AZURE_TENANT "$AZURE_TENANT"
    fi
  fi

  # 5. GCP
  if [[ -n "${GCP_PROJECT:-}" ]]; then
    if has gcloud; then
      if gcloud config set project "$GCP_PROJECT" --quiet 2>/dev/null; then
        [[ "$quiet" == "0" ]] && success "GCP → $GCP_PROJECT"
      else
        warn "GCP project switch failed"
      fi
      [[ -n "${GCP_ACCOUNT:-}" ]] && \
        gcloud config set account "$GCP_ACCOUNT" --quiet 2>/dev/null || true
    else
      _ctx_use_export GCLOUD_PROJECT "$GCP_PROJECT"
    fi
  fi

  # 6. kubectl
  if [[ -n "${KUBE_CONTEXT:-}" ]]; then
    if has kubectl; then
      if kubectl config use-context "$KUBE_CONTEXT" &>/dev/null; then
        [[ "$quiet" == "0" ]] && success "kubectl → $KUBE_CONTEXT"
      else
        warn "kubectl context '$KUBE_CONTEXT' not found"
      fi
    fi
  fi

  # 7. Secrets → export to shell
  if [[ -n "${SECRET_KEYS:-}" ]]; then
    local _sec_label="secret store"
    [[ "$(uname -s)" == "Darwin" ]] && _sec_label="Keychain"
    for key in $SECRET_KEYS; do
      if ! is_valid_env_key "$key"; then
        warn "Skipping invalid secret key name '$key'"
        continue
      fi
      local val
      val=$(keychain_get "$PROFILE_NAME" "$key")
      if [[ -n "$val" ]]; then
        _ctx_use_export "$key" "$val"
        [[ "$quiet" == "0" ]] && success "Secret → $key ($_sec_label)"
      else
        warn "Secret '$key' missing ($_sec_label) — run: ctx secret set $PROFILE_NAME $key"
      fi
    done
  fi

  # 8. Reload mise/direnv if in work dir
  if [[ -n "${WORK_DIR:-}" ]]; then
    if has mise && [[ -f "${WORK_DIR}/mise.toml" ]] && [[ "$PWD" == "${WORK_DIR}"* ]]; then
      mise hook-env 2>/dev/null || true
    elif has direnv && [[ -f "${WORK_DIR}/.envrc" ]] && [[ "$PWD" == "${WORK_DIR}"* ]]; then
      direnv reload 2>/dev/null || true
    fi
  fi

  local prev_active
  prev_active="$(active_profile)"
  set_active_profile "$name"
  _ctx_use_export CTX_ACTIVE_PROFILE "$name"

  if [[ "${CTX_AUTO_SWITCH:-0}" == "1" ]]; then
    # The hook re-applying a profile already locked by hand (e.g. in a new terminal) keeps the lock.
    if [[ "$prev_active" != "$name" || "$(ctx_activation_get_source)" != manual ]]; then
      ctx_activation_set_auto
    fi
  else
    ctx_activation_set_manual "${PWD:-$HOME}"
  fi

  if [[ "$quiet" == "0" ]]; then
    echo ""
    if $HAS_GUM; then
      gum style --foreground 78 "Active context: $name"
    else
      success "Active context: ${BOLD}$name${RESET}"
    fi
    echo ""
  fi

  return $errors
}

# Emit shell code to unset profile env in the *current* shell (eval from a hook).
# Usage: eval "$(ctx deactivate --eval bash)"  |  fish: ctx deactivate --eval fish | source
_ctx_deactivate_emit_unsets() {
  local shell_kind="${1:-bash}" wd="$2"
  local key
  case "$shell_kind" in
    fish)
      for key in $SECRET_KEYS; do
        is_valid_env_key "$key" || continue
        printf 'set -e %s\n' "$key"
      done
      printf 'set -e AWS_PROFILE AZURE_SUBSCRIPTION AZURE_TENANT GCLOUD_PROJECT GCP_PROJECT GCP_ACCOUNT\n'
      printf 'set -e CTX_ACTIVE_PROFILE CTX_ACTIVATION_TRIGGER\n'
      printf 'set -e CTX_PROMPT_SHOW CTX_PROMPT_PROFILE CTX_PROMPT_WORK_DIR\n'
      if [[ -n "$wd" ]]; then
        local qwd
        qwd="${wd//\'/\'\\\'\'}"
        printf "set -l __ctx_wd '%s'\n" "$qwd"
        printf '%s\n' 'if string match "$__ctx_wd/*" "$PWD" >/dev/null 2>&1; or test "$PWD" = "$__ctx_wd"'
        printf '%s\n' '  git config --unset user.name 2>/dev/null; or true'
        printf '%s\n' '  git config --unset user.email 2>/dev/null; or true'
        printf '%s\n' 'end'
      fi
      ;;
    *)
      for key in $SECRET_KEYS; do
        is_valid_env_key "$key" || continue
        printf 'unset %s 2>/dev/null || true\n' "$key"
      done
      printf 'unset AWS_PROFILE AZURE_SUBSCRIPTION AZURE_TENANT GCLOUD_PROJECT GCP_PROJECT GCP_ACCOUNT 2>/dev/null || true\n'
      printf 'unset CTX_ACTIVE_PROFILE CTX_ACTIVATION_TRIGGER 2>/dev/null || true\n'
      printf 'unset CTX_PROMPT_SHOW CTX_PROMPT_PROFILE CTX_PROMPT_WORK_DIR 2>/dev/null || true\n'
      if [[ -n "$wd" ]]; then
        local q
        q="$(printf '%q' "$wd")"
        printf 'if [[ "$PWD" == %s* ]]; then git config --unset user.name 2>/dev/null || true; git config --unset user.email 2>/dev/null || true; fi\n' "$q"
      fi
      ;;
  esac
  if [[ -n "${EXTRA_ENVS:-}" ]]; then
    local pair k
    while IFS= read -r pair; do
      [[ -z "$pair" ]] && continue
      k="${pair%%=*}"
      is_valid_env_key "$k" || continue
      if [[ "$shell_kind" == "fish" ]]; then
        printf 'set -e %s\n' "$k"
      else
        printf 'unset %s 2>/dev/null || true\n' "$k"
      fi
    done <<< "$EXTRA_ENVS"
  fi
}

# ─── deactivate ───────────────────────────────────────────────────────────────
# Clears ~/.ctx/config active marker and prints eval-able unsets for the parent shell.
# Removes the SSH key ctx added to the agent; does not edit mise.toml or secret stores.
cmd_deactivate() {
  local eval_shell="bash" want_eval=false
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --eval)
        want_eval=true
        if [[ -n "${2:-}" && "$2" != --* ]]; then
          eval_shell="$2"
          shift
        fi
        shift
        ;;
      *)
        shift
        ;;
    esac
  done

  ctx_init_dirs
  _ctx_agent_drop_added_key
  local current quiet="${CTX_QUIET:-0}"
  current="$(active_profile)"
  if [[ -z "$current" ]]; then
    [[ "$quiet" == "0" ]] && dim "No active profile." >&2
    return 0
  fi

  load_profile "$current"
  local wd_use=""
  if [[ -n "${WORK_DIR:-}" && "$PWD" == "${WORK_DIR}"* ]]; then
    wd_use="$WORK_DIR"
  fi

  clear_active_profile_activation

  eval_shell="$(echo "${eval_shell:-bash}" | tr '[:upper:]' '[:lower:]')"
  [[ "$eval_shell" == "fish" ]] || eval_shell="bash"
  _ctx_deactivate_emit_unsets "$eval_shell" "$wd_use"

  if ! $want_eval && [[ "$quiet" == "0" ]]; then
    dim "Tip (fish): ctx deactivate --eval fish | source" >&2
    dim "Tip (bash/zsh): eval \"\$(ctx deactivate)\"" >&2
  fi
  return 0
}

# ─── status ───────────────────────────────────────────────────────────────────
cmd_status() {
  if [[ "${CTX_JSON:-0}" == "1" ]]; then
    ctx_init_dirs
    local current
    current=$(active_profile)
    if [[ -z "$current" ]]; then
      printf '{"version":"%s","command":"status","active":null}\n' "$(ctx_json_escape "$CTX_VERSION")"
      return 0
    fi
    load_profile "$current"
    local act_src act_note mp
    act_src="$(ctx_activation_get_source)"
    if [[ "$act_src" == "manual" ]]; then
      mp="$(ctx_activation_get_manual_pwd 2>/dev/null || true)"
      if [[ -n "$mp" && "$PWD" == "$mp" ]]; then
        act_note="manual_locked_at_pwd"
      else
        act_note="manual_cleared_auto_applies"
      fi
    else
      act_note="auto"
    fi
    printf '{"version":"%s","command":"status","active":"%s","activation":"%s","activation_note":"%s","git_name":"%s","git_email":"%s","work_dir":"%s","github_user":"%s","ssh_key_path":"%s","aws_profile":"%s","azure_subscription":"%s","gcp_project":"%s","kube_context":"%s","secret_keys":"%s","ctx_active_profile":"%s"}\n' \
      "$(ctx_json_escape "$CTX_VERSION")" \
      "$(ctx_json_escape "$current")" \
      "$(ctx_json_escape "$act_src")" \
      "$(ctx_json_escape "$act_note")" \
      "$(ctx_json_escape "${GIT_NAME:-}")" \
      "$(ctx_json_escape "${GIT_EMAIL:-}")" \
      "$(ctx_json_escape "${WORK_DIR:-}")" \
      "$(ctx_json_escape "${GITHUB_USER:-}")" \
      "$(ctx_json_escape "${SSH_KEY_PATH:-}")" \
      "$(ctx_json_escape "${AWS_PROFILE_NAME:-}")" \
      "$(ctx_json_escape "${AZURE_SUBSCRIPTION:-}")" \
      "$(ctx_json_escape "${GCP_PROJECT:-}")" \
      "$(ctx_json_escape "${KUBE_CONTEXT:-}")" \
      "$(ctx_json_escape "${SECRET_KEYS:-}")" \
      "$(ctx_json_escape "${CTX_ACTIVE_PROFILE:-}")"
    return 0
  fi

  ctx_init_dirs
  local current
  current=$(active_profile)

  if $HAS_GUM; then
    gum style \
      --border rounded --border-foreground 99 \
      --padding "0 2" --margin "1 0" \
      --bold "ctx status"
  else
    bold "\n  ctx status\n"
  fi

  if [[ -z "$current" ]]; then
    warn "No active profile. Run: ctx use <profile>"
    echo ""
    return
  fi

  load_profile "$current"

  local act_src act_note
  act_src="$(ctx_activation_get_source)"
  if [[ "$act_src" == "manual" ]]; then
    local mp=""
    mp="$(ctx_activation_get_manual_pwd 2>/dev/null || true)"
    if [[ -n "$mp" && "$PWD" == "$mp" ]]; then
      act_note="Activation: manual (locked at this directory until you cd elsewhere)"
    else
      act_note="Activation: manual anchor cleared — directory auto-switch applies on cd"
    fi
  else
    act_note="Activation: auto (directory hook + longest WORK_DIR prefix)"
  fi
  [[ -n "${CTX_ACTIVE_PROFILE:-}" ]] && act_note+=" · CTX_ACTIVE_PROFILE=$CTX_ACTIVE_PROFILE"

  if $HAS_GUM; then
    gum style --border normal --padding "0 1" \
      "Profile  : $(gum style --bold "$current")" \
      "Git      : ${GIT_NAME:-—} <${GIT_EMAIL:-—}>" \
      "Dir      : ${WORK_DIR:-—}" \
      "GitHub   : ${GITHUB_USER:-—}" \
      "SSH key  : ${SSH_KEY_PATH:+$(basename "$SSH_KEY_PATH")}" \
      "AWS      : ${AWS_PROFILE_NAME:-—}" \
      "Azure    : ${AZURE_SUBSCRIPTION:-—}" \
      "GCP      : ${GCP_PROJECT:-—}" \
      "kubectl  : ${KUBE_CONTEXT:-—}" \
      "Secrets  : ${SECRET_KEYS:-none}"
    dim "  $act_note"
  else
    hr
    echo -e "  Profile  : ${BOLD}$current${RESET}"
    echo "  Git      : ${GIT_NAME:-—} <${GIT_EMAIL:-—}>"
    echo "  Dir      : ${WORK_DIR:-—}"
    echo "  GitHub   : ${GITHUB_USER:-—}"
    echo "  SSH key  : ${SSH_KEY_PATH:+$(basename "$SSH_KEY_PATH")}"
    echo "  AWS      : ${AWS_PROFILE_NAME:-—}"
    echo "  Azure    : ${AZURE_SUBSCRIPTION:-—}"
    echo "  GCP      : ${GCP_PROJECT:-—}"
    echo "  kubectl  : ${KUBE_CONTEXT:-—}"
    echo "  Secrets  : ${SECRET_KEYS:-none}"
    dim "  $act_note"
    hr
  fi

  echo ""
  bold "  Live checks"
  echo ""

  # gh account
  if has gh && [[ -n "${GITHUB_USER:-}" ]]; then
    local active_gh
    active_gh=$(gh auth status 2>&1 | grep "Logged in to github.com account" \
      | head -1 | awk '{for (i = 1; i < NF; i++) if ($i == "account") { print $(i + 1); exit }}' | tr -d '()')
    if [[ "$active_gh" == "$GITHUB_USER" ]]; then
      success "gh account matches ($active_gh)"
    else
      warn "gh active: '$active_gh', expected: '$GITHUB_USER' — run: ctx use $current"
    fi
  fi

  # SSH
  if [[ -n "${SSH_KEY_PATH:-}" ]]; then
    if [[ ! -f "$SSH_KEY_PATH" ]]; then
      warn "SSH key file not found: $SSH_KEY_PATH"
    elif ssh-add -l 2>/dev/null | grep -q "$SSH_KEY_PATH"; then
      success "SSH key loaded in agent"
    else
      warn "SSH key not in agent — run: ctx use $current"
    fi
  fi

  # mise.toml
  if [[ -n "${WORK_DIR:-}" ]] && has mise; then
    if [[ -f "${WORK_DIR}/mise.toml" ]]; then
      success "mise.toml present: $WORK_DIR/mise.toml"
    else
      warn "mise.toml not found at $WORK_DIR/mise.toml"
      info "Run: ctx use $current  (to regenerate)"
    fi
  fi

  # AWS
  if has aws && [[ -n "${AWS_PROFILE_NAME:-}" ]]; then
    if aws configure list --profile "$AWS_PROFILE_NAME" &>/dev/null; then
      success "AWS profile valid: $AWS_PROFILE_NAME"
    else
      warn "AWS profile not found: $AWS_PROFILE_NAME"
    fi
  fi

  # Git identity in current dir
  if [[ -n "${WORK_DIR:-}" && "$PWD" == "${WORK_DIR}"* ]]; then
    local local_email
    local_email=$(git config user.email 2>/dev/null || echo "")
    if [[ "$local_email" == "$GIT_EMAIL" ]]; then
      success "Git identity in current dir: $local_email"
    else
      warn "Git email here: '$local_email' (expected '$GIT_EMAIL')"
      info "Are you inside $WORK_DIR, or did you run: mise trust $WORK_DIR/mise.toml?"
    fi
  fi

  echo ""
}

# ─── list ─────────────────────────────────────────────────────────────────────
cmd_list() {
  if [[ "${CTX_JSON:-0}" == "1" ]]; then
    ctx_init_dirs
    local current sep="" f pname found=false
    current=$(active_profile)
    printf '{"version":"%s","command":"list","profiles":[' "$(ctx_json_escape "$CTX_VERSION")"
    for f in "$CTX_PROFILES_DIR"/*.conf; do
      [[ -e "$f" ]] || continue
      found=true
      pname=$(basename "$f" .conf)
      unset GIT_NAME GIT_EMAIL WORK_DIR GITHUB_USER AWS_PROFILE_NAME
      unset AZURE_SUBSCRIPTION GCP_PROJECT KUBE_CONTEXT SECRET_KEYS SSH_KEY_PATH
      # shellcheck source=/dev/null
      source "$f"
      local active_json="false"
      [[ "$pname" == "$current" ]] && active_json="true"
      printf '%s{"name":"%s","active":%s,"git_name":"%s","git_email":"%s","work_dir":"%s","github_user":"%s","aws_profile":"%s","azure_subscription":"%s","gcp_project":"%s","kube_context":"%s","secret_keys":"%s"}' \
        "$sep" \
        "$(ctx_json_escape "$pname")" \
        "$active_json" \
        "$(ctx_json_escape "${GIT_NAME:-}")" \
        "$(ctx_json_escape "${GIT_EMAIL:-}")" \
        "$(ctx_json_escape "${WORK_DIR:-}")" \
        "$(ctx_json_escape "${GITHUB_USER:-}")" \
        "$(ctx_json_escape "${AWS_PROFILE_NAME:-}")" \
        "$(ctx_json_escape "${AZURE_SUBSCRIPTION:-}")" \
        "$(ctx_json_escape "${GCP_PROJECT:-}")" \
        "$(ctx_json_escape "${KUBE_CONTEXT:-}")" \
        "$(ctx_json_escape "${SECRET_KEYS:-}")"
      sep=","
    done
    printf ']}\n'
    if ! $found; then
      :
    fi
    return 0
  fi

  ctx_init_dirs
  local current
  current=$(active_profile)

  bold "\n  ctx profiles\n"

  local found=false
  for f in "$CTX_PROFILES_DIR"/*.conf; do
    [[ -e "$f" ]] || continue
    found=true
    local pname
    pname=$(basename "$f" .conf)
    unset GIT_NAME GIT_EMAIL WORK_DIR GITHUB_USER AWS_PROFILE_NAME
    unset AZURE_SUBSCRIPTION GCP_PROJECT KUBE_CONTEXT SECRET_KEYS
    # shellcheck source=/dev/null
    source "$f"

    local marker=""
    [[ "$pname" == "$current" ]] && marker=" ${GREEN}← active${RESET}"

    local tags=() tilde='~'
    [[ -n "${WORK_DIR:-}"          ]] && tags+=("${WORK_DIR/$HOME/$tilde}")
    [[ -n "${GITHUB_USER:-}"       ]] && tags+=("gh:$GITHUB_USER")
    [[ -n "${AWS_PROFILE_NAME:-}"  ]] && tags+=("aws:$AWS_PROFILE_NAME")
    [[ -n "${AZURE_SUBSCRIPTION:-}" ]] && tags+=("azure")
    [[ -n "${GCP_PROJECT:-}"       ]] && tags+=("gcp:$GCP_PROJECT")
    [[ -n "${KUBE_CONTEXT:-}"      ]] && tags+=("k8s:$KUBE_CONTEXT")
    [[ -n "${SECRET_KEYS:-}"       ]] && tags+=("$(echo "$SECRET_KEYS" | wc -w | tr -d ' ') secrets")

    echo -e "  ${BOLD}${pname}${RESET}${marker}"
    echo -e "  ${DIM}${GIT_NAME:-—} <${GIT_EMAIL:-—}>${RESET}"
    if [[ ${#tags[@]} -gt 0 ]]; then
      local joined="" tag
      for tag in "${tags[@]}"; do joined+="${joined:+ | }$tag"; done
      echo -e "  ${DIM}${joined}${RESET}"
    fi
    echo ""
  done

  if ! $found; then
    warn "No profiles yet."
    info "Run: ctx setup"
  fi
}

# ─── remove ───────────────────────────────────────────────────────────────────
cmd_remove() {
  local name="${1:-}"
  [[ -z "$name" ]] && die "Usage: ctx remove <profile>"
  profile_exists "$name" || die "Profile '$name' not found."

  load_profile "$name"

  bold "\n  Remove profile: $name\n"
  echo "  Git    : $GIT_NAME <$GIT_EMAIL>"
  echo "  Dir    : ${WORK_DIR:-—}"
  echo "  GitHub : ${GITHUB_USER:-—}"
  echo ""
  warn "This removes the profile config, git identity file, SSH host block, and stored secrets."
  warn "Your code, SSH key files, and mise.toml are NOT deleted."
  echo ""

  ask_yn "Remove profile '$name'?" "n" || die "Aborted."

  # Stored secrets
  if [[ -n "${SECRET_KEYS:-}" ]]; then
    for key in $SECRET_KEYS; do
      keychain_delete "$name" "$key"
    done
    success "Profile secrets removed"
  fi
  rm -rf "${CTX_DIR}/secrets/${name}" 2>/dev/null || true

  # SSH host block from ctx_config (not from ~/.ssh/config)
  ctx_ssh_remove_host "github-${name}"
  success "SSH host removed from ctx_config"

  # Git identity file + gitconfig include
  gitconfig_remove_include "$HOME/.config/git/ctx-${name}"
  rm -f "$HOME/.config/git/ctx-${name}"
  success "Git identity removed"

  local cur
  cur="$(active_profile)"
  if [[ "$cur" == "$name" ]]; then
    clear_active_profile_activation
    dim "Active profile was removed — run: eval \"\$(ctx deactivate)\" to clear shell env (or open a new terminal)."
  fi

  rm -f "$CTX_PROFILES_DIR/${name}.conf"
  success "Profile '$name' removed."
  echo ""
}

# ─── secret ───────────────────────────────────────────────────────────────────
cmd_secret() {
  local subcmd="${1:-}" profile="${2:-}" key="${3:-}"
  case "$subcmd" in
    set)
      [[ -z "$profile" || -z "$key" ]] && die "Usage: ctx secret set <profile> <KEY>"
      profile_exists "$profile" || die "Profile '$profile' not found."
      local val; val=$(ask_secret "Value for $key")
      local _lbl; _lbl="$(ctx_secret_store_label)"
      if ! keychain_set "$profile" "$key" "$val"; then
        die "Failed to store secret using provider '$_lbl'. Check provider config and required CLI tools."
      fi
      success "$key stored ($_lbl) for $profile"
      ;;
    get)
      [[ -z "$profile" || -z "$key" ]] && die "Usage: ctx secret get <profile> <KEY>"
      local val; val=$(keychain_get "$profile" "$key")
      [[ -z "$val" ]] && die "No secret '$key' for profile '$profile'"
      echo "$val"
      ;;
    list)
      [[ -z "$profile" ]] && die "Usage: ctx secret list <profile>"
      profile_exists "$profile" || die "Profile '$profile' not found."
      bold "\n  Secrets for: $profile\n"
      local keys; keys=$(keychain_list_keys "$profile")
      if [[ -z "$keys" ]]; then
        info "No secrets stored for this profile."
      else
        while IFS= read -r k; do
          [[ -n "$k" ]] && echo -e "  ${GREEN}•${RESET} $k"
        done <<< "$keys"
      fi
      echo ""
      ;;
    delete)
      [[ -z "$profile" || -z "$key" ]] && die "Usage: ctx secret delete <profile> <KEY>"
      local _lbl; _lbl="$(ctx_secret_store_label)"
      if ! keychain_delete "$profile" "$key"; then
        die "Failed to delete secret using provider '$_lbl'. Check provider config and required CLI tools."
      fi
      success "Deleted $key from $_lbl"
      ;;
    migrate)
      [[ -z "$profile" ]] && die "Usage: ctx secret migrate <profile> <to-provider> [from-provider]"
      profile_exists "$profile" || die "Profile '$profile' not found."
      local to_provider="${3:-}" from_provider="${4:-}"
      [[ -z "$to_provider" ]] && die "Usage: ctx secret migrate <profile> <to-provider> [from-provider]"
      [[ -z "$from_provider" ]] && from_provider="$(ctx_effective_secret_provider)"
      to_provider="$(echo "$to_provider" | tr '[:upper:]' '[:lower:]')"
      from_provider="$(echo "$from_provider" | tr '[:upper:]' '[:lower:]')"
      case "$to_provider" in keychain|file|pass) ;; *) die "Invalid to-provider: $to_provider" ;; esac
      case "$from_provider" in keychain|file|pass) ;; *) die "Invalid from-provider: $from_provider" ;; esac
      [[ "$to_provider" == "$from_provider" ]] && die "Source and destination providers are the same: $to_provider"

      local keys copied=0 missing=0 copied_keys=""
      keys="$(secret_list_with_provider "$from_provider" "$profile")"
      [[ -z "$keys" ]] && die "No secrets found for '$profile' in provider '$from_provider'."
      while IFS= read -r mkey; do
        [[ -z "$mkey" ]] && continue
        local mval
        mval="$(secret_get_with_provider "$from_provider" "$profile" "$mkey")"
        if [[ -z "$mval" ]]; then
          missing=$((missing+1))
          continue
        fi
        if secret_set_with_provider "$to_provider" "$profile" "$mkey" "$mval"; then
          copied=$((copied+1))
          copied_keys="${copied_keys}${mkey}"$'\n'
        fi
      done <<< "$keys"
      success "Migrated $copied secret(s) for '$profile' from $from_provider to $to_provider."
      [[ $missing -gt 0 ]] && warn "$missing key(s) were empty/unreadable and skipped."
      # Only offer to delete keys that were actually copied.
      if [[ $copied -gt 0 ]] && ask_yn "Delete migrated keys from source provider '$from_provider'?" "n"; then
        while IFS= read -r mkey; do
          [[ -n "$mkey" ]] && secret_delete_with_provider "$from_provider" "$profile" "$mkey" >/dev/null 2>&1 || true
        done <<< "$copied_keys"
        success "Removed source keys from '$from_provider'."
      fi
      ;;
    *) echo "Usage: ctx secret <set|get|list|delete|migrate> <profile> [KEY]" ;;
  esac
}

# ─── config ───────────────────────────────────────────────────────────────────
cmd_config() {
  ctx_init_dirs
  local key="${1:-}" value="${2:-}"

  case "$key" in
    ""|show)
      bold "\n  ctx config\n"
      echo "  work_root: $(ctx_work_root)"
      echo "  secret_provider: $(ctx_secret_provider) (effective: $(ctx_effective_secret_provider))"
      echo "  prompt_workdir_max_depth: $(ctx_config_prompt_workdir_max_depth) (prompt: CTX_PROMPT_* under each profile WORK_DIR)"
      echo "  prompt_extra_paths: $(ctx_config_prompt_extra_paths_or_default)"
      if [[ "$(ctx_config_autoswitch_notify)" == "1" ]]; then
        echo "  autoswitch_notify: on ([ctx] ←/→ messages on profile change)"
      else
        echo "  autoswitch_notify: off (default — silent autoswitch; screen-share safe)"
      fi
      echo ""
      info "Set with: ctx config work-root <path>"
      info "      or: ctx config secret-provider <auto|keychain|file|pass>"
      info "      or: ctx config prompt-workdir-depth <n>   # max path segments under WORK_DIR for CTX_PROMPT_SHOW (default 2)"
      info "      or: ctx config prompt-extra-paths '<p1>:<p2>'   # optional extra absolute prefixes (colon-separated)"
      info "      or: ctx config autoswitch-notify <on|off>   # [ctx] ←/→ lines (default off)"
      info "      or: ctx config export <dir> / ctx config import <dir>"
      ;;
    work-root)
      if [[ -z "$value" ]]; then
        echo "  work_root: $(ctx_work_root)"
        echo ""
        info "Usage: ctx config work-root <path>"
        return 0
      fi
      value="${value/#\~/$HOME}"
      mkdir -p "$value" || die "Could not create directory: $value"
      _ctx_config_upsert_line work_root "$value" || die "Could not update $CTX_CONFIG"
      success "work_root set to: $value"
      ;;
    secret-provider)
      if [[ -z "$value" ]]; then
        echo "  secret_provider: $(ctx_secret_provider) (effective: $(ctx_effective_secret_provider))"
        echo ""
        info "Usage: ctx config secret-provider <auto|keychain|file|pass>"
        return 0
      fi
      value="$(echo "$value" | tr '[:upper:]' '[:lower:]')"
      case "$value" in
        auto|keychain|file|pass) ;;
        *) die "Invalid secret provider '$value'. Use: auto, keychain, file, pass" ;;
      esac
      _ctx_config_upsert_line secret_provider "$value" || die "Could not update $CTX_CONFIG"
      success "secret_provider set to: $value (effective: $(ctx_effective_secret_provider))"
      ;;
    prompt-workdir-depth)
      if [[ -z "$value" ]]; then
        echo "  prompt_workdir_max_depth: $(ctx_config_prompt_workdir_max_depth)"
        echo ""
        info "Usage: ctx config prompt-workdir-depth <n>  (non-negative integer; 0 = only exact WORK_DIR)"
        return 0
      fi
      [[ "$value" =~ ^[0-9]+$ ]] || die "prompt-workdir-depth must be a non-negative integer"
      _ctx_config_upsert_line prompt_workdir_max_depth "$value"
      success "prompt_workdir_max_depth set to: $value"
      ;;
    prompt-extra-paths)
      if [[ -z "$value" ]]; then
        echo "  prompt_extra_paths: $(ctx_config_prompt_extra_paths_or_default)"
        echo ""
        info "Usage: ctx config prompt-extra-paths '<abs1>:<abs2>:…'  (colon-separated), or: ctx config prompt-extra-paths clear"
        return 0
      fi
      if [[ "$value" == "clear" ]]; then
        _ctx_config_rm_key prompt_extra_paths
        success "prompt_extra_paths cleared."
      else
        _ctx_config_upsert_line prompt_extra_paths "$value"
        success "prompt_extra_paths set."
      fi
      ;;
    autoswitch-notify)
      if [[ -z "$value" ]]; then
        if [[ "$(ctx_config_autoswitch_notify)" == "1" ]]; then
          echo "  autoswitch_notify: on"
        else
          echo "  autoswitch_notify: off"
        fi
        echo ""
        info "Usage: ctx config autoswitch-notify <on|off>"
        dim "  Default is off so new tabs / cd do not print client profile names (screen-share safe)."
        dim "  Session override: CTX_AUTOSWITCH_NOTIFY=1"
        return 0
      fi
      value="$(echo "$value" | tr '[:upper:]' '[:lower:]')"
      case "$value" in
        on|1|true|yes)
          _ctx_config_upsert_line autoswitch_notify on
          success "autoswitch_notify: on — [ctx] ←/→ messages enabled"
          ;;
        off|0|false|no)
          _ctx_config_upsert_line autoswitch_notify off
          success "autoswitch_notify: off — silent autoswitch (default)"
          ;;
        *)
          die "Usage: ctx config autoswitch-notify <on|off>"
          ;;
      esac
      ;;
    export)
      [[ -z "$value" ]] && die "Usage: ctx config export <directory>"
      local outdir
      outdir="${value/#\~/$HOME}"
      mkdir -p "$outdir" || die "Could not create export directory: $outdir"
      mkdir -p "$outdir/profiles" "$outdir/git-identities" || die "Could not prepare export structure"

      [[ -f "$CTX_CONFIG" ]] && cp "$CTX_CONFIG" "$outdir/config"
      cp "$CTX_PROFILES_DIR"/*.conf "$outdir/profiles/" 2>/dev/null || true
      cp "$HOME/.config/git"/ctx-* "$outdir/git-identities/" 2>/dev/null || true
      [[ -f "$CTX_SSH_CONFIG" ]] && cp "$CTX_SSH_CONFIG" "$outdir/ctx_ssh_config"

      {
        echo "ctx_export_version=1"
        echo "created_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
        echo "created_host=$(hostname)"
        echo "includes_secrets=no"
        echo "notes=Secrets are intentionally excluded. Use secret backend migration or manual re-entry."
      } > "$outdir/manifest.env"

      success "Config exported to: $outdir"
      warn "Secrets were NOT exported."
      info "Move this directory to the new laptop securely (encrypted transfer)."
      ;;
    import)
      [[ -z "$value" ]] && die "Usage: ctx config import <directory>"
      local indir
      indir="${value/#\~/$HOME}"
      [[ -d "$indir" ]] || die "Import directory not found: $indir"
      [[ -f "$indir/manifest.env" ]] || warn "No manifest found in import directory (continuing)."

      backup_file "$CTX_CONFIG" || warn "Could not back up $CTX_CONFIG" >&2
      backup_file "$CTX_SSH_CONFIG" || warn "Could not back up $CTX_SSH_CONFIG" >&2
      backup_file "$HOME/.gitconfig" || warn "Could not back up $HOME/.gitconfig" >&2

      [[ -f "$indir/config" ]] && cp "$indir/config" "$CTX_CONFIG"
      mkdir -p "$CTX_PROFILES_DIR" "$HOME/.config/git"
      cp "$indir/profiles"/*.conf "$CTX_PROFILES_DIR/" 2>/dev/null || true
      cp "$indir/git-identities"/ctx-* "$HOME/.config/git/" 2>/dev/null || true
      [[ -f "$indir/ctx_ssh_config" ]] && cp "$indir/ctx_ssh_config" "$CTX_SSH_CONFIG"

      ensure_ssh_include

      local f pname work_dir git_identity
      for f in "$CTX_PROFILES_DIR"/*.conf; do
        [[ -e "$f" ]] || continue
        pname=$(basename "$f" .conf)
        unset WORK_DIR
        # shellcheck source=/dev/null
        source "$f"
        work_dir="${WORK_DIR:-}"
        git_identity="$HOME/.config/git/ctx-${pname}"
        if [[ -n "$work_dir" && -f "$git_identity" ]]; then
          gitconfig_add_include "$work_dir" "$git_identity" >/dev/null 2>&1 || true
        fi
      done

      success "Config imported from: $indir"
      warn "Secrets are not included in exports; rehydrate via your provider (keychain/file/pass) or 'ctx secret'."
      ;;
    *)
      die "Usage: ctx config [show|work-root <path>|secret-provider <auto|keychain|file|pass>|prompt-workdir-depth <n>|prompt-extra-paths '<paths>'|autoswitch-notify <on|off>|export <dir>|import <dir>]"
      ;;
  esac
}

# ─── clone ────────────────────────────────────────────────────────────────────
cmd_clone() {
  local prof="" url="" rest=() seen_dd=false i idx="" tok

  _clone_token_is_url() {
    [[ "$1" == *://* ]] && return 0
    [[ "$1" == git@*:*/* ]] && return 0
    [[ "$1" == *:*/* ]] && return 0
    return 1
  }

  while [[ $# -gt 0 ]]; do
    case "$1" in
      -p|--profile)
        prof="${2:-}"
        [[ -n "$prof" ]] || die "Usage: ctx clone [-p|--profile <name>] [--] [<git-clone-args>...] <repo-url> [<dir>]"
        shift 2
        ;;
      --)
        seen_dd=true
        shift
        rest+=("$@")
        break
        ;;
      *)
        rest+=("$1")
        shift
        ;;
    esac
  done

  if $seen_dd; then
    idx=""
    for i in "${!rest[@]}"; do
      tok="${rest[$i]}"
      if _clone_token_is_url "$tok"; then
        idx="$i"
        break
      fi
    done
    if [[ -z "$idx" ]]; then
      local joined=""
      [[ ${#rest[@]} -eq 0 ]] || joined="${rest[*]}" # no empty-array expansion: unbound on bash 3.2
      die "Could not find a repo URL in: ctx clone ... -- $joined"
    fi
    url="${rest[$idx]}"
  else
    [[ ${#rest[@]} -ge 1 ]] || die "Usage: ctx clone [-p|--profile <name>] <repo-url> [git-clone-args...]"
    url="${rest[0]}"
  fi

  # --dry-run changes nothing, not even the ctx dirs.
  [[ "${CTX_DRY_RUN:-0}" == "1" ]] || ctx_init_dirs
  if [[ -z "$prof" ]]; then
    prof="$(active_profile)"
  fi
  [[ -n "$prof" ]] || die "No active profile. Run: ctx use <profile> or ctx clone -p <profile> <url>"
  load_profile "$prof"

  if [[ "${CTX_QUIET:-0}" == "0" ]]; then
    dim "[ctx] cloning as $prof (${GIT_EMAIL:-${GIT_NAME:-unknown}})"
  fi

  local host="github-${prof}"
  local https_as_ssh="n"
  if [[ "${CTX_QUIET:-0}" == "1" && "$url" == https://github.com/* ]]; then
    https_as_ssh="y"
  elif [[ "$url" == https://github.com/* ]]; then
    if ask_yn "Use SSH clone via $host instead of HTTPS?" "y"; then
      https_as_ssh="y"
    else
      info "Keeping HTTPS URL (uses gh/credential helper, not ctx SSH host)."
    fi
  fi

  local clone_url
  clone_url="$(github_clone_url_for_profile "$prof" "$url" "$https_as_ssh")"
  if [[ "$clone_url" != "$url" ]]; then
    if [[ "$url" == git@github.com:* || "$url" == ssh://git@github.com/* ]]; then
      info "Rewrote GitHub SSH URL → $clone_url"
    elif [[ "$url" == https://github.com/* && "$https_as_ssh" == "y" ]]; then
      info "Using: $clone_url"
    fi
  fi

  # Do not pass the original <repo-url> to git again — a second arg is the target
  # directory, so we would create a folder named like the full URL.
  local gargs=() _i
  if $seen_dd; then
    for ((_i = 0; _i < idx; _i++)); do
      gargs+=("${rest[_i]}")
    done
    gargs+=("$clone_url")
    for ((_i = idx + 1; _i < ${#rest[@]}; _i++)); do
      gargs+=("${rest[_i]}")
    done
  else
    gargs+=("$clone_url")
    for ((_i = 1; _i < ${#rest[@]}; _i++)); do
      gargs+=("${rest[_i]}")
    done
  fi

  if [[ "${CTX_DRY_RUN:-0}" == "1" ]]; then
    local shown="git clone" a
    for a in "${gargs[@]}"; do
      shown+=" $(printf '%q' "$a")"
    done
    dim "  [dry-run] would run: $shown"
    return 0
  fi
  git clone "${gargs[@]}"
}

# ─── verify ───────────────────────────────────────────────────────────────────
# The active github.com account in `gh auth status` output ($1): the one whose
# block says "- Active account: true"; gh versions without that line list the
# active account first. Account lines are "Logged in to github.com account USER"
# (newer) or "... as USER" (older); "Failed to log in" is not a login.
_ctx_gh_active_account() {
  printf '%s\n' "$1" | awk '
    { l = $0; t = l; sub(/^[ \t]+/, "", t); sub(/[ \t]+$/, "", t) }
    t ~ /^- Active account:/ {
      if (cur > 0) {
        v = t; sub(/^- Active account:/, "", v); gsub(/^[ \t]+|[ \t]+$/, "", v)
        st[cur] = (v == "true") ? 1 : -1
      }
      next
    }
    l !~ /Logged in to / && l !~ /Failed to log in to / { next }
    {
      cur = 0
      if (l ~ /Logged in to github\.com (account|as) /)
        for (i = 1; i + 2 <= NF; i++)
          if ($i == "github.com" && ($(i+1) == "account" || $(i+1) == "as") && $(i-1) == "to") {
            n++; user[n] = $(i+2); cur = n; break
          }
    }
    END {
      known = 0
      for (i = 1; i <= n; i++) {
        if (st[i] == 1) { print user[i]; exit }
        if (st[i] != 0) known = 1
      }
      if (!known && n > 0) print user[1]
    }'
}

cmd_verify() {
  local name="${1:-}"
  ctx_init_dirs
  [[ -z "$name" ]] && name="$(active_profile)"
  [[ -n "$name" ]] || die "Usage: ctx verify <profile> (or activate a profile first)"
  profile_exists "$name" || die "Profile '$name' not found."
  load_profile "$name"

  bold "\n  ctx verify: $name\n"
  dim "  One profile — live ssh -T to github-${name}, gh vs profile, paths, email."
  echo ""

  local ok=true
  local host="github-${name}"

  [[ -n "${WORK_DIR:-}" && -d "$WORK_DIR" ]] && success "Work dir exists: $WORK_DIR" || {
    warn "Work dir missing: ${WORK_DIR:-}"; ok=false
  }

  if [[ -n "${SSH_KEY_PATH:-}" ]]; then
    [[ -f "$SSH_KEY_PATH" ]] && success "SSH private key: $SSH_KEY_PATH" || {
      warn "SSH private key missing: $SSH_KEY_PATH"; ok=false
    }
    if [[ -f "$CTX_SSH_CONFIG" ]] && grep -q "^Host ${host}$" "$CTX_SSH_CONFIG" 2>/dev/null; then
      success "SSH config has Host $host"
    else
      warn "SSH config missing Host $host in $CTX_SSH_CONFIG"; ok=false
    fi
    if has ssh && [[ -f "$SSH_KEY_PATH" ]]; then
      local st
      st=$(ssh -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -T "git@${host}" 2>&1 || true)
      if echo "$st" | grep -qi "successfully authenticated"; then
        success "ssh -T git@${host}: OK"
      else
        warn "ssh -T git@${host} failed or unexpected: ${st//$'\n'/ }"
        ok=false
      fi
    fi
  else
    warn "No SSH_KEY_PATH in profile"
    ok=false
  fi

  if [[ -n "${GITHUB_USER:-}" ]] && has gh; then
    local active_gh
    active_gh=$(_ctx_gh_active_account "$(gh auth status 2>&1)")
    if [[ "$active_gh" == "$GITHUB_USER" ]]; then
      success "gh active user matches: $GITHUB_USER"
    else
      warn "gh active: '$active_gh', profile expects: '$GITHUB_USER' — run: ctx use $name"
      ok=false
    fi
  fi

  if [[ -n "${GIT_EMAIL:-}" ]]; then
    if is_sensible_git_email "$GIT_EMAIL"; then
      success "GIT_EMAIL looks valid: $GIT_EMAIL"
    else
      warn "GIT_EMAIL looks invalid: $GIT_EMAIL"
      ok=false
    fi
  fi

  if $ok; then success "Profile verification passed."; else warn "Profile verification found issues."; fi
  echo ""
  $ok
}

# ─── uninstall ──────────────────────────────────────────────────────────────────
# Physical path of an existing directory (cd -P + pwd -P: symlinks resolved
# before any .., as the kernel does); fails when it cannot cd.
_ctx_phys_dir() { (cd -P -- "$1" 2>/dev/null && pwd -P); }

# Physical path of a file: symlinks of the last component followed (even to a
# missing target), the directory resolved. Fails on a link loop or a missing dir.
_ctx_phys_file() {
  local p="$1" l i=0 d
  while [[ -L "$p" ]]; do
    [[ $i -ge 40 ]] && return 1
    i=$((i + 1))
    l="$(readlink "$p")" || return 1
    case "$l" in /*) p="$l" ;; *) p="$(dirname "$p")/$l" ;; esac
  done
  d="$(_ctx_phys_dir "$(dirname "$p")")" || return 1
  printf '%s/%s\n' "${d%/}" "$(basename "$p")"
}

# Whether path P has a . or .. component (empty components are fine).
_ctx_has_dot_component() {
  case "/$1/" in */./*|*/../*) return 0 ;; esac
  return 1
}

# Whether directory CAND is PATH or one of its ancestors, by file identity
# (-ef), never by string: case-insensitive file systems and symlinks make
# equal directories with different spellings. A PATH that does not exist is
# walked from its nearest existing ancestor; the walk is over physical paths.
_ctx_holds() {
  local cand="$1" d="$2" ph
  while [[ ! -e "$d" && "$d" != / && "$d" != . ]]; do d="$(dirname "$d")"; done
  ph="$(_ctx_phys_dir "$d")" || ph="$d"
  while :; do
    [[ "$ph" -ef "$cand" ]] && return 0
    [[ "$ph" == / || "$ph" == . ]] && return 1
    ph="$(dirname "$ph")"
  done
}

# Whether `rm -rf DIR` is safe; KIND is lib (the install's lib dir) or data
# (CTX_DIR for --purge); further args are install dirs a data dir must not
# hold. On success it prints the physical path to delete (the caller deletes
# that, never DIR as given); otherwise it prints why not and returns 1: a
# newline in it, not absolute, a . or .. component, a symlink (trailing
# slashes ignored), not a directory, /, $HOME or a directory holding it,
# CTX_DIR's parent; a lib dir must be named ctx, hold core.sh, not hold
# CTX_DIR and not be a git checkout; a data dir must be named .ctx or hold a
# config file or profiles dir (R15), and not hold the install. Directories
# are compared by identity (-ef), not by name.
_ctx_rm_rf_ok() {
  local dir="$1" kind="$2" p t keep
  shift 2
  case "$dir" in *$'\n'*) echo "it contains a newline"; return 1 ;; esac
  case "$dir" in /*) ;; *) echo "it is not an absolute path"; return 1 ;; esac
  _ctx_has_dot_component "$dir" && { echo "it has a . or .. component"; return 1; }
  t="$dir"
  while [[ "$t" == */ && "$t" != / ]]; do t="${t%/}"; done
  [[ "$t" != / && -L "$t" ]] && { echo "it is a symlink"; return 1; }
  [[ -d "$t" ]] || { echo "it is not a directory"; return 1; }
  p="$(_ctx_phys_dir "$t")" || { echo "it cannot be resolved"; return 1; }
  case "$p" in *$'\n'*) echo "it resolves to a path with a newline"; return 1 ;; esac
  [[ "$p" -ef "$t" ]] || { echo "it cannot be resolved"; return 1; }
  [[ "$p" -ef / ]] && { echo "it is /"; return 1; }
  _ctx_holds "$p" "$HOME" && { echo "it is your home directory or holds it"; return 1; }
  if [[ "$p" -ef "$CTX_DIR/.." || "$p" -ef "$(dirname "$CTX_DIR")" ]]; then
    echo "it is the parent of CTX_DIR"; return 1
  fi
  if [[ "$kind" == lib ]]; then
    [[ "$(basename "$p")" == ctx ]] || { echo "its name is not 'ctx'"; return 1; }
    _ctx_holds "$p" "$CTX_DIR" && { echo "it holds CTX_DIR"; return 1; }
    [[ -f "$p/core.sh" ]] || { echo "it has no core.sh"; return 1; }
    [[ -e "$p/.git" ]] && { echo "it is a git checkout"; return 1; }
  else
    [[ "$(basename "$t")" == .ctx || -f "$p/config" || -d "$p/profiles" ]] \
      || { echo "it does not look like a ctx data directory (no config or profiles)"; return 1; }
    for keep in "$@"; do
      _ctx_holds "$p" "$keep" && { echo "it holds the ctx install ($keep)"; return 1; }
    done
  fi
  printf '%s\n' "$p"
}

# Print what to remove by hand after a refusal. Args: ctx path, lib dir ("" when
# unknown), purge (true|false).
_ctx_uninstall_manual() {
  local bin="$1" lib="$2" purge="$3"
  info "To uninstall by hand, remove:"
  dim "  $bin"
  [[ -e "$(dirname "$bin")/ctx-core" || -L "$(dirname "$bin")/ctx-core" ]] && dim "  $(dirname "$bin")/ctx-core"
  if [[ -n "$lib" ]]; then
    dim "  $lib"
  else
    dim "  its lib dir (the CTX_LIB= line in $bin, if any)"
  fi
  if $purge; then
    dim "  $CTX_DIR"
    dim "  Delete each profile's secrets first: ctx secret list <profile>, then ctx secret delete <profile> <KEY>."
  fi
}

# Refuse: the error, then what to remove by hand (uses cmd_uninstall's ctx_bin,
# install_lib and purge_data), exit 1.
_ctx_uninstall_refuse() {
  error "$1"
  _ctx_uninstall_manual "$ctx_bin" "$install_lib" "$purge_data"
  exit 1
}

# Secret keys left for profile P in provider PROV, one per line.
# Fails when the store could not be listed.
_ctx_uninstall_keys() {
  local out
  out="$(secret_list_with_provider "$1" "$2")" || return 1
  printf '%s\n' "$out" | grep . || true
}

# ctx uninstall [--purge]. The ctx on PATH must be the running one, a regular
# file with the installer's CTX_LIB="..." line, whose lib dir is this ctx's;
# every path is checked (_ctx_rm_rf_ok) before the prompt, and any doubt is a
# refusal with nothing removed and a list of what to remove by hand. What is
# deleted is the checked physical path. Removes the guard (when on), ctx,
# ctx-core next to it, the lib dir and, with --purge, each profile's secrets
# (the store must be available; a key left behind, or a store that cannot be
# listed, stops everything) and CTX_DIR.
cmd_uninstall() {
  local purge_data=false
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --purge) purge_data=true; shift ;;
      *) die "Usage: ctx uninstall [--purge]" ;;
    esac
  done

  local ctx_bin bin lib_line install_lib="" lib="" data="" core_bin why me failed=0
  ctx_bin="$(command -v ctx 2>/dev/null || true)"
  [[ -z "$ctx_bin" ]] && die "ctx not found in PATH"
  if [[ -L "$ctx_bin" ]]; then
    _ctx_uninstall_refuse "Refusing to uninstall: $ctx_bin is a symlink (to $(readlink "$ctx_bin")). Remove the link yourself, or put the installed ctx first on PATH."
  fi
  [[ -f "$ctx_bin" ]] || _ctx_uninstall_refuse "Refusing to uninstall: $ctx_bin is not a regular file."
  me="$(_ctx_phys_file "$CTX_BIN/ctx" || true)"
  bin="$(_ctx_phys_file "$ctx_bin" || true)"
  [[ -n "$me" && "$bin" == "$me" && "$bin" -ef "$ctx_bin" ]] \
    || _ctx_uninstall_refuse "Refusing to uninstall: the ctx on PATH ($ctx_bin) is not the one running ($CTX_BIN/ctx)."
  lib_line="$(grep -m1 '^CTX_LIB=' "$ctx_bin" 2>/dev/null || true)"
  if [[ "$lib_line" =~ ^CTX_LIB=\"(/[^\"]*)\"$ ]]; then
    install_lib="${BASH_REMATCH[1]}"
  else
    _ctx_uninstall_refuse "Refusing to uninstall: $ctx_bin has no installer CTX_LIB line (a source checkout?). Remove it by hand."
  fi
  [[ "$(_ctx_phys_dir "$install_lib" || true)" == "$(_ctx_phys_dir "$CTX_LIB" || echo "?")" ]] \
    || _ctx_uninstall_refuse "Refusing to uninstall: $ctx_bin names $install_lib, but this ctx runs from $CTX_LIB."
  why="$(_ctx_rm_rf_ok "$install_lib" lib)" || _ctx_uninstall_refuse "Refusing to remove $install_lib: $why."
  lib="$why"
  local cdt="$CTX_DIR"
  while [[ "$cdt" == */ && "$cdt" != / ]]; do cdt="${cdt%/}"; done
  if $purge_data && [[ -e "$cdt" || -L "$cdt" ]]; then
    why="$(_ctx_rm_rf_ok "$CTX_DIR" data "$(dirname "$bin")" "$lib")" || _ctx_uninstall_refuse "Refusing to remove $CTX_DIR: $why."
    data="$why"
  fi
  core_bin="$(dirname "$bin")/ctx-core"
  [[ -e "$core_bin" || -L "$core_bin" ]] || core_bin=""

  local provider label profiles=() p keys k n left
  if $purge_data; then
    provider="$(ctx_effective_secret_provider)"
    label="$(ctx_secret_store_label)"
    ctx_secret_provider_available \
      || _ctx_uninstall_refuse "Refusing to purge: the $label ($provider) is not available here, so the profiles' secrets cannot be deleted."
    while IFS= read -r p; do [[ -n "$p" ]] && profiles+=("$p"); done < <(profile_list)
  fi

  bold "\n  ctx uninstall\n"
  warn "This removes the ctx binary and library scripts from your install location."
  $purge_data && warn "Also deletes $CTX_DIR (profiles, backups) and every profile's secrets in the $label."
  dim "  $bin"
  [[ -n "$core_bin" ]] && dim "  $core_bin"
  dim "  $lib"
  [[ -n "$data" ]] && dim "  $data"
  echo ""

  if [[ "${CTX_DRY_RUN:-0}" == 1 ]]; then
    for p in "${profiles[@]+"${profiles[@]}"}"; do
      if keys="$(_ctx_uninstall_keys "$provider" "$p")"; then
        n="$(printf '%s' "$keys" | grep -c . || true)"
        [[ "$n" -gt 0 ]] && dim "  [dry-run] would delete $n secret(s) of $p from the $label"
      else
        dim "  [dry-run] could not list the secrets of $p in the $label"
      fi
    done
    _ctx_guard_installed && dim "  [dry-run] would run ctx guard uninstall"
    dim "  [dry-run] would remove $bin"
    [[ -n "$core_bin" ]] && dim "  [dry-run] would remove $core_bin"
    dim "  [dry-run] would remove $lib"
    [[ -n "$data" ]] && dim "  [dry-run] would remove $data"
    echo ""
    return 0
  fi

  ask_yn "Continue?" "n" || die "Aborted."

  # Secrets first; the store is listed again after deleting, and any key left
  # stops everything (the stores' delete commands do not report failures).
  for p in "${profiles[@]+"${profiles[@]}"}"; do
    if ! keys="$(_ctx_uninstall_keys "$provider" "$p")"; then
      error "Could not list the secrets of $p in the $label"
      failed=1
      continue
    fi
    [[ -z "$keys" ]] && continue
    while IFS= read -r k; do
      secret_delete_with_provider "$provider" "$p" "$k" || true
    done <<<"$keys"
    if ! left="$(_ctx_uninstall_keys "$provider" "$p")"; then
      error "Could not list the secrets of $p in the $label after deleting; cannot verify they are gone"
      failed=1
      continue
    fi
    n="$(printf '%s\n' "$keys" | grep -c .)"
    [[ -n "$left" ]] && n=$((n - $(printf '%s\n' "$left" | grep -c .)))
    [[ $n -gt 0 ]] && success "Deleted $n secret(s) of $p from the $label"
    if [[ -n "$left" ]]; then
      error "Could not delete from the $label for $p: $(printf '%s' "$left" | tr '\n' ' ')"
      failed=1
    fi
  done
  if [[ $failed -ne 0 ]]; then
    info "Delete them with: ctx secret delete <profile> <KEY> (or in the $label), then run ctx uninstall --purge again."
    die "Some secrets could not be deleted; nothing else was removed."
  fi

  _ctx_guard_installed && _ctx_guard_uninstall
  if rm -f "$bin" 2>/dev/null; then success "Removed $bin"; else error "Could not remove $bin"; failed=1; fi
  if [[ -n "$core_bin" ]]; then
    if rm -f "$core_bin" 2>/dev/null; then success "Removed $core_bin"; else error "Could not remove $core_bin"; failed=1; fi
  fi
  if rm -rf "$lib" 2>/dev/null; then success "Removed $lib"; else error "Could not remove $lib"; failed=1; fi
  if [[ -n "$data" ]]; then
    if rm -rf "$data" 2>/dev/null; then success "Removed $data"; else error "Could not remove $data"; failed=1; fi
  fi

  echo ""
  info "Remove the ctx blocks from your shell rc if desired (search for '# BEGIN ctx profile autoswitch')."
  echo ""
  [[ $failed -eq 0 ]] || exit 1
}

# ─── upgrade ──────────────────────────────────────────────────────────────────
# Download install.sh from CTX_UPGRADE_REPO (default: main on GitHub) and run it
# with bash (CTX_UPGRADE_ONLY=1), aimed at the ctx on PATH when its CTX_LIB line
# is the installer's CTX_LIB="/abs" form. --check compares CTX_VERSION with the
# remote lib/core.sh. --dry-run prints what would be downloaded and run. The
# temp file is removed on every path (die, Ctrl+C, installer failure).
cmd_upgrade() {
  local repo="${CTX_UPGRADE_REPO:-https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/main}"
  local url="${repo}/install.sh"
  local ctx_bin install_bin install_lib lib_line

  ctx_bin="$(command -v ctx 2>/dev/null || true)"
  [[ -z "$ctx_bin" ]] && die "ctx not found in PATH — install first: curl -fsSL ${url} | bash"

  # Only the line install.sh writes: a source checkout's CTX_LIB="$(cd …" is not a path.
  if [[ -f "$ctx_bin" ]]; then
    lib_line="$(grep -m1 '^CTX_LIB=' "$ctx_bin" 2>/dev/null || true)"
    if [[ "$lib_line" =~ ^CTX_LIB=\"(/[^\"]*)\"$ ]]; then
      install_lib="${BASH_REMATCH[1]}"
      install_bin="$(dirname "$ctx_bin")"
    fi
  fi

  if [[ "${1:-}" == "--check" ]]; then
    bold "\n  ctx upgrade --check\n"
    dim "  Installed: ctx version $CTX_VERSION"
    local remote_ver="" rtmp
    rtmp="$(mktemp "${TMPDIR:-/tmp}/ctx-upgrade.XXXXXX")" || die "Could not create temp file"
    _ctx_temp_guard "$rtmp"
    if command -v curl &>/dev/null; then
      curl -fsSL "${repo}/lib/core.sh" -o "$rtmp" 2>/dev/null || true
    elif command -v wget &>/dev/null; then
      wget -qO "$rtmp" "${repo}/lib/core.sh" 2>/dev/null || true
    fi
    if [[ -s "$rtmp" ]]; then
      remote_ver="$(parse_ctx_version_from_core_sh_file "$rtmp")"
    fi
    _ctx_temp_done
    if [[ -n "$remote_ver" ]]; then
      dim "  Remote main: ctx version $remote_ver"
      if [[ "$CTX_VERSION" == "$remote_ver" ]]; then
        success "Already on latest advertised version ($remote_ver)."
      else
        info "Update available: $CTX_VERSION → $remote_ver (run: ctx upgrade)"
      fi
    else
      warn "Could not fetch remote version (offline?)."
    fi
    echo ""
    return 0
  fi

  bold "\n  ctx upgrade\n"
  dim "  Source: $url"
  [[ -n "${install_bin:-}" && -n "${install_lib:-}" ]] \
    && dim "  Target: $ctx_bin + $install_lib" \
    || dim "  Target: default install paths (see installer output)"
  echo ""
  info "Updates ctx binary + libraries only — ~/.ctx profiles, secrets, client dirs, and shell hooks are not modified."

  local fetch=""
  if command -v curl &>/dev/null; then
    fetch=curl
  elif command -v wget &>/dev/null; then
    fetch=wget
  fi

  if [[ "${CTX_DRY_RUN:-0}" == 1 ]]; then
    [[ -n "$fetch" ]] || die "Need curl or wget to download install.sh"
    local env_desc
    env_desc="CTX_REPO=$(printf '%q' "$repo") CTX_UPGRADE_ONLY=1"
    [[ -n "${install_bin:-}" ]] && env_desc+=" CTX_INSTALL_BIN=$(printf '%q' "$install_bin")"
    [[ -n "${install_lib:-}" ]] && env_desc+=" CTX_INSTALL_LIB=$(printf '%q' "$install_lib")"
    dim "  [dry-run] would download $url with $fetch"
    dim "  [dry-run] would run it with bash: $env_desc"
    echo ""
    return 0
  fi

  ask_yn "Continue?" "y" || die "Aborted."

  [[ -n "$fetch" ]] || die "Need curl or wget to download install.sh"
  local tmp
  tmp="$(mktemp "${TMPDIR:-/tmp}/ctx-upgrade.XXXXXX")" || die "Could not create temp file"
  _ctx_temp_guard "$tmp" # a die below, Ctrl+C or TERM removes it
  if [[ "$fetch" == curl ]]; then
    curl -fsSL "$url" -o "$tmp" || die "Download failed: $url"
  else
    wget -qO "$tmp" "$url" || die "Download failed: $url"
  fi

  # Normalise line endings — CRLF payloads make bash treat `\r` as part of tokens,
  # which surfaces as confusing errors like `n: command not found` at EOF.
  if command -v perl &>/dev/null; then
    perl -0777 -pi -e 's/\r\n/\n/g; s/\r/\n/g' "$tmp" 2>/dev/null || true
  else
    local tmp2
    if tmp2="$(mktemp "${TMPDIR:-/tmp}/ctx-upgrade.XXXXXX")"; then
      { tr -d '\r' <"$tmp" >"$tmp2" && mv -f "$tmp2" "$tmp"; } || rm -f "$tmp2"
    fi
  fi

  export CTX_REPO="$repo"
  export CTX_UPGRADE_ONLY=1
  [[ -n "${install_bin:-}" ]] && export CTX_INSTALL_BIN="$install_bin"
  [[ -n "${install_lib:-}" ]] && export CTX_INSTALL_LIB="$install_lib"

  bash "$tmp" || die "Upgrade installer failed"
  _ctx_temp_done

  unset CTX_UPGRADE_ONLY 2>/dev/null || true

  echo ""
  success "Upgrade finished."
  dim "  Open a new terminal or reload your shell if PATH looks stale."
  echo ""
}

# ─── undo ─────────────────────────────────────────────────────────────────────
# Make a fresh backup stamp dir and print it: $CTX_DIR/backups/<stamp>, or
# <stamp>-2, -3… when that one exists (an undo in the same second as the
# backup it restores must not write into it).
_ctx_backup_new_dir() {
  local base="${CTX_DIR}/backups" stamp d n=1
  stamp="$(date '+%Y%m%d-%H%M%S')"
  mkdir -p "$base" 2>/dev/null || return 1
  d="$base/$stamp"
  while ! mkdir "$d" 2>/dev/null; do
    n=$((n + 1))
    [[ $n -gt 99 ]] && return 1
    d="$base/$stamp-$n"
  done
  printf '%s\n' "$d"
}

# Back SRC up into backup dir BDIR, like backup_file does into its stamp dir.
_ctx_backup_into() {
  local bdir="$1" src="$2"
  cp -f "$src" "$bdir/$(basename "$src")" 2>/dev/null || return 1
  echo "$src" >> "$bdir/manifest.txt"
  log "Backed up $src → $bdir"
}

# Put SRC's content and mode at DST: written through a symlink, via a temp file
# in the target's directory renamed over it, so a failure leaves DST as it was.
_ctx_restore_file() {
  local src="$1" dst="$2" real mode tmp
  real="$(_ctx_phys_file "$dst")" || return 1
  [[ -d "$real" ]] && return 1
  mode="$(_ctx_file_mode "$src")" || return 1
  tmp="$(mktemp "$(dirname "$real")/.ctx-undo.XXXXXX" 2>/dev/null)" || return 1
  if cp "$src" "$tmp" 2>/dev/null && chmod "$mode" "$tmp" 2>/dev/null && mv -f "$tmp" "$real" 2>/dev/null; then
    return 0
  fi
  rm -f "$tmp"
  return 1
}

# ctx undo: restore the files of the latest backup. The backup keeps one copy
# per basename, so when two manifest paths share a basename neither is
# restored ("Ambiguous backup"); a path that is not absolute, has a . or ..
# component, is not in the backup, or whose directory is gone is skipped too. The files are listed
# before the prompt; the current versions go to a new backup first (so the
# undo can be undone); a restored file gets the backup's mode. Exit 1 when any
# file was skipped or failed.
cmd_undo() {
  local latest; latest=$(latest_backup)
  [[ -z "$latest" ]] && die "No backups found in ${CTX_DIR}/backups"

  local bdir="${CTX_DIR}/backups/$latest"
  bold "\n  Restoring from backup: $latest\n"

  [[ ! -f "$bdir/manifest.txt" ]] && die "Manifest not found at $bdir/manifest.txt"
  [[ -r "$bdir/manifest.txt" ]] || die "Cannot read $bdir/manifest.txt"

  local origs=() todo=() seen=$'\n' orig other fname same failed=0
  while IFS= read -r orig || [[ -n "$orig" ]]; do
    [[ -z "$orig" ]] && continue
    case "$seen" in *$'\n'"$orig"$'\n'*) continue ;; esac
    seen="$seen$orig"$'\n'
    origs+=("$orig")
  done < "$bdir/manifest.txt"

  for orig in "${origs[@]+"${origs[@]}"}"; do
    fname="$(basename "$orig")"
    same=0
    [[ "$fname" == manifest.txt ]] && same=1
    for other in "${origs[@]}"; do
      [[ "$other" != "$orig" && "$(basename "$other")" == "$fname" ]] && same=1
    done
    if [[ $same -eq 1 ]]; then
      warn "Ambiguous backup for $orig: another file in it has the same name; skipped"
      failed=1
    elif [[ "$orig" != /* ]]; then
      warn "Cannot restore $orig: not an absolute path; skipped"
      failed=1
    elif _ctx_has_dot_component "$orig"; then
      warn "Cannot restore $orig: it has a . or .. component; skipped"
      failed=1
    elif [[ ! -f "$bdir/$fname" ]]; then
      warn "Not in backup: $orig"
      failed=1
    elif [[ ! -d "$(dirname "$orig")" ]]; then
      warn "Cannot restore $orig: $(dirname "$orig") no longer exists; skipped"
      failed=1
    else
      dim "  $orig"
      todo+=("$orig")
    fi
  done
  [[ ${#todo[@]} -eq 0 ]] && die "Nothing to restore from $latest."
  echo ""

  if [[ "${CTX_DRY_RUN:-0}" == 1 ]]; then
    for orig in "${todo[@]}"; do
      [[ -f "$orig" ]] && dim "  [dry-run] would back up the current $orig"
      dim "  [dry-run] would restore $orig"
    done
    echo ""
    return 0
  fi

  ask_yn "Restore these files?" "y" || die "Aborted."

  # The backup dir is made before anything is restored.
  local nb=""
  for orig in "${todo[@]}"; do
    if [[ -f "$orig" ]]; then
      nb="$(_ctx_backup_new_dir)" || { error "Could not create a backup directory; nothing restored."; exit 1; }
      break
    fi
  done
  for orig in "${todo[@]}"; do
    if [[ -f "$orig" ]]; then
      if ! _ctx_backup_into "$nb" "$orig"; then
        error "Could not back up the current $orig; not restored"
        failed=1
        continue
      fi
    fi
    if _ctx_restore_file "$bdir/$(basename "$orig")" "$orig"; then
      success "Restored: $orig"
    else
      error "Could not restore $orig"
      failed=1
    fi
  done

  [[ -n "$nb" ]] && dim "  The replaced versions are in $nb (run ctx undo again to put them back)."
  if [[ $failed -eq 0 ]]; then
    success "Undo complete."
    echo ""
  else
    warn "Undo finished, but some files were not restored."
    echo ""
    exit 1
  fi
}

# ─── doctor ───────────────────────────────────────────────────────────────────
cmd_doctor() {
  if $HAS_GUM; then
    gum style --border rounded --border-foreground 99 \
      --padding "0 2" --margin "1 0" --bold "ctx doctor"
  else
    bold "\n  ctx doctor\n"
  fi

  dim "  Machine-wide — tools on PATH, shell hooks, SSH Include → ctx_config, profile files."
  dim "  For live GitHub SSH auth for one client, run: ctx verify <name>"
  echo ""

  local all_ok=true

  _chk() {
    local cmd="$1" label="$2" install="$3" ver_cmd="${4:-}"
    [[ -z "$ver_cmd" ]] && ver_cmd="$cmd --version"
    if has "$cmd"; then
      local ver
      ver="$(bash -lc "$ver_cmd" 2>&1 | sed -n '1p')"
      success "$label: ${ver:-installed}"
    else
      warn "$label: not found  →  $install"
      all_ok=false
    fi
  }

  local install_hint_git="brew install git"
  local install_hint_gum="brew install gum"
  local install_hint_gh="brew install gh"
  local install_hint_aws="brew install awscli"
  local install_hint_az="brew install azure-cli"
  local install_hint_gcloud="brew install --cask google-cloud-sdk"
  local install_hint_kubectl="brew install kubectl"
  if [[ "$(uname -s)" == "Linux" ]]; then
    install_hint_git="sudo apt-get install -y git"
    install_hint_gum="brew install gum (or install from github.com/charmbracelet/gum)"
    install_hint_gh="sudo apt-get install -y gh"
    install_hint_aws="pipx install awscli"
    install_hint_az="curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash"
    install_hint_gcloud="See: https://cloud.google.com/sdk/docs/install"
    install_hint_kubectl="sudo apt-get install -y kubectl"
  fi

  _chk git       "git"       "$install_hint_git"
  _chk mise      "mise"      "curl https://mise.run | sh"
  _chk gum       "gum"       "$install_hint_gum"
  _chk gh        "gh"        "$install_hint_gh"
  _chk aws       "AWS CLI"   "$install_hint_aws"
  _chk az        "Azure CLI" "$install_hint_az" "az --version"
  _chk gcloud    "gcloud"    "$install_hint_gcloud"
  _chk kubectl   "kubectl"   "$install_hint_kubectl" "kubectl version --client=true"

  local core_bin core_ver
  if core_bin="$(_ctx_core_bin)" && [[ -n "$core_bin" ]]; then
    core_ver="$("$core_bin" version 2>&1 | sed -n '1p')"
    core_ver="${core_ver#ctx-core }"
    success "ctx-core: ${core_ver:-installed} ($core_bin)"
  else
    warn "ctx-core missing — ctx mcp unavailable  →  reinstall ctx (install.sh)"
    all_ok=false
  fi

  echo ""
  hr

  local sp eff
  sp="$(ctx_secret_provider)"
  eff="$(ctx_effective_secret_provider)"
  if ctx_secret_provider_available; then
    success "secret provider: $sp (effective: $eff)"
  else
    warn "secret provider unavailable: $sp (effective: $eff)"
    case "$eff" in
      keychain) info "Install/enable macOS security Keychain tooling, or set: ctx config secret-provider file" ;;
      pass) info "Install pass (and initialize it), or set: ctx config secret-provider auto" ;;
    esac
    all_ok=false
  fi

  echo ""
  hr

  # mise shell activation
  local shell_rc=""
  [[ "$SHELL" == *"zsh"*  ]] && shell_rc="$HOME/.zshrc"
  [[ "$SHELL" == *"bash"* ]] && shell_rc="$HOME/.bashrc"
  [[ "$SHELL" == *"fish"* ]] && shell_rc="$HOME/.config/fish/config.fish"

  if [[ -n "$shell_rc" ]]; then
    grep -q "mise activate" "$shell_rc" 2>/dev/null \
      && success "mise shell hook: installed" \
      || { warn "mise activate: not in $shell_rc"; info "Run: ctx install-hook"; all_ok=false; }

    if grep -q '_ctx_profile_autoswitch' "$shell_rc" 2>/dev/null \
      || grep -q '# BEGIN ctx profile autoswitch' "$shell_rc" 2>/dev/null \
      || grep -q 'ctx auto-switch' "$shell_rc" 2>/dev/null; then
      success "ctx profile autoswitch: installed in $shell_rc"
    else
      warn "ctx profile autoswitch: not in $shell_rc"
      info "Run: ctx install-hook"
      all_ok=false
    fi
  fi

  echo ""
  hr

  # SSH isolation check
  if grep -Eq '^[[:space:]]*Include[[:space:]]+.*ctx_config' "$HOME/.ssh/config" 2>/dev/null; then
    success "SSH isolation: ctx_config is included"
  else
    warn "SSH isolation: ~/.ssh/ctx_config not included in ~/.ssh/config"
    info "Run: ctx setup (or any command that writes an SSH host) to auto-add Include, or add manually:"
    info "  Include $CTX_SSH_CONFIG"
    all_ok=false
  fi

  # ~/.ssh directory must not be group/other writable or OpenSSH may ignore keys/config
  if [[ -d "$HOME/.ssh" ]]; then
    if find "$HOME/.ssh" -maxdepth 0 \( -perm -020 -o -perm -002 \) 2>/dev/null | grep -q .; then
      warn "SSH: ~/.ssh is group- or world-writable (OpenSSH may refuse to use keys here)"
      info "Run: chmod 700 $HOME/.ssh"
      all_ok=false
    else
      success "SSH: ~/.ssh directory permissions are strict enough"
    fi
  else
    dim "  SSH: ~/.ssh not present yet (normal before first key or setup)"
  fi

  if _ctx_guard_installed; then
    success "Git guard: on (wrong-identity commits/pushes are blocked)"
  else
    dim "  Git guard: off (optional) — ctx guard install"
  fi

  echo ""
  hr

  # Profile health
  local pc=0
  for f in "$CTX_PROFILES_DIR"/*.conf; do
    [[ -e "$f" ]] || continue
    pc=$((pc+1))
    local pname; pname=$(basename "$f" .conf)
    unset WORK_DIR SSH_KEY_PATH
    # shellcheck source=/dev/null
    source "$f"

    local ok=true
    [[ -n "${WORK_DIR:-}" && ! -d "$WORK_DIR" ]] && {
      warn "Profile '$pname': work dir missing: $WORK_DIR"; ok=false
    }
    [[ -n "${WORK_DIR:-}" && ! -f "${WORK_DIR}/mise.toml" ]] && {
      warn "Profile '$pname': mise.toml missing in $WORK_DIR"; ok=false
    }
    [[ -n "${SSH_KEY_PATH:-}" && ! -f "$SSH_KEY_PATH" ]] && {
      warn "Profile '$pname': SSH key missing: $SSH_KEY_PATH"; ok=false
    }
    if $ok; then
      success "Profile '$pname': ok"
    else
      all_ok=false
    fi
  done

  [[ $pc -eq 0 ]] && info "No profiles yet. Run: ctx setup"

  echo ""
  $all_ok && success "Everything looks good." || warn "Fix the issues above."
  echo ""
}

# Same prompt line as CTX_PROMPT_* rules (depth + prompt_extra_paths); stdout only when in scope.
cmd_workdir_prompt() {
  ctx_init_dirs
  local line name root pmd pex d prompt_show=0 piece display_root=""
  [[ -d "$CTX_PROFILES_DIR" ]] || return 0
  line="$(ctx_resolve_path_with_root "${PWD:-$HOME}" "$CTX_PROFILES_DIR")" || return 0
  name="${line%%$'\t'*}"
  root="${line#*$'\t'}"
  root="${root//$'\r'/}"
  root="${root//$'\n'/}"

  pmd="$(ctx_config_prompt_workdir_max_depth)"
  pex=""
  [[ -f "$CTX_CONFIG" ]] && pex="$(grep "^prompt_extra_paths=" "$CTX_CONFIG" 2>/dev/null | tail -1 | cut -d= -f2-)"

  display_root=""
  [[ -n "$root" ]] && display_root="$root"

  if [[ -n "$display_root" ]]; then
    d="$(ctx_rel_path_depth_under "${PWD:-$HOME}" "$display_root")"
    d="${d//$'\n'/}"
    [[ "$d" -ge 0 && "$d" -le "$pmd" ]] && prompt_show=1
  fi

  if [[ "$prompt_show" != "1" && -n "$pex" ]]; then
    while IFS= read -r piece; do
      [[ -z "$piece" ]] && continue
      piece="${piece/#\~/$HOME}"
      piece="${piece%/}"
      if [[ -n "$piece" && ( "${PWD:-$HOME}" == "$piece" || "${PWD:-$HOME}" == "$piece/"* ) ]]; then
        prompt_show=1
        display_root="$piece"
        break
      fi
    done < <(printf '%s' "$pex" | tr ':' '\n')
  fi

  if [[ "$prompt_show" == "1" && -n "$display_root" ]]; then
    printf "work_dir='WORK_DIR=%s'\n" "$display_root"
  fi
}

# ─── init ─────────────────────────────────────────────────────────────────────
# ctx init: create the ctx dirs, offer to install missing tools (install.sh
# next to the lib dir, else brew) and the shell hooks. --dry-run asks nothing
# and only says what it would do.
cmd_init() {
  local dry=0
  [[ "${CTX_DRY_RUN:-0}" == 1 ]] && dry=1
  if [[ $dry -eq 0 ]]; then
    ctx_init_dirs
  elif [[ ! -d "$CTX_PROFILES_DIR" || ! -d "$CTX_DIR/backups" || ! -f "$CTX_CONFIG" ]]; then
    dim "  [dry-run] would create $CTX_DIR"
  fi
  ctx_header "Setup check"

  echo ""
  info "Checking your installation..."
  echo ""

  # Check for missing deps and offer to install
  local missing_tools=() t
  has mise || missing_tools+=("mise")
  has gum  || missing_tools+=("gum")
  has gh   || missing_tools+=("gh")

  if [[ ${#missing_tools[@]} -gt 0 ]]; then
    warn "Missing tools: ${missing_tools[*]}"
    if [[ $dry -eq 1 ]] || ask_yn "Install missing tools now?" "y"; then
      local installer
      installer="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/install.sh"
      if [[ -f "$installer" ]]; then
        if [[ $dry -eq 1 ]]; then
          dim "  [dry-run] would run bash $installer"
        else
          bash "$installer"
        fi
      else
        for t in "${missing_tools[@]}"; do
          if [[ $dry -eq 1 ]] && has brew; then
            dim "  [dry-run] would run brew install $t"
          elif [[ $dry -eq 0 ]] && has brew && brew install "$t" &>/dev/null; then
            success "$t installed"
          else
            warn "$t: install manually"
          fi
        done
      fi
    fi
  fi

  # Shell hook check
  local shell_rc=""
  [[ "${SHELL:-}" == *"zsh"*  ]] && shell_rc="$HOME/.zshrc"
  [[ "${SHELL:-}" == *"bash"* ]] && shell_rc="$HOME/.bashrc"
  [[ "${SHELL:-}" == *"fish"* ]] && shell_rc="$HOME/.config/fish/config.fish"

  if [[ -n "$shell_rc" ]]; then
    if ! grep -q "mise activate" "$shell_rc" 2>/dev/null || \
       { ! grep -q '_ctx_profile_autoswitch' "$shell_rc" 2>/dev/null \
         && ! grep -q '# BEGIN ctx profile autoswitch' "$shell_rc" 2>/dev/null; }; then
      if [[ $dry -eq 1 ]]; then
        cmd_install_hook "$shell_rc"
      elif ask_yn "Install shell hooks (mise + ctx auto-switch) to $shell_rc?" "y"; then
        cmd_install_hook "$shell_rc"
      else
        info "Skipped. Run: ctx install-hook"
      fi
    else
      success "Shell hooks already installed"
    fi
  fi

  echo ""
  info "Config dir: $CTX_DIR"
  info "SSH config: $CTX_SSH_CONFIG"
  echo ""
  bold "  Next steps:"
  echo ""
  echo "  ctx setup     Configure your first client profile"
  echo "  ctx doctor    Tools, hooks, SSH layout (see also: ctx verify <profile>)"
  echo ""
}

# ─── install-hook ─────────────────────────────────────────────────────────────
# The shell an rc file is for, from its name: fish (config.fish), zsh (a name
# with "zsh" in it) or bash.
_ctx_rc_shell() {
  case "$1" in
    config.fish|*/config.fish) echo fish ;;
    *)
      case "$(basename -- "$1")" in
        *zsh*) echo zsh ;;
        *)     echo bash ;;
      esac
      ;;
  esac
}

# The headers of the full hook pastes ctx made from 970aa1a to a65d9a9 (the
# whole ctx_autoswitch.bash or .fish appended to the rc), and of the pastes
# before them (62d7522 to 970aa1a: install-hook's and install.sh's), exactly
# as ctx wrote them; git history has no other.
CTX_PASTE_HEADER_BASH="# ctx profile autoswitch — appended to shell rc by install.sh / ctx install-hook"
CTX_PASTE_HEADER_FISH="# ctx profile autoswitch — fish (see lib/ctx_autoswitch.bash for behavior)"
CTX_LEGACY_HEADER_HOOK="# ── ctx auto-switch ───────────────────────────────────────────────────────────"
CTX_LEGACY_HEADER_INSTALLER="# ── ctx auto-switch — added by ctx installer ─────────────────────────────────"

# The line two above a bash paste's final "fi" in every historic version
# (970aa1a has the second, the others the first).
# shellcheck disable=SC2016 # literal rc text
CTX_PASTE_PROMPT_BASH='  PROMPT_COMMAND="_ctx_profile_autoswitch${PROMPT_COMMAND:+;$PROMPT_COMMAND}"'
# shellcheck disable=SC2016
CTX_PASTE_PROMPT_BASH_OLD='  PROMPT_COMMAND="${PROMPT_COMMAND//_ctx_auto_switch;/}"'

# Print which full paste (bash or fish) ends at line L, given the two lines
# before it (P1 right before); print nothing and return 1 when none does.
# Every historic bash paste ends with one of the PROMPT_COMMAND lines above,
# "  _ctx_profile_autoswitch", then "fi"; every fish paste ends "end", maybe a
# blank line, then "_ctx_profile_autoswitch". A user's own `fi` elsewhere is
# not such an end.
_ctx_rc_paste_end() {
  local p2="$1" p1="$2" l="$3"
  if [[ "$l" == "fi" && "$p1" == "  _ctx_profile_autoswitch" &&
    ( "$p2" == "$CTX_PASTE_PROMPT_BASH" || "$p2" == "$CTX_PASTE_PROMPT_BASH_OLD" ) ]]; then
    printf 'bash'
  elif [[ "$l" == _ctx_profile_autoswitch && ( "$p1" == "end" || ( -z "$p1" && "$p2" == "end" ) ) ]]; then
    printf 'fish'
  else
    return 1
  fi
}

# Print rc file RC without its ctx autoswitch blocks:
#   - "# BEGIN ctx profile autoswitch" .. "# END ctx profile autoswitch";
#   - a full hook paste (exact header above) through its end
#     (_ctx_rc_paste_end), when such an end comes later in the file;
#   - other legacy pastes (one of the exact headers above), which end at their
#     closing "# ─────…" line (25 or more ─) or just before the next blank
#     line (kept), whichever comes first; one with neither runs to the end of
#     the file.
# A line ending in CR is matched without it and printed as it was.
# Returns 3 (output incomplete) when a BEGIN has no END or a BEGIN comes
# inside an open BEGIN block, and 2 when writing the output failed.
_ctx_rc_strip_autoswitch() {
  local line l n=0 state=none last_bash=0 last_fish=0 pend="" p1="" p2="" b1 b2 k
  # Lookahead: the last line where a full paste of each kind could end.
  while IFS= read -r line || [[ -n "$line" ]]; do
    n=$((n + 1))
    l="${line%$'\r'}"
    case "$l" in
      fi | _ctx_profile_autoswitch)
        k="$(_ctx_rc_paste_end "$p2" "$p1" "$l")" || k=""
        [[ "$k" == bash ]] && last_bash=$n
        [[ "$k" == fish ]] && last_fish=$n
        ;;
    esac
    p2="$p1" p1="$l"
  done <"$1"
  n=0 p1="" p2=""
  while IFS= read -r line || [[ -n "$line" ]]; do
    n=$((n + 1))
    l="${line%$'\r'}"
    b2="$p2" b1="$p1" # the two lines before this one, for a paste end
    p2="$p1" p1="$l"
    case "$state" in
      begin)
        case "$l" in
          "# BEGIN ctx profile autoswitch"*) return 3 ;;
          "# END ctx profile autoswitch"*) state=none ;;
        esac
        continue
        ;;
      paste)
        case "$l" in
          fi | _ctx_profile_autoswitch)
            [[ "$(_ctx_rc_paste_end "$b2" "$b1" "$l")" == "$pend" ]] && state=none
            ;;
        esac
        continue
        ;;
      legacy)
        case "$l" in
          "# BEGIN ctx profile autoswitch"*) state=begin ;;
          "# END ctx profile autoswitch"*) state=none ;;
          "") state=none; printf '%s\n' "$line" || return 2 ;;
          *) _ctx_rc_is_rule "$l" && state=none ;;
        esac
        continue
        ;;
    esac
    if [[ "$l" == "$CTX_PASTE_HEADER_BASH" && $last_bash -gt $n ]]; then
      state=paste pend=bash
      continue
    fi
    if [[ "$l" == "$CTX_PASTE_HEADER_FISH" && $last_fish -gt $n ]]; then
      state=paste pend=fish
      continue
    fi
    case "$l" in
      "# BEGIN ctx profile autoswitch"*) state=begin ;;
      "# END ctx profile autoswitch"*) ;;
      "$CTX_PASTE_HEADER_BASH" | "$CTX_PASTE_HEADER_FISH" | "$CTX_LEGACY_HEADER_HOOK" | "$CTX_LEGACY_HEADER_INSTALLER") state=legacy ;;
      *) printf '%s\n' "$line" || return 2 ;;
    esac
  done <"$1"
  [[ "$state" != begin ]] || return 3
}

# Whether line L is "# " and 25 or more ─ (a legacy block's closing rule).
_ctx_rc_is_rule() {
  local r="${1#\# }"
  [[ "$1" == "# "* && "$r" == "─────────────────────────"* && -z "${r//─/}" ]]
}

# The autoswitch block install-hook appends for SHELL_KIND (fish|bash|zsh),
# sourcing ctx_autoswitch.* from LIB (or a standard lib dir).
_ctx_autoswitch_block() {
  local kind="$1" _as_lib="$2"
  echo ""
  echo "# BEGIN ctx profile autoswitch"
  echo "# Sourced from ctx lib so upgrades (notify mute, etc.) apply without editing this file."
  if [[ "$kind" == fish ]]; then
    echo "set -l _ctx_as_lib ''"
    echo "for _ctx_as_cand in '$_as_lib' \$HOME/.local/lib/ctx /usr/local/lib/ctx /opt/homebrew/lib/ctx"
    echo "  if test -f \"\$_ctx_as_cand/ctx_autoswitch.fish\""
    echo "    set _ctx_as_lib \$_ctx_as_cand"
    echo "    break"
    echo "  end"
    echo "end"
    echo "if test -n \"\$_ctx_as_lib\""
    echo "  source \"\$_ctx_as_lib/ctx_autoswitch.fish\""
    echo "end"
  else
    cat <<EOF
_ctx_as_lib=""
for _ctx_as_cand in "$_as_lib" "\${HOME}/.local/lib/ctx" "/usr/local/lib/ctx" "/opt/homebrew/lib/ctx"; do
  [[ -f "\${_ctx_as_cand}/ctx_autoswitch.bash" ]] && { _ctx_as_lib="\${_ctx_as_cand}"; break; }
done
if [[ -n "\${_ctx_as_lib}" ]]; then
  # shellcheck disable=SC1090
  source "\${_ctx_as_lib}/ctx_autoswitch.bash"
fi
unset _ctx_as_lib _ctx_as_cand 2>/dev/null || true
EOF
  fi
  echo "# END ctx profile autoswitch"
}

# ctx install-hook [RC]: add the mise activation (when mise is installed and
# RC has none) and the ctx autoswitch block to RC (default: by $SHELL), and the
# SSH Include. Every check comes before any change: the autoswitch lib must
# exist, RC must be a readable, writable file (or not exist yet, in an existing
# dir) whose BEGIN lines all have an END. RC is rewritten in one atomic
# write-through (symlinks and mode kept); lines it keeps keep their line
# endings (CRLF too), the appended lines end in LF. --dry-run says what it
# would do and changes nothing.
cmd_install_hook() {
  local shell_rc="${1:-}"

  if [[ -z "$shell_rc" ]]; then
    case "${SHELL:-}" in
      */zsh)  shell_rc="$HOME/.zshrc" ;;
      */bash) shell_rc="$HOME/.bashrc" ;;
      */fish) shell_rc="$HOME/.config/fish/config.fish" ;;
      *)      die "Unknown shell. Pass rc file: ctx install-hook ~/.zshrc" ;;
    esac
  fi

  local kind _as_lib libfile target tmp mise_add=0 activate_line
  kind="$(_ctx_rc_shell "$shell_rc")"
  # ctx's lib dir: the block sources ctx_autoswitch.* from it.
  _as_lib="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  libfile="ctx_autoswitch.bash"
  [[ "$kind" == fish ]] && libfile="ctx_autoswitch.fish"
  [[ -f "$_as_lib/$libfile" ]] || die "Missing $_as_lib/$libfile — reinstall ctx."

  [[ -d "$shell_rc" ]] && die "$shell_rc is a directory"
  if [[ -e "$shell_rc" ]]; then
    [[ -f "$shell_rc" && -r "$shell_rc" ]] || die "Cannot read $shell_rc"
  fi
  target="$(_ctx_link_target "$shell_rc")"
  [[ -L "$target" ]] && die "Cannot write $shell_rc" # a link loop
  if [[ -e "$target" ]]; then
    [[ -w "$target" ]] || die "Cannot write $shell_rc"
  else
    [[ -d "$(dirname -- "$target")" ]] || die "Cannot write $shell_rc"
  fi

  tmp="$(mktemp "${TMPDIR:-/tmp}/ctx-rc.XXXXXX")" || die "Cannot create a temporary file"
  _ctx_temp_guard "$tmp" # a die below or Ctrl+C removes it
  if [[ -f "$shell_rc" ]]; then
    local st=0
    _ctx_rc_strip_autoswitch "$shell_rc" >"$tmp" || st=$?
    case $st in
      0) ;;
      3) die "Unterminated ctx block in $shell_rc; add the line '# END ctx profile autoswitch' or remove the '# BEGIN ctx profile autoswitch' line" ;;
      *) die "Cannot write $shell_rc" ;;
    esac
  fi

  if ! grep -q "mise activate" "$shell_rc" 2>/dev/null && has mise; then
    mise_add=1
    case "$kind" in
      zsh)  activate_line='eval "$(mise activate zsh)"' ;;
      fish) activate_line='mise activate fish | source' ;;
      *)    activate_line='eval "$(mise activate bash)"' ;;
    esac
  fi

  if [[ "${CTX_DRY_RUN:-0}" == 1 ]]; then
    _ctx_temp_done
    [[ -f "$shell_rc" ]] && dim "  [dry-run] would back up $shell_rc"
    [[ $mise_add -eq 1 ]] && dim "  [dry-run] would add mise activation to $shell_rc"
    dim "  [dry-run] would refresh the ctx profile autoswitch hook in $shell_rc"
    grep -qF "Include $CTX_SSH_CONFIG" "$HOME/.ssh/config" 2>/dev/null \
      || dim "  [dry-run] would add Include $CTX_SSH_CONFIG to $HOME/.ssh/config"
    return 0
  fi

  # Backup first
  backup_file "$shell_rc" || warn "Could not back up $shell_rc" >&2

  # 1. mise activate; 2. ctx profile autoswitch — sources the lib, so upgrades
  # refresh behavior without re-pasting. One write: all or nothing.
  # Every write to the temp file is checked: a short one must not replace the rc.
  if [[ $mise_add -eq 1 ]]; then
    printf '\n# mise-en-place — added by ctx\n%s\n' "$activate_line" >>"$tmp" || die "Cannot write $shell_rc"
  fi
  local block
  block="$(_ctx_autoswitch_block "$kind" "$_as_lib")" || die "Cannot write $shell_rc"
  printf '%s\n' "$block" >>"$tmp" || die "Cannot write $shell_rc"
  _ctx_write_through "$tmp" "$shell_rc" || die "Cannot write $shell_rc"
  _ctx_temp_done
  [[ $mise_add -eq 1 ]] && success "mise activation added to $shell_rc"
  success "ctx profile autoswitch hook refreshed in $shell_rc (sources lib)"

  # 3. Ensure SSH Include is wired up
  ensure_ssh_include && success "SSH config isolated to $CTX_SSH_CONFIG"

  info "Reload your shell: source $shell_rc"
}

# ─── env / run / agent-rules ──────────────────────────────────────────────────
# Profile for a command: -p NAME, else the folder's profile, else the active one.
_ctx_profile_for_cwd() {
  local p
  p="$(ctx_resolve_path_profile "$PWD" "$CTX_PROFILES_DIR" 2>/dev/null || true)"
  [[ -n "$p" ]] || p="$(active_profile)"
  printf '%s' "$p"
}

# Fills _ctx_env_k / _ctx_env_v with one process's identity for the loaded profile.
# Process-scoped only: no gh auth switch, no ssh-add, no kubectl/az/gcloud config writes.
_ctx_profile_env_collect() {
  local name="$1" with_secrets="$2" key val pair tok
  _ctx_env_k=() _ctx_env_v=()
  _ctx_env_add() { _ctx_env_k+=("$1"); _ctx_env_v+=("$2"); }
  _ctx_env_add CTX_ACTIVE_PROFILE "$name"
  if [[ -n "${GIT_NAME:-}" ]]; then
    _ctx_env_add GIT_AUTHOR_NAME "$GIT_NAME"
    _ctx_env_add GIT_COMMITTER_NAME "$GIT_NAME"
  fi
  if [[ -n "${GIT_EMAIL:-}" ]]; then
    _ctx_env_add GIT_AUTHOR_EMAIL "$GIT_EMAIL"
    _ctx_env_add GIT_COMMITTER_EMAIL "$GIT_EMAIL"
  fi
  if [[ -n "${SSH_KEY_PATH:-}" && -f "$SSH_KEY_PATH" ]]; then
    _ctx_env_add GIT_SSH_COMMAND "ssh -i $(printf '%q' "$SSH_KEY_PATH") -o IdentitiesOnly=yes"
  fi
  if [[ -n "${GITHUB_USER:-}" ]] && has gh; then
    tok="$(gh auth token -u "$GITHUB_USER" 2>/dev/null || true)"
    [[ -n "$tok" ]] && _ctx_env_add GH_TOKEN "$tok"
  fi
  [[ -n "${AWS_PROFILE_NAME:-}" ]] && _ctx_env_add AWS_PROFILE "$AWS_PROFILE_NAME"
  if [[ -n "${GCP_PROJECT:-}" ]]; then
    _ctx_env_add CLOUDSDK_CORE_PROJECT "$GCP_PROJECT"
    _ctx_env_add GOOGLE_CLOUD_PROJECT "$GCP_PROJECT"
  fi
  [[ -n "${GCP_ACCOUNT:-}" ]] && _ctx_env_add CLOUDSDK_CORE_ACCOUNT "$GCP_ACCOUNT"
  if [[ -n "${AZURE_SUBSCRIPTION:-}" ]]; then
    _ctx_env_add AZURE_SUBSCRIPTION "$AZURE_SUBSCRIPTION"
    _ctx_env_add AZURE_SUBSCRIPTION_ID "$AZURE_SUBSCRIPTION"
  fi
  [[ -n "${AZURE_TENANT:-}" ]] && _ctx_env_add AZURE_TENANT_ID "$AZURE_TENANT"
  if [[ -n "${EXTRA_ENVS:-}" ]]; then
    while IFS= read -r pair; do
      [[ -z "$pair" ]] && continue
      key="${pair%%=*}"
      is_valid_env_key "$key" && _ctx_env_add "$key" "${pair#*=}"
    done <<< "$EXTRA_ENVS"
  fi
  if [[ "$with_secrets" == "1" ]]; then
    for key in ${SECRET_KEYS:-}; do
      is_valid_env_key "$key" || continue
      val="$(keychain_get "$name" "$key" 2>/dev/null || true)"
      [[ -n "$val" ]] && _ctx_env_add "$key" "$val"
    done
  fi
  return 0
}

# Shared by env/run: parses [-p NAME] and loads the profile. Sets _ctx_cmd_profile, _ctx_cmd_rest.
_ctx_cmd_load_profile() {
  local p=""
  _ctx_cmd_rest=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -p|--profile) p="${2:-}"; shift 2 || shift ;;
      --) shift; _ctx_cmd_rest+=("$@"); break ;;
      *) _ctx_cmd_rest+=("$1"); shift ;;
    esac
  done
  [[ -n "$p" ]] || p="$(_ctx_profile_for_cwd)"
  [[ -n "$p" ]] || die "No profile for $PWD and none active. Use -p <profile>."
  load_profile "$p"
  _ctx_cmd_profile="$p"
}

# Print exports for eval: ctx env [-p NAME] [--fish] [--secrets]
cmd_env() {
  local shell_kind=bash with_secrets=0 a i
  _ctx_cmd_load_profile "$@"
  for a in "${_ctx_cmd_rest[@]+"${_ctx_cmd_rest[@]}"}"; do
    case "$a" in
      --fish) shell_kind=fish ;;
      --secrets) with_secrets=1 ;;
      *) die "Usage: ctx env [-p profile] [--fish] [--secrets]" ;;
    esac
  done
  _ctx_profile_env_collect "$_ctx_cmd_profile" "$with_secrets"
  for ((i = 0; i < ${#_ctx_env_k[@]}; i++)); do
    _ctx_emit_export "$shell_kind" "${_ctx_env_k[i]}" "${_ctx_env_v[i]}"
  done
}

# Run one command with the profile's identity (git author, SSH key, gh token, cloud, secrets).
cmd_run() {
  local i
  _ctx_cmd_load_profile "$@"
  [[ ${#_ctx_cmd_rest[@]} -gt 0 ]] || die "Usage: ctx run [-p profile] -- <command> [args...]"
  _ctx_profile_env_collect "$_ctx_cmd_profile" 1
  for ((i = 0; i < ${#_ctx_env_k[@]}; i++)); do
    export "${_ctx_env_k[i]}=${_ctx_env_v[i]}"
  done
  exec "${_ctx_cmd_rest[@]}"
}

CTX_AGENT_RULES_BEGIN="<!-- BEGIN ctx agent rules -->"
CTX_AGENT_RULES_END="<!-- END ctx agent rules -->"

_ctx_agent_rules_text() {
  cat <<EOF
$CTX_AGENT_RULES_BEGIN
## Client identity (ctx)

This machine uses \`ctx\` to keep each client's git, GitHub, SSH and cloud identity separate.

- Check which identity applies here: \`ctx status --json\`, or \`ctx env\` (prints the variables).
- Run commands that need this client's credentials through \`ctx run -- <command>\`
  (for example \`ctx run -- gh pr create\` or \`ctx run -- terraform plan\`).
- Do not change \`git config user.email\`, rewrite remote URLs, or run \`gh auth switch\`.
- If a commit or push is blocked by "ctx guard", stop and report it. Do not use \`--no-verify\`.
$CTX_AGENT_RULES_END
EOF
}

# ctx agent-rules [--write [FILE]] — print, or upsert into AGENTS.md (or FILE) between markers.
cmd_agent_rules() {
  if [[ "${1:-}" != "--write" ]]; then
    _ctx_agent_rules_text
    return 0
  fi
  local file="${2:-}" top tmp line skip=0
  if [[ -z "$file" ]]; then
    top="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
    file="$top/AGENTS.md"
  fi
  if [[ "${CTX_DRY_RUN:-0}" == "1" ]]; then
    info "[dry-run] Would write ctx agent rules to $file"
    return 0
  fi
  # A name starting with "-" must not be read as an option by the tools below.
  [[ "$file" == -* ]] && file="./$file"
  [[ -d "$file" ]] && die "$file is a directory"
  tmp="$(mktemp "${TMPDIR:-/tmp}/ctx-agent-rules.XXXXXX")" || die "Cannot create a temporary file"
  _ctx_temp_guard "$tmp" # a die below or Ctrl+C removes it
  # Every write to the temp file is checked: a short one must not replace FILE.
  local wfail=0
  if [[ -e "$file" ]]; then
    [[ -f "$file" && -r "$file" ]] || die "Cannot read $file"
    while IFS= read -r line || [[ -n "$line" ]]; do
      [[ "$line" == "$CTX_AGENT_RULES_BEGIN" ]] && { skip=1; continue; }
      if [[ $skip -eq 1 ]]; then
        [[ "$line" == "$CTX_AGENT_RULES_END" ]] && skip=0
        continue
      fi
      printf '%s\n' "$line" || { wfail=1; break; }
    done <"$file" >"$tmp" || wfail=1
    [[ $wfail -eq 0 ]] || die "Cannot write $file"
    [[ $skip -eq 1 ]] && die "Unterminated ctx block in $file; add the END marker or remove the BEGIN line"
    if [[ -s "$tmp" && -n "$(tail -n1 "$tmp")" ]]; then echo >>"$tmp" || die "Cannot write $file"; fi
  fi
  _ctx_agent_rules_text >>"$tmp" || die "Cannot write $file"
  _ctx_write_through "$tmp" "$file" || die "Cannot write $file"
  _ctx_temp_done
  success "ctx agent rules written to $file"
}

# ─── guard ────────────────────────────────────────────────────────────────────
# Global git hooks (core.hooksPath) that stop commits/pushes made with another
# profile's identity. Every stub chains to the repo's own .git/hooks/<name>.
# reference-transaction is left out: it fires on every ref update.
CTX_GUARD_HOOK_NAMES=(applypatch-msg pre-applypatch post-applypatch pre-commit
  pre-merge-commit prepare-commit-msg commit-msg post-commit pre-rebase
  post-checkout post-merge pre-push post-rewrite pre-auto-gc
  sendemail-validate post-index-change)
# Exit code of `ctx guard check` that makes the hook abort; any other failure lets git continue.
CTX_GUARD_BLOCK=3

_ctx_guard_dir() { printf '%s' "$CTX_DIR/git-hooks"; }

_ctx_guard_installed() {
  local cur
  cur="$(git config --global --get core.hooksPath 2>/dev/null || true)"
  [[ "${cur/#\~/$HOME}" == "$(_ctx_guard_dir)" && -x "$(_ctx_guard_dir)/pre-commit" ]]
}

_ctx_guard_write_hook() {
  local name="$1" file="$2"
  {
    echo '#!/bin/sh'
    echo '# Written by "ctx guard install"; changes are overwritten.'
    if [[ "$name" == pre-commit || "$name" == pre-push ]]; then
      cat <<EOF
if [ "\${CTX_GUARD:-on}" != off ] && command -v ctx >/dev/null 2>&1; then
  ctx guard check $name "\$@" </dev/null
  [ \$? -eq $CTX_GUARD_BLOCK ] && exit 1
fi
EOF
    fi
    cat <<EOF
h="\$(git rev-parse --git-common-dir 2>/dev/null)/hooks/$name"
[ -x "\$h" ] && exec "\$h" "\$@"
exit 0
EOF
  } >"$file"
  chmod 755 "$file"
}

_ctx_guard_profile_var() {
  bash -c 'source "$1" 2>/dev/null || true; printf %s "${!2:-}"' _ "$CTX_PROFILES_DIR/$1.conf" "$2"
}

# "Name <email> 1700000000 +0000" → email
_ctx_guard_ident_email() {
  local id="${1#*<}"
  printf '%s' "${id%%>*}"
}

_ctx_guard_lc() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }

# git@host:path, ssh://user@host:port/path, https://host/path → host
_ctx_guard_url_host() {
  local u="$1"
  case "$u" in
    *://*) u="${u#*://}"; u="${u%%/*}"; u="${u##*@}"; u="${u%%:*}" ;;
    *:*)   u="${u%%:*}"; u="${u##*@}" ;;
    *)     u="" ;;
  esac
  printf '%s' "$u"
}

_ctx_guard_check_commit() {
  local prof="$1" want got who fix
  want="$(_ctx_guard_profile_var "$prof" GIT_EMAIL)"
  [[ -n "$want" ]] || return 0
  for who in AUTHOR COMMITTER; do
    got="$(_ctx_guard_ident_email "$(git var "GIT_${who}_IDENT" 2>/dev/null)")"
    [[ "$(_ctx_guard_lc "$got")" == "$(_ctx_guard_lc "$want")" ]] && continue
    # git exports GIT_AUTHOR_EMAIL to hooks, so an env override shows up as a config mismatch.
    if [[ "$(_ctx_guard_lc "$(git config --get user.email 2>/dev/null)")" != "$(_ctx_guard_lc "$got")" ]]; then
      fix="unset GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL EMAIL   (set in your environment)"
    elif [[ -n "$(git config --local --get user.email 2>/dev/null)" ]]; then
      fix="git config --local --unset user.email   (repo-local override)"
    else
      fix="git config user.email '$want'"
    fi
    {
      error "ctx guard: commit blocked — $(_ctx_guard_lc "$who") email is '${got:-unset}', but this repo belongs to profile '$prof' ($want)."
      echo "  Fix:       $fix"
      echo "  Skip once: git commit --no-verify    Turn off: CTX_GUARD=off"
    } >&2
    return "$CTX_GUARD_BLOCK"
  done
}

_ctx_guard_check_push() {
  local prof="$1" remote="$2" url="$3" host other path
  host="$(_ctx_guard_url_host "$url")"
  case "$host" in
    github-*)
      other="${host#github-}"
      [[ "$other" != "$prof" && -f "$CTX_PROFILES_DIR/${other}.conf" ]] || return 0
      if [[ "$url" == *://* ]]; then path="${url#*://}"; path="${path#*/}"; else path="${url#*:}"; fi
      {
        error "ctx guard: push blocked — '$remote' uses profile '$other' (host $host), but this repo belongs to profile '$prof'."
        echo "  Fix:       git remote set-url $remote git@github-${prof}:${path}"
        echo "  Skip once: git push --no-verify    Turn off: CTX_GUARD=off"
      } >&2
      return "$CTX_GUARD_BLOCK"
      ;;
    github.com)
      grep -Eq "^Host[[:space:]]+github-${prof}([[:space:]]|\$)" "$CTX_SSH_CONFIG" 2>/dev/null || return 0
      {
        warn "ctx guard: '$remote' pushes via github.com, not github-${prof}, so profile '$prof's SSH key may not be used."
        echo "  Fix: ctx use $prof   (routes github.com through github-${prof} for repos in its WORK_DIR)"
        echo "   or: git remote set-url $remote $(github_clone_url_for_profile "$prof" "$url" y)"
      } >&2
      ;;
  esac
  return 0
}

# Hook entry point: ctx guard check pre-commit | pre-push <remote> <url>
_ctx_guard_check() {
  local hook="${1:-}" top prof
  top="$(git rev-parse --show-toplevel 2>/dev/null)" || return 0
  prof="$(ctx_resolve_path_profile "$top" "$CTX_PROFILES_DIR" 2>/dev/null)" || return 0
  [[ -n "$prof" ]] || return 0
  case "$hook" in
    pre-commit) _ctx_guard_check_commit "$prof" ;;
    pre-push)   _ctx_guard_check_push "$prof" "${2:-}" "${3:-}" ;;
  esac
}

_ctx_guard_install() {
  has git || die "git not found"
  local dir cur name
  dir="$(_ctx_guard_dir)"
  cur="$(git config --global --get core.hooksPath 2>/dev/null || true)"
  if [[ -n "$cur" && "${cur/#\~/$HOME}" != "$dir" ]]; then
    error "git already has a global core.hooksPath: $cur"
    info "Either unset it (git config --global --unset core.hooksPath) and re-run,"
    info "or call these from your own hooks (exit $CTX_GUARD_BLOCK means block):"
    info "  pre-commit: ctx guard check pre-commit"
    info "  pre-push:   ctx guard check pre-push \"\$@\""
    return 1
  fi
  if [[ "${CTX_DRY_RUN:-0}" == "1" ]]; then
    info "[dry-run] Would write ${#CTX_GUARD_HOOK_NAMES[@]} hooks to $dir"
    info "[dry-run] Would run: git config --global core.hooksPath $dir"
    return 0
  fi
  mkdir -p "$dir"
  for name in "${CTX_GUARD_HOOK_NAMES[@]}"; do
    _ctx_guard_write_hook "$name" "$dir/$name"
  done
  git config --global core.hooksPath "$dir"
  log "guard installed: $dir"
  success "ctx guard on: commits and pushes in client repos are checked against the profile"
  dim "  Repo hooks in .git/hooks still run. Repos that set their own core.hooksPath (e.g. husky) skip the guard."
  dim "  Skip once: --no-verify   Off for a session: CTX_GUARD=off   Remove: ctx guard uninstall"
}

_ctx_guard_uninstall() {
  local dir name
  dir="$(_ctx_guard_dir)"
  if _ctx_guard_installed || [[ -d "$dir" ]]; then
    if [[ "${CTX_DRY_RUN:-0}" == "1" ]]; then
      info "[dry-run] Would unset core.hooksPath and remove $dir"
      return 0
    fi
    _ctx_guard_installed && git config --global --unset core.hooksPath
    for name in "${CTX_GUARD_HOOK_NAMES[@]}"; do rm -f "$dir/$name"; done
    rmdir "$dir" 2>/dev/null || true
    log "guard removed"
    success "ctx guard off"
  else
    info "ctx guard is not installed"
  fi
}

_ctx_guard_status() {
  local top prof want got local_hp
  if _ctx_guard_installed; then
    success "ctx guard: on ($(_ctx_guard_dir))"
  else
    warn "ctx guard: off  →  ctx guard install"
  fi
  top="$(git rev-parse --show-toplevel 2>/dev/null)" || return 0
  prof="$(ctx_resolve_path_profile "$top" "$CTX_PROFILES_DIR" 2>/dev/null || true)"
  if [[ -z "$prof" ]]; then
    dim "  This repo is not under any profile — nothing to check."
    return 0
  fi
  local_hp="$(git config --local --get core.hooksPath 2>/dev/null || true)"
  [[ -n "$local_hp" ]] && warn "This repo sets core.hooksPath=$local_hp, so the guard does not run here."
  want="$(_ctx_guard_profile_var "$prof" GIT_EMAIL)"
  got="$(_ctx_guard_ident_email "$(git var GIT_AUTHOR_IDENT 2>/dev/null)")"
  if [[ -z "$want" ]]; then
    info "Profile '$prof' has no GIT_EMAIL — email check skipped."
  elif [[ "$(_ctx_guard_lc "$got")" == "$(_ctx_guard_lc "$want")" ]]; then
    success "Profile '$prof': commits here use $got"
  else
    warn "Profile '$prof' expects $want, but commits here would use ${got:-no email}"
  fi
}

cmd_guard() {
  local sub="${1:-status}"
  shift 2>/dev/null || true
  case "$sub" in
    install)           _ctx_guard_install ;;
    uninstall|remove)  _ctx_guard_uninstall ;;
    status)            _ctx_guard_status ;;
    check)             _ctx_guard_check "$@" ;;
    *) die "Usage: ctx guard [status|install|uninstall]" ;;
  esac
}
