package tab

// Layout is a serializable description of one tab's split tree: the shape of
// the splits, the divider ratios, and each pane's working directory. It is
// what session restore persists — no terminal contents and no process state,
// only enough to put the panes back where they were with fresh shells.
//
// A node is a leaf when it has no children; leaves carry Dir, containers carry
// Split/Ratio/Children.
type Layout struct {
	Dir      string   `json:"dir,omitempty"`
	Split    string   `json:"split,omitempty"` // "v" (side by side) or "h" (stacked)
	Ratio    float64  `json:"ratio,omitempty"`
	Children []Layout `json:"children,omitempty"`
}

// firstDir returns the working directory of the subtree's leftmost leaf. That
// leaf is the one that inherits the pane already present when the subtree is
// built, so it decides where that pane's shell starts.
func (l Layout) firstDir() string {
	for len(l.Children) > 0 {
		l = l.Children[0]
	}
	return l.Dir
}

// leafCount reports how many panes the layout describes, used to enforce
// MaxPanes before spawning any shells.
func (l Layout) leafCount() int {
	if len(l.Children) == 0 {
		return 1
	}
	n := 0
	for _, c := range l.Children {
		n += c.leafCount()
	}
	return n
}

// Layout captures the tab's current split tree for persistence.
func (t *Tab) Layout() Layout {
	t.mu.Lock()
	defer t.mu.Unlock()
	return layoutOf(t.root)
}

func layoutOf(n *SplitNode) Layout {
	if n == nil {
		return Layout{}
	}
	if n.IsLeaf() {
		return Layout{Dir: n.Pane.CurrentDir()}
	}
	split := "v"
	if n.SplitDir == SplitHorizontal {
		split = "h"
	}
	l := Layout{Split: split, Ratio: n.Ratio}
	for _, c := range n.Children {
		l.Children = append(l.Children, layoutOf(c))
	}
	return l
}

// Layouts captures every tab's split tree, in strip order.
func (tm *TabManager) Layouts() []Layout {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	out := make([]Layout, 0, len(tm.tabs))
	for _, t := range tm.tabs {
		out = append(out, t.Layout())
	}
	return out
}

// newTabFromLayout builds a tab whose split tree matches l, starting each
// pane's shell in its recorded directory.
func newTabFromLayout(id int, cols, rows uint16, l Layout) (*Tab, error) {
	// NewTab creates the single pane that the leftmost leaf will inherit, so
	// it has to start in that leaf's directory.
	t, err := NewTab(id, cols, rows, l.firstDir())
	if err != nil {
		return nil, err
	}
	if err := t.buildLayout(t.root, l); err != nil {
		t.Close() // don't leak the shells that did start
		return nil, err
	}
	t.activeNode = t.findFirstLeaf(t.root)
	t.updateTerminalRef()
	t.resizeNode(t.root, 0, 0, 1.0, 1.0)
	return t, nil
}

// buildLayout expands node — currently a leaf holding one live pane — into the
// subtree described by l, spawning a pane per additional leaf. The existing
// pane becomes the first child so no shell is wasted.
func (t *Tab) buildLayout(node *SplitNode, l Layout) error {
	if len(l.Children) == 0 {
		return nil
	}
	dir := SplitVertical
	if l.Split == "h" {
		dir = SplitHorizontal
	}
	ratio := l.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}

	existing := node.Pane
	node.Pane = nil
	node.SplitDir = dir
	node.Ratio = ratio
	node.Children = []*SplitNode{{Pane: existing, Parent: node, Ratio: 0.5}}

	for _, child := range l.Children[1:] {
		if t.countPanes() >= MaxPanes {
			break // saved layout exceeds the cap: keep what fits
		}
		pane, err := NewPane(t.nextPaneID, cellsFor(t.cols), cellsFor(t.rows), child.firstDir())
		if err != nil {
			return err
		}
		t.nextPaneID++
		node.Children = append(node.Children, &SplitNode{Pane: pane, Parent: node, Ratio: 0.5})
	}

	// Recurse only into the children that actually got built.
	for i := range node.Children {
		if err := t.buildLayout(node.Children[i], l.Children[i]); err != nil {
			return err
		}
	}
	return nil
}

// cellsFor gives a newly spawned pane a non-zero starting size; the real
// dimensions are assigned by the resizeNode pass once the tree is complete.
func cellsFor(n uint16) uint16 {
	if n < 2 {
		return 1
	}
	return n / 2
}

// NewTabManagerFromLayouts restores a saved session. Tabs that fail to rebuild
// are skipped rather than aborting the restore, and if nothing survives (or
// layouts is empty) it falls back to a normal single-tab manager — a stale or
// corrupt session file must never leave the user without a terminal.
func NewTabManagerFromLayouts(cols, rows uint16, layouts []Layout, activeIndex int) (*TabManager, error) {
	tm := &TabManager{
		tabs: make([]*Tab, 0, MaxTabs),
		cols: cols,
		rows: rows,
	}
	for _, l := range layouts {
		if len(tm.tabs) >= MaxTabs || l.leafCount() > MaxPanes {
			continue
		}
		t, err := newTabFromLayout(len(tm.tabs)+1, cols, rows, l)
		if err != nil {
			continue
		}
		tm.tabs = append(tm.tabs, t)
	}
	if len(tm.tabs) == 0 {
		return NewTabManager(cols, rows)
	}
	tm.activeIndex = clampIndex(activeIndex, len(tm.tabs))
	return tm, nil
}

// NewTabManagerWith builds a manager around an already-running tab. Used when
// a tab is torn off its strip into a window of its own: the tab keeps its
// panes, shells, and scrollback: only its owner changes.
func NewTabManagerWith(cols, rows uint16, t *Tab) *TabManager {
	tm := &TabManager{
		tabs: make([]*Tab, 0, MaxTabs),
		cols: cols,
		rows: rows,
	}
	tm.AdoptTab(t)
	return tm
}

func clampIndex(i, n int) int {
	if i < 0 || i >= n {
		return 0
	}
	return i
}
