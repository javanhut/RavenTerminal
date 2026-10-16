// Package sixel decodes DECSIXEL graphics data into an RGBA image. The decoder
// is pure (no GL/terminal dependencies) so it can be unit tested directly.
package sixel

import (
	"errors"

	"github.com/javanhut/RavenTerminal/src/images"
)

var errTooLarge = errors.New("sixel: image too large")

// Size caps: hostile repeat counts or raster attributes must not allocate
// unbounded memory.
const (
	MaxWidth  = 8192
	MaxHeight = 8192
	MaxPixels = 4096 * 4096
	// Raster attributes may pad the drawn extent up to this many pixels.
	maxRasterPad = 1 << 20
)

// NumColorRegisters is the size of the sixel color palette.
const NumColorRegisters = 256

type rgba struct{ r, g, b, a byte }

type canvas struct {
	rows  [][]byte // RGBA rows, grown on demand
	width int
}

func (c *canvas) set(x, y int, col rgba) {
	if x >= MaxWidth || y >= MaxHeight || max(x+1, c.width)*max(y+1, len(c.rows)) > MaxPixels {
		return
	}
	for len(c.rows) <= y {
		c.rows = append(c.rows, nil)
	}
	row := c.rows[y]
	if need := (x + 1) * 4; len(row) < need {
		if cap(row) >= need {
			row = row[:need]
		} else {
			nr := make([]byte, need, max(need, 2*cap(row)))
			copy(nr, row)
			row = nr
		}
		c.rows[y] = row
	}
	o := x * 4
	row[o], row[o+1], row[o+2], row[o+3] = col.r, col.g, col.b, col.a
	if x+1 > c.width {
		c.width = x + 1
	}
}

// Decode parses sixel data (the bytes between "DCS P1;P2;P3 q" and "ST") into
// an RGBA image. params are the numeric DCS parameters: P2 == 1 leaves unset
// pixels transparent, otherwise they take color register 0 (VT340 background).
func Decode(params []int, data []byte) (*images.Image, error) {
	transparent := len(params) > 1 && params[1] == 1
	pal := defaultPalette()
	var cv canvas
	cur := pal[0]
	x, y := 0, 0
	rasterW, rasterH := 0, 0
	bottom := 0 // lowest band reached by any sixel, drawn or blank

	writeSixel := func(b byte, n int) {
		bits := int(b - 0x3f)
		if bits != 0 && x < MaxWidth {
			end := min(x+n, MaxWidth)
			for i := range 6 {
				if bits&(1<<uint(i)) != 0 {
					for px := x; px < end; px++ {
						cv.set(px, y+i, cur)
					}
				}
			}
		}
		x = min(x+n, MaxWidth)
		cv.width = max(cv.width, x)
		bottom = max(bottom, y+6)
	}

	i := 0
	for i < len(data) {
		b := data[i]
		switch {
		case b == '#': // color introducer: #Pc[;Pu;Px;Py;Pz]
			nums, ni := readNums(data, i+1)
			i = ni
			if len(nums) == 0 {
				continue
			}
			idx := nums[0] % NumColorRegisters
			if len(nums) >= 5 {
				switch nums[1] {
				case 2: // RGB percentages 0-100
					pal[idx] = rgba{pct(nums[2]), pct(nums[3]), pct(nums[4]), 255}
				case 1: // HLS
					pal[idx] = hlsToRGBA(nums[2], nums[3], nums[4])
				}
			}
			cur = pal[idx]
		case b == '!': // repeat: !Pn <sixel>
			nums, ni := readNums(data, i+1)
			i = ni
			n := 1
			if len(nums) > 0 && nums[0] > 0 {
				n = nums[0]
			}
			if i < len(data) && data[i] >= 0x3f && data[i] <= 0x7e {
				writeSixel(data[i], n)
				i++
			}
		case b == '"': // raster attributes: "Pan;Pad;Ph;Pv
			nums, ni := readNums(data, i+1)
			i = ni
			if len(nums) >= 4 {
				rasterW, rasterH = min(nums[2], MaxWidth), min(nums[3], MaxHeight)
			}
		case b == '$': // graphics carriage return
			x = 0
			i++
		case b == '-': // graphics new line
			x = 0
			y += 6
			i++
		case b >= 0x3f && b <= 0x7e:
			writeSixel(b, 1)
			i++
		default:
			i++
		}
	}

	if rasterW*rasterH > maxRasterPad {
		rasterW, rasterH = min(rasterW, cv.width), min(rasterH, bottom)
	}
	width := max(cv.width, rasterW)
	height := max(len(cv.rows), rasterH)
	if width*height > MaxPixels {
		width, height = cv.width, len(cv.rows)
		if width*height > MaxPixels {
			return nil, errTooLarge
		}
	}
	if width == 0 || height == 0 {
		return &images.Image{Width: 0, Height: 0}, nil
	}
	pix := make([]byte, 4*width*height)
	if !transparent {
		bg := pal[0]
		for o := 0; o < len(pix); o += 4 {
			pix[o], pix[o+1], pix[o+2], pix[o+3] = bg.r, bg.g, bg.b, 255
		}
	}
	for ry, row := range cv.rows {
		base := ry * width * 4
		for o := 0; o+3 < len(row) && o < width*4; o += 4 {
			if row[o+3] != 0 {
				copy(pix[base+o:base+o+4], row[o:o+4])
			}
		}
	}
	return &images.Image{Width: width, Height: height, Pixels: pix}, nil
}

