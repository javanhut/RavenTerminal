package glfw

//#include <stdlib.h>
//#define GLFW_INCLUDE_NONE
//#include "glfw/include/GLFW/glfw3.h"
//void glfwSetPreeditCallbackCB(GLFWwindow *window);
import "C"

import "unsafe"

// Standard cursor shapes added in GLFW 3.4.
const (
	ResizeNWSECursor   StandardCursor = C.GLFW_RESIZE_NWSE_CURSOR
	ResizeNESWCursor   StandardCursor = C.GLFW_RESIZE_NESW_CURSOR
	ResizeAllCursor    StandardCursor = C.GLFW_RESIZE_ALL_CURSOR
	NotAllowedCursor   StandardCursor = C.GLFW_NOT_ALLOWED_CURSOR
	PointingHandCursor StandardCursor = HandCursor
	ResizeEWCursor     StandardCursor = HResizeCursor
	ResizeNSCursor     StandardCursor = VResizeCursor
)

// Hyper and Meta modifier bits (Raven extension, X11 and Wayland). Each is
// set only when the keymap gives it a modifier of its own; stock keymaps
// share Hyper with Super and Meta with Alt, and there neither bit appears.
const (
	ModHyper ModifierKey = C.GLFW_MOD_HYPER
	ModMeta  ModifierKey = C.GLFW_MOD_META
)

// KeySymbols is what a key produces in the active keyboard layout.
type KeySymbols struct {
	Keysym  uint32 // XKB keysym with no modifiers
	Rune    rune   // its character, 0 when it has none (media keys, modifiers)
	Shifted rune   // the character with Shift alone, 0 when none
}

// GetScancodeSymbols reports what the key with this platform scancode (as
// passed to the key callback) produces in the active layout group. It also
// covers keys that arrive as KeyUnknown, which is how media keys come in.
// ok is false on platforms without keysyms (macOS, Windows) and before the
// first keymap arrives.
//
// This function may only be called from the main thread.
func GetScancodeSymbols(scancode int) (syms KeySymbols, ok bool) {
	var keysym, cp, shifted C.uint
	found := C.glfwGetScancodeSymbols(C.int(scancode), &keysym, &cp, &shifted)
	panicError()
	if found == 0 {
		return KeySymbols{}, false
	}
	return KeySymbols{Keysym: uint32(keysym), Rune: rune(cp), Shifted: rune(shifted)}, true
}

// SetPrimarySelectionString sets the primary selection (X11 PRIMARY, Wayland
// zwp_primary_selection_v1, in-process elsewhere).
//
// This function may only be called from the main thread.
func SetPrimarySelectionString(str string) {
	cp := C.CString(str)
	defer C.free(unsafe.Pointer(cp))
	C.glfwSetPrimarySelectionString(cp)
	panicError()
}

// GetPrimarySelectionString returns the primary selection, or "" when there
// is none or it holds no text.
//
// This function may only be called from the main thread.
func GetPrimarySelectionString() string {
	cs := C.glfwGetPrimarySelectionString()
	if cs == nil {
		acceptError(FormatUnavailable)
		return ""
	}
	return C.GoString(cs)
}

//export goPreeditCB
func goPreeditCB(window unsafe.Pointer, text *C.char, cursorBytes C.int) {
	w := windows.get((*C.GLFWwindow)(window))
	if w.fPreeditHolder != nil {
		w.fPreeditHolder(w, C.GoString(text), int(cursorBytes))
	}
}

// SetPreeditCallback sets the input method composition callback. text is the
// UTF-8 pre-edit string ("" when composition ends) and cursorBytes the caret
// as a byte offset into it, or -1 when hidden. Committed text still arrives
// through the character callback.
func (w *Window) SetPreeditCallback(cb func(w *Window, text string, cursorBytes int)) {
	w.fPreeditHolder = cb
	if cb == nil {
		C.glfwSetPreeditCallback(w.data, nil)
	} else {
		C.glfwSetPreeditCallbackCB(w.data)
	}
	panicError()
}

// SetIMECursorRect tells the input method where the text cursor is, in window
// content coordinates (the units of GetSize and GetCursorPos), so candidate
// windows open next to it.
func (w *Window) SetIMECursorRect(x, y, width, height int) {
	C.glfwSetIMECursorRect(w.data, C.int(x), C.int(y), C.int(width), C.int(height))
	panicError()
}

// CurrentMonitor returns the monitor the window is on: on Wayland the first
// output its surface entered that it is still on, elsewhere the monitor
// containing the window's center. It returns nil when that is not known yet
// (e.g. an unmapped Wayland window).
func (w *Window) CurrentMonitor() *Monitor {
	m := C.glfwGetWindowCurrentMonitor(w.data)
	panicError()
	if m == nil {
		return nil
	}
	return &Monitor{m}
}
