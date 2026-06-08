package render

import (
	"image"

	"github.com/go-gl/gl/v4.1-core/gl"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Dynamic glyph atlas. Glyphs are rasterized on demand into fixed-size tiles
// (cell-sized) packed sequentially into a single RED texture, which doubles in
// size and re-rasterizes when full. This keeps Glyph semantics identical to the
// old static bake while eliminating the upfront cost and the fixed coverage.

const (
	atlasInitialSize = 1024
	atlasMaxSize     = 8192
)

// initAtlas allocates an empty RED atlas texture (and CPU mirror) of size×size.
func (r *Renderer) initAtlas(size int) {
	r.atlasSize = size
	r.atlasPix = make([]byte, size*size)
	r.atlasNextSlot = 0

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

func (r *Renderer) tileW() int { return int(r.cellWidth) }
func (r *Renderer) tileH() int { return int(r.cellHeight) }

func (r *Renderer) tilesPerRow() int {
	tw := r.tileW()
	if tw <= 0 {
		return 1
	}
	return r.atlasSize / tw
}

func (r *Renderer) atlasCapacity() int {
	th := r.tileH()
	if th <= 0 {
		return 0
	}
	return r.tilesPerRow() * (r.atlasSize / th)
}

// ensureGlyph returns the atlas entry for char, rasterizing it on first use.
// Returns ok=false when the font has no glyph for char (so callers can fall back).
func (r *Renderer) ensureGlyph(char rune) (Glyph, bool) {
	if g, ok := r.glyphs[char]; ok {
		return g, true
	}
	if r.face == nil {
		return Glyph{}, false
	}
	if _, has := r.face.GlyphAdvance(char); !has {
		return Glyph{}, false
	}
	if r.atlasNextSlot >= r.atlasCapacity() {
		if !r.growAtlas() {
			return Glyph{}, false
		}
	}

	tw, th := r.tileW(), r.tileH()
	cols := r.tilesPerRow()
	slot := r.atlasNextSlot
	px := (slot % cols) * tw
	py := (slot / cols) * th

	// Rasterize the glyph into a cell-sized tile (white on transparent).
	tile := image.NewRGBA(image.Rect(0, 0, tw, th))
	drawer := &font.Drawer{Dst: tile, Src: image.White, Face: r.face}
	drawer.Dot = fixed.P(0, r.atlasAscent)
	drawer.DrawString(string(char))

	// Copy the tile's alpha into the CPU mirror and upload just that subregion.
	sub := make([]byte, tw*th)
	for yy := 0; yy < th; yy++ {
		for xx := 0; xx < tw; xx++ {
			a := tile.Pix[(yy*tw+xx)*4+3]
			sub[yy*tw+xx] = a
			r.atlasPix[(py+yy)*r.atlasSize+(px+xx)] = a
		}
	}
	gl.BindTexture(gl.TEXTURE_2D, r.fontAtlas)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, int32(px), int32(py), int32(tw), int32(th),
		gl.RED, gl.UNSIGNED_BYTE, gl.Ptr(sub))
	gl.BindTexture(gl.TEXTURE_2D, 0)

	g := Glyph{
		X:           float32(px) / float32(r.atlasSize),
		Y:           float32(py) / float32(r.atlasSize),
		Width:       float32(tw) / float32(r.atlasSize),
		Height:      float32(th) / float32(r.atlasSize),
		PixelWidth:  tw,
		PixelHeight: th,
	}
	r.glyphs[char] = g
	r.atlasNextSlot++
	return g, true
}

// growAtlas doubles the atlas and re-rasterizes the cached glyphs. Returns false
// if the atlas is already at the maximum size.
func (r *Renderer) growAtlas() bool {
	if r.atlasSize >= atlasMaxSize {
		return false
	}
	chars := make([]rune, 0, len(r.glyphs))
	for c := range r.glyphs {
		chars = append(chars, c)
	}
	if r.fontAtlas != 0 {
		gl.DeleteTextures(1, &r.fontAtlas)
		r.fontAtlas = 0
	}
	r.glyphs = make(map[rune]Glyph)
	r.initAtlas(r.atlasSize * 2)
	for _, c := range chars {
		r.ensureGlyph(c)
	}
	return true
}
