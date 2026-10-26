# ctx profile autoswitch — appended to shell rc by install.sh / ctx install-hook
# Longest WORK_DIR prefix wins; sibling …/clients/<name>.conf under a broad prefix;
# nearest .ctx (walk $PWD → git root) overrides; manual ctx use locks until cd.
# Sets CTX_PROMPT_SHOW / CTX_PROMPT_WORK_DIR / CTX_PROMPT_PROFILE for prompts (see README).

_ctx_rel_depth_under() {
  local p="${1%/}" r="${2%/}" x
  [[ "$p" == "$r" ]] && { echo 0; return; }
  [[ "$p" == "$r/"* ]] || { echo -1; return; }
  x="${p#"${r}"/}"
  [[ -z "$x" ]] && { echo 0; return; }
  [[ "$x" != */* ]] && { echo 1; return; }
  printf '%s\n' "$x" | awk -F/ '{print NF}'
}

_ctx_prompt_update_exports() {
  local pmd=2 pex="" display_root="" profwd d prompt_show=0 piece
  pmd="$(grep "^prompt_workdir_max_depth=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2-)"
  pmd="${pmd// /}"
  [[ "$pmd" =~ ^[0-9]+$ ]] || pmd=2
  pex="$(grep "^prompt_extra_paths=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2-)"

  if [[ -n "$path_profile" ]]; then
    profwd="$(bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "${profiles_dir}/${path_profile}.conf")"
    profwd="${profwd/#\~/$HOME}"
    display_root="$profwd"
    [[ -z "$display_root" && -n "${best_work_dir:-}" ]] && display_root="$best_work_dir"
  fi

  if [[ -n "$display_root" ]]; then
    d="$(_ctx_rel_depth_under "$PWD" "$display_root")"
    d="${d//$'\n'/}"
    [[ "$d" -ge 0 && "$d" -le "$pmd" ]] && prompt_show=1
  fi

  if [[ "$prompt_show" != "1" && -n "$pex" ]]; then
    while IFS= read -r piece; do
      [[ -z "$piece" ]] && continue
      piece="${piece/#\~/$HOME}"
      piece="${piece%/}"
      if [[ -n "$piece" && ( "$PWD" == "$piece" || "$PWD" == "$piece/"* ) ]]; then
        prompt_show=1
        display_root="$piece"
        break
      fi
    done < <(printf '%s' "$pex" | tr ':' '\n')
  fi

  if [[ "$prompt_show" == "1" ]]; then
    export CTX_PROMPT_SHOW=1
    local pname_show="${path_profile:-}"
    if [[ -z "$pname_show" ]]; then
      pname_show="$(grep "^active=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2-)"
      pname_show="${pname_show//$'\r'/}"
    fi
    export CTX_PROMPT_PROFILE="${pname_show:-}"
    export CTX_PROMPT_WORK_DIR="${display_root:-}"
  else
    export CTX_PROMPT_SHOW=0
    unset CTX_PROMPT_PROFILE 2>/dev/null || true
    unset CTX_PROMPT_WORK_DIR 2>/dev/null || true
  fi
}

_ctx_profile_autoswitch() {
  command -v ctx &>/dev/null || return 0
  local profiles_dir="${CTX_DIR:-$HOME/.ctx}/profiles"
  local active_conf="${CTX_DIR:-$HOME/.ctx}/config"
  [[ -d "$profiles_dir" ]] || return 0

  local best_pname="" best_len=0 best_wd="" conf work_dir len
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
        best_wd="$work_dir"
      fi
    fi
  done

  local path_profile="$best_pname" best_work_dir="$best_wd"
  if [[ -n "$best_pname" && -n "$best_work_dir" && "$PWD" == "${best_work_dir}/"* ]]; then
    local rel="${PWD#"${best_work_dir}"/}"
    local first="${rel%%/*}"
    if [[ -n "$first" && -f "${profiles_dir}/${first}.conf" ]]; then
      local wdc
      wdc="$(bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "${profiles_dir}/${first}.conf")"
      wdc="${wdc/#\~/$HOME}"
      [[ -z "$wdc" ]] && wdc="${best_work_dir%/}/$first"
      if [[ -d "$wdc" && ( "$PWD" == "$wdc" || "$PWD" == "$wdc/"* ) ]]; then
        local cand_len=${#wdc}
        if [[ $cand_len -gt ${#best_work_dir} ]]; then
          path_profile="$first"
          best_work_dir="$wdc"
        fi
      fi
    fi
  fi

  if [[ -n "$path_profile" ]]; then
    # Nearest .ctx from $PWD up to git root (nested client dirs beat monorepo root .ctx).
    local repo_root d parent ov
    repo_root="$(git rev-parse --show-toplevel 2>/dev/null || echo "")"
    repo_root="${repo_root%/}"
    if [[ -n "$repo_root" ]]; then
      d="${PWD%/}"
      while true; do
        if [[ -f "$d/.ctx" ]]; then
          ov="$(grep "^profile=" "$d/.ctx" 2>/dev/null | tail -1 | cut -d= -f2-)"
          ov="${ov//$'\r'/}"
          ov="$(printf '%s' "$ov" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
          if [[ -n "$ov" && -f "${profiles_dir}/${ov}.conf" ]]; then
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

  if [[ -n "$path_profile" ]]; then
    local _sync_wd
    _sync_wd="$(bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "${profiles_dir}/${path_profile}.conf")"
    _sync_wd="${_sync_wd/#\~/$HOME}"
    [[ -n "$_sync_wd" ]] && best_work_dir="$_sync_wd"
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

  _ctx_prompt_update_exports

  local _as_key="${PWD}|${target:-}"
  if [[ "$_as_key" == "${_CTX_AS_STATE:-}" ]]; then
    return 0
  fi
  _CTX_AS_STATE="$_as_key"

  local dim="\033[2m" rst="\033[0m" _ctx_applied=0
  if [[ -n "$target" ]]; then
    if [[ -n "$current" && "$current" != "$target" ]]; then
      echo -e "${dim}[ctx] ← ${current} → ${target}${rst}" >&2
      eval "$(CTX_QUIET=1 ctx deactivate --eval bash 2>/dev/null)" || true
      if CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use "$target" 2>/dev/null; then
        _ctx_applied=1
      else
        echo -e "${dim}[ctx] ctx use ${target} failed — run: ctx use ${target}${rst}" >&2
      fi
    elif [[ -z "$current" ]]; then
      echo -e "${dim}[ctx] → ${target}${rst}" >&2
      if CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use "$target" 2>/dev/null; then
        _ctx_applied=1
      else
        echo -e "${dim}[ctx] ctx use ${target} failed — run: ctx use ${target}${rst}" >&2
      fi
    else
      _ctx_applied=1
    fi
    if [[ "$_ctx_applied" == "1" ]]; then
      export CTX_ACTIVE_PROFILE="$target"
      export CTX_ACTIVATION_TRIGGER=auto
    fi
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
  add-zsh-hook precmd _ctx_profile_autoswitch 2>/dev/null || true
  _ctx_profile_autoswitch
elif [[ -n "${BASH_VERSION:-}" ]]; then
  # Idempotent: strip our hook (and legacy token) so reinstall / re-source does not stack.
  PROMPT_COMMAND="${PROMPT_COMMAND//_ctx_profile_autoswitch;/}"
  PROMPT_COMMAND="${PROMPT_COMMAND//;_ctx_profile_autoswitch/}"
  PROMPT_COMMAND="${PROMPT_COMMAND//_ctx_profile_autoswitch/}"
  PROMPT_COMMAND="${PROMPT_COMMAND//_ctx_auto_switch;/}"
  PROMPT_COMMAND="${PROMPT_COMMAND//;_ctx_auto_switch/}"
  PROMPT_COMMAND="${PROMPT_COMMAND//_ctx_auto_switch/}"
  while [[ "$PROMPT_COMMAND" == *";;"* ]]; do
    PROMPT_COMMAND="${PROMPT_COMMAND//;;/;}"
  done
  PROMPT_COMMAND="${PROMPT_COMMAND#;}"
  PROMPT_COMMAND="${PROMPT_COMMAND%;}"
  PROMPT_COMMAND="_ctx_profile_autoswitch${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
  _ctx_profile_autoswitch
fi
