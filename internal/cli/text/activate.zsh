# ctx shell integration for zsh: eval "$(ctx activate zsh)"
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
      @CTX_CORE@ hook-env zsh)"
    eval "$_ctx_out"
    typeset -g __CTX_PWD="$PWD"
  fi
  return "$_ctx_st"
}

# Registers _ctx_hook once on chpwd and precmd, removing the legacy hook.
# The hook's globals are declared here so WARN_CREATE_GLOBAL stays quiet.
typeset -g __CTX_STATE="${__CTX_STATE-}" __CTX_STAMP="${__CTX_STAMP-}" __CTX_PWD="${__CTX_PWD-}"
autoload -Uz add-zsh-hook 2>/dev/null
add-zsh-hook -d chpwd _ctx_profile_autoswitch 2>/dev/null
add-zsh-hook -d precmd _ctx_profile_autoswitch 2>/dev/null
add-zsh-hook chpwd _ctx_hook 2>/dev/null
add-zsh-hook precmd _ctx_hook 2>/dev/null
unfunction _ctx_profile_autoswitch 2>/dev/null
_ctx_hook
