package searchpanel

import (
	"strconv"
	"strings"
	"time"

	"github.com/javanhut/RavenTerminal/src/websearch"
)

type Mode int

const (
	ModeResults Mode = iota
	ModePreview
)

const (
	linesPerResult   = 3
	maxHistorySize   = 100
	spinnerFrameRate = 100 * time.Millisecond
)

// Spinner frames for loading animation
var spinnerFrames = []string{"|", "/", "-", "\\"}

type Result struct {
	Title   string
	URL     string
	Snippet string
}

type Panel struct {
	Open             bool
	Enabled          bool
	Query            string
	LastQuery        string
	QueryDirty       bool
	Results          []Result
	Selected         int
	ResultsScroll    int
	Mode             Mode
	ProxyEnabled     bool
	Focused          bool
	PreviewTitle     string
	PreviewURL       string
	PreviewLines     []string
	PreviewWrapped   []string
	PreviewWrapChars int
	PreviewScroll    int
	Status           string
	Loading          bool
	SearchID         int
	PreviewID        int

	// Search history
	History      []string // Previous search queries
	HistoryIndex int      // Current position in history (-1 = current query)
	TempQuery    string   // Saved current query when browsing history

	// Loading animation
	LoadingStart time.Time // When loading started

	// Mouse text selection (preview mode). A drag selection persists after
	// the mouse is released (Dragging false, Active true) so Ctrl+A / Ctrl+I
	// can act on it; a zero-drag click never persists, and Esc, a new press,
	// page load, or click outside the panel clears it.
	SelectionActive   bool
	SelectionDragging bool
	SelectionStart    int // Start line index (in wrapped preview lines)
	SelectionEnd      int // End line index (in wrapped preview lines)

	// Bookmark view (results mode): bookmarks shown in place of the results
	// list; the normal results are saved for restore on toggle-off.
	ShowingBookmarks bool
	savedResults     []Result
	savedSelected    int
	savedScroll      int
	savedStatus      string

	// In-page find (preview mode)
	FindActive  bool // find UI on: matches highlighted, status shows the query
	FindEditing bool // typed runes edit FindQuery instead of the search query
	FindQuery   string
	FindMatch   int // index into FindMatchLines (-1 = none)

	// Links extracted from the previewed page ("[n]" markers in the text)
	Links        []websearch.Link
	SelectedLink int // index into Links (-1 = none)

	// Back/forward navigation across previewed pages
	NavStack      []NavEntry
	NavPos        int // index of the current page in NavStack (-1 = empty)
	PendingScroll int // scroll restored by the next SetPreview (nav back/forward)
}

// NavEntry is one page in the back/forward stack. Scroll holds the position
// the page was left at, restored when navigating back to it.
type NavEntry struct {
	URL    string
	Title  string
	Scroll int
}

type Layout struct {
	PanelX       float32
	PanelY       float32
	PanelWidth   float32
	PanelHeight  float32
	ContentX     float32
	ContentWidth float32
	LineHeight   float32
	HeaderY      float32
	InputLabelY  float32
	InputBoxY    float32
	StatusY      float32
	ResultsStart float32
	ResultsEnd   float32
	FooterY      float32
	VisibleLines int
}

func New() *Panel {
	return &Panel{
		Mode:         ModeResults,
		Selected:     0,
		History:      make([]string, 0, maxHistorySize),
		HistoryIndex: -1,
		SelectedLink: -1,
		NavPos:       -1,
	}
}

func (p *Panel) Toggle() {
	p.Open = !p.Open
	if p.Open {
		p.Focused = true
	}
}

func (p *Panel) SetEnabled(enabled bool) {
	p.Enabled = enabled
	if !enabled {
		p.Open = false
	}
}

