#!/usr/bin/env bash
# bump.sh — Unified version management for API-Switch
#
# Usage:
#   ./bump.sh                  Show current versions
#   ./bump.sh <new-version>    Bump everything to the given version
#   ./bump.sh --npm-only       Only update npm files (no git tag/build)
#   ./bump.sh --no-publish     Skip npm publish
#
# The version format is vX.Y.Z (with leading 'v').
# npm package.json uses X.Y.Z (without 'v').

set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

red()    { echo -e "\033[31m$*\033[0m"; }
green()  { echo -e "\033[32m$*\033[0m"; }
yellow() { echo -e "\033[33m$*\033[0m"; }

current_version() {
  grep -oP 'const VERSION = "\K[^"]+' npm/api-switch.js 2>/dev/null || echo "unknown"
}

npm_version() {
  grep -oP '"version":\s*"\K[^"]+' npm/package.json 2>/dev/null || echo "unknown"
}

VER="${1:-}"

if [ -z "$VER" ]; then
  echo "Current version: $(current_version)"
  echo "Current npm:      $(npm_version)"
  echo ""
  echo "Usage: $0 <new-version> [--no-publish] [--npm-only]"
  echo "  e.g.  $0 v0.4.7"
  exit 0
fi

# Parse flags
NPM_ONLY=false
NO_PUBLISH=false
for arg in "$@"; do
  case "$arg" in
    --npm-only)   NPM_ONLY=true ;;
    --no-publish) NO_PUBLISH=true ;;
  esac
done

# Validate version format
if [[ ! "$VER" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  red "Invalid version format: $VER"
  echo "Expected: vX.Y.Z (e.g. v0.4.7)"
  exit 1
fi

NPM_VER="${VER#v}"

echo ""
echo "  Bumping to $VER (npm $NPM_VER)..."
echo ""

# 1. Update npm wrapper version
sed -i "s/const VERSION = \"[^\"]*\"/const VERSION = \"$VER\"/" npm/api-switch.js
sed -i "s/const DOWNLOAD_VERSION = \"[^\"]*\"/const DOWNLOAD_VERSION = \"$VER\"/" npm/install.js
green "  npm/api-switch.js → $VER"
green "  npm/install.js     → $VER"

# 2. Update npm package.json version
sed -i "s/\"version\": \"[^\"]*\"/\"version\": \"$NPM_VER\"/" npm/package.json
green "  npm/package.json   → $NPM_VER"

# 3. Verify
ACTUAL=$(current_version)
ACTUAL_NPM=$(npm_version)
if [ "$ACTUAL" != "$VER" ]; then
  red "  api-switch.js VERSION mismatch: expected $VER, got $ACTUAL"
  exit 1
fi
if [ "$ACTUAL_NPM" != "$NPM_VER" ]; then
  red "  package.json version mismatch: expected $NPM_VER, got $ACTUAL_NPM"
  exit 1
fi
green "  All version references consistent"
echo ""

if $NPM_ONLY; then
  green "Done (npm only)."
  exit 0
fi

# 4. Build binary
echo "  Building binary..."
if go build -ldflags="-s -w -X main.Version=$VER" -o /tmp/api-switch-release ./cmd/api-switch/ 2>&1; then
  VERIFIED=$("/tmp/api-switch-release" version 2>/dev/null || echo "FAIL")
  green "  Binary built: $VERIFIED"
else
  yellow "  Build failed (check Go environment)"
fi
echo ""

# 5. Commit + tag + push
git add npm/api-switch.js npm/install.js npm/package.json
git commit -m "release: $VER" 2>&1
git tag -d "$VER" 2>/dev/null || true
git tag "$VER"
git push origin master --tags
green "  Pushed to GitHub ($VER)"
echo ""

# 6. npm publish (token from env or ~/.npmrc)
if $NO_PUBLISH; then
  yellow "  Skipping npm publish (--no-publish)"
else
  echo "  Publishing to npm..."
  NPM_TOKEN="${NPM_TOKEN:-}"
  if [ -z "$NPM_TOKEN" ] && [ -f ~/.npmrc ]; then
    NPM_TOKEN=$(grep -oP '//registry.npmjs.org/:_authToken=\K.*' ~/.npmrc 2>/dev/null | head -1 || true)
  fi
  if [ -z "$NPM_TOKEN" ]; then
    yellow "  NPM_TOKEN not set. Set it via environment or ~/.npmrc"
    yellow "    export NPM_TOKEN=npm_xxx"
    yellow "    npm login"
    exit 1
  fi
  cd npm
  echo "//registry.npmjs.org/:_authToken=$NPM_TOKEN" > .npmrc
  npm publish --access public 2>&1
  rm -f .npmrc
  cd "$ROOT"
  green "  Published to npm"
fi
echo ""
green "  Done! $VER"
echo ""
echo "  Manual steps:"
echo "    ./bump.sh --sync-gitee    (push tags to Gitee + update release branch)"
