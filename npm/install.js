#!/usr/bin/env node

/**
 * API-Switch — binary downloader & launcher
 *
 * On postinstall, installs the api-switch binary.
 * Tries multiple methods in order:
 *   1. go install (works behind GFW with goproxy.cn)
 *   2. Download pre-built binary from GitHub Releases
 *
 * Usage: npx api-switch-cc <command>
 *        npm install -g api-switch-cc && api-switch serve
 */

"use strict";

const { existsSync, mkdirSync, chmodSync, copyFileSync } = require("fs");
const { join } = require("path");
const { execSync } = require("child_process");

const PKG = require("./package.json");
const DOWNLOAD_VERSION = "v0.2.1";
const GH_RELEASE = `https://github.com/hxz0727/API-Switch/releases/download/${DOWNLOAD_VERSION}`;
const BIN_DIR = join(__dirname, "bin");

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

function binName() {
  return process.platform === "win32" ? "api-switch.exe" : "api-switch";
}

function install() {
  const name = binName();
  const binPath = join(BIN_DIR, name);

  if (existsSync(binPath)) return;

  mkdirSync(BIN_DIR, { recursive: true });

  // ── Method 1: go install via goproxy.cn (works in China) ──
  if (tryGoInstall(binPath, name)) return;

  // ── Method 2: Download from GitHub Releases ──
  if (tryGitHubDownload(binPath, name)) return;

  // ── All methods failed ──
  console.error();
  console.error("Installation failed. Try one of these:");
  console.error();
  console.error("  # Option A: Install via Go (quickest)");
  console.error("  GOPROXY=https://goproxy.cn,direct go install github.com/hxz0727/API-Switch/cmd/api-switch@v0.2.0");
  console.error("  sudo cp ~/go/bin/api-switch /usr/local/bin/");
  console.error();
  console.error("  # Option B: Download binary directly");
  const plat = platform();
  const isWin = process.platform === "win32";
  const archive = `api-switch-${plat}${isWin ? ".zip" : ".tar.gz"}`;
  console.error(`  curl -sSL "${GH_RELEASE}/${archive}" | tar xz`);
  console.error(`  sudo cp api-switch-${plat} /usr/local/bin/api-switch`);
  console.error();
  console.error("  # Option C: Build from source");
  console.error("  git clone https://github.com/hxz0727/API-Switch.git");
  console.error("  cd API-Switch && make build && sudo make install");
  process.exit(1);
}

function tryGoInstall(binPath, name) {
  try {
    console.log("Trying go install (works behind GFW)...");
    // Use goproxy.cn for Chinese users, fallback to default for others
    execSync(
      `go install github.com/hxz0727/API-Switch/cmd/api-switch@${DOWNLOAD_VERSION}`,
      { stdio: "pipe", timeout: 120000 }
    );
    // go install puts binary in GOPATH/bin or ~/go/bin
    const goPath = (execSync("go env GOPATH", { encoding: "utf8" }) || "~/go").trim();
    const goBin = join(goPath, "bin", name);
    if (existsSync(goBin)) {
      copyFileSync(goBin, binPath);
      chmodSync(binPath, 0o755);
      console.log(`Installed to ${binPath}`);
      return true;
    }
  } catch (_) {
    // go not installed or install failed — try next method
  }
  return false;
}

function tryGitHubDownload(binPath, name) {
  const plat = platform();
  const isWin = process.platform === "win32";
  const archive = `api-switch-${plat}${isWin ? ".zip" : ".tar.gz"}`;
  const url = `${GH_RELEASE}/${archive}`;
  const tmp = join(__dirname, `_${archive}`);

  console.log(`Downloading ${DOWNLOAD_VERSION} for ${plat} ...`);

  try {
    execSync(
      `curl -sSL --connect-timeout 10 --max-time 120 --retry 2 "${url}" -o "${tmp}"`,
      { stdio: "pipe", timeout: 180000 }
    );

    const archiveBin = `api-switch-${plat}${isWin ? ".exe" : ""}`;
    if (isWin) {
      execSync(`unzip -o "${tmp}" "${archiveBin}" -d "${BIN_DIR}"`, { stdio: "pipe" });
    } else {
      execSync(`tar xzf "${tmp}" -C "${BIN_DIR}" "${archiveBin}"`, { stdio: "pipe" });
    }

    const extractedPath = join(BIN_DIR, archiveBin);
    if (extractedPath !== binPath) {
      execSync(`mv "${extractedPath}" "${binPath}"`, { stdio: "pipe" });
    }

    chmodSync(binPath, 0o755);
    unlinkSync(tmp);
    console.log(`Installed to ${binPath}`);
    return true;
  } catch (err) {
    console.error(`   Download failed: ${err.message.split('\n')[0]}`);
    try { unlinkSync(tmp); } catch (_) {}
    return false;
  }
}

// Run on postinstall
if (process.argv[2] === "postinstall") {
  install();
  process.exit(0);
}

// When run as CLI: execute the binary
const binPath = join(BIN_DIR, binName());
const result = require("child_process").spawnSync(binPath, process.argv.slice(2), {
  stdio: "inherit",
});
process.exit(result.status ?? 1);
