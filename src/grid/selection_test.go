package grid

import "testing"

// writeLines writes each string as its own hard line (CR+LF between lines),
// mirroring cooked shell output.
func writeLines(g *Grid, lines ...string) {
	for i, s := range lines {
		writeStr(g, s)
		if i < len(lines)-1 {
			g.CarriageReturn()
			g.Newline()
		}
	}
}

func TestSelectedTextSkipsWideContinuation(t *testing.T) {
	g := NewGrid(10, 3)
	writeStr(g, "世界")
	g.SetSelection(0, 0, 9, 0)
	if got := g.SelectedText(); got != "世界" {
		t.Fatalf("SelectedText = %q, want %q", got, "世界")
	}
}

func TestSelectedTextJoinsSoftWrap(t *testing.T) {
	g := NewGrid(5, 4)
	writeStr(g, "abcdefgh") // wraps: "abcde" + "fgh"
	g.SetSelection(0, 0, 4, 1)
	if got := g.SelectedText(); got != "abcdefgh" {
		t.Fatalf("SelectedText = %q, want %q", got, "abcdefgh")
	}
}

func TestSelectedTextPreservesHardNewline(t *testing.T) {
	g := NewGrid(10, 4)
	writeLines(g, "abc", "def")
	g.SetSelection(0, 0, 9, 1)
	if got := g.SelectedText(); got != "abc\ndef" {
		t.Fatalf("SelectedText = %q, want %q", got, "abc\ndef")
	}
}

func TestSelectionSurvivesViewScroll(t *testing.T) {
	g := NewGrid(10, 3)
	writeLines(g, "L1", "L2", "L3", "L4", "L5", "L6")
	// Screen shows L4,L5,L6; history holds L1..L3.
	g.SetSelection(0, 0, 1, 0) // "L4"
	g.ScrollViewUp(2)
	if got := g.SelectedText(); got != "L4" {
		t.Fatalf("after ScrollViewUp: SelectedText = %q, want %q", got, "L4")
	}
	// viewTop is now L2, so L4 is displayed on viewport row 2.
	if !g.IsSelected(0, 2) {
		t.Fatalf("after ScrollViewUp: IsSelected(0,2) = false, want true")
	}
	if g.IsSelected(0, 0) {
		t.Fatalf("after ScrollViewUp: IsSelected(0,0) = true, want false")
	}
	g.ScrollViewDown(2)
	if got := g.SelectedText(); got != "L4" {
		t.Fatalf("after scroll back: SelectedText = %q, want %q", got, "L4")
	}
	if !g.IsSelected(0, 0) {
		t.Fatalf("after scroll back: IsSelected(0,0) = false, want true")
	}
}

func TestSelectionSurvivesContentScroll(t *testing.T) {
	g := NewGrid(10, 3)
	writeLines(g, "L1", "L2", "L3", "L4", "L5", "L6")
	g.SetSelection(0, 0, 1, 0) // "L4" (screen row 0)
	g.ScrollUp(1)              // L4 scrolls into history
	if got := g.SelectedText(); got != "L4" {
		t.Fatalf("after ScrollUp: SelectedText = %q, want %q", got, "L4")
	}
	// L4 left the screen: no viewport cell is selected at offset 0.
	if g.IsSelected(0, 0) {
		t.Fatalf("after ScrollUp: IsSelected(0,0) = true, want false (row now shows L5)")
	}
	// Scrolling the view up one line brings L4 (and the selection) back.
	g.ScrollViewUp(1)
	if !g.IsSelected(0, 0) {
		t.Fatalf("after ScrollViewUp: IsSelected(0,0) = false, want true")
	}
}

func TestSelectionSpansHistoryAndScreen(t *testing.T) {
	g := NewGrid(10, 3)
	writeLines(g, "L1", "L2", "L3", "L4", "L5", "L6")
	g.ScrollViewUp(3) // view shows L1,L2,L3
	g.StartSelection(0, 0)
	g.ScrollViewDown(3) // view shows L4,L5,L6
	g.ExtendSelection(1, 2)
	want := "L1\nL2\nL3\nL4\nL5\nL6"
	if got := g.SelectedText(); got != want {
		t.Fatalf("SelectedText = %q, want %q", got, want)
	}
}

func TestSelectWordAt(t *testing.T) {
	g := NewGrid(30, 3)
	writeStr(g, "foo bar-baz.qux !hi")
	cases := []struct {
		col  int
		want string
	}{
		{0, "foo"}, // start of line
		{2, "foo"}, // end of word
		{4, "bar-baz.qux"},
		{10, "bar-baz.qux"},
		{16, "!"},  // punctuation: single cell
		{17, "hi"}, // word at end of line
	}
	for _, c := range cases {
		g.SelectWordAt(c.col, 0)
		if got := g.SelectedText(); got != c.want {
			t.Errorf("SelectWordAt(%d,0): SelectedText = %q, want %q", c.col, got, c.want)
		}
	}
}

func TestSelectWordAtCJK(t *testing.T) {
	g := NewGrid(20, 3)
	writeStr(g, "ab 世界 cd")
	g.SelectWordAt(3, 0) // base cell of 世
	if got := g.SelectedText(); got != "世界" {
		t.Fatalf("SelectWordAt(3,0): SelectedText = %q, want %q", got, "世界")
	}
	g.SelectWordAt(4, 0) // continuation cell of 世
	if got := g.SelectedText(); got != "世界" {
		t.Fatalf("SelectWordAt(4,0): SelectedText = %q, want %q", got, "世界")
	}
}

