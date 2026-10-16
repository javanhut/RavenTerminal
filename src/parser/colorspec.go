package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/javanhut/RavenTerminal/src/grid"
)

// parseXColor parses an X11 color specification used by OSC 4/10/11/12:
// "rgb:R/G/B" (1-4 hex digits per channel), "rgbi:R/G/B" (floats 0..1),
// "#RGB", "#RRGGBB", "#RRRGGGBBB", "#RRRRGGGGBBBB", and X11 color names
// (case- and space-insensitive, including grayN/greyN).
func parseXColor(spec string) (grid.Color, bool) {
	spec = strings.TrimSpace(spec)
	switch {
	case strings.HasPrefix(spec, "rgb:"):
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
	case strings.HasPrefix(spec, "rgbi:"):
		parts := strings.Split(spec[5:], "/")
		if len(parts) != 3 {
			return grid.Color{}, false
		}
		var ch [3]uint8
		for i, p := range parts {
			f, err := strconv.ParseFloat(p, 64)
			if err != nil || f < 0 || f > 1 {
				return grid.Color{}, false
			}
			ch[i] = uint8(f * 255)
		}
		return grid.RGBColor(ch[0], ch[1], ch[2]), true
	case strings.HasPrefix(spec, "#"):
		hex := spec[1:]
		n := len(hex)
		if n != 3 && n != 6 && n != 9 && n != 12 {
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
	name := strings.ToLower(strings.ReplaceAll(spec, " ", ""))
	if c, ok := x11Colors[name]; ok {
		return grid.RGBColor(c[0], c[1], c[2]), true
	}
	for _, pfx := range []string{"gray", "grey"} {
		if rest, ok := strings.CutPrefix(name, pfx); ok && rest != "" {
			n, err := strconv.Atoi(rest)
			if err != nil || n < 0 || n > 100 {
				return grid.Color{}, false
			}
			v := uint8(float64(n)*2.55 + 0.5)
			return grid.RGBColor(v, v, v), true
		}
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
	max := (uint64(1) << (uint(len(s)) * 4)) - 1
	return uint8(v * 255 / max), true
}

// formatXColor renders an RGB color as the 16-bit "rgb:RRRR/GGGG/BBBB" reply
// form used in OSC query responses (each 8-bit channel doubled to 16 bits).
func formatXColor(c grid.Color) string {
	return formatRGB([3]uint8{c.R, c.G, c.B})
}

func formatRGB(c [3]uint8) string {
	return fmt.Sprintf("rgb:%04x/%04x/%04x", uint16(c[0])*0x101, uint16(c[1])*0x101, uint16(c[2])*0x101)
}

// x11Colors is the commonly used subset of X11 rgb.txt (lowercase, no spaces).
var x11Colors = map[string][3]uint8{
	"black": {0, 0, 0}, "white": {255, 255, 255}, "red": {255, 0, 0}, "green": {0, 255, 0},
	"blue": {0, 0, 255}, "yellow": {255, 255, 0}, "cyan": {0, 255, 255}, "magenta": {255, 0, 255},
	"gray": {190, 190, 190}, "grey": {190, 190, 190}, "darkgray": {169, 169, 169}, "darkgrey": {169, 169, 169},
	"lightgray": {211, 211, 211}, "lightgrey": {211, 211, 211}, "dimgray": {105, 105, 105}, "dimgrey": {105, 105, 105},
	"slategray": {112, 128, 144}, "slategrey": {112, 128, 144}, "lightslategray": {119, 136, 153},
	"darkslategray": {47, 79, 79}, "gainsboro": {220, 220, 220}, "whitesmoke": {245, 245, 245},
	"silver": {192, 192, 192}, "snow": {255, 250, 250}, "ivory": {255, 255, 240}, "beige": {245, 245, 220},
	"linen": {250, 240, 230}, "wheat": {245, 222, 179}, "tan": {210, 180, 140}, "khaki": {240, 230, 140},
	"darkkhaki": {189, 183, 107}, "navy": {0, 0, 128}, "navyblue": {0, 0, 128}, "midnightblue": {25, 25, 112},
	"darkblue": {0, 0, 139}, "mediumblue": {0, 0, 205}, "royalblue": {65, 105, 225}, "steelblue": {70, 130, 180},
	"dodgerblue": {30, 144, 255}, "deepskyblue": {0, 191, 255}, "skyblue": {135, 206, 235},
	"lightskyblue": {135, 206, 250}, "lightblue": {173, 216, 230}, "powderblue": {176, 224, 230},
	"cornflowerblue": {100, 149, 237}, "cadetblue": {95, 158, 160}, "slateblue": {106, 90, 205},
	"darkslateblue": {72, 61, 139}, "lightsteelblue": {176, 196, 222}, "aliceblue": {240, 248, 255},
	"turquoise": {64, 224, 208}, "darkturquoise": {0, 206, 209}, "mediumturquoise": {72, 209, 204},
	"aquamarine": {127, 255, 212}, "darkcyan": {0, 139, 139}, "lightcyan": {224, 255, 255}, "teal": {0, 128, 128},
	"darkgreen": {0, 100, 0}, "forestgreen": {34, 139, 34}, "seagreen": {46, 139, 87}, "mediumseagreen": {60, 179, 113},
	"lightseagreen": {32, 178, 170}, "darkseagreen": {143, 188, 143}, "palegreen": {152, 251, 152},
	"springgreen": {0, 255, 127}, "lawngreen": {124, 252, 0}, "chartreuse": {127, 255, 0},
	"greenyellow": {173, 255, 47}, "limegreen": {50, 205, 50}, "lime": {0, 255, 0}, "yellowgreen": {154, 205, 50},
	"olivedrab": {107, 142, 35}, "darkolivegreen": {85, 107, 47}, "olive": {128, 128, 0}, "lightgreen": {144, 238, 144},
	"lightyellow": {255, 255, 224}, "lightgoldenrodyellow": {250, 250, 210}, "gold": {255, 215, 0},
	"goldenrod": {218, 165, 32}, "darkgoldenrod": {184, 134, 11}, "palegoldenrod": {238, 232, 170},
	"orange": {255, 165, 0}, "darkorange": {255, 140, 0}, "coral": {255, 127, 80}, "lightcoral": {240, 128, 128},
	"tomato": {255, 99, 71}, "orangered": {255, 69, 0}, "salmon": {250, 128, 114}, "darksalmon": {233, 150, 122},
	"lightsalmon": {255, 160, 122}, "indianred": {205, 92, 92}, "firebrick": {178, 34, 34}, "brown": {165, 42, 42},
	"darkred": {139, 0, 0}, "maroon": {176, 48, 96}, "crimson": {220, 20, 60}, "pink": {255, 192, 203},
	"lightpink": {255, 182, 193}, "hotpink": {255, 105, 180}, "deeppink": {255, 20, 147},
	"palevioletred": {219, 112, 147}, "mediumvioletred": {199, 21, 133}, "violetred": {208, 32, 144},
	"violet": {238, 130, 238}, "plum": {221, 160, 221}, "orchid": {218, 112, 214}, "mediumorchid": {186, 85, 211},
	"darkorchid": {153, 50, 204}, "darkviolet": {148, 0, 211}, "blueviolet": {138, 43, 226}, "purple": {160, 32, 240},
	"mediumpurple": {147, 112, 219}, "thistle": {216, 191, 216}, "lavender": {230, 230, 250},
	"lavenderblush": {255, 240, 245}, "darkmagenta": {139, 0, 139}, "fuchsia": {255, 0, 255}, "indigo": {75, 0, 130},
	"sienna": {160, 82, 45}, "saddlebrown": {139, 69, 19}, "chocolate": {210, 105, 30}, "peru": {205, 133, 63},
	"sandybrown": {244, 164, 96}, "burlywood": {222, 184, 135}, "rosybrown": {188, 143, 143},
	"moccasin": {255, 228, 181}, "navajowhite": {255, 222, 173}, "peachpuff": {255, 218, 185},
	"mistyrose": {255, 228, 225}, "bisque": {255, 228, 196}, "blanchedalmond": {255, 235, 205},
	"papayawhip": {255, 239, 213}, "cornsilk": {255, 248, 220}, "lemonchiffon": {255, 250, 205},
	"seashell": {255, 245, 238}, "oldlace": {253, 245, 230}, "floralwhite": {255, 250, 240},
	"ghostwhite": {248, 248, 255}, "honeydew": {240, 255, 240}, "mintcream": {245, 255, 250},
	"azure": {240, 255, 255}, "antiquewhite": {250, 235, 215},
}
