package tab

import (
	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/shell"
	"sync"
)

const MaxTabs = 10
const MaxPanes = 16

// SplitDirection indicates how a node is split
type SplitDirection int

const (
	SplitNone       SplitDirection = iota
	SplitVertical                  // Children arranged left to right
	SplitHorizontal                // Children arranged top to bottom
)

// ResizeDirection indicates the direction to resize relative to the active pane.
type ResizeDirection int

const (
	ResizeLeft ResizeDirection = iota
	ResizeRight
	ResizeUp
	ResizeDown
)

const (
	minSplitRatio = 0.1
	maxSplitRatio = 0.9
)

// SplitNode represents a node in the split tree
// It can either be a leaf (containing a Pane) or a container (containing children)
type SplitNode struct {
	// For leaf nodes
	Pane *Pane

	// For container nodes
	SplitDir SplitDirection
	Children []*SplitNode
	Ratio    float64 // Size ratio (0.0 to 1.0), default 0.5 for equal split

	// Parent reference for navigation
	Parent *SplitNode
}

// IsLeaf returns true if this node contains a pane
func (n *SplitNode) IsLeaf() bool {
	return n.Pane != nil
}

// Pane represents a single terminal pane within a tab
type Pane struct {
	Terminal *parser.Terminal
	pty      *shell.PtySession
	id       int
	exited   bool
	exitedMu sync.Mutex
	readerMu sync.Mutex
}

// NewPane creates a new terminal pane
func NewPane(id int, cols, rows uint16, startDir string) (*Pane, error) {
	pty, err := shell.NewPtySession(cols, rows, startDir)
	if err != nil {
		return nil, err
	}

	pane := &Pane{
		Terminal: parser.NewTerminal(int(cols), int(rows)),
		pty:      pty,
		id:       id,
		exited:   false,
	}
	pane.Terminal.SetResponseWriter(func(data []byte) {
		_, _ = pty.Write(data)
	})

	// Start reader goroutine
	go pane.readLoop()

	return pane, nil
}

// readLoop continuously reads from the PTY and processes output
func (p *Pane) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := p.pty.Read(buf)
		if err != nil || n == 0 {
			p.exitedMu.Lock()
			p.exited = true
			p.exitedMu.Unlock()
			return
		}

		p.readerMu.Lock()
		p.Terminal.Process(buf[:n])
		p.readerMu.Unlock()
	}
}

// Write writes data to the PTY
func (p *Pane) Write(data []byte) error {
	_, err := p.pty.Write(data)
	return err
}

// HasExited returns true if the shell has exited
func (p *Pane) HasExited() bool {
	p.exitedMu.Lock()
	defer p.exitedMu.Unlock()
	return p.exited || p.pty.HasExited()
}

// Resize resizes the pane
func (p *Pane) Resize(cols, rows uint16) {
	p.readerMu.Lock()
	defer p.readerMu.Unlock()
	p.Terminal.Resize(int(cols), int(rows))
	p.pty.Resize(cols, rows)
}

// Close closes the pane
func (p *Pane) Close() {
	p.pty.Close()
}

// CurrentDir returns the pane working directory when available.
func (p *Pane) CurrentDir() string {
	if p == nil || p.pty == nil {
		return ""
	}
	if dir := p.pty.CurrentDir(); dir != "" {
		return dir
	}
	if p.Terminal == nil {
		return ""
	}
	return p.Terminal.WorkingDir()
}

// ID returns the pane ID
func (p *Pane) ID() int {
	return p.id
}

// PaneLayout contains layout information for rendering a pane
type PaneLayout struct {
	Pane   *Pane
	X      float32 // Offset X (0.0 to 1.0)
	Y      float32 // Offset Y (0.0 to 1.0)
	Width  float32 // Width (0.0 to 1.0)
	Height float32 // Height (0.0 to 1.0)
}

