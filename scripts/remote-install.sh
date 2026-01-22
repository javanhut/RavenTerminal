#!/bin/bash
#
# Raven Terminal Remote Installation Script
# Install Raven Terminal directly from GitHub with a single curl command
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-install.sh | bash
#   curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-install.sh | bash -s -- --global
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
INSTALL_MODE="user"
VERBOSE=false

# Application info
APP_NAME="raven-terminal"
APP_DISPLAY_NAME="Raven Terminal"
APP_COMMENT="GPU-accelerated terminal emulator"
REPO_URL="https://github.com/javanhut/RavenTerminal.git"
TEMP_DIR=""

# Installation paths
USER_BIN_DIR="$HOME/.local/bin"
USER_APP_DIR="$HOME/.local/share/applications"
USER_ICON_DIR="$HOME/.local/share/icons/hicolor/scalable/apps"

GLOBAL_BIN_DIR="/usr/local/bin"
GLOBAL_APP_DIR="/usr/share/applications"
GLOBAL_ICON_DIR="/usr/share/icons/hicolor/scalable/apps"

print_header() {
    echo -e "${BLUE}"
    echo "============================================"
    echo "   Raven Terminal Remote Installer"
    echo "============================================"
    echo -e "${NC}"
}

print_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

cleanup() {
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        print_info "Cleaning up temporary files..."
        rm -rf "$TEMP_DIR"
    fi
}

trap cleanup EXIT

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Install Raven Terminal from GitHub.

OPTIONS:
    -u, --user          Install for current user only (default)
                        Binary: ~/.local/bin/
                        Desktop: ~/.local/share/applications/

    -g, --global        Install system-wide (requires sudo)
                        Binary: /usr/local/bin/
                        Desktop: /usr/share/applications/

    -v, --verbose       Show verbose output

    -h, --help          Show this help message

EXAMPLES:
    # User installation (default)
    curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-install.sh | bash

    # System-wide installation
    curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-install.sh | bash -s -- --global

EOF
    exit 0
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -u|--user)
                INSTALL_MODE="user"
                shift
                ;;
            -g|--global)
                INSTALL_MODE="global"
                shift
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -h|--help)
                usage
                ;;
            *)
                print_error "Unknown option: $1"
                echo "Use --help for usage information."
                exit 1
                ;;
        esac
    done
}

detect_distro() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        echo "$ID"
    elif [ -f /etc/arch-release ]; then
        echo "arch"
    elif [ -f /etc/debian_version ]; then
        echo "debian"
    elif [ -f /etc/fedora-release ]; then
        echo "fedora"
    else
        echo "unknown"
    fi
}

print_dependency_hint() {
    local distro
    distro=$(detect_distro)

    echo ""
    echo "Install the required dependencies for your distribution:"
    echo ""

    case "$distro" in
        arch|manjaro|endeavouros)
            echo "  sudo pacman -S go base-devel libx11 libxcursor libxrandr libxinerama libxi mesa"
            ;;
        debian|ubuntu|pop|linuxmint)
            echo "  sudo apt install golang build-essential libgl1-mesa-dev xorg-dev"
            ;;
        fedora)
            echo "  sudo dnf install golang mesa-libGL-devel libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel"
            ;;
        opensuse*)
            echo "  sudo zypper install go Mesa-libGL-devel libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel"
            ;;
        *)
            echo "  Install Go and OpenGL/X11 development libraries for your distribution"
            ;;
    esac
    echo ""
}

check_dependencies() {
    print_info "Checking dependencies..."
    local missing=false

    # Check for Go
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed"
        missing=true
    else
        print_success "Go found: $(go version | awk '{print $3}')"
    fi

    # Check for git
    if ! command -v git &> /dev/null; then
        print_error "Git is not installed"
        missing=true
    else
        print_success "Git found"
    fi

    # Check for pkg-config
    if ! command -v pkg-config &> /dev/null; then
        print_warning "pkg-config not found (may be needed for build)"
    fi

    # Check for OpenGL/GLFW dependencies
    if command -v pkg-config &> /dev/null; then
        if ! pkg-config --exists gl 2>/dev/null; then
            print_warning "OpenGL development libraries may be missing"
        fi
    fi

    if [ "$missing" = true ]; then
        print_dependency_hint
        exit 1
    fi
}

