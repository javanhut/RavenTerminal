package render

import (
	"slices"
	"time"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/images"
	"github.com/javanhut/RavenTerminal/src/parser"
)

// imgQuad is a textured quad: screen rect (x0,y0)-(x1,y1) and texture
// coordinates (u0,v0)-(u1,v1), both top-left origin.
type imgQuad struct {
	x0, y0, x1, y1 float32
	u0, v0, u1, v1 float32
}

// zBelowBackgrounds is kitty's threshold: placements with a lower z-index are
// drawn under non-default cell backgrounds, not just under text.
const zBelowBackgrounds = -(1 << 30)

// srcRect resolves a placement's source rectangle against the image size.
func srcRect(p *images.Placement, img *images.Image) (sx, sy, sw, sh int) {
	sx, sy = min(p.SrcX, img.Width), min(p.SrcY, img.Height)
	sw, sh = p.SrcW, p.SrcH
	if sw <= 0 || sx+sw > img.Width {
		sw = img.Width - sx
	}
	if sh <= 0 || sy+sh > img.Height {
		sh = img.Height - sy
	}
	return
}

// placementQuad maps a placement to pane-relative pixels. A placement whose
// cell box is exactly what its pixel size needs at the terminal's cell size
// (termCW x termCH) is drawn at natural size, scaled with the renderer's cell
// size; one given an explicit c/r box is stretched to fill it.
func placementQuad(p *images.Placement, img *images.Image, viewTopAbs int, cw, ch float32, termCW, termCH int) (imgQuad, bool) {
	sx, sy, sw, sh := srcRect(p, img)
	if sw <= 0 || sh <= 0 || p.Cols <= 0 || p.Rows <= 0 || cw <= 0 || ch <= 0 {
		return imgQuad{}, false
	}
	scaleX, scaleY := float32(1), float32(1)
	if termCW > 0 && termCH > 0 {
		scaleX, scaleY = cw/float32(termCW), ch/float32(termCH)
	}
	ox, oy := float32(p.OffsetX)*scaleX, float32(p.OffsetY)*scaleY
	natural := termCW > 0 && termCH > 0 &&
		p.Cols == max((sw+p.OffsetX+termCW-1)/termCW, 1) &&
		p.Rows == max((sh+p.OffsetY+termCH-1)/termCH, 1)
	var w, h float32
	if natural {
		w, h = float32(sw)*scaleX, float32(sh)*scaleY
	} else {
		w, h = float32(p.Cols)*cw-ox, float32(p.Rows)*ch-oy
	}
	if w <= 0 || h <= 0 {
		return imgQuad{}, false
	}
	x0 := float32(p.AnchorCol)*cw + ox
	y0 := float32(p.AnchorAbsRow-viewTopAbs)*ch + oy
	iw, ih := float32(img.Width), float32(img.Height)
	return imgQuad{
		x0: x0, y0: y0, x1: x0 + w, y1: y0 + h,
		u0: float32(sx) / iw, v0: float32(sy) / ih,
		u1: float32(sx+sw) / iw, v1: float32(sy+sh) / ih,
	}, true
}

// clipQuad clips a quad to [0,w]x[0,h], trimming texture coordinates in
// proportion. It reports false when nothing is left.
func clipQuad(q imgQuad, w, h float32) (imgQuad, bool) {
	if q.x1 <= 0 || q.y1 <= 0 || q.x0 >= w || q.y0 >= h || q.x1 <= q.x0 || q.y1 <= q.y0 {
		return q, false
	}
	du := (q.u1 - q.u0) / (q.x1 - q.x0)
	dv := (q.v1 - q.v0) / (q.y1 - q.y0)
	if q.x0 < 0 {
		q.u0 -= q.x0 * du
		q.x0 = 0
	}
	if q.y0 < 0 {
		q.v0 -= q.y0 * dv
		q.y0 = 0
	}
	if q.x1 > w {
		q.u1 -= (q.x1 - w) * du
		q.x1 = w
	}
	if q.y1 > h {
		q.v1 -= (q.y1 - h) * dv
		q.y1 = h
	}
	return q, true
}

