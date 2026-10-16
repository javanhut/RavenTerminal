package render

import (
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
	"github.com/javanhut/RavenTerminal/src/grid"
)

// newFontRenderer builds a GL-free Renderer with real faces, style variants
// and CPU-side atlases for the named embedded font.
func newFontRenderer(t testing.TB, name string) *Renderer {
	t.Helper()
	data, ok := fonts.GetFont(name)
	if !ok {
		t.Fatalf("no embedded font %q", name)
	}
	r := &Renderer{
		fontSize:     15,
		currentFont:  name,
		contentScale: 1,
		fauxBold:     true,
		fauxItalic:   true,
		ligatures:    true,
	}
	r.theme.Background = [4]float32{0, 0, 0, 1}
	r.theme.Foreground = [4]float32{1, 1, 1, 1}
	if err := r.loadFontData(data); err != nil {
		t.Fatalf("loadFontData: %v", err)
	}
	return r
}

func coverage(m rasterMask) (sum int) {
	for _, a := range m.pix {
		sum += int(a)
	}
	return
}

// Rasterizing by glyph id reproduces x/image's rune rasterizer exactly.
func TestRasterMatchesXImage(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	face, _ := opentype.NewFace(r.primary.sf, &opentype.FaceOptions{Size: 15, DPI: 96, Hinting: font.HintingFull})
	for _, c := range "ag@W" {
		gid := r.primary.gid(c)
		m, ok := r.primary.raster(gid, r.ppem(), 0)
		if !ok {
			t.Fatalf("raster %q failed", c)
		}
		dr, mask, _, _, _ := face.Glyph(fixed.P(0, 0), c)
		if dr.Min.X != m.offX || dr.Min.Y != m.offY || dr.Dx() != m.w || dr.Dy() != m.h {
			t.Fatalf("%q bounds = (%d,%d %dx%d), want %v", c, m.offX, m.offY, m.w, m.h, dr)
		}
		for y := range m.h {
			for x := range m.w {
				_, _, _, a := mask.At(x, y).RGBA()
				if byte(a>>8) != m.pix[y*m.w+x] {
					t.Fatalf("%q pixel (%d,%d) differs", c, x, y)
				}
			}
		}
	}
}

func TestSyntheticBoldWidensStrokes(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	gid := r.primary.gid('l')
	reg, _ := r.primary.raster(gid, r.ppem(), 0)
	bold, _ := r.primary.raster(gid, r.ppem(), synthBold)
	if coverage(bold)*100 < coverage(reg)*115 {
		t.Errorf("synthetic bold coverage %d vs regular %d: strokes not widened", coverage(bold), coverage(reg))
	}
	if bold.w < reg.w {
		t.Errorf("synthetic bold narrower than regular (%d < %d)", bold.w, reg.w)
	}
}

func rowCentroid(m rasterMask, y0, y1 int) float64 {
	var sx, sa float64
	for y := y0; y < y1; y++ {
		for x := range m.w {
			a := float64(m.pix[y*m.w+x])
			sx += a * float64(x+m.offX)
			sa += a
		}
	}
	return sx / sa
}

func TestSyntheticObliqueSlants(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	gid := r.primary.gid('|')
	reg, _ := r.primary.raster(gid, r.ppem(), 0)
	obl, _ := r.primary.raster(gid, r.ppem(), synthOblique)
	q := reg.h / 4
	if d := rowCentroid(reg, 0, q) - rowCentroid(reg, reg.h-q, reg.h); d > 0.5 || d < -0.5 {
		t.Fatalf("regular bar not vertical (top-bottom %.2f)", d)
	}
	q = obl.h / 4
	if d := rowCentroid(obl, 0, q) - rowCentroid(obl, obl.h-q, obl.h); d < 2 {
		t.Errorf("oblique bar top only %.2fpx right of its bottom", d)
	}
}

