package grid

// Reflow rewraps soft-wrapped lines when the terminal is resized, so wrapped
// output reflows to the new width instead of being truncated. Driven by the
// per-row RowSoftWrapped flag (set at auto-wrap time in WriteChar).
//
// Stored cells move from the old rows into the new ones as-is, carrying their
// style references with them: nothing is inflated or re-interned, and rows and
// cells are carved from a few large slabs, so a resize is O(cells) with O(1)
// allocations.

// reflowTracker follows one position (cursor, history boundary, saved point)
// through a reflow.
type reflowTracker struct {
	abs, col       int  // old absolute row (index into seq) and column
	pend           bool // cursor with a pending wrap: sits one past col
	off            int  // offset within its logical line
	newAbs, newCol int
	found          bool
	newPend        bool
}

// reflowAlloc carves rows and cells for a reflow out of shared slabs.
type reflowAlloc struct {
	rows  []Row
	cells []storedCell
}

func (a *reflowAlloc) row(n int) *Row {
	if len(a.rows) == 0 {
		a.rows = make([]Row, 1024)
	}
	r := &a.rows[0]
	a.rows = a.rows[1:]
	if n > 0 {
		if len(a.cells) < n {
			a.cells = make([]storedCell, max(1<<16, n))
		}
		r.cells = a.cells[:n:n]
		a.cells = a.cells[n:]
	}
	return r
}

func isBlankStored(sc storedCell) bool {
	return sc.Style == 0 && sc.Grapheme == 0 && sc.Link == 0 &&
		(sc.Char == ' ' || sc.Char == 0) && sc.Width == CellWidthNormal
}

// effLen is the number of content cells in a source row (a wide-char pad at
// the end of a soft-wrapped row is not content).
func effLen(r *Row) int {
	n := len(r.cells)
	if r.flags&RowSoftWrapped != 0 && r.flags&RowWidePad != 0 && n > 0 {
		n--
	}
	return n
}

// reflow rebuilds the grid at (newCols,newRows), rewrapping soft-wrapped lines.
// Caller holds g.mu. Used for the main screen; the alternate screen clamps.
func (g *Grid) reflow(newCols, newRows int, pts []*Point) {
	// 0. Trim fully-blank rows below the cursor. They are the unused area under
	//    the shell prompt, not content: treating them as content pushes the real
	//    content into scrollback on shrink and leaves a duplicated, stale copy
	//    visible at the top after the next grow. Never trim a row that continues
	//    a soft-wrapped line.
	cursorAbs := len(g.history) + g.CursorRow
	seq := make([]*Row, 0, len(g.history)+len(g.rows))
	seq = append(seq, g.history...)
	seq = append(seq, g.rows...)
	keep := len(seq)
	for keep-1 > cursorAbs && g.rowIsBlank(seq[keep-1]) && seq[keep-2].flags&RowSoftWrapped == 0 {
		keep--
	}
	for _, r := range seq[keep:] {
		g.releaseRow(r)
	}
	seq = seq[:keep]

	trk := make([]reflowTracker, 2, 2+len(pts))
	trk[0] = reflowTracker{abs: cursorAbs, col: g.CursorCol, pend: g.wrapPending && g.CursorCol == g.Cols-1}
	trk[1] = reflowTracker{abs: len(g.history)}
	for _, p := range pts {
		trk = append(trk, reflowTracker{abs: len(g.history) + p.Row, col: p.Col})
	}

	var alloc reflowAlloc
	out := make([]*Row, 0, len(seq)+len(seq)/2+newRows)
	for i := 0; i < len(seq); {
		j := i
		for j < len(seq)-1 && seq[j].flags&RowSoftWrapped != 0 {
			j++
		}
		out = g.reflowLine(seq[i:j+1], i, newCols, trk, &alloc, out)
		i = j + 1
	}
	// Positions on rows that were trimmed away (or never existed) land just
	// past the content.
	for k := range trk {
		if !trk[k].found {
			trk[k].newAbs, trk[k].newCol = len(out), min(trk[k].col, newCols-1)
		}
	}

	// Choose the active window. Keep the old history/active boundary where
	// possible so resizing doesn't shuffle content between the screen and
	// scrollback (a prompt at the top of the screen stays at the top); the
	// boundary is only forced down when the rewrapped content needs more rows
	// than the new screen has.
	activeStart := max(trk[1].newAbs, len(out)-newRows, 0)
	for len(out) < activeStart+newRows {
		out = append(out, alloc.row(0))
	}
	active := make([]*Row, newRows)
	for k := range newRows {
		r := out[activeStart+k]
		if len(r.cells) != newCols {
			full := alloc.row(newCols).cells
			n := copy(full, r.cells)
			for c := n; c < newCols; c++ {
				full[c] = blankStored
			}
			r.cells = full
		}
		r.flags |= RowDirty
		active[k] = r
	}
	hist := out[:activeStart]
	if drop := len(hist) - g.maxScroll; drop > 0 {
		for k := range drop {
			g.releaseRow(hist[k])
			hist[k] = nil
		}
		hist = hist[drop:]
	}
	g.history = hist[:len(hist):len(hist)]
	g.histBuf = nil
	g.rows = active

	c := trk[0]
	g.CursorRow = clampInt(c.newAbs-activeStart, 0, newRows-1)
	g.CursorCol = clampInt(c.newCol, 0, newCols-1)
	g.wrapPending = c.newPend && g.CursorRow == c.newAbs-activeStart
	for k, p := range pts {
		t := trk[2+k]
		p.Row = clampInt(t.newAbs-activeStart, 0, newRows-1)
		p.Col = clampInt(t.newCol, 0, newCols-1)
	}

	g.Cols = newCols
	g.Rows = newRows
	g.scrollTop = 1
	g.scrollBottom = newRows
	g.scrollOffset = 0
	g.resizeTabStops(newCols)
	g.freeCells = g.freeCells[:0]
	// Rows were rebuilt; the next snapshot must re-copy fully.
	g.snapInvalid = true
}

