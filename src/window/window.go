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

	// Load and set application icon
	w.loadIcon()

	return w, nil
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

		// Enter fullscreen on primary monitor
		monitor := glfw.GetPrimaryMonitor()
		mode := monitor.GetVideoMode()
		w.glfw.SetMonitor(monitor, 0, 0, mode.Width, mode.Height, mode.RefreshRate)
		w.isFullscreen = true
	}
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
