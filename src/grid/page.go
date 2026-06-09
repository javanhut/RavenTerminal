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
	r := &Row{cells: make([]storedCell, cols), flags: RowDirty}
	id := g.styles.intern(styleOf(NewCellWithBg(bg))) // ref=1 owner
	for i := range r.cells {
		g.styles.retain(id)
		r.cells[i] = storedCell{Style: id, Char: ' ', Width: CellWidthNormal}
	}
	g.styles.release(id) // drop owner ref; cells hold len(cells) refs
	return r
}

// clearRow releases a row's existing style references and refills it with the
// given background, reusing the row's allocation. Marks the row dirty.
func (g *Grid) clearRow(r *Row, bg Color) {
	for i := range r.cells {
		g.styles.release(r.cells[i].Style)
	}
	id := g.styles.intern(styleOf(NewCellWithBg(bg)))
	for i := range r.cells {
		g.styles.retain(id)
		r.cells[i] = storedCell{Style: id, Char: ' ', Width: CellWidthNormal}
	}
	g.styles.release(id)
	r.flags = RowDirty
}

// releaseRow releases all style references held by a row (used when a row is
// dropped from history). The row itself becomes garbage.
func (g *Grid) releaseRow(r *Row) {
	for i := range r.cells {
		g.styles.release(r.cells[i].Style)
	}
}

// pushHistory appends a row to scrollback history, trimming (and releasing) the
// oldest rows past maxScroll. Alt screens use maxScroll==0 (no history).
func (g *Grid) pushHistory(r *Row) {
	if g.maxScroll <= 0 {
		g.releaseRow(r)
		return
	}
	g.history = append(g.history, r)
	for len(g.history) > g.maxScroll {
		g.releaseRow(g.history[0])
		g.history[0] = nil
		g.history = g.history[1:]
		g.scrolledOut++ // a row left the top of history (for image anchoring)
	}
}
