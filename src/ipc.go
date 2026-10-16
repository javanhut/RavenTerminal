package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/javanhut/RavenTerminal/src/keybindings"
	"github.com/javanhut/RavenTerminal/src/tab"
	"github.com/javanhut/RavenTerminal/src/window"
)

// IPC: one JSON object per line each way over a unix socket.
// Request {"cmd": "...", "args": [...]}; response {"ok": bool, "error": "...", "data": ...}.

const ipcSocketEnv = "RAVEN_TERMINAL_SOCKET"

type ipcRequest struct {
	Cmd  string   `json:"cmd"`
	Args []string `json:"args,omitempty"`
}

type ipcResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}

type ipcCommand struct {
	Name   string
	ID     int
	Dir    string
	Extent string
	Text   string
	Cwd    string
	Argv   []string
	Path   string
	Steps  []keyStep
	Mouse  ipcMouse
}

// ipcMouse moves the pointer to X,Y (window coordinates) and, when Press or
// Release is set, presses or releases Button there.
type ipcMouse struct {
	X, Y           float64
	Button         glfw.MouseButton
	Press, Release bool
}

type ipcPane struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Cwd    string `json:"cwd"`
	Active bool   `json:"active"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
	Exited bool   `json:"exited"`
}

type ipcTab struct {
	Index     int       `json:"index"`
	Title     string    `json:"title"`
	Active    bool      `json:"active"`
	Zoomed    bool      `json:"zoomed"`
	Broadcast bool      `json:"broadcast"`
	Panes     []ipcPane `json:"panes"`
}

type ipcWindow struct {
	ID      int      `json:"id"`
	Focused bool     `json:"focused"`
	Tabs    []ipcTab `json:"tabs"`
}

func ipcUsage() string {
	return `usage: raven-terminal @ [--to SOCKET] <command> [args]
  ls                                   windows, tabs and panes as JSON
  new-tab [--cwd DIR] [CMD ARGS...]    open a tab (shell when no command)
  new-window                           open a window
  split [--id N] h|v                   split a pane: h stacked, v side by side
  focus-pane left|right|up|down|N      focus a neighbour or the pane with id N
  send-text [--id N] TEXT...           type text (\n \r \t \e \xHH escapes)
  get-text [--id N] [--extent screen|all|last-output|selection]
  set-title [--id N] TITLE...          fix the tab title ("" via set-title -)
  close-pane [--id N]
  get-user-vars [--id N]               OSC 1337 SetUserVar values
  screenshot [--id N] FILE.png         next frame as PNG, cropped to pane N
  action NAME                          run a keybinding action (hints, copy mode, palette, zoom ...)
  send-key CHORD                       press and release a key (ctrl+shift+space, escape, j)
  send-mouse X Y [BUTTON[:press|:release]]
                                       move the pointer; click left|middle|right, or press/release it
  state                                overlay modes, focus, size, pointer and IME preedit as JSON
