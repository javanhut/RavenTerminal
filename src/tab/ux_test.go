package tab

import (
	"strings"
	"testing"
	"time"
)

// twoByOne builds [left | (topRight / bottomRight)] with left active.
func twoByOne() (*Tab, *Pane, *Pane, *Pane) {
	left, topRight, bottomRight := testPane(1), testPane(2), testPane(3)
	right := &SplitNode{SplitDir: SplitHorizontal, Ratio: 0.5}
	right.Children = []*SplitNode{
		{Pane: topRight, Ratio: 0.5, Parent: right},
		{Pane: bottomRight, Ratio: 0.5, Parent: right},
	}
	root := &SplitNode{SplitDir: SplitVertical, Ratio: 0.5}
	root.Children = []*SplitNode{{Pane: left, Ratio: 0.5, Parent: root}, right}
	right.Parent = root
	return &Tab{root: root, activeNode: root.Children[0], Terminal: left.Terminal, cols: 80, rows: 24}, left, topRight, bottomRight
}

func TestPaneZoomFillsTabAndRestores(t *testing.T) {
	tb, left, topRight, _ := twoByOne()
	if !tb.ToggleZoom() || !tb.Zoomed() {
		t.Fatal("ToggleZoom did not zoom a split tab")
	}
	l := tb.GetPaneLayouts()
	if len(l) != 1 || l[0].Pane != left || l[0].Width != 1 || l[0].Height != 1 {
		t.Fatalf("zoomed layouts = %+v, want the active pane alone at full size", l)
	}
	if g := left.Terminal.GetGrid(); g.Cols != 80 || g.Rows != 24 {
		t.Fatalf("zoomed pane grid = %dx%d, want 80x24", g.Cols, g.Rows)
	}
	tb.NextPane()
	if l := tb.GetPaneLayouts(); len(l) != 1 || l[0].Pane != topRight {
		t.Fatal("switching panes while zoomed must show the new pane zoomed")
	}
	if tb.ToggleZoom() || tb.Zoomed() {
		t.Fatal("second toggle must unzoom")
	}
	if l := tb.GetPaneLayouts(); len(l) != 3 {
		t.Fatalf("unzoomed layouts = %d panes, want the split restored", len(l))
	}
	if g := left.Terminal.GetGrid(); g.Cols != 40 {
		t.Fatalf("restored left pane cols = %d, want 40", g.Cols)
	}

	single := &Tab{root: &SplitNode{Pane: testPane(1)}}
	single.activeNode = single.root
	if single.ToggleZoom() {
		t.Fatal("a single-pane tab has nothing to zoom")
	}
}

func TestFocusDirection(t *testing.T) {
	tb, left, topRight, bottomRight := twoByOne()
	if !tb.FocusDirection("right") || tb.GetActivePane() != topRight {
		t.Fatal("right from left should reach the top-right pane")
	}
	if !tb.FocusDirection("down") || tb.GetActivePane() != bottomRight {
		t.Fatal("down should reach bottom-right")
	}
	if tb.FocusDirection("down") {
		t.Fatal("no pane below bottom-right")
	}
	if !tb.FocusDirection("left") || tb.GetActivePane() != left {
		t.Fatal("left should return to the left pane")
	}
	if tb.FocusDirection("up") || tb.FocusDirection("bogus") {
		t.Fatal("no pane above / unknown direction must fail")
	}
}

func TestBroadcastWritesEveryPane(t *testing.T) {
	isolateHome(t)
	tb := cmdTab(t, "cat")
	tb.mu.Lock()
	second := cmdPane(t, 2, "cat")
	tb.insertSplit(SplitVertical, second)
	tb.mu.Unlock()
	panes := tb.GetPanes()
	if len(panes) != 2 {
		t.Fatalf("want 2 panes, got %d", len(panes))
	}
	if tb.ToggleBroadcast() != true || !tb.Broadcasting() {
		t.Fatal("broadcast did not turn on")
	}
	if err := tb.Write([]byte("hello-all\r")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, p := range panes {
		for !strings.Contains(screenText(p.Terminal), "hello-all") {
			if time.Now().After(deadline) {
				t.Fatalf("pane %d never received the broadcast:\n%s", p.ID(), screenText(p.Terminal))
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	tb.ToggleBroadcast()
	tb.Write([]byte("only-active\r"))
	time.Sleep(200 * time.Millisecond)
	if strings.Contains(screenText(panes[0].Terminal), "only-active") {
		t.Fatal("with broadcast off, inactive panes must not get input")
	}
}

func TestNewTabWithCommand(t *testing.T) {
	isolateHome(t)
	tm := &TabManager{cols: 80, rows: 24}
	if err := tm.NewTabWithCommand([]string{"sh", "-c", "echo pager-ran; sleep 5"}, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tm.CloseAll)
	if tm.TabCount() != 1 {
		t.Fatalf("TabCount = %d", tm.TabCount())
	}
	term := tm.ActiveTab().Terminal
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(screenText(term), "pager-ran") {
		if time.Now().After(deadline) {
			t.Fatal("command tab never ran its command")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFocusReportGoesToActivePaneOnly(t *testing.T) {
	isolateHome(t)
	tb := cmdTab(t, "cat")
	tb.mu.Lock()
	second := cmdPane(t, 2, "cat")
	tb.insertSplit(SplitVertical, second)
	tb.mu.Unlock()
	panes := tb.GetPanes()
	tb.ToggleBroadcast()
	active := tb.GetActivePane()
	if tb.WriteFocus(true) {
		t.Fatal("focus report sent without ?1004")
	}
	for _, p := range panes {
		p.Terminal.Process([]byte("\x1b[?1004h"))
	}
	if !tb.WriteFocus(true) {
		t.Fatal("focus report not sent")
	}
	tb.Write([]byte("mark\r"))
	deadline := time.Now().Add(3 * time.Second)
	for _, p := range panes {
		for !strings.Contains(screenText(p.Terminal), "mark") {
			if time.Now().After(deadline) {
				t.Fatalf("pane %d never got the marker", p.ID())
			}
			time.Sleep(10 * time.Millisecond)
		}
		has := strings.Contains(screenText(p.Terminal), "[I")
		if has != (p == active) {
			t.Fatalf("pane %d (active=%v) focus report present=%v:\n%s", p.ID(), p == active, has, screenText(p.Terminal))
		}
	}
}

func TestPaneWriteFocusNeedsReporting(t *testing.T) {
	isolateHome(t)
	p := cmdPane(t, 1, "cat")
	if p.WriteFocus(true) {
		t.Fatal("focus report sent without ?1004")
	}
	p.Terminal.Process([]byte("\x1b[?1004h"))
	if !p.WriteFocus(true) {
		t.Fatal("focus report not sent with ?1004 on")
	}
}
