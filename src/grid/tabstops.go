package grid

// Tab-stop support (HTS/TBC) and DECALN screen fill.

// ensureTabStops lazily initializes default tab stops (every 8th column) for the
// current width. Caller must hold g.mu.
func (g *Grid) ensureTabStops() {
	if len(g.tabStops) == g.Cols {
		return
	}
	ts := make([]bool, g.Cols)
	for c := 8; c < g.Cols; c += 8 {
		ts[c] = true
	}
	g.tabStops = ts
}

// SetTabStop sets a tab stop at the current cursor column (HTS, ESC H).
func (g *Grid) SetTabStop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ensureTabStops()
	if g.CursorCol >= 0 && g.CursorCol < len(g.tabStops) {
		g.tabStops[g.CursorCol] = true
	}
}

// ClearTabStop clears tab stops (TBC, CSI Ps g): mode 0 = at cursor, 3 = all.
func (g *Grid) ClearTabStop(mode int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ensureTabStops()
	switch mode {
	case 0:
		if g.CursorCol >= 0 && g.CursorCol < len(g.tabStops) {
			g.tabStops[g.CursorCol] = false
		}
	case 3:
		for i := range g.tabStops {
			g.tabStops[i] = false
		}
	}
}

// FillScreen fills the entire active area with a rune using the default style
// (DECALN, ESC # 8 — used for screen-alignment tests).
func (g *Grid) FillScreen(r rune) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cell := Cell{Char: r, Fg: DefaultFg(), Bg: DefaultBg(), Width: CellWidthNormal}
	for row := 0; row < g.Rows; row++ {
		for col := 0; col < g.Cols; col++ {
			g.putCell(g.index(col, row), cell)
		}
	}
}
