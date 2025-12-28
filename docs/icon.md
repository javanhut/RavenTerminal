# Raven Terminal Icon

## Overview

Raven Terminal includes an embedded SVG icon (`raven_terminal_icon.svg`) that appears in the taskbar, window decorations, and application switchers.

## How It Works

The icon is embedded directly into the application binary using Go's embed directive. At startup, the SVG is rendered to multiple raster sizes (16x16, 32x32, 48x48, 64x64, 128x128, 256x256) using the oksvg and rasterx libraries. This ensures the icon displays crisply at any size the window manager requests.

## Icon Location

The source SVG file is located at:
- `assets/raven_terminal_icon.svg`

## Technical Details

The icon rendering is handled by the `assets` package:

| Function | Description |
|----------|-------------|
| `RenderIconSizes()` | Returns all icon sizes as `[]image.Image` |
| `RenderIcon(size)` | Renders the icon at a specific size |
| `LoadMultiSizeIcons()` | Alias for `RenderIconSizes()` |

## Modifying the Icon

To change the application icon:

1. Replace `assets/raven_terminal_icon.svg` with your new SVG file
2. Ensure the SVG uses standard SVG 2.0 path elements
3. Rebuild the application

### SVG Requirements

- Use standard path elements (`<path>`, `<circle>`, `<rect>`, etc.)
- Avoid complex filters or effects (not all SVG features are supported)
- Use a square viewBox for best results
- Keep the design simple for clarity at small sizes (16x16)

## Troubleshooting

**Icon not appearing:**
- Verify the SVG file exists in the assets directory
- Check that the SVG is valid and uses supported elements
- Rebuild the application after any changes

**Icon appears distorted:**
- Ensure the SVG has a square viewBox attribute
- Simplify complex paths if rendering fails
