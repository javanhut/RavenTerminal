package grid

// Rectangle primitives for left/right margins (DECSLRM) and horizontal
// scrolling (SL/SR), plus cursor accessors that move by columns without
// snapping off wide-character continuation cells (xterm behaviour). All
// coordinates are 0-based and inclusive; the caller must hold g.mu.

// WrapPendingLocked reports whether the next printed character wraps first.
func (g *Grid) WrapPendingLocked() bool { return g.wrapPending }

// SetWrapPendingLocked restores the wrap-pending flag (DECRC).
func (g *Grid) SetWrapPendingLocked(v bool) { g.wrapPending = v }

// SetCursorLocked places the cursor at a 0-based cell, clamped to the screen,
// without snapping off continuation cells, and clears any pending wrap.
func (g *Grid) SetCursorLocked(col, row int) {
	g.wrapPending = false
	g.CursorCol = clampInt(col, 0, g.Cols-1)
	g.CursorRow = clampInt(row, 0, g.Rows-1)
	g.clampCursorCol()
}

func (g *Grid) clampRect(top, bottom, left, right int) (int, int, int, int, bool) {
	top, bottom = max(top, 0), min(bottom, g.Rows-1)
	left, right = max(left, 0), min(right, g.Cols-1)
	return top, bottom, left, right, top <= bottom && left <= right
}

// ScrollRectUpLocked scrolls the rectangle up by n rows, blanking the bottom.
func (g *Grid) ScrollRectUpLocked(top, bottom, left, right, n int, bg Color) {
	top, bottom, left, right, ok := g.clampRect(top, bottom, left, right)
	if !ok || n <= 0 {
		return
	}
	n = min(n, bottom-top+1)
	g.flattenRectRows(top, bottom, left, right)
	g.shiftSizedRect(top, bottom, left, right, -n, 0)
	for row := top; row <= bottom-n; row++ {
		for col := left; col <= right; col++ {
			g.moveCell(g.index(col, row), g.index(col, row+n))
		}
	}
	for row := bottom - n + 1; row <= bottom; row++ {
		g.fillSpan(row, left, right+1, bg)
	}
}

// ScrollRectDownLocked scrolls the rectangle down by n rows, blanking the top.
func (g *Grid) ScrollRectDownLocked(top, bottom, left, right, n int, bg Color) {
	top, bottom, left, right, ok := g.clampRect(top, bottom, left, right)
	if !ok || n <= 0 {
		return
	}
	n = min(n, bottom-top+1)
	g.flattenRectRows(top, bottom, left, right)
	g.shiftSizedRect(top, bottom, left, right, n, 0)
	for row := bottom; row >= top+n; row-- {
		for col := left; col <= right; col++ {
			g.moveCell(g.index(col, row), g.index(col, row-n))
		}
	}
	for row := top; row < top+n; row++ {
		g.fillSpan(row, left, right+1, bg)
	}
}

// ScrollRectLeftLocked shifts the rectangle left by n columns (SL, DCH).
func (g *Grid) ScrollRectLeftLocked(top, bottom, left, right, n int, bg Color) {
	top, bottom, left, right, ok := g.clampRect(top, bottom, left, right)
	if !ok || n <= 0 {
		return
	}
	g.shiftSizedRect(top, bottom, left, right, 0, -n)
	for row := top; row <= bottom; row++ {
		r := min(right, g.rowLimit(g.rows[row])-1)
		if r < left {
			continue
		}
		k := min(n, r-left+1)
		for col := left; col <= r-k; col++ {
			g.moveCell(g.index(col, row), g.index(col+k, row))
		}
		g.fillSpan(row, r-k+1, r+1, bg)
	}
}

// ScrollRectRightLocked shifts the rectangle right by n columns (SR, ICH).
func (g *Grid) ScrollRectRightLocked(top, bottom, left, right, n int, bg Color) {
	top, bottom, left, right, ok := g.clampRect(top, bottom, left, right)
	if !ok || n <= 0 {
		return
	}
	g.shiftSizedRect(top, bottom, left, right, 0, n)
	for row := top; row <= bottom; row++ {
		r := min(right, g.rowLimit(g.rows[row])-1)
		if r < left {
			continue
		}
		k := min(n, r-left+1)
		for col := r; col >= left+k; col-- {
			g.moveCell(g.index(col, row), g.index(col-k, row))
		}
		g.fillSpan(row, left, left+k, bg)
	}
}

// flattenRectRows makes the rows of a partial-width rectangle single width
// before cells move between them. Margins and line sizes don't mix: a VT510
// sets every line single when DECLRMM is set and ignores DECDWL/DECDHL while
// it is, so the parser never builds this state. The grid still refuses it,
// since a double-width row's cells would land in a single-width one at half
// the size.
func (g *Grid) flattenRectRows(top, bottom, left, right int) {
	if left == 0 && right == g.Cols-1 {
		return
	}
	for row := top; row <= bottom; row++ {
		g.clearLineSize(row)
	}
}

// shiftSizedRect moves the OSC 66 blocks that lie wholly inside a rectangle
// by (dRow, dCol), as the rectangle's cells are about to move. A block that
// straddles the rectangle, or that the shift pushes past its edge, would be
// torn apart by the move, so it is dropped and its cells stay as plain text
// (the same as an overwrite does). Blocks outside the rectangle stay put.
func (g *Grid) shiftSizedRect(top, bottom, left, right, dRow, dCol int) {
	type block struct {
		row int // the block's top row
		e   SizedText
	}
	var hit []block
	seen := map[block]bool{}
	for row := top; row <= bottom; row++ {
		for _, e := range g.rows[row].sized {
			if e.Col > right || e.Col+e.Cells <= left {
				continue
			}
			b := block{row - e.SubRow, e}
			b.e.SubRow = 0
			if !seen[b] {
				seen[b] = true
				hit = append(hit, b)
			}
		}
	}
	if len(hit) == 0 {
		return
	}
	// Remove every touched block first, then put the moved ones back, so a
	// moved block never lands on an entry that is still to be removed.
	for _, b := range hit {
		for k := range b.e.Scale {
			if r := b.row + k; r >= 0 && r < len(g.rows) {
				g.removeSized(g.rows[r], b.e, k)
			}
		}
	}
	inside := func(row, col int, e SizedText) bool {
		return row >= top && row+e.Scale-1 <= bottom && col >= left && col+e.Cells-1 <= right
	}
	for _, b := range hit {
		if inside(b.row, b.e.Col, b.e) && inside(b.row+dRow, b.e.Col+dCol, b.e) {
			g.PutSizedTextLocked(b.e.Col+dCol, b.row+dRow, b.e)
		}
	}
}

// EraseBgLocked returns the current BCE colour.
func (g *Grid) EraseBgLocked() Color { return g.eraseBg }
