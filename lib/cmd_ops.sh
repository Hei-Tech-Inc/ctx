#!/usr/bin/env bash
# ctx/lib/cmd_ops.sh — v3: use, status, list, remove, undo, secret, doctor

# ─── use ─────────────────────────────────────────────────────────────────────
cmd_use() {
  local name="${1:-}" quiet="${CTX_QUIET:-0}"
  [[ -z "$name" ]] && die "Usage: ctx use <profile>"
  load_profile "$name"

  [[ "$quiet" == "0" ]] && info "Switching to: ${BOLD}${name}${RESET}"

  local errors=0

  # 1. GitHub
  if [[ -n "${GITHUB_USER:-}" ]] && has gh; then
    if gh auth switch -u "$GITHUB_USER" 2>/dev/null; then
      [[ "$quiet" == "0" ]] && success "GitHub → $GITHUB_USER"
    else
      warn "gh switch failed for '$GITHUB_USER' — run: gh auth login"
      errors=$((errors+1))
    fi
  fi

  # 2. SSH key — flush agent, load this profile's key only
  if [[ -n "${SSH_KEY_PATH:-}" && -f "$SSH_KEY_PATH" ]]; then
    ssh-add -D 2>/dev/null || true
    if ssh-add "$SSH_KEY_PATH" 2>/dev/null; then
      [[ "$quiet" == "0" ]] && success "SSH key → $(basename "$SSH_KEY_PATH")"
    else
      eval "$(ssh-agent -s)" &>/dev/null
      ssh-add "$SSH_KEY_PATH" 2>/dev/null || warn "SSH key load failed"
    fi
  fi

  # 3. AWS
  if [[ -n "${AWS_PROFILE_NAME:-}" ]]; then
    export AWS_PROFILE="$AWS_PROFILE_NAME"
    [[ "$quiet" == "0" ]] && success "AWS_PROFILE → $AWS_PROFILE_NAME"
  fi

  # 4. Azure
  if [[ -n "${AZURE_SUBSCRIPTION:-}" ]]; then
    if has az; then
      az account set --subscription "$AZURE_SUBSCRIPTION" &>/dev/null \
        && { [[ "$quiet" == "0" ]] && success "Azure → $AZURE_SUBSCRIPTION"; } \
        || { warn "Azure subscription switch failed"; errors=$((errors+1)); }
    else
      export AZURE_SUBSCRIPTION="$AZURE_SUBSCRIPTION"
      [[ -n "${AZURE_TENANT:-}" ]] && export AZURE_TENANT="$AZURE_TENANT"
    fi
  fi

  # 5. GCP
  if [[ -n "${GCP_PROJECT:-}" ]]; then
    if has gcloud; then
      gcloud config set project "$GCP_PROJECT" --quiet 2>/dev/null \
        && { [[ "$quiet" == "0" ]] && success "GCP → $GCP_PROJECT"; } \
        || warn "GCP project switch failed"
      [[ -n "${GCP_ACCOUNT:-}" ]] && \
        gcloud config set account "$GCP_ACCOUNT" --quiet 2>/dev/null || true
    else
      export GCLOUD_PROJECT="${GCP_PROJECT:-}"
    fi
  fi

  # 6. kubectl
  if [[ -n "${KUBE_CONTEXT:-}" ]]; then
    if has kubectl; then
      kubectl config use-context "$KUBE_CONTEXT" &>/dev/null \
        && { [[ "$quiet" == "0" ]] && success "kubectl → $KUBE_CONTEXT"; } \
        || warn "kubectl context '$KUBE_CONTEXT' not found"
    fi
  fi

  # 7. Secrets from Keychain → export to shell
  if [[ -n "${SECRET_KEYS:-}" ]]; then
    for key in $SECRET_KEYS; do
      if ! is_valid_env_key "$key"; then
        warn "Skipping invalid secret key name '$key'"
        continue
      fi
      local val
      val=$(keychain_get "$PROFILE_NAME" "$key")
      if [[ -n "$val" ]]; then
        declare -x "$key=$val"
        [[ "$quiet" == "0" ]] && success "Secret → $key (Keychain)"
      else
        warn "Secret '$key' not in Keychain — run: ctx secret set $PROFILE_NAME $key"
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

  set_active_profile "$name"

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

# ─── status ───────────────────────────────────────────────────────────────────
cmd_status() {
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
  else
    hr
    echo "  Profile  : ${BOLD}$current${RESET}"
    echo "  Git      : ${GIT_NAME:-—} <${GIT_EMAIL:-—}>"
    echo "  Dir      : ${WORK_DIR:-—}"
    echo "  GitHub   : ${GITHUB_USER:-—}"
    echo "  SSH key  : ${SSH_KEY_PATH:+$(basename "$SSH_KEY_PATH")}"
    echo "  AWS      : ${AWS_PROFILE_NAME:-—}"
    echo "  Azure    : ${AZURE_SUBSCRIPTION:-—}"
    echo "  GCP      : ${GCP_PROJECT:-—}"
    echo "  kubectl  : ${KUBE_CONTEXT:-—}"
    echo "  Secrets  : ${SECRET_KEYS:-none (in Keychain)}"
    hr
  fi

  echo ""
  bold "  Live checks"
  echo ""

  # gh account
  if has gh && [[ -n "${GITHUB_USER:-}" ]]; then
    local active_gh
    active_gh=$(gh auth status 2>&1 | grep "Logged in to github.com account" \
      | awk '{print $NF}' | tr -d '()' | head -1)
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

    local tags=()
    [[ -n "${WORK_DIR:-}"          ]] && tags+=("${WORK_DIR/$HOME/~}")
    [[ -n "${GITHUB_USER:-}"       ]] && tags+=("gh:$GITHUB_USER")
    [[ -n "${AWS_PROFILE_NAME:-}"  ]] && tags+=("aws:$AWS_PROFILE_NAME")
    [[ -n "${AZURE_SUBSCRIPTION:-}" ]] && tags+=("azure")
    [[ -n "${GCP_PROJECT:-}"       ]] && tags+=("gcp:$GCP_PROJECT")
    [[ -n "${KUBE_CONTEXT:-}"      ]] && tags+=("k8s:$KUBE_CONTEXT")
    [[ -n "${SECRET_KEYS:-}"       ]] && tags+=("$(echo "$SECRET_KEYS" | wc -w | tr -d ' ') secrets")

    echo -e "  ${BOLD}${pname}${RESET}${marker}"
    echo    "  ${DIM}${GIT_NAME:-—} <${GIT_EMAIL:-—}>${RESET}"
    [[ ${#tags[@]} -gt 0 ]] && echo -e "  ${DIM}$(IFS=' | '; echo "${tags[*]}")${RESET}"
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
  warn "This removes the profile config, git identity file, SSH host block, and Keychain secrets."
  warn "Your code, SSH key files, and mise.toml are NOT deleted."
  echo ""

  ask_yn "Remove profile '$name'?" "n" || die "Aborted."

  # Keychain secrets
  if [[ -n "${SECRET_KEYS:-}" ]]; then
    for key in $SECRET_KEYS; do
      keychain_delete "$name" "$key"
    done
    success "Keychain secrets removed"
  fi

  # SSH host block from ctx_config (not from ~/.ssh/config)
  ctx_ssh_remove_host "github-${name}"
  success "SSH host removed from ctx_config"

  # Git identity file + gitconfig include
  gitconfig_remove_include "$HOME/.config/git/ctx-${name}"
  rm -f "$HOME/.config/git/ctx-${name}"
  success "Git identity removed"

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
      keychain_set "$profile" "$key" "$val" && success "$key stored in Keychain for $profile"
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
      bold "\n  Keychain secrets for: $profile\n"
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
      keychain_delete "$profile" "$key" && success "Deleted $key from Keychain"
      ;;
    *) echo "Usage: ctx secret <set|get|list|delete> <profile> [KEY]" ;;
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
      echo ""
      info "Set it with: ctx config work-root <path>"
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
      if grep -q "^work_root=" "$CTX_CONFIG" 2>/dev/null; then
        sed -i.bak "s|^work_root=.*|work_root=$value|" "$CTX_CONFIG"
        rm -f "$CTX_CONFIG.bak"
      else
        echo "work_root=$value" >> "$CTX_CONFIG"
      fi
      success "work_root set to: $value"
      ;;
    *)
      die "Usage: ctx config [show|work-root <path>]"
      ;;
  esac
}

# ─── undo ─────────────────────────────────────────────────────────────────────
cmd_undo() {
  local latest; latest=$(latest_backup)
  [[ -z "$latest" ]] && die "No backups found in ${CTX_DIR}/backups"

  local bdir="${CTX_DIR}/backups/$latest"
  bold "\n  Restoring from backup: $latest\n"

  [[ ! -f "$bdir/manifest.txt" ]] && die "Manifest not found at $bdir/manifest.txt"

  ask_yn "Restore these files?" "y" || die "Aborted."

  while IFS= read -r orig; do
    [[ -z "$orig" ]] && continue
    local fname; fname=$(basename "$orig")
    if [[ -f "$bdir/$fname" ]]; then
      cp "$bdir/$fname" "$orig" && success "Restored: $orig"
    else
      warn "Not in backup: $orig"
    fi
  done < "$bdir/manifest.txt"

  success "Undo complete."
  echo ""
}

# ─── doctor ───────────────────────────────────────────────────────────────────
cmd_doctor() {
  if $HAS_GUM; then
    gum style --border rounded --border-foreground 99 \
      --padding "0 2" --margin "1 0" --bold "ctx doctor"
  else
    bold "\n  ctx doctor\n"
  fi

  local all_ok=true

  _chk() {
    local cmd="$1" label="$2" install="$3"
    if has "$cmd"; then
      success "$label: $($cmd --version 2>&1 | head -1)"
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
  _chk az        "Azure CLI" "$install_hint_az"
  _chk gcloud    "gcloud"    "$install_hint_gcloud"
  _chk kubectl   "kubectl"   "$install_hint_kubectl"

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

    grep -q "ctx auto-switch" "$shell_rc" 2>/dev/null \
      && success "ctx auto-switch: installed" \
      || { warn "ctx auto-switch: not in $shell_rc"; info "Run: ctx install-hook"; all_ok=false; }
  fi

  echo ""
  hr

  # SSH isolation check
  if grep -qF "Include.*ctx_config" "$HOME/.ssh/config" 2>/dev/null; then
    success "SSH isolation: ctx_config is included"
  else
    warn "SSH isolation: ~/.ssh/ctx_config not included in ~/.ssh/config"
    info "Run: ctx install-hook"
    all_ok=false
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
    $ok && success "Profile '$pname': ok"
  done

  [[ $pc -eq 0 ]] && info "No profiles yet. Run: ctx setup"

  echo ""
  $all_ok && success "Everything looks good." || warn "Fix the issues above."
  echo ""
}

# ─── init ─────────────────────────────────────────────────────────────────────
cmd_init() {
  ctx_init_dirs
  ctx_header "Setup check"

  echo ""
  info "Checking your installation..."
  echo ""

  # Check for missing deps and offer to install
  local missing_tools=()
  has mise || missing_tools+=("mise")
  has gum  || missing_tools+=("gum")
  has gh   || missing_tools+=("gh")

  if [[ ${#missing_tools[@]} -gt 0 ]]; then
    warn "Missing tools: ${missing_tools[*]}"
    if ask_yn "Install missing tools now?" "y"; then
      local installer
      installer="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/install.sh"
      if [[ -f "$installer" ]]; then
        bash "$installer"
      else
        for t in "${missing_tools[@]}"; do
          has brew && brew install "$t" &>/dev/null && success "$t installed" || warn "$t: install manually"
        done
      fi
    fi
  fi

  # Shell hook check
  local shell_rc=""
  [[ "$SHELL" == *"zsh"*  ]] && shell_rc="$HOME/.zshrc"
  [[ "$SHELL" == *"bash"* ]] && shell_rc="$HOME/.bashrc"
  [[ "$SHELL" == *"fish"* ]] && shell_rc="$HOME/.config/fish/config.fish"

  if [[ -n "$shell_rc" ]]; then
    if ! grep -q "mise activate" "$shell_rc" 2>/dev/null || \
       ! grep -q "ctx auto-switch" "$shell_rc" 2>/dev/null; then
      ask_yn "Install shell hooks (mise + ctx auto-switch) to $shell_rc?" "y" \
        && cmd_install_hook "$shell_rc" \
        || info "Skipped. Run: ctx install-hook"
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
  echo "  ctx doctor    Full health check"
  echo ""
}

# ─── install-hook ─────────────────────────────────────────────────────────────
cmd_install_hook() {
  local shell_rc="${1:-}"

  if [[ -z "$shell_rc" ]]; then
    case "$SHELL" in
      */zsh)  shell_rc="$HOME/.zshrc" ;;
      */bash) shell_rc="$HOME/.bashrc" ;;
      */fish) shell_rc="$HOME/.config/fish/config.fish" ;;
      *)      die "Unknown shell. Pass rc file: ctx install-hook ~/.zshrc" ;;
    esac
  fi

  # Backup first
  backup_file "$shell_rc"

  # 1. mise activate
  if ! grep -q "mise activate" "$shell_rc" 2>/dev/null && has mise; then
    local activate_line
    case "$(basename "$SHELL")" in
      zsh)  activate_line='eval "$(mise activate zsh)"' ;;
      bash) activate_line='eval "$(mise activate bash)"' ;;
      fish) activate_line='mise activate fish | source' ;;
      *)    activate_line='eval "$(mise activate)"' ;;
    esac
    { echo ""; echo "# mise-en-place — added by ctx"; echo "$activate_line"; } >> "$shell_rc"
    success "mise activation added to $shell_rc"
  fi

  # 2. ctx auto-switch hook
  if ! grep -q "ctx auto-switch" "$shell_rc" 2>/dev/null; then
    if [[ "$shell_rc" == *"/config.fish" ]]; then
      cat >> "$shell_rc" <<'HOOK'