// splitLayers sorts real placements into the three draw layers, each in
// ascending z order: below cell backgrounds, between backgrounds and text,
// and above text.
func splitLayers(ps []*images.Placement, below, under, over []*images.Placement) ([]*images.Placement, []*images.Placement, []*images.Placement) {
	below, under, over = below[:0], under[:0], over[:0]
	for _, p := range ps {
		switch {
		case p.Virtual:
		case p.ZIndex < zBelowBackgrounds:
			below = append(below, p)
		case p.ZIndex < 0:
			under = append(under, p)
		default:
			over = append(over, p)
		}
	}
	byZ := func(a, b *images.Placement) int { return a.ZIndex - b.ZIndex }
	slices.SortStableFunc(below, byZ)
	slices.SortStableFunc(under, byZ)
	slices.SortStableFunc(over, byZ)
	return below, under, over
}

// --- kitty unicode placeholders ---

const placeholderRune = 0x10EEEE

// placeholderDiacritics is kitty's rowcolumn-diacritics table: the n-th entry
// encodes the number n in a placeholder's row, column or id-MSB slot.
var placeholderDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A,
	0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357, 0x035B, 0x0363, 0x0364, 0x0365,
	0x0366, 0x0367, 0x0368, 0x0369, 0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F,
	0x0483, 0x0484, 0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1, 0x05A8, 0x05A9,
	0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611, 0x0612, 0x0613, 0x0614, 0x0615,
	0x0616, 0x0617, 0x0657, 0x0658, 0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6,
	0x06D7, 0x06D8, 0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733, 0x0735, 0x0736,
	0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743, 0x0745, 0x0747, 0x0749, 0x074A,
	0x07EB, 0x07EC, 0x07ED, 0x07EE, 0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817,
	0x0818, 0x0819, 0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C, 0x082D, 0x0951,
	0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87, 0x135D, 0x135E, 0x135F, 0x17DD,
	0x193A, 0x1A17, 0x1A75, 0x1A76, 0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C,
	0x1B6B, 0x1B6D, 0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4, 0x1DC5, 0x1DC6,
	0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1, 0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5,
	0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9, 0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF,
	0x1DE0, 0x1DE1, 0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7, 0x20E9, 0x20F0,
	0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2, 0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6,
	0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA, 0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0,
	0x2DF1, 0x2DF2, 0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D, 0xA6F0, 0xA6F1,
	0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5, 0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9,
	0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED, 0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2,
	0xAAB3, 0xAAB7, 0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23,
	0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187, 0x1D188, 0x1D189,
	0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243, 0x1D244,
}

func diacriticIndex(r rune) int {
	if i, ok := slices.BinarySearch(placeholderDiacritics, r); ok {
		return i
	}
	return -1
}

// placeholderInfo is one decoded placeholder cell.
type placeholderInfo struct {
	id, pid  uint32
	row, col int
	msb      uint32
	valid    bool
}

func colorID(c grid.Color) uint32 {
	switch c.Type {
	case grid.ColorRGB:
		return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
	case grid.ColorIndexed:
		return uint32(c.Index)
	}
	return 0
}

// decodePlaceholder reads a U+10EEEE cell. Missing diacritics are inherited
// from the placeholder to its left (prev) per the kitty spec.
func decodePlaceholder(c grid.Cell, prev placeholderInfo) placeholderInfo {
	low := colorID(c.Fg)
	info := placeholderInfo{pid: colorID(c.UnderlineColor), valid: true}
	d := make([]int, 0, 3)
	for _, r := range c.Combining {
		i := diacriticIndex(r)
		if i < 0 || len(d) == 3 {
			break
		}
		d = append(d, i)
	}
	cont := prev.valid && low == prev.id&0xFFFFFF && info.pid == prev.pid
	switch len(d) {
	case 0:
		if cont {
			info.row, info.col, info.msb = prev.row, prev.col+1, prev.msb
		}
	case 1:
		info.row = d[0]
		if cont && prev.row == d[0] {
			info.col, info.msb = prev.col+1, prev.msb
		}
	default:
		info.row, info.col = d[0], d[1]
		if len(d) == 3 {
			info.msb = uint32(d[2])
		} else if cont && prev.row == d[0] && prev.col == d[1]-1 {
			info.msb = prev.msb
		}
	}
	info.id = low | info.msb<<24
	return info
}

