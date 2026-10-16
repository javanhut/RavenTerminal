package grid

// MaxScrollbackLimit is the largest configurable scrollback, in rows.
const MaxScrollbackLimit = 1_000_000

// SetMaxScrollback sets the scrollback cap in rows (clamped to
// [0, MaxScrollbackLimit]), trimming the oldest history beyond it. Scrollback
// memory is only allocated as lines actually scroll off, and short lines are
// stored compactly, so a large cap costs nothing until it is used.
func (g *Grid) SetMaxScrollback(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.SetMaxScrollbackLocked(n)
}

// SetMaxScrollbackLocked is SetMaxScrollback for a caller holding g.mu.
func (g *Grid) SetMaxScrollbackLocked(n int) {
	n = clampInt(n, 0, MaxScrollbackLimit)
	g.maxScroll = n
	if drop := len(g.history) - n; drop > 0 {
		for i := range drop {
			g.recycleRow(g.history[i])
			g.history[i] = nil
		}
		g.scrolledOut += drop
		g.history = g.history[drop:]
		g.snapInvalid = true
	}
	if cap(g.histBuf) > 2*n+64 {
		g.histBuf = append(make([]*Row, 0, len(g.history)+64), g.history...)
		g.history = g.histBuf
	}
	g.scrollOffset = min(g.scrollOffset, len(g.history))
}

// MaxScrollbackRows returns the scrollback cap in rows.
func (g *Grid) MaxScrollbackRows() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.maxScroll
}

// ScrollbackLen returns how many rows are currently held in scrollback.
func (g *Grid) ScrollbackLen() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.history)
}

// resizeRows changes only the height (width unchanged), so no line needs
// rewrapping: rows keep their storage. Shrinking first drops blank rows below
// the cursor, then moves top rows into scrollback; growing appends blanks.
// Absolute row coordinates are unaffected, so the selection survives.
func (g *Grid) resizeRows(newRows int, pts []*Point) {
	if excess := len(g.rows) - newRows; excess > 0 {
		for excess > 0 {
			last := len(g.rows) - 1
			if last <= g.CursorRow || !g.rowIsBlank(g.rows[last]) || g.rows[last-1].flags&RowSoftWrapped != 0 {
				break
			}
			g.recycleRow(g.rows[last])
			g.rows[last] = nil
			g.rows = g.rows[:last]
			excess--
		}
		for i := range excess {
			g.pushHistory(g.rows[i])
		}
		n := copy(g.rows, g.rows[excess:])
		clear(g.rows[n:])
		g.rows = g.rows[:n]
		g.CursorRow -= excess
		for _, p := range pts {
			p.Row -= excess
		}
	}
	for len(g.rows) < newRows {
		g.rows = append(g.rows, g.blankRowN(g.Cols, DefaultBg()))
	}
	g.Rows = newRows
	if g.CursorRow < 0 {
		g.CursorRow, g.wrapPending = 0, false
	}
	g.CursorRow = min(g.CursorRow, newRows-1)
	for _, p := range pts {
		p.Row = clampInt(p.Row, 0, newRows-1)
		p.Col = clampInt(p.Col, 0, g.Cols-1)
	}
	g.scrollTop, g.scrollBottom = 1, newRows
	g.scrollOffset = min(g.scrollOffset, len(g.history))
	g.snapInvalid = true
}
