#!/usr/bin/env bash
set -euo pipefail
bin="$(cd "$(dirname "$0")/../bin" && pwd)/ctx"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
export HOME="$tmp" CTX_DIR="$tmp/.ctx" GIT_CONFIG_GLOBAL="$tmp/.gitconfig" GIT_CONFIG_NOSYSTEM=1
mkdir -p "$CTX_DIR/profiles" "$HOME/.config/git"
root="$tmp/work/acme/api"
git init -q "$root"
git -C "$root" remote add origin git@github.com:acme/api.git
printf 'GIT_EMAIL=acme@example.test\n' >"$CTX_DIR/profiles/acme.conf"
printf '[user]\nemail = acme@example.test\n' >"$HOME/.config/git/ctx-acme"
printf 'touch "%s"\nexport AWS_PROFILE=private-value\n' "$tmp/executed" >"$tmp/work/.envrc"
printf '[env]\nKUBECONFIG="private-value"\n[tasks.deploy]\nrun="private-value"\n' >"$root/mise.toml"
touch "$root/mise.lock"
mkdir "$root/mise-tasks"
printf 'GIT_SSH_COMMAND=should-never-appear\n' >"$tmp/elsewhere"
ln -s "$tmp/elsewhere" "$root/.env"
for args in scan adopt; do
  if [[ "$args" == scan ]]; then output=$("$bin" organize "$root")
  else output=$("$bin" --dry-run organize adopt acme "$root"); fi
  [[ "$output" == *"ancestor candidate:"*"/work/.envrc (review: AWS_PROFILE)"* ]]
  [[ "$output" == *"checkout candidate:"*"/mise.toml (review: tasks section, KUBECONFIG)"* ]]
  [[ "$output" == *"/mise.lock"* && "$output" == *"task directory:"*"/mise-tasks"* ]]
  [[ "$output" == *'contents uninspected'* && "$output" != *GIT_SSH_COMMAND* ]]
  [[ "$output" != *private-value* && "$output" != *should-never-appear* && ! -e "$tmp/executed" ]]
done
[[ ! -e "$CTX_DIR/projects" ]]
