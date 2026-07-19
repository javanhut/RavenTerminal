package main

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/ollama"
	"github.com/javanhut/RavenTerminal/src/parser"
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

// mouseCtxFor snapshots a pane's mouse-routing state for DecideMouse.
func mouseCtxFor(pane *tab.Pane, shift bool) parser.MouseContext {
	mode, sgr, alt, appCur := pane.Terminal.MouseState()
	return parser.MouseContext{Mode: mode, SGR: sgr, Shift: shift, AltScreen: alt, AppCursorKeys: appCur}
}

// previewCacheKey keys the preview cache: proxy and direct fetches return
// different text, so key on both.
func previewCacheKey(useProxy bool, url string) string {
	return fmt.Sprintf("%t|%s", useProxy, url)
}

func main() {
	app := newApp()
	defer app.Destroy()
	app.Run()
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
