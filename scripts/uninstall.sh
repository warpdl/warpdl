#!/usr/bin/env sh
# WarpDL Uninstall Script
# Removes the warpdl binary, stops the daemon, and optionally cleans up config data
#
# Usage: ./uninstall.sh [options]
#   --force       Skip confirmation prompts
#   --keep-data   Preserve configuration directory
#   --debug       Enable verbose output

set -e

# Error codes
# 1 = general error
# 2 = insufficient permissions

# Global variables
DEBUG=0
FORCE=0
KEEP_DATA=0
CLEAN_EXIT=0
OS="unknown"

cleanup() {
    exit_code=$?
    if [ "$exit_code" -ne 0 ] && [ "$CLEAN_EXIT" -ne 1 ]; then
        log "ERROR: Script failed during execution"

        if [ "$DEBUG" -eq 0 ]; then
            log "For more verbose output, re-run this script with --debug"
        fi
    fi

    clean_exit "$exit_code"
}
trap cleanup EXIT
trap cleanup INT

clean_exit() {
    CLEAN_EXIT=1
    exit "$1"
}

log() {
    # Print to stderr
    >&2 echo "$1"
}

log_debug() {
    if [ "$DEBUG" -eq 1 ]; then
        >&2 echo "DEBUG: $1"
    fi
}

log_warning() {
    >&2 echo "WARNING: $1"
}

# Get the configuration directory path
get_config_dir() {
    if [ -n "$WARPDL_CONFIG_DIR" ]; then
        echo "$WARPDL_CONFIG_DIR"
        return
    fi

    case "$OS" in
        macos)
            # Go's os.UserConfigDir() returns ~/Library/Application Support on macOS
            # but WarpDL uses ~/.config/warpdl for consistency
            echo "$HOME/.config/warpdl"
            ;;
        *)
            echo "${XDG_CONFIG_HOME:-$HOME/.config}/warpdl"
            ;;
    esac
}

# Get the socket path
get_socket_path() {
    if [ -n "$WARPDL_SOCKET_PATH" ]; then
        echo "$WARPDL_SOCKET_PATH"
        return
    fi
    echo "/tmp/warpdl.sock"
}

# Confirm an action with the user
confirm_action() {
    message="$1"

    if [ "$FORCE" -eq 1 ]; then
        return 0
    fi

    printf "%s [y/N]: " "$message"
    read -r response
    case "$response" in
        [yY]|[yY][eE][sS]) return 0 ;;
        *) return 1 ;;
    esac
}

# Stop the daemon gracefully
stop_daemon() {
    config_dir="$(get_config_dir)"
    pid_file="$config_dir/daemon.pid"

    if [ ! -f "$pid_file" ]; then
        log_debug "No PID file found, daemon not running"
        return 0
    fi

    pid=$(cat "$pid_file" 2>/dev/null)
    if [ -z "$pid" ]; then
        log_debug "PID file is empty, removing stale file"
        rm -f "$pid_file"
        return 0
    fi

    # Check if process is still running
    if ! kill -0 "$pid" 2>/dev/null; then
        log_debug "Stale PID file (process $pid not running), removing"
        rm -f "$pid_file"
        return 0
    fi

    log "Stopping daemon (PID $pid)..."

    # Send SIGTERM for graceful shutdown
    kill -TERM "$pid" 2>/dev/null || true

    # Wait up to 5 seconds for graceful shutdown
    timeout=50  # 50 * 100ms = 5s
    while [ "$timeout" -gt 0 ] && kill -0 "$pid" 2>/dev/null; do
        sleep 0.1
        timeout=$((timeout - 1))
    done

    # Force kill if still running
    if kill -0 "$pid" 2>/dev/null; then
        log_warning "Graceful shutdown timeout, forcing kill..."
        kill -KILL "$pid" 2>/dev/null || true
        sleep 0.5
    fi

    # Clean up PID file if daemon didn't remove it
    rm -f "$pid_file" 2>/dev/null || true

    log "Daemon stopped"
}

# Remove the socket file
remove_socket() {
    socket_path="$(get_socket_path)"

    if [ -e "$socket_path" ]; then
        log_debug "Removing socket file: $socket_path"
        rm -f "$socket_path" 2>/dev/null || true
    else
        log_debug "Socket file not found: $socket_path"
    fi
}

# Remove the configuration directory
remove_config() {
    if [ "$KEEP_DATA" -eq 1 ]; then
        log "Keeping configuration directory (--keep-data)"
        return 0
    fi

    config_dir="$(get_config_dir)"

    if [ ! -d "$config_dir" ]; then
        log_debug "Configuration directory not found: $config_dir"
        return 0
    fi

    if ! confirm_action "Remove configuration directory at $config_dir?"; then
        log "Skipping configuration removal"
        return 0
    fi

    log_debug "Removing configuration directory: $config_dir"
    rm -rf "$config_dir"
    log "Removed configuration directory"
}

# Remove the binary
remove_binary() {
    binary_path=$(command -v warpdl 2>/dev/null || true)

    if [ -z "$binary_path" ]; then
        log "No warpdl binary found in PATH"
        return 0
    fi

    if ! confirm_action "Remove binary at $binary_path?"; then
        log "Skipping binary removal"
        return 0
    fi

    # Check if we can write to the directory
    binary_dir=$(dirname "$binary_path")
    if [ ! -w "$binary_dir" ]; then
        log_warning "Cannot remove $binary_path (permission denied)"
        log "Run with sudo to remove the binary"
        return 1
    fi

    log_debug "Removing binary: $binary_path"
    rm -f "$binary_path"
    log "Removed $binary_path"
}

# Parse command line flags
for arg; do
    if [ "$arg" = "--debug" ]; then
        DEBUG=1
    fi

    if [ "$arg" = "--force" ]; then
        FORCE=1
    fi

    if [ "$arg" = "--keep-data" ]; then
        KEEP_DATA=1
    fi

    if [ "$arg" = "--help" ] || [ "$arg" = "-h" ]; then
        echo "WarpDL Uninstall Script"
        echo ""
        echo "Usage: $0 [options]"
        echo ""
        echo "Options:"
        echo "  --force       Skip confirmation prompts"
        echo "  --keep-data   Preserve configuration directory"
        echo "  --debug       Enable verbose output"
        echo "  --help, -h    Show this help message"
        clean_exit 0
    fi
done

# Identify OS
uname_os=$(uname -s)
case "$uname_os" in
    Darwin)    OS="macos"   ;;
    Linux)     OS="linux"   ;;
    FreeBSD)   OS="freebsd" ;;
    OpenBSD)   OS="openbsd" ;;
    NetBSD)    OS="netbsd"  ;;
    *)
        log_warning "Unknown OS: $uname_os, using default paths"
        OS="linux"
        ;;
esac

log_debug "Detected OS: $OS"

# Main execution
echo "WarpDL Uninstaller"
echo ""

# Step 1: Stop the daemon
stop_daemon

# Step 2: Remove socket file
remove_socket

# Step 3: Remove configuration directory (unless --keep-data)
remove_config

# Step 4: Remove binary
if ! remove_binary; then
    clean_exit 2
fi

echo ""
echo "WarpDL has been uninstalled."

if [ "$KEEP_DATA" -eq 1 ]; then
    echo "Configuration data preserved at $(get_config_dir)"
fi

clean_exit 0