func (p *Panel) SetQuery(text string) {
	// Editing the query leaves the bookmark view.
	p.HideBookmarks()
	p.Query = text
	p.QueryDirty = p.Query != p.LastQuery
	if p.Mode == ModePreview {
		p.Mode = ModeResults
		p.PreviewLines = nil
		p.PreviewScroll = 0
		p.ExitFind()
	}
}

func (p *Panel) AppendQuery(char rune) {
	p.SetQuery(p.Query + string(char))
}

func (p *Panel) Backspace() {
	if p.Query == "" {
		return
	}
	runes := []rune(p.Query)
	p.SetQuery(string(runes[:len(runes)-1]))
}

func (p *Panel) ClearQuery() {
	p.SetQuery("")
}

func (p *Panel) SetResults(query string, results []Result, err error) {
	p.Loading = false
	// Fresh results replace whatever was on screen, bookmark view included.
	p.ShowingBookmarks = false
	p.savedResults = nil
	if err != nil {
		// One line; the renderer truncates it to the panel width.
		p.Status = "Search failed: " + websearch.ErrorReason(err)
		p.Results = nil
		p.Selected = 0
		p.ResultsScroll = 0
		return
	}

	p.Status = ""
	p.Results = results
	p.Selected = 0
	p.ResultsScroll = 0
	p.LastQuery = query
	p.QueryDirty = p.Query != p.LastQuery
}

func (p *Panel) SetPreview(url, title string, lines []string, links []websearch.Link, err error) {
	p.Loading = false
	p.Mode = ModePreview
	// PendingScroll restores the saved position on nav back/forward; it is 0
	// for ordinary navigations.
	p.PreviewScroll = p.PendingScroll
	p.PendingScroll = 0
	p.PreviewWrapped = nil
	p.PreviewWrapChars = 0
	p.Links = links
	p.SelectedLink = -1
	p.SelectionActive = false
	p.SelectionDragging = false
	p.ExitFind()
	if err != nil {
		p.Status = "Preview failed: " + websearch.ErrorReason(err)
		p.PreviewLines = []string{"Failed to load preview."}
		return
	}
	p.Status = ""
	p.PreviewURL = url
	p.PreviewTitle = title
	p.PreviewLines = lines
}

func (p *Panel) ResultCount() int {
	return len(p.Results)
}

func (p *Panel) ResultsTotalLines() int {
	return len(p.Results) * linesPerResult
}

func (p *Panel) LinesPerResult() int {
	return linesPerResult
}

func (p *Panel) MoveSelection(delta int, visibleLines int) {
	if len(p.Results) == 0 {
		return
	}
	p.Selected += delta
	if p.Selected < 0 {
		p.Selected = 0
	}
	if p.Selected >= len(p.Results) {
		p.Selected = len(p.Results) - 1
	}
	p.ensureSelectionVisible(visibleLines)
}

func (p *Panel) ScrollResults(delta int, visibleLines int) {
	if len(p.Results) == 0 {
		return
	}
	p.ResultsScroll += delta
	maxScroll := max(p.ResultsTotalLines()-visibleLines, 0)
	if p.ResultsScroll < 0 {
		p.ResultsScroll = 0
	}
	if p.ResultsScroll > maxScroll {
		p.ResultsScroll = maxScroll
	}
}

func (p *Panel) ScrollPreview(delta int, visibleLines int) {
	totalLines := len(p.PreviewLines)
	if len(p.PreviewWrapped) > 0 && p.PreviewWrapChars > 0 {
		totalLines = len(p.PreviewWrapped)
	}
	if totalLines == 0 {
		return
	}
	p.PreviewScroll += delta
	maxScroll := max(totalLines-visibleLines, 0)
	if p.PreviewScroll < 0 {
		p.PreviewScroll = 0
	}
	if p.PreviewScroll > maxScroll {
		p.PreviewScroll = maxScroll
	}
}