// readNums reads a ';'-separated run of decimal numbers starting at i.
func readNums(data []byte, i int) ([]int, int) {
	var nums []int
	n := 0
	have := false
	for i < len(data) {
		c := data[i]
		if c >= '0' && c <= '9' {
			if n < 1<<30 {
				n = n*10 + int(c-'0')
			}
			have = true
			i++
		} else if c == ';' {
			nums = append(nums, n)
			n = 0
			have = false
			i++
		} else {
			break
		}
	}
	if have || len(nums) > 0 {
		nums = append(nums, n)
	}
	return nums, i
}

func pct(v int) byte {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return byte(v * 255 / 100)
}

// hlsToRGBA converts sixel HLS (H 0-360, L/S 0-100) to RGBA.
func hlsToRGBA(h, l, s int) rgba {
	lf := float64(min(l, 100)) / 100
	sf := float64(min(s, 100)) / 100
	// Sixel hue is offset so that 0 = blue; convert to standard hue degrees.
	hf := float64((h+240)%360) / 360
	var r, g, b float64
	if sf == 0 {
		r, g, b = lf, lf, lf
	} else {
		var q float64
		if lf < 0.5 {
			q = lf * (1 + sf)
		} else {
			q = lf + sf - lf*sf
		}
		p := 2*lf - q
		r = hue(p, q, hf+1.0/3)
		g = hue(p, q, hf)
		b = hue(p, q, hf-1.0/3)
	}
	return rgba{byte(r*255 + 0.5), byte(g*255 + 0.5), byte(b*255 + 0.5), 255}
}

func hue(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 1.0/2:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	default:
		return p
	}
}

// defaultPalette returns the VT340 16-color default sixel palette.
func defaultPalette() [NumColorRegisters]rgba {
	var p [NumColorRegisters]rgba
	base := [16]rgba{
		{0, 0, 0, 255}, {51, 51, 204, 255}, {204, 33, 33, 255}, {51, 204, 51, 255},
		{204, 51, 204, 255}, {51, 204, 204, 255}, {204, 204, 51, 255}, {135, 135, 135, 255},
		{66, 66, 66, 255}, {84, 84, 153, 255}, {153, 66, 66, 255}, {84, 153, 84, 255},
		{153, 84, 153, 255}, {84, 153, 153, 255}, {153, 153, 84, 255}, {204, 204, 204, 255},
	}
	for i := range p {
		p[i] = base[i%16]
	}
	return p
}
