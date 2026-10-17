package grid

import (
	"slices"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// MaxClusterRunes caps the codepoints stored in one grapheme cluster. Further
// codepoints that would extend a full cluster are dropped, so a flood of
// combining marks cannot grow a cell without bound.
const MaxClusterRunes = 32

// graphemeGCMin is the intern-table size below which the table is never swept.
const graphemeGCMin = 4096

// Text returns the cell's full grapheme cluster (base plus combining runes)
// for rendering and copying. An empty cell reads as a space.
func (c Cell) Text() string {
	if len(c.Combining) == 0 {
		if c.Char == 0 {
			return " "
		}
		return string(c.Char)
	}
	b := make([]byte, 0, 4*(1+len(c.Combining)))
	b = utf8.AppendRune(b, c.Char)
	for _, r := range c.Combining {
		b = utf8.AppendRune(b, r)
	}
	return string(b)
}

// SetGraphemeClustering switches mode 2027 (grapheme cluster segmentation and
// cluster widths). On by default; off restores per-codepoint widths where
// only zero-width codepoints attach to the previous cell.
func (g *Grid) SetGraphemeClustering(on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.SetGraphemeClusteringLocked(on)
}

// SetGraphemeClusteringLocked is SetGraphemeClustering for a caller holding g.mu.
func (g *Grid) SetGraphemeClusteringLocked(on bool) { g.graphemeOff = !on }

// GraphemeClustering reports whether mode 2027 is on.
func (g *Grid) GraphemeClustering() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return !g.graphemeOff
}

// GraphemeClusteringLocked is GraphemeClustering for a caller holding g.mu.
func (g *Grid) GraphemeClusteringLocked() bool { return !g.graphemeOff }

func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// mayExtend is a cheap prefilter for joinCluster: a printing codepoint can
// only extend the previous cluster after a ZWJ, as the second regional
// indicator of a flag, or as a Hangul jamo continuation. Zero-width
// codepoints always go through the full segmentation check.
func mayExtend(prevLast, c rune, w int) bool {
	switch {
	case w == 0, prevLast == 0x200D, isRegionalIndicator(c):
		return true
	case c >= 0x1160 && c <= 0x11FF, c >= 0xD7B0 && c <= 0xD7FF:
		return true
	case prevLast >= 0x1100 && prevLast <= 0x115F, prevLast >= 0xA960 && prevLast <= 0xA97F:
		return true
	}
	return false
}

// prevCell locates the cell holding the cluster the next codepoint would
// extend: the one just written (also across a pending wrap), or the end of
// the previous row when the cursor sits at column 0 of its soft-wrapped
// continuation. crossRow reports the latter case.
func (g *Grid) prevCell() (r *Row, col int, crossRow, ok bool) {
	r = g.rows[g.CursorRow]
	switch {
	case g.wrapPending:
		col = g.Cols - 1
	case g.CursorCol > 0:
		col = g.CursorCol - 1
	case g.CursorRow > 0 && g.rows[g.CursorRow-1].flags&RowSoftWrapped != 0:
		r = g.rows[g.CursorRow-1]
		col = len(r.cells) - 1
		if r.flags&RowWidePad != 0 {
			col--
		}
		crossRow = true
	default:
		return nil, 0, false, false
	}
	if col > 0 && r.cells[col].Width == CellWidthContinuation {
		col--
	}
	return r, col, crossRow, col >= 0
}

// joinCluster appends c to the previous cell's grapheme cluster when Unicode
// segmentation (UAX #29, via uniseg) says it continues that cluster, then
// resizes the cell to the cluster's width (mode 2027). It reports whether c
// was consumed. With clustering off only zero-width codepoints attach and
// widths never change. Caller holds g.mu.
func (g *Grid) joinCluster(c rune, w int) bool {
	if g.graphemeOff && w != 0 {
		return false
	}
	r, col, crossRow, ok := g.prevCell()
	if !ok {
		return false
	}
	p := &r.cells[col]
	cl := g.clusterBuf[:0]
	if p.Grapheme != 0 && int(p.Grapheme) < len(g.graphemes) {
		cl = append(cl, g.graphemes[p.Grapheme]...)
	} else {
		cl = append(cl, p.Char)
	}
	g.clusterBuf = cl
	if !mayExtend(cl[len(cl)-1], c, w) {
		return false
	}
	if len(cl) >= MaxClusterRunes && w == 0 {
		return true // full: drop the mark
	}
	width := 0
	if !g.graphemeOff {
		b := g.clusterBytes[:0]
		for _, x := range cl {
			b = utf8.AppendRune(b, x)
		}
		b = utf8.AppendRune(b, c)
		g.clusterBytes = b
		first, _, cw, _ := uniseg.FirstGraphemeCluster(b, -1)
		if len(first) != len(b) {
			return false
		}
		width = cw
		if c == 0xFE0F || slices.Contains(cl, 0xFE0F) {
			width = max(width, emojiKeycapWidth(cl[0]))
		}
	}
	if len(cl) >= MaxClusterRunes {
		return true
	}
	cl = append(cl, c)
	g.clusterBuf = cl
	p.Grapheme = g.internGrapheme(cl)
	r.flags |= RowDirty | RowHasCluster
	r.touch(col + 1) // the previous cell may be a blank in the known-blank tail
	if g.graphemeOff || crossRow {
		return true
	}
	width = min(max(width, 1), 2)
	if old := max(int(p.Width), 1); width != old {
		g.rewidth(r, col, width)
	}
	return true
}

