package main

import (
	"github.com/javanhut/RavenTerminal/src/tab"
)

// pendingDetach holds tabs torn off a tab strip, waiting for the main loop to
// give each one its own window. The tear-off is requested from a GLFW mouse
// callback, which runs while GLFW is dispatching events; creating a window
// there is asking for trouble, so the request is queued and serviced by
// runApps instead. Main-thread only (GLFW callbacks and the loop are the same
// thread), so no locking.
var pendingDetach []*tab.Tab

// takePendingDetach returns and clears the queued tear-off requests.
func takePendingDetach() []*tab.Tab {
	if len(pendingDetach) == 0 {
		return nil
	}
	out := pendingDetach
	pendingDetach = nil
	return out
}

// tearOffThreshold is how far past the tab bar's right edge (in logical
// pixels) a dragged chip must travel before it detaches. Wide enough that
// reordering — which naturally wanders a little sideways — never tears a tab
// off by accident.
const tearOffThreshold = 60.0

// detachTabAt tears the tab at index out of this window and queues it for a
// window of its own. Reports whether the tab was actually detached; the last
// tab in a window never is, since that would just be moving the window.
func (a *App) detachTabAt(index int) bool {
	t := a.tabManager.DetachTab(index)
	if t == nil {
		return false
	}
	pendingDetach = append(pendingDetach, t)
	return true
}

// newDetachedApp opens a new window hosting a single torn-off tab.
func newDetachedApp(t *tab.Tab) (*App, error) {
	return newAppWith(func(cols, rows uint16) (*tab.TabManager, error) {
		return tab.NewTabManagerWith(cols, rows, t), nil
	})
}
