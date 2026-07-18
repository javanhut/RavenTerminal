package grid

import (
	"reflect"
	"testing"
)

// writeSeq writes runes one at a time through the public per-rune path.
func writeSeq(g *Grid, rs []rune, fg, bg Color, flags CellFlags, link uint16, ulStyle uint8, ulColor Color) {
	for _, r := range rs {
		g.WriteChar(r, fg, bg, flags, link, ulStyle, ulColor)
	}
}

// gridsEqual compares full visible contents, cursor, and wrap-relevant
// observable state of two grids.
func gridsEqual(t *testing.T, a, b *Grid) {
	t.Helper()
	if a.Cols != b.Cols || a.Rows != b.Rows {
		t.Fatalf("size mismatch: %dx%d vs %dx%d", a.Cols, a.Rows, b.Cols, b.Rows)
	}
	for row := 0; row < a.Rows; row++ {
		for col := 0; col < a.Cols; col++ {
			ca, cb := a.GetCell(col, row), b.GetCell(col, row)
			if !reflect.DeepEqual(ca, cb) {
				t.Fatalf("cell (%d,%d) mismatch: %+v vs %+v", col, row, ca, cb)
			}
		}
	}
	aCol, aRow := a.GetCursor()
	bCol, bRow := b.GetCursor()
	if aCol != bCol || aRow != bRow {
		t.Fatalf("cursor mismatch: (%d,%d) vs (%d,%d)", aCol, aRow, bCol, bRow)
	}
	if a.VisibleText() != b.VisibleText() {
		t.Fatalf("text mismatch:\n%q\nvs\n%q", a.VisibleText(), b.VisibleText())
	}
}

// TestWriteRunesMatchesWriteChar is the batch-path equivalence property test:
// the same rune sequence through the per-rune path and the batch path must
// yield identical grid contents and cursor state — including link and
// underline attributes.
func TestWriteRunesMatchesWriteChar(t *testing.T) {
	red := RGBColor(200, 30, 30)
	cases := []struct {
		name    string
		runes   []rune
		fg      Color
		bg      Color
		flags   CellFlags
		link    bool // intern a hyperlink and write with its id
		ulStyle uint8
		ulColor Color
	}{
		{name: "ascii", runes: []rune("hello world"), fg: DefaultFg(), bg: DefaultBg()},
		{name: "styled", runes: []rune("styled text"), fg: red, bg: IndexedColor(4), flags: FlagBold | FlagUnderline},
		{name: "underline-attrs", runes: []rune("curly link"), fg: DefaultFg(), bg: DefaultBg(),
			flags: FlagUnderline, link: true, ulStyle: 3, ulColor: RGBColor(10, 20, 30)},
		{name: "wide", runes: []rune("ab世界 cd"), fg: DefaultFg(), bg: DefaultBg()},
		{name: "combining", runes: []rune("éx"), fg: DefaultFg(), bg: DefaultBg()},
		{name: "wrap", runes: []rune("0123456789012345678901234567890123456789012345"), fg: DefaultFg(), bg: DefaultBg()},
		{name: "wide-at-edge", runes: append([]rune("0123456789012345678"), '世', '界'), fg: DefaultFg(), bg: DefaultBg()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g1 := NewGrid(20, 5)
			g2 := NewGrid(20, 5)
			var link1, link2 uint16
			if tc.link {
				link1 = g1.InternLink("https://example.com")
				link2 = g2.InternLink("https://example.com")
			}
			writeSeq(g1, tc.runes, tc.fg, tc.bg, tc.flags, link1, tc.ulStyle, tc.ulColor)
			g2.WriteRunes(tc.runes, tc.fg, tc.bg, tc.flags, link2, tc.ulStyle, tc.ulColor)
			gridsEqual(t, g1, g2)
		})
	}
}

// TestWriteRunesSplitEquivalence: splitting one sequence across multiple
// WriteRunes calls behaves like a single call (chunk boundaries are invisible).
func TestWriteRunesSplitEquivalence(t *testing.T) {
	rs := []rune("the quick brown fox jumps over the lazy dog")
	g1 := NewGrid(15, 6)
	g2 := NewGrid(15, 6)
	g1.WriteRunes(rs, DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	for i := 0; i < len(rs); i += 7 {
		end := min(i+7, len(rs))
		g2.WriteRunes(rs[i:end], DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	}
	gridsEqual(t, g1, g2)
}

// TestSnapshotDirtySemantics: Dirty is true after a write, false after a
// snapshot with no intervening writes, and RowDirty flags are cleared by the
// snapshot (so a second snapshot sees a clean grid).
func TestSnapshotDirtySemantics(t *testing.T) {
	g := NewGrid(10, 4)
	g.WriteChar('a', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})

	s := g.Snapshot(nil)
	if !s.Dirty {
		t.Fatal("snapshot after write: Dirty = false, want true")
	}
	s2 := g.Snapshot(s)
	if s2.Dirty {
		t.Fatal("snapshot with no intervening writes: Dirty = true, want false")
	}

	g.WriteChar('b', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	s3 := g.Snapshot(s2)
	if !s3.Dirty {
		t.Fatal("snapshot after second write: Dirty = false, want true (RowDirty must survive until snapshot)")
	}
}

// TestRedrawNeeded verifies the non-clearing dirty peek the render loop uses
// to skip idle frames.
func TestRedrawNeeded(t *testing.T) {
	g := NewGrid(10, 4)
	if !g.RedrawNeeded() {
		t.Fatal("fresh grid (never snapshotted): RedrawNeeded = false, want true")
	}
	g.Snapshot(nil)
	if g.RedrawNeeded() {
		t.Fatal("just snapshotted, no changes: RedrawNeeded = true, want false")
	}

	// Content write.
	g.WriteChar('x', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	if !g.RedrawNeeded() {
		t.Fatal("after write: RedrawNeeded = false, want true")
	}
	// Peek must NOT clear: still true, and the next snapshot still sees dirty.
	if !g.RedrawNeeded() {
		t.Fatal("second peek: RedrawNeeded = false, want true (peek must not clear)")
	}
	if s := g.Snapshot(nil); !s.Dirty {
		t.Fatal("snapshot after peek: Dirty = false, want true")
	}
	if g.RedrawNeeded() {
		t.Fatal("after snapshot: RedrawNeeded = true, want false")
	}

	// Cursor move alone.
	g.SetCursorPos(5, 2)
	if !g.RedrawNeeded() {
		t.Fatal("after cursor move: RedrawNeeded = false, want true")
	}
	g.Snapshot(nil)

	// Scrollback offset change alone.
	writeStr(g, "abc")
	g.Newline()
	g.Snapshot(nil)
	g.ScrollViewUp(1) // no history yet; offset clamps to 0 => no change
	if g.RedrawNeeded() {
		t.Fatal("clamped scroll (no-op): RedrawNeeded = true, want false")
	}

	// Selection change alone.
	g.SetSelection(0, 0, 3, 0)
	if !g.RedrawNeeded() {
		t.Fatal("after selection: RedrawNeeded = false, want true")
	}
	g.Snapshot(nil)
	if g.RedrawNeeded() {
		t.Fatal("selection snapshotted: RedrawNeeded = true, want false")
	}
	g.ClearSelection()
	if !g.RedrawNeeded() {
		t.Fatal("after clearing selection: RedrawNeeded = false, want true")
	}
}
