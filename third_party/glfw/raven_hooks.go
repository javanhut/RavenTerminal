//go:build linux

package glfw

// Unexported hooks into the C side, used by the package tests.

//#include <stdlib.h>
//#include "glfw/src/internal.h"
//static int ravenInitialized(void) { return _glfw.initialized; }
//static void ravenIMECursorRect(GLFWwindow* handle, int* x, int* y, int* w, int* h) {
//    _GLFWwindow* window = (_GLFWwindow*) handle;
//    *x = window->imeCursor.x; *y = window->imeCursor.y;
//    *w = window->imeCursor.width; *h = window->imeCursor.height;
//}
//static void ravenInputPreedit(GLFWwindow* handle, const char* text, int cursor) {
//    _glfwInputPreedit((_GLFWwindow*) handle, text, cursor);
//}
//static void ravenInputCommit(GLFWwindow* handle, const char* text) {
//    _glfwInputCommitString((_GLFWwindow*) handle, text, 0);
//}
//static int ravenPreeditEdit(uint32_t** buffer, int length, int first, int change,
//                            const uint32_t* insert, int insertLength) {
//    return _glfwRavenPreeditEdit(buffer, length, first, change, insert, insertLength);
//}
import "C"

import "unsafe"

func glfwInitialized() bool { return C.ravenInitialized() != 0 }

func scaledSize(logical, scale120 int) int {
	return int(C._glfwRavenScaledSize(C.int(logical), C.int(scale120)))
}

func scaleFromWire(scale120 int) float32 {
	return float32(C._glfwRavenScaleFromWire(C.uint32_t(scale120)))
}

func takeEnv(name string) string {
	cn := C.CString(name)
	defer C.free(unsafe.Pointer(cn))
	v := C._glfwRavenTakeEnv(cn)
	if v == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(v))
	return C.GoString(v)
}

func preeditEdit(buf []rune, first, change int, insert []rune) []rune {
	var cbuf *C.uint32_t
	if len(buf) > 0 {
		cbuf = (*C.uint32_t)(C.malloc(C.size_t(len(buf) * 4)))
		copy(unsafe.Slice((*uint32)(unsafe.Pointer(cbuf)), len(buf)), runesToU32(buf))
	}
	var cins *C.uint32_t
	if len(insert) > 0 {
		cins = (*C.uint32_t)(C.malloc(C.size_t(len(insert) * 4)))
		defer C.free(unsafe.Pointer(cins))
		copy(unsafe.Slice((*uint32)(unsafe.Pointer(cins)), len(insert)), runesToU32(insert))
	}
	n := int(C.ravenPreeditEdit(&cbuf, C.int(len(buf)), C.int(first), C.int(change), cins, C.int(len(insert))))
	defer C.free(unsafe.Pointer(cbuf))
	out := make([]rune, n)
	for i, v := range unsafe.Slice((*uint32)(unsafe.Pointer(cbuf)), n) {
		out[i] = rune(v)
	}
	return out
}

func runesToU32(r []rune) []uint32 {
	out := make([]uint32, len(r))
	for i, v := range r {
		out[i] = uint32(v)
	}
	return out
}

func utf8ByteOffset(s string, runes int) int {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return int(C._glfwRavenUTF8ByteOffset(cs, C.int(runes)))
}

func inputPreedit(w *Window, text string, cursor int) {
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.ravenInputPreedit(w.data, cs, C.int(cursor))
}

func inputCommit(w *Window, text string) {
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.ravenInputCommit(w.data, cs)
}

func imeCursorRect(w *Window) (x, y, width, height int) {
	var cx, cy, cw, ch C.int
	C.ravenIMECursorRect(w.data, &cx, &cy, &cw, &ch)
	return int(cx), int(cy), int(cw), int(ch)
}
