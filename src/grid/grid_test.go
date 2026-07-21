package grid

import (
	"strings"
	"testing"
)

// writeStr writes a plain ASCII string to the grid using default attributes.
// It mirrors how the parser drives WriteChar so tests exercise the real path.
func writeStr(g *Grid, s string) {
	for _, r := range s {
		g.WriteChar(r, DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	}
}

// line returns the trimmed text of a single display row.
func line(g *Grid, row int) string {
	var b strings.Builder
	for col := 0; col < g.Cols; col++ {
		c := g.DisplayCell(col, row).Char
		if c == 0 {
			c = ' '
		}
		b.WriteRune(c)
	}
	return strings.TrimRight(b.String(), " ")
}

func TestWriteAndRead(t *testing.T) {
	g := NewGrid(20, 5)
	writeStr(g, "hello")
	if got := line(g, 0); got != "hello" {
		t.Fatalf("line 0 = %q, want %q", got, "hello")
	}
	col, row := g.GetCursor()
	if col != 5 || row != 0 {
		t.Fatalf("cursor = (%d,%d), want (5,0)", col, row)
	}
}

func TestAutoWrap(t *testing.T) {
	g := NewGrid(5, 3)
	writeStr(g, "abcdef") // 6 chars into 5 cols -> wraps
	if got := line(g, 0); got != "abcde" {
		t.Fatalf("line 0 = %q, want %q", got, "abcde")
	}
	if got := line(g, 1); got != "f" {
		t.Fatalf("line 1 = %q, want %q", got, "f")
	}
}

func TestWideChar(t *testing.T) {
	g := NewGrid(10, 2)
	g.WriteChar('世', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	if w := g.GetCell(0, 0).Width; w != CellWidthWide {
		t.Fatalf("cell0 width = %d, want %d", w, CellWidthWide)
	}
	if w := g.GetCell(1, 0).Width; w != CellWidthContinuation {
		t.Fatalf("cell1 width = %d, want %d", w, CellWidthContinuation)
	}
	col, _ := g.GetCursor()
	if col != 2 {
		t.Fatalf("cursor col = %d, want 2 (advanced by wide char)", col)
	}
}

// TestWideCharPadStyleAtWrap covers a wide char written at the last column:
// it wraps, leaving a pad space in the final cell. The pad must take the
// incoming character's style, not the previous cell's (g.lastStyle), or
// full-screen redraws leave stale background colors at the right edge.
func TestWideCharPadStyleAtWrap(t *testing.T) {
	g := NewGrid(5, 2)
	for _, c := range "abcd" {
		g.WriteChar(c, DefaultFg(), IndexedColor(1), 0, 0, 0, Color{})
	}
	g.WriteChar('世', DefaultFg(), IndexedColor(4), 0, 0, 0, Color{})
	pad := g.GetCell(4, 0)
	if pad.Char != ' ' || pad.Bg.Index != 4 {
		t.Fatalf("pad cell = %+v, want space with bg index 4 (incoming style)", pad)
	}
	if got := g.GetCell(0, 1); got.Char != '世' || got.Width != CellWidthWide {
		t.Fatalf("wrapped wide char = %+v, want 世 wide on next row", got)
	}
}

func TestCarriageReturnNewline(t *testing.T) {
	g := NewGrid(10, 3)
	writeStr(g, "abc")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "de")
	if got := line(g, 0); got != "abc" {
		t.Fatalf("line 0 = %q, want %q", got, "abc")
	}
	if got := line(g, 1); got != "de" {
		t.Fatalf("line 1 = %q, want %q", got, "de")
	}
}

func TestScrollOnBottom(t *testing.T) {
	g := NewGrid(4, 2)
	writeStr(g, "aa")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "bb")
	g.CarriageReturn()
	g.Newline() // scrolls: "aa" -> scrollback, "bb" -> row 0
	writeStr(g, "cc")
	if got := line(g, 0); got != "bb" {
		t.Fatalf("after scroll line 0 = %q, want %q", got, "bb")
	}
	if got := line(g, 1); got != "cc" {
		t.Fatalf("after scroll line 1 = %q, want %q", got, "cc")
	}
}

func TestInsertDeleteChars(t *testing.T) {
	g := NewGrid(10, 1)
	writeStr(g, "abcdef")
	g.SetCursorPos(1, 1) // col 0
	g.InsertChars(2)
	if got := line(g, 0); got != "abcdef" {
		// two blanks inserted at front shifts content right
		if got != "  abcdef" {
			t.Fatalf("after InsertChars line = %q", got)
		}
	}
	g2 := NewGrid(10, 1)
	writeStr(g2, "abcdef")
	g2.SetCursorPos(1, 1)
	g2.DeleteChars(2)
	if got := line(g2, 0); got != "cdef" {
		t.Fatalf("after DeleteChars line = %q, want %q", got, "cdef")
	}
}

func TestEraseChars(t *testing.T) {
	g := NewGrid(10, 1)
	writeStr(g, "abcdef")
	g.SetCursorPos(2, 1) // col 1
	g.EraseChars(2)      // erase 'b','c'
	if got := line(g, 0); got != "a" && got != "a  def" {
		t.Fatalf("after EraseChars line = %q", got)
	}
}

func TestClearLineToEnd(t *testing.T) {
	g := NewGrid(10, 1)
	writeStr(g, "abcdef")
	g.SetCursorPos(4, 1) // col 3
	g.ClearLineToEnd()
	if got := line(g, 0); got != "abc" {
		t.Fatalf("after ClearLineToEnd line = %q, want %q", got, "abc")
	}
}

