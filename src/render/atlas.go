package render

import (
	"image"

	"github.com/go-gl/gl/v4.1-core/gl"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Dynamic glyph atlas. Glyphs are rasterized on demand at their natural ink
// bounds (with bearings recorded in the Glyph, so wide glyphs like Nerd Font
// icons are never clipped to the cell box) and shelf-packed into a single RED
// texture that doubles in size and re-rasterizes when full.
//
// Two glyph classes get special treatment, following Ghostty:
//   - cell-exact glyphs (box drawing, powerline separators) are rasterized
//     clipped to the cell box so they tile seamlessly across cells;
//   - icon/symbol glyphs are placed by the draw-time constraint in glyphQuad
//     (scaled to fit their cell span and centered).

const (
	atlasInitialSize = 1024
	atlasMaxSize     = 8192
	// glyphPad is the transparent border around each atlas entry: it stops
	// LINEAR filtering from bleeding neighboring glyphs in, and absorbs the
	// occasional pixel of hinting drift outside the reported glyph bounds.
	glyphPad = 2
)

// initAtlas allocates an empty RED atlas texture (and CPU mirror) of size×size.
func (r *Renderer) initAtlas(size int) {
	r.atlasSize = size
	r.atlasPix = make([]byte, size*size)
	r.shelfX, r.shelfY, r.shelfH = 0, 0, 0

	gl.GenTextures(1, &r.fontAtlas)
	gl.BindTexture(gl.TEXTURE_2D, r.fontAtlas)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RED, int32(size), int32(size), 0,
		gl.RED, gl.UNSIGNED_BYTE, gl.Ptr(r.atlasPix))
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.BindTexture(gl.TEXTURE_2D, 0)
}

// allocRegion reserves a w×h region using shelf packing, growing the atlas
// (which re-rasterizes the cache) when full. Returns the region's top-left.
func (r *Renderer) allocRegion(w, h int) (int, int, bool) {
	for {
		if w <= r.atlasSize && h <= r.atlasSize {
			if r.shelfX+w > r.atlasSize {
				r.shelfY += r.shelfH
				r.shelfX, r.shelfH = 0, 0
			}
			if r.shelfY+h <= r.atlasSize {
				px, py := r.shelfX, r.shelfY
				r.shelfX += w
				if h > r.shelfH {
					r.shelfH = h
				}
				return px, py, true
			}
		}
		if !r.growAtlas() {
			return 0, 0, false
		}
	}
}

// uploadRegion copies the alpha channel of img into the CPU mirror and uploads
// just that subregion of the texture.
func (r *Renderer) uploadRegion(px, py int, img *image.RGBA) {
	w := img.Rect.Dx()
	h := img.Rect.Dy()
	sub := make([]byte, w*h)
	for yy := range h {
		for xx := range w {
			a := img.Pix[(yy*w+xx)*4+3]
			sub[yy*w+xx] = a
			r.atlasPix[(py+yy)*r.atlasSize+(px+xx)] = a
		}
	}
	gl.BindTexture(gl.TEXTURE_2D, r.fontAtlas)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, int32(px), int32(py), int32(w), int32(h),
		gl.RED, gl.UNSIGNED_BYTE, gl.Ptr(sub))
	gl.BindTexture(gl.TEXTURE_2D, 0)
}

// ensureGlyph returns the atlas entry for char, rasterizing it on first use
// from the first font in the fallback chain that covers it. Returns ok=false
// when no font has a glyph for char (so callers can fall back further).
func (r *Renderer) ensureGlyph(char rune) (Glyph, bool) {
	if g, ok := r.glyphs[char]; ok {
		return g, true
	}
	if r.glyphMisses[char] {
		return Glyph{}, false
	}
	face := r.faceFor(char)
	if face == nil {
		r.glyphMisses[char] = true
		return Glyph{}, false
	}
	g, ok := r.rasterizeGlyph(face, char)
	if !ok {
		r.glyphMisses[char] = true
		return Glyph{}, false
	}
	r.glyphs[char] = g
	return g, true
}

