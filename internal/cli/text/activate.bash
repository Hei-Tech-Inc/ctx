# ctx shell integration for bash: eval "$(ctx activate bash)"
# Runs in your interactive shell: no set -e/-u, no exit, _ctx names only.
# Safe to eval again (one registration; the legacy autoswitch hook is removed).

# ctx runs as a child process, so `ctx use` / `ctx deactivate` cannot change this shell's
# environment by themselves. This function evals their --eval output; every other
# subcommand goes straight to the binary. Global flags may come before the subcommand.
ctx() {
  local -a _ctx_pre=() _ctx_args=()
  local _ctx_a _ctx_out _ctx_rc _ctx_sub _ctx_dd="" _ctx_q="" _ctx_ng="" _ctx_js=""
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
  _ctx_sub="$1"
  shift
  if [[ "${CTX_CORE:-}" == off || ! -x @CTX_CORE@ ]]; then
    _ctx_out="$(command ctx ${_ctx_pre[@]+"${_ctx_pre[@]}"} "$_ctx_sub" --eval bash "$@")"
    _ctx_rc=$?
  else
    # bin/ctx's flag parsing: global flags anywhere before --, as environment.
    for _ctx_a in ${_ctx_pre[@]+"${_ctx_pre[@]}"} "$@"; do
      if [[ -n "$_ctx_dd" ]]; then _ctx_args+=("$_ctx_a"); continue; fi
      case "$_ctx_a" in
        --) _ctx_dd=1; _ctx_args+=("$_ctx_a") ;;
        --quiet|-q) _ctx_q=1 ;;
        --no-gum) _ctx_ng=1 ;;
        --json) _ctx_js=1 ;;
        *) _ctx_args+=("$_ctx_a") ;;
      esac
    done
    _ctx_out="$(
      export CTX_QUIET="${_ctx_q:-${CTX_QUIET:-0}}" CTX_DRY_RUN=0 CTX_JSON="${_ctx_js:-0}"
      [[ -z "$_ctx_ng" ]] || export CTX_NO_GUM=1
      exec @CTX_CORE@ "$_ctx_sub" --eval bash ${_ctx_args[@]+"${_ctx_args[@]}"}
    )"
    _ctx_rc=$?
  fi
  eval "$_ctx_out"
  return "$_ctx_rc"
}

# The autoswitch hook. An unchanged prompt runs builtins only: ctx-core hook-env runs
# when $PWD changed, this shell's stamp is missing, or the config or the profiles dir
# changed since (a `ctx use` in another terminal). Keeps the caller's $?.
_ctx_hook() {
  local _ctx_st=$? _ctx_out _ctx_d="${CTX_DIR:-$HOME/.ctx}"
  if [[ "$PWD" != "${__CTX_PWD-}" || ! -e "${__CTX_STAMP-}" || "$_ctx_d/config" -nt "$__CTX_STAMP" || "$_ctx_d/profiles" -nt "$__CTX_STAMP" ]]; then
    [[ -x @CTX_CORE@ ]] || return "$_ctx_st"
    _ctx_out="$(__CTX_STATE="${__CTX_STATE-}" __CTX_STAMP="${__CTX_STAMP-}" \
      __CTX_SH_NOTIFY="${CTX_AUTOSWITCH_NOTIFY+=$CTX_AUTOSWITCH_NOTIFY}" \
      __CTX_SH_GH_TOKEN="${GH_TOKEN+=$GH_TOKEN}" \
      __CTX_SH_ACTIVE="${CTX_ACTIVE_PROFILE+=$CTX_ACTIVE_PROFILE}" \
      __CTX_SH_GH_PROFILE="${_CTX_GH_PROFILE+=$_CTX_GH_PROFILE}" \
      @CTX_CORE@ hook-env bash)"
    eval "$_ctx_out"
    __CTX_PWD="$PWD"
  fi
  return "$_ctx_st"
}

# _ctx_pc_strip CMD: CMD without the hook's names (ours and the legacy ones) as
# ;- or newline-separated commands; the result is in _ctx_pc.
_ctx_pc_strip() {
  local _ctx_t _ctx_l _ctx_r _ctx_prev _ctx_nl=$'\n'
  _ctx_pc=";$1;"
  for _ctx_t in _ctx_hook _ctx_profile_autoswitch _ctx_auto_switch; do
    for _ctx_l in ";" "$_ctx_nl"; do
      for _ctx_r in ";" "$_ctx_nl"; do
        _ctx_prev=""
        while [[ "$_ctx_pc" != "$_ctx_prev" ]]; do
          _ctx_prev="$_ctx_pc"
          _ctx_pc="${_ctx_pc//"$_ctx_l$_ctx_t$_ctx_r"/$_ctx_l}"
        done
      done
    done
  done
  _ctx_pc="${_ctx_pc#;}"
  _ctx_pc="${_ctx_pc%;}"
}

# Registers _ctx_hook once: in bash-preexec's precmd_functions when bash-preexec is
# loaded, else first in PROMPT_COMMAND (an element when it is an array, bash 5.1+).
_ctx_activate() {
  local _ctx_pc _ctx_c _ctx_f _ctx_arr=""
  local -a _ctx_new=()
  if (( BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 1) )); then
    eval '[[ -n "${PROMPT_COMMAND+x}" && "${PROMPT_COMMAND@a}" == *a* ]]' && _ctx_arr=1
  fi
  if [[ -n "$_ctx_arr" ]]; then
    for _ctx_c in "${PROMPT_COMMAND[@]}"; do
      _ctx_pc_strip "$_ctx_c"
      [[ -z "$_ctx_pc" && "$_ctx_c" != "$_ctx_pc" ]] || _ctx_new+=("$_ctx_pc")
    done
    PROMPT_COMMAND=(${_ctx_new[@]+"${_ctx_new[@]}"})
  else
    _ctx_pc_strip "${PROMPT_COMMAND:-}"
    PROMPT_COMMAND="$_ctx_pc"
  fi
  _ctx_new=()
  if [[ -n "${precmd_functions+x}" ]]; then
    for _ctx_f in ${precmd_functions[@]+"${precmd_functions[@]}"}; do
      case "$_ctx_f" in
        _ctx_hook|_ctx_profile_autoswitch|_ctx_auto_switch) ;;
        *) _ctx_new+=("$_ctx_f") ;;
      esac
    done
    precmd_functions=(${_ctx_new[@]+"${_ctx_new[@]}"})
  fi
  if [[ -n "${bash_preexec_imported:-}${__bp_imported:-}" ]]; then
    precmd_functions+=(_ctx_hook)
  elif [[ -n "$_ctx_arr" ]]; then
    PROMPT_COMMAND=(_ctx_hook ${PROMPT_COMMAND[@]+"${PROMPT_COMMAND[@]}"})
  else
    PROMPT_COMMAND="_ctx_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
  fi
  unset -f _ctx_profile_autoswitch _ctx_auto_switch 2>/dev/null
}
_ctx_activate
unset -f _ctx_activate _ctx_pc_strip
_ctx_hook
