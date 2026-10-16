package grid

import "strings"

// SetSelectionBlock switches the active selection between stream and
// rectangular (block) shape.
func (g *Grid) SetSelectionBlock(on bool) {
	g.mu.Lock()
	g.selBlock = on
	g.mu.Unlock()
}

// SelectionBlock reports whether the selection is rectangular.
func (g *Grid) SelectionBlock() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.selectionActive && g.selBlock
}

// SetSelectionAbs selects between two absolute-row cells.
func (g *Grid) SetSelectionAbs(startAbs, startCol, endAbs, endCol int, block bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Cols == 0 {
		return
	}
	g.selectionActive, g.selBlock = true, block
	g.selAnchorAbsRow, g.selAnchorCol = startAbs, clampInt(startCol, 0, g.Cols-1)
	g.selEndAbsRow, g.selEndCol = endAbs, clampInt(endCol, 0, g.Cols-1)
}

// ViewTopAbs returns the absolute row shown on the top viewport row.
func (g *Grid) ViewTopAbs() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.viewTopAbsLocked()
}

// ScrollViewToAbs scrolls so abs is the top viewport row, clamped to the buffer.
func (g *Grid) ScrollViewToAbs(abs int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.scrollOffset = clampInt(g.scrolledOut+len(g.history)-abs, 0, len(g.history))
	g.snapInvalid = true
}

// RowCells returns the inflated cells of an absolute row, nil when the row is
// outside the buffer. Scrollback rows may be shorter than Cols.
func (g *Grid) RowCells(abs int) []Cell {
	g.mu.RLock()
	defer g.mu.RUnlock()
	r := g.rowAtAbsLocked(abs)
	if r == nil {
		return nil
	}
	out := make([]Cell, len(r.cells))
	for i, sc := range r.cells {
		out[i] = g.inflate(sc)
	}
	return out
}

// RowSoftWrapped reports whether an absolute row continues on the next one.
func (g *Grid) RowSoftWrapped(abs int) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	r := g.rowAtAbsLocked(abs)
	return r != nil && r.flags&RowSoftWrapped != 0
}

// TextRange returns the text between two absolute-row cells, as a stream or
// rectangular selection would copy it, without touching the selection.
func (g *Grid) TextRange(sAbs, sCol, eAbs, eCol int, block bool) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if block {
		return g.blockTextLocked(sAbs, sCol, eAbs, eCol)
	}
	if eAbs < sAbs || (eAbs == sAbs && eCol < sCol) {
		sAbs, sCol, eAbs, eCol = eAbs, eCol, sAbs, sCol
	}
	return g.linearTextLocked(sAbs, sCol, eAbs, eCol)
}

func blockBounds(sRow, sCol, eRow, eCol int) (int, int, int, int) {
	return min(sRow, eRow), min(sCol, eCol), max(sRow, eRow), max(sCol, eCol)
}

func (g *Grid) cellTextLocked(r *Row, from, to int, line *strings.Builder) {
	for col := max(from, 0); col <= to && col < len(r.cells); col++ {
		if r.cells[col].Width == CellWidthContinuation {
			continue
		}
		cell := g.inflate(r.cells[col])
		ch := cell.Char
		if ch == 0 {
			ch = ' '
		}
		line.WriteRune(ch)
		for _, cm := range cell.Combining {
			line.WriteRune(cm)
		}
	}
}

func (g *Grid) blockTextLocked(sRow, sCol, eRow, eCol int) string {
	sRow, sCol, eRow, eCol = blockBounds(sRow, sCol, eRow, eCol)
	lines := make([]string, 0, eRow-sRow+1)
	for abs := sRow; abs <= eRow; abs++ {
		var line strings.Builder
		if r := g.rowAtAbsLocked(abs); r != nil {
			g.cellTextLocked(r, sCol, eCol, &line)
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// linearTextLocked is SelectedText for an ordered stream range.
func (g *Grid) linearTextLocked(sRow, sCol, eRow, eCol int) string {
	var out, line strings.Builder
	for abs := sRow; abs <= eRow; abs++ {
		r := g.rowAtAbsLocked(abs)
		if r == nil {
			continue
		}
		colStart, colEnd := 0, len(r.cells)-1
		if abs == sRow {
			colStart = max(sCol, 0)
		}
		if abs == eRow {
			colEnd = min(eCol, len(r.cells)-1)
		}
		g.cellTextLocked(r, colStart, colEnd, &line)
		if r.flags&RowSoftWrapped == 0 || abs >= eRow {
			out.WriteString(strings.TrimRight(line.String(), " "))
			line.Reset()
			if abs < eRow {
				out.WriteByte('\n')
			}
		}
	}
	return strings.TrimRight(out.String(), "\n")
}