// placeholderTile returns the cell-relative quad of image tile (row, col) of a
// virtual placement: the image (or its source rect) is fitted into the
// placement's cell box preserving aspect ratio and centred.
func placeholderTile(p *images.Placement, img *images.Image, row, col int, cw, ch float32) (imgQuad, bool) {
	sx, sy, sw, sh := srcRect(p, img)
	if sw <= 0 || sh <= 0 || cw <= 0 || ch <= 0 {
		return imgQuad{}, false
	}
	cols, rows := p.Cols, p.Rows
	if cols <= 0 {
		cols = max(int((float32(sw)+cw-1)/cw), 1)
	}
	if rows <= 0 {
		rows = max(int((float32(sh)+ch-1)/ch), 1)
	}
	if row < 0 || col < 0 || row >= rows || col >= cols {
		return imgQuad{}, false
	}
	boxW, boxH := float32(cols)*cw, float32(rows)*ch
	scale := min(boxW/float32(sw), boxH/float32(sh))
	dw, dh := float32(sw)*scale, float32(sh)*scale
	full := imgQuad{
		x0: (boxW - dw) / 2, y0: (boxH - dh) / 2,
		u0: float32(sx) / float32(img.Width), v0: float32(sy) / float32(img.Height),
		u1: float32(sx+sw) / float32(img.Width), v1: float32(sy+sh) / float32(img.Height),
	}
	full.x1, full.y1 = full.x0+dw, full.y0+dh
	cx, cy := float32(col)*cw, float32(row)*ch
	full.x0, full.x1, full.y0, full.y1 = full.x0-cx, full.x1-cx, full.y0-cy, full.y1-cy
	return clipQuad(full, cw, ch)
}

// placeholderDraw is a placeholder cell recorded during the grid pass.
type placeholderDraw struct {
	x, y float32
	info placeholderInfo
}

// --- texture cache ---

type texEntry struct {
	tex   uint32
	owner *images.Store
	size  int
	frame uint64
	used  time.Time
}

// texCache keeps one GL texture per image, evicting least-recently-used
// textures past a byte budget and freeing those whose image was deleted.
type texCache struct {
	entries map[*images.Image]*texEntry
	bytes   int
	budget  int
	frame   uint64
	upload  func(*images.Image) uint32
	free    func(uint32)
}

func newTexCache(budget int, upload func(*images.Image) uint32, free func(uint32)) *texCache {
	return &texCache{entries: map[*images.Image]*texEntry{}, budget: budget, upload: upload, free: free}
}

func (c *texCache) beginFrame() { c.frame++ }

func (c *texCache) get(img *images.Image, owner *images.Store) uint32 {
	if e, ok := c.entries[img]; ok {
		e.frame, e.used = c.frame, time.Now()
		return e.tex
	}
	e := &texEntry{tex: c.upload(img), owner: owner, size: len(img.Pixels), frame: c.frame, used: time.Now()}
	c.entries[img] = e
	c.bytes += e.size
	c.evict()
	return e.tex
}

func (c *texCache) drop(img *images.Image, e *texEntry) {
	c.free(e.tex)
	c.bytes -= e.size
	delete(c.entries, img)
}

func (c *texCache) evict() {
	for c.bytes > c.budget {
		var victim *images.Image
		var ve *texEntry
		for img, e := range c.entries {
			if e.frame != c.frame && (ve == nil || e.frame < ve.frame) {
				victim, ve = img, e
			}
		}
		if ve == nil {
			return
		}
		c.drop(victim, ve)
	}
}

// sweepStore frees textures of owner's images that are gone or replaced.
func (c *texCache) sweepStore(owner *images.Store, live map[images.ImageID]*images.Image) {
	for img, e := range c.entries {
		if e.owner == owner && live[img.ID] != img {
			c.drop(img, e)
		}
	}
}

// sweepIdle frees textures unused for longer than d (closed tabs, old screens).
func (c *texCache) sweepIdle(now time.Time, d time.Duration) {
	for img, e := range c.entries {
		if now.Sub(e.used) > d {
			c.drop(img, e)
		}
	}
}

