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
	"unicode/utf8"

	"github.com/javanhut/RavenTerminal/src/aipanel"
	"github.com/javanhut/RavenTerminal/src/aitools"
	"github.com/javanhut/RavenTerminal/src/commands"
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/keybindings"
	"github.com/javanhut/RavenTerminal/src/menu"
	"github.com/javanhut/RavenTerminal/src/ollama"
	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/render"
	"github.com/javanhut/RavenTerminal/src/searchpanel"
	"github.com/javanhut/RavenTerminal/src/tab"
	"github.com/javanhut/RavenTerminal/src/websearch"
	"github.com/javanhut/RavenTerminal/src/window"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// clipboardReadReq ferries OSC 52 clipboard read requests from PTY reader
// goroutines to the main thread (GLFW clipboard access is main-thread only),
// mirroring the DrainClipboard pattern used for writes. Buffer of 1: a second
// concurrent query simply gets an empty reply.
var clipboardReadReq = make(chan chan string, 1)

// requestClipboardRead asks the main loop for the system clipboard contents.
// Called on PTY reader goroutines by the parser (OSC 52 query).
func requestClipboardRead() string {
	reply := make(chan string, 1)
	select {
	case clipboardReadReq <- reply:
	default:
		return ""
	}
	window.PostEmptyEvent() // wake the main loop so it services the request
	select {
	case s := <-reply:
		return s
	case <-time.After(500 * time.Millisecond):
		// ponytail: bounded wait — if the main loop is blocked (e.g. waiting on
		// this pane's reader lock), answer empty instead of stalling the PTY.
		return ""
	}
}

// applyClipboardReadGate wires or unwires OSC 52 clipboard-read answering
// according to allow_clipboard_read (default off — a data-exfiltration
// vector, gated the same way in kitty/wezterm). Called at startup and on
// every config reload so the option can be toggled at runtime.
func applyClipboardReadGate(allow bool) {
	if allow {
		parser.SetDefaultClipboardReader(requestClipboardRead)
	} else {
		parser.SetDefaultClipboardReader(nil)
	}
}

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
	links    []websearch.Link
	source   string
	proxyErr string
	cacheKey string
	err      error
}

const memoCacheMax = 24

// memoCache memoizes up to memoCacheMax values, evicting the oldest
// insertion. Main-thread only (key callbacks and channel drains both run on
// the GLFW thread), so no locking.
// ponytail: session-lifetime cache; Ctrl+R deletes the current entry so retry
// hits the network, other entries live until restart — add a TTL if stale
// data ever bites.
type memoCache[V any] struct {
	entries map[string]V
	order   []string
}

func newMemoCache[V any]() *memoCache[V] {
	return &memoCache[V]{entries: make(map[string]V)}
}

func (c *memoCache[V]) get(key string) (V, bool) {
	v, ok := c.entries[key]
	return v, ok
}

func (c *memoCache[V]) put(key string, v V) {
	if _, ok := c.entries[key]; !ok {
		if len(c.order) >= memoCacheMax {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, key)
	}
	c.entries[key] = v
}

// delete evicts an entry so the next get misses (Ctrl+R re-fetch).
func (c *memoCache[V]) delete(key string) {
	if _, ok := c.entries[key]; !ok {
		return
	}
	delete(c.entries, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// cachedPreview is a previewCache entry: the fetched page plus which source
// (proxy/direct) produced it, so the status line stays honest on hits.
type cachedPreview struct {
	lines  []string
	links  []websearch.Link
	source string
}

type aiResponse struct {
	id       int
	content  string
	thinking string // Thinking content from thinking models
	err      error
	loaded   bool
	token    string // For streaming: incremental token
	done     bool   // For streaming: indicates final response
	toolNote string // Tool activity line to surface in the panel ("web_search: ...")
}

type modelLoadResponse struct {
	url   string
	model string
	err   error
}

type ollamaModelsResponse struct {
	models []string
	err    error
}

// listOllamaModelsAsync fetches the Ollama model list on a goroutine and
// delivers the result on results; it returns immediately so it is safe to
// call from the GLFW main thread.
func listOllamaModelsAsync(baseURL string, timeout time.Duration, results chan<- ollamaModelsResponse) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		client := ollama.NewClient(baseURL, "")
		models, err := client.ListModels(ctx)
		results <- ollamaModelsResponse{models: models, err: err}
	}()
}