func (p *Panel) ensureSelectionVisible(visibleLines int) {
	if visibleLines <= 0 {
		return
	}
	startLine := p.Selected * linesPerResult
	endLine := startLine + linesPerResult - 1

	if startLine < p.ResultsScroll {
		p.ResultsScroll = startLine
	}
	if endLine >= p.ResultsScroll+visibleLines {
		p.ResultsScroll = endLine - visibleLines + 1
	}

	maxScroll := max(p.ResultsTotalLines()-visibleLines, 0)
	if p.ResultsScroll > maxScroll {
		p.ResultsScroll = maxScroll
	}
	if p.ResultsScroll < 0 {
		p.ResultsScroll = 0
	}
}

func (p *Panel) Layout(width, height int, cellWidth, cellHeight float32) Layout {
	panelWidth := float32(width) * 0.35
	minPanelWidth := float32(340)
	if cellWidth > 0 {
		wideMin := cellWidth * 32
		if wideMin > minPanelWidth {
			minPanelWidth = wideMin
		}
	}
	// Clamp the minimum to the available window width so a small window
	// never forces the panel past its own max-width boundary and clips.
	maxWidth := float32(width) - 20
	if maxWidth < 1 {
		maxWidth = 1
	}
	if minPanelWidth > maxWidth {
		minPanelWidth = maxWidth
	}
	if panelWidth < minPanelWidth {
		panelWidth = minPanelWidth
	}
	if panelWidth > 560 {
		panelWidth = 560
	}
	if panelWidth > maxWidth {
		panelWidth = maxWidth
	}

	panelHeight := float32(height) - 30
	// Clamp the height floor to the available window height so a small
	// window never forces the panel past its own max-height boundary.
	maxHeight := float32(height) - 20
	if maxHeight < 1 {
		maxHeight = 1
	}
	if panelHeight < 240 {
		panelHeight = 240
	}
	if panelHeight > maxHeight {
		panelHeight = maxHeight
	}
	// Final safety: the 240 floor above may still exceed maxHeight on very
	// small windows; clamp again so we never overflow.
	if panelHeight > maxHeight {
		panelHeight = maxHeight
	}

	panelX := float32(width) - panelWidth - 10
	panelY := float32(10)

	lineHeight := cellHeight * 1.35
	contentX := panelX + 18
	contentWidth := panelWidth - 36
	headerY := panelY + lineHeight*1.2
	inputLabelY := headerY + lineHeight*1.4
	inputBoxY := inputLabelY + lineHeight*0.45
	statusY := inputBoxY + lineHeight*1.4
	resultsStart := statusY + lineHeight*1.1
	footerY := panelY + panelHeight - lineHeight*0.6
	resultsEnd := footerY - lineHeight*1.2

	visibleLines := max(int((resultsEnd-resultsStart)/lineHeight), 1)

	return Layout{
		PanelX:       panelX,
		PanelY:       panelY,
		PanelWidth:   panelWidth,
		PanelHeight:  panelHeight,
		ContentX:     contentX,
		ContentWidth: contentWidth,
		LineHeight:   lineHeight,
		HeaderY:      headerY,
		InputLabelY:  inputLabelY,
		InputBoxY:    inputBoxY,
		StatusY:      statusY,
		ResultsStart: resultsStart,
		ResultsEnd:   resultsEnd,
		FooterY:      footerY,
		VisibleLines: visibleLines,
	}
}

func (p *Panel) ResultTitle(idx int) string {
	if idx < 0 || idx >= len(p.Results) {
		return ""
	}
	return strings.TrimSpace(p.Results[idx].Title)
}

// AddToHistory adds a query to the search history
func (p *Panel) AddToHistory(query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}

	// Remove duplicate if exists
	for i, h := range p.History {
		if h == query {
			p.History = append(p.History[:i], p.History[i+1:]...)
			break
		}
	}

	// Add to front
	p.History = append([]string{query}, p.History...)

	// Trim to max size
	if len(p.History) > maxHistorySize {
		p.History = p.History[:maxHistorySize]
	}

	// Reset history navigation
	p.HistoryIndex = -1
	p.TempQuery = ""
}

