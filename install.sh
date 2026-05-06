#!/usr/bin/env bash
# ctx installer — sets up everything so the user doesn't have to
# Usage: bash install.sh
# Remote: curl -fsSL https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/main/install.sh | bash
#
# In-place upgrade only (binary + lib — no Homebrew/mise/touch ~/.ctx): CTX_UPGRADE_ONLY=1 bash install.sh

set -uo pipefail

CTX_VERSION="3.1.1"
CTX_REPO="https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/main"

# ─── Colors ───────────────────────────────────────────────────────────────────
if [[ -t 1 ]]; then
  RED='\033[0;31m' GREEN='\033[0;32m' YELLOW='\033[0;33m'
  CYAN='\033[0;36m' BOLD='\033[1m' DIM='\033[2m' RESET='\033[0m'
else
  RED='' GREEN='' YELLOW='' CYAN='' BOLD='' DIM='' RESET=''
fi

info()    { echo -e "${CYAN}→${RESET} $*"; }
success() { echo -e "${GREEN}✓${RESET} $*"; }
warn()    { echo -e "${YELLOW}!${RESET} $*"; }
error()   { echo -e "${RED}✗${RESET} $*" >&2; }
bold()    { echo -e "${BOLD}$*${RESET}"; }
dim()     { echo -e "${DIM}$*${RESET}"; }
die()     { error "$*"; exit 1; }
step()    { echo ""; echo -e "${BOLD}  [$1]${RESET} $2"; echo ""; }

strip_cr_inplace() {
  local f="$1"
  [[ -f "$f" ]] || return 0
  if command -v perl &>/dev/null; then
    perl -0777 -pi -e 's/\r\n/\n/g; s/\r/\n/g' "$f" 2>/dev/null && return 0
  fi
  local t
  t="$(mktemp)" || return 1
  tr -d '\r' < "$f" > "$t" && mv "$t" "$f"
}

print_ctx_banner() {
  [[ -t 1 ]] || return 0
  echo -e "${CYAN}"
  cat <<'ART'
   ██████╗████████╗██╗  ██╗
  ██╔════╝╚══██╔══╝╚██╗██╔╝
  ██║        ██║    ╚███╔╝
  ██║        ██║    ██╔██╗
  ╚██████╗   ██║   ██╔╝ ██╗
   ╚═════╝   ╚═╝   ╚═╝  ╚═╝
ART
  echo -e "${RESET}${DIM}  client context switcher${RESET}"
  echo ""
}

# ─── OS detection ─────────────────────────────────────────────────────────────
OS="$(uname -s)"
ARCH="$(uname -m)"
IS_MAC=false; IS_LINUX=false

case "$OS" in
  Darwin) IS_MAC=true ;;
  Linux)  IS_LINUX=true ;;
  *)      die "Unsupported OS: $OS. ctx supports macOS and Linux." ;;
esac

# ─── Shell detection ──────────────────────────────────────────────────────────
detect_shell_rc() {
  case "$SHELL" in
    */zsh)  echo "$HOME/.zshrc" ;;
    */bash)
      if $IS_MAC; then
        [[ -f "$HOME/.bash_profile" ]] && echo "$HOME/.bash_profile" || echo "$HOME/.bashrc"
      else
        echo "$HOME/.bashrc"
      fi
      ;;
    */fish) echo "$HOME/.config/fish/config.fish" ;;
    *)      echo "$HOME/.profile" ;;
  esac
}

SHELL_RC="$(detect_shell_rc)"
SHELL_NAME="$(basename "$SHELL")"

# ─── Header (skipped for CTX_UPGRADE_ONLY — no clear/banner noise) ────────────
if [[ "${CTX_UPGRADE_ONLY:-0}" != "1" ]]; then
  # Some terminals hide/garble output after forced clear; keep scrollback by default.
  if [[ -t 1 && "${CTX_NO_CLEAR:-0}" != "1" ]]; then
    clear 2>/dev/null || true
  fi
  echo ""
  bold "  ctx v${CTX_VERSION} — client context switcher"
  dim  "  Sets up everything you need. No manual steps."
  echo ""
  dim "  OS      : $OS ($ARCH)"
  dim "  Shell   : $SHELL_NAME → $SHELL_RC"
  echo ""
  print_ctx_banner
