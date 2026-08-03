package tab

import "testing"

// leafDirs lists the recorded directories of a layout's leaves, left to right.
func leafDirs(l Layout) []string {
	if len(l.Children) == 0 {
		return []string{l.Dir}
	}
	var out []string
	for _, c := range l.Children {
		out = append(out, leafDirs(c)...)
	}
	return out
}

// layoutOf is what gets persisted, so it must describe the tree faithfully —
// split direction, ratio, and nesting. A wrong shape here silently restores
// the user into a different layout than they left.
func TestLayoutOfDescribesNestedSplits(t *testing.T) {
	// A vertical root whose second child is split horizontally.
	inner := &SplitNode{SplitDir: SplitHorizontal, Ratio: 0.25}
	inner.Children = []*SplitNode{
		{Pane: &Pane{}, Parent: inner},
		{Pane: &Pane{}, Parent: inner},
	}
	root := &SplitNode{SplitDir: SplitVertical, Ratio: 0.7}
	root.Children = []*SplitNode{{Pane: &Pane{}, Parent: root}, inner}
	inner.Parent = root

	got := layoutOf(root)

	if got.Split != "v" || got.Ratio != 0.7 {
		t.Fatalf("root = split %q ratio %v, want \"v\" 0.7", got.Split, got.Ratio)
	}
	if len(got.Children) != 2 {
		t.Fatalf("root has %d children, want 2", len(got.Children))
	}
	if got.Children[0].Split != "" || len(got.Children[0].Children) != 0 {
		t.Fatal("first child should be a leaf")
	}
	sub := got.Children[1]
	if sub.Split != "h" || sub.Ratio != 0.25 || len(sub.Children) != 2 {
		t.Fatalf("nested child = split %q ratio %v with %d children, want \"h\" 0.25 with 2",
			sub.Split, sub.Ratio, len(sub.Children))
	}
	if n := len(leafDirs(got)); n != 3 {
		t.Fatalf("layout describes %d panes, want 3", n)
	}
}

// firstDir picks the directory the pre-existing pane inherits when a subtree is
// rebuilt. If it did not follow the leftmost leaf, restore would start that
// shell in the wrong place.
func TestLayoutFirstDirFollowsLeftmostLeaf(t *testing.T) {
	l := Layout{Split: "v", Children: []Layout{
		{Split: "h", Children: []Layout{{Dir: "/left"}, {Dir: "/mid"}}},
		{Dir: "/right"},
	}}
	if got := l.firstDir(); got != "/left" {
		t.Fatalf("firstDir = %q, want %q", got, "/left")
	}
	if got := (Layout{Dir: "/only"}).firstDir(); got != "/only" {
		t.Fatalf("leaf firstDir = %q, want %q", got, "/only")
	}
}

func TestLayoutLeafCount(t *testing.T) {
	tests := []struct {
		name string
		l    Layout
		want int
	}{
		{"leaf", Layout{Dir: "/a"}, 1},
		{"one split", Layout{Split: "v", Children: []Layout{{}, {}}}, 2},
		{"nested", Layout{Split: "v", Children: []Layout{
			{Split: "h", Children: []Layout{{}, {}, {}}},
			{},
		}}, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.l.leafCount(); got != tt.want {
				t.Fatalf("leafCount = %d, want %d", got, tt.want)
			}
		})
	}
}

// A saved session must never be able to leave the user without a terminal, so
// layouts that cannot be rebuilt fall back to a plain single tab.
func TestNewTabManagerFromLayoutsFallsBackWhenNothingRestores(t *testing.T) {
	// A layout claiming more panes than the cap is rejected outright, before
	// any shell is spawned.
	tooMany := Layout{Split: "v"}
	for range MaxPanes + 1 {
		tooMany.Children = append(tooMany.Children, Layout{})
	}

	tm, err := NewTabManagerFromLayouts(80, 24, []Layout{tooMany}, 0)
	if err != nil {
		t.Fatalf("NewTabManagerFromLayouts: %v", err)
	}
	defer func() {
		for _, tb := range tm.tabs {
			tb.Close()
		}
	}()
	if got := tm.TabCount(); got != 1 {
		t.Fatalf("TabCount = %d, want 1 (fallback tab)", got)
	}
	if tm.ActiveTab() == nil {
		t.Fatal("fallback manager has no active tab")
	}
}

func TestClampIndex(t *testing.T) {
	for _, tt := range []struct{ i, n, want int }{
		{0, 3, 0}, {2, 3, 2}, {3, 3, 0}, {-1, 3, 0},
	} {
		if got := clampIndex(tt.i, tt.n); got != tt.want {
			t.Fatalf("clampIndex(%d, %d) = %d, want %d", tt.i, tt.n, got, tt.want)
		}
	}
}

