package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/javanhut/RavenTerminal/src/aipanel"
	"github.com/javanhut/RavenTerminal/src/commands"
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/keybindings"
	"github.com/javanhut/RavenTerminal/src/menu"
	"github.com/javanhut/RavenTerminal/src/ollama"
	"github.com/javanhut/RavenTerminal/src/render"
	"github.com/javanhut/RavenTerminal/src/searchpanel"
	"github.com/javanhut/RavenTerminal/src/tab"
	"github.com/javanhut/RavenTerminal/src/websearch"
	"github.com/javanhut/RavenTerminal/src/window"

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

type searchResponse struct {
	id      int
	query   string
	results []websearch.Result
	err     error
}

type previewResponse struct {
	id       int
	url      string
	title    string
	lines    []string
	source   string
	proxyErr string
	err      error
}

type aiResponse struct {
	id       int
	content  string
	thinking string // Thinking content from thinking models
	err      error
	loaded   bool
	token    string // For streaming: incremental token
	done     bool   // For streaming: indicates final response
}

type modelLoadResponse struct {
	url   string
	model string
	err   error
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
	var lastCursorX float64
	var lastCursorY float64
	var haveCursorPos bool
	lastAutoScroll := time.Time{}
	toast := &toastState{}
	showToast := func(message string) {
		if strings.TrimSpace(message) == "" {
			return
		}
		toast.message = message
		toast.expiresAt = time.Now().Add(900 * time.Millisecond)
	}
	searchPanel := searchpanel.New()
	aiPanel := aipanel.New()
	searchResponses := make(chan searchResponse, 4)
	previewResponses := make(chan previewResponse, 4)
	aiResponses := make(chan aiResponse, 4)
	modelLoadResponses := make(chan modelLoadResponse, 2)
	const maxSearchResults = 8
	const maxChatMessages = 6
	settingsMenu := menu.NewMenu()
	settingsMenu.OnConfigReload = func(cfg *config.Config) error {
		if cfg == nil {
			return nil
		}
		searchPanel.SetEnabled(cfg.WebSearch.Enabled)
		aiPanel.SetEnabled(cfg.Ollama.Enabled)
		aiPanel.ShowThinking = cfg.Ollama.ShowThinking
		aiPanel.ThinkingMode = cfg.Ollama.ThinkingMode
		settingsMenu.OllamaModels = nil
		if aiPanel.LoadedURL != cfg.Ollama.URL || aiPanel.LoadedModel != cfg.Ollama.Model {
			aiPanel.ModelLoaded = false
			aiPanel.LoadedURL = cfg.Ollama.URL
			aiPanel.LoadedModel = cfg.Ollama.Model
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
	settingsMenu.OnOllamaTest = func(baseURL string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client := ollama.NewClient(baseURL, "")
		_, err := client.ListModels(ctx)
		return err
	}
	settingsMenu.OnOllamaFetchModels = func(baseURL string) ([]string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		client := ollama.NewClient(baseURL, "")
		return client.ListModels(ctx)
	}
	settingsMenu.OnOllamaLoadModel = func(baseURL, model string) {
		// Show loading status immediately
		aiPanel.Status = "Loading model..."
		aiPanel.ModelLoaded = false
		// Load model in background
		go func(url, m string) {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second) // 5 min for slow remote APIs
			defer cancel()
			client := ollama.NewClient(url, m)
			err := client.LoadModel(ctx)
			modelLoadResponses <- modelLoadResponse{url: url, model: m, err: err}
		}(baseURL, model)
	}
	currentTheme := ""
	if settingsMenu.Config != nil {
		currentTheme = settingsMenu.Config.Theme
		searchPanel.SetEnabled(settingsMenu.Config.WebSearch.Enabled)
		aiPanel.SetEnabled(settingsMenu.Config.Ollama.Enabled)
		aiPanel.ShowThinking = settingsMenu.Config.Ollama.ShowThinking
		aiPanel.ThinkingMode = settingsMenu.Config.Ollama.ThinkingMode
		aiPanel.LoadedURL = settingsMenu.Config.Ollama.URL
		aiPanel.LoadedModel = settingsMenu.Config.Ollama.Model
		renderer.SetThemeByName(currentTheme)
		if err := renderer.SetDefaultFontSize(settingsMenu.Config.FontSize); err == nil {
			width, height := win.GetFramebufferSize()
			cols, rows := renderer.CalculateGridSize(width, height)
			tabManager.ResizeAll(uint16(cols), uint16(rows))
		}
	}

	startSearch := func(query string) {
		searchPanel.Mode = searchpanel.ModeResults
		searchPanel.Status = "Searching..."
		searchPanel.StartLoading()
		searchPanel.Results = nil
		searchPanel.Selected = 0
		searchPanel.ResultsScroll = 0
		searchPanel.ResetHistory()
		searchPanel.SearchID++
		searchID := searchPanel.SearchID
		go func(id int, q string) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			results, err := websearch.SearchDuckDuckGo(ctx, q, maxSearchResults)
			searchResponses <- searchResponse{id: id, query: q, results: results, err: err}
		}(searchID, query)
	}

	startPreview := func(result searchpanel.Result) {
		searchPanel.Mode = searchpanel.ModePreview
		searchPanel.Status = "Loading preview..."
		searchPanel.StartLoading()
		searchPanel.PreviewTitle = result.Title
		searchPanel.PreviewURL = result.URL
		searchPanel.PreviewLines = nil
		searchPanel.PreviewScroll = 0
		searchPanel.PreviewID++
		previewID := searchPanel.PreviewID
		useReaderProxy := searchPanel.ProxyEnabled
		var proxyURLs []string
		if settingsMenu.Config != nil {
			proxyURLs = settingsMenu.Config.WebSearch.ReaderProxyURLs
		}
		go func(id int, url, title string, useProxy bool) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			lines, source, proxyErr, err := websearch.FetchText(ctx, url, 12000, useProxy, proxyURLs)
			previewResponses <- previewResponse{id: id, url: url, title: title, lines: lines, source: source, proxyErr: proxyErr, err: err}
		}(previewID, result.URL, result.Title, useReaderProxy)
	}

	startAIChat := func(prompt string) {
		if settingsMenu.Config == nil {
			aiPanel.Status = "Missing config"
			return
		}
		trimmed := strings.TrimSpace(prompt)
		if trimmed == "" {
			return
		}

		cfg := settingsMenu.Config.Ollama
		if aiPanel.LoadedURL != cfg.URL || aiPanel.LoadedModel != cfg.Model {
			aiPanel.ModelLoaded = false
		}

		aiPanel.AddMessage("user", trimmed)
		aiPanel.TrimMessages(maxChatMessages)
		aiPanel.ClearInput()
		if !aiPanel.ModelLoaded {
			aiPanel.Status = "Loading model..."
		} else {
			aiPanel.Status = "Thinking..."
		}
		aiPanel.StartLoading()
		aiPanel.RequestID++
		requestID := aiPanel.RequestID
		needLoad := !aiPanel.ModelLoaded

		messages := make([]ollama.Message, 0, len(aiPanel.Messages))
		for _, msg := range aiPanel.Messages {
			messages = append(messages, ollama.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}

		// Configure timeout based on thinking mode
		timeout := 180 * time.Second
		if cfg.ThinkingMode && cfg.ExtendedTimeout > 0 {
			timeout = time.Duration(cfg.ExtendedTimeout) * time.Second
		}

		go func(id int, baseURL, model string, messages []ollama.Message, loadModel bool, thinkingEnabled bool, thinkingBudget int) {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			client := ollama.NewClient(baseURL, model)
			// Configure thinking mode
			client.Thinking = ollama.ThinkingOptions{
				Enabled: thinkingEnabled,
				Budget:  thinkingBudget,
			}

			loadSuccess := false
			if loadModel {
				aiResponses <- aiResponse{id: id, token: "", done: false} // Signal streaming start
				if err := client.LoadModel(ctx); err != nil {
					aiResponses <- aiResponse{id: id, err: err, done: true}
					return
				}
				loadSuccess = true
				// Signal: model loaded, now thinking
				aiResponses <- aiResponse{id: id, token: "", done: false, loaded: true}
			}

			// Use streaming chat with thinking support
			result, err := client.ChatStreamWithThinking(ctx, messages, func(token string) {
				aiResponses <- aiResponse{id: id, token: token, done: false}
			}, nil)
			aiResponses <- aiResponse{id: id, thinking: result.Thinking, err: err, done: true, loaded: loadSuccess}
		}(requestID, cfg.URL, cfg.Model, messages, needLoad, cfg.ThinkingMode, cfg.ThinkingBudget)
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

		// Handle AI panel focus and input
		if aiPanel.Open {
			appCursor := activeTab.Terminal.AppCursorKeys()
			result := keybindings.TranslateKey(key, mods, appCursor)
			if result.Action == keybindings.ActionNextPane || result.Action == keybindings.ActionPrevPane {
				if aiPanel.Focused {
					aiPanel.Focused = false
					if result.Action == keybindings.ActionNextPane {
						activeTab.NextPane()
					} else {
						activeTab.PrevPane()
					}
					showToast("Terminal focused")
				} else {
					aiPanel.Focused = true
					showToast("AI panel focused")
				}
				return
			}
			if result.Action == keybindings.ActionToggleAIPanel {
				aiPanel.Open = false
				aiPanel.Reset()
				return
			}
			if result.Action == keybindings.ActionToggleSearchPanel {
				aiPanel.Open = false
				aiPanel.Reset()
				if !searchPanel.Enabled {
					showToast("Enable web search in settings")
					return
				}
				searchPanel.Toggle()
				if searchPanel.Open {
					if settingsMenu.Config != nil {
						searchPanel.ProxyEnabled = settingsMenu.Config.WebSearch.UseReaderProxy
					}
					searchPanel.Focused = true
					showHelp = false
					renderer.ResetHelpScroll()
				}
				return
			}
			if !aiPanel.Focused {
				// Let terminal handle input while panel stays visible.
				goto handleTerminalInput
			}

			switch result.Action {
			case keybindings.ActionCopy:
				// In AI panel, copy the last assistant response
				lastResponse := aiPanel.GetLastAssistantMessage()
				if lastResponse != "" {
					glfw.SetClipboardString(lastResponse)
					showToast("Copied AI response")
				} else {
					showToast("No AI response to copy")
				}
				return
			case keybindings.ActionPaste:
				clip := glfw.GetClipboardString()
				if clip != "" {
					clip = strings.ReplaceAll(clip, "\r\n", "\n")
					clip = strings.ReplaceAll(clip, "\r", "\n")
					clip = strings.ReplaceAll(clip, "\n", " ")
					aiPanel.SetInput(aiPanel.Input + clip)
					showToast("Pasted into AI prompt")
				}
				return
			}

			width, height := win.GetFramebufferSize()
			cellW, cellH := renderer.CellDimensions()
			layout := aiPanel.Layout(width, height, cellW, cellH)
			maxChars := int(layout.ContentWidth/cellW) - 2
			if maxChars < 10 {
				maxChars = 10
			}
			wrapped := aipanel.BuildWrappedLinesWithThinking(aiPanel.Messages, maxChars, aiPanel.ShowThinking, aiPanel.ThinkingExpanded)
			totalLines := len(wrapped)
			visibleLines := layout.VisibleLines
			maxScroll := totalLines - visibleLines
			if maxScroll < 0 {
				maxScroll = 0
			}
			if aiPanel.Scroll > maxScroll {
				aiPanel.Scroll = maxScroll
			}

			if action == glfw.Repeat && (key == glfw.KeyEnter || key == glfw.KeyKPEnter) {
				return
			}

			if mods&glfw.ModControl != 0 && key == glfw.KeyU {
				aiPanel.ClearInput()
				return
			}

			// Ctrl+T: toggle thinking expansion
			if mods&glfw.ModControl != 0 && key == glfw.KeyT {
				if aipanel.HasThinkingContent(aiPanel.Messages) {
					aiPanel.ToggleThinkingExpanded()
				}
				return
			}

			// Ctrl+Enter: send message
			if mods&glfw.ModControl != 0 && (key == glfw.KeyEnter || key == glfw.KeyKPEnter) {
				if aiPanel.Loading {
					return
				}
				startAIChat(aiPanel.Input)
				return
			}

			switch key {
			case glfw.KeyEscape:
				aiPanel.Open = false
				aiPanel.Reset()
				return
			case glfw.KeyEnter, glfw.KeyKPEnter:
				// Regular Enter or Shift+Enter: add newline
				if action == glfw.Repeat {
					return
				}
				aiPanel.AppendNewline()
				return
			case glfw.KeyUp:
				// Scroll input if multiline, otherwise scroll messages
				if len(aiPanel.InputLines) > layout.InputLines {
					aiPanel.ScrollInputUp()
				} else if aiPanel.Scroll > 0 {
					aiPanel.Scroll--
				}
				return
			case glfw.KeyDown:
				// Scroll input if multiline, otherwise scroll messages
				if len(aiPanel.InputLines) > layout.InputLines {
					aiPanel.ScrollInputDown(layout.InputLines)
				} else if aiPanel.Scroll < maxScroll {
					aiPanel.Scroll++
				}
				return
			case glfw.KeyPageUp:
				aiPanel.Scroll -= visibleLines
				if aiPanel.Scroll < 0 {
					aiPanel.Scroll = 0
				}
				return
			case glfw.KeyPageDown:
				aiPanel.Scroll += visibleLines
				if aiPanel.Scroll > maxScroll {
					aiPanel.Scroll = maxScroll
				}
				return
			case glfw.KeyHome:
				if mods&glfw.ModControl != 0 {
					aiPanel.Scroll = 0
				}
				return
			case glfw.KeyEnd:
				if mods&glfw.ModControl != 0 {
					aiPanel.Scroll = maxScroll
				}
				return
			case glfw.KeyBackspace:
				aiPanel.Backspace()
				return
			}
			return
		}

		// Handle search panel focus and input
		if searchPanel.Open {
			appCursor := activeTab.Terminal.AppCursorKeys()
			result := keybindings.TranslateKey(key, mods, appCursor)
			if result.Action == keybindings.ActionNextPane || result.Action == keybindings.ActionPrevPane {
				if searchPanel.Focused {
					searchPanel.Focused = false
					if result.Action == keybindings.ActionNextPane {
						activeTab.NextPane()
					} else {
						activeTab.PrevPane()
					}
					showToast("Terminal focused")
				} else {
					searchPanel.Focused = true
					showToast("Search panel focused")
				}
				return
			}
			if result.Action == keybindings.ActionToggleSearchPanel {
				searchPanel.Toggle()
				return
			}
			if result.Action == keybindings.ActionToggleAIPanel {
				searchPanel.Open = false
				if !aiPanel.Enabled {
					showToast("Enable Ollama chat in settings")
					return
				}
				aiPanel.Toggle()
				if aiPanel.Open {
					aiPanel.Focused = true
					showHelp = false
					renderer.ResetHelpScroll()
				} else {
					aiPanel.Reset()
				}
				return
			}
			if !searchPanel.Focused {
				// Let terminal handle input while panel stays visible.
				goto handleTerminalInput
			}

			switch result.Action {
			case keybindings.ActionCopy:
				g := activeTab.Terminal.GetGrid()
				text := g.SelectedText()
				if text == "" {
					text = g.VisibleText()
				}
				if text != "" {
					glfw.SetClipboardString(text)
					showToast("Copied to clipboard")
				}
				return
			case keybindings.ActionPaste:
				clip := glfw.GetClipboardString()
				if clip != "" {
					clip = strings.ReplaceAll(clip, "\r\n", "\n")
					clip = strings.ReplaceAll(clip, "\n", "\r")
					activeTab.Write([]byte(clip))
					activeTab.Terminal.GetGrid().ResetScrollOffset()
					showToast("Pasted from clipboard")
				}
				return
			}

			width, height := win.GetFramebufferSize()
			cellW, cellH := renderer.CellDimensions()
			layout := searchPanel.Layout(width, height, cellW, cellH)
			previewVisible := layout.VisibleLines - 1
			if previewVisible < 1 {
				previewVisible = 1
			}
			previewTotal := len(searchPanel.PreviewLines)
			if len(searchPanel.PreviewWrapped) > 0 && searchPanel.PreviewWrapChars > 0 {
				previewTotal = len(searchPanel.PreviewWrapped)
			}
			if action == glfw.Repeat && (key == glfw.KeyEnter || key == glfw.KeyKPEnter) {
				return
			}
			if mods&glfw.ModControl != 0 && mods&glfw.ModShift != 0 && key == glfw.KeyR {
				searchPanel.ProxyEnabled = !searchPanel.ProxyEnabled
				if searchPanel.ProxyEnabled {
					searchPanel.Status = "Reader proxy enabled"
				} else {
					searchPanel.Status = "Reader proxy disabled"
				}
				if searchPanel.Mode == searchpanel.ModePreview && searchPanel.PreviewURL != "" {
					startPreview(searchpanel.Result{
						Title: searchPanel.PreviewTitle,
						URL:   searchPanel.PreviewURL,
					})
				}
				return
			}

			if mods&glfw.ModControl != 0 && key == glfw.KeyU {
				searchPanel.ClearQuery()
				return
			}

			// Ctrl+O: Open selected URL in browser
			if mods&glfw.ModControl != 0 && key == glfw.KeyO {
				var urlToOpen string
				if searchPanel.Mode == searchpanel.ModePreview {
					urlToOpen = searchPanel.PreviewURL
				} else {
					urlToOpen = searchPanel.GetSelectedURL()
				}
				if urlToOpen != "" {
					if err := openURL(urlToOpen); err != nil {
						searchPanel.Status = "Failed to open browser"
					} else {
						searchPanel.Status = "Opening in browser..."
					}
				}
				return
			}

			switch key {
			case glfw.KeyEscape:
				if searchPanel.Mode == searchpanel.ModePreview {
					searchPanel.Mode = searchpanel.ModeResults
					searchPanel.PreviewScroll = 0
				} else {
					searchPanel.Open = false
				}
				return
			case glfw.KeyEnter, glfw.KeyKPEnter:
				if searchPanel.Mode == searchpanel.ModePreview {
					searchPanel.Mode = searchpanel.ModeResults
					searchPanel.PreviewScroll = 0
					return
				}
				if strings.TrimSpace(searchPanel.Query) == "" {
					return
				}
				if searchPanel.QueryDirty || len(searchPanel.Results) == 0 {
					startSearch(searchPanel.Query)
					return
				}
				if searchPanel.Selected >= 0 && searchPanel.Selected < len(searchPanel.Results) {
					startPreview(searchPanel.Results[searchPanel.Selected])
				}
				return
			case glfw.KeyUp:
				if searchPanel.Mode == searchpanel.ModePreview {
					searchPanel.ScrollPreview(-1, previewVisible)
				} else if searchPanel.QueryDirty || len(searchPanel.Results) == 0 {
					// Navigate history when editing query
					searchPanel.HistoryUp()
				} else {
					searchPanel.MoveSelection(-1, layout.VisibleLines)
				}
				return
			case glfw.KeyDown:
				if searchPanel.Mode == searchpanel.ModePreview {
					searchPanel.ScrollPreview(1, previewVisible)
				} else if searchPanel.HistoryIndex >= 0 {
					// Navigate history back to current
					searchPanel.HistoryDown()
				} else {
					searchPanel.MoveSelection(1, layout.VisibleLines)
				}
				return
			case glfw.KeyPageUp:
				if searchPanel.Mode == searchpanel.ModePreview {
					searchPanel.ScrollPreview(-previewVisible, previewVisible)
				} else {
					searchPanel.ScrollResults(-layout.VisibleLines, layout.VisibleLines)
				}
				return
			case glfw.KeyPageDown:
				if searchPanel.Mode == searchpanel.ModePreview {
					searchPanel.ScrollPreview(previewVisible, previewVisible)
				} else {
					searchPanel.ScrollResults(layout.VisibleLines, layout.VisibleLines)
				}
				return
			case glfw.KeyHome:
				if searchPanel.Mode == searchpanel.ModePreview {
					searchPanel.PreviewScroll = 0
				} else {
					searchPanel.ResultsScroll = 0
					searchPanel.Selected = 0
				}
				return
			case glfw.KeyEnd:
				if searchPanel.Mode == searchpanel.ModePreview {
					searchPanel.ScrollPreview(previewTotal, previewVisible)
				} else if len(searchPanel.Results) > 0 {
					searchPanel.Selected = len(searchPanel.Results) - 1
					searchPanel.ScrollResults(searchPanel.ResultsTotalLines(), layout.VisibleLines)
				}
				return
			case glfw.KeyLeft:
				if searchPanel.Mode == searchpanel.ModePreview {
					searchPanel.Mode = searchpanel.ModeResults
					searchPanel.PreviewScroll = 0
				}
				return
			case glfw.KeyRight:
				if searchPanel.Mode == searchpanel.ModeResults && !searchPanel.QueryDirty && len(searchPanel.Results) > 0 {
					startPreview(searchPanel.Results[searchPanel.Selected])
				}
				return
			case glfw.KeyBackspace:
				searchPanel.Backspace()
				return
			}
			return
		}

	handleTerminalInput:
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
			win.ToggleFullscreen()
		case keybindings.ActionCopy:
			g := activeTab.Terminal.GetGrid()
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
				activeTab.Terminal.GetGrid().ResetScrollOffset()
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
				searchPanel.Open = false
				aiPanel.Open = false
				aiPanel.Reset()
				settingsMenu.Open()
			}
		case keybindings.ActionToggleResizeMode:
			resizeMode = !resizeMode
		case keybindings.ActionToggleSearchPanel:
			if !searchPanel.Enabled {
				showToast("Enable web search in settings")
				return
			}
			aiPanel.Open = false
			aiPanel.Reset()
			searchPanel.Toggle()
			if searchPanel.Open {
				if settingsMenu.Config != nil {
					searchPanel.ProxyEnabled = settingsMenu.Config.WebSearch.UseReaderProxy
				}
				searchPanel.Focused = true
				showHelp = false
				renderer.ResetHelpScroll()
			}
		case keybindings.ActionToggleAIPanel:
			if !aiPanel.Enabled {
				showToast("Enable Ollama chat in settings")
				return
			}
			searchPanel.Open = false
			aiPanel.Toggle()
			if aiPanel.Open {
				aiPanel.Focused = true
				showHelp = false
				renderer.ResetHelpScroll()
			} else {
				aiPanel.Reset()
			}
		}
	})

	win.GLFW().SetCharCallback(func(w *glfw.Window, char rune) {
		// Handle character input for settings menu
		if settingsMenu.IsOpen() && settingsMenu.InputMode() {
			settingsMenu.HandleChar(char)
			return
		}

		if aiPanel.Open && aiPanel.Focused {
			aiPanel.AppendInput(char)
			return
		}

		if searchPanel.Open && searchPanel.Focused {
			searchPanel.AppendQuery(char)
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
		activeTab.Terminal.GetGrid().ResetScrollOffset()
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

		if selection.active && selection.pane != nil {
			pane := selection.pane
			g := pane.Terminal.GetGrid()
			steps := int(math.Abs(yoff))
			if steps == 0 {
				steps = 1
			}
			if yoff > 0 {
				g.ScrollViewUp(steps)
				selection.startRow += steps
			} else if yoff < 0 {
				g.ScrollViewDown(steps)
				selection.startRow -= steps
			} else {
				return
			}

			selection.startRow = clampInt(selection.startRow, 0, g.Rows-1)

			width, height := win.GetFramebufferSize()
			x, y := w.GetCursorPos()
			rectX, rectY, rectW, rectH, ok := renderer.PaneRectFor(activeTab, pane, width, height)
			if !ok {
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
			col = clampInt(col, 0, g.Cols-1)
			row = clampInt(row, 0, g.Rows-1)

			g.SetSelection(selection.startCol, selection.startRow, col, row)
			renderer.ClearHoverURL()
			return
		}

		if aiPanel.Open && aiPanel.Focused {
			width, height := win.GetFramebufferSize()
			cellW, cellH := renderer.CellDimensions()
			layout := aiPanel.Layout(width, height, cellW, cellH)
			maxChars := int(layout.ContentWidth/cellW) - 2
			if maxChars < 10 {
				maxChars = 10
			}
			totalLines := len(aipanel.BuildWrappedLinesWithThinking(aiPanel.Messages, maxChars, aiPanel.ShowThinking, aiPanel.ThinkingExpanded))
			visibleLines := layout.VisibleLines
			maxScroll := totalLines - visibleLines
			if maxScroll < 0 {
				maxScroll = 0
			}
			steps := int(math.Abs(yoff))
			if steps == 0 {
				steps = 1
			}
			for i := 0; i < steps; i++ {
				if yoff > 0 {
					if aiPanel.Scroll > 0 {
						aiPanel.Scroll--
					}
				} else if yoff < 0 {
					if aiPanel.Scroll < maxScroll {
						aiPanel.Scroll++
					}
				}
			}
			return
		}

		if searchPanel.Open && searchPanel.Focused {
			width, height := win.GetFramebufferSize()
			cellW, cellH := renderer.CellDimensions()
			layout := searchPanel.Layout(width, height, cellW, cellH)
			previewVisible := layout.VisibleLines - 1
			if previewVisible < 1 {
				previewVisible = 1
			}
			steps := int(math.Abs(yoff))
			if steps == 0 {
				steps = 1
			}
			for i := 0; i < steps; i++ {
				if yoff > 0 {
					if searchPanel.Mode == searchpanel.ModePreview {
						searchPanel.ScrollPreview(-1, previewVisible)
					} else {
						searchPanel.ScrollResults(-1, layout.VisibleLines)
					}
				} else if yoff < 0 {
					if searchPanel.Mode == searchpanel.ModePreview {
						searchPanel.ScrollPreview(1, previewVisible)
					} else {
						searchPanel.ScrollResults(1, layout.VisibleLines)
					}
				}
			}
			return
		}

		if yoff > 0 {
			activeTab.Terminal.GetGrid().ScrollViewUp(3)
		} else if yoff < 0 {
			activeTab.Terminal.GetGrid().ScrollViewDown(3)
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
				// Check AI panel first for click-to-focus and text selection
				if aiPanel.Open {
					cellW, cellH := renderer.CellDimensions()
					layout := aiPanel.Layout(width, height, cellW, cellH)
					fx, fy := float32(x), float32(y)
					if fx >= layout.PanelX && fx <= layout.PanelX+layout.PanelWidth &&
						fy >= layout.PanelY && fy <= layout.PanelY+layout.PanelHeight {
						aiPanel.Focused = true
						// Check if click is in message area for text selection
						if fx >= layout.ContentX && fx <= layout.ContentX+layout.ContentWidth &&
							fy >= layout.MessagesStart && fy <= layout.MessagesEnd {
							lineIdx := int((fy-layout.MessagesStart)/layout.LineHeight) + aiPanel.Scroll
							aiPanel.SelectionActive = true
							aiPanel.SelectionStart = lineIdx
							aiPanel.SelectionEnd = lineIdx
						}
						return
					}
					// Click is outside AI panel
					aiPanel.Focused = false
				}
				pane, col, row, ok := renderer.HitTestPane(activeTab, x, y, width, height)
				if !ok || pane == nil {
					if selection.pane != nil {
						selection.pane.Terminal.GetGrid().ClearSelection()
					}
					selection.active = false
					selection.pane = nil
					return
				}

				if selection.pane != nil && selection.pane != pane {
					selection.pane.Terminal.GetGrid().ClearSelection()
				}

				if mods&glfw.ModControl != 0 {
					if urlText, _, _ := urlAtCellRange(pane.Terminal.GetGrid(), col, row); urlText != "" {
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
				pane.Terminal.GetGrid().SetSelection(col, row, col, row)
				activeTab.SetActivePane(pane)
			case glfw.Release:
				// Handle AI panel text selection release
				if aiPanel.SelectionActive {
					cellW, cellH := renderer.CellDimensions()
					layout := aiPanel.Layout(width, height, cellW, cellH)
					fy := float32(y)
					if fy < layout.MessagesStart {
						fy = layout.MessagesStart
					}
					if fy > layout.MessagesEnd {
						fy = layout.MessagesEnd
					}
					endLine := int((fy-layout.MessagesStart)/layout.LineHeight) + aiPanel.Scroll
					startLine := aiPanel.SelectionStart
					if endLine < startLine {
						startLine, endLine = endLine, startLine
					}
					var selectedText strings.Builder
					for i := startLine; i <= endLine && i < len(aiPanel.WrappedLines); i++ {
						if i < 0 {
							continue
						}
						if i > startLine {
							selectedText.WriteString("\n")
						}
						selectedText.WriteString(aiPanel.WrappedLines[i].Text)
					}
					if text := selectedText.String(); strings.TrimSpace(text) != "" {
						glfw.SetClipboardString(text)
						showToast("Copied to clipboard")
					}
					aiPanel.SelectionActive = false
					return
				}
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
				g := pane.Terminal.GetGrid()
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
			g := pane.Terminal.GetGrid()

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
		lastCursorX = xpos
		lastCursorY = ypos
		haveCursorPos = true

		if settingsMenu.IsOpen() || showHelp {
			renderer.ClearHoverURL()
			return
		}

		activeTab := tabManager.ActiveTab()
		if activeTab == nil {
			renderer.ClearHoverURL()
			return
		}

		// Track AI panel text selection during drag
		if aiPanel.SelectionActive && aiPanel.Open {
			width, height := win.GetFramebufferSize()
			cellW, cellH := renderer.CellDimensions()
			layout := aiPanel.Layout(width, height, cellW, cellH)
			fy := float32(ypos)
			if fy < layout.MessagesStart {
				fy = layout.MessagesStart
			}
			if fy > layout.MessagesEnd {
				fy = layout.MessagesEnd
			}
			aiPanel.SelectionEnd = int((fy-layout.MessagesStart)/layout.LineHeight) + aiPanel.Scroll
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
			g := selection.pane.Terminal.GetGrid()
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

		if _, startCol, endCol := urlAtCellRange(pane.Terminal.GetGrid(), col, row); startCol <= endCol {
			renderer.SetHoverURL(pane.Terminal.GetGrid(), row, startCol, endCol)
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
		if settingsMenu.Config != nil {
			searchPanel.SetEnabled(settingsMenu.Config.WebSearch.Enabled)
			if !searchPanel.Open {
				searchPanel.ProxyEnabled = settingsMenu.Config.WebSearch.UseReaderProxy
			}
		}

		for {
			select {
			case resp := <-searchResponses:
				if resp.id != searchPanel.SearchID {
					break
				}
				results := make([]searchpanel.Result, 0, len(resp.results))
				for _, r := range resp.results {
					results = append(results, searchpanel.Result{
						Title:   r.Title,
						URL:     r.URL,
						Snippet: r.Snippet,
					})
				}
				searchPanel.SetResults(resp.query, results, resp.err)
				if resp.err == nil {
					// Add successful query to history
					searchPanel.AddToHistory(resp.query)
					if len(results) == 0 {
						searchPanel.Status = "No results"
					} else {
						searchPanel.Status = fmt.Sprintf("%d results", len(results))
					}
				}
			default:
				goto searchDone
			}
		}
	searchDone:

		for {
			select {
			case resp := <-previewResponses:
				if resp.id != searchPanel.PreviewID {
					break
				}
				searchPanel.SetPreview(resp.url, resp.title, resp.lines, resp.err)
				if resp.err == nil {
					if resp.source == "proxy" {
						searchPanel.Status = "Source: reader proxy"
					} else {
						searchPanel.Status = "Source: direct HTML"
					}
					if resp.proxyErr != "" && resp.source != "proxy" {
						searchPanel.Status = "Proxy failed: " + resp.proxyErr
					}
				}
			default:
				goto previewDone
			}
		}
	previewDone:

		for {
			select {
			case resp := <-aiResponses:
				if resp.id != aiPanel.RequestID {
					break
				}
				if !resp.done {
					if resp.loaded {
						// Model finished loading, now generating
						aiPanel.Status = "Thinking..."
					}
					// Streaming token - append to assistant message
					if resp.token != "" {
						aiPanel.Status = ""
						aiPanel.AppendToLastMessage("assistant", resp.token)
					}
					break
				}
				// Final response
				aiPanel.Loading = false
				if resp.err != nil {
					aiPanel.Status = "Error occurred"
					aiPanel.AddMessage("error", resp.err.Error())
					break
				}
				aiPanel.Status = ""

				// Add thinking content to the last assistant message if present
				if resp.thinking != "" && len(aiPanel.Messages) > 0 {
					lastIdx := len(aiPanel.Messages) - 1
					if aiPanel.Messages[lastIdx].Role == "assistant" {
						aiPanel.Messages[lastIdx].Thinking = resp.thinking
					}
				}

				aiPanel.TrimMessages(maxChatMessages)
				if resp.loaded {
					if settingsMenu.Config != nil {
						aiPanel.ModelLoaded = true
						aiPanel.LoadedURL = settingsMenu.Config.Ollama.URL
						aiPanel.LoadedModel = settingsMenu.Config.Ollama.Model
					}
				}
			default:
				goto aiDone
			}
		}
	aiDone:

		// Handle model load responses
		for {
			select {
			case resp := <-modelLoadResponses:
				if resp.err != nil {
					aiPanel.Status = "Load failed"
					aiPanel.AddMessage("error", "Failed to load model: "+resp.err.Error())
					aiPanel.ModelLoaded = false
				} else {
					aiPanel.Status = "Model Loaded: " + resp.model
					aiPanel.ModelLoaded = true
					aiPanel.LoadedURL = resp.url
					aiPanel.LoadedModel = resp.model
				}
			default:
				goto modelLoadDone
			}
		}
	modelLoadDone:

		// Handle cursor blinking
		now := time.Now()
		if now.Sub(lastBlink) >= blinkInterval {
			cursorVisible = !cursorVisible
			lastBlink = now
		}

		if selection.active && selection.pane != nil && haveCursorPos {
			if now.Sub(lastAutoScroll) >= time.Millisecond*50 {
				activeTab := tabManager.ActiveTab()
				if activeTab != nil {
					width, height := win.GetFramebufferSize()
					rectX, rectY, rectW, rectH, ok := renderer.PaneRectFor(activeTab, selection.pane, width, height)
					if ok {
						cellW, cellH := renderer.CellSize()
						edge := float64(cellH)
						var dir int
						if lastCursorY < float64(rectY)+edge {
							dir = -1
						} else if lastCursorY > float64(rectY+rectH)-edge {
							dir = 1
						}
						if dir != 0 {
							g := selection.pane.Terminal.GetGrid()
							prevOffset := g.GetScrollOffset()
							if dir < 0 {
								g.ScrollViewUp(1)
							} else {
								g.ScrollViewDown(1)
							}
							if g.GetScrollOffset() != prevOffset {
								if dir < 0 {
									selection.startRow++
								} else {
									selection.startRow--
								}
								selection.startRow = clampInt(selection.startRow, 0, g.Rows-1)

								fx := float32(lastCursorX)
								fy := float32(lastCursorY)
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

								col := int((fx - rectX) / cellW)
								row := int((fy - rectY) / cellH)
								col = clampInt(col, 0, g.Cols-1)
								row = clampInt(row, 0, g.Rows-1)
								g.SetSelection(selection.startCol, selection.startRow, col, row)
								renderer.ClearHoverURL()
								lastAutoScroll = now
							}
						}
					}
				}
			}
		}

		// Render
		width, height := win.GetFramebufferSize()
		win.SetViewport(width, height)
		drawCursor := cursorVisible
		if activeTab := tabManager.ActiveTab(); activeTab != nil && activeTab.Terminal != nil {
			drawCursor = drawCursor && activeTab.Terminal.IsCursorVisible()
		}
		if settingsMenu.IsOpen() {
			renderer.RenderWithMenu(tabManager, width, height, drawCursor, settingsMenu)
		} else {
			renderer.RenderWithHelpAndPanels(tabManager, width, height, drawCursor, showHelp, searchPanel, aiPanel)
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
