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

func TestLinefeedBelowScrollRegionDoesNotScroll(t *testing.T) {
	term := NewTerminal(10, 6)
	term.Process([]byte("\x1b[1;4r")) // region rows 1-4
	term.Process([]byte("\x1b[5;1Hstatus"))
	term.Process([]byte("\x1b[6;1H")) // cursor on last screen row, below region
	term.Process([]byte("\n"))        // LF below the region must not scroll it
	_, row := term.Grid.GetCursor()
	if row != 5 {
		t.Fatalf("cursor row = %d, want 5 (clamped at screen bottom)", row)
	}
	if got := term.Grid.GetCell(0, 4).Char; got != 's' {
		t.Fatalf("row 5 cell = %q, want 's' (status line must not move)", got)
	}
}

func TestResizeInAltScreenRestoresFullScrollRegion(t *testing.T) {
	term := NewTerminal(10, 10)
	term.Process([]byte("\x1b[?1049h")) // enter alt screen (saves main region 1-10)
	term.Resize(10, 20)                 // grow while in alt screen
	term.Process([]byte("\x1b[?1049l")) // exit alt screen
	top, bottom := term.Grid.GetScrollRegion()
	if top != 1 || bottom != 20 {
		t.Fatalf("scroll region after resize+alt-exit = (%d,%d), want (1,20)", top, bottom)
	}
}

func TestC0ControlsInsideCSI(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process([]byte("ab"))
	// BS embedded mid-CSI executes immediately; the CSI still completes.
	term.Process([]byte("\x1b[\x081m"))
	col, _ := term.Grid.GetCursor()
	if col != 1 {
		t.Fatalf("cursor col = %d, want 1 (BS inside CSI must execute)", col)
	}
	if term.currentFlags&grid.FlagBold == 0 {
		t.Fatal("SGR 1 after embedded BS should still apply")
	}
}

func TestDECRQMReportsModes(t *testing.T) {
	term := NewTerminal(10, 5)
	var out []byte
	term.SetResponseWriter(func(b []byte) { out = append(out, b...) })
	term.Process([]byte("\x1b[?2004h"))
	term.Process([]byte("\x1b[?2004$p"))
	if got := string(out); got != "\x1b[?2004;1$y" {
		t.Fatalf("DECRQM reply = %q, want set (1)", got)
	}
	out = nil
	term.Process([]byte("\x1b[?2004l"))
	term.Process([]byte("\x1b[?2004$p"))
	if got := string(out); got != "\x1b[?2004;2$y" {
		t.Fatalf("DECRQM reply = %q, want reset (2)", got)
	}
	out = nil
	term.Process([]byte("\x1b[?9999$p"))
	if got := string(out); got != "\x1b[?9999;0$y" {
		t.Fatalf("DECRQM reply = %q, want not-recognized (0)", got)
	}
}

func TestDECSTRSoftReset(t *testing.T) {
	term := NewTerminal(10, 5)
	term.Process([]byte("\x1b[2;4r\x1b[?6h\x1b[?25l\x1b[1;31m\x1b[4h"))
	term.Process([]byte("\x1b[!p"))
	top, bottom := term.Grid.GetScrollRegion()
	if top != 1 || bottom != 5 {
		t.Fatalf("scroll region after DECSTR = (%d,%d), want (1,5)", top, bottom)
	}
	if term.originMode || term.insertMode || !term.cursorVisible {
		t.Fatal("DECSTR should reset origin/insert modes and re-enable the cursor")
	}
	if term.currentFlags != 0 {
		t.Fatal("DECSTR should reset SGR attributes")
	}
}

func TestWindowSizeReport(t *testing.T) {
	term := NewTerminal(80, 24)
	var out []byte
	term.SetResponseWriter(func(b []byte) { out = append(out, b...) })
	term.Process([]byte("\x1b[18t"))
	if got := string(out); got != "\x1b[8;24;80t" {
		t.Fatalf("CSI 18t reply = %q, want \\x1b[8;24;80t", got)
	}
}
