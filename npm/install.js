#!/usr/bin/env node

/**
 * API-Switch — binary downloader & launcher
 *
 * On postinstall, downloads the correct pre-built binary for the
 * current platform from GitHub Releases and caches it locally.
 *
 * Usage: npx api-switch <command>
 *        npm install -g api-switch && api-switch serve
 */

"use strict";

const { existsSync, mkdirSync, chmodSync, unlinkSync } = require("fs");
const { join } = require("path");
const { execSync } = require("child_process");

const PKG = require("./package.json");
const VER = PKG.version;
const BASE = `https://github.com/hxz0727/API-Switch/releases/download/v${VER}`;
const BIN_DIR = join(__dirname, "bin");

// ── Platform detection ──────────────────────────────
function platform() {
  const map = {
    "darwin-x64":    "darwin-amd64",
    "darwin-arm64":  "darwin-arm64",
    "linux-x64":     "linux-amd64",
    "linux-arm64":   "linux-arm64",
    "win32-x64":     "windows-amd64",
  };
  const key = `${process.platform}-${process.arch}`;
  if (!map[key]) {
    console.error(`Unsupported platform: ${process.platform} ${process.arch}`);
    process.exit(1);
  }
  return map[key];
}

function binaryName() {
  if (process.platform === "win32") return "api-switch.exe";
  return "api-switch";
}

function archivedBinaryName() {
  return `api-switch-${platform()}${process.platform === "win32" ? ".exe" : ""}`;
}

// ── Download & extract ──────────────────────────────
function install() {
  const binName = binaryName();
  const binPath = join(BIN_DIR, binName);

  // Skip if binary exists and is fresh
  if (existsSync(binPath)) {
    return;
  }

  mkdirSync(BIN_DIR, { recursive: true });

  const plat = platform();
  const isWin = process.platform === "win32";
  const archive = `api-switch-${plat}${isWin ? ".zip" : ".tar.gz"}`;
  const url = `${BASE}/${archive}`;
  const tmp = join(__dirname, `_${archive}`);

  console.log(`Downloading api-switch v${VER} for ${plat} ...`);

  try {
    // Download
    execSync(`curl -sSL "${url}" -o "${tmp}"`, { stdio: "pipe" });

    // Extract (archive contains binary named api-switch-<plat>)
    const archiveBin = archivedBinaryName();
    if (isWin) {
      execSync(`unzip -o "${tmp}" "${archiveBin}" -d "${BIN_DIR}"`, { stdio: "pipe" });
    } else {
      execSync(`tar xzf "${tmp}" -C "${BIN_DIR}" "${archiveBin}"`, { stdio: "pipe" });
    }

    // Rename to simple binary name (e.g. api-switch-linux-amd64 → api-switch)
    const extractedPath = join(BIN_DIR, archiveBin);
    if (extractedPath !== binPath) {
      execSync(`mv "${extractedPath}" "${binPath}"`, { stdio: "pipe" });
    }

    chmodSync(binPath, 0o755);
    unlinkSync(tmp);

    console.log(`Installed to ${binPath}`);
  } catch (err) {
    console.error(`Download failed: ${err.message}`);
    console.log("Falling back to source build (requires Go)...");
    execSync(
      `cd ${join(__dirname, "..")} && go build -ldflags="-s -w" -o "${binPath}" ./cmd/api-switch/`,
      { stdio: "inherit" }
    );
  }
}

// Run on postinstall
if (process.argv[2] === "postinstall") {
  install();
  process.exit(0);
}

// When run as CLI: execute the binary
const binPath = join(BIN_DIR, binaryName());
const result = require("child_process").spawnSync(binPath, process.argv.slice(2), {
  stdio: "inherit",
});
process.exit(result.status ?? 1);
