#!/bin/bash
#
# Raven Terminal Uninstallation Script
# Removes Raven Terminal and associated files
#

set -e

# Detect OS type
OS_TYPE="$(uname -s)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
UNINSTALL_MODE=""
REMOVE_CONFIG=false
FORCE=false
VERBOSE=false
REPORT=()

# Application info
APP_NAME="raven-terminal"

# Installation paths
USER_BIN_DIR="$HOME/.local/bin"
USER_APP_DIR="$HOME/.local/share/applications"
USER_ICON_DIR="$HOME/.local/share/icons/hicolor/scalable/apps"
USER_PIXMAP_DIR="$HOME/.local/share/pixmaps"
USER_CONFIG_DIR="$HOME/.config/raven-terminal"

GLOBAL_BIN_DIR="/usr/local/bin"
GLOBAL_APP_DIR="/usr/share/applications"
GLOBAL_ICON_DIR="/usr/share/icons/hicolor/scalable/apps"
GLOBAL_PIXMAP_DIR="/usr/share/pixmaps"
LEGACY_GLOBAL_BIN_DIR="/usr/bin"

# macOS paths
MACOS_USER_APP_DIR="$HOME/Applications"
MACOS_GLOBAL_APP_DIR="/Applications"
MACOS_APP_NAME="Raven Terminal"

print_header() {
    echo -e "${BLUE}"
    echo "============================================"
    echo "      Raven Terminal Uninstaller"
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

record_action() {
    REPORT+=("$1")
}

usage() {
    cat << EOF
Usage: $(basename "$0") [OPTIONS]

Uninstall Raven Terminal from your system.

OPTIONS:
    -u, --user          Uninstall user installation
                        (from ~/.local/)

    -g, --global        Uninstall system-wide installation (requires sudo)
                        (from /usr/local/ and /usr/share/)

    -a, --all           Uninstall from both user and global locations

    -c, --config        Also remove configuration files
                        (~/.config/raven-terminal/)

    -f, --force         Don't ask for confirmation

    -v, --verbose       Show verbose output

    -h, --help          Show this help message

EXAMPLES:
    $(basename "$0") --user           # Remove user installation
    $(basename "$0") --global         # Remove system-wide installation
    $(basename "$0") --all --config   # Remove everything including config

EOF
    exit 0
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -u|--user)
                UNINSTALL_MODE="user"
                shift
                ;;
            -g|--global)
                UNINSTALL_MODE="global"
                shift
                ;;
            -a|--all)
                UNINSTALL_MODE="all"
                shift
                ;;
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
    
    if [ -z "$UNINSTALL_MODE" ]; then
        print_error "Please specify uninstall mode: --user, --global, or --all"
        echo "Use --help for usage information."
        exit 1
    fi
}

detect_installations() {
    local found_user=false
    local found_global=false

    if [ "$OS_TYPE" = "Darwin" ]; then
        # macOS: Check for app bundles
        if [ -d "$MACOS_USER_APP_DIR/${MACOS_APP_NAME}.app" ] || \
           [ -L "$HOME/.local/bin/$APP_NAME" ]; then
            found_user=true
        fi

        if [ -d "$MACOS_GLOBAL_APP_DIR/${MACOS_APP_NAME}.app" ] || \
           [ -L "$GLOBAL_BIN_DIR/$APP_NAME" ]; then
            found_global=true
        fi

        echo "Detected installations:"
        if [ "$found_user" = true ]; then
            echo "  - User installation (in ~/Applications/)"
        fi
        if [ "$found_global" = true ]; then
            echo "  - Global installation (in /Applications/)"
        fi
    else
        # Linux: Check traditional paths
        if [ -f "$USER_BIN_DIR/$APP_NAME" ] || \
           [ -f "$USER_APP_DIR/$APP_NAME.desktop" ] || \
           [ -f "$USER_ICON_DIR/$APP_NAME.svg" ]; then
            found_user=true
        fi

        if [ -f "$GLOBAL_BIN_DIR/$APP_NAME" ] || \
           [ -f "$LEGACY_GLOBAL_BIN_DIR/$APP_NAME" ] || \
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
    fi

    if [ "$found_user" = false ] && [ "$found_global" = false ]; then
        echo "  - None found"
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
    local target="$file"
    if [ -n "$label" ]; then
        target="$label ($file)"
    fi
    
    if [ -f "$file" ]; then
        if [ "$use_sudo" = true ]; then
            sudo rm -f "$file"
        else
            rm -f "$file"
        fi
        record_action "Removed: $target"
        return 0
    else
        record_action "Not found: $target"
        return 1
    fi
}

