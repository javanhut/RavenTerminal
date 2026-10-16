package keybindings

import (
	"runtime"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// isMacOS reports whether app shortcuts should use Cmd (Super) instead of Ctrl+Shift.
var isMacOS = runtime.GOOS == "darwin"

// digitForKey maps the top-row number keys 1..9 to their value, 0 otherwise.
func digitForKey(key glfw.Key) int {
	if key >= glfw.Key1 && key <= glfw.Key9 {
		return int(key-glfw.Key1) + 1
	}
	return 0
}

// KeyAction represents the action to take for a key press
type KeyAction int

const (
	ActionNone KeyAction = iota
	ActionExit
	ActionInput
	ActionScrollUp
	ActionScrollDown
	ActionScrollUpLine
	ActionScrollDownLine
	ActionNewTab
	ActionCloseTab
	ActionNextTab
	ActionPrevTab
	ActionToggleFullscreen
	ActionSplitVertical
	ActionSplitHorizontal
	ActionClosePane
	ActionNextPane
	ActionPrevPane
	ActionShowHelp
	ActionHelpScrollUp
	ActionHelpScrollDown
	ActionZoomIn
	ActionZoomOut
	ActionZoomReset
	ActionOpenMenu
	ActionToggleSearchPanel
	ActionToggleAIPanel
	ActionCopy
	ActionPaste
	ActionToggleResizeMode
	ActionSelectTab
	ActionFindInScrollback
	ActionPrevPrompt
	ActionNextPrompt
	ActionCopyLastOutput
	ActionHints
	ActionCopyMode
	ActionCommandPalette
	ActionTogglePaneZoom
	ActionToggleBroadcast
	ActionScrollbackPager
)

// KeyResult contains the result of processing a key
type KeyResult struct {
	Action KeyAction
	Data   []byte
	Num    int // payload for actions that need a number (e.g. ActionSelectTab -> 1-based tab)
}

// Event is one key event from the window system.
type Event struct {
	Key    glfw.Key
	Mods   glfw.ModifierKey
	Action glfw.Action
	// Rune is the unshifted character the key produces in the active layout
	// (glfw.GetKeyName); 0 when unknown.
	Rune rune
}

// Mode is the terminal state that shapes how keys are encoded.
type Mode struct {
	AppCursor       bool // DECCKM
	AppKeypad       bool // DECKPAM
	AltScreen       bool
	MouseReporting  bool
	ModifyOtherKeys int
	KittyFlags      uint8
	// AltNoEscape: the program turned off both ?1036 and ?1039, so Alt
	// does not prefix ESC.
	AltNoEscape bool
}

const (
	modShift = glfw.ModShift
	modCtrl  = glfw.ModControl
	modAlt   = glfw.ModAlt
	modSuper = glfw.ModSuper
	modReal  = modShift | modCtrl | modAlt | modSuper
)

// TranslateKey translates a key press with only DECCKM known. Overlays use it
// to recognise app shortcuts.
func TranslateKey(key glfw.Key, mods glfw.ModifierKey, appCursorMode bool) KeyResult {
	return Translate(Event{Key: key, Mods: mods, Action: glfw.Press}, Mode{AppCursor: appCursorMode})
}

// Translate maps a key event to an app shortcut or to the bytes the
// terminal application should receive. Printable keys that the legacy
// encoding leaves alone return ActionNone: their text arrives through the
// char callback (TranslateChar), which knows the layout's exact character.
func Translate(ev Event, m Mode) KeyResult {
	if ev.Action != glfw.Release {
		if res, ok := shortcut(ev.Key, ev.Mods, m); ok {
			return res
		}
	}
	var data []byte
	if m.KittyFlags&(kittyDisambiguate|kittyAllKeys) != 0 {
		data = encodeKitty(ev, m)
	} else if ev.Action != glfw.Release {
		data = encodeLegacy(ev, m)
	}
	if len(data) == 0 {
		return KeyResult{}
	}
	return KeyResult{Action: ActionInput, Data: data}
}

// TranslateChar translates text from the char callback. Alt prefixes ESC,
// or with ?1036/?1039 off sets the eighth bit of ASCII like xterm.
func TranslateChar(char rune, mods glfw.ModifierKey, m Mode) []byte {
	if mods&modAlt != 0 {
		if !m.AltNoEscape {
			return utf8.AppendRune([]byte{0x1b}, char)
		}
		if char < 0x80 {
			char |= 0x80
		}
	}
	return utf8.AppendRune(nil, char)
}

func shortcut(key glfw.Key, mods glfw.ModifierKey, m Mode) (KeyResult, bool) {
	ctrl := mods&modCtrl != 0
	shift := mods&modShift != 0
	super := mods&modSuper != 0
	linux := !isMacOS

	// The leader is Super (Cmd) on every platform, plus Ctrl+Shift on Linux.
	// Plain Ctrl and Alt chords always belong to the running program.
	leader := super || (linux && ctrl && shift)

	// Super+Ctrl is the compositor's layer on Linux (Huginn puts window
	// management there). Swallowed so it neither acts here nor reaches the
	// shell when another compositor forwards it. See docs/keybindings.md.
	if linux && super && ctrl {
		return KeyResult{}, true
	}
	act := func(a KeyAction) (KeyResult, bool) { return KeyResult{Action: a}, true }

	// Leader+Shift: Super+Shift, or Ctrl+Shift+Alt for the Ctrl+Shift leader.
	if (super && shift) || (linux && ctrl && shift && mods&modAlt != 0) {
		switch key {
		case glfw.KeyC:
			return act(ActionCopyLastOutput)
		case glfw.KeyH:
			return act(ActionScrollbackPager)
		case glfw.KeyV:
			return act(ActionCopyMode)
		case glfw.KeyP:
			return act(ActionCommandPalette)
		case glfw.KeyD:
			return act(ActionSplitHorizontal)
		case glfw.KeyTab:
			return act(ActionPrevTab)
		}
	}

	if leader {
		if n := digitForKey(key); n != 0 {
			return KeyResult{Action: ActionSelectTab, Num: n}, true
		}
		switch key {
		case glfw.KeyQ:
			return act(ActionExit)
		case glfw.KeyC:
			return act(ActionCopy)
		case glfw.KeyT:
			return act(ActionNewTab)
		case glfw.KeyX:
			return act(ActionCloseTab)
		case glfw.KeyW:
			return act(ActionClosePane)
		case glfw.KeyK:
			return act(ActionShowHelp)
		case glfw.KeyEqual:
			return act(ActionZoomIn)
		case glfw.KeyMinus:
			return act(ActionZoomOut)
		case glfw.Key0:
			return act(ActionZoomReset)
		case glfw.KeyS:
			return act(ActionOpenMenu)
		case glfw.KeyA:
			return act(ActionToggleAIPanel)
		case glfw.KeyR:
			return act(ActionToggleResizeMode)
		case glfw.KeyUp:
			return act(ActionPrevPrompt)
		case glfw.KeyDown:
			return act(ActionNextPrompt)
		case glfw.KeyE, glfw.KeyH:
			return act(ActionHints)
		case glfw.KeyP:
			return act(ActionCommandPalette)
		case glfw.KeyV:
			return act(ActionPaste)
		case glfw.KeyD:
			return act(ActionSplitVertical)
		case glfw.KeyTab:
			return act(ActionNextTab)
		case glfw.KeySpace:
			return act(ActionCopyMode)
		case glfw.KeyZ:
			return act(ActionTogglePaneZoom)
		case glfw.KeyB:
			return act(ActionToggleBroadcast)
		case glfw.KeyEnter, glfw.KeyKPEnter:
			return act(ActionToggleFullscreen)
		case glfw.KeyF:
			// Find lives on Super+Shift+F: on Linux the leader is Ctrl+Shift,
			// so Ctrl+Shift+F must stay the web-search panel.
			if super && shift {
				return act(ActionFindInScrollback)
			}
			return act(ActionToggleSearchPanel)
		}
	}

	// Super+Shift+]/[ cycle tabs; Super+]/[ (and Ctrl+Shift+]/[ on Linux) cycle panes.
	if super && shift && key == glfw.KeyRightBracket {
		return act(ActionNextTab)
	}
	if super && shift && key == glfw.KeyLeftBracket {
		return act(ActionPrevTab)
	}
	if leader && key == glfw.KeyRightBracket {
		return act(ActionNextPane)
	}
	if leader && key == glfw.KeyLeftBracket {
		return act(ActionPrevPane)
	}
	if key == glfw.KeyF11 && mods&modReal == 0 {
		return act(ActionToggleFullscreen)
	}

	// Shift scrolling is the terminal's only on the main screen while the
	// program has not claimed the mouse or the kitty keyboard; otherwise the
	// keys belong to the program.
	if mods&modReal == modShift && !m.AltScreen && !m.MouseReporting && m.KittyFlags == 0 {
		switch key {
		case glfw.KeyPageUp:
			return act(ActionScrollUp)
		case glfw.KeyPageDown:
			return act(ActionScrollDown)
		case glfw.KeyUp:
			return act(ActionScrollUpLine)
		case glfw.KeyDown:
			return act(ActionScrollDownLine)
		}
	}
	return KeyResult{}, false
}

// xtermMod is the xterm modifier parameter: 1 + shift|alt<<1|ctrl<<2|super<<3.
func xtermMod(mods glfw.ModifierKey) int {
	m := 1
	if mods&modShift != 0 {
		m += 1
	}
	if mods&modAlt != 0 {
		m += 2
	}
	if mods&modCtrl != 0 {
		m += 4
	}
	if mods&modSuper != 0 {
		m += 8
	}
	return m
}

var (
	cursorFinal = map[glfw.Key]byte{
		glfw.KeyUp: 'A', glfw.KeyDown: 'B', glfw.KeyRight: 'C', glfw.KeyLeft: 'D',
		glfw.KeyHome: 'H', glfw.KeyEnd: 'F',
	}
	ss3Final = map[glfw.Key]byte{glfw.KeyF1: 'P', glfw.KeyF2: 'Q', glfw.KeyF3: 'R', glfw.KeyF4: 'S'}
	tildeNum = map[glfw.Key]int{
		glfw.KeyInsert: 2, glfw.KeyDelete: 3, glfw.KeyPageUp: 5, glfw.KeyPageDown: 6,
		glfw.KeyF5: 15, glfw.KeyF6: 17, glfw.KeyF7: 18, glfw.KeyF8: 19,
		glfw.KeyF9: 20, glfw.KeyF10: 21, glfw.KeyF11: 23, glfw.KeyF12: 24,
	}
	kpApp = map[glfw.Key]byte{
		glfw.KeyKP0: 'p', glfw.KeyKP1: 'q', glfw.KeyKP2: 'r', glfw.KeyKP3: 's', glfw.KeyKP4: 't',
		glfw.KeyKP5: 'u', glfw.KeyKP6: 'v', glfw.KeyKP7: 'w', glfw.KeyKP8: 'x', glfw.KeyKP9: 'y',
		glfw.KeyKPDecimal: 'n', glfw.KeyKPDivide: 'o', glfw.KeyKPMultiply: 'j',
		glfw.KeyKPSubtract: 'm', glfw.KeyKPAdd: 'k', glfw.KeyKPEnter: 'M', glfw.KeyKPEqual: 'X',
	}
	// Keypad keys with Num Lock off act as the navigation keys printed on them.
	kpNav = map[glfw.Key]glfw.Key{
		glfw.KeyKP0: glfw.KeyInsert, glfw.KeyKP1: glfw.KeyEnd, glfw.KeyKP2: glfw.KeyDown,
		glfw.KeyKP3: glfw.KeyPageDown, glfw.KeyKP4: glfw.KeyLeft, glfw.KeyKP6: glfw.KeyRight,
		glfw.KeyKP7: glfw.KeyHome, glfw.KeyKP8: glfw.KeyUp, glfw.KeyKP9: glfw.KeyPageUp,
		glfw.KeyKPDecimal: glfw.KeyDelete,
	}
)

func isKPNumeric(key glfw.Key) bool {
	return (key >= glfw.KeyKP0 && key <= glfw.KeyKP9) || key == glfw.KeyKPDecimal
}

func csiParams(final byte, params ...int) []byte {
	b := []byte{0x1b, '['}
	for i, p := range params {
		if i > 0 {
			b = append(b, ';')
		}
		b = strconv.AppendInt(b, int64(p), 10)
	}
	return append(b, final)
}

func modifyOther(mod int, code rune) []byte {
	return csiParams('~', 27, mod, int(code))
}

func encodeLegacy(ev Event, m Mode) []byte {
	key, mods := ev.Key, ev.Mods
	mod := xtermMod(mods)

	if key >= glfw.KeyF13 && key <= glfw.KeyF24 {
		ev.Key, ev.Mods = key-12, mods|modShift
		return encodeLegacy(ev, m)
	}
	if f, ok := cursorFinal[key]; ok {
		if mod > 1 {
			return csiParams(f, 1, mod)
		}
		if m.AppCursor {
			return []byte{0x1b, 'O', f}
		}
		return []byte{0x1b, '[', f}
	}
	if f, ok := ss3Final[key]; ok {
		if mod > 1 {
			return csiParams(f, 1, mod)
		}
		return []byte{0x1b, 'O', f}
	}
	if n, ok := tildeNum[key]; ok {
		if mod > 1 {
			return csiParams('~', n, mod)
		}
		return csiParams('~', n)
	}
	if code, ok := kpApp[key]; ok {
		if isKPNumeric(key) && mods&glfw.ModNumLock == 0 {
			if key == glfw.KeyKP5 {
				if mod > 1 {
					return csiParams('E', 1, mod)
				}
				return []byte("\x1b[E")
			}
			ev.Key = kpNav[key]
			return encodeLegacy(ev, m)
		}
		if m.AppKeypad && mods&(modCtrl|modAlt|modSuper) == 0 {
			return []byte{0x1b, 'O', code}
		}
		if key != glfw.KeyKPEnter {
			return nil // text arrives via the char callback
		}
		key = glfw.KeyEnter
	}

	switch key {
	case glfw.KeyEnter:
		return legacySpecial(13, []byte{'\r'}, mods, m)
	case glfw.KeyTab:
		return legacySpecial(9, []byte{'\t'}, mods, m)
	case glfw.KeyBackspace:
		return legacySpecial(127, []byte{0x7f}, mods, m)
	case glfw.KeyEscape:
		return legacySpecial(27, []byte{0x1b}, mods, m)
	}
	return legacyText(ev, m)
}

// legacySpecial encodes Enter/Tab/Backspace/Escape.
func legacySpecial(code rune, base []byte, mods glfw.ModifierKey, m Mode) []byte {
	real := mods & modReal
	if real == 0 {
		return base
	}
	mod := xtermMod(mods)
	if m.ModifyOtherKeys >= 2 && !(code == 9 && real == modShift) {
		return modifyOther(mod, code)
	}
	if m.ModifyOtherKeys >= 1 && (code == 13 || code == 27) && real&(modShift|modCtrl) != 0 {
		return modifyOther(mod, code)
	}
	out := base
	switch {
	case code == 9 && real&modShift != 0:
		out = []byte("\x1b[Z")
	case code == 127 && real&modCtrl != 0:
		out = []byte{0x08}
	}
	if real&modAlt != 0 && !m.AltNoEscape {
		out = append([]byte{0x1b}, out...)
	}
	return out
}

// legacyText encodes Ctrl (and modifyOtherKeys) chords on printable keys.
// Plain, Shift and Alt chords are left to the char callback.
func legacyText(ev Event, m Mode) []byte {
	us := usRune(ev.Key)
	c := ev.Rune
	if c == 0 {
		c = us
	}
	if c == 0 {
		return nil
	}
	mods := ev.Mods
	ctrl, alt, shift := mods&modCtrl != 0, mods&modAlt != 0, mods&modShift != 0
	if !ctrl && !(alt && m.ModifyOtherKeys >= 2) {
		return nil
	}
	code := c
	if shift && unicode.IsLetter(c) {
		code = unicode.ToUpper(c)
	}
	mod := xtermMod(mods)
	if m.ModifyOtherKeys >= 2 {
		return modifyOther(mod, code)
	}
	// Non-Latin layouts: control characters come from the physical key.
	cc := c
	if cc >= 0x80 && us != 0 {
		cc = us
	}
	if b, ok := ctrlByte(cc); ok && !(m.ModifyOtherKeys >= 1 && shift) {
		if alt && !m.AltNoEscape {
			return []byte{0x1b, b}
		}
		return []byte{b}
	}
	if m.ModifyOtherKeys >= 1 {
		return modifyOther(mod, code)
	}
	return nil
}

// ctrlByte is the control character xterm sends for Ctrl+c.
func ctrlByte(c rune) (byte, bool) {
	switch {
	case c >= 'a' && c <= 'z':
		return byte(c) & 0x1f, true
	case c >= '@' && c <= '_':
		return byte(c) & 0x1f, true
	}
	switch c {
	case ' ', '2':
		return 0, true
	case '3':
		return 0x1b, true
	case '4':
		return 0x1c, true
	case '5':
		return 0x1d, true
	case '6', '~':
		return 0x1e, true
	case '7', '/':
		return 0x1f, true
	case '8', '?':
		return 0x7f, true
	}
	return 0, false
}

// usRune is the character a key produces on a US layout without Shift (the
// kitty "base layout key"), 0 for non-text keys.
func usRune(key glfw.Key) rune {
	switch {
	case key >= glfw.KeyA && key <= glfw.KeyZ:
		return rune('a' + (key - glfw.KeyA))
	case key >= glfw.Key0 && key <= glfw.Key9, key == glfw.KeySpace, key == glfw.KeyApostrophe,
		key >= glfw.KeyComma && key <= glfw.KeySlash, key == glfw.KeySemicolon, key == glfw.KeyEqual,
		key >= glfw.KeyLeftBracket && key <= glfw.KeyRightBracket, key == glfw.KeyGraveAccent:
		return rune(key)
	}
	return 0
}

var usShifted = map[rune]rune{
	'1': '!', '2': '@', '3': '#', '4': '$', '5': '%', '6': '^', '7': '&', '8': '*', '9': '(', '0': ')',
	'-': '_', '=': '+', '[': '{', ']': '}', '\\': '|', ';': ':', '\'': '"', ',': '<', '.': '>',
	'/': '?', '`': '~',
}

// shiftedRune is the best known Shift variant of c on the active layout.
func shiftedRune(c, us rune) rune {
	if unicode.IsLetter(c) {
		return unicode.ToUpper(c)
	}
	if c == us {
		if s, ok := usShifted[c]; ok {
			return s
		}
	}
	return c
}