// Tab represents a single terminal tab with nested splits
type Tab struct {
	Terminal   *parser.Terminal // For backward compatibility - points to active pane's terminal
	pty        *shell.PtySession
	id         int
	root       *SplitNode
	activeNode *SplitNode // Points to the currently active leaf node
	nextPaneID int
	cols       uint16
	rows       uint16
	mu         sync.Mutex
}

// NewTab creates a new terminal tab
func NewTab(id int, cols, rows uint16, startDir string) (*Tab, error) {
	// Create the first pane
	pane, err := NewPane(1, cols, rows, startDir)
	if err != nil {
		return nil, err
	}

	rootNode := &SplitNode{
		Pane:  pane,
		Ratio: 1.0,
	}

	tab := &Tab{
		Terminal:   pane.Terminal,
		pty:        pane.pty,
		id:         id,
		root:       rootNode,
		activeNode: rootNode,
		nextPaneID: 2,
		cols:       cols,
		rows:       rows,
	}

	return tab, nil
}

// countPanes counts total panes in the tree
func (t *Tab) countPanes() int {
	return countPanesInNode(t.root)
}

func countPanesInNode(node *SplitNode) int {
	if node == nil {
		return 0
	}
	if node.IsLeaf() {
		return 1
	}
	count := 0
	for _, child := range node.Children {
		count += countPanesInNode(child)
	}
	return count
}

// SplitVertical splits the current pane vertically (side by side)
func (t *Tab) SplitVertical() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.countPanes() >= MaxPanes {
		return nil
	}

	return t.splitActivePane(SplitVertical)
}

// SplitHorizontal splits the current pane horizontally (stacked)
func (t *Tab) SplitHorizontal() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.countPanes() >= MaxPanes {
		return nil
	}

	return t.splitActivePane(SplitHorizontal)
}

// splitActivePane splits the active pane in the given direction
func (t *Tab) splitActivePane(dir SplitDirection) error {
	if t.activeNode == nil || !t.activeNode.IsLeaf() {
		return nil
	}

	// Create new pane
	startDir := t.activeNode.Pane.CurrentDir()
	newPane, err := NewPane(t.nextPaneID, t.cols/2, t.rows/2, startDir)
	if err != nil {
		return err
	}
	t.nextPaneID++

	// Create new leaf node for the new pane
	newLeaf := &SplitNode{
		Pane:  newPane,
		Ratio: 0.5,
	}

	// Get the current pane from active node
	currentPane := t.activeNode.Pane

	// Convert the active node from a leaf to a container
	t.activeNode.Pane = nil
	t.activeNode.SplitDir = dir
	t.activeNode.Ratio = 0.5

	// Create a leaf node for the existing pane
	existingLeaf := &SplitNode{
		Pane:   currentPane,
		Ratio:  0.5,
		Parent: t.activeNode,
	}

	// Set parent for new leaf
	newLeaf.Parent = t.activeNode

	// Add children (existing pane first, then new pane)
	t.activeNode.Children = []*SplitNode{existingLeaf, newLeaf}

	// Move active to the new pane
	t.activeNode = newLeaf
	t.updateTerminalRef()

	// Recalculate sizes
	t.resizeNode(t.root, 0, 0, 1.0, 1.0)

	return nil
}

// ClosePane closes the current pane
func (t *Tab) ClosePane() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeNode == nil || !t.activeNode.IsLeaf() {
		return
	}

	// Don't close the last pane
	if t.countPanes() <= 1 {
		return
	}

	parent := t.activeNode.Parent
	if parent == nil {
		return // Can't close root
	}

	// Close the pane
	t.activeNode.Pane.Close()

	// Find sibling
	var sibling *SplitNode
	for _, child := range parent.Children {
		if child != t.activeNode {
			sibling = child
			break
		}
	}

	if sibling == nil {
		return
	}

	// Replace parent with sibling
	if parent.Parent == nil {
		// Parent is root
		t.root = sibling
		sibling.Parent = nil
	} else {
		// Replace parent with sibling in grandparent's children
		grandparent := parent.Parent
		for i, child := range grandparent.Children {
			if child == parent {
				grandparent.Children[i] = sibling
				sibling.Parent = grandparent
				break
			}
		}
	}

	// Set active to sibling (or first leaf in sibling if it's a container)
	t.activeNode = t.findFirstLeaf(sibling)
	t.updateTerminalRef()

	// Recalculate sizes
	t.resizeNode(t.root, 0, 0, 1.0, 1.0)
}

