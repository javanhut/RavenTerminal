#!/bin/bash
#
# Raven Terminal Launcher Wrapper
# This wrapper ensures proper environment for launching from desktop
#

# Log file for debugging
LOG_DIR="$HOME/.local/share/raven-terminal"
LOG_FILE="$LOG_DIR/launch.log"
mkdir -p "$LOG_DIR"

# Log timestamp
echo "=== Launch attempt at $(date) ===" >> "$LOG_FILE"

# Ensure XDG_RUNTIME_DIR is set (needed for Wayland)
if [ -z "$XDG_RUNTIME_DIR" ]; then
    export XDG_RUNTIME_DIR="/run/user/$(id -u)"
fi

# Ensure display variables are set
# For X11
if [ -z "$DISPLAY" ] && [ -e "/tmp/.X11-unix/X0" ]; then
    export DISPLAY=":0"
fi

# Log environment for debugging
{
    echo "DISPLAY=$DISPLAY"
    echo "WAYLAND_DISPLAY=$WAYLAND_DISPLAY"
    echo "XDG_RUNTIME_DIR=$XDG_RUNTIME_DIR"
    echo "XDG_SESSION_TYPE=$XDG_SESSION_TYPE"
    echo "SHELL=$SHELL"
    echo "PATH=$PATH"
} >> "$LOG_FILE"

# Find the binary - prefer the one next to this wrapper, then fall back.
WRAPPER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY=""
for loc in \
    "$WRAPPER_DIR/raven-terminal" \
    "$HOME/.local/bin/raven-terminal" \
    "/usr/local/bin/raven-terminal" \
    "/usr/bin/raven-terminal"; do
    if [ -x "$loc" ]; then
        BINARY="$loc"
        break
    fi
done

if [ -z "$BINARY" ]; then
    echo "ERROR: raven-terminal binary not found in any location" >> "$LOG_FILE"
    notify-send "Raven Terminal" "Binary not found. Please reinstall." 2>/dev/null
    exit 1
fi

echo "Running: $BINARY" >> "$LOG_FILE"

# Run the application
exec "$BINARY" "$@" 2>> "$LOG_FILE"
