#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../lib/core.sh
source "$ROOT_DIR/lib/core.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

pass() {
  echo "PASS: $*"
}

test_valid_env_keys() {
  is_valid_env_key "AWS_PROFILE" || fail "expected AWS_PROFILE to be valid"
  is_valid_env_key "_TOKEN1" || fail "expected _TOKEN1 to be valid"
  ! is_valid_env_key "1TOKEN" || fail "expected 1TOKEN to be invalid"
  ! is_valid_env_key "BAD-NAME" || fail "expected BAD-NAME to be invalid"
  pass "env key validation"
}

test_timeout_helper() {
  run_with_timeout 1 sleep 2 && fail "timeout should have failed"
  run_with_timeout 2 sleep 1 || fail "short command should complete"
  pass "timeout helper"
}

test_clone_extra_args_drop_url() {
  # Mirrors cmd_clone (non --): git clone <rewritten-url> only gets optional args after URL.
  local rest=(git@github.com:Org/repo.git my-dir)
  local url="${rest[0]}"
  local after=() _i
  for ((_i = 1; _i < ${#rest[@]}; _i++)); do
    after+=("${rest[_i]}")
  done
  [[ "$url" == "git@github.com:Org/repo.git" ]] || fail "url token"
  [[ ${#after[@]} -eq 1 && "${after[0]}" == "my-dir" ]] || fail "after argv: ${after[*]}"
  pass "clone extra args (drop URL)"
}

test_github_clone_url_for_profile() {
  local o
  o="$(github_clone_url_for_profile "acme" "git@github.com:Org/repo.git" "n")"
  [[ "$o" == "git@github-acme:Org/repo.git" ]] || fail "scp-style rewrite: $o"

  o="$(github_clone_url_for_profile "acme" "ssh://git@github.com/Org/repo" "n")"
  [[ "$o" == "git@github-acme:Org/repo.git" ]] || fail "ssh:// rewrite: $o"

  o="$(github_clone_url_for_profile "acme" "https://github.com/Org/repo" "y")"
  [[ "$o" == "git@github-acme:Org/repo.git" ]] || fail "https rewrite: $o"

  o="$(github_clone_url_for_profile "acme" "https://github.com/Org/repo" "n")"
  [[ "$o" == "https://github.com/Org/repo" ]] || fail "https passthrough: $o"

  o="$(github_clone_url_for_profile "acme" "https://example.com/x" "y")"
  [[ "$o" == "https://example.com/x" ]] || fail "non-github passthrough: $o"
  pass "github_clone_url_for_profile"
}

test_parse_ctx_version_from_core_sh_file() {
  local f v
  f="$(mktemp)"
  printf '# stub\nCTX_VERSION="9.8.7"\n' > "$f"
  v="$(parse_ctx_version_from_core_sh_file "$f")"
  rm -f "$f"
  [[ "$v" == "9.8.7" ]] || fail "version parse: expected 9.8.7 got '$v'"

  f="$(mktemp)"
  printf 'CTX_VERSION="1.2.3"\r\n' > "$f"
  v="$(parse_ctx_version_from_core_sh_file "$f")"
  rm -f "$f"
  [[ "$v" == "1.2.3" ]] || fail "CRLF version parse: got '$v'"
  pass "parse_ctx_version_from_core_sh_file"
}

test_secret_file_path() {
  (
    set -euo pipefail
    local ROOT td p
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    td="$(mktemp -d)"
    export CTX_DIR="$td"
    # shellcheck source=../lib/core.sh
    source "$ROOT/lib/core.sh"
    p="$(_secret_file "p1" "MY_KEY")"
    [[ "$p" == "$CTX_DIR/secrets/p1/MY_KEY" ]] || exit 1
    rm -rf "$td"
  ) || fail "secret file path"
  pass "secret file path (_secret_file)"
}

test_secret_provider_resolution() {
  (
    set -euo pipefail
    local ROOT td p
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    td="$(mktemp -d)"
    export CTX_DIR="$td"
    # shellcheck source=../lib/core.sh
    source "$ROOT/lib/core.sh"
    ctx_init_dirs
    p="$(ctx_secret_provider)"
    [[ "$p" == "auto" ]] || exit 1
    echo "secret_provider=file" >> "$CTX_CONFIG"
    p="$(ctx_secret_provider)"
    [[ "$p" == "file" ]] || exit 1
    echo "secret_provider=pass" > "$CTX_CONFIG"
    p="$(ctx_secret_provider)"
    [[ "$p" == "pass" ]] || exit 1
    rm -rf "$td"
  ) || fail "secret provider resolution"
  pass "secret provider resolution"
}

test_ctx_profile_read_work_dir() {
  local f wd
  f="$(mktemp)"
  {
    printf 'PROFILE_NAME=%q\n' "acme"
    printf 'WORK_DIR=%q\n' "/tmp/clients/acme corp"
  } > "$f"
  wd="$(ctx_profile_read_work_dir "$f")"
  rm -f "$f"
  [[ "$wd" == "/tmp/clients/acme corp" ]] || fail "ctx_profile_read_work_dir: got '$wd'"
  pass "ctx_profile_read_work_dir"
}

test_ctx_cli_version() {
  local out want
  want="$CTX_VERSION"
  out="$("$ROOT_DIR/bin/ctx" version 2>&1)" || fail "bin/ctx version exited non-zero"
  [[ "$out" == *"$want"* ]] || fail "ctx version output missing $want (got: $out)"
  pass "ctx CLI (bin/ctx version)"
}

test_ctx_rel_path_depth_under() {
  (
    set -euo pipefail
    local ROOT d
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    # shellcheck source=../lib/core.sh
    source "$ROOT/lib/core.sh"
    d="$(ctx_rel_path_depth_under "/tmp/a" "/tmp/a")"
    [[ "$d" == "0" ]] || exit 1
    d="$(ctx_rel_path_depth_under "/tmp/a/b" "/tmp/a")"
    [[ "$d" == "1" ]] || exit 1
    d="$(ctx_rel_path_depth_under "/tmp/a/b/c" "/tmp/a")"
    [[ "$d" == "2" ]] || exit 1
    d="$(ctx_rel_path_depth_under "/tmp/a/b/c/d" "/tmp/a")"
    [[ "$d" == "3" ]] || exit 1
    d="$(ctx_rel_path_depth_under "/tmp/other" "/tmp/a")"
    [[ "$d" == "-1" ]] || exit 1
  ) || fail "ctx_rel_path_depth_under"
  pass "ctx_rel_path_depth_under"
}

test_ctx_resolve_path_profile() {
  (
    set -euo pipefail
    local ROOT td prof want got _git_tpl
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    td="$(mktemp -d)"
    _git_tpl="$(mktemp -d)"
    mkdir -p "$_git_tpl/hooks"
    export GIT_TEMPLATE_DIR="$_git_tpl"
    prof="$td/profiles"
    mkdir -p "$prof"
    printf 'WORK_DIR=%q\n' "$td/root" >"$prof/short.conf"
    printf 'WORK_DIR=%q\n' "$td/root/client" >"$prof/long.conf"
    # shellcheck source=../lib/core.sh
    source "$ROOT/lib/core.sh"
    got="$(ctx_resolve_path_profile "$td/root/client/app" "$prof")"
    [[ "$got" == "long" ]] || exit 1

    mkdir -p "$td/w/repo/sub"
    git -C "$td/w/repo" init -q
    printf 'profile=short\n' >"$td/w/repo/.ctx"
    printf 'WORK_DIR=%q\n' "$td/w" >"$prof/short.conf"
    printf 'WORK_DIR=%q\n' "$td/w/repo" >"$prof/long.conf"
    got="$(ctx_resolve_path_profile "$td/w/repo/sub" "$prof")"
    [[ "$got" == "short" ]] || exit 1

    mkdir -p "$td/m/repo/deep"
    git -C "$td/m/repo" init -q
    printf 'profile=long\n' >"$td/m/repo/.ctx"
    printf 'profile=short\n' >"$td/m/repo/deep/.ctx"
    printf 'WORK_DIR=%q\n' "$td/m" >"$prof/short.conf"
    printf 'WORK_DIR=%q\n' "$td/m/repo" >"$prof/long.conf"
    got="$(ctx_resolve_path_profile "$td/m/repo/deep" "$prof")"
    [[ "$got" == "short" ]] || exit 1

    rm -f "$prof"/*.conf
    mkdir -p "$td/clients/hubtel/deep"
    printf 'WORK_DIR=%q\n' "$td/clients" >"$prof/deladetech.conf"
    printf 'PROFILE_NAME=hubtel\n' >"$prof/hubtel.conf"
    got="$(ctx_resolve_path_profile "$td/clients/hubtel/deep" "$prof")"
    [[ "$got" == "hubtel" ]] || exit 1

    got="$(ctx_resolve_path_profile "/nonexistent/nope/zz" "$prof")"
    [[ -z "$got" ]] || exit 1

    line="$(ctx_resolve_path_with_root "$td/clients/hubtel/deep" "$prof")"
    got="${line%%$'\t'*}"
    root="${line#*$'\t'}"
    [[ "$got" == "hubtel" ]] || exit 1
    [[ "$root" == "$td/clients/hubtel" ]] || exit 1

    rm -rf "$td" "${_git_tpl:-}"
  ) || fail "ctx_resolve_path_profile"
  pass "ctx_resolve_path_profile (prefix, sibling, .ctx, no match, with_root)"
}

test_cmd_workdir_prompt() {
  (
    set -euo pipefail
    local ROOT td out
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    td="$(mktemp -d)"
    export CTX_DIR="$td"
    mkdir -p "$td/profiles" "$td/clients/hubtel/deep"
    printf 'prompt_workdir_max_depth=5\n' >"$td/config"
    printf 'WORK_DIR=%q\n' "$td/clients" >"$td/profiles/deladetech.conf"
    printf 'PROFILE_NAME=hubtel\n' >"$td/profiles/hubtel.conf"
    cd "$td/clients/hubtel/deep"
    out="$("$ROOT/bin/ctx" workdir-prompt)"
    [[ "$out" == "work_dir='WORK_DIR=$td/clients/hubtel'" ]] || exit 1
    cd /
    out="$("$ROOT/bin/ctx" workdir-prompt)"
    [[ -z "$out" ]] || exit 1
    rm -rf "$td"
  ) || fail "cmd_workdir_prompt"
  pass "workdir-prompt"
}

test_generate_mise_toml_matches_fixture() {
  (
    set -euo pipefail
    local ROOT td want got
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    td="$(mktemp -d)"
    mkdir -p "$td/w"
    # shellcheck source=../lib/core.sh
    source "$ROOT/lib/core.sh"
    generate_mise_toml "snap" "$td/w" "Test User" "test@example.com" "" "" "" "" "" ""
    want="$ROOT/test/fixtures/mise_generated_minimal.toml"
    got="$td/w/mise.toml"
    cmp -s "$want" "$got" || {
      diff -u "$want" "$got" >&2 || true
      exit 1
    }
    rm -rf "$td"
  ) || fail "generate_mise_toml golden mismatch"
  pass "generate_mise_toml matches fixture"
}

test_autoswitch_notify_default_off() {
  (
    set -euo pipefail
    local ROOT td
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    td="$(mktemp -d)"
    export CTX_DIR="$td"
    mkdir -p "$td"
    # shellcheck source=../lib/core.sh
    source "$ROOT/lib/core.sh"
    unset CTX_AUTOSWITCH_NOTIFY
    [[ "$(ctx_config_autoswitch_notify)" == "0" ]] || exit 1
    echo "autoswitch_notify=on" >"$td/config"
    [[ "$(ctx_config_autoswitch_notify)" == "1" ]] || exit 1
    echo "autoswitch_notify=off" >"$td/config"
    [[ "$(ctx_config_autoswitch_notify)" == "0" ]] || exit 1
    CTX_AUTOSWITCH_NOTIFY=1
    export CTX_AUTOSWITCH_NOTIFY
    [[ "$(ctx_config_autoswitch_notify)" == "1" ]] || exit 1
    rm -rf "$td"
  ) || fail "autoswitch_notify default/config/env"
  pass "autoswitch_notify default off"
}

test_base64_decode_gnu_style() {
  (
    set -euo pipefail
    local td
    td="$(mktemp -d)"
    # GNU-style: -d prints the text, then exits 1 on stray padding; -D is unknown.
    mkdir -p "$td/bin"
    cat >"$td/bin/base64" <<'STUB'
#!/bin/sh
case "$1" in
  -d|--decode) printf '/tmp/x'; exit 1 ;;
  *) exit 1 ;;
esac
STUB
    chmod +x "$td/bin/base64"
    [[ "$(PATH="$td/bin:$PATH" ctx_base64_decode 'L3RtcC94==')" == /tmp/x ]] || { echo "decoded twice or not at all"; exit 1; }
    export CTX_DIR="$td/ctx" CTX_CONFIG="$td/ctx/config"
    mkdir -p "$CTX_DIR"
    printf 'manual_pwd=L3RtcC94\r\n' >"$CTX_CONFIG"
    [[ "$(PATH="$td/bin:$PATH" ctx_activation_get_manual_pwd)" == /tmp/x ]] || { echo "manual_pwd with CRLF"; exit 1; }
    rm -rf "$td"
  ) || fail "base64 decode (GNU style)"
  pass "manual_pwd decodes once with GNU base64 and CRLF config"
}

test_config_upsert_literal_values() {
  (
    set -euo pipefail
    local ROOT td got
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    td="$(mktemp -d)"
    export CTX_DIR="$td"
    # shellcheck source=../lib/core.sh
    source "$ROOT/lib/core.sh"
    printf 'active=x\nprompt_extra_paths=old\nprompt_extra_paths=dup\nwork_root=/w\n' >"$CTX_CONFIG"
    _ctx_config_upsert_line prompt_extra_paths '/a|b:/c&d:/e\f'
    [[ "$(grep -c '^prompt_extra_paths=' "$CTX_CONFIG")" == "1" ]] || exit 1
    got="$(grep '^prompt_extra_paths=' "$CTX_CONFIG")"
    [[ "$got" == 'prompt_extra_paths=/a|b:/c&d:/e\f' ]] || { echo "got: $got" >&2; exit 1; }
    grep -qx 'active=x' "$CTX_CONFIG" || exit 1
    grep -qx 'work_root=/w' "$CTX_CONFIG" || exit 1
    set_active_profile 'acme' 2>/dev/null
    grep -qx 'active=acme' "$CTX_CONFIG" || exit 1
    ! _ctx_config_upsert_line work_root $'two\nlines' 2>/dev/null || exit 1
    grep -qx 'work_root=/w' "$CTX_CONFIG" || exit 1
    rm -rf "$td"
  ) || fail "_ctx_config_upsert_line literal values"
  pass "config upsert writes values literally"
}

test_autoswitch_hook_resolution() {
  (
    set -euo pipefail
    local ROOT td used
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/ctx"
    mkdir -p "$CTX_DIR/profiles" "$HOME/b/sub" "$td/w/a/deep" "$td/w/c d" "$td/w/clients/acme" "$td/w/mono/.git" "$td/w/mono/svc"
    : >"$CTX_DIR/config"
    printf 'WORK_DIR=%s\nGITHUB_USER=alice\n' "$td/w/a" >"$CTX_DIR/profiles/a.conf"
    # shellcheck disable=SC2016
    printf 'WORK_DIR="$HOME/b"\n' >"$CTX_DIR/profiles/b.conf"
    printf 'WORK_DIR=%q\n' "$td/w/c d" >"$CTX_DIR/profiles/c.conf"
    printf 'WORK_DIR=%s\n' "$td/w/clients" >"$CTX_DIR/profiles/broad.conf"
    printf 'PROFILE_NAME=acme\n' >"$CTX_DIR/profiles/acme.conf"
    printf 'WORK_DIR=%s\nGITHUB_USER="bob"\n' "$td/w/mono" >"$CTX_DIR/profiles/mono.conf"
    printf 'profile= a \r\n' >"$td/w/mono/svc/.ctx"
    used="$td/used"
    unset GH_TOKEN _CTX_GH_PROFILE
    # Stub binary on PATH: the hook runs `command ctx use --eval bash <name>`, never a shell function.
    mkdir -p "$td/bin"
    cat >"$td/bin/ctx" <<'STUB'
#!/bin/sh
[ "$1" = use ] || exit 0
for a; do last=$a; done
printf '%s\n' "$last" >>"$CTX_TEST_USED"
STUB
    chmod +x "$td/bin/ctx"
    export PATH="$td/bin:$PATH" CTX_TEST_USED="$used"
    gh() { [[ "$1 $2 $3" == "auth token -u" ]] && printf 'tok-%s' "$4"; }
    # shellcheck source=../lib/ctx_autoswitch.bash
    source "$ROOT/lib/ctx_autoswitch.bash"
    _expect() {
      cd "$1"
      : >"$used"
      _ctx_profile_autoswitch 2>"$td/stderr"
      [[ "$(cat "$used")" == "$2" ]] || { echo "in $1: used='$(cat "$used")' want '$2'" >&2; exit 1; }
      [[ ! -s "$td/stderr" ]] || { echo "unexpected stderr: $(cat "$td/stderr")" >&2; exit 1; }
    }
    _expect "$td/w/a/deep" a
    [[ "$CTX_PROMPT_SHOW" == 1 && "$CTX_PROMPT_PROFILE" == a && "$CTX_PROMPT_WORK_DIR" == "$td/w/a" ]] || exit 1
    [[ "${GH_TOKEN:-}" == tok-alice ]] || { echo "GH_TOKEN in a: '${GH_TOKEN:-}'" >&2; exit 1; }
    _expect "$HOME/b/sub" b
    [[ -z "${GH_TOKEN:-}" && -z "${_CTX_GH_PROFILE:-}" ]] || { echo "GH_TOKEN kept in b" >&2; exit 1; }
    _expect "$td/w/c d" c
    _expect "$td/w/clients/acme" acme
    _expect "$td/w/mono/svc" a
    [[ "${GH_TOKEN:-}" == tok-alice ]] || exit 1
    _expect "$td/w/mono" mono
    [[ "${GH_TOKEN:-}" == tok-bob ]] || { echo "GH_TOKEN in mono: '${GH_TOKEN:-}'" >&2; exit 1; }
    cd "$td"
    _ctx_profile_autoswitch
    [[ -z "${GH_TOKEN:-}" ]] || { echo "GH_TOKEN kept outside profiles" >&2; exit 1; }
    export GH_TOKEN=mine
    _expect "$td/w/a/deep" a
    [[ "$GH_TOKEN" == mine ]] || { echo "user GH_TOKEN overwritten" >&2; exit 1; }
    rm -rf "$td"
  ) || fail "autoswitch hook resolution"
  pass "autoswitch hook resolution (plain, \$HOME, escaped, sibling, .ctx, per-shell GH_TOKEN)"
}

test_trailing_slash_work_dir() {
  (
    set -euo pipefail
    local td ROOT mode out rc used
    ROOT="$ROOT_DIR"
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" GIT_CONFIG_NOSYSTEM=1
    unset GIT_COMMITTER_EMAIL CTX_GUARD
    mkdir -p "$CTX_DIR/profiles" "$td/w/acme/r"
    : >"$CTX_DIR/config"
    printf 'GIT_EMAIL=dev@acme.com\nWORK_DIR="%s/"\n' "$td/w/acme" >"$CTX_DIR/profiles/acme.conf"
    git -C "$td/w/acme/r" init -q
    # Library resolvers
    [[ "$(ctx_profile_read_work_dir "$CTX_DIR/profiles/acme.conf")" == "$td/w/acme" ]] || { echo "read_work_dir keeps slash"; exit 1; }
    [[ "$(ctx_resolve_path_with_root "$td/w/acme/r" "$CTX_DIR/profiles")" == "acme"$'\t'"$td/w/acme" ]] || { echo "resolve keeps slash"; exit 1; }
    printf 'WORK_DIR=/\n' >"$td/root.conf"
    [[ "$(ctx_profile_read_work_dir "$td/root.conf")" == "/" ]] || { echo "root WORK_DIR trimmed"; exit 1; }
    # Guard in both modes
    for mode in off default; do
      rc=0
      if [[ $mode == off ]]; then
        out="$(cd "$td/w/acme/r" && GIT_AUTHOR_EMAIL=x@y.z CTX_CORE=off "$ROOT_DIR/bin/ctx" guard check pre-commit 2>&1)" || rc=$?
      else
        out="$(cd "$td/w/acme/r" && env -u CTX_CORE GIT_AUTHOR_EMAIL=x@y.z "$ROOT_DIR/bin/ctx" guard check pre-commit 2>&1)" || rc=$?
      fi
      [[ $rc -ne 0 && "$out" == *"commit blocked"* ]] || { echo "guard ($mode): rc=$rc out=$out"; exit 1; }
    done
    # Autoswitch hook
    used="$td/used"
    # Stub binary on PATH: the hook runs `command ctx use --eval bash <name>`, never a shell function.
    mkdir -p "$td/bin"
    cat >"$td/bin/ctx" <<'STUB'
#!/bin/sh
[ "$1" = use ] || exit 0
for a; do last=$a; done
printf '%s\n' "$last" >>"$CTX_TEST_USED"
STUB
    chmod +x "$td/bin/ctx"
    export PATH="$td/bin:$PATH" CTX_TEST_USED="$used"
    # shellcheck source=../lib/ctx_autoswitch.bash
    source "$ROOT/lib/ctx_autoswitch.bash"
    cd "$td/w/acme/r"
    : >"$used"
    _ctx_profile_autoswitch 2>/dev/null
    [[ "$(cat "$used")" == acme ]] || { echo "autoswitch: '$(cat "$used")'"; exit 1; }
    rm -rf "$td"
  ) || fail "trailing-slash WORK_DIR"
  pass "trailing-slash WORK_DIR matches in Bash and Go (resolver, guard, autoswitch)"
}

test_setup_scope_clone_batch() {
  (
    set -euo pipefail
    local ROOT td home cfg
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    td="$(mktemp -d)"
    home="$td/home"
    mkdir -p "$home/.ssh"
    chmod 700 "$home/.ssh"
    ssh-keygen -t ed25519 -f "$home/.ssh/id_testclone" -N "" -q
    export HOME="$home"
    export CTX_DIR="$home/.ctx"
    cfg="$td/clone.conf"
    cat >"$cfg" <<EOF
SETUP_SCOPE=clone
PROFILE_NAME=cloneonly
SSH_MODE=existing
SSH_KEY_PATH=$home/.ssh/id_testclone
AUTO_CONFIRM=true
EOF
    "$ROOT/bin/ctx" setup --config "$cfg" --yes >/dev/null
    [[ -f "$CTX_DIR/profiles/cloneonly.conf" ]] || exit 1
    grep -q '^Host github-cloneonly$' "$home/.ssh/ctx_config" || exit 1
    # shellcheck source=../lib/core.sh
    source "$ROOT/lib/core.sh"
    out="$(github_clone_url_for_profile cloneonly 'git@github.com:Org/repo.git' n)"
    [[ "$out" == 'git@github-cloneonly:Org/repo.git' ]] || exit 1
    rm -rf "$td"
  ) || fail "setup SETUP_SCOPE=clone batch"
  pass "setup scope clone (batch)"
}

test_ctx_json_list_and_status() {
  (
    set -euo pipefail
    local td out
    td="$(mktemp -d)"
    export CTX_DIR="$td"
    mkdir -p "$td/profiles"
    {
      printf 'PROFILE_NAME=%q\n' "alpha"
      printf 'GIT_NAME=%q\n' "A User"
      printf 'GIT_EMAIL=%q\n' "a@example.com"
      printf 'WORK_DIR=%q\n' "$td/w"
    } >"$td/profiles/alpha.conf"
    : >"$td/config"
    out="$("$ROOT_DIR/bin/ctx" list --json 2>&1)" || exit 1
    echo "$out" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["command"]=="list" and "version" in d and len(d["profiles"])==1 and d["profiles"][0]["name"]=="alpha" and d["profiles"][0]["active"] is False' || exit 1
    out="$("$ROOT_DIR/bin/ctx" --json status 2>&1)" || exit 1
    echo "$out" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["command"]=="status" and d["active"] is None' || exit 1
    echo "active=alpha" >>"$td/config"
    out="$("$ROOT_DIR/bin/ctx" --json status 2>&1)" || exit 1
    echo "$out" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["command"]=="status" and d["active"]=="alpha" and d["git_email"]=="a@example.com"' || exit 1
    rm -rf "$td"
  ) || fail "ctx --json list / status"
  pass "ctx JSON list and status"
}

test_guard_git_hooks() {
  (
    set -euo pipefail
    local td hooks repo out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" GIT_CONFIG_NOSYSTEM=1
    export PATH="$ROOT_DIR/bin:$PATH"
    unset GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL CTX_GUARD
    mkdir -p "$CTX_DIR/profiles" "$td/w/acme/r" "$td/w/other" "$td/elsewhere"
    printf 'GIT_EMAIL=dev@acme.com\nWORK_DIR=%s\n' "$td/w/acme" >"$CTX_DIR/profiles/acme.conf"
    printf 'WORK_DIR=%s\n' "$td/w/other" >"$CTX_DIR/profiles/other.conf"
    : >"$CTX_DIR/config"
    git config --global user.name Tester
    git config --global user.email me@personal.com

    ctx guard install >/dev/null
    hooks="$(git config --global --get core.hooksPath)"
    [[ "$hooks" == "$CTX_DIR/git-hooks" && -x "$hooks/pre-commit" && -x "$hooks/commit-msg" ]] \
      || { echo "install: hooksPath=$hooks"; exit 1; }

    repo="$td/w/acme/r"
    git -C "$repo" init -q
    if out="$(git -C "$repo" commit -q --allow-empty -m wrong 2>&1)"; then
      echo "wrong-email commit was allowed"; exit 1
    fi
    [[ "$out" == *"commit blocked"*"dev@acme.com"*"git config user.email"* ]] || { echo "message: $out"; exit 1; }
    git -C "$repo" config user.email other@corp.com
    out="$(git -C "$repo" commit -q --allow-empty -m wrong 2>&1)" && { echo "local override allowed"; exit 1; }
    [[ "$out" == *"--unset user.email"* ]] || { echo "local hint: $out"; exit 1; }
    CTX_GUARD=off git -C "$repo" commit -q --allow-empty -m bypass || { echo "CTX_GUARD=off"; exit 1; }

    git -C "$repo" config user.email DEV@acme.com
    printf '#!/bin/sh\ntouch "%s/chained"\n' "$td" >"$repo/.git/hooks/pre-commit"
    chmod +x "$repo/.git/hooks/pre-commit"
    git -C "$repo" commit -q --allow-empty -m ok || { echo "matching email blocked"; exit 1; }
    [[ -f "$td/chained" ]] || { echo "repo hook not chained"; exit 1; }

    (cd "$repo" && ctx guard check pre-push origin git@github-acme:org/r.git) || { echo "own alias blocked"; exit 1; }
    (cd "$repo" && ctx guard check pre-push origin git@github-other:org/r.git 2>/dev/null) && { echo "other alias allowed"; exit 1; }
    (cd "$repo" && "$hooks/pre-push" origin ssh://git@github-other/org/r.git </dev/null 2>/dev/null) \
      && { echo "pre-push stub did not block"; exit 1; }

    git -C "$td/elsewhere" init -q
    git -C "$td/elsewhere" commit -q --allow-empty -m free || { echo "repo outside profiles blocked"; exit 1; }

    ctx guard uninstall >/dev/null
    [[ -z "$(git config --global --get core.hooksPath || true)" && ! -e "$hooks" ]] || { echo "uninstall"; exit 1; }
    git config --global core.hooksPath "$td/mine"
    ctx guard install >/dev/null 2>&1 && { echo "overwrote foreign hooksPath"; exit 1; }
    [[ "$(git config --global --get core.hooksPath)" == "$td/mine" ]] || exit 1
    rm -rf "$td"
  ) || fail "ctx guard git hooks"
  pass "ctx guard (commit email, push alias, chaining, bypass, uninstall)"
}

test_git_identity_github_rewrite() {
  (
    set -euo pipefail
    local td id
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" GIT_CONFIG_NOSYSTEM=1
    # shellcheck source=../lib/core.sh
    source "$ROOT_DIR/lib/core.sh"
    CTX_SSH_CONFIG="$HOME/.ssh/ctx_config"
    mkdir -p "$HOME/.ssh" "$HOME/.config/git" "$td/w/acme/r" "$td/out/r"
    id="$HOME/.config/git/ctx-acme"
    git config --file "$id" user.email dev@acme.com
    printf '[includeIf "gitdir:%s/"]\n\tpath = %s\n' "$td/w/acme" "$id" >"$HOME/.gitconfig"

    ctx_git_identity_set_rewrite acme
    [[ -z "$(git config --file "$id" --get-all url.git@github-acme:.insteadOf || true)" ]] \
      || { echo "rewrite added without SSH host"; exit 1; }

    printf 'Host github-acme\n  HostName github.com\n' >"$CTX_SSH_CONFIG"
    ctx_git_identity_set_rewrite acme
    ctx_git_identity_set_rewrite acme
    [[ "$(git config --file "$id" --get-all url.git@github-acme:.insteadOf | wc -l | tr -d ' ')" == 2 ]] \
      || { echo "rewrite not idempotent"; exit 1; }

    git -C "$td/w/acme/r" init -q
    git -C "$td/w/acme/r" remote add origin git@github.com:org/r.git
    [[ "$(git -C "$td/w/acme/r" ls-remote --get-url origin)" == "git@github-acme:org/r.git" ]] \
      || { echo "inside: $(git -C "$td/w/acme/r" ls-remote --get-url origin)"; exit 1; }
    git -C "$td/w/acme/r" remote set-url origin ssh://git@github.com/org/r.git
    [[ "$(git -C "$td/w/acme/r" ls-remote --get-url origin)" == "git@github-acme:org/r.git" ]] \
      || { echo "ssh:// form not rewritten"; exit 1; }

    git -C "$td/out/r" init -q
    git -C "$td/out/r" remote add origin git@github.com:org/r.git
    [[ "$(git -C "$td/out/r" ls-remote --get-url origin)" == "git@github.com:org/r.git" ]] \
      || { echo "rewrite leaked outside WORK_DIR"; exit 1; }
    rm -rf "$td"
  ) || fail "git identity github.com rewrite"
  pass "git identity rewrites github.com to github-<profile> inside WORK_DIR only"
}

test_ctx_run_env_agent_rules() {
  (
    set -euo pipefail
    local td out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" PATH="$td/bin:$ROOT_DIR/bin:$PATH"
    unset GH_TOKEN AWS_PROFILE GIT_AUTHOR_EMAIL
    mkdir -p "$CTX_DIR/profiles" "$CTX_DIR/secrets/acme" "$td/bin" "$HOME/.ssh" "$td/w/acme/r"
    : >"$HOME/.ssh/id_acme"
    printf '#!/bin/sh\n[ "$1 $2 $3" = "auth token -u" ] && printf "tok-%%s" "$4"\n' >"$td/bin/gh"
    chmod +x "$td/bin/gh"
    {
      printf 'GIT_NAME=%q\nGIT_EMAIL=dev@acme.com\nWORK_DIR=%s\n' "Dev One" "$td/w/acme"
      printf 'GITHUB_USER=acme-dev\nAWS_PROFILE_NAME=acme-prod\nSSH_KEY_PATH=%s\n' "$HOME/.ssh/id_acme"
      printf 'SECRET_KEYS=API_KEY\nEXTRA_ENVS=%q\n' $'FOO=bar baz\n'
    } >"$CTX_DIR/profiles/acme.conf"
    printf 'secret_provider=file\n' >"$CTX_DIR/config"
    printf 's3cret' >"$CTX_DIR/secrets/acme/API_KEY"
    cd "$td/w/acme/r"

    out="$(ctx run -- env)"
    for want in CTX_ACTIVE_PROFILE=acme "GIT_AUTHOR_NAME=Dev One" GIT_COMMITTER_EMAIL=dev@acme.com \
      GH_TOKEN=tok-acme-dev AWS_PROFILE=acme-prod "FOO=bar baz" API_KEY=s3cret; do
      grep -qxF "$want" <<<"$out" || { echo "run env missing: $want"; exit 1; }
    done
    grep -q '^GIT_SSH_COMMAND=ssh -i .*id_acme -o IdentitiesOnly=yes$' <<<"$out" || { echo "GIT_SSH_COMMAND"; exit 1; }
    [[ "$(ctx run -- printf '%s,' --json -q --dry-run)" == "--json,-q,--dry-run," ]] || { echo "flags not passed through"; exit 1; }
    (cd "$td" && ctx run -p acme -- sh -c 'printf %s "$AWS_PROFILE"' | grep -qx acme-prod) || { echo "-p outside folder"; exit 1; }

    printf '#!/bin/sh\necho rel-ran\n' >reltool && chmod +x reltool
    [[ "$(PATH=".:$PATH" ctx run -- reltool)" == rel-ran ]] || { echo "run: relative PATH entry"; exit 1; }
    printf 'echo ns-ran "$1"\n' >ns.sh && chmod +x ns.sh
    [[ "$(ctx run -- ./ns.sh a)" == "ns-ran a" ]] || { echo "run: script without shebang"; exit 1; }
    rm -f reltool ns.sh

    out="$(ctx env)"
    grep -q API_KEY <<<"$out" && { echo "env printed secrets without --secrets"; exit 1; }
    (eval "$out"; [[ "$GIT_AUTHOR_NAME" == "Dev One" && "$FOO" == "bar baz" ]]) || { echo "env eval"; exit 1; }
    grep -qxF "set -gx GIT_AUTHOR_NAME 'Dev One'" <<<"$(ctx env --fish)" || { echo "env --fish"; exit 1; }
    grep -q '^export API_KEY=s3cret$' <<<"$(ctx env --secrets)" || { echo "env --secrets"; exit 1; }

    printf '# Project\n\nKeep me.\n' >"$td/w/acme/r/AGENTS.md"
    git -C "$td/w/acme/r" init -q
    ctx agent-rules --write >/dev/null
    ctx agent-rules --write >/dev/null
    [[ "$(grep -c 'BEGIN ctx agent rules' AGENTS.md)" == 1 ]] || { echo "agent rules duplicated"; exit 1; }
    grep -q 'Keep me.' AGENTS.md && grep -q 'ctx run -- <command>' AGENTS.md || { echo "agent rules content"; exit 1; }
    rm -rf "$td"
  ) || fail "ctx run / env / agent-rules"
  pass "ctx run, env (bash, fish, secrets opt-in) and agent-rules"
}

test_setup_commit_signing() {
  (
    set -euo pipefail
    local td id cfg
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" GIT_CONFIG_NOSYSTEM=1
    mkdir -p "$HOME/.ssh" "$td/w/acme/r"
    chmod 700 "$HOME/.ssh"
    ssh-keygen -t ed25519 -f "$HOME/.ssh/id_acme" -N "" -q
    cfg="$td/s.conf"
    {
      printf 'PROFILE_NAME=acme\nGIT_NAME=Dev\nGIT_EMAIL=dev@acme.com\nWORK_DIR=%s\n' "$td/w/acme"
      printf 'IMPORT_MODE=manual\nSSH_MODE=existing\nSSH_KEY_PATH=%s\n' "$HOME/.ssh/id_acme"
      printf 'SIGNING_FORMAT=ssh\nAUTO_CONFIRM=true\n'
    } >"$cfg"
    "$ROOT_DIR/bin/ctx" --no-gum setup --config "$cfg" --yes >/dev/null 2>&1 || { echo "setup failed"; exit 1; }
    id="$HOME/.config/git/ctx-acme"
    [[ "$(git config --file "$id" gpg.format)" == ssh \
      && "$(git config --file "$id" user.signingkey)" == "$HOME/.ssh/id_acme" \
      && "$(git config --file "$id" commit.gpgsign)" == true ]] || { echo "identity file: $(cat "$id")"; exit 1; }
    grep -q '^SIGNING_FORMAT=ssh$' "$CTX_DIR/profiles/acme.conf" || { echo "profile conf"; exit 1; }

    git -C "$td/w/acme/r" init -q
    git -C "$td/w/acme/r" commit -q --allow-empty -m signed || { echo "signed commit failed"; exit 1; }
    git -C "$td/w/acme/r" cat-file -p HEAD | grep -q '^gpgsig -----BEGIN SSH SIGNATURE' || { echo "commit not signed"; exit 1; }

    sed -i.bak 's/^SIGNING_FORMAT=ssh$/SIGNING_FORMAT=gpg/' "$cfg"
    printf 'OVERWRITE=true\n' >>"$cfg"
    "$ROOT_DIR/bin/ctx" --no-gum setup --config "$cfg" --yes >/dev/null 2>&1 && { echo "gpg without key accepted"; exit 1; }
    sed -i.bak 's/^SIGNING_FORMAT=gpg$/SIGNING_FORMAT=none/' "$cfg"
    "$ROOT_DIR/bin/ctx" --no-gum setup --config "$cfg" --yes >/dev/null 2>&1 || { echo "re-setup failed"; exit 1; }
    [[ -z "$(git config --file "$id" --get commit.gpgsign || true)" ]] || { echo "signing not removed"; exit 1; }
    rm -rf "$td"
  ) || fail "setup commit signing"
  pass "setup commit signing (ssh signs commits, gpg needs a key, none removes it)"
}

test_use_keeps_user_agent_keys() {
  (
    set -euo pipefail
    local td k
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" PATH="$ROOT_DIR/bin:$PATH" CTX_QUIET=1
    mkdir -p "$CTX_DIR/profiles" "$HOME/.ssh"
    eval "$(ssh-agent -s -a "/tmp/ctx-agent.$$")" >/dev/null
    trap 'ssh-agent -k >/dev/null 2>&1; rm -rf "$td" "/tmp/ctx-agent.$$"' EXIT
    for k in own acme beta; do
      ssh-keygen -t ed25519 -f "$HOME/.ssh/id_$k" -N "" -q
      mkdir -p "$td/w/$k"
      printf 'GIT_NAME=Dev\nGIT_EMAIL=dev@%s.com\nWORK_DIR=%s\nSSH_KEY_PATH=%s\n' \
        "$k" "$td/w/$k" "$HOME/.ssh/id_$k" >"$CTX_DIR/profiles/$k.conf"
    done
    in_agent() { ssh-add -l 2>/dev/null | grep -qF "$(ssh-keygen -lf "$HOME/.ssh/id_$1.pub" | awk '{print $2}')"; }
    ssh-add -q "$HOME/.ssh/id_own" 2>/dev/null

    ctx use acme >/dev/null 2>&1
    in_agent own && in_agent acme || { echo "use acme: $(ssh-add -l)"; exit 1; }
    ctx use beta >/dev/null 2>&1
    in_agent own && in_agent beta && ! in_agent acme || { echo "use beta: $(ssh-add -l)"; exit 1; }
    ctx deactivate >/dev/null 2>&1
    in_agent own && ! in_agent beta || { echo "deactivate: $(ssh-add -l)"; exit 1; }
    ctx use own >/dev/null 2>&1
    ctx deactivate >/dev/null 2>&1
    in_agent own || { echo "removed the user's own key"; exit 1; }
  ) || fail "ctx use keeps user agent keys"
  pass "ctx use adds the profile SSH key without flushing the agent; deactivate removes only ctx's key"
}

test_core_forward_fallback() {
  (
    set -euo pipefail
    local td out want rc
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    export CTX_DIR="$td/ctx" HOME="$td/home"
    unset CTX_CORE # the suite may run with CTX_CORE=off; this test sets it where needed
    mkdir -p "$CTX_DIR/profiles" "$HOME"
    printf 'PROFILE_NAME=alpha\nGIT_NAME=A\nGIT_EMAIL=a@example.com\n' >"$CTX_DIR/profiles/alpha.conf"
    : >"$CTX_DIR/config"
    want="$(CTX_CORE=off "$ROOT_DIR/bin/ctx" --json list)"
    [[ "$want" == *'"command"'*'list'* ]] || { echo "bash json: $want"; exit 1; }
    # implements exits 1: Bash runs, silently
    cat >"$td/fakeno" <<EOS
#!/usr/bin/env bash
echo "\$*" >>"$td/calls"
exit 1
EOS
    chmod +x "$td/fakeno"
    out="$(CTX_CORE_BIN="$td/fakeno" "$ROOT_DIR/bin/ctx" --json list 2>"$td/err")"
    [[ "$out" == "$want" ]] || { echo "fallback: $out"; exit 1; }
    [[ ! -s "$td/err" ]] || { echo "fallback stderr: $(cat "$td/err")"; exit 1; }
    [[ "$(cat "$td/calls")" == "implements list --json" ]] || { echo "calls: $(cat "$td/calls")"; exit 1; }
    # CTX_CORE=off: Bash JSON and the fake is never called
    rm -f "$td/calls"
    out="$(CTX_CORE=off CTX_CORE_BIN="$td/fakeno" "$ROOT_DIR/bin/ctx" --json list)"
    [[ "$out" == "$want" ]] || { echo "off: $out"; exit 1; }
    [[ ! -e "$td/calls" ]] || { echo "CTX_CORE=off still called ctx-core"; exit 1; }
    # Non-executable CTX_CORE_BIN: Bash
    printf '#!/usr/bin/env bash\necho GO\n' >"$td/noexec"
    chmod -x "$td/noexec"
    out="$(CTX_CORE_BIN="$td/noexec" "$ROOT_DIR/bin/ctx" --json list 2>/dev/null)"
    [[ "$out" == "$want" ]] || { echo "noexec: $out"; exit 1; }
    # implements exits 0: ctx-core runs with the exported env
    cat >"$td/fakego" <<'EOS'
#!/usr/bin/env bash
[[ "$1" == implements ]] && exit 0
echo "GO $*"
echo "env $CTX_VERSION|$CTX_DIR|$CTX_BIN_PATH" >&2
EOS
    chmod +x "$td/fakego"
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" --json list 2>"$td/err")"
    [[ "$out" == "GO list --json" ]] || { echo "go list: $out"; exit 1; }
    [[ "$(cat "$td/err")" == "env "*"|$CTX_DIR|$ROOT_DIR/bin/ctx" ]] || { echo "env: $(cat "$td/err")"; exit 1; }
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" guard check pre-commit 2>/dev/null </dev/null)"
    [[ "$out" == "GO guard check pre-commit" ]] || { echo "go guard: $out"; exit 1; }
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" env -p alpha 2>/dev/null)"
    [[ "$out" == "GO env -p alpha" ]] || { echo "go env: $out"; exit 1; }
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" deactivate --eval bash 2>/dev/null)"
    [[ "$out" == "GO deactivate --eval bash" ]] || { echo "go deactivate: $out"; exit 1; }
    # deactivate falls back to Bash when ctx-core does not implement it
    rm -f "$td/calls"
    out="$(CTX_CORE_BIN="$td/fakeno" "$ROOT_DIR/bin/ctx" deactivate --eval bash 2>"$td/err")"
    [[ -z "$out" && "$(cat "$td/err")" == "No active profile." ]] || { echo "deactivate fallback: $out / $(cat "$td/err")"; exit 1; }
    [[ "$(cat "$td/calls")" == "implements deactivate --eval bash" ]] || { echo "deactivate calls: $(cat "$td/calls")"; exit 1; }
    # setup and its aliases forward as setup, with every argument
    rm -f "$td/calls"
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" --dry-run import --config "$td/s conf" -y 2>/dev/null)"
    [[ "$out" == "GO setup --config $td/s conf -y" ]] || { echo "go setup: $out"; exit 1; }
    CTX_CORE_BIN="$td/fakeno" "$ROOT_DIR/bin/ctx" --no-gum add --config "$td/missing" --yes >/dev/null 2>&1 || true
    [[ "$(head -1 "$td/calls")" == "implements setup --config $td/missing --yes" ]] || { echo "setup calls: $(cat "$td/calls")"; exit 1; }
    # The wizard and help forward too (bare setup, --scope, --help)
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" setup </dev/null 2>/dev/null)"
    [[ "$out" == "GO setup" ]] || { echo "go wizard: $out"; exit 1; }
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" add --scope full --help 2>/dev/null)"
    [[ "$out" == "GO setup --scope full --help" ]] || { echo "go help: $out"; exit 1; }
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" setup --extend acme -y </dev/null 2>/dev/null)"
    [[ "$out" == "GO setup --extend acme -y" ]] || { echo "go extend: $out"; exit 1; }
    # The real ctx-core takes every setup shape, --extend NAME included
    if [[ -x "$ROOT_DIR/bin/ctx-core" ]]; then
      local shape
      for shape in "" "--scope full" "--scope clone" "--help" "-y" "--config $td/missing" "--bogus" "--extend" \
        "--extend acme" "-y --extend acme"; do
        # shellcheck disable=SC2086 # shape is a list of words
        "$ROOT_DIR/bin/ctx-core" implements setup $shape || { echo "implements setup $shape: no"; exit 1; }
      done
    fi
    # implements 0 but the command exits 127: that is the answer, Bash must not run
    cat >"$td/fake127" <<'EOS'
#!/usr/bin/env bash
[[ "$1" == implements ]] && exit 0
exit 127
EOS
    chmod +x "$td/fake127"
    set +e
    out="$(CTX_CORE_BIN="$td/fake127" "$ROOT_DIR/bin/ctx" --json list 2>/dev/null)"
    rc=$?
    set -e
    [[ $rc -eq 127 && -z "$out" ]] || { echo "rc127: rc=$rc out=$out"; exit 1; }
    # Text list and its ls alias forward as the canonical name
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" ls 2>/dev/null)"
    [[ "$out" == "GO list" ]] || { echo "text list not forwarded: $out"; exit 1; }
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" st 2>/dev/null)"
    [[ "$out" == "GO status" ]] || { echo "text status not forwarded: $out"; exit 1; }
    # Any exit code is ctx-core's answer
    printf '#!/usr/bin/env bash\n[[ "$1" == implements ]] && exit 0\necho nope\nexit 3\n' >"$td/fake3"
    chmod +x "$td/fake3"
    set +e
    out="$(CTX_CORE_BIN="$td/fake3" "$ROOT_DIR/bin/ctx" --json list)"
    rc=$?
    set -e
    [[ $rc -eq 3 && "$out" == nope ]] || { echo "rc3: rc=$rc out=$out"; exit 1; }
  ) || fail "ctx-core forward fallback"
  pass "read-only commands forward to ctx-core and run only when ctx-core implements them; otherwise Bash (also CTX_CORE=off)"
}

test_ctx_core_dispatch() {
  (
    set -euo pipefail
    local td out err
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    printf '#!/usr/bin/env bash\necho "$*"\n' >"$td/ctx-core"
    chmod +x "$td/ctx-core"
    out="$(CTX_CORE_BIN="$td/ctx-core" "$ROOT_DIR/bin/ctx" mcp --x)"
    [[ "$out" == "mcp --x" ]] || { echo "mcp dispatch: $out"; exit 1; }
    out="$(CTX_CORE_BIN="$td/ctx-core" "$ROOT_DIR/bin/ctx" knowledge list)"
    [[ "$out" == "knowledge list" ]] || { echo "knowledge dispatch: $out"; exit 1; }
    # CTX_CORE_BIN set but missing: no fall-through
    set +e
    err="$(CTX_CORE_BIN="$td/missing" "$ROOT_DIR/bin/ctx" mcp 2>&1 >/dev/null)"
    local rc=$?
    set -e
    [[ $rc -eq 1 ]] || { echo "missing: rc=$rc"; exit 1; }
    [[ "$err" == *"ctx-core not found"* ]] || { echo "missing msg: $err"; exit 1; }
  ) || fail "ctx-core dispatch"
  pass "ctx mcp/knowledge dispatch to ctx-core; missing binary exits 1 with guidance"
}

test_install_builds_ctx_core() {
  command -v go >/dev/null 2>&1 || { echo "SKIP: test_install_builds_ctx_core (go not installed)"; return 0; }
  (
    set -euo pipefail
    local td out want
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    mkdir -p "$td/bin" "$td/lib"
    out="$(CTX_INSTALL_BIN="$td/bin" CTX_INSTALL_LIB="$td/lib" CTX_UPGRADE_ONLY=1 \
      bash "$ROOT_DIR/install.sh" 2>&1)" || { echo "$out"; exit 1; }
    [[ -x "$td/lib/ctx-core" ]] || { echo "no executable $td/lib/ctx-core: $out"; exit 1; }
    want="$(parse_ctx_version_from_core_sh_file "$ROOT_DIR/lib/core.sh")"
    out="$("$td/lib/ctx-core" version)"
    [[ "$out" == "ctx-core $want" ]] || { echo "version: '$out' want 'ctx-core $want'"; exit 1; }
    [[ -x "$td/bin/ctx" ]] || exit 1
  ) || fail "install builds ctx-core"
  pass "install.sh builds ctx-core next to the libs with the ctx version"
}

# The Bash doctor (CTX_CORE=off); the Go one is covered by internal/cli/doctor_test.go.
test_doctor_reports_ctx_core() {
  (
    set -euo pipefail
    local td out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    mkdir -p "$td/home" "$td/ctx"
    printf '#!/usr/bin/env bash\necho "ctx-core 9.9.9"\n' >"$td/ctx-core"
    chmod +x "$td/ctx-core"
    out="$(HOME="$td/home" CTX_DIR="$td/ctx" CTX_CORE=off CTX_CORE_BIN="$td/ctx-core" "$ROOT_DIR/bin/ctx" doctor 2>&1 || true)"
    [[ "$out" == *"ctx-core: 9.9.9 ($td/ctx-core)"* ]] || { echo "present: $out"; exit 1; }
    out="$(HOME="$td/home" CTX_DIR="$td/ctx" CTX_CORE=off CTX_CORE_BIN="$td/missing" "$ROOT_DIR/bin/ctx" doctor 2>&1 || true)"
    [[ "$out" == *"ctx-core missing"*"ctx mcp unavailable"* ]] || { echo "missing: $out"; exit 1; }
  ) || fail "doctor reports ctx-core"
  pass "ctx doctor reports ctx-core version/path or a warning"
}

# R4: a profile warning must not be followed by "Everything looks good.". Everything else
# is made healthy (fake tools, hooks, SSH, secret provider) so only the profile can warn.
test_doctor_profile_warning_is_not_all_good() {
  (
    set -euo pipefail
    local td out t
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    mkdir -p "$td/home/.ctx/profiles" "$td/home/.ssh" "$td/bin" "$td/work"
    chmod 700 "$td/home/.ssh"
    for t in git mise gum gh aws az gcloud kubectl; do
      printf '#!/bin/sh\necho "%s 1.0"\n' "$t" >"$td/bin/$t"
      chmod +x "$td/bin/$t"
    done
    # doctor probes versions with `bash -lc`; a login shell puts the machine's own tool dirs
    # (path_helper) before the fakes, so this bash drops the -l.
    printf '#!/bin/sh\nif [ "$1" = -lc ]; then exec /bin/bash -c "$2"; fi\nexec /bin/bash "$@"\n' >"$td/bin/bash"
    chmod +x "$td/bin/bash"
    printf '#!/bin/sh\necho "ctx-core 9.9.9"\n' >"$td/ctx-core"
    chmod +x "$td/ctx-core"
    printf 'eval "$(mise activate bash)"\n# BEGIN ctx profile autoswitch\n' >"$td/home/.bashrc"
    printf 'Include %s\n' "$td/home/.ssh/ctx_config" >"$td/home/.ssh/config"
    printf 'WORK_DIR=%s\n' "$td/work" >"$td/work.conf"
    touch "$td/work/mise.toml"
    local run='env HOME="$td/home" CTX_DIR="$td/home/.ctx" SHELL=/bin/bash PATH="$td/bin:/usr/bin:/bin" CTX_CORE=off CTX_CORE_BIN="$td/ctx-core" CTX_SECRET_PROVIDER=file "$ROOT_DIR/bin/ctx" --no-gum doctor'
    cp "$td/work.conf" "$td/home/.ctx/profiles/good.conf"
    out="$(eval "$run" 2>&1 || true)"
    [[ "$out" == *"Everything looks good."* ]] || { echo "healthy fixture is not all good: $out"; exit 1; }
    printf 'WORK_DIR=%s\nSSH_KEY_PATH=%s\n' "$td/work" "$td/nokey" >"$td/home/.ctx/profiles/acme.conf"
    out="$(eval "$run" 2>&1 || true)"
    [[ "$out" == *"Profile 'acme': SSH key missing: $td/nokey"* ]] || { echo "no profile warning: $out"; exit 1; }
    [[ "$out" != *"Everything looks good."* ]] || { echo "all good after a warning: $out"; exit 1; }
    [[ "$out" == *"Fix the issues above."* ]] || { echo "no closing warning: $out"; exit 1; }
  ) || fail "doctor profile warning is not all good"
  pass "ctx doctor never says all good after a profile warning"
}
test_doctor_profile_warning_is_not_all_good

# Coverage guard: with CTX_CORE enabled, make sure the suite really exercises the Go
# path. Without it a missing or unfound bin/ctx-core silently turns the default-mode
# run into a second Bash-only run. CTX_CORE_OPTIONAL=1 skips it for local runs without Go.
test_ctx_core_is_exercised() {
  if [[ "${CTX_CORE:-}" == "off" ]]; then
    pass "ctx-core coverage guard skipped (CTX_CORE=off)"
    return 0
  fi
  if [[ ! -x "$ROOT_DIR/bin/ctx-core" ]]; then
    if [[ "${CTX_CORE_OPTIONAL:-}" == "1" ]]; then
      pass "ctx-core coverage guard skipped (bin/ctx-core missing, CTX_CORE_OPTIONAL=1)"
      return 0
    fi
    fail "bin/ctx-core is missing: build it (go build -o bin/ctx-core ./cmd/ctx-core) or set CTX_CORE_OPTIONAL=1 to run Bash-only locally"
  fi
  (
    set -euo pipefail
    local td out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    mkdir -p "$td/home" "$td/ctx"
    # bin/ctx must find the repo's bin/ctx-core before any ctx-core on PATH and hand over to it:
    # a ctx-core on PATH that records being asked would otherwise be consulted ("implements").
    mkdir -p "$td/path"
    printf '#!/bin/sh\necho asked >"%s/asked"\nexit 1\n' "$td" >"$td/path/ctx-core"
    chmod +x "$td/path/ctx-core"
    out="$(env -u CTX_CORE_BIN PATH="$td/path:$PATH" HOME="$td/home" CTX_DIR="$td/ctx" "$ROOT_DIR/bin/ctx" list --json 2>&1)" \
      || { echo "ctx list --json failed: $out"; exit 1; }
    [[ ! -e "$td/asked" ]] || { echo "bin/ctx consulted the ctx-core on PATH instead of $ROOT_DIR/bin/ctx-core"; exit 1; }
    [[ "$("$ROOT_DIR/bin/ctx-core" version)" == "ctx-core "* ]] || { echo "bin/ctx-core version"; exit 1; }
    "$ROOT_DIR/bin/ctx-core" implements list --json >/dev/null || { echo "ctx-core implements list --json failed"; exit 1; }
    "$ROOT_DIR/bin/ctx-core" implements doctor >/dev/null || { echo "ctx-core implements doctor failed"; exit 1; }
    # bin/ctx must hand over to ctx-core: a wrapper around the real binary logs every call,
    # and the forwarded commands must show up as "implements <cmd>" followed by "<cmd>".
    printf '#!/bin/sh\necho "$*" >>"%s/core-calls"\nexec "%s" "$@"\n' "$td" "$ROOT_DIR/bin/ctx-core" >"$td/core"
    chmod +x "$td/core"
    : >"$td/core-calls"
    env CTX_CORE_BIN="$td/core" HOME="$td/home" CTX_DIR="$td/ctx" "$ROOT_DIR/bin/ctx" list --json >/dev/null 2>&1 || { echo "list --json failed"; exit 1; }
    out="$(env CTX_CORE_BIN="$td/core" HOME="$td/home" CTX_DIR="$td/ctx" "$ROOT_DIR/bin/ctx" --no-gum doctor 2>&1 || true)"
    env CTX_CORE_BIN="$td/core" HOME="$td/home" CTX_DIR="$td/ctx" "$ROOT_DIR/bin/ctx" version >/dev/null 2>&1 || { echo "version failed"; exit 1; }
    env CTX_CORE_BIN="$td/core" HOME="$td/home" CTX_DIR="$td/ctx" SHELL=/bin/zsh "$ROOT_DIR/bin/ctx" --no-gum --dry-run install-hook "$td/home/rc" </dev/null >/dev/null 2>&1 \
      || { echo "install-hook --dry-run failed"; exit 1; }
    env CTX_CORE_BIN="$td/core" HOME="$td/home" CTX_DIR="$td/ctx" SHELL=/bin/zsh "$ROOT_DIR/bin/ctx" --no-gum --dry-run init </dev/null >/dev/null 2>&1 \
      || { echo "init --dry-run failed"; exit 1; }
    local want
    for want in 'implements list --json' 'list --json' 'implements doctor' 'doctor' 'implements ctx-version' 'ctx-version' \
      "implements install-hook $td/home/rc" "install-hook $td/home/rc" 'implements init' 'init'; do
      grep -qx -- "$want" "$td/core-calls" || { echo "ctx-core was not asked '$want'; calls: $(cat "$td/core-calls")"; exit 1; }
    done
    [[ "$out" == *"ctx-core: "*"($td/core)"* || "$out" == *"ctx-core: "*"/ctx-core)"* ]] || { echo "doctor does not report ctx-core: $out"; exit 1; }
    [[ "$out" != *"ctx-core missing"* ]] || { echo "doctor says ctx-core is missing: $out"; exit 1; }
  ) || fail "ctx-core coverage guard"
  pass "bin/ctx finds the repo's bin/ctx-core before PATH and forwards list, doctor, version, install-hook and init to it"
}

test_ctx_core_is_exercised
# Profile "demo" with AWS_PROFILE_NAME and a file-provider secret full of shell metacharacters.
# Sets td and exports CTX_TEST_SECRET in the calling subshell.
_use_eval_fixture() {
  td="$(cd "$(mktemp -d)" && pwd -P)"
  export HOME="$td/home" CTX_DIR="$td/home/.ctx" GIT_CONFIG_NOSYSTEM=1 PATH="$ROOT_DIR/bin:$PATH"
  unset AWS_PROFILE API_KEY CTX_ACTIVE_PROFILE CTX_ACTIVATION_TRIGGER CTX_QUIET GH_TOKEN
  mkdir -p "$CTX_DIR/profiles" "$CTX_DIR/secrets/demo" "$td/w/demo/r"
  {
    printf 'PROFILE_NAME=demo\nGIT_NAME=Dev\nGIT_EMAIL=dev@demo.com\nWORK_DIR=%s\n' "$td/w/demo"
    printf 'AWS_PROFILE_NAME="demo-prod"\nSECRET_KEYS=API_KEY\n'
  } >"$CTX_DIR/profiles/demo.conf"
  printf 'secret_provider=file\n' >"$CTX_DIR/config"
  # Literal quotes, $, backticks, a backslash and a newline are the point of this value.
  # shellcheck disable=SC2089,SC2090
  CTX_TEST_SECRET=$'a b \'q\' $x `id` "dq" \\\nline2'
  # shellcheck disable=SC2090
  export CTX_TEST_SECRET
  printf '%s' "$CTX_TEST_SECRET" >"$CTX_DIR/secrets/demo/API_KEY"
}

test_use_eval_exports() {
  (
    set -euo pipefail
    local td out err
    _use_eval_fixture
    cd "$td"
    out="$(ctx --no-gum use demo --eval bash 2>"$td/err")" || { echo "use --eval failed: $(cat "$td/err")"; exit 1; }
    grep -q 'Switching to' "$td/err" || { echo "human messages not on stderr"; exit 1; }
    if grep -vq '^export [A-Za-z_][A-Za-z0-9_]*=' <<<"$out"; then echo "non-export stdout: $out"; exit 1; fi
    (
      [[ -z "$(eval "$out")" ]] || { echo "eval printed output"; exit 1; }
      eval "$out"
      [[ "$AWS_PROFILE" == demo-prod && "$CTX_ACTIVE_PROFILE" == demo ]] || { echo "AWS/active: $out"; exit 1; }
      [[ "$API_KEY" == "$CTX_TEST_SECRET" ]] || { echo "secret did not round-trip: '$API_KEY'"; exit 1; }
      eval "$(ctx deactivate --eval bash 2>/dev/null)"
      [[ -z "${AWS_PROFILE+x}${API_KEY+x}${CTX_ACTIVE_PROFILE+x}" ]] || { echo "deactivate left vars set"; exit 1; }
    ) || exit 1
    [[ "$(ctx -q use --eval bash demo 2>/dev/null)" == "$out" ]] || { echo "--eval before name"; exit 1; }

    out="$(ctx use demo --eval fish 2>/dev/null)"
    grep -qxF "set -gx AWS_PROFILE 'demo-prod'" <<<"$out" || { echo "fish output: $out"; exit 1; }
    grep -qxF "set -gx CTX_ACTIVE_PROFILE 'demo'" <<<"$out" || { echo "fish active: $out"; exit 1; }
    if command -v fish >/dev/null 2>&1; then
      # shellcheck disable=SC2016
      fish -c 'source; test "$API_KEY" = "$CTX_TEST_SECRET"; and test "$AWS_PROFILE" = demo-prod' <<<"$out" \
        || { echo "fish source"; exit 1; }
    fi

    err="$(ctx --no-gum use demo 2>&1 >/dev/null)"
    grep -q 'not applied to this shell' <<<"$err" || { echo "no plain-use warning: $err"; exit 1; }
    # shellcheck disable=SC2016
    grep -qF 'eval "$(ctx use demo --eval bash)"' <<<"$err" || { echo "warning lacks fix: $err"; exit 1; }
    [[ -z "$(ctx -q use demo 2>&1 >/dev/null)" ]] || { echo "quiet use warned"; exit 1; }
    rm -rf "$td"
  ) || fail "ctx use --eval"
  pass "ctx use --eval bash|fish prints only shell code; plain use warns the vars did not reach the shell"
}

test_shell_wrapper_applies_use() {
  local sh
  for sh in bash zsh; do
    command -v "$sh" >/dev/null 2>&1 || continue
    (
      set -euo pipefail
      local td
      _use_eval_fixture
      cd "$td"
      # shellcheck disable=SC2016
      "$sh" -c '
        source "$1/lib/ctx_autoswitch.bash"
        ctx --no-gum use demo >/dev/null 2>&1 || { echo "use failed"; exit 1; }
        [[ "$AWS_PROFILE" == demo-prod && "$API_KEY" == "$CTX_TEST_SECRET" && "$CTX_ACTIVE_PROFILE" == demo ]] || { echo "use: AWS=${AWS_PROFILE:-}"; exit 1; }
        ctx deactivate >/dev/null 2>&1
        [[ -z "${AWS_PROFILE+x}${API_KEY+x}${CTX_ACTIVE_PROFILE+x}" ]] || { echo "deactivate left vars"; exit 1; }
        ctx -q use demo || { echo "-q use failed"; exit 1; }
        [[ "$AWS_PROFILE" == demo-prod ]] || { echo "-q use: AWS unset"; exit 1; }
        ctx deactivate >/dev/null 2>&1
        ctx --dry-run use demo >/dev/null 2>&1 || true
        [[ -z "${AWS_PROFILE+x}" ]] || { echo "--dry-run use changed the shell"; exit 1; }
        [[ "$(ctx list 2>&1)" == "$(command ctx list 2>&1)" ]] || { echo "list passthrough"; exit 1; }
        ctx version | grep -q "^ctx version" || { echo "version passthrough"; exit 1; }
        ctx use nosuch >/dev/null 2>&1 && { echo "bad profile exit 0"; exit 1; }
        ctx nosuchcmd >/dev/null 2>&1 && { echo "bad command exit 0"; exit 1; }
        exit 0
      ' _ "$ROOT_DIR" || exit 1
      rm -rf "$td"
    ) || fail "shell wrapper ($sh)"
  done
  pass "ctx shell wrapper applies use/deactivate to the current shell (bash, zsh)"
}

test_autoswitch_exports_profile_env() {
  (
    set -euo pipefail
    local td
    _use_eval_fixture
    cd "$td"
    # shellcheck disable=SC2016
    bash -c '
      source "$1/lib/ctx_autoswitch.bash"
      cd "$2/w/demo/r" && _ctx_profile_autoswitch
      [[ "${AWS_PROFILE:-}" == demo-prod && "${API_KEY:-}" == "$CTX_TEST_SECRET" ]] || { echo "hook did not export: AWS=${AWS_PROFILE:-}"; exit 1; }
      cd "$2" && _ctx_profile_autoswitch
      [[ -z "${AWS_PROFILE+x}${API_KEY+x}" ]] || { echo "hook left vars after leaving"; exit 1; }
      cd "$2/w/demo/r" && _ctx_profile_autoswitch
    ' _ "$ROOT_DIR" "$td" || exit 1
    grep -qx 'active=demo' "$CTX_DIR/config" || { echo "not active"; exit 1; }
    # A new terminal opened in the folder while demo is already active still gets the env.
    # shellcheck disable=SC2016
    bash -c '
      cd "$2/w/demo/r" && source "$1/lib/ctx_autoswitch.bash"
      [[ "${AWS_PROFILE:-}" == demo-prod ]] || { echo "new shell: AWS unset"; exit 1; }
    ' _ "$ROOT_DIR" "$td" || exit 1
    rm -rf "$td"
  ) || fail "autoswitch exports profile env"
  pass "autoswitch hook applies AWS_PROFILE and secrets to the shell"
}

test_autoswitch_keeps_manual_lock() {
  (
    set -euo pipefail
    local td
    _use_eval_fixture
    cd "$td"
    ctx --no-gum use demo >/dev/null 2>&1
    grep -qx 'active_source=manual' "$CTX_DIR/config" || { echo "manual use did not lock"; exit 1; }
    # The hook re-applying the same profile (a new terminal) must not downgrade the lock.
    CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use demo --eval bash >/dev/null 2>&1
    grep -qx 'active_source=manual' "$CTX_DIR/config" || { echo "lock downgraded to auto"; exit 1; }
    rm -rf "$td"
  ) || fail "autoswitch keeps manual lock"
  pass "autoswitch re-applying the active profile keeps a manual lock"
}

test_quiet_use_cloud_tools_succeed() {
  (
    set -euo pipefail
    local td t err rc=0
    _use_eval_fixture
    mkdir -p "$td/bin"
    for t in az gcloud kubectl; do
      printf '#!/bin/sh\nexit 0\n' >"$td/bin/$t"
      chmod +x "$td/bin/$t"
    done
    printf 'AZURE_SUBSCRIPTION=sub\nGCP_PROJECT=proj\nKUBE_CONTEXT=kc\n' >>"$CTX_DIR/profiles/demo.conf"
    export PATH="$td/bin:$PATH"
    cd "$td"
    ctx -q use demo --eval bash >/dev/null 2>"$td/err" || rc=$?
    err="$(cat "$td/err")"
    [[ $rc -eq 0 ]] || { echo "quiet use exit $rc: $err"; exit 1; }
    [[ -z "$err" ]] || { echo "quiet use warned on success: $err"; exit 1; }
    rm -rf "$td"
  ) || fail "quiet use with az/gcloud/kubectl"
  pass "quiet ctx use treats az/gcloud/kubectl success as success"
}

test_inherited_quiet_use() {
  (
    set -euo pipefail
    local td out mode
    _use_eval_fixture
    cd "$td"
    # The autoswitch hook runs `CTX_QUIET=1 CTX_AUTO_SWITCH=1 command ctx use --eval bash <p>`.
    for mode in off on; do
      out="$(CTX_CORE="$mode" CTX_QUIET=1 ctx use demo --eval bash 2>"$td/err")" || { echo "use failed ($mode): $(cat "$td/err")"; exit 1; }
      [[ -z "$(cat "$td/err")" ]] || { echo "inherited CTX_QUIET=1 not quiet ($mode): $(cat "$td/err")"; exit 1; }
      grep -qx 'export AWS_PROFILE=demo-prod' <<<"$out" || { echo "exports ($mode): $out"; exit 1; }
    done
    # With ctx-core installed, quiet use runs in Go; non-quiet use runs there too.
    if [[ "${CTX_CORE:-}" != off && -x "$ROOT_DIR/bin/ctx-core" ]]; then
      printf '#!/bin/sh\necho "$*" >>"%s/core-calls"\nexec "%s" "$@"\n' "$td" "$ROOT_DIR/bin/ctx-core" >"$td/core"
      chmod +x "$td/core"
      : >"$td/core-calls"
      CTX_CORE_BIN="$td/core" CTX_QUIET=1 ctx use demo --eval bash >/dev/null 2>&1
      grep -qx 'use demo --eval bash' "$td/core-calls" || { echo "quiet use not forwarded: $(cat "$td/core-calls")"; exit 1; }
      : >"$td/core-calls"
      CTX_CORE_BIN="$td/core" ctx --no-gum use demo --eval bash >/dev/null 2>&1
      grep -qx 'use demo --eval bash' "$td/core-calls" || { echo "non-quiet use not forwarded: $(cat "$td/core-calls")"; exit 1; }
    fi
    rm -rf "$td"
  ) || fail "inherited CTX_QUIET"
  pass "ctx use honors an inherited CTX_QUIET=1 (the autoswitch hook); quiet use runs in ctx-core"
}

# With the real bin/ctx-core, quiet use and deactivate are served by Go (a wrapper logs each ctx-core argv).
test_use_deactivate_served_by_go() {
  if [[ "${CTX_CORE:-}" == off || ! -x "$ROOT_DIR/bin/ctx-core" ]]; then
    pass "Go use/deactivate check skipped (CTX_CORE=off or no bin/ctx-core)"
    return 0
  fi
  (
    set -euo pipefail
    local td out
    _use_eval_fixture
    cd "$td"
    printf '#!/bin/sh\necho "$*" >>"%s/core-calls"\nexec "%s" "$@"\n' "$td" "$ROOT_DIR/bin/ctx-core" >"$td/core"
    chmod +x "$td/core"
    : >"$td/core-calls"
    export CTX_CORE_BIN="$td/core"
    out="$(CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use demo --eval bash 2>/dev/null)"
    grep -qx 'export AWS_PROFILE=demo-prod' <<<"$out" || { echo "use exports: $out"; exit 1; }
    out="$(CTX_QUIET=1 ctx deactivate --eval bash 2>/dev/null)"
    grep -q '^unset AWS_PROFILE ' <<<"$out" || { echo "deactivate unsets: $out"; exit 1; }
    grep -qx 'use demo --eval bash' "$td/core-calls" || { echo "use not run by ctx-core: $(cat "$td/core-calls")"; exit 1; }
    grep -qx 'deactivate --eval bash' "$td/core-calls" || { echo "deactivate not run by ctx-core: $(cat "$td/core-calls")"; exit 1; }
    rm -rf "$td"
  ) || fail "use/deactivate served by ctx-core"
  pass "quiet ctx use and ctx deactivate run in ctx-core"
}

test_plain_prompts_use_stderr() {
  (
    set -euo pipefail
    local td out err
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" PATH="$ROOT_DIR/bin:$PATH"
    mkdir -p "$CTX_DIR/profiles" "$CTX_DIR/secrets/p"
    printf 'PROFILE_NAME=p\nGIT_NAME=Dev\nGIT_EMAIL=d@p.com\n' >"$CTX_DIR/profiles/p.conf"
    printf 'secret_provider=file\n' >"$CTX_DIR/config"

    out="$(printf 'hunter2\n' | ctx --no-gum secret set p API_KEY 2>/dev/null)"
    [[ "$(cat "$CTX_DIR/secrets/p/API_KEY")" == hunter2 ]] || { echo "secret value: $(cat "$CTX_DIR/secrets/p/API_KEY")"; exit 1; }
    ! grep -q '?' <<<"$out" || { echo "prompt leaked to stdout: $out"; exit 1; }

    # shellcheck disable=SC2034 # read by pick_one/pick_many from core.sh
    HAS_GUM=false
    out="$(printf '2\n' | pick_one "Pick" a b c 2>/dev/null)"
    [[ "$out" == b ]] || { echo "pick_one captured: '$out'"; exit 1; }
    out="$(printf '1,3\n' | pick_many "Pick" a b c 2>/dev/null)"
    [[ "$out" == $'a\nc' ]] || { echo "pick_many captured: '$out'"; exit 1; }

    # The label sub-check needs a PATH without a real `pass`; skip it when /usr/bin:/bin has one.
    if [[ -z "$(PATH="/usr/bin:/bin" type -P pass)" ]]; then
      printf 'secret_provider=pass\n' >"$CTX_DIR/config"
      err="$(printf 'x\n' | PATH="$ROOT_DIR/bin:/usr/bin:/bin" ctx --no-gum secret set p API_KEY 2>&1 >/dev/null || true)"
      grep -q "provider 'pass password store'" <<<"$err" || { echo "error label: $err"; exit 1; }
      ! grep -q 'command not found' <<<"$err" || { echo "command not found in: $err"; exit 1; }
    fi
    rm -rf "$td"
  ) || fail "plain prompts write to stderr"
  pass "plain prompts keep stdout clean"
}

test_remove_gitconfig_symlink_and_nomatch() {
  (
    set -euo pipefail
    local td m
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" PATH="$ROOT_DIR/bin:$PATH"
    mkdir -p "$CTX_DIR/profiles" "$HOME/.config/git" "$HOME/dotfiles"
    printf 'secret_provider=file\n' >"$CTX_DIR/config"
    printf 'PROFILE_NAME=acme\nGIT_NAME=Dev\nGIT_EMAIL=d@p.com\n' >"$CTX_DIR/profiles/acme.conf"
    printf '[user]\n\tname = Dev\n' >"$HOME/.config/git/ctx-acme"
    printf '[user]\n\tname = Me\n\n[includeIf "gitdir:~/w/acme/"]\n  path = %s/.config/git/ctx-acme\n' "$HOME" >"$HOME/dotfiles/gitconfig"
    chmod 644 "$HOME/dotfiles/gitconfig"
    ln -s "$HOME/dotfiles/gitconfig" "$HOME/.gitconfig"
    printf 'y\n' | ctx --no-gum remove acme >/dev/null 2>&1 || { echo "remove failed"; exit 1; }
    [[ -L "$HOME/.gitconfig" ]] || { echo ".gitconfig no longer a symlink"; exit 1; }
    ! grep -q includeIf "$HOME/dotfiles/gitconfig" || { echo "target not updated"; exit 1; }
    m="$(stat -c %a "$HOME/dotfiles/gitconfig" 2>/dev/null || stat -f %Lp "$HOME/dotfiles/gitconfig")"
    [[ "$m" == 644 ]] || { echo "target mode $m"; exit 1; }
    # Nothing to drop: the file stays as it is.
    printf '[user]\n\tname = Me\n' >"$HOME/dotfiles/gitconfig"
    printf 'PROFILE_NAME=acme\nGIT_NAME=Dev\nGIT_EMAIL=d@p.com\n' >"$CTX_DIR/profiles/acme.conf"
    rm -rf "$CTX_DIR/backups"
    printf 'y\n' | ctx --no-gum remove acme >/dev/null 2>&1 || { echo "second remove failed"; exit 1; }
    [[ -L "$HOME/.gitconfig" && "$(cat "$HOME/dotfiles/gitconfig")" == $'[user]\n\tname = Me' ]] || { echo "no-match changed the file"; exit 1; }
    ! grep -rq gitconfig "$CTX_DIR/backups" 2>/dev/null || { echo "backup made without a rewrite"; exit 1; }
    rm -rf "$td"
  ) || fail "remove keeps a symlinked gitconfig and leaves it alone when nothing matches"
  pass "remove writes through a symlinked ~/.gitconfig and skips no-match files"
}

test_remove_only_own_include() {
  (
    set -euo pipefail
    local td gc
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" PATH="$ROOT_DIR/bin:$PATH"
    mkdir -p "$CTX_DIR/profiles" "$HOME/.config/git" "$HOME/.ssh"
    printf 'secret_provider=file\n' >"$CTX_DIR/config"
    local n
    for n in acme acme-old; do
      printf 'PROFILE_NAME=%s\nGIT_NAME=Dev\nGIT_EMAIL=d@p.com\n' "$n" >"$CTX_DIR/profiles/$n.conf"
      printf '[user]\n\tname = Dev\n' >"$HOME/.config/git/ctx-$n"
    done
    printf '[user]\n\tname = Me\n\n[includeIf "gitdir:~/w/acme/"]\n  path = %s/.config/git/ctx-acme\n\n[includeIf "gitdir:~/w/acme-old/"]\n  path = %s/.config/git/ctx-acme-old\n[alias]\n\tco = checkout\n' \
      "$HOME" "$HOME" >"$HOME/.gitconfig"
    printf '\nHost github-acme\n  HostName github.com\n  User git\n  IdentityFile k\n  IdentitiesOnly yes\n\nHost github-acme-old\n  HostName github.com\n  User git\n  IdentityFile k2\n  IdentitiesOnly yes\n' >"$HOME/.ssh/ctx_config"
    printf 'y\n' | ctx --no-gum remove acme >/dev/null 2>&1 || { echo "remove failed"; exit 1; }
    gc="$(cat "$HOME/.gitconfig")"
    grep -q 'ctx-acme-old' <<<"$gc" || { echo "acme-old include lost: $gc"; exit 1; }
    ! grep -q 'ctx-acme$' <<<"$gc" || { echo "acme path left: $gc"; exit 1; }
    [[ "$(grep -c 'includeIf' <<<"$gc")" == 1 ]] || { echo "includeIf headers: $gc"; exit 1; }
    grep -q 'name = Me' <<<"$gc" && grep -q 'co = checkout' <<<"$gc" || { echo "user sections lost: $gc"; exit 1; }
    grep -q 'Host github-acme-old' "$HOME/.ssh/ctx_config" || { echo "acme-old host lost"; exit 1; }
    ! grep -qx 'Host github-acme' "$HOME/.ssh/ctx_config" || { echo "acme host left"; exit 1; }
    [[ ! -e "$CTX_DIR/profiles/acme.conf" && -e "$CTX_DIR/profiles/acme-old.conf" ]] || { echo "profile files wrong"; exit 1; }
    rm -rf "$td"
  ) || fail "remove deletes only the profile's own include"
  pass "remove keeps other profiles' includes and user sections"
}

# ── Interactive setup wizard (plain mode, answers on stdin) ─────────────────
# _wizard_sandbox TD: fake HOME under TD, file secrets, CTX_CORE=off, and fake
# ssh-keygen/gh/mise/kubectl/az/gcloud on PATH that append argv to TD/argv.log.
# gh auth status prints TD/gh_status. The caller cds into the work dir.
_wizard_sandbox() {
  local td="$1" t
  # CTX_CORE follows the suite: with ctx-core built, these wizard runs are
  # served by Go; under CTX_CORE=off by Bash.
  export HOME="$td/home" CTX_DIR="$td/home/.ctx" GIT_CONFIG_NOSYSTEM=1
  export CTX_SECRET_PROVIDER=file
  unset XDG_CONFIG_HOME CTX_WORK_ROOT CTX_NO_GUM
  mkdir -p "$HOME/.ssh" "$td/fake" "$td/w/acme"
  : >"$td/argv.log"
  : >"$td/gh_status"
  cat >"$td/fake/ssh-keygen" <<EOS
#!/bin/sh
{ printf 'ssh-keygen'; printf ' [%s]' "\$@"; echo; } >>"$td/argv.log"
f=""
while [ \$# -gt 0 ]; do
  case "\$1" in
    -lf) echo "256 SHA256:fake x (ED25519)"; exit 0 ;;
    -f) f="\$2"; shift ;;
  esac
  shift
done
[ -n "\$f" ] && { echo key >"\$f"; echo "ssh-ed25519 AAAAfake ctx" >"\$f.pub"; }
exit 0
EOS
  cat >"$td/fake/gh" <<EOS
#!/bin/sh
{ printf 'gh'; printf ' [%s]' "\$@"; echo; } >>"$td/argv.log"
[ "\$1 \$2" = "auth status" ] && cat "$td/gh_status"
exit 0
EOS
  for t in mise kubectl az gcloud; do
    printf '#!/bin/sh\n{ printf %s; printf " [%%s]" "$@"; echo; } >>"%s"\nexit 0\n' "$t" "$td/argv.log" >"$td/fake/$t"
  done
  chmod +x "$td/fake/"*
  export PATH="$td/fake:$ROOT_DIR/bin:$PATH"
}

# Paths and file hashes under a directory (for "nothing changed" checks).
_tree_digest() {
  (cd "$1" && find . -print | LC_ALL=C sort && find . -type f -exec shasum {} + | LC_ALL=C sort)
}

test_setup_extend_reaches_flow() {
  (
    set -euo pipefail
    local td out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    mkdir -p "$CTX_DIR/profiles"
    echo key >"$HOME/.ssh/id_acme"
    printf '# keep\n' >"$td/w/acme/mise.toml"
    {
      printf 'PROFILE_NAME=acme\nGIT_NAME=Dev\nGIT_EMAIL=dev@acme.com\nWORK_DIR=%s\n' "$td/w/acme"
      printf 'SSH_KEY_PATH=%s\nGITHUB_USER=octo\nAWS_PROFILE_NAME=dev\n' "$HOME/.ssh/id_acme"
    } >"$CTX_DIR/profiles/acme.conf"
    cd "$td/w/acme"
    # identity? n, secrets? n, env vars? n, write? y
    out="$(printf '%s\n' n n n y | ctx --no-gum setup --extend acme 2>&1)" || { echo "extend failed: $out"; exit 1; }
    grep -q 'Extend profile: acme' <<<"$out" || { echo "extend flow not reached: $out"; exit 1; }
    grep -q "Profile 'acme' updated." <<<"$out" || { echo "no update summary: $out"; exit 1; }
    # R7: an existing mise.toml is kept and says so.
    grep -qF "mise.toml kept (already exists): $td/w/acme/mise.toml" <<<"$out" || { echo "kept line missing: $out"; exit 1; }
    ! grep -q 'mise.toml generated' <<<"$out" || { echo "claims generated: $out"; exit 1; }
    [[ "$(cat "$td/w/acme/mise.toml")" == '# keep' ]] || { echo "mise.toml overwritten"; exit 1; }
    rm -rf "$td"
  ) || fail "setup --extend reaches the extend flow"
  pass "setup --extend works on an existing profile (and keeps mise.toml)"
}

# R26: extend never duplicates SECRET_KEYS: a key already listed (or entered
# twice) is kept once; new keys are appended.
test_setup_extend_secret_keys_once() {
  (
    set -euo pipefail
    local td out conf
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    mkdir -p "$CTX_DIR/profiles"
    echo key >"$HOME/.ssh/id_acme"
    {
      printf 'PROFILE_NAME=acme\nGIT_NAME=Dev\nGIT_EMAIL=dev@acme.com\nWORK_DIR=%s\n' "$td/w/acme"
      printf 'SSH_KEY_PATH=%s\nGITHUB_USER=octo\nAWS_PROFILE_NAME=dev\nSECRET_KEYS=GH_TOKEN\n' "$HOME/.ssh/id_acme"
    } >"$CTX_DIR/profiles/acme.conf"
    cd "$td/w/acme"
    # identity? n, secrets? y, GH_TOKEN? y + value, another? y NEW_KEY + value,
    # another? y GH_TOKEN + value, another? n, env vars? n, write? y
    out="$(printf '%s\n' n y y tok1 y NEW_KEY v2 y GH_TOKEN tok3 n n y | ctx --no-gum setup --extend acme 2>&1)" \
      || { echo "extend failed: $out"; exit 1; }
    conf="$(cat "$CTX_DIR/profiles/acme.conf")"
    grep -qxF 'SECRET_KEYS=GH_TOKEN\ NEW_KEY' <<<"$conf" || { echo "SECRET_KEYS wrong: $conf"; exit 1; }
    [[ "$(cat "$CTX_DIR/secrets/acme/GH_TOKEN")" == tok3 ]] || { echo "GH_TOKEN not updated"; exit 1; }
    rm -rf "$td"
  ) || fail "setup --extend keeps each SECRET_KEYS entry once (R26)"
  pass "setup --extend keeps each SECRET_KEYS entry once (R26)"
}

test_setup_dry_run_changes_nothing() {
  (
    set -euo pipefail
    local td out before
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    cd "$td/w/acme"
    before="$(_tree_digest "$td/home"; _tree_digest "$td/w")"
    # Full, acme, name, email, no signing, existing path (missing → create? y), auto import,
    # generate key? y, gh login? y, user octo, upload? y, aws/azure/gcp/kube blank,
    # secrets: add, GH_TOKEN y + value, NPM n, DOCKER n, another n, env vars n.
    out="$(printf '%s\n' 2 acme Dev dev@acme.com 1 2 "$td/w/newdir" y 1 y y octo y '' '' '' '' 2 y s3cr3tval n n n n \
      | ctx --no-gum --dry-run setup 2>&1)" || { echo "dry-run failed: $out"; exit 1; }
    [[ "$(_tree_digest "$td/home"; _tree_digest "$td/w")" == "$before" ]] || { echo "dry-run changed files"; find "$td/home" "$td/w"; exit 1; }
    [[ ! -e "$CTX_DIR" ]] || { echo "dry-run created CTX_DIR"; exit 1; }
    ! grep -q '^ssh-keygen' "$td/argv.log" || { echo "ssh-keygen ran: $(cat "$td/argv.log")"; exit 1; }
    ! grep -v '^gh \[auth\] \[status\]$' "$td/argv.log" | grep -q '^gh' || { echo "gh changed state: $(cat "$td/argv.log")"; exit 1; }
    local want
    for want in \
      "[dry-run] would create $CTX_DIR" \
      "[dry-run] would create directory $td/w/newdir" \
      "[dry-run] would generate SSH key $HOME/.ssh/ctx_acme" \
      "[dry-run] would run gh auth login" \
      "[dry-run] would upload SSH key to GitHub (octo)" \
      "[dry-run] would store secret GH_TOKEN" \
      "$td/w/newdir/mise.toml" \
      "(host block: github-acme)" \
      "(includeIf block)"; do
      grep -qF -- "$want" <<<"$out" || { echo "missing '$want' in: $out"; exit 1; }
    done
    ! grep -q s3cr3tval <<<"$out" || { echo "secret value echoed"; exit 1; }

    # Clone scope, existing key, no folder: only files that would be written are listed.
    echo key >"$HOME/.ssh/id_c"
    before="$(_tree_digest "$td/home")"
    out="$(printf '%s\n' c1 1 "$HOME/.ssh/id_c" '' 1 | ctx --no-gum --dry-run setup --scope clone 2>&1)" \
      || { echo "clone dry-run failed: $out"; exit 1; }
    [[ "$(_tree_digest "$td/home")" == "$before" ]] || { echo "clone dry-run changed files"; exit 1; }
    grep -qF '(host block: github-c1)' <<<"$out" || { echo "no host line: $out"; exit 1; }
    ! grep -qF 'mise.toml' <<<"$out" || { echo "mise.toml listed without WORK_DIR: $out"; exit 1; }
    ! grep -qF 'includeIf' <<<"$out" || { echo "includeIf listed without WORK_DIR: $out"; exit 1; }
    rm -rf "$td"
  ) || fail "setup --dry-run changes nothing"
  pass "setup --dry-run leaves HOME untouched and lists what it would do"
}

test_setup_gh_accounts_both_formats() {
  (
    set -euo pipefail
    local td out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    printf 'github.com\n  X Failed to log in to github.com account carol (keyring)\n  ✓ Logged in to github.com account alice (keyring)\n  - Active account: true\n  ✓ Logged in to github.com as bob (/x/hosts.yml)\n' >"$td/gh_status"
    cd "$td/w/acme"
    # ... generate key? n, gh login? n, pick account 3 (bob), aws/azure/gcp/kube, skip secrets, env n, write y
    out="$(printf '%s\n' 2 acme Dev dev@acme.com 1 1 1 n n 3 '' '' '' '' 1 n y | ctx --no-gum setup 2>&1)" \
      || { echo "setup failed: $out"; exit 1; }
    grep -q '^  2  alice$' <<<"$out" && grep -q '^  3  bob$' <<<"$out" || { echo "picker items: $out"; exit 1; }
    ! grep -q carol <<<"$out" || { echo "failed login listed: $out"; exit 1; }
    grep -q '^GITHUB_USER=bob$' "$CTX_DIR/profiles/acme.conf" || { echo "conf: $(cat "$CTX_DIR/profiles/acme.conf")"; exit 1; }
    rm -rf "$td"
  ) || fail "detect_gh_accounts parses both gh formats"
  pass "setup GitHub picker lists 'account USER' and 'as USER' logins"
}

test_setup_secrets_wait_for_confirm() {
  (
    set -euo pipefail
    local td out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    cd "$td/w/acme"
    # ... secrets: add, GH_TOKEN y + value, NPM n, DOCKER n, another n, env n, write? <last>
    local -a ans=(2 acme Dev dev@acme.com 1 1 1 n n '' '' '' '' '' 2 y s3cr3tval n n n n)
    out="$(printf '%s\n' "${ans[@]}" n | ctx --no-gum setup 2>&1)" && { echo "abort exited 0: $out"; exit 1; }
    [[ ! -e "$CTX_DIR/secrets/acme/GH_TOKEN" ]] || { echo "secret stored before confirm"; exit 1; }
    [[ ! -e "$CTX_DIR/profiles/acme.conf" ]] || { echo "profile written on abort"; exit 1; }

    out="$(printf '%s\n' "${ans[@]}" y | ctx --no-gum setup 2>&1)" || { echo "confirm failed: $out"; exit 1; }
    [[ "$(cat "$CTX_DIR/secrets/acme/GH_TOKEN")" == s3cr3tval ]] || { echo "secret not stored"; exit 1; }
    grep -q '^SECRET_KEYS=GH_TOKEN$' "$CTX_DIR/profiles/acme.conf" || { echo "conf: $(cat "$CTX_DIR/profiles/acme.conf")"; exit 1; }
    grep -q 'GH_TOKEN → file store' <<<"$out" || { echo "no stored line: $out"; exit 1; }
    ! grep -q s3cr3tval <<<"$out" || { echo "secret value echoed"; exit 1; }

    # A store that fails warns and leaves the key out of SECRET_KEYS.
    mkdir -p "$td/w/acme2" && cd "$td/w/acme2"
    printf 'x' >"$CTX_DIR/secrets/acme2"
    ans[1]=acme2
    out="$(printf '%s\n' "${ans[@]}" y | ctx --no-gum setup 2>&1)" || { echo "failing-store run failed: $out"; exit 1; }
    grep -q 'Could not store GH_TOKEN' <<<"$out" || { echo "no warning: $out"; exit 1; }
    grep -q "^SECRET_KEYS=''$" "$CTX_DIR/profiles/acme2.conf" || { echo "conf: $(cat "$CTX_DIR/profiles/acme2.conf")"; exit 1; }
    rm -rf "$td"
  ) || fail "setup stores secrets only after the review"
  pass "setup stores secrets only after confirm, and only lists stored keys"
}

test_setup_key_comment_never_empty() {
  (
    set -euo pipefail
    local td out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    cd "$td/w/acme"
    # empty email; generate key? y, passphrase? n, Enter after adding; gh login n; user blank; ...
    out="$(printf '%s\n' 2 acme Dev '' 1 1 1 y n '' n '' '' '' '' '' 1 n y | ctx --no-gum setup 2>&1)" \
      || { echo "setup failed: $out"; exit 1; }
    grep -qF 'ssh-keygen [-t] [ed25519] [-C] [ctx-acme@local]' "$td/argv.log" || { echo "argv: $(cat "$td/argv.log")"; exit 1; }
    ! grep -qF '[-C] []' "$td/argv.log" || { echo "empty comment: $(cat "$td/argv.log")"; exit 1; }
    rm -rf "$td"
  ) || fail "generated key comment falls back to ctx-<profile>@local"
  pass "setup never runs ssh-keygen -C \"\""
}

# A generated key on a HOME with no ~/.ssh: the wizard creates ~/.ssh (0700).
test_setup_generate_creates_ssh_dir() {
  (
    set -euo pipefail
    local td out mode
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    rm -rf "$HOME/.ssh"
    cd "$td/w/acme"
    # import mode; "No SSH keys found. Generate one?" y, passphrase? n, Enter; gh login n; ...
    out="$(printf '%s\n' 2 acme Dev dev@acme.com 1 1 1 y n '' n '' '' '' '' '' 1 n y | ctx --no-gum setup 2>&1)" \
      || { echo "setup failed: $out"; exit 1; }
    mode="$(ls -ld "$HOME/.ssh" | cut -c1-10)"
    [[ "$mode" == drwx------ ]] || { echo "$HOME/.ssh mode: $mode"; exit 1; }
    [[ -f "$HOME/.ssh/ctx_acme" ]] || { echo "no key: $out"; exit 1; }
    rm -rf "$td"
  ) || fail "setup creates ~/.ssh before generating a key"
  pass "setup creates ~/.ssh (0700) before generating a key"
}

test_setup_pick_existing_key_index() {
  (
    set -euo pipefail
    local td out k
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    for k in id_a id_b; do echo key >"$HOME/.ssh/$k"; echo pub >"$HOME/.ssh/$k.pub"; done
    cd "$td/w/acme"
    # key picker: 1 Generate, 2 [1] id_a, 3 [2] id_b
    out="$(printf '%s\n' 2 acme Dev dev@acme.com 1 1 1 3 n '' '' '' '' '' 1 n y | ctx --no-gum setup 2>&1)" \
      || { echo "setup failed: $out"; exit 1; }
    grep -qx "SSH_KEY_PATH=$HOME/.ssh/id_b" "$CTX_DIR/profiles/acme.conf" || { echo "conf: $(cat "$CTX_DIR/profiles/acme.conf")"; exit 1; }
    [[ ! -e "$HOME/.ssh/ctx_acme" ]] || { echo "generated a key instead"; exit 1; }
    rm -rf "$td"
  ) || fail "SSH key choice [2] uses key 2"
  pass "setup existing-key choice is parsed portably"
}

test_setup_mise_toml_escaping() {
  (
    set -euo pipefail
    local td out name
    if ! python3 -c 'import tomllib' 2>/dev/null; then
      pass "setup mise.toml escaping (skipped: python3 has no tomllib)"
      exit 0
    fi
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    cd "$td/w/acme"
    name='Dev "Q" \ $HOME `x`'
    # ... env vars? y, FOO=a"b\c, stop, write y
    out="$(printf '%s\n' 2 acme "$name" dev@acme.com 1 1 1 n n '' '' '' '' '' 1 y FOO 'a"b\c' '' y | ctx --no-gum setup 2>&1)" \
      || { echo "setup failed: $out"; exit 1; }
    out="$(python3 -c 'import sys,tomllib; d=tomllib.load(open(sys.argv[1],"rb")); print(d["env"]["FOO"])' "$td/w/acme/mise.toml")" \
      || { echo "mise.toml does not parse: $(cat "$td/w/acme/mise.toml")"; exit 1; }
    [[ "$out" == 'a"b\c' ]] || { echo "FOO parsed as: $out"; exit 1; }
    # The enter hook passes the git name to git unchanged.
    mkdir -p "$td/gitlog"
    printf '#!/bin/sh\nprintf "%%s\\n" "$3" >>"%s"\n' "$td/gitlog/out" >"$td/gitlog/git"
    chmod +x "$td/gitlog/git"
    python3 -c 'import sys,tomllib; print(tomllib.load(open(sys.argv[1],"rb"))["hooks"]["enter"]["script"])' "$td/w/acme/mise.toml" \
      | PATH="$td/gitlog:/usr/bin:/bin" bash
    [[ "$(head -1 "$td/gitlog/out")" == "$name" ]] || { echo "hook git name: $(cat "$td/gitlog/out")"; exit 1; }
    rm -rf "$td"
  ) || fail "mise.toml values are escaped"
  pass "setup mise.toml escapes TOML strings and the hook's git name"
}

test_setup_no_secrets_bash32() {
  (
    set -euo pipefail
    local td out
    [[ -x /bin/bash ]] || { pass "setup under /bin/bash (skipped: no /bin/bash)"; exit 0; }
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    echo key >"$HOME/.ssh/id_c"
    cd "$td/w/acme"
    out="$(printf '%s\n' c1 1 "$HOME/.ssh/id_c" '' 1 y | /bin/bash "$ROOT_DIR/bin/ctx" --no-gum setup --scope clone 2>&1)" \
      || { echo "setup failed: $out"; exit 1; }
    ! grep -q 'unbound variable' <<<"$out" || { echo "unbound: $out"; exit 1; }
    [[ -f "$CTX_DIR/profiles/c1.conf" ]] || { echo "no profile: $out"; exit 1; }
    rm -rf "$td"
  ) || fail "setup with no secrets under /bin/bash"
  pass "setup with no secrets works on /bin/bash ($(/bin/bash -c 'echo $BASH_VERSION' 2>/dev/null))"
}

test_edit_editor_with_args() {
  (
    set -euo pipefail
    local td out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx"
    unset EDITOR VISUAL CTX_NO_GUM
    mkdir -p "$CTX_DIR/profiles" "$td/fake"
    echo 'GIT_EMAIL=a@b.co' >"$CTX_DIR/profiles/acme.conf"
    cat >"$td/fake/ed1" <<EOS
#!/bin/sh
{ printf 'ed'; printf ' [%s]' "\$@"; echo; } >>"$td/argv.log"
EOS
    cat >"$td/fake/ed2" <<EOS
#!/bin/sh
{ printf 'ed2'; printf ' [%s]' "\$@"; echo; } >>"$td/argv.log"
EOS
    mkdir "$td/sp ace"
    cp "$td/fake/ed1" "$td/sp ace/ed"
    chmod +x "$td/fake/ed1" "$td/fake/ed2" "$td/sp ace/ed"
    : >"$td/argv.log"
    EDITOR="$td/fake/ed1 -w" CTX_CORE=off /bin/bash "$ROOT_DIR/bin/ctx" --no-gum edit acme \
      || { echo "edit failed"; exit 1; }
    grep -qx "ed \[-w\] \[$CTX_DIR/profiles/acme.conf\]" "$td/argv.log" \
      || { echo "EDITOR with args: $(cat "$td/argv.log")"; exit 1; }
    : >"$td/argv.log"
    VISUAL="$td/fake/ed2" EDITOR="$td/fake/ed1" CTX_CORE=off /bin/bash "$ROOT_DIR/bin/ctx" --no-gum edit acme \
      || { echo "edit failed"; exit 1; }
    out="$(cat "$td/argv.log")"
    [[ "$out" == "ed2 [$CTX_DIR/profiles/acme.conf]" ]] || { echo "VISUAL should win: $out"; exit 1; }
    : >"$td/argv.log"
    EDITOR="\"$td/sp ace/ed\" -w" CTX_CORE=off /bin/bash "$ROOT_DIR/bin/ctx" --no-gum edit acme \
      || { echo "edit failed"; exit 1; }
    grep -qx "ed \[-w\] \[$CTX_DIR/profiles/acme.conf\]" "$td/argv.log" \
      || { echo "quoted path with a space: $(cat "$td/argv.log")"; exit 1; }
    # a failing editor: the exit status is the editor's
    printf '#!/bin/sh\nexit 7\n' >"$td/fake/bad"; chmod +x "$td/fake/bad"
    rc=0; EDITOR="$td/fake/bad" CTX_CORE=off /bin/bash "$ROOT_DIR/bin/ctx" --no-gum edit acme || rc=$?
    [[ $rc -eq 7 ]] || { echo "exit status $rc, want 7"; exit 1; }
    rm -rf "$td"
  ) || fail "edit runs EDITOR/VISUAL like git"
  pass "edit honors VISUAL, EDITOR with arguments"
}

test_clone_no_extra_args_bash32() {
  (
    set -euo pipefail
    local td
    [[ -x /bin/bash ]] || { pass "clone under /bin/bash (skipped: no /bin/bash)"; exit 0; }
    td="$(cd "$(mktemp -d)" && pwd -P)"
    export HOME="$td/home" CTX_DIR="$td/home/.ctx"
    unset CTX_NO_GUM
    mkdir -p "$CTX_DIR/profiles" "$td/fake"
    echo 'GIT_EMAIL=a@b.co' >"$CTX_DIR/profiles/acme.conf"
    printf '#!/bin/sh\n{ printf "git"; printf " [%%s]" "$@"; echo; } >>"%s/argv.log"\n' "$td" >"$td/fake/git"
    chmod +x "$td/fake/git"
    : >"$td/argv.log"
    out="$(PATH="$td/fake:$PATH" CTX_CORE=off /bin/bash "$ROOT_DIR/bin/ctx" --no-gum clone -p acme git@gitlab.com:o/r.git 2>&1)" \
      || { echo "clone failed: $out"; exit 1; }
    ! grep -q 'unbound variable' <<<"$out" || { echo "unbound: $out"; exit 1; }
    grep -qx 'git \[clone\] \[git@gitlab.com:o/r.git\]' "$td/argv.log" || { echo "argv: $(cat "$td/argv.log")"; exit 1; }
    : >"$td/argv.log"
    out="$(PATH="$td/fake:$PATH" CTX_CORE=off /bin/bash "$ROOT_DIR/bin/ctx" --no-gum clone -p acme -- git@gitlab.com:o/r.git 2>&1)" \
      || { echo "clone -- failed: $out"; exit 1; }
    grep -qx 'git \[clone\] \[git@gitlab.com:o/r.git\]' "$td/argv.log" || { echo "argv --: $(cat "$td/argv.log")"; exit 1; }
    out="$(PATH="$td/fake:$PATH" CTX_CORE=off /bin/bash "$ROOT_DIR/bin/ctx" --no-gum clone -p acme -- 2>&1)" && { echo "clone -- without url succeeded: $out"; exit 1; }
    [[ "$out" == *"Could not find a repo URL in: ctx clone ... -- "* && "$out" != *unbound* ]] || { echo "clone --: $out"; exit 1; }
    rm -rf "$td"
  ) || fail "clone with no extra git args under /bin/bash"
  pass "clone with no extra args works on /bin/bash ($(/bin/bash -c 'echo $BASH_VERSION' 2>/dev/null))"
}

test_verify_gh_account_and_exit_code() {
  (
    set -euo pipefail
    local td out rc
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    export HOME="$td/home" CTX_DIR="$td/home/.ctx"
    unset CTX_NO_GUM
    mkdir -p "$HOME/.ssh" "$HOME/work" "$CTX_DIR/profiles" "$td/fake"
    : >"$HOME/.ssh/id_acme"
    printf 'Host github-acme\n  HostName github.com\n' >"$HOME/.ssh/ctx_config"
    printf 'GIT_EMAIL=a@b.co\nGITHUB_USER=alice\nWORK_DIR=%s\nSSH_KEY_PATH=%s\n' "$HOME/work" "$HOME/.ssh/id_acme" >"$CTX_DIR/profiles/acme.conf"
    # ssh hangs (sleeps) unless it is told never to prompt
    cat >"$td/fake/ssh" <<'EOS'
#!/bin/sh
printf '%s\n' "$*" >>"$FAKE_LOG/ssh.log"
case " $* " in *" BatchMode=yes "*) ;; *) sleep 30 ;; esac
echo "Hi acme! You've successfully authenticated, but GitHub does not provide shell access."
exit 1
EOS
    cat >"$td/fake/gh" <<'EOS'
#!/bin/sh
cat <<EOT
github.com
  X Failed to log in to github.com account carol (keyring)
  - Active account: false
  ✓ Logged in to github.com account ${GH_FIRST:-bob} (keyring)
  - Active account: ${GH_FIRST_ACTIVE:-false}
  ✓ Logged in to github.com account ${GH_SECOND:-alice} (keyring)
  - Active account: ${GH_SECOND_ACTIVE:-true}
EOT
EOS
    chmod +x "$td/fake/ssh" "$td/fake/gh"
    export FAKE_LOG="$td" PATH="$td/fake:$PATH"

    rc=0; out="$("$ROOT_DIR/bin/ctx" --no-gum verify acme </dev/null 2>&1)" || rc=$?
    [[ $rc -eq 0 ]] || { echo "all ok: exit $rc: $out"; exit 1; }
    [[ "$out" == *"gh active user matches: alice"* && "$out" == *"verification passed"* ]] || { echo "all ok: $out"; exit 1; }
    grep -qx -- '-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -T git@github-acme' "$td/ssh.log" \
      || { echo "ssh argv: $(cat "$td/ssh.log")"; exit 1; }

    rc=0; out="$(GH_SECOND_ACTIVE=false GH_FIRST_ACTIVE=true "$ROOT_DIR/bin/ctx" --no-gum verify acme </dev/null 2>&1)" || rc=$?
    [[ $rc -eq 1 ]] || { echo "gh mismatch: exit $rc, want 1: $out"; exit 1; }
    [[ "$out" == *"gh active: 'bob', profile expects: 'alice'"* && "$out" == *"found issues"* ]] || { echo "gh mismatch: $out"; exit 1; }
  ) || fail "verify reads the active gh account and exits 1 on failure"
  pass "verify reads the active gh account and exits 1 on failure"
}

test_clone_dry_run() {
  (
    set -euo pipefail
    local td out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    export HOME="$td/home" CTX_DIR="$td/home/.ctx"
    unset CTX_NO_GUM
    mkdir -p "$CTX_DIR/profiles" "$td/fake"
    echo 'GIT_EMAIL=a@b.co' >"$CTX_DIR/profiles/acme.conf"
    printf '#!/bin/sh\necho ran >>"%s/git.ran"\n' "$td" >"$td/fake/git"
    chmod +x "$td/fake/git"
    out="$(PATH="$td/fake:$PATH" "$ROOT_DIR/bin/ctx" --no-gum --dry-run clone -p acme git@github.com:o/r.git 'my dir' 2>&1)" \
      || { echo "dry-run failed: $out"; exit 1; }
    [[ "$out" == *"[dry-run] would run: git clone git@github-acme:o/r.git my\\ dir"* ]] || { echo "dry-run: $out"; exit 1; }
    [[ ! -e "$td/git.ran" ]] || { echo "git ran"; exit 1; }
    out="$(PATH="$td/fake:$PATH" "$ROOT_DIR/bin/ctx" --no-gum --dry-run clone -p ghost git@github.com:o/r.git 2>&1)" && { echo "ghost: $out"; exit 1; }
    [[ ! -e "$td/git.ran" ]] || { echo "git ran"; exit 1; }
  ) || fail "clone --dry-run prints the git command and changes nothing"
  pass "clone --dry-run prints the git command and changes nothing"
}

test_setup_cancel_exits_130() {
  (
    set -euo pipefail
    local td before rc n input
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    cd "$td/w/acme"
    before="$(_tree_digest "$td/home"; _tree_digest "$td/w")"
    # EOF at: the first prompt (scope picker), the profile name (ask in $(…)),
    # and a later ask_yn ("No SSH keys found. Generate one…?", main shell).
    for input in '' $'2\n' $'2\nacme\nDev\ndev@acme.com\n1\n1\n1\n'; do
      rc=0
      printf '%s' "$input" | ctx --no-gum setup >"$td/out" 2>"$td/err" || rc=$?
      [[ "$rc" == 130 ]] || { echo "rc=$rc for input $(printf %q "$input"): $(cat "$td/err")"; exit 1; }
      n="$(grep -c 'Cancelled by user\.' "$td/err" || true)"
      [[ "$n" == 1 ]] || { echo "message printed $n times: $(cat "$td/err")"; exit 1; }
      [[ "$(_tree_digest "$td/home"; _tree_digest "$td/w")" == "$before" ]] || { echo "files changed"; find "$td/home" "$td/w"; exit 1; }
    done
    rm -rf "$td"
  ) || fail "EOF at a setup prompt exits 130"
  pass "setup EOF at a prompt prints one cancel line, exits 130, writes nothing"
}

# user_cancelled in $(…) when the main shell is already waiting for the
# subshell (its stdout closed first): bash drops a SIGINT that arrives then if
# the child exits normally, so the child must die by SIGINT for the trap to run.
test_user_cancelled_while_main_waits() {
  local rc out sh
  for sh in bash /bin/bash; do
    [[ "$sh" == bash || -x "$sh" ]] || continue
    rc=0
    out="$("$sh" -c 'source "$1/lib/core.sh"; trap "exit 130" INT
      x="$(exec >&-; sleep 0.3; user_cancelled 2>/dev/null)"
      echo "continued with x=[$x]"' _ "$ROOT_DIR" 2>&1)" || rc=$?
    [[ "$rc" == 130 && -z "$out" ]] || fail "user_cancelled in \$(…) while main waits ($sh): rc=$rc out=$out"
  done
  pass "user_cancelled in \$(…) stops the main shell even when it is already waiting"
}

# R25: a failing ssh-keygen in the wizard stops setup (doctor hint, exit 1).
test_setup_keygen_failure_dies() {
  (
    set -euo pipefail
    local td out rc=0
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    printf '#!/bin/sh\necho "ssh-keygen: boom" >&2\nexit 1\n' >"$td/fake/ssh-keygen"
    cd "$td/w/acme"
    # clone scope: name, generate, passphrase? n
    out="$(printf '%s\n' acme 2 n '' '' 1 y | ctx --no-gum setup --scope clone 2>&1)" || rc=$?
    [[ "$rc" == 1 ]] || { echo "rc=$rc: $out"; exit 1; }
    grep -qF "ssh-keygen failed for $HOME/.ssh/ctx_acme" <<<"$out" || { echo "no die line: $out"; exit 1; }
    grep -qF 'ctx doctor' <<<"$out" || { echo "no doctor hint: $out"; exit 1; }
    ! grep -q 'SSH key generated' <<<"$out" || { echo "claims generated: $out"; exit 1; }
    [[ ! -e "$CTX_DIR" ]] || { echo "wrote ~/.ctx"; exit 1; }
    rm -rf "$td"
  ) || fail "ssh-keygen failure in the wizard dies"
  pass "setup wizard stops with a doctor hint when ssh-keygen fails"
}

# R17: a rejected --config leaves no WORK_DIR and no generated key behind.
test_setup_batch_checks_before_side_effects() {
  (
    set -euo pipefail
    local td cfg core="${CTX_CORE:-}" rc
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    export CTX_CORE="$core"
    cfg="$td/s.conf"
    printf 'PROFILE_NAME=acme\nGIT_NAME=Dev\nWORK_DIR=%s\nSSH_MODE=generate\nSIGNING_FORMAT=zzz\n' "$td/w/new" >"$cfg"
    set +e
    ctx --no-gum setup --config "$cfg" --yes >/dev/null 2>"$td/err"
    rc=$?
    set -e
    [[ $rc -eq 1 ]] || { echo "rc=$rc"; exit 1; }
    grep -q 'SIGNING_FORMAT must be ssh|gpg|none' "$td/err" || { echo "err: $(cat "$td/err")"; exit 1; }
    [[ ! -e "$td/w/new" ]] || { echo "WORK_DIR created"; exit 1; }
    [[ ! -e "$HOME/.ssh/ctx_acme" ]] || { echo "key generated"; exit 1; }
    ! grep -q ssh-keygen "$td/argv.log" || { echo "ssh-keygen ran"; exit 1; }
    rm -rf "$td"
  ) || fail "setup --config checks before side effects"
  pass "setup --config validates the whole file before creating WORK_DIR or a key"
}

# R18: quoted --config values work on bash 3.2 (no negative substring length).
test_setup_cfg_quoted_bash32() {
  (
    set -euo pipefail
    local td cfg out
    [[ -x /bin/bash ]] || { pass "quoted --config values (skipped: no /bin/bash)"; exit 0; }
    td="$(cd "$(mktemp -d)" && pwd -P)"
    _wizard_sandbox "$td"
    cfg="$td/s.conf"
    printf 'PROFILE_NAME='"'"'acme'"'"'\nGIT_NAME="Jane Doe"\nGIT_EMAIL="jane@acme.com"\nWORK_DIR="%s"\nSSH_MODE=skip\n' "$td/w/acme" >"$cfg"
    out="$(/bin/bash "$ROOT_DIR/bin/ctx" --no-gum setup --config "$cfg" --yes 2>&1)" || { echo "setup failed: $out"; exit 1; }
    grep -qx 'GIT_NAME=Jane\\ Doe' "$CTX_DIR/profiles/acme.conf" || { echo "conf: $(cat "$CTX_DIR/profiles/acme.conf")"; exit 1; }
    grep -qx "WORK_DIR=$td/w/acme" "$CTX_DIR/profiles/acme.conf" || { echo "conf: $(cat "$CTX_DIR/profiles/acme.conf")"; exit 1; }
    rm -rf "$td"
  ) || fail "quoted --config values on /bin/bash"
  pass "quoted --config values work on /bin/bash ($(/bin/bash -c 'echo $BASH_VERSION' 2>/dev/null))"
}

test_mise_hook_comment_no_injection() {
  (
    set -euo pipefail
    local td
    td="$(cd "$(mktemp -d)" && pwd -P)"
    mkdir -p "$td/w" "$td/run"
    # shellcheck source=../lib/core.sh
    source "$ROOT_DIR/lib/core.sh"
    generate_mise_toml "acme" "$td/w" "Dev" $'bad\ntouch PWNED\r\ttouch PWNED2' "" "" "" "" "" ""
    if python3 -c 'import tomllib' 2>/dev/null; then
      python3 -c 'import sys,tomllib; print(tomllib.load(open(sys.argv[1],"rb"))["hooks"]["enter"]["script"])' "$td/w/mise.toml" >"$td/hook.sh" \
        || { echo "mise.toml does not parse: $(cat "$td/w/mise.toml")"; exit 1; }
    else
      awk '/^script = """/{f=1; next} f && /^"""/{exit} f' "$td/w/mise.toml" | sed 's/\\u000A/\n/g' >"$td/hook.sh"
    fi
    (cd "$td/run" && PATH="/usr/bin:/bin" bash "$td/hook.sh" >/dev/null 2>&1) || true
    [[ ! -e "$td/run/PWNED" && ! -e "$td/run/PWNED2" ]] || { echo "comment injection ran: $(cat "$td/hook.sh")"; exit 1; }
    grep -q 'address): "bad touch PWNED  touch PWNED2"' "$td/hook.sh" || { echo "comment text: $(cat "$td/hook.sh")"; exit 1; }
    rm -rf "$td"
  ) || fail "mise hook comment lines cannot inject commands"
  pass "mise.toml hook comments flatten control characters"
}


# Octal mode of a file (GNU stat, else BSD stat).
_t_mode() { stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1"; }

test_undo_lists_backs_up_and_keeps_mode() {
  (
    set -euo pipefail
    local td out rc b1 nb
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" GIT_CONFIG_NOSYSTEM=1
    unset CTX_NO_GUM
    mkdir -p "$HOME/.ssh" "$CTX_DIR/backups"
    b1="$CTX_DIR/backups/20200101-000000"
    mkdir -p "$b1"
    printf 'new git\n' >"$HOME/.gitconfig"
    printf 'new secret\n' >"$HOME/.secret"; chmod 644 "$HOME/.secret"
    printf 'old git\n' >"$b1/.gitconfig"
    printf 'old secret\n' >"$b1/.secret"; chmod 600 "$b1/.secret"
    printf 'gone\n' >"$b1/file.txt"
    printf '%s\n' "$HOME/.gitconfig" "$HOME/.secret" "$HOME/gone/file.txt" "$HOME/.missing" >"$b1/manifest.txt"

    # dry-run: lists, changes nothing, exits 0
    out="$("$ROOT_DIR/bin/ctx" --no-gum --dry-run undo </dev/null 2>&1)" || { echo "dry-run exit: $out"; exit 1; }
    [[ "$out" == *"  $HOME/.gitconfig"* && "$out" == *"[dry-run] would restore $HOME/.secret"* ]] || { echo "dry-run: $out"; exit 1; }
    [[ "$(cat "$HOME/.gitconfig")" == "new git" && "$(ls "$CTX_DIR/backups")" == 20200101-000000 ]] || { echo "dry-run changed files"; exit 1; }

    rc=0; out="$(printf 'y\n' | "$ROOT_DIR/bin/ctx" --no-gum undo 2>&1)" || rc=$?
    [[ $rc -eq 1 ]] || { echo "exit $rc, want 1 (skipped files): $out"; exit 1; }
    [[ "$out" == *"Cannot restore $HOME/gone/file.txt: $HOME/gone no longer exists; skipped"* ]] || { echo "gone dir: $out"; exit 1; }
    [[ "$out" == *"Not in backup: $HOME/.missing"* ]] || { echo "missing: $out"; exit 1; }
    # the list comes before the prompt
    [[ "$out" == *"  $HOME/.secret"*"Restore these files?"* ]] || { echo "list before prompt: $out"; exit 1; }
    [[ "$(cat "$HOME/.gitconfig")" == "old git" && "$(cat "$HOME/.secret")" == "old secret" ]] || { echo "not restored"; exit 1; }
    [[ "$(_t_mode "$HOME/.secret")" == 600 ]] || { echo "mode $(_t_mode "$HOME/.secret"), want 600"; exit 1; }
    [[ ! -e "$HOME/gone" ]] || { echo "created gone/"; exit 1; }
    nb="$(ls -1t "$CTX_DIR/backups" | head -1)"
    [[ "$nb" != 20200101-000000 && "$(cat "$CTX_DIR/backups/$nb/.gitconfig")" == "new git" ]] || { echo "no pre-restore backup: $nb"; exit 1; }
    [[ "$out" == *"The replaced versions are in $CTX_DIR/backups/$nb"* ]] || { echo "backup hint: $out"; exit 1; }

    # the undo can be undone
    printf 'y\n' | "$ROOT_DIR/bin/ctx" --no-gum undo >/dev/null 2>&1 || { echo "second undo failed"; exit 1; }
    [[ "$(cat "$HOME/.gitconfig")" == "new git" && "$(_t_mode "$HOME/.secret")" == 644 ]] || { echo "undo of undo"; exit 1; }
  ) || fail "undo lists files, backs up the current versions and keeps the backup's mode"
  pass "undo lists files, backs up the current versions and keeps the backup's mode"
}

test_undo_ambiguous_and_same_second() {
  (
    set -euo pipefail
    local td out rc b1 fake
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    export HOME="$td/home" CTX_DIR="$td/home/.ctx"
    unset CTX_NO_GUM
    mkdir -p "$HOME/.ssh" "$CTX_DIR" "$td/fake"
    b1="$CTX_DIR/backups/20261020-101010"
    mkdir -p "$b1"
    printf 'ssh now\n' >"$HOME/.ssh/config"
    printf 'ctx now\n' >"$CTX_DIR/config"
    printf 'git now\n' >"$HOME/.gitconfig"
    printf 'one of them\n' >"$b1/config"
    printf 'git old\n' >"$b1/.gitconfig"
    printf '%s\n' "$HOME/.ssh/config" "$CTX_DIR/config" "$HOME/.gitconfig" "$HOME/.gitconfig" >"$b1/manifest.txt"
    # a date frozen at the backup's own stamp: the new backup must not land in it
    printf '#!/bin/sh\nif [ "$1" = "+%%Y%%m%%d-%%H%%M%%S" ]; then echo 20261020-101010; exit 0; fi\nexec /bin/date "$@"\n' >"$td/fake/date"
    chmod +x "$td/fake/date"
    rc=0; out="$(printf 'y\n' | PATH="$td/fake:$PATH" "$ROOT_DIR/bin/ctx" --no-gum undo 2>&1)" || rc=$?
    [[ $rc -eq 1 ]] || { echo "exit $rc: $out"; exit 1; }
    [[ "$out" == *"Ambiguous backup for $HOME/.ssh/config"* && "$out" == *"Ambiguous backup for $CTX_DIR/config"* ]] || { echo "ambiguous: $out"; exit 1; }
    [[ "$(cat "$HOME/.ssh/config")" == "ssh now" && "$(cat "$CTX_DIR/config")" == "ctx now" ]] || { echo "ambiguous file overwritten"; exit 1; }
    [[ "$(cat "$HOME/.gitconfig")" == "git old" ]] || { echo "gitconfig not restored"; exit 1; }
    [[ "$(grep -c . "$b1/manifest.txt")" -eq 4 ]] || { echo "restored backup was written into"; exit 1; }
    # Bash's date is frozen (-2 suffix); Go's clock is not, so accept any new dir.
    fake="$(ls -1t "$CTX_DIR/backups" | head -1)"
    [[ "$fake" != 20261020-101010 ]] || { echo "pre-restore backup went into the restored dir"; exit 1; }
    [[ "${CTX_CORE:-}" != off ]] || [[ "$fake" == 20261020-101010-2 ]] || { echo "same-second dir: $fake"; exit 1; }
    fake="$CTX_DIR/backups/$fake"
    [[ "$(cat "$fake/.gitconfig")" == "git now" && "$(cat "$fake/manifest.txt")" == "$HOME/.gitconfig" ]] || { echo "same-second backup dir: $(ls "$CTX_DIR/backups")"; exit 1; }
    [[ "$(echo "$out" | grep -c "Restored: $HOME/.gitconfig")" -eq 1 ]] || { echo "duplicate manifest line restored twice: $out"; exit 1; }
  ) || fail "undo skips same-name files and never writes into the backup it restores"
  pass "undo skips same-name files and never writes into the backup it restores"
}

# _t_fake_install DIR: a ctx install like install.sh makes (bin/ctx with the
# CTX_LIB line rewritten, lib/ctx with the scripts). ctx-core goes next to the
# binary: the real build when there is one and CTX_CORE is not off (so the Go
# command runs), else a stub that implements nothing.
_t_fake_install() {
  local d="$1"
  mkdir -p "$d/bin" "$d/lib/ctx"
  sed "9s|^CTX_LIB=.*|CTX_LIB=\"$d/lib/ctx\"|" "$ROOT_DIR/bin/ctx" >"$d/bin/ctx"
  chmod 755 "$d/bin/ctx"
  cp "$ROOT_DIR"/lib/*.sh "$ROOT_DIR"/lib/*.bash "$ROOT_DIR"/lib/*.fish "$d/lib/ctx/"
  if [[ "${CTX_CORE:-}" != off && -x "$ROOT_DIR/bin/ctx-core" ]]; then
    cp "$ROOT_DIR/bin/ctx-core" "$d/bin/ctx-core"
  else
    printf '#!/bin/sh\nexit 1\n' >"$d/bin/ctx-core"
    chmod 755 "$d/bin/ctx-core"
  fi
}

test_uninstall_removes_install_and_purges() {
  (
    set -euo pipefail
    local td out p
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" GIT_CONFIG_GLOBAL="$td/home/.gitconfig" GIT_CONFIG_NOSYSTEM=1
    export CTX_SECRET_PROVIDER=file PATH="/usr/bin:/bin:/usr/sbin:/sbin"
    unset CTX_NO_GUM CTX_CORE_BIN
    p="$td/prefix"
    _t_fake_install "$p"
    mkdir -p "$CTX_DIR/profiles" "$CTX_DIR/secrets/acme"
    printf 'PROFILE_NAME=acme\n' >"$CTX_DIR/profiles/acme.conf"
    printf 's3cret' >"$CTX_DIR/secrets/acme/API_KEY"
    export PATH="$p/bin:$PATH"
    [[ "$(command -v ctx)" == "$p/bin/ctx" ]] || { echo "ctx on PATH: $(command -v ctx)"; exit 1; }

    out="$(ctx --no-gum --dry-run uninstall --purge </dev/null 2>&1)" || { echo "dry-run: $out"; exit 1; }
    [[ "$out" == *"[dry-run] would remove $p/lib/ctx"* && "$out" == *"[dry-run] would delete 1 secret(s) of acme from the file store"* ]] || { echo "dry-run: $out"; exit 1; }
    [[ -f "$p/bin/ctx" && -d "$p/lib/ctx" && -d "$CTX_DIR" ]] || { echo "dry-run removed files"; exit 1; }

    out="$(printf 'n\n' | ctx --no-gum uninstall 2>&1)" && { echo "No should fail: $out"; exit 1; }
    [[ -f "$p/bin/ctx" ]] || { echo "No removed files"; exit 1; }

    # a secret that cannot be deleted stops everything
    chmod 500 "$CTX_DIR/secrets/acme"
    out="$(printf 'y\n' | ctx --no-gum uninstall --purge 2>&1)" && { echo "left secret should fail: $out"; exit 1; }
    [[ "$out" == *"Could not delete from the file store for acme: API_KEY"* && -f "$p/bin/ctx" && -d "$CTX_DIR" ]] \
      || { echo "secret failure: $out"; exit 1; }
    chmod 700 "$CTX_DIR/secrets/acme"
    # a store that cannot be listed stops everything too
    chmod 000 "$CTX_DIR/secrets/acme"
    out="$(printf 'y\n' | ctx --no-gum uninstall --purge 2>&1)" && { echo "unlistable store should fail: $out"; exit 1; }
    [[ "$out" == *"Could not list the secrets of acme in the file store"* && -f "$p/bin/ctx" && -d "$CTX_DIR" ]] \
      || { echo "list failure: $out"; exit 1; }
    chmod 700 "$CTX_DIR/secrets/acme"

    out="$(printf 'y\n' | ctx --no-gum uninstall --purge 2>&1)" || { echo "uninstall: $out"; exit 1; }
    [[ ! -e "$p/bin/ctx" && ! -e "$p/bin/ctx-core" && ! -e "$p/lib/ctx" && ! -e "$CTX_DIR" ]] || { echo "left behind: $(find "$td" | head -20)"; exit 1; }
    [[ -d "$p/lib" && -d "$HOME" ]] || { echo "removed too much"; exit 1; }
    [[ "$out" == *"Deleted 1 secret(s) of acme from the file store"* ]] || { echo "secrets: $out"; exit 1; }
    [[ "$out" == *"# BEGIN ctx profile autoswitch"* ]] || { echo "hint: $out"; exit 1; }
  ) || fail "uninstall removes ctx, ctx-core and the lib dir; --purge the data and secrets"
  pass "uninstall removes ctx, ctx-core and the lib dir; --purge the data and secrets"
}

test_uninstall_refuses_unsafe_paths() {
  (
    set -euo pipefail
    local td out rc p before
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" GIT_CONFIG_GLOBAL="$td/home/.gitconfig" GIT_CONFIG_NOSYSTEM=1
    export PATH="/usr/bin:/bin:/usr/sbin:/sbin" CTX_SECRET_PROVIDER=file # never the real install
    unset CTX_NO_GUM CTX_CORE_BIN
    p="$td/prefix"
    _t_fake_install "$p"
    mkdir -p "$CTX_DIR/profiles" "$td/links" "$td/checkout/bin" "$td/checkout/.git" "$td/x" "$td/z/home"
    ln -s "$p/bin/ctx" "$td/links/ctx"
    cp "$ROOT_DIR/bin/ctx" "$td/checkout/bin/ctx"; cp -R "$ROOT_DIR/lib" "$td/checkout/lib"
    # z/l/../home is z/home (a ctx dir) lexically, but HOME for the kernel (l -> x).
    ln -s "$td/x" "$td/z/l"; : >"$td/z/home/config"
    ln -s "$CTX_DIR" "$HOME/link.ctx"
    before="$(cd "$td" && find . | LC_ALL=C sort)"
    # _refuse ANSWER WANT CTX PATH-DIR [VAR=value...]: a refusal before any
    # prompt, the list of what to remove by hand, and no change. Inside the
    # sandbox it answers y (typed ahead: the refusal must come before any
    # deletion); a CTX_DIR outside it answers n, so a missing guard would stop
    # at the prompt instead of deleting (CTX_DIR=/).
    _refuse() {
      local answer="$1" want="$2" bin="$3" pre="$4"; shift 4
      rc=0; out="$(printf '%s\n' "$answer" | env PATH="$pre:$PATH" "$@" "$bin" --no-gum uninstall --purge 2>&1)" || rc=$?
      [[ $rc -eq 1 && "$out" == *"$want"* ]] || { echo "want refusal '$want', exit $rc: $out"; exit 1; }
      [[ "$out" != *"Continue?"* ]] || { echo "asked before refusing: $out"; exit 1; }
      local on_path; on_path="$(PATH="$pre:$PATH" command -v ctx)"
      [[ "$out" == *"To uninstall by hand, remove:"$'\n'"  $on_path"$'\n'*"ctx secret delete <profile> <KEY>"* ]] \
        || { echo "manual list: $out"; exit 1; }
      [[ "$(cd "$td" && find . | LC_ALL=C sort)" == "$before" ]] || { echo "files changed after: $want"; exit 1; }
    }
    _refuse y "is a symlink" "$td/links/ctx" "$td/links"
    _refuse y "no installer CTX_LIB line" "$td/checkout/bin/ctx" "$td/checkout/bin"
    _refuse y "is not the one running" "$td/checkout/bin/ctx" "$p/bin"
    _refuse n "Refusing to remove /: it is /" "$p/bin/ctx" "$p/bin" CTX_DIR=/
    _refuse y "Refusing to remove $HOME: it is your home directory" "$p/bin/ctx" "$p/bin" CTX_DIR="$HOME"
    _refuse y "does not look like a ctx data directory" "$p/bin/ctx" "$p/bin" CTX_DIR="$td/checkout"
    _refuse y "it has a . or .. component" "$p/bin/ctx" "$p/bin" CTX_DIR="$td/z/l/../home"
    _refuse y "it is a symlink" "$p/bin/ctx" "$p/bin" CTX_DIR="$HOME/link.ctx//"
    _refuse y "the pass password store (pass) is not available here" "$p/bin/ctx" "$p/bin" CTX_SECRET_PROVIDER=pass
    # a data dir that holds the install; a newline in CTX_DIR; and, on a
    # case-insensitive file system (macOS default), HOME spelled another way
    mkdir -p "$p/profiles" "$HOME/profiles" "$HOME/nl"$'\n'
    before="$(cd "$td" && find . | LC_ALL=C sort)"
    _refuse y "it holds the ctx install ($p/bin)" "$p/bin/ctx" "$p/bin" CTX_DIR="$p"
    _refuse y "it contains a newline" "$p/bin/ctx" "$p/bin" CTX_DIR="$HOME/nl"$'\n'
    if [[ -e "$td/HOME" ]]; then
      _refuse y "Refusing to remove $td/HOME: it is your home directory or holds it" "$p/bin/ctx" "$p/bin" CTX_DIR="$td/HOME"
    fi
    # a hand-edited CTX_LIB pointing at HOME (with the scripts in it, so ctx runs)
    cp "$ROOT_DIR"/lib/*.sh "$HOME/"
    sed -i.bak "9s|^CTX_LIB=.*|CTX_LIB=\"$HOME\"|" "$p/bin/ctx" && rm -f "$p/bin/ctx.bak"
    before="$(cd "$td" && find . | LC_ALL=C sort)"
    _refuse y "Refusing to remove $HOME: it is your home directory" "$p/bin/ctx" "$p/bin"
  ) || fail "uninstall refuses symlinks, checkouts, .., /, HOME and odd data dirs"
  pass "uninstall refuses symlinks, checkouts, .., /, HOME and odd data dirs"
}

test_install_hook_keeps_rc() {
  (
    set -euo pipefail
    local td out rc rule before want
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'chmod -R u+w "$td" 2>/dev/null; rm -rf "$td"' EXIT
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" SHELL=/bin/bash
    mkdir -p "$HOME/.ssh" "$HOME/dots" "$td/fake"
    printf '#!/bin/sh\nexit 0\n' >"$td/fake/mise"; chmod +x "$td/fake/mise"
    export PATH="$td/fake:$PATH"
    rule='# ─────────────────────────────────────────────'
    _hook() { "$ROOT_DIR/bin/ctx" --no-gum install-hook "$@" </dev/null 2>&1; }

    # a legacy pasted block ends at its closing rule: what follows stays
    printf 'top\n# ── ctx auto-switch ──────\n_ctx_auto_switch() { :; }\n%s\nafter=1\n\nlast=2\n' "$rule" >"$HOME/.zshrc"
    out="$(_hook "$HOME/.zshrc")" || { echo "legacy rule: $out"; exit 1; }
    grep -q '^after=1$' "$HOME/.zshrc" && grep -q '^last=2$' "$HOME/.zshrc" || { echo "lost the rest: $(cat "$HOME/.zshrc")"; exit 1; }
    ! grep -q '_ctx_auto_switch()' "$HOME/.zshrc" || { echo "legacy kept: $(cat "$HOME/.zshrc")"; exit 1; }
    # the mise line follows the rc file's shell (zsh), not $SHELL (bash)
    grep -qx 'eval "$(mise activate zsh)"' "$HOME/.zshrc" || { echo "mise line: $(cat "$HOME/.zshrc")"; exit 1; }
    [[ "$(grep -c '^# BEGIN ctx profile autoswitch$' "$HOME/.zshrc")" == 1 ]] || { echo "block count"; exit 1; }

    # a legacy block without a closing rule ends at the next blank line
    printf 'top\n# ctx profile autoswitch — old paste\nold=1\n\nkeep=1\n' >"$HOME/.bashrc"
    out="$(_hook "$HOME/.bashrc")" || { echo "legacy blank: $out"; exit 1; }
    grep -q '^keep=1$' "$HOME/.bashrc" && ! grep -q '^old=1$' "$HOME/.bashrc" || { echo "blank end: $(cat "$HOME/.bashrc")"; exit 1; }
    grep -qx 'eval "$(mise activate bash)"' "$HOME/.bashrc" || { echo "bash mise line"; exit 1; }
    # rerun: one block, the mise line once
    out="$(_hook "$HOME/.bashrc")" || { echo "rerun: $out"; exit 1; }
    [[ "$(grep -c '^# BEGIN ctx profile autoswitch$' "$HOME/.bashrc")" == 1 && "$(grep -c 'mise activate' "$HOME/.bashrc")" == 1 ]] \
      || { echo "rerun: $(cat "$HOME/.bashrc")"; exit 1; }

    # BEGIN without END: refused, nothing changed, no backup
    printf 'a\n# BEGIN ctx profile autoswitch\nb\n' >"$HOME/.zshrc"
    rm -rf "$CTX_DIR/backups"
    rc=0; out="$(_hook "$HOME/.zshrc")" || rc=$?
    [[ $rc -eq 1 && "$out" == *"Unterminated ctx block in $HOME/.zshrc"* ]] || { echo "unterminated: $rc $out"; exit 1; }
    [[ "$(cat "$HOME/.zshrc")" == $'a\n# BEGIN ctx profile autoswitch\nb' && ! -d "$CTX_DIR/backups" ]] || { echo "unterminated changed"; exit 1; }

    # symlinked rc: the link stays, the target is written, its mode kept; CRLF lines stay
    printf 'one\r\n# BEGIN ctx profile autoswitch\r\nold\r\n# END ctx profile autoswitch\r\ntwo\r\n' >"$HOME/dots/zshrc"
    chmod 640 "$HOME/dots/zshrc"
    ln -sf dots/zshrc "$HOME/.zshrc"
    out="$(_hook "$HOME/.zshrc")" || { echo "symlink: $out"; exit 1; }
    [[ -L "$HOME/.zshrc" && "$(readlink "$HOME/.zshrc")" == dots/zshrc ]] || { echo "link replaced"; exit 1; }
    [[ "$(_t_mode "$HOME/dots/zshrc")" == 640 ]] || { echo "mode $(_t_mode "$HOME/dots/zshrc")"; exit 1; }
    [[ "$(head -1 "$HOME/dots/zshrc")" == $'one\r' && "$(grep -c $'\r$' "$HOME/dots/zshrc")" == 2 ]] && grep -q $'^two\r$' "$HOME/dots/zshrc" || { echo "crlf: $(od -c "$HOME/dots/zshrc" | head)"; exit 1; }
    ! grep -q '^old' "$HOME/dots/zshrc" || { echo "old block kept"; exit 1; }

    # read-only rc: refused, untouched
    printf 'ro\n' >"$HOME/ro.zshrc"; chmod 444 "$HOME/ro.zshrc"
    rc=0; out="$(_hook "$HOME/ro.zshrc")" || rc=$?
    [[ $rc -eq 1 && "$out" == *"Cannot write $HOME/ro.zshrc"* && "$(cat "$HOME/ro.zshrc")" == ro ]] || { echo "read-only: $rc $out"; exit 1; }

    # config.fish gets the fish mise line and the fish block
    mkdir -p "$HOME/.config/fish"
    out="$(_hook "$HOME/.config/fish/config.fish")" || { echo "fish: $out"; exit 1; }
    grep -qx 'mise activate fish | source' "$HOME/.config/fish/config.fish" || { echo "fish mise"; exit 1; }
    grep -q 'ctx_autoswitch.fish' "$HOME/.config/fish/config.fish" || { echo "fish block"; exit 1; }

    # dry-run: says what it would do, changes nothing
    printf 'x\n' >"$HOME/.zshrc.dry"
    before="$(cd "$HOME" && find . -exec ls -ld {} + | LC_ALL=C sort; cat "$HOME/.zshrc.dry")"
    out="$("$ROOT_DIR/bin/ctx" --no-gum --dry-run install-hook "$HOME/.zshrc.dry" </dev/null 2>&1)" || { echo "dry: $out"; exit 1; }
    [[ "$out" == *"[dry-run] would add mise activation to $HOME/.zshrc.dry"* && "$out" == *"[dry-run] would refresh the ctx profile autoswitch hook in $HOME/.zshrc.dry"* ]] || { echo "dry out: $out"; exit 1; }
    [[ "$(cd "$HOME" && find . -exec ls -ld {} + | LC_ALL=C sort; cat "$HOME/.zshrc.dry")" == "$before" ]] || { echo "dry-run changed files"; exit 1; }
    # init --dry-run asks nothing and changes nothing
    out="$("$ROOT_DIR/bin/ctx" --no-gum --dry-run init </dev/null 2>&1)" || { echo "init dry: $out"; exit 1; }
    [[ "$out" != *"?"*"[Y/n]"* && "$out" == *"[dry-run] would"* ]] || { echo "init dry out: $out"; exit 1; }
    [[ "$(cd "$HOME" && find . -exec ls -ld {} + | LC_ALL=C sort; cat "$HOME/.zshrc.dry")" == "$before" ]] || { echo "init dry-run changed files"; exit 1; }

    # a missing autoswitch lib stops it before any edit
    mkdir -p "$td/inst/bin"; cp "$ROOT_DIR/bin/ctx" "$td/inst/bin/"; cp -R "$ROOT_DIR/lib" "$td/inst/lib"
    rm -f "$td/inst/lib/ctx_autoswitch.bash"
    printf 'keep\n' >"$HOME/.nolib"; rm -rf "$CTX_DIR/backups"
    # the copy has no ctx-core next to it: point it at the repo's (when that mode runs)
    local core=()
    [[ "${CTX_CORE:-}" != off && -x "$ROOT_DIR/bin/ctx-core" ]] && core=(CTX_CORE_BIN="$ROOT_DIR/bin/ctx-core")
    rc=0; out="$(env "${core[@]+"${core[@]}"}" "$td/inst/bin/ctx" --no-gum install-hook "$HOME/.nolib" </dev/null 2>&1)" || rc=$?
    want="Missing $td/inst/lib/ctx_autoswitch.bash — reinstall ctx."
    [[ $rc -eq 1 && "$out" == *"$want"* && "$out" != *"mise activation added"* ]] || { echo "missing lib: $rc $out"; exit 1; }
    [[ "$(cat "$HOME/.nolib")" == keep && ! -d "$CTX_DIR/backups" ]] || { echo "missing lib edited: $(cat "$HOME/.nolib")"; exit 1; }
  ) || fail "install-hook keeps the rest of the rc file, its link and mode"
  pass "install-hook keeps the rest of the rc file, its link and mode"
}

test_rc_writes_checked() {
  (
    set -euo pipefail
    local td out rc i sum
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" SHELL=/bin/zsh TMPDIR="$td/tmp"
    mkdir -p "$HOME/.ssh" "$TMPDIR"
    for i in $(seq 1 300); do printf 'export LINE_%03d=xxxxxxxxxxxxxxxxx\n' "$i"; done >"$HOME/.zshrc"
    cp "$HOME/.zshrc" "$HOME/AGENTS.md"
    sum="$(cksum <"$HOME/.zshrc")"
    # A short write into the temp file (TMPDIR full) must not replace the ~9 KB
    # file: exit 1, file as it was, no temp left. Modelled by a 4 KiB soft file
    # size limit (SIGXFSZ ignored) for the shell, while cp and cat lift it again,
    # as writes to the target's own file system would still succeed.
    local tool
    mkdir -p "$td/lift"
    for tool in cp cat; do
      printf '#!/bin/bash\nulimit -S -f unlimited\nexec %s "$@"\n' "$(command -v "$tool")" >"$td/lift/$tool"
      chmod +x "$td/lift/$tool"
    done
    export PATH="$td/lift:$PATH"
    rc=0; out="$(trap '' XFSZ; ulimit -S -f 4; "$ROOT_DIR/bin/ctx" --no-gum install-hook "$HOME/.zshrc" </dev/null 2>&1)" || rc=$?
    [[ $rc -eq 1 && "$out" == *"Cannot write $HOME/.zshrc"* ]] || { echo "install-hook short write: exit $rc: $out"; exit 1; }
    [[ "$(cksum <"$HOME/.zshrc")" == "$sum" ]] || { echo "rc changed after a failed write"; exit 1; }
    rc=0; out="$(cd "$HOME" && trap '' XFSZ; ulimit -S -f 4; "$ROOT_DIR/bin/ctx" --no-gum agent-rules --write AGENTS.md </dev/null 2>&1)" || rc=$?
    [[ $rc -eq 1 && "$out" == *"Cannot write AGENTS.md"* ]] || { echo "agent-rules short write: exit $rc: $out"; exit 1; }
    [[ "$(cksum <"$HOME/AGENTS.md")" == "$sum" ]] || { echo "AGENTS.md changed after a failed write"; exit 1; }
    [[ -z "$(ls -A "$TMPDIR")" ]] || { echo "temp files left: $(ls -A "$TMPDIR")"; exit 1; }
    local left
    left="$(cd "$HOME" && find . -mindepth 1 -maxdepth 1 ! -name .zshrc ! -name AGENTS.md ! -name .ssh ! -name .ctx)"
    [[ -z "$left" ]] || { echo "temp next to target: $left"; exit 1; }
    # a refusal (die) also removes the temp file
    printf '# BEGIN ctx profile autoswitch\n' >"$HOME/.zshrc"
    "$ROOT_DIR/bin/ctx" --no-gum install-hook "$HOME/.zshrc" </dev/null >/dev/null 2>&1 && { echo "unterminated accepted"; exit 1; }
    [[ -z "$(ls -A "$TMPDIR")" ]] || { echo "temp left after die: $(ls -A "$TMPDIR")"; exit 1; }
  ) || fail "install-hook and agent-rules never replace a file after a short write"
  pass "install-hook and agent-rules never replace a file after a short write"
}

test_installer_runs_install_hook() {
  (
    set -euo pipefail
    local td out p
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    export HOME="$td/home" CTX_DIR="$td/home/.ctx" SHELL=/bin/zsh
    mkdir -p "$HOME/dots" "$HOME/.ssh"
    p="$td/prefix"
    _t_fake_install "$p"
    printf 'user=1\n# ── ctx auto-switch ────\nold() { :; }\n# ──────────────────────────────\nafter=2\n' >"$HOME/dots/zshrc"
    chmod 640 "$HOME/dots/zshrc"
    ln -s dots/zshrc "$HOME/.zshrc"
    # install.sh's hook step runs the installed ctx install-hook (R18)
    out="$(CTX_INSTALL_SOURCE_ONLY=1 CTX_INSTALLED_BIN="$p/bin/ctx" bash -c 'source "$1"; install_ctx_hook' _ "$ROOT_DIR/install.sh" 2>&1)" \
      || { echo "hook step: $out"; exit 1; }
    [[ -L "$HOME/.zshrc" && "$(readlink "$HOME/.zshrc")" == dots/zshrc && "$(_t_mode "$HOME/dots/zshrc")" == 640 ]] || { echo "link or mode lost"; exit 1; }
    grep -qx 'user=1' "$HOME/dots/zshrc" && grep -qx 'after=2' "$HOME/dots/zshrc" && ! grep -q 'old()' "$HOME/dots/zshrc" \
      || { echo "rc: $(cat "$HOME/dots/zshrc")"; exit 1; }
    grep -qF "\"$p/lib/ctx\"" "$HOME/dots/zshrc" || { echo "block does not source the installed lib: $(cat "$HOME/dots/zshrc")"; exit 1; }
    # ctx cannot run: the manual command, exit 0, rc untouched
    cp "$HOME/dots/zshrc" "$td/before"
    out="$(CTX_INSTALL_SOURCE_ONLY=1 CTX_INSTALLED_BIN="$td/nope/ctx" bash -c 'source "$1"; install_ctx_hook' _ "$ROOT_DIR/install.sh" 2>&1)" \
      || { echo "missing ctx must not fail the installer: $out"; exit 1; }
    [[ "$out" == *"Run: ctx install-hook $HOME/.zshrc"* ]] || { echo "no manual command: $out"; exit 1; }
    cmp -s "$td/before" "$HOME/dots/zshrc" || { echo "rc changed"; exit 1; }
  ) || fail "install.sh installs the hook with the installed ctx install-hook"
  pass "install.sh installs the hook with the installed ctx install-hook"
}

test_temp_guard_restores_traps() {
  (
    set -euo pipefail
    local before after f
    trap 'echo outer-exit' EXIT
    trap 'echo outer-term' TERM
    before="$(trap -p EXIT; trap -p TERM)"
    f="$(mktemp)"
    _ctx_temp_guard "$f"
    [[ "$(trap -p EXIT)" == *'_ctx_guarded_tmp'* ]] || { echo "guard trap not set"; exit 1; }
    _ctx_temp_done
    after="$(trap -p EXIT; trap -p TERM)"
    [[ "$after" == "$before" && ! -e "$f" ]] || { echo "traps not restored: $after"; exit 1; }
    trap - EXIT TERM
    # no trap before: none after
    _ctx_temp_guard "$(mktemp)"; _ctx_temp_done
    [[ -z "$(trap -p EXIT; trap -p TERM)" ]] || { echo "left a trap: $(trap -p EXIT)"; exit 1; }
  ) >/dev/null || fail "_ctx_temp_done puts the EXIT and TERM traps back"
  pass "_ctx_temp_done puts the EXIT and TERM traps back"
}

test_valid_env_keys
test_timeout_helper
test_clone_extra_args_drop_url
test_github_clone_url_for_profile
test_parse_ctx_version_from_core_sh_file
test_secret_file_path
test_secret_provider_resolution
test_ctx_profile_read_work_dir
test_ctx_cli_version
test_ctx_rel_path_depth_under
test_ctx_resolve_path_profile
test_cmd_workdir_prompt
test_generate_mise_toml_matches_fixture
test_autoswitch_notify_default_off
test_config_upsert_literal_values
test_base64_decode_gnu_style
test_autoswitch_hook_resolution
test_trailing_slash_work_dir
test_setup_scope_clone_batch
test_ctx_json_list_and_status
test_guard_git_hooks
test_git_identity_github_rewrite
test_ctx_run_env_agent_rules
test_setup_commit_signing
test_use_keeps_user_agent_keys
test_ctx_core_dispatch
test_core_forward_fallback
test_install_builds_ctx_core
test_doctor_reports_ctx_core
test_use_eval_exports
test_shell_wrapper_applies_use
test_autoswitch_exports_profile_env
test_autoswitch_keeps_manual_lock
test_quiet_use_cloud_tools_succeed
test_inherited_quiet_use
test_use_deactivate_served_by_go
test_plain_prompts_use_stderr

test_remove_only_own_include
test_remove_gitconfig_symlink_and_nomatch
test_setup_extend_reaches_flow
test_setup_extend_secret_keys_once
test_setup_dry_run_changes_nothing
test_setup_gh_accounts_both_formats
test_setup_secrets_wait_for_confirm
test_setup_key_comment_never_empty
test_setup_generate_creates_ssh_dir
test_setup_pick_existing_key_index
test_setup_mise_toml_escaping
test_setup_no_secrets_bash32
test_setup_cancel_exits_130
test_user_cancelled_while_main_waits
test_setup_keygen_failure_dies
test_mise_hook_comment_no_injection
test_setup_batch_checks_before_side_effects
test_setup_cfg_quoted_bash32
test_edit_editor_with_args
test_clone_no_extra_args_bash32
test_verify_gh_account_and_exit_code
test_clone_dry_run
test_undo_lists_backs_up_and_keeps_mode
test_undo_ambiguous_and_same_second
test_uninstall_removes_install_and_purges
test_uninstall_refuses_unsafe_paths
test_install_hook_keeps_rc
test_rc_writes_checked
test_installer_runs_install_hook
test_temp_guard_restores_traps
echo "All tests passed."
