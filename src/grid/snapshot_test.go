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
	for row := 0; row < 24; row++ {
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
