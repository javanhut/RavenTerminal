package grid

import "testing"

func TestSnapshotContent(t *testing.T) {
	g := NewGrid(10, 3)
	writeStr(g, "abc")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "de")
	g.SetCursorPos(2, 2) // col 1, row 1

	s := g.Snapshot(nil)
	if s.Cols != 10 || s.Rows != 3 {
		t.Fatalf("snapshot dims = %dx%d", s.Cols, s.Rows)
	}
	if s.At(0, 0).Char != 'a' || s.At(2, 0).Char != 'c' {
		t.Fatalf("row 0 content wrong: %c%c", s.At(0, 0).Char, s.At(2, 0).Char)
	}
	if s.At(1, 1).Char != 'e' {
		t.Fatalf("row 1 content wrong: %c", s.At(1, 1).Char)
	}
	if s.CursorCol != 1 || s.CursorRow != 1 {
		t.Fatalf("cursor = (%d,%d), want (1,1)", s.CursorCol, s.CursorRow)
	}
}

func TestSnapshotStyle(t *testing.T) {
	g := NewGrid(10, 1)
	g.WriteChar('Z', IndexedColor(3), RGBColor(1, 2, 3), FlagBold|FlagUnderline, 0, 2, Color{})
	s := g.Snapshot(nil)
	c := s.At(0, 0)
	if c.Fg.Index != 3 || c.Bg.Type != ColorRGB || c.Flags&FlagBold == 0 || c.UnderlineStyle != 2 {
		t.Fatalf("snapshot did not resolve style: %+v", c)
	}
}

func TestSnapshotSelection(t *testing.T) {
	g := NewGrid(10, 2)
	writeStr(g, "hello")
	g.SetSelection(1, 0, 3, 0)
	s := g.Snapshot(nil)
	if !s.SelActive {
		t.Fatal("snapshot selection inactive")
	}
	if s.Selected(0, 0) || !s.Selected(1, 0) || !s.Selected(3, 0) || s.Selected(4, 0) {
		t.Fatal("snapshot selection range wrong")
	}
}

func TestSnapshotDirtyFastPath(t *testing.T) {
	g := NewGrid(10, 2)
	writeStr(g, "x")
	s := g.Snapshot(nil)
	if !s.Dirty {
		t.Fatal("first snapshot should be dirty")
	}
	// No changes -> next snapshot should report not dirty (reusing buffer).
	s2 := g.Snapshot(s)
	if s2.Dirty {
		t.Fatal("snapshot with no changes should not be dirty")
	}
	// A write marks it dirty again.
	writeStr(g, "y")
	s3 := g.Snapshot(s2)
	if !s3.Dirty {
		t.Fatal("snapshot after a write should be dirty")
	}
}

func TestSnapshotReusesBuffer(t *testing.T) {
	g := NewGrid(8, 4)
	s1 := g.Snapshot(nil)
	s2 := g.Snapshot(s1)
	if s1 != s2 {
		t.Fatal("expected snapshot buffer to be reused (same pointer)")
	}
}

func BenchmarkSnapshot(b *testing.B) {
	g := NewGrid(80, 24)
	for range 24 {
		writeStr(g, "the quick brown fox jumps over the lazy dog 0123456789")
		g.CarriageReturn()
		g.Newline()
	}
	var prev *Snapshot
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prev = g.Snapshot(prev)
	}
}

// assertSnapshotMatchesGrid verifies every visible snapshot cell against the
// grid's own display mapping (the ground truth Snapshot is meant to copy).
func assertSnapshotMatchesGrid(t *testing.T, g *Grid, s *Snapshot) {
	t.Helper()
	if s.Cols != g.Cols || s.Rows != g.Rows {
		t.Fatalf("snapshot dims %dx%d, grid %dx%d", s.Cols, s.Rows, g.Cols, g.Rows)
	}
	for row := 0; row < s.Rows; row++ {
		for col := 0; col < s.Cols; col++ {
			got := s.At(col, row)
			want := g.DisplayCell(col, row)
			if got.Char != want.Char || got.Fg != want.Fg || got.Bg != want.Bg ||
				got.Flags != want.Flags || got.Width != want.Width {
				t.Fatalf("cell (%d,%d): snapshot %+v, grid %+v", col, row, got, want)
			}
		}
	}
}

// TestSnapshotIncrementalDirtyRows: with the view at the bottom and unchanged
// dimensions, a second snapshot copies only dirty rows — clean rows must keep
// their previous contents in the reused buffer (they are not re-inflated).
func TestSnapshotIncrementalDirtyRows(t *testing.T) {
	g := NewGrid(10, 3)
	writeStr(g, "aaaaaaaa")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "bbbbbbbb")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "cccccccc")

	s := g.Snapshot(nil)
	assertSnapshotMatchesGrid(t, g, s)

	// Dirty only row 1; rows 0 and 2 must survive untouched in the buffer.
	g.SetCursorPos(1, 2) // col 0, row 1
	writeStr(g, "B")

	s2 := g.Snapshot(s)
	if s2 != s {
		t.Fatal("expected buffer reuse (same snapshot pointer)")
	}
	if !s2.Dirty {
		t.Fatal("snapshot after write: Dirty = false, want true")
	}
	assertSnapshotMatchesGrid(t, g, s2)
	if got := string([]rune{s2.At(0, 0).Char, s2.At(1, 0).Char}); got != "aa" {
		t.Fatalf("clean row 0 clobbered: %q", got)
	}
	if got := string([]rune{s2.At(0, 2).Char, s2.At(1, 2).Char}); got != "cc" {
		t.Fatalf("clean row 2 clobbered: %q", got)
	}
	if s2.At(0, 1).Char != 'B' {
		t.Fatalf("dirty row 1 not updated: %c", s2.At(0, 1).Char)
	}
}

