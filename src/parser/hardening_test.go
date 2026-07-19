package parser

import (
	"bytes"
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

// ITU T.416 colon form with empty colorspace ID: 38:2::R:G:B.
// Regression: the empty slot used to shift into G and the trailing param ran
// as a spurious SGR reset.
func TestSGRColonRGBWithColorspace(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b[1m\x1b[38:2::10:20:30mX"))
	cell := term.Grid.GetCell(0, 0)
	if cell.Fg.Type != grid.ColorRGB || cell.Fg.R != 10 || cell.Fg.G != 20 || cell.Fg.B != 30 {
		t.Fatalf("fg = %+v, want rgb(10,20,30)", cell.Fg)
	}
	if cell.Flags&grid.FlagBold == 0 {
		t.Fatal("bold lost: colon RGB form triggered a spurious reset")
	}
}

func TestSGRColonRGBNoColorspace(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b[38:2:10:20:30mX"))
	cell := term.Grid.GetCell(0, 0)
	if cell.Fg.Type != grid.ColorRGB || cell.Fg.R != 10 || cell.Fg.G != 20 || cell.Fg.B != 30 {
		t.Fatalf("fg = %+v, want rgb(10,20,30)", cell.Fg)
	}
}

func TestSGRColon256(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b[38:5:123mX"))
	cell := term.Grid.GetCell(0, 0)
	if cell.Fg.Type != grid.ColorIndexed || cell.Fg.Index != 123 {
		t.Fatalf("fg = %+v, want indexed 123", cell.Fg)
	}
}

// ED 3 (CSI 3 J) must erase scrollback, not push the screen into it.
func TestEraseScrollback(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process([]byte("a\r\nb\r\nc\r\nd\r\ne")) // scrolls a,b into history
	term.Grid.ScrollViewUp(10)
	if term.Grid.GetScrollOffset() == 0 {
		t.Fatal("expected scrollback before ED 3")
	}
	term.Grid.ResetScrollOffset()
	term.Process([]byte("\x1b[3J"))
	term.Grid.ScrollViewUp(10)
	if off := term.Grid.GetScrollOffset(); off != 0 {
		t.Fatalf("scrollback remains after ED 3: offset %d", off)
	}
	if got := lineText(term, 0); got != "c" {
		t.Fatalf("screen row 0 = %q, want %q (ED 3 must not touch the screen)", got, "c")
	}
}

// An unterminated OSC must not buffer the stream unboundedly, and an
// overflowed payload must be discarded, not dispatched truncated.
func TestOSCOverflowBounded(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b]2;"))
	term.Process(bytes.Repeat([]byte{'A'}, maxOSCLen+1024))
	if len(term.oscParams) > maxOSCLen {
		t.Fatalf("oscParams grew past cap: %d", len(term.oscParams))
	}
	term.Process([]byte("\x07")) // terminate
	if title := term.GetWindowTitle(); len(title) >= maxOSCLen-2 {
		t.Fatal("overflowed OSC was dispatched")
	}
	term.Process([]byte("X")) // parser must be back in ground state
	if got := lineText(term, 0); got != "X" {
		t.Fatalf("parser wedged after OSC overflow: row = %q", got)
	}
}

// Oversized CSI params are capped and the sequence is dropped, not executed
// truncated.
func TestCSIOverflowBounded(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b["))
	term.Process(bytes.Repeat([]byte{'1', ';'}, maxCSILen))
	term.Process([]byte("m")) // would be a giant SGR; must be dropped
	if len(term.csiParams) != 0 {
		t.Fatalf("csiParams not cleared: %d", len(term.csiParams))
	}
	term.Process([]byte("X"))
	if got := lineText(term, 0); got != "X" {
		t.Fatalf("parser wedged after CSI overflow: row = %q", got)
	}
}

func TestDCSOverflowBounded(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1bPq"))
	term.Process(bytes.Repeat([]byte{'B'}, maxDCSLen+1024))
	if len(term.dcsParams) > maxDCSLen {
		t.Fatalf("dcsParams grew past cap: %d", len(term.dcsParams))
	}
	term.Process([]byte("\x1b\\X"))
	if got := lineText(term, 0); got != "X" {
		t.Fatalf("parser wedged after DCS overflow: row = %q", got)
	}
}

// REP (CSI b) must repeat a wide char as wide — with continuation cells and
// double cursor advance — not as a desynced single-width cell.
func TestRepeatWideChar(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\xe4\xbd\xa0\x1b[2b")) // 你 then REP 2
	col, _ := term.Grid.GetCursor()
	if col != 6 {
		t.Fatalf("cursor col = %d, want 6", col)
	}
	for i := range 3 {
		c := term.Grid.GetCell(i*2, 0)
		if c.Char != '你' || c.Width != grid.CellWidthWide {
			t.Fatalf("cell %d = char %q width %d, want wide 你", i*2, c.Char, c.Width)
		}
	}
}
