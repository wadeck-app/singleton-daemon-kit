#!/usr/bin/env bash
# Computes the npm version string for this build.
#
# Format: 1.0.YYYYMMDD-HHMMSS-BUILD-SHA
# Examples: 1.0.20260712-195044-142-a3f2b1c4
#
# Why this format:
# - major.minor (1.0) stay free for real API breaking changes
# - ^1.0.0 in consumers works forever (no year rollover breakage)
# - YYYYMMDD-HHMMSS-BUILD ensures correct semver sort for multiple releases/day
# - BUILD (commit count) is monotone and acts as tiebreaker
#
# Outputs (to $GITHUB_OUTPUT or stdout when GITHUB_OUTPUT is unset):
#   version   e.g. 1.0.20260712-195044-142-a3f2b1c4
#   npm_tag   latest
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

DATE=$(date -u '+%Y%m%d')
TIME=$(date -u '+%H%M%S')
BUILD=$(git -C "$REPO_ROOT" rev-list --count HEAD)
SHA=$(git -C "$REPO_ROOT" rev-parse --short=8 HEAD)

VERSION="1.0.${DATE}-${TIME}-${BUILD}-${SHA}"
NPM_TAG="latest"

OUT="${GITHUB_OUTPUT:-/dev/stdout}"
echo "version=${VERSION}" >> "$OUT"
echo "npm_tag=${NPM_TAG}" >> "$OUT"

echo "Computed: version=${VERSION} npm_tag=${NPM_TAG}" >&2
