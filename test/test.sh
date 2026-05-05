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

test_valid_env_keys
test_timeout_helper

echo "All tests passed."
