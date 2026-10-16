package parser

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

func reply(term *Terminal, seq string) string {
	out := captureResponses(term)
	feed(term, seq)
	return string(*out)
}

func TestDECRQMPrivateModes(t *testing.T) {
	cases := []struct {
		setup string
		mode  int
		want  int
	}{
		{"", 12, 1},
		{"\x1b[?12l", 12, 2},
		{"\x1b[?1048h", 1048, 1},
		{"", 1048, 2},
		{"\x1b[?1005h", 1005, 1},
		{"\x1b[?1015h", 1015, 1},
		{"\x1b[?1016h", 1016, 1},
		{"\x1b[?1016h", 1006, 2},
		{"", 2027, 1},
		{"\x1b[?2027l", 2027, 2},
		{"\x1b[?2031h", 2031, 1},
		{"", 2031, 2},
		{"\x1b[?2048h", 2048, 1},
		{"\x1b[?69h", 69, 1},
		{"", 69, 2},
		{"\x1b[?66h", 66, 1},
		{"\x1b=", 66, 1},
		{"", 1007, 1},
		{"\x1b[?1007l", 1007, 2},
		{"", 1036, 1},
		{"\x1b[?1036l", 1036, 2},
		{"", 1039, 1},
		{"\x1b[?5h", 5, 1},
		{"\x1b[?9h", 9, 1},
		{"\x1b[?80h", 80, 1},
		{"", 80, 2},
		{"", 7, 1},
		{"", 25, 1},
		{"", 9999, 0},
	}
	for _, tc := range cases {
		term := NewTerminal(10, 3)
		feed(term, tc.setup)
		got := reply(term, fmt.Sprintf("\x1b[?%d$p", tc.mode))
		want := fmt.Sprintf("\x1b[?%d;%d$y", tc.mode, tc.want)
		if got != want {
			t.Errorf("setup %q mode %d: got %q want %q", tc.setup, tc.mode, got, want)
		}
	}
}

func TestDECRQMAnsiModes(t *testing.T) {
	cases := []struct {
		setup string
		mode  int
		want  int
	}{
		{"", 4, 2},
		{"\x1b[4h", 4, 1},
		{"\x1b[20h", 20, 1},
		{"", 20, 2},
		{"", 2, 4},
		{"", 77, 0},
	}
	for _, tc := range cases {
		term := NewTerminal(10, 3)
		feed(term, tc.setup)
		got := reply(term, fmt.Sprintf("\x1b[%d$p", tc.mode))
		want := fmt.Sprintf("\x1b[%d;%d$y", tc.mode, tc.want)
		if got != want {
			t.Errorf("mode %d: got %q want %q", tc.mode, got, want)
		}
	}
}

func TestCursorBlinkMode12(t *testing.T) {
	term := NewTerminal(10, 3)
	feed(term, "\x1b[?12l")
	if term.CursorBlinking() {
		t.Fatal("?12l should stop blinking")
	}
	feed(term, "\x1b[?12h")
	if !term.CursorBlinking() {
		t.Fatal("?12h should blink")
	}
}

func TestXTSAVERESTORE(t *testing.T) {
	term := NewTerminal(10, 3)
	feed(term, "\x1b[?2004h\x1b[?1000h\x1b[?2004;1000s\x1b[?2004l\x1b[?1000l")
	if term.BracketedPasteEnabled() || term.GetMouseMode() != 0 {
		t.Fatal("reset failed")
	}
	feed(term, "\x1b[?2004;1000r")
	if !term.BracketedPasteEnabled() || term.GetMouseMode() != 1000 {
		t.Fatal("XTRESTORE did not restore modes")
	}
}

