#!/usr/bin/env sh
# Patch the generated Scoop manifest to add service management hooks
# GoReleaser doesn't support post_install, pre_uninstall, and notes for Scoop,
# so we patch the manifest post-generation
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

# Check if service management hooks already exist
# We check for all three fields to ensure complete service management setup
if jq -e '.post_install and .pre_uninstall and .notes' "$MANIFEST_PATH" > /dev/null 2>&1; then
    echo "Service management hooks already present in manifest, skipping"
    exit 0
fi

# Add service management hooks and notes
# post_install: Show instructions after installation
# pre_uninstall: Stop and uninstall Windows service if installed, or stop daemon
# notes: Comprehensive service usage instructions
jq '. + {
  "post_install": [
    "Write-Host \"\"",
    "Write-Host \"WarpDL installed successfully!\" -ForegroundColor Green",
    "Write-Host \"\"",
    "Write-Host \"To run as Windows Service (requires Administrator):\" -ForegroundColor Yellow",
    "Write-Host \"  warpdl service install    # Register as Windows Service\"",
    "Write-Host \"  warpdl service start      # Start the service\"",
    "Write-Host \"\"",
    "Write-Host \"Or run in foreground:\" -ForegroundColor Yellow",
    "Write-Host \"  warpdl daemon             # Run daemon manually\"",
    "Write-Host \"\"",
    "Write-Host \"For more info: warpdl service --help\" -ForegroundColor Cyan",
    "Write-Host \"\""
  ],
  "pre_uninstall": [
    "# Try to stop and uninstall Windows service first (silently ignore errors if not installed)",
    "try {",
    "  & \"$dir\\warpdl.exe\" service stop 2>&1 | Out-Null",
    "  Start-Sleep -Milliseconds 1000",
    "  & \"$dir\\warpdl.exe\" service uninstall 2>&1 | Out-Null",
    "  Write-Host \"Windows service stopped and uninstalled.\"",
    "} catch {",
    "  # Service not installed, try stopping daemon via PID file",
    "  $pidFile = Join-Path $env:APPDATA \"warpdl\\daemon.pid\"",
    "  if (Test-Path $pidFile) {",
    "    $pid = Get-Content $pidFile -ErrorAction SilentlyContinue",
    "    if ($pid) {",
    "      try { Stop-Process -Id $pid -Force -ErrorAction SilentlyContinue } catch {}",
    "    }",
    "    Remove-Item $pidFile -Force -ErrorAction SilentlyContinue",
    "    Write-Host \"WarpDL daemon stopped.\"",
    "  }",
    "}"
  ],
  "notes": [
    "Windows Service (requires Administrator privileges):",
    "  warpdl service install      - Register WarpDL as Windows Service",
    "  warpdl service start        - Start the service",
    "  warpdl service stop         - Stop the service",
    "  warpdl service status       - Check service status",
    "  warpdl service uninstall    - Remove the service",
    "",
    "Foreground daemon:",
    "  warpdl daemon               - Run daemon manually (no admin required)",
    "",
    "The service will automatically start on system boot once installed."
  ]
}' "$MANIFEST_PATH" > "${MANIFEST_PATH}.tmp" && mv "${MANIFEST_PATH}.tmp" "$MANIFEST_PATH"

echo "Successfully patched $MANIFEST_PATH with service management hooks"