clone_repo() {
    print_info "Cloning Raven Terminal repository..."
    TEMP_DIR=$(mktemp -d)

    if [ "$VERBOSE" = true ]; then
        git clone "$REPO_URL" "$TEMP_DIR/RavenTerminal"
    else
        git clone --quiet "$REPO_URL" "$TEMP_DIR/RavenTerminal"
    fi

    print_success "Repository cloned"
}

build_application() {
    print_info "Building Raven Terminal..."
    cd "$TEMP_DIR/RavenTerminal"

    if [ "$VERBOSE" = true ]; then
        go build -v -o "$APP_NAME" ./src
    else
        go build -o "$APP_NAME" ./src 2>&1
    fi

    if [ -f "$TEMP_DIR/RavenTerminal/$APP_NAME" ]; then
        print_success "Build successful"
        chmod +x "$TEMP_DIR/RavenTerminal/$APP_NAME"
    else
        print_error "Build failed"
        exit 1
    fi
}

create_desktop_file() {
    local bin_path="$1"
    local desktop_file="$2"
    local icon_name="$3"

    cat > "$desktop_file" << EOF
[Desktop Entry]
Version=1.0
Name=$APP_DISPLAY_NAME
Comment=$APP_COMMENT
Exec=$bin_path
Icon=$icon_name
Terminal=false
Type=Application
Categories=System;TerminalEmulator;Utility;
Keywords=terminal;console;shell;command;prompt;
StartupNotify=true
StartupWMClass=raven-terminal
EOF
}

create_wrapper_script() {
    local wrapper_path="$1"

    cat > "$wrapper_path" << 'EOF'
#!/bin/bash
# Raven Terminal Launcher Wrapper
# Ensures proper environment for launching from desktop entry

# Ensure we have a proper PATH
export PATH="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:$PATH"

# Find the actual binary
if [ -x "$HOME/.local/bin/raven-terminal" ]; then
    exec "$HOME/.local/bin/raven-terminal" "$@"
elif [ -x "/usr/local/bin/raven-terminal" ]; then
    exec "/usr/local/bin/raven-terminal" "$@"
elif [ -x "/usr/bin/raven-terminal" ]; then
    exec "/usr/bin/raven-terminal" "$@"
else
    echo "Error: raven-terminal not found in PATH"
    exit 1
fi
EOF
    chmod +x "$wrapper_path"
}

install_user() {
    print_info "Installing for current user..."

    # Create directories
    mkdir -p "$USER_BIN_DIR"
    mkdir -p "$USER_APP_DIR"
    mkdir -p "$USER_ICON_DIR"
    mkdir -p "$HOME/.local/share/raven-terminal"
    mkdir -p "$HOME/.local/share/pixmaps"

    # Install binary
    cp "$TEMP_DIR/RavenTerminal/$APP_NAME" "$USER_BIN_DIR/"
    chmod +x "$USER_BIN_DIR/$APP_NAME"
    print_success "Binary installed to $USER_BIN_DIR/$APP_NAME"

    # Install launcher wrapper
    create_wrapper_script "$USER_BIN_DIR/raven-terminal-launcher"
    print_success "Launcher wrapper installed"

    # Install icon
    local icon_name="utilities-terminal"
    if [ -f "$TEMP_DIR/RavenTerminal/src/assets/raven_terminal_icon.svg" ]; then
        cp "$TEMP_DIR/RavenTerminal/src/assets/raven_terminal_icon.svg" "$USER_ICON_DIR/$APP_NAME.svg"
        cp "$TEMP_DIR/RavenTerminal/src/assets/raven_terminal_icon.svg" "$HOME/.local/share/pixmaps/$APP_NAME.svg"
        icon_name="$APP_NAME"
        print_success "Icon installed"
    fi

    # Create desktop file
    create_desktop_file "$USER_BIN_DIR/raven-terminal-launcher" "$USER_APP_DIR/$APP_NAME.desktop" "$icon_name"
    print_success "Desktop entry created"

    # Update icon cache
    if command -v gtk-update-icon-cache &> /dev/null; then
        gtk-update-icon-cache -f -t "$HOME/.local/share/icons/hicolor" 2>/dev/null || true
    fi

    # Update desktop database
    if command -v update-desktop-database &> /dev/null; then
        update-desktop-database "$USER_APP_DIR" 2>/dev/null || true
    fi

    # Check if ~/.local/bin is in PATH
    if [[ ":$PATH:" != *":$USER_BIN_DIR:"* ]]; then
        print_warning "$USER_BIN_DIR is not in your PATH"
        echo ""
        echo "Add this to your ~/.bashrc or ~/.zshrc:"
        echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
    fi
}

