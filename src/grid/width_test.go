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
