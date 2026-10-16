package render

import (
	"math"
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

type testQuad struct {
	x0, y0, x1, y1 float32 // bottom-left x, top y, bottom-right x, bottom y
	u0, v0, u1, v1 float32
}

func glyphQuads(b *glyphBatch) []testQuad {
	var out []testQuad
	v := b.verts
	for i := 0; i+6*glyphVertFloats <= len(v); i += 6 * glyphVertFloats {
		q := v[i : i+6*glyphVertFloats]
		v0, v2, v5 := q[0:], q[2*glyphVertFloats:], q[5*glyphVertFloats:]
		out = append(out, testQuad{
			x0: v5[0], y0: v0[1], x1: v2[0], y1: v2[1],
			u0: v5[2], v0: v0[3], u1: v2[2], v1: v2[3],
		})
	}
	return out
}

func approx(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func textSnap(cols int, rows ...string) *grid.Snapshot {
	s := &grid.Snapshot{Cols: cols, Rows: len(rows), Cells: make([]grid.Cell, cols*len(rows)), LineSizes: make([]grid.LineSize, len(rows))}
	for r, txt := range rows {
		for c := range cols {
			cell := grid.NewCell()
			if c < len(txt) {
				cell.Char = rune(txt[c])
			}
			s.Cells[r*cols+c] = cell
		}
	}
	return s
}

func TestDoubleWidthRowDrawsAt2x(t *testing.T) {
	r := newBenchRenderer()
	snap := textSnap(6, "ab", "ab")
	snap.LineSizes[1] = grid.LineDoubleWidth
	r.buildGridBatches(snap, 0, 0, 1000, 1000, false)
	q := glyphQuads(&r.gridGlyphs)
	if len(q) != 4 {
		t.Fatalf("quads = %d", len(q))
	}
	// Row 0 single: 'b' at x=9, 9 wide. Row 1 double: 'b' at x=18, 18 wide,
	// same vertical extent.
	if !approx(q[1].x0, 9) || !approx(q[1].x1-q[1].x0, 9) {
		t.Fatalf("single row quad %+v", q[1])
	}
	b := q[3]
	if !approx(b.x0, 18) || !approx(b.x1-b.x0, 18) {
		t.Fatalf("double-width quad x = %v..%v", b.x0, b.x1)
	}
	if !approx(b.y0-18, q[1].y0) || !approx(b.y1-18, q[1].y1) {
		t.Fatalf("double-width quad y = %v..%v", b.y0, b.y1)
	}
}

func TestDoubleHeightRowsDrawHalvesClipped(t *testing.T) {
	r := newBenchRenderer()
	snap := textSnap(6, "a", "a")
	snap.LineSizes[0] = grid.LineDoubleTop
	snap.LineSizes[1] = grid.LineDoubleBottom
	r.buildGridBatches(snap, 0, 0, 1000, 1000, false)
	q := glyphQuads(&r.gridGlyphs)
	if len(q) != 2 {
		t.Fatalf("quads = %d", len(q))
	}
	// The 1x glyph spans y 3..17 in its cell with v 0.1..0.12. At 2x it spans
	// 6..34 from the top of the top-half row.
	top, bot := q[0], q[1]
	if !approx(top.y0, 6) || !approx(top.y1, 18) {
		t.Fatalf("top half y = %v..%v, want 6..18", top.y0, top.y1)
	}
	if !approx(top.v0, 0.1) || !approx(top.v1, 0.1+0.02*12/28) {
		t.Fatalf("top half v = %v..%v", top.v0, top.v1)
	}
	if !approx(bot.y0, 18) || !approx(bot.y1, 34) {
		t.Fatalf("bottom half y = %v..%v, want 18..34", bot.y0, bot.y1)
	}
	if !approx(bot.v0, 0.1+0.02*12/28) || !approx(bot.v1, 0.12) {
		t.Fatalf("bottom half v = %v..%v", bot.v0, bot.v1)
	}
	if !approx(top.x1-top.x0, 18) || !approx(bot.x1-bot.x0, 18) {
		t.Fatal("double-height rows not 2x wide")
	}
}

func TestDoubleWidthRowBackgroundsAndDecor(t *testing.T) {
	r := newBenchRenderer()
	snap := textSnap(6, "ab")
	snap.LineSizes[0] = grid.LineDoubleWidth
	snap.Cells[1].Bg = grid.IndexedColor(1)
	snap.Cells[1].Flags = grid.FlagUnderline
	r.buildGridBatches(snap, 0, 0, 1000, 1000, false)
	v := r.gridRects.verts
	if len(v) != 36 || !approx(v[0], 18) || !approx(v[6]-v[0], 18.5) {
		t.Fatalf("background rect = %v", v)
	}
	if len(r.pass2) != 1 || !approx(r.pass2[0].x, 18) || r.pass2[0].ls != grid.LineDoubleWidth {
		t.Fatalf("pass2 = %+v", r.pass2)
	}
	// Only the first cols/2 cells of a double-width row are visible.
	snap = textSnap(6, "abcdef")
	snap.LineSizes[0] = grid.LineDoubleWidth
	r.buildGridBatches(snap, 0, 0, 1000, 1000, false)
	if n := len(glyphQuads(&r.gridGlyphs)); n != 3 {
		t.Fatalf("visible glyphs = %d, want 3", n)
	}
}

func TestSizedTextDrawsScaledAcrossItsRows(t *testing.T) {
	r := newBenchRenderer()
	snap := textSnap(6, "Hi", "", "z")
	snap.Sized = make([][]grid.SizedText, 3)
	blk := grid.SizedText{Col: 0, Cells: 4, Scale: 2, Text: "Hi"}
	snap.Sized[0] = []grid.SizedText{blk}
	blk.SubRow = 1
	snap.Sized[1] = []grid.SizedText{blk}
	r.buildGridBatches(snap, 0, 0, 1000, 1000, false)
	q := glyphQuads(&r.gridGlyphs)
	// Row 0's 1x "Hi" is replaced by the 2x block: H and i, top halves on
	// row 0 and bottom halves on row 1; then row 2's 'z' at 1x.
	if len(q) != 5 {
		t.Fatalf("quads = %d: %+v", len(q), q)
	}
	h0, i0, h1, i1, z := q[0], q[1], q[2], q[3], q[4]
	if !approx(h0.x0, 0) || !approx(h0.x1, 18) || !approx(i0.x0, 18) || !approx(i0.x1, 36) {
		t.Fatalf("x: H %v..%v i %v..%v", h0.x0, h0.x1, i0.x0, i0.x1)
	}
	if !approx(h0.y0, 6) || !approx(h0.y1, 18) || !approx(h1.y0, 18) || !approx(h1.y1, 34) || !approx(i1.y1, 34) {
		t.Fatalf("y: top %v..%v bottom %v..%v", h0.y0, h0.y1, h1.y0, h1.y1)
	}
	if !approx(z.y0, 36+3) || !approx(z.x1-z.x0, 9) {
		t.Fatalf("plain row after block: %+v", z)
	}
}

func TestSizedTextFractionalAlignment(t *testing.T) {
	r := newBenchRenderer()
	snap := textSnap(6, "a")
	snap.Sized = [][]grid.SizedText{{{Col: 1, Cells: 2, Scale: 1, Num: 1, Den: 2, Text: "ab", VAlign: 1, HAlign: 1}}}
	r.buildGridBatches(snap, 0, 0, 1000, 1000, false)
	q := glyphQuads(&r.gridGlyphs)
	if len(q) != 3 {
		t.Fatalf("quads = %d", len(q))
	}
	// Block cols 1-2 (x 9..27); text 2 cells at 1/2 = 9px wide, right aligned
	// from x 18; bottom aligned: 9px tall in an 18px row, from y 9.
	a, b := q[1], q[2]
	if !approx(a.x0, 18) || !approx(a.x1, 22.5) || !approx(b.x0, 22.5) || !approx(b.x1, 27) {
		t.Fatalf("x: a %v..%v b %v..%v", a.x0, a.x1, b.x0, b.x1)
	}
	if !approx(a.y0, 9+1.5) || !approx(a.y1, 9+8.5) {
		t.Fatalf("y: %v..%v", a.y0, a.y1)
	}
	snap.Sized[0][0].HAlign, snap.Sized[0][0].VAlign = 2, 2
	r.buildGridBatches(snap, 0, 0, 1000, 1000, false)
	q = glyphQuads(&r.gridGlyphs)
	if a := q[1]; !approx(a.x0, 13.5) || !approx(a.y0, 4.5+1.5) {
		t.Fatalf("centred: %+v", a)
	}
}

func TestScaledRowGlyphsKeepTheirOwnBackground(t *testing.T) {
	r := newBenchRenderer()
	snap := textSnap(6, "ab")
	snap.LineSizes[0] = grid.LineDoubleTop
	snap.Cells[0].Bg = grid.RGBColor(255, 255, 255)
	r.buildGridBatches(snap, 0, 0, 1000, 1000, false)
	v := r.gridGlyphs.verts
	if len(v) != 2*6*glyphVertFloats {
		t.Fatalf("verts = %d", len(v))
	}
	a, b := v[glyphVertFloats-1], v[6*glyphVertFloats+glyphVertFloats-1]
	if !approx(a, 1) || approx(b, 1) {
		t.Fatalf("background luminance a=%v b=%v: quads share the last cell's background", a, b)
	}
}

func TestCellColumnOnDoubledRows(t *testing.T) {
	for _, tc := range []struct {
		dx   float32
		size grid.LineSize
		want int
	}{
		{25, grid.LineSingle, 2},
		{25, grid.LineDoubleWidth, 1},
		{25, grid.LineDoubleTop, 1},
		{79, grid.LineDoubleBottom, 3},
		{500, grid.LineDoubleWidth, 4}, // past the drawn half: last usable column
		{500, grid.LineSingle, 9},
		{-3, grid.LineDoubleWidth, 0},
	} {
		if got := CellColumn(tc.dx, 10, tc.size, 10); got != tc.want {
			t.Errorf("CellColumn(%v, %v) = %d, want %d", tc.dx, tc.size, got, tc.want)
		}
	}
}

func uiRectBoxes(r *Renderer) [][4]float32 {
	var out [][4]float32
	v := r.uiRects.verts
	for i := 0; i+36 <= len(v); i += 36 {
		out = append(out, [4]float32{v[i], v[i+1], v[i+12] - v[i], v[i+13] - v[i+1]})
	}
	return out
}

// Decorations on a double-height row are laid out on the 2x-wide, 2x-tall
// cell the glyph occupies and clipped to the half the row shows.
func TestDoubleHeightDecorationsAreTall(t *testing.T) {
	r := &Renderer{cellWidth: 10, cellHeight: 18}
	var proj [16]float32
	fg := [4]float32{1, 1, 1, 1}
	draw := func(c grid.Cell, ls grid.LineSize, row int) [][4]float32 {
		r.uiRects.reset()
		y := float32(row) * r.cellHeight
		r.drawPass2(&pass2Item{row: row, y: y, fg: fg, ls: ls}, c, proj)
		return uiRectBoxes(r)
	}
	ul := grid.Cell{Char: 'a', Flags: grid.FlagUnderline}
	if got := draw(ul, grid.LineDoubleBottom, 1); len(got) != 1 || got[0] != [4]float32{0, 34, 20, 2} {
		t.Errorf("bottom-half underline = %v, want one 20x2 rect at the pair's bottom (y 34)", got)
	}
	if got := draw(ul, grid.LineDoubleTop, 0); len(got) != 0 {
		t.Errorf("top-half underline = %v, want none: it belongs to the bottom half", got)
	}
	if got := draw(ul, grid.LineDoubleWidth, 0); len(got) != 1 || got[0] != [4]float32{0, 17, 20, 1} {
		t.Errorf("double-width underline = %v, want 20x1 at y 17", got)
	}
	full := grid.Cell{Char: '█'}
	if got := draw(full, grid.LineDoubleTop, 0); len(got) != 1 || got[0] != [4]float32{0, 0, 20, 18} {
		t.Errorf("top-half full block = %v, want 20x18 clipped to the row", got)
	}
	lower := grid.Cell{Char: '▄'}
	if got := draw(lower, grid.LineDoubleTop, 0); len(got) != 0 {
		t.Errorf("lower half block on the top row = %v, want nothing", got)
	}
	if got := draw(lower, grid.LineDoubleBottom, 1); len(got) != 1 || got[0] != [4]float32{0, 18, 20, 18} {
		t.Errorf("lower half block on the bottom row = %v, want the whole row", got)
	}
	if r.cellWidth != 10 || r.cellHeight != 18 || r.rectClip != nil {
		t.Fatalf("cell size %vx%v clip %v not restored", r.cellWidth, r.cellHeight, r.rectClip)
	}
}
