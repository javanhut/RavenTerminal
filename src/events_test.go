package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/render"
	"github.com/javanhut/RavenTerminal/src/tab"
)

type fakeSink struct {
	notes     []notification
	attention int
	bells     int
	fonts     []string
	resizes   [][2]int
}

func (f *fakeSink) notify(n notification) { f.notes = append(f.notes, n) }
func (f *fakeSink) requestAttention()     { f.attention++ }
func (f *fakeSink) visualBell(*tab.Pane)  { f.bells++ }
func (f *fakeSink) changeFont(name string) {
	f.fonts = append(f.fonts, name)
}

func (f *fakeSink) resizePane(_ *tab.Pane, cols, rows int) {
	f.resizes = append(f.resizes, [2]int{cols, rows})
}

func route(p eventPolicy, visible bool, evs ...parser.Event) (*fakeSink, bool) {
	s := &fakeSink{}
	redraw := routeEvents([]paneEvents{{visible: visible, events: evs}}, p, s)
	return s, redraw
}

func TestRouteBell(t *testing.T) {
	bell := parser.Event{Kind: parser.EventBell}
	cases := []struct {
		mode             string
		focused          bool
		bells, attention int
	}{
		{"visual", true, 1, 0},
		{"visual", false, 1, 1},
		{"attention", true, 0, 0},
		{"attention", false, 0, 1},
		{"none", false, 0, 0},
		{"", false, 1, 1}, // unset behaves as the default, visual
	}
	for _, c := range cases {
		s, _ := route(eventPolicy{bell: c.mode, focused: c.focused}, true, bell)
		if s.bells != c.bells || s.attention != c.attention {
			t.Errorf("bell=%q focused=%v: bells=%d attention=%d, want %d/%d", c.mode, c.focused, s.bells, s.attention, c.bells, c.attention)
		}
	}
}

func TestRouteNotify(t *testing.T) {
	ev := parser.Event{Kind: parser.EventNotify, Title: "Build", Body: "done", ID: "x1", Urgency: 2}
	s, _ := route(eventPolicy{notifications: true, focused: true}, true, ev)
	want := []notification{{Summary: "Build", Body: "done", Urgency: 2, Tag: "x1"}}
	if !reflect.DeepEqual(s.notes, want) {
		t.Fatalf("notes = %+v, want %+v", s.notes, want)
	}
	// OSC 9 carries only a body: it becomes the summary.
	s, _ = route(eventPolicy{notifications: true}, true, parser.Event{Kind: parser.EventNotify, Body: "hi", Urgency: 1})
	if len(s.notes) != 1 || s.notes[0].Summary != "hi" || s.notes[0].Body != "" {
		t.Fatalf("body-only notify = %+v", s.notes)
	}
	s, _ = route(eventPolicy{notifications: false}, true, ev)
	if len(s.notes) != 0 {
		t.Fatalf("notifications off still sent %+v", s.notes)
	}
}

func TestRouteCommandFinished(t *testing.T) {
	ev := parser.Event{Kind: parser.EventCommandFinished, ExitCode: 2, Body: "make test", Value: "12000"}
	p := eventPolicy{notifications: true, notifyAfter: 10 * time.Second}

	s, _ := route(p, true, ev) // unfocused window
	if len(s.notes) != 1 {
		t.Fatalf("unfocused long command: notes = %+v", s.notes)
	}
	if n := s.notes[0]; n.Summary != "Command failed (exit 2)" || n.Body != "make test (12s)" {
		t.Fatalf("note = %+v", n)
	}

	ok := ev
	ok.ExitCode = 0
	if s, _ = route(p, true, ok); len(s.notes) != 1 || s.notes[0].Summary != "Command finished" {
		t.Fatalf("success note = %+v", s.notes)
	}

	focused := p
	focused.focused = true
	if s, _ = route(focused, true, ev); len(s.notes) != 0 {
		t.Fatalf("focused visible pane notified: %+v", s.notes)
	}
	if s, _ = route(focused, false, ev); len(s.notes) != 1 {
		t.Fatalf("background tab not notified: %+v", s.notes)
	}

	short := ev
	short.Value = "5000"
	if s, _ = route(p, true, short); len(s.notes) != 0 {
		t.Fatalf("short command notified: %+v", s.notes)
	}
	unknown := ev
	unknown.Value = ""
	if s, _ = route(p, true, unknown); len(s.notes) != 0 {
		t.Fatalf("unknown duration notified: %+v", s.notes)
	}
	off := p
	off.notifyAfter = 0
	if s, _ = route(off, true, ev); len(s.notes) != 0 {
		t.Fatalf("notify_command_after=0 notified: %+v", s.notes)
	}
	muted := p
	muted.notifications = false
	if s, _ = route(muted, true, ev); len(s.notes) != 0 {
		t.Fatalf("notifications off notified: %+v", s.notes)
	}
}