`
}

// unescapeIPC expands \n \r \t \e \\ \xHH and \0.
func unescapeIPC(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'e':
			b.WriteByte(0x1b)
		case '0':
			b.WriteByte(0)
		case 'x':
			if i+2 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
					b.WriteByte(byte(v))
					i += 2
					continue
				}
			}
			b.WriteString(`\x`)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func parseIPC(req ipcRequest) (ipcCommand, error) {
	c := ipcCommand{Name: req.Cmd}
	flags := map[string]bool{}
	switch req.Cmd {
	case "ls", "new-window", "state", "send-key", "action", "send-mouse":
	case "new-tab":
		flags["--cwd"] = true
	case "split", "send-text", "set-title", "close-pane", "get-user-vars", "screenshot":
		flags["--id"] = true
	case "get-text":
		flags["--id"], flags["--extent"] = true, true
		c.Extent = "screen"
	case "focus-pane":
	default:
		return c, fmt.Errorf("unknown command %q", req.Cmd)
	}
	args := req.Args
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") || len(pos) > 0 {
			pos = append(pos, args[i:]...)
			break
		}
		if !flags[a] {
			return c, fmt.Errorf("%s: unknown option %s", req.Cmd, a)
		}
		if i+1 >= len(args) {
			return c, fmt.Errorf("%s: %s needs a value", req.Cmd, a)
		}
		i++
		switch a {
		case "--id":
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return c, fmt.Errorf("%s: bad pane id %q", req.Cmd, args[i])
			}
			c.ID = n
		case "--extent":
			switch args[i] {
			case "screen", "all", "last-output", "selection":
				c.Extent = args[i]
			default:
				return c, fmt.Errorf("get-text: unknown extent %q", args[i])
			}
		case "--cwd":
			c.Cwd = args[i]
		}
	}
	switch req.Cmd {
	case "ls", "new-window", "close-pane", "get-user-vars", "get-text", "state":
		if len(pos) > 0 {
			return c, fmt.Errorf("%s takes no arguments", req.Cmd)
		}
	case "new-tab":
		c.Argv = pos
	case "split":
		if len(pos) != 1 {
			return c, errors.New("split needs h or v")
		}
		switch pos[0] {
		case "h", "horizontal":
			c.Dir = "h"
		case "v", "vertical":
			c.Dir = "v"
		default:
			return c, fmt.Errorf("split: %q is not h or v", pos[0])
		}
	case "focus-pane":
		if len(pos) != 1 {
			return c, errors.New("focus-pane needs a direction or pane id")
		}
		switch pos[0] {
		case "left", "right", "up", "down":
			c.Dir = pos[0]
		default:
			n, err := strconv.Atoi(pos[0])
			if err != nil || n <= 0 {
				return c, fmt.Errorf("focus-pane: %q is not a direction or id", pos[0])
			}
			c.ID = n
		}
	case "send-text":
		if len(pos) == 0 {
			return c, errors.New("send-text needs text")
		}
		c.Text = unescapeIPC(strings.Join(pos, " "))
	case "set-title":
		if len(pos) == 0 {
			return c, errors.New("set-title needs a title")
		}
		if c.Text = strings.Join(pos, " "); c.Text == "-" {
			c.Text = ""
		}
	case "screenshot":
		if len(pos) != 1 {
			return c, errors.New("screenshot needs one file name")
		}
		if !filepath.IsAbs(pos[0]) || !strings.EqualFold(filepath.Ext(pos[0]), ".png") {
			return c, fmt.Errorf("screenshot: %q is not an absolute .png path", pos[0])
		}
		c.Path = pos[0]
	case "send-key":
		if len(pos) != 1 {
			return c, errors.New("send-key needs one chord")
		}
		ch, err := parseChord(pos[0])
		if err != nil {
			return c, err
		}
		c.Steps = ch.steps()
	case "send-mouse":
		m, err := parseMouse(pos)
		if err != nil {
			return c, err
		}
		c.Mouse = m
	case "action":
		info, err := lookupAction(strings.Join(pos, " "))
		if err != nil {
			return c, err
		}
		ch, err := actionChord(info)
		if err != nil {
			return c, err
		}
		c.Steps = ch.steps()
	}
	return c, nil
}

var mouseButtons = map[string]glfw.MouseButton{
	"left": glfw.MouseButtonLeft, "middle": glfw.MouseButtonMiddle, "right": glfw.MouseButtonRight,
}

func parseMouse(pos []string) (ipcMouse, error) {
	var m ipcMouse
	if len(pos) < 2 || len(pos) > 3 {
		return m, errors.New("send-mouse needs X Y and an optional button")
	}
	for i, v := range []*float64{&m.X, &m.Y} {
		n, err := strconv.ParseFloat(pos[i], 64)
		if err != nil || n < 0 || math.IsInf(n, 0) || math.IsNaN(n) {
			return m, fmt.Errorf("send-mouse: bad coordinate %q", pos[i])
		}
		*v = n
	}
	if len(pos) == 2 {
		return m, nil
	}
	name, phase, _ := strings.Cut(pos[2], ":")
	b, ok := mouseButtons[name]
	if !ok {
		return m, fmt.Errorf("send-mouse: unknown button %q", name)
	}
	m.Button = b
	switch phase {
	case "":
		m.Press, m.Release = true, true
	case "press":
		m.Press = true
	case "release":
		m.Release = true
	default:
		return m, fmt.Errorf("send-mouse: %q is not press or release", phase)
	}
	return m, nil
}

// keyChord is a key with its modifiers, as written on the command line.
type keyChord struct {
	Key  glfw.Key
	Mods glfw.ModifierKey
}

// keyStep is one injected event: a key press or release, or (Char != 0) the
// char event GLFW sends after a printable key.
type keyStep struct {
	Key    glfw.Key
	Action glfw.Action
	Mods   glfw.ModifierKey
	Char   rune
}

var chordMods = []struct {
	mod glfw.ModifierKey
	key glfw.Key
}{
	{glfw.ModControl, glfw.KeyLeftControl}, {glfw.ModShift, glfw.KeyLeftShift},
	{glfw.ModAlt, glfw.KeyLeftAlt}, {glfw.ModSuper, glfw.KeyLeftSuper},
}

var modNames = map[string]glfw.ModifierKey{
	"ctrl": glfw.ModControl, "control": glfw.ModControl, "shift": glfw.ModShift,
	"alt": glfw.ModAlt, "opt": glfw.ModAlt, "option": glfw.ModAlt,
	"super": glfw.ModSuper, "cmd": glfw.ModSuper, "command": glfw.ModSuper, "logo": glfw.ModSuper,
}

var keyNames = func() map[string]glfw.Key {
	m := map[string]glfw.Key{
		"space": glfw.KeySpace, "enter": glfw.KeyEnter, "return": glfw.KeyEnter, "tab": glfw.KeyTab,
		"backspace": glfw.KeyBackspace, "escape": glfw.KeyEscape, "esc": glfw.KeyEscape,
		"insert": glfw.KeyInsert, "delete": glfw.KeyDelete, "del": glfw.KeyDelete,
		"home": glfw.KeyHome, "end": glfw.KeyEnd, "pageup": glfw.KeyPageUp, "pgup": glfw.KeyPageUp,
		"pagedown": glfw.KeyPageDown, "pgdn": glfw.KeyPageDown,
		"up": glfw.KeyUp, "down": glfw.KeyDown, "left": glfw.KeyLeft, "right": glfw.KeyRight,
		"kpenter": glfw.KeyKPEnter, "menu": glfw.KeyMenu,
		"minus": glfw.KeyMinus, "equal": glfw.KeyEqual, "comma": glfw.KeyComma, "period": glfw.KeyPeriod,
		"slash": glfw.KeySlash, "backslash": glfw.KeyBackslash, "semicolon": glfw.KeySemicolon,
		"apostrophe": glfw.KeyApostrophe, "grave": glfw.KeyGraveAccent,
		"leftbracket": glfw.KeyLeftBracket, "rightbracket": glfw.KeyRightBracket,
		"ctrl": glfw.KeyLeftControl, "control": glfw.KeyLeftControl, "shift": glfw.KeyLeftShift,
		"alt": glfw.KeyLeftAlt, "super": glfw.KeyLeftSuper,
	}
	for _, r := range "abcdefghijklmnopqrstuvwxyz0123456789-=[]\\;',./`" {
		m[string(r)] = glfw.Key(unicode.ToUpper(r))
	}
	for i := 1; i <= 25; i++ {
		m["f"+strconv.Itoa(i)] = glfw.KeyF1 + glfw.Key(i-1)
	}
	return m
}()

