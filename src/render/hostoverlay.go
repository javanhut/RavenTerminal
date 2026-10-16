package render

import "github.com/javanhut/RavenTerminal/src/tab"

// Palette16 returns the theme's 16 ANSI colours.
func (t Theme) Palette16() [16][4]float32 {
	if t.ANSI != nil {
		return *t.ANSI
	}
	return standard16
}

// SetBackgroundOpacity sets the default background's alpha (window clear and
// default-background cells). Re-apply after SetThemeByName.
func (r *Renderer) SetBackgroundOpacity(alpha float32) {
	if alpha <= 0 || alpha > 1 {
		alpha = 1
	}
	if r.theme.Background[3] != alpha {
		r.theme.Background[3] = alpha
		r.themeGen++
		r.uiDirty = true
	}
}

// CursorCellRect returns the pane's cursor cell in framebuffer pixels.
func (r *Renderer) CursorCellRect(t *tab.Tab, pane *tab.Pane, width, height int) (x, y, w, h float32, ok bool) {
	if pane == nil || pane.Terminal == nil {
		return 0, 0, 0, 0, false
	}
	g := pane.Terminal.GetGrid()
	col, row := g.GetCursor()
	row += g.GetScrollOffset()
	if row < 0 || row >= g.Rows {
		return 0, 0, 0, 0, false
	}
	for _, pr := range r.paneRects(t, width, height) {
		if pr.pane == pane {
			return pr.x + float32(col)*r.cellWidth, pr.y + float32(row)*r.cellHeight, r.cellWidth, r.cellHeight, true
		}
	}
	return 0, 0, 0, 0, false
}
