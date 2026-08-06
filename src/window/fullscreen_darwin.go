//go:build darwin

package window

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static int ravenIsFullscreen(void *win) {
	return ([(NSWindow *)win styleMask] & NSWindowStyleMaskFullScreen) != 0;
}

static void ravenToggleFullscreen(void *win) {
	[(NSWindow *)win toggleFullScreen:nil];
}
*/
import "C"

// macOS uses AppKit's own fullscreen rather than GLFW's SetMonitor. GLFW already
// marks resizable windows NSWindowCollectionBehaviorFullScreenPrimary, so the
// green button and Cmd+Ctrl+F drive this path whether we like it or not; sharing
// it keeps our shortcut and the OS in one state instead of two that disagree.
//
// It also removes the whole savedX/savedY/savedWidth/savedHeight dance: AppKit
// records the pre-fullscreen frame itself and restores it exactly. Reading that
// geometry ourselves was unreliable because glfwSetWindowMonitor pumps the event
// loop mid-transition and the frame change lands asynchronously, so a fast second
// toggle (or a held Shift+Enter, which key-repeats into this) captured the
// fullscreen rect as the windowed one.
func toggleFullscreen(w *Window) {
	C.ravenToggleFullscreen(w.glfw.GetCocoaWindow())
}

func isFullscreen(w *Window) bool {
	return C.ravenIsFullscreen(w.glfw.GetCocoaWindow()) != 0
}
