package main

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/javanhut/RavenTerminal/src/aipanel"
	"github.com/javanhut/RavenTerminal/src/commands"
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/keybindings"
	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/searchpanel"
	"github.com/javanhut/RavenTerminal/src/tab"

	"github.com/go-gl/glfw/v3.3/glfw"
)

const resizeStep = 0.05

// clampedPaneCell maps window coordinates to a cell of a specific pane,
// clamping positions outside the pane rect to its nearest edge cell.
func (a *App) clampedPaneCell(activeTab *tab.Tab, pane *tab.Pane, x, y float64) (int, int, bool) {
	width, height := a.win.GetFramebufferSize()
	rectX, rectY, rectW, rectH, ok := a.renderer.PaneRectFor(activeTab, pane, width, height)
	if !ok {
		return 0, 0, false
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
	cellW, cellH := a.renderer.CellSize()
	g := pane.Terminal.GetGrid()
	col := clampInt(int((fx-rectX)/cellW), 0, g.Cols-1)
	row := clampInt(int((fy-rectY)/cellH), 0, g.Rows-1)
	return col, row, true
}

// reportMousePress forwards a button press to the pane's application when a
// mouse tracking mode is active (and shift is not held). Returns true when
// the event was consumed.
func (a *App) reportMousePress(pane *tab.Pane, btn, col, row int, shift bool) bool {
	act := parser.DecideMouse(mouseCtxFor(pane, shift), parser.MousePress, btn, col, row, -1, false)
	if act.Kind != parser.MouseActionSend {
		return false
	}
	a.report.pane, a.report.held = pane, btn
	a.report.lastCol, a.report.lastRow = col, row
	pane.Write(act.Bytes)
	return true
}

func (a *App) showToast(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	a.toast.message = message
	a.toast.expiresAt = time.Now().Add(900 * time.Millisecond)
}

func (a *App) onKey(w *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
	if action == glfw.Release {
		return
	}

	a.currentMods = mods
	activeTab := a.tabManager.ActiveTab()
	if activeTab == nil {
		return
	}

	// Find bar owns the keyboard while it is up: it is a modal prompt, so
	// Enter/Escape/Backspace edit the search rather than reaching the shell.
	// Printable runes arrive via onChar. Checked before the panels so the bar
	// can be dismissed from anywhere.
	if a.find.open {
		switch key {
		case glfw.KeyEscape:
			a.closeFind()
			return
		case glfw.KeyEnter, glfw.KeyKPEnter:
			if mods&glfw.ModShift != 0 {
				a.findStep(-1)
			} else {
				a.findStep(1)
			}
			return
		case glfw.KeyBackspace:
			if q := []rune(a.find.query); len(q) > 0 {
				a.find.query = string(q[:len(q)-1])
				a.runFind()
			}
			return
		case glfw.KeyUp:
			a.findStep(-1)
			return
		case glfw.KeyDown:
			a.findStep(1)
			return
		}
		// Super+Shift+F again closes the bar, matching the toggle it opened with.
		if keybindings.TranslateKey(key, mods, false).Action == keybindings.ActionFindInScrollback {
			a.closeFind()
			return
		}
		// Everything else (app shortcuts, plain modifiers) falls through so
		// e.g. Cmd+C still copies the highlighted match.
	}

	// Handle settings menu input when open
	if a.settingsMenu.IsOpen() {
		appCursor := activeTab.Terminal.AppCursorKeys()
		result := keybindings.TranslateKey(key, mods, appCursor)
		if result.Action == keybindings.ActionPaste && a.settingsMenu.InputMode() {
			clip := glfw.GetClipboardString()
			if clip != "" {
				a.settingsMenu.HandlePaste(clip)
				a.showToast("Pasted from clipboard")
			}
			return
		}
		switch key {
		case glfw.KeyUp:
			a.settingsMenu.MoveUp()
			return
		case glfw.KeyDown:
			a.settingsMenu.MoveDown()
			return
		case glfw.KeyEnter, glfw.KeyKPEnter:
			if action == glfw.Repeat {
				if a.debugMenu {
					log.Printf("menu: key repeat ignored key=%v input=%v title=%s", key, a.settingsMenu.InputMode(), a.settingsMenu.GetTitle())
				}
				return
			}
			if a.settingsMenu.InputMode() && a.settingsMenu.InputIsMultiline() && mods&glfw.ModControl == 0 {
				a.settingsMenu.HandleChar('\n')
				return
			}
			if a.debugMenu {
				log.Printf("menu: key enter key=%v input=%v title=%s", key, a.settingsMenu.InputMode(), a.settingsMenu.GetTitle())
			}
			if a.settingsMenu.InputMode() {
				a.settingsMenu.HandleEnter()
			} else {
				a.settingsMenu.Select()
			}
			return
		case glfw.KeyEscape:
			if a.debugMenu {
				log.Printf("menu: key escape input=%v title=%s", a.settingsMenu.InputMode(), a.settingsMenu.GetTitle())
			}
			a.settingsMenu.HandleEscape()
			return
		case glfw.KeyBackspace:
			if a.settingsMenu.InputMode() {
				a.settingsMenu.HandleBackspace()
			}
			return
		case glfw.KeyDelete:
			a.settingsMenu.HandleDelete()
			return
		}
		return
	}

	// Handle AI panel focus and input
	if a.aiPanel.Open {
		appCursor := activeTab.Terminal.AppCursorKeys()
		result := keybindings.TranslateKey(key, mods, appCursor)
		if result.Action == keybindings.ActionNextPane || result.Action == keybindings.ActionPrevPane {
			if a.aiPanel.Focused {
				a.aiPanel.Focused = false
				if result.Action == keybindings.ActionNextPane {
					activeTab.NextPane()
				} else {
					activeTab.PrevPane()
				}
				a.showToast("Terminal focused")
			} else {
				a.aiPanel.Focused = true
				a.showToast("AI panel focused")
			}
			return
		}
		if result.Action == keybindings.ActionToggleAIPanel {
			a.aiPanel.Open = false
			return
		}
		if result.Action == keybindings.ActionToggleSearchPanel {
			a.aiPanel.Open = false
			if !a.searchPanel.Enabled {
				a.showToast("Enable web search in settings")
				return
			}
			a.searchPanel.Toggle()
			if a.searchPanel.Open {
				if a.settingsMenu.Config != nil {
					a.searchPanel.ProxyEnabled = a.settingsMenu.Config.WebSearch.UseReaderProxy
				}
				a.searchPanel.Focused = true
				a.showHelp = false
				a.renderer.ResetHelpScroll()
			}
			return
		}
		if !a.aiPanel.Focused {
			// Let terminal handle input while panel stays visible.
			goto handleTerminalInput
		}

		switch result.Action {
		case keybindings.ActionCopy:
			// In AI panel, copy the last assistant response
			lastResponse := a.aiPanel.GetLastAssistantMessage()
			if lastResponse != "" {
				glfw.SetClipboardString(lastResponse)
				a.showToast("Copied AI response")
			} else {
				a.showToast("No AI response to copy")
			}
			return
		case keybindings.ActionPaste:
			clip := glfw.GetClipboardString()
			if clip != "" {
				clip = strings.ReplaceAll(clip, "\r\n", "\n")
				clip = strings.ReplaceAll(clip, "\r", "\n")
				clip = strings.ReplaceAll(clip, "\n", " ")
				a.aiPanel.SetInput(a.aiPanel.Input + clip)
				a.showToast("Pasted into AI prompt")
			}
			return
		}

		width, height := a.win.GetFramebufferSize()
		cellW, cellH := a.renderer.CellDimensions()
		layout := a.aiPanel.Layout(width, height, cellW, cellH)
		maxChars := max(int(layout.ContentWidth/cellW)-2, 10)
		wrapped := a.aiPanel.WrappedForRender(maxChars)
		totalLines := len(wrapped)
		visibleLines := layout.VisibleLines
		maxScroll := max(totalLines-visibleLines, 0)
		if a.aiPanel.Scroll > maxScroll {
			a.aiPanel.Scroll = maxScroll
		}

		if action == glfw.Repeat && (key == glfw.KeyEnter || key == glfw.KeyKPEnter) {
			return
		}

		if mods&glfw.ModControl != 0 && key == glfw.KeyU {
			a.aiPanel.ClearInput()
			return
		}

		// Ctrl+T: toggle thinking expansion
		if mods&glfw.ModControl != 0 && key == glfw.KeyT {
			if aipanel.HasThinkingContent(a.aiPanel.Messages) {
				a.aiPanel.ToggleThinkingExpanded()
			}
			return
		}

		// Ctrl+Enter: send message (legacy chord, same as plain Enter)
		if mods&glfw.ModControl != 0 && (key == glfw.KeyEnter || key == glfw.KeyKPEnter) {
			if a.aiPanel.Loading {
				return
			}
			a.startAIChat(a.aiPanel.Input)
			return
		}

		switch key {
		case glfw.KeyEscape:
			// Close but keep the conversation; reopening restores it.
			a.aiPanel.Open = false
			return
		case glfw.KeyEnter, glfw.KeyKPEnter:
			if action == glfw.Repeat {
				return
			}
			// Shift+Enter inserts a newline; plain Enter sends.
			if mods&glfw.ModShift != 0 {
				a.aiPanel.AppendNewline()
				return
			}
			if a.aiPanel.Loading {
				return
			}
			a.startAIChat(a.aiPanel.Input)
			return
		case glfw.KeyUp:
			// Scroll input if multiline, otherwise scroll messages
			if len(a.aiPanel.InputLines) > layout.InputLines {
				a.aiPanel.ScrollInputUp()
			} else if a.aiPanel.Scroll > 0 {
				a.aiPanel.Scroll--
			}
			return
		case glfw.KeyDown:
			// Scroll input if multiline, otherwise scroll messages
			if len(a.aiPanel.InputLines) > layout.InputLines {
				a.aiPanel.ScrollInputDown(layout.InputLines)
			} else if a.aiPanel.Scroll < maxScroll {
				a.aiPanel.Scroll++
			}
			return
		case glfw.KeyPageUp:
			a.aiPanel.Scroll -= visibleLines
			if a.aiPanel.Scroll < 0 {
				a.aiPanel.Scroll = 0
			}
			return
		case glfw.KeyPageDown:
			a.aiPanel.Scroll += visibleLines
			if a.aiPanel.Scroll > maxScroll {
				a.aiPanel.Scroll = maxScroll
			}
			return
		case glfw.KeyHome:
			if mods&glfw.ModControl != 0 {
				a.aiPanel.Scroll = 0
			}
			return
		case glfw.KeyEnd:
			if mods&glfw.ModControl != 0 {
				a.aiPanel.Scroll = maxScroll
			}
			return
		case glfw.KeyBackspace:
			a.aiPanel.Backspace()
			return
		}
		return
	}

	// Handle search panel focus and input
	if a.searchPanel.Open {
		appCursor := activeTab.Terminal.AppCursorKeys()
		result := keybindings.TranslateKey(key, mods, appCursor)
		if result.Action == keybindings.ActionNextPane || result.Action == keybindings.ActionPrevPane {
			if a.searchPanel.Focused {
				a.searchPanel.Focused = false
				if result.Action == keybindings.ActionNextPane {
					activeTab.NextPane()
				} else {
					activeTab.PrevPane()
				}
				a.showToast("Terminal focused")
			} else {
				a.searchPanel.Focused = true
				a.showToast("Search panel focused")
			}
			return
		}
		if result.Action == keybindings.ActionToggleSearchPanel {
			a.searchPanel.Toggle()
			return
		}
		if result.Action == keybindings.ActionToggleAIPanel {
			a.searchPanel.Open = false
			if !a.aiPanel.Enabled {
				a.showToast("Enable Ollama chat in settings")
				return
			}
			a.aiPanel.Toggle()
			if a.aiPanel.Open {
				a.aiPanel.Focused = true
				a.showHelp = false
				a.renderer.ResetHelpScroll()
			}
			return
		}
		if !a.searchPanel.Focused {
			// Let terminal handle input while panel stays visible.
			goto handleTerminalInput
		}

		switch result.Action {
		case keybindings.ActionCopy:
			// No selection: leave the clipboard alone.
			if text := activeTab.Terminal.GetGrid().SelectedText(); text != "" {
				glfw.SetClipboardString(text)
				a.showToast("Copied to clipboard")
			} else {
				a.showToast("Nothing selected")
			}
			return
		case keybindings.ActionPaste:
			clip := glfw.GetClipboardString()
			if clip != "" {
				writePaste(activeTab.Terminal, activeTab, clip)
				activeTab.Terminal.GetGrid().ResetScrollOffset()
				a.showToast("Pasted from clipboard")
			}
			return
		}

		width, height := a.win.GetFramebufferSize()
		cellW, cellH := a.renderer.CellDimensions()
		layout := a.searchPanel.Layout(width, height, cellW, cellH)
		previewVisible := max(layout.VisibleLines-1, 1)
		previewTotal := len(a.searchPanel.PreviewLines)
		if len(a.searchPanel.PreviewWrapped) > 0 && a.searchPanel.PreviewWrapChars > 0 {
			previewTotal = len(a.searchPanel.PreviewWrapped)
		}
		if action == glfw.Repeat && (key == glfw.KeyEnter || key == glfw.KeyKPEnter) {
			return
		}
		if mods&glfw.ModControl != 0 && mods&glfw.ModShift != 0 && key == glfw.KeyR {
			a.searchPanel.ProxyEnabled = !a.searchPanel.ProxyEnabled
			if a.searchPanel.ProxyEnabled {
				a.searchPanel.Status = "Reader proxy enabled"
			} else {
				a.searchPanel.Status = "Reader proxy disabled"
			}
			if a.searchPanel.Mode == searchpanel.ModePreview && a.searchPanel.PreviewURL != "" {
				// A deliberate source switch must re-fetch, not replay
				// the cached copy for the new proxy state.
				a.previewCache.delete(previewCacheKey(a.searchPanel.ProxyEnabled, a.searchPanel.PreviewURL))
				a.startPreview(searchpanel.Result{
					Title: a.searchPanel.PreviewTitle,
					URL:   a.searchPanel.PreviewURL,
				})
			}
			return
		}

		if mods&glfw.ModControl != 0 && key == glfw.KeyU {
			// While editing the find query, Ctrl+U clears it, not the
			// search query (mirrors the find-aware Backspace).
			if a.searchPanel.FindEditing {
				a.searchPanel.FindClear()
				return
			}
			a.searchPanel.ClearQuery()
			return
		}

		// Ctrl+R: re-run the last search / re-fetch the current preview.
		// Retry must hit the network, so evict the cached copy first.
		if mods&glfw.ModControl != 0 && key == glfw.KeyR {
			if a.searchPanel.Mode == searchpanel.ModePreview && a.searchPanel.PreviewURL != "" {
				a.previewCache.delete(previewCacheKey(a.searchPanel.ProxyEnabled, a.searchPanel.PreviewURL))
				a.startPreview(searchpanel.Result{
					Title: a.searchPanel.PreviewTitle,
					URL:   a.searchPanel.PreviewURL,
				})
				return
			}
			q := a.searchPanel.Query
			if strings.TrimSpace(q) == "" {
				q = a.searchPanel.LastQuery
			}
			if strings.TrimSpace(q) != "" {
				a.searchCache.delete(strings.TrimSpace(q))
				a.startSearch(q)
			}
			return
		}

		// Ctrl+Left / Ctrl+Right: back/forward through previewed pages.
		// (Backspace is not used for back: in preview it still edits the
		// search query, which is existing behavior.)
		if mods&glfw.ModControl != 0 && key == glfw.KeyLeft && a.searchPanel.Mode == searchpanel.ModePreview {
			if entry, ok := a.searchPanel.NavBack(); ok {
				a.searchPanel.PendingScroll = entry.Scroll
				a.startPreview(searchpanel.Result{Title: entry.Title, URL: entry.URL})
			} else {
				// Back past the first page returns to the results list,
				// mirroring Esc.
				a.searchPanel.ExitFind()
				a.searchPanel.Mode = searchpanel.ModeResults
				a.searchPanel.PreviewScroll = 0
			}
			return
		}
		if mods&glfw.ModControl != 0 && key == glfw.KeyRight && a.searchPanel.Mode == searchpanel.ModePreview {
			if entry, ok := a.searchPanel.NavForward(); ok {
				a.searchPanel.PendingScroll = entry.Scroll
				a.startPreview(searchpanel.Result{Title: entry.Title, URL: entry.URL})
			}
			return
		}

		// Ctrl+O: Open selected URL in browser
		if mods&glfw.ModControl != 0 && key == glfw.KeyO {
			var urlToOpen string
			if a.searchPanel.Mode == searchpanel.ModePreview {
				urlToOpen = a.searchPanel.PreviewURL
			} else {
				urlToOpen = a.searchPanel.GetSelectedURL()
			}
			if urlToOpen != "" {
				if err := openURL(urlToOpen); err != nil {
					a.searchPanel.Status = "Failed to open browser"
				} else {
					a.searchPanel.Status = "Opening in browser..."
				}
			}
			return
		}

		// Ctrl+Y: copy the previewed / selected result URL to the clipboard
		if mods&glfw.ModControl != 0 && key == glfw.KeyY {
			urlToCopy := a.searchPanel.GetSelectedURL()
			if a.searchPanel.Mode == searchpanel.ModePreview {
				urlToCopy = a.searchPanel.PreviewURL
			}
			if urlToCopy != "" {
				glfw.SetClipboardString(urlToCopy)
				a.showToast("URL copied")
			}
			return
		}

		// Ctrl+I: insert the preview mouse selection into the shell
		if mods&glfw.ModControl != 0 && key == glfw.KeyI && a.searchPanel.Mode == searchpanel.ModePreview {
			if text := a.searchPanel.SelectedPreviewText(); strings.TrimSpace(text) != "" {
				writePaste(activeTab.Terminal, activeTab, text)
				a.showToast("Inserted into shell")
			}
			return
		}

		// Ctrl+A: send the previewed page (or the mouse selection) to the
		// AI panel for a summary.
		if mods&glfw.ModControl != 0 && key == glfw.KeyA && a.searchPanel.Mode == searchpanel.ModePreview {
			if !a.aiPanel.Enabled {
				a.showToast("Enable Ollama chat in settings")
				return
			}
			if a.aiPanel.Loading {
				return
			}
			text := strings.Join(a.searchPanel.PreviewLines, "\n")
			if sel := a.searchPanel.SelectedPreviewText(); strings.TrimSpace(sel) != "" {
				text = sel
			}
			// Cap like the fetch_page tool does, cutting on a rune boundary.
			const maxAIPageChars = 8000
			if len(text) > maxAIPageChars {
				cut := maxAIPageChars
				for cut > 0 && !utf8.RuneStart(text[cut]) {
					cut--
				}
				text = text[:cut]
			}
			if strings.TrimSpace(text) == "" {
				return
			}
			// Swap panels like Leader+A does, then reuse the normal chat path.
			a.searchPanel.Open = false
			a.aiPanel.Open = true
			a.aiPanel.Focused = true
			a.showHelp = false
			a.renderer.ResetHelpScroll()
			a.startAIChat(fmt.Sprintf("Here is the text of %s (%s):\n\n%s\n\nSummarize the key points.",
				a.searchPanel.PreviewURL, a.searchPanel.PreviewTitle, text))
			return
		}

		// Ctrl+B: bookmark the previewed page; in results mode, toggle
		// showing the bookmark list as the results.
		if mods&glfw.ModControl != 0 && key == glfw.KeyB {
			if a.searchPanel.Mode == searchpanel.ModePreview {
				if a.searchPanel.PreviewURL != "" {
					a.bookmarks = config.AddBookmark(a.bookmarks, config.Bookmark{
						Title: a.searchPanel.PreviewTitle,
						URL:   a.searchPanel.PreviewURL,
					})
					config.SaveBookmarks(a.bookmarks)
					a.showToast("Bookmarked")
				}
				return
			}
			if a.searchPanel.ShowingBookmarks {
				a.searchPanel.HideBookmarks()
				return
			}
			results := make([]searchpanel.Result, len(a.bookmarks))
			for i, b := range a.bookmarks {
				results[i] = searchpanel.Result{Title: b.Title, URL: b.URL}
			}
			a.searchPanel.ShowBookmarks(results)
			return
		}

		switch key {
		case glfw.KeyEscape:
			if a.searchPanel.Mode == searchpanel.ModePreview {
				// Esc peels one layer at a time: find mode, then a
				// persisted mouse selection, then the preview itself.
				if a.searchPanel.FindActive {
					a.searchPanel.ExitFind()
					return
				}
				if a.searchPanel.SelectionActive {
					a.searchPanel.SelectionActive = false
					return
				}
				a.searchPanel.Mode = searchpanel.ModeResults
				a.searchPanel.PreviewScroll = 0
			} else {
				a.searchPanel.Open = false
			}
			return
		case glfw.KeyEnter, glfw.KeyKPEnter:
			if a.searchPanel.Mode == searchpanel.ModePreview {
				if a.searchPanel.FindEditing {
					a.searchPanel.ConfirmFind(previewVisible)
					return
				}
				// Follow the selected link (Tab cycles) instead of
				// leaving the preview.
				if link, ok := a.searchPanel.SelectedLinkTarget(); ok {
					title := link.Text
					if title == "" {
						title = link.URL
					}
					a.startPreview(searchpanel.Result{Title: title, URL: link.URL})
					return
				}
				a.searchPanel.ExitFind()
				a.searchPanel.Mode = searchpanel.ModeResults
				a.searchPanel.PreviewScroll = 0
				return
			}
			if a.searchPanel.ShowingBookmarks {
				// Enter previews the selected bookmark.
				if a.searchPanel.Selected >= 0 && a.searchPanel.Selected < len(a.searchPanel.Results) {
					a.startPreview(a.searchPanel.Results[a.searchPanel.Selected])
				}
				return
			}
			if strings.TrimSpace(a.searchPanel.Query) == "" {
				return
			}
			if a.searchPanel.QueryDirty || len(a.searchPanel.Results) == 0 {
				a.startSearch(a.searchPanel.Query)
				return
			}
			if a.searchPanel.Selected >= 0 && a.searchPanel.Selected < len(a.searchPanel.Results) {
				a.startPreview(a.searchPanel.Results[a.searchPanel.Selected])
			}
			return
		case glfw.KeyUp:
			if a.searchPanel.Mode == searchpanel.ModePreview {
				a.searchPanel.ScrollPreview(-1, previewVisible)
			} else if !a.searchPanel.ShowingBookmarks && (a.searchPanel.QueryDirty || len(a.searchPanel.Results) == 0) {
				// Navigate history when editing query
				a.searchPanel.HistoryUp()
			} else {
				a.searchPanel.MoveSelection(-1, layout.VisibleLines)
			}
			return
		case glfw.KeyDown:
			if a.searchPanel.Mode == searchpanel.ModePreview {
				a.searchPanel.ScrollPreview(1, previewVisible)
			} else if !a.searchPanel.ShowingBookmarks && a.searchPanel.HistoryIndex >= 0 {
				// Navigate history back to current
				a.searchPanel.HistoryDown()
			} else {
				a.searchPanel.MoveSelection(1, layout.VisibleLines)
			}
			return
		case glfw.KeyPageUp:
			if a.searchPanel.Mode == searchpanel.ModePreview {
				a.searchPanel.ScrollPreview(-previewVisible, previewVisible)
			} else {
				a.searchPanel.ScrollResults(-layout.VisibleLines, layout.VisibleLines)
			}
			return
		case glfw.KeyPageDown:
			if a.searchPanel.Mode == searchpanel.ModePreview {
				a.searchPanel.ScrollPreview(previewVisible, previewVisible)
			} else {
				a.searchPanel.ScrollResults(layout.VisibleLines, layout.VisibleLines)
			}
			return
		case glfw.KeyHome:
			if a.searchPanel.Mode == searchpanel.ModePreview {
				a.searchPanel.PreviewScroll = 0
			} else {
				a.searchPanel.ResultsScroll = 0
				a.searchPanel.Selected = 0
			}
			return
		case glfw.KeyEnd:
			if a.searchPanel.Mode == searchpanel.ModePreview {
				a.searchPanel.ScrollPreview(previewTotal, previewVisible)
			} else if len(a.searchPanel.Results) > 0 {
				a.searchPanel.Selected = len(a.searchPanel.Results) - 1
				a.searchPanel.ScrollResults(a.searchPanel.ResultsTotalLines(), layout.VisibleLines)
			}
			return
		case glfw.KeyLeft:
			if a.searchPanel.Mode == searchpanel.ModePreview {
				a.searchPanel.ExitFind()
				a.searchPanel.Mode = searchpanel.ModeResults
				a.searchPanel.PreviewScroll = 0
			}
			return
		case glfw.KeyRight:
			if a.searchPanel.Mode == searchpanel.ModeResults && !a.searchPanel.QueryDirty && len(a.searchPanel.Results) > 0 {
				a.startPreview(a.searchPanel.Results[a.searchPanel.Selected])
			}
			return
		case glfw.KeyTab:
			// Tab / Shift+Tab cycle the extracted page links in preview.
			// Ctrl/Super chords (tab and pane cycling) stay untouched.
			if a.searchPanel.Mode == searchpanel.ModePreview && mods&(glfw.ModControl|glfw.ModSuper) == 0 {
				delta := 1
				if mods&glfw.ModShift != 0 {
					delta = -1
				}
				a.searchPanel.CycleLink(delta, previewVisible)
			}
			return
		case glfw.KeyBackspace:
			if a.searchPanel.FindEditing {
				a.searchPanel.FindBackspace()
				return
			}
			a.searchPanel.Backspace()
			return
		}
		return
	}

handleTerminalInput:
	// Handle help panel scrolling with arrow keys when help is open
	if a.showHelp {
		switch key {
		case glfw.KeyUp:
			a.renderer.ScrollHelpUp()
			return
		case glfw.KeyDown:
			a.renderer.ScrollHelpDown()
			return
		case glfw.KeyPageUp:
			for range 5 {
				a.renderer.ScrollHelpUp()
			}
			return
		case glfw.KeyPageDown:
			for range 5 {
				a.renderer.ScrollHelpDown()
			}
			return
		case glfw.KeyHome:
			a.renderer.ResetHelpScroll()
			return
		case glfw.KeyEscape:
			a.showHelp = false
			a.renderer.ResetHelpScroll()
			return
		}
	}

	if a.resizeMode {
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
			a.resizeMode = false
			return
		}
	}

	appCursor := activeTab.Terminal.AppCursorKeys()
	result := keybindings.TranslateKey(key, mods, appCursor)

	switch result.Action {
	case keybindings.ActionExit:
		a.win.SetShouldClose(true)
	case keybindings.ActionInput:
		// Don't process input when help is shown (except for closing it)
		if a.showHelp {
			return
		}
		// Check for Enter key (carriage return)
		if len(result.Data) == 1 && result.Data[0] == '\r' {
			line := a.lineBuf.getLine()
			cmdResult := commands.HandleCommand(line, a.renderer)
			if cmdResult.Handled {
				// Echo the command (so it appears in terminal)
				activeTab.Write([]byte("\r\n"))
				// Display command output
				output := strings.ReplaceAll(cmdResult.Output, "\n", "\r\n")
				activeTab.Terminal.Process([]byte(output))
				a.lineBuf.clear()
				return
			}
			a.lineBuf.clear()
		}
		// Check for backspace
		if len(result.Data) == 1 && result.Data[0] == 0x7f {
			a.lineBuf.backspace()
		}
		// Check for Ctrl+C or Ctrl+U (line clear)
		if len(result.Data) == 1 && (result.Data[0] == 0x03 || result.Data[0] == 0x15) {
			a.lineBuf.clear()
		}
		activeTab.Write(result.Data)
		activeTab.Terminal.GetGrid().ResetScrollOffset()
	case keybindings.ActionScrollUp:
		activeTab.Terminal.GetGrid().ScrollViewUp(5)
	case keybindings.ActionScrollDown:
		activeTab.Terminal.GetGrid().ScrollViewDown(5)
	case keybindings.ActionScrollUpLine:
		activeTab.Terminal.GetGrid().ScrollViewUp(1)
	case keybindings.ActionScrollDownLine:
		activeTab.Terminal.GetGrid().ScrollViewDown(1)
	case keybindings.ActionToggleFullscreen:
		a.win.ToggleFullscreen()
		// Re-fit immediately: the framebuffer-size callback isn't reliably
		// delivered on fullscreen<->windowed (or cross-monitor) transitions,
		// so without this the grid keeps the old column count and a
		// full-screen TUI overflows the new viewport. ponytail: same pattern
		// as the zoom handlers.
		width, height := a.win.GetFramebufferSize()
		cols, rows := a.renderer.CalculateGridSize(width, height)
		a.tabManager.ResizeAll(uint16(cols), uint16(rows))
	case keybindings.ActionCopy:
		// No selection: leave the clipboard alone.
		if text := activeTab.Terminal.GetGrid().SelectedText(); text != "" {
			glfw.SetClipboardString(text)
			a.showToast("Copied to clipboard")
		} else {
			a.showToast("Nothing selected")
		}
	case keybindings.ActionPaste:
		clip := glfw.GetClipboardString()
		if clip != "" {
			writePaste(activeTab.Terminal, activeTab, clip)
			activeTab.Terminal.GetGrid().ResetScrollOffset()
			a.showToast("Pasted from clipboard")
		}
	case keybindings.ActionNewTab:
		a.lineBuf.clear()
		a.tabManager.NewTab()
	case keybindings.ActionCloseTab:
		a.tabManager.CloseCurrentTab()
	case keybindings.ActionNextTab:
		a.lineBuf.clear()
		a.tabManager.NextTab()
	case keybindings.ActionPrevTab:
		a.lineBuf.clear()
		a.tabManager.PrevTab()
	case keybindings.ActionSelectTab:
		a.lineBuf.clear()
		a.tabManager.SelectTab(result.Num - 1)
	case keybindings.ActionSplitVertical:
		a.lineBuf.clear()
		activeTab.SplitVertical()
	case keybindings.ActionSplitHorizontal:
		a.lineBuf.clear()
		activeTab.SplitHorizontal()
	case keybindings.ActionClosePane:
		a.lineBuf.clear()
		activeTab.ClosePane()
	case keybindings.ActionNextPane:
		a.lineBuf.clear()
		activeTab.NextPane()
	case keybindings.ActionPrevPane:
		a.lineBuf.clear()
		activeTab.PrevPane()
	case keybindings.ActionShowHelp:
		a.showHelp = !a.showHelp
		if !a.showHelp {
			a.renderer.ResetHelpScroll()
		}
	case keybindings.ActionZoomIn:
		if err := a.renderer.ZoomIn(); err == nil {
			// Recalculate grid size after zoom
			width, height := a.win.GetFramebufferSize()
			cols, rows := a.renderer.CalculateGridSize(width, height)
			a.tabManager.ResizeAll(uint16(cols), uint16(rows))
		}
	case keybindings.ActionZoomOut:
		if err := a.renderer.ZoomOut(); err == nil {
			// Recalculate grid size after zoom
			width, height := a.win.GetFramebufferSize()
			cols, rows := a.renderer.CalculateGridSize(width, height)
			a.tabManager.ResizeAll(uint16(cols), uint16(rows))
		}
	case keybindings.ActionZoomReset:
		if err := a.renderer.ZoomReset(); err == nil {
			// Recalculate grid size after zoom
			width, height := a.win.GetFramebufferSize()
			cols, rows := a.renderer.CalculateGridSize(width, height)
			a.tabManager.ResizeAll(uint16(cols), uint16(rows))
		}
	case keybindings.ActionOpenMenu:
		if a.settingsMenu.IsOpen() {
			a.settingsMenu.Close()
		} else {
			a.searchPanel.Open = false
			a.aiPanel.Open = false
			a.settingsMenu.Open()
		}
	case keybindings.ActionToggleResizeMode:
		a.resizeMode = !a.resizeMode
	case keybindings.ActionFindInScrollback:
		a.openFind()
	case keybindings.ActionToggleSearchPanel:
		if !a.searchPanel.Enabled {
			a.showToast("Enable web search in settings")
			return
		}
		a.aiPanel.Open = false
		a.searchPanel.Toggle()
		if a.searchPanel.Open {
			if a.settingsMenu.Config != nil {
				a.searchPanel.ProxyEnabled = a.settingsMenu.Config.WebSearch.UseReaderProxy
			}
			a.searchPanel.Focused = true
			a.showHelp = false
			a.renderer.ResetHelpScroll()
		}
	case keybindings.ActionToggleAIPanel:
		if !a.aiPanel.Enabled {
			a.showToast("Enable Ollama chat in settings")
			return
		}
		a.searchPanel.Open = false
		a.aiPanel.Toggle()
		if a.aiPanel.Open {
			a.aiPanel.Focused = true
			a.showHelp = false
			a.renderer.ResetHelpScroll()
		}
	}
}

