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

// Margins and line sizes don't mix (VT510 DECLRMM; xterm's repaint_line):
// setting DECLRMM makes every line on the page single width, and DECDWL/DECDHL
// do nothing while it is set. DECSWL still works.
func TestDECLRMMFlattensLineSizes(t *testing.T) {
	term := NewTerminal(10, 4)
	feed(term, "\x1b#6\r\n\x1b#3\r\n\x1b#4")
	feed(term, "\x1b[?69h")
	for row := range 3 {
		if got := term.Grid.LineSize(row); got != grid.LineSingle {
			t.Errorf("row %d = %v after DECLRMM set, want single", row, got)
		}
	}
}

func TestDECDWLIgnoredUnderDECLRMM(t *testing.T) {
	term := NewTerminal(10, 4)
	feed(term, "\x1b[?69h\x1b[3;8s\x1b[1;3H\x1b#6\x1b#3\x1b#4abcdef")
	g := term.Grid
	if got := g.LineSize(0); got != grid.LineSingle {
		t.Fatalf("line size %v set while DECLRMM is on", got)
	}
	if c := g.GetCell(7, 0).Char; c != 'f' {
		t.Errorf("row 0 col 7 = %q, want text running to the right margin", c)
	}
	feed(term, "\x1b[?69l\x1b#6")
	if got := g.LineSize(0); got != grid.LineDoubleWidth {
		t.Errorf("DECDWL after DECLRMM reset = %v", got)
	}
}

func TestDECLRMMFlattensMainScreenFromAltScreen(t *testing.T) {
	term := NewTerminal(10, 4)
	feed(term, "\x1b#6\x1b[?1049h\x1b[?69h\x1b[?69l\x1b[?1049l")
	if got := term.Grid.LineSize(0); got != grid.LineSingle {
		t.Fatalf("main-screen row kept %v through DECLRMM set on the alt screen", got)
	}
}

// IL inside DECSLRM margins moves an OSC 66 block that lies inside them.
func TestOSC66BlockFollowsMarginScroll(t *testing.T) {
	term := NewTerminal(20, 6)
	feed(term, "\x1b[2;4H")
	term.Process(osc("66;s=2;Hi")) // cols 3-6, rows 1-2
	feed(term, "\x1b[?69h\x1b[2;12s\x1b[1;2H\x1b[L")
	if got := sizedAt(term, 1); len(got) != 0 {
		t.Fatalf("row 1 kept %+v after IL", got)
	}
	for row, sub := range map[int]int{2: 0, 3: 1} {
		got := sizedAt(term, row)
		if len(got) != 1 || got[0].Col != 3 || got[0].SubRow != sub {
			t.Fatalf("row %d = %+v, want the block's sub-row %d", row, got, sub)
		}
	}
	feed(term, "\x1b[3;6H\x1b[2@") // ICH on one row of the two-row block
	for row := 2; row <= 3; row++ {
		if got := sizedAt(term, row); len(got) != 0 {
			t.Fatalf("row %d kept a torn block %+v", row, got)
		}
	}
}
