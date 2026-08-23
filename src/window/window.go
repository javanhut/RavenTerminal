package window

import (
	"fmt"
	"image"
	"runtime"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/javanhut/RavenTerminal/src/assets"
)

func init() {
	// GLFW event handling must run on the main thread
	runtime.LockOSThread()
}

// Config holds window configuration
type Config struct {
	Width  int
	Height int
	Title  string
}

// DefaultConfig returns the default window configuration
func DefaultConfig() Config {
	return Config{
		Width:  900,
		Height: 600,
		Title:  "Raven Terminal",
	}
}

// MinWindowWidth and MinWindowHeight are the hard floor enforced via GLFW's
// SetWindowSizeLimits. They are sized to keep the grid above the renderer's
// minGridCols/minGridRows floor even with the tab bar visible and default
// chrome padding:  tabBar(200) + 10px margins + 10 cols * ~10px cellWidth
// ~= 310 wide; 24px vertical padding + 3 rows * ~22px cellHeight ~= 90 tall.
// Rounded up to comfortable defaults.
const (
	MinWindowWidth  = 480
	MinWindowHeight = 320
)

// Window wraps a GLFW window with OpenGL context.
//
// Note there is no isFullscreen field: fullscreen state is queried from the OS
// on every use (see ToggleFullscreen) because the user can enter or leave
// fullscreen without going through us.
type Window struct {
	glfw *glfw.Window
	// Windowed geometry to restore on leaving fullscreen. Used only by the
	// SetMonitor path (see fullscreen_other.go); on macOS AppKit remembers it.
	savedX       int
	savedY       int
	savedWidth   int
	savedHeight  int
	contentScale float32 // HiDPI scale (framebuffer px per logical point)
}

// glfwReady guards one-time GLFW initialization. Every window after the first
// (tab tear-out opens more) must NOT re-init: glfw.Init is only idempotent in
// the sense that it does nothing on repeat, but pairing each Init with the
// Terminate in Destroy would tear down every other window's context when one
// closes. Init happens once here; Terminate is an explicit process-exit call.
var glfwReady bool

// glReady guards one-time OpenGL function-pointer loading. gl.Init binds the
// entry points for the current context; contexts created afterwards share the
// same driver entry points, so once is enough.
var glReady bool

// Terminate shuts GLFW down. Call it once, when the last window has closed and
// the process is exiting — never from Window.Destroy, which may be closing one
// of several open windows.
func Terminate() {
	if glfwReady {
		glfw.Terminate()
		glfwReady = false
	}
}

// NewWindow creates a new GLFW window with OpenGL context
func NewWindow(config Config) (*Window, error) {
	if !glfwReady {
		if err := glfw.Init(); err != nil {
			return nil, fmt.Errorf("failed to initialize GLFW: %w", err)
		}
		glfwReady = true
	}

	// OpenGL context hints
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Resizable, glfw.True)
	glfw.WindowHint(glfw.DoubleBuffer, glfw.True)
	// Don't auto-minimize exclusive-fullscreen windows on focus loss
	// (macOS: swiping to another Space was hiding the window)
	glfw.WindowHint(glfw.AutoIconify, glfw.False)

	// Set X11 window class for proper WM integration (Hyprland, i3, etc.)
	glfw.WindowHintString(glfw.X11ClassName, "raven-terminal")
	glfw.WindowHintString(glfw.X11InstanceName, "raven-terminal")

	window, err := glfw.CreateWindow(config.Width, config.Height, config.Title, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create window: %w", err)
	}

	window.MakeContextCurrent()

	// Enforce a hard minimum window size so the grid can never collapse below
	// the renderer's usable floor (see MinWindowWidth/Height docs). GLFW clamps
	// user-driven resizes and programmatic SetSize calls to these limits.
	window.SetSizeLimits(MinWindowWidth, MinWindowHeight, glfw.DontCare, glfw.DontCare)

	// Initialize OpenGL
	if !glReady {
		if err := gl.Init(); err != nil {
			window.Destroy()
			return nil, fmt.Errorf("failed to initialize OpenGL: %w", err)
		}
		glReady = true
	}

	// Enable VSync. This is per-context, so each window throttles its own
	// presents. Idle windows are gated out of drawing entirely by the redraw
	// triggers, so multiple windows do not serialize into a divided frame rate
	// unless they are all animating at once.
	glfw.SwapInterval(1)

	// Enable blending for text rendering
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)

	w := &Window{glfw: window}

	// HiDPI: track the window's content scale (queried at startup, updated by
	// GLFW when the window moves to a monitor with a different scale). The
	// main loop polls ContentScale and re-rasterizes fonts on change.
	sx, _ := window.GetContentScale()
	w.contentScale = sx
	window.SetContentScaleCallback(func(_ *glfw.Window, x, y float32) {
		w.contentScale = x
	})

	// Load and set application icon
	w.loadIcon()

	return w, nil
}

