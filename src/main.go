package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
	"github.com/javanhut/RavenTerminal/src/commands"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/ollama"
	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/shell"
	"github.com/javanhut/RavenTerminal/src/tab"
	"github.com/javanhut/RavenTerminal/src/version"
	"github.com/javanhut/RavenTerminal/src/websearch"
	"github.com/javanhut/RavenTerminal/src/window"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// clipboardReadReq ferries OSC 52 clipboard read requests from PTY reader
// goroutines to the main thread (GLFW clipboard access is main-thread only),
// mirroring the DrainClipboard pattern used for writes. Buffer of 1: a second
// concurrent query simply gets an empty reply.
var clipboardReadReq = make(chan clipboardRead, 1)

type clipboardRead struct {
	primary bool
	reply   chan string
}

// requestClipboardRead asks the main loop for the system clipboard contents.
// Called on PTY reader goroutines by the parser (OSC 52 query).
func requestClipboardRead() string { return requestSelectionRead(false) }

func requestPrimaryRead() string { return requestSelectionRead(true) }

func requestSelectionRead(primary bool) string {
	reply := make(chan string, 1)
	select {
	case clipboardReadReq <- clipboardRead{primary, reply}:
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
		parser.SetDefaultPrimaryReader(requestPrimaryRead)
	} else {
		parser.SetDefaultClipboardReader(nil)
		parser.SetDefaultPrimaryReader(nil)
	}
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
	listOllamaModels(context.Background(), baseURL, timeout, results)
}

// listOllamaModels is listOllamaModelsAsync bound to ctx: the fetch is
// aborted, and its result dropped, once ctx ends.
func listOllamaModels(ctx context.Context, baseURL string, timeout time.Duration, results chan<- ollamaModelsResponse) {
	goSafe("ollama model list", func() {
		tctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		client := ollama.NewClient(baseURL, "")
		models, err := client.ListModels(tctx)
		deliver(ctx, results, ollamaModelsResponse{models: models, err: err})
	})
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

// tabDragState tracks a press on a tab-bar chip: pending until the cursor
// moves past a small threshold, then active while the tab is live-reordered.
type tabDragState struct {
	pending bool
	active  bool
	index   int      // current slot of the dragged tab
	tab     *tab.Tab // identity of the dragged tab; its slot changes as it moves
	startX  float64  // logical cursor X at press
	startY  float64  // logical cursor Y at press
}

func dragThresholdPassed(startX, startY, x, y, threshold float64) bool {
	dx, dy := x-startX, y-startY
	return dx*dx+dy*dy > threshold*threshold
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
	ctx := pane.Terminal.MouseCtx()
	ctx.Shift = shift
	if w := glfw.GetCurrentContext(); w != nil {
		down := func(a, b glfw.Key) bool { return w.GetKey(a) == glfw.Press || w.GetKey(b) == glfw.Press }
		ctx.Alt = down(glfw.KeyLeftAlt, glfw.KeyRightAlt)
		ctx.Ctrl = down(glfw.KeyLeftControl, glfw.KeyRightControl)
	}
	return ctx
}

// previewCacheKey keys the preview cache: proxy and direct fetches return
// different text, so key on both.
func previewCacheKey(useProxy bool, url string) string {
	return fmt.Sprintf("%t|%s", useProxy, url)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "@" {
		os.Exit(ipcClientMain(os.Args[2:], os.Stdout, os.Stderr))
	}
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "raven-terminal: %v\n\n%s", err, usageText())
		os.Exit(2)
	}
	switch {
	case opts.help:
		fmt.Print(usageText())
		return
	case opts.listFonts:
		fmt.Print(listFontsText())
		return
	case opts.keybindings:
		fmt.Print(keybindingsText())
		return
	case opts.version:
		fmt.Println("raven-terminal " + version.String())
		return
	}
	tab.SetHoldOnExit(opts.hold)
	windowClass = opts.class
	initialTitle = opts.title
	stop := installSignalHandler(window.PostEmptyEvent)
	defer startIPCServer()()
	// runApps owns the lifetime of every window, including destroying each one
	// as it closes and terminating GLFW after the last.
	runApps(newApp(opts.dir, opts.command))
	stop()
	// Closed panes hang up their shells in the background; give stragglers
	// their SIGKILL before the process (and with it the reapers) goes away.
	shell.WaitReaped(2 * time.Second)
}

