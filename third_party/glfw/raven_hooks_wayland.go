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
import "C"

func waylandNextProxyID() uint32 { return uint32(C.ravenNextProxyID()) }

func cursorShapeFor(shape StandardCursor) int {
	return int(C._glfwRavenCursorShapeWayland(C.int(shape)))
}