// The round trip that session restore depends on: a saved layout must rebuild
// into the same tree shape, with one pane per leaf.
func TestNewTabFromLayoutRebuildsTree(t *testing.T) {
	dir := t.TempDir()
	saved := Layout{Split: "v", Ratio: 0.3, Children: []Layout{
		{Dir: dir},
		{Split: "h", Ratio: 0.6, Children: []Layout{{Dir: dir}, {Dir: dir}}},
	}}

	tb, err := newTabFromLayout(1, 80, 24, saved)
	if err != nil {
		t.Fatalf("newTabFromLayout: %v", err)
	}
	defer tb.Close()

	if got := tb.PaneCount(); got != 3 {
		t.Fatalf("PaneCount = %d, want 3", got)
	}
	if tb.GetActivePane() == nil {
		t.Fatal("restored tab has no active pane")
	}

	// Re-capturing the layout must reproduce the shape that was restored;
	// directories are not compared because the shell may report a resolved
	// path (/private/var vs /var on macOS).
	got := tb.Layout()
	if got.Split != "v" || got.Ratio != 0.3 || len(got.Children) != 2 {
		t.Fatalf("root = split %q ratio %v with %d children, want \"v\" 0.3 with 2",
			got.Split, got.Ratio, len(got.Children))
	}
	sub := got.Children[1]
	if sub.Split != "h" || sub.Ratio != 0.6 || len(sub.Children) != 2 {
		t.Fatalf("nested = split %q ratio %v with %d children, want \"h\" 0.6 with 2",
			sub.Split, sub.Ratio, len(sub.Children))
	}
	if n := len(leafDirs(got)); n != 3 {
		t.Fatalf("restored layout has %d leaves, want 3", n)
	}
}

// Tearing a tab off must MOVE it, not recreate it: the same *Tab (and so the
// same panes, PTYs, and scrollback) has to arrive in the new window. Anything
// else silently kills the user's running shell.
func TestDetachAndAdoptMovesTheLiveTab(t *testing.T) {
	a, b, c := &Tab{id: 1}, &Tab{id: 2}, &Tab{id: 3}
	src := &TabManager{tabs: []*Tab{a, b, c}, activeIndex: 2, cols: 80, rows: 24}

	got := src.DetachTab(1)
	if got != b {
		t.Fatal("DetachTab returned a different tab than the one at the index")
	}
	if src.TabCount() != 2 || src.tabs[0] != a || src.tabs[1] != c {
		t.Fatalf("source strip = %v, want [a c]", src.tabs)
	}
	if src.tabs[0].id != 1 || src.tabs[1].id != 2 {
		t.Fatalf("source IDs not renumbered: %d, %d", src.tabs[0].id, src.tabs[1].id)
	}

	dst := &TabManager{cols: 100, rows: 30}
	dst.AdoptTab(got)
	if dst.TabCount() != 1 || dst.tabs[0] != b {
		t.Fatal("adopting window did not receive the same tab pointer")
	}
	if dst.ActiveTab() != b {
		t.Fatal("adopted tab is not active in its new window")
	}
	if b.cols != 100 || b.rows != 30 {
		t.Fatalf("adopted tab kept its old grid size %dx%d, want 100x30", b.cols, b.rows)
	}
}

// A window must keep at least one tab; tearing off the last one would just be
// moving the window, and would leave the source with nothing to render.
func TestDetachRefusesTheLastTab(t *testing.T) {
	only := &Tab{id: 1}
	tm := &TabManager{tabs: []*Tab{only}}
	if got := tm.DetachTab(0); got != nil {
		t.Fatal("DetachTab tore off the last tab")
	}
	if tm.TabCount() != 1 {
		t.Fatalf("TabCount = %d, want 1", tm.TabCount())
	}

	// Out-of-range indices are no-ops, not panics.
	multi := &TabManager{tabs: []*Tab{{id: 1}, {id: 2}}}
	for _, i := range []int{-1, 2, 99} {
		if got := multi.DetachTab(i); got != nil {
			t.Fatalf("DetachTab(%d) returned %v, want nil", i, got)
		}
	}
	if multi.TabCount() != 2 {
		t.Fatalf("out-of-range detach changed the strip: %d tabs", multi.TabCount())
	}
}

// The active tab must stay valid when a tab ahead of it is torn away.
func TestDetachKeepsActiveIndexInRange(t *testing.T) {
	tm := &TabManager{tabs: []*Tab{{id: 1}, {id: 2}, {id: 3}}, activeIndex: 2}
	tm.DetachTab(2) // tear off the active one
	if tm.activeIndex != 1 {
		t.Fatalf("activeIndex = %d, want 1", tm.activeIndex)
	}
	if tm.ActiveTab() == nil {
		t.Fatal("no active tab after detaching the active one")
	}
}
