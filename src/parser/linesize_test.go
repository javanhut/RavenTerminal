package parser

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

func TestDECDHLAndDECDWLSetLineSize(t *testing.T) {
	term := NewTerminal(20, 5)
	feed(term, "\x1b#3top\r\n\x1b#4bot\r\n\x1b#6wide\r\n\x1b#6\x1b#5single")
	g := term.Grid
	want := []grid.LineSize{grid.LineDoubleTop, grid.LineDoubleBottom, grid.LineDoubleWidth, grid.LineSingle}
	for row, w := range want {
		if got := g.LineSize(row); got != w {
			t.Errorf("row %d size = %v, want %v", row, got, w)
		}
	}
}

func TestDECDWLWrapsAtHalfWidth(t *testing.T) {
	term := NewTerminal(10, 3)
	feed(term, "\x1b#6abcdefg")
	if c := term.Grid.GetCell(4, 0).Char; c != 'e' {
		t.Fatalf("col 4 = %q", c)
	}
	if c := term.Grid.GetCell(0, 1).Char; c != 'f' {
		t.Fatalf("wrap did not happen at cols/2: row1 col0 = %q", c)
	}
	feed(term, "\x1b[1;9H")
	if col, row := term.Grid.GetCursor(); col != 4 || row != 0 {
		t.Fatalf("CUP into right half: (%d,%d), want (4,0)", col, row)
	}
}

func TestDECALNResetsLineSize(t *testing.T) {
	term := NewTerminal(10, 3)
	feed(term, "\x1b#6\x1b#8")
	if term.Grid.LineSize(0) != grid.LineSingle {
		t.Fatal("DECALN kept double width")
	}
}

// With DECSLRM margins 3-8 on a 10-column screen, a double-width row ends
// at column 5: the cursor stops there and text wraps to the left margin of
// the next row, not to column 1 as the grid's own wrap would.
func TestDECDWLWrapsInsideMargins(t *testing.T) {
	term := NewTerminal(10, 4)
	feed(term, "\x1b[?69h\x1b[3;8s\x1b[1;3H\x1b#6abc")
	g := term.Grid
	for col, want := range map[int]rune{2: 'a', 3: 'b', 4: 'c'} {
		if c := g.GetCell(col, 0).Char; c != want {
			t.Errorf("row 0 col %d = %q, want %q", col, c, want)
		}
	}
	if col, row := g.GetCursor(); col != 4 || row != 0 {
		t.Errorf("after abc the cursor is at (%d,%d), want the row's last usable column (4,0)", col, row)
	}
	feed(term, "d")
	if c := g.GetCell(2, 1).Char; c != 'd' {
		t.Errorf("row 1 col 2 = %q, want 'd' wrapped to the left margin (row 1 col 0 = %q)", c, g.GetCell(0, 1).Char)
	}
}

func TestDECDWLWideCharWrapsInsideMargins(t *testing.T) {
	term := NewTerminal(10, 4)
	feed(term, "\x1b[?69h\x1b[3;8s\x1b[1;3H\x1b#6ab中")
	if c := term.Grid.GetCell(2, 1).Char; c != '中' {
		t.Errorf("row 1 col 2 = %q, want the wide char wrapped to the left margin (row 1 col 0 = %q)", c, term.Grid.GetCell(0, 1).Char)
	}
}
