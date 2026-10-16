package render

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

// quadX returns a quad's left/right screen x and left/right texture u.
func quadX(q []float32) (x0, x1, u0, u1 float32) {
	return q[0], q[glyphVertFloats+0], q[2], q[glyphVertFloats+2]
}

func quadColor(q []float32) [4]float32 { return [4]float32(q[4:8]) }

func sgrRGB(r, g, b uint8) grid.Color { return grid.Color{Type: grid.ColorRGB, R: r, G: g, B: b} }

// A ligature spanning cells of different colours draws each cell's slice of
// the ligature glyph in that cell's colour, with continuous texture.
func TestLigatureSlicesTakeEachCellsColour(t *testing.T) {
	for _, tc := range []struct{ font, text string }{{"firacode", "->"}, {"jetbrains", "!="}, {"firacode", "<=>"}} {
		r := newFontRenderer(t, tc.font)
		runes := []rune(tc.text)
		cols := []grid.Color{sgrRGB(255, 0, 0), sgrRGB(0, 0, 255), sgrRGB(0, 255, 0)}
		var cells []grid.Cell
		for i, c := range runes {
			cell := cellOf(c, 0)
			cell.Fg = cols[i]
			cells = append(cells, cell)
		}
		q := buildRow(r, oneRow(cells...))
		cw := r.cellWidth
		for i := range runes {
			want := r.colorToRGBA(cols[i], false)
			lo, hi := float32(i)*cw, float32(i+1)*cw
			found := false
			for _, quad := range q {
				x0, x1, _, _ := quadX(quad)
				if x1 <= lo || x0 >= hi {
					continue
				}
				// Only the run's outer edges may overhang their cell.
				if (i > 0 && x0 < lo-0.01) || (i < len(runes)-1 && x1 > hi+0.01) {
					if quadColor(quad) == want {
						t.Errorf("%s %q: cell %d slice [%v,%v] not clipped to [%v,%v]", tc.font, tc.text, i, x0, x1, lo, hi)
					}
					continue
				}
				if quadColor(quad) != want {
					t.Errorf("%s %q: quad [%v,%v] over cell %d coloured %v, want %v", tc.font, tc.text, x0, x1, i, quadColor(quad), want)
				}
				found = true
			}
			if !found {
				t.Errorf("%s %q: nothing drawn over cell %d", tc.font, tc.text, i)
			}
		}
		// Slices of one glyph (same texture origin on screen) abut exactly.
		origin := func(q []float32) float32 {
			x0, x1, u0, u1 := quadX(q)
			return x0 - u0*(x1-x0)/(u1-u0)
		}
		joined := false
		for _, a := range q {
			ax0, ax1, _, _ := quadX(a)
			var next []float32
			for _, b := range q {
				bx0, _, _, _ := quadX(b)
				if bx0 > ax0 && a[8] == b[8] && a[3] == b[3] && approx(origin(a), origin(b)) && (next == nil || bx0 < next[0]) {
					next = b
				}
			}
			if next == nil {
				continue
			}
			if next[0] != ax1 {
				t.Errorf("%s %q: slices of one glyph end at x=%v and resume at %v", tc.font, tc.text, ax1, next[0])
			}
			joined = joined || quadColor(a) != quadColor(next)
		}
		if !joined {
			t.Errorf("%s %q: no ligature glyph was split between cells", tc.font, tc.text)
		}
	}
}

// A block cursor inside a ligature redraws that cell's slice of the ligature
// glyph, not the plain character.
func TestCursorOverLigatureDrawsSlice(t *testing.T) {
	r := newFontRenderer(t, "firacode")
	row := []grid.Cell{cellOf('-', 0), cellOf('>', 0)}
	grid0 := buildRow(r, oneRow(row...))
	lone := buildRow(r, oneRow(cellOf('-', 0)))
	var proj [16]float32
	ink := [4]float32{0.1, 0.2, 0.3, 1}
	cw := r.cellWidth
	for col := range row {
		r.uiGlyphs.reset()
		x := float32(col) * cw
		r.drawCellGlyph(x, 0, row, col, ink, r.cursorColor(), proj)
		v := r.uiGlyphs.verts
		if len(v) == 0 {
			t.Fatalf("cursor on col %d drew nothing", col)
		}
		for i := 0; i < len(v); i += 6 * glyphVertFloats {
			q := v[i : i+6*glyphVertFloats]
			x0, x1, u0, _ := quadX(q)
			if quadColor(q) != ink {
				t.Errorf("col %d: cursor text colour %v, want %v", col, quadColor(q), ink)
			}
			if col == 0 && (x1 > cw+0.01 || u0 == lone[0][2]) {
				t.Errorf("col 0: drew [%v,%v] u=%v, want the ligature's slice of cell 0", x0, x1, u0)
			}
			if col == 1 && x0 < cw-0.01 {
				t.Errorf("col 1: slice starts at %v, left of the cell", x0)
			}
			match := false
			for _, g := range grid0 {
				gx0, gx1, gu0, _ := quadX(g)
				if gx0 == x0 && gx1 == x1 && gu0 == u0 {
					match = true
				}
			}
			if !match {
				t.Errorf("col %d: cursor quad [%v,%v] matches no grid slice", col, x0, x1)
			}
		}
	}
}

