package render

import (
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	gtfont "github.com/go-text/typesetting/font"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
	"github.com/javanhut/RavenTerminal/src/grid"
)

const (
	styleRegular uint8 = iota
	styleBold
	styleItalic
	styleBoldItalic
)

// textState is the renderer's font, glyph-cache, atlas and shaping state.
type textState struct {
	fallbackSrcs []*fontSrc // fonts behind fallbackFaces, same order

	atlas      *glyphAtlas // RED glyph pages
	colorAtlas *glyphAtlas // RGBA color-emoji pages

	primary     *fontSrc
	srcCount    int
	styleSrcs   [4]*fontSrc
	styleSynths [4]synthStyle
	styleReady  [4]bool
	styled      [4]map[rune]Glyph // bold/italic glyphs by rune (index 0 unused)
	gidGlyphs   map[gidKey]Glyph  // shaped glyphs by (font, glyph id, synthesis)

	// Color emoji (coloremoji.go).
	colorFace     *gtfont.Face
	colorFontFile *os.File
	colorGlyphs   map[colorKey]Glyph
	colorRunes    map[rune]uint32 // rune -> color glyph id (0 = none)

	shaper     shaping.HarfbuzzShaper
	wordShapes *lruCache[[]shapedGlyph]
	clusters   *lruCache[[]placedGlyph]
	shapeCalls int
	ligatures  bool
	ligProbe   [4]uint8 // 0 unknown, 1 none, 2 has ligatures
	keyBuf     []byte
	wordRunes  []rune
	ligRow     []ligCell
	oneCell    [1]grid.Cell

	textGamma    float32
	textContrast float32
}

type gidKey struct {
	src   int
	gid   uint32
	synth synthStyle
}

// placedGlyph is a cached atlas glyph with its pen offset from the cell
// origin on the baseline (mono) or from the cell's top-left (color).
type placedGlyph struct {
	g      Glyph
	dx, dy float32
}

// ligCell holds the shaped glyphs a ligature run assigns to one cell.
type ligCell struct {
	set    bool
	glyphs []shapedGlyph
}

func (r *Renderer) ppem() fixed.Int26_6 {
	return fixed.Int26_6(0.5 + float64(r.fontSize)*r.fontDPI()*64/72)
}

func (r *Renderer) newFontSrc(f *sfnt.Font, face font.Face, data []byte, ttc bool) *fontSrc {
	r.srcCount++
	return &fontSrc{id: r.srcCount, sf: f, face: face, data: data, ttc: ttc}
}

func (r *Renderer) faceOptions() *opentype.FaceOptions {
	return &opentype.FaceOptions{Size: float64(r.fontSize), DPI: r.fontDPI(), Hinting: font.HintingFull}
}

// loadFontData parses a font, builds the faces and metrics, and resets the
// glyph caches and atlases. Glyphs are rasterized lazily; GL textures are
// (re)created on the next draw.
func (r *Renderer) loadFontData(fontData []byte) error {
	parsedFont, err := opentype.Parse(fontData)
	if err != nil {
		return fmt.Errorf("failed to parse font: %w", err)
	}
	face, err := opentype.NewFace(parsedFont, r.faceOptions())
	if err != nil {
		return fmt.Errorf("failed to create font face: %w", err)
	}
	if r.face != nil {
		r.face.Close()
	}
	r.face = face
	r.fontParsed = parsedFont
	r.clearLabels()

	metrics := face.Metrics()
	r.cellHeight = float32((metrics.Ascent + metrics.Descent).Ceil())
	advance, _ := face.GlyphAdvance('M')
	r.cellWidth = float32(advance.Ceil())
	r.atlasAscent = metrics.Ascent.Ceil()

	r.primary = r.newFontSrc(parsedFont, face, fontData, false)
	r.styleSrcs = [4]*fontSrc{}
	r.styleReady = [4]bool{}
	r.ligProbe = [4]uint8{}
	r.rebuildFallbackFaces()
	r.initAtlases(monoPageSize, monoMaxPages, colorPageSize, colorMaxPages)
	r.uiDirty = true
	return nil
}

