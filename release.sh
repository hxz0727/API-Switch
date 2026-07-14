#!/usr/bin/env bash
# release.sh — Complete release workflow for API-Switch
#
# Usage:
#   ./release.sh                    Show current version + checklist
#   ./release.sh vX.Y.Z             Full release
#   ./release.sh vX.Y.Z --dry-run   Show what would happen without doing it
#   ./release.sh vX.Y.Z --skip-npm  Skip npm publish (token issues)
#   ./release.sh vX.Y.Z --skip-test Skip pre-release tests (emergency only)
#
# Required environment variables:
#   GITEE_TOKEN   Gitee API token for creating releases (optional, skips release page)
#   NPM_TOKEN      npm publish token (optional, skips npm)

set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

GITEE_TOKEN="${GITEE_TOKEN:-}"
NPM_TOKEN="${NPM_TOKEN:-}"

red()    { echo -e "\033[31m$*\033[0m"; }
green()  { echo -e "\033[32m$*\033[0m"; }
yellow() { echo -e "\033[33m$*\033[0m"; }
bold()   { echo -e "\033[1m$*\033[0m"; }

# ── Argument parsing ──────────────────────────────────────────────

DRY_RUN=false
SKIP_NPM=false
SKIP_TEST=false
VER="${1:-}"

shift 2>/dev/null || true
for arg in "$@"; do
  case "$arg" in
    --dry-run)   DRY_RUN=true ;;
    --skip-npm)  SKIP_NPM=true ;;
    --skip-test) SKIP_TEST=true ;;
    *)           red "Unknown flag: $arg"; exit 1 ;;
  esac
done