fi

# ─── Homebrew ─────────────────────────────────────────────────────────────────
install_homebrew() {
  if command -v brew &>/dev/null; then
    success "Homebrew already installed: $(brew --version | head -1)"
    return 0
  fi

  if $IS_LINUX && ! command -v curl &>/dev/null; then
    die "curl required to install Homebrew. Install it first: sudo apt install curl"
  fi

  info "Installing Homebrew..."
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" \
    < /dev/null 2>&1 | grep -E "(==>|✓|Error)" || true

  # Add brew to PATH for this session (Linux puts it in different place)
  if $IS_LINUX; then
    local brew_paths=("/home/linuxbrew/.linuxbrew/bin/brew" "$HOME/.linuxbrew/bin/brew")
    for p in "${brew_paths[@]}"; do
      [[ -f "$p" ]] && eval "$("$p" shellenv)" && break
    done
  fi

  command -v brew &>/dev/null || die "Homebrew installation failed. Visit https://brew.sh"
  success "Homebrew installed"
}

# ─── Generic brew install with status ─────────────────────────────────────────
brew_install() {
  local pkg="$1" cmd="${2:-$1}" label="${3:-$1}"

  if command -v "$cmd" &>/dev/null; then
    success "$label: already installed"
    return 0
  fi

  info "Installing $label..."
  if brew install "$pkg" &>/dev/null 2>&1; then
    success "$label: installed"
  else
    warn "$label: brew install failed — trying alternative..."
    return 1
  fi
}

brew_cask_install() {
  local cask="$1" cmd="$2" label="${3:-$1}"
  if command -v "$cmd" &>/dev/null; then
    success "$label: already installed"
    return 0
  fi
  info "Installing $label (cask)..."
  brew install --cask "$cask" &>/dev/null 2>&1 \
    && success "$label: installed" \
    || { warn "$label: cask install failed"; return 1; }
}

# ─── Install: mise ────────────────────────────────────────────────────────────
install_mise() {
  if command -v mise &>/dev/null; then
    success "mise: already installed ($(mise --version 2>/dev/null | head -1))"
    return 0
  fi

  info "Installing mise..."

  # Prefer brew, fall back to official installer
  if command -v brew &>/dev/null; then
    brew install mise &>/dev/null 2>&1 && { success "mise: installed via brew"; return 0; }
  fi

  # Official mise installer
  curl -fsSL https://mise.run | sh &>/dev/null 2>&1 || \
  curl -fsSL https://mise.jdx.dev/install.sh | sh &>/dev/null 2>&1

  # Add to PATH for this session
  export PATH="$HOME/.local/bin:$PATH"
  command -v mise &>/dev/null && { success "mise: installed"; return 0; }

  warn "mise installation failed — env loading will fall back to direnv"
  return 1
}

# ─── Install: gum ─────────────────────────────────────────────────────────────
install_gum() {
  brew_install "gum" "gum" "gum (TUI prompts)" && return 0

  # Fallback: direct binary from GitHub releases
  info "Trying direct binary for gum..."
  local gum_ver="0.14.5"
  local gum_os gum_arch gum_file
  $IS_MAC  && gum_os="Darwin"  || gum_os="Linux"
  case "$ARCH" in
    x86_64)  gum_arch="x86_64" ;;
    arm64|aarch64) gum_arch="arm64" ;;
    *) warn "Unsupported arch for gum: $ARCH"; return 1 ;;
  esac
  gum_file="gum_${gum_ver}_${gum_os}_${gum_arch}.tar.gz"
  local url="https://github.com/charmbracelet/gum/releases/download/v${gum_ver}/${gum_file}"

  local tmp; tmp=$(mktemp -d)
  curl -fsSL "$url" -o "$tmp/gum.tar.gz" 2>/dev/null \
    && tar -xzf "$tmp/gum.tar.gz" -C "$tmp" 2>/dev/null \
    && mv "$tmp/gum" /usr/local/bin/gum 2>/dev/null \
    && chmod +x /usr/local/bin/gum \
    && { success "gum: installed via binary"; rm -rf "$tmp"; return 0; }

  rm -rf "$tmp"
  warn "gum installation failed — wizards will use basic prompts"
  return 1
}

