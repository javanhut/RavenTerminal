package parser

import (
	"testing"
)

// captureResponses wires a responseWriter that accumulates all bytes the
// terminal writes back to the host (DSR/DA/OSC query replies).
func captureResponses(t *Terminal) *[]byte {
	out := &[]byte{}
	t.SetResponseWriter(func(b []byte) {
		*out = append(*out, b...)
	})
	return out
}

func TestDeviceAttributes(t *testing.T) {
	term := NewTerminal(10, 1)
	out := captureResponses(term)
	term.Process([]byte("\x1b[c"))
	if string(*out) != "\x1b[?62;22c" {
		t.Fatalf("DA reply = %q, want %q", string(*out), "\x1b[?62;22c")
	}
}

func TestSecondaryDeviceAttributes(t *testing.T) {
	term := NewTerminal(10, 1)
	out := captureResponses(term)
	term.Process([]byte("\x1b[>c"))
	if string(*out) != "\x1b[>0;136;0c" {
		t.Fatalf("secondary DA reply = %q", string(*out))
	}
}

func TestDeviceStatusReport(t *testing.T) {
	term := NewTerminal(10, 5)
	out := captureResponses(term)
	term.Process([]byte("\x1b[5n")) // status
	if string(*out) != "\x1b[0n" {
		t.Fatalf("DSR status reply = %q, want %q", string(*out), "\x1b[0n")
	}
}

func TestCursorPositionReport(t *testing.T) {
	term := NewTerminal(20, 10)
	out := captureResponses(term)
	term.Process([]byte("\x1b[3;5H")) // move to row 3 col 5
	term.Process([]byte("\x1b[6n"))   // request CPR
	if string(*out) != "\x1b[3;5R" {
		t.Fatalf("CPR reply = %q, want %q", string(*out), "\x1b[3;5R")
	}
}

func TestDECALN(t *testing.T) {
	term := NewTerminal(5, 3)
	term.Process([]byte("\x1b#8")) // DECALN: fill with 'E'
	for row := range 3 {
		for col := range 5 {
			if c := term.Grid.GetCell(col, row).Char; c != 'E' {
				t.Fatalf("DECALN cell (%d,%d) = %q, want E", col, row, c)
			}
		}
	}
}

func TestCustomTabStops(t *testing.T) {
	term := NewTerminal(40, 1)
	// Default stops every 8: tab from col 0 -> col 8.
	term.Process([]byte("\t"))
	if col, _ := term.Grid.GetCursor(); col != 8 {
		t.Fatalf("default tab -> col %d, want 8", col)
	}
	// Clear all stops, set one at col 20, return home, tab -> col 20.
	term.Process([]byte("\x1b[3g"))  // TBC all
	term.Process([]byte("\x1b[21G")) // CHA to col 21 (1-based) -> col 20
	term.Process([]byte("\x1bH"))    // HTS at col 20
	term.Process([]byte("\r"))       // CR to col 0
	term.Process([]byte("\t"))
	if col, _ := term.Grid.GetCursor(); col != 20 {
		t.Fatalf("custom tab -> col %d, want 20", col)
	}
}

func TestInsertMode(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("ABC"))
	term.Process([]byte("\x1b[H"))  // home (col 0)
	term.Process([]byte("\x1b[4h")) // IRM on
	term.Process([]byte("X"))       // insert X, shifting ABC right
	if got := lineText(term, 0); got != "XABC" {
		t.Fatalf("insert mode line = %q, want %q", got, "XABC")
	}
}

func TestLineFeedNewLineMode(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process([]byte("\x1b[20h")) // LNM on
	term.Process([]byte("ab\nc"))    // LF should also CR
	if got := lineText(term, 1); got != "c" {
		t.Fatalf("LNM line 1 = %q, want %q (LF performed CR)", got, "c")
	}
}

