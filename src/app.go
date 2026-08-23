package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/javanhut/RavenTerminal/src/aipanel"
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/menu"
	"github.com/javanhut/RavenTerminal/src/ollama"
	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/render"
	"github.com/javanhut/RavenTerminal/src/searchpanel"
	"github.com/javanhut/RavenTerminal/src/tab"
	"github.com/javanhut/RavenTerminal/src/window"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// App owns the terminal's runtime state: window, renderer, tab manager, side
// panels, and all input/redraw state. Everything runs on the GLFW main thread
// except the PTY reader goroutines, which communicate through the *Response
// channels and the wake notifier.
type App struct {
	win        *window.Window
	renderer   *render.Renderer
	tabManager *tab.TabManager
	debugMenu  bool
	// primary marks the window the process started with. Only it reads and
	// writes the session file — otherwise every open window would race to
	// overwrite the same layout with its own.
	primary bool

	// Input and cursor state.
	currentMods    glfw.ModifierKey
	cursorVisible  bool
	cursorBlinkOn  bool
	lastBlink      time.Time
	lastInput      time.Time
	windowFocused  bool
	blinkInterval  time.Duration
	lineBuf        *lineBuffer
	showHelp       bool
	resizeMode     bool
	selection      *mouseSelection
	report         *mouseReportState
	tabDrag        tabDragState
	find           findState
	lastCursorX    float64
	lastCursorY    float64
	haveCursorPos  bool
	lastAutoScroll time.Time

	// Panels and their async response channels.
	toast                 *toastState
	searchPanel           *searchpanel.Panel
	bookmarks             []config.Bookmark
	aiPanel               *aipanel.Panel
	searchResponses       chan searchResponse
	previewResponses      chan previewResponse
	aiResponses           chan aiResponse
	modelLoadResponses    chan modelLoadResponse
	ollamaTestResponses   chan ollamaModelsResponse
	ollamaModelsResponses chan ollamaModelsResponse

	settingsMenu *menu.Menu
	currentTheme string

	searchCache  *memoCache[[]searchpanel.Result]
	previewCache *memoCache[cachedPreview]

	// Fallback for heuristic direct-URL queries ("node.js" parses as a host
	// but is really a search term): when the preview fetch for this PreviewID
	// fails, the query is re-run as a normal search instead.
	directFallbackQuery string
	directFallbackID    int

	// Redraw-gating state. The full list of redraw triggers lives in ONE place:
	// render.RedrawTriggers. Each loop wake compares against these previous
	// values; when no trigger fires, all GL work (render + SwapBuffers) is
	// skipped, so an idle terminal with cursor blink off does zero draws
	// between events.
	prevDrawCursor   bool
	prevToastVisible bool
	// Panel-open states are ORed with their previous value so the frame after
	// a close still renders once, erasing the overlay.
	prevMenuOpen   bool
	prevSearchOpen bool
	prevAIOpen     bool
	prevHelpOpen   bool
	prevFindOpen   bool
	prevFocused    bool
	prevFBWidth    int
	prevFBHeight   int
	prevSyncActive bool
	prevActiveTab  *tab.Tab
	lastScale      float32 // 0 forces the content scale to apply on frame one
	// Grid size last pushed to the tabs, so fitGrid can skip the PTY ioctls
	// when nothing actually changed.
	fitCols uint16
	fitRows uint16
	// ponytail: entries for closed panes are never pruned; bounded by panes
	// ever opened, a few pointers each.
	lastGrids map[*tab.Pane]*grid.Grid
}

