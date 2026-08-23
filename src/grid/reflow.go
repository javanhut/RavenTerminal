package grid

// Reflow rewraps soft-wrapped lines when the terminal is resized, so wrapped
// output reflows to the new width instead of being truncated. Driven by the
// per-row RowSoftWrapped flag (set at auto-wrap time in WriteChar).

// logicalLine is a wrapped paragraph: the concatenation of one or more rows
// where every row except the last was soft-wrapped.
type logicalLine struct {
	cells   []Cell // inflated content
	hardEnd bool   // true if the line ended on a hard newline (not a wrap)
}

// reflow rebuilds the grid at (newCols,newRows), rewrapping soft-wrapped lines.
// Caller holds g.mu. Used for the main screen; the alternate screen clamps.
func (g *Grid) reflow(newCols, newRows int) {
	// 0. Trim fully-blank rows below the cursor. They are the unused area under
	//    the shell prompt, not content: treating them as content pushes the real
	//    content into scrollback on shrink and leaves a duplicated, stale copy
	//    visible at the top after the next grow. Never trim a row that continues
	//    a soft-wrapped line.
	cursorAbsRow := len(g.history) + g.CursorRow
	seq := make([]*Row, 0, len(g.history)+len(g.rows))
	seq = append(seq, g.history...)
	seq = append(seq, g.rows...)
	keep := len(seq)
	for keep-1 > cursorAbsRow &&
		g.rowIsBlank(seq[keep-1]) &&
		seq[keep-2].flags&RowSoftWrapped == 0 {
		keep--
	}
	for _, r := range seq[keep:] {
		g.releaseRow(r)
	}
	seq = seq[:keep]

	// 1. Gather logical lines from history + active rows, tracking the cursor's
	//    logical position AND the history/active boundary so both can be
	//    restored afterward.
	var lines []logicalLine
	cur := logicalLine{}
	cursorLine, cursorOffset := -1, 0
	boundaryAbsRow := len(g.history) // first active row
	boundaryLine, boundaryOffset := 0, 0

	for absRow, r := range seq {
		offsetInLine := len(cur.cells)
		for _, sc := range r.cells {
			cur.cells = append(cur.cells, g.inflate(sc))
		}
		if absRow == cursorAbsRow {
			cursorLine = len(lines)
			cursorOffset = offsetInLine + g.CursorCol
		}
		if absRow == boundaryAbsRow {
			boundaryLine = len(lines)
			boundaryOffset = offsetInLine
		}
		if r.flags&RowSoftWrapped == 0 {
			cur.hardEnd = true
			lines = append(lines, cur)
			cur = logicalLine{}
		}
	}
	if len(cur.cells) > 0 || cur.hardEnd {
		lines = append(lines, cur)
	}

	// 2. Release all old style references; we rebuild fresh rows below.
	for _, r := range seq {
		g.releaseRow(r)
	}

	// 3. Re-wrap each logical line into rows of newCols (wide chars never split).
	var newSeq []*Row
	var cursorNewAbsRow, cursorNewCol int
	cursorFound := false
	boundaryNewAbsRow := 0
	boundaryFound := false
	for li, ln := range lines {
		trimLogicalTrailingBlanks(&ln)
		segments := wrapCells(ln.cells, newCols)
		for si, seg := range segments {
			r := newRow(newCols)
			for i, c := range seg {
				g.putRowCell(r, i, c)
			}
			// Mark soft-wrapped unless this is the final segment of the line.
			if si < len(segments)-1 {
				r.flags |= RowSoftWrapped
			}
			// Locate the history/active boundary's new row.
			if li == boundaryLine && !boundaryFound {
				start := si * newCols
				if boundaryOffset >= start && boundaryOffset < start+newCols || si == len(segments)-1 {
					boundaryNewAbsRow = len(newSeq)
					boundaryFound = true
				}
			}
			// Locate the cursor's new row/col.
			if li == cursorLine && !cursorFound {
				start := si * newCols
				if cursorOffset >= start && cursorOffset < start+newCols || si == len(segments)-1 {
					cursorNewAbsRow = len(newSeq)
					cursorNewCol = max(cursorOffset-start, 0)
					if cursorNewCol >= newCols {
						cursorNewCol = newCols - 1
					}
					cursorFound = true
				}
			}
			newSeq = append(newSeq, r)
		}
		// An empty hard line still occupies one row.
		if len(segments) == 0 {
			r := newRow(newCols)
			if li == boundaryLine && !boundaryFound {
				boundaryNewAbsRow = len(newSeq)
				boundaryFound = true
			}
			if li == cursorLine && !cursorFound {
				cursorNewAbsRow = len(newSeq)
				cursorNewCol = 0
				cursorFound = true
			}
			newSeq = append(newSeq, r)
		}
	}

	// 4. Choose the active window. Keep the old history/active boundary where
	//    possible so resizing doesn't shuffle content between the screen and
	//    scrollback (a prompt at the top of the screen stays at the top); the
	//    boundary is only forced down when the rewrapped content needs more
	//    rows than the new screen has. activeStart is the PRE-TRIM boundary:
	//    front-trimming history below never moves it, so cursor math against
	//    it is invariant to trimming.
	activeStart := boundaryNewAbsRow
	if !boundaryFound {
		activeStart = len(newSeq) - newRows
	}
	if floor := len(newSeq) - newRows; activeStart < floor {
		activeStart = floor
	}
	if activeStart < 0 {
		activeStart = 0
	}
	// Pad with blank rows so the active region is exactly newRows tall.
	for len(newSeq) < activeStart+newRows {
		newSeq = append(newSeq, newRow(newCols))
	}

	// Compute the cursor's row within the active region now, using the pre-trim
	// boundary. The found case anchors to its content row; the not-found
	// fallback parks the cursor on the last active row.
	cursorInActive := newRows - 1
	if cursorFound {
		cursorInActive = cursorNewAbsRow - activeStart
	}

	// Capture the active region as its own slice BEFORE trimming history, so the
	// in-place front-trim below cannot alias or clobber the active rows.
	active := append([]*Row(nil), newSeq[activeStart:activeStart+newRows]...)
	histRows := newSeq[:activeStart]

	// Trim history beyond the cap (releasing the dropped front rows).
	if len(histRows) > g.maxScroll {
		drop := len(histRows) - g.maxScroll
		for i := range drop {
			g.releaseRow(histRows[i])
		}
		histRows = histRows[drop:]
	}

	g.history = append([]*Row(nil), histRows...)
	g.rows = active

	// 5. Restore cursor (clamped to the active area).
	g.CursorRow = max(cursorInActive, 0)
	if g.CursorRow >= newRows {
		g.CursorRow = newRows - 1
	}
	g.CursorCol = max(cursorNewCol, 0)
	if g.CursorCol >= newCols {
		g.CursorCol = newCols - 1
	}

	g.Cols = newCols
	g.Rows = newRows
	g.scrollTop = 1
	g.scrollBottom = newRows
	g.scrollOffset = 0
	g.tabStops = nil
	g.wrapPending = false
	// Rows were rebuilt from scratch; the next snapshot must re-copy fully.
	g.snapInvalid = true
}

