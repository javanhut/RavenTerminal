package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/javanhut/RavenTerminal/src/assets/fonts"
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/keybindings"
	"github.com/javanhut/RavenTerminal/src/render"
	"github.com/javanhut/RavenTerminal/src/tab"
)

// uxState holds the keyboard overlays: hints, copy mode and the palette.
type uxState struct {
	hints   *hintState
	copy    *copyMode
	palette paletteState
	dirty   bool
}

func (a *App) uxModal() bool {
	return a.ux.hints != nil || a.ux.copy != nil || a.ux.palette.open
}

func (a *App) consumeUXDirty() bool {
	d := a.ux.dirty
	a.ux.dirty = false
	return d
}

func (a *App) closeUX() {
	if a.ux.copy != nil {
		a.ux.copy.leave()
	}
	a.ux.hints, a.ux.copy = nil, nil
	a.ux.palette.open = false
	a.ux.dirty = true
}

// copyText puts text on the clipboard and the primary selection.
func (a *App) copyText(text, toast string) {
	if text == "" {
		return
	}
	glfw.SetClipboardString(text)
	glfw.SetPrimarySelectionString(text)
	a.showToast(toast)
}

func activePane(t *tab.Tab) *tab.Pane {
	if t == nil {
		return nil
	}
	return t.GetActivePane()
}

// runUXAction performs the actions added for navigation and overlays.
func (a *App) runUXAction(t *tab.Tab, act keybindings.KeyAction) {
	pane := activePane(t)
	if pane == nil {
		return
	}
	g := pane.Terminal.GetGrid()
	a.ux.dirty = true
	switch act {
	case keybindings.ActionPrevPrompt, keybindings.ActionNextPrompt:
		top := g.ViewTopAbs()
		target := g.PrevPrompt(top)
		if act == keybindings.ActionNextPrompt {
			target = g.NextPrompt(top)
		}
		switch {
		case target >= 0:
			g.ScrollViewToAbs(target)
		case act == keybindings.ActionNextPrompt:
			g.ResetScrollOffset()
		default:
			a.showToast("No earlier prompt")
		}
	case keybindings.ActionCopyLastOutput:
		s, e, ok := g.LastOutputRange()
		if !ok {
			a.showToast("No command output (needs shell integration)")
			return
		}
		a.copyText(g.TextRange(s, 0, e, g.Cols-1, false), "Copied last output")
	case keybindings.ActionHints:
		a.closeUX()
		ms := collectHints(g)
		if len(ms) == 0 {
			a.showToast("No hints on screen")
			return
		}
		a.ux.hints = newHintState(pane, ms)
	case keybindings.ActionCopyMode:
		a.closeUX()
		a.ux.copy = newCopyMode(g)
		a.ux.copy.pane = pane
		a.ux.copy.sync()
	case keybindings.ActionCommandPalette:
		if a.ux.palette.open {
			a.closeUX()
			return
		}
		a.closeUX()
		var fontList []fonts.FontInfo
		if a.renderer != nil {
			fontList = a.renderer.GetAvailableFonts()
		}
		a.ux.palette.openWith(paletteEntries(config.ThemeOptions(), fontList))
	case keybindings.ActionTogglePaneZoom:
		if t.ToggleZoom() {
			a.showToast("Pane zoomed")
		} else if t.PaneCount() > 1 {
			a.showToast("Pane unzoomed")
		}
	case keybindings.ActionToggleBroadcast:
		if t.ToggleBroadcast() {
			a.showToast("Broadcasting input to all panes")
		} else {
			a.showToast("Broadcast off")
		}
	case keybindings.ActionScrollbackPager:
		a.openScrollbackPager(pane)
	}
}

func (a *App) openScrollbackPager(pane *tab.Pane) {
	path, err := writeScrollbackFile(scrollbackANSI(pane.Terminal.GetGrid()))
	if err != nil {
		a.showToast("Can't write scrollback: " + err.Error())
		return
	}
	if err := a.tabManager.NewTabWithCommand(pagerCommand(path), pane.CurrentDir()); err != nil {
		os.Remove(path)
		a.showToast("Can't open pager: " + err.Error())
	}
}

// openPath hands an existing local file or folder to raven-open.
func openPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	name, args, err := desktopOpener(exec.LookPath)
	if err != nil {
		return err
	}
	return exec.Command(name, append(args, path)...).Start()
}

func (a *App) finishHint(m hintMatch, open bool) {
	pane := a.ux.hints.pane
	a.ux.hints = nil
	a.ux.dirty = true
	if !open {
		a.copyText(m.Text, "Copied "+m.Text)
		return
	}
	target := hintOpenTarget(m, pane.CurrentDir())
	switch {
	case m.Kind == hintURL:
		if err := openURL(target); err != nil {
			log.Printf("hint open %q: %v", target, err)
			a.showToast("Can't open link")
		}
		return
	case target != "":
		if err := openPath(target); err == nil {
			return
		}
	}
	pane.Write([]byte(m.Text))
}

