package parser

import (
	"encoding/base64"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/javanhut/RavenTerminal/src/grid"
)

const (
	maxTitleLen      = 2048
	maxNotifyPayload = 64 << 10
	maxPendingNotify = 8
	maxUserVars      = 256
	maxPointerStack  = 16
	maxSizedText     = 4096
)

// oscState is the OSC-owned part of Terminal state.
type oscState struct {
	events eventQueue

	defaults     bool
	defFg        [3]uint8
	defBg        [3]uint8
	defCursor    [3]uint8
	defPal       [16][3]uint8
	links        *grid.LinkTable
	primaryWrite func(string)
	primaryRead  func() string

	promptAbs    int
	cmdPromptAbs int
	cmdRunning   bool
	cmdStart     time.Time
	cmdLine      string

	progressState, progressValue int
	notify                       map[string]*pendingNotify
	pointer                      []string
	altPointer                   []string
	bel                          bool
	userVars                     map[string]string
	colors                       colorStack
}

type pendingNotify struct {
	title, body strings.Builder
	urgency     int
}

var (
	defaultPrimaryReaderMu sync.Mutex
	defaultPrimaryReader   func() string
)

// SetDefaultPrimaryReader sets the process-wide fallback reader answering
// OSC 52 primary-selection queries (nil disables them, like the clipboard gate).
func SetDefaultPrimaryReader(reader func() string) {
	defaultPrimaryReaderMu.Lock()
	defaultPrimaryReader = reader
	defaultPrimaryReaderMu.Unlock()
}

// SetPrimaryWriter sets the callback receiving OSC 52 writes to the primary selection.
func (t *Terminal) SetPrimaryWriter(writer func(string)) {
	t.mu.Lock()
	t.osc.primaryWrite = writer
	t.mu.Unlock()
}

// SetPrimaryReader sets the callback answering OSC 52 primary-selection queries.
func (t *Terminal) SetPrimaryReader(reader func() string) {
	t.mu.Lock()
	t.osc.primaryRead = reader
	t.mu.Unlock()
}

func (t *Terminal) handleOSC(params string) {
	code, value, _ := strings.Cut(params, ";")
	switch code {
	case "0":
		t.iconName = sanitizeTitle(value)
		t.setTitle(value)
	case "1":
		t.iconName = sanitizeTitle(value)
	case "2":
		t.setTitle(value)
	case "4":
		t.handleOSC4(value)
	case "10", "11", "12":
		t.handleDynamicColors(code, value)
	case "104":
		t.resetPalette(value)
	case "110":
		t.fgColor = grid.Color{}
	case "111":
		t.bgColor = grid.Color{}
	case "112":
		t.cursorColor = grid.Color{}
	case "7":
		if path := parseOSC7Path(value); path != "" {
			t.lastWorkingDir = path
		}
	case "8":
		t.handleOSC8(value)
	case "9":
		t.handleOSC9(value)
	case "22":
		t.handleOSC22(value)
	case "52":
		t.handleOSC52(value)
	case "66":
		t.handleOSC66(value)
	case "99":
		t.handleOSC99(value)
	case "133":
		t.handleOSC133(value)
	case "777":
		if rest, ok := strings.CutPrefix(value, "notify;"); ok {
			title, body, _ := strings.Cut(rest, ";")
			t.pushEvent(Event{Kind: EventNotify, Title: title, Body: body, Urgency: 1})
		}
	case "1337":
		t.handleOSC1337(value)
	case "7771":
		name, arg, _ := strings.Cut(value, "=")
		if validName(name, 64) {
			t.pushEvent(Event{Kind: EventRavenCommand, Name: name, Value: arg})
		}
	}
}

func (t *Terminal) reply(s string) {
	if t.responseWriter != nil {
		t.responseWriter([]byte(s))
	}
}

