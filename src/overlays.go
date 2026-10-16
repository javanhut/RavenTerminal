package main

import (
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/javanhut/RavenTerminal/src/render"
	"github.com/javanhut/RavenTerminal/src/tab"
)

// animationDue reports whether a renderer effect (bell flash, indeterminate
// progress, blinking text) is running, and how long the loop may sleep.
func (a *App) animationDue(now time.Time) (time.Duration, bool) {
	return a.renderer.NextAnimationFrame(now)
}

func (a *App) applyEffects() {
	e := render.Effects{BlinkInterval: a.blinkInterval}
	if cfg := a.settingsMenu.Config; cfg != nil {
		e.ReduceMotion = cfg.Appearance.ReducedMotion
		e.PromptMarks = cfg.Appearance.MarksGutter
	}
	a.renderer.SetEffects(e)
}

// updateIMERect tells the input method where the cursor cell is, in window
// content coordinates, whenever it moves.
func (a *App) updateIMERect(active *tab.Tab, fbW, fbH int) {
	if active == nil {
		return
	}
	x, y, w, h, ok := a.renderer.CursorCellRect(active, active.GetActivePane(), fbW, fbH)
	if !ok {
		return
	}
	winW, _ := a.win.GetSize()
	scale := float32(1)
	if winW > 0 && fbW > 0 {
		scale = float32(fbW) / float32(winW)
	}
	r := [4]int{int(x / scale), int(y / scale), max(int(w/scale), 1), max(int(h/scale), 1)}
	if r != a.imeRect {
		a.imeRect = r
		a.win.GLFW().SetIMECursorRect(r[0], r[1], r[2], r[3])
	}
}

func (a *App) onPreedit(_ *glfw.Window, text string, caretBytes int) {
	a.renderer.SetPreedit(text, caretBytes)
	ipcNotePreedit(a, text, caretBytes)
}

// onDrop pastes dropped files' shell-quoted paths into the focused pane.
func (a *App) onDrop(_ *glfw.Window, names []string) {
	t := a.tabManager.ActiveTab()
	if t == nil {
		return
	}
	p := t.GetActivePane()
	if p == nil || p.Terminal == nil {
		return
	}
	dropPaths(p.Terminal, p, names)
}
