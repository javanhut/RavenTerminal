package render

import "testing"

// The settings input scrolls horizontally by deriving its window from the
// caret, so the caret is always inside the returned slice.
func TestInputWindow(t *testing.T) {
	line := []rune("abcdefghij")
	cases := []struct {
		col, width int
		want       string
		wantCol    int
	}{
		{0, 5, "abcde", 0},       // start of a long line
		{4, 5, "abcde", 4},       // caret at the right edge, not yet scrolled
		{7, 5, "defgh", 4},       // scrolled: caret rides the right edge
		{10, 5, "ghij", 4},       // append position: caret takes the last cell
		{3, 20, "abcdefghij", 3}, // field wider than the text
		{0, 0, "a", 0},           // degenerate width still yields a valid window
	}
	for _, c := range cases {
		vis, col := inputWindow(line, c.col, c.width)
		if string(vis) != c.want || col != c.wantCol {
			t.Errorf("inputWindow(col=%d width=%d) = %q,%d want %q,%d",
				c.col, c.width, string(vis), col, c.want, c.wantCol)
		}
		if col < 0 || col > len(vis) {
			t.Errorf("caret column %d outside window of len %d", col, len(vis))
		}
	}

	// An empty line is a valid state (new value, or a blank script line).
	if vis, col := inputWindow(nil, 0, 8); len(vis) != 0 || col != 0 {
		t.Errorf("empty line = %q,%d", string(vis), col)
	}
}
