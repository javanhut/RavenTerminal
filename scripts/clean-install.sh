#!/bin/bash
#
# Raven Terminal Clean Install Script
# Builds a fresh binary, removes every trace of any previous installation
# (binaries, app bundles, desktop entries, config, caches, running instances),
# then installs the new build and verifies that it is the only copy on the
# system. Works on Linux and macOS.
#
# The build happens BEFORE the purge, so a failed build never leaves you
# without a working install.
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
KEEP_CONFIG=false
DEEP_CLEAN=false
VERBOSE=false

# Application info
APP_NAME="raven-terminal"
MACOS_APP_NAME="Raven Terminal"

# Get script directory (where the repo is)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"

USER_CONFIG_DIR="$HOME/.config/raven-terminal"
CONFIG_BACKUP_DIR=""

print_header() {
    echo -e "${BLUE}"
    echo "============================================"
    echo "    Raven Terminal Clean Installer"
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

Completely remove any existing Raven Terminal installation (including
config and caches) and install a freshly built copy, so nothing carries
over from the previous install.

OPTIONS:
    -u, --user          Install for current user only (default)

    -g, --global        Install system-wide (requires sudo)

    -k, --keep-config   Preserve config.toml and user-init.fish across the
                        reinstall. Generated init scripts are still wiped
                        and recreated.

    -d, --deep          Also wipe the Go build cache before building, so the
                        binary is compiled entirely from scratch.

    -v, --verbose       Show verbose output

    -h, --help          Show this help message

EXAMPLES:
    $(basename "$0")                  # Clean user install, config wiped
    $(basename "$0") --global         # Clean system-wide install
    $(basename "$0") --keep-config    # Clean install but keep your settings
    $(basename "$0") --deep           # Paranoid: rebuild with no Go cache

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
            -k|--keep-config)
                KEEP_CONFIG=true
                shift
                ;;
            -d|--deep)
                DEEP_CLEAN=true
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

backup_config() {
    if [ "$KEEP_CONFIG" != true ]; then
        return 0
    fi

    local files=(
        "$USER_CONFIG_DIR/config.toml"
        "$USER_CONFIG_DIR/scripts/user-init.fish"
    )

    local file found=false
    for file in "${files[@]}"; do
        if [ -f "$file" ]; then
            found=true
            break
        fi
    done

    if [ "$found" != true ]; then
        print_info "No user config found to preserve"
        return 0
    fi

    CONFIG_BACKUP_DIR="$(mktemp -d)"
    for file in "${files[@]}"; do
        if [ -f "$file" ]; then
            cp "$file" "$CONFIG_BACKUP_DIR/"
        fi
    done
    print_success "Backed up user config to $CONFIG_BACKUP_DIR"
}

restore_config() {
    if [ -z "$CONFIG_BACKUP_DIR" ] || [ ! -d "$CONFIG_BACKUP_DIR" ]; then
        return 0
    fi

    if [ -f "$CONFIG_BACKUP_DIR/config.toml" ]; then
        mkdir -p "$USER_CONFIG_DIR"
        cp "$CONFIG_BACKUP_DIR/config.toml" "$USER_CONFIG_DIR/config.toml"
    fi
    if [ -f "$CONFIG_BACKUP_DIR/user-init.fish" ]; then
        mkdir -p "$USER_CONFIG_DIR/scripts"
        cp "$CONFIG_BACKUP_DIR/user-init.fish" "$USER_CONFIG_DIR/scripts/user-init.fish"
    fi

    rm -rf "$CONFIG_BACKUP_DIR"
    CONFIG_BACKUP_DIR=""
    print_success "Restored user config"
}

cleanup_backup() {
    if [ -n "$CONFIG_BACKUP_DIR" ] && [ -d "$CONFIG_BACKUP_DIR" ]; then
        print_warning "Config backup preserved at: $CONFIG_BACKUP_DIR"
    fi
}

trap cleanup_backup EXIT

clean_build_artifacts() {
    print_info "Cleaning build artifacts..."

    rm -f "$REPO_DIR/$APP_NAME"

    if command -v go &> /dev/null; then
        (cd "$REPO_DIR" && go clean 2>/dev/null) || true
        if [ "$DEEP_CLEAN" = true ]; then
            print_info "Wiping Go build cache (deep clean)..."
            go clean -cache -testcache 2>/dev/null || true
        fi
    fi
}

