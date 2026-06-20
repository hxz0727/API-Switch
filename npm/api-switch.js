#!/usr/bin/env node

/**
 * API-Switch — lazy-install launcher
 *
 * On first run, installs the api-switch binary, then execs it.
 * Subsequent runs use the cached binary directly.
 *
 * Install order:
 *   1. GitHub release raw binary (no tar needed)
 *   2. Gitee clone + go build (China mirror, needs Go + Git)
 *   3. go install (needs Go)
 *   4. Friendly error with platform-specific instructions
 */

"use strict";

const { existsSync, mkdirSync, chmodSync, unlinkSync, copyFileSync } = require("fs");
const { join } = require("path");
const { execSync, spawnSync } = require("child_process");

const PKG = require("./package.json");
const VERSION = "v" + PKG.version;
const BIN_DIR = join(__dirname, ".bin");
const IS_WIN = process.platform === "win32";
const BIN_NAME = IS_WIN ? "api-switch.exe" : "api-switch";
const BIN_PATH = join(BIN_DIR, BIN_NAME);

const GITEE_REPO = "https://gitee.com/776311606/API-Switch.git";
const GITEE_RAW  = `https://gitee.com/776311606/API-Switch/raw/release`;
const GH_RELEASE = `https://github.com/hxz0727/API-Switch/releases/download/${VERSION}`;

function platform() {
  const map = {
    "darwin-x64": "darwin-amd64", "darwin-arm64": "darwin-arm64",
    "linux-x64": "linux-amd64", "linux-arm64": "linux-arm64",
    "win32-x64": "windows-amd64",
  };
  return map[`${process.platform}-${process.arch}`];
}

function hasGo() {
  try { execSync("go version", { stdio: "pipe" }); return true; } catch (_) { return false; }
}

function hasGit() {
  try { execSync("git --version", { stdio: "pipe" }); return true; } catch (_) { return false; }
}

function ensureInstalled() {
  if (existsSync(BIN_PATH)) {
    try {
      const current = execSync(`"${BIN_PATH}" version`, { encoding: "utf8", stdio: "pipe", timeout: 5000 }).trim();
      if (current.includes(VERSION)) return;
      console.log(`Updating binary from ${current} to ${VERSION}...`);
    } catch (_) {}
  }

  mkdirSync(BIN_DIR, { recursive: true });

  const plat = platform();
  if (!plat) {
    console.error("Unsupported platform: " + process.platform + " " + process.arch);
    process.exit(1);
  }

  // ── Step 1: GitHub raw binary download (no tar, no Go, just curl) ──
  console.log("Installing api-switch " + VERSION + " for " + plat + "...");
  if (tryGitHubRaw(plat)) return;

  // ── Step 2: Gitee clone + build (China mirror, needs Go + Git) ──
  console.log("GitHub not reachable, trying Gitee mirror...");
  if (tryGiteeRaw(plat)) return;
  if (tryGiteeBuild(plat)) return;

  // ── Step 3: go install (needs Go only) ──
  if (tryGoInstall(plat)) return;

  // ── All failed: friendly error ──
  printInstallHelp(plat);
  process.exit(1);
}

function tryGitHubRaw(plat) {
  try {
    const url = IS_WIN
      ? `${GH_RELEASE}/api-switch-windows-amd64.exe`
      : `${GH_RELEASE}/api-switch-${plat}`;
    execSync(
      IS_WIN
        ? `powershell -Command "Invoke-WebRequest '${url}' -OutFile '${BIN_PATH}'"`
        : `curl -sSL --connect-timeout 10 --max-time 120 "${url}" -o "${BIN_PATH}"`,
      { stdio: "pipe", timeout: 150000 }
    );
    chmodSync(BIN_PATH, 0o755);
    console.log("  Done (GitHub)");
    return true;
  } catch (_) {
    console.log("  GitHub not reachable");
  }
  return false;
}

function tryGiteeRaw(plat) {
  try {
    const url = `${GITEE_RAW}/api-switch-${plat}`;
    execSync(`curl -sSL --connect-timeout 10 --max-time 120 "${url}" -o "${BIN_PATH}"`, { stdio: "pipe", timeout: 150000 });
    chmodSync(BIN_PATH, 0o755);
    console.log("  Done (Gitee)");
    return true;
  } catch (_) {}
  console.log("  Gitee download failed");
  return false;
}

