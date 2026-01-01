package main

import (
	"log"
	"math"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/javanhut/RavenTerminal/commands"
	"github.com/javanhut/RavenTerminal/config"
	"github.com/javanhut/RavenTerminal/grid"
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

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type mouseSelection struct {
	active   bool
	pane     *tab.Pane
	startCol int
	startRow int
}

type toastState struct {
	message   string
	expiresAt time.Time
}

func main() {
	// Create window
	winConfig := window.DefaultConfig()
	win, err := window.NewWindow(winConfig)
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
	toast := &toastState{}
	showToast := func(message string) {
		if strings.TrimSpace(message) == "" {
			return
		}
		toast.message = message
		toast.expiresAt = time.Now().Add(900 * time.Millisecond)
	}
	settingsMenu := menu.NewMenu()
	settingsMenu.OnConfigReload = func(cfg *config.Config) error {
		if cfg == nil {
			return nil
		}
		renderer.SetThemeByName(cfg.Theme)
		if err := renderer.SetDefaultFontSize(cfg.FontSize); err != nil {
			return err
		}
		width, height := win.GetFramebufferSize()
		cols, rows := renderer.CalculateGridSize(width, height)
		tabManager.ResizeAll(uint16(cols), uint16(rows))
		return nil
	}
	settingsMenu.OnInitScriptUpdated = func(initPath string) error {
		if initPath == "" {
			return nil
		}
		activeTab := tabManager.ActiveTab()
		if activeTab == nil {
			return nil
		}
		cmd := ". " + shellQuote(initPath) + "\n"
		return activeTab.Write([]byte(cmd))
	}
	currentTheme := ""
	if settingsMenu.Config != nil {
		currentTheme = settingsMenu.Config.Theme
		renderer.SetThemeByName(currentTheme)
		if err := renderer.SetDefaultFontSize(settingsMenu.Config.FontSize); err == nil {
			width, height := win.GetFramebufferSize()
			cols, rows := renderer.CalculateGridSize(width, height)
			tabManager.ResizeAll(uint16(cols), uint16(rows))
		}
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
			appCursor := activeTab.Terminal.AppCursorKeys()
			result := keybindings.TranslateKey(key, mods, appCursor)
			if result.Action == keybindings.ActionPaste && settingsMenu.InputMode() {
				clip := glfw.GetClipboardString()
				if clip != "" {
					settingsMenu.HandlePaste(clip)
					showToast("Pasted from clipboard")
				}
				return
			}
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
			g := activeTab.Terminal.Grid
			text := g.SelectedText()
			if text == "" {
				text = g.VisibleText()
			}
			if text != "" {
				glfw.SetClipboardString(text)
				showToast("Copied to clipboard")
			}
		case keybindings.ActionPaste:
			clip := glfw.GetClipboardString()
			if clip != "" {
				clip = strings.ReplaceAll(clip, "\r\n", "\n")
				clip = strings.ReplaceAll(clip, "\n", "\r")
				activeTab.Write([]byte(clip))
				activeTab.Terminal.Grid.ResetScrollOffset()
				showToast("Pasted from clipboard")
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

				if mods&glfw.ModControl != 0 {
					if urlText, _, _ := urlAtCellRange(pane.Terminal.Grid, col, row); urlText != "" {
						if err := openURL(urlText); err != nil {
							log.Printf("failed to open url %q: %v", urlText, err)
						}
						return
					}
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
					showToast("Copied to clipboard")
				}

				selection.active = false
			}
		case glfw.MouseButtonRight:
			if action != glfw.Press {
				return
			}
			pane, col, row, ok := renderer.HitTestPane(activeTab, x, y, width, height)
			if !ok || pane == nil {
				return
			}

			activeTab.SetActivePane(pane)
			g := pane.Terminal.Grid

			if mods&glfw.ModControl != 0 {
				if urlText, _, _ := urlAtCellRange(g, col, row); urlText != "" {
					if err := openURL(urlText); err != nil {
						log.Printf("failed to open url %q: %v", urlText, err)
					}
					return
				}
			}

			if g.HasSelection() {
				if text := g.SelectedText(); text != "" {
					glfw.SetClipboardString(text)
					showToast("Copied to clipboard")
				}
				return
			}

			clip := glfw.GetClipboardString()
			if clip != "" {
				clip = strings.ReplaceAll(clip, "\r\n", "\n")
				clip = strings.ReplaceAll(clip, "\n", "\r")
				pane.Write([]byte(clip))
				g.ResetScrollOffset()
				showToast("Pasted from clipboard")
			}
		}
	})

	win.GLFW().SetCursorPosCallback(func(w *glfw.Window, xpos, ypos float64) {
		if settingsMenu.IsOpen() || showHelp {
			renderer.ClearHoverURL()
			return
		}

		activeTab := tabManager.ActiveTab()
		if activeTab == nil {
			renderer.ClearHoverURL()
			return
		}

		if selection.active && selection.pane != nil {
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
			renderer.ClearHoverURL()
			return
		}

		width, height := win.GetFramebufferSize()
		pane, col, row, ok := renderer.HitTestPane(activeTab, xpos, ypos, width, height)
		if !ok || pane == nil {
			renderer.ClearHoverURL()
			return
		}

		if _, startCol, endCol := urlAtCellRange(pane.Terminal.Grid, col, row); startCol <= endCol {
			renderer.SetHoverURL(pane.Terminal.Grid, row, startCol, endCol)
			return
		}
		renderer.ClearHoverURL()
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
		if now.Before(toast.expiresAt) {
			renderer.DrawToast(toast.message, width, height)
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

func urlAtCell(g *grid.Grid, col, row int) string {
	urlText, _, _ := urlAtCellRange(g, col, row)
	return urlText
}

func urlAtCellRange(g *grid.Grid, col, row int) (string, int, int) {
	if g == nil || row < 0 || row >= g.Rows || col < 0 || col >= g.Cols {
		return "", -1, -1
	}

	line := make([]rune, g.Cols)
	for c := 0; c < g.Cols; c++ {
		cell := g.DisplayCell(c, row)
		ch := cell.Char
		if ch == 0 {
			ch = ' '
		}
		line[c] = ch
	}

	if line[col] == ' ' {
		return "", -1, -1
	}

	start := col
	for start > 0 && line[start-1] != ' ' {
		start--
	}
	end := col
	for end+1 < len(line) && line[end+1] != ' ' {
		end++
	}

	trimLeftChars := "<>\"'()[]{}"
	trimRightChars := "<>\"'()[]{}.,;:!?"
	for start <= end && strings.ContainsRune(trimLeftChars, line[start]) {
		start++
	}
	for end >= start && strings.ContainsRune(trimRightChars, line[end]) {
		end--
	}
	if start > end {
		return "", -1, -1
	}

	display := string(line[start : end+1])
	target := display
	if strings.HasPrefix(target, "www.") {
		target = "http://" + target
	}
	if !strings.Contains(target, "://") {
		return "", -1, -1
	}

	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", -1, -1
	}

	return target, start, end
}

func openURL(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
