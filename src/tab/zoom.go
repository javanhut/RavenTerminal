package tab

// ToggleZoom makes the active pane fill the tab, or restores the split
// layout, and reports whether the tab is now zoomed. A single pane never zooms.
func (t *Tab) ToggleZoom() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.zoomed {
		t.zoomed = false
	} else if t.countPanes() > 1 && t.activeNode != nil && t.activeNode.IsLeaf() {
		t.zoomed = true
	}
	t.resizeNode(t.root, 0, 0, 1.0, 1.0)
	return t.zoomed
}

// Zoomed reports whether the active pane fills the tab.
func (t *Tab) Zoomed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.zoomedLeaf() != nil
}

// zoomedLeaf is the leaf shown alone, or nil. Caller holds t.mu.
func (t *Tab) zoomedLeaf() *SplitNode {
	if t.zoomed && t.activeNode != nil && t.activeNode.IsLeaf() && t.activeNode.Pane != nil {
		return t.activeNode
	}
	return nil
}

// refitZoom gives a newly active pane the full tab while zoomed. Caller holds t.mu.
func (t *Tab) refitZoom() {
	if t.zoomed {
		t.resizeNode(t.root, 0, 0, 1.0, 1.0)
	}
}

// ToggleBroadcast switches sending input to every pane in the tab.
func (t *Tab) ToggleBroadcast() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.broadcast = !t.broadcast
	return t.broadcast
}

// Broadcasting reports whether input goes to every pane.
func (t *Tab) Broadcasting() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.broadcast
}

// broadcastLocked writes data to every pane, returning the active pane's error.
func (t *Tab) broadcastLocked(data []byte) error {
	var leaves []*SplitNode
	t.collectLeaves(t.root, &leaves)
	var err error
	for _, leaf := range leaves {
		if e := leaf.Pane.Write(data); leaf == t.activeNode {
			err = e
		}
	}
	return err
}

// FocusDirection activates the nearest pane to the left, right, up or down of
// the active one, reporting whether one was found.
func (t *Tab) FocusDirection(dir string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.activeNode == nil || !t.activeNode.IsLeaf() {
		return false
	}
	var layouts []PaneLayout
	t.collectLayouts(t.root, 0, 0, 1.0, 1.0, &layouts)
	var cur PaneLayout
	for _, l := range layouts {
		if l.Pane == t.activeNode.Pane {
			cur = l
		}
	}
	const eps = 1e-4
	overlap := func(a0, a1, b0, b1 float32) float32 { return min(a1, b1) - max(a0, b0) }
	var best *Pane
	bestDist, bestOv := float32(2), float32(-1)
	for _, l := range layouts {
		if l.Pane == cur.Pane {
			continue
		}
		var dist, ov float32
		switch dir {
		case "left":
			dist, ov = cur.X-(l.X+l.Width), overlap(cur.Y, cur.Y+cur.Height, l.Y, l.Y+l.Height)
		case "right":
			dist, ov = l.X-(cur.X+cur.Width), overlap(cur.Y, cur.Y+cur.Height, l.Y, l.Y+l.Height)
		case "up":
			dist, ov = cur.Y-(l.Y+l.Height), overlap(cur.X, cur.X+cur.Width, l.X, l.X+l.Width)
		case "down":
			dist, ov = l.Y-(cur.Y+cur.Height), overlap(cur.X, cur.X+cur.Width, l.X, l.X+l.Width)
		default:
			return false
		}
		if dist < -eps || ov <= eps {
			continue
		}
		if dist < bestDist-eps || (dist < bestDist+eps && ov > bestOv) {
			best, bestDist, bestOv = l.Pane, dist, ov
		}
	}
	if best == nil {
		return false
	}
	var target *SplitNode
	t.findNodeForPane(t.root, best, &target)
	t.activeNode = target
	t.updateTerminalRef()
	t.refitZoom()
	return true
}

// NewTabWithCommand opens a tab after the active one running command in dir
// ("" = the active pane's directory).
func (tm *TabManager) NewTabWithCommand(command []string, dir string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if len(tm.tabs) >= MaxTabs {
		return nil
	}
	if dir == "" && tm.activeIndex >= 0 && tm.activeIndex < len(tm.tabs) {
		dir = tm.tabs[tm.activeIndex].freshActiveDir()
	}
	t, err := newTabSized(len(tm.tabs)+1, tm.cols, tm.rows, tm.cellW, tm.cellH, dir, command)
	if err != nil {
		return err
	}
	tm.insertAfterActive(t)
	return nil
}

// SetTitle fixes the tab title ("" returns to the program's OSC title).
func (t *Tab) SetTitle(title string) {
	t.mu.Lock()
	t.title = title
	t.mu.Unlock()
}
