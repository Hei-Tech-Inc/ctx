# ctx profile autoswitch — appended to shell rc by install.sh / ctx install-hook
# Longest WORK_DIR prefix wins; sibling …/clients/<name>.conf under a broad prefix;
# nearest .ctx (walk $PWD → git root) overrides; manual ctx use locks until cd.
# Sets CTX_PROMPT_SHOW / CTX_PROMPT_WORK_DIR / CTX_PROMPT_PROFILE for prompts (see README).
# Runs on every prompt: the no-switch path must stay builtin-only (no subprocesses).

# ctx runs as a child process, so `ctx use` / `ctx deactivate` cannot change this shell's
# environment by themselves. This function evals their --eval output; every other
# subcommand goes straight to the binary. Global flags may come before the subcommand.
ctx() {
  local -a _ctx_pre=()
  local _ctx_a _ctx_out _ctx_rc
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --quiet|-q|--no-gum|--dry-run|--json) _ctx_pre+=("$1"); shift ;;
      *) break ;;
    esac
  done
  case "${1:-}" in
    use|deactivate) ;;
    *) command ctx ${_ctx_pre[@]+"${_ctx_pre[@]}"} "$@"; return ;;
  esac
  # --dry-run must not touch the shell; an explicit --eval means the caller evals it.
  for _ctx_a in ${_ctx_pre[@]+"${_ctx_pre[@]}"} "$@"; do
    [[ "$_ctx_a" == -- ]] && break
    case "$_ctx_a" in
      --dry-run|--eval|--eval=*) command ctx ${_ctx_pre[@]+"${_ctx_pre[@]}"} "$@"; return ;;
    esac
  done
  _ctx_a="$1"
  shift
  _ctx_out="$(command ctx ${_ctx_pre[@]+"${_ctx_pre[@]}"} "$_ctx_a" --eval bash "$@")"
  _ctx_rc=$?
  eval "$_ctx_out"
  return "$_ctx_rc"
}

# True when the ctx executable (not this function) is on PATH.
_ctx_as_have_bin() {
  if [[ -n "${ZSH_VERSION:-}" ]]; then
    whence -p ctx >/dev/null 2>&1
  else
    type -P ctx >/dev/null 2>&1
  fi
}

# Run ctx use for the hook and apply its exports to this shell. Returns ctx use's status.
_ctx_as_use() {
  local out rc
  out="$(CTX_QUIET=1 CTX_AUTO_SWITCH=1 command ctx use --eval bash "$1" 2>/dev/null)"
  rc=$?
  eval "$out"
  return "$rc"
}