// newApp builds the process's first window: it owns session restore and
// session saving, which is why it is flagged primary.
func newApp() *App {
	// Wake the event-driven main loop whenever a pane produces output, so shell output
	// and key echoes render immediately. Registered before any pane (and its reader
	// goroutine) is created to avoid a data race on the notifier.
	tab.SetWakeNotifier(window.PostEmptyEvent)

	a, err := newAppWith(func(cols, rows uint16) (*tab.TabManager, error) {
		// Reopen the previous session's layout when the user has that enabled.
		// The config is read directly here because the settings menu (which
		// normally owns it) is not built until after the tab manager exists.
		restore := false
		if cfg, err := config.Load(); err == nil && cfg != nil {
			restore = cfg.RestoreSession
		}
		return restoredTabManager(cols, rows, restore)
	})
	if err != nil {
		log.Fatalf("Failed to create window: %v", err)
	}
	a.primary = true
	return a
}

// newAppWith builds a window, its renderer, and its tabs. tabsFor receives the
// grid size the new window can display, so a restored or adopted tab strip is
// sized correctly from its first frame.
func newAppWith(tabsFor func(cols, rows uint16) (*tab.TabManager, error)) (*App, error) {
	// Create window. This also makes the new window's GL context current,
	// which the renderer below is built against.
	winConfig := window.DefaultConfig()
	win, err := window.NewWindow(winConfig)
	if err != nil {
		return nil, err
	}

	// Create renderer
	renderer, err := render.NewRenderer()
	if err != nil {
		win.Destroy()
		return nil, err
	}

	// Calculate initial grid size
	width, height := win.GetFramebufferSize()
	cols, rows := renderer.CalculateGridSize(width, height)

	tabManager, err := tabsFor(uint16(cols), uint16(rows))
	if err != nil {
		renderer.Destroy()
		win.Destroy()
		return nil, err
	}

	a := &App{
		win:        win,
		renderer:   renderer,
		tabManager: tabManager,
		debugMenu:  os.Getenv("RAVEN_DEBUG_MENU") == "1",

		cursorVisible: true,
		cursorBlinkOn: true,
		lastBlink:     time.Now(),
		lastInput:     time.Now(),
		windowFocused: true,
		blinkInterval: 530 * time.Millisecond,
		lineBuf:       &lineBuffer{},
		selection:     &mouseSelection{},
		// lastCol/lastRow start at -1 so the first motion event — including one
		// over cell (0,0) — is never throttled as "same cell".
		report: &mouseReportState{held: -1, lastCol: -1, lastRow: -1},

		toast:                 &toastState{},
		searchResponses:       make(chan searchResponse, 4),
		previewResponses:      make(chan previewResponse, 4),
		aiResponses:           make(chan aiResponse, 4),
		modelLoadResponses:    make(chan modelLoadResponse, 2),
		ollamaTestResponses:   make(chan ollamaModelsResponse, 2),
		ollamaModelsResponses: make(chan ollamaModelsResponse, 2),

		fitCols: uint16(cols),
		fitRows: uint16(rows),

		prevDrawCursor: true,
		prevFocused:    true, // matches windowFocused
		// -1 forces a first-frame draw
		prevFBWidth:  -1,
		prevFBHeight: -1,
		lastGrids:    make(map[*tab.Pane]*grid.Grid),
	}

	a.searchPanel = searchpanel.New()
	a.searchPanel.LoadHistory(config.LoadSearchHistory())
	a.bookmarks = config.LoadBookmarks()
	a.aiPanel = aipanel.New()
	a.settingsMenu = menu.NewMenu()
	a.wireSettingsMenu()
	a.applyInitialConfig()

	a.searchCache = newMemoCache[[]searchpanel.Result]()
	a.previewCache = newMemoCache[cachedPreview]()

	return a, nil
}

// Destroy releases the renderer and window, in the same order the deferred
// calls in the old main() did.
func (a *App) Destroy() {
	a.renderer.Destroy()
	a.win.Destroy()
}

// saveSessionIfEnabled persists the tab layout on exit when the user has
// restore turned on. Gated on the setting so a user who does not want session
// restore never has a session file written for them.
func (a *App) saveSessionIfEnabled() {
	if a.settingsMenu.Config != nil && a.settingsMenu.Config.RestoreSession {
		a.saveSession()
	}
}

