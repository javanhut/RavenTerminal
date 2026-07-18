#!/bin/bash
#
# Raven Terminal Installation Script
# Installs the Raven Terminal application with desktop integration
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

warn_path_conflict() {
    local expected_path="$1"
    local found_path=""

    if command -v "$APP_NAME" &> /dev/null; then
        found_path="$(command -v "$APP_NAME")"
    fi

    if [ -n "$found_path" ] && [ "$found_path" != "$expected_path" ]; then
        print_warning "PATH resolves $APP_NAME to $found_path"
        print_warning "Expected: $expected_path"
        echo "Run uninstall for the other install or adjust PATH."
    fi
}

desktop_exec_path() {
    local desktop_file="$1"
    if [ ! -f "$desktop_file" ]; then
        return 1
    fi

    local exec_line
    exec_line="$(sed -n 's/^Exec=//p' "$desktop_file" | head -n 1)"
    if [ -z "$exec_line" ]; then
        return 1
    fi

    echo "$exec_line" | awk '{print $1}'
}

ensure_launcher() {
    local launcher_path="$1"
    local launcher_dir

    if [ -x "$launcher_path" ]; then
        return 0
    fi

    launcher_dir="$(dirname "$launcher_path")"
    mkdir -p "$launcher_dir"
    cp "$SCRIPT_DIR/raven-terminal-wrapper.sh" "$launcher_path"
    chmod +x "$launcher_path"
}