// findFirstLeaf finds the first leaf node in a subtree
func (t *Tab) findFirstLeaf(node *SplitNode) *SplitNode {
	if node.IsLeaf() {
		return node
	}
	if len(node.Children) > 0 {
		return t.findFirstLeaf(node.Children[0])
	}
	return nil
}

// collectLeaves collects all leaf nodes in order
func (t *Tab) collectLeaves(node *SplitNode, leaves *[]*SplitNode) {
	if node == nil {
		return
	}
	if node.IsLeaf() {
		*leaves = append(*leaves, node)
		return
	}
	for _, child := range node.Children {
		t.collectLeaves(child, leaves)
	}
}

// NextPane switches to the next pane
func (t *Tab) NextPane() {
	t.mu.Lock()
	defer t.mu.Unlock()

	var leaves []*SplitNode
	t.collectLeaves(t.root, &leaves)

	if len(leaves) <= 1 {
		return
	}

	// Find current index
	currentIdx := 0
	for i, leaf := range leaves {
		if leaf == t.activeNode {
			currentIdx = i
			break
		}
	}

	// Move to next
	nextIdx := (currentIdx - 1 + len(leaves)) % len(leaves)
	t.activeNode = leaves[nextIdx]
	t.updateTerminalRef()
}

// PrevPane switches to the previous pane
func (t *Tab) PrevPane() {
	t.mu.Lock()
	defer t.mu.Unlock()

	var leaves []*SplitNode
	t.collectLeaves(t.root, &leaves)

	if len(leaves) <= 1 {
		return
	}

	// Find current index
	currentIdx := 0
	for i, leaf := range leaves {
		if leaf == t.activeNode {
			currentIdx = i
			break
		}
	}

	// Move to previous
	prevIdx := (currentIdx + 1) % len(leaves)
	t.activeNode = leaves[prevIdx]
	t.updateTerminalRef()
}

// ResizeActivePane expands the active pane toward the given direction when possible.
func (t *Tab) ResizeActivePane(direction ResizeDirection, delta float64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeNode == nil {
		return false
	}

	if delta < 0 {
		delta = -delta
	}

	var splitDir SplitDirection
	ratioDelta := delta
	switch direction {
	case ResizeLeft:
		splitDir = SplitVertical
		ratioDelta = -delta
	case ResizeRight:
		splitDir = SplitVertical
		ratioDelta = delta
	case ResizeUp:
		splitDir = SplitHorizontal
		ratioDelta = -delta
	case ResizeDown:
		splitDir = SplitHorizontal
		ratioDelta = delta
	default:
		return false
	}

	node := t.activeNode
	for node.Parent != nil {
		parent := node.Parent
		if parent.SplitDir == splitDir && len(parent.Children) == 2 {
			ratio := parent.Ratio
			if ratio <= 0.0 || ratio >= 1.0 {
				ratio = 0.5
			}
			ratio += ratioDelta
			if ratio < minSplitRatio {
				ratio = minSplitRatio
			}
			if ratio > maxSplitRatio {
				ratio = maxSplitRatio
			}
			if ratio == parent.Ratio {
				return false
			}
			parent.Ratio = ratio
			t.resizeNode(t.root, 0, 0, 1.0, 1.0)
			return true
		}
		node = parent
	}

	return false
}

// updateTerminalRef updates the Terminal reference to point to active pane
func (t *Tab) updateTerminalRef() {
	if t.activeNode != nil && t.activeNode.IsLeaf() && t.activeNode.Pane != nil {
		t.Terminal = t.activeNode.Pane.Terminal
		t.pty = t.activeNode.Pane.pty
	}
}

