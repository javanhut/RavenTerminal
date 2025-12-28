package main

import (
	"log"
	"strings"
	"time"

	"github.com/javanhut/RavenTerminal/commands"
	"github.com/javanhut/RavenTerminal/keybindings"
	"github.com/javanhut/RavenTerminal/menu"
	"github.com/javanhut/RavenTerminal/render"
	"github.com/javanhut/RavenTerminal/tab"
	"github.com/javanhut/RavenTerminal/window"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// lineBuffer tracks the current line being typed for command interception
type lineBuffer struct {
	buffer strings.Builder
}

func (lb *lineBuffer) addChar(c rune) {
	lb.buffer.WriteRune(c)
}

func (lb *lineBuffer) addBytes(data []byte) {
	lb.buffer.Write(data)
}

func (lb *lineBuffer) backspace() {
	s := lb.buffer.String()
	if len(s) > 0 {
		// Remove last rune
		runes := []rune(s)
		lb.buffer.Reset()
		lb.buffer.WriteString(string(runes[:len(runes)-1]))
	}
}

func (lb *lineBuffer) clear() {
	lb.buffer.Reset()
}

func (lb *lineBuffer) getLine() string {
	return lb.buffer.String()
}

func main() {
	// Create window
	config := window.DefaultConfig()
	win, err := window.NewWindow(config)
	if err != nil {
		log.Fatalf("Failed to create window: %v", err)
	}
	defer win.Destroy()

	// Create renderer
	renderer, err := render.NewRenderer()
	if err != nil {
		log.Fatalf("Failed to create renderer: %v", err)
	}
	defer renderer.Destroy()

	// Calculate initial grid size
	width, height := win.GetFramebufferSize()
	cols, rows := renderer.CalculateGridSize(width, height)

	// Create tab manager
	tabManager, err := tab.NewTabManager(uint16(cols), uint16(rows))
	if err != nil {
		log.Fatalf("Failed to create tab manager: %v", err)
	}

	// Set up input callbacks
	var currentMods glfw.ModifierKey
	cursorVisible := true
	lastBlink := time.Now()
	blinkInterval := 500 * time.Millisecond
	lineBuf := &lineBuffer{}
	showHelp := false
	settingsMenu := menu.NewMenu()

	win.GLFW().SetKeyCallback(func(w *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
		if action == glfw.Release {
			return
		}

		currentMods = mods
		activeTab := tabManager.ActiveTab()
		if activeTab == nil {
			return
		}

		// Handle settings menu input when open
		if settingsMenu.IsOpen() {
			switch key {
			case glfw.KeyUp:
				settingsMenu.MoveUp()
				return
			case glfw.KeyDown:
				settingsMenu.MoveDown()
				return
			case glfw.KeyEnter, glfw.KeyKPEnter:
				if settingsMenu.InputMode {
					settingsMenu.HandleInputEnter()
				} else {
					settingsMenu.Select()
				}
				return
			case glfw.KeyEscape:
				settingsMenu.HandleInputEscape()
				return
			case glfw.KeyBackspace:
				if settingsMenu.InputMode {
					settingsMenu.HandleInputBackspace()
				}
				return
			case glfw.KeyDelete:
				settingsMenu.DeleteSelected()
				return
			}
			return
		}

		// Handle help panel scrolling with arrow keys when help is open
		if showHelp {
			switch key {
			case glfw.KeyUp:
				renderer.ScrollHelpUp()
				return
			case glfw.KeyDown:
				renderer.ScrollHelpDown()
				return
			case glfw.KeyPageUp:
				for i := 0; i < 5; i++ {
					renderer.ScrollHelpUp()
				}
				return
			case glfw.KeyPageDown:
				for i := 0; i < 5; i++ {
					renderer.ScrollHelpDown()
				}
				return
			case glfw.KeyHome:
				renderer.ResetHelpScroll()
				return
			case glfw.KeyEscape:
				showHelp = false
				renderer.ResetHelpScroll()
				return
			}
		}

		appCursor := activeTab.Terminal.AppCursorKeys()
		result := keybindings.TranslateKey(key, mods, appCursor)

		switch result.Action {
		case keybindings.ActionExit:
			win.SetShouldClose(true)
		case keybindings.ActionInput:
			// Don't process input when help is shown (except for closing it)
			if showHelp {
				return
			}
			// Check for Enter key (carriage return)
			if len(result.Data) == 1 && result.Data[0] == '\r' {
				line := lineBuf.getLine()
				cmdResult := commands.HandleCommand(line, renderer)
				if cmdResult.Handled {
					// Echo the command (so it appears in terminal)
					activeTab.Write([]byte("\r\n"))
					// Display command output
					output := strings.ReplaceAll(cmdResult.Output, "\n", "\r\n")
					activeTab.Terminal.Process([]byte(output))
					lineBuf.clear()
					return
				}
				lineBuf.clear()
			}
			// Check for backspace
			if len(result.Data) == 1 && result.Data[0] == 0x7f {
				lineBuf.backspace()
			}
			// Check for Ctrl+C or Ctrl+U (line clear)
			if len(result.Data) == 1 && (result.Data[0] == 0x03 || result.Data[0] == 0x15) {
				lineBuf.clear()
			}
			activeTab.Write(result.Data)
			activeTab.Terminal.Grid.ResetScrollOffset()
		case keybindings.ActionScrollUp:
			activeTab.Terminal.Grid.ScrollViewUp(5)
		case keybindings.ActionScrollDown:
			activeTab.Terminal.Grid.ScrollViewDown(5)
		case keybindings.ActionScrollUpLine:
			activeTab.Terminal.Grid.ScrollViewUp(1)
		case keybindings.ActionScrollDownLine:
			activeTab.Terminal.Grid.ScrollViewDown(1)
		case keybindings.ActionToggleFullscreen:
			win.ToggleFullscreen()
		case keybindings.ActionNewTab:
			lineBuf.clear()
			tabManager.NewTab()
		case keybindings.ActionCloseTab:
			tabManager.CloseCurrentTab()
		case keybindings.ActionNextTab:
			lineBuf.clear()
			tabManager.NextTab()
		case keybindings.ActionPrevTab:
			lineBuf.clear()
			tabManager.PrevTab()
		case keybindings.ActionSplitVertical:
			lineBuf.clear()
			activeTab.SplitVertical()
		case keybindings.ActionSplitHorizontal:
			lineBuf.clear()
			activeTab.SplitHorizontal()
		case keybindings.ActionClosePane:
			lineBuf.clear()
			activeTab.ClosePane()
		case keybindings.ActionNextPane:
			lineBuf.clear()
			activeTab.NextPane()
		case keybindings.ActionPrevPane:
			lineBuf.clear()
			activeTab.PrevPane()
		case keybindings.ActionShowHelp:
			showHelp = !showHelp
			if !showHelp {
				renderer.ResetHelpScroll()
			}
		case keybindings.ActionZoomIn:
			if err := renderer.ZoomIn(); err == nil {
				// Recalculate grid size after zoom
				width, height := win.GetFramebufferSize()
				cols, rows := renderer.CalculateGridSize(width, height)
				tabManager.ResizeAll(uint16(cols), uint16(rows))
			}
		case keybindings.ActionZoomOut:
			if err := renderer.ZoomOut(); err == nil {
				// Recalculate grid size after zoom
				width, height := win.GetFramebufferSize()
				cols, rows := renderer.CalculateGridSize(width, height)
				tabManager.ResizeAll(uint16(cols), uint16(rows))
			}
		case keybindings.ActionZoomReset:
			if err := renderer.ZoomReset(); err == nil {
				// Recalculate grid size after zoom
				width, height := win.GetFramebufferSize()
				cols, rows := renderer.CalculateGridSize(width, height)
				tabManager.ResizeAll(uint16(cols), uint16(rows))
			}
		case keybindings.ActionOpenMenu:
			if settingsMenu.IsOpen() {
				settingsMenu.Close()
			} else {
				settingsMenu.Open()
			}
		}
	})

	win.GLFW().SetCharCallback(func(w *glfw.Window, char rune) {
		// Handle character input for settings menu
		if settingsMenu.IsOpen() && settingsMenu.InputMode {
			settingsMenu.HandleInputChar(char)
			return
		}

		// Don't process char input when help or menu is shown
		if showHelp || settingsMenu.IsOpen() {
			return
		}

		activeTab := tabManager.ActiveTab()
		if activeTab == nil {
			return
		}

		// Add character to line buffer
		lineBuf.addChar(char)

		data := keybindings.TranslateChar(char, currentMods)
		activeTab.Write(data)
		activeTab.Terminal.Grid.ResetScrollOffset()
	})

	win.GLFW().SetFramebufferSizeCallback(func(w *glfw.Window, width, height int) {
		win.SetViewport(width, height)
		cols, rows := renderer.CalculateGridSize(width, height)
		tabManager.ResizeAll(uint16(cols), uint16(rows))
	})

	win.GLFW().SetScrollCallback(func(w *glfw.Window, xoff, yoff float64) {
		activeTab := tabManager.ActiveTab()
		if activeTab == nil {
			return
		}
		if yoff > 0 {
			activeTab.Terminal.Grid.ScrollViewUp(3)
		} else if yoff < 0 {
			activeTab.Terminal.Grid.ScrollViewDown(3)
		}
	})

	// Main loop
	for !win.ShouldClose() {
		// Check for exited tabs
		tabManager.CleanupExited()
		if tabManager.AllExited() {
			break
		}

		// Handle cursor blinking
		now := time.Now()
		if now.Sub(lastBlink) >= blinkInterval {
			cursorVisible = !cursorVisible
			lastBlink = now
		}

		// Render
		width, height := win.GetFramebufferSize()
		win.SetViewport(width, height)
		if settingsMenu.IsOpen() {
			renderer.RenderWithMenu(tabManager, width, height, cursorVisible, settingsMenu)
		} else {
			renderer.RenderWithHelp(tabManager, width, height, cursorVisible, showHelp)
		}

		// Swap buffers and poll events
		win.SwapBuffers()
		window.PollEvents()

		// Small sleep to prevent 100% CPU usage
		time.Sleep(time.Millisecond * 16) // ~60 FPS
	}
}