// initAtlases replaces both atlases (releasing their textures) and empties
// every cache that points into them.
func (r *Renderer) initAtlases(monoSize, monoPages, colorSize, colorPages int) {
	r.atlas.release()
	r.colorAtlas.release()
	r.atlas = newGlyphAtlas(monoSize, 1, monoPages)
	r.atlas.onEvict = r.evictMono
	r.colorAtlas = newGlyphAtlas(colorSize, 4, colorPages)
	r.colorAtlas.onEvict = r.evictColor
	r.resetGlyphCaches()
	r.atlasGen++
}

func (r *Renderer) resetGlyphCaches() {
	r.glyphs = make(map[rune]Glyph)
	r.glyphMisses = make(map[rune]bool)
	r.asciiCache = [128]asciiGlyphSlot{}
	for i := range r.styled {
		r.styled[i] = make(map[rune]Glyph)
	}
	r.gidGlyphs = make(map[gidKey]Glyph)
	r.colorGlyphs = make(map[colorKey]Glyph)
	r.colorRunes = make(map[rune]uint32)
	r.clusters = newLRU[[]placedGlyph](clusterCacheSize)
	r.wordShapes = newShapeCache(shapeCacheSize)
}

func (r *Renderer) evictMono(layer int) {
	l := uint16(layer)
	drop := func(m map[rune]Glyph) {
		for k, g := range m {
			if g.Layer == l && g.PixelWidth > 0 {
				delete(m, k)
			}
		}
	}
	drop(r.glyphs)
	for _, m := range r.styled {
		drop(m)
	}
	for k, g := range r.gidGlyphs {
		if g.Layer == l && g.PixelWidth > 0 {
			delete(r.gidGlyphs, k)
		}
	}
	r.asciiCache = [128]asciiGlyphSlot{}
	r.clusters.reset()
	r.atlasGen++
}

func (r *Renderer) evictColor(layer int) {
	for k, g := range r.colorGlyphs {
		if g.Layer == uint16(layer) {
			delete(r.colorGlyphs, k)
		}
	}
	r.clusters.reset()
	r.atlasGen++
}

// styleFace returns the face and synthesis for a style: the family's real
// bold/italic face when embedded, otherwise the nearest face plus synthetic
// emboldening and/or oblique.
func (r *Renderer) styleFace(style uint8) (*fontSrc, synthStyle) {
	if style == styleRegular || r.primary == nil {
		return r.primary, 0
	}
	if !r.styleReady[style] {
		r.styleReady[style] = true
		bold := style == styleBold || style == styleBoldItalic
		italic := style == styleItalic || style == styleBoldItalic
		src, synth := r.primary, synthFor(style)
		if s := r.variantSrc(bold, italic); s != nil {
			src, synth = s, 0
		} else if bold && italic {
			if s := r.variantSrc(true, false); s != nil {
				src, synth = s, synthOblique
			} else if s := r.variantSrc(false, true); s != nil {
				src, synth = s, synthBold
			}
		}
		r.styleSrcs[style], r.styleSynths[style] = src, synth
	}
	return r.styleSrcs[style], r.styleSynths[style]
}

func (r *Renderer) variantSrc(bold, italic bool) *fontSrc {
	style := styleBold
	switch {
	case bold && italic:
		style = styleBoldItalic
	case italic:
		style = styleItalic
	}
	if r.styleReady[style] && r.styleSynths[style] == 0 && r.styleSrcs[style] != nil {
		return r.styleSrcs[style]
	}
	data, ok := fonts.Variant(r.currentFont, bold, italic)
	if !ok {
		return nil
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil
	}
	face, err := opentype.NewFace(f, r.faceOptions())
	if err != nil {
		return nil
	}
	return r.newFontSrc(f, face, data, false)
}

func synthFor(style uint8) synthStyle {
	var s synthStyle
	if style&styleBold != 0 {
		s |= synthBold
	}
	if style&styleItalic != 0 {
		s |= synthOblique
	}
	return s
}

func (r *Renderer) cellStyle(flags grid.CellFlags) uint8 {
	s := styleRegular
	if r.fauxBold && flags&grid.FlagBold != 0 {
		s |= styleBold
	}
	if r.fauxItalic && flags&grid.FlagItalic != 0 {
		s |= styleItalic
	}
	return s
}

