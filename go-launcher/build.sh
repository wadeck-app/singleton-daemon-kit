#!/usr/bin/env bash
# build.sh — Cross-compile the go-launcher consumer binary for 3 targets.
# Usage: build.sh <config.json> <outputDir> [<main.go.tmpl>]
#
# Arguments:
#   config.json   — consumer config (appName, nodeScript, defaultConfigDir, cliFlags, ...)
#   outputDir     — directory to write compiled binaries
#   main.go.tmpl  — optional: consumer-provided template (default: SDK's minimal template)
#                   Use this when your consumer needs custom arg parsing, flag conventions,
#                   or any logic that goes beyond the SDK's default ConfigDir resolution.
#
# config.json fields:
#   appName          (string)   — binary name prefix
#   nodeScript       (string)   — path to the .cjs bundle relative to the exe
#   defaultConfigDir (string)   — default config directory name (e.g. ".myapp")
#   cliFlags         (string[]) — flags that trigger HTTP dispatch instead of daemon spawn
#   silentFlags      (string[]) — flags that suppress launcher lifecycle noise on stderr

set -euo pipefail

CONFIG_FILE="${1:-}"
OUTPUT_DIR="${2:-}"
CUSTOM_TEMPLATE="${3:-}"

# --- Validate arguments ---
if [[ -z "$CONFIG_FILE" ]]; then
  echo "Error: missing config file argument" >&2
  echo "Usage: $0 <config.json> <outputDir>" >&2
  exit 1
fi

if [[ -z "$OUTPUT_DIR" ]]; then
  echo "Error: missing output directory argument" >&2
  echo "Usage: $0 <config.json> <outputDir>" >&2
  exit 1
fi

if [[ ! -f "$CONFIG_FILE" ]]; then
  echo "Error: config file not found: $CONFIG_FILE" >&2
  exit 1
fi

# --- Check go is in PATH ---
if ! command -v go &>/dev/null; then
  echo "Error: 'go' not found in PATH — install Go and ensure it is in PATH" >&2
  exit 1
fi

# --- Check node is in PATH (used for JSON parsing) ---
if ! command -v node &>/dev/null; then
  echo "Error: 'node' not found in PATH — install Node.js and ensure it is in PATH" >&2
  exit 1
fi

# --- Parse config.json using node ---
APP_NAME=$(node -e "const c=require('$CONFIG_FILE'); process.stdout.write(c.appName)")
DISPLAY_NAME=$(node -e "const c=require('$CONFIG_FILE'); process.stdout.write(c.displayName || c.appName)")
NODE_SCRIPT=$(node -e "const c=require('$CONFIG_FILE'); process.stdout.write(c.nodeScript)")
DEFAULT_CONFIG_DIR=$(node -e "const c=require('$CONFIG_FILE'); process.stdout.write(c.defaultConfigDir)")
# CLIFlags as a JSON array string, used below for template generation
CLI_FLAGS_JSON=$(node -e "const c=require('$CONFIG_FILE'); process.stdout.write(JSON.stringify(c.cliFlags))")
SILENT_FLAGS_JSON=$(node -e "const c=require('$CONFIG_FILE'); process.stdout.write(JSON.stringify(c.silentFlags || []))")

if [[ -z "$APP_NAME" ]]; then
  echo "Error: 'appName' is missing or empty in $CONFIG_FILE" >&2
  exit 1
fi

if [[ -z "$NODE_SCRIPT" ]]; then
  echo "Error: 'nodeScript' is missing or empty in $CONFIG_FILE" >&2
  exit 1
fi

if [[ -z "$DEFAULT_CONFIG_DIR" ]]; then
  echo "Error: 'defaultConfigDir' is missing or empty in $CONFIG_FILE" >&2
  exit 1
fi

# --- Locate the template file (same directory as this script) ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Use consumer-provided template if given, otherwise SDK's minimal default
if [[ -n "$CUSTOM_TEMPLATE" ]]; then
  TEMPLATE_FILE="$CUSTOM_TEMPLATE"
  echo "Using consumer template: $TEMPLATE_FILE"
else
  TEMPLATE_FILE="$SCRIPT_DIR/main.go.tmpl"
fi

if [[ ! -f "$TEMPLATE_FILE" ]]; then
  echo "Error: template file not found: $TEMPLATE_FILE" >&2
  exit 1
fi

# --- Create output directory ---
mkdir -p "$OUTPUT_DIR"

# --- Generate a temporary directory for the build ---
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# --- Generate main.go from template using node ---
node - "$TEMPLATE_FILE" "$APP_NAME" "$NODE_SCRIPT" "$DEFAULT_CONFIG_DIR" "$CLI_FLAGS_JSON" "$SILENT_FLAGS_JSON" "$TMPDIR" <<'EOF'
const fs = require('fs');
const path = require('path');

const [templateFile, appName, nodeScript, defaultConfigDir, cliFlagsJson, silentFlagsJson, outDir] = process.argv.slice(2);

const cliFlags = JSON.parse(cliFlagsJson);
const silentFlags = JSON.parse(silentFlagsJson);
const flagLines = cliFlags.map(f => `\t\t\t"${f}",`).join('\n');
const silentLines = silentFlags.map(f => `\t\t\t"${f}",`).join('\n');

