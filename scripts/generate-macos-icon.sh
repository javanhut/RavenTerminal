#!/bin/bash
#
# Generate macOS ICNS icon from SVG
# Run this script on macOS to create the app icon
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_success() { echo -e "${GREEN}[OK]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error() { echo -e "${RED}[ERROR]${NC} $1"; }
print_info() { echo -e "${BLUE}[INFO]${NC} $1"; }

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"

SVG_PATH="$REPO_DIR/src/assets/raven_terminal_icon.svg"
ICONSET_DIR="/tmp/raven-terminal.iconset"
ICNS_PATH="$REPO_DIR/src/assets/raven-terminal.icns"

# Check we're on macOS
if [ "$(uname -s)" != "Darwin" ]; then
    print_error "This script must be run on macOS"
    exit 1
fi

# Check for SVG source
if [ ! -f "$SVG_PATH" ]; then
    print_error "SVG icon not found: $SVG_PATH"
    exit 1
fi

# Check for conversion tools, install if missing
CONVERTER=""
if command -v rsvg-convert &> /dev/null; then
    CONVERTER="rsvg-convert"
    print_info "Using rsvg-convert for SVG conversion"
elif command -v convert &> /dev/null; then
    CONVERTER="convert"
    print_info "Using ImageMagick for SVG conversion"
else
    print_info "No SVG conversion tool found. Installing librsvg..."
    if ! command -v brew &> /dev/null; then
        print_error "Homebrew is not installed. Please install it first:"
        echo "  /bin/bash -c \"\$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""
        exit 1
    fi

    if brew install librsvg; then
        print_success "librsvg installed"
        CONVERTER="rsvg-convert"
    else
        print_error "Failed to install librsvg"
        exit 1
    fi
fi

# Check for iconutil
if ! command -v iconutil &> /dev/null; then
    print_error "iconutil not found. This should be built into macOS."
    exit 1
fi

# Clean up any previous attempt
rm -rf "$ICONSET_DIR"
mkdir -p "$ICONSET_DIR"

print_info "Generating icon sizes..."

# Generate required icon sizes
# macOS iconset requires: 16, 32, 128, 256, 512 (plus @2x retina versions)
sizes=(16 32 128 256 512)

for size in "${sizes[@]}"; do
    retina_size=$((size * 2))

    print_info "  ${size}x${size} and ${size}x${size}@2x..."

    if [ "$CONVERTER" = "rsvg-convert" ]; then
        rsvg-convert -w $size -h $size "$SVG_PATH" -o "${ICONSET_DIR}/icon_${size}x${size}.png"
        rsvg-convert -w $retina_size -h $retina_size "$SVG_PATH" -o "${ICONSET_DIR}/icon_${size}x${size}@2x.png"
    else
        convert -background none -resize ${size}x${size} "$SVG_PATH" "${ICONSET_DIR}/icon_${size}x${size}.png"
        convert -background none -resize ${retina_size}x${retina_size} "$SVG_PATH" "${ICONSET_DIR}/icon_${size}x${size}@2x.png"
    fi
done

print_info "Creating ICNS file..."

# Create ICNS using iconutil
iconutil -c icns "$ICONSET_DIR" -o "$ICNS_PATH"

# Clean up
rm -rf "$ICONSET_DIR"

if [ -f "$ICNS_PATH" ]; then
    print_success "Created: $ICNS_PATH"
    echo ""
    echo "The icon will be automatically used during installation."
    echo "Run 'make install-local' to install with the new icon."
else
    print_error "Failed to create ICNS file"
    exit 1
fi