var shiftedASCII = map[rune]rune{
	'1': '!', '2': '@', '3': '#', '4': '$', '5': '%', '6': '^', '7': '&', '8': '*', '9': '(', '0': ')',
	'-': '_', '=': '+', '[': '{', ']': '}', '\\': '|', ';': ':', '\'': '"', ',': '<', '.': '>',
	'/': '?', '`': '~',
}

// parseChord reads "ctrl+shift+space", "escape" or "Leader+H". Leader is the
// app leader: Ctrl+Shift on Linux, where Leader+Shift adds Alt.
func parseChord(s string) (keyChord, error) {
	parts := strings.Split(s, "+")
	var ch keyChord
	leader := false
	for i, p := range parts {
		name := strings.ToLower(strings.TrimSpace(p))
		if len(name) > 1 {
			name = strings.NewReplacer("_", "", "-", "").Replace(name)
		}
		if name == "" {
			return ch, fmt.Errorf("bad key chord %q", s)
		}
		if i < len(parts)-1 {
			if name == "leader" {
				leader = true
				continue
			}
			m, ok := modNames[name]
			if !ok {
				return ch, fmt.Errorf("%q: unknown modifier %q", s, p)
			}
			ch.Mods |= m
			continue
		}
		k, ok := keyNames[name]
		if !ok {
			return ch, fmt.Errorf("%q: unknown key %q", s, p)
		}
		ch.Key = k
	}
	if leader {
		switch {
		case runtime.GOOS == "darwin":
			ch.Mods |= glfw.ModSuper
		case ch.Mods&glfw.ModShift != 0:
			ch.Mods |= glfw.ModControl | glfw.ModAlt
		default:
			ch.Mods |= glfw.ModControl | glfw.ModShift
		}
	}
	return ch, nil
}

// steps is the event sequence a keyboard produces for the chord: modifiers
// down, the key with its text, then everything up in reverse. Like Wayland,
// a modifier's own press does not yet carry its bit and its release does.
func (ch keyChord) steps() []keyStep {
	var out []keyStep
	var mods glfw.ModifierKey
	for _, m := range chordMods {
		if ch.Mods&m.mod != 0 {
			out = append(out, keyStep{Key: m.key, Action: glfw.Press, Mods: mods})
			mods |= m.mod
		}
	}
	out = append(out, keyStep{Key: ch.Key, Action: glfw.Press, Mods: mods})
	if r := chordRune(ch.Key); r != 0 && mods&glfw.ModControl == 0 {
		if mods&glfw.ModShift != 0 {
			if s, ok := shiftedASCII[r]; ok {
				r = s
			}
			r = unicode.ToUpper(r)
		}
		out = append(out, keyStep{Char: r, Mods: mods})
	}
	out = append(out, keyStep{Key: ch.Key, Action: glfw.Release, Mods: mods})
	for _, m := range slices.Backward(chordMods) {
		if ch.Mods&m.mod != 0 {
			out = append(out, keyStep{Key: m.key, Action: glfw.Release, Mods: mods})
			mods &^= m.mod
		}
	}
	return out
}

// chordRune is the US-layout text of a printable key, 0 for the rest.
func chordRune(k glfw.Key) rune {
	switch {
	case k >= glfw.KeyA && k <= glfw.KeyZ:
		return rune('a' + (k - glfw.KeyA))
	case k == glfw.KeySpace:
		return ' '
	case k < glfw.KeyWorld1:
		if _, ok := shiftedASCII[rune(k)]; ok {
			return rune(k)
		}
	}
	return 0
}

