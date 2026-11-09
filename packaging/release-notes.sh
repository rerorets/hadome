#!/bin/sh
# Prints the body of one version's section of a changelog: the lines after "## <version>" up to the
# next "## " heading. Exits non-zero when the section is missing or empty, so a release never goes out
# without its notes.
#   release-notes.sh <version without the v> <CHANGELOG.md>
set -eu
[ $# -eq 2 ] || { echo "usage: release-notes.sh <version> <changelog>" >&2; exit 2; }
awk -v v="$1" '
  /^## / { if (found) exit; if ($2 == v) { found = 1; next } }
  found { lines[++n] = $0; if ($0 ~ /[^[:space:]]/) { last = n; if (!first) first = n } }
  END {
    if (!first) { printf "%s has no section \"## %s\", or it is empty\n", FILENAME, v > "/dev/stderr"; exit 1 }
    for (i = first; i <= last; i++) print lines[i]
  }
' "$2"
