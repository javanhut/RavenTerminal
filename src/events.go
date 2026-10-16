package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/javanhut/RavenTerminal/src/commands"
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/render"
	"github.com/javanhut/RavenTerminal/src/tab"
)

// eventSink is what terminal events act on; the App implements it.
type eventSink interface {
	notify(n notification)
	requestAttention()
	visualBell(p *tab.Pane)
	changeFont(name string)
}

type eventPolicy struct {
	bell          string
	notifications bool
	notifyAfter   time.Duration
	focused       bool
}

// paneEvents are one pane's drained events; visible means the pane is in the
// window's active tab.
type paneEvents struct {
	pane    *tab.Pane
	visible bool
	events  []parser.Event
}

// routeEvents acts on drained terminal events and reports whether a redraw
// is needed. Title, progress and pointer shape are state the frame reads
// back from the terminal, so their events only request a redraw.
func routeEvents(batch []paneEvents, p eventPolicy, s eventSink) (redraw bool) {
	font, fontSet := "", false
	for _, pe := range batch {
		for _, ev := range pe.events {
			switch ev.Kind {
			case parser.EventBell:
				switch p.bell {
				case "none":
				case "attention":
					if !p.focused {
						s.requestAttention()
					}
				default:
					s.visualBell(pe.pane)
					if !p.focused {
						s.requestAttention()
					}
				}
				redraw = true
			case parser.EventNotify:
				if !p.notifications {
					continue
				}
				n := notification{Summary: ev.Title, Body: ev.Body, Urgency: ev.Urgency, Tag: ev.ID}
				if n.Summary == "" {
					n.Summary, n.Body = n.Body, ""
				}
				if n.Summary != "" {
					s.notify(n)
				}
			case parser.EventCommandFinished:
				ms, err := strconv.ParseInt(ev.Value, 10, 64)
				took := time.Duration(ms) * time.Millisecond
				if !p.notifications || p.notifyAfter <= 0 || err != nil || took < p.notifyAfter || (p.focused && pe.visible) {
					continue
				}
				n := notification{Summary: "Command finished", Urgency: 1}
				if ev.ExitCode != 0 {
					n.Summary = fmt.Sprintf("Command failed (exit %d)", ev.ExitCode)
				}
				dur := took.Round(time.Second).String()
				if cmd := strings.TrimSpace(ev.Body); cmd != "" {
					n.Body = cmd + " (" + dur + ")"
				} else {
					n.Body = "took " + dur
				}
				s.notify(n)
			case parser.EventRavenCommand:
				if ev.Name == "change-font" { // applied once, after the batch
					font, fontSet = ev.Value, true
					redraw = true
				}
			case parser.EventTitle, parser.EventProgress, parser.EventPointerShape:
				redraw = true
			}
		}
	}
	if fontSet {
		s.changeFont(font)
	}
	return redraw
}

// windowTitle is the active pane's OSC title, else the window's base title
// (--title or the default).
func windowTitle(oscTitle, base string) string {
	if oscTitle != "" {
		return oscTitle
	}
	return base
}

// cursorShapeFor maps an OSC 22 (CSS) pointer name to a GLFW cursor. def
// means the normal pointer; hidden means no pointer at all.
func cursorShapeFor(css string) (shape glfw.StandardCursor, hidden, def bool) {
	switch css {
	case "none":
		return 0, true, false
	case "text", "vertical-text":
		return glfw.IBeamCursor, false, false
	case "pointer", "alias", "copy", "grab", "grabbing":
		return glfw.PointingHandCursor, false, false
	case "crosshair", "cell":
		return glfw.CrosshairCursor, false, false
	case "ew-resize", "e-resize", "w-resize", "col-resize":
		return glfw.ResizeEWCursor, false, false
	case "ns-resize", "n-resize", "s-resize", "row-resize":
		return glfw.ResizeNSCursor, false, false
	case "nwse-resize", "nw-resize", "se-resize":
		return glfw.ResizeNWSECursor, false, false
	case "nesw-resize", "ne-resize", "sw-resize":
		return glfw.ResizeNESWCursor, false, false
	case "move", "all-scroll":
		return glfw.ResizeAllCursor, false, false
	case "not-allowed", "no-drop":
		return glfw.NotAllowedCursor, false, false
	}
	return 0, false, true
}

// frameDecision gates drawing while an application holds synchronized
// output (?2026) on a visible pane: pane-driven redraws are deferred until
// the release (or the parser's watchdog), UI-driven ones still draw.
func frameDecision(trig render.RedrawTriggers, syncHeld, pending bool) (draw, stillPending bool) {
	want := render.ShouldRedraw(trig) || pending
	if !syncHeld {
		return want, false
	}
	ui := trig
	ui.PaneContentDirty, ui.GridSwapped, ui.CursorPhaseChanged, ui.SyncActiveOrEnded = false, false, false, false
	if render.ShouldRedraw(ui) {
		return true, false
	}
	return false, want
}

func to8(v float32) uint8 {
	return uint8(min(max(v, 0), 1)*255 + 0.5)
}

func gridColor(c [4]float32) grid.Color {
	return grid.RGBColor(to8(c[0]), to8(c[1]), to8(c[2]))
}

// themeDefaults converts a theme to the terminal's colour defaults and
// colour-scheme (?2031) preference.
func themeDefaults(th render.Theme) (fg, bg, cursor grid.Color, pal [16]grid.Color, dark bool) {
	for i, c := range th.Palette16() {
		pal[i] = gridColor(c)
	}
	return gridColor(th.Foreground), gridColor(th.Background), gridColor(th.Cursor), pal, !th.Light
}