func TestStyleFacesPreferRealVariants(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	reg, _ := r.styleFace(styleRegular)
	for s := styleBold; s <= styleBoldItalic; s++ {
		if src, synth := r.styleFace(s); src == reg || synth != 0 {
			t.Errorf("jetbrains style %d: want a real variant face without synthesis", s)
		}
	}
	r = newFontRenderer(t, "firacode")
	reg, _ = r.styleFace(styleRegular)
	bold, _ := r.styleFace(styleBold)
	if src, synth := r.styleFace(styleItalic); src != reg || synth != synthOblique {
		t.Errorf("firacode italic must be regular + synthetic oblique")
	}
	if src, synth := r.styleFace(styleBoldItalic); src != bold || src == reg || synth != synthOblique {
		t.Errorf("firacode bold italic must be real bold + synthetic oblique")
	}

	regA, _ := r.ensureGlyph('a')
	boldA, ok := r.styledGlyph('a', styleBold)
	if !ok || boldA == regA {
		t.Errorf("bold 'a' did not get its own atlas entry")
	}
	box, _ := r.ensureGlyph('─')
	if b, _ := r.styledGlyph('─', styleBold); b != box {
		t.Errorf("cell-exact box drawing must not be restyled")
	}
}

func TestStyledGlyphFallsBackToSynthesis(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	c := '➛' // only in the Hack fallback
	reg, ok := r.ensureGlyph(c)
	if !ok {
		t.Skip("no fallback covers ➛")
	}
	bold, ok := r.styledGlyph(c, styleBold)
	if !ok || bold == reg || bold.PixelWidth < reg.PixelWidth {
		t.Errorf("fallback glyph in bold must be synthesized, got %+v vs %+v", bold, reg)
	}
}

// When the atlas is full the least recently used page is recycled: every
// glyph keeps rendering instead of degrading to a permanent miss.
func TestGlyphCacheEvictsInsteadOfMissing(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	r.initAtlases(48, 1, 64, 1)
	gen := r.atlasGen
	for c := 'A'; c <= 'Z'; c++ {
		if _, ok := r.ensureGlyph(c); !ok {
			t.Fatalf("ensureGlyph(%q) failed with a full atlas", c)
		}
	}
	if len(r.glyphMisses) != 0 {
		t.Errorf("glyph misses recorded: %v", r.glyphMisses)
	}
	if len(r.glyphs) >= 26 {
		t.Errorf("all %d glyphs still cached in a page that cannot hold them", len(r.glyphs))
	}
	if g, ok := r.ensureGlyph('A'); !ok || g.PixelWidth == 0 {
		t.Errorf("evicted glyph did not re-rasterize")
	}
	if r.atlasGen == gen {
		t.Errorf("atlasGen not bumped on eviction")
	}
}

func TestShapeCacheLRU(t *testing.T) {
	c := newShapeCache(2)
	c.put("a", []shapedGlyph{{gid: 1}})
	c.put("b", []shapedGlyph{{gid: 2}})
	c.get("a")
	c.put("c", []shapedGlyph{{gid: 3}})
	if _, ok := c.get("b"); ok {
		t.Errorf("least recently used entry survived")
	}
	if v, ok := c.get("a"); !ok || v[0].gid != 1 {
		t.Errorf("recently used entry evicted")
	}
	if _, ok := c.get("c"); !ok {
		t.Errorf("newest entry missing")
	}
	if c.len() != 2 {
		t.Errorf("len = %d, want 2", c.len())
	}
}

