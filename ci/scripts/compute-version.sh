#!/usr/bin/env bash
# Computes the npm version string for this build.
#
# Inputs (env vars set by the workflow):
#   GITHUB_EVENT_NAME  -- "push" or "workflow_dispatch"
#   VERSION_INPUT      -- version string from workflow_dispatch input (e.g. "1.2.0")
#   BREAKING_INPUT     -- "true" or "false" from workflow_dispatch input
#   GITHUB_SHA         -- full commit SHA (set by GitHub Actions)
#
# Outputs (to $GITHUB_OUTPUT or stdout when GITHUB_OUTPUT is unset):
#   version   e.g. 2026.08.22-5-d3140e53  (edge) or  1.2.0  (stable)
#   npm_tag   edge | latest | breaking-edge
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

EVENT="${GITHUB_EVENT_NAME:-push}"
VERSION_INPUT="${VERSION_INPUT:-}"
BREAKING_INPUT="${BREAKING_INPUT:-false}"

if [[ "$EVENT" == "workflow_dispatch" && -n "$VERSION_INPUT" ]]; then
  VERSION="$VERSION_INPUT"
  if [[ "$BREAKING_INPUT" == "true" ]]; then
    NPM_TAG="breaking-edge"
  else
    NPM_TAG="latest"
  fi
else
  # Push to main (default): edge release with date-based version
  DATE=$(date -u '+%Y.%m.%d')
  BUILD=$(git -C "$REPO_ROOT" rev-list --count HEAD)
  SHA=$(git -C "$REPO_ROOT" rev-parse --short=8 HEAD)
  VERSION="${DATE}-${BUILD}-${SHA}"
  NPM_TAG="edge"
fi

OUT="${GITHUB_OUTPUT:-/dev/stdout}"
echo "version=${VERSION}" >> "$OUT"
echo "npm_tag=${NPM_TAG}"  >> "$OUT"

echo "Computed: version=${VERSION} npm_tag=${NPM_TAG}" >&2