// ensureGlyph returns the regular-style atlas entry for char, rasterizing it
// on first use from the first font in the fallback chain that covers it.
func (r *Renderer) ensureGlyph(char rune) (Glyph, bool) {
	if g, ok := r.glyphs[char]; ok {
		return g, true
	}
	if r.glyphMisses[char] {
		return Glyph{}, false
	}
	src := r.srcFor(char)
	if src == nil {
		r.glyphMisses[char] = true
		return Glyph{}, false
	}
	g, ok := r.rasterizeGlyph(src, src.gid(char), 0, isCellExact(char))
	if !ok {
		r.glyphMisses[char] = true
		return Glyph{}, false
	}
	r.glyphs[char] = g
	return g, true
}

// styledGlyph is resolveGlyph for a bold/italic style. Cell-exact glyphs (box
// drawing, powerline) are never restyled so borders keep tiling.
func (r *Renderer) styledGlyph(char rune, style uint8) (Glyph, bool) {
	if style == styleRegular || r.primary == nil || isCellExact(char) {
		return r.resolveGlyph(char)
	}
	if g, ok := r.styled[style][char]; ok {
		return g, true
	}
	src, synth := r.styleFace(style)
	gid := src.gid(char)
	if gid == 0 {
		if src = r.srcFor(char); src == nil {
			return r.resolveGlyph(char)
		}
		gid = src.gid(char)
		synth = synthFor(style)
	}
	g, ok := r.rasterizeGlyph(src, gid, synth, false)
	if !ok {
		return r.resolveGlyph(char)
	}
	r.styled[style][char] = g
	return g, true
}

// gidGlyph returns the atlas entry for a shaped glyph id.
func (r *Renderer) gidGlyph(src *fontSrc, gid uint32, synth synthStyle) (Glyph, bool) {
	k := gidKey{src.id, gid, synth}
	if g, ok := r.gidGlyphs[k]; ok {
		return g, true
	}
	g, ok := r.rasterizeGlyph(src, sfnt.GlyphIndex(gid), synth, false)
	if ok {
		r.gidGlyphs[k] = g
	}
	return g, ok
}

// rasterizeGlyph rasterizes a glyph into the atlas. Cell-exact glyphs are
// clipped to the cell box; everything else keeps its natural ink bounds with
// the bearings recorded in the Glyph.
func (r *Renderer) rasterizeGlyph(src *fontSrc, gid sfnt.GlyphIndex, synth synthStyle, cellExact bool) (Glyph, bool) {
	if gid == 0 {
		return Glyph{}, false
	}
	m, ok := src.raster(gid, r.ppem(), synth)
	if !ok {
		return Glyph{}, false
	}
	if cellExact {
		return r.placeCellGlyph(m)
	}
	if m.w == 0 || m.h == 0 {
		// No ink (e.g. a space): a zero-size glyph draw sites skip.
		return Glyph{}, true
	}
	w, h := m.w+2*glyphPad, m.h+2*glyphPad
	layer, px, py, ok := r.atlas.alloc(w, h)
	if !ok {
		return Glyph{}, false
	}
	buf := make([]byte, w*h)
	for y := range m.h {
		copy(buf[(y+glyphPad)*w+glyphPad:], m.pix[y*m.w:(y+1)*m.w])
	}
	r.atlas.write(layer, px, py, w, h, buf)
	size := float32(r.atlas.size)
	return Glyph{
		X:           float32(px) / size,
		Y:           float32(py) / size,
		Width:       float32(w) / size,
		Height:      float32(h) / size,
		PixelWidth:  w,
		PixelHeight: h,
		OffsetX:     float32(m.offX - glyphPad),
		OffsetY:     float32(m.offY - glyphPad),
		Layer:       uint16(layer),
	}, true
}

