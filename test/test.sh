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

test_ctx_resolve_path_profile() {
  (
    set -euo pipefail
    local ROOT td prof want got
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    td="$(mktemp -d)"
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

    got="$(ctx_resolve_path_profile "/nonexistent/nope/zz" "$prof")"
    [[ -z "$got" ]] || exit 1

    rm -rf "$td"
  ) || fail "ctx_resolve_path_profile"
  pass "ctx_resolve_path_profile (prefix, .ctx override, no match)"
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

test_valid_env_keys
test_timeout_helper
test_clone_extra_args_drop_url
test_github_clone_url_for_profile
test_parse_ctx_version_from_core_sh_file
test_secret_file_path
test_secret_provider_resolution
test_ctx_profile_read_work_dir
test_ctx_cli_version
test_ctx_resolve_path_profile
test_generate_mise_toml_matches_fixture
test_ctx_json_list_and_status

echo "All tests passed."