func TestLigaturesKeepOneGlyphPerCell(t *testing.T) {
	for _, tc := range []struct{ font, text string }{{"firacode", "->"}, {"jetbrains", "!="}, {"firacode", "==="}} {
		r := newFontRenderer(t, tc.font)
		runes := []rune(tc.text)
		out := r.shapeWord(styleRegular, runes)
		if out == nil {
			t.Fatalf("%s %q: no ligature", tc.font, tc.text)
		}
		changed := false
		for _, g := range out {
			if g.cell < 0 || g.cell >= len(runes) {
				t.Fatalf("%s %q: glyph cell %d out of range", tc.font, tc.text, g.cell)
			}
			if src, _ := r.styleFace(styleRegular); g.gid != uint32(src.gid(runes[g.cell])) {
				changed = true
			}
		}
		if !changed {
			t.Errorf("%s %q: shaped glyphs equal nominal ones", tc.font, tc.text)
		}
		r.ligatures = false
		if r.shapeWord(styleRegular, runes) != nil {
			t.Errorf("%s %q: ligatures disabled but still substituted", tc.font, tc.text)
		}
	}
	r := newFontRenderer(t, "firacode")
	if r.shapeWord(styleRegular, []rune("ab")) != nil {
		t.Errorf("plain letters reported a substitution")
	}
	if newFontRenderer(t, "hack").styleHasLigatures(styleRegular) {
		t.Errorf("Hack has no ligatures; shaping should be skipped")
	}
}

func TestShapeWordCached(t *testing.T) {
	r := newFontRenderer(t, "firacode")
	r.styleHasLigatures(styleBold)
	r.shapeWord(styleRegular, []rune("<=>"))
	n := r.shapeCalls
	for range 5 {
		r.shapeWord(styleRegular, []rune("<=>"))
	}
	if r.shapeCalls != n {
		t.Errorf("cached word reshaped %d times", r.shapeCalls-n)
	}
	r.shapeWord(styleBold, []rune("<=>"))
	if r.shapeCalls != n+1 {
		t.Errorf("style must be part of the cache key")
	}
}

func TestClusterCombiningMark(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	pg := r.clusterGlyphs('q', []rune{0x301}, styleRegular, 1)
	if len(pg) != 2 {
		t.Fatalf("q+acute gave %d glyphs, want base and mark", len(pg))
	}
	if pg[0].g.Color || pg[1].g.Color || pg[1].g.PixelWidth == 0 {
		t.Fatalf("bad glyphs %+v", pg)
	}
	baseTop := pg[0].dy + pg[0].g.OffsetY
	markBottom := pg[1].dy + pg[1].g.OffsetY + float32(pg[1].g.PixelHeight)
	if markBottom > baseTop+2*glyphPad+1 {
		t.Errorf("acute (bottom %.1f) not placed above the q (top %.1f)", markBottom, baseTop)
	}
}

func newEmojiRenderer(t *testing.T) *Renderer {
	r := newFontRenderer(t, "jetbrains")
	r.loadColorFont()
	if r.colorFace == nil {
		t.Skip("no color emoji font installed")
	}
	return r
}

func TestClusterEmojiSequences(t *testing.T) {
	r := newEmojiRenderer(t)
	plain := r.clusterGlyphs(0x1F44D, []rune{}, styleRegular, 2)
	cases := []struct {
		name  string
		runes []rune
	}{
		{"zwj family", []rune{0x1F468, 0x200D, 0x1F469, 0x200D, 0x1F467}},
		{"flag", []rune{0x1F1FA, 0x1F1F8}},
		{"skin tone", []rune{0x1F44D, 0x1F3FD}},
		{"keycap", []rune{'1', 0xFE0F, 0x20E3}},
		{"vs16", []rune{0x263A, 0xFE0F}},
	}
	for _, tc := range cases {
		pg := r.clusterGlyphs(tc.runes[0], tc.runes[1:], styleRegular, 2)
		if len(pg) != 1 || !pg[0].g.Color {
			t.Errorf("%s: got %d glyphs (color=%v), want one color glyph", tc.name, len(pg), len(pg) > 0 && pg[0].g.Color)
		}
	}
	tone := r.clusterGlyphs(0x1F44D, []rune{0x1F3FD}, styleRegular, 2)
	if len(plain) != 1 || len(tone) != 1 || plain[0].g == tone[0].g {
		t.Errorf("skin-toned thumbs up drew the plain glyph")
	}
	text := r.clusterGlyphs(0x263A, []rune{0xFE0E}, styleRegular, 1)
	if len(text) == 0 || text[0].g.Color {
		t.Errorf("VS15 must force text presentation")
	}
}