// HistoryUp navigates to older query in history
func (p *Panel) HistoryUp() bool {
	if len(p.History) == 0 {
		return false
	}

	// Save current query when first navigating
	if p.HistoryIndex == -1 {
		p.TempQuery = p.Query
	}

	// Move up in history
	nextIdx := p.HistoryIndex + 1
	if nextIdx >= len(p.History) {
		return false
	}

	p.HistoryIndex = nextIdx
	p.Query = p.History[p.HistoryIndex]
	p.QueryDirty = p.Query != p.LastQuery
	return true
}

// HistoryDown navigates to newer query in history
func (p *Panel) HistoryDown() bool {
	if p.HistoryIndex < 0 {
		return false
	}

	p.HistoryIndex--
	if p.HistoryIndex < 0 {
		// Restore original query
		p.Query = p.TempQuery
		p.TempQuery = ""
	} else {
		p.Query = p.History[p.HistoryIndex]
	}
	p.QueryDirty = p.Query != p.LastQuery
	return true
}

// ResetHistory resets history navigation state
func (p *Panel) ResetHistory() {
	p.HistoryIndex = -1
	p.TempQuery = ""
}

// StartLoading marks the panel as loading with timestamp
func (p *Panel) StartLoading() {
	p.Loading = true
	p.LoadingStart = time.Now()
}

// SpinnerFrame returns the current spinner character based on elapsed time
func (p *Panel) SpinnerFrame() string {
	if !p.Loading {
		return ""
	}
	elapsed := time.Since(p.LoadingStart)
	frameCount := int(elapsed / spinnerFrameRate)
	return spinnerFrames[frameCount%len(spinnerFrames)]
}

// findMatchLines returns the indices of lines containing query as a
// case-insensitive substring, including matches that span a wrap boundary
// into the next line. An empty query matches nothing.
func findMatchLines(lines []string, query string) []int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	var out []int
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, query) {
			out = append(out, i)
			continue
		}
		// Wrapping breaks lines between words, so a multi-word query can span
		// a boundary: check the pair joined by the space wrapping removed,
		// counting only matches that start on this line (the next line
		// reports its own).
		// ponytail: covers a two-line span; a query longer than a whole
		// wrapped line still misses.
		if i+1 < len(lines) {
			pair := lower + " " + strings.ToLower(lines[i+1])
			if idx := strings.Index(pair, query); idx >= 0 && idx < len(lower) {
				out = append(out, i)
			}
		}
	}
	return out
}

// FindMatchLines returns the wrapped-preview line indices matching FindQuery.
func (p *Panel) FindMatchLines() []int {
	return findMatchLines(p.PreviewWrapped, p.FindQuery)
}

// StartFind enters in-page find mode with an empty query.
func (p *Panel) StartFind() {
	p.FindActive = true
	p.FindEditing = true
	p.FindQuery = ""
	p.FindMatch = -1
}

// ExitFind leaves in-page find mode and clears its state.
func (p *Panel) ExitFind() {
	p.FindActive = false
	p.FindEditing = false
	p.FindQuery = ""
	p.FindMatch = -1
}

// AppendFind appends a rune to the find query.
func (p *Panel) AppendFind(char rune) {
	p.FindQuery += string(char)
	p.FindMatch = -1
}

// FindClear empties the find query (Ctrl+U while editing it).
func (p *Panel) FindClear() {
	p.FindQuery = ""
	p.FindMatch = -1
}

// FindBackspace removes the last rune of the find query.
func (p *Panel) FindBackspace() {
	if p.FindQuery == "" {
		return
	}
	runes := []rune(p.FindQuery)
	p.FindQuery = string(runes[:len(runes)-1])
	p.FindMatch = -1
}

