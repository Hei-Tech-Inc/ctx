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

test_ctx_cli_version() {
  local out want
  want="$CTX_VERSION"
  out="$("$ROOT_DIR/bin/ctx" version 2>&1)" || fail "bin/ctx version exited non-zero"
  [[ "$out" == *"$want"* ]] || fail "ctx version output missing $want (got: $out)"
  pass "ctx CLI (bin/ctx version)"
}

test_valid_env_keys
test_timeout_helper
test_github_clone_url_for_profile
test_parse_ctx_version_from_core_sh_file
test_secret_file_path
test_ctx_cli_version

echo "All tests passed."