remove_dir() {
    local dir="$1"
    local use_sudo="$2"
    local label="$3"
    local target="$dir"
    if [ -n "$label" ]; then
        target="$label ($dir)"
    fi
    
    if [ -d "$dir" ]; then
        if [ "$use_sudo" = true ]; then
            sudo rm -rf "$dir"
        else
            rm -rf "$dir"
        fi
        record_action "Removed: $target"
        return 0
    else
        record_action "Not found: $target"
        return 1
    fi
}

uninstall_user() {
    print_info "Removing user installation..."
    
    local removed=0
    
    # Remove binary
    if remove_file "$USER_BIN_DIR/$APP_NAME" false "User binary"; then
        ((++removed))
    fi
    
    # Remove launcher wrapper
    if remove_file "$USER_BIN_DIR/raven-terminal-launcher" false "User launcher"; then
        ((++removed))
    fi
    
    # Remove desktop file
    if remove_file "$USER_APP_DIR/$APP_NAME.desktop" false "User desktop file"; then
        ((++removed))
    fi
    
    # Remove icon
    if remove_file "$USER_ICON_DIR/$APP_NAME.svg" false "User icon"; then
        ((++removed))
    fi
    
    # Remove pixmap icon
    if remove_file "$USER_PIXMAP_DIR/$APP_NAME.svg" false "User pixmap icon"; then
        ((++removed))
    fi

    # Remove log directory
    if remove_dir "$HOME/.local/share/raven-terminal" false "User data dir"; then
        ((++removed))
    fi
    
    if [ $removed -gt 0 ]; then
        print_success "User installation removed ($removed files)"
        
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
    print_info "Removing global installation (requires sudo)..."
    
    # Check for sudo
    if ! command -v sudo &> /dev/null; then
        print_error "sudo is required for removing global installation"
        exit 1
    fi
    
    local removed=0
    
    # Remove binary
    if remove_file "$GLOBAL_BIN_DIR/$APP_NAME" true "Global binary"; then
        ((++removed))
    fi
    
    # Remove launcher wrapper
    if remove_file "$GLOBAL_BIN_DIR/raven-terminal-launcher" true "Global launcher"; then
        ((++removed))
    fi

    # Remove legacy binary locations
    if remove_file "$LEGACY_GLOBAL_BIN_DIR/$APP_NAME" true "Legacy global binary"; then
        ((++removed))
    fi
    if remove_file "$LEGACY_GLOBAL_BIN_DIR/raven-terminal-launcher" true "Legacy global launcher"; then
        ((++removed))
    fi
    
    # Remove desktop file
    if remove_file "$GLOBAL_APP_DIR/$APP_NAME.desktop" true "Global desktop file"; then
        ((++removed))
    fi
    
    # Remove icon
    if remove_file "$GLOBAL_ICON_DIR/$APP_NAME.svg" true "Global icon"; then
        ((++removed))
    fi

    # Remove pixmap icon
    if remove_file "$GLOBAL_PIXMAP_DIR/$APP_NAME.svg" true "Global pixmap icon"; then
        ((++removed))
    fi
    
    if [ $removed -gt 0 ]; then
        print_success "Global installation removed ($removed files)"
        
        # Update icon cache
        if command -v gtk-update-icon-cache &> /dev/null; then
            sudo gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
        fi
        
        # Update desktop database
        if command -v update-desktop-database &> /dev/null; then
            sudo update-desktop-database "$GLOBAL_APP_DIR" 2>/dev/null || true
        fi
    else
        print_info "No global installation found"
    fi
}