// ConfirmFind leaves edit mode and jumps to the first match at or after the
// current scroll position, wrapping to the top match when there is none.
func (p *Panel) ConfirmFind(visibleLines int) {
	p.FindEditing = false
	matches := p.FindMatchLines()
	if len(matches) == 0 {
		p.FindMatch = -1
		return
	}
	p.FindMatch = 0
	for i, line := range matches {
		if line >= p.PreviewScroll {
			p.FindMatch = i
			break
		}
	}
	p.scrollFindIntoView(matches, visibleLines)
}

// FindStep jumps to the next (+1) or previous (-1) match, wrapping around.
func (p *Panel) FindStep(delta, visibleLines int) {
	matches := p.FindMatchLines()
	n := len(matches)
	if n == 0 {
		p.FindMatch = -1
		return
	}
	if p.FindMatch < 0 || p.FindMatch >= n {
		p.FindMatch = 0
	} else {
		p.FindMatch = (p.FindMatch + delta%n + n) % n
	}
	p.scrollFindIntoView(matches, visibleLines)
}

// scrollFindIntoView scrolls just far enough to make the current match visible.
func (p *Panel) scrollFindIntoView(matches []int, visibleLines int) {
	if p.FindMatch < 0 || p.FindMatch >= len(matches) {
		return
	}
	p.scrollLineIntoView(matches[p.FindMatch], visibleLines)
}

// scrollLineIntoView scrolls just far enough to make line visible.
func (p *Panel) scrollLineIntoView(line, visibleLines int) {
	if visibleLines <= 0 {
		return
	}
	if line < p.PreviewScroll {
		p.PreviewScroll = line
	}
	if line >= p.PreviewScroll+visibleLines {
		p.PreviewScroll = line - visibleLines + 1
	}
}

// LoadHistory replaces the history with persisted entries (newest first),
// trimming to the max size.
func (p *Panel) LoadHistory(entries []string) {
	if len(entries) == 0 {
		return
	}
	if len(entries) > maxHistorySize {
		entries = entries[:maxHistorySize]
	}
	p.History = entries
	p.HistoryIndex = -1
	p.TempQuery = ""
}

// NavPush records a page visit at the current stack position, truncating any
// forward entries. Re-navigating to the current page (reload, nav
// back/forward restore) is a no-op so those flows never duplicate entries.
func (p *Panel) NavPush(url, title string) {
	if p.NavPos >= 0 && p.NavPos < len(p.NavStack) {
		if p.NavStack[p.NavPos].URL == url {
			return
		}
		p.NavStack[p.NavPos].Scroll = p.PreviewScroll
	}
	p.NavStack = append(p.NavStack[:p.NavPos+1], NavEntry{URL: url, Title: title})
	p.NavPos = len(p.NavStack) - 1
	// A fresh navigation invalidates any scroll restore still pending from an
	// abandoned back/forward fetch.
	p.PendingScroll = 0
}

// NavBack steps to the previous page, saving the current scroll position.
// Returns false on the first page (caller falls back to the results list).
func (p *Panel) NavBack() (NavEntry, bool) {
	if p.NavPos <= 0 {
		return NavEntry{}, false
	}
	p.NavStack[p.NavPos].Scroll = p.PreviewScroll
	p.NavPos--
	return p.NavStack[p.NavPos], true
}

// NavForward steps to the next page, saving the current scroll position.
func (p *Panel) NavForward() (NavEntry, bool) {
	if p.NavPos < 0 || p.NavPos >= len(p.NavStack)-1 {
		return NavEntry{}, false
	}
	p.NavStack[p.NavPos].Scroll = p.PreviewScroll
	p.NavPos++
	return p.NavStack[p.NavPos], true
}

// ResetNav clears the back/forward stack (new search = new browsing context).
func (p *Panel) ResetNav() {
	p.NavStack = nil
	p.NavPos = -1
	p.PendingScroll = 0
}

