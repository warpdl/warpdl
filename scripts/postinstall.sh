#!/bin/sh
# WarpDL post-installation script
# POSIX sh compatible

echo "==================================================================="
echo "WarpDL installation complete!"
echo "==================================================================="
echo ""
echo "To run WarpDL daemon as a systemd user service:"
echo ""
echo "  1. Enable and start the service:"
echo "     systemctl --user enable --now warpdl.service"
echo ""
echo "  2. Check status:"
echo "     systemctl --user status warpdl.service"
echo ""
echo "  3. (Optional) For servers, enable lingering to keep daemon"
echo "     running after logout:"
echo "     sudo loginctl enable-linger \$USER"
echo ""
echo "==================================================================="
echo ""

# Install native messaging host for browser extensions (non-fatal)
warpdl native-host install --auto 2>/dev/null || true
