#!/bin/bash
#
# Raven Terminal Installation Script
# Installs the Raven Terminal application with desktop integration
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
BUILD_ONLY=false
SKIP_BUILD=false
VERBOSE=false

# Application info
APP_NAME="raven-terminal"
APP_DISPLAY_NAME="Raven Terminal"
APP_COMMENT="GPU-accelerated terminal emulator"

# Get script directory (where the repo is)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"

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
    echo "       Raven Terminal Installer"
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

usage() {
    cat << EOF
Usage: $(basename "$0") [OPTIONS]

Install Raven Terminal on your system.

OPTIONS:
    -u, --user          Install for current user only (default)
                        Binary: ~/.local/bin/
                        Desktop: ~/.local/share/applications/
                        Icon: ~/.local/share/icons/

    -g, --global        Install system-wide (requires sudo)
                        Binary: /usr/local/bin/
                        Desktop: /usr/share/applications/
                        Icon: /usr/share/icons/

    -b, --build-only    Only build the binary, don't install
    
    -s, --skip-build    Skip building, use existing binary
    
    -v, --verbose       Show verbose output
    
    -h, --help          Show this help message

EXAMPLES:
    $(basename "$0")              # User installation (default)
    $(basename "$0") --global     # System-wide installation
    $(basename "$0") --build-only # Just build the binary

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
            -b|--build-only)
                BUILD_ONLY=true
                shift
                ;;
            -s|--skip-build)
                SKIP_BUILD=true
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

