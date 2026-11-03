#!/bin/sh
# Unpacks a release's archives into <out>/<os>_<arch>/hadome[.exe], the layout the package builders read.
#   extract.sh <dir with hadome_<version>_<os>_<arch>.{tar.gz,zip}> <out>
set -eu
[ $# -eq 2 ] || { echo "usage: extract.sh <archives dir> <out>" >&2; exit 2; }
for a in "$1"/hadome_*.tar.gz "$1"/hadome_*.zip; do
  [ -e "$a" ] || continue
  platform=$(basename "$a" | sed -E 's/^hadome_[^_]+_([a-z]+_[a-z0-9]+)\..*$/\1/')
  mkdir -p "$2/$platform"
  case "$a" in
    *.zip) unzip -q -o "$a" -d "$2/$platform" ;;
    *) tar -xzf "$a" -C "$2/$platform" ;;
  esac
done
