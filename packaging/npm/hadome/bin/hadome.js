#!/usr/bin/env node
// Runs the hadome binary of the platform package npm installed next to this one. Each platform
// package declares its os and cpu, so npm installs only the one that matches. Windows on ARM64 runs
// the x86-64 package, which declares both CPUs, through the emulation Windows 11 provides.
"use strict";

const { spawnSync } = require("node:child_process");

const platforms = {
  "linux x64": "hadome-linux-x64",
  "linux arm64": "hadome-linux-arm64",
  "darwin x64": "hadome-darwin-x64",
  "darwin arm64": "hadome-darwin-arm64",
  "win32 x64": "hadome-win32-x64",
  "win32 arm64": "hadome-win32-x64",
};

const key = `${process.platform} ${process.arch}`;
const pkg = platforms[key];
if (!pkg) {
  console.error(`hadome: no prebuilt binary for ${key}. Supported: ${Object.keys(platforms).join(", ")}.`);
  process.exit(2);
}

let binary;
try {
  binary = require.resolve(`${pkg}/bin/hadome${process.platform === "win32" ? ".exe" : ""}`);
} catch {
  console.error(`hadome: the package ${pkg} is not installed. It is an optional dependency of hadome; ` +
    "reinstall without --no-optional (or --omit=optional).");
  process.exit(2);
}

const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  console.error(`hadome: cannot run ${binary}: ${result.error.message}`);
  process.exit(2);
}
process.exit(result.status === null ? 2 : result.status);