install_global() {
    print_info "Installing system-wide (requires sudo)..."

    # Check for sudo
    if ! command -v sudo &> /dev/null; then
        print_error "sudo is required for global installation"
        exit 1
    fi

    # Install binary
    sudo cp "$TEMP_DIR/RavenTerminal/$APP_NAME" "$GLOBAL_BIN_DIR/"
    sudo chmod +x "$GLOBAL_BIN_DIR/$APP_NAME"
    print_success "Binary installed to $GLOBAL_BIN_DIR/$APP_NAME"

    # Install launcher wrapper
    local tmp_wrapper=$(mktemp)
    create_wrapper_script "$tmp_wrapper"
    sudo mv "$tmp_wrapper" "$GLOBAL_BIN_DIR/raven-terminal-launcher"
    sudo chmod +x "$GLOBAL_BIN_DIR/raven-terminal-launcher"
    print_success "Launcher wrapper installed"

    # Install icon
    local icon_name="utilities-terminal"
    if [ -f "$TEMP_DIR/RavenTerminal/src/assets/raven_terminal_icon.svg" ]; then
        sudo mkdir -p "$GLOBAL_ICON_DIR"
        sudo mkdir -p /usr/share/pixmaps
        sudo cp "$TEMP_DIR/RavenTerminal/src/assets/raven_terminal_icon.svg" "$GLOBAL_ICON_DIR/$APP_NAME.svg"
        sudo cp "$TEMP_DIR/RavenTerminal/src/assets/raven_terminal_icon.svg" "/usr/share/pixmaps/$APP_NAME.svg"
        icon_name="$APP_NAME"
        print_success "Icon installed"
    fi

    # Create desktop file
    local tmp_desktop=$(mktemp)
    create_desktop_file "$GLOBAL_BIN_DIR/raven-terminal-launcher" "$tmp_desktop" "$icon_name"
    sudo mv "$tmp_desktop" "$GLOBAL_APP_DIR/$APP_NAME.desktop"
    print_success "Desktop entry created"

    # Update icon cache
    if command -v gtk-update-icon-cache &> /dev/null; then
        sudo gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
    fi

    # Update desktop database
    if command -v update-desktop-database &> /dev/null; then
        sudo update-desktop-database "$GLOBAL_APP_DIR" 2>/dev/null || true
    fi
}

print_completion() {
    echo ""
    echo -e "${GREEN}============================================${NC}"
    echo -e "${GREEN}     Installation Complete!${NC}"
    echo -e "${GREEN}============================================${NC}"
    echo ""
    echo "You can now launch Raven Terminal:"
    echo ""
    echo "  - From terminal: $APP_NAME"
    echo "  - From application menu: Search for 'Raven Terminal'"
    echo ""
    echo "To uninstall, run:"
    echo "  curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-uninstall.sh | bash"
    echo ""
}

main() {
    print_header
    parse_args "$@"

    echo "Installation mode: $INSTALL_MODE"
    echo ""

    check_dependencies
    clone_repo
    build_application

    case $INSTALL_MODE in
        user)
            install_user
            ;;
        global)
            install_global
            ;;
    esac

    print_completion
}

main "$@"