// wireSettingsMenu installs the menu's action hooks. They fire from menu
// interactions later, never during setup.
func (a *App) wireSettingsMenu() {
	a.settingsMenu.OnConfigReload = a.onConfigReload
	a.settingsMenu.OnInitScriptUpdated = a.onInitScriptUpdated
	a.settingsMenu.OnOllamaTest = a.onOllamaTest
	a.settingsMenu.OnOllamaFetchModels = a.onOllamaFetchModels
	a.settingsMenu.OnOllamaLoadModel = a.onOllamaLoadModel
}

func (a *App) onConfigReload(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	a.searchPanel.SetEnabled(cfg.WebSearch.Enabled)
	a.aiPanel.SetEnabled(cfg.Ollama.Enabled)
	a.searchPanel.WidthPercent = cfg.Appearance.PanelWidthPercent
	a.aiPanel.WidthPercent = cfg.Appearance.PanelWidthPercent
	a.aiPanel.ShowThinking = cfg.Ollama.ShowThinking
	a.aiPanel.ThinkingMode = cfg.Ollama.ThinkingMode
	a.settingsMenu.OllamaModels = nil
	if a.aiPanel.LoadedURL != cfg.Ollama.URL || a.aiPanel.LoadedModel != cfg.Ollama.Model {
		a.aiPanel.ModelLoaded = false
		a.aiPanel.LoadedURL = cfg.Ollama.URL
		a.aiPanel.LoadedModel = cfg.Ollama.Model
	}
	a.renderer.SetThemeByName(cfg.Theme)
	if err := a.renderer.SetDefaultFontSize(cfg.FontSize); err != nil {
		return err
	}
	a.renderer.SetTextStyleOptions(cfg.Appearance.FauxBold, cfg.Appearance.FauxItalic, cfg.Appearance.Undercurl)
	applyClipboardReadGate(cfg.AllowClipboardRead)
	a.fitGrid()
	return nil
}

func (a *App) onInitScriptUpdated(initPath string) error {
	if initPath == "" {
		return nil
	}
	activeTab := a.tabManager.ActiveTab()
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

func (a *App) onOllamaTest(baseURL string) {
	// Off-thread: a blocking call here would freeze the UI (menu shows
	// "Testing..." meanwhile); result applied in the main loop.
	listOllamaModelsAsync(baseURL, 5*time.Second, a.ollamaTestResponses)
}

func (a *App) onOllamaFetchModels(baseURL string) {
	listOllamaModelsAsync(baseURL, 8*time.Second, a.ollamaModelsResponses)
}

func (a *App) onOllamaLoadModel(baseURL, model string) {
	// Show loading status immediately
	a.aiPanel.Status = "Loading model..."
	a.aiPanel.ModelLoaded = false
	// Load model in background
	go func(url, m string) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second) // 5 min for slow remote APIs
		defer cancel()
		client := ollama.NewClient(url, m)
		err := client.LoadModel(ctx)
		a.modelLoadResponses <- modelLoadResponse{url: url, model: m, err: err}
	}(baseURL, model)
}

// applyInitialConfig applies the loaded config once at startup, mirroring
// what OnConfigReload does on later changes.
func (a *App) applyInitialConfig() {
	a.currentTheme = ""
	if a.settingsMenu.Config != nil {
		a.currentTheme = a.settingsMenu.Config.Theme
		a.searchPanel.SetEnabled(a.settingsMenu.Config.WebSearch.Enabled)
		a.aiPanel.SetEnabled(a.settingsMenu.Config.Ollama.Enabled)
		a.searchPanel.WidthPercent = a.settingsMenu.Config.Appearance.PanelWidthPercent
		a.aiPanel.WidthPercent = a.settingsMenu.Config.Appearance.PanelWidthPercent
		a.aiPanel.ShowThinking = a.settingsMenu.Config.Ollama.ShowThinking
		a.aiPanel.ThinkingMode = a.settingsMenu.Config.Ollama.ThinkingMode
		a.aiPanel.LoadedURL = a.settingsMenu.Config.Ollama.URL
		a.aiPanel.LoadedModel = a.settingsMenu.Config.Ollama.Model
		a.renderer.SetThemeByName(a.currentTheme)
		if err := a.renderer.SetDefaultFontSize(a.settingsMenu.Config.FontSize); err == nil {
			a.fitGrid()
		}
		a.renderer.SetTextStyleOptions(a.settingsMenu.Config.Appearance.FauxBold, a.settingsMenu.Config.Appearance.FauxItalic, a.settingsMenu.Config.Appearance.Undercurl)
		applyClipboardReadGate(a.settingsMenu.Config.AllowClipboardRead)
	}
}