func TestSelectLineAtJoinsSoftWrap(t *testing.T) {
	g := NewGrid(5, 5)
	writeLines(g, "xx", "abcdefgh", "yy") // middle line wraps over two rows
	// Display rows: "xx", "abcde"(soft), "fgh", "yy"
	g.SelectLineAt(1, 2) // click on "fgh" row
	if got := g.SelectedText(); got != "abcdefgh" {
		t.Fatalf("SelectLineAt on wrapped tail: SelectedText = %q, want %q", got, "abcdefgh")
	}
	g.SelectLineAt(0, 1) // click on "abcde" row
	if got := g.SelectedText(); got != "abcdefgh" {
		t.Fatalf("SelectLineAt on wrapped head: SelectedText = %q, want %q", got, "abcdefgh")
	}
	g.SelectLineAt(0, 0)
	if got := g.SelectedText(); got != "xx" {
		t.Fatalf("SelectLineAt on hard line: SelectedText = %q, want %q", got, "xx")
	}
}

// The rendered highlight (snapshot) must agree with IsSelected/SelectedText
// after the buffer moves under it, with no explicit sync call in between —
// snapshot bounds are derived, not cached.
func TestSnapshotHighlightTracksBufferMovement(t *testing.T) {
	g := NewGrid(10, 3)
	writeLines(g, "L1", "L2", "L3", "L4", "L5", "L6")
	g.SetSelection(0, 0, 1, 0) // "L4", screen row 0

	check := func(when string, wantRow int) {
		t.Helper()
		s := g.Snapshot(nil)
		for row := range 3 {
			want := wantRow == row
			if got := s.Selected(0, row); got != want {
				t.Fatalf("%s: snapshot.Selected(0,%d) = %v, want %v", when, row, got, want)
			}
			if got := g.IsSelected(0, row); got != want {
				t.Fatalf("%s: IsSelected(0,%d) = %v, want %v", when, row, got, want)
			}
		}
	}

	check("initial", 0)
	g.ScrollViewUp(2) // L4 is now on viewport row 2
	check("after view scroll", 2)
	g.ScrollViewDown(2)
	g.ScrollUp(1) // new output pushes L4 off the screen
	check("after content scroll", -1)
	g.ScrollViewUp(1) // scrolled back into view on row 0
	check("after scrolling back", 0)
}

func TestSelectionClearedOnResize(t *testing.T) {
	g := NewGrid(10, 3)
	writeStr(g, "hello")
	g.SetSelection(0, 0, 4, 0)
	g.Resize(12, 4)
	if g.HasSelection() {
		t.Fatalf("selection should be cleared by resize/reflow")
	}
}

// A row that once soft-wrapped but was redrawn shorter (readline / TUI style:
// reposition, rewrite, erase to end of line) must be its own line again.
func TestSelectedTextEraseToEndClearsSoftWrap(t *testing.T) {
	g := NewGrid(10, 4)
	writeStr(g, "aaaaaaaaaaBB") // row 0 wraps onto row 1
	g.SetCursorPos(1, 1)
	writeStr(g, "short")
	g.ClearLineToEnd()
	g.SetSelection(0, 0, 9, 1)
	if got, want := g.SelectedText(), "short\nBB"; got != want {
		t.Fatalf("SelectedText = %q, want %q", got, want)
	}
}

// Overwriting the last column ends the earlier soft wrap; a following CR/LF
// makes the row a hard line, and a real overflow re-marks it.
func TestSelectedTextOverwriteLastColumnClearsSoftWrap(t *testing.T) {
	g := NewGrid(5, 4)
	writeStr(g, "abcdefgh") // "abcde" (wrapped) + "fgh"
	g.SetCursorPos(5, 1)
	writeStr(g, "X")
	g.CarriageReturn()
	g.Newline()
	g.SetSelection(0, 0, 4, 1)
	if got, want := g.SelectedText(), "abcdX\nfgh"; got != want {
		t.Fatalf("SelectedText = %q, want %q", got, want)
	}

	// Overflowing again re-marks the row as wrapped.
	g.SetCursorPos(5, 1)
	writeStr(g, "YZ")
	g.SetSelection(0, 0, 4, 1)
	if got, want := g.SelectedText(), "abcdYZgh"; got != want {
		t.Fatalf("after re-wrap SelectedText = %q, want %q", got, want)
	}
}

// DeleteLines / InsertLines move rows within the scroll region; the soft-wrap
// mark belongs to the content and must move with it rather than stay behind.
func TestSelectedTextDeleteInsertLinesMoveSoftWrap(t *testing.T) {
	g := NewGrid(5, 4)
	writeStr(g, "abcdefgh") // rows 0-1: "abcde"(wrapped) "fgh"
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "xy") // row 2
	g.SetCursorPos(1, 1)
	g.DeleteLines(1) // rows: "fgh" "xy" ""
	g.SetSelection(0, 0, 4, 1)
	if got, want := g.SelectedText(), "fgh\nxy"; got != want {
		t.Fatalf("after DL SelectedText = %q, want %q", got, want)
	}

	g = NewGrid(5, 4)
	writeStr(g, "abcdefgh")
	g.SetCursorPos(1, 1)
	g.InsertLines(1) // rows: "" "abcde"(wrapped) "fgh"
	g.SetSelection(0, 0, 4, 2)
	if got, want := g.SelectedText(), "\nabcdefgh"; got != want {
		t.Fatalf("after IL SelectedText = %q, want %q", got, want)
	}
}
