package grid

// Snapshot is an immutable, fully-resolved copy of the visible grid region
// produced under a single lock for the renderer to consume lock-free. It
// replaces the renderer's per-cell DisplayCell reads (each of which previously
// took the grid RWMutex), eliminating ~Cols*Rows lock acquisitions per frame.
type Snapshot struct {
	Cols, Rows int
	Cells      []Cell // row-major, inflated public DTO, len == Cols*Rows

	CursorCol, CursorRow int
	CursorVisible        bool
	ScrollOffset         int

	// Normalized selection bounds (start <= end). Valid only when SelActive.
	SelActive                          bool
	selSCol, selSRow, selECol, selERow int

	// Dirty reports whether anything changed versus the previous snapshot
	// (content, cursor, selection, or scroll position). Advisory: a renderer
	// may skip presenting when false.
	Dirty      bool
	Generation uint64
}

// Selected reports whether a display cell falls within the snapshot's selection.
func (s *Snapshot) Selected(col, row int) bool {
	if !s.SelActive {
		return false
	}
	if row < s.selSRow || row > s.selERow {
		return false
	}
	if s.selSRow == s.selERow {
		return col >= s.selSCol && col <= s.selECol
	}
	if row == s.selSRow {
		return col >= s.selSCol
	}
	if row == s.selERow {
		return col <= s.selECol
	}
	return true
}

// At returns the cell at a visible (col,row); out-of-bounds yields a blank.
func (s *Snapshot) At(col, row int) Cell {
	if col < 0 || col >= s.Cols || row < 0 || row >= s.Rows {
		return NewCell()
	}
	return s.Cells[row*s.Cols+col]
}

// Snapshot copies the visible region into a Snapshot under a single read lock.
// If prev is non-nil and the same size, its Cells backing array is reused to
// avoid per-frame allocation. Clears per-row dirty flags as a side effect.
func (g *Grid) Snapshot(prev *Snapshot) *Snapshot {
	g.mu.Lock() // write lock: we clear RowDirty flags
	defer g.mu.Unlock()

	cols, rows := g.Cols, g.Rows
	var s *Snapshot
	if prev != nil && prev.Cols == cols && prev.Rows == rows && len(prev.Cells) == cols*rows {
		s = prev
	} else {
		s = &Snapshot{Cells: make([]Cell, cols*rows)}
	}

	// Determine whether any visible content changed since the last snapshot.
	dirty := false
	for _, r := range g.rows {
		if r.flags&RowDirty != 0 {
			dirty = true
			r.flags &^= RowDirty
		}
	}

	// Copy the visible region (reuses the existing scrollback-aware mapping).
	for row := range rows {
		base := row * cols
		for col := range cols {
			s.Cells[base+col] = g.displayCellLocked(col, row)
		}
	}

	// Cursor / scroll.
	if g.CursorCol != s.CursorCol || g.CursorRow != s.CursorRow || g.scrollOffset != s.ScrollOffset {
		dirty = true
	}
	s.Cols, s.Rows = cols, rows
	s.CursorCol, s.CursorRow = g.CursorCol, g.CursorRow
	s.ScrollOffset = g.scrollOffset

	// Selection (normalized), valid only when the view hasn't scrolled away.
	selActive := g.selectionActive && g.scrollOffset == g.selectionScrollOffset
	sSCol, sSRow := g.selectionStartCol, g.selectionStartRow
	sECol, sERow := g.selectionEndCol, g.selectionEndRow
	if sERow < sSRow || (sERow == sSRow && sECol < sSCol) {
		sSCol, sECol = sECol, sSCol
		sSRow, sERow = sERow, sSRow
	}
	if selActive != s.SelActive || sSCol != s.selSCol || sSRow != s.selSRow ||
		sECol != s.selECol || sERow != s.selERow {
		dirty = true
	}
	s.SelActive = selActive
	s.selSCol, s.selSRow, s.selECol, s.selERow = sSCol, sSRow, sECol, sERow

	s.Dirty = dirty
	if dirty {
		s.Generation++
	}
	return s
}
