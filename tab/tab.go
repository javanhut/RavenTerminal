package tab

import (
	"github.com/javanhut/RavenTerminal/parser"
	"github.com/javanhut/RavenTerminal/shell"
	"sync"
)

const MaxTabs = 10

// Tab represents a single terminal tab
type Tab struct {
	Terminal *parser.Terminal
	pty      *shell.PtySession
	id       int
	exited   bool
	exitedMu sync.Mutex
	readerMu sync.Mutex
}

// NewTab creates a new terminal tab
func NewTab(id int, cols, rows uint16) (*Tab, error) {
	pty, err := shell.NewPtySession(cols, rows)
	if err != nil {
		return nil, err
	}

	tab := &Tab{
		Terminal: parser.NewTerminal(int(cols), int(rows)),
		pty:      pty,
		id:       id,
		exited:   false,
	}

	// Start reader goroutine
	go tab.readLoop()

	return tab, nil
}

// readLoop continuously reads from the PTY and processes output
func (t *Tab) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := t.pty.Read(buf)
		if err != nil || n == 0 {
			t.exitedMu.Lock()
			t.exited = true
			t.exitedMu.Unlock()
			return
		}

		t.readerMu.Lock()
		t.Terminal.Process(buf[:n])
		t.readerMu.Unlock()
	}
}

// Write writes data to the PTY
func (t *Tab) Write(data []byte) error {
	_, err := t.pty.Write(data)
	return err
}

// HasExited returns true if the shell has exited
func (t *Tab) HasExited() bool {
	t.exitedMu.Lock()
	defer t.exitedMu.Unlock()
	return t.exited || t.pty.HasExited()
}

// Resize resizes the tab
func (t *Tab) Resize(cols, rows uint16) {
	t.readerMu.Lock()
	defer t.readerMu.Unlock()
	t.Terminal.Resize(int(cols), int(rows))
	t.pty.Resize(cols, rows)
}

// Close closes the tab
func (t *Tab) Close() {
	t.pty.Close()
}

// ID returns the tab ID
func (t *Tab) ID() int {
	return t.id
}

// TabManager manages multiple terminal tabs
type TabManager struct {
	tabs        []*Tab
	activeIndex int
	nextID      int
	cols        uint16
	rows        uint16
	mu          sync.RWMutex
}

// NewTabManager creates a new tab manager
func NewTabManager(cols, rows uint16) (*TabManager, error) {
	tm := &TabManager{
		tabs:        make([]*Tab, 0, MaxTabs),
		activeIndex: 0,
		nextID:      1,
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

	tab, err := NewTab(tm.nextID, tm.cols, tm.rows)
	if err != nil {
		return err
	}

	tm.nextID++
	tm.tabs = append(tm.tabs, tab)
	tm.activeIndex = len(tm.tabs) - 1

	return nil
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