func (a *App) registerCallbacks() {
	a.win.GLFW().SetKeyCallback(a.onKey)
	a.win.GLFW().SetCharCallback(a.onChar)
	a.win.GLFW().SetFramebufferSizeCallback(a.onFramebufferSize)
	a.win.GLFW().SetScrollCallback(a.onScroll)
	a.win.GLFW().SetMouseButtonCallback(a.onMouseButton)
	a.win.GLFW().SetCursorPosCallback(a.onCursorPos)
	a.win.GLFW().SetFocusCallback(a.onFocus)
}

// tick runs one main-loop iteration for this window: state updates, async
// drains, and a frame (which the redraw triggers may skip). It returns false
// when the window should close — the user closed it, or every tab exited.
func (a *App) tick(now time.Time) bool {
	if a.win.ShouldClose() {
		return false
	}

	// Check for exited tabs
	a.tabManager.CleanupExited()
	if a.tabManager.AllExited() {
		return false
	}

	// Bind this window's GL context before any renderer call: with several
	// windows open the context left current by the previous tick belongs to a
	// different window, and its draws would land there.
	a.win.MakeContextCurrent()

	{
		// Show the tab bar only when there's more than one tab. When visibility
		// flips (a tab is added or removed), the usable width changes — the
		// fitGrid below picks that up along with everything else.
		if wantTabBar := a.tabManager.TabCount() > 1; wantTabBar != a.renderer.TabBarVisible() {
			a.renderer.SetTabBarVisible(wantTabBar)
		}
		a.fitGrid()

		if a.settingsMenu.Config != nil && a.settingsMenu.Config.Theme != a.currentTheme {
			a.renderer.SetThemeByName(a.settingsMenu.Config.Theme)
			a.currentTheme = a.settingsMenu.Config.Theme
		}
		if a.settingsMenu.Config != nil {
			a.searchPanel.SetEnabled(a.settingsMenu.Config.WebSearch.Enabled)
			if !a.searchPanel.Open {
				a.searchPanel.ProxyEnabled = a.settingsMenu.Config.WebSearch.UseReaderProxy
			}
		}

		a.drainSearchResponses()
		a.drainPreviewResponses()
		a.drainAIResponses()
		a.drainModelLoadResponses()
		a.drainOllamaMenuResponses()

		a.updateCursorBlink(now)
		a.autoScrollSelection(now)
		a.renderFrame(now)

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
	}
	return true
}

// fitGrid re-fits every tab to the window's current framebuffer size. It is the
// single authority for grid size, and it runs every tick, so the grid converges
// on the real window size no matter how the size changed.
//
// The framebuffer callback alone was not enough. Around fullscreen transitions
// it fires at the wrong moments: on macOS the transition is animated and
// asynchronous, so the size read immediately after toggling is still the old
// one, and glfwSetWindowMonitor pumps the event loop from inside itself. When
// the callback landed before the OS had applied the new frame, nothing ever
// corrected the grid — the renderer drew at the new pixel size while the PTY
// still believed the old cols/rows, which is the "sometimes it breaks" the
// scattered re-fits could not cover. Polling instead of trusting one event
// makes every path self-healing.
//
// It also subsumes the other re-fit sites — zoom, font size, content scale, tab
// bar visibility — since all of them land in CalculateGridSize.
func (a *App) fitGrid() {
	width, height := a.win.GetFramebufferSize()
	cols, rows := a.renderer.CalculateGridSize(width, height)
	if uint16(cols) == a.fitCols && uint16(rows) == a.fitRows {
		return // unchanged: skip the per-pane TIOCSWINSZ and SIGWINCH
	}
	a.fitCols, a.fitRows = uint16(cols), uint16(rows)
	a.tabManager.ResizeAll(a.fitCols, a.fitRows)
}

