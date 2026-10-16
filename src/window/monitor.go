package window

type rect struct{ x, y, w, h int }

// pickMonitor returns the index of the monitor containing win's center, else
// the one it overlaps most, or -1 when it overlaps none.
func pickMonitor(win rect, monitors []rect) int {
	cx, cy := win.x+win.w/2, win.y+win.h/2
	best, bestArea := -1, 0
	for i, m := range monitors {
		if m.w <= 0 || m.h <= 0 {
			continue
		}
		if cx >= m.x && cx < m.x+m.w && cy >= m.y && cy < m.y+m.h {
			return i
		}
		ow := min(win.x+win.w, m.x+m.w) - max(win.x, m.x)
		oh := min(win.y+win.h, m.y+m.h) - max(win.y, m.y)
		if ow > 0 && oh > 0 && ow*oh > bestArea {
			best, bestArea = i, ow*oh
		}
	}
	return best
}