// launchOptions is the parsed command line.
type launchOptions struct {
	dir         string   // first tab's working directory ("" = home)
	command     []string // -e / -- command instead of a shell
	title       string   // initial window title
	class       string   // X11 class / Wayland app_id
	hold        bool     // keep panes open after their process exits
	listFonts   bool
	keybindings bool
	help        bool
	version     bool
}

// parseArgs reads the command line. Everything after -e/--execute/-- is the
// command, verbatim (`Exec=raven-terminal -e crow %F` must run crow). A bare
// argument before that is the working directory, as a path or a file:// URI
// (how file managers and `raven-open --terminal DIR` hand over a folder). A
// value flag at the very end with no value is ignored rather than fatal.
func parseArgs(args []string) (launchOptions, error) {
	o := launchOptions{class: "raven-terminal"}
	positional := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, val, hasVal := strings.Cut(arg, "=")
		if !strings.HasPrefix(arg, "--") {
			name, hasVal = arg, false
		}
		value := func() (string, bool) {
			if hasVal {
				return val, true
			}
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}
		switch name {
		case "-e", "--execute", "--":
			if i+1 < len(args) {
				o.command = args[i+1:]
			}
			return o, nil
		case "--working-directory", "-d":
			if v, ok := value(); ok {
				o.dir = v
			}
		case "--title", "-T":
			if v, ok := value(); ok {
				o.title = v
			}
		case "--class", "--app-id":
			if v, ok := value(); ok && v != "" {
				o.class = v
			}
		case "--hold":
			o.hold = true
		case "--list-fonts":
			o.listFonts = true
		case "--keybindings":
			o.keybindings = true
		case "-h", "--help":
			o.help = true
		case "-V", "--version":
			o.version = true
		default:
			if strings.HasPrefix(arg, "-") {
				return o, fmt.Errorf("unknown option %q", arg)
			}
			if positional {
				return o, fmt.Errorf("unexpected argument %q", arg)
			}
			positional = true
			dir, err := dirFromArg(arg)
			if err != nil {
				return o, err
			}
			o.dir = dir
		}
	}
	return o, nil
}

// dirFromArg accepts a path or a local file:// URI.
func dirFromArg(arg string) (string, error) {
	if !strings.HasPrefix(arg, "file:") {
		return arg, nil
	}
	u, err := url.Parse(arg)
	if err != nil {
		return "", fmt.Errorf("bad directory URI %q: %w", arg, err)
	}
	if u.Host != "" && u.Host != "localhost" {
		if h, _ := os.Hostname(); u.Host != h {
			return "", fmt.Errorf("directory URI %q is not on this host", arg)
		}
	}
	return u.Path, nil
}

func usageText() string {
	return `Usage: raven-terminal [OPTIONS] [DIR] [-e COMMAND [ARGS...]]

  DIR                         start in DIR (a path or file:// URI)
  -d, --working-directory DIR start in DIR
  -e, --execute CMD ...       run CMD instead of a shell (also: -- CMD ...)
      --hold                  keep panes open after their process exits
  -T, --title TITLE           initial window title
      --class ID, --app-id ID window class / Wayland app_id (default raven-terminal)
      --list-fonts            print the available fonts and exit
      --keybindings           print the keybindings and exit
  -h, --help                  print this help and exit
  -V, --version               print the version and exit
`
}

func listFontsText() string {
	return strings.TrimLeft(commands.FontList(fonts.AvailableFonts(), fonts.DefaultFontName()), "\n")
}