let tmpl = fs.readFileSync(templateFile, 'utf8');
tmpl = tmpl.replace(/\{\{\.AppName\}\}/g, appName);
tmpl = tmpl.replace(/\{\{\.DefaultConfigDir\}\}/g, defaultConfigDir);
tmpl = tmpl.replace(/\{\{\.NodeScript\}\}/g, nodeScript);
// Replace the CLIFlags range block
tmpl = tmpl.replace(/\{\{range \.CLIFlags\}\}.*?\{\{end\}\}/s, flagLines + '\n');
// Replace the SilentFlags range block
tmpl = tmpl.replace(/\{\{range \.SilentFlags\}\}.*?\{\{end\}\}/s, silentLines + '\n');

fs.writeFileSync(path.join(outDir, 'main.go'), tmpl, 'utf8');
EOF

# Copy go.mod and go.sum into the temp dir so `go build` resolves the module
cp "$SCRIPT_DIR/go.mod" "$TMPDIR/go.mod"
cp "$SCRIPT_DIR/go.sum" "$TMPDIR/go.sum"

# Symlink (or copy) the go-launcher package sources into the temp module so the
# replace directive isn't needed — instead, we copy the generated main.go into a
# sub-package structure that imports the library from the module cache.
# Simpler approach: use a replace directive in a fresh go.mod inside TMPDIR.

# Rewrite go.mod to add a replace directive pointing at the library source
GO_VERSION=$(grep '^go ' "$SCRIPT_DIR/go.mod" | awk '{print $2}')
TOOLCHAIN_LINE=$(grep '^toolchain ' "$SCRIPT_DIR/go.mod" || true)
REQUIRE_LINES=$(grep '^require' "$SCRIPT_DIR/go.mod" -A 100 | tail -n +1)

node - "$TMPDIR/go.mod" "$SCRIPT_DIR" "$GO_VERSION" "$TOOLCHAIN_LINE" <<'EOF'
const fs = require('fs');
const [gomodPath, launcherDir, goVersion, toolchainLine] = process.argv.slice(2);

const content = [
  `module consumer`,
  ``,
  `go ${goVersion}`,
  toolchainLine ? toolchainLine : '',
  ``,
  `require wadeck.ch/singleton-daemon-kit/go-launcher v0.0.0`,
  ``,
  `require golang.org/x/sys v0.47.0 // indirect`,
  ``,
  `replace wadeck.ch/singleton-daemon-kit/go-launcher => ${launcherDir.replace(/\\/g, '/')}`,
].filter(line => line !== undefined).join('\n');

fs.writeFileSync(gomodPath, content, 'utf8');
EOF

# Copy go.sum (the library's sum file covers all deps)
cp "$SCRIPT_DIR/go.sum" "$TMPDIR/go.sum"

# Embed Windows version info (FileDescription, ProductName, etc.) via goversioninfo.
# resource.syso is picked up automatically by `go build` for GOOS=windows.
# Non-fatal: if goversioninfo is absent the build still succeeds without version info.
VERSIONINFO_SRC="$SCRIPT_DIR/versioninfo.json"
if [[ -f "$VERSIONINFO_SRC" ]]; then
  sed -e "s/{{APP_NAME}}/${APP_NAME}/g" \
      -e "s/{{DISPLAY_NAME}}/${DISPLAY_NAME}/g" \
      "$VERSIONINFO_SRC" > "$TMPDIR/versioninfo.json"
  if command -v goversioninfo &>/dev/null; then
    echo "Generating Windows resource file (resource.syso)..."
    if (cd "$TMPDIR" && goversioninfo -o resource.syso versioninfo.json); then
      echo "  resource.syso generated"
    else
      echo "  goversioninfo failed (non-fatal, skipping)"
    fi
  else
    echo "  goversioninfo not in PATH — skipping Windows resource embedding"
  fi
fi

echo "Generated main.go at $TMPDIR/main.go"
echo "Building 3 targets for $APP_NAME..."

# --- Build targets ---
build_target() {
  local goos="$1"
  local goarch="$2"
  local output="$3"

  echo "  Building $goos/$goarch -> $output"
  if ! (cd "$TMPDIR" && GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w" \
    -o "$output" .); then
    echo "Error: build failed for $goos/$goarch" >&2
    exit 1
  fi
  echo "  OK: $output"
}

build_target "windows" "amd64"  "$OUTPUT_DIR/${APP_NAME}_windows_release.exe"
build_target "darwin"  "arm64"  "$OUTPUT_DIR/${APP_NAME}_darwin_arm64_release"
build_target "darwin"  "amd64"  "$OUTPUT_DIR/${APP_NAME}_darwin_amd64_release"

echo "Build complete. Outputs in $OUTPUT_DIR:"
ls -lh "$OUTPUT_DIR/${APP_NAME}_windows_release.exe" \
        "$OUTPUT_DIR/${APP_NAME}_darwin_arm64_release" \
        "$OUTPUT_DIR/${APP_NAME}_darwin_amd64_release" 2>/dev/null || true