// rasterizeGlyph rasterizes char from face into the atlas. Cell-exact glyphs
// (box drawing, powerline) are clipped to the cell box; everything else is
// rasterized at its natural ink bounds with bearings recorded.
func (r *Renderer) rasterizeGlyph(face font.Face, char rune) (Glyph, bool) {
	if isCellExact(char) {
		return r.rasterizeCellGlyph(face, char)
	}

	bounds, _, ok := face.GlyphBounds(char)
	if !ok {
		return Glyph{}, false
	}
	w := (bounds.Max.X - bounds.Min.X).Ceil() + 2*glyphPad
	h := (bounds.Max.Y - bounds.Min.Y).Ceil() + 2*glyphPad
	if w <= 2*glyphPad || h <= 2*glyphPad {
		// No ink (e.g. a space from a fallback font): cache a zero-size
		// glyph so draw sites skip it without re-probing the font chain.
		return Glyph{}, true
	}

	px, py, ok := r.allocRegion(w, h)
	if !ok {
		return Glyph{}, false
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	drawer := &font.Drawer{Dst: img, Src: image.White, Face: face}
	// Position the dot so the ink lands inside the padded bitmap.
	drawer.Dot = fixed.Point26_6{
		X: fixed.I(glyphPad) - bounds.Min.X,
		Y: fixed.I(glyphPad) - bounds.Min.Y,
	}
	drawer.DrawString(string(char))
	r.uploadRegion(px, py, img)

	return Glyph{
		X:           float32(px) / float32(r.atlasSize),
		Y:           float32(py) / float32(r.atlasSize),
		Width:       float32(w) / float32(r.atlasSize),
		Height:      float32(h) / float32(r.atlasSize),
		PixelWidth:  w,
		PixelHeight: h,
		// Bitmap placement relative to the pen position (x: from the cell
		// origin, y: from the baseline; negative y is above the baseline).
		OffsetX: float32(bounds.Min.X)/64 - glyphPad,
		OffsetY: float32(bounds.Min.Y)/64 - glyphPad,
	}, true
}

// rasterizeCellGlyph rasterizes char clipped to exactly one cell, anchored at
// the cell box. Box-drawing and powerline glyphs must align to the cell grid
// to connect seamlessly with neighbors and background rectangles.
func (r *Renderer) rasterizeCellGlyph(face font.Face, char rune) (Glyph, bool) {
	if _, ok := face.GlyphAdvance(char); !ok {
		return Glyph{}, false
	}
	w, h := int(r.cellWidth), int(r.cellHeight)
	if w <= 0 || h <= 0 {
		return Glyph{}, false
	}

	px, py, ok := r.allocRegion(w+2*glyphPad, h+2*glyphPad)
	if !ok {
		return Glyph{}, false
	}

	// Draw into a padded bitmap but expose only the inner cell-sized region,
	// so the transparent border never shows inside the cell.
	img := image.NewRGBA(image.Rect(0, 0, w+2*glyphPad, h+2*glyphPad))
	clipped := img.SubImage(image.Rect(glyphPad, glyphPad, glyphPad+w, glyphPad+h)).(*image.RGBA)
	drawer := &font.Drawer{Dst: clipped, Src: image.White, Face: face}
	drawer.Dot = fixed.P(glyphPad, glyphPad+r.atlasAscent)
	drawer.DrawString(string(char))
	r.uploadRegion(px, py, img)

	return Glyph{
		X:           float32(px+glyphPad) / float32(r.atlasSize),
		Y:           float32(py+glyphPad) / float32(r.atlasSize),
		Width:       float32(w) / float32(r.atlasSize),
		Height:      float32(h) / float32(r.atlasSize),
		PixelWidth:  w,
		PixelHeight: h,
		OffsetX:     0,
		OffsetY:     -float32(r.atlasAscent),
	}, true
}

// growAtlas doubles the atlas and re-rasterizes the cached glyphs. Returns
// false if the atlas is already at the maximum size.
func (r *Renderer) growAtlas() bool {
	if r.atlasSize >= atlasMaxSize {
		return false
	}
	r.atlasGen++ // all previously recorded UVs become invalid
	chars := make([]rune, 0, len(r.glyphs))
	for c := range r.glyphs {
		chars = append(chars, c)
	}
	if r.fontAtlas != 0 {
		gl.DeleteTextures(1, &r.fontAtlas)
		r.fontAtlas = 0
	}
	r.glyphs = make(map[rune]Glyph)
	r.asciiCache = [128]asciiGlyphSlot{} // UVs change on repack
	r.initAtlas(r.atlasSize * 2)
	for _, c := range chars {
		r.ensureGlyph(c)
	}
	return true
}
