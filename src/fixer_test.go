package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/tab"
)

func TestPrunePanesDropsClosedPaneState(t *testing.T) {
	live, closed := &tab.Pane{}, &tab.Pane{}
	a := &App{
		pumpTick:  5,
		panes:     map[*tab.Pane]paneState{live: {seen: 5}, closed: {seen: 4}},
		lastGrids: map[*tab.Pane]*grid.Grid{live: nil, closed: nil, {}: nil},
	}
	a.prunePanes()
	if _, ok := a.panes[closed]; ok {
		t.Fatal("closed pane kept in panes")
	}
	if len(a.lastGrids) != 1 {
		t.Fatalf("lastGrids keeps %d panes, want only the live one", len(a.lastGrids))
	}
	if _, ok := a.lastGrids[live]; !ok {
		t.Fatal("live pane dropped")
	}
}

func TestRouteCoalescesFontChanges(t *testing.T) {
	var evs []parser.Event
	for _, f := range []string{"a", "b", "c"} {
		evs = append(evs, parser.Event{Kind: parser.EventRavenCommand, Name: "change-font", Value: f})
	}
	s, redraw := route(eventPolicy{}, true, evs...)
	if len(s.fonts) != 1 || s.fonts[0] != "c" || !redraw {
		t.Fatalf("fonts = %v (redraw %v), want one change to c", s.fonts, redraw)
	}
}

func newTestNotifier() (*notifier, chan queuedNote) {
	n := &notifier{tags: map[string]uint32{}, owners: map[uint32]any{}}
	q := make(chan queuedNote, 64)
	n.once.Do(func() { n.queue = q })
	return n, q
}

func TestNotifierRateLimitsPerOwner(t *testing.T) {
	n, q := newTestNotifier()
	now := time.Unix(1000, 0)
	n.now = func() time.Time { return now }
	for range 20 {
		n.send(notification{Summary: "x"}, "a")
	}
	n.send(notification{Summary: "y"}, "b")
	if len(q) != notifyBurst+1 {
		t.Fatalf("queued %d, want burst %d plus the other owner", len(q), notifyBurst)
	}
	now = now.Add(notifyRefill)
	n.send(notification{Summary: "z"}, "a")
	if len(q) != notifyBurst+2 {
		t.Fatal("bucket did not refill")
	}
}

func TestNotifierTruncatesText(t *testing.T) {
	n, q := newTestNotifier()
	n.send(notification{Summary: strings.Repeat("s", 10000), Body: strings.Repeat("é", 100000)}, "a")
	got := (<-q).n
	if utf8.RuneCountInString(got.Summary) > maxNotifySummary || utf8.RuneCountInString(got.Body) > maxNotifyBody {
		t.Fatalf("summary %d / body %d runes", utf8.RuneCountInString(got.Summary), utf8.RuneCountInString(got.Body))
	}
	if !utf8.ValidString(got.Body) {
		t.Fatal("truncation split a rune")
	}
}

func TestNotifierBoundsTags(t *testing.T) {
	n, _ := newTestNotifier()
	for i := range 1000 {
		n.remember("tag"+itoa(i), uint32(i), nil)
	}
	if len(n.tags) > 257 {
		t.Fatalf("tags grew to %d", len(n.tags))
	}
}

func TestIPCDirRejectsInsecureDirs(t *testing.T) {
	t.Setenv("RAVEN_TERMINAL_SOCKET", "")
	open := t.TempDir()
	os.Chmod(open, 0o777)
	t.Setenv("XDG_RUNTIME_DIR", open)
	if _, _, err := startIPCServerWith(func(ipcRequest) ipcResponse { return ipcResponse{} }); err == nil {
		t.Fatal("served from a world-writable runtime dir")
	}

	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)
	other := t.TempDir()
	if err := os.Symlink(other, filepath.Join(base, "raven-terminal")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := startIPCServerWith(func(ipcRequest) ipcResponse { return ipcResponse{} }); err == nil {
		t.Fatal("served from a symlinked socket dir")
	}
	if p := findIPCSocket(); p != "" {
		t.Fatalf("client trusted %s", p)
	}

	good := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", good)
	os.Mkdir(filepath.Join(good, "raven-terminal"), 0o755)
	_, stop, err := startIPCServerWith(func(ipcRequest) ipcResponse { return ipcResponse{} })
	if err != nil {
		t.Fatalf("own dir with loose mode refused: %v", err)
	}
	defer stop()
	if st, _ := os.Stat(filepath.Join(good, "raven-terminal")); st.Mode().Perm() != 0o700 {
		t.Fatalf("own socket dir mode %v not tightened", st.Mode().Perm())
	}
	if findIPCSocket() == "" {
		t.Fatal("client refused a secure dir")
	}
}

func TestPaletteListsSettings(t *testing.T) {
	entries := paletteEntries(nil, nil)
	find := func(sub string) paletteEntry {
		t.Helper()
		for _, e := range entries {
			if e.Apply != nil && strings.Contains(strings.ToLower(e.Label), sub) {
				return e
			}
		}
		t.Fatalf("no settings entry for %q", sub)
		return paletteEntry{}
	}
	c := config.DefaultConfig()
	find("ligatures").Apply(c)
	find("marks gutter").Apply(c)
	find("reduced motion").Apply(c)
	find("cursor blink").Apply(c)
	find("desktop notifications").Apply(c)
	find("bell: none").Apply(c)
	find("cursor style: bar").Apply(c)
	find("notify after commands: off").Apply(c)
	find("scrollback: 100000").Apply(c)
	d := config.DefaultConfig()
	if c.Appearance.Ligatures == d.Appearance.Ligatures || c.Appearance.MarksGutter == d.Appearance.MarksGutter ||
		c.Appearance.ReducedMotion == d.Appearance.ReducedMotion || c.Appearance.CursorBlink == d.Appearance.CursorBlink ||
		c.Notifications == d.Notifications || c.Bell != "none" || c.Appearance.CursorStyle != "bar" ||
		c.NotifyCommandAfter != 0 || c.ScrollbackLines != 100000 {
		t.Fatalf("settings not applied: %+v", c)
	}
}

func TestCursorStyleConfig(t *testing.T) {
	for in, want := range map[string]parser.CursorStyle{
		"block": parser.CursorStyleBlock, "underline": parser.CursorStyleUnderline,
		"bar": parser.CursorStyleBar, "": parser.CursorStyleBlock, "beam": parser.CursorStyleBar,
	} {
		if got := cursorStyleFor(in); got != want {
			t.Errorf("cursorStyleFor(%q) = %v, want %v", in, got, want)
		}
	}
	c := config.DefaultConfig()
	c.Appearance.CursorStyle = "bar"
	if d := diffConfig(config.DefaultConfig(), c); d != (configDelta{cursor: true}) {
		t.Fatalf("cursor style delta = %+v", d)
	}
}

func TestPrunePanesForgetsClosedPaneIPCID(t *testing.T) {
	closed, moved := &tab.Pane{}, &tab.Pane{}
	closed.Close()
	ipcIDs[closed], ipcIDs[moved] = 9001, 9002
	defer delete(ipcIDs, moved)
	a := &App{pumpTick: 2, panes: map[*tab.Pane]paneState{closed: {seen: 1}, moved: {seen: 1}}}
	a.prunePanes()
	if _, ok := ipcIDs[closed]; ok {
		t.Fatal("closed pane keeps its IPC id")
	}
	if ipcIDs[moved] != 9002 {
		t.Fatal("a pane that left this window (not closed) must keep its id")
	}
}
