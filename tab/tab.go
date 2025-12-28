package tab

import (
	"github.com/javanhut/RavenTerminal/parser"
	"github.com/javanhut/RavenTerminal/shell"
	"sync"
)

const MaxTabs = 10
const MaxPanes = 4

// SplitDirection indicates how a pane is split
type SplitDirection int

const (
	SplitNone SplitDirection = iota
	SplitVertical
	SplitHorizontal
)

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
func NewPane(id int, cols, rows uint16) (*Pane, error) {
	pty, err := shell.NewPtySession(cols, rows)
	if err != nil {
		return nil, err
	}

	pane := &Pane{
		Terminal: parser.NewTerminal(int(cols), int(rows)),
		pty:      pty,
		id:       id,
		exited:   false,
	}

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

// ID returns the pane ID
func (p *Pane) ID() int {
	return p.id
}

// Tab represents a single terminal tab with optional splits
type Tab struct {
	Terminal   *parser.Terminal // For backward compatibility - points to active pane's terminal
	pty        *shell.PtySession
	id         int
	exited     bool
	exitedMu   sync.Mutex
	readerMu   sync.Mutex
	panes      []*Pane
	activePane int
	splitDir   SplitDirection
	nextPaneID int
	cols       uint16
	rows       uint16
}

// NewTab creates a new terminal tab
func NewTab(id int, cols, rows uint16) (*Tab, error) {
	// Create the first pane
	pane, err := NewPane(1, cols, rows)
	if err != nil {
		return nil, err
	}

	tab := &Tab{
		Terminal:   pane.Terminal,
		pty:        pane.pty,
		id:         id,
		exited:     false,
		panes:      []*Pane{pane},
		activePane: 0,
		splitDir:   SplitNone,
		nextPaneID: 2,
		cols:       cols,
		rows:       rows,
	}

	return tab, nil
}

// SplitVertical splits the current pane vertically (side by side)
func (t *Tab) SplitVertical() error {
	if len(t.panes) >= MaxPanes {
		return nil // Silently ignore if at max
	}

	// Calculate size for new pane (half width)
	newCols := t.cols / 2
	newRows := t.rows

	pane, err := NewPane(t.nextPaneID, newCols, newRows)
	if err != nil {
		return err
	}

	t.nextPaneID++
	t.panes = append(t.panes, pane)
	t.activePane = len(t.panes) - 1
	t.splitDir = SplitVertical
	t.updateTerminalRef()

	// Resize existing panes
	t.resizePanes()

	return nil
}

// SplitHorizontal splits the current pane horizontally (stacked)
func (t *Tab) SplitHorizontal() error {
	if len(t.panes) >= MaxPanes {
		return nil // Silently ignore if at max
	}

	// Calculate size for new pane (half height)
	newCols := t.cols
	newRows := t.rows / 2

	pane, err := NewPane(t.nextPaneID, newCols, newRows)
	if err != nil {
		return err
	}

	t.nextPaneID++
	t.panes = append(t.panes, pane)
	t.activePane = len(t.panes) - 1
	t.splitDir = SplitHorizontal
	t.updateTerminalRef()

	// Resize existing panes
	t.resizePanes()

	return nil
}

// ClosePane closes the current pane
func (t *Tab) ClosePane() {
	if len(t.panes) <= 1 {
		return // Keep at least one pane
	}

	t.panes[t.activePane].Close()
	t.panes = append(t.panes[:t.activePane], t.panes[t.activePane+1:]...)

	if t.activePane >= len(t.panes) {
		t.activePane = len(t.panes) - 1
	}

	if len(t.panes) == 1 {
		t.splitDir = SplitNone
	}

	t.updateTerminalRef()
	t.resizePanes()
}

// NextPane switches to the next pane
func (t *Tab) NextPane() {
	if len(t.panes) > 1 {
		t.activePane = (t.activePane + 1) % len(t.panes)
		t.updateTerminalRef()
	}
}

// PrevPane switches to the previous pane
func (t *Tab) PrevPane() {
	if len(t.panes) > 1 {
		t.activePane = (t.activePane - 1 + len(t.panes)) % len(t.panes)
		t.updateTerminalRef()
	}
}

// updateTerminalRef updates the Terminal reference to point to active pane
func (t *Tab) updateTerminalRef() {
	if len(t.panes) > 0 && t.activePane < len(t.panes) {
		t.Terminal = t.panes[t.activePane].Terminal
		t.pty = t.panes[t.activePane].pty
	}
}

// resizePanes resizes all panes based on split direction
func (t *Tab) resizePanes() {
	if len(t.panes) == 0 {
		return
	}

	if len(t.panes) == 1 {
		t.panes[0].Resize(t.cols, t.rows)
		return
	}

	switch t.splitDir {
	case SplitVertical:
		paneWidth := t.cols / uint16(len(t.panes))
		for _, pane := range t.panes {
			pane.Resize(paneWidth, t.rows)
		}
	case SplitHorizontal:
		paneHeight := t.rows / uint16(len(t.panes))
		for _, pane := range t.panes {
			pane.Resize(t.cols, paneHeight)
		}
	}
}

// Write writes data to the PTY (writes to active pane)
func (t *Tab) Write(data []byte) error {
	if len(t.panes) > 0 && t.activePane < len(t.panes) {
		return t.panes[t.activePane].Write(data)
	}
	return nil
}

// HasExited returns true if all panes have exited
func (t *Tab) HasExited() bool {
	for _, pane := range t.panes {
		if !pane.HasExited() {
			return false
		}
	}
	return true
}

// Resize resizes the tab and all panes
func (t *Tab) Resize(cols, rows uint16) {
	t.cols = cols
	t.rows = rows
	t.resizePanes()
}

// Close closes the tab and all panes
func (t *Tab) Close() {
	for _, pane := range t.panes {
		pane.Close()
	}
}

// ID returns the tab ID
func (t *Tab) ID() int {
	return t.id
}

// GetPanes returns all panes in this tab
func (t *Tab) GetPanes() []*Pane {
	return t.panes
}

// ActivePaneIndex returns the index of the active pane
func (t *Tab) ActivePaneIndex() int {
	return t.activePane
}

// GetSplitDirection returns the split direction
func (t *Tab) GetSplitDirection() SplitDirection {
	return t.splitDir
}

// PaneCount returns the number of panes
func (t *Tab) PaneCount() int {
	return len(t.panes)
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

	tab, err := NewTab(newID, tm.cols, tm.rows)
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
