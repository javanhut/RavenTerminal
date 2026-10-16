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
	// RowWidePad marks a soft-wrapped row whose last cell is padding left by a
	// wide character that did not fit; reflow drops it instead of keeping it
	// as content.
	RowWidePad
	// RowHasCluster marks a row that may hold multi-rune grapheme clusters, so
	// the grapheme table sweep can skip every other row.
	RowHasCluster
)

// rowWrapFlags are the flags that describe how a row's content continues.
const rowWrapFlags = RowSoftWrapped | RowWidePad

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
	mark  MarkKind // OSC 133 marks (marks.go)
	exit  int32
	size  LineSize    // DECDWL/DECDHL line attribute (linesize.go)
	sized []SizedText // OSC 66 blocks (sizedtext.go)
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
	var r *Row
	if n := len(g.freeRows); n > 0 {
		r = g.freeRows[n-1]
		g.freeRows = g.freeRows[:n-1]
	} else {
		r = &Row{}
	}
	if len(r.cells) != cols {
		r.cells = nil
		if n := len(g.freeCells); n > 0 {
			if len(g.freeCells[n-1]) == cols {
				r.cells = g.freeCells[n-1]
				g.freeCells = g.freeCells[:n-1]
			} else {
				// A resize changed the width: every pooled array is the wrong size.
				g.freeCells = g.freeCells[:0]
			}
		}
		if r.cells == nil {
			r.cells = make([]storedCell, cols)
		}
	}
	g.fillRow(r, bg) // refs already released by recycleRow
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
	r.mark, r.exit = 0, 0
	r.size = LineSingle
	r.sized = nil
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
	if len(g.rows[row].sized) > 0 {
		g.dropSized(row, col0, col1)
	}
	// An erase that reaches the last column ends the logical line here: the
	// row no longer continues onto the next one. Clear the soft-wrap mark even
	// on the no-op fast path below, since a stale mark makes SelectedText and
	// reflow glue this row to its successor.
	if col1 == g.Cols {
		g.rows[row].flags &^= rowWrapFlags
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

// historyArenaChunk is how many cells compacted scrollback rows carve from one
// allocation.
const historyArenaChunk = 1 << 14

// compactForHistory trims a hard-ended row's trailing default blanks as it
// enters scrollback, moving the content into a right-sized slice carved from a
// shared arena and returning the full-width array to the pool. Short lines
// then cost their length, not the terminal width, which is what makes a large
// scrollback affordable. Soft-wrapped rows stay full width.
func (g *Grid) compactForHistory(r *Row) {
	if r.flags&RowSoftWrapped != 0 {
		return
	}
	n := len(r.cells)
	for n > 0 && r.cells[n-1] == blankStored {
		n--
	}
	if n == len(r.cells) {
		return
	}
	full := r.cells
	if n == 0 {
		r.cells = nil
	} else {
		if len(g.arena) < n {
			g.arena = make([]storedCell, max(historyArenaChunk, n))
		}
		r.cells = g.arena[:n:n]
		g.arena = g.arena[n:]
		copy(r.cells, full)
	}
	if len(full) == g.Cols && len(g.freeCells) < rowPoolMax {
		g.freeCells = append(g.freeCells, full)
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
		g.scrolledOut++
		return
	}
	for len(g.history) >= g.maxScroll {
		g.recycleRow(g.history[0])
		g.history[0] = nil
		g.history = g.history[1:]
		g.scrolledOut++ // a row left the top of history (for image anchoring)
	}
	g.compactForHistory(r)

	// Trimming reslices the window forward, so it walks toward the end of its
	// backing array; once it arrives, append allocates a fresh array and copies
	// the whole window. Under sustained scroll that repeats every maxScroll
	// lines, and handing those arrays back to the OS (runtime.madvise) cost more
	// than the scrolling itself. Slide the window back to the front of a
	// double-width buffer instead: same one memmove, but the array is reused
	// forever, so steady-state scrolling allocates nothing.
	// The buffer grows geometrically up to that double width, so a large
	// configured scrollback costs nothing until it is actually used.
	if len(g.history) == cap(g.history) {
		if want := min(max(2*len(g.history), 64), 2*g.maxScroll); cap(g.histBuf) < want {
			g.histBuf = make([]*Row, want)
		}
		g.histBuf = g.histBuf[:cap(g.histBuf)]
		n := copy(g.histBuf, g.history)
		clear(g.histBuf[n:]) // don't pin dropped rows via stale tail pointers
		g.history = g.histBuf[:n]
	}
	g.history = append(g.history, r)
}