func TestOSC11BackgroundQuery(t *testing.T) {
	term := NewTerminal(10, 1)
	out := captureResponses(term)
	term.Process([]byte("\x1b]11;rgb:1234/5678/9abc\x1b\\")) // set bg
	term.Process([]byte("\x1b]11;?\x1b\\"))                  // query bg
	want := "\x1b]11;rgb:1212/5656/9a9a\x1b\\"               // 8-bit round-trip (0x12,0x56,0x9a)
	if string(*out) != want {
		t.Fatalf("OSC 11 query reply = %q, want %q", string(*out), want)
	}
}

func TestOSC4PaletteSetQuery(t *testing.T) {
	term := NewTerminal(10, 1)
	out := captureResponses(term)
	term.Process([]byte("\x1b]4;5;#ff0000\x1b\\")) // set palette 5 = red
	term.Process([]byte("\x1b]4;5;?\x1b\\"))       // query
	want := "\x1b]4;5;rgb:ffff/0000/0000\x1b\\"
	if string(*out) != want {
		t.Fatalf("OSC 4 query reply = %q, want %q", string(*out), want)
	}
}

func TestOverlongUTF8Rejected(t *testing.T) {
	term := NewTerminal(10, 1)
	// Overlong encoding of '/' (0x2F) as 0xC0 0xAF must decode to U+FFFD.
	term.Process([]byte{0xC0, 0xAF})
	if c := term.Grid.GetCell(0, 0).Char; c != 0xFFFD {
		t.Fatalf("overlong UTF-8 char = %U, want U+FFFD", c)
	}
}

func TestSurrogateUTF8Rejected(t *testing.T) {
	term := NewTerminal(10, 1)
	// Encoded surrogate U+D800 (0xED 0xA0 0x80) must decode to U+FFFD.
	term.Process([]byte{0xED, 0xA0, 0x80})
	if c := term.Grid.GetCell(0, 0).Char; c != 0xFFFD {
		t.Fatalf("surrogate UTF-8 char = %U, want U+FFFD", c)
	}
}

// Bare LF must move the cursor down with the COLUMN PRESERVED (index
// semantics); only CR homes the column. Raw-mode TUIs (nvim) position the
// cursor then send LF as a cheap "cursor down" — homing the column scattered
// their writes across column 1 (LNM mode 20 restores the legacy behavior).
func TestLineFeedPreservesColumn(t *testing.T) {
	term := NewTerminal(20, 5)
	term.Process([]byte("\x1b[2;10H\n"))
	col, row := term.Grid.GetCursor()
	if row != 2 || col != 9 {
		t.Errorf("after CUP(2,10)+LF: cursor row=%d col=%d, want row=2 col=9 (column preserved)", row, col)
	}

	// VT and FF behave like LF.
	term.Process([]byte("\x1b[2;10H\x0b\x0c"))
	col, row = term.Grid.GetCursor()
	if row != 3 || col != 9 {
		t.Errorf("after CUP(2,10)+VT+FF: cursor row=%d col=%d, want row=3 col=9", row, col)
	}

	// NEL (ESC E) does CR+LF.
	term.Process([]byte("\x1b[2;10H\x1bE"))
	col, row = term.Grid.GetCursor()
	if row != 2 || col != 0 {
		t.Errorf("after NEL: cursor row=%d col=%d, want row=2 col=0", row, col)
	}

	// LNM mode 20 set: LF performs CR too.
	term.Process([]byte("\x1b[20h\x1b[2;10H\n"))
	col, row = term.Grid.GetCursor()
	if row != 2 || col != 0 {
		t.Errorf("LNM: after CUP(2,10)+LF: cursor row=%d col=%d, want row=2 col=0", row, col)
	}
	term.Process([]byte("\x1b[20l"))

	// Autowrap continuation still starts at column 0.
	term.Process([]byte("\x1b[2J\x1b[4;1H"))
	term.Process([]byte("abcdefghijklmnopqrst")) // exactly 20 cols, wrap pending
	term.Process([]byte("X"))
	col, row = term.Grid.GetCursor()
	if row != 4 || col != 1 {
		t.Errorf("after wrap write: cursor row=%d col=%d, want row=4 col=1", row, col)
	}
	if c := term.Grid.GetCell(0, 4); c.Char != 'X' {
		t.Errorf("wrapped char at col0 = %q, want X", c.Char)
	}
}
