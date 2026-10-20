function __fish_ctx_profiles
    ctx list 2>/dev/null | awk '/^  [a-z]/ {print $1}'
end

complete -c ctx -f

complete -c ctx -n '__fish_use_subcommand' -l json -d 'JSON output (list, status)'

complete -c ctx -n '__fish_use_subcommand' -a 'init setup import add use deactivate list status clone workdir-prompt config secret verify edit remove undo install-hook upgrade uninstall doctor guard run env agent-rules mcp knowledge completion version help'

complete -c ctx -n '__fish_seen_subcommand_from ctx help' -a 'setup' -d 'Wizard and --config reference'
complete -c ctx -n '__fish_seen_subcommand_from ctx guard' -a 'status install uninstall'

complete -c ctx -n '__fish_seen_subcommand_from ctx use' -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx use' -l eval -d 'Print shell exports for eval' -x -a 'fish bash'
complete -c ctx -n '__fish_seen_subcommand_from ctx edit' -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx remove' -a '(__fish_ctx_profiles)'
complete -c ctx -n '__fish_seen_subcommand_from ctx verify' -a '(__fish_ctx_profiles)'

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

complete -c ctx -n '__fish_seen_subcommand_from ctx list' -l json -d 'JSON output'
complete -c ctx -n '__fish_seen_subcommand_from ctx status' -l json -d 'JSON output'

complete -c ctx -n '__fish_seen_subcommand_from ctx clone' -s p -l profile -d 'Profile' -a '(__fish_ctx_profiles)'