remove_config() {
    print_info "Removing configuration files..."

    local removed_any=false
    if remove_file "$USER_CONFIG_DIR/config.toml" false "Config file"; then
        removed_any=true
    fi
    if remove_dir "$USER_CONFIG_DIR" false "Config dir"; then
        removed_any=true
    fi

    if [ "$removed_any" = true ]; then
        print_success "Configuration files removed"
    else
        print_info "No configuration files found"
    fi
}

# macOS: Remove app bundle
uninstall_macos_user() {
    print_info "Removing macOS user installation..."

    local removed=0
    local app_bundle="$MACOS_USER_APP_DIR/${MACOS_APP_NAME}.app"

    # Remove user app bundle
    if remove_dir "$app_bundle" false "User app bundle"; then
        ((++removed))
    fi

    # Remove CLI symlink
    if remove_file "$HOME/.local/bin/$APP_NAME" false "User CLI symlink"; then
        ((++removed))
    fi

    if [ $removed -gt 0 ]; then
        print_success "macOS user installation removed ($removed items)"
    else
        print_info "No macOS user installation found"
    fi
}

uninstall_macos_global() {
    print_info "Removing macOS global installation (requires sudo)..."

    # Check for sudo
    if ! command -v sudo &> /dev/null; then
        print_error "sudo is required for removing global installation"
        exit 1
    fi

    local removed=0
    local app_bundle="$MACOS_GLOBAL_APP_DIR/${MACOS_APP_NAME}.app"

    # Remove global app bundle
    if [ -d "$app_bundle" ]; then
        sudo rm -rf "$app_bundle"
        record_action "Removed: Global app bundle ($app_bundle)"
        ((++removed))
    else
        record_action "Not found: Global app bundle ($app_bundle)"
    fi

    # Remove CLI symlink
    if [ -L "$GLOBAL_BIN_DIR/$APP_NAME" ]; then
        sudo rm "$GLOBAL_BIN_DIR/$APP_NAME"
        record_action "Removed: Global CLI symlink ($GLOBAL_BIN_DIR/$APP_NAME)"
        ((++removed))
    else
        record_action "Not found: Global CLI symlink ($GLOBAL_BIN_DIR/$APP_NAME)"
    fi

    if [ $removed -gt 0 ]; then
        print_success "macOS global installation removed ($removed items)"
    else
        print_info "No macOS global installation found"
    fi
}

print_completion() {
    echo ""
    echo -e "${GREEN}============================================${NC}"
    echo -e "${GREEN}     Uninstallation Complete!${NC}"
    echo -e "${GREEN}============================================${NC}"
    echo ""

    if [ "$VERBOSE" = true ] && [ ${#REPORT[@]} -gt 0 ]; then
        echo "Removal details:"
        for line in "${REPORT[@]}"; do
            echo "  - $line"
        done
        echo ""
    fi
    
    if [ "$REMOVE_CONFIG" = false ] && [ -d "$USER_CONFIG_DIR" ]; then
        echo "Configuration files were preserved at:"
        echo "  $USER_CONFIG_DIR"
        echo ""
        echo "To remove them, run with --config flag"
    fi
    echo ""
}

main() {
    print_header
    parse_args "$@"
    
    detect_installations
    confirm_uninstall
    
    # Branch based on OS type
    if [ "$OS_TYPE" = "Darwin" ]; then
        # macOS: Remove app bundles
        case $UNINSTALL_MODE in
            user)
                uninstall_macos_user
                ;;
            global)
                uninstall_macos_global
                ;;
            all)
                uninstall_macos_user
                uninstall_macos_global
                ;;
        esac
    else
        # Linux: Use traditional uninstallation
        case $UNINSTALL_MODE in
            user)
                uninstall_user
                ;;
            global)
                uninstall_global
                ;;
            all)
                uninstall_user
                uninstall_global
                ;;
        esac
    fi
    
    if [ "$REMOVE_CONFIG" = true ]; then
        remove_config
    fi
    
    print_completion
}

main "$@"
