#!/bin/sh
# Prints the -ldflags value for go build. It is the only definition of the
# build metadata injected into internal/app/buildinfo: the Makefile, the
# Dockerfile and the release workflow all call it, so every artifact reports
# its version, channel and build time the same way.
#
#   VERSION     version shown to operators, without the tag's leading "v"
#               (default: "unknown version")
#   CHANNEL     stable, beta, nightly or dev (default: dev)
#   BUILD_TIME  build time as UTC RFC 3339, YYYY-MM-DDTHH:MM:SSZ
#               (default: now). CI passes one value to every artifact of a run.
#
# Usage: go build -ldflags "$(VERSION=1.2.3 CHANNEL=stable sh script/ldflags.sh)"
set -eu

pkg=github.com/perfect-panel/backend/internal/app/buildinfo
version=${VERSION:-unknown version}
channel=${CHANNEL:-dev}
build_time=${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}

case $channel in
stable | beta | nightly | dev) ;;
*)
	echo "ldflags.sh: CHANNEL must be stable, beta, nightly or dev, got \"$channel\"" >&2
	exit 1
	;;
esac

case $build_time in
[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]Z) ;;
*)
	echo "ldflags.sh: BUILD_TIME must be UTC RFC 3339 (YYYY-MM-DDTHH:MM:SSZ), got \"$build_time\"" >&2
	exit 1
	;;
esac

# The values are single-quoted for go build's flag parser, so they must not
# contain quotes themselves.
case $version in
*\'* | *\"*)
	echo "ldflags.sh: VERSION must not contain quotes, got $version" >&2
	exit 1
	;;
esac

printf '%s\n' "-s -w -buildid= -X '$pkg.Version=$version' -X '$pkg.BuildTime=$build_time' -X '$pkg.Channel=$channel'"
