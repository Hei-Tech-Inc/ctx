#!/usr/bin/env bash
# Exercise the remote installer layout using local tag contents and release assets.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd -P)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
version=$(cat "$root/VERSION")
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; x86_64) arch=amd64 ;; *) exit 2 ;; esac
asset="ctx-core_${os}_${arch}"
mkdir -p "$stage/release/v$version" "$stage/prefix/bin" "$stage/prefix/lib" "$stage/home/.ssh"
(cd "$root" && go build -ldflags "-X main.version=$version" -o "$stage/release/v$version/$asset" ./cmd/ctx)
(cd "$stage/release/v$version" && shasum -a 256 "$asset" > checksums.txt)
cp "$root/install.sh" "$stage/install.sh"
export HOME="$stage/home" CTX_DIR="$stage/home/.ctx" SHELL=/bin/bash
unset CTX_ACTIVE_PROFILE CTX_CORE CTX_CORE_BIN
# Confirm the release default before substituting a local mirror for both origins.
(
  unset CTX_REPO
  CTX_INSTALL_SOURCE_ONLY=1 source "$stage/install.sh"
  [[ "$CTX_REPO" == "https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/v$version" ]]
)
CTX_REPO="file://$root" CTX_RELEASE_BASE="file://$stage/release" CTX_UPGRADE_ONLY=1 \
  CTX_INSTALL_BIN="$stage/prefix/bin" CTX_INSTALL_LIB="$stage/prefix/lib" bash "$stage/install.sh" </dev/null
export PATH="$stage/prefix/bin:$PATH"
[[ "$(ctx version)" == "ctx version $version" ]]
[[ "$("$stage/prefix/lib/ctx-core" version)" == "ctx-core $version" ]]
ssh-keygen -q -t ed25519 -N '' -f "$HOME/.ssh/demo"
for name in acme orbit; do
  printf 'SETUP_SCOPE=clone\nPROFILE_NAME=%s\nSSH_MODE=existing\nSSH_KEY_PATH=%s\nAUTO_CONFIRM=true\n' "$name" "$HOME/.ssh/demo" > "$stage/setup"
  ctx setup --config "$stage/setup" --yes </dev/null
done
ctx doctor --json > "$stage/doctor.json" || true
python3 -c 'import json, sys; assert json.load(open(sys.argv[1]))["command"] == "doctor"' "$stage/doctor.json"
CTX_ACTIVE_PROFILE=orbit ctx explain | grep -F 'Comparing with profile: orbit'
ctx offboard orbit | grep -F 'Shared SSH key with acme'
ctx activate bash > "$stage/activate.bash"
bash -n "$stage/activate.bash"
# Source the installed hook in a fresh shell and inspect its exported identity.
bash -c 'source "$1"; ctx use orbit >/dev/null; [[ "${CTX_ACTIVE_PROFILE:-}" == orbit ]]' _ "$stage/activate.bash"
python3 - <<'PY'
import json, subprocess
with subprocess.Popen(["ctx","mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True) as server:
    server.stdin.write(json.dumps({"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"release-test","version":"1"}}})+"\n")
    server.stdin.flush()
    assert json.loads(server.stdout.readline())["result"]["serverInfo"]["name"] == "ctx"
    server.stdin.close()
    server.wait(timeout=10)
PY
