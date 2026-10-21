function __fish_ctx_profiles
    ctx list 2>/dev/null | awk '/^  [a-z]/ {print $1}'
end

complete -c ctx -f

complete -c ctx -n '__fish_use_subcommand' -l json -d 'JSON output (list, status)'
complete -c ctx -n '__fish_use_subcommand' -l dry-run -d 'Preview without writing files'
complete -c ctx -n '__fish_use_subcommand' -s q -l quiet -d 'Suppress output'
complete -c ctx -n '__fish_use_subcommand' -l no-gum -d 'Plain prompts'
complete -c ctx -n '__fish_use_subcommand' -s v -l version -d 'Print version'
complete -c ctx -n '__fish_use_subcommand' -s h -l help -d 'Show help'

complete -c ctx -n '__fish_use_subcommand' -a 'init setup import add use deactivate list ls status st clone workdir-prompt config secret verify edit remove rm del undo install-hook upgrade uninstall doctor guard run env agent-rules mcp knowledge completion version help'

complete -c ctx -n '__fish_seen_subcommand_from ctx help' -a 'setup' -d 'Wizard and --config reference'
complete -c ctx -n '__fish_seen_subcommand_from ctx guard' -a 'status install uninstall remove check'

complete -c ctx -n '__fish_seen_subcommand_from ctx use' -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx use' -l eval -d 'Print shell exports for eval' -x -a 'fish bash'
complete -c ctx -n '__fish_seen_subcommand_from ctx edit' -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx remove rm del' -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx verify' -a '(__fish_ctx_profiles)'

complete -c ctx -n '__fish_seen_subcommand_from ctx env' -s p -l profile -d 'Profile' -x -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx env' -l secrets -d 'Include secrets'
complete -c ctx -n '__fish_seen_subcommand_from ctx env' -l fish -d 'Fish syntax'
complete -c ctx -n '__fish_seen_subcommand_from ctx run' -s p -l profile -d 'Profile' -x -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx mcp' -a 'install' -d 'Register with Claude Code or Cursor'
complete -c ctx -n '__fish_seen_subcommand_from ctx mcp' -l profile -d 'Serve one profile' -x -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from mcp; and __fish_seen_subcommand_from install' -a 'claude cursor'
complete -c ctx -n '__fish_seen_subcommand_from ctx knowledge' -a 'init'
complete -c ctx -n '__fish_seen_subcommand_from knowledge; and __fish_seen_subcommand_from init' -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx agent-rules' -l write -d 'Write to a file (default AGENTS.md)' -r

complete -c ctx -n '__fish_seen_subcommand_from ctx secret' -a 'set get list delete migrate'
complete -c ctx -n '__fish_seen_subcommand_from ctx secret set' -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx secret get' -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx secret delete' -a '(__fish_ctx_profiles)'

complete -c ctx -n '__fish_seen_subcommand_from ctx config' -a 'show work-root secret-provider prompt-workdir-depth prompt-extra-paths autoswitch-notify export import'
complete -c ctx -n '__fish_seen_subcommand_from ctx upgrade' -a '--check'
complete -c ctx -n '__fish_seen_subcommand_from ctx uninstall' -a '--purge'
complete -c ctx -n '__fish_seen_subcommand_from ctx completion' -a 'zsh bash fish'

complete -c ctx -n '__fish_seen_subcommand_from ctx setup import add' -s y -l yes -d 'Skip final review confirm'
complete -c ctx -n '__fish_seen_subcommand_from ctx setup import add' -s h -l help -d 'Full setup reference'
complete -c ctx -n '__fish_seen_subcommand_from ctx setup import add' -l config -d 'Non-interactive KEY=value file' -r
complete -c ctx -n '__fish_seen_subcommand_from ctx setup import add; and __fish_prev_arg_in --config' -f -a '(__fish_complete_path)'
complete -c ctx -n '__fish_seen_subcommand_from ctx setup import add' -l scope -d 'Profile scope' -x -a 'clone full'
complete -c ctx -n '__fish_seen_subcommand_from ctx setup import add' -l extend -d 'Add to an existing profile' -x -a '(__fish_ctx_profiles)'

complete -c ctx -n '__fish_seen_subcommand_from ctx list ls' -l json -d 'JSON output'
complete -c ctx -n '__fish_seen_subcommand_from ctx status st' -l json -d 'JSON output'

complete -c ctx -n '__fish_seen_subcommand_from ctx clone' -s p -l profile -d 'Profile' -a '(__fish_ctx_profiles)'
