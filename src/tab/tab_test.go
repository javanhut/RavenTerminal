package tab

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/parser"
)

func testPane(id int) *Pane {
	return &Pane{id: id, Terminal: parser.NewTerminal(80, 24)}
}

func TestSplitTreeLayoutsAndNavigation(t *testing.T) {
	left, topRight, bottomRight := testPane(1), testPane(2), testPane(3)
	right := &SplitNode{SplitDir: SplitHorizontal, Ratio: 0.5}
	right.Children = []*SplitNode{
		{Pane: topRight, Ratio: 0.5, Parent: right},
		{Pane: bottomRight, Ratio: 0.5, Parent: right},
	}
	root := &SplitNode{SplitDir: SplitVertical, Ratio: 0.5}
	root.Children = []*SplitNode{
		{Pane: left, Ratio: 0.5, Parent: root},
		right,
	}
	right.Parent = root
	tab := &Tab{root: root, activeNode: root.Children[0], Terminal: left.Terminal}

	if got := tab.PaneCount(); got != 3 {
		t.Fatalf("PaneCount = %d, want 3", got)
	}
	layouts := tab.GetPaneLayouts()
	if len(layouts) != 3 || layouts[0].Width != 0.5 || layouts[1].Height != 0.5 {
		t.Fatalf("unexpected layouts: %#v", layouts)
	}
	tab.NextPane()
	if tab.GetActivePane() != topRight {
		t.Fatal("NextPane did not select the next leaf")
	}
	tab.PrevPane()
	if tab.GetActivePane() != left {
		t.Fatal("PrevPane did not wrap back to the prior leaf")
	}
}

func TestClipboardMailbox(t *testing.T) {
	_, _ = DrainClipboard()
	QueueClipboard("first")
	QueueClipboard("latest")
	if got, ok := DrainClipboard(); !ok || got != "latest" {
		t.Fatalf("DrainClipboard = %q, %v", got, ok)
	}
	if _, ok := DrainClipboard(); ok {
		t.Fatal("clipboard mailbox was not cleared")
	}
}

func TestTabManagerMoveTab(t *testing.T) {
	newManager := func(active int) *TabManager {
		return &TabManager{
			tabs:        []*Tab{{id: 1}, {id: 2}, {id: 3}},
			activeIndex: active,
		}
	}
	order := func(tm *TabManager) []int {
		ids := make([]int, len(tm.tabs))
		for i, tb := range tm.tabs {
			ids[i] = tb.id
		}
		return ids
	}

	t.Run("move forward renumbers and keeps active tab", func(t *testing.T) {
		tm := newManager(0)
		tm.MoveTab(0, 2)
		if got := order(tm); got[0] != 1 || got[1] != 2 || got[2] != 3 {
			t.Fatalf("IDs not renumbered: %v", got)
		}
		// The moved tab was active; it must still be active at its new slot.
		if tm.activeIndex != 2 {
			t.Fatalf("activeIndex = %d, want 2", tm.activeIndex)
		}
		if tm.tabs[2] != tm.ActiveTab() {
			t.Fatal("ActiveTab does not match activeIndex")
		}
	})

	t.Run("move backward", func(t *testing.T) {
		tm := newManager(2)
		tm.MoveTab(2, 0)
		if tm.activeIndex != 0 {
			t.Fatalf("activeIndex = %d, want 0", tm.activeIndex)
		}
		if got := order(tm); got[0] != 1 || got[1] != 2 || got[2] != 3 {
			t.Fatalf("IDs not renumbered: %v", got)
		}
	})

	t.Run("active tab follows when another tab crosses it", func(t *testing.T) {
		tm := newManager(0) // first tab active
		tm.MoveTab(2, 0)    // last tab jumps to the front
		if tm.activeIndex != 1 {
			t.Fatalf("activeIndex = %d, want 1", tm.activeIndex)
		}
	})

	t.Run("no-op on invalid indices", func(t *testing.T) {
		for _, m := range [][2]int{{0, 0}, {-1, 1}, {0, 3}, {1, -1}} {
			tm := newManager(1)
			before := order(tm)
			tm.MoveTab(m[0], m[1])
			if tm.activeIndex != 1 {
				t.Fatalf("MoveTab(%d,%d): activeIndex = %d, want 1", m[0], m[1], tm.activeIndex)
			}
			if got := order(tm); got[0] != before[0] || got[1] != before[1] || got[2] != before[2] {
				t.Fatalf("MoveTab(%d,%d) changed order: %v", m[0], m[1], got)
			}
		}
	})
}