// resizeNode recursively resizes nodes
func (t *Tab) resizeNode(node *SplitNode, x, y, width, height float32) {
	if node == nil {
		return
	}

	if node.IsLeaf() {
		// Calculate actual pixel dimensions
		cols := uint16(float32(t.cols) * width)
		rows := uint16(float32(t.rows) * height)
		if cols < 1 {
			cols = 1
		}
		if rows < 1 {
			rows = 1
		}
		node.Pane.Resize(cols, rows)
		return
	}

	// Container node - divide space among children
	numChildren := len(node.Children)
	if numChildren == 0 {
		return
	}

	switch node.SplitDir {
	case SplitVertical:
		if numChildren == 2 {
			ratio := float32(node.Ratio)
			if ratio <= 0.0 || ratio >= 1.0 {
				ratio = 0.5
			}
			firstWidth := width * ratio
			secondWidth := width - firstWidth
			t.resizeNode(node.Children[0], x, y, firstWidth, height)
			t.resizeNode(node.Children[1], x+firstWidth, y, secondWidth, height)
		} else {
			// Divide width equally
			childWidth := width / float32(numChildren)
			for i, child := range node.Children {
				childX := x + float32(i)*childWidth
				t.resizeNode(child, childX, y, childWidth, height)
			}
		}
	case SplitHorizontal:
		if numChildren == 2 {
			ratio := float32(node.Ratio)
			if ratio <= 0.0 || ratio >= 1.0 {
				ratio = 0.5
			}
			firstHeight := height * ratio
			secondHeight := height - firstHeight
			t.resizeNode(node.Children[0], x, y, width, firstHeight)
			t.resizeNode(node.Children[1], x, y+firstHeight, width, secondHeight)
		} else {
			// Divide height equally
			childHeight := height / float32(numChildren)
			for i, child := range node.Children {
				childY := y + float32(i)*childHeight
				t.resizeNode(child, x, childY, width, childHeight)
			}
		}
	}
}

// GetPaneLayouts returns layout information for all panes (for rendering)
func (t *Tab) GetPaneLayouts() []PaneLayout {
	t.mu.Lock()
	defer t.mu.Unlock()

	var layouts []PaneLayout
	t.collectLayouts(t.root, 0, 0, 1.0, 1.0, &layouts)
	return layouts
}

func (t *Tab) collectLayouts(node *SplitNode, x, y, width, height float32, layouts *[]PaneLayout) {
	if node == nil {
		return
	}

	if node.IsLeaf() {
		*layouts = append(*layouts, PaneLayout{
			Pane:   node.Pane,
			X:      x,
			Y:      y,
			Width:  width,
			Height: height,
		})
		return
	}

	numChildren := len(node.Children)
	if numChildren == 0 {
		return
	}

	switch node.SplitDir {
	case SplitVertical:
		if numChildren == 2 {
			ratio := float32(node.Ratio)
			if ratio <= 0.0 || ratio >= 1.0 {
				ratio = 0.5
			}
			firstWidth := width * ratio
			secondWidth := width - firstWidth
			t.collectLayouts(node.Children[0], x, y, firstWidth, height, layouts)
			t.collectLayouts(node.Children[1], x+firstWidth, y, secondWidth, height, layouts)
		} else {
			childWidth := width / float32(numChildren)
			for i, child := range node.Children {
				childX := x + float32(i)*childWidth
				t.collectLayouts(child, childX, y, childWidth, height, layouts)
			}
		}
	case SplitHorizontal:
		if numChildren == 2 {
			ratio := float32(node.Ratio)
			if ratio <= 0.0 || ratio >= 1.0 {
				ratio = 0.5
			}
			firstHeight := height * ratio
			secondHeight := height - firstHeight
			t.collectLayouts(node.Children[0], x, y, width, firstHeight, layouts)
			t.collectLayouts(node.Children[1], x, y+firstHeight, width, secondHeight, layouts)
		} else {
			childHeight := height / float32(numChildren)
			for i, child := range node.Children {
				childY := y + float32(i)*childHeight
				t.collectLayouts(child, x, childY, width, childHeight, layouts)
			}
		}
	}
}