func oneRow(cells ...grid.Cell) *grid.Snapshot {
	return &grid.Snapshot{Cols: len(cells), Rows: 1, Cells: cells}
}

func cellOf(c rune, flags grid.CellFlags, combining ...rune) grid.Cell {
	cell := grid.NewCell()
	cell.Char = c
	cell.Flags = flags
	if len(combining) > 0 {
		cell.Combining = combining
	}
	return cell
}

func quads(r *Renderer) [][]float32 {
	var q [][]float32
	v := r.gridGlyphs.verts
	for i := 0; i+6*glyphVertFloats <= len(v); i += 6 * glyphVertFloats {
		q = append(q, append([]float32(nil), v[i:i+6*glyphVertFloats]...))
	}
	return q
}

// uv returns the quad's top-left texture coordinate and layer.
func uv(q []float32) [3]float32 { return [3]float32{q[2], q[3], q[8]} }

func buildRow(r *Renderer, snap *grid.Snapshot) [][]float32 {
	r.buildGridBatches(snap, 0, 0, float32(snap.Cols)*r.cellWidth, r.cellHeight, false)
	return quads(r)
}

func TestBoldCellUsesBoldFaceSingleQuad(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	q := buildRow(r, oneRow(cellOf('a', 0), cellOf('a', grid.FlagBold), cellOf('a', grid.FlagItalic)))
	if len(q) != 3 {
		t.Fatalf("got %d quads, want 3 (no faux double draw)", len(q))
	}
	if uv(q[0]) == uv(q[1]) || uv(q[0]) == uv(q[2]) || uv(q[1]) == uv(q[2]) {
		t.Errorf("styled cells reuse the regular glyph")
	}
	r.fauxBold = false
	q = buildRow(r, oneRow(cellOf('a', 0), cellOf('a', grid.FlagBold)))
	if uv(q[0]) != uv(q[1]) {
		t.Errorf("bold disabled but bold glyph drawn")
	}
}

func TestLigatureCellsDrawShapedGlyphs(t *testing.T) {
	r := newFontRenderer(t, "firacode")
	row := oneRow(cellOf('-', 0), cellOf('>', 0))
	lone := buildRow(r, oneRow(cellOf('-', 0)))
	q := buildRow(r, row)
	if len(q) == 0 {
		t.Fatalf("ligature drew nothing")
	}
	for _, quad := range q {
		if uv(quad) == uv(lone[0]) {
			t.Errorf("ligature run drew the nominal '-' glyph")
		}
	}
	r.ligatures = false
	q = buildRow(r, row)
	if len(q) != 2 || uv(q[0]) != uv(lone[0]) {
		t.Errorf("ligatures off must draw nominal glyphs")
	}
}

func TestClusterCellDrawsMark(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	if q := buildRow(r, oneRow(cellOf('q', 0, 0x301))); len(q) != 2 {
		t.Errorf("q+acute drew %d quads, want 2", len(q))
	}
}

func TestColorEmojiBatchedWithText(t *testing.T) {
	r := newEmojiRenderer(t)
	emoji := cellOf(0x1F642, 0, 0xFE0F)
	emoji.Width = grid.CellWidthWide
	cont := grid.NewCell()
	cont.Char = 0
	cont.Width = grid.CellWidthContinuation
	q := buildRow(r, oneRow(emoji, cont, cellOf('a', 0)))
	if len(q) != 2 {
		t.Fatalf("got %d quads, want emoji + text in one batch", len(q))
	}
	if q[0][8] >= 0 || q[1][8] < 0 {
		t.Errorf("layers = %v, %v: want color (negative) then mono", q[0][8], q[1][8])
	}
	if c := q[0][4:8]; c[0] != 1 || c[1] != 1 || c[2] != 1 {
		t.Errorf("color glyph tinted %v", c)
	}
}

