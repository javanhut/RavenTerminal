package main

import (
	"fmt"

	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/tab"
)

// findState drives the scrollback find bar. It is bound to one pane for the
// duration of a search: matches are absolute buffer rows, which are only
// meaningful within the grid that produced them.
type findState struct {
	open    bool
	query   string
	pane    *tab.Pane
	matches []grid.Match
	index   int // position within matches; meaningless when matches is empty
}

// findGrid returns the grid the current search is bound to, or nil if the find
// bar is closed or its pane has gone away (closed split, exited tab).
func (a *App) findGrid() *grid.Grid {
	if !a.find.open || a.find.pane == nil || a.find.pane.Terminal == nil {
		return nil
	}
	return a.find.pane.Terminal.GetGrid()
}

// openFind binds the find bar to the active pane and clears any previous
// query. Re-opening while already open restarts the search rather than
// stacking state.
func (a *App) openFind() {
	activeTab := a.tabManager.ActiveTab()
	if activeTab == nil {
		return
	}
	pane := activeTab.GetActivePane()
	if pane == nil {
		return
	}
	a.find = findState{open: true, pane: pane}
	// The find bar owns the highlight while it is up, so drop a mouse
	// selection that would otherwise sit underneath it.
	if g := pane.Terminal.GetGrid(); g != nil {
		g.ClearSelection()
	}
	a.selection.active = false
	a.selection.pane = nil
}

// closeFind dismisses the find bar, leaving the current match selected so it
// can still be copied.
func (a *App) closeFind() {
	a.find = findState{}
}

// runFind re-runs the search after a query edit and jumps to the first match,
// so results track typing the way an incremental find should.
func (a *App) runFind() {
	g := a.findGrid()
	if g == nil {
		return
	}
	if a.find.query == "" {
		a.find.matches = nil
		a.find.index = 0
		g.ClearSelection()
		return
	}
	a.find.matches = g.Search(a.find.query)
	a.find.index = 0
	a.revealFindMatch()
}

// findStep moves to the next (+1) or previous (-1) match, wrapping at both
// ends so repeated presses cycle rather than dead-ending.
func (a *App) findStep(delta int) {
	n := len(a.find.matches)
	if n == 0 {
		return
	}
	a.find.index = ((a.find.index+delta)%n + n) % n
	a.revealFindMatch()
}

// revealFindMatch highlights the current match and scrolls it into view.
func (a *App) revealFindMatch() {
	g := a.findGrid()
	if g == nil || len(a.find.matches) == 0 {
		if g != nil {
			g.ClearSelection()
		}
		return
	}
	m := a.find.matches[a.find.index]
	g.SelectMatch(m)
	g.ScrollToAbsRow(m.StartAbsRow)
}

// findStatus is the counter shown at the right of the find bar.
func (a *App) findStatus() string {
	switch {
	case a.find.query == "":
		return ""
	case len(a.find.matches) == 0:
		return "no matches"
	case len(a.find.matches) >= grid.MaxMatches:
		// Be explicit that the list was truncated rather than implying the
		// cap is the real total.
		return fmt.Sprintf("%d/%d+", a.find.index+1, grid.MaxMatches)
	default:
		return fmt.Sprintf("%d/%d", a.find.index+1, len(a.find.matches))
	}
}
