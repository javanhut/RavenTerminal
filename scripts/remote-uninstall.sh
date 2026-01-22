#!/bin/bash
#
# Raven Terminal Remote Uninstallation Script
# Uninstall Raven Terminal with a single curl command
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-uninstall.sh | bash
#   curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-uninstall.sh | bash -s -- --config
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
REMOVE_CONFIG=false
FORCE=false
VERBOSE=false

# Application info
APP_NAME="raven-terminal"

# Installation paths
USER_BIN_DIR="$HOME/.local/bin"
USER_APP_DIR="$HOME/.local/share/applications"
USER_ICON_DIR="$HOME/.local/share/icons/hicolor/scalable/apps"
USER_PIXMAP_DIR="$HOME/.local/share/pixmaps"
USER_DATA_DIR="$HOME/.local/share/raven-terminal"
USER_CONFIG_DIR="$HOME/.config/raven-terminal"

GLOBAL_BIN_DIR="/usr/local/bin"
GLOBAL_APP_DIR="/usr/share/applications"
GLOBAL_ICON_DIR="/usr/share/icons/hicolor/scalable/apps"
GLOBAL_PIXMAP_DIR="/usr/share/pixmaps"

print_header() {
    echo -e "${BLUE}"
    echo "============================================"
    echo "   Raven Terminal Remote Uninstaller"
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
Usage: $0 [OPTIONS]

Uninstall Raven Terminal from your system.

OPTIONS:
    -c, --config        Also remove configuration files
                        (~/.config/raven-terminal/)

    -f, --force         Don't ask for confirmation

    -v, --verbose       Show verbose output

    -h, --help          Show this help message

EXAMPLES:
    # Remove Raven Terminal (preserves config)
    curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-uninstall.sh | bash

    # Remove everything including config
    curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-uninstall.sh | bash -s -- --config

    # Force remove without confirmation
    curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-uninstall.sh | bash -s -- --force

EOF
    exit 0
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -c|--config)
                REMOVE_CONFIG=true
                shift
                ;;
            -f|--force)
                FORCE=true
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

detect_installations() {
    local found_user=false
    local found_global=false

    # Check user installation
    if [ -f "$USER_BIN_DIR/$APP_NAME" ] || \
       [ -f "$USER_APP_DIR/$APP_NAME.desktop" ] || \
       [ -f "$USER_ICON_DIR/$APP_NAME.svg" ]; then
        found_user=true
    fi

    # Check global installation
    if [ -f "$GLOBAL_BIN_DIR/$APP_NAME" ] || \
       [ -f "$GLOBAL_APP_DIR/$APP_NAME.desktop" ] || \
       [ -f "$GLOBAL_ICON_DIR/$APP_NAME.svg" ]; then
        found_global=true
    fi

    echo "Detected installations:"
    if [ "$found_user" = true ]; then
        echo "  - User installation (in ~/.local/)"
    fi
    if [ "$found_global" = true ]; then
        echo "  - Global installation (in /usr/)"
    fi
    if [ "$found_user" = false ] && [ "$found_global" = false ]; then
        echo "  - None found"
        if [ "$REMOVE_CONFIG" = false ]; then
            print_info "Nothing to uninstall"
            exit 0
        fi
    fi

    if [ -d "$USER_CONFIG_DIR" ]; then
        echo "  - Configuration files (in ~/.config/raven-terminal/)"
    fi

    echo ""
}

confirm_uninstall() {
    if [ "$FORCE" = true ]; then
        return 0
    fi

    # When piped from curl, stdin is not a terminal
    # In this case, proceed without confirmation
    if [ ! -t 0 ]; then
        print_info "Running in non-interactive mode, proceeding with uninstall..."
        return 0
    fi

    echo -e "${YELLOW}This will remove Raven Terminal from your system.${NC}"
    if [ "$REMOVE_CONFIG" = true ]; then
        echo -e "${YELLOW}Configuration files will also be removed.${NC}"
    fi
    echo ""
    read -p "Are you sure you want to continue? [y/N] " -n 1 -r
    echo ""

    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_info "Uninstallation cancelled."
        exit 0
    fi
}

remove_file() {
    local file="$1"
    local use_sudo="$2"
    local label="$3"

    if [ -f "$file" ]; then
        if [ "$use_sudo" = true ]; then
            sudo rm -f "$file"
        else
            rm -f "$file"
        fi
        if [ "$VERBOSE" = true ]; then
            print_success "Removed: $label"
        fi
        return 0
    fi
    return 1
}