if [ -z "$VER" ]; then
  bold "API-Switch Release Tool"
  echo ""
  echo "  Current main.go:     $(grep -oP 'var Version = "\K[^"]+' cmd/api-switch/main.go)"
  echo "  Current package.json: $(grep -oP '"version":\s*"\K[^"]+' npm/package.json)"
  echo ""
  echo "Usage: $0 <version> [--dry-run] [--skip-npm] [--skip-test]"
  echo "  $0 v0.9.3              # Full release"
  echo "  $0 v0.9.3 --skip-npm   # Skip npm publish"
  exit 0
fi

if [[ ! "$VER" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  red "Invalid version: $VER (expected vX.Y.Z)"
  exit 1
fi

NPM_VER="${VER#v}"

# ── Step 0: Pre-release checks ───────────────────────────────────

echo ""
bold "═══ Phase 0: Pre-release Checks ═══"
echo ""

# Check clean git
if ! git diff --quiet 2>/dev/null; then
  red "  ✗ Git working tree is dirty — commit or stash changes first"
  git status --short
  exit 1
fi
green "  ✓ Git working tree clean"

# Check go environment
if ! command -v go &>/dev/null; then
  red "  ✗ Go not found"
  exit 1
fi
green "  ✓ Go $(go version | awk '{print $3}')"

# Run tests
if ! $SKIP_TEST; then
  echo -n "  Running tests..."
  if go test ./... > /tmp/release-test.log 2>&1; then
    green " ✓ All tests pass"
  else
    red "  ✗ Tests failed:"
    cat /tmp/release-test.log | tail -20
    exit 1
  fi
  rm -f /tmp/release-test.log
fi

# Run vet
echo -n "  Running go vet..."
if go vet ./... > /tmp/release-vet.log 2>&1; then
  green " ✓ No issues"
else
  red "  ✗ go vet found issues:"
  cat /tmp/release-vet.log
  exit 1
fi
rm -f /tmp/release-vet.log

# Check version consistency
CURRENT_GO=$(grep -oP 'var Version = "\K[^"]+' cmd/api-switch/main.go)
CURRENT_NPM=$(grep -oP '"version":\s*"\K[^"]+' npm/package.json)
if [ "$CURRENT_GO" != "$CURRENT_NPM" ]; then
  red "  ✗ Version mismatch: main.go=$CURRENT_GO, package.json=$CURRENT_NPM"
  exit 1
fi
green "  ✓ Version consistent: $CURRENT_GO"

# Check CHANGELOG.md exists and has new version section
if grep -q "^## $VER" CHANGELOG.md 2>/dev/null; then
  green "  ✓ CHANGELOG.md has $VER entry"
else
  yellow "  ⚠ CHANGELOG.md missing $VER entry — add before releasing:"
  yellow "    ## $VER ($(date +%Y-%m-%d))"
  echo ""
  read -p "  Continue without CHANGELOG? [y/N] " yn
  if [ "${yn,,}" != "y" ]; then exit 1; fi
fi

# Check npm login status (if not skipping npm)
if ! $SKIP_NPM; then
  if [ -n "$NPM_TOKEN" ]; then
    echo "//registry.npmjs.org/:_authToken=$NPM_TOKEN" > /tmp/.npmrc-test
    if npm whoami --registry https://registry.npmjs.org --userconfig /tmp/.npmrc-test >/dev/null 2>&1; then
      green "  ✓ npm authenticated as $(npm whoami --registry https://registry.npmjs.org --userconfig /tmp/.npmrc-test 2>/dev/null)"
    else
      yellow "  ⚠ npm authentication failed — publish will be skipped"
      SKIP_NPM=true
    fi
    rm -f /tmp/.npmrc-test
  else
    yellow "  ⚠ NPM_TOKEN not set — publish will be skipped"
    SKIP_NPM=true
  fi
fi

if $DRY_RUN; then
  echo ""
  green "  Dry run complete — no changes made."
  exit 0
fi

echo ""

# ── Step 1: Bump versions ────────────────────────────────────────

bold "═══ Phase 1: Bump versions ═══"
echo ""

# Update main.go
sed -i "s/var Version = \"[^\"]*\"/var Version = \"$NPM_VER\"/" cmd/api-switch/main.go
green "  cmd/api-switch/main.go → $NPM_VER"

# Update package.json
sed -i "s/\"version\": \"[^\"]*\"/\"version\": \"$NPM_VER\"/" npm/package.json
green "  npm/package.json      → $NPM_VER"

# Verify
ACTUAL_GO=$(grep -oP 'var Version = "\K[^"]+' cmd/api-switch/main.go)
ACTUAL_NPM=$(grep -oP '"version":\s*"\K[^"]+' npm/package.json)
if [ "$ACTUAL_GO" != "$NPM_VER" ] || [ "$ACTUAL_NPM" != "$NPM_VER" ]; then
  red "  Version bump failed — run manually"
  exit 1
fi

echo ""

# ── Step 2: Build & verify ───────────────────────────────────────

bold "═══ Phase 2: Build Binary ═══"
echo ""

PLAT="linux-amd64"
BINARY="/tmp/api-switch-$PLAT"
CHECKSUMS="/tmp/checksums.txt"

echo "  Building $PLAT..."
go build -ldflags="-s -w -X main.Version=$NPM_VER" -o "$BINARY" ./cmd/api-switch/ 2>/tmp/build-err.log
if [ $? -ne 0 ]; then
  red "  Build failed:"
  cat /tmp/build-err.log
  exit 1
fi

# Verify version output
VER_OUTPUT=$("$BINARY" version 2>/dev/null || echo "FAIL")
if [ "$VER_OUTPUT" != "api-switch version $NPM_VER" ]; then
  red "  Version mismatch: expected 'api-switch version $NPM_VER', got '$VER_OUTPUT'"
  exit 1
fi
green "  ✓ Binary: $VER_OUTPUT"

# Verify binary size (must be > 1MB)
SIZE=$(stat -c%s "$BINARY" 2>/dev/null || stat -f%z "$BINARY" 2>/dev/null)
if [ "$SIZE" -lt 1048576 ]; then
  red "  ✗ Binary too small: $SIZE bytes (< 1MB)"
  exit 1
fi
green "  ✓ Binary size: $(( SIZE / 1024 / 1024 ))MB"

# Generate checksums
sha256sum "$BINARY" 2>/dev/null | awk -v name="api-switch-$PLAT" '{print $1 "  " name}' > "$CHECKSUMS"
green "  ✓ Checksums generated: $(head -1 $CHECKSUMS)"
echo ""

# ── Step 3: Commit & tag ─────────────────────────────────────────

bold "═══ Phase 3: Commit + Tag + Push ═══"
echo ""

git add cmd/api-switch/main.go npm/package.json

# Check if CHANGELOG was modified
if ! git diff --cached --quiet CHANGELOG.md 2>/dev/null; then
  git add CHANGELOG.md
  echo "  Including CHANGELOG.md in commit"
fi

git commit -m "release: $VER" 2>&1
green "  ✓ Committed"

git tag -d "$VER" 2>/dev/null || true
git tag -a "$VER" -m "$VER" 2>&1
green "  ✓ Tagged $VER"

echo "  Pushing to GitHub..."
git push origin master --tags 2>&1
green "  ✓ GitHub"

echo "  Pushing to Gitee..."
if git remote get-url gitee >/dev/null 2>&1; then
  git push gitee master --tags 2>&1
  green "  ✓ Gitee"
else
  yellow "  ⚠ Gitee remote not configured — skipping"
fi
echo ""

# ── Step 4: Gitee release assets ─────────────────────────────────

bold "═══ Phase 4: Gitee Release Assets ═══"
echo ""

if git remote get-url gitee >/dev/null 2>&1; then
  echo "  Uploading binary + checksums to Gitee release branch..."
  rm -rf /tmp/gitee-release
  if git clone -b release "https://gitee.com/776311606/API-Switch.git" /tmp/gitee-release 2>/dev/null; then
    cd /tmp/gitee-release
    mkdir -p "$VER"
    BINARY_NAME="api-switch-$PLAT"
    cp "$BINARY" "$VER/$BINARY_NAME"
    cp "$CHECKSUMS" "$VER/checksums.txt"
    git add -f "$VER/$BINARY_NAME" "$VER/checksums.txt"
    git commit -m "release: $VER $PLAT binary + checksums" 2>&1 || true
    git push origin release 2>&1
    cd "$ROOT"
    rm -rf /tmp/gitee-release
    green "  ✓ Release branch updated: $VER"
  else
    yellow "  ⚠ Could not clone Gitee release branch — skipping"
  fi

  # Create Gitee release page
  if [ -n "$GITEE_TOKEN" ]; then
    echo "  Creating Gitee release page..."
    curl -s -X POST "https://gitee.com/api/v5/repos/776311606/API-Switch/releases" \
      -H "Content-Type: application/json" \
      -H "Authorization: token $GITEE_TOKEN" \
      -d "{\"tag_name\":\"$VER\",\"name\":\"$VER\",\"body\":\"See CHANGELOG.md for details.\",\"target_commitish\":\"master\",\"prerelease\":false}" > /tmp/gitee-release.json 2>/dev/null
    RELEASE_ID=$(python3 -c "import json; print(json.load(open('/tmp/gitee-release.json')).get('id',''))" 2>/dev/null || echo "")
    if [ -n "$RELEASE_ID" ]; then
      green "  ✓ Gitee release created: ID $RELEASE_ID"
    else
      yellow "  ⚠ Gitee API failed — create release manually"
    fi
    rm -f /tmp/gitee-release.json
  else
    yellow "  ⚠ GITEE_TOKEN not set — create release page manually:"
    yellow "    https://gitee.com/776311606/API-Switch/releases/new"
  fi
fi
echo ""

# ── Step 5: npm publish ──────────────────────────────────────────

bold "═══ Phase 5: npm Publish ═══"
echo ""

if $SKIP_NPM; then
  yellow "  Skipping npm (--skip-npm or no token)"
else
  echo "  Publishing api-switch-cc@$NPM_VER..."
  echo "//registry.npmjs.org/:_authToken=$NPM_TOKEN" > npm/.npmrc
  if npm publish --access public --prefix npm 2>&1; then
    green "  ✓ Published to npm"
  else
    red "  ✗ npm publish failed — check token permissions (needs 2FA bypass)"
  fi
  rm -f npm/.npmrc
fi
echo ""

# ── Step 6: Post-release verification ────────────────────────────

bold "═══ Phase 6: Post-release Verification ═══"
echo ""

# Verify tags on remotes
echo "  GitHub tag: $(git ls-remote --tags origin "$VER" | cut -f1)"
if git remote get-url gitee >/dev/null 2>&1; then
  echo "  Gitee  tag: $(git ls-remote --tags gitee "$VER" | cut -f1)"
fi

# Verify npm
if npm view "api-switch-cc@$NPM_VER" version 2>/dev/null; then
  green "  ✓ npm: api-switch-cc@$NPM_VER available"
else
  yellow "  ⚠ npm package not found (may take a moment to propagate)"
fi
echo ""

# ── Cleanup ──────────────────────────────────────────────────────

rm -f "$BINARY" "$CHECKSUMS" /tmp/build-err.log

green "═══ Release $VER complete! ═══"
echo ""
echo "  Verify locally:"
echo "    npm install -g api-switch-cc@$NPM_VER"
echo "    api-switch version"
echo ""
echo "  Verify remotely:"
echo "    api-switch update"
