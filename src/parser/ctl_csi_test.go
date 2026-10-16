package parser

import (
	"strings"
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

func feed(term *Terminal, s string) { term.Process([]byte(s)) }

func cur(term *Terminal) (int, int) { return term.Grid.GetCursor() }

func expectCursor(t *testing.T, term *Terminal, col, row int) {
	t.Helper()
	if c, r := cur(term); c != col || r != row {
		t.Fatalf("cursor = (%d,%d), want (%d,%d)", c, r, col, row)
	}
}

func screenText(term *Terminal) string {
	var lines []string
	for r := 0; r < term.Grid.Rows; r++ {
		lines = append(lines, lineText(term, r))
	}
	return strings.Join(lines, "|")
}

func TestCSIUnknownMarkersIgnored(t *testing.T) {
	cases := []struct {
		name string
		seq  string
	}{
		{"XTSMGRAPHICS does not scroll", "\x1b[?1;1;0S"},
		{"XTRMTITLE does not scroll down", "\x1b[>0T"},
		{"private CUP ignored", "\x1b[?5;5H"},
		{"intermediate ED ignored", "\x1b[2$J"},
		{"DECSCA not cursor", "\x1b[1\"q"},
		{"DECLL not cursor", "\x1b[2q"},
		{"DECCARA ignored", "\x1b[1;1;2;2;1$r"},
		{"gt CUU ignored", "\x1b[>3A"},
		{"param after intermediate invalid", "\x1b[ 2q"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			term := NewTerminal(10, 4)
			feed(term, "a\r\nb\r\nc\x1b[2;3H")
			before := screenText(term)
			c0, r0 := cur(term)
			feed(term, tc.seq)
			if got := screenText(term); got != before {
				t.Fatalf("screen changed: %q -> %q", before, got)
			}
			expectCursor(t, term, c0, r0)
			if term.CursorStyle() != CursorStyleBlock || !term.CursorBlinking() {
				t.Fatalf("cursor style changed by %q", tc.seq)
			}
		})
	}
}

func TestXTSMGRAPHICSReplies(t *testing.T) {
	term := NewTerminal(80, 24)
	term.SetCellPixelSize(10, 20)
	out := captureResponses(term)
	feed(term, "\x1b[?1;1;0S")
	if got := string(*out); got != "\x1b[?1;0;256S" {
		t.Fatalf("color registers reply = %q", got)
	}
	*out = nil
	feed(term, "\x1b[?2;1;0S")
	if got := string(*out); got != "\x1b[?2;0;800;480S" {
		t.Fatalf("geometry reply = %q", got)
	}
	*out = nil
	feed(term, "\x1b[?3;1;0S")
	if got := string(*out); got != "\x1b[?3;1;0S" {
		t.Fatalf("regis reply = %q", got)
	}
}

func TestCSIControlsNew(t *testing.T) {
	cases := []struct {
		name     string
		seq      string
		col, row int
	}{
		{"CHT", "\x1b[1;1H\x1b[2I", 16, 0},
		{"CBT", "\x1b[1;20H\x1b[2Z", 8, 0},
		{"CBT clamps", "\x1b[1;3H\x1b[5Z", 0, 0},
		{"HPA", "\x1b[3;1H\x1b[7`", 6, 2},
		{"HPR", "\x1b[3;2H\x1b[4a", 5, 2},
		{"VPR", "\x1b[2;2H\x1b[3e", 1, 4},
		{"HVP", "\x1b[4;5f", 4, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			term := NewTerminal(30, 8)
			feed(term, tc.seq)
			expectCursor(t, term, tc.col, tc.row)
		})
	}
}

func TestILDLCarriageReturn(t *testing.T) {
	term := NewTerminal(10, 5)
	feed(term, "\x1b[2;5H\x1b[L")
	expectCursor(t, term, 0, 1)
	feed(term, "\x1b[3;6H\x1b[M")
	expectCursor(t, term, 0, 2)
}

func TestCUUCUDStopAtMargins(t *testing.T) {
	term := NewTerminal(10, 10)
	feed(term, "\x1b[3;7r\x1b[5;1H\x1b[10A")
	expectCursor(t, term, 0, 2)
	feed(term, "\x1b[10B")
	expectCursor(t, term, 0, 6)
	// Outside the region the cursor travels to the screen edge.
	feed(term, "\x1b[9;1H\x1b[10B")
	expectCursor(t, term, 0, 9)
	feed(term, "\x1b[1;1H\x1b[1A")
	expectCursor(t, term, 0, 0)
}

