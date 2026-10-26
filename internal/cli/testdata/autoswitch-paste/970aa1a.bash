# ctx profile autoswitch — appended to shell rc by install.sh / ctx install-hook
# Longest WORK_DIR prefix wins; .ctx in git repo overrides; manual ctx use locks until cd.

_ctx_profile_autoswitch() {
  command -v ctx &>/dev/null || return 0
  local profiles_dir="${CTX_DIR:-$HOME/.ctx}/profiles"
  local active_conf="${CTX_DIR:-$HOME/.ctx}/config"
  [[ -d "$profiles_dir" ]] || return 0

  local best_pname="" best_len=0 conf work_dir len
  for conf in "$profiles_dir"/*.conf; do
    [[ -e "$conf" ]] || continue
    work_dir="$(bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "$conf")"
    work_dir="${work_dir/#\~/$HOME}"
    [[ -z "$work_dir" ]] && continue
    if [[ "$PWD" == "$work_dir" || "$PWD" == "$work_dir/"* ]]; then
      len=${#work_dir}
      if [[ $len -gt $best_len ]]; then
        best_len=$len
        best_pname="$(basename "$conf" .conf)"
      fi
    fi
  done

  local path_profile="$best_pname"
  if [[ -n "$path_profile" ]]; then
    local repo_root
    repo_root="$(git rev-parse --show-toplevel 2>/dev/null || echo "")"
    if [[ -n "$repo_root" && -f "$repo_root/.ctx" ]]; then
      local ov
      ov="$(grep "^profile=" "$repo_root/.ctx" 2>/dev/null | tail -1 | cut -d= -f2-)"
      if [[ -n "$ov" ]] && [[ -f "${profiles_dir}/${ov}.conf" ]]; then
        path_profile="$ov"
      fi
    fi
  fi

  local current src manual_anchor target
  current="$(grep "^active=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2-)"
  src="$(grep "^active_source=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2- | tr '[:upper:]' '[:lower:]')"
  manual_anchor=""
  if [[ "$src" == "manual" ]]; then
    local raw dec
    raw="$(grep "^manual_pwd=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2-)"
    if [[ -n "$raw" ]]; then
      dec="$(printf '%s' "$raw" | base64 -d 2>/dev/null || printf '%s' "$raw" | base64 -D 2>/dev/null || printf '%s' "$raw" | base64 --decode 2>/dev/null || true)"
      manual_anchor="${dec:-$raw}"
    fi
  fi

  if [[ "$src" == "manual" && -n "$manual_anchor" && "$PWD" == "$manual_anchor" ]]; then
    target="$current"
  else
    target="$path_profile"
  fi

  local _as_key="${PWD}|${target:-}"
  if [[ "$_as_key" == "${_CTX_AS_STATE:-}" ]]; then
    return 0
  fi
  _CTX_AS_STATE="$_as_key"

  local dim="\033[2m" rst="\033[0m"
  if [[ -n "$target" ]]; then
    if [[ -n "$current" && "$current" != "$target" ]]; then
      echo -e "${dim}[ctx] ← ${current} → ${target}${rst}" >&2
      eval "$(CTX_QUIET=1 ctx deactivate --eval bash 2>/dev/null)" || true
      CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use "$target" 2>/dev/null || true
    elif [[ -z "$current" ]]; then
      echo -e "${dim}[ctx] → ${target}${rst}" >&2
      CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use "$target" 2>/dev/null || true
    fi
    export CTX_ACTIVE_PROFILE="$target"
    export CTX_ACTIVATION_TRIGGER=auto
  else
    if [[ -n "$current" ]]; then
      echo -e "${dim}[ctx] ← ${current}${rst}" >&2
      eval "$(CTX_QUIET=1 ctx deactivate --eval bash 2>/dev/null)" || true
    fi
    unset CTX_ACTIVE_PROFILE 2>/dev/null || true
    unset CTX_ACTIVATION_TRIGGER 2>/dev/null || true
  fi
}

if [[ -n "${ZSH_VERSION:-}" ]]; then
  autoload -Uz add-zsh-hook 2>/dev/null || true
  add-zsh-hook chpwd _ctx_profile_autoswitch 2>/dev/null || true
  _ctx_profile_autoswitch
elif [[ -n "${BASH_VERSION:-}" ]]; then
  PROMPT_COMMAND="_ctx_profile_autoswitch${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
  PROMPT_COMMAND="${PROMPT_COMMAND//_ctx_auto_switch;/}"
  _ctx_profile_autoswitch
fi
