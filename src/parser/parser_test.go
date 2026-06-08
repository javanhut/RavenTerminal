package parser

import (
	"strings"
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

// lineText returns the trimmed text of a display row of the terminal's grid.
func lineText(t *Terminal, row int) string {
	g := t.Grid
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

func TestPrintPlain(t *testing.T) {
	term := NewTerminal(20, 3)
	term.Process([]byte("hello world"))
	if got := lineText(term, 0); got != "hello world" {
		t.Fatalf("line = %q", got)
	}
}

func TestCursorPosition(t *testing.T) {
	term := NewTerminal(20, 10)
	term.Process([]byte("\x1b[3;5H")) // row 3 col 5 (1-based)
	col, row := term.Grid.GetCursor()
	if col != 4 || row != 2 {
		t.Fatalf("cursor = (%d,%d), want (4,2)", col, row)
	}
}

func TestCursorMovement(t *testing.T) {
	term := NewTerminal(20, 10)
	term.Process([]byte("\x1b[5;5H")) // (4,4)
	term.Process([]byte("\x1b[2A"))   // up 2 -> row 2
	term.Process([]byte("\x1b[3C"))   // right 3 -> col 7
	col, row := term.Grid.GetCursor()
	if col != 7 || row != 2 {
		t.Fatalf("cursor = (%d,%d), want (7,2)", col, row)
	}
}

func TestEraseDisplay(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process([]byte("abc\r\ndef"))
	term.Process([]byte("\x1b[H")) // home
	term.Process([]byte("\x1b[J")) // clear to end
	if got := lineText(term, 0); got != "" {
		t.Fatalf("line 0 = %q, want blank", got)
	}
	if got := lineText(term, 1); got != "" {
		t.Fatalf("line 1 = %q, want blank", got)
	}
}

func TestSGRForeground(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b[31mX"))
	cell := term.Grid.GetCell(0, 0)
	if cell.Fg.Type != grid.ColorIndexed || cell.Fg.Index != 1 {
		t.Fatalf("fg = %+v, want indexed 1", cell.Fg)
	}
}

func TestSGRTrueColor(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b[38;2;10;20;30mX"))
	cell := term.Grid.GetCell(0, 0)
	if cell.Fg.Type != grid.ColorRGB || cell.Fg.R != 10 || cell.Fg.G != 20 || cell.Fg.B != 30 {
		t.Fatalf("fg = %+v, want rgb(10,20,30)", cell.Fg)
	}
}

func TestSGRBoldReset(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b[1mA\x1b[0mB"))
	if term.Grid.GetCell(0, 0).Flags&grid.FlagBold == 0 {
		t.Fatal("cell 0 should be bold")
	}
	if term.Grid.GetCell(1, 0).Flags&grid.FlagBold != 0 {
		t.Fatal("cell 1 should not be bold after reset")
	}
}

func TestCursorVisibility(t *testing.T) {
	term := NewTerminal(10, 1)
	if !term.IsCursorVisible() {
		t.Fatal("cursor should start visible")
	}
	term.Process([]byte("\x1b[?25l"))
	if term.IsCursorVisible() {
		t.Fatal("cursor should be hidden after ?25l")
	}
	term.Process([]byte("\x1b[?25h"))
	if !term.IsCursorVisible() {
		t.Fatal("cursor should be visible after ?25h")
	}
}

func TestAppCursorKeys(t *testing.T) {
	term := NewTerminal(10, 1)
	if term.AppCursorKeys() {
		t.Fatal("app cursor keys should default off")
	}
	term.Process([]byte("\x1b[?1h"))
	if !term.AppCursorKeys() {
		t.Fatal("app cursor keys should be on after ?1h")
	}
}

func TestWindowTitle(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b]2;My Title\x07"))
	if got := term.GetWindowTitle(); got != "My Title" {
		t.Fatalf("title = %q, want %q", got, "My Title")
	}
}

func TestBracketedPaste(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b[?2004h"))
	if !term.BracketedPasteEnabled() {
		t.Fatal("bracketed paste should be enabled")
	}
}

func TestUTF8MultiByte(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("é")) // 2-byte UTF-8
	if got := term.Grid.GetCell(0, 0).Char; got != 'é' {
		t.Fatalf("char = %q, want é", got)
	}
}

func TestScrollRegion(t *testing.T) {
	term := NewTerminal(10, 5)
	term.Process([]byte("\x1b[2;4r")) // region rows 2-4
	top, bottom := term.Grid.GetScrollRegion()
	if top != 2 || bottom != 4 {
		t.Fatalf("scroll region = (%d,%d), want (2,4)", top, bottom)
	}
}