# ── ctx auto-switch ───────────────────────────────────────────────────────────
function _ctx_auto_switch --on-variable PWD
  command -v ctx >/dev/null 2>/dev/null; or return
  set -l profiles_dir (set -q CTX_DIR; and echo "$CTX_DIR"; or echo "$HOME/.ctx")/profiles
  set -l active_conf (set -q CTX_DIR; and echo "$CTX_DIR"; or echo "$HOME/.ctx")/config
  test -d "$profiles_dir"; or return

  for conf in "$profiles_dir"/*.conf
    test -e "$conf"; or continue
    set -l work_dir (grep "^WORK_DIR=" "$conf" | sed 's/^WORK_DIR=//')
    set work_dir (string replace -r '^~' "$HOME" -- "$work_dir")
    test -n "$work_dir"; and string match -q "$work_dir*" -- "$PWD"; or continue

    set -l pname (basename "$conf" .conf)
    set -l repo_root (git rev-parse --show-toplevel 2>/dev/null; or echo "")
    if test -n "$repo_root"; and test -f "$repo_root/.ctx"
      set -l override (grep "^profile=" "$repo_root/.ctx" 2>/dev/null | cut -d= -f2)
      test -n "$override"; and set pname "$override"
    end
    set -l current (grep "^active=" "$active_conf" 2>/dev/null | cut -d= -f2)
    if test "$pname" != "$current"
      env CTX_QUIET=1 ctx use "$pname" 2>/dev/null
    end
    echo -e "\033[2m[ctx] $pname\033[0m"
    return
  end
end
# ─────────────────────────────────────────────────────────────────────────────
HOOK
    else
      cat >> "$shell_rc" <<'HOOK'

# ── ctx auto-switch ───────────────────────────────────────────────────────────
_ctx_auto_switch() {
  command -v ctx &>/dev/null || return 0
  local profiles_dir="${CTX_DIR:-$HOME/.ctx}/profiles"
  local active_conf="${CTX_DIR:-$HOME/.ctx}/config"
  [[ -d "$profiles_dir" ]] || return 0
  for conf in "$profiles_dir"/*.conf; do
    [[ -e "$conf" ]] || continue
    local work_dir; work_dir=$(grep "^WORK_DIR=" "$conf" | cut -d'"' -f2)
    work_dir="${work_dir/#\~/$HOME}"
    [[ -z "$work_dir" || "$PWD" != "$work_dir"* ]] && continue
    local pname; pname=$(basename "$conf" .conf)
    local current; current=$(grep "^active=" "$active_conf" 2>/dev/null | cut -d= -f2)
    local repo_root; repo_root=$(git rev-parse --show-toplevel 2>/dev/null || echo "")
    [[ -n "$repo_root" && -f "$repo_root/.ctx" ]] && \
      pname=$(grep "^profile=" "$repo_root/.ctx" 2>/dev/null | cut -d= -f2 || echo "$pname")
    if [[ "$pname" != "$current" ]]; then
      CTX_QUIET=1 ctx use "$pname" 2>/dev/null
    fi
    echo -e "\033[2m[ctx] $pname\033[0m"
    return 0
  done
}
[[ -n "${ZSH_VERSION:-}" ]] && { autoload -Uz add-zsh-hook; add-zsh-hook chpwd _ctx_auto_switch; }
[[ -n "${BASH_VERSION:-}" ]] && PROMPT_COMMAND="_ctx_auto_switch;${PROMPT_COMMAND:+ $PROMPT_COMMAND}"
# ─────────────────────────────────────────────────────────────────────────────
HOOK
    fi
    success "ctx auto-switch hook added to $shell_rc"
  fi

  # 3. Ensure SSH Include is wired up
  ensure_ssh_include
  success "SSH config isolated to $CTX_SSH_CONFIG"

  info "Reload your shell: source $shell_rc"
}