# ─── Install: gh ──────────────────────────────────────────────────────────────
install_gh() {
  brew_install "gh" "gh" "GitHub CLI (gh)" && return 0

  # Fallback for Linux (Debian/Ubuntu)
  if $IS_LINUX && command -v apt-get &>/dev/null; then
    info "Trying apt for gh CLI..."
    {
      curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg 2>/dev/null
      echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
      sudo apt-get update -qq 2>/dev/null
      sudo apt-get install -y gh -qq 2>/dev/null
    } && { success "gh: installed via apt"; return 0; } || true
  fi

  warn "gh CLI installation failed — GitHub account switching will be limited"
  return 1
}

# ─── Install: direnv (fallback if mise not available) ─────────────────────────
install_direnv() {
  command -v mise &>/dev/null && return 0  # mise handles this
  brew_install "direnv" "direnv" "direnv" && return 0
  warn "direnv installation failed"
  return 1
}

# ─── Install: optional cloud CLIs ─────────────────────────────────────────────
install_optional_tools() {
  # AWS CLI
  if ! command -v aws &>/dev/null; then
    info "Installing AWS CLI..."
    brew install awscli &>/dev/null 2>&1 \
      && success "AWS CLI: installed" \
      || warn "AWS CLI: skipped (optional)"
  else
    success "AWS CLI: already installed"
  fi

  # Azure CLI — only if user has existing Azure setup or explicitly wants it
  if [[ -d "$HOME/.azure" ]] && ! command -v az &>/dev/null; then
    info "Detected ~/.azure — installing Azure CLI..."
    brew install azure-cli &>/dev/null 2>&1 \
      && success "Azure CLI: installed" \
      || warn "Azure CLI: skipped"
  elif command -v az &>/dev/null; then
    success "Azure CLI: already installed"
  fi

  # kubectl — only if kube config exists
  if [[ -f "$HOME/.kube/config" ]] && ! command -v kubectl &>/dev/null; then
    info "Detected ~/.kube/config — installing kubectl..."
    brew install kubectl &>/dev/null 2>&1 \
      && success "kubectl: installed" \
      || warn "kubectl: skipped"
  elif command -v kubectl &>/dev/null; then
    success "kubectl: already installed"
  fi
}

# ─── Activate mise in shell rc ────────────────────────────────────────────────
activate_mise_in_shell() {
  command -v mise &>/dev/null || return 0

  local activate_line
  case "$SHELL_NAME" in
    zsh)  activate_line='eval "$(mise activate zsh)"' ;;
    bash) activate_line='eval "$(mise activate bash)"' ;;
    fish) activate_line='mise activate fish | source' ;;
    *)    activate_line='eval "$(mise activate)"' ;;
  esac

  if grep -qF "mise activate" "$SHELL_RC" 2>/dev/null; then
    success "mise shell activation: already in $SHELL_RC"
    return 0
  fi

  # Back up before touching the rc file
  cp "$SHELL_RC" "${SHELL_RC}.ctx-backup-$(date +%Y%m%d-%H%M%S)" 2>/dev/null || true

  {
    echo ""
    echo "# mise-en-place — added by ctx installer"
    echo "$activate_line"
  } >> "$SHELL_RC"

  success "mise shell activation: added to $SHELL_RC"
  info "  This makes mise activate automatically per directory (like direnv but better)"
}