// rewidth changes the cluster at (r, col) on the cursor row to width cells,
// moving the cursor with it. A cluster that widens in the last column moves
// to the next line, as a wide character written there would.
func (g *Grid) rewidth(r *Row, col, width int) {
	p := &r.cells[col]
	if width == 1 {
		p.Width = CellWidthNormal
		if col+1 < len(r.cells) {
			blankHalf(&r.cells[col+1])
		}
		g.CursorCol = col + 1
		g.wrapPending = false
		return
	}
	if g.Cols < 2 {
		return
	}
	if col == g.Cols-1 {
		if !g.autoWrap {
			return
		}
		sc := *p
		blankHalf(p)
		p.Grapheme = 0
		r.flags |= RowSoftWrapped | RowWidePad
		g.wrapPending = false
		g.cursorNewline()
		r = g.rows[g.CursorRow]
		col = 0
		g.clearOrphans(r, 0, 2)
		g.styles.retain(sc.Style)
		sc.Width = CellWidthWide
		g.putStored(r, 0, sc)
	} else {
		r.cells[col].Width = CellWidthWide
		r.touch(col + 1)
	}
	if col+2 < len(r.cells) && r.cells[col+1].Width == CellWidthWide {
		blankHalf(&r.cells[col+2])
	}
	g.styles.retain(r.cells[col].Style)
	g.putStored(r, col+1, storedCell{Style: r.cells[col].Style, Char: ' ', Width: CellWidthContinuation, Link: r.cells[col].Link})
	r.flags |= RowDirty | RowHasCluster
	g.CursorCol = col + 2
	g.wrapPending = false
	if g.CursorCol >= g.Cols {
		g.wrapPending = g.autoWrap
		g.CursorCol = g.Cols - 1
	}
}

// putStored stores sc (whose style reference the caller transfers in) at col,
// releasing the overwritten cell's reference.
func (g *Grid) putStored(r *Row, col int, sc storedCell) {
	old := r.cells[col].Style
	r.cells[col] = sc
	g.styles.release(old)
	r.flags |= RowDirty
	r.touch(col + 1)
	if col == len(r.cells)-1 {
		r.flags &^= rowWrapFlags
	}
}

// blankHalf turns the surviving half of a split wide character into a blank,
// keeping its style (and so its style reference and background).
func blankHalf(c *storedCell) {
	c.Char, c.Width, c.Grapheme, c.Link = ' ', CellWidthNormal, 0, 0
}

// clearOrphans blanks the halves of wide characters that a write of width w
// at col would split: the base left of an overwritten continuation cell and
// the continuation right of an overwritten base (xterm behaviour).
func (g *Grid) clearOrphans(r *Row, col, w int) {
	c := r.cells
	if col > 0 && c[col].Width == CellWidthContinuation {
		blankHalf(&c[col-1])
	}
	if end := col + w - 1; end < len(c) && end+1 < len(c) && c[end].Width == CellWidthWide {
		blankHalf(&c[end+1])
	}
	r.flags |= RowDirty
}

// internGrapheme returns a compact id for a grapheme cluster (base rune plus
// combining/ZWJ codepoints), deduplicating repeats. A single-rune cluster
// returns 0 (meaning "use Cell.Char, no combining"). Ids nothing references
// any more are swept when the table doubles, so churned clusters cannot grow
// it without bound.
func (g *Grid) internGrapheme(cluster []rune) uint32 {
	if len(cluster) <= 1 {
		return 0
	}
	if len(cluster) > MaxClusterRunes {
		cluster = cluster[:MaxClusterRunes]
	}
	key := string(cluster)
	if g.graphemeMap == nil {
		g.graphemeMap = make(map[string]uint32)
		g.graphemes = make([][]rune, 1) // index 0 reserved for "none"
	}
	if id, ok := g.graphemeMap[key]; ok {
		return id
	}
	if len(g.graphemes) >= max(g.graphemeGCAt, graphemeGCMin) {
		g.sweepGraphemes()
	}
	cp := make([]rune, len(cluster))
	copy(cp, cluster)
	id := uint32(len(g.graphemes))
	g.graphemes = append(g.graphemes, cp)
	g.graphemeMap[key] = id
	return id
}

// sweepGraphemes rebuilds the intern table from the ids still stored in
// scrollback and the screen, renumbering them densely. Rows without clusters
// are skipped by flag. The next sweep happens once the table doubles again.
func (g *Grid) sweepGraphemes() {
	remap := make([]uint32, len(g.graphemes))
	next := make([][]rune, 1, 64)
	byKey := make(map[string]uint32)
	visit := func(rows []*Row) {
		for _, r := range rows {
			if r.flags&RowHasCluster == 0 {
				continue
			}
			for i := range r.cells {
				id := r.cells[i].Grapheme
				if id == 0 || int(id) >= len(remap) {
					continue
				}
				if remap[id] == 0 {
					cl := g.graphemes[id]
					remap[id] = uint32(len(next))
					next = append(next, cl)
					byKey[string(cl)] = remap[id]
				}
				r.cells[i].Grapheme = remap[id]
			}
		}
	}
	visit(g.history)
	visit(g.rows)
	g.graphemes, g.graphemeMap = next, byKey
	g.graphemeGCAt = 2 * len(next)
}

// emojiKeycapWidth is 2 for keycap bases, which uniseg leaves narrow in
// emoji presentation (base + VS16), 0 otherwise.
func emojiKeycapWidth(base rune) int {
	if base >= '0' && base <= '9' || base == '#' || base == '*' {
		return 2
	}
	return 0
}