func keybindingsText() string {
	return strings.TrimLeft(commands.KeybindingsHelp(), "\n")
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

// cellPos is a viewport cell coordinate.
type cellPos struct{ col, row int }

// linkSpan is a link's extent in the viewport: it may cover several rows when
// the link soft-wraps. Start and End are inclusive; ok is false for no link.
type linkSpan struct {
	start, end cellPos
	ok         bool
}

// logicalLine gathers the soft-wrap-joined viewport rows containing row, as
// parallel slices of cells and their positions. Wide-char continuation cells
// are dropped so a wide glyph is one rune, not a rune plus a blank.
func logicalLine(g *grid.Grid, row int) ([]grid.Cell, []cellPos) {
	first, last := row, row
	for first > 0 && g.DisplayRowSoftWrapped(first-1) {
		first--
	}
	for last < g.Rows-1 && g.DisplayRowSoftWrapped(last) {
		last++
	}
	cells := make([]grid.Cell, 0, (last-first+1)*g.Cols)
	pos := make([]cellPos, 0, cap(cells))
	for r := first; r <= last; r++ {
		for c := 0; c < g.Cols; c++ {
			cell := g.DisplayCell(c, r)
			if cell.Width == grid.CellWidthContinuation {
				continue
			}
			cells = append(cells, cell)
			pos = append(pos, cellPos{c, r})
		}
	}
	return cells, pos
}

// linkAtCell resolves a clickable URL at (col,row), preferring an explicit OSC 8
// hyperlink and falling back to heuristic URL detection. Both follow the line
// across soft wraps. The returned span covers the whole link, for hover
// underlining.
func linkAtCell(g *grid.Grid, col, row int) (string, linkSpan) {
	if g == nil || row < 0 || row >= g.Rows || col < 0 || col >= g.Cols {
		return "", linkSpan{}
	}
	cells, pos := logicalLine(g, row)
	idx := -1
	for i, p := range pos {
		if p.row == row && p.col <= col {
			idx = i // last cell starting at or before col: the one under it
		}
	}
	if idx < 0 {
		return "", linkSpan{}
	}

	if id := cells[idx].Link; id != 0 {
		if url := g.LinkURL(id); url != "" {
			start, end := idx, idx
			for start > 0 && cells[start-1].Link == id {
				start--
			}
			for end+1 < len(cells) && cells[end+1].Link == id {
				end++
			}
			return url, linkSpan{pos[start], pos[end], true}
		}
	}

	line := make([]rune, len(cells))
	for i, cell := range cells {
		line[i] = cell.Char
		if line[i] == 0 {
			line[i] = ' '
		}
	}
	target, start, end := urlInLine(line, idx)
	if target == "" {
		return "", linkSpan{}
	}
	return target, linkSpan{pos[start], pos[end], true}
}

// urlInLine finds a URL in the whitespace-delimited word containing line[idx],
// returning it with its inclusive rune range (trimmed of surrounding
// punctuation), or "" if the word isn't a URL.
func urlInLine(line []rune, idx int) (string, int, int) {
	if idx < 0 || idx >= len(line) || line[idx] == ' ' {
		return "", -1, -1
	}

	start := idx
	for start > 0 && line[start-1] != ' ' {
		start--
	}
	end := idx
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
	if start > end || idx < start || idx > end {
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
	if err := validateOpenTarget(target); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		name, args, err := desktopOpener(exec.LookPath)
		if err != nil {
			return err
		}
		cmd = exec.Command(name, append(args, target)...)
	}
	return cmd.Start()
}

// desktopOpener picks the command that hands a URL to the desktop's default
// browser. On Raven that is raven-open, the system's own opener, called by
// its real name so a missing or replaced xdg-open symlink cannot break link
// clicks. Elsewhere it is xdg-open, and failing that gio, which ships with
// GLib on any GTK desktop and reads the same mimeapps.list.
func desktopOpener(lookPath func(string) (string, error)) (string, []string, error) {
	for _, opener := range []struct {
		name string
		args []string
	}{
		{"raven-open", nil},
		{"xdg-open", nil},
		{"gio", []string{"open"}},
	} {
		if _, err := lookPath(opener.name); err == nil {
			return opener.name, opener.args, nil
		}
	}
	return "", nil, fmt.Errorf("no url opener found (tried raven-open, xdg-open and gio)")
}

// validateOpenTarget gates which links the OS opener may see. Terminal
// content can come from a remote SSH session or a cat'ed file, so schemes
// like file:// or smb:// must not be handed to open/xdg-open/rundll32.
func validateOpenTarget(target string) error {
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("refusing to open non-http(s) url: %q", target)
	}
	return nil
}
