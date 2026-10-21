# ctx shell integration for fish: ctx activate fish | source
# Safe to source again (one handler; the legacy autoswitch hook is removed).

# ctx runs as a child process, so `ctx use` / `ctx deactivate` cannot change this shell's
# environment by themselves. This function sources their --eval output; every other
# subcommand goes straight to the binary. Global flags may come before the subcommand.
function ctx --description 'ctx (applies use/deactivate to this shell)'
    set -l pre
    set -l rest $argv
    while set -q rest[1]; and contains -- $rest[1] --quiet -q --no-gum --dry-run --json
        set -a pre $rest[1]
        set -e rest[1]
    end
    if not set -q rest[1]; or not contains -- $rest[1] use deactivate
        command ctx $argv
        return $status
    end
    # --dry-run must not touch the shell; an explicit --eval means the caller sources it.
    for a in $argv
        test "$a" = --; and break
        if contains -- $a --dry-run --eval; or string match -q -- '--eval=*' $a
            command ctx $argv
            return $status
        end
    end
    set -l sub $rest[1]
    set -e rest[1]
    set -l out
    set -l rc 0
    if test "$CTX_CORE" = off; or not test -x @CTX_CORE@
        set out (command ctx $pre $sub --eval fish $rest)
        set rc $status
    else
        # bin/ctx's flag parsing: global flags anywhere before --, as environment.
        set -l args
        set -l dd
        set -l q 0
        set -q CTX_QUIET; and test -n "$CTX_QUIET"; and set q $CTX_QUIET
        set -l e CTX_DRY_RUN=0 CTX_JSON=0
        for a in $pre $rest
            if set -q dd[1]
                set -a args $a
                continue
            end
            switch $a
                case --
                    set dd 1
                    set -a args $a
                case --quiet -q
                    set q 1
                case --no-gum
                    set -a e CTX_NO_GUM=1
                case --json
                    set -a e CTX_JSON=1
                case '*'
                    set -a args $a
            end
        end
        set out (env CTX_QUIET=$q $e @CTX_CORE@ $sub --eval fish $args)
        set rc $status
    end
    printf '%s\n' $out | source
    return $rc
end

# The legacy hook's functions (and their event handlers) and an older _ctx_hook.
functions -e _ctx_profile_autoswitch _ctx_profile_autoswitch_postexec _ctx_hook _ctx_shvar 2>/dev/null

# The autoswitch hook. An unchanged prompt runs builtins only: ctx-core hook-env runs
# when $PWD changed, this shell's stamp is missing, or the config or the profiles dir
# changed since (a `ctx use` in another terminal).
function _ctx_hook --on-variable PWD --on-event fish_postexec
    set -l d "$HOME/.ctx"
    set -q CTX_DIR; and test -n "$CTX_DIR"; and set d "$CTX_DIR"
    if test "$PWD" != "$__CTX_PWD"; or not test -e "$__CTX_STAMP"; or test "$d/config" -nt "$__CTX_STAMP" 2>/dev/null; or test "$d/profiles" -nt "$__CTX_STAMP" 2>/dev/null
        test -x @CTX_CORE@; or return
        # Presence-encoded shell variables: "" when unset, "=value" when set.
        set -l n ""; set -q CTX_AUTOSWITCH_NOTIFY; and set n "=$CTX_AUTOSWITCH_NOTIFY"
        set -l g ""; set -q GH_TOKEN; and set g "=$GH_TOKEN"
        set -l a ""; set -q CTX_ACTIVE_PROFILE; and set a "=$CTX_ACTIVE_PROFILE"
        set -l p ""; set -q _CTX_GH_PROFILE; and set p "=$_CTX_GH_PROFILE"
        env __CTX_STATE="$__CTX_STATE" __CTX_STAMP="$__CTX_STAMP" \
            __CTX_SH_NOTIFY="$n" __CTX_SH_GH_TOKEN="$g" __CTX_SH_ACTIVE="$a" __CTX_SH_GH_PROFILE="$p" \
            @CTX_CORE@ hook-env fish | source
        set -gu __CTX_PWD $PWD
    end
end

_ctx_hook
