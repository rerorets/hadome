#!/bin/sh
# Stages one release's npm packages for a maintainer to approve with 2FA.
#   stage.sh <version> <dir that build.mjs wrote>
#
# It runs in the release workflow, where npm trusted publishing (OIDC) allows `npm stage publish`
# and nothing else. Its outcomes, per package:
#   - the version is already published: skipped
#   - the version is already staged and awaits approval: reported, not staged again
#   - the package does not exist on npm: the script stops before staging anything, because a
#     trusted publisher can only be configured on an existing package
#   - anything else: the script fails
# The launcher package `hadome` is staged only after every platform package is published or
# staged, so it never names a platform package that is missing from the registry.
set -eu
[ $# -eq 2 ] || { echo "usage: stage.sh <version> <packages dir>" >&2; exit 2; }
version=$1
dir=$2
tag=latest
case "$version" in *-*) tag=next ;; esac # a trial version never becomes what `npm install hadome` gets
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Platform packages first, the launcher last.
packages=""
for d in "$dir"/hadome-*; do
  [ -d "$d" ] && packages="$packages $d"
done
[ -n "$packages" ] || { echo "no platform packages in $dir" >&2; exit 1; }
packages="$packages $dir/hadome"

name_of() { node -p 'require(require("path").resolve(process.argv[1], "package.json")).name' "$1"; }

# published <name>: exit 0 if <name>@<version> is on the registry, 1 if not, 3 if the package does
# not exist. Any other failure of `npm view` (network, registry error) stops the script.
published() {
  if ! npm view "$1" versions --json --prefer-online >"$tmp/view" 2>"$tmp/view.err"; then
    if grep -q 'E404' "$tmp/view.err"; then return 3; fi
    cat "$tmp/view.err" >&2
    echo "error: could not read $1 from the npm registry" >&2
    exit 1
  fi
  node -e '
    const v = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"));
    process.exit([].concat(v).includes(process.argv[2]) ? 0 : 1);
  ' "$tmp/view" "$version"
}

missing=""
for d in $packages; do
  name=$(name_of "$d")
  status=0
  published "$name" || status=$?
  if [ "$status" -eq 3 ]; then missing="$missing $name"; fi
done
if [ -n "$missing" ]; then
  for name in $missing; do
    echo "error: $name does not exist on npm, and trusted publishing cannot create a package." >&2
  done
  echo "Nothing was staged. Create each package once as described in docs/maintaining/releasing.md" >&2
  echo "(\"Creating an npm package\"), then re-run this job." >&2
  exit 1
fi

for d in $packages; do
  name=$(name_of "$d")
  status=0
  published "$name" || status=$?
  if [ "$status" -eq 0 ]; then echo "skip: $name@$version is published"; continue; fi
  if (cd "$d" && npm stage publish --provenance --access public --tag "$tag") >"$tmp/stage.log" 2>&1; then
    cat "$tmp/stage.log"
    continue
  fi
  cat "$tmp/stage.log"
  # Published and staged versions share one version index, so the registry refuses to stage a
  # version that is already staged. `npm stage list` cannot run under OIDC, so the refusal is the
  # only sign of it.
  status=0
  published "$name" || status=$?
  if [ "$status" -eq 0 ]; then echo "skip: $name@$version was published meanwhile"; continue; fi
  if grep -qiE 'previously (published|staged)|already (exists|staged)|cannot publish over' "$tmp/stage.log"; then
    echo "skip: $name@$version is already staged and awaits approval"
    continue
  fi
  echo "error: staging $name@$version failed" >&2
  exit 1
done
echo "Every package of hadome $version is published or staged. A maintainer approves each staged version with 2FA."
