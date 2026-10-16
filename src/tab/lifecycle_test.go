package tab

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javanhut/RavenTerminal/src/parser"
)

func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func cmdPane(t *testing.T, id int, argv ...string) *Pane {
	t.Helper()
	p, err := newPaneCommand(id, 80, 24, 0, 0, "", argv)
	if err != nil {
		t.Fatalf("start %v: %v", argv, err)
	}
	t.Cleanup(p.Close)
	return p
}

func cmdTab(t *testing.T, argv ...string) *Tab {
	t.Helper()
	tb, err := newTabCommand(1, 80, 24, "", argv)
	if err != nil {
		t.Fatalf("start %v: %v", argv, err)
	}
	t.Cleanup(tb.Close)
	return tb
}

func waitPaneExited(t *testing.T, p *Pane) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !p.HasExited() {
		if time.Now().After(deadline) {
			t.Fatal("pane never noticed its process exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func screenText(term *parser.Terminal) string {
	g := term.GetGrid()
	var sb strings.Builder
	for r := 0; r < g.Rows; r++ {
		for c := 0; c < g.Cols; c++ {
			ch := g.DisplayCell(c, r).Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestCellPixelSizeReachesPTY(t *testing.T) {
	isolateHome(t)
	tm, err := NewTabManagerIn(80, 24, "", []string{"sleep", "30"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tm.CloseAll)
	tm.SetCellPixelSize(9, 18)
	p := tm.ActiveTab().GetActivePane()
	if c, r, w, h, _ := p.pty.Size(); c != 80 || r != 24 || w != 720 || h != 432 {
		t.Fatalf("winsize %dx%d %dx%dpx, want 80x24 720x432px", c, r, w, h)
	}
	tm.ResizeAll(100, 30)
	if c, r, w, h, _ := p.pty.Size(); c != 100 || r != 30 || w != 900 || h != 540 {
		t.Fatalf("after resize %dx%d %dx%dpx, want 100x30 900x540px", c, r, w, h)
	}
	if err := tm.NewTab(); err != nil {
		t.Fatal(err)
	}
	np := tm.ActiveTab().GetActivePane()
	if _, _, w, h, _ := np.pty.Size(); w != 900 || h != 540 {
		t.Fatalf("new tab winsize %dx%dpx, want 900x540px", w, h)
	}
}

func TestDeadPaneCollapsesSplit(t *testing.T) {
	isolateHome(t)
	tb := cmdTab(t, "sleep", "30")
	keep := tb.GetActivePane()
	dying := cmdPane(t, 2, "sh", "-c", "exit 3")
	tb.mu.Lock()
	tb.insertSplit(SplitVertical, dying)
	tb.mu.Unlock()
	tm := &TabManager{tabs: []*Tab{tb}}
	waitPaneExited(t, dying)
	tm.CleanupExited()
	if tm.TabCount() != 1 || tb.PaneCount() != 1 {
		t.Fatalf("tabs=%d panes=%d, want the split collapsed to one pane", tm.TabCount(), tb.PaneCount())
	}
	if tb.GetActivePane() != keep || !tb.root.IsLeaf() || tb.Terminal != keep.Terminal {
		t.Fatal("surviving pane is not the active root leaf")
	}
}

func TestLastPaneExitClosesTab(t *testing.T) {
	isolateHome(t)
	live := cmdTab(t, "sleep", "30")
	dead := cmdTab(t, "true")
	tm := &TabManager{tabs: []*Tab{dead, live}, activeIndex: 1}
	waitPaneExited(t, dead.GetActivePane())
	tm.CleanupExited()
	if tm.TabCount() != 1 || tm.ActiveTab() != live || tm.AllExited() {
		t.Fatalf("tabs=%d active ok=%v", tm.TabCount(), tm.ActiveTab() == live)
	}
	live.GetActivePane().pty.Close()
	waitPaneExited(t, live.GetActivePane())
	tm.CleanupExited()
	if tm.TabCount() != 0 || !tm.AllExited() || tm.ActiveTab() != nil {
		t.Fatal("manager should be empty once every pane is gone")
	}
}

func TestHoldKeepsDeadPaneUntilKey(t *testing.T) {
	isolateHome(t)
	SetHoldOnExit(true)
	defer SetHoldOnExit(false)
	tb := cmdTab(t, "sh", "-c", "echo bye; exit 5")
	p := tb.GetActivePane()
	tm := &TabManager{tabs: []*Tab{tb}}
	waitPaneExited(t, p)
	tm.CleanupExited()
	if tm.TabCount() != 1 {
		t.Fatal("held pane closed without a key press")
	}
	txt := screenText(p.Terminal)
	if !strings.Contains(txt, "bye") || !strings.Contains(txt, "[Process exited with status 5") {
		t.Fatalf("screen lacks exit notice:\n%s", txt)
	}
	p.Write([]byte("\x1b[I"))
	p.Write([]byte("\x1b[<0;3;4M"))
	tm.CleanupExited()
	if tm.TabCount() != 1 {
		t.Fatal("focus/mouse reports must not dismiss a held pane")
	}
	tb.Write([]byte("q"))
	tm.CleanupExited()
	if tm.TabCount() != 0 {
		t.Fatal("key press did not close the held pane")
	}
}

func TestExitWakesMainLoop(t *testing.T) {
	isolateHome(t)
	var wakes atomic.Int32
	SetWakeNotifier(func() { wakes.Add(1) })
	defer SetWakeNotifier(nil)
	p := cmdPane(t, 1, "sleep", "0.3")
	time.Sleep(100 * time.Millisecond)
	ClearWakePending()
	wakes.Store(0)
	waitPaneExited(t, p)
	if wakes.Load() == 0 {
		t.Fatal("process exit did not wake the main loop")
	}
}

func TestCloseAllKillsShellsAndStopsReaders(t *testing.T) {
	isolateHome(t)
	tm, err := NewTabManagerIn(80, 24, "", []string{"sh", "-c", "trap '' HUP; while :; do sleep 1; done"})
	if err != nil {
		t.Fatal(err)
	}
	other := cmdTab(t, "sleep", "30")
	tm.AdoptTab(other)
	var panes []*Pane
	for _, tb := range tm.GetTabs() {
		panes = append(panes, tb.GetPanes()...)
	}
	start := time.Now()
	tm.CloseAll()
	if el := time.Since(start); el > 100*time.Millisecond {
		t.Fatalf("CloseAll blocked for %v", el)
	}
	if tm.TabCount() != 0 {
		t.Fatal("tabs remain after CloseAll")
	}
	for _, p := range panes {
		select {
		case <-p.pty.Exited():
		case <-time.After(3 * time.Second):
			t.Fatal("shell survived window close")
		}
		select {
		case <-p.readerDone:
		case <-time.After(3 * time.Second):
			t.Fatal("reader goroutine survived window close")
		}
	}
}

func TestActiveDirIsCached(t *testing.T) {
	var calls atomic.Int32
	prevCwd, prevNow := paneCwd, nowFunc
	defer func() { paneCwd, nowFunc = prevCwd, prevNow }()
	paneCwd = func(*Pane) string { calls.Add(1); return "/srv/a" }
	clock := time.Unix(1000, 0)
	nowFunc = func() time.Time { return clock }

	p := testPane(1)
	tb := &Tab{root: &SplitNode{Pane: p}, Terminal: p.Terminal}
	tb.activeNode = tb.root
	for range 50 {
		if d := tb.ActiveDir(); d != "/srv/a" {
			t.Fatalf("ActiveDir = %q", d)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("cwd looked up %d times within the TTL, want 1", calls.Load())
	}
	clock = clock.Add(600 * time.Millisecond)
	tb.ActiveDir()
	if calls.Load() != 2 {
		t.Fatalf("cwd not refreshed after TTL (calls=%d)", calls.Load())
	}
	p.Terminal.Process([]byte("\x1b]7;file://host/srv/b\x07"))
	tb.ActiveDir()
	if calls.Load() != 3 {
		t.Fatalf("OSC 7 did not invalidate the cache (calls=%d)", calls.Load())
	}
}

func TestTabTitlePrefersOSCTitle(t *testing.T) {
	p := testPane(1)
	tb := &Tab{root: &SplitNode{Pane: p}, Terminal: p.Terminal}
	tb.activeNode = tb.root
	if tb.Title() != "" {
		t.Fatalf("Title = %q before any OSC title", tb.Title())
	}
	p.Terminal.Process([]byte("\x1b]2;nvim main.go\x07"))
	if tb.Title() != "nvim main.go" {
		t.Fatalf("Title = %q, want OSC 2 title", tb.Title())
	}
}
