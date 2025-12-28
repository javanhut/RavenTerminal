package keybindings

import (
	"github.com/go-gl/glfw/v3.3/glfw"
)

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
)

// KeyResult contains the result of processing a key
type KeyResult struct {
	Action KeyAction
	Data   []byte
}

// TranslateKey translates a GLFW key event to terminal input
func TranslateKey(key glfw.Key, mods glfw.ModifierKey, appCursorMode bool) KeyResult {
	ctrl := mods&glfw.ModControl != 0
	shift := mods&glfw.ModShift != 0
	alt := mods&glfw.ModAlt != 0

	// Special key combinations
	if ctrl && key == glfw.KeyQ {
		return KeyResult{Action: ActionExit}
	}

	if ctrl && shift && key == glfw.KeyT {
		return KeyResult{Action: ActionNewTab}
	}

	if ctrl && shift && key == glfw.KeyX {
		return KeyResult{Action: ActionCloseTab}
	}

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
		if shift {
			return KeyResult{Action: ActionInput, Data: []byte("\x1b[Z")}
		}
		return KeyResult{Action: ActionInput, Data: []byte{'\t'}}
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
	alt := mods&glfw.ModAlt != 0

	if alt {
		// Alt sends ESC prefix
		return []byte{0x1b, byte(char)}
	}

	// UTF-8 encode the character
	buf := make([]byte, 4)
	n := encodeRune(buf, char)
	return buf[:n]
}

// encodeRune encodes a rune as UTF-8
func encodeRune(buf []byte, r rune) int {
	if r < 0x80 {
		buf[0] = byte(r)
		return 1
	}
	if r < 0x800 {
		buf[0] = byte(0xC0 | (r >> 6))
		buf[1] = byte(0x80 | (r & 0x3F))
		return 2
	}
	if r < 0x10000 {
		buf[0] = byte(0xE0 | (r >> 12))
		buf[1] = byte(0x80 | ((r >> 6) & 0x3F))
		buf[2] = byte(0x80 | (r & 0x3F))
		return 3
	}
	buf[0] = byte(0xF0 | (r >> 18))
	buf[1] = byte(0x80 | ((r >> 12) & 0x3F))
	buf[2] = byte(0x80 | ((r >> 6) & 0x3F))
	buf[3] = byte(0x80 | (r & 0x3F))
	return 4
}