// ContentScale returns the window's current HiDPI content scale (1.0 on
// standard-DPI displays; e.g. 2.0 on a Retina display).
func (w *Window) ContentScale() float32 {
	if w.contentScale <= 0 {
		return 1
	}
	return w.contentScale
}

// GLFW returns the underlying GLFW window
func (w *Window) GLFW() *glfw.Window {
	return w.glfw
}

// GetSize returns the current window size
func (w *Window) GetSize() (int, int) {
	return w.glfw.GetSize()
}

// GetFramebufferSize returns the framebuffer size
func (w *Window) GetFramebufferSize() (int, int) {
	return w.glfw.GetFramebufferSize()
}

// ShouldClose returns true if the window should close
func (w *Window) ShouldClose() bool {
	return w.glfw.ShouldClose()
}

// SetShouldClose sets the window close flag
func (w *Window) SetShouldClose(close bool) {
	w.glfw.SetShouldClose(close)
}

// SwapBuffers swaps the front and back buffers
func (w *Window) SwapBuffers() {
	w.glfw.SwapBuffers()
}

// Clear clears the screen with the given color
func (w *Window) Clear(r, g, b, a float32) {
	gl.ClearColor(r, g, b, a)
	gl.Clear(gl.COLOR_BUFFER_BIT)
}

// SetViewport sets the OpenGL viewport
func (w *Window) SetViewport(width, height int) {
	gl.Viewport(0, 0, int32(width), int32(height))
}

// ToggleFullscreen toggles between fullscreen and windowed mode.
//
// The current state is asked of the OS every time rather than kept in a bool.
// The user can enter or leave fullscreen without going through this function —
// on macOS via the green button, Cmd+Ctrl+F or a Space swipe, on Linux via the
// window manager — and a flag that says "windowed" for a window that is really
// fullscreen makes the next toggle save the *fullscreen* rect as the windowed
// geometry. After that, leaving fullscreen restores a full-screen-sized
// "window", permanently, which is the resize inconsistency this replaced.
func (w *Window) ToggleFullscreen() {
	toggleFullscreen(w)
}

// IsFullscreen returns whether the window is currently fullscreen.
func (w *Window) IsFullscreen() bool {
	return isFullscreen(w)
}

// loadIcon attempts to load and set the application icon
func (w *Window) loadIcon() {
	icons := assets.LoadMultiSizeIcons()
	if len(icons) > 0 {
		w.glfw.SetIcon(icons)
	}
}

// SetIcon sets the window icon from the provided images
func (w *Window) SetIcon(icons []image.Image) {
	if len(icons) > 0 {
		w.glfw.SetIcon(icons)
	}
}

// Destroy releases this window. It deliberately does not terminate GLFW —
// other windows may still be open; see Terminate.
func (w *Window) Destroy() {
	w.glfw.Destroy()
}

// MakeContextCurrent binds this window's GL context to the calling thread.
// With more than one window open, every renderer call must be preceded by this
// or the draws land in whichever context happened to be current.
func (w *Window) MakeContextCurrent() {
	w.glfw.MakeContextCurrent()
}

// PollEvents processes pending events
func PollEvents() {
	glfw.PollEvents()
}

// WaitEventsTimeout blocks until an event arrives or the timeout (in seconds)
// elapses, then processes pending events. This makes the main loop event-driven:
// keystrokes and mouse input wake it immediately, while the timeout bounds how long
// it sleeps when idle (for cursor blink, toasts, etc.).
func WaitEventsTimeout(seconds float64) {
	glfw.WaitEventsTimeout(seconds)
}

// PostEmptyEvent wakes a thread blocked in WaitEventsTimeout. It is safe to call
// from any goroutine (e.g. the PTY reader) to request a prompt re-render.
func PostEmptyEvent() {
	glfw.PostEmptyEvent()
}