// GetActivePane returns the active pane
func (t *Tab) GetActivePane() *Pane {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeNode != nil && t.activeNode.IsLeaf() {
		return t.activeNode.Pane
	}
	return nil
}

// SetActivePane sets the active pane by pointer.
func (t *Tab) SetActivePane(pane *Pane) bool {
	if pane == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeNode != nil && t.activeNode.Pane == pane {
		return true
	}

	var target *SplitNode
	t.findNodeForPane(t.root, pane, &target)
	if target == nil {
		return false
	}

	t.activeNode = target
	t.Terminal = target.Pane.Terminal
	t.pty = target.Pane.pty
	return true
}

func (t *Tab) findNodeForPane(node *SplitNode, pane *Pane, target **SplitNode) {
	if node == nil || *target != nil {
		return
	}
	if node.IsLeaf() {
		if node.Pane == pane {
			*target = node
		}
		return
	}
	for _, child := range node.Children {
		t.findNodeForPane(child, pane, target)
		if *target != nil {
			return
		}
	}
}

// Write writes data to the PTY (writes to active pane)
func (t *Tab) Write(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeNode != nil && t.activeNode.IsLeaf() && t.activeNode.Pane != nil {
		return t.activeNode.Pane.Write(data)
	}
	return nil
}

// HasExited returns true if all panes have exited
func (t *Tab) HasExited() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.hasExitedNode(t.root)
}

func (t *Tab) hasExitedNode(node *SplitNode) bool {
	if node == nil {
		return true
	}
	if node.IsLeaf() {
		return node.Pane.HasExited()
	}
	for _, child := range node.Children {
		if !t.hasExitedNode(child) {
			return false
		}
	}
	return true
}

// Resize resizes the tab and all panes
func (t *Tab) Resize(cols, rows uint16) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.cols = cols
	t.rows = rows
	t.resizeNode(t.root, 0, 0, 1.0, 1.0)
}

// Close closes the tab and all panes
func (t *Tab) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.closeNode(t.root)
}

func (t *Tab) closeNode(node *SplitNode) {
	if node == nil {
		return
	}
	if node.IsLeaf() {
		node.Pane.Close()
		return
	}
	for _, child := range node.Children {
		t.closeNode(child)
	}
}

// ID returns the tab ID
func (t *Tab) ID() int {
	return t.id
}

// PaneCount returns the number of panes
func (t *Tab) PaneCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.countPanes()
}

// GetPanes returns all panes in this tab (for backward compatibility)
func (t *Tab) GetPanes() []*Pane {
	t.mu.Lock()
	defer t.mu.Unlock()

	var panes []*Pane
	t.collectPanes(t.root, &panes)
	return panes
}

func (t *Tab) collectPanes(node *SplitNode, panes *[]*Pane) {
	if node == nil {
		return
	}
	if node.IsLeaf() {
		*panes = append(*panes, node.Pane)
		return
	}
	for _, child := range node.Children {
		t.collectPanes(child, panes)
	}
}

// ActivePaneIndex returns the index of the active pane
func (t *Tab) ActivePaneIndex() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	var leaves []*SplitNode
	t.collectLeaves(t.root, &leaves)

	for i, leaf := range leaves {
		if leaf == t.activeNode {
			return i
		}
	}
	return 0
}

// GetSplitDirection returns the root split direction (for backward compatibility)
func (t *Tab) GetSplitDirection() SplitDirection {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.root == nil || t.root.IsLeaf() {
		return SplitNone
	}
	return t.root.SplitDir
}

// ActiveDir returns the active pane working directory when available.
func (t *Tab) ActiveDir() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeNode == nil || t.activeNode.Pane == nil {
		return ""
	}
	return t.activeNode.Pane.CurrentDir()
}

// TabManager manages multiple terminal tabs
type TabManager struct {
	tabs        []*Tab
	activeIndex int
	cols        uint16
	rows        uint16
	mu          sync.RWMutex
}

