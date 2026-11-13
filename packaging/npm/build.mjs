// Builds the npm packages of one release from its binaries.
//
//   node build.mjs <version> <binaries dir> <out dir>
//
// <binaries dir> holds <os>_<arch>/hadome[.exe] (os: linux, darwin, windows; arch: amd64, arm64).
// Platforms whose binary is missing are left out, so a package can be tried on one machine.
// Windows on ARM64 has no package of its own: hadome-win32-x64 declares both CPUs, and Windows 11
// runs its x86-64 binary through emulation (docs/design/distribution.md, "npm platform packages").
import { copyFileSync, chmodSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const [version, binaries, out] = process.argv.slice(2);
if (!version || !binaries || !out) {
  console.error("usage: node build.mjs <version> <binaries dir> <out dir>");
  process.exit(2);
}
const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..", "..");
// Every package that holds the binary, and the launcher, carries the licenses of hadome and of every
// module linked into it.
const legal = ["LICENSE", "THIRD_PARTY_NOTICES"];
const copyLegal = (dir) => legal.forEach((f) => copyFileSync(join(root, f), join(dir, f)));
// [Go os, Go arch, npm os, npm cpu in the name, npm cpus the package installs on]
const targets = [
  ["linux", "amd64", "linux", "x64", ["x64"]], ["linux", "arm64", "linux", "arm64", ["arm64"]],
  ["darwin", "amd64", "darwin", "x64", ["x64"]], ["darwin", "arm64", "darwin", "arm64", ["arm64"]],
  ["windows", "amd64", "win32", "x64", ["x64", "arm64"]],
];
const common = {
  version,
  license: "MIT",
  homepage: "https://github.com/rerorets/hadome",
  repository: { type: "git", url: "git+https://github.com/rerorets/hadome.git" },
};

const optional = {};
for (const [goos, goarch, os, cpu, cpus] of targets) {
  const exe = goos === "windows" ? "hadome.exe" : "hadome";
  const src = join(binaries, `${goos}_${goarch}`, exe);
  if (!existsSync(src)) continue;
  const name = `hadome-${os}-${cpu}`;
  const dir = join(out, name);
  mkdirSync(join(dir, "bin"), { recursive: true });
  copyFileSync(src, join(dir, "bin", exe));
  chmodSync(join(dir, "bin", exe), 0o755);
  copyLegal(dir);
  writeFileSync(join(dir, "package.json"), JSON.stringify({
    name, ...common, description: `The hadome binary for ${os} ${cpu}.`,
    os: [os], cpu: cpus, files: ["bin", ...legal], preferUnplugged: true,
  }, null, 2) + "\n");
  optional[name] = version;
}

const main = join(out, "hadome");
mkdirSync(join(main, "bin"), { recursive: true });
copyFileSync(join(here, "hadome", "bin", "hadome.js"), join(main, "bin", "hadome.js"));
copyFileSync(join(root, "README.md"), join(main, "README.md"));
copyLegal(main);
writeFileSync(join(main, "package.json"), JSON.stringify({
  name: "hadome", ...common,
  description: "Reports the violations a change adds and the tests it weakens.",
  bin: { hadome: "bin/hadome.js" }, files: ["bin", "README.md", ...legal],
  optionalDependencies: optional,
  engines: { node: ">=18" },
}, null, 2) + "\n");
console.log(`built hadome ${version} with ${Object.keys(optional).join(", ") || "no platform packages"}`);