# Last-wins KEY=value lookup (same result as grep | tail -1). Sets _ctx_kv.
_ctx_as_kv_get() {
  local file="$1" key="$2" line
  _ctx_kv=""
  [[ -f "$file" ]] || return 0
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" == "${key}="* ]] && _ctx_kv="${line#"${key}"=}"
  done <"$file"
  _ctx_kv="${_ctx_kv//$'\r'/}"
}

# WORK_DIR from a profile .conf. Sets _ctx_wd.
# Values that need shell expansion or unquoting fall back to sourcing the file in bash.
_ctx_as_profile_work_dir() {
  local conf="$1" v
  _ctx_wd=""
  _ctx_as_kv_get "$conf" WORK_DIR
  v="$_ctx_kv"
  if [[ "$v" == \"*\" && ${#v} -ge 2 ]]; then
    v="${v#\"}"
    v="${v%\"}"
  fi
  case "$v" in
    '${HOME}'|'${HOME}'/*) v="$HOME${v#'${HOME}'}" ;;
    '$HOME'|'$HOME'/*) v="$HOME${v#'$HOME'}" ;;
  esac
  case "$v" in
    *'$'*|*'`'*|*'\'*|*"'"*|*'"'*)
      v="$(bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "$conf")"
      ;;
  esac
  _ctx_wd="${v/#\~/$HOME}"
}

# Per-terminal gh login: GH_TOKEN for the profile's GITHUB_USER, so a switch in one shell
# never changes the account another shell uses (gh auth switch is machine-wide).
# A GH_TOKEN the user set themselves is left alone. Uses $profiles_dir from the caller.
_ctx_as_gh_token() {
  local p="$1" user tok
  if [[ -n "${_CTX_GH_PROFILE+x}" ]]; then
    [[ "$_CTX_GH_PROFILE" == "$p" ]] && return 0
    unset GH_TOKEN _CTX_GH_PROFILE
  fi
  [[ -n "$p" && -z "${GH_TOKEN:-}" ]] || return 0
  _ctx_as_kv_get "$profiles_dir/$p.conf" GITHUB_USER
  user="${_ctx_kv//[\"\']/}"
  [[ -n "$user" ]] && command -v gh >/dev/null 2>&1 || return 0
  tok="$(gh auth token -u "$user" 2>/dev/null)" || return 0
  [[ -n "$tok" ]] || return 0
  export GH_TOKEN="$tok" _CTX_GH_PROFILE="$p"
}

# One pass over ~/.ctx/config (last value wins). Sets _cfg_* in the caller's scope.
_ctx_as_read_config() {
  local line
  _cfg_active="" _cfg_source="" _cfg_manual_pwd="" _cfg_pmd="" _cfg_pex="" _cfg_notify=""
  [[ -f "$active_conf" ]] || return 0
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line//$'\r'/}"
    case "$line" in
      active=*) _cfg_active="${line#active=}" ;;
      active_source=*) _cfg_source="${line#active_source=}" ;;
      manual_pwd=*) _cfg_manual_pwd="${line#manual_pwd=}" ;;
      prompt_workdir_max_depth=*) _cfg_pmd="${line#prompt_workdir_max_depth=}" ;;
      prompt_extra_paths=*) _cfg_pex="${line#prompt_extra_paths=}" ;;
      autoswitch_notify=*) _cfg_notify="${line#autoswitch_notify=}" ;;
    esac
  done <"$active_conf"
}

# Path segments of $1 below $2, or -1 when not under it. Sets _ctx_depth.
_ctx_rel_depth_under() {
  local p="${1%/}" r="${2%/}" x s
  _ctx_depth=-1
  if [[ "$p" == "$r" ]]; then
    _ctx_depth=0
    return 0
  fi
  [[ "$p" == "$r/"* ]] || return 0
  x="${p#"${r}"/}"
  s="${x//[!\/]/}"
  _ctx_depth=$(( ${#s} + 1 ))
}

_ctx_prompt_update_exports() {
  local pmd="${_cfg_pmd// /}" display_root="" prompt_show=0 piece rest
  [[ "$pmd" =~ ^[0-9]+$ ]] || pmd=2

  if [[ -n "$path_profile" ]]; then
    display_root="$path_wd"
    [[ -z "$display_root" && -n "${best_work_dir:-}" ]] && display_root="$best_work_dir"
  fi

  if [[ -n "$display_root" ]]; then
    _ctx_rel_depth_under "$PWD" "$display_root"
    [[ "$_ctx_depth" -ge 0 && "$_ctx_depth" -le "$pmd" ]] && prompt_show=1
  fi

  if [[ "$prompt_show" != "1" && -n "$_cfg_pex" ]]; then
    rest="${_cfg_pex}:"
    while [[ -n "$rest" ]]; do
      piece="${rest%%:*}"
      rest="${rest#*:}"
      [[ -z "$piece" ]] && continue
      piece="${piece/#\~/$HOME}"
      piece="${piece%/}"
      if [[ -n "$piece" && ( "$PWD" == "$piece" || "$PWD" == "$piece/"* ) ]]; then
        prompt_show=1
        display_root="$piece"
        break
      fi
    done
  fi

  if [[ "$prompt_show" == "1" ]]; then
    export CTX_PROMPT_SHOW=1
    export CTX_PROMPT_PROFILE="${path_profile:-$_cfg_active}"
    export CTX_PROMPT_WORK_DIR="${display_root:-}"
  else
    export CTX_PROMPT_SHOW=0
    unset CTX_PROMPT_PROFILE 2>/dev/null || true
    unset CTX_PROMPT_WORK_DIR 2>/dev/null || true
  fi
}

_ctx_profile_autoswitch() {
  _ctx_as_have_bin || return 0
  local profiles_dir="${CTX_DIR:-$HOME/.ctx}/profiles"
  local active_conf="${CTX_DIR:-$HOME/.ctx}/config"
  [[ -d "$profiles_dir" ]] || return 0

  local _ctx_kv="" _ctx_wd="" _ctx_depth=0
  local _cfg_active="" _cfg_source="" _cfg_manual_pwd="" _cfg_pmd="" _cfg_pex="" _cfg_notify=""
  local best_pname="" best_len=0 best_wd="" conf len conf_name
  for conf in "$profiles_dir"/*.conf; do
    [[ -e "$conf" ]] || continue
    _ctx_as_profile_work_dir "$conf"
    [[ -z "$_ctx_wd" ]] && continue
    if [[ "$PWD" == "$_ctx_wd" || "$PWD" == "$_ctx_wd/"* ]]; then
      len=${#_ctx_wd}
      if [[ $len -gt $best_len ]]; then
        best_len=$len
        conf_name="${conf##*/}"
        best_pname="${conf_name%.conf}"
        best_wd="$_ctx_wd"
      fi
    fi
  done

  local path_profile="$best_pname" best_work_dir="$best_wd"
  if [[ -n "$best_pname" && -n "$best_work_dir" && "$PWD" == "${best_work_dir}/"* ]]; then
    local rel="${PWD#"${best_work_dir}"/}"
    local first="${rel%%/*}"
    if [[ -n "$first" && -f "${profiles_dir}/${first}.conf" ]]; then
      local wdc
      _ctx_as_profile_work_dir "${profiles_dir}/${first}.conf"
      wdc="$_ctx_wd"
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
    local repo_root="" d parent ov
    d="${PWD%/}"
    while [[ -n "$d" ]]; do
      if [[ -e "$d/.git" ]]; then
        repo_root="$d"
        break
      fi
      parent="${d%/*}"
      [[ "$parent" == "$d" ]] && break
      d="$parent"
    done
    if [[ -n "$repo_root" ]]; then
      d="${PWD%/}"
      while true; do
        if [[ -f "$d/.ctx" ]]; then
          _ctx_as_kv_get "$d/.ctx" profile
          ov="$_ctx_kv"
          ov="${ov#"${ov%%[![:space:]]*}"}"
          ov="${ov%"${ov##*[![:space:]]}"}"
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

  local path_wd=""
  if [[ -n "$path_profile" ]]; then
    _ctx_as_profile_work_dir "${profiles_dir}/${path_profile}.conf"
    path_wd="$_ctx_wd"
    [[ -n "$path_wd" ]] && best_work_dir="$path_wd"
  fi

  _ctx_as_read_config
  local current="$_cfg_active" src_manual=0 manual_anchor="" target
  case "$_cfg_source" in [Mm][Aa][Nn][Uu][Aa][Ll]) src_manual=1 ;; esac
  if [[ "$src_manual" == "1" && -n "$_cfg_manual_pwd" ]]; then
    # Decode once per change: the manual lock persists across many prompts.
    if [[ "$_cfg_manual_pwd" != "${_CTX_AS_MANUAL_RAW:-}" ]]; then
      local raw="$_cfg_manual_pwd" dec
      dec="$(printf '%s' "$raw" | base64 -d 2>/dev/null || printf '%s' "$raw" | base64 -D 2>/dev/null || printf '%s' "$raw" | base64 --decode 2>/dev/null || true)"
      _CTX_AS_MANUAL_RAW="$raw"
      _CTX_AS_MANUAL_DEC="${dec:-$raw}"
    fi
    manual_anchor="$_CTX_AS_MANUAL_DEC"
  fi

  if [[ "$src_manual" == "1" && -n "$manual_anchor" && "$PWD" == "$manual_anchor" ]]; then
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

  # Default off: avoid leaking client profile names on new tabs / screen share.
  # Enable: ctx config autoswitch-notify on  (or CTX_AUTOSWITCH_NOTIFY=1)
  local _ctx_notify=0 _nv="${CTX_AUTOSWITCH_NOTIFY:-$_cfg_notify}"
  _nv="${_nv//[[:space:]]/}"
  case "$_nv" in 1|[Tt][Rr][Uu][Ee]|[Yy][Ee][Ss]|[Oo][Nn]) _ctx_notify=1 ;; esac

  local dim="\033[2m" rst="\033[0m" _ctx_applied=0
  if [[ -n "$target" ]]; then
    if [[ -n "$current" && "$current" != "$target" ]]; then
      [[ "$_ctx_notify" == "1" ]] && echo -e "${dim}[ctx] ← ${current} → ${target}${rst}" >&2
      eval "$(CTX_QUIET=1 command ctx deactivate --eval bash 2>/dev/null)" || true
      if _ctx_as_use "$target"; then
        _ctx_applied=1
      else
        [[ "$_ctx_notify" == "1" ]] && echo -e "${dim}[ctx] ctx use ${target} failed — run: ctx use ${target}${rst}" >&2
      fi
    elif [[ -z "$current" ]]; then
      [[ "$_ctx_notify" == "1" ]] && echo -e "${dim}[ctx] → ${target}${rst}" >&2
      if _ctx_as_use "$target"; then
        _ctx_applied=1
      else
        [[ "$_ctx_notify" == "1" ]] && echo -e "${dim}[ctx] ctx use ${target} failed — run: ctx use ${target}${rst}" >&2
      fi
    elif [[ "${CTX_ACTIVE_PROFILE:-}" != "$target" ]]; then
      # Already active machine-wide (e.g. a new terminal): this shell still needs the exports.
      _ctx_as_use "$target" || true
      _ctx_applied=1
    else
      _ctx_applied=1
    fi
    if [[ "$_ctx_applied" == "1" ]]; then
      export CTX_ACTIVE_PROFILE="$target"
      export CTX_ACTIVATION_TRIGGER=auto
      _ctx_as_gh_token "$target"
    fi
  else
    if [[ -n "$current" ]]; then
      [[ "$_ctx_notify" == "1" ]] && echo -e "${dim}[ctx] ← ${current}${rst}" >&2
      eval "$(CTX_QUIET=1 command ctx deactivate --eval bash 2>/dev/null)" || true
    fi
    unset CTX_ACTIVE_PROFILE 2>/dev/null || true
    unset CTX_ACTIVATION_TRIGGER 2>/dev/null || true
    _ctx_as_gh_token ""
  fi
}

if [[ -n "${ZSH_VERSION:-}" ]]; then
  autoload -Uz add-zsh-hook 2>/dev/null || true
  add-zsh-hook chpwd _ctx_profile_autoswitch 2>/dev/null || true
  add-zsh-hook precmd _ctx_profile_autoswitch 2>/dev/null || true
  _ctx_profile_autoswitch
elif [[ -n "${BASH_VERSION:-}" ]]; then
  # Idempotent: strip our hook (and legacy token) so reinstall / re-source does not stack.
  PROMPT_COMMAND="${PROMPT_COMMAND:-}"
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
