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

// currentMonitor returns the monitor the window is on. GLFW tracks it per
// platform (on Wayland, the outputs the surface entered, since window
// positions are unknown there); otherwise the monitor containing the window's
// center, else the one it overlaps most, else the primary.
func currentMonitor(w *Window) *glfw.Monitor {
	if m := w.glfw.CurrentMonitor(); m != nil {
		return m
	}
	monitors := glfw.GetMonitors()
	rects := make([]rect, len(monitors))
	for i, m := range monitors {
		if mode := m.GetVideoMode(); mode != nil {
			mx, my := m.GetPos()
			rects[i] = rect{mx, my, mode.Width, mode.Height}
		}
	}
	wx, wy := w.glfw.GetPos()
	ww, wh := w.glfw.GetSize()
	if i := pickMonitor(rect{wx, wy, ww, wh}, rects); i >= 0 {
		return monitors[i]
	}
	return glfw.GetPrimaryMonitor()
}