// NewTabManager creates a new tab manager
func NewTabManager(cols, rows uint16) (*TabManager, error) {
	tm := &TabManager{
		tabs:        make([]*Tab, 0, MaxTabs),
		activeIndex: 0,
		cols:        cols,
		rows:        rows,
	}

	// Create initial tab
	if err := tm.NewTab(); err != nil {
		return nil, err
	}

	return tm, nil
}

// NewTab creates a new tab
func (tm *TabManager) NewTab() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.tabs) >= MaxTabs {
		return nil // Silently ignore if at max
	}

	// New tab ID is based on current tab count + 1
	newID := len(tm.tabs) + 1

	startDir := ""
	if len(tm.tabs) > 0 && tm.activeIndex >= 0 && tm.activeIndex < len(tm.tabs) {
		startDir = tm.tabs[tm.activeIndex].ActiveDir()
	}

	tab, err := NewTab(newID, tm.cols, tm.rows, startDir)
	if err != nil {
		return err
	}

	tm.tabs = append(tm.tabs, tab)
	tm.activeIndex = len(tm.tabs) - 1

	return nil
}

// renumberTabs reassigns sequential IDs to all tabs
func (tm *TabManager) renumberTabs() {
	for i, t := range tm.tabs {
		t.id = i + 1
	}
}

// CloseCurrentTab closes the current tab
func (tm *TabManager) CloseCurrentTab() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.tabs) <= 1 {
		return // Keep at least one tab
	}

	tm.tabs[tm.activeIndex].Close()
	tm.tabs = append(tm.tabs[:tm.activeIndex], tm.tabs[tm.activeIndex+1:]...)

	if tm.activeIndex >= len(tm.tabs) {
		tm.activeIndex = len(tm.tabs) - 1
	}

	// Renumber remaining tabs to keep IDs sequential
	tm.renumberTabs()
}

// NextTab switches to the next tab
func (tm *TabManager) NextTab() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.tabs) > 1 {
		tm.activeIndex = (tm.activeIndex + 1) % len(tm.tabs)
	}
}

// PrevTab switches to the previous tab
func (tm *TabManager) PrevTab() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.tabs) > 1 {
		tm.activeIndex = (tm.activeIndex - 1 + len(tm.tabs)) % len(tm.tabs)
	}
}

// ActiveTab returns the currently active tab
func (tm *TabManager) ActiveTab() *Tab {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if len(tm.tabs) == 0 {
		return nil
	}
	return tm.tabs[tm.activeIndex]
}

// ResizeAll resizes all tabs
func (tm *TabManager) ResizeAll(cols, rows uint16) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.cols = cols
	tm.rows = rows

	for _, tab := range tm.tabs {
		tab.Resize(cols, rows)
	}
}

// CleanupExited removes exited tabs
func (tm *TabManager) CleanupExited() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var activeTabs []*Tab
	for _, tab := range tm.tabs {
		if !tab.HasExited() {
			activeTabs = append(activeTabs, tab)
		} else {
			tab.Close()
		}
	}

	if len(activeTabs) > 0 {
		tm.tabs = activeTabs
		if tm.activeIndex >= len(tm.tabs) {
			tm.activeIndex = len(tm.tabs) - 1
		}
		// Renumber remaining tabs to keep IDs sequential
		tm.renumberTabs()
	}
}

// AllExited returns true if all tabs have exited
func (tm *TabManager) AllExited() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if len(tm.tabs) == 0 {
		return true
	}

	for _, tab := range tm.tabs {
		if !tab.HasExited() {
			return false
		}
	}
	return true
}

// TabCount returns the number of tabs
func (tm *TabManager) TabCount() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.tabs)
}

// ActiveIndex returns the index of the active tab
func (tm *TabManager) ActiveIndex() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.activeIndex
}

// GetTabs returns all tabs (for rendering tab bar)
func (tm *TabManager) GetTabs() []*Tab {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	result := make([]*Tab, len(tm.tabs))
	copy(result, tm.tabs)
	return result
}