// uxKey routes keys to an open overlay. Printable keys are left for uxChar.
func (a *App) uxKey(t *tab.Tab, key glfw.Key, mods glfw.ModifierKey) bool {
	if !a.uxModal() {
		return false
	}
	if mods&(glfw.ModControl|glfw.ModSuper) != 0 {
		a.suppressChar = true
	}
	act := keybindings.TranslateKey(key, mods, false).Action
	opener := keybindings.ActionNone
	switch {
	case a.ux.palette.open:
		opener = keybindings.ActionCommandPalette
	case a.ux.hints != nil:
		opener = keybindings.ActionHints
	case a.ux.copy != nil:
		opener = keybindings.ActionCopyMode
	}
	if act == opener {
		a.closeUX()
		return true
	}
	leader := mods&glfw.ModSuper != 0 || mods&(glfw.ModControl|glfw.ModShift) == glfw.ModControl|glfw.ModShift
	if leader && act != keybindings.ActionNone && act != keybindings.ActionInput {
		a.closeUX()
		return false
	}
	a.ux.dirty = true
	switch {
	case a.ux.palette.open:
		p := &a.ux.palette
		switch {
		case key == glfw.KeyEscape:
			a.closeUX()
		case key == glfw.KeyEnter || key == glfw.KeyKPEnter:
			e, ok := p.choose()
			a.closeUX()
			if ok {
				a.runPaletteEntry(t, e)
			}
		case key == glfw.KeyUp || (key == glfw.KeyP && mods&glfw.ModControl != 0):
			p.move(-1)
		case key == glfw.KeyDown || key == glfw.KeyTab || (key == glfw.KeyN && mods&glfw.ModControl != 0):
			p.move(1)
		case key == glfw.KeyPageUp:
			p.move(-10)
		case key == glfw.KeyPageDown:
			p.move(10)
		case key == glfw.KeyBackspace:
			p.backspace()
		}
	case a.ux.hints != nil:
		switch key {
		case glfw.KeyEscape:
			a.closeUX()
		case glfw.KeyBackspace:
			a.ux.hints.backspace()
		}
	case a.ux.copy != nil:
		a.handleCopyResult(a.ux.copy.key(key, mods))
	}
	return true
}

// uxChar routes typed text to an open overlay.
func (a *App) uxChar(char rune) bool {
	if !a.uxModal() {
		return false
	}
	a.ux.dirty = true
	switch {
	case a.ux.palette.open:
		a.ux.palette.typeRune(char)
	case a.ux.hints != nil:
		if m, open, done := a.ux.hints.input(char); done {
			a.finishHint(m, open)
		}
	case a.ux.copy != nil:
		a.handleCopyResult(a.ux.copy.rune(char))
	}
	return true
}

func (a *App) handleCopyResult(r copyResult) {
	if !r.exit {
		return
	}
	a.ux.copy = nil
	a.copyText(r.yank, "Copied to clipboard")
}

func (a *App) runPaletteEntry(t *tab.Tab, e paletteEntry) {
	switch {
	case e.Theme != "" || e.Apply != nil:
		if a.settingsMenu == nil || a.settingsMenu.Config == nil {
			return
		}
		cfg := a.settingsMenu.Config
		if e.Apply != nil {
			e.Apply(cfg)
		} else {
			cfg.Theme = e.Theme
		}
		if err := cfg.Save(); err != nil {
			log.Printf("palette: save setting: %v", err)
		}
		if err := a.applyConfig(a.applied, cfg); err != nil {
			a.showToast(err.Error())
		}
	case e.Font != "":
		if err := a.renderer.ChangeFont(e.Font); err != nil {
			a.showToast(fmt.Sprintf("Can't load font: %v", err))
			return
		}
		a.fitGrid()
	default:
		a.runAction(t, keybindings.KeyResult{Action: e.Action}, glfw.Press)
	}
}

// terminalCursorAllowed is false while an overlay draws its own cursor.
func (a *App) terminalCursorAllowed() bool { return a.ux.copy == nil }

// drawUXOverlays draws the open overlay and the broadcast frame.
func (a *App) drawUXOverlays(t *tab.Tab, width, height int) {
	if t == nil {
		return
	}
	pane := t.GetActivePane()
	if (a.ux.hints != nil && a.ux.hints.pane != pane) || (a.ux.copy != nil && a.ux.copy.pane != pane) {
		a.closeUX()
	}
	if t.Broadcasting() {
		a.renderer.DrawBroadcastFrame(t, width, height)
	}
	switch {
	case a.ux.hints != nil:
		hs := a.ux.hints
		labels := make([]render.HintLabel, len(hs.matches))
		for i, m := range hs.matches {
			l := hs.labels[i]
			labels[i] = render.HintLabel{Row: m.Row, Col: m.Col, EndRow: m.EndRow, EndCol: m.EndCol, Label: l,
				Typed: len(hs.typed), Hidden: len(hs.typed) > 0 && l[:min(len(hs.typed), len(l))] != hs.typed}
		}
		a.renderer.DrawHints(t, pane, labels, width, height)
	case a.ux.copy != nil:
		c := a.ux.copy
		a.renderer.DrawCopyCursor(t, pane, c.col, c.viewRow(), c.status(), width, height)
	case a.ux.palette.open:
		p := &a.ux.palette
		rows := make([]render.PaletteRow, len(p.shown))
		for i, e := range p.shown {
			rows[i] = render.PaletteRow{Label: e.Label, Detail: e.Detail}
		}
		a.renderer.DrawPalette(p.query, rows, p.sel, width, height)
	}
}

// leaderClick reports a click with the leader held (Super, or Ctrl+Shift).
func leaderClick(mods glfw.ModifierKey) bool {
	return mods&glfw.ModSuper != 0 || mods&(glfw.ModControl|glfw.ModShift) == glfw.ModControl|glfw.ModShift
}

// selectCommandOutput selects and copies the output of the command under row.
func (a *App) selectCommandOutput(pane *tab.Pane, row int) bool {
	g := pane.Terminal.GetGrid()
	s, e, ok := g.CommandOutputAt(g.AbsRowForViewRow(row))
	if !ok {
		return false
	}
	if a.selection.pane != nil && a.selection.pane != pane {
		a.selection.pane.Terminal.GetGrid().ClearSelection()
	}
	g.SetSelectionAbs(s, 0, e, g.Cols-1, false)
	a.selection.active, a.selection.pane = false, pane
	a.copyText(g.SelectedText(), "Copied command output")
	return true
}