func TestRouteRavenCommandAndRedraw(t *testing.T) {
	s, redraw := route(eventPolicy{}, true, parser.Event{Kind: parser.EventRavenCommand, Name: "change-font", Value: "fira code"})
	if !reflect.DeepEqual(s.fonts, []string{"fira code"}) || !redraw {
		t.Fatalf("change-font: fonts=%v redraw=%v", s.fonts, redraw)
	}
	s, _ = route(eventPolicy{}, true, parser.Event{Kind: parser.EventRavenCommand, Name: "bogus", Value: "x"})
	if len(s.fonts) != 0 {
		t.Fatalf("unknown command changed font: %v", s.fonts)
	}
	for _, k := range []parser.EventKind{parser.EventTitle, parser.EventProgress, parser.EventPointerShape} {
		if _, redraw := route(eventPolicy{}, true, parser.Event{Kind: k}); !redraw {
			t.Errorf("event %d did not request a redraw", k)
		}
	}
	if _, redraw := route(eventPolicy{}, true); redraw {
		t.Error("no events requested a redraw")
	}
}

func TestWindowTitle(t *testing.T) {
	if got := windowTitle("vim foo.go", "Raven Terminal"); got != "vim foo.go" {
		t.Errorf("osc title: %q", got)
	}
	if got := windowTitle("", "My Term"); got != "My Term" {
		t.Errorf("fallback: %q", got)
	}
}

func TestCursorShapeFor(t *testing.T) {
	cases := []struct {
		css    string
		shape  glfw.StandardCursor
		hidden bool
		def    bool
	}{
		{"", 0, false, true},
		{"default", 0, false, true},
		{"text", glfw.IBeamCursor, false, false},
		{"pointer", glfw.PointingHandCursor, false, false},
		{"crosshair", glfw.CrosshairCursor, false, false},
		{"ew-resize", glfw.ResizeEWCursor, false, false},
		{"ns-resize", glfw.ResizeNSCursor, false, false},
		{"nwse-resize", glfw.ResizeNWSECursor, false, false},
		{"ne-resize", glfw.ResizeNESWCursor, false, false},
		{"move", glfw.ResizeAllCursor, false, false},
		{"not-allowed", glfw.NotAllowedCursor, false, false},
		{"none", 0, true, false},
	}
	for _, c := range cases {
		shape, hidden, def := cursorShapeFor(c.css)
		if def != c.def || hidden != c.hidden || (!def && !hidden && shape != c.shape) {
			t.Errorf("%q: got (%v,%v,%v) want (%v,%v,%v)", c.css, shape, hidden, def, c.shape, c.hidden, c.def)
		}
	}
}

func TestFrameDecisionDefersPaneDrawsWhileSynced(t *testing.T) {
	pane := render.RedrawTriggers{PaneContentDirty: true, CursorPhaseChanged: true}
	if draw, pending := frameDecision(pane, true, false); draw || !pending {
		t.Fatalf("held sync drew pane frame: draw=%v pending=%v", draw, pending)
	}
	// Released: the deferred frame is drawn even with no new trigger.
	if draw, pending := frameDecision(render.RedrawTriggers{}, false, true); !draw || pending {
		t.Fatalf("release did not flush deferred frame: draw=%v pending=%v", draw, pending)
	}
	// UI changes are not held hostage by an application's sync.
	ui := render.RedrawTriggers{PaneContentDirty: true, MenuOpen: true}
	if draw, _ := frameDecision(ui, true, false); !draw {
		t.Fatal("menu frame suppressed during sync")
	}
	if draw, pending := frameDecision(render.RedrawTriggers{}, false, false); draw || pending {
		t.Fatal("idle frame drawn")
	}
}

