package render

import "github.com/javanhut/RavenTerminal/src/tab"

// HintLabel is a hint overlay entry in pane viewport cells.
type HintLabel struct {
	Row, Col       int
	EndRow, EndCol int
	Label          string
	Typed          int // leading label runes already typed
	Hidden         bool
}

// PaletteRow is one command palette line.
type PaletteRow struct{ Label, Detail string }

func (r *Renderer) paneOrigin(t *tab.Tab, pane *tab.Pane, width, height int) (float32, float32, int, bool) {
	if pane == nil || pane.Terminal == nil {
		return 0, 0, 0, false
	}
	for _, pr := range r.paneRects(t, width, height) {
		if pr.pane == pane {
			return pr.x, pr.y, pane.Terminal.GetGrid().Cols, true
		}
	}
	return 0, 0, 0, false
}

// DrawHints tints each hint's text and draws its label over the first cells.
func (r *Renderer) DrawHints(t *tab.Tab, pane *tab.Pane, hints []HintLabel, width, height int) {
	ox, oy, cols, ok := r.paneOrigin(t, pane, width, height)
	if !ok {
		return
	}
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)
	cw, ch := r.cellWidth, r.cellHeight
	tint := withAlpha(r.theme.TabActive, 0.18)
	for _, h := range hints {
		if h.Hidden {
			continue
		}
		for row := h.Row; row <= h.EndRow; row++ {
			c0, c1 := 0, cols-1
			if row == h.Row {
				c0 = h.Col
			}
			if row == h.EndRow {
				c1 = h.EndCol
			}
			r.drawRect(ox+float32(c0)*cw, oy+float32(row+1)*ch-2, float32(c1-c0+1)*cw, 2, r.theme.TabActive, proj)
			r.drawRect(ox+float32(c0)*cw, oy+float32(row)*ch, float32(c1-c0+1)*cw, ch, tint, proj)
		}
	}
	for _, h := range hints {
		if h.Hidden {
			continue
		}
		x, y := ox+float32(h.Col)*cw, oy+float32(h.Row)*ch
		runes := []rune(h.Label)
		r.drawRect(x, y, float32(len(runes))*cw, ch, r.theme.TabActive, proj)
		for i, c := range runes {
			clr := r.theme.Background
			clr[3] = 1
			if i < h.Typed {
				clr = withAlpha(clr, 0.45)
			}
			r.drawChar(x+float32(i)*cw, y+ch, c, clr, proj)
		}
	}
	r.uiFlush()
}

// DrawCopyCursor outlines the copy-mode cursor cell and shows the mode line
// at the bottom of the pane.
func (r *Renderer) DrawCopyCursor(t *tab.Tab, pane *tab.Pane, col, row int, status string, width, height int) {
	ox, oy, _, ok := r.paneOrigin(t, pane, width, height)
	if !ok {
		return
	}
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)
	cw, ch := r.cellWidth, r.cellHeight
	if row >= 0 {
		x, y := ox+float32(col)*cw, oy+float32(row)*ch
		r.drawRect(x, y, cw, ch, withAlpha(r.theme.Cursor, 0.35), proj)
		r.drawRect(x, y, cw, 2, r.theme.Cursor, proj)
		r.drawRect(x, y+ch-2, cw, 2, r.theme.Cursor, proj)
		r.drawRect(x, y, 2, ch, r.theme.Cursor, proj)
		r.drawRect(x+cw-2, y, 2, ch, r.theme.Cursor, proj)
	}
	g := pane.Terminal.GetGrid()
	by := oy + float32(g.Rows-1)*ch
	text := " " + status + " "
	w := float32(textCells(text)) * cw
	bx := ox + float32(g.Cols)*cw - w
	r.drawRect(bx, by, w, ch, r.theme.TabActive, proj)
	bg := r.theme.Background
	bg[3] = 1
	r.drawText(bx, by+ch, text, bg, proj)
	r.uiFlush()
}

// DrawPalette draws the command palette centred near the top of the window.
func (r *Renderer) DrawPalette(query string, rows []PaletteRow, selected, width, height int) {
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)
	cw, ch := r.cellWidth, r.cellHeight
	boxW := min32(float32(width)-cw*4, cw*72)
	maxRows := max(min(len(rows), 12, int(float32(height)/ch)-4), 0)
	pad := ch * 0.4
	boxH := ch*float32(maxRows+1) + pad*3
	x := (float32(width) - boxW) / 2
	y := ch * 2
	bg := r.theme.TabBar
	bg[3] = 0.97
	r.drawRect(x, y, boxW, boxH, bg, proj)
	r.drawRect(x, y, boxW, 2, r.theme.TabActive, proj)
	cols := int((boxW - pad*2) / cw)
	r.drawText(x+pad, y+pad+ch, truncateHeadToCells("> "+query+"█", cols), r.theme.Foreground, proj)
	top := 0
	if selected >= maxRows {
		top = selected - maxRows + 1
	}
	for i := 0; i < maxRows && top+i < len(rows); i++ {
		row := rows[top+i]
		ry := y + pad*2 + ch*float32(i+1)
		if top+i == selected {
			r.drawRect(x+2, ry, boxW-4, ch, withAlpha(r.theme.TabActive, 0.35), proj)
		}
		detail := truncateToCells(row.Detail, cols/3)
		label := truncateToCells(row.Label, cols-textCells(detail)-2)
		r.drawText(x+pad, ry+ch, label, r.theme.Foreground, proj)
		r.drawTextRight(x+boxW-pad, ry+ch, detail, withAlpha(r.theme.Foreground, 0.55), proj)
	}
	if len(rows) == 0 {
		r.drawText(x+pad, y+pad*2+ch*2, "no matches", withAlpha(r.theme.Foreground, 0.55), proj)
	}
	r.uiFlush()
}

// DrawPaneBadges marks a pane with small labels (ZOOM, BROADCAST) and, when
// accent is set, a coloured frame.
func (r *Renderer) DrawPaneBadges(t *tab.Tab, pane *tab.Pane, badges []string, accent bool, width, height int) {
	if len(badges) == 0 {
		return
	}
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)
	cw, ch := r.cellWidth, r.cellHeight
	for _, pr := range r.paneRects(t, width, height) {
		if pane != nil && pr.pane != pane {
			continue
		}
		if accent {
			c := [4]float32{0.95, 0.45, 0.15, 1}
			r.drawRect(pr.x, pr.y, pr.width, 2, c, proj)
			r.drawRect(pr.x, pr.y+pr.height-2, pr.width, 2, c, proj)
			r.drawRect(pr.x, pr.y, 2, pr.height, c, proj)
			r.drawRect(pr.x+pr.width-2, pr.y, 2, pr.height, c, proj)
		}
		if pane == nil && pr.pane != t.GetActivePane() {
			continue
		}
		x := pr.x + pr.width
		for _, b := range badges {
			text := " " + b + " "
			w := float32(textCells(text)) * cw
			x -= w + cw*0.3
			r.drawRect(x, pr.y+2, w, ch, [4]float32{0.95, 0.45, 0.15, 0.92}, proj)
			r.drawText(x, pr.y+2+ch, text, [4]float32{0.05, 0.05, 0.05, 1}, proj)
		}
	}
	r.uiFlush()
}