func (a *App) onChar(w *glfw.Window, char rune) {
	// Ignore Cmd/Super-modified keys: they are app shortcuts (handled in the key
	// callback), never text input. Prevents e.g. Cmd+T from also typing "t".
	if a.currentMods&glfw.ModSuper != 0 {
		return
	}

	// Find bar: printable runes extend the query and re-run the search.
	if a.find.open {
		a.find.query += string(char)
		a.runFind()
		return
	}

	// Handle character input for settings menu
	if a.settingsMenu.IsOpen() && a.settingsMenu.InputMode() {
		a.settingsMenu.HandleChar(char)
		return
	}

	if a.aiPanel.Open && a.aiPanel.Focused {
		a.aiPanel.AppendInput(char)
		return
	}

	if a.searchPanel.Open && a.searchPanel.Focused {
		// In preview mode "/" starts in-page find; while find is active,
		// runes edit the find query (or n/N step matches) instead of the
		// search query.
		if a.searchPanel.Mode == searchpanel.ModePreview {
			width, height := a.win.GetFramebufferSize()
			cellW, cellH := a.renderer.CellDimensions()
			layout := a.searchPanel.Layout(width, height, cellW, cellH)
			previewVisible := max(layout.VisibleLines-1, 1)
			switch {
			case a.searchPanel.FindEditing:
				a.searchPanel.AppendFind(char)
			case char == '/':
				a.searchPanel.StartFind()
			case a.searchPanel.FindActive && char == 'n':
				a.searchPanel.FindStep(1, previewVisible)
			case a.searchPanel.FindActive && char == 'N':
				a.searchPanel.FindStep(-1, previewVisible)
			default:
				a.searchPanel.AppendQuery(char)
			}
			return
		}
		a.searchPanel.AppendQuery(char)
		return
	}

	// Don't process char input when help or menu is shown
	if a.showHelp || a.settingsMenu.IsOpen() {
		return
	}

	activeTab := a.tabManager.ActiveTab()
	if activeTab == nil {
		return
	}

	// Add character to line buffer
	a.lineBuf.addChar(char)

	data := keybindings.TranslateChar(char, a.currentMods)
	activeTab.Write(data)
	activeTab.Terminal.GetGrid().ResetScrollOffset()
	a.lastInput = time.Now() // hold the cursor solid right after typing
}