func validName(s string, maxLen int) bool {
	if s == "" || len(s) > maxLen {
		return false
	}
	for _, c := range s {
		if !(c == '-' || c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

func sanitizeTitle(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > maxTitleLen {
		cut := maxTitleLen
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return s
}

func (t *Terminal) setTitle(v string) {
	v = sanitizeTitle(v)
	if v == t.windowTitle {
		return
	}
	t.windowTitle = v
	t.pushEvent(Event{Kind: EventTitle, Title: v})
}

// Title returns the current window title (OSC 0/2).
func (t *Terminal) Title() string { return t.GetWindowTitle() }

// --- colours ---

var fallbackPalette = [16][3]uint8{
	{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0}, {0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0}, {92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

// SetDefaultColors sets the theme colours OSC 4/10/11/12 queries report and
// OSC 104/110/111/112 reset to.
func (t *Terminal) SetDefaultColors(fg, bg, cursor grid.Color, palette [16]grid.Color) {
	t.mu.Lock()
	defer t.mu.Unlock()
	o := &t.osc
	o.defaults = true
	for i, c := range palette {
		o.defPal[i] = resolveColor(c, fallbackPalette[i], fallbackPalette)
	}
	o.defFg = resolveColor(fg, fallbackPalette[7], o.defPal)
	o.defBg = resolveColor(bg, fallbackPalette[0], o.defPal)
	o.defCursor = resolveColor(cursor, o.defFg, o.defPal)
}

func resolveColor(c grid.Color, def [3]uint8, pal [16][3]uint8) [3]uint8 {
	switch c.Type {
	case grid.ColorRGB:
		return [3]uint8{c.R, c.G, c.B}
	case grid.ColorIndexed:
		if c.Index < 16 {
			return pal[c.Index]
		}
		return defaultIndexed(int(c.Index), pal)
	}
	return def
}

func defaultIndexed(i int, pal [16][3]uint8) [3]uint8 {
	switch {
	case i < 16:
		return pal[i]
	case i < 232:
		lvl := func(n int) uint8 {
			if n == 0 {
				return 0
			}
			return uint8(55 + 40*n)
		}
		i -= 16
		return [3]uint8{lvl(i / 36), lvl(i / 6 % 6), lvl(i % 6)}
	}
	v := uint8(8 + 10*(i-232))
	return [3]uint8{v, v, v}
}

func (t *Terminal) defaultsLocked() (fg, bg, cursor [3]uint8, pal [16][3]uint8) {
	o := &t.osc
	if !o.defaults {
		return fallbackPalette[7], fallbackPalette[0], fallbackPalette[7], fallbackPalette
	}
	return o.defFg, o.defBg, o.defCursor, o.defPal
}

func (t *Terminal) paletteColorLocked(i int) [3]uint8 {
	if c, ok := t.paletteOverride[i]; ok && c.Type == grid.ColorRGB {
		return [3]uint8{c.R, c.G, c.B}
	}
	_, _, _, pal := t.defaultsLocked()
	return defaultIndexed(i, pal)
}

func (t *Terminal) dynamicColorLocked(code int) [3]uint8 {
	fg, bg, cur, _ := t.defaultsLocked()
	pick := func(c grid.Color, def [3]uint8) [3]uint8 {
		if c.Type == grid.ColorRGB {
			return [3]uint8{c.R, c.G, c.B}
		}
		return def
	}
	switch code {
	case 10:
		return pick(t.fgColor, fg)
	case 11:
		return pick(t.bgColor, bg)
	}
	return pick(t.cursorColor, cur)
}

// EffectiveColors returns the colours currently in force: theme defaults
// with any OSC 4/10/11/12 overrides applied. The renderer consults this.
func (t *Terminal) EffectiveColors() (palette [256][3]uint8, fg, bg, cursor [3]uint8) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range palette {
		palette[i] = t.paletteColorLocked(i)
	}
	return palette, t.dynamicColorLocked(10), t.dynamicColorLocked(11), t.dynamicColorLocked(12)
}

// handleDynamicColors implements OSC 10/11/12 set/query, including the
// multi-value form where successive values address the following codes.
func (t *Terminal) handleDynamicColors(code, value string) {
	n, _ := strconv.Atoi(code)
	for spec := range strings.SplitSeq(value, ";") {
		if n > 12 {
			return
		}
		if spec == "?" {
			t.reply(fmt.Sprintf("\x1b]%d;%s%s", n, formatRGB(t.dynamicColorLocked(n)), t.oscEnd()))
		} else if c, ok := parseXColor(spec); ok {
			switch n {
			case 10:
				t.fgColor = c
			case 11:
				t.bgColor = c
			case 12:
				t.cursorColor = c
			}
		}
		n++
	}
}

// handleOSC4 implements OSC 4 palette set/query for any number of
// "index;spec" pairs (spec "?" queries).
func (t *Terminal) handleOSC4(value string) {
	parts := strings.Split(value, ";")
	for i := 0; i+1 < len(parts); i += 2 {
		idx, err := strconv.Atoi(parts[i])
		if err != nil || idx < 0 || idx > 255 {
			continue
		}
		if parts[i+1] == "?" {
			t.reply(fmt.Sprintf("\x1b]4;%d;%s%s", idx, formatRGB(t.paletteColorLocked(idx)), t.oscEnd()))
			continue
		}
		if c, ok := parseXColor(parts[i+1]); ok {
			if t.paletteOverride == nil {
				t.paletteOverride = make(map[int]grid.Color)
			}
			t.paletteOverride[idx] = c
		}
	}
}

// resetPalette implements OSC 104: reset listed indices, or all when none.
func (t *Terminal) resetPalette(value string) {
	reset := false
	for s := range strings.SplitSeq(value, ";") {
		if idx, err := strconv.Atoi(s); err == nil {
			delete(t.paletteOverride, idx)
			reset = true
		}
	}
	if !reset {
		t.paletteOverride = nil
	}
}

// ForegroundColor / BackgroundColor / CursorColor expose any OSC-set colors
// (Type == ColorRGB when set; ColorDefault otherwise).
func (t *Terminal) ForegroundColor() grid.Color { t.mu.Lock(); defer t.mu.Unlock(); return t.fgColor }
func (t *Terminal) BackgroundColor() grid.Color { t.mu.Lock(); defer t.mu.Unlock(); return t.bgColor }
func (t *Terminal) CursorColor() grid.Color     { t.mu.Lock(); defer t.mu.Unlock(); return t.cursorColor }

// PaletteOverride returns the OSC 4 color override for an index, if any.
func (t *Terminal) PaletteOverride(idx int) (grid.Color, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.paletteOverride[idx]
	return c, ok
}

// --- OSC 8 hyperlinks ---

// linkTable returns the terminal-wide link table, attaching it to both screens.
func (t *Terminal) linkTable() *grid.LinkTable {
	if t.osc.links == nil {
		t.osc.links = grid.NewLinkTable()
		t.Grid.SetLinkTableLocked(t.osc.links)
		if t.savedMainGrid != nil {
			t.savedMainGrid.SetLinkTableLocked(t.osc.links)
		}
	}
	return t.osc.links
}

func (t *Terminal) handleOSC8(value string) {
	params, uri, ok := strings.Cut(value, ";")
	if !ok || uri == "" {
		t.currentLinkID = 0
		return
	}
	key := ""
	for kv := range strings.SplitSeq(params, ":") {
		if v, ok := strings.CutPrefix(kv, "id="); ok {
			key = v
		}
	}
	lt := t.linkTable()
	id, ok := lt.Intern(key, uri)
	if !ok {
		live := map[uint16]bool{t.currentLinkID: true, t.ctl.savedMain.link: true, t.ctl.savedAlt.link: true}
		t.Grid.MarkLiveLinksLocked(live)
		if t.savedMainGrid != nil {
			t.savedMainGrid.MarkLiveLinksLocked(live)
		}
		lt.Sweep(live)
		id, _ = lt.Intern(key, uri)
	}
	t.currentLinkID = id
}

// --- OSC 52 clipboard ---

// handleOSC52 implements OSC 52 set/query. Pc selects the target: c is the
// clipboard, p and s the primary selection, empty means c.
func (t *Terminal) handleOSC52(value string) {
	sel, data, ok := strings.Cut(value, ";")
	if !ok {
		return
	}
	if sel == "" {
		sel = "c"
	}
	clip := strings.ContainsRune(sel, 'c')
	prim := strings.ContainsAny(sel, "ps")
	if data == "?" {
		letter := ""
		for _, c := range sel {
			if c == 'c' || c == 'p' || c == 's' {
				letter = string(c)
				break
			}
		}
		var reader func() string
		switch letter {
		case "":
			return
		case "c":
			reader = t.clipboardReader
			if reader == nil {
				defaultClipboardReaderMu.Lock()
				reader = defaultClipboardReader
				defaultClipboardReaderMu.Unlock()
			}
		default:
			reader = t.osc.primaryRead
			if reader == nil {
				defaultPrimaryReaderMu.Lock()
				reader = defaultPrimaryReader
				defaultPrimaryReaderMu.Unlock()
			}
		}
		writer := t.responseWriter
		if reader == nil || writer == nil {
			return
		}
		// The reader may block on the main thread, which needs t.mu to render:
		// answer off the lock, in one write so the reply stays contiguous.
		end := t.oscEnd()
		go func() {
			enc := base64.StdEncoding.EncodeToString([]byte(reader()))
			writer([]byte("\x1b]52;" + letter + ";" + enc + end))
		}()
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return
	}
	if clip && t.clipboardWriter != nil {
		t.clipboardWriter(string(decoded))
	}
	if prim && t.osc.primaryWrite != nil {
		t.osc.primaryWrite(string(decoded))
	}
}

// --- OSC 9 / 99 notifications and progress ---

func (t *Terminal) handleOSC9(value string) {
	if sub, rest, ok := strings.Cut(value, ";"); ok && sub != "" && strings.Trim(sub, "0123456789") == "" {
		switch sub {
		case "4":
			st, val, _ := strings.Cut(rest, ";")
			state, err := strconv.Atoi(st)
			if err != nil || state < 0 || state > 4 {
				return
			}
			v, _ := strconv.Atoi(val)
			t.setProgress(state, min(max(v, 0), 100))
		case "9":
			if p := strings.Trim(rest, "\""); strings.HasPrefix(p, "/") {
				t.lastWorkingDir = p
			}
		}
		return
	}
	if value != "" {
		t.pushEvent(Event{Kind: EventNotify, Body: value, Urgency: 1})
	}
}

func (t *Terminal) setProgress(state, value int) {
	if state == 0 {
		value = 0
	}
	t.osc.progressState, t.osc.progressValue = state, value
	t.pushEvent(Event{Kind: EventProgress, ProgressState: state, ProgressValue: value})
}

// Progress returns the current OSC 9;4 progress state and value.
func (t *Terminal) Progress() (state, value int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.osc.progressState, t.osc.progressValue
}

// resetOSC clears OSC state that RIS resets.
func (t *Terminal) resetOSC() {
	if t.osc.progressState != 0 {
		t.setProgress(0, 0)
	}
	t.osc.notify = nil
	t.osc.colors = colorStack{}
	t.osc.altPointer = nil
	if len(t.osc.pointer) > 0 {
		t.osc.pointer = nil
		t.pushEvent(Event{Kind: EventPointerShape})
	}
}

// handleOSC99 implements kitty desktop notifications:
// OSC 99 ; key=val:key=val ; payload, chunked by id while d=0.
func (t *Terminal) handleOSC99(value string) {
	meta, payload, _ := strings.Cut(value, ";")
	id, done, kind, enc, urgency := "", true, "title", false, -1
	for kv := range strings.SplitSeq(meta, ":") {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "i":
			id = v
		case "d":
			done = v != "0"
		case "p":
			kind = v
		case "e":
			enc = v == "1"
		case "u":
			if u, err := strconv.Atoi(v); err == nil && u >= 0 && u <= 2 {
				urgency = u
			}
		}
	}
	if id != "" && !validName(id, 64) {
		return
	}
	if kind == "?" {
		t.reply("\x1b]99;i=" + id + ":p=?;p=title,body:u=0,1,2" + t.oscEnd())
		return
	}
	if enc {
		b, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			if b, err = base64.RawStdEncoding.DecodeString(payload); err != nil {
				return
			}
		}
		payload = string(b)
	}
	if t.osc.notify == nil {
		t.osc.notify = map[string]*pendingNotify{}
	}
	n := t.osc.notify[id]
	if n == nil {
		if len(t.osc.notify) >= maxPendingNotify {
			for k := range t.osc.notify {
				delete(t.osc.notify, k)
				break
			}
		}
		n = &pendingNotify{urgency: 1}
		t.osc.notify[id] = n
	}
	if urgency >= 0 {
		n.urgency = urgency
	}
	switch kind {
	case "title":
		if n.title.Len()+len(payload) <= maxNotifyPayload {
			n.title.WriteString(payload)
		}
	case "body":
		if n.body.Len()+len(payload) <= maxNotifyPayload {
			n.body.WriteString(payload)
		}
	}
	if !done {
		return
	}
	delete(t.osc.notify, id)
	if n.title.Len() == 0 && n.body.Len() == 0 {
		return
	}
	t.pushEvent(Event{Kind: EventNotify, ID: id, Title: strings.ToValidUTF8(n.title.String(), ""),
		Body: strings.ToValidUTF8(n.body.String(), ""), Urgency: n.urgency})
}

// --- OSC 133 semantic prompts ---

func (t *Terminal) handleOSC133(value string) {
	fields := strings.Split(value, ";")
	params := map[string]string{}
	for _, f := range fields[1:] {
		if k, v, ok := strings.Cut(f, "="); ok {
			params[k] = v
		}
	}
	o := &t.osc
	switch fields[0] {
	case "A", "P":
		if k := params["k"]; k != "" && k != "i" {
			return
		}
		t.Grid.SetMarkLocked(grid.MarkPrompt)
		o.promptAbs = t.Grid.AbsoluteCursorRowLocked()
	case "B":
		t.Grid.SetMarkLocked(grid.MarkInput)
	case "C":
		t.Grid.SetMarkLocked(grid.MarkOutput)
		o.cmdRunning, o.cmdStart, o.cmdPromptAbs = true, time.Now(), o.promptAbs
		o.cmdLine = params["cmdline"]
		if u, ok := params["cmdline_url"]; ok {
			if s, err := url.PathUnescape(u); err == nil {
				o.cmdLine = s
			}
		}
	case "D":
		t.Grid.SetMarkLocked(grid.MarkEnd)
		if !o.cmdRunning {
			return
		}
		o.cmdRunning = false
		code := 0
		if len(fields) > 1 {
			if c, err := strconv.Atoi(fields[1]); err == nil {
				code = c
			}
		}
		t.Grid.SetExitCodeLocked(o.cmdPromptAbs, code)
		t.pushEvent(Event{Kind: EventCommandFinished, ExitCode: code, Body: o.cmdLine,
			Value: strconv.FormatInt(time.Since(o.cmdStart).Milliseconds(), 10)})
	}
}

// --- OSC 22 pointer shape ---

var pointerShapes = map[string]bool{
	"default": true, "none": true, "context-menu": true, "help": true, "pointer": true, "progress": true,
	"wait": true, "cell": true, "crosshair": true, "text": true, "vertical-text": true, "alias": true,
	"copy": true, "move": true, "no-drop": true, "not-allowed": true, "grab": true, "grabbing": true,
	"e-resize": true, "n-resize": true, "ne-resize": true, "nw-resize": true, "s-resize": true,
	"se-resize": true, "sw-resize": true, "w-resize": true, "ew-resize": true, "ns-resize": true,
	"nesw-resize": true, "nwse-resize": true, "col-resize": true, "row-resize": true, "all-scroll": true,
	"zoom-in": true, "zoom-out": true,
}

var x11Pointers = map[string]string{
	"left_ptr": "default", "arrow": "default", "top_left_arrow": "default", "xterm": "text", "ibeam": "text",
	"hand": "pointer", "hand1": "pointer", "hand2": "pointer", "pointing_hand": "pointer",
	"watch": "wait", "left_ptr_watch": "progress", "question_arrow": "help", "cross": "crosshair",
	"tcross": "crosshair", "fleur": "move", "pirate": "not-allowed", "x_cursor": "not-allowed",
	"sb_h_double_arrow": "ew-resize", "sb_v_double_arrow": "ns-resize", "right_side": "e-resize",
	"left_side": "w-resize", "top_side": "n-resize", "bottom_side": "s-resize",
	"top_left_corner": "nw-resize", "top_right_corner": "ne-resize", "bottom_left_corner": "sw-resize",
	"bottom_right_corner": "se-resize", "plus": "cell", "dnd-copy": "copy", "dnd-link": "alias",
}

func normalizePointer(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if pointerShapes[name] {
		return name, true
	}
	s, ok := x11Pointers[name]
	return s, ok
}

func (t *Terminal) currentPointer() string {
	if n := len(t.osc.pointer); n > 0 {
		return t.osc.pointer[n-1]
	}
	return ""
}

func (t *Terminal) handleOSC22(value string) {
	before := t.currentPointer()
	op := byte('=')
	if value != "" && strings.IndexByte("=><?", value[0]) >= 0 {
		op, value = value[0], value[1:]
	}
	st := &t.osc.pointer
	switch op {
	case '?':
		var ans []string
		for n := range strings.SplitSeq(value, ",") {
			switch n {
			case "__current__":
				ans = append(ans, cmpOr(t.currentPointer(), "default"))
			case "__default__", "__grabbed__":
				ans = append(ans, "default")
			default:
				if _, ok := normalizePointer(n); ok {
					ans = append(ans, "1")
				} else {
					ans = append(ans, "0")
				}
			}
		}
		t.reply("\x1b]22;" + strings.Join(ans, ",") + t.oscEnd())
		return
	case '>':
		for n := range strings.SplitSeq(value, ",") {
			if s, ok := normalizePointer(n); ok && len(*st) < maxPointerStack {
				*st = append(*st, s)
			}
		}
	case '<':
		if len(*st) > 0 {
			*st = (*st)[:len(*st)-1]
		}
	default:
		if value == "" {
			*st = nil
			break
		}
		for n := range strings.SplitSeq(value, ",") {
			if s, ok := normalizePointer(n); ok {
				if len(*st) == 0 {
					*st = append(*st, s)
				} else {
					(*st)[len(*st)-1] = s
				}
				break
			}
		}
	}
	if now := t.currentPointer(); now != before {
		t.pushEvent(Event{Kind: EventPointerShape, Shape: now})
	}
}

// oscEnd is the terminator for a reply: BEL when the request ended with BEL,
// ST otherwise.
func (t *Terminal) oscEnd() string {
	if t.osc.bel {
		return "\x07"
	}
	return "\x1b\\"
}

// swapPointerStacks gives the alternate screen its own OSC 22 stack, fresh
// on every entry.
func (t *Terminal) swapPointerStacks(enter bool) {
	before := t.currentPointer()
	t.osc.pointer, t.osc.altPointer = t.osc.altPointer, t.osc.pointer
	if !enter {
		t.osc.altPointer = nil
	}
	if now := t.currentPointer(); now != before {
		t.pushEvent(Event{Kind: EventPointerShape, Shape: now})
	}
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// PointerShape returns the CSS pointer shape requested via OSC 22 ("" = default).
func (t *Terminal) PointerShape() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.currentPointer()
}

// --- OSC 1337 user vars ---

func (t *Terminal) handleOSC1337(value string) {
	kv, ok := strings.CutPrefix(value, "SetUserVar=")
	if !ok {
		return
	}
	name, enc, _ := strings.Cut(kv, "=")
	if !validName(name, 128) || len(enc) > maxNotifyPayload {
		return
	}
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return
	}
	if t.osc.userVars == nil {
		t.osc.userVars = map[string]string{}
	}
	if _, exists := t.osc.userVars[name]; !exists && len(t.osc.userVars) >= maxUserVars {
		return
	}
	t.osc.userVars[name] = string(b)
	t.pushEvent(Event{Kind: EventUserVar, Name: name, Value: string(b)})
}

// UserVars returns a copy of the variables set via OSC 1337 SetUserVar.
func (t *Terminal) UserVars() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]string, len(t.osc.userVars))
	maps.Copy(out, t.osc.userVars)
	return out
}