// A mark the primary font lacks pulls the whole cluster into the fallback
// font that has it, so that font's mark positioning applies.
func TestClusterFallsBackWhole(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	const mark = 0x342 // perispomeni: Fira Code has it, JetBrains Mono doesn't
	if r.primary.gid(mark) != 0 {
		t.Skip("primary covers the mark")
	}
	var fb *fontSrc
	for _, s := range r.fallbackSrcs {
		if s.gid(mark) != 0 && s.gid('A') != 0 {
			fb = s
			break
		}
	}
	if fb == nil {
		t.Skip("no embedded fallback has the mark")
	}
	pg := r.clusterGlyphs('A', []rune{mark}, styleRegular, 1)
	if len(pg) != 2 {
		t.Fatalf("A+perispomeni gave %d glyphs, want base and mark", len(pg))
	}
	want := r.shapeRunes(fb.shapeFace(), []rune{'A', mark}, r.ppem())
	baseG, _ := r.gidGlyph(fb, uint32(fb.gid('A')), 0)
	if pg[0].g != baseG {
		t.Errorf("base not drawn from the mark's font")
	}
	markG, _ := r.gidGlyph(fb, want[1].gid, 0)
	if pg[1].g != markG || pg[1].dx != want[1].dx || pg[1].dy != want[1].dy {
		t.Errorf("mark at (%v,%v), want the font's GPOS position (%v,%v)", pg[1].dx, pg[1].dy, want[1].dx, want[1].dy)
	}
	if want[1].dy >= 0 {
		t.Errorf("fallback font did not raise the mark over a capital (dy %v)", want[1].dy)
	}
}

// An emoji ZWJ sequence the colour font has no glyph for draws its component
// emoji side by side within the cluster's cells.
func TestUnknownZWJSequenceDrawsComponents(t *testing.T) {
	r := newEmojiRenderer(t)
	seq := []rune{0x1F436, 0x200D, 0x1F355} // dog ZWJ pizza: not a real sequence
	pg := r.clusterGlyphs(seq[0], seq[1:], styleRegular, 2)
	if len(pg) != 2 {
		t.Fatalf("got %d glyphs, want the two components", len(pg))
	}
	span := 2 * r.cellWidth
	for i, p := range pg {
		if !p.g.Color {
			t.Fatalf("component %d not a colour glyph", i)
		}
		if p.dx < 0 || p.dx+float32(p.g.PixelWidth) > span+0.01 || p.dy < 0 || p.dy+float32(p.g.PixelHeight) > r.cellHeight+0.01 {
			t.Errorf("component %d at (%v,%v) %dx%d outside the %vx%v cluster box", i, p.dx, p.dy, p.g.PixelWidth, p.g.PixelHeight, span, r.cellHeight)
		}
	}
	if pg[0].dx+float32(pg[0].g.PixelWidth) > pg[1].dx+0.01 {
		t.Errorf("components overlap: first ends at %v, second starts at %v", pg[0].dx+float32(pg[0].g.PixelWidth), pg[1].dx)
	}
	if pg[0].g == pg[1].g {
		t.Errorf("both components drew the same glyph")
	}

	emoji := cellOf(seq[0], 0, seq[1:]...)
	emoji.Width = grid.CellWidthWide
	cont := grid.NewCell()
	cont.Char = 0
	cont.Width = grid.CellWidthContinuation
	if q := buildRow(r, oneRow(emoji, cont)); len(q) != 2 || q[0][8] >= 0 || q[1][8] >= 0 {
		t.Errorf("grid drew %d quads, want two colour components", len(q))
	}
}

func lum(c [4]float32) float32 { return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2] }

// Every glyph vertex carries the luminance of the background actually behind
// its cell, for gamma and contrast correction.
func TestGlyphsCarryCellBackground(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	r.theme.Background = [4]float32{0.1, 0.1, 0.1, 1}
	r.theme.Selection = [4]float32{0.2, 0.6, 0.2, 1}
	g := grid.NewGrid(4, 1)
	g.SetSelection(3, 0, 3, 0)
	snap := g.Snapshot(nil)
	sgr := cellOf('b', 0)
	sgr.Bg = sgrRGB(255, 255, 255)
	inv := cellOf('c', grid.FlagInverse)
	inv.Fg = sgrRGB(200, 200, 0)
	copy(snap.Cells, []grid.Cell{cellOf('a', 0), sgr, inv, cellOf('d', 0)})
	q := buildRow(r, snap)
	if len(q) != 4 {
		t.Fatalf("got %d quads, want 4", len(q))
	}
	want := []float32{
		lum(r.defaultBg()),
		lum(r.colorToRGBA(sgr.Bg, true)),
		lum(r.colorToRGBA(inv.Fg, false)),
		lum(r.theme.Selection),
	}
	for i, quad := range q {
		for v := range 6 {
			if got := quad[v*glyphVertFloats+9]; got < want[i]-1e-4 || got > want[i]+1e-4 {
				t.Errorf("cell %d vertex %d background %v, want %v", i, v, got, want[i])
				break
			}
		}
	}

	var proj [16]float32
	r.uiGlyphs.reset()
	r.drawCellGlyph(0, 0, snap.Cells[:1], 0, r.defaultBg(), r.cursorColor(), proj)
	if v := r.uiGlyphs.verts; len(v) == 0 || !approx(v[9], lum(r.cursorColor())) {
		t.Errorf("text under the cursor not corrected against the cursor colour")
	}
	r.uiGlyphs.reset()
	r.drawText(0, r.cellHeight, "x", r.defaultFg(), proj)
	if v := r.uiGlyphs.verts; len(v) == 0 || v[9] >= 0 {
		t.Errorf("panel text should defer to the theme background (negative), got %v", v[9:10])
	}
}
