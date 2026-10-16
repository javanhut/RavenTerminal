package parser

import (
	"os"
	"strings"
	"testing"
)

// terminfoCaps parses terminfo/xterm-raven.ti into name -> value (nil for
// booleans), skipping the names line and use=.
func terminfoCaps(t *testing.T) map[string]*string {
	t.Helper()
	data, err := os.ReadFile("../../terminfo/xterm-raven.ti")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for line := range strings.SplitSeq(string(data), "\n") {
		if s := strings.TrimSpace(line); s != "" && !strings.HasPrefix(s, "#") {
			body.WriteString(s)
		}
	}
	caps := map[string]*string{}
	fields := strings.Split(body.String(), ",")
	for _, f := range fields[1:] {
		f = strings.TrimSpace(f)
		if f == "" || strings.HasPrefix(f, "use=") {
			continue
		}
		name, val, ok := strings.Cut(f, "=")
		if !ok {
			caps[name] = nil
			continue
		}
		r := strings.NewReplacer(`\E`, "\x1b", `\\`, `\`, `\007`, "\x07", "^G", "\x07")
		v := r.Replace(val)
		caps[name] = &v
	}
	return caps
}

func TestXTGETTCAPMatchesTerminfo(t *testing.T) {
	term := NewTerminal(80, 24)
	caps := terminfoCaps(t)
	if len(caps) < 15 {
		t.Fatalf("parsed only %d caps", len(caps))
	}
	for name, val := range caps {
		got := reply(term, "\x1bP+q"+hexs(name)+"\x1b\\")
		want := "\x1bP1+r" + hexs(name) + "\x1b\\"
		if val != nil {
			want = "\x1bP1+r" + hexs(name) + "=" + hexs(*val) + "\x1b\\"
		}
		if got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}
}

func TestSetulcSequenceSetsUnderlineColor(t *testing.T) {
	term := NewTerminal(10, 2)
	feed(term, "\x1b[58:2::1:2:3mX")
	c := term.Grid.GetCell(0, 0)
	if c.UnderlineColor.R != 1 || c.UnderlineColor.G != 2 || c.UnderlineColor.B != 3 {
		t.Fatalf("underline color = %+v", c.UnderlineColor)
	}
}

func TestOSCReplyMatchesRequestTerminator(t *testing.T) {
	term := NewTerminal(10, 2)
	if got := reply(term, "\x1b]10;?\x07"); !strings.HasSuffix(got, "\x07") || strings.Contains(got, "\x1b\\") {
		t.Errorf("BEL request: %q", got)
	}
	if got := reply(term, "\x1b]10;?\x1b\\"); !strings.HasSuffix(got, "\x1b\\") {
		t.Errorf("ST request: %q", got)
	}
	if got := reply(term, "\x1b]4;1;?\x07"); !strings.HasPrefix(got, "\x1b]4;1;rgb:") || !strings.HasSuffix(got, "\x07") {
		t.Errorf("OSC 4 BEL: %q", got)
	}
	if got := reply(term, "\x1b]22;?__current__\x07"); got != "\x1b]22;default\x07" {
		t.Errorf("OSC 22 BEL: %q", got)
	}
}

func TestPointerStackPerScreen(t *testing.T) {
	term := NewTerminal(10, 2)
	feed(term, "\x1b]22;>text\x1b\\")
	if s := term.PointerShape(); s != "text" {
		t.Fatalf("main = %q", s)
	}
	feed(term, "\x1b[?1049h")
	if s := term.PointerShape(); s != "" {
		t.Fatalf("alt starts with main's stack: %q", s)
	}
	feed(term, "\x1b]22;>pointer\x1b\\")
	if s := term.PointerShape(); s != "pointer" {
		t.Fatalf("alt = %q", s)
	}
	feed(term, "\x1b[?1049l")
	if s := term.PointerShape(); s != "text" {
		t.Fatalf("main after alt = %q", s)
	}
	feed(term, "\x1b[?1049h")
	if s := term.PointerShape(); s != "" {
		t.Fatalf("alt stack not fresh: %q", s)
	}
}

func TestKittyImageChangesMarkGridDirty(t *testing.T) {
	term := NewTerminal(20, 5)
	px := "AAAA/w==" // 1x1 RGBA
	term.Process(apc("f=32,s=1,v=1,i=9,a=t,q=2;" + px))
	settle := func() {
		g := term.GetGrid()
		g.Snapshot(nil)
		if g.RedrawNeeded() {
			t.Fatal("snapshot did not clear dirt")
		}
	}
	settle()
	term.Process(apc("a=p,i=9,C=1,q=2"))
	if !term.GetGrid().RedrawNeeded() {
		t.Fatal("placement without cursor move did not request a redraw")
	}
	settle()
	term.Process(apc("a=d,d=i,i=9,q=2"))
	if !term.GetGrid().RedrawNeeded() {
		t.Fatal("delete did not request a redraw")
	}
}

func TestKittyUnsupportedActionReportsError(t *testing.T) {
	term := NewTerminal(20, 5)
	for _, a := range []string{"f", "a", "c"} {
		got := reply(term, string(apc("a="+a+",i=3")))
		if !strings.HasPrefix(got, "\x1b_Gi=3;E") || strings.Contains(got, ";OK") {
			t.Errorf("a=%s reply = %q, want an error", a, got)
		}
	}
}

func TestKittyRelativePlacement(t *testing.T) {
	term := NewTerminal(40, 10)
	px := "AAAA/w=="
	term.Process(apc("f=32,s=1,v=1,i=1,a=t,q=2;" + px))
	feed(term, "\x1b[3;5H")
	term.Process(apc("a=p,i=1,p=7,C=1,q=2"))
	feed(term, "\x1b[8;30H")
	term.Process(apc("a=p,i=1,p=8,P=1,Q=7,H=2,V=-1,q=2"))
	var child *struct{ col, row int }
	for _, p := range term.Images().Placements() {
		if p.PlacementID == 8 {
			child = &struct{ col, row int }{p.AnchorCol, p.AnchorAbsRow}
		}
	}
	if child == nil || child.col != 6 || child.row != 1 {
		t.Fatalf("relative placement = %+v, want col 6 row 1", child)
	}
	if c, r := term.GetGrid().GetCursor(); c != 29 || r != 7 {
		t.Fatalf("relative placement moved the cursor to %d,%d", c, r)
	}
	if got := reply(term, string(apc("a=p,i=1,p=9,P=1,Q=99"))); !strings.Contains(got, "ENOPARENT") {
		t.Fatalf("missing parent reply = %q", got)
	}
}
