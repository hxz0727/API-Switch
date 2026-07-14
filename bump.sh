#!/usr/bin/env bash
# bump.sh — Thin wrapper around release.sh for backward compatibility
# Use "./release.sh" directly for the full release workflow with pre-flight checks.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"

if [ $# -eq 0 ] || [ "$1" = "help" ] || [ "$1" = "--help" ]; then
  exec "$ROOT/release.sh"
fi

exec "$ROOT/release.sh" "$@"
