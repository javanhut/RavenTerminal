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

// Snapshot copies the visible region into a Snapshot under a single lock.
// If prev is non-nil and the same size, its Cells backing array is reused to
// avoid per-frame allocation. Clears per-row dirty flags as a side effect.
//
// When the buffer is reused, the view is at the bottom (scrollOffset == 0,
// matching the previous snapshot), and no full invalidation was requested
// (resize/reflow/view-scroll), only rows flagged RowDirty are re-inflated;
// clean rows keep their contents from the previous snapshot. Every other
// case falls back to a full copy, because the row-to-display mapping may have
// changed without dirty flags reflecting it.
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

	full := s != prev || g.scrollOffset != 0 || s.ScrollOffset != 0 || g.snapInvalid
	g.snapInvalid = false

	// Copy the visible region, clearing per-row dirty flags as we go. A set
	// RowDirty flag also means "visible content changed", which feeds the
	// Dirty advisory below.
	dirty := false
	if full {
		for row := range rows {
			base := row * cols
			for col := range cols {
				s.Cells[base+col] = g.displayCellLocked(col, row)
			}
			if r := g.rows[row]; r.flags&RowDirty != 0 {
				dirty = true
				r.flags &^= RowDirty
			}
		}
	} else {
		for row := range rows {
			r := g.rows[row]
			if r.flags&RowDirty == 0 {
				continue
			}
			dirty = true
			r.flags &^= RowDirty
			base := row * cols
			for col := range cols {
				s.Cells[base+col] = g.displayCellLocked(col, row)
			}
		}
	}

	// Cursor / scroll.
	if g.CursorCol != s.CursorCol || g.CursorRow != s.CursorRow || g.scrollOffset != s.ScrollOffset {
		dirty = true
	}
	s.Cols, s.Rows = cols, rows
	s.CursorCol, s.CursorRow = g.CursorCol, g.CursorRow
	s.ScrollOffset = g.scrollOffset

	// Selection, projected onto the current viewport (false when off-screen).
	selActive, sSCol, sSRow, sECol, sERow := g.selectionViewLocked()
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

	// Record the captured state so RedrawNeeded can peek for changes.
	g.lastSnap = snapState{
		valid:        true,
		cols:         cols,
		rows:         rows,
		cursorCol:    g.CursorCol,
		cursorRow:    g.CursorRow,
		scrollOffset: g.scrollOffset,
		selActive:    selActive,
		sSCol:        sSCol, sSRow: sSRow, sECol: sECol, sERow: sERow,
	}
	return s
}

// RedrawNeeded reports whether the visible state (content, cursor, scroll
// position, selection, or size) has changed since the last Snapshot, WITHOUT
// clearing any dirty flags. The render loop uses it to skip frames entirely
// when nothing changed; the following Snapshot still observes the same dirt.
func (g *Grid) RedrawNeeded() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if !g.lastSnap.valid || g.lastSnap.cols != g.Cols || g.lastSnap.rows != g.Rows {
		return true
	}
	for _, r := range g.rows {
		if r.flags&RowDirty != 0 {
			return true
		}
	}
	if g.CursorCol != g.lastSnap.cursorCol || g.CursorRow != g.lastSnap.cursorRow ||
		g.scrollOffset != g.lastSnap.scrollOffset {
		return true
	}
	// Selection, projected exactly as Snapshot records it.
	selActive, sSCol, sSRow, sECol, sERow := g.selectionViewLocked()
	if selActive != g.lastSnap.selActive {
		return true
	}
	if selActive && (sSCol != g.lastSnap.sSCol || sSRow != g.lastSnap.sSRow ||
		sECol != g.lastSnap.sECol || sERow != g.lastSnap.sERow) {
		return true
	}
	return false
}
