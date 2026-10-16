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
		r.drawRect(x, y, float32(textCells(h.Label))*cw, ch, r.theme.TabActive, proj)
		r.drawHintLabel(x, y, h.Label, h.Typed, proj)
	}
	r.uiFlush()
}

// drawHintLabel draws a label as shaped text, the typed prefix dimmed.
func (r *Renderer) drawHintLabel(x, y float32, label string, typed int, proj [16]float32) {
	clr := r.theme.Background
	clr[3] = 1
	cut := len(label)
	if runes := []rune(label); typed < len(runes) {
		cut = len(string(runes[:max(typed, 0)]))
	}
	if cut > 0 {
		r.drawText(x, y+r.cellHeight, label[:cut], withAlpha(clr, 0.45), proj)
	}
	if cut < len(label) {
		r.drawText(x+float32(textCells(label[:cut]))*r.cellWidth, y+r.cellHeight, label[cut:], clr, proj)
	}
}

// copyStatusOrigin places the copy-mode line at the pane's bottom-left, away
// from the toast in the window's bottom-right corner.
func copyStatusOrigin(ox, oy float32, rows int, ch float32) (float32, float32) {
	return ox, oy + float32(rows-1)*ch
}

type uiBox struct{ x, y, w, h float32 }

func (b uiBox) overlaps(o uiBox) bool {
	return b.x < o.x+o.w && o.x < b.x+b.w && b.y < o.y+o.h && o.y < b.y+b.h
}

// toastOrigin puts the toast in the bottom-right corner, lifted above the
// copy-mode status when a narrow right-hand pane puts the two in one place.
func toastOrigin(width, height, boxW, boxH, margin float32, avoid *uiBox) (float32, float32) {
	x, y := width-boxW-margin, height-boxH-margin
	if avoid != nil && (uiBox{x, y, boxW, boxH}).overlaps(*avoid) {
		y = avoid.y - boxH - margin/2
	}
	return x, y
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
	text := " " + status + " "
	w := float32(textCells(text)) * cw
	bx, by := copyStatusOrigin(ox, oy, pane.Terminal.GetGrid().Rows, ch)
	r.copyStatusBox = &uiBox{bx, by, w, ch}
	r.drawRect(bx, by, w, ch, r.theme.TabActive, proj)
	bg := r.theme.Background
	bg[3] = 1
	r.drawText(bx, by+ch, text, bg, proj)
	r.uiFlush()
}

func (r *Renderer) paletteBackground() [4]float32 {
	bg := r.theme.TabBar
	bg[3] = 1
	return bg
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
	r.drawRect(x-2, y-2, boxW+4, boxH+4, withAlpha(r.theme.TabActive, 0.6), proj)
	r.drawRect(x, y, boxW, boxH, r.paletteBackground(), proj)
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

// tabTags are the short mode labels shown on a tab's chip.
func tabTags(t *tab.Tab) []string {
	var tags []string
	if t.Zoomed() {
		tags = append(tags, "ZOOM")
	}
	if t.Broadcasting() {
		tags = append(tags, "BCAST")
	}
	return tags
}

// DrawBroadcastFrame outlines every pane of a broadcasting tab.
func (r *Renderer) DrawBroadcastFrame(t *tab.Tab, width, height int) {
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)
	c := [4]float32{0.95, 0.45, 0.15, 1}
	for _, pr := range r.paneRects(t, width, height) {
		r.drawRect(pr.x, pr.y, pr.width, 2, c, proj)
		r.drawRect(pr.x, pr.y+pr.height-2, pr.width, 2, c, proj)
		r.drawRect(pr.x, pr.y, 2, pr.height, c, proj)
		r.drawRect(pr.x+pr.width-2, pr.y, 2, pr.height, c, proj)
	}
	r.uiFlush()
}