// linkMarkerPos returns the line index and byte offset of the occ-th
// occurrence (0-based) of the "[n]" marker, or (-1, -1) when it is not found.
// Occurrence counting skips past literal page text like a "[3]" citation that
// would otherwise shadow the real marker.
func linkMarkerPos(lines []string, n, occ int) (int, int) {
	marker := "[" + strconv.Itoa(n) + "]"
	for i, line := range lines {
		for from := 0; ; {
			idx := strings.Index(line[from:], marker)
			if idx < 0 {
				break
			}
			if occ == 0 {
				return i, from + idx
			}
			occ--
			from += idx + len(marker)
		}
	}
	return -1, -1
}

// SelectedLinkMarkerPos returns the wrapped line index and byte offset of the
// selected link's "[n]" marker, or (-1, -1) when no link is selected or its
// marker is not on any line.
func (p *Panel) SelectedLinkMarkerPos() (int, int) {
	if p.SelectedLink < 0 || p.SelectedLink >= len(p.Links) {
		return -1, -1
	}
	return linkMarkerPos(p.PreviewWrapped, p.SelectedLink+1, p.Links[p.SelectedLink].Occ)
}

// CycleLink advances the selected link by delta (wrapping) and scrolls its
// "[n]" marker into view.
func (p *Panel) CycleLink(delta, visibleLines int) {
	n := len(p.Links)
	if n == 0 {
		return
	}
	if p.SelectedLink < 0 || p.SelectedLink >= n {
		if delta < 0 {
			p.SelectedLink = n - 1
		} else {
			p.SelectedLink = 0
		}
	} else {
		p.SelectedLink = (p.SelectedLink + delta%n + n) % n
	}
	line, _ := p.SelectedLinkMarkerPos()
	if line < 0 {
		return
	}
	p.scrollLineIntoView(line, visibleLines)
}

// SelectedLinkTarget returns the currently selected link, if any.
func (p *Panel) SelectedLinkTarget() (websearch.Link, bool) {
	if p.SelectedLink < 0 || p.SelectedLink >= len(p.Links) {
		return websearch.Link{}, false
	}
	return p.Links[p.SelectedLink], true
}

// SelectedPreviewText returns the mouse-selected wrapped preview lines joined
// with newlines ("" when nothing usable is selected).
func (p *Panel) SelectedPreviewText() string {
	if !p.SelectionActive || len(p.PreviewWrapped) == 0 {
		return ""
	}
	start, end := p.SelectionStart, p.SelectionEnd
	if end < start {
		start, end = end, start
	}
	start = max(start, 0)
	end = min(end, len(p.PreviewWrapped)-1)
	if start > end {
		return ""
	}
	return strings.Join(p.PreviewWrapped[start:end+1], "\n")
}

// ShowBookmarks swaps the results list for the bookmark list, saving the
// normal results so HideBookmarks can restore them.
func (p *Panel) ShowBookmarks(bookmarks []Result) {
	if !p.ShowingBookmarks {
		p.savedResults = p.Results
		p.savedSelected = p.Selected
		p.savedScroll = p.ResultsScroll
		p.savedStatus = p.Status
		p.ShowingBookmarks = true
	}
	p.Results = bookmarks
	p.Selected = 0
	p.ResultsScroll = 0
	p.Status = "Bookmarks"
}

// HideBookmarks restores the normal results list; no-op outside bookmark view.
func (p *Panel) HideBookmarks() {
	if !p.ShowingBookmarks {
		return
	}
	p.Results = p.savedResults
	p.Selected = p.savedSelected
	p.ResultsScroll = p.savedScroll
	p.Status = p.savedStatus
	p.savedResults = nil
	p.ShowingBookmarks = false
}

// GetSelectedURL returns the URL of the currently selected result
func (p *Panel) GetSelectedURL() string {
	if p.Selected < 0 || p.Selected >= len(p.Results) {
		return ""
	}
	return p.Results[p.Selected].URL
}
