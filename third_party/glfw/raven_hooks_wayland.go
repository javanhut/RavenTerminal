//go:build linux && wayland

package glfw

//#include "glfw/src/internal.h"
//extern int _glfwRavenCursorShapeWayland(int shape);
//static uint32_t ravenNextProxyID(void) {
//    struct wl_callback* callback = wl_display_sync(_glfw.wl.display);
//    const uint32_t id = wl_proxy_get_id((struct wl_proxy*) callback);
//    wl_callback_destroy(callback);
//    return id;
//}
//// ravenSwapKeymap installs keymap (taking ownership; NULL restores none)
//// the way keyboardHandleKeymap does and returns the previous one, owned
//// by the caller.
//static struct xkb_keymap* ravenSwapKeymap(struct xkb_keymap* keymap) {
//    struct xkb_keymap* old = _glfw.wl.xkb.keymap;
//    if (_glfw.wl.xkb.state)
//        xkb_state_unref(_glfw.wl.xkb.state);
//    _glfw.wl.xkb.keymap = keymap;
//    _glfw.wl.xkb.state = keymap ? xkb_state_new(keymap) : NULL;
//    if (keymap) {
//        _glfw.wl.xkb.controlIndex  = xkb_keymap_mod_get_index(keymap, "Control");
//        _glfw.wl.xkb.altIndex      = xkb_keymap_mod_get_index(keymap, "Mod1");
//        _glfw.wl.xkb.shiftIndex    = xkb_keymap_mod_get_index(keymap, "Shift");
//        _glfw.wl.xkb.superIndex    = xkb_keymap_mod_get_index(keymap, "Mod4");
//        _glfw.wl.xkb.capsLockIndex = xkb_keymap_mod_get_index(keymap, "Lock");
//        _glfw.wl.xkb.numLockIndex  = xkb_keymap_mod_get_index(keymap, "Mod2");
//    }
//    _glfwRavenKeymapChangedWayland();
//    return old;
//}
//static struct xkb_keymap* ravenKeymapFromNames(const char* layout, const char* options) {
//    struct xkb_rule_names names = { "evdev", "pc105", layout, "", options };
//    return xkb_keymap_new_from_names(_glfw.wl.xkb.context, &names, 0);
//}
//static struct xkb_keymap* ravenKeymapFromString(const char* text) {
//    return xkb_keymap_new_from_string(_glfw.wl.xkb.context, text, XKB_KEYMAP_FORMAT_TEXT_V1, 0);
//}
//static int ravenSetModifiers(unsigned int depressed, unsigned int locked, unsigned int group) {
//    xkb_state_update_mask(_glfw.wl.xkb.state, depressed, 0, locked, 0, 0, group);
//    return _glfwRavenModifiersWayland();
//}
//static unsigned int ravenModMask(const char* name) {
//    xkb_mod_index_t i = xkb_keymap_mod_get_index(_glfw.wl.xkb.keymap, name);
//    return i == XKB_MOD_INVALID ? 0 : 1u << i;
//}
//static unsigned int ravenHyperMask(void) { return _glfw.wl.xkb.hyperMask; }
//static unsigned int ravenMetaMask(void) { return _glfw.wl.xkb.metaMask; }
//static void ravenUnrefKeymap(struct xkb_keymap* keymap) { if (keymap) xkb_keymap_unref(keymap); }
import "C"

import (
	"errors"
	"unsafe"
)

func waylandNextProxyID() uint32 { return uint32(C.ravenNextProxyID()) }

func cursorShapeFor(shape StandardCursor) int {
	return int(C._glfwRavenCursorShapeWayland(C.int(shape)))
}

// useTestKeymap swaps in a keymap compiled from RMLVO names, or from full
// keymap text when text is set, and returns a func restoring the previous one.
func useTestKeymap(layout, options, text string) (restore func(), err error) {
	var km *C.struct_xkb_keymap
	if text != "" {
		ct := C.CString(text)
		defer C.free(unsafe.Pointer(ct))
		km = C.ravenKeymapFromString(ct)
	} else {
		cl, co := C.CString(layout), C.CString(options)
		defer C.free(unsafe.Pointer(cl))
		defer C.free(unsafe.Pointer(co))
		km = C.ravenKeymapFromNames(cl, co)
	}
	if km == nil {
		return nil, errors.New("keymap did not compile")
	}
	old := C.ravenSwapKeymap(km)
	return func() { C.ravenUnrefKeymap(C.ravenSwapKeymap(old)) }, nil
}

// testModifiers sets the keyboard state and returns the GLFW modifier bits.
func testModifiers(depressed, locked, group uint32) ModifierKey {
	return ModifierKey(C.ravenSetModifiers(C.uint(depressed), C.uint(locked), C.uint(group)))
}

func testModMask(name string) uint32 {
	cn := C.CString(name)
	defer C.free(unsafe.Pointer(cn))
	return uint32(C.ravenModMask(cn))
}

func testHyperMetaMasks() (hyper, meta uint32) {
	return uint32(C.ravenHyperMask()), uint32(C.ravenMetaMask())
}
