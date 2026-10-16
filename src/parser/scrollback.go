package parser

import "github.com/javanhut/RavenTerminal/src/grid"

// SetMaxScrollback sets the main screen's scrollback cap in rows (see
// grid.SetMaxScrollback), also while the alternate screen is up.
func (t *Terminal) SetMaxScrollback(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	g := t.Grid
	if t.savedMainGrid != nil {
		g = t.savedMainGrid
	}
	g.SetMaxScrollback(n)
}

// resizeMainGrid resizes (reflows) a main-screen grid, remapping the main
// screen's saved cursor (DECSC and the 1049 save) so DECRC / leaving the
// alternate screen returns to the same content.
func (t *Terminal) resizeMainGrid(g *grid.Grid, cols, rows int) {
	p := grid.Point{Col: t.ctl.savedMain.col, Row: t.ctl.savedMain.row}
	g.ResizeTracking(cols, rows, &p)
	t.ctl.savedMain.col, t.ctl.savedMain.row = p.Col, p.Row
}

// syncGraphemeMode applies ?2027 to the screens. The active grid is held
// under the batch lock during Process; the saved main grid is not.
func (t *Terminal) syncGraphemeMode() {
	on := !t.ctl.graphemeOff
	t.Grid.SetGraphemeClusteringLocked(on)
	if t.savedMainGrid != nil {
		t.savedMainGrid.SetGraphemeClustering(on)
	}
}