func TestKeypadAndModifyOtherKeys(t *testing.T) {
	term := NewTerminal(10, 3)
	if term.AppKeypad() {
		t.Fatal("keypad default should be numeric")
	}
	feed(term, "\x1b=")
	if !term.AppKeypad() {
		t.Fatal("DECKPAM")
	}
	feed(term, "\x1b>")
	if term.AppKeypad() {
		t.Fatal("DECKPNM")
	}
	feed(term, "\x1b[?66h")
	if !term.AppKeypad() {
		t.Fatal("DECNKM")
	}
	feed(term, "\x1b[>4;2m")
	if term.ModifyOtherKeys() != 2 {
		t.Fatalf("modifyOtherKeys = %d", term.ModifyOtherKeys())
	}
	if got := reply(term, "\x1b[?4m"); got != "\x1b[>4;2m" {
		t.Fatalf("XTQMODKEYS = %q", got)
	}
	feed(term, "\x1b[>4m")
	if term.ModifyOtherKeys() != 0 {
		t.Fatal("CSI > 4 m should reset")
	}
	feed(term, "\x1b[>4;1m\x1b[>4n")
	if term.ModifyOtherKeys() != 0 {
		t.Fatal("CSI > 4 n should disable")
	}
	// The SGR attributes must not be touched by XTMODKEYS.
	feed(term, "\x1b[>4;1mA")
	if term.Grid.GetCell(0, 0).Flags&grid.FlagUnderline != 0 {
		t.Fatal("XTMODKEYS set underline")
	}
}

func TestModeGetters(t *testing.T) {
	term := NewTerminal(10, 3)
	if !term.MetaSendsEscape() || !term.AltSendsEscape() || !term.AlternateScroll() || !term.GraphemeClustering() {
		t.Fatal("defaults")
	}
	feed(term, "\x1b[?1036l\x1b[?1039l\x1b[?1007l\x1b[?2027l\x1b[?5h")
	if term.MetaSendsEscape() || term.AltSendsEscape() || term.AlternateScroll() || term.GraphemeClustering() || !term.ReverseVideo() {
		t.Fatal("setters")
	}
}

func TestDarkModeReports(t *testing.T) {
	term := NewTerminal(10, 3)
	term.SetDarkMode(true)
	if got := reply(term, "\x1b[?996n"); got != "\x1b[?997;1n" {
		t.Fatalf("996n dark = %q", got)
	}
	out := captureResponses(term)
	term.SetDarkMode(false)
	if len(*out) != 0 {
		t.Fatal("unsolicited report without ?2031")
	}
	feed(term, "\x1b[?2031h")
	term.SetDarkMode(true)
	if got := string(*out); got != "\x1b[?997;1n" {
		t.Fatalf("unsolicited = %q", got)
	}
	*out = nil
	term.SetDarkMode(true)
	if len(*out) != 0 {
		t.Fatal("no report when unchanged")
	}
	term.SetDarkMode(false)
	if got := string(*out); got != "\x1b[?997;2n" {
		t.Fatalf("light = %q", got)
	}
}

func TestInBandResize2048(t *testing.T) {
	term := NewTerminal(80, 24)
	term.SetCellPixelSize(10, 20)
	if got := reply(term, "\x1b[?2048h"); got != "\x1b[48;24;80;480;800t" {
		t.Fatalf("enable report = %q", got)
	}
	out := captureResponses(term)
	term.Resize(100, 30)
	if got := string(*out); got != "\x1b[48;30;100;600;1000t" {
		t.Fatalf("resize report = %q", got)
	}
	*out = nil
	term.SetCellPixelSize(8, 16)
	if got := string(*out); got != "\x1b[48;30;100;480;800t" {
		t.Fatalf("cell size report = %q", got)
	}
	*out = nil
	feed(term, "\x1b[?2048l")
	term.Resize(90, 30)
	if len(*out) != 0 {
		t.Fatal("report after disable")
	}
}

