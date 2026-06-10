#!/usr/bin/env node

/**
 * API-Switch — lazy-install launcher
 *
 * On first run, installs the api-switch binary, then execs it.
 * Subsequent runs use the cached binary directly.
 *
 * Install order: Gitee release → GitHub release → Gitee clone+build → go install
 */

"use strict";

const { existsSync, mkdirSync, chmodSync, unlinkSync, copyFileSync } = require("fs");
const { join } = require("path");
const { execSync, spawnSync } = require("child_process");

const VERSION = "v0.4.0";
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

function binName() { return BIN_NAME; }

function ensureInstalled() {
  // Reinstall if binary is outdated (version mismatch)
  if (existsSync(BIN_PATH)) {
    try {
      const current = execSync(`"${BIN_PATH}" version`, { encoding: "utf8", stdio: "pipe", timeout: 5000 }).trim();
      if (current.includes(VERSION)) return;
      console.log(`Updating binary from ${current} to ${VERSION}...`);
    } catch (_) {}
  }

  mkdirSync(BIN_DIR, { recursive: true });
  console.log("First run — installing api-switch...");

  const plat = platform();
  if (!plat) return showManualSteps();

  // 1. Gitee release download (fast in China, no Go/Git needed)
  if (tryGiteeDownload(plat)) return;

  // 2. GitHub release download (fallback)
  if (tryGitHubDownload(plat)) return;

  // 3. Gitee clone + build (requires Go + Git)
  if (tryGiteeBuild()) return;

  // 4. go install (requires Go)
  if (tryGoInstall()) return;

  showManualSteps();
}

function tryGiteeDownload(plat) {
  try {
    console.log("Downloading from Gitee mirror...");
    const url = `${GITEE_RAW}/api-switch-${plat}`;
    execSync(`curl -sSL --connect-timeout 10 --max-time 120 "${url}" -o "${BIN_PATH}"`, { stdio: "pipe", timeout: 150000 });
    chmodSync(BIN_PATH, 0o755);
    console.log("Installed via Gitee.");
    return true;
  } catch (e) {
    console.error("  Gitee download failed: " + e.message.split("\n")[0].slice(0, 80));
  }
  return false;
}

function tryGitHubDownload(plat) {
  try {
    console.log("Downloading from GitHub...");
    const archive = `api-switch-${plat}${IS_WIN ? ".exe" : ".tar.gz"}`;
    const url = `${GH_RELEASE}/${archive}`;
    const tmp = join(__dirname, IS_WIN ? "_.tmp.exe" : "_.tmp.tar.gz");
    execSync(
      IS_WIN
        ? `powershell -Command "Invoke-WebRequest '${url}' -OutFile '${tmp}'"`
        : `curl -sSL --connect-timeout 10 --max-time 120 "${url}" -o "${tmp}"`,
      { stdio: "pipe", timeout: 150000 }
    );
    if (IS_WIN) {
      execSync(`move /Y "${tmp}" "${BIN_PATH}"`, { stdio: "pipe", shell: true });
    } else {
      try {
        execSync(`tar xzf "${tmp}" -C "${BIN_DIR}" "${archive}"`, { stdio: "pipe" });
        execSync(`mv "${join(BIN_DIR, archive)}" "${BIN_PATH}"`, { stdio: "pipe" });
        unlinkSync(tmp);
      } catch (_) {
        try { unlinkSync(tmp); } catch (_) {}
        const rawUrl = `${GH_RELEASE}/api-switch-${plat}`;
        execSync(`curl -sSL --connect-timeout 10 --max-time 120 "${rawUrl}" -o "${BIN_PATH}"`, { stdio: "pipe", timeout: 150000 });
      }
    }
    chmodSync(BIN_PATH, 0o755);
    console.log("Installed via GitHub.");
    return true;
  } catch (e) {
    console.error("  GitHub download failed: " + e.message.split("\n")[0].slice(0, 80));
  }
  return false;
}

function tryGiteeBuild() {
  try {
    console.log("Trying Gitee mirror (needs Go + Git)...");
    const tmp = join(__dirname, ".gitee-build");
    execSync(`rm -rf ${tmp} && git clone --depth=1 ${GITEE_REPO} ${tmp}`, { stdio: "pipe", timeout: 60000 });
    execSync(
      `cd ${tmp} && GOTOOLCHAIN=local GOPROXY=https://goproxy.cn,direct go build -ldflags="-s -w -X main.Version=${VERSION}" -o "${BIN_PATH}" ./cmd/api-switch/`,
      { stdio: "pipe", timeout: 120000 }
    );
    execSync(`rm -rf ${tmp}`, { stdio: "pipe" });
    chmodSync(BIN_PATH, 0o755);
    console.log("Installed via Gitee.");
    return true;
  } catch (_) {}
  return false;
}

function tryGoInstall() {
  try {
    console.log("Trying go install (needs Go)...");
    const gopath = (execSync("go env GOPATH", { encoding: "utf8", stdio: "pipe" }) || "~/go").trim();
    const goBin = join(gopath, "bin", BIN_NAME);
    execSync(
      `GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=local go install github.com/hxz0727/API-Switch/cmd/api-switch@${VERSION}`,
      { stdio: "pipe", timeout: 120000 }
    );
    if (existsSync(goBin)) {
      copyFileSync(goBin, BIN_PATH);
      chmodSync(BIN_PATH, 0o755);
      console.log("Installed via go install.");
      return true;
    }
  } catch (_) {}
  return false;
}

function showManualSteps() {
  console.error();
  console.error("Installation failed. Try manually:");
  console.error();
  if (IS_WIN) {
    console.error(`  curl -sSL "${GH_RELEASE}/api-switch-windows-amd64.exe" -o %APPDATA%\\npm\\api-switch.exe`);
  } else {
    console.error(`  curl -sSL "${GITEE_RAW}/api-switch-linux-amd64" -o api-switch`);
    console.error("  chmod +x api-switch && sudo cp api-switch /usr/local/bin/");
    console.error();
    console.error("  # Or clone and build:");
    console.error(`  git clone --depth=1 ${GITEE_REPO}`);
    console.error("  cd API-Switch && GOTOOLCHAIN=local GOPROXY=https://goproxy.cn,direct go build -o api-switch ./cmd/api-switch/");
    console.error("  cp api-switch /usr/local/bin/");
  }
  process.exit(1);
}

ensureInstalled();
const r = spawnSync(BIN_PATH, process.argv.slice(2), { stdio: "inherit" });
process.exit(r.status ?? 1);
