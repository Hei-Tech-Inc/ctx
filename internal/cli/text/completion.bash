_ctx_comp() {
  local cur="${COMP_WORDS[COMP_CWORD]}" prev="${COMP_WORDS[COMP_CWORD-1]}"
  local cmds="init setup import add use deactivate list ls status st clone workdir-prompt config secret verify edit remove rm del undo install-hook upgrade uninstall doctor guard run env agent-rules mcp knowledge completion version help"
  local profiles; profiles=$(ctx list 2>/dev/null | grep '^  [a-z]' | awk '{print $1}' | tr '\n' ' ')
  local w1="${COMP_WORDS[1]-}"
  # ctx list|status [--json]
  if [[ "$w1" =~ ^(list|ls|status|st)$ && "$prev" == "$w1" ]]; then
    COMPREPLY=($(compgen -W "--json" -- "$cur"))
    return
  fi
  # ctx setup [--config FILE] [-y|--yes]
  if [[ "$w1" =~ ^(setup|import|add)$ ]]; then
    if [[ "$prev" == "--config" ]]; then
      COMPREPLY=($(compgen -f -- "$cur"))
      return
    fi
    if [[ "$prev" == "--scope" ]]; then
      COMPREPLY=($(compgen -W "clone full" -- "$cur"))
      return
    fi
    if [[ "$prev" == "--extend" ]]; then
      COMPREPLY=($(compgen -W "$profiles" -- "$cur"))
      return
    fi
    COMPREPLY=($(compgen -W "--config --scope --extend -y --yes --help -h" -- "$cur"))
    return
  fi
  if [[ "$prev" == "help" ]]; then
    COMPREPLY=($(compgen -W "setup" -- "$cur"))
    return
  fi
  case "$prev" in
    use|edit|remove|rm|del|verify) COMPREPLY=($(compgen -W "$profiles" -- "$cur")) ;;
    env) COMPREPLY=($(compgen -W "-p --profile --secrets --fish" -- "$cur")) ;;
    run) COMPREPLY=($(compgen -W "-p --profile" -- "$cur")) ;;
    mcp) COMPREPLY=($(compgen -W "install --profile" -- "$cur")) ;;
    install) [[ "$w1" == mcp ]] && COMPREPLY=($(compgen -W "claude cursor" -- "$cur")) ;;
    knowledge) COMPREPLY=($(compgen -W "init" -- "$cur")) ;;
    init) [[ "$w1" == knowledge ]] && COMPREPLY=($(compgen -W "$profiles" -- "$cur")) ;;
    agent-rules) COMPREPLY=($(compgen -W "--write" -- "$cur")) ;;
    --write) [[ "$w1" == agent-rules ]] && COMPREPLY=($(compgen -f -- "$cur")) ;;
    -p|--profile) [[ "$w1" =~ ^(env|run|mcp)$ ]] && COMPREPLY=($(compgen -W "$profiles" -- "$cur")) ;;
    clone) COMPREPLY=($(compgen -W "-p --profile" -- "$cur")) ;;
    secret) COMPREPLY=($(compgen -W "set get list delete migrate" -- "$cur")) ;;
    config) COMPREPLY=($(compgen -W "show work-root secret-provider prompt-workdir-depth prompt-extra-paths autoswitch-notify export import" -- "$cur")) ;;
    guard) COMPREPLY=($(compgen -W "status install uninstall remove check" -- "$cur")) ;;
    upgrade) COMPREPLY=($(compgen -W "--check" -- "$cur")) ;;
    uninstall) COMPREPLY=($(compgen -W "--purge" -- "$cur")) ;;
    completion) COMPREPLY=($(compgen -W "zsh bash fish" -- "$cur")) ;;
    ctx) COMPREPLY=($(compgen -W "$cmds --dry-run --quiet -q --no-gum --json -v --version -h --help" -- "$cur")) ;;
  esac
  if [[ ${COMP_WORDS[1]} == clone && ${COMP_WORDS[COMP_CWORD-2]} == @(-p|--profile) ]]; then
    COMPREPLY=($(compgen -W "$profiles" -- "$cur"))
  fi
  if [[ ${COMP_WORDS[1]} == use ]]; then
    [[ $COMP_CWORD -eq 3 ]] && COMPREPLY=($(compgen -W "--eval" -- "$cur"))
    [[ "$prev" == --eval ]] && COMPREPLY=($(compgen -W "bash fish" -- "$cur"))
  fi
  if [[ ${COMP_WORDS[1]} == secret && ${COMP_WORDS[COMP_CWORD-1]} =~ ^(set|get|delete)$ ]]; then
    COMPREPLY=($(compgen -W "$profiles" -- "$cur"))
  fi
}
complete -F _ctx_comp ctx
