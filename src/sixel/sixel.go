// Package sixel decodes DECSIXEL graphics data into an RGBA image. The decoder
// is pure (no GL/terminal dependencies) so it can be unit tested directly.
package sixel

import "github.com/javanhut/RavenTerminal/src/images"

type rgba struct{ r, g, b, a byte }

// Decode parses sixel data (the bytes between "DCS ... q" and "ST") into an
// RGBA image. params are the numeric DCS parameters (currently unused beyond
// presence). Unset pixels are transparent.
func Decode(params []int, data []byte) (*images.Image, error) {
	pal := defaultPalette()
	var rows [][]rgba // rows[y][x]
	cur := pal[0]
	x, y := 0, 0

	set := func(px, py int, c rgba) {
		for len(rows) <= py {
			rows = append(rows, nil)
		}
		row := rows[py]
		for len(row) <= px {
			row = append(row, rgba{})
		}
		row[px] = c
		rows[py] = row
	}

	// writeSixel sets the 6 vertical pixels encoded by b at column x.
	writeSixel := func(b byte) {
		bits := int(b - 0x3f)
		for i := 0; i < 6; i++ {
			if bits&(1<<uint(i)) != 0 {
				set(x, y+i, cur)
			}
		}
		x++
	}

	i := 0
	for i < len(data) {
		b := data[i]
		switch {
		case b == '#': // color introducer: #Pc[;Pu;Px;Py;Pz]
			i++
			nums, ni := readNums(data, i)
			i = ni
			if len(nums) == 0 {
				break
			}
			idx := nums[0] & 0xff
			if len(nums) >= 5 {
				switch nums[1] {
				case 2: // RGB percentages 0-100
					pal[idx] = rgba{pct(nums[2]), pct(nums[3]), pct(nums[4]), 255}
				case 1: // HLS
					pal[idx] = hlsToRGBA(nums[2], nums[3], nums[4])
				}
			}
			cur = pal[idx]
			continue
		case b == '!': // repeat: !Pn <sixel>
			i++
			nums, ni := readNums(data, i)
			i = ni
			n := 1
			if len(nums) > 0 {
				n = nums[0]
			}
			if i < len(data) && data[i] >= 0x3f && data[i] <= 0x7e {
				sb := data[i]
				i++
				for k := 0; k < n; k++ {
					writeSixel(sb)
				}
			}
			continue
		case b == '"': // raster attributes: "Pan;Pad;Ph;Pv  (ignored except to skip)
			i++
			_, ni := readNums(data, i)
			i = ni
			continue
		case b == '$': // carriage return (same 6-px band)
			x = 0
			i++
			continue
		case b == '-': // newline (next band)
			x = 0
			y += 6
			i++
			continue
		case b >= 0x3f && b <= 0x7e: // sixel data
			writeSixel(b)
			i++
			continue
		default:
			i++ // skip unknown/whitespace
		}
	}

	// Flatten into a tightly-sized RGBA buffer.
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	height := len(rows)
	if width == 0 || height == 0 {
		return &images.Image{Width: 0, Height: 0}, nil
	}
	pix := make([]byte, 4*width*height)
	for ry, row := range rows {
		for rx, c := range row {
			o := (ry*width + rx) * 4
			pix[o], pix[o+1], pix[o+2], pix[o+3] = c.r, c.g, c.b, c.a
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
			n = n*10 + int(c-'0')
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
	lf := float64(l) / 100
	sf := float64(s) / 100
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
	return rgba{byte(r * 255), byte(g * 255), byte(b * 255), 255}
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
func defaultPalette() [256]rgba {
	var p [256]rgba
	base := [16]rgba{
		{0, 0, 0, 255}, {51, 51, 204, 255}, {204, 33, 33, 255}, {51, 204, 51, 255},
		{204, 51, 204, 255}, {51, 204, 204, 255}, {204, 204, 51, 255}, {135, 135, 135, 255},
		{66, 66, 66, 255}, {84, 84, 153, 255}, {153, 66, 66, 255}, {84, 153, 84, 255},
		{153, 84, 153, 255}, {84, 153, 153, 255}, {153, 153, 84, 255}, {204, 204, 204, 255},
	}
	for i := 0; i < 16; i++ {
		p[i] = base[i]
	}
	return p
}