// idleTimeout is the longest this window is willing to sleep before it needs
// another frame. The main loop waits for the shortest across all windows.
func (a *App) idleTimeout() float64 {
	if a.selection.active {
		return 0.016 // keep edge auto-scroll smooth during a drag
	}
	return 0.03
}

// shutdown closes one window, saving the session first if it owns it.
func (a *App) shutdown() {
	a.saveSessionIfEnabled()
	// Destroying GL objects needs this window's own context current.
	a.win.MakeContextCurrent()
	a.renderer.Destroy()
	a.win.Destroy()
}

// runApps drives every open window from the one GLFW thread until the last
// one closes. Each pass ticks all windows, then blocks once in
// WaitEventsTimeout — the wait is shared, so N windows do not mean N waits.
//
// Event-driven: the wait returns immediately when a key/mouse event arrives or
// a pane posts output (see SetWakeNotifier), so input is processed with
// near-zero latency. The timeout only bounds idle re-renders (cursor blink,
// toasts) and edge auto-scroll while dragging a selection. This replaces the
// old fixed PollEvents + 16ms sleep, which serialized input behind the frame
// timer and, stacked on vsync, doubled keystroke-to-pixel latency.
func runApps(first *App) {
	defer window.Terminate()

	apps := []*App{first}
	first.registerCallbacks()

	for len(apps) > 0 {
		now := time.Now()
		// Consume any outstanding output wake before rendering: chunks that
		// arrived earlier are rendered below, and chunks arriving after this
		// point re-arm the wake so WaitEventsTimeout returns immediately. It is
		// process-wide, so it is cleared once per pass rather than per window.
		tab.ClearWakePending()

		timeout := 1.0
		live := apps[:0]
		for _, a := range apps {
			if !a.tick(now) {
				a.shutdown()
				continue
			}
			timeout = min(timeout, a.idleTimeout())
			live = append(live, a)
		}
		apps = live

		// Windows torn off a tab strip during this pass are created here, on
		// the loop thread, rather than inside the mouse callback that
		// requested them — creating a GLFW window while dispatching its events
		// is not worth the risk.
		for _, t := range takePendingDetach() {
			a, err := newDetachedApp(t)
			if err != nil {
				// The tab has already left its old strip, so hand it to a
				// window that still exists rather than dropping the user's
				// running shell on the floor.
				log.Printf("tear-off failed, returning tab to an open window: %v", err)
				if len(apps) > 0 {
					apps[0].tabManager.AdoptTab(t)
				} else {
					t.Close()
				}
				continue
			}
			a.registerCallbacks()
			apps = append(apps, a)
		}

		if len(apps) == 0 {
			break
		}
		window.WaitEventsTimeout(timeout)
	}
}

