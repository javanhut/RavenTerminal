package render

import (
	"image"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// Glyph rasterization by glyph id (so shaped output, ligatures and cluster
// marks render), with FreeType-style synthetic emboldening (outline points
// pushed outward along the corner bisectors) and oblique (outline shear) for
// families without a real bold or italic face.

type synthStyle uint8

const (
	synthBold synthStyle = 1 << iota
	synthOblique
)

const obliqueShear = 0.2126 // FreeType's 12° slant

// fontSrc is one font in the chain: the x/image face (metrics, coverage), the
// parsed outlines, and the raw data for the shaper (parsed on first use).
type fontSrc struct {
	id     int
	sf     *sfnt.Font
	face   font.Face
	data   []byte
	ttc    bool
	buf    sfnt.Buffer
	shaper shapeFace
}

func (s *fontSrc) gid(c rune) sfnt.GlyphIndex {
	if s == nil {
		return 0
	}
	g, err := s.sf.GlyphIndex(&s.buf, c)
	if err != nil {
		return 0
	}
	return g
}

// rasterMask is an 8-bit coverage bitmap; (offX, offY) is its top-left
// relative to the pen on the baseline.
type rasterMask struct {
	pix        []byte
	w, h       int
	offX, offY int
}

type fpt struct{ x, y float32 }

type fseg struct {
	op sfnt.SegmentOp
	p  [3]fpt
}

func segArgs(op sfnt.SegmentOp) int {
	switch op {
	case sfnt.SegmentOpQuadTo:
		return 2
	case sfnt.SegmentOpCubeTo:
		return 3
	}
	return 1
}

func (s *fontSrc) raster(gid sfnt.GlyphIndex, ppem fixed.Int26_6, synth synthStyle) (rasterMask, bool) {
	segs, err := s.sf.LoadGlyph(&s.buf, gid, ppem, nil)
	if err != nil {
		return rasterMask{}, false
	}
	fs := make([]fseg, len(segs))
	for i, sg := range segs {
		fs[i].op = sg.Op
		for k := range segArgs(sg.Op) {
			fs[i].p[k] = fpt{float32(sg.Args[k].X) / 64, float32(sg.Args[k].Y) / 64}
		}
	}
	if synth&synthBold != 0 {
		embolden(fs, float32(ppem)/64/48)
	}
	if synth&synthOblique != 0 {
		for i := range fs {
			for k := range segArgs(fs[i].op) {
				fs[i].p[k].x -= fs[i].p[k].y * obliqueShear
			}
		}
	}
	if len(fs) == 0 {
		return rasterMask{}, true
	}
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	for i := range fs {
		for k := range segArgs(fs[i].op) {
			p := fs[i].p[k]
			minX, maxX = min(minX, p.x), max(maxX, p.x)
			minY, maxY = min(minY, p.y), max(maxY, p.y)
		}
	}
	x0, y0 := int(math.Floor(float64(minX))), int(math.Floor(float64(minY)))
	w := int(math.Ceil(float64(maxX))) - x0
	h := int(math.Ceil(float64(maxY))) - y0
	if w <= 0 || h <= 0 {
		return rasterMask{}, true
	}
	bx, by := float32(-x0), float32(-y0)
	var rz vector.Rasterizer
	rz.Reset(w, h)
	rz.DrawOp = draw.Src
	for i := range fs {
		p := fs[i].p
		switch fs[i].op {
		case sfnt.SegmentOpMoveTo:
			rz.MoveTo(p[0].x+bx, p[0].y+by)
		case sfnt.SegmentOpLineTo:
			rz.LineTo(p[0].x+bx, p[0].y+by)
		case sfnt.SegmentOpQuadTo:
			rz.QuadTo(p[0].x+bx, p[0].y+by, p[1].x+bx, p[1].y+by)
		case sfnt.SegmentOpCubeTo:
			rz.CubeTo(p[0].x+bx, p[0].y+by, p[1].x+bx, p[1].y+by, p[2].x+bx, p[2].y+by)
		}
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	rz.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	return rasterMask{pix: mask.Pix, w: w, h: h, offX: x0, offY: y0}, true
}

// embolden moves every outline point outward by s along the bisector of its
// two edge normals (FT_Outline_EmboldenXY), widening strokes by 2s, then
// shifts the result so the left bearing and baseline stay put.
func embolden(fs []fseg, s float32) {
	type ref struct{ seg, k int }
	var contours [][]ref
	for i := range fs {
		if fs[i].op == sfnt.SegmentOpMoveTo || len(contours) == 0 {
			contours = append(contours, nil)
		}
		c := &contours[len(contours)-1]
		for k := range segArgs(fs[i].op) {
			*c = append(*c, ref{i, k})
		}
	}
	at := func(r ref) fpt { return fs[r.seg].p[r.k] }
	var area float32
	for _, c := range contours {
		for i := range c {
			p, q := at(c[i]), at(c[(i+1)%len(c)])
			area += p.x*q.y - q.x*p.y
		}
	}
	sign := float32(1)
	if area < 0 {
		sign = -1
	}
	unit := func(a, b fpt) (fpt, float32) {
		dx, dy := b.x-a.x, b.y-a.y
		l := float32(math.Hypot(float64(dx), float64(dy)))
		if l == 0 {
			return fpt{}, 0
		}
		return fpt{dx / l, dy / l}, l
	}
	for _, c := range contours {
		n := len(c)
		if n < 3 {
			continue
		}
		pts := make([]fpt, n)
		for i := range c {
			pts[i] = at(c[i])
		}
		for i := range n {
			p := pts[i]
			var in, out fpt
			var lin, lout float32
			for j := 1; j < n && lin == 0; j++ {
				in, lin = unit(pts[(i-j+n)%n], p)
			}
			for j := 1; j < n && lout == 0; j++ {
				out, lout = unit(p, pts[(i+j)%n])
			}
			np := fpt{p.x + s, p.y - s}
			if lin > 0 && lout > 0 {
				d := in.x*out.x + in.y*out.y
				if d > -0.9375 {
					// outward normals of the incoming and outgoing edges
					nx := sign * (in.y + out.y)
					ny := -sign * (in.x + out.x)
					f := s / (1 + d)
					q := float32(math.Abs(float64(out.x*in.y - out.y*in.x)))
					if l := min(lin, lout); s*q > l*(1+d) {
						f = l / q
					}
					np.x += nx * f
					np.y += ny * f
				}
			}
			fs[c[i].seg].p[c[i].k] = np
		}
	}
}
