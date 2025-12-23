#!/bin/sh
# WarpDL post-installation script
# POSIX sh compatible

set -e

echo "==================================================================="
echo "WarpDL installation complete!"
echo "==================================================================="
echo ""
echo "To run WarpDL daemon as a systemd user service:"
echo ""
echo "  1. Copy the service file to systemd user directory:"
echo "     mkdir -p ~/.config/systemd/user"
echo "     cp /usr/local/share/warpdl/warpdl.service ~/.config/systemd/user/"
echo ""
echo "  2. Reload systemd user daemon:"
echo "     systemctl --user daemon-reload"
echo ""
echo "  3. Enable and start the service:"
echo "     systemctl --user enable --now warpdl.service"
echo ""
echo "  4. (Optional) For server use, enable lingering to keep daemon"
echo "     running after logout:"
echo "     sudo loginctl enable-linger \$USER"
echo ""
echo "==================================================================="
echo "Checking for service file installation..."
echo "==================================================================="

# Create systemd user share directory and install service file
SYSTEMD_SHARE_DIR="/usr/local/share/warpdl"
if [ ! -d "$SYSTEMD_SHARE_DIR" ]; then
    mkdir -p "$SYSTEMD_SHARE_DIR"
    echo "Created directory: $SYSTEMD_SHARE_DIR"
fi

# Find the service file (should be in same directory as this script)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_FILE="$SCRIPT_DIR/warpdl.service"

if [ -f "$SERVICE_FILE" ]; then
    cp "$SERVICE_FILE" "$SYSTEMD_SHARE_DIR/warpdl.service"
    chmod 644 "$SYSTEMD_SHARE_DIR/warpdl.service"
    echo "Installed service file to: $SYSTEMD_SHARE_DIR/warpdl.service"
else
    echo "Warning: Service file not found at $SERVICE_FILE"
fi

# Reload systemd daemon if running as root (for package managers)
if [ "$(id -u)" -eq 0 ]; then
    if command -v systemctl >/dev/null 2>&1; then
        systemctl daemon-reload || true
        echo "Reloaded systemd daemon"
    fi
fi

echo ""
echo "Installation complete. Follow the instructions above to enable the daemon."
echo ""
