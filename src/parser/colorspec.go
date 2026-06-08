package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/javanhut/RavenTerminal/src/grid"
)

// parseXColor parses an X11 color specification used by OSC 4/10/11/12.
// Supported forms: "rgb:RR/GG/BB", "rgb:RRRR/GGGG/BBBB" (1-4 hex digits per
// channel), and "#RGB"/"#RRGGBB"/"#RRRRGGGGBBBB". Returns an RGB grid.Color.
func parseXColor(spec string) (grid.Color, bool) {
	spec = strings.TrimSpace(spec)
	if strings.HasPrefix(spec, "rgb:") {
		parts := strings.Split(spec[4:], "/")
		if len(parts) != 3 {
			return grid.Color{}, false
		}
		r, ok1 := scaleHexChannel(parts[0])
		g, ok2 := scaleHexChannel(parts[1])
		b, ok3 := scaleHexChannel(parts[2])
		if !ok1 || !ok2 || !ok3 {
			return grid.Color{}, false
		}
		return grid.RGBColor(r, g, b), true
	}
	if strings.HasPrefix(spec, "#") {
		hex := spec[1:]
		n := len(hex)
		if n != 3 && n != 6 && n != 12 {
			return grid.Color{}, false
		}
		w := n / 3
		r, ok1 := scaleHexChannel(hex[0:w])
		g, ok2 := scaleHexChannel(hex[w : 2*w])
		b, ok3 := scaleHexChannel(hex[2*w : 3*w])
		if !ok1 || !ok2 || !ok3 {
			return grid.Color{}, false
		}
		return grid.RGBColor(r, g, b), true
	}
	return grid.Color{}, false
}

// scaleHexChannel parses a 1-4 hex-digit channel and scales it to 8 bits.
func scaleHexChannel(s string) (uint8, bool) {
	if len(s) == 0 || len(s) > 4 {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	// Scale from len*4 bits down to 8 bits.
	max := (uint64(1) << (uint(len(s)) * 4)) - 1
	return uint8(v * 255 / max), true
}

// formatXColor renders an RGB color as the 16-bit "rgb:RRRR/GGGG/BBBB" reply
// form used in OSC query responses (each 8-bit channel doubled to 16 bits).
func formatXColor(c grid.Color) string {
	r := uint16(c.R) * 0x101
	g := uint16(c.G) * 0x101
	b := uint16(c.B) * 0x101
	return fmt.Sprintf("rgb:%04x/%04x/%04x", r, g, b)
}
