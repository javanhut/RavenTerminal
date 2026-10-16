package grid

import "slices"

// SizedText is one row's share of an OSC 66 (kitty text sizing) block: Text
// drawn at Scale (times Num/Den when both are set) across Cells columns and
// Scale rows. Every row of the block carries an entry; SubRow says which row
// of the block it is, so the renderer can draw the part that falls on it.
type SizedText struct {
	Col      int
	SubRow   int
	Cells    int
	Scale    int
	Num, Den int // fractional scale, 0 = none
	VAlign   int // 0 top, 1 bottom, 2 centre
	HAlign   int // 0 left, 1 right, 2 centre
	Text     string
}

// PutSizedTextLocked records a sized-text block whose top-left cell is
// (col, row) on the active screen. Rows below the screen are dropped. The
// cells themselves are written by the caller. Caller holds g.mu.
func (g *Grid) PutSizedTextLocked(col, row int, st SizedText) {
	st.Col, st.Scale = col, max(st.Scale, 1)
	for i := range st.Scale {
		r := row + i
		if r < 0 || r >= len(g.rows) {
			continue
		}
		e := st
		e.SubRow = i
		rr := g.rows[r]
		rr.sized = append(rr.sized, e)
		rr.flags |= RowDirty
	}
}

// dropSized removes the blocks overlapping columns [col0, col1) of an active
// row, together with their entries on the block's other rows.
func (g *Grid) dropSized(row, col0, col1 int) {
	r := g.rows[row]
	for i := 0; i < len(r.sized); {
		e := r.sized[i]
		if e.Col >= col1 || e.Col+e.Cells <= col0 {
			i++
			continue
		}
		top := row - e.SubRow
		for k := range e.Scale {
			if rr := top + k; rr >= 0 && rr < len(g.rows) && rr != row {
				g.removeSized(g.rows[rr], e, k)
			}
		}
		r.sized = slices.Delete(r.sized, i, i+1)
		r.flags |= RowDirty
	}
}

// removeSized deletes the entry of block e at sub-row k from r.
func (g *Grid) removeSized(r *Row, e SizedText, k int) {
	for i, x := range r.sized {
		if x.Col == e.Col && x.SubRow == k && x.Cells == e.Cells && x.Scale == e.Scale && x.Text == e.Text {
			r.sized = slices.Delete(r.sized, i, i+1)
			r.flags |= RowDirty
			return
		}
	}
}

// clearSized drops every block touching an active row.
func (g *Grid) clearSized(row int) {
	if len(g.rows[row].sized) > 0 {
		g.dropSized(row, 0, max(g.Cols, len(g.rows[row].cells)))
	}
}

// keepSizedFitting returns the entries of src that lie within cols columns.
func keepSizedFitting(src []SizedText, cols int) []SizedText {
	var out []SizedText
	for _, e := range src {
		if e.Col+e.Cells <= cols {
			out = append(out, e)
		}
	}
	return out
}

// SizedText returns the sized-text entries of a visible row.
func (s *Snapshot) SizedText(row int) []SizedText {
	if row < 0 || row >= len(s.Sized) {
		return nil
	}
	return s.Sized[row]
}
