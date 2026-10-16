package grid

import "testing"

func TestBlockSelectionTextAndHighlight(t *testing.T) {
	g := NewGrid(10, 4)
	writeLines(g, "abcdef", "ghijkl", "mn")
	g.StartSelection(4, 0)
	g.ExtendSelection(1, 2)
	g.SetSelectionBlock(true)
	if !g.SelectionBlock() {
		t.Fatal("SelectionBlock not set")
	}
	if got := g.SelectedText(); got != "bcde\nhijk\nn" {
		t.Fatalf("block text = %q", got)
	}
	if g.IsSelected(0, 1) || !g.IsSelected(2, 1) || g.IsSelected(5, 0) {
		t.Fatal("IsSelected ignores the rectangle")
	}
	snap := g.Snapshot(nil)
	if snap.Selected(0, 1) || !snap.Selected(1, 1) || !snap.Selected(4, 2) || snap.Selected(5, 2) {
		t.Fatal("snapshot highlight ignores the rectangle")
	}
	g.StartSelection(0, 0)
	if g.SelectionBlock() {
		t.Fatal("a new selection must start linear")
	}
}

func TestTextRangeAndAbsSelection(t *testing.T) {
	g := NewGrid(10, 3)
	writeLines(g, "one", "two", "three", "four", "five")
	first, last := g.AbsBounds()
	if last-first != 4 {
		t.Fatalf("AbsBounds = %d..%d, want 5 rows", first, last)
	}
	if got := g.TextRange(first+1, 0, first+3, 9, false); got != "two\nthree\nfour" {
		t.Fatalf("TextRange = %q", got)
	}
	if got := g.TextRange(first+1, 1, first+2, 2, true); got != "wo\nhr" {
		t.Fatalf("block TextRange = %q", got)
	}
	g.SetSelectionAbs(first, 0, first, 2, false)
	if got := g.SelectedText(); got != "one" {
		t.Fatalf("SetSelectionAbs text = %q", got)
	}
	cells := g.RowCells(first + 2)
	if len(cells) == 0 || cells[0].Char != 't' || cells[4].Char != 'e' {
		t.Fatalf("RowCells = %v", cells)
	}
	if g.RowCells(last+5) != nil {
		t.Fatal("RowCells past the buffer must be nil")
	}
}

func TestScrollViewToAbs(t *testing.T) {
	g := NewGrid(10, 3)
	for range 10 {
		writeLines(g, "x")
		g.CarriageReturn()
		g.Newline()
	}
	first, _ := g.AbsBounds()
	g.ScrollViewToAbs(first + 2)
	if g.ViewTopAbs() != first+2 {
		t.Fatalf("ViewTopAbs = %d, want %d", g.ViewTopAbs(), first+2)
	}
	g.ScrollViewToAbs(first + 1000)
	if g.GetScrollOffset() != 0 {
		t.Fatal("scrolling past the end must clamp to the live screen")
	}
}