func (a *App) onFramebufferSize(w *glfw.Window, width, height int) {
	a.win.SetViewport(width, height)
	cols, rows := a.renderer.CalculateGridSize(width, height)
	a.tabManager.ResizeAll(uint16(cols), uint16(rows))
}

func (a *App) onScroll(w *glfw.Window, xoff, yoff float64) {
	if a.settingsMenu.IsOpen() {
		if a.settingsMenu.InputMode() {
			return
		}
		if a.debugMenu {
			log.Printf("menu: scroll yoff=%.2f input=%v title=%s", yoff, a.settingsMenu.InputMode(), a.settingsMenu.GetTitle())
		}
		steps := int(math.Abs(yoff))
		if steps == 0 {
			steps = 1
		}
		for i := 0; i < steps; i++ {
			if yoff > 0 {
				a.settingsMenu.MoveUp()
			} else if yoff < 0 {
				a.settingsMenu.MoveDown()
			}
		}
		return
	}

	activeTab := a.tabManager.ActiveTab()
	if activeTab == nil {
		return
	}

	if a.selection.active && a.selection.pane != nil {
		pane := a.selection.pane
		g := pane.Terminal.GetGrid()
		steps := int(math.Abs(yoff))
		if steps == 0 {
			steps = 1
		}
		if yoff > 0 {
			g.ScrollViewUp(steps)
		} else if yoff < 0 {
			g.ScrollViewDown(steps)
		} else {
			return
		}

		// The anchor is absolute (survives the scroll); just extend to the
		// cell under the cursor in the new view.
		x, y := w.GetCursorPos()
		if col, row, ok := a.clampedPaneCell(activeTab, pane, x, y); ok {
			g.ExtendSelection(col, row)
		}
		a.renderer.ClearHoverURL()
		return
	}

	if a.aiPanel.Open && a.aiPanel.Focused {
		width, height := a.win.GetFramebufferSize()
		cellW, cellH := a.renderer.CellDimensions()
		layout := a.aiPanel.Layout(width, height, cellW, cellH)
		maxChars := max(int(layout.ContentWidth/cellW)-2, 10)
		totalLines := len(a.aiPanel.WrappedForRender(maxChars))
		visibleLines := layout.VisibleLines
		maxScroll := max(totalLines-visibleLines, 0)
		steps := int(math.Abs(yoff))
		if steps == 0 {
			steps = 1
		}
		for i := 0; i < steps; i++ {
			if yoff > 0 {
				if a.aiPanel.Scroll > 0 {
					a.aiPanel.Scroll--
				}
			} else if yoff < 0 {
				if a.aiPanel.Scroll < maxScroll {
					a.aiPanel.Scroll++
				}
			}
		}
		return
	}

	if a.searchPanel.Open && a.searchPanel.Focused {
		width, height := a.win.GetFramebufferSize()
		cellW, cellH := a.renderer.CellDimensions()
		layout := a.searchPanel.Layout(width, height, cellW, cellH)
		previewVisible := max(layout.VisibleLines-1, 1)
		steps := int(math.Abs(yoff))
		if steps == 0 {
			steps = 1
		}
		for i := 0; i < steps; i++ {
			if yoff > 0 {
				if a.searchPanel.Mode == searchpanel.ModePreview {
					a.searchPanel.ScrollPreview(-1, previewVisible)
				} else {
					a.searchPanel.ScrollResults(-1, layout.VisibleLines)
				}
			} else if yoff < 0 {
				if a.searchPanel.Mode == searchpanel.ModePreview {
					a.searchPanel.ScrollPreview(1, previewVisible)
				} else {
					a.searchPanel.ScrollResults(1, layout.VisibleLines)
				}
			}
		}
		return
	}

	if yoff == 0 {
		return
	}

	// Route the wheel: app mouse reporting (buttons 64/65), alt-screen
	// arrow-key translation, or local scrollback.
	width, height := a.win.GetFramebufferSize()
	x, y := w.GetCursorPos()
	if pane, col, row, ok := a.renderer.HitTestPane(activeTab, x, y, width, height); ok && pane != nil {
		shift := w.GetKey(glfw.KeyLeftShift) == glfw.Press || w.GetKey(glfw.KeyRightShift) == glfw.Press
		kind := parser.MouseWheelUp
		if yoff < 0 {
			kind = parser.MouseWheelDown
		}
		act := parser.DecideMouse(mouseCtxFor(pane, shift), kind, 0, col, row, -1, false)
		if act.Kind == parser.MouseActionSend {
			steps := int(math.Abs(yoff))
			if steps == 0 {
				steps = 1
			}
			for range steps {
				pane.Write(act.Bytes)
			}
			return
		}
	}

	if yoff > 0 {
		activeTab.Terminal.GetGrid().ScrollViewUp(3)
	} else {
		activeTab.Terminal.GetGrid().ScrollViewDown(3)
	}
}