// putRowCell writes an inflated Cell into a freshly-allocated row, interning its
// style (the row's blank style at i is released first).
func (g *Grid) putRowCell(r *Row, i int, c Cell) {
	old := r.cells[i].Style
	var gid uint32
	if len(c.Combining) > 0 {
		cl := append([]rune{c.Char}, c.Combining...)
		gid = g.internGrapheme(cl)
	}
	r.cells[i] = storedCell{
		Style:    g.styles.intern(styleOf(c)),
		Grapheme: gid,
		Char:     c.Char,
		Width:    c.Width,
		Link:     c.Link,
	}
	g.styles.release(old)
}

// wrapCells splits a logical line's cells into segments of at most cols,
// never splitting a wide character across the boundary.
func wrapCells(cells []Cell, cols int) [][]Cell {
	if len(cells) == 0 {
		return nil
	}
	var segs [][]Cell
	i := 0
	for i < len(cells) {
		end := min(i+cols, len(cells))
		// Don't split a wide char: if the last kept cell is a wide-char start,
		// drop it to the next segment.
		if end < len(cells) && end > i && cells[end-1].Width == CellWidthWide {
			end--
		}
		if end == i { // pathological (cols too small); force progress
			end = i + 1
		}
		seg := make([]Cell, end-i)
		copy(seg, cells[i:end])
		segs = append(segs, seg)
		i = end
	}
	return segs
}

// trimLogicalTrailingBlanks removes trailing default-blank cells from a hard
// line (padding); soft-wrapped interior is already full-width content.
func trimLogicalTrailingBlanks(ln *logicalLine) {
	if !ln.hardEnd {
		return
	}
	n := len(ln.cells)
	for n > 0 && isBlankCell(ln.cells[n-1]) {
		n--
	}
	ln.cells = ln.cells[:n]
}

// rowIsBlank reports whether every cell in a row is an unstyled, link-free blank.
func (g *Grid) rowIsBlank(r *Row) bool {
	for _, sc := range r.cells {
		c := g.inflate(sc)
		if !isBlankCell(c) || c.Link != 0 {
			return false
		}
	}
	return true
}

func isBlankCell(c Cell) bool {
	return (c.Char == ' ' || c.Char == 0) &&
		c.Fg.Type == ColorDefault && c.Bg.Type == ColorDefault &&
		c.Flags == 0 && len(c.Combining) == 0
}
