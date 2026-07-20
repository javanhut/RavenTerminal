package grid

// RowFlags are per-row state bits used by reflow and the renderer snapshot.
type RowFlags uint8

const (
	// RowSoftWrapped marks a row whose line continued onto the next row because
	// it ran out of columns (auto-wrap), as opposed to ending on an explicit
	// newline. Reflow uses this to coalesce wrapped lines. Set in C0.
	RowSoftWrapped RowFlags = 1 << iota
	// RowDirty marks a row whose contents changed since the last snapshot.
	RowDirty
)

// rowPoolMax bounds the recycled-row pool. Scrolling needs one row at a time
// and ScrollUp(n) a handful, so a small cap keeps the pool useful without
// pinning a screenful of cells that may never be reused.
const rowPoolMax = 64

// Row is a single line of cells plus its flags. The active area and the
// scrollback history are both sequences of *Row, so a row scrolling off-screen
// is a pointer move (no cell copy) and its interned style references travel
// with it unchanged.
type Row struct {
	cells []storedCell
	flags RowFlags
}

// newRow allocates a blank row of the given width (default style).
func newRow(cols int) *Row {
	r := &Row{cells: make([]storedCell, cols)}
	for i := range r.cells {
		r.cells[i] = blankStored
	}
	return r
}

// blankRow allocates a blank row at the grid's current width.
func (g *Grid) blankRow(bg Color) *Row { return g.blankRowN(g.Cols, bg) }

// blankRowN allocates a blank row of exactly cols cells, each carrying the given
// background (BCE). Each cell takes one reference to the shared interned fill
// style. Use this (not blankRow) when g.Cols may not yet match the target width,
// e.g. mid-resize before g.Cols is updated.
func (g *Grid) blankRowN(cols int, bg Color) *Row {
	if n := len(g.freeRows); n > 0 {
		r := g.freeRows[n-1]
		if len(r.cells) == cols {
			g.freeRows = g.freeRows[:n-1]
			g.fillRow(r, bg) // refs already released by recycleRow
			return r
		}
		// A resize changed the width, so every pooled row is the wrong size.
		g.freeRows = g.freeRows[:0]
	}
	r := &Row{cells: make([]storedCell, cols)}
	g.fillRow(r, bg)
	return r
}

// fillRow paints a row blank at the given background (BCE), taking one style
// reference per cell. It does NOT release what the row previously held, so the
// caller must own a row that is fresh or already released.
func (g *Grid) fillRow(r *Row, bg Color) {
	id := g.styles.intern(styleOf(NewCellWithBg(bg))) // ref=1 owner
	g.styles.retainN(id, len(r.cells))                // one ref per cell, in bulk
	for i := range r.cells {
		r.cells[i] = storedCell{Style: id, Char: ' ', Width: CellWidthNormal}
	}
	g.styles.release(id) // drop owner ref; cells hold len(cells) refs
	r.flags = RowDirty
}