func (a *App) onMouseButton(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
	if a.settingsMenu.IsOpen() || a.showHelp {
		return
	}

	activeTab := a.tabManager.ActiveTab()
	if activeTab == nil {
		return
	}

	width, height := a.win.GetFramebufferSize()
	x, y := w.GetCursorPos()

	// A press that was forwarded to the application gets its matching
	// release forwarded too, wherever the cursor ended up (shift state at
	// release is irrelevant: the app saw the press).
	if action == glfw.Release && a.report.pane != nil && terminalMouseButton(button) == a.report.held {
		mode, sgr, _, _ := a.report.pane.Terminal.MouseState()
		col, row := a.report.lastCol, a.report.lastRow
		if c, r, ok := a.clampedPaneCell(activeTab, a.report.pane, x, y); ok {
			col, row = c, r
		}
		act := parser.DecideMouse(parser.MouseContext{Mode: mode, SGR: sgr}, parser.MouseRelease, a.report.held, col, row, -1, false)
		if act.Kind == parser.MouseActionSend {
			a.report.pane.Write(act.Bytes)
		}
		a.report.pane, a.report.held = nil, -1
		a.report.lastCol, a.report.lastRow = -1, -1 // don't throttle the next context's first motion
		return
	}

	switch button {
	case glfw.MouseButtonLeft:
		switch action {
		case glfw.Press:
			// Check AI panel first for click-to-focus and text selection
			if a.aiPanel.Open {
				cellW, cellH := a.renderer.CellDimensions()
				layout := a.aiPanel.Layout(width, height, cellW, cellH)
				// Layout is in framebuffer pixels; cursor pos is logical.
				s := a.renderer.ContentScale()
				fx, fy := float32(x)*s, float32(y)*s
				if fx >= layout.PanelX && fx <= layout.PanelX+layout.PanelWidth &&
					fy >= layout.PanelY && fy <= layout.PanelY+layout.PanelHeight {
					a.aiPanel.Focused = true
					// Check if click is in message area for text selection
					if fx >= layout.ContentX && fx <= layout.ContentX+layout.ContentWidth &&
						fy >= layout.MessagesStart && fy <= layout.MessagesEnd {
						lineIdx := int((fy-layout.MessagesStart)/layout.LineHeight) - a.aiPanel.AnchorOffset + a.aiPanel.Scroll
						a.aiPanel.SelectionActive = true
						a.aiPanel.SelectionStart = lineIdx
						a.aiPanel.SelectionEnd = lineIdx
					}
					return
				}
				// Click is outside AI panel
				a.aiPanel.Focused = false
			}
			// Check search panel for click-to-focus and click-to-select
			if a.searchPanel.Open {
				cellW, cellH := a.renderer.CellDimensions()
				layout := a.searchPanel.Layout(width, height, cellW, cellH)
				s := a.renderer.ContentScale()
				fx, fy := float32(x)*s, float32(y)*s
				if fx >= layout.PanelX && fx <= layout.PanelX+layout.PanelWidth &&
					fy >= layout.PanelY && fy <= layout.PanelY+layout.PanelHeight {
					a.searchPanel.Focused = true
					// Check if click is in results/preview area
					if fx >= layout.ContentX && fx <= layout.ContentX+layout.ContentWidth &&
						fy >= layout.ResultsStart && fy <= layout.ResultsEnd {
						if a.searchPanel.Mode == searchpanel.ModePreview {
							// Start text selection in preview
							lineIdx := int((fy-layout.ResultsStart-layout.LineHeight)/layout.LineHeight) + a.searchPanel.PreviewScroll
							a.searchPanel.SelectionActive = true
							a.searchPanel.SelectionDragging = true
							a.searchPanel.SelectionStart = lineIdx
							a.searchPanel.SelectionEnd = lineIdx
						} else if len(a.searchPanel.Results) > 0 {
							// Click to select a result
							relY := fy - layout.ResultsStart
							clickedLine := int(relY/layout.LineHeight) + a.searchPanel.ResultsScroll
							clickedResult := clickedLine / a.searchPanel.LinesPerResult()
							if clickedResult >= 0 && clickedResult < len(a.searchPanel.Results) {
								a.searchPanel.Selected = clickedResult
							}
						}
					}
					return
				}
				// Click is outside search panel
				a.searchPanel.Focused = false
				a.searchPanel.SelectionActive = false
			}
			// Tab bar: click a chip to switch tabs, or the "+" to open a new one.
			if idx, newTab, hit := a.renderer.HitTestTabBar(a.tabManager, x, y); hit {
				a.lineBuf.clear()
				if newTab {
					a.tabManager.NewTab()
				} else {
					a.tabManager.SelectTab(idx)
					// Capture the tab by pointer: its slot index changes as the
					// drag reorders the strip, but its identity does not.
					var dragged *tab.Tab
					if tabs := a.tabManager.GetTabs(); idx < len(tabs) {
						dragged = tabs[idx]
					}
					a.tabDrag = tabDragState{pending: true, index: idx, tab: dragged, startX: x, startY: y}
				}
				return
			}
			pane, col, row, ok := a.renderer.HitTestPane(activeTab, x, y, width, height)
			if !ok || pane == nil {
				if a.selection.pane != nil {
					a.selection.pane.Terminal.GetGrid().ClearSelection()
				}
				a.selection.active = false
				a.selection.pane = nil
				return
			}

			if a.selection.pane != nil && a.selection.pane != pane {
				a.selection.pane.Terminal.GetGrid().ClearSelection()
			}

			// Application mouse reporting (vim/tmux/htop): forward the press
			// unless shift is held (shift = local selection, by convention).
			if a.reportMousePress(pane, 0, col, row, mods&glfw.ModShift != 0) {
				activeTab.SetActivePane(pane)
				return
			}

			if mods&glfw.ModControl != 0 {
				if urlText, _, _ := linkAtCell(pane.Terminal.GetGrid(), col, row); urlText != "" {
					if err := openURL(urlText); err != nil {
						log.Printf("failed to open url %q: %v", urlText, err)
					}
					return
				}
			}

			// Multi-click detection: same cell within 400ms.
			now := time.Now()
			if now.Sub(a.selection.lastClickTime) <= 400*time.Millisecond &&
				col == a.selection.lastClickCol && row == a.selection.lastClickRow {
				a.selection.clickCount++
			} else {
				a.selection.clickCount = 1
			}
			a.selection.lastClickTime = now
			a.selection.lastClickCol, a.selection.lastClickRow = col, row

			g := pane.Terminal.GetGrid()
			switch {
			case a.selection.clickCount == 2: // double-click: word
				g.SelectWordAt(col, row)
				a.selection.active = false
				a.selection.pane = pane
				if text := g.SelectedText(); text != "" {
					glfw.SetClipboardString(text)
					a.showToast("Copied to clipboard")
				}
			case a.selection.clickCount >= 3: // triple-click: logical line
				g.SelectLineAt(col, row)
				a.selection.active = false
				a.selection.pane = pane
				a.selection.clickCount = 0
				if text := g.SelectedText(); text != "" {
					glfw.SetClipboardString(text)
					a.showToast("Copied to clipboard")
				}
			default:
				a.selection.active = true
				a.selection.pane = pane
				a.selection.startCol = col
				a.selection.startAbsRow = g.AbsRowForViewRow(row)
				g.StartSelection(col, row)
			}
			activeTab.SetActivePane(pane)
		case glfw.Release:
			// A tab-bar drag ends here; the reorder already happened live.
			// Releasing well clear of the strip tears the tab off into its own
			// window instead, carrying its running shells with it.
			if a.tabDrag.pending || a.tabDrag.active {
				dragged := a.tabDrag
				a.tabDrag = tabDragState{}
				// Release the chip back into the animation so it eases into
				// its final slot instead of staying pinned to the cursor.
				a.renderer.SetTabDrag(nil)
				if dragged.active && x > a.renderer.TabBarWidthLogical()+tearOffThreshold {
					if !a.detachTabAt(dragged.index) {
						a.showToast("Can't tear off the last tab")
					}
				}
				return
			}
			// Handle AI panel text selection release
			if a.aiPanel.SelectionActive {
				cellW, cellH := a.renderer.CellDimensions()
				layout := a.aiPanel.Layout(width, height, cellW, cellH)
				fy := float32(y) * a.renderer.ContentScale()
				if fy < layout.MessagesStart {
					fy = layout.MessagesStart
				}
				if fy > layout.MessagesEnd {
					fy = layout.MessagesEnd
				}
				endLine := int((fy-layout.MessagesStart)/layout.LineHeight) - a.aiPanel.AnchorOffset + a.aiPanel.Scroll
				startLine := a.aiPanel.SelectionStart
				if endLine < startLine {
					startLine, endLine = endLine, startLine
				}
				var selectedText strings.Builder
				for i := startLine; i <= endLine && i < len(a.aiPanel.WrappedLines); i++ {
					if i < 0 {
						continue
					}
					if i > startLine {
						selectedText.WriteString("\n")
					}
					selectedText.WriteString(a.aiPanel.WrappedLines[i].Text)
				}
				if text := selectedText.String(); strings.TrimSpace(text) != "" {
					glfw.SetClipboardString(text)
					a.showToast("Copied to clipboard")
				}
				a.aiPanel.SelectionActive = false
				return
			}
			// Handle search panel preview text selection release. A drag
			// selection stays active (highlighted) so Ctrl+A / Ctrl+I can
			// act on it; only the drag tracking stops.
			if a.searchPanel.SelectionDragging {
				cellW, cellH := a.renderer.CellDimensions()
				layout := a.searchPanel.Layout(width, height, cellW, cellH)
				fy := float32(y) * a.renderer.ContentScale()
				if fy < layout.ResultsStart+layout.LineHeight {
					fy = layout.ResultsStart + layout.LineHeight
				}
				if fy > layout.ResultsEnd {
					fy = layout.ResultsEnd
				}
				a.searchPanel.SelectionEnd = int((fy-layout.ResultsStart-layout.LineHeight)/layout.LineHeight) + a.searchPanel.PreviewScroll
				a.searchPanel.SelectionDragging = false
				// A click with no drag is just focus/positioning: keep no
				// selection and leave the clipboard alone.
				if a.searchPanel.SelectionStart == a.searchPanel.SelectionEnd {
					a.searchPanel.SelectionActive = false
					return
				}
				if text := a.searchPanel.SelectedPreviewText(); strings.TrimSpace(text) != "" {
					glfw.SetClipboardString(text)
					a.showToast("Copied to clipboard")
				}
				return
			}
			if !a.selection.active || a.selection.pane == nil {
				return
			}

			pane := a.selection.pane
			g := pane.Terminal.GetGrid()
			col, row, ok := a.clampedPaneCell(activeTab, pane, x, y)
			if !ok {
				a.selection.active = false
				return
			}

			// A click (no drag): compare in absolute rows so scrolling
			// mid-press doesn't fake a drag.
			if a.selection.startCol == col && a.selection.startAbsRow == g.AbsRowForViewRow(row) {
				g.ClearSelection()
				a.selection.active = false
				return
			}

			g.ExtendSelection(col, row)
			if text := g.SelectedText(); text != "" {
				glfw.SetClipboardString(text)
				a.showToast("Copied to clipboard")
			}

			a.selection.active = false
		}
	case glfw.MouseButtonRight:
		if action != glfw.Press {
			return
		}
		pane, col, row, ok := a.renderer.HitTestPane(activeTab, x, y, width, height)
		if !ok || pane == nil {
			return
		}

		activeTab.SetActivePane(pane)
		g := pane.Terminal.GetGrid()

		if a.reportMousePress(pane, 2, col, row, mods&glfw.ModShift != 0) {
			return
		}

		if mods&glfw.ModControl != 0 {
			if urlText, _, _ := linkAtCell(g, col, row); urlText != "" {
				if err := openURL(urlText); err != nil {
					log.Printf("failed to open url %q: %v", urlText, err)
				}
				return
			}
		}

		if g.HasSelection() {
			if text := g.SelectedText(); text != "" {
				glfw.SetClipboardString(text)
				a.showToast("Copied to clipboard")
			}
			return
		}

		clip := glfw.GetClipboardString()
		if clip != "" {
			// Route through writePaste: strips embedded paste-end markers and
			// honors bracketed paste, targeting the pane under the cursor.
			writePaste(pane.Terminal, pane, clip)
			g.ResetScrollOffset()
			a.showToast("Pasted from clipboard")
		}
	case glfw.MouseButtonMiddle:
		if action != glfw.Press {
			return
		}
		if pane, col, row, ok := a.renderer.HitTestPane(activeTab, x, y, width, height); ok && pane != nil {
			if a.reportMousePress(pane, 1, col, row, mods&glfw.ModShift != 0) {
				activeTab.SetActivePane(pane)
			}
		}
	}
}

