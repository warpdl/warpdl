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

# Method 3: Try systemd user service
if command -v systemctl >/dev/null 2>&1; then
    if [ "$(id -u)" -eq 0 ]; then
        # Running as root during package removal
        echo "Stopping systemd user services for all users..."

        for user_home in /home/* /root; do
            [ -d "$user_home" ] || continue
            username=$(basename "$user_home")

            # Get the user's UID
            uid=$(id -u "$username" 2>/dev/null) || continue

            # Check if user's runtime dir exists (user has lingering or is logged in)
            user_runtime_dir="/run/user/$uid"
            if [ -d "$user_runtime_dir" ]; then
                echo "Stopping service for user: $username (uid=$uid)"

                # Set XDG_RUNTIME_DIR and try to stop the service
                XDG_RUNTIME_DIR="$user_runtime_dir" \
                    systemctl --user -M "$username@" stop warpdl.service 2>/dev/null || true
                XDG_RUNTIME_DIR="$user_runtime_dir" \
                    systemctl --user -M "$username@" disable warpdl.service 2>/dev/null || true
            fi

            # Also check user's PID file as fallback
            user_pid_file="$user_home/.config/warpdl/daemon.pid"
            if [ -f "$user_pid_file" ]; then
                pid=$(cat "$user_pid_file" 2>/dev/null || echo "")
                if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
                    echo "Stopping daemon for $username via PID file (pid=$pid)"
                    kill -TERM "$pid" 2>/dev/null || true
                    sleep 1
                    kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null || true
                fi
                rm -f "$user_pid_file" 2>/dev/null || true
            fi
        done
    else
        # Running as regular user
        echo "Attempting to stop systemd user service..."
        systemctl --user stop warpdl.service 2>/dev/null || true
        systemctl --user disable warpdl.service 2>/dev/null || true
    fi
fi

# Method 4: Kill any remaining warpdl daemon processes as final fallback
if command -v pkill >/dev/null 2>&1; then
    echo "Checking for remaining warpdl daemon processes..."
    pkill -TERM -f "warpdl daemon" 2>/dev/null || true
    sleep 1
    pkill -KILL -f "warpdl daemon" 2>/dev/null || true
fi

# Clean up socket file
SOCKET_FILE="/tmp/warpdl.sock"
if [ -S "$SOCKET_FILE" ] || [ -e "$SOCKET_FILE" ]; then
    echo "Removing socket file: $SOCKET_FILE"
    rm -f "$SOCKET_FILE" 2>/dev/null || true
fi

echo "Daemon cleanup complete."
echo "==================================================================="

# Always exit successfully - package removal should not fail
exit 0
