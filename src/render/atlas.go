package render

import (
	"github.com/go-gl/gl/v4.1-core/gl"
)

// Glyph atlas: fixed-size pages, each shelf-packed and mirrored on the CPU,
// uploaded as the layers of one GL texture array. Adding a page re-uploads the
// existing pages from their mirrors (no glyph is re-rasterized and no UV
// moves). When every page is full the least recently drawn page is recycled
// and the caches drop the glyphs that lived on it, so a full atlas never turns
// into permanent '?' misses. Mono glyphs use a RED atlas, color emoji an RGBA
// one; both are sampled by the same batch shader, so text and emoji share one
// draw call.

const (
	monoPageSize  = 2048
	monoMaxPages  = 4
	colorPageSize = 1024
	colorMaxPages = 4
	// glyphPad is the transparent border around each atlas entry: it stops
	// LINEAR filtering from bleeding neighboring glyphs in, and absorbs the
	// occasional pixel of hinting drift outside the reported glyph bounds.
	glyphPad = 2
)

type atlasPage struct {
	pix                    []byte
	shelfX, shelfY, shelfH int
	lastUse                uint64
	dirtyY0, dirtyY1       int // rows awaiting upload; empty when y0 >= y1
}

type glyphAtlas struct {
	size, bpp, maxPages int
	pages               []*atlasPage
	clock               uint64
	stale               bool            // a page drawn from since the last tick was recycled
	onEvict             func(layer int) // drop cache entries living on layer

	tex       uint32
	texLayers int
}

func newGlyphAtlas(size, bpp, maxPages int) *glyphAtlas {
	return &glyphAtlas{size: size, bpp: bpp, maxPages: max(1, maxPages), clock: 1}
}

// shelf reserves w×h on the page, or reports false when it does not fit.
func (p *atlasPage) shelf(w, h, size int) (int, int, bool) {
	x, y, sh := p.shelfX, p.shelfY, p.shelfH
	if x+w > size {
		y += sh
		x, sh = 0, 0
	}
	if y+h > size {
		return 0, 0, false
	}
	p.shelfX, p.shelfY, p.shelfH = x+w, y, max(sh, h)
	return x, y, true
}

// alloc reserves a w×h region, adding a page or recycling the least recently
// used one when needed. Fails only for regions larger than a page.
func (a *glyphAtlas) alloc(w, h int) (layer, x, y int, ok bool) {
	if w <= 0 || h <= 0 || w > a.size || h > a.size {
		return 0, 0, 0, false
	}
	for i, p := range a.pages {
		if x, y, ok := p.shelf(w, h, a.size); ok {
			p.lastUse = a.clock
			return i, x, y, true
		}
	}
	if len(a.pages) < a.maxPages {
		a.pages = append(a.pages, &atlasPage{pix: make([]byte, a.size*a.size*a.bpp), lastUse: a.clock})
		layer = len(a.pages) - 1
		x, y, _ = a.pages[layer].shelf(w, h, a.size)
		return layer, x, y, true
	}
	for i, p := range a.pages {
		if p.lastUse < a.pages[layer].lastUse {
			layer = i
		}
	}
	p := a.pages[layer]
	if p.lastUse >= a.clock {
		a.stale = true
	}
	clear(p.pix)
	p.shelfX, p.shelfY, p.shelfH = 0, 0, 0
	p.dirtyY0, p.dirtyY1 = 0, a.size
	if a.onEvict != nil {
		a.onEvict(layer)
	}
	p.lastUse = a.clock
	x, y, _ = p.shelf(w, h, a.size)
	return layer, x, y, true
}

// write copies a tightly packed w×h bitmap into the page mirror.
func (a *glyphAtlas) write(layer, x, y, w, h int, src []byte) {
	p := a.pages[layer]
	row := w * a.bpp
	for yy := range h {
		off := ((y+yy)*a.size + x) * a.bpp
		copy(p.pix[off:off+row], src[yy*row:(yy+1)*row])
	}
	if p.dirtyY0 >= p.dirtyY1 {
		p.dirtyY0, p.dirtyY1 = y, y+h
	} else {
		p.dirtyY0, p.dirtyY1 = min(p.dirtyY0, y), max(p.dirtyY1, y+h)
	}
}

// touch marks a page as drawn from in the current frame.
func (a *glyphAtlas) touch(layer int) {
	if a != nil && layer < len(a.pages) {
		a.pages[layer].lastUse = a.clock
	}
}

// tick starts a new frame for LRU purposes.
func (a *glyphAtlas) tick() {
	if a != nil {
		a.clock++
		a.stale = false
	}
}

// evictedInFrame reports whether a page used since the last tick was recycled,
// invalidating UVs recorded since then.
func (a *glyphAtlas) evictedInFrame() bool { return a != nil && a.stale }

// sync uploads pending pixels to the GL texture array, (re)allocating it when
// pages were added, and returns the texture.
func (a *glyphAtlas) sync() uint32 {
	if a == nil || len(a.pages) == 0 {
		return 0
	}
	internal, format := int32(gl.R8), uint32(gl.RED)
	if a.bpp == 4 {
		internal, format = gl.RGBA8, gl.RGBA
	}
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	if a.tex == 0 || a.texLayers < len(a.pages) {
		if a.tex == 0 {
			gl.GenTextures(1, &a.tex)
		}
		gl.BindTexture(gl.TEXTURE_2D_ARRAY, a.tex)
		gl.TexImage3D(gl.TEXTURE_2D_ARRAY, 0, internal, int32(a.size), int32(a.size), int32(len(a.pages)), 0,
			format, gl.UNSIGNED_BYTE, nil)
		gl.TexParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
		gl.TexParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
		gl.TexParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
		gl.TexParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
		for i, p := range a.pages {
			gl.TexSubImage3D(gl.TEXTURE_2D_ARRAY, 0, 0, 0, int32(i), int32(a.size), int32(a.size), 1,
				format, gl.UNSIGNED_BYTE, gl.Ptr(p.pix))
			p.dirtyY0, p.dirtyY1 = 0, 0
		}
		a.texLayers = len(a.pages)
		return a.tex
	}
	gl.BindTexture(gl.TEXTURE_2D_ARRAY, a.tex)
	for i, p := range a.pages {
		if p.dirtyY0 >= p.dirtyY1 {
			continue
		}
		off := p.dirtyY0 * a.size * a.bpp
		gl.TexSubImage3D(gl.TEXTURE_2D_ARRAY, 0, 0, int32(p.dirtyY0), int32(i), int32(a.size), int32(p.dirtyY1-p.dirtyY0), 1,
			format, gl.UNSIGNED_BYTE, gl.Ptr(p.pix[off:]))
		p.dirtyY0, p.dirtyY1 = 0, 0
	}
	return a.tex
}

func (a *glyphAtlas) release() {
	if a != nil && a.tex != 0 {
		gl.DeleteTextures(1, &a.tex)
		a.tex, a.texLayers = 0, 0
	}
}
