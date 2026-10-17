package grid

import "testing"

// sizedRows returns, per active row, the OSC 66 entries on it.
func sizedRows(g *Grid) [][]SizedText {
	s := g.Snapshot(nil)
	out := make([][]SizedText, g.Rows)
	for row := range g.Rows {
		out[row] = s.SizedText(row)
	}
	return out
}

// wantBlock checks that rows [row, row+scale) hold exactly one entry each,
// for a block at col, and every other row holds none.
func wantBlock(t *testing.T, g *Grid, col, row, scale int) {
	t.Helper()
	for r, got := range sizedRows(g) {
		if r < row || r >= row+scale {
			if len(got) != 0 {
				t.Fatalf("row %d holds %+v, want nothing", r, got)
			}
			continue
		}
		if len(got) != 1 || got[0].Col != col || got[0].SubRow != r-row {
			t.Fatalf("row %d = %+v, want block at col %d sub-row %d", r, got, col, r-row)
		}
	}
}

func wantNoBlocks(t *testing.T, g *Grid) {
	t.Helper()
	for r, got := range sizedRows(g) {
		if len(got) != 0 {
			t.Fatalf("row %d kept %+v", r, got)
		}
	}
}

func rectOp(g *Grid, f func()) {
	g.mu.Lock()
	f()
	g.mu.Unlock()
}

func TestRectScrollUpMovesContainedBlock(t *testing.T) {
	g := newGrid(20, 6, 100)
	putBlock(g, 4, 2, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	rectOp(g, func() { g.ScrollRectUpLocked(0, 5, 2, 10, 1, DefaultBg()) })
	wantBlock(t, g, 4, 1, 2)
}

func TestRectScrollDownMovesContainedBlock(t *testing.T) {
	g := newGrid(20, 6, 100)
	putBlock(g, 4, 1, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	rectOp(g, func() { g.ScrollRectDownLocked(1, 5, 2, 10, 2, DefaultBg()) })
	wantBlock(t, g, 4, 3, 2)
}

func TestRectScrollDropsBlockLeavingRect(t *testing.T) {
	g := newGrid(20, 6, 100)
	putBlock(g, 4, 1, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	rectOp(g, func() { g.ScrollRectUpLocked(1, 5, 2, 10, 1, DefaultBg()) })
	wantNoBlocks(t, g) // its top row scrolled off the top margin

	g = newGrid(20, 6, 100)
	putBlock(g, 4, 3, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	rectOp(g, func() { g.ScrollRectDownLocked(0, 4, 2, 10, 1, DefaultBg()) })
	wantNoBlocks(t, g) // its bottom row went past the bottom margin
}

func TestRectScrollDropsBlockStraddlingMargins(t *testing.T) {
	// Columns: the block spans the right margin, so only part of it moves.
	g := newGrid(20, 6, 100)
	putBlock(g, 8, 2, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	rectOp(g, func() { g.ScrollRectUpLocked(0, 5, 2, 10, 1, DefaultBg()) })
	wantNoBlocks(t, g)

	// Rows: the block's lower row is outside the region.
	g = newGrid(20, 6, 100)
	putBlock(g, 4, 2, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	rectOp(g, func() { g.ScrollRectUpLocked(0, 2, 2, 10, 1, DefaultBg()) })
	wantNoBlocks(t, g)
}

func TestRectScrollLeavesBlocksOutsideAlone(t *testing.T) {
	g := newGrid(20, 6, 100)
	putBlock(g, 14, 2, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	rectOp(g, func() { g.ScrollRectUpLocked(0, 5, 2, 10, 1, DefaultBg()) })
	wantBlock(t, g, 14, 2, 2)
}

func TestRectShiftMovesContainedBlock(t *testing.T) {
	g := newGrid(20, 6, 100)
	putBlock(g, 4, 2, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	rectOp(g, func() { g.ScrollRectRightLocked(0, 5, 2, 15, 3, DefaultBg()) })
	wantBlock(t, g, 7, 2, 2)
	rectOp(g, func() { g.ScrollRectLeftLocked(0, 5, 2, 15, 2, DefaultBg()) })
	wantBlock(t, g, 5, 2, 2)
	rectOp(g, func() { g.ScrollRectLeftLocked(0, 5, 2, 15, 4, DefaultBg()) })
	wantNoBlocks(t, g) // pushed past the left margin
}

func TestRectShiftOnOneRowDropsTallBlock(t *testing.T) {
	// ICH/DCH inside margins shift a single row: a two-row block would be
	// torn in half, so it goes.
	g := newGrid(20, 6, 100)
	putBlock(g, 4, 2, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	rectOp(g, func() { g.ScrollRectRightLocked(3, 3, 2, 15, 1, DefaultBg()) })
	wantNoBlocks(t, g)
}

func TestRectScrollFlattensDoubledRows(t *testing.T) {
	g := newGrid(10, 4, 100)
	g.SetCursorLocked(0, 1)
	g.SetLineSizeLocked(LineDoubleWidth)
	g.SetCursorLocked(0, 3)
	g.SetLineSizeLocked(LineDoubleTop)
	rectOp(g, func() { g.ScrollRectUpLocked(0, 2, 2, 6, 1, DefaultBg()) })
	if g.LineSize(1) != LineSingle {
		t.Fatalf("row 1 in a partial-width rect scroll kept %v", g.LineSize(1))
	}
	if g.LineSize(3) != LineDoubleTop {
		t.Fatal("row outside the rect lost its size")
	}
}

// shiftedRow returns the first lim visible characters of a row.
func visible(g *Grid, row, lim int) string {
	b := make([]rune, lim)
	for col := range lim {
		c := g.DisplayCell(col, row).Char
		if c == 0 {
			c = ' '
		}
		b[col] = c
	}
	return string(b)
}

func TestRectShiftStaysInDoubledRowColumns(t *testing.T) {
	g := newGrid(10, 2, 100)
	writeStr(g, "abcdefgh")
	g.SetCursorLocked(0, 0)
	g.SetLineSizeLocked(LineDoubleWidth) // "fgh" now sits past the usable half
	rectOp(g, func() { g.ScrollRectLeftLocked(0, 0, 0, 9, 2, DefaultBg()) })
	if got := visible(g, 0, 5); got != "cde  " {
		t.Fatalf("SL on a double-width row = %q, pulled hidden columns in", got)
	}
	rectOp(g, func() { g.ScrollRectRightLocked(0, 0, 0, 9, 3, DefaultBg()) })
	if got := visible(g, 0, 5); got != "   cd" {
		t.Fatalf("SR on a double-width row = %q", got)
	}
}

func TestInsertDeleteCharsStayInDoubledRowColumns(t *testing.T) {
	g := newGrid(10, 2, 100)
	writeStr(g, "abcdefgh")
	g.SetCursorLocked(0, 0)
	g.SetLineSizeLocked(LineDoubleWidth)
	g.SetCursorLocked(1, 0)
	g.DeleteChars(2)
	if got := visible(g, 0, 5); got != "ade  " {
		t.Fatalf("DCH on a double-width row = %q, pulled hidden columns in", got)
	}
	g.InsertChars(1)
	if got := visible(g, 0, 5); got != "a de " {
		t.Fatalf("ICH on a double-width row = %q", got)
	}
}
