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
    printf 'WORK_DIR=%s\n' "$td/w/a" >"$CTX_DIR/profiles/a.conf"
    # shellcheck disable=SC2016
    printf 'WORK_DIR="$HOME/b"\n' >"$CTX_DIR/profiles/b.conf"
    printf 'WORK_DIR=%q\n' "$td/w/c d" >"$CTX_DIR/profiles/c.conf"
    printf 'WORK_DIR=%s\n' "$td/w/clients" >"$CTX_DIR/profiles/broad.conf"
    printf 'PROFILE_NAME=acme\n' >"$CTX_DIR/profiles/acme.conf"
    printf 'WORK_DIR=%s\n' "$td/w/mono" >"$CTX_DIR/profiles/mono.conf"
    printf 'profile= a \r\n' >"$td/w/mono/svc/.ctx"
    used="$td/used"
    ctx() { [[ "$1" == use ]] && printf '%s\n' "$2" >>"$used"; return 0; }
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
    _expect "$HOME/b/sub" b
    _expect "$td/w/c d" c
    _expect "$td/w/clients/acme" acme
    _expect "$td/w/mono/svc" a
    _expect "$td/w/mono" mono
    rm -rf "$td"
  ) || fail "autoswitch hook resolution"
  pass "autoswitch hook resolution (plain, \$HOME, escaped, sibling, .ctx)"
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
test_autoswitch_hook_resolution
test_setup_scope_clone_batch
test_ctx_json_list_and_status
test_guard_git_hooks

echo "All tests passed."
