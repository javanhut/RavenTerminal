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