// placeCellGlyph clips a mask to exactly one cell anchored at the cell box,
// so box-drawing and powerline glyphs connect seamlessly with neighbors.
func (r *Renderer) placeCellGlyph(m rasterMask) (Glyph, bool) {
	cw, ch := int(r.cellWidth), int(r.cellHeight)
	if cw <= 0 || ch <= 0 {
		return Glyph{}, false
	}
	w, h := cw+2*glyphPad, ch+2*glyphPad
	layer, px, py, ok := r.atlas.alloc(w, h)
	if !ok {
		return Glyph{}, false
	}
	buf := make([]byte, w*h)
	for y := range m.h {
		cy := r.atlasAscent + m.offY + y
		if cy < 0 || cy >= ch {
			continue
		}
		for x := range m.w {
			cx := m.offX + x
			if cx < 0 || cx >= cw {
				continue
			}
			buf[(cy+glyphPad)*w+cx+glyphPad] = m.pix[y*m.w+x]
		}
	}
	r.atlas.write(layer, px, py, w, h, buf)
	size := float32(r.atlas.size)
	return Glyph{
		X:           float32(px+glyphPad) / size,
		Y:           float32(py+glyphPad) / size,
		Width:       float32(cw) / size,
		Height:      float32(ch) / size,
		PixelWidth:  cw,
		PixelHeight: ch,
		OffsetY:     -float32(r.atlasAscent),
		Layer:       uint16(layer),
	}, true
}

// Emoji presentation helpers.
func isRegionalIndicator(c rune) bool { return c >= 0x1F1E6 && c <= 0x1F1FF }

func isEmojiModifier(c rune) bool { return c >= 0x1F3FB && c <= 0x1F3FF }

// clusterGlyphs resolves a multi-codepoint cell (base plus combining runes)
// to positioned glyphs: emoji sequences (ZWJ, flags, skin tones, keycaps,
// VS16) as a single color glyph when the emoji font has the sequence, VS15
// and everything else shaped with the text font so marks land where the font
// positions them. span is the cell's column span.
func (r *Renderer) clusterGlyphs(base rune, comb []rune, style uint8, span int) []placedGlyph {
	runes := append(append(r.wordRunes[:0], base), comb...)
	r.wordRunes = runes
	key := append(r.keyBuf[:0], style, byte(span))
	for _, c := range runes {
		key = utf8.AppendRune(key, c)
	}
	r.keyBuf = key
	if v, ok := r.clusters.getBytes(key); ok {
		return v
	}
	text := append([]rune(nil), runes...)
	out := r.resolveCluster(text, style, span)
	r.clusters.put(string(key), out)
	return out
}

func (r *Renderer) resolveCluster(text []rune, style uint8, span int) []placedGlyph {
	vs15, emoji := false, isRegionalIndicator(text[0])
	for _, c := range text[1:] {
		switch {
		case c == 0xFE0E:
			vs15 = true
		case c == 0xFE0F, c == 0x200D, c == 0x20E3, isEmojiModifier(c), c >= 0xE0020 && c <= 0xE007F:
			emoji = true
		}
	}
	if emoji && !vs15 {
		if pg := r.colorCluster(text, span); pg != nil {
			return pg
		}
	}
	if pg := r.monoCluster(text, style); pg != nil {
		return pg
	}
	if !vs15 {
		if pg := r.colorCluster(text, span); pg != nil {
			return pg
		}
	}
	if g, ok := r.ensureGlyph('?'); ok {
		return []placedGlyph{{g: g}}
	}
	return []placedGlyph{}
}

