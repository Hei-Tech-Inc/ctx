# ctx profile autoswitch — fish (see lib/ctx_autoswitch.bash for behavior)

function _ctx_profile_autoswitch --on-variable PWD
  command -v ctx >/dev/null 2>/dev/null; or return
  set -l profiles_dir (set -q CTX_DIR; and echo "$CTX_DIR"; or echo "$HOME/.ctx")/profiles
  set -l active_conf (set -q CTX_DIR; and echo "$CTX_DIR"; or echo "$HOME/.ctx")/config
  test -d "$profiles_dir"; or return

  set -l best_pname ""
  set -l best_len 0
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
          end
        end
      end
    end
  end

  set -l path_profile "$best_pname"
  if test -n "$path_profile"
    set -l repo_root (git rev-parse --show-toplevel 2>/dev/null; or echo "")
    if test -n "$repo_root"; and test -f "$repo_root/.ctx"
      set -l ov (grep "^profile=" "$repo_root/.ctx" 2>/dev/null | tail -1 | cut -d= -f2-)
      if test -n "$ov"; and test -f "$profiles_dir/$ov.conf"
        set path_profile "$ov"
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

  set -l _as_key "$PWD|$target"
  if set -q __CTX_AS_STATE; and test "$_as_key" = "$__CTX_AS_STATE"
    return
  end
  set -g __CTX_AS_STATE "$_as_key"

  if test -n "$target"
    if test -n "$current"; and test "$current" != "$target"
      echo -e "\033[2m[ctx] ← $current → $target\033[0m" >&2
      env CTX_QUIET=1 ctx deactivate --eval fish 2>/dev/null | source
      env CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use "$target" 2>/dev/null
    else if test -z "$current"
      echo -e "\033[2m[ctx] → $target\033[0m" >&2
      env CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use "$target" 2>/dev/null
    end
    set -gx CTX_ACTIVE_PROFILE "$target"
    set -gx CTX_ACTIVATION_TRIGGER auto
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