func (c *texCache) freeAll() {
	for img, e := range c.entries {
		c.drop(img, e)
	}
}

// --- GL drawing ---

const imageTexBudget = 512 << 20

func uploadImageTexture(img *images.Image) uint32 {
	var tex uint32
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	if len(img.Pixels) >= 4*img.Width*img.Height && img.Width > 0 && img.Height > 0 {
		gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, int32(img.Width), int32(img.Height), 0,
			gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(img.Pixels))
	}
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	return tex
}

func freeImageTexture(tex uint32) { gl.DeleteTextures(1, &tex) }

func (r *Renderer) imageCache() *texCache {
	if r.texCache == nil {
		r.texCache = newTexCache(imageTexBudget, uploadImageTexture, freeImageTexture)
	}
	return r.texCache
}

// drawImageQuad draws a texture quad offset by (ox, oy) with the RGBA shader.
func (r *Renderer) drawImageQuad(tex uint32, q imgQuad, ox, oy float32, proj [16]float32) {
	if r.colorProgram == 0 {
		return
	}
	r.uiFlush()
	x0, y0, x1, y1 := q.x0+ox, q.y0+oy, q.x1+ox, q.y1+oy
	verts := []float32{
		x0, y0, q.u0, q.v0,
		x1, y0, q.u1, q.v0,
		x1, y1, q.u1, q.v1,
		x0, y0, q.u0, q.v0,
		x1, y1, q.u1, q.v1,
		x0, y1, q.u0, q.v1,
	}
	gl.UseProgram(r.colorProgram)
	gl.UniformMatrix4fv(r.colorProjLoc, 1, false, &proj[0])
	gl.Uniform1i(r.colorTexLoc, 0)
	gl.Uniform1f(r.colorAlphaLoc, 1)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.BindVertexArray(r.fontVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.fontVBO)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(verts)*4, gl.Ptr(verts))
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
}

// paneImages is the per-pane image state for one frame.
type paneImages struct {
	frame                    *parser.RenderFrame
	below, under, over       []*images.Placement
	offX, offY, paneW, paneH float32
}

func (r *Renderer) prepareImages(f *parser.RenderFrame, offX, offY, paneW, paneH float32) {
	pi := &r.paneImgs
	pi.frame, pi.offX, pi.offY, pi.paneW, pi.paneH = f, offX, offY, paneW, paneH
	pi.below, pi.under, pi.over = splitLayers(f.Placements, pi.below, pi.under, pi.over)
	if f.ImageStore != nil {
		r.imageCache().sweepStore(f.ImageStore, f.Images)
	}
}

func (r *Renderer) drawPlacements(layer []*images.Placement, proj [16]float32) {
	pi := &r.paneImgs
	if pi.frame == nil {
		return
	}
	for _, p := range layer {
		img := pi.frame.Images[p.ImageID]
		if img == nil {
			continue
		}
		q, ok := placementQuad(p, img, pi.frame.ViewTopAbs, r.cellWidth, r.cellHeight, pi.frame.CellW, pi.frame.CellH)
		if !ok {
			continue
		}
		if q, ok = clipQuad(q, pi.paneW, pi.paneH); ok {
			r.drawImageQuad(r.imageCache().get(img, pi.frame.ImageStore), q, pi.offX, pi.offY, proj)
		}
	}
}

func (r *Renderer) drawPlaceholders(proj [16]float32) {
	pi := &r.paneImgs
	if pi.frame == nil || len(r.placeholders) == 0 {
		return
	}
	for _, ph := range r.placeholders {
		var p *images.Placement
		for i := len(pi.frame.Virtuals) - 1; i >= 0; i-- {
			v := pi.frame.Virtuals[i]
			if uint32(v.ImageID) == ph.info.id && (ph.info.pid == 0 || v.PlacementID == ph.info.pid) {
				p = v
				break
			}
		}
		if p == nil {
			continue
		}
		img := pi.frame.Images[p.ImageID]
		if img == nil {
			continue
		}
		if q, ok := placeholderTile(p, img, ph.info.row, ph.info.col, r.cellWidth, r.cellHeight); ok {
			r.drawImageQuad(r.imageCache().get(img, pi.frame.ImageStore), q, ph.x, ph.y, proj)
		}
	}
}