func (a *App) onCursorPos(w *glfw.Window, xpos, ypos float64) {
	a.lastCursorX = xpos
	a.lastCursorY = ypos
	a.haveCursorPos = true

	if a.settingsMenu.IsOpen() || a.showHelp {
		a.renderer.ClearHoverURL()
		return
	}

	activeTab := a.tabManager.ActiveTab()
	if activeTab == nil {
		a.renderer.ClearHoverURL()
		return
	}

	// Tab-bar drag: once the cursor moves past a small threshold, live-reorder
	// the dragged chip as it crosses slot boundaries.
	if a.tabDrag.pending || a.tabDrag.active {
		if a.tabDrag.pending {
			const threshold = 5.0
			if dragThresholdPassed(a.tabDrag.startX, a.tabDrag.startY, xpos, ypos, threshold) {
				a.tabDrag.pending = false
				a.tabDrag.active = true
				a.renderer.SetTabDrag(a.tabDrag.tab)
			} else {
				return
			}
		}
		target := a.renderer.TabDropIndex(a.tabManager, ypos)
		if target != a.tabDrag.index {
			a.tabManager.MoveTab(a.tabDrag.index, target)
			a.tabDrag.index = target
		}
		return
	}

	// Track AI panel text selection during drag
	if a.aiPanel.SelectionActive && a.aiPanel.Open {
		width, height := a.win.GetFramebufferSize()
		cellW, cellH := a.renderer.CellDimensions()
		layout := a.aiPanel.Layout(width, height, cellW, cellH)
		fy := float32(ypos) * a.renderer.ContentScale()
		if fy < layout.MessagesStart {
			fy = layout.MessagesStart
		}
		if fy > layout.MessagesEnd {
			fy = layout.MessagesEnd
		}
		a.aiPanel.SelectionEnd = int((fy-layout.MessagesStart)/layout.LineHeight) - a.aiPanel.AnchorOffset + a.aiPanel.Scroll
		return
	}

	// Track search panel preview text selection during drag
	if a.searchPanel.SelectionDragging && a.searchPanel.Open {
		width, height := a.win.GetFramebufferSize()
		cellW, cellH := a.renderer.CellDimensions()
		layout := a.searchPanel.Layout(width, height, cellW, cellH)
		fy := float32(ypos) * a.renderer.ContentScale()
		if fy < layout.ResultsStart+layout.LineHeight {
			fy = layout.ResultsStart + layout.LineHeight
		}
		if fy > layout.ResultsEnd {
			fy = layout.ResultsEnd
		}
		a.searchPanel.SelectionEnd = int((fy-layout.ResultsStart-layout.LineHeight)/layout.LineHeight) + a.searchPanel.PreviewScroll
		return
	}

	if a.selection.active && a.selection.pane != nil {
		if col, row, ok := a.clampedPaneCell(activeTab, a.selection.pane, xpos, ypos); ok {
			a.selection.pane.Terminal.GetGrid().ExtendSelection(col, row)
		}
		a.renderer.ClearHoverURL()
		return
	}

	// Application mouse motion reporting (1002 button-drag / 1003 any-motion),
	// throttled to cell changes. A reported drag targets the pressed pane
	// (clamped to its rect); hover motion targets the pane under the cursor.
	{
		var target *tab.Pane
		var mcol, mrow int
		if a.report.pane != nil {
			if c, r, ok := a.clampedPaneCell(activeTab, a.report.pane, xpos, ypos); ok {
				target, mcol, mrow = a.report.pane, c, r
			}
		} else {
			width, height := a.win.GetFramebufferSize()
			if pane, c, r, ok := a.renderer.HitTestPane(activeTab, xpos, ypos, width, height); ok && pane != nil {
				target, mcol, mrow = pane, c, r
			}
		}
		if target != nil {
			shift := w.GetKey(glfw.KeyLeftShift) == glfw.Press || w.GetKey(glfw.KeyRightShift) == glfw.Press
			held := -1
			if a.report.pane == target {
				held = a.report.held
			}
			if held >= 0 {
				// The press was already forwarded: the shift override is
				// latched at press time (xterm behavior), so pressing shift
				// mid-drag doesn't punch a hole in the app's motion stream.
				shift = false
			}
			cellChanged := mcol != a.report.lastCol || mrow != a.report.lastRow
			act := parser.DecideMouse(mouseCtxFor(target, shift), parser.MouseMotion, 0, mcol, mrow, held, cellChanged)
			switch act.Kind {
			case parser.MouseActionSend:
				a.report.lastCol, a.report.lastRow = mcol, mrow
				target.Write(act.Bytes)
				a.renderer.ClearHoverURL()
				return
			case parser.MouseActionIgnore:
				a.renderer.ClearHoverURL()
				return
			}
		}
	}

	width, height := a.win.GetFramebufferSize()
	pane, col, row, ok := a.renderer.HitTestPane(activeTab, xpos, ypos, width, height)
	if !ok || pane == nil {
		a.renderer.ClearHoverURL()
		return
	}

	if _, startCol, endCol := linkAtCell(pane.Terminal.GetGrid(), col, row); startCol <= endCol {
		a.renderer.SetHoverURL(pane.Terminal.GetGrid(), row, startCol, endCol)
		return
	}
	a.renderer.ClearHoverURL()
}

// onFocus implements focus reporting (?1004): tell apps that requested it
// when the window gains/loses focus.
func (a *App) onFocus(w *glfw.Window, focused bool) {
	a.windowFocused = focused // drives cursor-blink pausing on focus loss
	activeTab := a.tabManager.ActiveTab()
	if activeTab == nil || activeTab.Terminal == nil {
		return
	}
	if !activeTab.Terminal.FocusReportingEnabled() {
		return
	}
	if focused {
		activeTab.Write([]byte("\x1b[I"))
	} else {
		activeTab.Write([]byte("\x1b[O"))
	}
}