func (a *App) drainSearchResponses() {
	for {
		select {
		case resp := <-a.searchResponses:
			if resp.id != a.searchPanel.SearchID {
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
			a.searchPanel.SetResults(resp.query, results, resp.err)
			if resp.err == nil {
				if len(results) > 0 {
					a.searchCache.put(strings.TrimSpace(resp.query), results)
				}
				// Add successful query to history
				a.searchPanel.AddToHistory(resp.query)
				config.SaveSearchHistory(a.searchPanel.History)
				if len(results) == 0 {
					a.searchPanel.Status = "No results"
				} else {
					a.searchPanel.Status = fmt.Sprintf("%d results", len(results))
				}
			}
		default:
			return
		}
	}
}

func (a *App) drainPreviewResponses() {
	for {
		select {
		case resp := <-a.previewResponses:
			if resp.id != a.searchPanel.PreviewID {
				break
			}
			if resp.err != nil && resp.id == a.directFallbackID && a.directFallbackQuery != "" {
				// The dotted query wasn't a real host after all: run the
				// search it was mistaken for.
				q := a.directFallbackQuery
				a.directFallbackQuery = ""
				a.runSearch(q)
				break
			}
			a.searchPanel.SetPreview(resp.url, resp.title, resp.lines, resp.links, resp.err)
			if resp.err == nil {
				a.previewCache.put(resp.cacheKey, cachedPreview{lines: resp.lines, links: resp.links, source: resp.source})
				if resp.source == "proxy" {
					a.searchPanel.Status = "Source: reader proxy"
				} else {
					a.searchPanel.Status = "Source: direct HTML"
				}
				if resp.proxyErr != "" && resp.source != "proxy" {
					a.searchPanel.Status = "Proxy failed: " + resp.proxyErr
				}
			}
		default:
			return
		}
	}
}

func (a *App) drainAIResponses() {
	for {
		select {
		case resp := <-a.aiResponses:
			if resp.id != a.aiPanel.RequestID {
				break
			}
			if !resp.done {
				if resp.loaded {
					// Model finished loading, now generating
					a.aiPanel.Status = "Thinking..."
				}
				if resp.toolNote != "" {
					// Tool activity: show as its own dim line in the
					// conversation and keep the spinner running.
					a.aiPanel.AddMessage("tool", resp.toolNote)
					a.aiPanel.Status = "Running tools..."
					break
				}
				// Streaming token - append to assistant message
				if resp.token != "" {
					a.aiPanel.Status = ""
					a.aiPanel.AppendToLastMessage("assistant", resp.token)
				}
				break
			}
			// Final response
			a.aiPanel.Loading = false
			if resp.err != nil {
				a.aiPanel.Status = "Error occurred"
				a.aiPanel.AddMessage("error", resp.err.Error())
				break
			}
			a.aiPanel.Status = ""

			// Add thinking content to the last assistant message if present
			if resp.thinking != "" && len(a.aiPanel.Messages) > 0 {
				lastIdx := len(a.aiPanel.Messages) - 1
				if a.aiPanel.Messages[lastIdx].Role == "assistant" {
					a.aiPanel.Messages[lastIdx].Thinking = resp.thinking
				}
			}

			a.aiPanel.TrimMessages(maxChatMessages)
			if resp.loaded {
				if a.settingsMenu.Config != nil {
					a.aiPanel.ModelLoaded = true
					a.aiPanel.LoadedURL = a.settingsMenu.Config.Ollama.URL
					a.aiPanel.LoadedModel = a.settingsMenu.Config.Ollama.Model
				}
			}
		default:
			return
		}
	}
}

func (a *App) drainModelLoadResponses() {
	for {
		select {
		case resp := <-a.modelLoadResponses:
			if resp.err != nil {
				a.aiPanel.Status = "Load failed"
				a.aiPanel.AddMessage("error", "Failed to load model: "+resp.err.Error())
				a.aiPanel.ModelLoaded = false
			} else {
				a.aiPanel.Status = "Model Loaded: " + resp.model
				a.aiPanel.ModelLoaded = true
				a.aiPanel.LoadedURL = resp.url
				a.aiPanel.LoadedModel = resp.model
			}
		default:
			return
		}
	}
}

// drainOllamaMenuResponses handles async Ollama menu actions (connection
// test / model refresh).
func (a *App) drainOllamaMenuResponses() {
	for {
		select {
		case resp := <-a.ollamaTestResponses:
			if resp.err != nil {
				a.settingsMenu.StatusMessage = "Ollama test failed: " + resp.err.Error()
			} else {
				a.settingsMenu.StatusMessage = "Ollama connection OK"
			}
		case resp := <-a.ollamaModelsResponses:
			if resp.err != nil {
				a.settingsMenu.StatusMessage = "Model refresh failed: " + resp.err.Error()
			} else {
				a.settingsMenu.OllamaModels = resp.models
				if len(resp.models) == 0 {
					a.settingsMenu.StatusMessage = "No models found"
				} else {
					a.settingsMenu.StatusMessage = fmt.Sprintf("Models loaded (%d)", len(resp.models))
				}
			}
		default:
			return
		}
	}
}

// updateCursorBlink handles cursor blinking. Blink only when enabled in
// config, the active terminal's DECSCUSR style requests it, the window is
// focused, and the user hasn't just typed (typing forces a solid cursor).
// Otherwise the cursor is held solid.
func (a *App) updateCursorBlink(now time.Time) {
	blinkCfg := true
	if a.settingsMenu.Config != nil {
		blinkCfg = a.settingsMenu.Config.Appearance.CursorBlink
	}
	styleBlinks := true
	if at := a.tabManager.ActiveTab(); at != nil && at.Terminal != nil {
		styleBlinks = at.Terminal.CursorBlinking()
	}
	recentlyTyped := now.Sub(a.lastInput) < a.blinkInterval
	if parser.EffectiveBlink(blinkCfg, styleBlinks, a.windowFocused, recentlyTyped) {
		if now.Sub(a.lastBlink) >= a.blinkInterval {
			a.cursorBlinkOn = !a.cursorBlinkOn
			a.lastBlink = now
		}
	} else {
		a.cursorBlinkOn = true // solid
		a.lastBlink = now
	}
	a.cursorVisible = a.cursorBlinkOn
}

// autoScrollSelection scrolls the pane while a drag selection is held at its
// top or bottom edge, extending the selection into scrollback.
func (a *App) autoScrollSelection(now time.Time) {
	if !a.selection.active || a.selection.pane == nil || !a.haveCursorPos {
		return
	}
	if now.Sub(a.lastAutoScroll) < time.Millisecond*50 {
		return
	}
	activeTab := a.tabManager.ActiveTab()
	if activeTab == nil {
		return
	}
	width, height := a.win.GetFramebufferSize()
	rectX, rectY, rectW, rectH, ok := a.renderer.PaneRectFor(activeTab, a.selection.pane, width, height)
	if !ok {
		return
	}
	cellW, cellH := a.renderer.CellSize()
	edge := float64(cellH)
	var dir int
	if a.lastCursorY < float64(rectY)+edge {
		dir = -1
	} else if a.lastCursorY > float64(rectY+rectH)-edge {
		dir = 1
	}
	if dir == 0 {
		return
	}
	g := a.selection.pane.Terminal.GetGrid()
	prevOffset := g.GetScrollOffset()
	if dir < 0 {
		g.ScrollViewUp(1)
	} else {
		g.ScrollViewDown(1)
	}
	if g.GetScrollOffset() == prevOffset {
		return
	}
	// The anchor is absolute: extending at the edge cell grows the selection
	// into scrollback.
	fx := float32(a.lastCursorX)
	fy := float32(a.lastCursorY)
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
	a.renderer.ClearHoverURL()
	a.lastAutoScroll = now
}

// renderFrame draws a frame — gated: a frame is drawn only when a trigger in
// render.RedrawTriggers fired (the single enumeration of every condition that
// must force a redraw).
func (a *App) renderFrame(now time.Time) {
	width, height := a.win.GetFramebufferSize()

	// HiDPI: apply content-scale changes (startup on a 2x display, or the
	// window moving to a monitor with a different scale). Fonts are
	// re-rasterized at 96*scale DPI and the grid re-fit to the new cells.
	scaleChanged := false
	if s := a.win.ContentScale(); s != a.lastScale {
		a.lastScale = s
		cwOld, chOld := a.renderer.CellDimensions()
		if err := a.renderer.SetContentScale(s); err == nil {
			if cw, ch := a.renderer.CellDimensions(); cw != cwOld || ch != chOld {
				scaleChanged = true
				a.fitGrid()
			}
		}
	}

	activeTab := a.tabManager.ActiveTab()
	// A find is bound to one pane's grid (its matches are that buffer's
	// absolute rows), so it cannot survive a tab switch. Closing it here
	// rather than at each of the tab-switch call sites — key bindings, tab-bar
	// clicks, tab close — keeps the rule in one place. The FindBarOpen trigger
	// below still sees prevFindOpen, so the bar is erased on this frame.
	if a.find.open && activeTab != a.prevActiveTab {
		a.closeFind()
	}

	drawCursor := a.cursorVisible
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
			if a.lastGrids[pl.Pane] != pg {
				a.lastGrids[pl.Pane] = pg
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

	toastVisible := now.Before(a.toast.expiresAt)
	menuOpen := a.settingsMenu.IsOpen()
	trig := render.RedrawTriggers{
		PaneContentDirty:   paneDirty,
		GridSwapped:        gridSwapped,
		ActiveTabChanged:   activeTab != a.prevActiveTab,
		CursorPhaseChanged: drawCursor != a.prevDrawCursor,
		SelectionDragging:  a.selection.active,
		TabDragging:        a.tabDrag.active,
		ToastVisible:       toastVisible,
		ToastJustExpired:   a.prevToastVisible && !toastVisible,
		MenuOpen:           menuOpen || a.prevMenuOpen,
		SearchPanelOpen:    a.searchPanel.Open || a.prevSearchOpen,
		AIPanelOpen:        a.aiPanel.Open || a.prevAIOpen,
		HelpOpen:           a.showHelp || a.prevHelpOpen,
		FindBarOpen:        a.find.open || a.prevFindOpen,
		SizeChanged:        width != a.prevFBWidth || height != a.prevFBHeight,
		FocusChanged:       a.windowFocused != a.prevFocused,
		ScaleChanged:       scaleChanged,
		SyncActiveOrEnded:  syncActive || a.prevSyncActive,
		UIStateChanged:     a.renderer.ConsumeUIDirty(),
	}
	a.prevSyncActive = syncActive
	a.prevActiveTab = activeTab
	a.prevDrawCursor = drawCursor
	a.prevToastVisible = toastVisible
	a.prevMenuOpen = menuOpen
	a.prevSearchOpen = a.searchPanel.Open
	a.prevAIOpen = a.aiPanel.Open
	a.prevHelpOpen = a.showHelp
	a.prevFindOpen = a.find.open
	a.prevFocused = a.windowFocused
	a.prevFBWidth, a.prevFBHeight = width, height

	if render.ShouldRedraw(trig) {
		a.win.SetViewport(width, height)
		if a.settingsMenu.IsOpen() {
			a.renderer.RenderWithMenu(a.tabManager, width, height, drawCursor, a.settingsMenu)
		} else {
			a.renderer.RenderWithHelpAndPanels(a.tabManager, width, height, drawCursor, a.showHelp, a.searchPanel, a.aiPanel)
		}
		if a.find.open {
			a.renderer.DrawFindBar(a.find.query, a.findStatus(), width, height)
		}
		if toastVisible {
			a.renderer.DrawToast(a.toast.message, width, height)
		}

		// Swap buffers. While an app holds synchronized output (?2026),
		// skip presenting the partial frame but keep polling input so the
		// UI stays responsive; a watchdog in SyncActive() resumes within
		// ~100ms (the SyncActiveOrEnded trigger guarantees one presented
		// frame after release).
		if !syncActive {
			a.win.SwapBuffers()
		}
	}
}