remove_dir() {
    local dir="$1"
    local use_sudo="$2"
    local label="$3"

    if [ -d "$dir" ]; then
        if [ "$use_sudo" = true ]; then
            sudo rm -rf "$dir"
        else
            rm -rf "$dir"
        fi
        if [ "$VERBOSE" = true ]; then
            print_success "Removed: $label"
        fi
        return 0
    fi
    return 1
}

uninstall_user() {
    print_info "Removing user installation..."

    local removed=0

    # Remove binary
    if remove_file "$USER_BIN_DIR/$APP_NAME" false "Binary"; then
        ((++removed))
    fi

    # Remove launcher wrapper
    if remove_file "$USER_BIN_DIR/raven-terminal-launcher" false "Launcher"; then
        ((++removed))
    fi

    # Remove desktop file
    if remove_file "$USER_APP_DIR/$APP_NAME.desktop" false "Desktop entry"; then
        ((++removed))
    fi

    # Remove icons
    if remove_file "$USER_ICON_DIR/$APP_NAME.svg" false "Icon"; then
        ((++removed))
    fi

    if remove_file "$USER_PIXMAP_DIR/$APP_NAME.svg" false "Pixmap icon"; then
        ((++removed))
    fi

    # Remove data directory
    if remove_dir "$USER_DATA_DIR" false "Data directory"; then
        ((++removed))
    fi

    if [ $removed -gt 0 ]; then
        print_success "User installation removed ($removed items)"

        # Update icon cache
        if command -v gtk-update-icon-cache &> /dev/null; then
            gtk-update-icon-cache -f -t "$HOME/.local/share/icons/hicolor" 2>/dev/null || true
        fi

        # Update desktop database
        if command -v update-desktop-database &> /dev/null; then
            update-desktop-database "$USER_APP_DIR" 2>/dev/null || true
        fi
    else
        print_info "No user installation found"
    fi
}

uninstall_global() {
    # Check if any global files exist
    if [ ! -f "$GLOBAL_BIN_DIR/$APP_NAME" ] && \
       [ ! -f "$GLOBAL_APP_DIR/$APP_NAME.desktop" ] && \
       [ ! -f "$GLOBAL_ICON_DIR/$APP_NAME.svg" ]; then
        return 0
    fi

    print_info "Removing global installation (requires sudo)..."

    # Check for sudo
    if ! command -v sudo &> /dev/null; then
        print_warning "sudo not available, skipping global uninstall"
        return 0
    fi

    local removed=0

    # Remove binary
    if remove_file "$GLOBAL_BIN_DIR/$APP_NAME" true "Binary"; then
        ((++removed))
    fi

    # Remove launcher wrapper
    if remove_file "$GLOBAL_BIN_DIR/raven-terminal-launcher" true "Launcher"; then
        ((++removed))
    fi

    # Remove desktop file
    if remove_file "$GLOBAL_APP_DIR/$APP_NAME.desktop" true "Desktop entry"; then
        ((++removed))
    fi

    # Remove icons
    if remove_file "$GLOBAL_ICON_DIR/$APP_NAME.svg" true "Icon"; then
        ((++removed))
    fi

    if remove_file "$GLOBAL_PIXMAP_DIR/$APP_NAME.svg" true "Pixmap icon"; then
        ((++removed))
    fi

    if [ $removed -gt 0 ]; then
        print_success "Global installation removed ($removed items)"

        # Update icon cache
        if command -v gtk-update-icon-cache &> /dev/null; then
            sudo gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
        fi

        # Update desktop database
        if command -v update-desktop-database &> /dev/null; then
            sudo update-desktop-database "$GLOBAL_APP_DIR" 2>/dev/null || true
        fi
    fi
}

remove_config() {
    print_info "Removing configuration files..."

    if remove_dir "$USER_CONFIG_DIR" false "Configuration directory"; then
        print_success "Configuration files removed"
    else
        print_info "No configuration files found"
    fi
}

print_completion() {
    echo ""
    echo -e "${GREEN}============================================${NC}"
    echo -e "${GREEN}     Uninstallation Complete!${NC}"
    echo -e "${GREEN}============================================${NC}"
    echo ""

    if [ "$REMOVE_CONFIG" = false ] && [ -d "$USER_CONFIG_DIR" ]; then
        echo "Configuration files were preserved at:"
        echo "  $USER_CONFIG_DIR"
        echo ""
        echo "To remove them, run with --config flag:"
        echo "  curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-uninstall.sh | bash -s -- --config"
    fi
    echo ""
}

main() {
    print_header
    parse_args "$@"

    detect_installations
    confirm_uninstall

    uninstall_user
    uninstall_global

    if [ "$REMOVE_CONFIG" = true ]; then
        remove_config
    fi

    print_completion
}

main "$@"