check_dependencies() {
    print_info "Checking dependencies..."
    
    # Check for Go
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed. Please install Go first."
        echo "  Arch: sudo pacman -S go"
        echo "  Ubuntu/Debian: sudo apt install golang"
        echo "  Fedora: sudo dnf install golang"
        exit 1
    fi
    print_success "Go found: $(go version | awk '{print $3}')"
    
    # Check for required system libraries (for OpenGL)
    local missing_deps=()
    
    # Check pkg-config
    if ! command -v pkg-config &> /dev/null; then
        missing_deps+=("pkg-config")
    fi
    
    # Check for OpenGL/GLFW dependencies
    if ! pkg-config --exists gl 2>/dev/null; then
        missing_deps+=("OpenGL development libraries")
    fi
    
    if ! pkg-config --exists x11 2>/dev/null && ! pkg-config --exists wayland-client 2>/dev/null; then
        missing_deps+=("X11 or Wayland development libraries")
    fi
    
    if [ ${#missing_deps[@]} -gt 0 ]; then
        print_warning "Some dependencies may be missing: ${missing_deps[*]}"
        echo "  Arch: sudo pacman -S base-devel libx11 libxcursor libxrandr libxinerama libxi mesa"
        echo "  Ubuntu/Debian: sudo apt install build-essential libgl1-mesa-dev xorg-dev"
        echo "  Fedora: sudo dnf install mesa-libGL-devel libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel"
    fi
}

build_application() {
    if [ "$SKIP_BUILD" = true ]; then
        if [ -f "$REPO_DIR/$APP_NAME" ]; then
            print_info "Using existing binary"
            return 0
        else
            print_error "No existing binary found. Cannot skip build."
            exit 1
        fi
    fi
    
    print_info "Building Raven Terminal..."
    cd "$REPO_DIR"
    
    if [ "$VERBOSE" = true ]; then
        go build -v -o "$APP_NAME" .
    else
        go build -o "$APP_NAME" . 2>&1
    fi
    
    if [ -f "$REPO_DIR/$APP_NAME" ]; then
        print_success "Build successful"
        chmod +x "$REPO_DIR/$APP_NAME"
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

install_user() {
    print_info "Installing for current user..."
    
    # Create directories
    mkdir -p "$USER_BIN_DIR"
    mkdir -p "$USER_APP_DIR"
    mkdir -p "$USER_ICON_DIR"
    mkdir -p "$HOME/.local/share/raven-terminal"
    
    # Install binary
    cp "$REPO_DIR/$APP_NAME" "$USER_BIN_DIR/"
    chmod +x "$USER_BIN_DIR/$APP_NAME"
    print_success "Binary installed to $USER_BIN_DIR/$APP_NAME"
    
    # Install launcher wrapper
    cp "$SCRIPT_DIR/raven-terminal-wrapper.sh" "$USER_BIN_DIR/raven-terminal-launcher"
    chmod +x "$USER_BIN_DIR/raven-terminal-launcher"
    print_success "Launcher wrapper installed"
    
    # Install icon
    if [ -f "$REPO_DIR/assets/raven_terminal_icon.svg" ]; then
        cp "$REPO_DIR/assets/raven_terminal_icon.svg" "$USER_ICON_DIR/$APP_NAME.svg"
        print_success "Icon installed to $USER_ICON_DIR/$APP_NAME.svg"
        
        # Also install to pixmaps for better compatibility
        mkdir -p "$HOME/.local/share/pixmaps"
        cp "$REPO_DIR/assets/raven_terminal_icon.svg" "$HOME/.local/share/pixmaps/$APP_NAME.svg"
    else
        print_warning "Icon file not found, using default terminal icon"
    fi
    
    # Create desktop file (use launcher wrapper for better environment handling)
    local icon_name="$APP_NAME"
    if [ ! -f "$USER_ICON_DIR/$APP_NAME.svg" ]; then
        icon_name="utilities-terminal"
    fi
    
    create_desktop_file "$USER_BIN_DIR/raven-terminal-launcher" "$USER_APP_DIR/$APP_NAME.desktop" "$icon_name"
    print_success "Desktop entry created at $USER_APP_DIR/$APP_NAME.desktop"
    
    # Update icon cache if gtk-update-icon-cache is available
    if command -v gtk-update-icon-cache &> /dev/null; then
        gtk-update-icon-cache -f -t "$HOME/.local/share/icons/hicolor" 2>/dev/null || true
    fi
    
    # Also try gtk4 icon cache update
    if command -v gtk4-update-icon-cache &> /dev/null; then
        gtk4-update-icon-cache -f -t "$HOME/.local/share/icons/hicolor" 2>/dev/null || true
    fi
    
    # Update desktop database if available
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
    sudo cp "$REPO_DIR/$APP_NAME" "$GLOBAL_BIN_DIR/"
    sudo chmod +x "$GLOBAL_BIN_DIR/$APP_NAME"
    print_success "Binary installed to $GLOBAL_BIN_DIR/$APP_NAME"
    
    # Install launcher wrapper
    sudo cp "$SCRIPT_DIR/raven-terminal-wrapper.sh" "$GLOBAL_BIN_DIR/raven-terminal-launcher"
    sudo chmod +x "$GLOBAL_BIN_DIR/raven-terminal-launcher"
    print_success "Launcher wrapper installed"
    
    # Install icon
    if [ -f "$REPO_DIR/assets/raven_terminal_icon.svg" ]; then
        sudo mkdir -p "$GLOBAL_ICON_DIR"
        sudo cp "$REPO_DIR/assets/raven_terminal_icon.svg" "$GLOBAL_ICON_DIR/$APP_NAME.svg"
        print_success "Icon installed to $GLOBAL_ICON_DIR/$APP_NAME.svg"
        
        # Also install to pixmaps for better compatibility
        sudo mkdir -p /usr/share/pixmaps
        sudo cp "$REPO_DIR/assets/raven_terminal_icon.svg" "/usr/share/pixmaps/$APP_NAME.svg"
    else
        print_warning "Icon file not found, using default terminal icon"
    fi
    
    # Create desktop file (use launcher wrapper for better environment handling)
    local icon_name="$APP_NAME"
    if [ ! -f "$GLOBAL_ICON_DIR/$APP_NAME.svg" ]; then
        icon_name="utilities-terminal"
    fi
    
    local tmp_desktop=$(mktemp)
    create_desktop_file "$GLOBAL_BIN_DIR/raven-terminal-launcher" "$tmp_desktop" "$icon_name"
    sudo mv "$tmp_desktop" "$GLOBAL_APP_DIR/$APP_NAME.desktop"
    print_success "Desktop entry created at $GLOBAL_APP_DIR/$APP_NAME.desktop"
    
    # Update icon cache
    if command -v gtk-update-icon-cache &> /dev/null; then
        sudo gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
    fi
    
    # Also try gtk4 icon cache update
    if command -v gtk4-update-icon-cache &> /dev/null; then
        sudo gtk4-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
    fi
    
    # Update desktop database
    if command -v update-desktop-database &> /dev/null; then
        sudo update-desktop-database "$GLOBAL_APP_DIR" 2>/dev/null || true
    fi
}

cleanup_build() {
    if [ "$BUILD_ONLY" = false ] && [ -f "$REPO_DIR/$APP_NAME" ]; then
        rm -f "$REPO_DIR/$APP_NAME"
        print_info "Cleaned up build artifacts"
    fi
}

print_completion() {
    echo ""
    echo -e "${GREEN}============================================${NC}"
    echo -e "${GREEN}     Installation Complete!${NC}"
    echo -e "${GREEN}============================================${NC}"
    echo ""
    
    if [ "$BUILD_ONLY" = true ]; then
        echo "Binary built at: $REPO_DIR/$APP_NAME"
        echo ""
        echo "To run: ./$APP_NAME"
    else
        echo "You can now launch Raven Terminal:"
        echo ""
        if [ "$INSTALL_MODE" = "user" ]; then
            echo "  - From terminal: $APP_NAME"
            echo "  - From application menu: Search for 'Raven Terminal'"
        else
            echo "  - From terminal: $APP_NAME"
            echo "  - From application menu: Search for 'Raven Terminal'"
        fi
        echo ""
        echo "To uninstall, run: $(dirname "$0")/uninstall.sh --$INSTALL_MODE"
    fi
    echo ""
}

main() {
    print_header
    parse_args "$@"
    
    echo "Installation mode: $INSTALL_MODE"
    echo "Repository: $REPO_DIR"
    echo ""
    
    check_dependencies
    build_application
    
    if [ "$BUILD_ONLY" = true ]; then
        print_completion
        exit 0
    fi
    
    case $INSTALL_MODE in
        user)
            install_user
            ;;
        global)
            install_global
            ;;
    esac
    
    cleanup_build
    print_completion
}

main "$@"
