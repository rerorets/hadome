#!/bin/sh
# Writes the version and checksums of a release where both JVM plugins read them.
#   release.sh <version> <SHA256SUMS>
set -eu
[ $# -eq 2 ] || { echo "usage: release.sh <version> <SHA256SUMS>" >&2; exit 2; }
dir="$(dirname "$0")/release/io/github/rerorets/hadome/installer"
mkdir -p "$dir"
printf '%s' "$1" > "$dir/hadome-version.txt"
cp "$2" "$dir/SHA256SUMS"