func TestScrollRegionInsertDeleteLines(t *testing.T) {
	g := NewGrid(4, 4)
	writeStr(g, "11")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "22")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "33")
	g.SetCursorPos(1, 1) // home
	g.InsertLines(1)
	if got := line(g, 0); got != "" {
		t.Fatalf("after InsertLines line 0 = %q, want blank", got)
	}
	if got := line(g, 1); got != "11" {
		t.Fatalf("after InsertLines line 1 = %q, want %q", got, "11")
	}
}

func TestVisibleText(t *testing.T) {
	g := NewGrid(10, 3)
	writeStr(g, "abc")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "def")
	if got := g.VisibleText(); got != "abc\ndef" {
		t.Fatalf("VisibleText = %q, want %q", got, "abc\ndef")
	}
}

func TestSelection(t *testing.T) {
	g := NewGrid(10, 2)
	writeStr(g, "hello")
	g.SetSelection(0, 0, 4, 0)
	if !g.HasSelection() {
		t.Fatal("expected active selection")
	}
	if got := g.SelectedText(); got != "hello" {
		t.Fatalf("SelectedText = %q, want %q", got, "hello")
	}
	g.ClearSelection()
	if g.HasSelection() {
		t.Fatal("expected cleared selection")
	}
}

func TestResizeClampsCursor(t *testing.T) {
	g := NewGrid(10, 5)
	g.SetCursorPos(10, 5)
	g.Resize(4, 2)
	col, row := g.GetCursor()
	if col >= 4 || row >= 2 {
		t.Fatalf("cursor (%d,%d) not clamped to 4x2", col, row)
	}
}

func TestScrollbackReadback(t *testing.T) {
	g := NewGrid(4, 2)
	writeStr(g, "aa")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "bb")
	g.CarriageReturn()
	g.Newline() // "aa" -> history
	writeStr(g, "cc")
	// Scroll the view up one line so history row "aa" becomes visible at row 0.
	g.ScrollViewUp(1)
	if got := line(g, 0); got != "aa" {
		t.Fatalf("scrolled-back line 0 = %q, want %q", got, "aa")
	}
	if got := line(g, 1); got != "bb" {
		t.Fatalf("scrolled-back line 1 = %q, want %q", got, "bb")
	}
	g.ResetScrollOffset()
	if got := line(g, 0); got != "bb" {
		t.Fatalf("after reset line 0 = %q, want %q", got, "bb")
	}
}

func TestScrollbackPreservesStyle(t *testing.T) {
	g := NewGrid(4, 2)
	g.WriteChar('Z', IndexedColor(9), DefaultBg(), FlagBold, 0, 0, Color{})
	g.CarriageReturn()
	g.Newline()
	g.CarriageReturn()
	g.Newline() // push the styled row into history
	g.ScrollViewUp(2)
	c := g.DisplayCell(0, 0)
	if c.Char != 'Z' || c.Fg.Index != 9 || c.Flags&FlagBold == 0 {
		t.Fatalf("scrollback cell lost style: %+v", c)
	}
}

func TestAltGridNoScrollback(t *testing.T) {
	g := NewAltGrid(4, 2)
	writeStr(g, "aa")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "bb")
	g.CarriageReturn()
	g.Newline() // would push "aa" to history on a normal grid
	g.ScrollViewUp(5)
	if g.GetScrollOffset() != 0 {
		t.Fatalf("alt grid should have no scrollback; offset = %d", g.GetScrollOffset())
	}
}

// TestAltResizeGrowThenWrite reproduces the panic where the alternate-screen
// clamp resize allocated rows at the old (narrower) width while g.Cols grew,
// causing cellAt to index past the short row when a TUI wrote near the new edge.
func TestAltResizeGrowThenWrite(t *testing.T) {
	g := NewAltGrid(20, 5)
	g.Resize(100, 10) // grow wider (clamp path, no scrollback)
	for i, r := range g.rows {
		if len(r.cells) != g.Cols {
			t.Fatalf("row %d has %d cells, want %d after grow", i, len(r.cells), g.Cols)
		}
	}
	// Absolute-position the cursor near the new right edge and write.
	g.SetCursorPos(95, 5)
	g.WriteChar('X', IndexedColor(2), DefaultBg(), 0, 0, 0, Color{})
	if c := g.GetCell(94, 4).Char; c != 'X' {
		t.Fatalf("write after alt grow = %q, want X", c)
	}
}

// TestReflowGrowThenWriteHighColumn covers the main-screen reflow grow path
// writing at a high column (the 142-vs-74 shape from the crash report).
func TestReflowGrowThenWriteHighColumn(t *testing.T) {
	g := NewGrid(74, 5)
	g.Resize(150, 10)
	g.SetCursorPos(143, 1) // col 142 (1-based)
	g.WriteChar('Y', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	if c := g.GetCell(142, 0).Char; c != 'Y' {
		t.Fatalf("reflow grow write = %q, want Y", c)
	}
}

func TestColorConstructors(t *testing.T) {
	if c := IndexedColor(5); c.Type != ColorIndexed || c.Index != 5 {
		t.Fatalf("IndexedColor wrong: %+v", c)
	}
	if c := RGBColor(1, 2, 3); c.Type != ColorRGB || c.R != 1 || c.G != 2 || c.B != 3 {
		t.Fatalf("RGBColor wrong: %+v", c)
	}
}
