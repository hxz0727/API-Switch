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

const { existsSync, mkdirSync, chmodSync, unlinkSync, copyFileSync, readFileSync } = require("fs");
const { join } = require("path");
const { execSync, spawnSync } = require("child_process");

const PKG = require("./package.json");
const VERSION = "v" + PKG.version;
const BIN_DIR = join(__dirname, ".bin");
const IS_WIN = process.platform === "win32";
const IS_MAC = process.platform === "darwin";
const BIN_NAME = IS_WIN ? "api-switch.exe" : "api-switch";
const BIN_PATH = join(BIN_DIR, BIN_NAME);

const GITEE_REPO = "https://gitee.com/776311606/API-Switch.git";
// Gitee raw URL with version subdirectory: release/vX.Y.Z/api-switch-{plat}
const GITEE_RAW  = `https://gitee.com/776311606/API-Switch/raw/release/${VERSION}`;
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

  // ── Step 1: Gitee raw binary download (China mirror, fast) ──
  console.log("Installing api-switch " + VERSION + " for " + plat + "...");
  if (tryGiteeRaw(plat)) return;

  // ── Step 2: GitHub raw binary download ──
  console.log("Gitee not reachable, trying GitHub...");
  if (tryGitHubRaw(plat)) return;

  // ── Step 3: Gitee clone + go build (ensures correct version) ──
  if (tryGiteeBuild(plat)) return;

  // ── Step 4: go install (needs Go only) ──
  if (tryGoInstall(plat)) return;

  // ── All failed: friendly error ──
  printInstallHelp(plat);
  process.exit(1);
}

function verifyBinary() {
  // Check that the file is a real binary, not an HTML error page
  if (!isValidBinary()) {
    console.log("  Downloaded file is not a valid binary (likely an HTML error page)");
    return false;
  }
  try {
    const ver = execSync(`"${BIN_PATH}" version`, { encoding: "utf8", stdio: "pipe", timeout: 5000 }).trim();
    // Strict version match: extract semver from output and compare
    const match = ver.match(/v?(\d+\.\d+\.\d+)/);
    if (!match) {
      console.log(`  Binary output unexpected: "${ver}"`);
      return false;
    }
    const binaryVer = match[1];
    const expectedVer = VERSION.replace(/^v/, "");
    if (binaryVer !== expectedVer) {
      console.log(`  Binary version mismatch: got v${binaryVer}, expected ${VERSION}`);
      return false;
    }
    return true;
  } catch (_) {
    return false;
  }
}

// isValidBinary checks the file magic bytes to ensure it's a real binary,
// not an HTML error page or other garbage downloaded from a broken URL.
function isValidBinary() {
  try {
    const buf = readFileSync(BIN_PATH);
    if (buf.length < 4) return false;
    // ELF (Linux): starts with \x7fELF
    if (buf[0] === 0x7f && buf[1] === 0x45 && buf[2] === 0x4c && buf[3] === 0x46) return true;
    // Mach-O (macOS): starts with various magic numbers
    if (IS_MAC) {
      const macho64 = buf.readUInt32LE(0);
      if (macho64 === 0xfeedfacf || macho64 === 0xcffaedfe ||
          macho64 === 0xfeedface || macho64 === 0xcefaedfe) return true;
    }
    // PE (Windows): starts with MZ
    if (buf[0] === 0x4d && buf[1] === 0x5a) return true;
    return false;
  } catch (_) {
    return false;
  }
}