// fillSpan blanks cells [col0, col1) of a row at the given background (BCE).
// The style is interned once for the whole span rather than per cell: every
// cell in an erase shares one style, so re-interning it per column turns a
// line erase into one hash lookup per column for no benefit.
//
// Fast path: if the span is already blank in the target style — the common
// case, e.g. EL on a fresh or already-erased line — nothing is stored, no
// style reference is taken or released, and the row is not re-dirtied (a
// no-op erase must not force a redraw).
func (g *Grid) fillSpan(row, col0, col1 int, bg Color) {
	if col0 < 0 {
		col0 = 0
	}
	if col1 > g.Cols {
		col1 = g.Cols
	}
	if col0 >= col1 {
		return
	}
	cells := g.rows[row].cells
	st := styleOf(NewCellWithBg(bg))
	// Compare against the target blank WITHOUT touching refcounts first:
	// lookup takes no reference, so an all-match span costs only comparisons.
	// If the style isn't interned at all, no cell can carry it and the span
	// necessarily needs writing.
	if id, ok := g.styles.lookup(st); ok {
		blank := storedCell{Style: id, Char: ' ', Width: CellWidthNormal}
		for col0 < col1 && cells[col0] == blank {
			col0++
		}
		if col0 == col1 {
			return // already erased
		}
	}
	id := g.styles.intern(st) // ref=1 owner
	g.styles.retainN(id, col1-col0)
	// Release the overwritten cells' styles, coalescing runs of equal ids so a
	// uniformly-styled span (the common case) costs one releaseN, not one
	// release per cell.
	for col := col0; col < col1; {
		run := col + 1
		for run < col1 && cells[run].Style == cells[col].Style {
			run++
		}
		g.styles.releaseN(cells[col].Style, run-col)
		col = run
	}
	// Fill via doubling copy: one store, then exponentially larger memmoves.
	blank := storedCell{Style: id, Char: ' ', Width: CellWidthNormal}
	cells[col0] = blank
	for n := 1; n < col1-col0; n *= 2 {
		copy(cells[col0+n:col1], cells[col0:col0+n])
	}
	g.styles.release(id) // drop owner ref; cells hold (col1-col0) refs
	g.rows[row].flags |= RowDirty
}

// clearRow releases a row's existing style references and refills it with the
// given background, reusing the row's allocation. Marks the row dirty.
func (g *Grid) clearRow(r *Row, bg Color) {
	g.releaseRow(r)
	g.fillRow(r, bg)
}

// recycleRow releases a dropped row's style references and keeps its cell
// allocation for reuse. Steady-state scrolling drops exactly one row per new
// blank row, so this makes the scroll path allocation-free once scrollback is
// full — which is where a terminal spends its time under heavy output.
func (g *Grid) recycleRow(r *Row) {
	g.releaseRow(r)
	if len(g.freeRows) < rowPoolMax {
		g.freeRows = append(g.freeRows, r)
	}
}

// releaseRow releases all style references held by a row (used when a row is
// dropped from history). The row itself becomes garbage. Adjacent cells sharing
// one style id are released as a single run — a uniformly-styled row (the
// common case) is one releaseN instead of one release per cell.
func (g *Grid) releaseRow(r *Row) {
	cells := r.cells
	i := 0
	for i < len(cells) {
		id := cells[i].Style
		j := i + 1
		for j < len(cells) && cells[j].Style == id {
			j++
		}
		g.styles.releaseN(id, j-i)
		i = j
	}
}

// pushHistory appends a row to scrollback history, trimming (and releasing) the
// oldest rows past maxScroll. Alt screens use maxScroll==0 (no history).
func (g *Grid) pushHistory(r *Row) {
	if g.maxScroll <= 0 {
		g.recycleRow(r)
		return
	}
	for len(g.history) >= g.maxScroll {
		g.recycleRow(g.history[0])
		g.history[0] = nil
		g.history = g.history[1:]
		g.scrolledOut++ // a row left the top of history (for image anchoring)
	}

	// Trimming reslices the window forward, so it walks toward the end of its
	// backing array; once it arrives, append allocates a fresh array and copies
	// the whole window. Under sustained scroll that repeats every maxScroll
	// lines, and handing those arrays back to the OS (runtime.madvise) cost more
	// than the scrolling itself. Slide the window back to the front of a
	// double-width buffer instead: same one memmove, but the array is reused
	// forever, so steady-state scrolling allocates nothing.
	if len(g.history) == cap(g.history) {
		if cap(g.histBuf) < 2*g.maxScroll {
			g.histBuf = make([]*Row, 2*g.maxScroll)
		}
		g.histBuf = g.histBuf[:cap(g.histBuf)]
		n := copy(g.histBuf, g.history)
		clear(g.histBuf[n:]) // don't pin dropped rows via stale tail pointers
		g.history = g.histBuf[:n]
	}
	g.history = append(g.history, r)
}
