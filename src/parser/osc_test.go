package parser

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/javanhut/RavenTerminal/src/grid"
)

func osc(s string) []byte { return []byte("\x1b]" + s + "\x1b\\") }

func eventsOf(term *Terminal, kind EventKind) []Event {
	var out []Event
	for _, e := range term.DrainEvents() {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestBellEvent(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process([]byte("a\x07b\x1b]2;x\x07"))
	if n := len(eventsOf(term, EventBell)); n != 1 {
		t.Fatalf("bell events = %d, want 1 (OSC BEL terminator is not a bell)", n)
	}
	if len(term.DrainEvents()) != 0 {
		t.Fatal("drain must clear the queue")
	}
}

func TestEventQueueBounded(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process([]byte(strings.Repeat("\x07", maxEvents+50)))
	term.Process(osc("2;last"))
	ev := term.DrainEvents()
	if len(ev) != maxEvents {
		t.Fatalf("queue len = %d, want %d", len(ev), maxEvents)
	}
	if ev[len(ev)-1].Kind != EventTitle || ev[len(ev)-1].Title != "last" {
		t.Fatalf("newest event lost: %+v", ev[len(ev)-1])
	}
}

func TestTitleEventAndCap(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process(osc("2;héllo ✳"))
	term.Process(osc("2;héllo ✳")) // unchanged: no second event
	term.Process(osc("1;icon"))    // icon only: no title event
	ev := eventsOf(term, EventTitle)
	if len(ev) != 1 || ev[0].Title != "héllo ✳" || term.Title() != "héllo ✳" {
		t.Fatalf("title events %+v, Title()=%q", ev, term.Title())
	}
	term.Process(osc("0;" + strings.Repeat("é", maxTitleLen)))
	if got := term.Title(); len(got) > maxTitleLen || !strings.HasPrefix(got, "éé") || !utf8Valid(got) {
		t.Fatalf("title not capped on a rune boundary: len=%d", len(got))
	}
	term.Process(osc("2;bad\xffbyte"))
	if !utf8Valid(term.Title()) {
		t.Fatalf("invalid UTF-8 kept in title: %q", term.Title())
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "") == s }

func rgb(r, g, b uint8) grid.Color { return grid.RGBColor(r, g, b) }

func themed() *Terminal {
	term := NewTerminal(10, 3)
	var pal [16]grid.Color
	for i := range pal {
		pal[i] = rgb(uint8(i), uint8(i*2), uint8(i*3))
	}
	term.SetDefaultColors(rgb(0xdd, 0xee, 0xff), rgb(0x11, 0x22, 0x33), rgb(0xaa, 0xbb, 0xcc), pal)
	return term
}

func TestOSCColorQueriesUseDefaults(t *testing.T) {
	term := themed()
	out := captureResponses(term)
	term.Process(osc("10;?"))
	term.Process(osc("11;?"))
	term.Process(osc("12;?"))
	term.Process(osc("4;3;?;196;?;232;?"))
	want := "\x1b]10;rgb:dddd/eeee/ffff\x1b\\" +
		"\x1b]11;rgb:1111/2222/3333\x1b\\" +
		"\x1b]12;rgb:aaaa/bbbb/cccc\x1b\\" +
		"\x1b]4;3;rgb:0303/0606/0909\x1b\\" +
		"\x1b]4;196;rgb:ffff/0000/0000\x1b\\" +
		"\x1b]4;232;rgb:0808/0808/0808\x1b\\"
	if string(*out) != want {
		t.Fatalf("got  %q\nwant %q", *out, want)
	}
}

func TestOSCColorMultiValueAndReset(t *testing.T) {
	term := themed()
	out := captureResponses(term)
	term.Process(osc("10;red;#00ff00"))
	term.Process(osc("10;?;?"))
	want := "\x1b]10;rgb:ffff/0000/0000\x1b\\\x1b]11;rgb:0000/ffff/0000\x1b\\"
	if string(*out) != want {
		t.Fatalf("multi-value: got %q want %q", *out, want)
	}
	*out = nil
	term.Process(osc("110"))
	term.Process(osc("111"))
	term.Process(osc("10;?;?"))
	if want := "\x1b]10;rgb:dddd/eeee/ffff\x1b\\\x1b]11;rgb:1111/2222/3333\x1b\\"; string(*out) != want {
		t.Fatalf("after 110/111: got %q", *out)
	}
	term.Process(osc("12;blue"))
	term.Process(osc("112"))
	_, _, _, cur := term.EffectiveColors()
	if cur != [3]uint8{0xaa, 0xbb, 0xcc} {
		t.Fatalf("112 did not reset cursor: %v", cur)
	}
}

func TestOSC4SetResetAndEffective(t *testing.T) {
	term := themed()
	term.Process(osc("4;1;rgb:12/34/56;200;Slate Gray;17;rgbi:1/0.5/0"))
	pal, fg, bg, _ := term.EffectiveColors()
	if pal[1] != [3]uint8{0x12, 0x34, 0x56} || pal[200] != [3]uint8{112, 128, 144} || pal[17] != [3]uint8{255, 127, 0} {
		t.Fatalf("palette = %v %v %v", pal[1], pal[200], pal[17])
	}
	if pal[16] != [3]uint8{0, 0, 0} || pal[21] != [3]uint8{0, 0, 255} || pal[255] != [3]uint8{238, 238, 238} || pal[2] != [3]uint8{2, 4, 6} {
		t.Fatalf("defaults wrong: %v %v %v %v", pal[16], pal[21], pal[255], pal[2])
	}
	if fg != [3]uint8{0xdd, 0xee, 0xff} || bg != [3]uint8{0x11, 0x22, 0x33} {
		t.Fatalf("fg/bg = %v %v", fg, bg)
	}
	term.Process(osc("104;1"))
	pal, _, _, _ = term.EffectiveColors()
	if pal[1] != [3]uint8{1, 2, 3} || pal[200] != [3]uint8{112, 128, 144} {
		t.Fatal("104;1 must reset only index 1")
	}
	term.Process(osc("104"))
	pal, _, _, _ = term.EffectiveColors()
	if pal[200] != [3]uint8{0xff, 0x00, 0xd7} {
		t.Fatalf("104 must reset all, got %v", pal[200])
	}
}

func TestParseXColorForms(t *testing.T) {
	cases := map[string][3]uint8{
		"#fff":          {255, 255, 255},
		"#123456":       {0x12, 0x34, 0x56},
		"#fffeeeddd":    {255, 238, 221},
		"#ffffeeeedddd": {255, 238, 221},
		"rgb:f/0/80":    {255, 0, 128},
		"rgbi:0/1/0.5":  {0, 255, 127},
		"White":         {255, 255, 255},
		"dark green":    {0, 100, 0},
		"gray50":        {127, 127, 127},
		"grey100":       {255, 255, 255},
		"orange":        {255, 165, 0},
	}
	for spec, want := range cases {
		c, ok := parseXColor(spec)
		if !ok || [3]uint8{c.R, c.G, c.B} != want {
			t.Errorf("parseXColor(%q) = %v,%v want %v", spec, c, ok, want)
		}
	}
	for _, bad := range []string{"", "#ff", "nosuchcolor", "rgb:1/2", "rgbi:2/0/0", "gray101"} {
		if _, ok := parseXColor(bad); ok {
			t.Errorf("parseXColor(%q) accepted", bad)
		}
	}
}

func TestOSC133MarksAndFinishEvent(t *testing.T) {
	term := NewTerminal(20, 5)
	term.Process(osc("133;A;aid=1;cl=m"))
	term.Process([]byte("$ "))
	term.Process(osc("133;B"))
	term.Process([]byte("ls\r\n"))
	term.Process(osc("133;C;cmdline_url=ls%20-l"))
	term.Process([]byte("out1\r\nout2\r\n"))
	time.Sleep(5 * time.Millisecond)
	term.Process(osc("133;D;2;aid=1"))
	term.Process(osc("133;A;k=i"))
	term.Process([]byte("$ "))
	ev := eventsOf(term, EventCommandFinished)
	if len(ev) != 1 || ev[0].ExitCode != 2 || ev[0].Body != "ls -l" {
		t.Fatalf("finish events = %+v", ev)
	}
	if ms, err := strconv.Atoi(ev[0].Value); err != nil || ms < 5 {
		t.Fatalf("duration = %q", ev[0].Value)
	}
	g := term.GetGrid()
	if p := g.PrevPrompt(3); p != 0 {
		t.Fatalf("PrevPrompt(3) = %d, want 0", p)
	}
	if p := g.NextPrompt(0); p != 3 {
		t.Fatalf("NextPrompt(0) = %d, want 3", p)
	}
	s, e, ok := g.LastOutputRange()
	if !ok || s != 1 || e != 2 {
		t.Fatalf("LastOutputRange = %d,%d,%v want 1,2,true", s, e, ok)
	}
	if s, e, ok := g.CommandOutputAt(2); !ok || s != 1 || e != 2 {
		t.Fatalf("CommandOutputAt(2) = %d,%d,%v", s, e, ok)
	}
	if _, _, ok := g.CommandOutputAt(0); ok {
		t.Fatal("prompt row is not output")
	}
	if code, ok := g.ExitCodeAt(0); !ok || code != 2 {
		t.Fatalf("ExitCodeAt(0) = %d,%v", code, ok)
	}
	// Marks survive scrolling into history.
	term.Process([]byte(strings.Repeat("x\r\n", 10)))
	if p := g.PrevPrompt(g.AbsoluteCursorRow()); p != 3 {
		t.Fatalf("after scroll PrevPrompt = %d, want 3", p)
	}
}

func TestNotifications(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process(osc("9;hello there"))
	term.Process(osc("777;notify;Title;Body;with;semis"))
	b64 := base64.StdEncoding.EncodeToString([]byte("encoded body"))
	term.Process(osc("99;i=x:d=0:u=2;Kitty "))
	term.Process(osc("99;i=x:d=0;Title"))
	term.Process(osc("99;i=x:p=body:e=1;" + b64))
	term.Process(osc("99;;simple"))
	ev := eventsOf(term, EventNotify)
	want := []Event{
		{Kind: EventNotify, Body: "hello there", Urgency: 1},
		{Kind: EventNotify, Title: "Title", Body: "Body;with;semis", Urgency: 1},
		{Kind: EventNotify, ID: "x", Title: "Kitty Title", Body: "encoded body", Urgency: 2},
		{Kind: EventNotify, Title: "simple", Urgency: 1},
	}
	if len(ev) != len(want) {
		t.Fatalf("notify events = %+v", ev)
	}
	for i := range want {
		if ev[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, ev[i], want[i])
		}
	}
}

func TestKittyNotifyQuery(t *testing.T) {
	term := NewTerminal(10, 3)
	out := captureResponses(term)
	term.Process(osc("99;i=q:p=?;"))
	if s := string(*out); !strings.HasPrefix(s, "\x1b]99;i=q:p=?;") || !strings.Contains(s, "p=title,body") {
		t.Fatalf("query reply %q", s)
	}
}

func TestProgress(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process(osc("9;4;1;42"))
	term.Process(osc("9;4;2;250"))
	term.Process(osc("9;4;3"))
	ev := eventsOf(term, EventProgress)
	if len(ev) != 3 || ev[0].ProgressState != 1 || ev[0].ProgressValue != 42 ||
		ev[1].ProgressState != 2 || ev[1].ProgressValue != 100 || ev[2].ProgressState != 3 {
		t.Fatalf("progress events %+v", ev)
	}
	if len(eventsOf(term, EventNotify)) != 0 {
		t.Fatal("9;4 must not notify")
	}
	if st, _ := term.Progress(); st != 3 {
		t.Fatalf("Progress state = %d", st)
	}
	term.Process([]byte("\x1bc"))
	if st, _ := term.Progress(); st != 0 {
		t.Fatal("RIS must clear progress")
	}
	ev = eventsOf(term, EventProgress)
	if len(ev) != 1 || ev[0].ProgressState != 0 {
		t.Fatalf("RIS progress event %+v", ev)
	}
}

func TestOSC52Selections(t *testing.T) {
	term := NewTerminal(10, 3)
	var clip, prim []string
	term.SetClipboardWriter(func(s string) { clip = append(clip, s) })
	term.SetPrimaryWriter(func(s string) { prim = append(prim, s) })
	enc := base64.StdEncoding.EncodeToString
	term.Process(osc("52;c;" + enc([]byte("C"))))
	term.Process(osc("52;p;" + enc([]byte("P"))))
	term.Process(osc("52;s;" + enc([]byte("S"))))
	term.Process(osc("52;;" + enc([]byte("E"))))
	term.Process(osc("52;cp;" + enc([]byte("B"))))
	if strings.Join(clip, ",") != "C,E,B" || strings.Join(prim, ",") != "P,S,B" {
		t.Fatalf("clip=%v prim=%v", clip, prim)
	}

	responses := make(chan []byte, 2)
	term.SetResponseWriter(func(b []byte) { responses <- append([]byte(nil), b...) })
	term.SetClipboardReader(func() string { return "cb" })
	term.SetPrimaryReader(func() string { return "pr" })
	term.Process(osc("52;p;?"))
	select {
	case got := <-responses:
		if want := "\x1b]52;p;" + enc([]byte("pr")) + "\x1b\\"; string(got) != want {
			t.Fatalf("primary query = %q want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no primary reply")
	}
	term2 := NewTerminal(10, 3)
	out := captureResponses(term2)
	term2.Process(osc("52;p;?"))
	time.Sleep(20 * time.Millisecond)
	if len(*out) != 0 {
		t.Fatal("primary query without a reader (read gate off) must stay silent")
	}
}

func TestPointerShape(t *testing.T) {
	term := NewTerminal(10, 3)
	out := captureResponses(term)
	term.Process(osc("22;xterm"))
	term.Process(osc("22;>pointer"))
	term.Process(osc("22;<"))
	term.Process(osc("22;<"))
	term.Process(osc("22;bogus-shape"))
	ev := eventsOf(term, EventPointerShape)
	var shapes []string
	for _, e := range ev {
		shapes = append(shapes, e.Shape)
	}
	if strings.Join(shapes, ",") != "text,pointer,text," {
		t.Fatalf("shapes = %q", shapes)
	}
	term.Process(osc("22;?pointer,bogus,left_ptr"))
	if string(*out) != "\x1b]22;1,0,1\x1b\\" {
		t.Fatalf("query reply %q", *out)
	}
}

func TestUserVarsAndRavenCommand(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process(osc("1337;SetUserVar=foo=" + base64.StdEncoding.EncodeToString([]byte("bar baz"))))
	term.Process(osc("7771;change-font=JetBrains Mono"))
	term.Process(osc("7771;list-fonts"))
	ev := term.DrainEvents()
	if len(ev) != 3 || ev[0] != (Event{Kind: EventUserVar, Name: "foo", Value: "bar baz"}) ||
		ev[1] != (Event{Kind: EventRavenCommand, Name: "change-font", Value: "JetBrains Mono"}) ||
		ev[2] != (Event{Kind: EventRavenCommand, Name: "list-fonts"}) {
		t.Fatalf("events %+v", ev)
	}
	if term.UserVars()["foo"] != "bar baz" {
		t.Fatal("user var not stored")
	}
}

func TestOSC8IDsAndAltScreen(t *testing.T) {
	term := NewTerminal(20, 3)
	term.Process([]byte("\x1b]8;id=a;http://x\x1b\\A\x1b]8;;\x1b\\"))
	term.Process([]byte("\x1b]8;id=b;http://x\x1b\\B\x1b]8;;\x1b\\"))
	term.Process([]byte("\x1b]8;id=a;http://x\x1b\\C\x1b]8;;\x1b\\"))
	g := term.GetGrid()
	a, b, c := g.GetCell(0, 0).Link, g.GetCell(1, 0).Link, g.GetCell(2, 0).Link
	if a == 0 || a == b || a != c {
		t.Fatalf("link ids a=%d b=%d c=%d", a, b, c)
	}
	// A link opened on the main screen stays valid on the alt screen and back.
	term.Process([]byte("\x1b]8;;http://alt\x1b\\"))
	term.Process([]byte("\x1b[?1049hZ"))
	if u := term.GetGrid().LinkURL(term.GetGrid().GetCell(0, 0).Link); u != "http://alt" {
		t.Fatalf("alt screen link url = %q", u)
	}
	term.Process([]byte("\x1b]8;;http://y\x1b\\Y\x1b]8;;\x1b\\\x1b[?1049l"))
	if u := term.GetGrid().LinkURL(term.GetGrid().GetCell(0, 0).Link); u != "http://x" {
		t.Fatalf("main link after alt = %q", u)
	}
}

func TestOSC8IDsNeverReusedWhileReferenced(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process([]byte("\x1b]8;;http://keep\x1b\\K\x1b]8;;\x1b\\"))
	keep := term.GetGrid().GetCell(0, 0).Link
	for i := range 70000 {
		term.Process([]byte("\x1b]8;;http://n/" + strconv.Itoa(i) + "\x1b\\\x1b]8;;\x1b\\"))
	}
	term.Process([]byte("\x1b]8;;http://new\x1b\\N\x1b]8;;\x1b\\"))
	g := term.GetGrid()
	if g.LinkURL(keep) != "http://keep" {
		t.Fatalf("referenced link id %d was recycled: %q", keep, g.LinkURL(keep))
	}
	if n := g.GetCell(1, 0).Link; n == 0 || n == keep || g.LinkURL(n) != "http://new" {
		t.Fatalf("new link id %d url %q", n, g.LinkURL(n))
	}
}

func TestOSC66TextSizing(t *testing.T) {
	term := NewTerminal(20, 5)
	term.Process([]byte("a"))
	term.Process(osc("66;w=3;x"))
	term.Process([]byte("b"))
	if got := lineText(term, 0); got != "ax  b" {
		t.Fatalf("w=3 line = %q", got)
	}
	term.Process([]byte("\r\n"))
	term.Process([]byte("\x1b[2;1Hzzzzzzzz\x1b[3;1Hzzzzzzzz\x1b[2;1H"))
	term.Process(osc("66;s=2;ab"))
	term.Process([]byte("c"))
	if got := lineText(term, 1); got != "ab  czzz" {
		t.Fatalf("s=2 row1 = %q", got)
	}
	if got := lineText(term, 2); got != "    zzzz" {
		t.Fatalf("s=2 row2 = %q", got)
	}
	col, row := term.GetGrid().GetCursor()
	if col != 5 || row != 1 {
		t.Fatalf("cursor = %d,%d want 5,1", col, row)
	}
}

func TestOSC9CwdAndOSC7(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process(osc("7;file://host/tmp/a%20b"))
	if term.WorkingDir() != "/tmp/a b" {
		t.Fatalf("osc7 = %q", term.WorkingDir())
	}
	term.Process(osc("9;9;\"/srv\""))
	if term.WorkingDir() != "/srv" {
		t.Fatalf("osc 9;9 = %q", term.WorkingDir())
	}
	if len(eventsOf(term, EventNotify)) != 0 {
		t.Fatal("9;9 must not notify")
	}
}
