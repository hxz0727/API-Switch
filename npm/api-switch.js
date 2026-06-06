#!/usr/bin/env node

/**
 * API-Switch — lazy-install launcher
 *
 * On first run, installs the api-switch binary (via go/gitee/github),
 * then execs it with the original arguments.
 * Subsequent runs use the cached binary directly.
 *
 * Usage: api-switch <args...>
 */

"use strict";

const { existsSync, mkdirSync, chmodSync, unlinkSync, copyFileSync } = require("fs");
const { join } = require("path");
const { execSync, spawnSync } = require("child_process");

const VERSION = "v0.3.0";
const BIN_DIR = join(__dirname, ".bin");
const BIN_NAME = process.platform === "win32" ? "api-switch.exe" : "api-switch";
const BIN_PATH = join(BIN_DIR, BIN_NAME);

const GITEE_REPO = "https://gitee.com/776311606/API-Switch.git";
const GH_RELEASE = `https://github.com/hxz0727/API-Switch/releases/download/${VERSION}`;

function platform() {
  const map = {
    "darwin-x64": "darwin-amd64", "darwin-arm64": "darwin-arm64",
    "linux-x64": "linux-amd64", "linux-arm64": "linux-arm64",
    "win32-x64": "windows-amd64",
  };
  return map[`${process.platform}-${process.arch}`];
}

// ── Install binary (lazy, on first use) ──
function ensureInstalled() {
  if (existsSync(BIN_PATH)) return;

  mkdirSync(BIN_DIR, { recursive: true });
  console.log("First run — installing api-switch...");

  // 1. go install via goproxy.cn
  try {
    const gopath = (execSync("go env GOPATH", { encoding: "utf8", stdio: "pipe" }) || "~/go").trim();
    const goBin = join(gopath, "bin", BIN_NAME);
    execSync(`GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=local go install github.com/hxz0727/API-Switch/cmd/api-switch@${VERSION}`, { stdio: "pipe", timeout: 120000 });
    if (existsSync(goBin)) {
      copyFileSync(goBin, BIN_PATH);
      chmodSync(BIN_PATH, 0o755);
      console.log("Installed via go install.");
      return;
    }
  } catch (_) {}

  // 2. Clone from Gitee + build
  try {
    console.log("Trying Gitee mirror...");
    const tmp = join(__dirname, ".gitee-build");
    execSync(`rm -rf ${tmp} && git clone --depth=1 ${GITEE_REPO} ${tmp}`, { stdio: "pipe", timeout: 60000 });
    execSync(`cd ${tmp} && GOTOOLCHAIN=local GOPROXY=https://goproxy.cn,direct go build -ldflags="-s -w -X main.Version=${VERSION}" -o "${BIN_PATH}" ./cmd/api-switch/`, { stdio: "pipe", timeout: 120000 });
    execSync(`rm -rf ${tmp}`, { stdio: "pipe" });
    chmodSync(BIN_PATH, 0o755);
    console.log("Installed via Gitee mirror.");
    return;
  } catch (_) {}

  // 3. GitHub release download
  const plat = platform();
  if (plat) {
    try {
      console.log("Trying GitHub release...");
      const isWin = process.platform === "win32";
      const archive = `api-switch-${plat}${isWin ? ".zip" : ".tar.gz"}`;
      const url = `${GH_RELEASE}/${archive}`;
      const tmp = join(__dirname, ".gh-download");
      execSync(`curl -sSL --connect-timeout 10 --max-time 120 "${url}" -o "${tmp}"`, { stdio: "pipe", timeout: 150000 });
      if (isWin) {
        execSync(`unzip -o "${tmp}" "api-switch-${plat}.exe"`, { stdio: "pipe" });
        renameSync(`api-switch-${plat}.exe`, BIN_PATH);
      } else {
        execSync(`tar xzf "${tmp}" -C "${BIN_DIR}" "api-switch-${plat}"`, { stdio: "pipe" });
        execSync(`mv "${join(BIN_DIR, 'api-switch-' + plat)}" "${BIN_PATH}"`, { stdio: "pipe" });
      }
      unlinkSync(tmp);
      chmodSync(BIN_PATH, 0o755);
      console.log("Installed via GitHub.");
      return;
    } catch (_) {}
  }

  console.error();
  console.error("Installation failed. Run manually:");
  console.error(`  git clone --depth=1 ${GITEE_REPO}`);
  console.error("  cd API-Switch && GOTOOLCHAIN=local GOPROXY=https://goproxy.cn,direct go build -o api-switch ./cmd/api-switch/");
  console.error("  cp api-switch /usr/local/bin/");
  process.exit(1);
}

// ── Entry ──
ensureInstalled();
const r = spawnSync(BIN_PATH, process.argv.slice(2), { stdio: "inherit" });
process.exit(r.status ?? 1);
