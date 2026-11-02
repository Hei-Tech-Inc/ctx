#!/usr/bin/env bash
# Benchmarks the shell autoswitch hook: the legacy shell hook (lib/ctx_autoswitch.* with
# CTX_CORE=off) against ctx activate (the same lib evaluating `ctx-core activate`, which
# runs `ctx-core hook-env`), in bash and zsh, and fish when installed. Ten profiles.
#
# Cases, as a prompt runs them (the prompt hooks; zsh and fish also their cd hooks):
#   unchanged    a prompt with no $PWD or config change (mean of 200; fish legacy 60)
#   enter/leave  cd into a profile's folder and out again (mean per transition)
#   switch       cd from one profile's folder to another's (mean per transition)
# Before timing, a canary ctx-core that logs every call checks that 50 unchanged prompts
# run no ctx-core at all (both modes; the legacy hook never calls it there either).
#
# Usage: scripts/bench-hook.sh [--assert FACTOR]   (FACTOR: a positive integer)
# Prints the numbers. With --assert, or in CI (CI set; FACTOR 5 there), it fails unless
# activate stays within the budget times FACTOR: unchanged <= 4 ms, enter/leave <= 25 ms,
# switch <= 30 ms (bash and zsh; fish timings are reported only, but its canary must
# pass). A session that exits non-zero prints its stderr. Needs bin/ctx-core (go build -ldflags
# "-X main.version=$(cat VERSION)" -o bin/ctx-core ./cmd/ctx) and a bash with EPOCHREALTIME (5.0+) for the bash rows.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
factor=""
if [[ "${1:-}" == --assert ]]; then
  factor="${2:?--assert needs a factor}"
elif [[ -n "${1:-}" ]]; then
  echo "usage: $0 [--assert FACTOR]" >&2
  exit 2
elif [[ -n "${CI:-}" ]]; then
  factor=5
fi
if [[ -n "$factor" && ! "$factor" =~ ^[1-9][0-9]*$ ]]; then
  echo "bench-hook: --assert FACTOR must be a positive integer, got '$factor'" >&2
  exit 2
fi
[[ -x "$ROOT/bin/ctx-core" ]] || { echo "bench-hook: build bin/ctx-core first (go build -ldflags \"-X main.version=\$(cat VERSION)\" -o bin/ctx-core ./cmd/ctx)" >&2; exit 1; }

T="$(cd "$(mktemp -d)" && pwd -P)"
trap 'rm -rf "$T"' EXIT
export HOME="$T/home" CTX_DIR="$T/home/.ctx" GIT_CONFIG_NOSYSTEM=1 LC_ALL=C
unset CTX_CORE CTX_CORE_BIN CTX_ACTIVE_PROFILE CTX_AUTOSWITCH_NOTIFY GH_TOKEN _CTX_GH_PROFILE AWS_PROFILE
mkdir -p "$CTX_DIR/profiles" "$T/log" "$T/syspath"
printf 'secret_provider=file\n' >"$CTX_DIR/config"
for i in 0 1 2 3 4 5 6 7 8 9; do
  mkdir -p "$T/w/p$i/r"
  printf 'PROFILE_NAME=p%s\nWORK_DIR=%s\nAWS_PROFILE_NAME=aws%s\nSECRET_KEYS=\nEXTRA_ENVS=\n' "$i" "$T/w/p$i" "$i" >"$CTX_DIR/profiles/p$i.conf"
done
# The canary: logs every call; for activate it bakes its own path into the glue.
cat >"$T/log/ctx-core" <<EOF
#!/bin/sh
printf '%s\n' "\$*" >>"$T/log/calls"
if [ "\$1" = activate ]; then
  out="\$("$ROOT/bin/ctx-core" "\$@")" || exit \$?
  printf '%s\n' "\$out" | sed "s|$ROOT/bin/ctx-core|$T/log/ctx-core|g"
  exit 0
