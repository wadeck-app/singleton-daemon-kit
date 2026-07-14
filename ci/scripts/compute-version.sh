#!/usr/bin/env bash
# Computes the npm version string for this build.
#
# Format: 1.YYYYMMDD.HHMMSS+BUILD.SHA
# Examples: 1.20260714.183622+23.70228b01
#
# Why this format:
# - major (1) stays free for real API breaking changes
# - minor = YYYYMMDD, patch = HHMMSS — human-readable date/time, no hyphens
# - NO hyphens in MAJOR.MINOR.PATCH = NOT a semver prerelease — ^1.0.0 matches
# - +BUILD.SHA = semver build metadata, ignored by range comparisons
#   - BUILD (commit count) is monotone and guarantees no collision
#   - SHA allows tracing the exact commit
# - Correct semver sort: later dates/times always sort higher
#
# Outputs (to $GITHUB_OUTPUT or stdout when GITHUB_OUTPUT is unset):
#   version   e.g. 1.20260714.183622+23.70228b01
#   npm_tag   latest
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

DATE=$(date -u '+%Y%m%d')
TIME=$(date -u '+%H%M%S')
BUILD=$(git -C "$REPO_ROOT" rev-list --count HEAD)
SHA=$(git -C "$REPO_ROOT" rev-parse --short=8 HEAD)

VERSION="1.${DATE}.${TIME}+${BUILD}.${SHA}"
NPM_TAG="latest"

OUT="${GITHUB_OUTPUT:-/dev/stdout}"
echo "version=${VERSION}" >> "$OUT"
echo "npm_tag=${NPM_TAG}" >> "$OUT"

echo "Computed: version=${VERSION} npm_tag=${NPM_TAG}" >&2