func TestSLSR(t *testing.T) {
	term := NewTerminal(6, 2)
	feed(term, "abcdef\r\nghijkl\x1b[1;1H\x1b[2 @")
	if got := screenText(term); got != "cdef|ijkl" {
		t.Fatalf("SL = %q", got)
	}
	feed(term, "\x1b[1 A")
	if got := screenText(term); got != " cdef| ijkl" {
		t.Fatalf("SR = %q", got)
	}
}

func TestDECSLRM(t *testing.T) {
	term := NewTerminal(10, 4)
	// Without DECLRMM, CSI s is SCOSC.
	feed(term, "\x1b[2;3H\x1b[s\x1b[1;1H\x1b[u")
	expectCursor(t, term, 2, 1)
	feed(term, "\x1b[?69h\x1b[3;6s")
	expectCursor(t, term, 0, 0)
	// Text wraps at the right margin back to the left margin.
	feed(term, "\x1b[1;3HABCDEF")
	if got := lineText(term, 0); got != "  ABCD" {
		t.Fatalf("row0 = %q", got)
	}
	if got := lineText(term, 1); got != "  EF" {
		t.Fatalf("row1 = %q", got)
	}
	// CR returns to the left margin.
	feed(term, "\r")
	expectCursor(t, term, 2, 1)
	// Scrolling at the bottom only moves the margin rectangle.
	term2 := NewTerminal(6, 3)
	feed(term2, "xxxxxx\r\nyyyyyy\r\nzzzzzz")
	feed(term2, "\x1b[?69h\x1b[2;4s\x1b[3;2H\n")
	if got := screenText(term2); got != "xyyyxx|yzzzyy|z   zz" {
		t.Fatalf("rect scroll = %q", got)
	}
	// Origin mode homes to the margin corner.
	term3 := NewTerminal(10, 5)
	feed(term3, "\x1b[?69h\x1b[2;4r\x1b[3;8s\x1b[?6h\x1b[1;1H")
	expectCursor(t, term3, 2, 1)
	out := captureResponses(term3)
	feed(term3, "\x1b[6n")
	if got := string(*out); got != "\x1b[1;1R" {
		t.Fatalf("CPR in origin+margins = %q", got)
	}
	// Disabling DECLRMM resets the margins.
	feed(term3, "\x1b[?6l\x1b[?69l\x1b[1;9HABC")
	if got := lineText(term3, 1); got != "C" {
		t.Fatalf("after DECLRMM off row1 = %q", got)
	}
}

func TestDECSLRMICHDCH(t *testing.T) {
	term := NewTerminal(8, 1)
	feed(term, "abcdefgh\x1b[?69h\x1b[2;5s\x1b[1;2H\x1b[@")
	if got := lineText(term, 0); got != "a bcdfgh" {
		t.Fatalf("ICH in margins = %q", got)
	}
	feed(term, "\x1b[1;2H\x1b[2P")
	if got := lineText(term, 0); got != "acd  fgh" {
		t.Fatalf("DCH in margins = %q", got)
	}
}

func TestWideCharCursorByColumns(t *testing.T) {
	term := NewTerminal(10, 2)
	feed(term, "中文") // cols 0-3
	feed(term, "\x1b[1;2H")
	expectCursor(t, term, 1, 0)
	feed(term, "\x1b[1;1H\x1b[C")
	expectCursor(t, term, 1, 0)
	feed(term, "\x1b[1;4H\x1b[D")
	expectCursor(t, term, 2, 0)
	feed(term, "\x1b[1;4H\b")
	expectCursor(t, term, 2, 0)
	feed(term, "\x1b[1;2H\x1b[2C")
	expectCursor(t, term, 3, 0)
	feed(term, "\x1b[2G")
	expectCursor(t, term, 1, 0)
}

