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

// Window wraps a GLFW window with OpenGL context
type Window struct {
	glfw         *glfw.Window
	width        int
	height       int
	config       Config
	isFullscreen bool
	savedX       int
	savedY       int
	savedWidth   int
	savedHeight  int
	contentScale float32 // HiDPI scale (framebuffer px per logical point)
}

// NewWindow creates a new GLFW window with OpenGL context
func NewWindow(config Config) (*Window, error) {
	if err := glfw.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize GLFW: %w", err)
	}

	// OpenGL context hints
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Resizable, glfw.True)
	glfw.WindowHint(glfw.DoubleBuffer, glfw.True)

	// Set X11 window class for proper WM integration (Hyprland, i3, etc.)
	glfw.WindowHintString(glfw.X11ClassName, "raven-terminal")
	glfw.WindowHintString(glfw.X11InstanceName, "raven-terminal")

	window, err := glfw.CreateWindow(config.Width, config.Height, config.Title, nil, nil)
	if err != nil {
		glfw.Terminate()
		return nil, fmt.Errorf("failed to create window: %w", err)
	}

	window.MakeContextCurrent()

	// Enforce a hard minimum window size so the grid can never collapse below
	// the renderer's usable floor (see MinWindowWidth/Height docs). GLFW clamps
	// user-driven resizes and programmatic SetSize calls to these limits.
	window.SetSizeLimits(MinWindowWidth, MinWindowHeight, glfw.DontCare, glfw.DontCare)

	// Initialize OpenGL
	if err := gl.Init(); err != nil {
		window.Destroy()
		glfw.Terminate()
		return nil, fmt.Errorf("failed to initialize OpenGL: %w", err)
	}

	// Enable VSync
	glfw.SwapInterval(1)

	// Enable blending for text rendering
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)

	w := &Window{
		glfw:   window,
		width:  config.Width,
		height: config.Height,
		config: config,
	}

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

// ToggleFullscreen toggles between fullscreen and windowed mode
func (w *Window) ToggleFullscreen() {
	if w.isFullscreen {
		// Restore windowed mode
		w.glfw.SetMonitor(nil, w.savedX, w.savedY, w.savedWidth, w.savedHeight, 0)
		w.isFullscreen = false
	} else {
		// Save current window position and size
		w.savedX, w.savedY = w.glfw.GetPos()
		w.savedWidth, w.savedHeight = w.glfw.GetSize()

		// Enter fullscreen on the monitor the window is on
		monitor := w.currentMonitor()
		mode := monitor.GetVideoMode()
		w.glfw.SetMonitor(monitor, 0, 0, mode.Width, mode.Height, mode.RefreshRate)
		w.isFullscreen = true
	}
}

// currentMonitor returns the monitor containing the largest portion of the
// window, falling back to the primary monitor. GLFW only exposes a window's
// monitor while it is fullscreen, so for windowed mode we compute the overlap
// between the window rect and each monitor's work area ourselves.
func (w *Window) currentMonitor() *glfw.Monitor {
	wx, wy := w.glfw.GetPos()
	ww, wh := w.glfw.GetSize()
	best := glfw.GetPrimaryMonitor()
	bestArea := 0
	for _, m := range glfw.GetMonitors() {
		mode := m.GetVideoMode()
		if mode == nil {
			continue
		}
		mx, my := m.GetPos()
		overlapW := min(wx+ww, mx+mode.Width) - max(wx, mx)
		overlapH := min(wy+wh, my+mode.Height) - max(wy, my)
		if overlapW > 0 && overlapH > 0 && overlapW*overlapH > bestArea {
			bestArea = overlapW * overlapH
			best = m
		}
	}
	return best
}

// IsFullscreen returns whether the window is in fullscreen mode
func (w *Window) IsFullscreen() bool {
	return w.isFullscreen
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

// Destroy cleans up window resources
func (w *Window) Destroy() {
	w.glfw.Destroy()
	glfw.Terminate()
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