build_fresh() {
    print_info "Building fresh binary..."
    local flags=""
    if [ "$VERBOSE" = true ]; then
        flags="--verbose"
    fi
    bash "$SCRIPT_DIR/install.sh" --build-only $flags
}

purge_existing() {
    print_info "Purging all existing installations, config, and caches..."
    # Config dir is wiped even with --keep-config (generated init scripts must
    # not survive); the user's own files were backed up and will be restored.
    local flags="--purge --force"
    if [ "$VERBOSE" = true ]; then
        flags="$flags --verbose"
    fi
    bash "$SCRIPT_DIR/uninstall.sh" $flags
}

install_fresh() {
    print_info "Installing fresh build..."
    local flags="--$INSTALL_MODE --skip-build"
    if [ "$VERBOSE" = true ]; then
        flags="$flags --verbose"
    fi
    bash "$SCRIPT_DIR/install.sh" $flags
}

file_sha256() {
    local file="$1"
    if command -v shasum &> /dev/null; then
        shasum -a 256 "$file" | awk '{print $1}'
    elif command -v sha256sum &> /dev/null; then
        sha256sum "$file" | awk '{print $1}'
    else
        echo "(no sha256 tool available)"
    fi
}

verify_install() {
    print_info "Verifying installation..."

    local expected=""
    if [ "$OS_TYPE" = "Darwin" ]; then
        if [ "$INSTALL_MODE" = "global" ]; then
            expected="/Applications/${MACOS_APP_NAME}.app/Contents/MacOS/$APP_NAME"
        else
            expected="$HOME/Applications/${MACOS_APP_NAME}.app/Contents/MacOS/$APP_NAME"
        fi
    else
        if [ "$INSTALL_MODE" = "global" ]; then
            expected="/usr/local/bin/$APP_NAME"
        else
            expected="$HOME/.local/bin/$APP_NAME"
        fi
    fi

    if [ ! -x "$expected" ]; then
        print_error "Expected binary not found at: $expected"
        exit 1
    fi
    print_success "Installed binary: $expected"
    print_success "sha256: $(file_sha256 "$expected")"

    # Check what PATH resolves to and flag shadowing by stray copies
    hash -r 2>/dev/null || true
    local resolved
    resolved="$(command -v "$APP_NAME" 2>/dev/null || true)"

    if [ -z "$resolved" ]; then
        if [ "$OS_TYPE" = "Darwin" ] && [ "$INSTALL_MODE" = "user" ]; then
            print_warning "$APP_NAME is not in PATH. Add ~/.local/bin to PATH to use it from the terminal."
        else
            print_warning "$APP_NAME is not in PATH for this shell."
        fi
        return 0
    fi

    # Resolve symlinks so the macOS CLI symlink counts as the expected binary
    local resolved_target="$resolved"
    if [ -L "$resolved" ]; then
        resolved_target="$(readlink "$resolved")"
    fi

    if [ "$resolved" = "$expected" ] || [ "$resolved_target" = "$expected" ]; then
        print_success "PATH resolves to the fresh install: $resolved"
    else
        print_warning "PATH resolves $APP_NAME to: $resolved"
        print_warning "Expected the fresh install at: $expected"
        print_warning "A stale copy is shadowing the new install. Remove it or fix PATH order."
        exit 1
    fi
}

print_completion() {
    echo ""
    echo -e "${GREEN}============================================${NC}"
    echo -e "${GREEN}     Clean Install Complete!${NC}"
    echo -e "${GREEN}============================================${NC}"
    echo ""
    echo "This install carried nothing over from previous versions."
    if [ "$KEEP_CONFIG" = true ]; then
        echo "Your config.toml and user-init.fish were preserved."
    else
        echo "All previous config was removed; defaults will be regenerated on first run."
    fi
    echo ""
    if pgrep -x "$APP_NAME" &> /dev/null; then
        print_warning "A Raven Terminal window from before the install is still open."
        print_warning "Close and reopen it to use the new version."
    fi
}

main() {
    print_header
    parse_args "$@"

    echo "Install mode:  $INSTALL_MODE"
    echo "Keep config:   $KEEP_CONFIG"
    echo "Deep clean:    $DEEP_CLEAN"
    echo "Repository:    $REPO_DIR"
    echo ""

    backup_config
    clean_build_artifacts
    build_fresh
    purge_existing
    install_fresh
    restore_config
    verify_install
    print_completion
}

main "$@"
