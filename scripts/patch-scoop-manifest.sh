#!/usr/bin/env sh
# Patch the generated Scoop manifest to add post_install and pre_uninstall hooks
# GoReleaser doesn't support these hooks for Scoop, so we patch it post-generation
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

# Track if any patches were applied
PATCHED=0

# Add post_install hook if not present
if ! jq -e '.post_install' "$MANIFEST_PATH" > /dev/null 2>&1; then
    # Add post_install hook that installs native messaging host
    # Uses --auto flag to use official extension IDs (silent no-op if not configured)
    jq '. + {
      "post_install": [
        "$warpdl = Join-Path $dir \"warpdl.exe\"",
        "& $warpdl native-host install --auto 2>$null"
      ]
    }' "$MANIFEST_PATH" > "${MANIFEST_PATH}.tmp" && mv "${MANIFEST_PATH}.tmp" "$MANIFEST_PATH"
    echo "Added post_install hook"
    PATCHED=1
else
    echo "post_install already present in manifest, skipping"
fi

# Add pre_uninstall hook if not present
if ! jq -e '.pre_uninstall' "$MANIFEST_PATH" > /dev/null 2>&1; then
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
    echo "Added pre_uninstall hook"
    PATCHED=1
else
    echo "pre_uninstall already present in manifest, skipping"
fi

if [ "$PATCHED" -eq 1 ]; then
    echo "Successfully patched $MANIFEST_PATH"
else
    echo "No patches needed for $MANIFEST_PATH"
fi