// eventPolicyFor reads the live config.
func eventPolicyFor(cfg *config.Config, focused bool) eventPolicy {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return eventPolicy{
		bell:          cfg.BellMode(),
		notifications: cfg.Notifications,
		notifyAfter:   time.Duration(cfg.NotifyCommandAfter) * time.Second,
		focused:       focused,
	}
}

// paneState is what the app last pushed into a pane's terminal.
type paneState struct {
	serial uint64
	seen   uint64
}

// prunePanes forgets panes that were not seen this tick (closed), so their
// terminals, grids and images can be collected.
func (a *App) prunePanes() {
	for p, st := range a.panes {
		if st.seen != a.pumpTick {
			delete(a.panes, p)
			if p.Closed() {
				delete(ipcIDs, p)
			}
		}
	}
	for p := range a.lastGrids {
		if _, ok := a.panes[p]; !ok {
			delete(a.lastGrids, p)
		}
	}
}

// pumpEvents drains every pane's events, applies theme/scrollback settings
// to panes that have not seen the current ones, and routes the events.
func (a *App) pumpEvents() {
	a.pumpTick++
	active := a.tabManager.ActiveTab()
	var batch []paneEvents
	for _, t := range a.tabManager.GetTabs() {
		for _, p := range t.GetPanes() {
			if p == nil || p.Terminal == nil {
				continue
			}
			st := a.panes[p]
			if st.serial != a.themeSerial {
				a.seedPane(p)
				st.serial = a.themeSerial
			}
			st.seen = a.pumpTick
			a.panes[p] = st
			if evs := p.Terminal.DrainEvents(); len(evs) > 0 {
				batch = append(batch, paneEvents{pane: p, visible: t == active, events: evs})
			}
		}
	}
	a.prunePanes()
	if len(batch) > 0 && routeEvents(batch, eventPolicyFor(a.settingsMenu.Config, a.windowFocused), a) {
		a.eventRedraw = true
	}
	a.syncWindowTitle(active)
	a.syncPointer(active)
}

func (a *App) seedPane(p *tab.Pane) {
	fg, bg, cur, pal, dark := themeDefaults(a.theme)
	p.Terminal.SetDefaultColors(fg, bg, cur, pal)
	p.Terminal.SetDarkMode(dark)
	if a.applied != nil {
		p.Terminal.SetMaxScrollback(a.applied.Scrollback())
		p.Terminal.SetDefaultCursorStyle(cursorStyleFor(a.applied.Appearance.CursorStyle))
	}
}

// cursorStyleFor maps the cursor_style setting to the terminal's style.
func cursorStyleFor(s string) parser.CursorStyle {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "underline":
		return parser.CursorStyleUnderline
	case "bar", "beam":
		return parser.CursorStyleBar
	}
	return parser.CursorStyleBlock
}

func activeTerminal(t *tab.Tab) *parser.Terminal {
	if t == nil {
		return nil
	}
	if p := t.GetActivePane(); p != nil {
		return p.Terminal
	}
	return t.Terminal
}

func (a *App) syncWindowTitle(active *tab.Tab) {
	osc := ""
	if term := activeTerminal(active); term != nil {
		osc = term.Title()
	}
	if title := windowTitle(osc, a.baseTitle); title != a.shownTitle {
		a.shownTitle = title
		a.win.GLFW().SetTitle(title)
	}
}

// pointerFor applies an application's OSC 22 pointer only over its panes;
// the tab bar and panels keep the normal pointer.
func pointerFor(css string, overPane bool) string {
	if !overPane {
		return ""
	}
	return css
}

func (a *App) syncPointer(active *tab.Tab) {
	css := ""
	if term := activeTerminal(active); term != nil && a.haveCursorPos && !a.settingsMenu.IsOpen() && !a.showHelp {
		w, h := a.win.GetFramebufferSize()
		_, _, _, over := a.renderer.HitTestPane(active, a.lastCursorX, a.lastCursorY, w, h)
		css = pointerFor(term.PointerShape(), over)
	}
	if css == a.pointerCSS {
		return
	}
	a.pointerCSS = css
	w := a.win.GLFW()
	shape, hidden, def := cursorShapeFor(css)
	if hidden {
		w.SetInputMode(glfw.CursorMode, glfw.CursorHidden)
		return
	}
	w.SetInputMode(glfw.CursorMode, glfw.CursorNormal)
	if def {
		w.SetCursor(nil)
		return
	}
	if a.cursors == nil {
		a.cursors = map[glfw.StandardCursor]*glfw.Cursor{}
	}
	c, ok := a.cursors[shape]
	if !ok {
		c = glfw.CreateStandardCursor(shape)
		a.cursors[shape] = c
	}
	w.SetCursor(c)
}

func (a *App) notify(n notification) {
	desktopNotifier.send(n, a)
}

func (a *App) requestAttention() {
	a.win.GLFW().RequestAttention()
}

func (a *App) visualBell(p *tab.Pane) {
	a.renderer.FlashPane(p)
}

func (a *App) changeFont(name string) {
	if strings.ToLower(strings.TrimSpace(name)) == a.renderer.CurrentFont() {
		return
	}
	msg, _ := commands.ChangeFont(a.renderer, name)
	a.fitGrid()
	if line, _, _ := strings.Cut(strings.TrimSpace(msg), "\n"); line != "" {
		a.showToast(line)
	}
}
