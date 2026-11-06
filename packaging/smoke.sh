#!/bin/sh
# Builds every package from a pretend release of this machine's Linux binary, and uses each one:
# what a user would run after installing it must print the version and pass exit codes through.
#   smoke.sh <version>          SMOKE_SKIP_JVM=1 leaves out Gradle and Maven (they need their registries)
set -eu
[ $# -eq 1 ] || { echo "usage: smoke.sh <version>" >&2; exit 2; }
v=$1
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
expect_version() { out=$("$@" version); [ "$out" = "hadome $v" ] || { echo "FAIL: $* version printed '$out'" >&2; exit 1; }; }
# Every artifact that holds the binary carries the licenses of hadome and of every module linked into it.
expect_legal() { for f in LICENSE THIRD_PARTY_NOTICES; do cmp "$root/$f" "$1/$f" || { echo "FAIL: $1 lacks $f" >&2; exit 1; }; done; }
expect_exit() { want=$1; shift; set +e; "$@" explain nonsense >/dev/null 2>&1; got=$?; set -e
  [ "$got" = "$want" ] || { echo "FAIL: $* exited $got, want $want" >&2; exit 1; }; }

echo "== binary and pretend release"
mkdir -p "$work/bins/linux_amd64" "$work/rel/v$v"
(cd "$root" && CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=$v -linkmode external -extldflags -static" \
  -o "$work/bins/linux_amd64/hadome" ./cmd/hadome)
cp "$root/LICENSE" "$root/THIRD_PARTY_NOTICES" "$work/bins/linux_amd64/"
tar -C "$work/bins/linux_amd64" -czf "$work/rel/v$v/hadome_${v}_linux_amd64.tar.gz" hadome LICENSE THIRD_PARTY_NOTICES
(cd "$work/rel/v$v" && sha256sum hadome_* > SHA256SUMS)
"$root/packaging/extract.sh" "$work/rel/v$v" "$work/extracted"
cmp "$work/bins/linux_amd64/hadome" "$work/extracted/linux_amd64/hadome"
expect_legal "$work/extracted/linux_amd64"

echo "== npm"
node "$root/packaging/npm/build.mjs" "$v" "$work/bins" "$work/npm"
(cd "$work/npm/hadome-linux-x64" && npm pack --silent >/dev/null) && (cd "$work/npm/hadome" && npm pack --silent >/dev/null)
mkdir "$work/npmproj" && cd "$work/npmproj" && npm init -y >/dev/null
npm install --silent --no-audit --no-fund "$work/npm/hadome-linux-x64/hadome-linux-x64-$v.tgz" "$work/npm/hadome/hadome-$v.tgz"
expect_legal node_modules/hadome && expect_legal node_modules/hadome-linux-x64
expect_version npx --no-install hadome
expect_exit 2 npx --no-install hadome

echo "== python"
python3 -I "$root/packaging/python/build_wheels.py" "$v" "$work/bins" "$work/wheels" >/dev/null
python3 -m venv "$work/venv" && "$work/venv/bin/pip" install -q "$work"/wheels/*.whl
expect_legal "$(echo "$work"/venv/lib/python*/site-packages/hadome-"$v".dist-info/licenses)"
expect_version "$work/venv/bin/hadome"
expect_exit 2 "$work/venv/bin/hadome"

echo "== cargo"
cp -r "$root/packaging/cargo" "$work/crate" && cd "$work/crate"
sed -i "s/^version = \"0.0.0\"/version = \"$v\"/" Cargo.toml && cp "$root/README.md" README.md
cargo package -q --allow-dirty --no-verify --offline
set +e; cargo run -q --offline >/dev/null 2>&1; got=$?; set -e
[ "$got" = 2 ] || { echo "FAIL: the crate built from source should explain how to install and exit 2, got $got" >&2; exit 1; }

echo "== homebrew"
for p in darwin_arm64 darwin_amd64 linux_arm64; do echo "0000  hadome_${v}_$p.tar.gz"; done > "$work/sums"
cat "$work/rel/v$v/SHA256SUMS" >> "$work/sums"
python3 -I "$root/packaging/homebrew/formula.py" "$v" "$work/sums" > "$work/hadome.rb"
if command -v ruby >/dev/null; then ruby -c "$work/hadome.rb" >/dev/null; fi
grep -q 'prefix.install "LICENSE", "THIRD_PARTY_NOTICES"' "$work/hadome.rb" || { echo "FAIL: the formula does not install the licenses" >&2; exit 1; }

if [ "${SMOKE_SKIP_JVM:-}" = 1 ]; then echo "ok (JVM skipped)"; exit 0; fi

echo "== gradle"
"$root/packaging/jvm/release.sh" "$v" "$work/rel/v$v/SHA256SUMS"
mkdir "$work/gproj" && cd "$work/gproj"
printf 'pluginManagement { includeBuild("%s") }\nrootProject.name = "sample"\n' "$root/packaging/jvm/gradle-plugin" > settings.gradle.kts
printf 'plugins { id("io.github.rerorets.hadome") }\n' > build.gradle.kts
# --no-daemon: a daemon left running keeps writing under $work/gh, and the cleanup's rm -rf fails.
out=$(gradle -q --no-daemon -g "$work/gh" -PhadomeVersion="$v" -Dhadome.downloadBaseUrl="file://$work/rel" hadomeCheck --args=version)
[ "$out" = "hadome $v" ] || { echo "FAIL: gradle hadomeCheck printed '$out'" >&2; exit 1; }

echo "== maven"
cd "$root/packaging/jvm/maven-plugin" && mvn -q -B install -Drevision="$v" -Dmaven.repo.local="$work/m2"
mkdir "$work/mproj" && cd "$work/mproj"
out=$(mvn -q -B -Dmaven.repo.local="$work/m2" "io.github.rerorets:hadome-maven-plugin:$v:check" -Dhadome.args=version \
  -Dhadome.downloadBaseUrl="file://$work/rel")
[ "$out" = "hadome $v" ] || { echo "FAIL: mvn hadome:check printed '$out'" >&2; exit 1; }
rm -rf "$root/packaging/jvm/release"
echo ok