# ─── Install ctx itself ───────────────────────────────────────────────────────
install_ctx() {
  local install_bin="${CTX_INSTALL_BIN:-/usr/local/bin}"
  local install_lib="${CTX_INSTALL_LIB:-/usr/local/lib/ctx}"
  local fallback_bin="$HOME/.local/bin"
  local fallback_lib="$HOME/.local/lib/ctx"

  _ensure_path_on_shell_rc() {
    local path_dir="$1"
    if ! grep -q "$path_dir" "$SHELL_RC" 2>/dev/null; then
      {
        echo ""
        echo "# ctx — added by ctx installer"
        echo "export PATH=\"$path_dir:\$PATH\""
      } >> "$SHELL_RC"
      info "Added $path_dir to PATH in $SHELL_RC"
    fi
  }

  _use_fallback_paths() {
    install_bin="$fallback_bin"
    install_lib="$fallback_lib"
    mkdir -p "$install_bin" "$install_lib"
    _ensure_path_on_shell_rc "$install_bin"
  }

  # Fall back when either default bin or lib path is unavailable.
  if [[ ! -w "$install_bin" || ! -w "$(dirname "$install_lib")" ]]; then
    _use_fallback_paths
  else
    mkdir -p "$install_lib" 2>/dev/null || _use_fallback_paths
  fi

  mkdir -p "$install_bin" "$install_lib" || die "Unable to create install paths: $install_bin and $install_lib"

  # Locate source — local install vs remote
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd || echo "")"

  if [[ -f "$script_dir/bin/ctx" && -d "$script_dir/lib" ]]; then
    info "Installing ctx from local source..."
    cp "$script_dir/bin/ctx"            "$install_bin/ctx"
    cp "$script_dir/lib/core.sh"        "$install_lib/core.sh"
    cp "$script_dir/lib/cmd_import.sh"  "$install_lib/cmd_import.sh"
    cp "$script_dir/lib/cmd_ops.sh"     "$install_lib/cmd_ops.sh"
  else
    info "Downloading ctx..."
    _dl() {
      local url="$1" dest="$2"
      local dest_dir
      dest_dir="$(dirname "$dest")"
      [[ -w "$dest_dir" ]] || die "Cannot write to $dest_dir. Set CTX_INSTALL_BIN/CTX_INSTALL_LIB or use user-writable paths."
      curl -fsSL "$url" -o "$dest" 2>/dev/null \
        || wget -qO "$dest" "$url" 2>/dev/null \
        || die "Failed to download $url"
    }
    _dl_text() {
      _dl "$1" "$2"
      strip_cr_inplace "$2"
    }
    _dl_text "$CTX_REPO/bin/ctx"            "$install_bin/ctx"
    _dl_text "$CTX_REPO/lib/core.sh"        "$install_lib/core.sh"
    _dl_text "$CTX_REPO/lib/cmd_import.sh"  "$install_lib/cmd_import.sh"
    _dl_text "$CTX_REPO/lib/cmd_ops.sh"     "$install_lib/cmd_ops.sh"
  fi

  chmod +x "$install_bin/ctx"
  chmod 644 "$install_lib"/*.sh

  # Patch the lib path in the binary so it finds its libs after install
  local sed_inplace=(-i)
  $IS_MAC && sed_inplace=(-i '')

  # Replace only line 9 of bin/ctx (CTX_LIB bootstrap probe; see comment on that line).
  # A broad `CTX_LIB=.*` match across the whole file can corrupt completion blocks.
  sed "${sed_inplace[@]}" \
    "9s|^CTX_LIB=.*|CTX_LIB=\"${install_lib}\"|" \
    "$install_bin/ctx" 2>/dev/null || true

  success "ctx: installed at $install_bin/ctx"
}

# ─── Install ctx auto-switch hook ────────────────────────────────────────────
install_ctx_hook() {
  if grep -q "ctx auto-switch" "$SHELL_RC" 2>/dev/null; then
    success "ctx auto-switch hook: already in $SHELL_RC"
    return 0
  fi

  cat >> "$SHELL_RC" <<'HOOK'

# ── ctx auto-switch — added by ctx installer ─────────────────────────────────
_ctx_auto_switch() {
  command -v ctx &>/dev/null || return 0
  local profiles_dir="${CTX_DIR:-$HOME/.ctx}/profiles"
  local active_conf="${CTX_DIR:-$HOME/.ctx}/config"
  [[ -d "$profiles_dir" ]] || return 0

  for conf in "$profiles_dir"/*.conf; do
    [[ -e "$conf" ]] || continue
    local work_dir
    work_dir=$(grep "^WORK_DIR=" "$conf" | cut -d'"' -f2)
    work_dir="${work_dir/#\~/$HOME}"
    [[ -z "$work_dir" ]] && continue
    if [[ "$PWD" == "$work_dir"* ]]; then
      local pname; pname=$(basename "$conf" .conf)
      local current; current=$(grep "^active=" "$active_conf" 2>/dev/null | cut -d= -f2)
      # Per-repo override: .ctx file in repo root
      local repo_root; repo_root=$(git rev-parse --show-toplevel 2>/dev/null || echo "")
      if [[ -n "$repo_root" && -f "$repo_root/.ctx" ]]; then
        local override; override=$(grep "^profile=" "$repo_root/.ctx" | cut -d= -f2)
        [[ -n "$override" ]] && pname="$override"
      fi
      if [[ "$pname" != "$current" ]]; then
        CTX_QUIET=1 ctx use "$pname" 2>/dev/null
      fi
      echo -e "\033[2m[ctx] $pname\033[0m"
      return 0
    fi
  done
}
if [[ -n "${ZSH_VERSION:-}" ]]; then
  autoload -Uz add-zsh-hook && add-zsh-hook chpwd _ctx_auto_switch
elif [[ -n "${BASH_VERSION:-}" ]]; then
  PROMPT_COMMAND="_ctx_auto_switch;${PROMPT_COMMAND:+ $PROMPT_COMMAND}"
fi
# ─────────────────────────────────────────────────────────────────────────────
HOOK

  success "ctx auto-switch hook: added to $SHELL_RC"
}

# ─── Verify everything installed correctly ────────────────────────────────────
verify_install() {
  local all_ok=true

  echo ""
  bold "  Verifying installation..."
  echo ""

  _v() {
    local cmd="$1" label="$2"
    if command -v "$cmd" &>/dev/null; then
      success "$label: $($cmd --version 2>&1 | head -1)"
    else
      warn "$label: not found (may need shell reload)"
      all_ok=false
    fi
  }

  _v ctx   "ctx"
  _v mise  "mise"
  _v gum   "gum"
  _v gh    "gh CLI"
  _v git   "git"
  _v aws   "AWS CLI" 2>/dev/null || true

  echo ""
  if $all_ok; then
    success "All tools installed correctly."
  else
    warn "Some tools may need a shell reload to appear in PATH."
  fi
}

# ─── Print final instructions ─────────────────────────────────────────────────
print_next_steps() {
  echo ""
  bold "  Installation complete."
  echo ""
  echo -e "  ${DIM}One step required — reload your shell:${RESET}"
  echo ""
  echo -e "    ${CYAN}source $SHELL_RC${RESET}"
  echo ""
  echo -e "  ${DIM}Then get started:${RESET}"
  echo ""
  echo -e "    ${CYAN}ctx init${RESET}      ${DIM}# verify everything is wired up${RESET}"
  echo -e "    ${CYAN}ctx setup${RESET}     ${DIM}# configure a client profile${RESET}"
  echo -e "    ${CYAN}ctx use <name>${RESET} ${DIM}# activate a client context${RESET}"
  echo ""
  dim "  Profiles live in: ~/.ctx/profiles/"
  dim "  Secrets live in:  macOS Keychain, or ~/.ctx/secrets on other OSes (0600; encrypt disk)"
  dim "  Env vars live in: ~/clients/<name>/mise.toml (auto-loaded on cd)"
  echo ""
}

# ─── Main installation sequence ───────────────────────────────────────────────
main() {
  # Invoked by `ctx upgrade`: refresh ctx binary + libs only; do not install tools,
  # append shell rc snippets, or touch ~/.ctx profiles / client directories.
  if [[ "${CTX_UPGRADE_ONLY:-0}" == "1" ]]; then
    echo ""
    bold "  ctx upgrade (in-place)"
    dim "  Updating the ctx program only (~/.ctx profiles and client trees are unchanged)."
    echo ""
    install_ctx
    echo ""
    dim "  Open a new terminal or: source $SHELL_RC"
    return 0
  fi

  step "1/7" "Homebrew"
  install_homebrew

  step "2/7" "Core tools (mise, gum, gh)"
  install_mise
  install_gum
  install_gh

  step "3/7" "Fallback env loader (direnv)"
  install_direnv

  step "4/7" "Optional cloud CLIs"
  install_optional_tools

  step "5/7" "mise shell activation"
  activate_mise_in_shell

  step "6/7" "ctx"
  install_ctx

  step "7/7" "ctx auto-switch hook"
  install_ctx_hook

  verify_install
  print_next_steps
}

main "$@"