func TestDECALNResetsMarginsAndHomes(t *testing.T) {
	term := NewTerminal(5, 4)
	feed(term, "\x1b[2;3r\x1b[4;4H\x1b#8")
	expectCursor(t, term, 0, 0)
	if top, bot := term.Grid.GetScrollRegion(); top != 1 || bot != 4 {
		t.Fatalf("margins = %d,%d", top, bot)
	}
	if got := lineText(term, 3); got != "EEEEE" {
		t.Fatalf("row3 = %q", got)
	}
}

func TestESCIntermediatesConsumed(t *testing.T) {
	for _, seq := range []string{"\x1b F", "\x1b G", "\x1b#3", "\x1b#4", "\x1b#5", "\x1b#6", "\x1b%G", "\x1b%@", "\x1b(%5", "\x1b)\"4", "\x1b*<", "\x1b-A", "\x1b^private\x1b\\", "\x1bXsos\x1b\\"} {
		term := NewTerminal(10, 2)
		feed(term, seq+"x")
		if got := lineText(term, 0); got != "x" {
			t.Fatalf("%q left %q", seq, got)
		}
	}
}

func TestCharsetsG2G3(t *testing.T) {
	term := NewTerminal(20, 1)
	// G2 = DEC graphics, SS2 maps one char only.
	feed(term, "\x1b*0\x1bNqq")
	if got := lineText(term, 0); got != "─q" {
		t.Fatalf("SS2 = %q", got)
	}
	term = NewTerminal(20, 1)
	feed(term, "\x1b+0\x1boqq\x1b(B\x1b\x6e")
	// LS3 locks G3 into GL.
	if got := lineText(term, 0); got != "──" {
		t.Fatalf("LS3 = %q", got)
	}
	term = NewTerminal(20, 1)
	feed(term, "\x1b*0\x1bnq\x0fq")
	if got := lineText(term, 0); got != "─q" {
		t.Fatalf("LS2 then SI = %q", got)
	}
	term = NewTerminal(20, 1)
	feed(term, "\x1b(A#")
	if got := lineText(term, 0); got != "£" {
		t.Fatalf("UK charset = %q", got)
	}
	term = NewTerminal(20, 1)
	feed(term, "\x1b+0\x1bOqq")
	if got := lineText(term, 0); got != "─q" {
		t.Fatalf("SS3 = %q", got)
	}
}

func TestCANSUBAbortStrings(t *testing.T) {
	for _, seq := range []string{"\x1b]0;title\x18", "\x1bPq#0;2;0;0;0\x1a", "\x1b_Gf=32;AAAA\x18", "\x1b]2;x\x1a"} {
		term := NewTerminal(20, 1)
		feed(term, seq+"ok")
		if got := lineText(term, 0); got != "ok" {
			t.Fatalf("%q -> %q", seq, got)
		}
	}
	term := NewTerminal(20, 1)
	feed(term, "\x1b]2;abc\x18\x1b\\")
	if term.GetWindowTitle() != "" {
		t.Fatalf("aborted OSC set title %q", term.GetWindowTitle())
	}
}

func TestInvalidUTF8Replacement(t *testing.T) {
	cases := map[string]string{
		"stray continuation": "a\x80b",
		"bad lead":           "a\xffb",
		"truncated":          "a\xe4\xb8b",
		"overlong lead":      "a\xc0b",
	}
	for name, in := range cases {
		term := NewTerminal(10, 1)
		feed(term, in)
		if got := lineText(term, 0); got != "a�b" {
			t.Fatalf("%s: %q", name, got)
		}
	}
	term := NewTerminal(10, 1)
	feed(term, "\xe4\x1b[1mz")
	if got := lineText(term, 0); got != "�z" {
		t.Fatalf("ESC interrupting UTF-8: %q", got)
	}
}

func TestTabStopsSurviveResize(t *testing.T) {
	term := NewTerminal(20, 2)
	feed(term, "\x1b[3g\x1b[1;4H\x1bH\x1b[1;1H")
	term.Resize(30, 2)
	feed(term, "\t")
	expectCursor(t, term, 3, 0)
	feed(term, "\t")
	expectCursor(t, term, 29, 0)
	term.Resize(10, 2)
	feed(term, "\x1b[1;1H\t")
	expectCursor(t, term, 3, 0)
	// RIS and DECSTR restore the default stops.
	feed(term, "\x1bc\t")
	expectCursor(t, term, 8, 0)
	feed(term, "\x1b[3g\x1b[!p\x1b[1;1H\t")
	expectCursor(t, term, 8, 0)
}