fi
exec "$ROOT/bin/ctx-core" "\$@"
EOF
chmod +x "$T/log/ctx-core"
# The shells, from the caller's PATH.
shells=()
if bash -c '[[ -n "${EPOCHREALTIME:-}" ]]' 2>/dev/null; then shells+=("$(command -v bash)"); else echo "bash $(bash -c 'echo $BASH_VERSION'): no EPOCHREALTIME, skipped" >&2; fi
if command -v zsh >/dev/null 2>&1; then shells+=("$(command -v zsh)"); else echo "zsh not installed, skipped" >&2; fi
if command -v fish >/dev/null 2>&1; then shells+=("$(command -v fish)"); else echo "fish not installed, skipped" >&2; fi
# A hermetic PATH: ctx from this checkout, then the system dirs only (no cloud CLIs).
for d in /usr/bin /bin; do ln -sf "$d"/* "$T/syspath/" 2>/dev/null || true; done
rm -f "$T/syspath/ctx" "$T/syspath/ctx-core"
export PATH="$ROOT/bin:$T/syspath"

# The bash and zsh session: SHELL session.sh ROOT T MODE(legacy|activate) CORE N.
# Prints "CANARY <calls>" or "<case> <microseconds per prompt or transition>" lines.
cat >"$T/session.sh" <<'EOF'
R=$1 T=$2 M=$3 CORE=$4 N=$5
[ -z "${ZSH_VERSION-}" ] || zmodload zsh/datetime
us() { local s=${EPOCHREALTIME%[.,]*} f=${EPOCHREALTIME#*[.,]}000000; REPLY=$((s * 1000000 + 10#${f:0:6})); }
if [ -n "${ZSH_VERSION-}" ]; then
  prompt() { local _f; for _f in $precmd_functions; do "$_f"; done; }
else
  prompt() { eval "$PROMPT_COMMAND"; }
fi
# CTX_CORE_BIN: the lib's ctx-core (activate), and the one bin/ctx forwards to (legacy).
export CTX_CORE_BIN="$CORE"
if [ "$M" = legacy ]; then
  CTX_CORE=off source "$R/lib/ctx_autoswitch.bash"
else
  source "$R/lib/ctx_autoswitch.bash"
fi
cd "$T/w/p0/r" && prompt && prompt
if [ "$N" = canary ]; then
  : >"$T/log/calls"
  for _i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 \
    26 27 28 29 30 31 32 33 34 35 36 37 38 39 40 41 42 43 44 45 46 47 48 49 50; do prompt; done
  _n=0; while IFS= read -r _l; do _n=$((_n + 1)); done <"$T/log/calls"
  echo "CANARY $_n"
  exit 0
fi
_k=0; us; _t0=$REPLY
while [ $_k -lt $((N * 20)) ]; do prompt; _k=$((_k + 1)); done
us; echo "unchanged $(((REPLY - _t0) / (N * 20)))"
_k=0; us; _t0=$REPLY
while [ $_k -lt "$N" ]; do cd "$T" && prompt; cd "$T/w/p0/r" && prompt; _k=$((_k + 1)); done
us; echo "enter/leave $(((REPLY - _t0) / (2 * N)))"
_k=0; us; _t0=$REPLY
while [ $_k -lt "$N" ]; do cd "$T/w/p1/r" && prompt; cd "$T/w/p0/r" && prompt; _k=$((_k + 1)); done
us; echo "switch $(((REPLY - _t0) / (2 * N)))"
EOF

# The fish session: fish session.fish ROOT T MODE CORE N (same output). fish has no
# clock builtin: perl reads one per loop, so its cost is spread over the loop.
cat >"$T/session.fish" <<'EOF'
set -l R $argv[1]; set -l T $argv[2]; set -l M $argv[3]; set -l CORE $argv[4]; set -l N $argv[5]
function us; perl -MTime::HiRes=time -e 'printf "%d\n", time * 1e6'; end
function prompt; emit fish_postexec; end
set -gx CTX_CORE_BIN $CORE
if test $M = legacy
    set -gx CTX_CORE off; source $R/lib/ctx_autoswitch.fish; set -e CTX_CORE
else
    source $R/lib/ctx_autoswitch.fish
end
cd $T/w/p0/r; prompt; prompt
if test $N = canary
    true >$T/log/calls
    for i in (seq 50); prompt; end
    echo CANARY (wc -l <$T/log/calls | string trim)
    exit 0
end
set -l t0 (us)
for i in (seq (math $N \* 20)); prompt; end
echo unchanged (math --scale=0 "("(us)" - $t0) / ($N * 20)")
set t0 (us)
for i in (seq $N); cd $T; prompt; cd $T/w/p0/r; prompt; end
echo enter/leave (math --scale=0 "("(us)" - $t0) / (2 * $N)")
set t0 (us)
for i in (seq $N); cd $T/w/p1/r; prompt; cd $T/w/p0/r; prompt; end
echo switch (math --scale=0 "("(us)" - $t0) / (2 * $N)")
EOF

budget_us() { case "$1" in unchanged) echo 4000 ;; enter/leave) echo 25000 ;; switch) echo 30000 ;; esac; }
ms() { printf '%d.%03d' $(($1 / 1000)) $(($1 % 1000)); }
# session CORE N: runs one session of $sh/$mode (via run) into out, its stderr into
# $T/err. When it exits non-zero it prints that stderr and returns 1, so the caller
# reports the row (under set -e a bare failing $(…) would end the bench silently).
session() {
  local rc=0
  out="$(cd "$T" && "${run[@]}" "$ROOT" "$T" "$mode" "$1" "$2" 2>"$T/err")" || rc=$?
  ((rc != 0)) || return 0
  echo "bench-hook: $sh $mode session ($2) exited $rc; its stderr:" >&2
  sed 's/^/  /' "$T/err" >&2
  return 1
}

failed=0
printf '%-5s %-8s %-12s %10s\n' shell mode case ms
for shp in ${shells[@]+"${shells[@]}"}; do
  sh="${shp##*/}"
  if [[ "$sh" == fish ]]; then run=("$shp" --no-config "$T/session.fish"); else run=("$shp" "$T/session.sh"); fi
  for mode in legacy activate; do
    n=10
    # The legacy fish hook forks per profile and per config key: fewer rounds.
    [[ "$sh/$mode" != fish/legacy ]] || n=3
    session "$T/log/ctx-core" canary || out="FAILED:$out"
    if [[ "$out" != "CANARY 0" ]]; then
      echo "FAIL: $sh $mode: 50 unchanged prompts ran ctx-core: ${out:-no output}" >&2
      failed=1
    fi
    ok=1
    session "$ROOT/bin/ctx-core" "$n" || ok=0
    while read -r c v; do
      [[ -n "$c" ]] || continue
      printf '%-5s %-8s %-12s %10s\n' "$sh" "$mode" "$c" "$(ms "$v")"
      if [[ -n "$factor" && "$mode" == activate && "$sh" != fish ]]; then
        b="$(budget_us "$c")"
        if ((v > b * factor)); then
          echo "FAIL: $sh $c: $(ms "$v") ms, budget $(ms "$b") ms x $factor" >&2
          failed=1
        fi
      fi
    done <<<"$out"
    if [[ -z "$out" || "$ok" == 0 ]]; then
      why="no timings"
      [[ -z "$out" ]] || why="session failed"
      if [[ "$sh" == fish ]]; then
        echo "fish $mode: $why (fish timings are report-only)" >&2
      else
        echo "FAIL: $sh $mode: $why" >&2
        failed=1
      fi
    fi
  done
done
exit "$failed"
