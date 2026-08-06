//go:build !darwin

package window

import "github.com/go-gl/glfw/v3.3/glfw"

// Everywhere but macOS, GLFW's SetMonitor is the platform path. GLFW does track
// that state — GetMonitor is non-nil exactly while fullscreen — so asking it
// means the windowed geometry is saved only on a genuine windowed->fullscreen
// transition and can never be overwritten with the fullscreen rect.
func toggleFullscreen(w *Window) {
	if isFullscreen(w) {
		w.glfw.SetMonitor(nil, w.savedX, w.savedY, w.savedWidth, w.savedHeight, 0)
		return
	}

	monitor := currentMonitor(w)
	mode := monitor.GetVideoMode()
	if mode == nil {
		return // no usable video mode; stay windowed rather than resize to 0x0
	}

	w.savedX, w.savedY = w.glfw.GetPos()
	w.savedWidth, w.savedHeight = w.glfw.GetSize()
	w.glfw.SetMonitor(monitor, 0, 0, mode.Width, mode.Height, mode.RefreshRate)
}

func isFullscreen(w *Window) bool {
	return w.glfw.GetMonitor() != nil
}

// currentMonitor returns the monitor containing the largest portion of the
// window, falling back to the primary monitor. GLFW only exposes a window's
// monitor while it is fullscreen, so for windowed mode we compute the overlap
// between the window rect and each monitor's work area ourselves.
func currentMonitor(w *Window) *glfw.Monitor {
	wx, wy := w.glfw.GetPos()
	ww, wh := w.glfw.GetSize()
	best := glfw.GetPrimaryMonitor()
	bestArea := 0
	for _, m := range glfw.GetMonitors() {
		mode := m.GetVideoMode()
		if mode == nil {
			continue
		}
		mx, my := m.GetPos()
		overlapW := min(wx+ww, mx+mode.Width) - max(wx, mx)
		overlapH := min(wy+wh, my+mode.Height) - max(wy, my)
		if overlapW > 0 && overlapH > 0 && overlapW*overlapH > bestArea {
			bestArea = overlapW * overlapH
			best = m
		}
	}
	return best
}