func TestXTWINOPS(t *testing.T) {
	term := NewTerminal(80, 24)
	term.SetCellPixelSize(10, 20)
	cases := map[string]string{
		"\x1b[14t":      "\x1b[4;480;800t",
		"\x1b[16t":      "\x1b[6;20;10t",
		"\x1b[18t":      "\x1b[8;24;80t",
		"\x1b[19t":      "\x1b[9;24;80t",
		"\x1b[20t":      "\x1b]L\x1b\\",
		"\x1b[21t":      "\x1b]l\x1b\\",
		"\x1b[8;10;10t": "",
	}
	for in, want := range cases {
		if got := reply(term, in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if term.Grid.Cols != 80 {
		t.Fatal("8t resized")
	}
}

func TestTitleStack(t *testing.T) {
	term := NewTerminal(10, 3)
	feed(term, "\x1b]2;one\x07\x1b[22;0t\x1b]2;two\x07")
	if term.GetWindowTitle() != "two" {
		t.Fatal("set")
	}
	feed(term, "\x1b[23;0t")
	if term.GetWindowTitle() != "one" {
		t.Fatalf("pop = %q", term.GetWindowTitle())
	}
	feed(term, "\x1b[23;0t")
	if term.GetWindowTitle() != "one" {
		t.Fatal("pop on empty stack changed title")
	}
}

func TestXTVERSIONWithVersion(t *testing.T) {
	term := NewTerminal(10, 3)
	want := "\x1bP>|RavenTerminal(" + Version + ")\x1b\\"
	if got := reply(term, "\x1b[>q"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDA1AdvertisesSixel(t *testing.T) {
	term := NewTerminal(10, 3)
	got := reply(term, "\x1b[c")
	if !strings.HasPrefix(got, "\x1b[?62;") || !strings.Contains(got[2:], ";4;") && !strings.HasSuffix(got, ";4c") {
		t.Fatalf("DA1 = %q", got)
	}
}

func TestDECXCPR(t *testing.T) {
	term := NewTerminal(10, 5)
	feed(term, "\x1b[3;4H")
	if got := reply(term, "\x1b[?6n"); got != "\x1b[?3;4R" {
		t.Fatalf("DECXCPR = %q", got)
	}
}

func hexs(s string) string { return strings.ToUpper(hex.EncodeToString([]byte(s))) }

func TestXTGETTCAP(t *testing.T) {
	term := NewTerminal(10, 3)
	cases := map[string]string{
		"TN":     "xterm-raven",
		"Co":     "256",
		"colors": "256",
		"Smulx":  "\x1b[4:%p1%dm",
		"Ms":     "\x1b]52;%p1%s;%p2%s\x07",
		"kcuu1":  "\x1bOA",
	}
	for name, val := range cases {
		got := reply(term, "\x1bP+q"+hexs(name)+"\x1b\\")
		want := "\x1bP1+r" + hexs(name) + "=" + hexs(val) + "\x1b\\"
		if got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}
	// Boolean caps reply without a value.
	for _, b := range []string{"Tc", "RGB", "Su"} {
		if got := reply(term, "\x1bP+q"+hexs(b)+"\x1b\\"); got != "\x1bP1+r"+hexs(b)+"\x1b\\" {
			t.Errorf("%s = %q", b, got)
		}
	}
	// Multiple caps; an unknown one fails the request from that point.
	got := reply(term, "\x1bP+q"+hexs("Co")+";"+hexs("TN")+"\x1b\\")
	want := "\x1bP1+r" + hexs("Co") + "=" + hexs("256") + ";" + hexs("TN") + "=" + hexs("xterm-raven") + "\x1b\\"
	if got != want {
		t.Errorf("multi: got %q want %q", got, want)
	}
	if got := reply(term, "\x1bP+q"+hexs("nope")+"\x1b\\"); got != "\x1bP0+r"+hexs("nope")+"\x1b\\" {
		t.Errorf("unknown = %q", got)
	}
	// Lowercase hex is accepted.
	if got := reply(term, "\x1bP+q"+strings.ToLower(hexs("Co"))+"\x1b\\"); !strings.HasPrefix(got, "\x1bP1+r") {
		t.Errorf("lowercase hex = %q", got)
	}
}

func TestDECRQSS(t *testing.T) {
	term := NewTerminal(20, 10)
	feed(term, "\x1b[1;4;38;5;100;48;2;1;2;3m")
	if got := reply(term, "\x1bP$qm\x1b\\"); got != "\x1bP1$r0;1;4;38;5;100;48;2;1;2;3m\x1b\\" {
		t.Errorf("SGR = %q", got)
	}
	feed(term, "\x1b[0m")
	if got := reply(term, "\x1bP$qm\x1b\\"); got != "\x1bP1$r0m\x1b\\" {
		t.Errorf("SGR reset = %q", got)
	}
	feed(term, "\x1b[3;8r")
	if got := reply(term, "\x1bP$qr\x1b\\"); got != "\x1bP1$r3;8r\x1b\\" {
		t.Errorf("DECSTBM = %q", got)
	}
	feed(term, "\x1b[?69h\x1b[2;9s")
	if got := reply(term, "\x1bP$qs\x1b\\"); got != "\x1bP1$r2;9s\x1b\\" {
		t.Errorf("DECSLRM = %q", got)
	}
	feed(term, "\x1b[4 q")
	if got := reply(term, "\x1bP$q q\x1b\\"); got != "\x1bP1$r4 q\x1b\\" {
		t.Errorf("DECSCUSR = %q", got)
	}
	if got := reply(term, "\x1bP$q\"p\x1b\\"); got != "\x1bP1$r62;1\"p\x1b\\" {
		t.Errorf("DECSCL = %q", got)
	}
	if got := reply(term, "\x1bP$qz\x1b\\"); got != "\x1bP0$r\x1b\\" {
		t.Errorf("invalid = %q", got)
	}
}

func TestSGRNewAttributes(t *testing.T) {
	term := NewTerminal(20, 2)
	feed(term, "\x1b[53;5mA\x1b[55;25mB\x1b[21mC\x1b[24;6mD\x1b[0;73;74;75mE")
	a, b, c, d, e := term.Grid.GetCell(0, 0), term.Grid.GetCell(1, 0), term.Grid.GetCell(2, 0), term.Grid.GetCell(3, 0), term.Grid.GetCell(4, 0)
	if a.Flags&grid.FlagOverline == 0 || a.Flags&grid.FlagBlink == 0 {
		t.Fatalf("A flags %v", a.Flags)
	}
	if b.Flags&(grid.FlagOverline|grid.FlagBlink) != 0 {
		t.Fatalf("B flags %v", b.Flags)
	}
	if c.Flags&grid.FlagUnderline == 0 || c.UnderlineStyle != 2 {
		t.Fatalf("C double underline %+v", c)
	}
	if d.Flags&grid.FlagUnderline != 0 || d.Flags&grid.FlagBlink == 0 {
		t.Fatalf("D %+v", d)
	}
	if e.Flags != 0 || e.Fg.Type != grid.ColorDefault {
		t.Fatalf("E %+v", e)
	}
}

func TestSGRColonForms(t *testing.T) {
	cases := []struct {
		seq     string
		fg, bg  grid.Color
		ul      grid.Color
		flags   grid.CellFlags
		ulStyle uint8
	}{
		{"38:2::10:20:30", grid.RGBColor(10, 20, 30), grid.DefaultBg(), grid.Color{}, 0, 0},
		{"38:2:10:20:30", grid.RGBColor(10, 20, 30), grid.DefaultBg(), grid.Color{}, 0, 0},
		{"38:5:100;1", grid.IndexedColor(100), grid.DefaultBg(), grid.Color{}, grid.FlagBold, 0},
		{"38:5;1", grid.DefaultFg(), grid.DefaultBg(), grid.Color{}, grid.FlagBold, 0},
		{"48:2::1:2:3:0:0;3", grid.DefaultFg(), grid.RGBColor(1, 2, 3), grid.Color{}, grid.FlagItalic, 0},
		{"58:2::4:5:6;4:3", grid.DefaultFg(), grid.DefaultBg(), grid.RGBColor(4, 5, 6), grid.FlagUnderline, 3},
		{"38;2;7;8;9;48;5;3", grid.RGBColor(7, 8, 9), grid.IndexedColor(3), grid.Color{}, 0, 0},
		{"1:2;3", grid.DefaultFg(), grid.DefaultBg(), grid.Color{}, grid.FlagBold | grid.FlagItalic, 0},
	}
	for _, tc := range cases {
		term := NewTerminal(5, 1)
		feed(term, "\x1b["+tc.seq+"mX")
		c := term.Grid.GetCell(0, 0)
		if c.Fg != tc.fg || c.Bg != tc.bg || c.UnderlineColor != tc.ul || c.Flags != tc.flags || c.UnderlineStyle != tc.ulStyle && tc.ulStyle != 0 {
			t.Errorf("%q: got fg=%v bg=%v ul=%v flags=%v style=%d", tc.seq, c.Fg, c.Bg, c.UnderlineColor, c.Flags, c.UnderlineStyle)
		}
	}
}