// summarizeToolArgs renders tool-call arguments as a short human-readable
// suffix for the panel's activity line, e.g. `: rust async runtime`.
func summarizeToolArgs(args map[string]any) string {
	// Prefer the primary argument each tool has.
	for _, key := range []string{"query", "url", "path", "command"} {
		if v, ok := args[key].(string); ok && strings.TrimSpace(v) != "" {
			v = strings.TrimSpace(v)
			if len([]rune(v)) > 60 {
				v = string([]rune(v)[:57]) + "..."
			}
			return ": " + v
		}
	}
	return ""
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type mouseSelection struct {
	active      bool
	pane        *tab.Pane
	startCol    int
	startAbsRow int // press position in absolute buffer rows (stable across scroll)

	// Multi-click tracking (double=word, triple=line).
	lastClickTime time.Time
	lastClickCol  int
	lastClickRow  int
	clickCount    int
}

// mouseReportState tracks a mouse press that was forwarded to the application
// (mouse tracking modes 1000/1002/1003), so the matching release and drag
// motion go to the same pane, and motion is throttled to cell changes.
type mouseReportState struct {
	pane    *tab.Pane
	held    int // terminal button code held (-1 = none)
	lastCol int
	lastRow int
}

// terminalMouseButton maps a GLFW button to the terminal encoding
// (0=left, 1=middle, 2=right), or -1 for unsupported buttons.
func terminalMouseButton(b glfw.MouseButton) int {
	switch b {
	case glfw.MouseButtonLeft:
		return 0
	case glfw.MouseButtonMiddle:
		return 1
	case glfw.MouseButtonRight:
		return 2
	}
	return -1
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

	// Wake the event-driven main loop whenever a pane produces output, so shell output
	// and key echoes render immediately. Registered before any pane (and its reader
	// goroutine) is created to avoid a data race on the notifier.
	tab.SetWakeNotifier(window.PostEmptyEvent)

	// Create tab manager
	tabManager, err := tab.NewTabManager(uint16(cols), uint16(rows))
	if err != nil {
		log.Fatalf("Failed to create tab manager: %v", err)
	}

	debugMenu := os.Getenv("RAVEN_DEBUG_MENU") == "1"

	// Set up input callbacks
	var currentMods glfw.ModifierKey
	cursorVisible := true
	cursorBlinkOn := true
	lastBlink := time.Now()
	lastInput := time.Now()
	windowFocused := true
	blinkInterval := 530 * time.Millisecond
	lineBuf := &lineBuffer{}
	showHelp := false
	resizeMode := false
	const resizeStep = 0.05
	selection := &mouseSelection{}
	// lastCol/lastRow start at -1 so the first motion event — including one
	// over cell (0,0) — is never throttled as "same cell".
	report := &mouseReportState{held: -1, lastCol: -1, lastRow: -1}
	var lastCursorX float64
	var lastCursorY float64
	var haveCursorPos bool
	lastAutoScroll := time.Time{}
	// clampedPaneCell maps window coordinates to a cell of a specific pane,
	// clamping positions outside the pane rect to its nearest edge cell.
	clampedPaneCell := func(activeTab *tab.Tab, pane *tab.Pane, x, y float64) (int, int, bool) {
		width, height := win.GetFramebufferSize()
		rectX, rectY, rectW, rectH, ok := renderer.PaneRectFor(activeTab, pane, width, height)
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
		cellW, cellH := renderer.CellSize()
		g := pane.Terminal.GetGrid()
		col := clampInt(int((fx-rectX)/cellW), 0, g.Cols-1)
		row := clampInt(int((fy-rectY)/cellH), 0, g.Rows-1)
		return col, row, true
	}

	// mouseCtxFor snapshots a pane's mouse-routing state for DecideMouse.
	mouseCtxFor := func(pane *tab.Pane, shift bool) parser.MouseContext {
		mode, sgr, alt, appCur := pane.Terminal.MouseState()
		return parser.MouseContext{Mode: mode, SGR: sgr, Shift: shift, AltScreen: alt, AppCursorKeys: appCur}
	}

	// reportMousePress forwards a button press to the pane's application when a
	// mouse tracking mode is active (and shift is not held). Returns true when
	// the event was consumed.
	reportMousePress := func(pane *tab.Pane, btn, col, row int, shift bool) bool {
		act := parser.DecideMouse(mouseCtxFor(pane, shift), parser.MousePress, btn, col, row, -1, false)
		if act.Kind != parser.MouseActionSend {
			return false
		}
		report.pane, report.held = pane, btn
		report.lastCol, report.lastRow = col, row
		pane.Write(act.Bytes)
		return true
	}

	toast := &toastState{}
	showToast := func(message string) {
		if strings.TrimSpace(message) == "" {
			return
		}
		toast.message = message
		toast.expiresAt = time.Now().Add(900 * time.Millisecond)
	}
	searchPanel := searchpanel.New()
	searchPanel.LoadHistory(config.LoadSearchHistory())
	bookmarks := config.LoadBookmarks()
	aiPanel := aipanel.New()
	searchResponses := make(chan searchResponse, 4)
	previewResponses := make(chan previewResponse, 4)
	aiResponses := make(chan aiResponse, 4)
	modelLoadResponses := make(chan modelLoadResponse, 2)
	ollamaTestResponses := make(chan ollamaModelsResponse, 2)
	ollamaModelsResponses := make(chan ollamaModelsResponse, 2)
	// 20 (not 8): DDG pagination needs a per-query vqd token and ignores GET
	// offsets, so one bigger first page replaces a load-more key.
	const maxSearchResults = 20
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
		renderer.SetTextStyleOptions(cfg.Appearance.FauxBold, cfg.Appearance.FauxItalic, cfg.Appearance.Undercurl)
		applyClipboardReadGate(cfg.AllowClipboardRead)
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
		// Source the script matching the shell in the active pane: init.sh is
		// bash syntax and breaks fish, which gets its own init.fish.
		cmd := ". " + shellQuote(initPath) + "\n"
		switch activeTab.ShellName() {
		case "fish":
			cmd = "source " + config.FishQuote(config.FishInitPath()) + "\n"
		case "ravenshell":
			// rsh has no source command; the regenerated init.rsh applies to
			// new shells via $RAVEN_INIT_SCRIPT.
			return nil
		}
		return activeTab.Write([]byte(cmd))
	}
	settingsMenu.OnOllamaTest = func(baseURL string) {
		// Off-thread: a blocking call here would freeze the UI (menu shows
		// "Testing..." meanwhile); result applied in the main loop.
		listOllamaModelsAsync(baseURL, 5*time.Second, ollamaTestResponses)
	}
	settingsMenu.OnOllamaFetchModels = func(baseURL string) {
		listOllamaModelsAsync(baseURL, 8*time.Second, ollamaModelsResponses)
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
		renderer.SetTextStyleOptions(settingsMenu.Config.Appearance.FauxBold, settingsMenu.Config.Appearance.FauxItalic, settingsMenu.Config.Appearance.Undercurl)
		applyClipboardReadGate(settingsMenu.Config.AllowClipboardRead)
	}

	searchCache := newMemoCache[[]searchpanel.Result]()
	previewCache := newMemoCache[cachedPreview]()

	// Proxy and direct fetches return different text, so key on both.
	previewCacheKey := func(useProxy bool, url string) string {
		return fmt.Sprintf("%t|%s", useProxy, url)
	}

	// Fallback for heuristic direct-URL queries ("node.js" parses as a host
	// but is really a search term): when the preview fetch for this PreviewID
	// fails, the query is re-run as a normal search instead.
	directFallbackQuery := ""
	directFallbackID := 0

	startPreview := func(result searchpanel.Result) {
		searchPanel.NavPush(result.URL, result.Title)
		searchPanel.Mode = searchpanel.ModePreview
		searchPanel.PreviewTitle = result.Title
		searchPanel.PreviewURL = result.URL
		searchPanel.PreviewLines = nil
		searchPanel.PreviewScroll = 0
		searchPanel.PreviewID++
		useReaderProxy := searchPanel.ProxyEnabled
		cacheKey := previewCacheKey(useReaderProxy, result.URL)
		if entry, ok := previewCache.get(cacheKey); ok {
			searchPanel.SetPreview(result.URL, result.Title, entry.lines, entry.links, nil)
			if entry.source == "proxy" {
				searchPanel.Status = "Source: reader proxy (cached)"
			} else {
				searchPanel.Status = "Source: direct HTML (cached)"
			}
			return
		}
		searchPanel.Status = "Loading preview..."
		searchPanel.StartLoading()
		previewID := searchPanel.PreviewID
		var proxyURLs []string
		if settingsMenu.Config != nil {
			proxyURLs = settingsMenu.Config.WebSearch.ReaderProxyURLs
		}
		go func(id int, url, title string, useProxy bool) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			lines, links, source, proxyErr, err := websearch.FetchText(ctx, url, 12000, useProxy, proxyURLs)
			previewResponses <- previewResponse{id: id, url: url, title: title, lines: lines, links: links, source: source, proxyErr: proxyErr, cacheKey: cacheKey, err: err}
		}(previewID, result.URL, result.Title, useReaderProxy)
	}

	// runSearch always performs a web search (no direct-URL sniffing); it is
	// also the fallback when a heuristic direct-URL preview fails to load.
	runSearch := func(query string) {
		searchPanel.Mode = searchpanel.ModeResults
		searchPanel.Results = nil
		searchPanel.Selected = 0
		searchPanel.ResultsScroll = 0
		searchPanel.ResetHistory()
		searchPanel.ResetNav()
		searchPanel.SearchID++
		if results, ok := searchCache.get(strings.TrimSpace(query)); ok {
			searchPanel.SetResults(query, results, nil)
			searchPanel.AddToHistory(query)
			config.SaveSearchHistory(searchPanel.History)
			searchPanel.Status = fmt.Sprintf("%d results (cached)", len(results))
			return
		}
		searchPanel.Status = "Searching..."
		searchPanel.StartLoading()
		searchID := searchPanel.SearchID
		go func(id int, q string) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			results, err := websearch.SearchDuckDuckGo(ctx, q, maxSearchResults)
			searchResponses <- searchResponse{id: id, query: q, results: results, err: err}
		}(searchID, query)
	}

	startSearch := func(query string) {
		// A query that looks like a URL skips the search and previews the
		// page directly. Schemeless matches are only a heuristic ("node.js"
		// is a search term, not a host), so remember the query and fall back
		// to a real search if that preview fetch fails.
		if u, ok := websearch.DirectURL(query); ok {
			startPreview(searchpanel.Result{Title: u, URL: u})
			q := strings.TrimSpace(query)
			if !strings.HasPrefix(q, "http://") && !strings.HasPrefix(q, "https://") {
				directFallbackQuery = query
				directFallbackID = searchPanel.PreviewID
			}
			return
		}
		runSearch(query)
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

		// History for the API: only the actual conversation. Tool-activity
		// notes and error lines are panel UI, not chat turns.
		messages := make([]ollama.Message, 0, len(aiPanel.Messages)+1)
		toolsEnabled := settingsMenu.Config.Ollama.Tools
		if toolsEnabled {
			messages = append(messages, ollama.Message{Role: "system", Content: aitools.SystemPrompt()})
		}
		for _, msg := range aiPanel.Messages {
			if msg.Role != "user" && msg.Role != "assistant" {
				continue
			}
			messages = append(messages, ollama.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}

		// Tool execution context (working dir follows the active pane).
		workDir := ""
		if at := tabManager.ActiveTab(); at != nil {
			if pane := at.GetActivePane(); pane != nil {
				workDir = pane.CurrentDir()
			}
		}
		toolCfg := aitools.Config{
			UseReaderProxy: settingsMenu.Config.WebSearch.UseReaderProxy,
			ProxyURLs:      settingsMenu.Config.WebSearch.ReaderProxyURLs,
			WorkDir:        workDir,
		}

		// Configure timeout based on thinking mode
		timeout := 180 * time.Second
		if cfg.ThinkingMode && cfg.ExtendedTimeout > 0 {
			timeout = time.Duration(cfg.ExtendedTimeout) * time.Second
		}

		go func(id int, baseURL, model string, messages []ollama.Message, loadModel bool, thinkingEnabled bool, thinkingBudget int, useTools bool, toolCfg aitools.Config) {
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

			var toolDefs []ollama.ToolDef
			var registry *aitools.Registry
			if useTools {
				registry = aitools.NewRegistry(toolCfg)
				for _, t := range registry.Tools() {
					toolDefs = append(toolDefs, ollama.ToolDef{
						Type: "function",
						Function: ollama.ToolDefFunction{
							Name:        t.Name,
							Description: t.Description,
							Parameters:  t.Parameters,
						},
					})
				}
			}

			onToken := func(token string) {
				aiResponses <- aiResponse{id: id, token: token, done: false}
			}

			// Agent loop: stream a response; if the model calls tools, run
			// them (read-only by construction) and continue with the results
			// until it answers in plain text or the round budget runs out.
			const maxToolRounds = 6
			var result ollama.ChatResult
			var err error
			for round := 0; ; round++ {
				result, err = client.ChatStreamWithTools(ctx, messages, toolDefs, onToken, nil)
				if err != nil && len(toolDefs) > 0 && strings.Contains(strings.ToLower(err.Error()), "does not support tools") {
					// Model can't do tool calls; degrade to a plain chat
					// rather than failing the whole conversation.
					toolDefs = nil
					result, err = client.ChatStreamWithTools(ctx, messages, nil, onToken, nil)
				}
				if err != nil || len(result.ToolCalls) == 0 || round >= maxToolRounds {
					break
				}

				messages = append(messages, ollama.Message{
					Role:      "assistant",
					Content:   result.Content,
					ToolCalls: result.ToolCalls,
				})
				for _, call := range result.ToolCalls {
					name := call.Function.Name
					aiResponses <- aiResponse{id: id, toolNote: name + summarizeToolArgs(call.Function.Arguments)}
					output, terr := registry.Execute(ctx, name, call.Function.Arguments)
					if terr != nil {
						output = "Error: " + terr.Error()
					}
					messages = append(messages, ollama.Message{
						Role:     "tool",
						Content:  output,
						ToolName: name,
					})
				}
			}
			aiResponses <- aiResponse{id: id, thinking: result.Thinking, err: err, done: true, loaded: loadSuccess}
		}(requestID, cfg.URL, cfg.Model, messages, needLoad, cfg.ThinkingMode, cfg.ThinkingBudget, toolsEnabled, toolCfg)
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
				return
			}
			if result.Action == keybindings.ActionToggleSearchPanel {
				aiPanel.Open = false
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
			maxChars := max(int(layout.ContentWidth/cellW)-2, 10)
			wrapped := aipanel.BuildWrappedLinesWithThinking(aiPanel.Messages, maxChars, aiPanel.ShowThinking, aiPanel.ThinkingExpanded)
			totalLines := len(wrapped)
			visibleLines := layout.VisibleLines
			maxScroll := max(totalLines-visibleLines, 0)
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

			// Ctrl+Enter: send message (legacy chord, same as plain Enter)
			if mods&glfw.ModControl != 0 && (key == glfw.KeyEnter || key == glfw.KeyKPEnter) {
				if aiPanel.Loading {
					return
				}
				startAIChat(aiPanel.Input)
				return
			}

			switch key {
			case glfw.KeyEscape:
				// Close but keep the conversation; reopening restores it.
				aiPanel.Open = false
				return
			case glfw.KeyEnter, glfw.KeyKPEnter:
				if action == glfw.Repeat {
					return
				}
				// Shift+Enter inserts a newline; plain Enter sends.
				if mods&glfw.ModShift != 0 {
					aiPanel.AppendNewline()
					return
				}
				if aiPanel.Loading {
					return
				}
				startAIChat(aiPanel.Input)
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
				}
				return
			}
			if !searchPanel.Focused {
				// Let terminal handle input while panel stays visible.
				goto handleTerminalInput
			}

			switch result.Action {
			case keybindings.ActionCopy:
				// No selection: leave the clipboard alone.
				if text := activeTab.Terminal.GetGrid().SelectedText(); text != "" {
					glfw.SetClipboardString(text)
					showToast("Copied to clipboard")
				} else {
					showToast("Nothing selected")
				}
				return
			case keybindings.ActionPaste:
				clip := glfw.GetClipboardString()
				if clip != "" {
					writePaste(activeTab.Terminal, activeTab, clip)
					activeTab.Terminal.GetGrid().ResetScrollOffset()
					showToast("Pasted from clipboard")
				}
				return
			}

			width, height := win.GetFramebufferSize()
			cellW, cellH := renderer.CellDimensions()
			layout := searchPanel.Layout(width, height, cellW, cellH)
			previewVisible := max(layout.VisibleLines-1, 1)
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
					// A deliberate source switch must re-fetch, not replay
					// the cached copy for the new proxy state.
					previewCache.delete(previewCacheKey(searchPanel.ProxyEnabled, searchPanel.PreviewURL))
					startPreview(searchpanel.Result{
						Title: searchPanel.PreviewTitle,
						URL:   searchPanel.PreviewURL,
					})
				}
				return
			}

			if mods&glfw.ModControl != 0 && key == glfw.KeyU {
				// While editing the find query, Ctrl+U clears it, not the
				// search query (mirrors the find-aware Backspace).
				if searchPanel.FindEditing {
					searchPanel.FindClear()
					return
				}
				searchPanel.ClearQuery()
				return
			}

			// Ctrl+R: re-run the last search / re-fetch the current preview.
			// Retry must hit the network, so evict the cached copy first.
			if mods&glfw.ModControl != 0 && key == glfw.KeyR {
				if searchPanel.Mode == searchpanel.ModePreview && searchPanel.PreviewURL != "" {
					previewCache.delete(previewCacheKey(searchPanel.ProxyEnabled, searchPanel.PreviewURL))
					startPreview(searchpanel.Result{
						Title: searchPanel.PreviewTitle,
						URL:   searchPanel.PreviewURL,
					})
					return
				}
				q := searchPanel.Query
				if strings.TrimSpace(q) == "" {
					q = searchPanel.LastQuery
				}
				if strings.TrimSpace(q) != "" {
					searchCache.delete(strings.TrimSpace(q))
					startSearch(q)
				}
				return
			}

			// Ctrl+Left / Ctrl+Right: back/forward through previewed pages.
			// (Backspace is not used for back: in preview it still edits the
			// search query, which is existing behavior.)
			if mods&glfw.ModControl != 0 && key == glfw.KeyLeft && searchPanel.Mode == searchpanel.ModePreview {
				if entry, ok := searchPanel.NavBack(); ok {
					searchPanel.PendingScroll = entry.Scroll
					startPreview(searchpanel.Result{Title: entry.Title, URL: entry.URL})
				} else {
					// Back past the first page returns to the results list,
					// mirroring Esc.
					searchPanel.ExitFind()
					searchPanel.Mode = searchpanel.ModeResults
					searchPanel.PreviewScroll = 0
				}
				return
			}
			if mods&glfw.ModControl != 0 && key == glfw.KeyRight && searchPanel.Mode == searchpanel.ModePreview {
				if entry, ok := searchPanel.NavForward(); ok {
					searchPanel.PendingScroll = entry.Scroll
					startPreview(searchpanel.Result{Title: entry.Title, URL: entry.URL})
				}
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

			// Ctrl+Y: copy the previewed / selected result URL to the clipboard
			if mods&glfw.ModControl != 0 && key == glfw.KeyY {
				urlToCopy := searchPanel.GetSelectedURL()
				if searchPanel.Mode == searchpanel.ModePreview {
					urlToCopy = searchPanel.PreviewURL
				}
				if urlToCopy != "" {
					glfw.SetClipboardString(urlToCopy)
					showToast("URL copied")
				}
				return
			}

			// Ctrl+I: insert the preview mouse selection into the shell
			if mods&glfw.ModControl != 0 && key == glfw.KeyI && searchPanel.Mode == searchpanel.ModePreview {
				if text := searchPanel.SelectedPreviewText(); strings.TrimSpace(text) != "" {
					writePaste(activeTab.Terminal, activeTab, text)
					showToast("Inserted into shell")
				}
				return
			}

			// Ctrl+A: send the previewed page (or the mouse selection) to the
			// AI panel for a summary.
			if mods&glfw.ModControl != 0 && key == glfw.KeyA && searchPanel.Mode == searchpanel.ModePreview {
				if !aiPanel.Enabled {
					showToast("Enable Ollama chat in settings")
					return
				}
				if aiPanel.Loading {
					return
				}
				text := strings.Join(searchPanel.PreviewLines, "\n")
				if sel := searchPanel.SelectedPreviewText(); strings.TrimSpace(sel) != "" {
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
				searchPanel.Open = false
				aiPanel.Open = true
				aiPanel.Focused = true
				showHelp = false
				renderer.ResetHelpScroll()
				startAIChat(fmt.Sprintf("Here is the text of %s (%s):\n\n%s\n\nSummarize the key points.",
					searchPanel.PreviewURL, searchPanel.PreviewTitle, text))
				return
			}

			// Ctrl+B: bookmark the previewed page; in results mode, toggle
			// showing the bookmark list as the results.
			if mods&glfw.ModControl != 0 && key == glfw.KeyB {
				if searchPanel.Mode == searchpanel.ModePreview {
					if searchPanel.PreviewURL != "" {
						bookmarks = config.AddBookmark(bookmarks, config.Bookmark{
							Title: searchPanel.PreviewTitle,
							URL:   searchPanel.PreviewURL,
						})
						config.SaveBookmarks(bookmarks)
						showToast("Bookmarked")
					}
					return
				}
				if searchPanel.ShowingBookmarks {
					searchPanel.HideBookmarks()
					return
				}
				results := make([]searchpanel.Result, len(bookmarks))
				for i, b := range bookmarks {
					results[i] = searchpanel.Result{Title: b.Title, URL: b.URL}
				}
				searchPanel.ShowBookmarks(results)
				return
			}

			switch key {
			case glfw.KeyEscape:
				if searchPanel.Mode == searchpanel.ModePreview {
					// Esc peels one layer at a time: find mode, then a
					// persisted mouse selection, then the preview itself.
					if searchPanel.FindActive {
						searchPanel.ExitFind()
						return
					}
					if searchPanel.SelectionActive {
						searchPanel.SelectionActive = false
						return
					}
					searchPanel.Mode = searchpanel.ModeResults
					searchPanel.PreviewScroll = 0
				} else {
					searchPanel.Open = false
				}
				return
			case glfw.KeyEnter, glfw.KeyKPEnter:
				if searchPanel.Mode == searchpanel.ModePreview {
					if searchPanel.FindEditing {
						searchPanel.ConfirmFind(previewVisible)
						return
					}
					// Follow the selected link (Tab cycles) instead of
					// leaving the preview.
					if link, ok := searchPanel.SelectedLinkTarget(); ok {
						title := link.Text
						if title == "" {
							title = link.URL
						}
						startPreview(searchpanel.Result{Title: title, URL: link.URL})
						return
					}
					searchPanel.ExitFind()
					searchPanel.Mode = searchpanel.ModeResults
					searchPanel.PreviewScroll = 0
					return
				}
				if searchPanel.ShowingBookmarks {
					// Enter previews the selected bookmark.
					if searchPanel.Selected >= 0 && searchPanel.Selected < len(searchPanel.Results) {
						startPreview(searchPanel.Results[searchPanel.Selected])
					}
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
				} else if !searchPanel.ShowingBookmarks && (searchPanel.QueryDirty || len(searchPanel.Results) == 0) {
					// Navigate history when editing query
					searchPanel.HistoryUp()
				} else {
					searchPanel.MoveSelection(-1, layout.VisibleLines)
				}
				return
			case glfw.KeyDown:
				if searchPanel.Mode == searchpanel.ModePreview {
					searchPanel.ScrollPreview(1, previewVisible)
				} else if !searchPanel.ShowingBookmarks && searchPanel.HistoryIndex >= 0 {
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
					searchPanel.ExitFind()
					searchPanel.Mode = searchpanel.ModeResults
					searchPanel.PreviewScroll = 0
				}
				return
			case glfw.KeyRight:
				if searchPanel.Mode == searchpanel.ModeResults && !searchPanel.QueryDirty && len(searchPanel.Results) > 0 {
					startPreview(searchPanel.Results[searchPanel.Selected])
				}
				return
			case glfw.KeyTab:
				// Tab / Shift+Tab cycle the extracted page links in preview.
				// Ctrl/Super chords (tab and pane cycling) stay untouched.
				if searchPanel.Mode == searchpanel.ModePreview && mods&(glfw.ModControl|glfw.ModSuper) == 0 {
					delta := 1
					if mods&glfw.ModShift != 0 {
						delta = -1
					}
					searchPanel.CycleLink(delta, previewVisible)
				}
				return
			case glfw.KeyBackspace:
				if searchPanel.FindEditing {
					searchPanel.FindBackspace()
					return
				}
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
				for range 5 {
					renderer.ScrollHelpUp()
				}
				return
			case glfw.KeyPageDown:
				for range 5 {
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
			// Re-fit immediately: the framebuffer-size callback isn't reliably
			// delivered on fullscreen<->windowed (or cross-monitor) transitions,
			// so without this the grid keeps the old column count and a
			// full-screen TUI overflows the new viewport. ponytail: same pattern
			// as the zoom handlers.
			width, height := win.GetFramebufferSize()
			cols, rows := renderer.CalculateGridSize(width, height)
			tabManager.ResizeAll(uint16(cols), uint16(rows))
		case keybindings.ActionCopy:
			// No selection: leave the clipboard alone.
			if text := activeTab.Terminal.GetGrid().SelectedText(); text != "" {
				glfw.SetClipboardString(text)
				showToast("Copied to clipboard")
			} else {
				showToast("Nothing selected")
			}
		case keybindings.ActionPaste:
			clip := glfw.GetClipboardString()
			if clip != "" {
				writePaste(activeTab.Terminal, activeTab, clip)
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
		case keybindings.ActionSelectTab:
			lineBuf.clear()
			tabManager.SelectTab(result.Num - 1)
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
			}
		}
	})

	win.GLFW().SetCharCallback(func(w *glfw.Window, char rune) {
		// Ignore Cmd/Super-modified keys: they are app shortcuts (handled in the key
		// callback), never text input. Prevents e.g. Cmd+T from also typing "t".
		if currentMods&glfw.ModSuper != 0 {
			return
		}

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
			// In preview mode "/" starts in-page find; while find is active,
			// runes edit the find query (or n/N step matches) instead of the
			// search query.
			if searchPanel.Mode == searchpanel.ModePreview {
				width, height := win.GetFramebufferSize()
				cellW, cellH := renderer.CellDimensions()
				layout := searchPanel.Layout(width, height, cellW, cellH)
				previewVisible := max(layout.VisibleLines-1, 1)
				switch {
				case searchPanel.FindEditing:
					searchPanel.AppendFind(char)
				case char == '/':
					searchPanel.StartFind()
				case searchPanel.FindActive && char == 'n':
					searchPanel.FindStep(1, previewVisible)
				case searchPanel.FindActive && char == 'N':
					searchPanel.FindStep(-1, previewVisible)
				default:
					searchPanel.AppendQuery(char)
				}
				return
			}
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
		lastInput = time.Now() // hold the cursor solid right after typing
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
			} else if yoff < 0 {
				g.ScrollViewDown(steps)
			} else {
				return
			}

			// The anchor is absolute (survives the scroll); just extend to the
			// cell under the cursor in the new view.
			x, y := w.GetCursorPos()
			if col, row, ok := clampedPaneCell(activeTab, pane, x, y); ok {
				g.ExtendSelection(col, row)
			}
			renderer.ClearHoverURL()
			return
		}

		if aiPanel.Open && aiPanel.Focused {
			width, height := win.GetFramebufferSize()
			cellW, cellH := renderer.CellDimensions()
			layout := aiPanel.Layout(width, height, cellW, cellH)
			maxChars := max(int(layout.ContentWidth/cellW)-2, 10)
			totalLines := len(aipanel.BuildWrappedLinesWithThinking(aiPanel.Messages, maxChars, aiPanel.ShowThinking, aiPanel.ThinkingExpanded))
			visibleLines := layout.VisibleLines
			maxScroll := max(totalLines-visibleLines, 0)
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
			previewVisible := max(layout.VisibleLines-1, 1)
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

		if yoff == 0 {
			return
		}

		// Route the wheel: app mouse reporting (buttons 64/65), alt-screen
		// arrow-key translation, or local scrollback.
		width, height := win.GetFramebufferSize()
		x, y := w.GetCursorPos()
		if pane, col, row, ok := renderer.HitTestPane(activeTab, x, y, width, height); ok && pane != nil {
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

		// A press that was forwarded to the application gets its matching
		// release forwarded too, wherever the cursor ended up (shift state at
		// release is irrelevant: the app saw the press).
		if action == glfw.Release && report.pane != nil && terminalMouseButton(button) == report.held {
			mode, sgr, _, _ := report.pane.Terminal.MouseState()
			col, row := report.lastCol, report.lastRow
			if c, r, ok := clampedPaneCell(activeTab, report.pane, x, y); ok {
				col, row = c, r
			}
			act := parser.DecideMouse(parser.MouseContext{Mode: mode, SGR: sgr}, parser.MouseRelease, report.held, col, row, -1, false)
			if act.Kind == parser.MouseActionSend {
				report.pane.Write(act.Bytes)
			}
			report.pane, report.held = nil, -1
			report.lastCol, report.lastRow = -1, -1 // don't throttle the next context's first motion
			return
		}

		switch button {
		case glfw.MouseButtonLeft:
			switch action {
			case glfw.Press:
				// Check AI panel first for click-to-focus and text selection
				if aiPanel.Open {
					cellW, cellH := renderer.CellDimensions()
					layout := aiPanel.Layout(width, height, cellW, cellH)
					// Layout is in framebuffer pixels; cursor pos is logical.
					s := renderer.ContentScale()
					fx, fy := float32(x)*s, float32(y)*s
					if fx >= layout.PanelX && fx <= layout.PanelX+layout.PanelWidth &&
						fy >= layout.PanelY && fy <= layout.PanelY+layout.PanelHeight {
						aiPanel.Focused = true
						// Check if click is in message area for text selection
						if fx >= layout.ContentX && fx <= layout.ContentX+layout.ContentWidth &&
							fy >= layout.MessagesStart && fy <= layout.MessagesEnd {
							lineIdx := int((fy-layout.MessagesStart)/layout.LineHeight) - aiPanel.AnchorOffset + aiPanel.Scroll
							aiPanel.SelectionActive = true
							aiPanel.SelectionStart = lineIdx
							aiPanel.SelectionEnd = lineIdx
						}
						return
					}
					// Click is outside AI panel
					aiPanel.Focused = false
				}
				// Check search panel for click-to-focus and click-to-select
				if searchPanel.Open {
					cellW, cellH := renderer.CellDimensions()
					layout := searchPanel.Layout(width, height, cellW, cellH)
					s := renderer.ContentScale()
					fx, fy := float32(x)*s, float32(y)*s
					if fx >= layout.PanelX && fx <= layout.PanelX+layout.PanelWidth &&
						fy >= layout.PanelY && fy <= layout.PanelY+layout.PanelHeight {
						searchPanel.Focused = true
						// Check if click is in results/preview area
						if fx >= layout.ContentX && fx <= layout.ContentX+layout.ContentWidth &&
							fy >= layout.ResultsStart && fy <= layout.ResultsEnd {
							if searchPanel.Mode == searchpanel.ModePreview {
								// Start text selection in preview
								lineIdx := int((fy-layout.ResultsStart-layout.LineHeight)/layout.LineHeight) + searchPanel.PreviewScroll
								searchPanel.SelectionActive = true
								searchPanel.SelectionDragging = true
								searchPanel.SelectionStart = lineIdx
								searchPanel.SelectionEnd = lineIdx
							} else if len(searchPanel.Results) > 0 {
								// Click to select a result
								relY := fy - layout.ResultsStart
								clickedLine := int(relY/layout.LineHeight) + searchPanel.ResultsScroll
								clickedResult := clickedLine / searchPanel.LinesPerResult()
								if clickedResult >= 0 && clickedResult < len(searchPanel.Results) {
									searchPanel.Selected = clickedResult
								}
							}
						}
						return
					}
					// Click is outside search panel
					searchPanel.Focused = false
					searchPanel.SelectionActive = false
				}
				// Tab bar: click a chip to switch tabs, or the "+" to open a new one.
				if idx, newTab, hit := renderer.HitTestTabBar(tabManager, x, y); hit {
					lineBuf.clear()
					if newTab {
						tabManager.NewTab()
					} else {
						tabManager.SelectTab(idx)
					}
					return
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

				// Application mouse reporting (vim/tmux/htop): forward the press
				// unless shift is held (shift = local selection, by convention).
				if reportMousePress(pane, 0, col, row, mods&glfw.ModShift != 0) {
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
				if now.Sub(selection.lastClickTime) <= 400*time.Millisecond &&
					col == selection.lastClickCol && row == selection.lastClickRow {
					selection.clickCount++
				} else {
					selection.clickCount = 1
				}
				selection.lastClickTime = now
				selection.lastClickCol, selection.lastClickRow = col, row

				g := pane.Terminal.GetGrid()
				switch {
				case selection.clickCount == 2: // double-click: word
					g.SelectWordAt(col, row)
					selection.active = false
					selection.pane = pane
					if text := g.SelectedText(); text != "" {
						glfw.SetClipboardString(text)
						showToast("Copied to clipboard")
					}
				case selection.clickCount >= 3: // triple-click: logical line
					g.SelectLineAt(col, row)
					selection.active = false
					selection.pane = pane
					selection.clickCount = 0
					if text := g.SelectedText(); text != "" {
						glfw.SetClipboardString(text)
						showToast("Copied to clipboard")
					}
				default:
					selection.active = true
					selection.pane = pane
					selection.startCol = col
					selection.startAbsRow = g.AbsRowForViewRow(row)
					g.StartSelection(col, row)
				}
				activeTab.SetActivePane(pane)
			case glfw.Release:
				// Handle AI panel text selection release
				if aiPanel.SelectionActive {
					cellW, cellH := renderer.CellDimensions()
					layout := aiPanel.Layout(width, height, cellW, cellH)
					fy := float32(y) * renderer.ContentScale()
					if fy < layout.MessagesStart {
						fy = layout.MessagesStart
					}
					if fy > layout.MessagesEnd {
						fy = layout.MessagesEnd
					}
					endLine := int((fy-layout.MessagesStart)/layout.LineHeight) - aiPanel.AnchorOffset + aiPanel.Scroll
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
				// Handle search panel preview text selection release. A drag
				// selection stays active (highlighted) so Ctrl+A / Ctrl+I can
				// act on it; only the drag tracking stops.
				if searchPanel.SelectionDragging {
					cellW, cellH := renderer.CellDimensions()
					layout := searchPanel.Layout(width, height, cellW, cellH)
					fy := float32(y) * renderer.ContentScale()
					if fy < layout.ResultsStart+layout.LineHeight {
						fy = layout.ResultsStart + layout.LineHeight
					}
					if fy > layout.ResultsEnd {
						fy = layout.ResultsEnd
					}
					searchPanel.SelectionEnd = int((fy-layout.ResultsStart-layout.LineHeight)/layout.LineHeight) + searchPanel.PreviewScroll
					searchPanel.SelectionDragging = false
					// A click with no drag is just focus/positioning: keep no
					// selection and leave the clipboard alone.
					if searchPanel.SelectionStart == searchPanel.SelectionEnd {
						searchPanel.SelectionActive = false
						return
					}
					if text := searchPanel.SelectedPreviewText(); strings.TrimSpace(text) != "" {
						glfw.SetClipboardString(text)
						showToast("Copied to clipboard")
					}
					return
				}
				if !selection.active || selection.pane == nil {
					return
				}

				pane := selection.pane
				g := pane.Terminal.GetGrid()
				col, row, ok := clampedPaneCell(activeTab, pane, x, y)
				if !ok {
					selection.active = false
					return
				}

				// A click (no drag): compare in absolute rows so scrolling
				// mid-press doesn't fake a drag.
				if selection.startCol == col && selection.startAbsRow == g.AbsRowForViewRow(row) {
					g.ClearSelection()
					selection.active = false
					return
				}

				g.ExtendSelection(col, row)
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

			if reportMousePress(pane, 2, col, row, mods&glfw.ModShift != 0) {
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
					showToast("Copied to clipboard")
				}
				return
			}

			clip := glfw.GetClipboardString()
			if clip != "" {
				// Route through writePaste: strips embedded paste-end markers and
				// honors bracketed paste, targeting the pane under the cursor.
				writePaste(pane.Terminal, pane, clip)
				g.ResetScrollOffset()
				showToast("Pasted from clipboard")
			}
		case glfw.MouseButtonMiddle:
			if action != glfw.Press {
				return
			}
			if pane, col, row, ok := renderer.HitTestPane(activeTab, x, y, width, height); ok && pane != nil {
				if reportMousePress(pane, 1, col, row, mods&glfw.ModShift != 0) {
					activeTab.SetActivePane(pane)
				}
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
			fy := float32(ypos) * renderer.ContentScale()
			if fy < layout.MessagesStart {
				fy = layout.MessagesStart
			}
			if fy > layout.MessagesEnd {
				fy = layout.MessagesEnd
			}
			aiPanel.SelectionEnd = int((fy-layout.MessagesStart)/layout.LineHeight) - aiPanel.AnchorOffset + aiPanel.Scroll
			return
		}

		// Track search panel preview text selection during drag
		if searchPanel.SelectionDragging && searchPanel.Open {
			width, height := win.GetFramebufferSize()
			cellW, cellH := renderer.CellDimensions()
			layout := searchPanel.Layout(width, height, cellW, cellH)
			fy := float32(ypos) * renderer.ContentScale()
			if fy < layout.ResultsStart+layout.LineHeight {
				fy = layout.ResultsStart + layout.LineHeight
			}
			if fy > layout.ResultsEnd {
				fy = layout.ResultsEnd
			}
			searchPanel.SelectionEnd = int((fy-layout.ResultsStart-layout.LineHeight)/layout.LineHeight) + searchPanel.PreviewScroll
			return
		}

		if selection.active && selection.pane != nil {
			if col, row, ok := clampedPaneCell(activeTab, selection.pane, xpos, ypos); ok {
				selection.pane.Terminal.GetGrid().ExtendSelection(col, row)
			}
			renderer.ClearHoverURL()
			return
		}

		// Application mouse motion reporting (1002 button-drag / 1003 any-motion),
		// throttled to cell changes. A reported drag targets the pressed pane
		// (clamped to its rect); hover motion targets the pane under the cursor.
		{
			var target *tab.Pane
			var mcol, mrow int
			if report.pane != nil {
				if c, r, ok := clampedPaneCell(activeTab, report.pane, xpos, ypos); ok {
					target, mcol, mrow = report.pane, c, r
				}
			} else {
				width, height := win.GetFramebufferSize()
				if pane, c, r, ok := renderer.HitTestPane(activeTab, xpos, ypos, width, height); ok && pane != nil {
					target, mcol, mrow = pane, c, r
				}
			}
			if target != nil {
				shift := w.GetKey(glfw.KeyLeftShift) == glfw.Press || w.GetKey(glfw.KeyRightShift) == glfw.Press
				held := -1
				if report.pane == target {
					held = report.held
				}
				if held >= 0 {
					// The press was already forwarded: the shift override is
					// latched at press time (xterm behavior), so pressing shift
					// mid-drag doesn't punch a hole in the app's motion stream.
					shift = false
				}
				cellChanged := mcol != report.lastCol || mrow != report.lastRow
				act := parser.DecideMouse(mouseCtxFor(target, shift), parser.MouseMotion, 0, mcol, mrow, held, cellChanged)
				switch act.Kind {
				case parser.MouseActionSend:
					report.lastCol, report.lastRow = mcol, mrow
					target.Write(act.Bytes)
					renderer.ClearHoverURL()
					return
				case parser.MouseActionIgnore:
					renderer.ClearHoverURL()
					return
				}
			}
		}

		width, height := win.GetFramebufferSize()
		pane, col, row, ok := renderer.HitTestPane(activeTab, xpos, ypos, width, height)
		if !ok || pane == nil {
			renderer.ClearHoverURL()
			return
		}

		if _, startCol, endCol := linkAtCell(pane.Terminal.GetGrid(), col, row); startCol <= endCol {
			renderer.SetHoverURL(pane.Terminal.GetGrid(), row, startCol, endCol)
			return
		}
		renderer.ClearHoverURL()
	})

	// Focus reporting (?1004): tell apps that requested it when the window gains/loses focus.
	win.GLFW().SetFocusCallback(func(w *glfw.Window, focused bool) {
		windowFocused = focused // drives cursor-blink pausing on focus loss
		activeTab := tabManager.ActiveTab()
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
	})

	// Redraw-gating state. The full list of redraw triggers lives in ONE place:
	// render.RedrawTriggers. Each loop wake compares against these previous
	// values; when no trigger fires, all GL work (render + SwapBuffers) is
	// skipped, so an idle terminal with cursor blink off does zero draws
	// between events.
	prevDrawCursor := true
	prevToastVisible := false
	// Panel-open states are ORed with their previous value so the frame after
	// a close still renders once, erasing the overlay.
	prevMenuOpen := false
	prevSearchOpen := false
	prevAIOpen := false
	prevHelpOpen := false
	prevFocused := windowFocused
	prevFBWidth, prevFBHeight := -1, -1 // -1 forces a first-frame draw
	prevSyncActive := false
	var prevActiveTab *tab.Tab
	lastScale := float32(0) // 0 forces the content scale to apply on frame one
	// ponytail: entries for closed panes are never pruned; bounded by panes
	// ever opened, a few pointers each.
	lastGrids := make(map[*tab.Pane]*grid.Grid)

	// Main loop
	for !win.ShouldClose() {
		// Check for exited tabs
		tabManager.CleanupExited()
		if tabManager.AllExited() {
			break
		}

		// Show the tab bar only when there's more than one tab. When visibility flips
		// (a tab is added or removed), the usable width changes, so re-fit the grid.
		if wantTabBar := tabManager.TabCount() > 1; wantTabBar != renderer.TabBarVisible() {
			renderer.SetTabBarVisible(wantTabBar)
			width, height := win.GetFramebufferSize()
			cols, rows := renderer.CalculateGridSize(width, height)
			tabManager.ResizeAll(uint16(cols), uint16(rows))
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
					if len(results) > 0 {
						searchCache.put(strings.TrimSpace(resp.query), results)
					}
					// Add successful query to history
					searchPanel.AddToHistory(resp.query)
					config.SaveSearchHistory(searchPanel.History)
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
				if resp.err != nil && resp.id == directFallbackID && directFallbackQuery != "" {
					// The dotted query wasn't a real host after all: run the
					// search it was mistaken for.
					q := directFallbackQuery
					directFallbackQuery = ""
					runSearch(q)
					break
				}
				searchPanel.SetPreview(resp.url, resp.title, resp.lines, resp.links, resp.err)
				if resp.err == nil {
					previewCache.put(resp.cacheKey, cachedPreview{lines: resp.lines, links: resp.links, source: resp.source})
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
					if resp.toolNote != "" {
						// Tool activity: show as its own dim line in the
						// conversation and keep the spinner running.
						aiPanel.AddMessage("tool", resp.toolNote)
						aiPanel.Status = "Running tools..."
						break
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

		// Handle async Ollama menu actions (connection test / model refresh)
		for {
			select {
			case resp := <-ollamaTestResponses:
				if resp.err != nil {
					settingsMenu.StatusMessage = "Ollama test failed: " + resp.err.Error()
				} else {
					settingsMenu.StatusMessage = "Ollama connection OK"
				}
			case resp := <-ollamaModelsResponses:
				if resp.err != nil {
					settingsMenu.StatusMessage = "Model refresh failed: " + resp.err.Error()
				} else {
					settingsMenu.OllamaModels = resp.models
					if len(resp.models) == 0 {
						settingsMenu.StatusMessage = "No models found"
					} else {
						settingsMenu.StatusMessage = fmt.Sprintf("Models loaded (%d)", len(resp.models))
					}
				}
			default:
				goto ollamaMenuDone
			}
		}
	ollamaMenuDone:

		// Handle cursor blinking. Blink only when enabled in config, the active
		// terminal's DECSCUSR style requests it, the window is focused, and the
		// user hasn't just typed (typing forces a solid cursor). Otherwise the
		// cursor is held solid.
		now := time.Now()
		blinkCfg := true
		if settingsMenu.Config != nil {
			blinkCfg = settingsMenu.Config.Appearance.CursorBlink
		}
		styleBlinks := true
		if at := tabManager.ActiveTab(); at != nil && at.Terminal != nil {
			styleBlinks = at.Terminal.CursorBlinking()
		}
		recentlyTyped := now.Sub(lastInput) < blinkInterval
		if parser.EffectiveBlink(blinkCfg, styleBlinks, windowFocused, recentlyTyped) {
			if now.Sub(lastBlink) >= blinkInterval {
				cursorBlinkOn = !cursorBlinkOn
				lastBlink = now
			}
		} else {
			cursorBlinkOn = true // solid
			lastBlink = now
		}
		cursorVisible = cursorBlinkOn

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
								// The anchor is absolute: extending at the edge
								// cell grows the selection into scrollback.
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
								g.ExtendSelection(col, row)
								renderer.ClearHoverURL()
								lastAutoScroll = now
							}
						}
					}
				}
			}
		}

		// Render — gated: a frame is drawn only when a trigger in
		// render.RedrawTriggers fired (the single enumeration of every
		// condition that must force a redraw).
		width, height := win.GetFramebufferSize()

		// HiDPI: apply content-scale changes (startup on a 2x display, or the
		// window moving to a monitor with a different scale). Fonts are
		// re-rasterized at 96*scale DPI and the grid re-fit to the new cells.
		scaleChanged := false
		if s := win.ContentScale(); s != lastScale {
			lastScale = s
			cwOld, chOld := renderer.CellDimensions()
			if err := renderer.SetContentScale(s); err == nil {
				if cw, ch := renderer.CellDimensions(); cw != cwOld || ch != chOld {
					scaleChanged = true
					cols, rows := renderer.CalculateGridSize(width, height)
					tabManager.ResizeAll(uint16(cols), uint16(rows))
				}
			}
		}

		activeTab := tabManager.ActiveTab()
		drawCursor := cursorVisible
		if activeTab != nil && activeTab.Terminal != nil {
			drawCursor = drawCursor && activeTab.Terminal.IsCursorVisible()
		}

		// Peek (without clearing) whether any visible pane changed, and detect
		// active-grid pointer swaps (alt screen enter/exit, new panes) that
		// per-grid dirty tracking cannot see.
		paneDirty := false
		gridSwapped := false
		if activeTab != nil {
			for _, pl := range activeTab.GetPaneLayouts() {
				if pl.Pane == nil || pl.Pane.Terminal == nil {
					continue
				}
				pg := pl.Pane.Terminal.GetGrid()
				if lastGrids[pl.Pane] != pg {
					lastGrids[pl.Pane] = pg
					gridSwapped = true
				}
				if !paneDirty && pg.RedrawNeeded() {
					paneDirty = true
				}
			}
		}

		syncActive := false
		if activeTab != nil && activeTab.Terminal != nil {
			syncActive = activeTab.Terminal.SyncActive()
		}

		toastVisible := now.Before(toast.expiresAt)
		menuOpen := settingsMenu.IsOpen()
		trig := render.RedrawTriggers{
			PaneContentDirty:   paneDirty,
			GridSwapped:        gridSwapped,
			ActiveTabChanged:   activeTab != prevActiveTab,
			CursorPhaseChanged: drawCursor != prevDrawCursor,
			SelectionDragging:  selection.active,
			ToastVisible:       toastVisible,
			ToastJustExpired:   prevToastVisible && !toastVisible,
			MenuOpen:           menuOpen || prevMenuOpen,
			SearchPanelOpen:    searchPanel.Open || prevSearchOpen,
			AIPanelOpen:        aiPanel.Open || prevAIOpen,
			HelpOpen:           showHelp || prevHelpOpen,
			SizeChanged:        width != prevFBWidth || height != prevFBHeight,
			FocusChanged:       windowFocused != prevFocused,
			ScaleChanged:       scaleChanged,
			SyncActiveOrEnded:  syncActive || prevSyncActive,
			UIStateChanged:     renderer.ConsumeUIDirty(),
		}
		prevSyncActive = syncActive
		prevActiveTab = activeTab
		prevDrawCursor = drawCursor
		prevToastVisible = toastVisible
		prevMenuOpen = menuOpen
		prevSearchOpen = searchPanel.Open
		prevAIOpen = aiPanel.Open
		prevHelpOpen = showHelp
		prevFocused = windowFocused
		prevFBWidth, prevFBHeight = width, height

		if render.ShouldRedraw(trig) {
			win.SetViewport(width, height)
			if settingsMenu.IsOpen() {
				renderer.RenderWithMenu(tabManager, width, height, drawCursor, settingsMenu)
			} else {
				renderer.RenderWithHelpAndPanels(tabManager, width, height, drawCursor, showHelp, searchPanel, aiPanel)
			}
			if toastVisible {
				renderer.DrawToast(toast.message, width, height)
			}

			// Swap buffers. While an app holds synchronized output (?2026),
			// skip presenting the partial frame but keep polling input so the
			// UI stays responsive; a watchdog in SyncActive() resumes within
			// ~100ms (the SyncActiveOrEnded trigger guarantees one presented
			// frame after release).
			if !syncActive {
				win.SwapBuffers()
			}
		}

		// Drain any OSC 52 clipboard writes queued from PTY reader goroutines
		// (GLFW clipboard access must happen on the main thread).
		if text, ok := tab.DrainClipboard(); ok {
			glfw.SetClipboardString(text)
		}
		// Answer any pending OSC 52 clipboard read (see requestClipboardRead).
		select {
		case reply := <-clipboardReadReq:
			reply <- glfw.GetClipboardString()
		default:
		}

		// Event-driven wait: returns immediately when a key/mouse event arrives or a
		// pane posts output (see SetWakeNotifier), so input is processed with near-zero
		// latency. The timeout only bounds idle re-renders (cursor blink, toasts) and
		// edge auto-scroll while dragging a selection. This replaces the old fixed
		// PollEvents + 16ms sleep, which serialized input behind the frame timer and,
		// stacked on vsync, doubled keystroke-to-pixel latency.
		waitTimeout := 0.03
		if selection.active {
			waitTimeout = 0.016 // keep edge auto-scroll smooth during a drag
		}
		window.WaitEventsTimeout(waitTimeout)
	}
}

// writePaste sends clipboard text to a terminal, normalizing newlines and,
// when the application has enabled bracketed paste (?2004), wrapping the text in
// paste markers. Any embedded end-marker is stripped first to prevent
// paste-injection. The target is the terminal's PTY writer (a *tab.Tab or a
// *tab.Pane), so split panes paste into the pane under the cursor.
func writePaste(term *parser.Terminal, target interface{ Write([]byte) error }, clip string) {
	clip = strings.ReplaceAll(clip, "\r\n", "\n")
	clip = strings.ReplaceAll(clip, "\n", "\r")
	if term.BracketedPasteEnabled() {
		clip = strings.ReplaceAll(clip, "\x1b[201~", "")
		clip = "\x1b[200~" + clip + "\x1b[201~"
	}
	target.Write([]byte(clip))
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

// linkAtCell resolves a clickable URL at (col,row), preferring an explicit OSC 8
// hyperlink and falling back to heuristic URL detection. The returned column range
// spans the run of cells sharing the link, for hover underlining.
func linkAtCell(g *grid.Grid, col, row int) (string, int, int) {
	if g == nil || row < 0 || row >= g.Rows || col < 0 || col >= g.Cols {
		return "", -1, -1
	}
	cell := g.DisplayCell(col, row)
	if cell.Link != 0 {
		if url := g.LinkURL(cell.Link); url != "" {
			start, end := col, col
			for c := col - 1; c >= 0; c-- {
				if g.DisplayCell(c, row).Link != cell.Link {
					break
				}
				start = c
			}
			for c := col + 1; c < g.Cols; c++ {
				if g.DisplayCell(c, row).Link != cell.Link {
					break
				}
				end = c
			}
			return url, start, end
		}
	}
	return urlAtCellRange(g, col, row)
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
