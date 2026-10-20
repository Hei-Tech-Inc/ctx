#compdef ctx
_ctx() {
  local cmds profiles
  cmds=(
    'init:Check deps, install hooks'
    'setup:Configure a new profile'
    'import:Alias for setup'
    'add:Alias for setup'
    'use:Activate a profile'
    'deactivate:Clear active profile (shell eval)'
    'list:List all profiles'
    'status:Show active profile and live checks'
    'clone:git clone with GitHub host rewrite'
    'workdir-prompt:Print WORK_DIR line when cwd is in prompt scope'
    'config:Configure ctx defaults'
    'secret:Manage profile secrets'
    'verify:Verify profile wiring'
    'edit:Open profile config in $EDITOR'
    'remove:Delete a profile'
    'undo:Restore last backup'
    'install-hook:Install shell hooks'
    'upgrade:Update ctx via installer'
    'uninstall:Remove ctx install'
    'doctor:Health check'
    'guard:Git identity guard (status, install, uninstall)'
    'run:Run a command with the profile identity'
    'env:Print profile identity as exports'
    'agent-rules:Print or write AI agent instructions'
    'mcp:Run the ctx MCP server'
    'knowledge:Manage per-client knowledge notes'
    'completion:Print shell completion'
    'version:Print version'
    'help:Show help'
  )
  profiles=("${(@f)$(ctx list 2>/dev/null | grep '^  [a-z]' | awk '{print $1}')}")
  local setup_flags=(
    '--config:Non-interactive KEY=value file'
    '--scope:Profile scope (clone or full)'
    '--extend:Add to an existing profile'
    '-y:Skip final confirm'
    '--yes:Skip final confirm'
    '--help:Full setup reference'
    '-h:Full setup reference'
  )
  local help_topics=('setup:Wizard and --config reference')
  local global_flags=(
    '--dry-run:Preview without writing files'
    '--quiet:Suppress output'
    '--no-gum:Plain prompts'
    '--json:JSON output (list, status)'
  )
  case $CURRENT in
    2) _describe 'ctx flags' global_flags && _describe 'ctx commands' cmds ;;
    3)
      case ${words[2]} in
        --dry-run|--quiet|--no-gum|--json) _describe 'ctx commands' cmds ;;
        use|edit|remove|verify) _describe 'profiles' profiles ;;
        setup|import|add) _describe 'setup options' setup_flags ;;
        help) _describe 'help topics' help_topics ;;
        secret) local sub=('set' 'get' 'list' 'delete' 'migrate'); _describe 'subcommands' sub ;;
        config) local sub=('show' 'work-root' 'secret-provider' 'prompt-workdir-depth' 'prompt-extra-paths' 'autoswitch-notify' 'export' 'import'); _describe 'subcommands' sub ;;
        guard) local sub=('status' 'install' 'uninstall'); _describe 'subcommands' sub ;;
        upgrade) local sub=('--check'); _describe 'upgrade flags' sub ;;
        uninstall) local sub=('--purge'); _describe 'uninstall flags' sub ;;
        completion) local sh=('zsh' 'bash' 'fish'); _describe 'shells' sh ;;
      esac ;;
    4)
      case ${words[2]} in
        secret) _describe 'profiles' profiles ;;
        setup|import|add)
          if [[ ${words[3]} == --config ]]; then
            _files
          elif [[ ${words[3]} == --scope ]]; then
            local scopes=('clone' 'full'); _describe 'scopes' scopes
          elif [[ ${words[3]} == --extend ]]; then
            _describe 'profiles' profiles
          else
            _describe 'setup options' setup_flags
          fi
          ;;
        clone)
          case ${words[3]} in
            -p|--profile) _describe 'profiles' profiles ;;
          esac
          ;;
        use) local ev=('--eval:Print shell exports for eval'); _describe 'use flags' ev ;;
      esac
      ;;
    5)
      case ${words[2]} in
        use) [[ ${words[4]} == --eval ]] && { local sh=('bash' 'fish'); _describe 'shells' sh; } ;;
        setup|import|add) _describe 'setup options' setup_flags ;;
        secret)
          [[ ${words[3]} =~ ^(set|get|delete)$ ]] && _describe 'profiles' profiles
          ;;
      esac
      ;;
  esac
}
_ctx "$@"
