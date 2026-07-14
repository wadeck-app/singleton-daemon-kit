#!/usr/bin/env bash
# Computes the npm version string for this build.
#
# Format: 1.YYYYMMDD.HHMMSSbbb  (bbb = zero-padded build count)
# Examples: 1.20260712.195044019
#
# Why this format:
# - major (1) stays free for real API breaking changes
# - minor = YYYYMMDD (date), patch = HHMMSS + build count suffix
# - NO hyphens = NOT a prerelease in semver — ^1.0.0 matches all 1.x.x
# - Correct semver sort: later dates always sort higher; build count is
#   appended to the seconds to break ties within the same second
# - Consumers use ^1.0.0 which matches any 1.x.x release, no prerelease issues
#
# Outputs (to $GITHUB_OUTPUT or stdout when GITHUB_OUTPUT is unset):
#   version   e.g. 1.20260712.195044019
#   npm_tag   latest
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

DATE=$(date -u '+%Y%m%d')
TIME=$(date -u '+%H%M%S')
BUILD=$(git -C "$REPO_ROOT" rev-list --count HEAD)
SHA=$(git -C "$REPO_ROOT" rev-parse --short=8 HEAD)

VERSION="1.${DATE}.${TIME}$(printf '%03d' "$BUILD")"
NPM_TAG="latest"

OUT="${GITHUB_OUTPUT:-/dev/stdout}"
echo "version=${VERSION}" >> "$OUT"
echo "npm_tag=${NPM_TAG}" >> "$OUT"

echo "Computed: version=${VERSION} npm_tag=${NPM_TAG}" >&2
