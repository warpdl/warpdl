#!/bin/sh
# WarpDL pre-removal script
# POSIX sh compatible
# Gracefully stops daemon and cleans up - never fails

echo "==================================================================="
echo "Stopping WarpDL daemon before removal..."
echo "==================================================================="

# Method 1: Try CLI stop-daemon command
if command -v warpdl >/dev/null 2>&1; then
    echo "Attempting to stop daemon via CLI..."
    warpdl stop-daemon 2>/dev/null || true
    sleep 1
fi

# Method 2: Try PID file from standard config location
DAEMON_PID_FILE="$HOME/.config/warpdl/daemon.pid"
if [ -f "$DAEMON_PID_FILE" ]; then
    PID=$(cat "$DAEMON_PID_FILE" 2>/dev/null || echo "")
    if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
        echo "Found daemon PID $PID, sending SIGTERM..."
        kill -TERM "$PID" 2>/dev/null || true
        sleep 2
        # Force kill if still running
        if kill -0 "$PID" 2>/dev/null; then
            echo "Daemon still running, sending SIGKILL..."
            kill -KILL "$PID" 2>/dev/null || true
        fi
    fi
    rm -f "$DAEMON_PID_FILE" 2>/dev/null || true
fi

# Method 3: Try systemd user service (check all users if running as root)
if command -v systemctl >/dev/null 2>&1; then
    if [ "$(id -u)" -eq 0 ]; then
        # Running as root during package removal - try to stop for all users
        echo "Attempting to stop systemd user services for all users..."
        for user_home in /home/* /root; do
            if [ -d "$user_home" ]; then
                username=$(basename "$user_home")
                # Try to stop user service
                su - "$username" -c "systemctl --user stop warpdl.service 2>/dev/null" 2>/dev/null || true
                su - "$username" -c "systemctl --user disable warpdl.service 2>/dev/null" 2>/dev/null || true
            fi
        done
    else
        # Running as regular user
        echo "Attempting to stop systemd user service..."
        systemctl --user stop warpdl.service 2>/dev/null || true
        systemctl --user disable warpdl.service 2>/dev/null || true
    fi
fi

# Clean up socket file
SOCKET_FILE="/tmp/warpdl.sock"
if [ -S "$SOCKET_FILE" ] || [ -e "$SOCKET_FILE" ]; then
    echo "Removing socket file: $SOCKET_FILE"
    rm -f "$SOCKET_FILE" 2>/dev/null || true
fi

# Clean up service file from share directory
SYSTEMD_SHARE_DIR="/usr/local/share/warpdl"
if [ -f "$SYSTEMD_SHARE_DIR/warpdl.service" ]; then
    echo "Removing service file: $SYSTEMD_SHARE_DIR/warpdl.service"
    rm -f "$SYSTEMD_SHARE_DIR/warpdl.service" 2>/dev/null || true
    rmdir "$SYSTEMD_SHARE_DIR" 2>/dev/null || true
fi

echo "Daemon cleanup complete."
echo "==================================================================="

# Always exit successfully - package removal should not fail
exit 0
