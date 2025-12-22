#!/usr/bin/env sh
# Patch the generated Scoop manifest to add pre_uninstall hook
# GoReleaser doesn't support pre_uninstall for Scoop, so we patch it post-generation
#
# Usage: ./patch-scoop-manifest.sh <path-to-manifest.json>

set -e

MANIFEST_PATH="$1"

if [ -z "$MANIFEST_PATH" ]; then
    echo "Usage: $0 <path-to-manifest.json>" >&2
    exit 1
fi

if [ ! -f "$MANIFEST_PATH" ]; then
    echo "Error: Manifest not found: $MANIFEST_PATH" >&2
    exit 1
fi

# Check if jq is available
if ! command -v jq > /dev/null 2>&1; then
    echo "Error: jq is required but not installed" >&2
    exit 1
fi

# Check if pre_uninstall already exists
if jq -e '.pre_uninstall' "$MANIFEST_PATH" > /dev/null 2>&1; then
    echo "pre_uninstall already present in manifest, skipping"
    exit 0
fi

# Add pre_uninstall hook that stops the daemon
# The PowerShell script:
# 1. Finds the PID file in user's config directory
# 2. If it exists, stops the process
# 3. Cleans up the PID file
jq '. + {
  "pre_uninstall": [
    "$pidFile = Join-Path $env:APPDATA \"warpdl\\daemon.pid\"",
    "if (Test-Path $pidFile) {",
    "  $pid = Get-Content $pidFile -ErrorAction SilentlyContinue",
    "  if ($pid) {",
    "    try { Stop-Process -Id $pid -Force -ErrorAction SilentlyContinue } catch {}",
    "  }",
    "  Remove-Item $pidFile -Force -ErrorAction SilentlyContinue",
    "}",
    "Write-Host \"WarpDL daemon stopped.\""
  ]
}' "$MANIFEST_PATH" > "${MANIFEST_PATH}.tmp" && mv "${MANIFEST_PATH}.tmp" "$MANIFEST_PATH"

echo "Successfully patched $MANIFEST_PATH with pre_uninstall hook"
