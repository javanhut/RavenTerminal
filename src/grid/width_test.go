package grid

import "testing"

func TestClusterWidth(t *testing.T) {
	cases := []struct {
		cluster string
		want    int
	}{
		{"a", 1},
		{"世", 2},
		{"é", 1},          // precomposed
		{"é", 1},    // e + combining acute
		{"👨‍👩‍👧", 2}, // ZWJ family
		{"🇺🇸", 2},                // regional-indicator flag
	}
	for _, c := range cases {
		if got := ClusterWidth(c.cluster); got != c.want {
			t.Errorf("ClusterWidth(%q) = %d, want %d", c.cluster, got, c.want)
		}
	}
}

func TestRuneWidth(t *testing.T) {
	cases := []struct {
		name string
		r    rune
		want int
	}{
		{"ascii", 'T', 1},
		{"cjk", '好', 2},
		{"nerd branch icon (PUA U+E0A0)", 0xE0A0, 1},
		{"powerline (PUA U+E0B0)", 0xE0B0, 1},
		{"devicon node (PUA U+E718)", 0xE718, 1},
		{"PUA-A plane-15 (U+F0001)", 0xF0001, 1},
		{"combining acute", 0x0301, 0},
		{"variation selector 16", 0xFE0F, 0},
		{"null", 0x0000, 0},
		{"C0 control", 0x0007, 0},
	}
	for _, c := range cases {
		if got := RuneWidth(c.r); got != c.want {
			t.Errorf("RuneWidth(%s, U+%04X) = %d, want %d", c.name, c.r, got, c.want)
		}
	}
}

// A starship-style prompt mixes a Private-Use branch icon with normal text.
// The PUA glyph must advance the cursor by one cell (not be swallowed as a
// zero-width combining mark), keeping the grid column aligned with the app.
func TestPrivateUseIconAdvancesCursor(t *testing.T) {
	g := NewGrid(10, 1)
	for _, r := range []rune{'a', 0xE0A0, 'b'} { // 'a', branch icon, 'b'
		g.WriteChar(r, DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	}
	if col, _ := g.GetCursor(); col != 3 {
		t.Fatalf("cursor col = %d, want 3 (PUA icon must occupy its own cell)", col)
	}
	if c := g.GetCell(1, 0); c.Char != 0xE0A0 {
		t.Fatalf("cell[1] char = U+%04X, want U+E0A0 (icon must own the cell)", c.Char)
	}
}

func TestCombiningAttachesToBase(t *testing.T) {
	g := NewGrid(10, 1)
	g.WriteChar('e', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	g.WriteChar('́', DefaultFg(), DefaultBg(), 0, 0, 0, Color{}) // combining acute
	// Cursor should have advanced only once (combining mark attached, not placed).
	if col, _ := g.GetCursor(); col != 1 {
		t.Fatalf("cursor col = %d, want 1 (combining must not advance)", col)
	}
	c := g.GetCell(0, 0)
	if c.Char != 'e' {
		t.Fatalf("base char = %q, want e", c.Char)
	}
	if len(c.Combining) != 1 || c.Combining[0] != '́' {
		t.Fatalf("combining = %v, want [U+0301]", c.Combining)
	}
}

func TestCombiningInVisibleText(t *testing.T) {
	g := NewGrid(10, 1)
	for _, r := range "éx" {
		g.WriteChar(r, DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	}
	if got := g.VisibleText(); got != "éx" {
		t.Fatalf("VisibleText = %q, want %q", got, "éx")
	}
}

func TestCombiningOnWideChar(t *testing.T) {
	g := NewGrid(10, 1)
	g.WriteChar('世', DefaultFg(), DefaultBg(), 0, 0, 0, Color{}) // wide, cols 0-1
	g.WriteChar('́', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	// Combining should attach to the wide base at col 0, not the continuation.
	c := g.GetCell(0, 0)
	if c.Char != '世' || len(c.Combining) != 1 {
		t.Fatalf("wide base combining wrong: char=%q combining=%v", c.Char, c.Combining)
	}
}