// TestSnapshotAfterContentScroll: scrolling rotates row pointers; the moved
// rows must be re-copied even though their cell contents did not change.
func TestSnapshotAfterContentScroll(t *testing.T) {
	g := NewGrid(8, 2)
	writeStr(g, "aa")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "bb")

	s := g.Snapshot(nil)
	assertSnapshotMatchesGrid(t, g, s)

	// Scroll the screen up: "aa" enters history, "bb" moves to row 0.
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "cc")

	s2 := g.Snapshot(s)
	assertSnapshotMatchesGrid(t, g, s2)
	if got := s2.At(0, 0).Char; got != 'b' {
		t.Fatalf("row 0 after scroll: %c, want b (stale rotated row)", got)
	}

	// Scroll down (region scroll via SD) must likewise re-copy rotated rows.
	g3 := NewGrid(8, 3)
	writeStr(g3, "x")
	g3.CarriageReturn()
	g3.Newline()
	writeStr(g3, "y")
	s3 := g3.Snapshot(nil)
	g3.ScrollDown(1)
	s4 := g3.Snapshot(s3)
	assertSnapshotMatchesGrid(t, g3, s4)
}

// TestSnapshotViewScroll: scrolling up into history forces a full copy (the
// row mapping changes), and returning to the bottom must not leave stale
// history rows behind in the reused buffer.
func TestSnapshotViewScroll(t *testing.T) {
	g := NewGrid(8, 2)
	writeStr(g, "aa")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "bb")
	g.CarriageReturn()
	g.Newline() // "aa" -> history
	writeStr(g, "cc")

	s := g.Snapshot(nil)
	assertSnapshotMatchesGrid(t, g, s)

	g.ScrollViewUp(1)
	s2 := g.Snapshot(s)
	assertSnapshotMatchesGrid(t, g, s2)
	if got := s2.At(0, 0).Char; got != 'a' {
		t.Fatalf("scrolled view row 0: %c, want a (history)", got)
	}

	g.ResetScrollOffset()
	s3 := g.Snapshot(s2)
	assertSnapshotMatchesGrid(t, g, s3)
	if got := s3.At(0, 0).Char; got != 'b' {
		t.Fatalf("back at bottom row 0: %c, want b (stale history row)", got)
	}
}

// TestSnapshotAfterReflow: resize rebuilds rows from scratch; the next
// snapshot must be a full copy of the reflowed content.
func TestSnapshotAfterReflow(t *testing.T) {
	g := NewGrid(10, 3)
	writeStr(g, "abcdefghijklmnop") // wraps at col 10
	s := g.Snapshot(nil)
	assertSnapshotMatchesGrid(t, g, s)

	g.Resize(8, 3)
	s2 := g.Snapshot(s)
	if s2 == s {
		t.Fatal("resize changed dims: expected a fresh snapshot buffer")
	}
	assertSnapshotMatchesGrid(t, g, s2)

	g.Resize(10, 3)
	s3 := g.Snapshot(s2)
	assertSnapshotMatchesGrid(t, g, s3)
}

// TestSnapshotPartialSequence: a mixed sequence of writes, scrolls, erases,
// and view scrolls, verifying the reused buffer against the grid after every
// step — the incremental path must always match a full copy would.
func TestSnapshotPartialSequence(t *testing.T) {
	g := NewGrid(12, 4)
	var s *Snapshot
	step := func(name string) {
		t.Helper()
		s = g.Snapshot(s)
		assertSnapshotMatchesGrid(t, g, s)
		if s.ScrollOffset == 0 && s.Cols == g.Cols {
			// Buffer reuse is expected throughout (dims never change).
		}
		_ = name
	}

	writeStr(g, "one")
	step("write")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "two")
	step("newline+write")
	// Overflow the screen to force content scrolling.
	for _, w := range []string{"three", "four", "five", "six"} {
		g.CarriageReturn()
		g.Newline()
		writeStr(g, w)
		step("scroll " + w)
	}
	// View scroll into history and back.
	g.ScrollViewUp(2)
	step("view up")
	g.ScrollViewUp(5) // clamps
	step("view up clamp")
	g.ScrollViewDown(1)
	step("view down")
	g.ResetScrollOffset()
	step("view reset")
	// Erase a line (mid-screen edit on an otherwise clean grid).
	g.SetCursorPos(1, 2)
	g.ClearLine()
	step("erase line")
	// Styled write on a single row.
	g.SetCursorPos(1, 3)
	g.WriteChar('Z', IndexedColor(5), DefaultBg(), FlagBold, 0, 0, Color{})
	step("styled write")
}

// BenchmarkSnapshotDirtyRow measures the incremental path: one row written
// per frame on an otherwise clean grid (interactive/steady-state load).
func BenchmarkSnapshotDirtyRow(b *testing.B) {
	g := NewGrid(80, 24)
	for range 24 {
		writeStr(g, "the quick brown fox jumps over the lazy dog 0123456789")
		g.CarriageReturn()
		g.Newline()
	}
	var prev *Snapshot
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.SetCursorPos(1, 5)
		g.WriteChar('x', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
		prev = g.Snapshot(prev)
	}
}
