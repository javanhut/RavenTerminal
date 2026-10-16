package grid

// LineSize is a row's DEC line attribute (DECSWL/DECDWL/DECDHL, ESC # 3-6).
type LineSize uint8

const (
	LineSingle       LineSize = iota // ESC # 5
	LineDoubleWidth                  // ESC # 6
	LineDoubleTop                    // ESC # 3: top half of double-height text
	LineDoubleBottom                 // ESC # 4: bottom half
)

// Doubled reports whether the row's cells are drawn two columns wide.
func (s LineSize) Doubled() bool { return s != LineSingle }

// rowLimit is the number of usable columns on a row: half the width on a
// double-width or double-height row.
func (g *Grid) rowLimit(r *Row) int {
	if r.size != LineSingle {
		return max(g.Cols/2, 1)
	}
	return g.Cols
}

// RowLimitLocked is the number of usable columns on an active row. Caller
// holds g.mu.
func (g *Grid) RowLimitLocked(row int) int {
	if row < 0 || row >= len(g.rows) {
		return g.Cols
	}
	return g.rowLimit(g.rows[row])
}

// clampCursorCol keeps the cursor inside its row's usable columns.
func (g *Grid) clampCursorCol() {
	if lim := g.rowLimit(g.rows[g.CursorRow]); g.CursorCol >= lim {
		g.CursorCol = lim - 1
	}
}

// SetLineSizeLocked sets the cursor row's line attribute. The cursor is pulled
// back into the row's usable columns. Caller holds g.mu.
func (g *Grid) SetLineSizeLocked(s LineSize) {
	r := g.rows[g.CursorRow]
	if r.size != s {
		r.size = s
		r.flags |= RowDirty
	}
	g.wrapPending = false
	g.clampCursorCol()
}

// LineSize returns the line attribute of an active-screen row.
func (g *Grid) LineSize(row int) LineSize {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.LineSizeLocked(row)
}

// ViewLineSize returns the line attribute of a viewport row, following the
// scrollback offset.
func (g *Grid) ViewLineSize(row int) LineSize {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if row < 0 || row >= g.Rows {
		return LineSingle
	}
	if r := g.rowAtAbsLocked(g.viewTopAbsLocked() + row); r != nil {
		return r.size
	}
	return LineSingle
}

// LineSizeLocked is LineSize without locking; the caller must hold g.mu.
func (g *Grid) LineSizeLocked(row int) LineSize {
	if row < 0 || row >= len(g.rows) {
		return LineSingle
	}
	return g.rows[row].size
}

// clearLineSize makes an active row single width, as fully erasing it with ED
// does.
func (g *Grid) clearLineSize(row int) {
	if r := g.rows[row]; r.size != LineSingle {
		r.size = LineSingle
		r.flags |= RowDirty
	}
}

// resetLineSizes makes every active row single width and drops sized text
// (DECALN).
func (g *Grid) resetLineSizes() {
	for _, r := range g.rows {
		if r.size != LineSingle || r.sized != nil {
			r.size, r.sized = LineSingle, nil
			r.flags |= RowDirty
		}
	}
}

// LineSize returns the line attribute of a visible row (single when out of range).
func (s *Snapshot) LineSize(row int) LineSize {
	if row < 0 || row >= len(s.LineSizes) {
		return LineSingle
	}
	return s.LineSizes[row]
}