// monoCluster shapes the cluster with the style face covering its base. Marks
// that face lacks are drawn from the fallback chain at the pen after the base.
func (r *Renderer) monoCluster(text []rune, style uint8) []placedGlyph {
	src, synth := r.styleFace(style)
	if src.gid(text[0]) == 0 {
		if src = r.srcFor(text[0]); src == nil {
			return nil
		}
		synth = synthFor(style)
	}
	face := src.shapeFace()
	if face == nil {
		g, ok := r.styledGlyph(text[0], style)
		if !ok {
			return nil
		}
		return []placedGlyph{{g: g}}
	}
	var out []placedGlyph
	var baseAdv float32
	if adv, ok := src.face.GlyphAdvance(text[0]); ok {
		baseAdv = float32(adv) / 64
	}
	for _, sg := range r.shapeRunes(face, text, r.ppem()) {
		if sg.gid == 0 {
			c := text[min(sg.cell, len(text)-1)]
			if fb := r.srcFor(c); fb != nil && c != text[0] {
				if g, ok := r.gidGlyph(fb, uint32(fb.gid(c)), synth); ok && g.PixelWidth > 0 {
					out = append(out, placedGlyph{g: g, dx: baseAdv})
				}
			}
			continue
		}
		g, ok := r.gidGlyph(src, sg.gid, synth)
		if !ok || g.PixelWidth == 0 {
			continue
		}
		out = append(out, placedGlyph{g: g, dx: sg.dx, dy: sg.dy})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// emitGlyph appends a mono glyph quad with its pen at (penX, baseline).
func (r *Renderer) emitPlaced(b *glyphBatch, pg placedGlyph, cellX, cellTop float32, span int, fg [4]float32) {
	g := pg.g
	if g.Color {
		r.emitColor(b, g, cellX, cellTop, span, fg[3])
		return
	}
	if g.PixelWidth == 0 {
		return
	}
	r.atlas.touch(int(g.Layer))
	x := cellX + pg.dx + g.OffsetX
	yTop := cellTop + float32(r.atlasAscent) + pg.dy + g.OffsetY
	h := float32(g.PixelHeight)
	b.addGlyphL(x, yTop+h, float32(g.PixelWidth), h, g.X, g.Y, g.Width, g.Height, fg, 0, float32(g.Layer))
}

// emitColor appends a color glyph centered over its cell span.
func (r *Renderer) emitColor(b *glyphBatch, g Glyph, cellX, cellTop float32, span int, alpha float32) {
	r.colorAtlas.touch(int(g.Layer))
	w, h := float32(g.PixelWidth), float32(g.PixelHeight)
	x := cellX + (float32(span)*r.cellWidth-w)/2
	yTop := cellTop + (r.cellHeight-h)/2
	b.addGlyphL(x, yTop+h, w, h, g.X, g.Y, g.Width, g.Height, [4]float32{1, 1, 1, alpha}, 0, -1-float32(g.Layer))
}

// shapeable reports whether a cell can join a ligature run: a single-cell,
// visible, printable ASCII character (every coding-font ligature is ASCII).
func shapeable(c *grid.Cell) bool {
	return c.Char > ' ' && c.Char < 0x7F && c.Width == grid.CellWidthNormal &&
		len(c.Combining) == 0 && c.Flags&grid.FlagHidden == 0
}

// prepareRowShaping shapes the ligature runs of a row (cached), recording in
// ligRow the glyphs each covered cell draws instead of its nominal glyph.
func (r *Renderer) prepareRowShaping(row []grid.Cell) {
	r.ligRow = r.ligRow[:0]
	if !r.ligatures || r.primary == nil {
		return
	}
	for i := 0; i < len(row); {
		if !shapeable(&row[i]) {
			i++
			continue
		}
		style := r.cellStyle(row[i].Flags)
		j := i + 1
		for j < len(row) && shapeable(&row[j]) && r.cellStyle(row[j].Flags) == style {
			j++
		}
		if j-i >= 2 {
			word := r.wordRunes[:0]
			for k := i; k < j; k++ {
				word = append(word, row[k].Char)
			}
			r.wordRunes = word
			if out := r.shapeWord(style, word); out != nil {
				if len(r.ligRow) < len(row) {
					if cap(r.ligRow) < len(row) {
						r.ligRow = make([]ligCell, len(row))
					} else {
						r.ligRow = r.ligRow[:len(row)]
						clear(r.ligRow)
					}
				}
				for k := i; k < j; k++ {
					r.ligRow[k] = ligCell{set: true}
				}
				for a := 0; a < len(out); {
					b := a + 1
					for b < len(out) && out[b].cell == out[a].cell {
						b++
					}
					r.ligRow[i+out[a].cell].glyphs = out[a:b]
					a = b
				}
			}
		}
		i = j
	}
}

// emitCellGlyph appends the glyph quads of one visible, non-blank cell:
// ligature glyphs, grapheme clusters, styled text, color emoji, or '?'.
func (r *Renderer) emitCellGlyph(b *glyphBatch, row []grid.Cell, col int, x, y, clipRight float32, fg [4]float32) {
	cell := &row[col]
	style := r.cellStyle(cell.Flags)
	span := 1
	if cell.Width == grid.CellWidthWide {
		span = 2
	}
	if col < len(r.ligRow) && r.ligRow[col].set {
		src, synth := r.styleFace(style)
		for _, sg := range r.ligRow[col].glyphs {
			if g, ok := r.gidGlyph(src, sg.gid, synth); ok {
				r.emitPlaced(b, placedGlyph{g: g, dx: sg.dx, dy: sg.dy}, x, y, 1, fg)
			}
		}
		return
	}
	if len(cell.Combining) > 0 {
		for _, pg := range r.clusterGlyphs(cell.Char, cell.Combining, style, span) {
			r.emitPlaced(b, pg, x, y, span, fg)
		}
		return
	}
	var g Glyph
	var ok bool
	if c := cell.Char; style == styleRegular && c < 128 && r.asciiCache[c].resolved {
		slot := &r.asciiCache[c]
		g, ok = slot.g, slot.ok
	} else {
		g, ok = r.styledGlyph(c, style)
	}
	if !ok {
		if cg, isColor := r.ensureColorGlyph(cell.Char); isColor {
			if cgl, ok := r.colorGlyphAt(cg.gid, span); ok {
				r.emitColor(b, cgl, x, y, span, fg[3])
			}
			return
		}
		g, ok = r.ensureGlyph('?')
	}
	if !ok || g.PixelWidth == 0 {
		return
	}
	// Icons may also use a following blank cell (Ghostty-style), which is
	// how Nerd Font icons are typically spaced in TUIs.
	if span == 1 && isIconRune(cell.Char) && col+1 < len(row) {
		if next := row[col+1]; next.Char == ' ' || next.Char == 0 {
			span = 2
		}
	}
	if span == 2 && x+2*r.cellWidth > clipRight {
		span = 1
	}
	r.atlas.touch(int(g.Layer))
	gx, gyTop, gw, gh := r.glyphQuad(cell.Char, g, x, y, span)
	b.addGlyphL(gx, gyTop+gh, gw, gh, g.X, g.Y, g.Width, g.Height, fg, 0, float32(g.Layer))
}

// drawCellGlyph draws one cell's glyphs (cluster, style, color emoji) into the
// UI batch with its top-left at (x, yTop), e.g. the text under a block cursor.
func (r *Renderer) drawCellGlyph(x, yTop float32, cell grid.Cell, clr [4]float32, proj [16]float32) {
	r.uiEnsure(uiKindGlyph, proj)
	if r.uiAtlasGen != r.atlasGen {
		r.uiGlyphs.reset()
		r.uiAtlasGen = r.atlasGen
	}
	r.ligRow = r.ligRow[:0]
	r.oneCell[0] = cell
	r.emitCellGlyph(&r.uiGlyphs, r.oneCell[:], 0, x, yTop, x+2*r.cellWidth, clr)
}

// SetLigatures toggles programming ligatures.
func (r *Renderer) SetLigatures(on bool) {
	if r.ligatures != on {
		r.ligatures = on
		r.uiDirty = true
	}
}

// SetTextGamma sets the text blending gamma and contrast (kitty-style
// text_composition_strategy): gamma > 1 thickens dark-on-light text, contrast
// multiplies coverage. Non-positive values keep the defaults.
func (r *Renderer) SetTextGamma(gamma, contrast float32) {
	if gamma <= 0 {
		gamma = defaultTextGamma
	}
	if contrast <= 0 {
		contrast = defaultTextContrast
	}
	r.textGamma, r.textContrast = gamma, contrast
	r.uiDirty = true
}

const (
	defaultTextGamma    = 1.7
	defaultTextContrast = 1.0
)

func (r *Renderer) destroyText() {
	r.atlas.release()
	r.colorAtlas.release()
	if r.face != nil {
		r.face.Close()
	}
	for _, f := range r.fallbackFaces {
		f.Close()
	}
	r.destroyColorEmoji()
}