function tryGiteeBuild(plat) {
  if (!hasGo() || !hasGit()) {
    console.log("  Go/Git not found, skipped");
    return false;
  }
  try {
    const tmp = join(__dirname, ".gitee-build");
    execSync(`rm -rf ${tmp} && git clone --depth=1 ${GITEE_REPO} ${tmp}`, { stdio: "pipe", timeout: 60000 });
    execSync(
      `cd ${tmp} && GOTOOLCHAIN=local GOPROXY=https://goproxy.cn,direct go build -ldflags="-s -w -X main.Version=${VERSION}" -o "${BIN_PATH}" ./cmd/api-switch/`,
      { stdio: "pipe", timeout: 120000 }
    );
    execSync(`rm -rf ${tmp}`, { stdio: "pipe" });
    chmodSync(BIN_PATH, 0o755);
    console.log("  Done (Gitee + go build)");
    return true;
  } catch (_) {}
  console.log("  Gitee build failed");
  return false;
}

function tryGoInstall(plat) {
  if (!hasGo()) {
    console.log("  Go not found, skipped");
    return false;
  }
  try {
    const gopath = (execSync("go env GOPATH", { encoding: "utf8", stdio: "pipe" }) || "~/go").trim();
    const goBin = join(gopath, "bin", BIN_NAME);
    execSync(
      `GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=local go install github.com/hxz0727/API-Switch/cmd/api-switch@${VERSION}`,
      { stdio: "pipe", timeout: 120000 }
    );
    if (existsSync(goBin)) {
      copyFileSync(goBin, BIN_PATH);
      chmodSync(BIN_PATH, 0o755);
      console.log("  Done (go install)");
      return true;
    }
  } catch (_) {}
  console.log("  go install failed");
  return false;
}

function printInstallHelp(plat) {
  console.log();
  console.log("  Unable to install api-switch automatically on this machine.");
  console.log();

  const hasGoInstalled = hasGo();
  const hasGitInstalled = hasGit();

  if (IS_WIN) {
    console.log("  Windows — download manually:");
    console.log("    " + GH_RELEASE + "/api-switch-windows-amd64.exe");
    console.log("    Save to: %APPDATA%\\npm\\api-switch.exe");
    console.log();
    console.log("  Or with PowerShell:");
    console.log('    Invoke-WebRequest "' + GH_RELEASE + '/api-switch-windows-amd64.exe" -OutFile $env:APPDATA\\npm\\api-switch.exe');
    return;
  }

  // Linux / macOS — show the best option based on what's available
  if (hasGitInstalled && hasGoInstalled) {
    console.log("  Fastest: clone from Gitee and build");
    console.log("    git clone --depth=1 " + GITEE_REPO);
    console.log("    cd API-Switch && GOTOOLCHAIN=local GOPROXY=https://goproxy.cn,direct go build -o api-switch ./cmd/api-switch/");
    console.log("    sudo cp api-switch /usr/local/bin/");
  } else if (hasGoInstalled) {
    console.log("  Install via go (needs Go, no Git):");
    console.log("    GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=local go install github.com/hxz0727/API-Switch/cmd/api-switch@" + VERSION);
    console.log("    sudo cp ~/go/bin/api-switch /usr/local/bin/");
  } else {
    console.log("  Option A: Install Go first, then clone from Gitee");
    console.log("    # Install Go (one-time):");
    console.log("    wget -q https://go.dev/dl/go1.23.4.linux-amd64.tar.gz");
    console.log("    sudo tar -C /usr/local -xzf go1.23.4.linux-amd64.tar.gz");
    console.log("    export PATH=/usr/local/go/bin:\\$PATH");
    console.log();
    console.log("    # Clone and build from Gitee (China mirror):");
    console.log("    git clone --depth=1 " + GITEE_REPO);
    console.log("    cd API-Switch && GOTOOLCHAIN=local GOPROXY=https://goproxy.cn,direct go build -o api-switch ./cmd/api-switch/");
    console.log("    sudo cp api-switch /usr/local/bin/");
    console.log();
    console.log("  Option B: Copy pre-built binary from another machine");
    console.log("    # On a machine with Go:");
    console.log("    go build -o api-switch ./cmd/api-switch/");
    console.log("    scp api-switch user@this-machine:/usr/local/bin/");
    console.log();
    console.log("  Option C: Download from GitHub (if accessible)");
    console.log("    curl -sSL \"" + GH_RELEASE + "/api-switch-" + plat + "\" -o api-switch");
    console.log("    chmod +x api-switch && sudo cp api-switch /usr/local/bin/");
  }
}

ensureInstalled();
const r = spawnSync(BIN_PATH, process.argv.slice(2), { stdio: "inherit" });
process.exit(r.status ?? 1);