func TestTabStopsWidenedGetDefaults(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Resize(30, 1)
	feed(term, "\x1b[1;10H\t\t")
	expectCursor(t, term, 24, 0)
}

func TestDECSCDECRCFullState(t *testing.T) {
	term := NewTerminal(10, 5)
	feed(term, "\x1b]8;;http://x\x1b\\\x1b[4:3;58;5;3;1m\x1b(0\x1b[?6h\x1b[?7l\x1b[2;3H\x1b7")
	feed(term, "\x1b[0m\x1b]8;;\x1b\\\x1b(B\x1b[?6l\x1b[?7h\x1b[5;5H\x1b8q")
	cell := term.Grid.GetCell(2, 1)
	if cell.Char != '─' {
		t.Fatalf("charset not restored: %q", cell.Char)
	}
	if cell.Flags&grid.FlagBold == 0 || cell.UnderlineStyle != 3 || cell.UnderlineColor != grid.IndexedColor(3) {
		t.Fatalf("attrs not restored: %+v", cell)
	}
	if cell.Link == 0 {
		t.Fatal("hyperlink not restored")
	}
	if term.Grid.GetAutoWrap() {
		t.Fatal("autowrap not restored")
	}
	feed(term, "\x1b[1;1H")
	expectCursor(t, term, 0, 0) // origin mode restored, region is full screen
	out := captureResponses(term)
	feed(term, "\x1b[?6$p")
	if string(*out) != "\x1b[?6;1$y" {
		t.Fatalf("origin mode after DECRC: %q", string(*out))
	}
}

func TestDECSCRestoresWrapPending(t *testing.T) {
	term := NewTerminal(5, 3)
	feed(term, "abcde\x1b7\x1b[3;1H\x1b8X")
	if got := lineText(term, 1); got != "X" {
		t.Fatalf("wrap pending not restored: row1=%q row0=%q", got, lineText(term, 0))
	}
}

func TestRISFullReset(t *testing.T) {
	term := NewTerminal(10, 5)
	feed(term, "\x1b[?1000h\x1b[?1006h\x1b[?2004h\x1b[?1004h\x1b[?7l\x1b[?6h\x1b[2;4r\x1b=\x1b[>4;2m\x1b[>1u\x1b[?2026h\x1b[?69h\x1b[2;5s\x1b(0")
	feed(term, "\x1b]0;hello\x07\x1b]11;#102030\x07\x1b]4;1;#ffffff\x07")
	feed(term, "\x1b[?1049hALT")
	feed(term, "\x1bc")
	if term.GetMouseMode() != 0 || term.MouseSGREnabled() || term.BracketedPasteEnabled() || term.FocusReportingEnabled() {
		t.Fatal("mouse/paste/focus not reset")
	}
	if !term.Grid.GetAutoWrap() || term.AppKeypad() || term.ModifyOtherKeys() != 0 || term.KittyKeyboardFlags() != 0 || term.SyncActive() {
		t.Fatal("modes not reset")
	}
	if term.IsAlternateScreen() {
		t.Fatal("still on alternate screen")
	}
	if term.GetWindowTitle() != "" {
		t.Fatalf("title = %q", term.GetWindowTitle())
	}
	if c := term.BackgroundColor(); c.Type != grid.ColorDefault {
		t.Fatal("dynamic bg not reset")
	}
	if _, ok := term.PaletteOverride(1); ok {
		t.Fatal("palette not reset")
	}
	if top, bot := term.Grid.GetScrollRegion(); top != 1 || bot != 5 {
		t.Fatal("margins not reset")
	}
	feed(term, "q\x1b[3;1Habcdefghijkl")
	if got := lineText(term, 0); got != "q" {
		t.Fatalf("charset not reset: %q", got)
	}
	if got := lineText(term, 3); got != "kl" {
		t.Fatalf("lr margins/autowrap not reset: %q", got)
	}
}

func TestRISClearsMainAfterAltExit(t *testing.T) {
	term := NewTerminal(10, 3)
	feed(term, "MAIN\x1b[?1049hALT\x1bc")
	if got := screenText(term); got != "||" {
		t.Fatalf("screen after RIS = %q", got)
	}
}
