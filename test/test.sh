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
    ctx() { [[ "$1" == use ]] && printf '%s\n' "$2" >>"$used"; return 0; }
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
    # Text list is not forwarded
    out="$(CTX_CORE_BIN="$td/fakego" "$ROOT_DIR/bin/ctx" list 2>/dev/null)"
    [[ "$out" != GO* ]] || { echo "text list forwarded: $out"; exit 1; }
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

test_doctor_reports_ctx_core() {
  (
    set -euo pipefail
    local td out
    td="$(cd "$(mktemp -d)" && pwd -P)"
    trap 'rm -rf "$td"' EXIT
    mkdir -p "$td/home" "$td/ctx"
    printf '#!/usr/bin/env bash\necho "ctx-core 9.9.9"\n' >"$td/ctx-core"
    chmod +x "$td/ctx-core"
    out="$(HOME="$td/home" CTX_DIR="$td/ctx" CTX_CORE_BIN="$td/ctx-core" "$ROOT_DIR/bin/ctx" doctor 2>&1 || true)"
    [[ "$out" == *"ctx-core: 9.9.9 ($td/ctx-core)"* ]] || { echo "present: $out"; exit 1; }
    out="$(HOME="$td/home" CTX_DIR="$td/ctx" CTX_CORE_BIN="$td/missing" "$ROOT_DIR/bin/ctx" doctor 2>&1 || true)"
    [[ "$out" == *"ctx-core missing"*"ctx mcp unavailable"* ]] || { echo "missing: $out"; exit 1; }
  ) || fail "doctor reports ctx-core"
  pass "ctx doctor reports ctx-core version/path or a warning"
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
test_git_identity_github_rewrite
test_ctx_run_env_agent_rules
test_setup_commit_signing
test_use_keeps_user_agent_keys
test_ctx_core_dispatch
test_core_forward_fallback
test_install_builds_ctx_core
test_doctor_reports_ctx_core

echo "All tests passed."
