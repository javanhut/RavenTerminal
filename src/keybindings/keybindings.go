package keybindings

import (
	"runtime"
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
)

// KeyResult contains the result of processing a key
type KeyResult struct {
	Action KeyAction
	Data   []byte
	Num    int // payload for actions that need a number (e.g. ActionSelectTab -> 1-based tab)
}

// TranslateKey translates a GLFW key event to terminal input
func TranslateKey(key glfw.Key, mods glfw.ModifierKey, appCursorMode bool) KeyResult {
	ctrl := mods&glfw.ModControl != 0
	shift := mods&glfw.ModShift != 0
	alt := mods&glfw.ModAlt != 0
	super := mods&glfw.ModSuper != 0

	// primary is the app-shortcut leader. The Super (Cmd) layer is identical
	// on every platform so muscle memory transfers between macOS and Linux;
	// Linux additionally accepts the conventional Ctrl+Shift chords. Keeping
	// plain Ctrl out of it leaves the Control key free for terminal control
	// characters (Ctrl+C, Ctrl+D, ...), matching iTerm2/Terminal.app.
	primary := super || (!isMacOS && ctrl && shift)

	// Super+Ctrl is the compositor's layer on Linux. Huginn (RavenGUI) puts
	// every window-management chord there — Super+Ctrl+T opens a terminal,
	// Super+Ctrl+Q closes a window, Super+Ctrl+Shift+1..9 sends one to a
	// workspace — and never forwards them, so under Huginn these never
	// arrive. Under another compositor they would, and `primary` would read
	// Super+Ctrl+T as "new tab": a chord the desktop documents as the
	// compositor's must not do something else the moment the compositor is
	// not there to take it. Swallowed rather than sent to the shell, since no
	// control character was meant. See docs/keybindings.md, "Reserved".
	if !isMacOS && super && ctrl {
		return KeyResult{Action: ActionNone}
	}

	// Exit: Ctrl+Q everywhere, plus Super+Q (Cmd+Q-style) on both platforms.
	if (ctrl && key == glfw.KeyQ) || (super && key == glfw.KeyQ) {
		return KeyResult{Action: ActionExit}
	}

	// Quick tab access: primary + 1..9 (Cmd+1..9 on macOS, Ctrl+Shift+1..9 elsewhere).
	if primary {
		if n := digitForKey(key); n != 0 {
			return KeyResult{Action: ActionSelectTab, Num: n}
		}
	}

	// Copy follows the primary modifier on both platforms (Cmd+C / Ctrl+Shift+C).
	if primary && key == glfw.KeyC {
		return KeyResult{Action: ActionCopy}
	}
	// Paste: Super+V on both platforms (Cmd+V-style), plus the legacy
	// Ctrl+Shift+P on Linux.
	if (super && key == glfw.KeyV) || (!isMacOS && ctrl && shift && key == glfw.KeyP) {
		return KeyResult{Action: ActionPaste}
	}

	if primary && key == glfw.KeyT {
		return KeyResult{Action: ActionNewTab}
	}

	if primary && key == glfw.KeyX {
		return KeyResult{Action: ActionCloseTab}
	}

	if primary && key == glfw.KeyW {
		return KeyResult{Action: ActionClosePane}
	}

	// Splits: iTerm-style Super+D / Super+Shift+D on both platforms (Super+V
	// is paste, so splits can't use V there); Linux also keeps the
	// conventional Ctrl+Shift+V / Ctrl+Shift+H.
	if (super && !shift && key == glfw.KeyD) || (!isMacOS && ctrl && shift && key == glfw.KeyV) {
		return KeyResult{Action: ActionSplitVertical}
	}
	if (super && shift && key == glfw.KeyD) || (!isMacOS && ctrl && shift && key == glfw.KeyH) {
		return KeyResult{Action: ActionSplitHorizontal}
	}

	if primary && key == glfw.KeyK {
		return KeyResult{Action: ActionShowHelp}
	}

	// Tab cycling with brackets: Super+Shift+] / Super+Shift+[ on both
	// platforms (the Safari / Terminal.app convention; Cmd+Tab can't be used
	// — macOS reserves it for the app switcher). Checked before the pane
	// bindings so Shift selects tabs over panes.
	if super && shift && key == glfw.KeyRightBracket {
		return KeyResult{Action: ActionNextTab}
	}
	if super && shift && key == glfw.KeyLeftBracket {
		return KeyResult{Action: ActionPrevTab}
	}

	// Pane cycling: Super+] / Super+[ on both platforms (no Shift); Linux
	// also keeps Ctrl+Shift+] / [.
	if (super && !shift && key == glfw.KeyRightBracket) || (!isMacOS && ctrl && shift && key == glfw.KeyRightBracket) {
		return KeyResult{Action: ActionNextPane}
	}
	if (super && !shift && key == glfw.KeyLeftBracket) || (!isMacOS && ctrl && shift && key == glfw.KeyLeftBracket) {
		return KeyResult{Action: ActionPrevPane}
	}

	// Zoom controls: primary + (= / - / 0).
	if primary && key == glfw.KeyEqual {
		return KeyResult{Action: ActionZoomIn}
	}

	if primary && key == glfw.KeyMinus {
		return KeyResult{Action: ActionZoomOut}
	}

	if primary && key == glfw.Key0 {
		return KeyResult{Action: ActionZoomReset}
	}

	if primary && key == glfw.KeyS {
		return KeyResult{Action: ActionOpenMenu}
	}
	// Find in scrollback: Super+Shift+F on every platform. It has to live on
	// the Super layer rather than follow `primary`: on Linux primary already
	// means Ctrl+Shift, so Ctrl+Shift+F is the web-search panel below and
	// cannot also mean find. Checked first so Cmd+Shift+F on macOS reaches
	// find instead of falling into the shift-agnostic panel binding.
	if super && shift && key == glfw.KeyF {
		return KeyResult{Action: ActionFindInScrollback}
	}
	if primary && key == glfw.KeyF {
		return KeyResult{Action: ActionToggleSearchPanel}
	}
	if primary && key == glfw.KeyA {
		return KeyResult{Action: ActionToggleAIPanel}
	}

	// Resize mode: Ctrl+R everywhere, plus Super+R on both platforms.
	if (ctrl && !shift && key == glfw.KeyR) || (super && !shift && key == glfw.KeyR) {
		return KeyResult{Action: ActionToggleResizeMode}
	}

	// Cycle tabs with Ctrl+Tab / Ctrl+Shift+Tab (works on all platforms; Ctrl here is
	// the physical Control key, distinct from Cmd on macOS).
	if ctrl && key == glfw.KeyTab {
		if shift {
			return KeyResult{Action: ActionPrevTab}
		}
		return KeyResult{Action: ActionNextTab}
	}

	if shift && key == glfw.KeyPageUp {
		return KeyResult{Action: ActionScrollUp}
	}

	if shift && key == glfw.KeyPageDown {
		return KeyResult{Action: ActionScrollDown}
	}

	// Shift+Arrow for single line scrolling
	if shift && key == glfw.KeyUp {
		return KeyResult{Action: ActionScrollUpLine}
	}

	if shift && key == glfw.KeyDown {
		return KeyResult{Action: ActionScrollDownLine}
	}

	// Arrow keys
	if key == glfw.KeyUp {
		if appCursorMode {
			return KeyResult{Action: ActionInput, Data: []byte("\x1bOA")}
		}
		return KeyResult{Action: ActionInput, Data: []byte("\x1b[A")}
	}
	if key == glfw.KeyDown {
		if appCursorMode {
			return KeyResult{Action: ActionInput, Data: []byte("\x1bOB")}
		}
		return KeyResult{Action: ActionInput, Data: []byte("\x1b[B")}
	}
	if key == glfw.KeyRight {
		if appCursorMode {
			return KeyResult{Action: ActionInput, Data: []byte("\x1bOC")}
		}
		return KeyResult{Action: ActionInput, Data: []byte("\x1b[C")}
	}
	if key == glfw.KeyLeft {
		if appCursorMode {
			return KeyResult{Action: ActionInput, Data: []byte("\x1bOD")}
		}
		return KeyResult{Action: ActionInput, Data: []byte("\x1b[D")}
	}

	// Home/End
	if key == glfw.KeyHome {
		return KeyResult{Action: ActionInput, Data: []byte("\x1b[H")}
	}
	if key == glfw.KeyEnd {
		return KeyResult{Action: ActionInput, Data: []byte("\x1b[F")}
	}

	// Page Up/Down (without shift)
	if key == glfw.KeyPageUp {
		return KeyResult{Action: ActionInput, Data: []byte("\x1b[5~")}
	}
	if key == glfw.KeyPageDown {
		return KeyResult{Action: ActionInput, Data: []byte("\x1b[6~")}
	}

	// Insert/Delete
	if key == glfw.KeyInsert {
		return KeyResult{Action: ActionInput, Data: []byte("\x1b[2~")}
	}
	if key == glfw.KeyDelete {
		return KeyResult{Action: ActionInput, Data: []byte("\x1b[3~")}
	}

	// Function keys
	fKeySeqs := map[glfw.Key][]byte{
		glfw.KeyF1:  []byte("\x1bOP"),
		glfw.KeyF2:  []byte("\x1bOQ"),
		glfw.KeyF3:  []byte("\x1bOR"),
		glfw.KeyF4:  []byte("\x1bOS"),
		glfw.KeyF5:  []byte("\x1b[15~"),
		glfw.KeyF6:  []byte("\x1b[17~"),
		glfw.KeyF7:  []byte("\x1b[18~"),
		glfw.KeyF8:  []byte("\x1b[19~"),
		glfw.KeyF9:  []byte("\x1b[20~"),
		glfw.KeyF10: []byte("\x1b[21~"),
		glfw.KeyF11: []byte("\x1b[23~"),
		glfw.KeyF12: []byte("\x1b[24~"),
	}
	if seq, ok := fKeySeqs[key]; ok {
		return KeyResult{Action: ActionInput, Data: seq}
	}

	// Backspace
	if key == glfw.KeyBackspace {
		return KeyResult{Action: ActionInput, Data: []byte{0x7f}}
	}

	// Shift+Enter for fullscreen toggle
	if shift && (key == glfw.KeyEnter || key == glfw.KeyKPEnter) {
		return KeyResult{Action: ActionToggleFullscreen}
	}

	// Enter
	if key == glfw.KeyEnter || key == glfw.KeyKPEnter {
		return KeyResult{Action: ActionInput, Data: []byte{'\r'}}
	}

	// Tab
	if key == glfw.KeyTab {
		// Pane cycling: Cmd+Shift+Tab on macOS, Super+Shift+Tab on Linux —
		// the same physical chord everywhere. (macOS reserves Cmd+Shift+Tab
		// for the app switcher unless remapped, and tiling WMs often grab
		// Super; the leader+]/[ bindings always cycle panes as well.)
		if super && shift {
			return KeyResult{Action: ActionNextPane}
		}
		if shift && !ctrl && !super {
			// Plain Shift+Tab belongs to the running app: it's back-tab
			// (CSI Z) in nvim, Claude Code's mode cycling, form navigation
			// in TUIs, etc.
			return KeyResult{Action: ActionInput, Data: []byte("\x1b[Z")}
		}
		if !ctrl && !shift {
			return KeyResult{Action: ActionInput, Data: []byte{'\t'}}
		}
	}

	// Escape
	if key == glfw.KeyEscape {
		return KeyResult{Action: ActionInput, Data: []byte{0x1b}}
	}

	// Control + letter combinations
	if ctrl && key >= glfw.KeyA && key <= glfw.KeyZ {
		// Ctrl+A = 1, Ctrl+B = 2, etc.
		return KeyResult{Action: ActionInput, Data: []byte{byte(key - glfw.KeyA + 1)}}
	}

	// Space - only handle Ctrl+Space here; normal space is handled by char callback
	if key == glfw.KeySpace {
		if ctrl {
			return KeyResult{Action: ActionInput, Data: []byte{0}}
		}
		// Let the char callback handle normal space to avoid double input
		return KeyResult{Action: ActionNone}
	}

	// Alt + key sends ESC prefix
	if alt && key >= glfw.KeyA && key <= glfw.KeyZ {
		c := byte(key - glfw.KeyA + 'a')
		if shift {
			c = byte(key - glfw.KeyA + 'A')
		}
		return KeyResult{Action: ActionInput, Data: []byte{0x1b, c}}
	}

	return KeyResult{Action: ActionNone}
}

// TranslateChar translates a character input to terminal bytes
func TranslateChar(char rune, mods glfw.ModifierKey) []byte {
	if mods&glfw.ModAlt != 0 {
		// Alt sends ESC prefix
		return utf8.AppendRune([]byte{0x1b}, char)
	}
	return utf8.AppendRune(nil, char)
}
