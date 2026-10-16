//========================================================================
// Raven Terminal extensions to GLFW 3.3: shared API
//========================================================================

#include "internal.h"
#include "raven.h"

#include <assert.h>
#include <stdlib.h>
#include <string.h>


//////////////////////////////////////////////////////////////////////////
//////                         GLFW event API                       //////
//////////////////////////////////////////////////////////////////////////

void _glfwInputPreedit(_GLFWwindow* window, const char* text, int cursorBytes)
{
    if (window->callbacks.preedit)
        window->callbacks.preedit((GLFWwindow*) window, text ? text : "", cursorBytes);
}

void _glfwInputCommitString(_GLFWwindow* window, const char* text, int mods)
{
    const char* s = text;

    while (s && *s)
        _glfwInputChar(window, _glfwRavenDecodeUTF8(&s), mods, GLFW_TRUE);
}

#if !defined(_GLFW_WAYLAND)
// The monitor containing the window's center, else the one it overlaps most
//
static _GLFWmonitor* _glfwRavenMonitorAtWindowCenter(_GLFWwindow* window)
{
    _GLFWmonitor* best = NULL;
    long bestArea = 0;
    int x, y, width, height;

    if (window->monitor)
        return window->monitor;

    _glfwPlatformGetWindowPos(window, &x, &y);
    _glfwPlatformGetWindowSize(window, &width, &height);

    const int cx = x + width / 2, cy = y + height / 2;

    for (int i = 0;  i < _glfw.monitorCount;  i++)
    {
        _GLFWmonitor* monitor = _glfw.monitors[i];
        GLFWvidmode mode;
        int mx, my;

        _glfwPlatformGetVideoMode(monitor, &mode);
        _glfwPlatformGetMonitorPos(monitor, &mx, &my);

        if (cx >= mx && cx < mx + mode.width && cy >= my && cy < my + mode.height)
            return monitor;

        const long ow = (long) _glfw_min(x + width, mx + mode.width) - _glfw_max(x, mx);
        const long oh = (long) _glfw_min(y + height, my + mode.height) - _glfw_max(y, my);
        if (ow > 0 && oh > 0 && ow * oh > bestArea)
        {
            bestArea = ow * oh;
            best = monitor;
        }
    }

    return best;
}

_GLFWmonitor* _glfwPlatformGetWindowCurrentMonitor(_GLFWwindow* window)
{
    return _glfwRavenMonitorAtWindowCenter(window);
}
#endif

#if !defined(_GLFW_X11) && !defined(_GLFW_WAYLAND)
// No system primary selection here; keep it in-process so selecting and
// middle-click pasting inside the application still works
static char* _glfwRavenPrimarySelection;

void _glfwPlatformSetPrimarySelectionString(const char* string)
{
    char* copy = _glfw_strdup(string);
    if (!copy)
    {
        _glfwInputError(GLFW_OUT_OF_MEMORY, NULL);
        return;
    }

    free(_glfwRavenPrimarySelection);
    _glfwRavenPrimarySelection = copy;
}

const char* _glfwPlatformGetPrimarySelectionString(void)
{
    if (!_glfwRavenPrimarySelection)
        _glfwInputError(GLFW_FORMAT_UNAVAILABLE, "No primary selection available");
    return _glfwRavenPrimarySelection;
}

// Cocoa reads window->imeCursor when the input method asks for it
void _glfwPlatformSetIMECursorRect(_GLFWwindow* window)
{
}
#endif


//////////////////////////////////////////////////////////////////////////
//////                        GLFW public API                       //////
//////////////////////////////////////////////////////////////////////////

GLFWAPI void glfwSetPrimarySelectionString(const char* string)
{
    assert(string != NULL);

    _GLFW_REQUIRE_INIT();
    _glfwPlatformSetPrimarySelectionString(string);
}

GLFWAPI const char* glfwGetPrimarySelectionString(void)
{
    _GLFW_REQUIRE_INIT_OR_RETURN(NULL);
    return _glfwPlatformGetPrimarySelectionString();
}

GLFWAPI GLFWpreeditfun glfwSetPreeditCallback(GLFWwindow* handle, GLFWpreeditfun cbfun)
{
    _GLFWwindow* window = (_GLFWwindow*) handle;
    assert(window != NULL);

    _GLFW_REQUIRE_INIT_OR_RETURN(NULL);
    _GLFW_SWAP_POINTERS(window->callbacks.preedit, cbfun);
    return cbfun;
}

GLFWAPI void glfwSetIMECursorRect(GLFWwindow* handle, int x, int y, int width, int height)
{
    _GLFWwindow* window = (_GLFWwindow*) handle;
    assert(window != NULL);

    _GLFW_REQUIRE_INIT();

    if (window->imeCursor.x == x && window->imeCursor.y == y &&
        window->imeCursor.width == width && window->imeCursor.height == height)
    {
        return;
    }

    window->imeCursor.x = x;
    window->imeCursor.y = y;
    window->imeCursor.width = width;
    window->imeCursor.height = height;
    _glfwPlatformSetIMECursorRect(window);
}

GLFWAPI GLFWmonitor* glfwGetWindowCurrentMonitor(GLFWwindow* handle)
{
    _GLFWwindow* window = (_GLFWwindow*) handle;
    assert(window != NULL);

    _GLFW_REQUIRE_INIT_OR_RETURN(NULL);
    return (GLFWmonitor*) _glfwPlatformGetWindowCurrentMonitor(window);
}