// reflowLine rewraps one logical line (src rows, the first at absolute index
// base) into rows of newCols appended to out, mapping trackers on the way.
func (g *Grid) reflowLine(src []*Row, base, newCols int, trk []reflowTracker, alloc *reflowAlloc, out []*Row) []*Row {
	var mark MarkKind
	var exit int32
	var flags RowFlags
	L := 0
	for k, r := range src {
		mark |= r.mark
		if r.mark&markHasExit != 0 {
			exit = r.exit
		}
		flags |= r.flags & RowHasCluster
		for t := range trk {
			if trk[t].abs == base+k {
				trk[t].off = L + trk[t].col
				if trk[t].pend {
					trk[t].off++
				}
			}
		}
		n := effLen(r)
		if n < len(r.cells) {
			g.styles.release(r.cells[n].Style) // dropped wide-char pad
		}
		L += n
	}
	last := src[len(src)-1]
	if last.flags&RowSoftWrapped == 0 {
		// Trim trailing blanks of a hard-ended line (padding, not content), but
		// never below the cursor: its cell stays so it reflows to the right row.
		minL := 0
		if c := trk[0]; c.abs >= base && c.abs < base+len(src) {
			minL = c.off
			if !c.pend {
				minL++
			}
		}
		for k := len(src) - 1; k >= 0 && L > minL; k-- {
			r := src[k]
			n := effLen(r)
			blank := 0
			for blank < n && L-blank > minL && isBlankStored(r.cells[n-1-blank]) {
				blank++
			}
			L -= blank
			if blank < n {
				break
			}
		}
		if L < minL {
			L = minL // cursor past the stored cells: pad with blanks below
		}
	}

	// at returns the virtual cell x of the line.
	k, kStart := 0, 0
	at := func(x int) storedCell {
		kk, ks := k, kStart
		for kk < len(src) && x >= ks+effLen(src[kk]) {
			ks += effLen(src[kk])
			kk++
		}
		if kk >= len(src) || x-ks >= len(src[kk].cells) {
			return blankStored
		}
		return src[kk].cells[x-ks]
	}

	pos := 0
	for first := true; first || pos < L; first = false {
		end := min(pos+newCols, L)
		if end < L && end-pos > 1 && at(end-1).Width == CellWidthWide {
			end-- // a wide char never splits: push it to the next row
		}
		soft := end < L
		w := end - pos
		if soft {
			w = newCols
		}
		r := alloc.row(w)
		// Copy [pos,end) across source rows.
		for x := pos; x < end; {
			for k < len(src) && x >= kStart+effLen(src[k]) {
				kStart += effLen(src[k])
				k++
			}
			if k >= len(src) {
				for ; x < end; x++ {
					r.cells[x-pos] = blankStored
				}
				break
			}
			sr := src[k]
			m := min(end, kStart+effLen(sr)) - x
			copy(r.cells[x-pos:], sr.cells[x-kStart:x-kStart+m])
			x += m
		}
		for c := end - pos; c < len(r.cells); c++ {
			r.cells[c] = blankStored
		}
		r.flags = RowDirty | flags
		if soft {
			r.flags |= RowSoftWrapped
			if end-pos < newCols {
				r.flags |= RowWidePad
			}
		}
		if first {
			r.mark, r.exit = mark, exit
		}
		for t := range trk {
			tt := &trk[t]
			if tt.found || tt.abs < base || tt.abs >= base+len(src) {
				continue
			}
			if tt.off >= pos && (tt.off < end || !soft) {
				tt.found = true
				tt.newAbs = len(out)
				tt.newCol = tt.off - pos
				if tt.newCol >= newCols {
					tt.newCol = newCols - 1
					tt.newPend = tt.pend
				}
			}
		}
		out = append(out, r)
		pos = end
	}
	return out
}

// rowIsBlank reports whether every cell in a row is an unstyled, link-free blank.
func (g *Grid) rowIsBlank(r *Row) bool {
	for _, sc := range r.cells {
		if !isBlankStored(sc) {
			return false
		}
	}
	return true
}
