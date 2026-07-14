#!/usr/bin/env bash
# Computes the npm version string for this build.
#
# Format: 1.YYYYMMDDHHMMSS.BUILD
# Examples: 1.20260714191529.25
#
# Why this format:
# - major (1) stays free for real API breaking changes
# - minor = YYYYMMDDHHMMSS — date+time compact, no separators, pure integer, sortable
# - patch = BUILD (git commit count) — monotone, unique tiebreaker for same-second builds
# - NO hyphens = NOT a semver prerelease — ^1.0.0 matches all 1.x.x
# - SHA is visible in the tarball filename logged by npm during publish
#
# Outputs (to $GITHUB_OUTPUT or stdout when GITHUB_OUTPUT is unset):
#   version   e.g. 1.20260714191529.25
#   npm_tag   latest
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

DATETIME=$(date -u '+%Y%m%d%H%M%S')
BUILD=$(git -C "$REPO_ROOT" rev-list --count HEAD)

VERSION="1.${DATETIME}.${BUILD}"
NPM_TAG="latest"

OUT="${GITHUB_OUTPUT:-/dev/stdout}"
echo "version=${VERSION}" >> "$OUT"
echo "npm_tag=${NPM_TAG}" >> "$OUT"

echo "Computed: version=${VERSION} npm_tag=${NPM_TAG}" >&2
