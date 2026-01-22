package assets

import (
	_ "embed"
	"image"
	"image/draw"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

//go:embed raven_terminal_icon.svg
var iconSVG string

// RenderIconSizes renders the embedded SVG icon at multiple sizes
// Returns a slice of images suitable for GLFW SetIcon
func RenderIconSizes() []image.Image {
	sizes := []int{16, 32, 48, 64, 128, 256}
	var icons []image.Image

	for _, size := range sizes {
		if img := renderSVGToSize(iconSVG, size); img != nil {
			icons = append(icons, img)
		}
	}

	return icons
}

// RenderIcon renders the embedded SVG icon at the specified size
func RenderIcon(size int) image.Image {
	return renderSVGToSize(iconSVG, size)
}

// renderSVGToSize renders an SVG string to an RGBA image of the specified size
func renderSVGToSize(svgData string, size int) image.Image {
	icon, err := oksvg.ReadIconStream(strings.NewReader(svgData))
	if err != nil {
		return nil
	}

	// Set the target size
	icon.SetTarget(0, 0, float64(size), float64(size))

	// Create the destination image
	rgba := image.NewRGBA(image.Rect(0, 0, size, size))

	// Create a scanner/rasterizer
	scanner := rasterx.NewScannerGV(size, size, rgba, rgba.Bounds())
	rasterizer := rasterx.NewDasher(size, size, scanner)

	// Render the icon
	icon.Draw(rasterizer, 1.0)

	return rgba
}

// LoadMultiSizeIcons returns the embedded SVG rendered at multiple sizes
func LoadMultiSizeIcons() []image.Image {
	return RenderIconSizes()
}

// LoadIcon returns the embedded SVG rendered at 64x64 (standard icon size)
func LoadIcon() (image.Image, error) {
	return RenderIcon(64), nil
}

// CopyImage creates a copy of an image (utility function)
func CopyImage(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
	return dst
}