// --- OSC 66 text sizing ---

// handleOSC66 implements kitty text sizing: s (scale, 1-7), w (width in
// cells, 0 = natural), n/d (fractional scale), v/h (alignment of fractionally
// scaled text). The text occupies s*w cells on s rows; the cells hold it at
// 1x for copying and the block is recorded on each row for the renderer.
func (t *Terminal) handleOSC66(value string) {
	meta, text, ok := strings.Cut(value, ";")
	if !ok || text == "" || len(text) > maxSizedText {
		return
	}
	st := grid.SizedText{Scale: 1}
	w := 0
	for kv := range strings.SplitSeq(meta, ":") {
		k, v, _ := strings.Cut(kv, "=")
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		switch k {
		case "s":
			st.Scale = min(max(n, 1), 7)
		case "w":
			w = min(max(n, 0), 7)
		case "n":
			st.Num = min(max(n, 0), 15)
		case "d":
			st.Den = min(max(n, 0), 15)
		case "v":
			if n >= 0 && n <= 2 {
				st.VAlign = n
			}
		case "h":
			if n >= 0 && n <= 2 {
				st.HAlign = n
			}
		}
	}
	if st.Num == 0 || st.Den == 0 || st.Num >= st.Den {
		st.Num, st.Den = 0, 0
	}
	var runes []rune
	natural := 0
	for _, r := range strings.ToValidUTF8(text, "") {
		if r < 0x20 || r == 0x7f {
			continue
		}
		runes = append(runes, r)
		natural += grid.RuneWidth(r)
	}
	cells := w
	if cells == 0 {
		cells = natural
	}
	g := t.Grid
	total := min(st.Scale*cells, g.Cols)
	if total == 0 {
		return
	}
	if g.WrapPendingLocked() && g.GetAutoWrapLocked() || g.CursorCol+total > g.Cols {
		g.CarriageReturnLocked()
		g.NewlineLocked()
	}
	if st.Scale > 1 {
		for i := 1; i < st.Scale; i++ {
			g.NewlineLocked()
		}
		g.MoveCursorLocked(0, -(st.Scale - 1))
	}
	col, row := g.CursorCol, g.CursorRow
	for i := 1; i < st.Scale; i++ {
		g.SetCursorPosLocked(col+1, row+i+1)
		for range total {
			g.WriteCharLocked(' ', t.currentFg, t.currentBg, t.currentFlags, 0, 0, grid.Color{})
		}
	}
	g.SetCursorPosLocked(col+1, row+1)
	used := 0
	for _, r := range runes {
		rw := grid.RuneWidth(r)
		if used+rw > total {
			break
		}
		t.printRune(r)
		used += rw
	}
	for ; used < total; used++ {
		t.printRune(' ')
	}
	st.Cells, st.Text = total, string(runes)
	g.PutSizedTextLocked(col, row, st)
}

func parseOSC7Path(value string) string {
	if strings.HasPrefix(value, "file://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return ""
		}
		if parsed.Path == "" {
			return ""
		}
		return parsed.Path
	}
	if strings.HasPrefix(value, "/") {
		return value
	}
	return ""
}