// ipcActions is what `action` can run: the palette's actions plus the palette.
func ipcActions() []keybindings.ActionInfo {
	return append(keybindings.Actions(),
		keybindings.ActionInfo{Action: keybindings.ActionCommandPalette, Name: "Command palette", Chord: "Leader+P"})
}

func actionKey(s string) string {
	s = strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

// lookupAction finds an action by its name or a unique part of it.
func lookupAction(name string) (keybindings.ActionInfo, error) {
	q := actionKey(name)
	if q == "" {
		return keybindings.ActionInfo{}, errors.New("action needs a name")
	}
	var found []keybindings.ActionInfo
	for _, info := range ipcActions() {
		n := actionKey(info.Name)
		if n == q {
			return info, nil
		}
		if strings.Contains(n, q) {
			found = append(found, info)
		}
	}
	switch len(found) {
	case 0:
		return keybindings.ActionInfo{}, fmt.Errorf("unknown action %q", name)
	case 1:
		return found[0], nil
	}
	names := make([]string, len(found))
	for i, f := range found {
		names[i] = f.Name
	}
	return keybindings.ActionInfo{}, fmt.Errorf("action %q is ambiguous: %s", name, strings.Join(names, "; "))
}

// actionChord is the first chord listed for an action.
func actionChord(info keybindings.ActionInfo) (keyChord, error) {
	first, _, _ := strings.Cut(info.Chord, " / ")
	return parseChord(first)
}

// Pane ids are process-wide and stable for a pane's life. Main thread only.
var (
	ipcIDs    = map[*tab.Pane]int{}
	ipcNextID = 1
)

type ipcLoc struct {
	app *App
	tab int
	t   *tab.Tab
}

// ipcWalk visits every live pane, assigning ids and forgetting dead ones.
func ipcWalk(apps []*App, fn func(id int, p *tab.Pane, loc ipcLoc)) {
	live := map[*tab.Pane]bool{}
	for _, a := range apps {
		for ti, t := range a.tabManager.GetTabs() {
			for _, p := range t.GetPanes() {
				live[p] = true
				id, ok := ipcIDs[p]
				if !ok {
					id = ipcNextID
					ipcNextID++
					ipcIDs[p] = id
				}
				if fn != nil {
					fn(id, p, ipcLoc{a, ti, t})
				}
			}
		}
	}
	for p := range ipcIDs {
		if !live[p] {
			delete(ipcIDs, p)
		}
	}
}

func ipcPaneByID(apps []*App, id int) *tab.Pane {
	p, _ := ipcFind(apps, id)
	return p
}

func ipcFind(apps []*App, id int) (*tab.Pane, ipcLoc) {
	var found *tab.Pane
	var at ipcLoc
	ipcWalk(apps, func(pid int, p *tab.Pane, loc ipcLoc) {
		if pid == id {
			found, at = p, loc
		}
	})
	return found, at
}

func ipcID(apps []*App, p *tab.Pane) int {
	ipcWalk(apps, nil)
	return ipcIDs[p]
}

func focusedApp(apps []*App) *App {
	for _, a := range apps {
		if a.windowFocused {
			return a
		}
	}
	return apps[0]
}

// ipcTarget resolves --id, or the focused window's active pane.
func ipcTarget(apps []*App, c ipcCommand) (*tab.Pane, ipcLoc, error) {
	if c.ID != 0 {
		p, loc := ipcFind(apps, c.ID)
		if p == nil {
			return nil, loc, fmt.Errorf("no pane with id %d", c.ID)
		}
		return p, loc, nil
	}
	a := focusedApp(apps)
	t := a.tabManager.ActiveTab()
	if t == nil || t.GetActivePane() == nil {
		return nil, ipcLoc{}, errors.New("no active pane")
	}
	return t.GetActivePane(), ipcLoc{a, a.tabManager.ActiveIndex(), t}, nil
}

func ipcErr(err error) ipcResponse { return ipcResponse{Error: err.Error()} }

// dispatchIPC runs a parsed command against the windows. Main thread only.
func dispatchIPC(apps []*App, c ipcCommand) ipcResponse {
	if len(apps) == 0 {
		return ipcResponse{Error: "no windows"}
	}
	if c.Name == "ls" {
		var wins []ipcWindow
		ipcWalk(apps, nil)
		for wi, a := range apps {
			w := ipcWindow{ID: wi + 1, Focused: a.windowFocused}
			for ti, t := range a.tabManager.GetTabs() {
				it := ipcTab{Index: ti + 1, Title: t.Title(), Active: ti == a.tabManager.ActiveIndex(),
					Zoomed: t.Zoomed(), Broadcast: t.Broadcasting()}
				active := t.GetActivePane()
				for _, p := range t.GetPanes() {
					g := p.Terminal.GetGrid()
					it.Panes = append(it.Panes, ipcPane{ID: ipcIDs[p], Title: p.Terminal.Title(), Cwd: p.CurrentDir(),
						Active: p == active, Cols: g.Cols, Rows: g.Rows, Exited: p.HasExited()})
				}
				w.Tabs = append(w.Tabs, it)
			}
			wins = append(wins, w)
		}
		return ipcResponse{OK: true, Data: wins}
	}
	if c.Name == "new-tab" {
		a := focusedApp(apps)
		n := a.tabManager.TabCount()
		if err := a.tabManager.NewTabWithCommand(c.Argv, c.Cwd); err != nil {
			return ipcErr(err)
		}
		if a.tabManager.TabCount() == n {
			return ipcResponse{Error: "tab limit reached"}
		}
		return ipcResponse{OK: true, Data: ipcID(apps, a.tabManager.ActiveTab().GetActivePane())}
	}
	switch c.Name {
	case "send-key", "action":
		a := focusedApp(apps)
		ipcInjectKeys(a, c.Steps)
		a.eventRedraw = true
		return ipcResponse{OK: true}
	case "send-mouse":
		a := focusedApp(apps)
		ipcInjectMouse(a, c.Mouse)
		a.eventRedraw = true
		return ipcResponse{OK: true}
	case "state":
		return ipcResponse{OK: true, Data: ipcStateOf(apps)}
	}
	if c.Name == "focus-pane" && c.Dir != "" {
		t := focusedApp(apps).tabManager.ActiveTab()
		if t == nil || !t.FocusDirection(c.Dir) {
			return ipcResponse{Error: "no pane " + c.Dir}
		}
		return ipcResponse{OK: true, Data: ipcID(apps, t.GetActivePane())}
	}

	p, loc, err := ipcTarget(apps, c)
	if err != nil {
		return ipcErr(err)
	}
	switch c.Name {
	case "focus-pane":
		loc.app.tabManager.SelectTab(loc.tab)
		loc.t.SetActivePane(p)
		if loc.app.win != nil {
			loc.app.win.GLFW().Focus()
		}
		return ipcResponse{OK: true, Data: c.ID}
	case "split":
		loc.t.SetActivePane(p)
		if c.Dir == "h" {
			err = loc.t.SplitHorizontal()
		} else {
			err = loc.t.SplitVertical()
		}
		if err != nil {
			return ipcErr(err)
		}
		np := loc.t.GetActivePane()
		if np == p {
			return ipcResponse{Error: "pane limit reached"}
		}
		return ipcResponse{OK: true, Data: ipcID(apps, np)}
	case "send-text":
		if err := p.Write([]byte(c.Text)); err != nil {
			return ipcErr(err)
		}
		p.Terminal.GetGrid().ResetScrollOffset()
		return ipcResponse{OK: true}
	case "get-text":
		g := p.Terminal.GetGrid()
		switch c.Extent {
		case "all":
			first, last := g.AbsBounds()
			return ipcResponse{OK: true, Data: g.TextRange(first, 0, last, g.Cols-1, false)}
		case "last-output":
			s, e, ok := g.LastOutputRange()
			if !ok {
				return ipcResponse{Error: "no command output (shell integration marks missing?)"}
			}
			return ipcResponse{OK: true, Data: g.TextRange(s, 0, e, g.Cols-1, false)}
		case "selection":
			return ipcResponse{OK: true, Data: g.SelectedText()}
		}
		top := g.ViewTopAbs()
		return ipcResponse{OK: true, Data: g.TextRange(top, 0, top+g.Rows-1, g.Cols-1, false)}
	case "set-title":
		loc.t.SetTitle(c.Text)
		return ipcResponse{OK: true}
	case "close-pane":
		if loc.t.PaneCount() > 1 {
			loc.t.SetActivePane(p)
			loc.t.ClosePane()
		} else {
			loc.app.tabManager.SelectTab(loc.tab)
			loc.app.tabManager.CloseCurrentTab()
		}
		return ipcResponse{OK: true}
	case "get-user-vars":
		vars := p.Terminal.UserVars()
		if vars == nil {
			vars = map[string]string{}
		}
		return ipcResponse{OK: true, Data: vars}
	}
	return ipcResponse{Error: "unsupported command " + c.Name}
}

type ipcModes struct {
	Hints     bool `json:"hints"`
	Copy      bool `json:"copy"`
	Palette   bool `json:"palette"`
	Zoom      bool `json:"zoom"`
	Broadcast bool `json:"broadcast"`
	Resize    bool `json:"resize"`
	Help      bool `json:"help"`
	Find      bool `json:"find"`
	Menu      bool `json:"menu"`
}

type ipcPreedit struct {
	Text  string `json:"text"`
	Caret int    `json:"caret_bytes"`
}

type ipcHint struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

type ipcState struct {
	Window       int        `json:"window"`
	Focused      bool       `json:"focused"`
	FocusedPane  int        `json:"focused_pane"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	FBWidth      int        `json:"fb_width"`
	FBHeight     int        `json:"fb_height"`
	Modes        ipcModes   `json:"modes"`
	Pointer      string     `json:"pointer"`
	Preedit      ipcPreedit `json:"preedit"`
	Toast        string     `json:"toast,omitempty"`
	Hints        []ipcHint  `json:"hints,omitempty"`
	CopyStatus   string     `json:"copy_status,omitempty"`
	PaletteQuery string     `json:"palette_query,omitempty"`
	PaletteRows  []string   `json:"palette_rows,omitempty"`
}

// ipcWinState is per-window harness state kept outside App. Main thread only.
type ipcWinState struct {
	shots   []ipcShot
	preedit ipcPreedit
}

type ipcShot struct {
	path  string
	pane  *tab.Pane
	reply chan ipcResponse
}

var ipcWins = map[*App]*ipcWinState{}

func ipcWinFor(a *App) *ipcWinState {
	st := ipcWins[a]
	if st == nil {
		st = &ipcWinState{}
		ipcWins[a] = st
	}
	return st
}

func ipcNotePreedit(a *App, text string, caretBytes int) {
	ipcWinFor(a).preedit = ipcPreedit{Text: text, Caret: caretBytes}
}

// ipcInjectKeys feeds events through the window's own key callbacks.
var ipcInjectKeys = func(a *App, steps []keyStep) {
	w := a.win.GLFW()
	for _, st := range steps {
		if st.Char != 0 {
			a.onChar(w, st.Char, st.Mods)
		} else {
			a.onKey(w, st.Key, 0, st.Action, st.Mods)
		}
	}
}

// ipcInjectMouse feeds pointer events through the window's own callbacks.
var ipcInjectMouse = func(a *App, m ipcMouse) {
	w := a.win.GLFW()
	a.onCursorPos(w, m.X, m.Y)
	if m.Press {
		a.onMouseButton(w, m.Button, glfw.Press, 0)
	}
	if m.Release {
		a.onMouseButton(w, m.Button, glfw.Release, 0)
	}
}

// ipcStateOf describes the focused window.
func ipcStateOf(apps []*App) ipcState {
	a := focusedApp(apps)
	st := ipcState{Focused: a.windowFocused, Pointer: a.pointerCSS, Preedit: ipcWinFor(a).preedit}
	st.Window = slices.Index(apps, a) + 1
	if st.Pointer == "" {
		st.Pointer = "default"
	}
	if a.win != nil {
		st.Width, st.Height = a.win.GetSize()
		st.FBWidth, st.FBHeight = a.win.GetFramebufferSize()
	}
	t := a.tabManager.ActiveTab()
	if p := activePane(t); p != nil {
		st.FocusedPane = ipcID(apps, p)
	}
	st.Modes = ipcModes{
		Hints: a.ux.hints != nil, Copy: a.ux.copy != nil, Palette: a.ux.palette.open,
		Resize: a.resizeMode, Help: a.showHelp, Find: a.find.open,
		Menu: a.settingsMenu != nil && a.settingsMenu.IsOpen(),
	}
	if t != nil {
		st.Modes.Zoom, st.Modes.Broadcast = t.Zoomed(), t.Broadcasting()
	}
	if a.toast != nil && time.Now().Before(a.toast.expiresAt) {
		st.Toast = a.toast.message
	}
	if hs := a.ux.hints; hs != nil {
		for i, m := range hs.matches {
			st.Hints = append(st.Hints, ipcHint{Label: hs.labels[i], Text: m.Text})
		}
	}
	if a.ux.copy != nil {
		st.CopyStatus = a.ux.copy.status()
	}
	if p := &a.ux.palette; p.open {
		st.PaletteQuery = p.query
		for _, e := range p.shown {
			st.PaletteRows = append(st.PaletteRows, e.Label)
		}
	}
	return st
}

// queueScreenshot asks for the window's next frame; the reply waits for it.
func queueScreenshot(apps []*App, call ipcCall) error {
	if len(apps) == 0 {
		return errors.New("no windows")
	}
	a := focusedApp(apps)
	var pane *tab.Pane
	if call.cmd.ID != 0 {
		p, loc := ipcFind(apps, call.cmd.ID)
		if p == nil {
			return fmt.Errorf("no pane with id %d", call.cmd.ID)
		}
		if loc.app.tabManager.ActiveTab() != loc.t {
			return fmt.Errorf("pane %d is not in the visible tab", call.cmd.ID)
		}
		a, pane = loc.app, p
	}
	st := ipcWinFor(a)
	st.shots = append(st.shots, ipcShot{path: call.cmd.Path, pane: pane, reply: call.reply})
	a.eventRedraw = true
	return nil
}

// captureFrames reads back the frame just drawn, before it is swapped, for
// any queued screenshots. Render thread, with this window's context current.
func (a *App) captureFrames(width, height int) {
	if st := ipcWins[a]; st == nil || len(st.shots) == 0 || width <= 0 || height <= 0 {
		return
	}
	var prev int32
	gl.GetIntegerv(gl.READ_FRAMEBUFFER_BINDING, &prev)
	gl.BindFramebuffer(gl.READ_FRAMEBUFFER, 0)
	pix := make([]byte, width*height*4)
	gl.ReadPixels(0, 0, int32(width), int32(height), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pix))
	gl.BindFramebuffer(gl.READ_FRAMEBUFFER, uint32(prev))
	scale := float32(1)
	if winW, _ := a.win.GetSize(); winW > 0 {
		scale = float32(width) / float32(winW)
	}
	t := a.tabManager.ActiveTab()
	a.deliverShots(pix, width, height, func(p *tab.Pane) (image.Rectangle, bool) {
		x, y, w, h, ok := a.renderer.PaneRectFor(t, p, width, height)
		px := func(v float32) int { return int(math.Round(float64(v * scale))) }
		return image.Rect(px(x), px(y), px(x+w), px(y+h)), ok
	})
}

// deliverShots crops and writes the queued screenshots from one frame of
// bottom-up RGBA pixels; files are encoded off the main thread.
func (a *App) deliverShots(pix []byte, w, h int, rectFor func(*tab.Pane) (image.Rectangle, bool)) {
	st := ipcWinFor(a)
	shots := st.shots
	st.shots = nil
	for _, s := range shots {
		crop := image.Rect(0, 0, w, h)
		if s.pane != nil {
			r, ok := rectFor(s.pane)
			if !ok {
				s.reply <- ipcResponse{Error: "pane is not on screen"}
				continue
			}
			crop = r
		}
		img := frameImage(pix, w, h, crop)
		goSafe("ipc screenshot", func() {
			if err := writePNG(s.path, img); err != nil {
				s.reply <- ipcErr(err)
				return
			}
			s.reply <- ipcResponse{OK: true, Data: s.path}
		})
	}
}

// frameImage turns glReadPixels output (rows bottom-up) into an opaque image
// of crop, given top-down and clamped to the frame.
func frameImage(pix []byte, w, h int, crop image.Rectangle) *image.NRGBA {
	crop = crop.Intersect(image.Rect(0, 0, w, h))
	img := image.NewNRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	for y := crop.Min.Y; y < crop.Max.Y; y++ {
		src := pix[((h-1-y)*w+crop.Min.X)*4 : ((h-1-y)*w+crop.Max.X)*4]
		dst := img.Pix[(y-crop.Min.Y)*img.Stride:]
		copy(dst, src)
		for x := 3; x < len(src); x += 4 {
			dst[x] = 255
		}
	}
	return img
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

type ipcCall struct {
	cmd   ipcCommand
	reply chan ipcResponse
}

var ipcCalls = make(chan ipcCall, 64)

// ipcMainHandler hands a request to the main loop and waits for its answer.
func ipcMainHandler(req ipcRequest) ipcResponse {
	c, err := parseIPC(req)
	if err != nil {
		return ipcErr(err)
	}
	call := ipcCall{cmd: c, reply: make(chan ipcResponse, 1)}
	select {
	case ipcCalls <- call:
	case <-time.After(5 * time.Second):
		return ipcResponse{Error: "terminal busy"}
	}
	window.PostEmptyEvent()
	select {
	case r := <-call.reply:
		return r
	case <-time.After(10 * time.Second):
		return ipcResponse{Error: "timed out"}
	}
}

// serveIPC answers queued requests on the main loop; new-window adds a window
// and screenshot waits for the next frame.
func serveIPC(apps []*App) []*App {
	for a, st := range ipcWins {
		if !slices.Contains(apps, a) {
			for _, s := range st.shots {
				s.reply <- ipcResponse{Error: "window closed"}
			}
			delete(ipcWins, a)
		}
	}
	served := false
	defer func() {
		// The loop is about to sleep: wake it so the effects get drawn.
		if served {
			window.PostEmptyEvent()
		}
	}()
	for {
		select {
		case call := <-ipcCalls:
			served = true
			if call.cmd.Name == "screenshot" {
				if err := queueScreenshot(apps, call); err != nil {
					call.reply <- ipcErr(err)
				}
				continue
			}
			if call.cmd.Name == "new-window" {
				a, err := newAppWith(func(cols, rows uint16) (*tab.TabManager, error) {
					return tab.NewTabManager(cols, rows)
				})
				if err != nil {
					call.reply <- ipcErr(err)
					continue
				}
				a.registerCallbacks()
				a.win.GLFW().Focus()
				apps = append(apps, a)
				call.reply <- ipcResponse{OK: true, Data: len(apps)}
				continue
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						call.reply <- ipcResponse{Error: fmt.Sprint("internal error: ", r)}
					}
				}()
				call.reply <- dispatchIPC(apps, call.cmd)
			}()
		default:
			return apps
		}
	}
}

func ipcSocketDir() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = filepath.Join(os.TempDir(), "raven-terminal-"+strconv.Itoa(os.Getuid()))
	}
	return filepath.Join(base, "raven-terminal")
}

// secureDir requires path to be a real directory owned by this user that no
// one else can write to; with fix our own directory is tightened to 0700.
func secureDir(path string, fix bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s: not a directory owned by this user", path)
	}
	if fix && fi.Mode().Perm()&0o077 != 0 {
		return os.Chmod(path, 0o700)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s: writable by other users", path)
	}
	return nil
}

// checkIPCDir verifies the socket directory and its parent before anything
// in them is trusted.
func checkIPCDir(dir string, fix bool) error {
	if err := secureDir(filepath.Dir(dir), false); err != nil {
		return err
	}
	return secureDir(dir, fix)
}

// startIPCServerWith listens on <runtime dir>/raven-terminal/<pid>.sock and
// exports its path to child processes.
func startIPCServerWith(handle func(ipcRequest) ipcResponse) (string, func(), error) {
	dir := ipcSocketDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	if err := checkIPCDir(dir, true); err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, strconv.Itoa(os.Getpid())+".sock")
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return "", nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return "", nil, err
	}
	os.Setenv(ipcSocketEnv, path)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveIPCConn(conn, handle)
		}
	}()
	var once sync.Once
	return path, func() {
		once.Do(func() {
			ln.Close()
			os.Remove(path)
		})
	}, nil
}

func serveIPCConn(conn net.Conn, handle func(ipcRequest) ipcResponse) {
	defer conn.Close()
	defer recoverLog("ipc connection")
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	enc := json.NewEncoder(conn)
	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		if !sc.Scan() {
			return
		}
		var req ipcRequest
		resp := ipcResponse{Error: "bad request"}
		if err := json.Unmarshal(sc.Bytes(), &req); err == nil {
			resp = handle(req)
		}
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

// findIPCSocket picks the newest socket that accepts a connection.
func findIPCSocket() string {
	dir := ipcSocketDir()
	if checkIPCDir(dir, false) != nil {
		return ""
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.sock"))
	paths = slices.DeleteFunc(paths, func(p string) bool {
		fi, err := os.Lstat(p)
		if err != nil {
			return true
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		return !ok || int(st.Uid) != os.Getuid()
	})
	sort.Slice(paths, func(i, j int) bool {
		a, _ := os.Stat(paths[i])
		b, _ := os.Stat(paths[j])
		return a != nil && b != nil && a.ModTime().After(b.ModTime())
	})
	for _, p := range paths {
		if c, err := net.DialTimeout("unix", p, time.Second); err == nil {
			c.Close()
			return p
		}
	}
	return ""
}

// ipcClientMain implements `raven-terminal @ <cmd> [args]`.
func ipcClientMain(args []string, stdout, stderr io.Writer) int {
	path := os.Getenv(ipcSocketEnv)
	if len(args) >= 2 && args[0] == "--to" {
		path, args = args[1], args[2:]
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stderr, ipcUsage())
		return 2
	}
	if path == "" {
		if path = findIPCSocket(); path == "" {
			fmt.Fprintln(stderr, "raven-terminal @: no running terminal found ($"+ipcSocketEnv+" unset)")
			return 1
		}
	}
	if args[0] == "screenshot" {
		args = absScreenshotArgs(args)
	}
	conn, err := net.DialTimeout("unix", path, 3*time.Second)
	if err != nil {
		fmt.Fprintf(stderr, "raven-terminal @: %v\n", err)
		return 1
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := json.NewEncoder(conn).Encode(ipcRequest{Cmd: args[0], Args: args[1:]}); err != nil {
		fmt.Fprintf(stderr, "raven-terminal @: %v\n", err)
		return 1
	}
	var resp ipcResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		fmt.Fprintf(stderr, "raven-terminal @: %v\n", err)
		return 1
	}
	if !resp.OK {
		fmt.Fprintf(stderr, "raven-terminal @ %s: %s\n", args[0], resp.Error)
		return 1
	}
	switch d := resp.Data.(type) {
	case nil:
	case string:
		fmt.Fprint(stdout, d)
		if !strings.HasSuffix(d, "\n") {
			fmt.Fprintln(stdout)
		}
	default:
		out, _ := json.MarshalIndent(d, "", "  ")
		fmt.Fprintf(stdout, "%s\n", out)
	}
	return 0
}

// absScreenshotArgs resolves the file name against the client's directory,
// since the terminal's own working directory is unrelated.
func absScreenshotArgs(args []string) []string {
	out := slices.Clone(args)
	for i := 1; i < len(out); i++ {
		if out[i] == "--id" {
			i++
			continue
		}
		if abs, err := filepath.Abs(out[i]); err == nil && !strings.HasPrefix(out[i], "--") {
			out[i] = abs
		}
	}
	return out
}

// startIPCServer starts the app's socket; failures only disable remote control.
func startIPCServer() func() {
	_, stop, err := startIPCServerWith(ipcMainHandler)
	if err != nil {
		log.Printf("remote control disabled: %v", err)
		return func() {}
	}
	return stop
}

func itoa(n int) string { return strconv.Itoa(n) }
