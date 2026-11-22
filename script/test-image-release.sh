#!/usr/bin/env bash
set -euo pipefail
script=$(cd "$(dirname "$0")" && pwd)/image-release.sh
repo=$(mktemp -d)
trap 'rm -r "$repo"' EXIT
cd "$repo"
git init -q -b main
git config user.name 'Release test'
git config user.email 'release@example.invalid'
git -c commit.gpgsign=false commit -qm initial --allow-empty
stable=$(git rev-parse HEAD)
git update-ref refs/remotes/origin/main "$stable"
git switch -qc dev
git -c commit.gpgsign=false commit -qm development --allow-empty
dev=$(git rev-parse HEAD)
git -c tag.gpgSign=false tag -a v2.0.0 -m stable "$stable"

expect() {
  local actual expected
  actual=$("$script" "$1" "$2")
  expected=$(printf 'version=%s\nchannel=%s\ntags=%s\n' "$3" "$4" "$5")
  [[ $actual == "$expected" ]] || { echo "Unexpected metadata for $1: $actual" >&2; exit 1; }
}
reject() {
  if "$script" "$1" "$2" >/dev/null 2>&1; then
    echo "Unexpected release for $1" >&2
    exit 1
  fi
}

expect refs/heads/dev "$dev" "${dev:0:8}" nightly "nightly nightly-${dev:0:8}"
expect refs/tags/v2.0.0 v2.0.0 2.0.0 stable "latest 2.0.0 2.0 2.0.0-${stable:0:8}"
expect refs/tags/v2.1.0-beta.1 "$dev" 2.1.0-beta.1 beta 'beta 2.1.0-beta.1'
reject refs/heads/main "$stable"
reject refs/tags/v2.1.0 "$dev"
reject refs/tags/v2.0.0-rc.1 "$stable"
