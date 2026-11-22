#!/usr/bin/env bash
# Resolve the build channel and identical image tags for both registries.
# The checkout must include origin/main for stable releases.
set -euo pipefail
ref=${1:?expected a Git ref}
commit=$(git rev-parse --verify "${2:?expected a Git commit}^{commit}")
short_sha=${commit:0:8}
number='(0|[1-9][0-9]*)'
semver="$number\\.$number\\.$number"

case "$ref" in
  refs/heads/dev)
    channel=nightly
    version=$short_sha
    tags="nightly nightly-$short_sha"
    ;;
  refs/tags/v*)
    version=${ref#refs/tags/v}
    if [[ $version =~ ^${semver}$ ]]; then
      if ! git merge-base --is-ancestor "$commit" origin/main; then
        echo "Stable release $ref requires a commit in origin/main." >&2
        exit 1
      fi
      channel=stable
      tags="latest $version ${version%.*} $version-$short_sha"
    elif [[ $version =~ ^${semver}-beta(\.${number})?$ ]]; then
      channel=beta
      tags="beta $version"
    else
      echo "Unsupported release tag: $ref (expected vX.Y.Z or vX.Y.Z-beta.N)." >&2
      exit 1
    fi
    ;;
  *)
    echo "No image channel for $ref." >&2
    exit 1
    ;;
esac

printf 'version=%s\nchannel=%s\ntags=%s\n' "$version" "$channel" "$tags"