func codeSnapshot(cols, rows int) *grid.Snapshot {
	snap := &grid.Snapshot{Cols: cols, Rows: rows, Cells: make([]grid.Cell, cols*rows)}
	lines := []string{
		"func (r *Renderer) emit(x, y float32) -> error { if a != b && c >= d { return nil } }",
		"    for i := 0; i <= n; i++ { total += xs[i] /* sum */ } // www.example.org => ok",
		"  let f = |x| x |> g >>= h;  a === b !== c  <!-- html --> ::scope  0xFF",
	}
	for row := range rows {
		line := []rune(lines[row%len(lines)])
		for col := range cols {
			cell := grid.NewCell()
			if col < len(line) {
				cell.Char = line[col]
			}
			if row%4 == 1 && col%7 == 0 {
				cell.Flags |= grid.FlagBold
			}
			snap.Cells[row*cols+col] = cell
		}
	}
	return snap
}

func TestSteadyStateFramesDoNotShape(t *testing.T) {
	r := newFontRenderer(t, "firacode")
	snap := codeSnapshot(100, 12)
	r.buildGridBatches(snap, 0, 0, 100*r.cellWidth, 12*r.cellHeight, false)
	n, atlas := r.shapeCalls, len(r.glyphs)
	r.buildGridBatches(snap, 0, 0, 100*r.cellWidth, 12*r.cellHeight, false)
	if r.shapeCalls != n || len(r.glyphs) != atlas {
		t.Errorf("second identical frame shaped %d runs and rasterized %d glyphs", r.shapeCalls-n, len(r.glyphs)-atlas)
	}
	if n == 0 {
		t.Errorf("code with ligature sequences never reached the shaper")
	}
}

// BenchmarkBuildGridBatchesShaped measures a steady-state frame with real
// fonts, ligature shaping (cached) and bold runs.
func BenchmarkBuildGridBatchesShaped(b *testing.B) {
	for _, name := range []string{"jetbrains", "firacode"} {
		for _, geo := range [][2]int{{107, 30}, {147, 41}} {
			b.Run(name+"_"+itoa(geo[0])+"x"+itoa(geo[1]), func(b *testing.B) {
				r := newFontRenderer(b, name)
				snap := codeSnapshot(geo[0], geo[1])
				w, h := float32(geo[0])*r.cellWidth, float32(geo[1])*r.cellHeight
				r.buildGridBatches(snap, 0, 0, w, h, false)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					r.buildGridBatches(snap, 0, 0, w, h, false)
				}
			})
		}
	}
}

// The cursor redraws its cell's full cluster in the cell's style.
func TestDrawCellGlyphUsesClusterAndStyle(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	var proj [16]float32
	r.drawCellGlyph(0, 0, cellOf('q', 0, 0x301), [4]float32{0, 0, 0, 1}, proj)
	if n := len(r.uiGlyphs.verts) / (6 * glyphVertFloats); n != 2 {
		t.Errorf("cursor cluster drew %d quads, want 2", n)
	}
	r.uiGlyphs.reset()
	r.drawCellGlyph(0, 0, cellOf('a', grid.FlagBold), [4]float32{0, 0, 0, 1}, proj)
	bold := append([]float32(nil), r.uiGlyphs.verts...)
	r.uiGlyphs.reset()
	r.drawCellGlyph(0, 0, cellOf('a', 0), [4]float32{0, 0, 0, 1}, proj)
	if len(bold) == 0 || bold[2] == r.uiGlyphs.verts[2] {
		t.Errorf("cursor over bold text drew the regular glyph")
	}
}
