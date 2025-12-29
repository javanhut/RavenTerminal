package main

import (
	"log"
	"math"
	"os"
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

type mouseSelection struct {
	active   bool
	pane     *tab.Pane
	startCol int
	startRow int
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

	debugMenu := os.Getenv("RAVEN_DEBUG_MENU") == "1"

	// Set up input callbacks
	var currentMods glfw.ModifierKey
	cursorVisible := true
	lastBlink := time.Now()
	blinkInterval := 500 * time.Millisecond
	lineBuf := &lineBuffer{}
	showHelp := false
	resizeMode := false
	const resizeStep = 0.05
	selection := &mouseSelection{}
	settingsMenu := menu.NewMenu()
	currentTheme := ""
	if settingsMenu.Config != nil {
		currentTheme = settingsMenu.Config.Theme
		renderer.SetThemeByName(currentTheme)
	}

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
				if action == glfw.Repeat {
					if debugMenu {
						log.Printf("menu: key repeat ignored key=%v input=%v title=%s", key, settingsMenu.InputMode(), settingsMenu.GetTitle())
					}
					return
				}
				if settingsMenu.InputMode() && settingsMenu.InputIsMultiline() && mods&glfw.ModControl == 0 {
					settingsMenu.HandleChar('\n')
					return
				}
				if debugMenu {
					log.Printf("menu: key enter key=%v input=%v title=%s", key, settingsMenu.InputMode(), settingsMenu.GetTitle())
				}
				if settingsMenu.InputMode() {
					settingsMenu.HandleEnter()
				} else {
					settingsMenu.Select()
				}
				return
			case glfw.KeyEscape:
				if debugMenu {
					log.Printf("menu: key escape input=%v title=%s", settingsMenu.InputMode(), settingsMenu.GetTitle())
				}
				settingsMenu.HandleEscape()
				return
			case glfw.KeyBackspace:
				if settingsMenu.InputMode() {
					settingsMenu.HandleBackspace()
				}
				return
			case glfw.KeyDelete:
				settingsMenu.HandleDelete()
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

		if resizeMode {
			switch key {
			case glfw.KeyUp:
				activeTab.ResizeActivePane(tab.ResizeUp, resizeStep)
				return
			case glfw.KeyDown:
				activeTab.ResizeActivePane(tab.ResizeDown, resizeStep)
				return
			case glfw.KeyLeft:
				activeTab.ResizeActivePane(tab.ResizeLeft, resizeStep)
				return
			case glfw.KeyRight:
				activeTab.ResizeActivePane(tab.ResizeRight, resizeStep)
				return
			case glfw.KeyEscape:
				resizeMode = false
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
		case keybindings.ActionCopy:
			text := activeTab.Terminal.Grid.VisibleText()
			if text != "" {
				glfw.SetClipboardString(text)
			}
		case keybindings.ActionPaste:
			clip := glfw.GetClipboardString()
			if clip != "" {
				clip = strings.ReplaceAll(clip, "\r\n", "\n")
				clip = strings.ReplaceAll(clip, "\n", "\r")
				activeTab.Write([]byte(clip))
				activeTab.Terminal.Grid.ResetScrollOffset()
			}
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
		case keybindings.ActionToggleResizeMode:
			resizeMode = !resizeMode
		}
	})

	win.GLFW().SetCharCallback(func(w *glfw.Window, char rune) {
		// Handle character input for settings menu
		if settingsMenu.IsOpen() && settingsMenu.InputMode() {
			settingsMenu.HandleChar(char)
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
		if settingsMenu.IsOpen() {
			if settingsMenu.InputMode() {
				return
			}
			if debugMenu {
				log.Printf("menu: scroll yoff=%.2f input=%v title=%s", yoff, settingsMenu.InputMode(), settingsMenu.GetTitle())
			}
			steps := int(math.Abs(yoff))
			if steps == 0 {
				steps = 1
			}
			for i := 0; i < steps; i++ {
				if yoff > 0 {
					settingsMenu.MoveUp()
				} else if yoff < 0 {
					settingsMenu.MoveDown()
				}
			}
			return
		}

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

	win.GLFW().SetMouseButtonCallback(func(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		if settingsMenu.IsOpen() || showHelp {
			return
		}

		activeTab := tabManager.ActiveTab()
		if activeTab == nil {
			return
		}

		width, height := win.GetFramebufferSize()
		x, y := w.GetCursorPos()

		switch button {
		case glfw.MouseButtonLeft:
			switch action {
			case glfw.Press:
				pane, col, row, ok := renderer.HitTestPane(activeTab, x, y, width, height)
				if !ok || pane == nil {
					if selection.pane != nil {
						selection.pane.Terminal.Grid.ClearSelection()
					}
					selection.active = false
					selection.pane = nil
					return
				}

				if selection.pane != nil && selection.pane != pane {
					selection.pane.Terminal.Grid.ClearSelection()
				}

				selection.active = true
				selection.pane = pane
				selection.startCol = col
				selection.startRow = row
				pane.Terminal.Grid.SetSelection(col, row, col, row)
				activeTab.SetActivePane(pane)
			case glfw.Release:
				if !selection.active || selection.pane == nil {
					return
				}

				pane := selection.pane
				rectX, rectY, rectW, rectH, ok := renderer.PaneRectFor(activeTab, pane, width, height)
				if !ok {
					selection.active = false
					return
				}

				fx := float32(x)
				fy := float32(y)
				if fx < rectX {
					fx = rectX
				} else if fx >= rectX+rectW {
					fx = rectX + rectW - 1
				}
				if fy < rectY {
					fy = rectY
				} else if fy >= rectY+rectH {
					fy = rectY + rectH - 1
				}

				cellW, cellH := renderer.CellSize()
				col := int((fx - rectX) / cellW)
				row := int((fy - rectY) / cellH)
				g := pane.Terminal.Grid
				col = clampInt(col, 0, g.Cols-1)
				row = clampInt(row, 0, g.Rows-1)

				if selection.startCol == col && selection.startRow == row {
					g.ClearSelection()
					selection.active = false
					return
				}

				g.SetSelection(selection.startCol, selection.startRow, col, row)
				if text := g.SelectedText(); text != "" {
					glfw.SetClipboardString(text)
				}

				selection.active = false
			}
		case glfw.MouseButtonRight:
			if action != glfw.Press {
				return
			}
			pane, _, _, ok := renderer.HitTestPane(activeTab, x, y, width, height)
			if !ok || pane == nil {
				return
			}

			activeTab.SetActivePane(pane)
			g := pane.Terminal.Grid

			if g.HasSelection() {
				if text := g.SelectedText(); text != "" {
					glfw.SetClipboardString(text)
				}
				return
			}

			clip := glfw.GetClipboardString()
			if clip != "" {
				clip = strings.ReplaceAll(clip, "\r\n", "\n")
				clip = strings.ReplaceAll(clip, "\n", "\r")
				pane.Write([]byte(clip))
				g.ResetScrollOffset()
			}
		}
	})

	win.GLFW().SetCursorPosCallback(func(w *glfw.Window, xpos, ypos float64) {
		if !selection.active || selection.pane == nil {
			return
		}
		if settingsMenu.IsOpen() || showHelp {
			return
		}

		activeTab := tabManager.ActiveTab()
		if activeTab == nil {
			return
		}

		width, height := win.GetFramebufferSize()
		rectX, rectY, rectW, rectH, ok := renderer.PaneRectFor(activeTab, selection.pane, width, height)
		if !ok {
			return
		}

		fx := float32(xpos)
		fy := float32(ypos)
		if fx < rectX {
			fx = rectX
		} else if fx >= rectX+rectW {
			fx = rectX + rectW - 1
		}
		if fy < rectY {
			fy = rectY
		} else if fy >= rectY+rectH {
			fy = rectY + rectH - 1
		}

		cellW, cellH := renderer.CellSize()
		col := int((fx - rectX) / cellW)
		row := int((fy - rectY) / cellH)
		g := selection.pane.Terminal.Grid
		col = clampInt(col, 0, g.Cols-1)
		row = clampInt(row, 0, g.Rows-1)

		g.SetSelection(selection.startCol, selection.startRow, col, row)
	})

	// Main loop
	for !win.ShouldClose() {
		// Check for exited tabs
		tabManager.CleanupExited()
		if tabManager.AllExited() {
			break
		}

		if settingsMenu.Config != nil && settingsMenu.Config.Theme != currentTheme {
			renderer.SetThemeByName(settingsMenu.Config.Theme)
			currentTheme = settingsMenu.Config.Theme
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

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