fix_stale_desktop_entry() {
    local desktop_file="$1"
    local expected_exec="$2"
    local fallback_exec="$3"
    local create_launcher="$4"
    local exec_path

    exec_path="$(desktop_exec_path "$desktop_file")" || exec_path=""
    if [ -z "$exec_path" ]; then
        return 0
    fi

    if [ -x "$exec_path" ]; then
        return 0
    fi

    if [ "$create_launcher" = true ] && [ "$exec_path" = "$expected_exec" ]; then
        ensure_launcher "$expected_exec"
        if [ -x "$expected_exec" ]; then
            print_warning "Created missing launcher: $expected_exec"
            return 0
        fi
    fi

    if [ -x "$expected_exec" ]; then
        print_warning "Fixing stale desktop entry: $desktop_file"
        local tmp_desktop
        tmp_desktop="$(mktemp)"
        sed "s|^Exec=.*|Exec=$expected_exec|" "$desktop_file" > "$tmp_desktop"
        mv "$tmp_desktop" "$desktop_file"
        if [ "$VERBOSE" = true ]; then
            print_success "Updated Exec to $expected_exec"
        fi
        return 0
    fi

    if [ -n "$fallback_exec" ] && [ -x "$fallback_exec" ]; then
        print_warning "Fixing stale desktop entry: $desktop_file"
        local tmp_desktop
        tmp_desktop="$(mktemp)"
        sed "s|^Exec=.*|Exec=$fallback_exec|" "$desktop_file" > "$tmp_desktop"
        mv "$tmp_desktop" "$desktop_file"
        if [ "$VERBOSE" = true ]; then
            print_success "Updated Exec to $fallback_exec"
        fi
    fi
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
        if [ "$OS_TYPE" = "Darwin" ]; then
            echo "  macOS: brew install go"
        else
            echo "  Arch: sudo pacman -S go"
            echo "  Ubuntu/Debian: sudo apt install golang"
            echo "  Fedora: sudo dnf install golang"
        fi
        exit 1
    fi
    print_success "Go found: $(go version | awk '{print $3}')"

    # macOS-specific dependency checks
    if [ "$OS_TYPE" = "Darwin" ]; then
        # Check for Homebrew
        if ! command -v brew &> /dev/null; then
            print_error "Homebrew is not installed. Please install it first:"
            echo "  /bin/bash -c \"\$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""
            exit 1
        fi
        print_success "Homebrew found"

        # Check for icon conversion tools and install if missing
        if ! command -v rsvg-convert &> /dev/null && ! command -v convert &> /dev/null; then
            print_info "Installing librsvg for icon generation..."
            if brew install librsvg; then
                print_success "librsvg installed"
            else
                print_warning "Failed to install librsvg. App icon may not be generated."
            fi
        else
            print_success "SVG conversion tools found"
        fi
        return 0
    fi

    # Linux: Check for required system libraries (for OpenGL)
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
        go build -v -o "$APP_NAME" ./src
    else
        go build -o "$APP_NAME" ./src 2>&1
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

# macOS: Convert SVG to ICNS icon format
convert_svg_to_icns() {
    local resources_dir="$1"
    local svg_path="$REPO_DIR/src/assets/raven_terminal_icon.svg"
    local prebuilt_icns="$REPO_DIR/src/assets/raven-terminal.icns"
    local iconset_dir="/tmp/raven-terminal.iconset"
    local icns_path="${resources_dir}/raven-terminal.icns"

    # First check for pre-built ICNS file in repo
    if [ -f "$prebuilt_icns" ]; then
        cp "$prebuilt_icns" "$icns_path"
        print_success "Copied pre-built icon: $icns_path"
        return 0
    fi

    # Check for conversion tools, install if missing
    if ! command -v rsvg-convert &> /dev/null && ! command -v convert &> /dev/null; then
        print_info "SVG conversion tools not found. Installing librsvg..."
        if command -v brew &> /dev/null; then
            if brew install librsvg; then
                print_success "librsvg installed"
            else
                print_warning "Failed to install librsvg. Skipping icon generation."
                return 0
            fi
        else
            print_warning "Homebrew not found. Skipping icon generation."
            return 0
        fi
    fi

    if [ ! -f "$svg_path" ]; then
        print_warning "SVG icon not found at $svg_path. Skipping icon generation."
        return 0
    fi

    mkdir -p "$iconset_dir"

    print_info "Generating macOS icon from SVG..."

    # Generate required icon sizes (macOS iconset requires specific sizes)
    # Standard sizes: 16, 32, 128, 256, 512 (plus @2x retina versions)
    local sizes=(16 32 128 256 512)
    local failed=false

    for size in "${sizes[@]}"; do
        local retina_size=$((size * 2))
        if command -v rsvg-convert &> /dev/null; then
            rsvg-convert -w $size -h $size "$svg_path" -o "${iconset_dir}/icon_${size}x${size}.png" 2>/dev/null || failed=true
            rsvg-convert -w $retina_size -h $retina_size "$svg_path" -o "${iconset_dir}/icon_${size}x${size}@2x.png" 2>/dev/null || failed=true
        else
            convert -background none -resize ${size}x${size} "$svg_path" "${iconset_dir}/icon_${size}x${size}.png" 2>/dev/null || failed=true
            convert -background none -resize ${retina_size}x${retina_size} "$svg_path" "${iconset_dir}/icon_${size}x${size}@2x.png" 2>/dev/null || failed=true
        fi
    done

    if [ "$failed" = true ]; then
        print_warning "Some icon sizes failed to generate"
        rm -rf "$iconset_dir"
        return 0
    fi

    # Create ICNS file using macOS iconutil
    if command -v iconutil &> /dev/null; then
        if iconutil -c icns "$iconset_dir" -o "$icns_path" 2>/dev/null; then
            print_success "Created icon: $icns_path"
        else
            print_warning "iconutil failed to create ICNS file"
        fi
    else
        print_warning "iconutil not found. Cannot create ICNS file."
    fi

    rm -rf "$iconset_dir"
}

# macOS: Install as app bundle
install_macos() {
    local app_name="Raven Terminal"
    local app_bundle

    if [ "$INSTALL_MODE" = "global" ]; then
        app_bundle="/Applications/${app_name}.app"
        print_info "Installing system-wide to /Applications (requires sudo)..."
    else
        app_bundle="$HOME/Applications/${app_name}.app"
        mkdir -p "$HOME/Applications"
        print_info "Installing for current user to ~/Applications..."
    fi

    # Create app bundle structure
    if [ "$INSTALL_MODE" = "global" ]; then
        sudo mkdir -p "${app_bundle}/Contents/MacOS"
        sudo mkdir -p "${app_bundle}/Contents/Resources"
    else
        mkdir -p "${app_bundle}/Contents/MacOS"
        mkdir -p "${app_bundle}/Contents/Resources"
    fi

    # Copy binary
    if [ "$INSTALL_MODE" = "global" ]; then
        sudo cp "$REPO_DIR/$APP_NAME" "${app_bundle}/Contents/MacOS/"
        sudo chmod +x "${app_bundle}/Contents/MacOS/$APP_NAME"
    else
        cp "$REPO_DIR/$APP_NAME" "${app_bundle}/Contents/MacOS/"
        chmod +x "${app_bundle}/Contents/MacOS/$APP_NAME"
    fi
    print_success "Binary installed to ${app_bundle}/Contents/MacOS/$APP_NAME"

    # Clean up build artifact
    rm -f "$REPO_DIR/$APP_NAME"
    print_info "Cleaned up build artifact from source directory"

    # Create Info.plist
    local plist_content='<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>raven-terminal</string>
    <key>CFBundleIconFile</key>
    <string>raven-terminal</string>
    <key>CFBundleIdentifier</key>
    <string>com.javanhut.raven-terminal</string>
    <key>CFBundleName</key>
    <string>Raven Terminal</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0</string>
    <key>CFBundleVersion</key>
    <string>1</string>
    <key>LSMinimumSystemVersion</key>
    <string>10.13</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>'

    if [ "$INSTALL_MODE" = "global" ]; then
        echo "$plist_content" | sudo tee "${app_bundle}/Contents/Info.plist" > /dev/null
    else
        echo "$plist_content" > "${app_bundle}/Contents/Info.plist"
    fi
    print_success "Created Info.plist"

    # Convert SVG to ICNS (needs to be done to a temp location first for global install)
    if [ "$INSTALL_MODE" = "global" ]; then
        local temp_resources="/tmp/raven-terminal-resources"
        mkdir -p "$temp_resources"
        convert_svg_to_icns "$temp_resources"
        if [ -f "$temp_resources/raven-terminal.icns" ]; then
            sudo cp "$temp_resources/raven-terminal.icns" "${app_bundle}/Contents/Resources/"
        fi
        rm -rf "$temp_resources"
    else
        convert_svg_to_icns "${app_bundle}/Contents/Resources"
    fi

    # Create CLI symlink for terminal access
    if [ "$INSTALL_MODE" = "global" ]; then
        sudo ln -sf "${app_bundle}/Contents/MacOS/$APP_NAME" /usr/local/bin/$APP_NAME
        print_success "Created CLI symlink at /usr/local/bin/$APP_NAME"
    else
        mkdir -p "$HOME/.local/bin"
        ln -sf "${app_bundle}/Contents/MacOS/$APP_NAME" "$HOME/.local/bin/$APP_NAME"
        print_success "Created CLI symlink at $HOME/.local/bin/$APP_NAME"

        # Check if ~/.local/bin is in PATH
        if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
            print_warning "$HOME/.local/bin is not in your PATH"
            echo ""
            echo "Add this to your ~/.bashrc or ~/.zshrc:"
            echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
            echo ""
        fi
    fi

    print_success "Installed app bundle to: ${app_bundle}"
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

    # Clean up build artifact immediately after copying
    rm -f "$REPO_DIR/$APP_NAME"
    print_info "Cleaned up build artifact from source directory"
    
    # Install launcher wrapper
    cp "$SCRIPT_DIR/raven-terminal-wrapper.sh" "$USER_BIN_DIR/raven-terminal-launcher"
    chmod +x "$USER_BIN_DIR/raven-terminal-launcher"
    print_success "Launcher wrapper installed"
    
    # Install icon
    if [ -f "$REPO_DIR/src/assets/raven_terminal_icon.svg" ]; then
        cp "$REPO_DIR/src/assets/raven_terminal_icon.svg" "$USER_ICON_DIR/$APP_NAME.svg"
        print_success "Icon installed to $USER_ICON_DIR/$APP_NAME.svg"

        # Also install to pixmaps for better compatibility
        mkdir -p "$HOME/.local/share/pixmaps"
        cp "$REPO_DIR/src/assets/raven_terminal_icon.svg" "$HOME/.local/share/pixmaps/$APP_NAME.svg"
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

    # Clean up build artifact immediately after copying
    rm -f "$REPO_DIR/$APP_NAME"
    print_info "Cleaned up build artifact from source directory"
    
    # Install launcher wrapper
    sudo cp "$SCRIPT_DIR/raven-terminal-wrapper.sh" "$GLOBAL_BIN_DIR/raven-terminal-launcher"
    sudo chmod +x "$GLOBAL_BIN_DIR/raven-terminal-launcher"
    print_success "Launcher wrapper installed"
    
    # Install icon
    if [ -f "$REPO_DIR/src/assets/raven_terminal_icon.svg" ]; then
        sudo mkdir -p "$GLOBAL_ICON_DIR"
        sudo cp "$REPO_DIR/src/assets/raven_terminal_icon.svg" "$GLOBAL_ICON_DIR/$APP_NAME.svg"
        print_success "Icon installed to $GLOBAL_ICON_DIR/$APP_NAME.svg"

        # Also install to pixmaps for better compatibility
        sudo mkdir -p /usr/share/pixmaps
        sudo cp "$REPO_DIR/src/assets/raven_terminal_icon.svg" "/usr/share/pixmaps/$APP_NAME.svg"
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
        if [ "$OS_TYPE" = "Darwin" ]; then
            if [ "$INSTALL_MODE" = "user" ]; then
                echo "  - From Finder: ~/Applications/Raven Terminal.app"
            else
                echo "  - From Finder: /Applications/Raven Terminal.app"
            fi
            echo "  - From Spotlight: Search for 'Raven Terminal'"
            echo "  - From terminal: $APP_NAME"
        else
            if [ "$INSTALL_MODE" = "user" ]; then
                echo "  - From terminal: $APP_NAME"
                echo "  - From application menu: Search for 'Raven Terminal'"
            else
                echo "  - From terminal: $APP_NAME"
                echo "  - From application menu: Search for 'Raven Terminal'"
            fi
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
    
    # Branch based on OS type
    if [ "$OS_TYPE" = "Darwin" ]; then
        # macOS: Create app bundle
        install_macos
    else
        # Linux: Use traditional installation
        case $INSTALL_MODE in
            user)
                install_user
                warn_path_conflict "$USER_BIN_DIR/$APP_NAME"
                fix_stale_desktop_entry "$USER_APP_DIR/$APP_NAME.desktop" "$USER_BIN_DIR/raven-terminal-launcher" "" true
                ;;
            global)
                install_global
                warn_path_conflict "$GLOBAL_BIN_DIR/$APP_NAME"
                fix_stale_desktop_entry "$GLOBAL_APP_DIR/$APP_NAME.desktop" "$GLOBAL_BIN_DIR/raven-terminal-launcher" "" false
                fix_stale_desktop_entry "$USER_APP_DIR/$APP_NAME.desktop" "$USER_BIN_DIR/raven-terminal-launcher" "$GLOBAL_BIN_DIR/raven-terminal-launcher" true
                ;;
        esac
    fi

    print_completion
}

main "$@"