function tryGitHubRaw(plat) {
  try {
    const url = IS_WIN
      ? `${GH_RELEASE}/api-switch-windows-amd64.exe`
      : `${GH_RELEASE}/api-switch-${plat}`;
    execSync(
      IS_WIN
        ? `powershell -Command "Invoke-WebRequest '${url}' -OutFile '${BIN_PATH}'"`
        : `curl -sSL --connect-timeout 5 --max-time 30 "${url}" -o "${BIN_PATH}"`,
      { stdio: "pipe", timeout: 40000 }
    );
    chmodSync(BIN_PATH, 0o755);
    if (!isValidBinary()) {
      console.log("  GitHub returned non-binary content (network issue or wrong URL)");
      return false;
    }
    if (!verifyBinary()) {
      console.log("  GitHub binary version mismatch, retrying...");
      return false;
    }
    console.log("  Done (GitHub)");
    return true;
  } catch (e) {
    console.log("  GitHub not reachable: " + (e.message || e));
  }
  return false;
}

function tryGiteeRaw(plat) {
  try {
    const url = `${GITEE_RAW}/api-switch-${plat}`;
    execSync(`curl -sSL --connect-timeout 5 --max-time 30 "${url}" -o "${BIN_PATH}"`, { stdio: "pipe", timeout: 40000 });
    chmodSync(BIN_PATH, 0o755);
    if (!isValidBinary()) {
      console.log("  Gitee returned non-binary content (the release branch may not have a binary yet)");
      return false;
    }
    if (!verifyBinary()) {
      console.log("  Gitee binary is outdated, trying build instead...");
      return false;
    }
    console.log("  Done (Gitee)");
    return true;
  } catch (e) {
    console.log("  Gitee download failed: " + (e.message || e));
  }
  return false;
}

function tryGiteeBuild(plat) {
  if (!hasGo() || !hasGit()) {
    if (!hasGit()) console.log("  Git not found, skipped");
    if (!hasGo()) console.log("  Go not found, skipped");
    return false;
  }
  try {
    const tmp = join(__dirname, ".gitee-build");
    execSync(`rm -rf ${tmp} && git clone --depth=1 ${GITEE_REPO} ${tmp}`, { stdio: "pipe", timeout: 60000 });
    execSync(
      `cd ${tmp} && GOTOOLCHAIN=local GOPROXY=https://goproxy.cn,direct go build -ldflags="-s -w -X main.Version=${VERSION.replace(/^v/, '')}" -o "${BIN_PATH}" ./cmd/api-switch/`,
      { stdio: "pipe", timeout: 120000 }
    );
    execSync(`rm -rf ${tmp}`, { stdio: "pipe" });
    chmodSync(BIN_PATH, 0o755);
    if (!verifyBinary()) {
      console.log("  Gitee build produced wrong version");
      return false;
    }
    console.log("  Done (Gitee + go build)");
    return true;
  } catch (e) {
    const msg = (e.stderr || e.message || "").toString().split("\n").filter(Boolean).slice(-3).join("\n    ");
    console.log("  Gitee build failed: " + (msg || e.message));
  }
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
      if (!verifyBinary()) {
        console.log("  go install produced wrong version");
        return false;
      }
      console.log("  Done (go install)");
      return true;
    }
    console.log("  go install completed but binary not found at " + goBin);
  } catch (e) {
    const msg = (e.stderr || e.message || "").toString().split("\n").filter(Boolean).slice(-3).join("\n    ");
    console.log("  go install failed: " + (msg || e.message));
  }
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

// Verify binary one final time before execution
if (!existsSync(BIN_PATH)) {
  console.error("Error: binary not found at " + BIN_PATH);
  process.exit(1);
}

if (!isValidBinary()) {
  console.error("Error: " + BIN_PATH + " is not a valid executable (may have been corrupted during download)");
  process.exit(1);
}

const r = spawnSync(BIN_PATH, process.argv.slice(2), { stdio: "inherit" });
if (r.error) {
  if (r.error.code === "ENOENT") {
    console.error("Error: cannot execute " + BIN_PATH + " — file may be missing or corrupted");
  } else if (r.error.code === "EACCES") {
    console.error("Error: permission denied executing " + BIN_PATH + " — try reinstalling");
  } else {
    console.error("Error executing binary: " + r.error.message);
  }
  process.exit(1);
}
process.exit(r.status ?? 1);
