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
	n = min(n, right-left+1)
	for row := top; row <= bottom; row++ {
		for col := left; col <= right-n; col++ {
			g.moveCell(g.index(col, row), g.index(col+n, row))
		}
		g.fillSpan(row, right-n+1, right+1, bg)
	}
}

// ScrollRectRightLocked shifts the rectangle right by n columns (SR, ICH).
func (g *Grid) ScrollRectRightLocked(top, bottom, left, right, n int, bg Color) {
	top, bottom, left, right, ok := g.clampRect(top, bottom, left, right)
	if !ok || n <= 0 {
		return
	}
	n = min(n, right-left+1)
	for row := top; row <= bottom; row++ {
		for col := right; col >= left+n; col-- {
			g.moveCell(g.index(col, row), g.index(col-n, row))
		}
		g.fillSpan(row, left, left+n, bg)
	}
}

// EraseBgLocked returns the current BCE colour.
func (g *Grid) EraseBgLocked() Color { return g.eraseBg }