func TestThemeDefaults(t *testing.T) {
	th := render.ThemeByName("catppuccin-mocha")
	fg, bg, cur, pal, dark := themeDefaults(th)
	if fg != grid.RGBColor(0xcd, 0xd6, 0xf4) || bg != grid.RGBColor(0x1e, 0x1e, 0x2e) || cur != grid.RGBColor(0xf5, 0xc2, 0xe7) {
		t.Fatalf("fg/bg/cursor = %+v %+v %+v", fg, bg, cur)
	}
	if !dark {
		t.Fatal("catppuccin-mocha reported light")
	}
	p16 := th.Palette16()
	for i := range pal {
		want := grid.RGBColor(to8(p16[i][0]), to8(p16[i][1]), to8(p16[i][2]))
		if pal[i] != want {
			t.Fatalf("palette[%d] = %+v, want %+v", i, pal[i], want)
		}
	}
	// The terminal then answers OSC 11 with the theme background.
	term := parser.NewTerminal(10, 3)
	var reply []byte
	term.SetResponseWriter(func(b []byte) { reply = append(reply, b...) })
	term.SetDefaultColors(fg, bg, cur, pal)
	term.Process([]byte("\x1b]11;?\x1b\\"))
	if want := "\x1b]11;rgb:1e1e/1e1e/2e2e\x1b\\"; string(reply) != want {
		t.Fatalf("OSC 11 reply %q, want %q", reply, want)
	}
}

func TestPointerOnlyOverPanes(t *testing.T) {
	if got := pointerFor("text", false); got != "" {
		t.Fatalf("outside panes = %q, want default", got)
	}
	if got := pointerFor("text", true); got != "text" {
		t.Fatalf("over pane = %q", got)
	}
}

func TestRouteResizeRequestNeedsOptInAndVisibility(t *testing.T) {
	req := parser.Event{Kind: parser.EventResizeRequest, Cols: 100, Rows: 30}
	cases := []struct {
		windowOps, visible bool
		want               int
	}{
		{false, true, 0},
		{true, false, 0},
		{true, true, 1},
	}
	for _, c := range cases {
		s, _ := route(eventPolicy{windowOps: c.windowOps}, c.visible, req)
		if len(s.resizes) != c.want {
			t.Errorf("windowOps=%v visible=%v: %d resizes, want %d", c.windowOps, c.visible, len(s.resizes), c.want)
		}
		if c.want == 1 && s.resizes[0] != [2]int{100, 30} {
			t.Errorf("resize %v, want 100x30", s.resizes[0])
		}
	}
}

func TestEventPolicyWindowOpsDefaultsOff(t *testing.T) {
	if eventPolicyFor(nil, true).windowOps {
		t.Fatal("window ops on by default")
	}
}

func TestResizedWindow(t *testing.T) {
	cases := []struct {
		name                       string
		fbW, fbH, winW, winH       int
		cw, ch                     float64
		dCols, dRows, wantW, wantH int
	}{
		{"scale 1 grows by whole cells", 800, 600, 800, 600, 10, 20, 20, -6, 1000, 480},
		{"scale 2 halves into window coords", 1600, 1200, 800, 600, 20, 40, 20, -6, 1000, 480},
		// 1.25: 7 more 12.5px cells = 87.5 fb px = 70 window px.
		{"fractional scale rounds up", 1000, 750, 800, 600, 12.5, 25, 7, 0, 870, 600},
		{"no change", 800, 600, 800, 600, 10, 20, 0, 0, 800, 600},
	}
	for _, c := range cases {
		w, h := resizedWindow(c.fbW, c.fbH, c.winW, c.winH, c.cw, c.ch, c.dCols, c.dRows)
		if w != c.wantW || h != c.wantH {
			t.Errorf("%s: got %dx%d, want %dx%d", c.name, w, h, c.wantW, c.wantH)
		}
	}
}
