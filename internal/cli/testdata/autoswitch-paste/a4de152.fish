# ctx profile autoswitch — fish (see lib/ctx_autoswitch.bash for behavior)
# Nearest .ctx from $PWD up to git root wins (not repo-root only).
# Sets CTX_PROMPT_SHOW / CTX_PROMPT_WORK_DIR / CTX_PROMPT_PROFILE (see README).

function _ctx_rel_depth_under -a p r
  set p (string trim -r / -- (string replace -r '^~' "$HOME" -- "$p"))
  set r (string trim -r / -- (string replace -r '^~' "$HOME" -- "$r"))
  if test "$p" = "$r"
    echo 0
    return
  end
  if not string match -q "$r/*" "$p"
    echo -1
    return
  end
  set -l x (string replace -- "$r/" "" "$p")
  if test -z "$x"
    echo 0
    return
  end
  if not string match -q '*/*' "$x"
    echo 1
    return
  end
  count (string split / -- $x)
end

function _ctx_profile_autoswitch --on-variable PWD
  command -v ctx >/dev/null 2>/dev/null; or return
  set -l profiles_dir (set -q CTX_DIR; and echo "$CTX_DIR"; or echo "$HOME/.ctx")/profiles
  set -l active_conf (set -q CTX_DIR; and echo "$CTX_DIR"; or echo "$HOME/.ctx")/config
  test -d "$profiles_dir"; or return

  set -l best_pname ""
  set -l best_len 0
  set -l best_wd ""
  for conf in "$profiles_dir"/*.conf
    test -e "$conf"; or continue
    set -l work_dir (bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "$conf")
    set work_dir (string replace -r '^~' "$HOME" -- "$work_dir")
    test -n "$work_dir"; or continue
    set -l plen (string length -- "$work_dir")
    if test (string length -- "$PWD") -ge "$plen"
      set -l pref (string sub -s 1 -l "$plen" -- "$PWD")
      if test "$pref" = "$work_dir"
        set -l nextch ""
        test (string length -- "$PWD") -gt "$plen"; and set nextch (string sub -s (math $plen + 1) -l 1 -- "$PWD")
        if test (string length -- "$PWD") -eq "$plen"; or test "$nextch" = /
          if test "$plen" -gt "$best_len"
            set best_len "$plen"
            set best_pname (basename "$conf" .conf)
            set best_wd "$work_dir"
          end
        end
      end
    end
  end

  set -l path_profile "$best_pname"
  set -l best_work_dir "$best_wd"
  if test -n "$best_pname"; and test -n "$best_work_dir"
    set -l bl (string length -- "$best_work_dir")
    if test (string length -- "$PWD") -gt "$bl"
      set -l ch (string sub -s (math $bl + 1) -l 1 -- "$PWD")
      if test "$ch" = /
        set -l rel (string sub -s (math $bl + 2) -- "$PWD")
        set -l first (printf '%s' "$rel" | cut -d/ -f1)
        if test -n "$first"; and test -f "$profiles_dir/$first.conf"
          set -l wdc (bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "$profiles_dir/$first.conf")
          set wdc (string replace -r '^~' "$HOME" -- "$wdc")
          test -z "$wdc"; and set wdc "$best_work_dir/$first"
          if test -d "$wdc"
            set -l wlen (string length -- "$wdc")
            set -l pfx (string sub -s 1 -l "$wlen" -- "$PWD")
            set -l ok 0
            if test "$PWD" = "$wdc"
              set ok 1
            else if test (string length -- "$PWD") -gt "$wlen"; and test "$pfx" = "$wdc"
              set -l mid (string sub -s (math $wlen + 1) -l 1 -- "$PWD")
              test "$mid" = /; and set ok 1
            end
            if test "$ok" -eq 1; and test "$wlen" -gt "$bl"
              set path_profile "$first"
              set best_work_dir "$wdc"
            end
          end
        end
      end
    end
  end

  if test -n "$path_profile"
    set -l repo_root (git rev-parse --show-toplevel 2>/dev/null; or echo "")
    set repo_root (string trim -r / -- "$repo_root")
    if test -n "$repo_root"
      set -l d (string trim -r / -- "$PWD")
      while true
        if test -f "$d/.ctx"
          set -l ov (grep "^profile=" "$d/.ctx" 2>/dev/null | tail -1 | cut -d= -f2-)
          set ov (string trim -- "$ov")
          if test -n "$ov"; and test -f "$profiles_dir/$ov.conf"
            set path_profile "$ov"
            break
          end
        end
        if test "$d" = "$repo_root"
          break
        end
        set -l parent (command dirname "$d")
        if test "$parent" = "$d"
          break
        end
        set d "$parent"
      end
    end
  end

  set -l current (grep "^active=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2-)
  set -l src (grep "^active_source=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2- | string lower)
  set -l manual_anchor ""
  if test "$src" = manual
    set -l raw (grep "^manual_pwd=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2-)
    if test -n "$raw"
      set manual_anchor (printf '%s' "$raw" | base64 -d 2>/dev/null)
      test -z "$manual_anchor"; and set manual_anchor (printf '%s' "$raw" | base64 -D 2>/dev/null)
      test -z "$manual_anchor"; and set manual_anchor "$raw"
    end
  end

  set -l target "$path_profile"
  if test "$src" = manual; and test -n "$manual_anchor"; and test "$PWD" = "$manual_anchor"
    set target "$current"
  end

  # Prompt hints: only under configured WORK_DIR within depth (default 2), or prompt_extra_paths.
  set -l pmd 2
  set -l pmd_raw (grep "^prompt_workdir_max_depth=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2-)
  set pmd_raw (string trim -- "$pmd_raw")
  if test -n "$pmd_raw"; and string match -qr '^[0-9]+$' -- "$pmd_raw"
    set pmd "$pmd_raw"
  end
  set -l pex (grep "^prompt_extra_paths=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2-)
  set -l display_root ""
  set -l profwd ""
  set -l prompt_show 0
  if test -n "$path_profile"
    set profwd (bash -c 'source "$1" 2>/dev/null || true; printf %s "${WORK_DIR:-}"' _ "$profiles_dir/$path_profile.conf")
    set profwd (string replace -r '^~' "$HOME" -- "$profwd")
    set display_root "$profwd"
    test -z "$display_root"; and test -n "$best_work_dir"; and set display_root "$best_work_dir"
  end
  if test -n "$display_root"
    set -l d (_ctx_rel_depth_under "$PWD" "$display_root" | string trim)
    if test "$d" -ge 0; and test "$d" -le "$pmd"
      set prompt_show 1
    end
  end
  if test "$prompt_show" != 1; and test -n "$pex"
    for piece in (string split ":" -- "$pex")
      test -z "$piece"; and continue
      set piece (string replace -r '^~' "$HOME" -- "$piece")
      set piece (string trim -r / -- "$piece")
      if test -n "$piece"; and begin; test "$PWD" = "$piece"; or string match -q "$piece/*" "$PWD"; end
        set prompt_show 1
        set display_root "$piece"
        break
      end
    end
  end
  if test "$prompt_show" -eq 1
    set -gx CTX_PROMPT_SHOW 1
    set -l pname_show "$path_profile"
    if test -z "$pname_show"
      set pname_show (grep "^active=" "$active_conf" 2>/dev/null | tail -1 | cut -d= -f2-)
    end
    set -gx CTX_PROMPT_PROFILE "$pname_show"
    set -gx CTX_PROMPT_WORK_DIR "$display_root"
  else
    set -gx CTX_PROMPT_SHOW 0
    set -e CTX_PROMPT_PROFILE 2>/dev/null
    set -e CTX_PROMPT_WORK_DIR 2>/dev/null
  end

  set -l _as_key "$PWD|$target"
  if set -q __CTX_AS_STATE; and test "$_as_key" = "$__CTX_AS_STATE"
    return
  end
  set -g __CTX_AS_STATE "$_as_key"

  if test -n "$target"
    set -l _ctx_applied 0
    if test -n "$current"; and test "$current" != "$target"
      echo -e "\033[2m[ctx] ← $current → $target\033[0m" >&2
      env CTX_QUIET=1 ctx deactivate --eval fish 2>/dev/null | source
      if env CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use "$target" 2>/dev/null
        set _ctx_applied 1
      else
        echo -e "\033[2m[ctx] ctx use $target failed — run: ctx use $target\033[0m" >&2
      end
    else if test -z "$current"
      echo -e "\033[2m[ctx] → $target\033[0m" >&2
      if env CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use "$target" 2>/dev/null
        set _ctx_applied 1
      else
        echo -e "\033[2m[ctx] ctx use $target failed — run: ctx use $target\033[0m" >&2
      end
    else
      set _ctx_applied 1
    end
    if test "$_ctx_applied" -eq 1
      set -gx CTX_ACTIVE_PROFILE "$target"
      set -gx CTX_ACTIVATION_TRIGGER auto
    end
  else
    if test -n "$current"
      echo -e "\033[2m[ctx] ← $current\033[0m" >&2
      env CTX_QUIET=1 ctx deactivate --eval fish 2>/dev/null | source
    end
    set -e CTX_ACTIVE_PROFILE 2>/dev/null
    set -e CTX_ACTIVATION_TRIGGER 2>/dev/null
  end
end
_ctx_profile_autoswitch
