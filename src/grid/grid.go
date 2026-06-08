package grid

import (
	"strings"
	"sync"
)

const (
	MaxScrollback = 10000
)

// CellFlags represents text attributes
type CellFlags uint8

const (
	FlagBold CellFlags = 1 << iota
	FlagDim
	FlagItalic
	FlagUnderline
	FlagInverse
	FlagHidden
	FlagStrikethrough
)

// ColorType identifies the type of color
type ColorType uint8

const (
	ColorDefault ColorType = iota
	ColorIndexed
	ColorRGB
)

// Color represents a terminal color
type Color struct {
	Type    ColorType
	Index   uint8 // For indexed colors (0-255)
	R, G, B uint8 // For RGB colors
}

// DefaultFg returns the default foreground color
func DefaultFg() Color {
	return Color{Type: ColorDefault}
}

// DefaultBg returns the default background color
func DefaultBg() Color {
	return Color{Type: ColorDefault}
}

// IndexedColor creates an indexed color
func IndexedColor(index uint8) Color {
	return Color{Type: ColorIndexed, Index: index}
}

// RGBColor creates an RGB color
func RGBColor(r, g, b uint8) Color {
	return Color{Type: ColorRGB, R: r, G: g, B: b}
}

// Cell width constants
const (
	CellWidthContinuation uint8 = 0 // Second cell of a wide character (placeholder)
	CellWidthNormal       uint8 = 1 // Normal single-width character
	CellWidthWide         uint8 = 2 // First cell of a wide character
)

// Cell represents a single terminal cell
type Cell struct {
	Char           rune
	Fg             Color
	Bg             Color
	UnderlineColor Color // Underline color (ColorDefault = follow Fg). Set via SGR 58.
	Flags          CellFlags
	Width          uint8    // 0=continuation cell, 1=normal width, 2=wide cell start
	UnderlineStyle uint8    // 0=default/solid, 1=solid, 2=double, 3=curly, 4=dotted, 5=dashed (SGR 4:n)
	Link           uint16   // OSC 8 hyperlink id (0=none); resolve via Grid.LinkURL
	Combining      []rune   // codepoints after Char (combining marks / ZWJ cont.); nil for none
}

// NewCell creates an empty cell
func NewCell() Cell {
	return Cell{
		Char:  ' ',
		Fg:    DefaultFg(),
		Bg:    DefaultBg(),
		Flags: 0,
		Width: CellWidthNormal,
	}
}

// NewCellWithBg creates an empty cell with a specific background color (for BCE)
func NewCellWithBg(bg Color) Cell {
	return Cell{
		Char:  ' ',
		Fg:    DefaultFg(),
		Bg:    bg,
		Flags: 0,
		Width: CellWidthNormal,
	}
}

// Grid represents the terminal grid buffer
type Grid struct {
	rows         []*Row // active on-screen area, len == Rows
	history      []*Row // scrollback (oldest first)
	maxScroll    int    // history cap in rows (0 = no scrollback, e.g. alt screen)
	styles       *styleSet
	Cols         int
	Rows         int
	CursorCol    int
	CursorRow    int
	scrollOffset int
	mu           sync.RWMutex

	// Scroll region (1-based, inclusive)
	scrollTop    int
	scrollBottom int
	wrapPending  bool

	// Last written character for REP sequence
	lastChar           rune
	lastFg             Color
	lastBg             Color
	lastFlags          CellFlags
	lastLink           uint16
	lastUnderlineStyle uint8
	lastUnderlineColor Color

	// OSC 8 hyperlink interning: url <-> compact id stored on cells
	links      map[string]uint16
	linkURLs   map[uint16]string
	nextLinkID uint16

	// Grapheme interning: cluster (base + combining/ZWJ) <-> compact id.
	// Append-only; index 0 = none. Combining clusters are rare so this stays small.
	graphemes   [][]rune
	graphemeMap map[string]uint32

	// Selection state (display coordinates)
	selectionActive       bool
	selectionStartCol     int
	selectionStartRow     int
	selectionEndCol       int
	selectionEndRow       int
	selectionScrollOffset int

	// Auto-wrap mode (DECAWM ?7) - default true
	autoWrap bool

	// BCE (Background Color Erase) - background color for scroll/erase operations
	eraseBg Color

	// Tab stops (HTS/TBC). Lazily initialized to every 8th column per width.
	tabStops []bool

	// scrolledOut counts rows that have left the top of history (trimmed). Used
	// to give image placements a stable absolute-row anchor that scrolls with
	// content even as scrollback is trimmed.
	scrolledOut int
}

// NewGrid creates a new grid with the given dimensions and a full scrollback.
func NewGrid(cols, rows int) *Grid {
	return newGrid(cols, rows, MaxScrollback)
}

// NewAltGrid creates a grid with no scrollback, for the alternate screen.
func NewAltGrid(cols, rows int) *Grid {
	return newGrid(cols, rows, 0)
}

func newGrid(cols, rows, maxScroll int) *Grid {
	active := make([]*Row, rows)
	for i := range active {
		active[i] = newRow(cols)
	}
	return &Grid{
		rows:         active,
		history:      nil,
		maxScroll:    maxScroll,
		styles:       newStyleSet(),
		Cols:         cols,
		Rows:         rows,
		CursorCol:    0,
		CursorRow:    0,
		scrollOffset: 0,
		scrollTop:    1,
		scrollBottom: rows,
		wrapPending:  false,
		lastChar:     ' ',
		autoWrap:     true, // DECAWM ?7 default on
	}
}

// index returns the linear index for a cell position
func (g *Grid) index(col, row int) int {
	return row*g.Cols + col
}

// GetCell returns the cell at the given position
func (g *Grid) GetCell(col, row int) Cell {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if col < 0 || col >= g.Cols || row < 0 || row >= g.Rows {
		return NewCellWithBg(g.eraseBg)
	}
	return g.getCell(g.index(col, row))
}

// SetCell sets the cell at the given position
func (g *Grid) SetCell(col, row int, cell Cell) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if col < 0 || col >= g.Cols || row < 0 || row >= g.Rows {
		return
	}
	g.putCell(g.index(col, row), cell)
}

// InternLink returns a compact id for a hyperlink URL, deduplicating repeats.
// Returns 0 for an empty URL (meaning "no link").
func (g *Grid) InternLink(url string) uint16 {
	if url == "" {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.links == nil {
		g.links = make(map[string]uint16)
		g.linkURLs = make(map[uint16]string)
	}
	if id, ok := g.links[url]; ok {
		return id
	}
	g.nextLinkID++
	if g.nextLinkID == 0 { // wrap-around guard: 0 is reserved for "no link"
		g.nextLinkID = 1
	}
	id := g.nextLinkID
	g.links[url] = id
	g.linkURLs[id] = url
	return id
}

// LinkURL resolves a hyperlink id back to its URL ("" if none).
func (g *Grid) LinkURL(id uint16) string {
	if id == 0 {
		return ""
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.linkURLs[id]
}

// WriteChar writes a character at the cursor position and advances
func (g *Grid) WriteChar(c rune, fg, bg Color, flags CellFlags, link uint16, ulStyle uint8, ulColor Color) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.wrapPending {
		if g.autoWrap {
			// The line we're leaving continued because it filled the last
			// column: mark it soft-wrapped so reflow can rejoin it.
			g.rows[g.CursorRow].flags |= RowSoftWrapped
			g.cursorNewline()
		}
		g.wrapPending = false
	}

	// Handle auto-wrap if at end of line
	if g.CursorCol >= g.Cols {
		if g.autoWrap {
			g.cursorNewline()
		} else {
			// No auto-wrap: stay at last column, overwrite
			g.CursorCol = g.Cols - 1
		}
	}

	// Get character width
	charWidth := RuneWidth(c)
	if charWidth == 0 {
		// Zero-width codepoint (combining mark / ZWJ continuation): attach it to
		// the most recently written cell instead of dropping it.
		g.appendCombining(c)
		return
	}

	// Check if wide character fits on current line
	if charWidth == 2 && g.CursorCol >= g.Cols-1 {
		if g.autoWrap {
			// Wide char at last column - fill with space and wrap
			idx := g.index(g.CursorCol, g.CursorRow)
			g.putCell(idx, Cell{
				Char:  ' ',
				Fg:    g.lastFg,
				Bg:    g.lastBg,
				Width: CellWidthNormal,
			})
			g.cursorNewline()
		} else {
			// No auto-wrap: treat wide char as single width at last column
			charWidth = 1
		}
	}

	// Write the character to current cell
	idx := g.index(g.CursorCol, g.CursorRow)
	g.putCell(idx, Cell{
		Char:           c,
		Fg:             fg,
		Bg:             bg,
		UnderlineColor: ulColor,
		Flags:          flags,
		Width:          uint8(charWidth),
		UnderlineStyle: ulStyle,
		Link:           link,
	})
	g.CursorCol++

	// If wide character, write continuation cell
	if charWidth == 2 && g.CursorCol < g.Cols {
		contIdx := g.index(g.CursorCol, g.CursorRow)
		g.putCell(contIdx, Cell{
			Char:           ' ', // Placeholder for continuation
			Fg:             fg,
			Bg:             bg,
			UnderlineColor: ulColor,
			Flags:          flags,
			Width:          CellWidthContinuation,
			UnderlineStyle: ulStyle,
			Link:           link,
		})
		g.CursorCol++
	}

	// If we advanced past the last column, set wrap pending (DECAWM behavior)
	if g.CursorCol >= g.Cols {
		if g.autoWrap {
			g.wrapPending = true
		}
		g.CursorCol = g.Cols - 1
	}

	// Save for REP sequence
	g.lastChar = c
	g.lastFg = fg
	g.lastBg = bg
	g.lastFlags = flags
	g.lastLink = link
	g.lastUnderlineStyle = ulStyle
	g.lastUnderlineColor = ulColor
}

// appendCombining attaches a zero-width codepoint to the most recently written
// cell (the base of the current grapheme cluster). Must be called under g.mu.
func (g *Grid) appendCombining(r rune) {
	row := g.CursorRow
	var col int
	if g.wrapPending {
		col = g.Cols - 1
	} else if g.CursorCol > 0 {
		col = g.CursorCol - 1
	} else {
		return // nothing to attach to
	}
	// Step back over a continuation cell to the wide-char base.
	if col > 0 && g.rows[row].cells[col].Width == CellWidthContinuation {
		col--
	}
	p := &g.rows[row].cells[col]
	var cluster []rune
	if p.Grapheme != 0 && int(p.Grapheme) < len(g.graphemes) {
		cluster = append(cluster, g.graphemes[p.Grapheme]...)
	} else {
		cluster = append(cluster, p.Char)
	}
	cluster = append(cluster, r)
	p.Grapheme = g.internGrapheme(cluster)
	g.rows[row].flags |= RowDirty
}

// cursorNewline moves cursor to next line (internal, no lock)
func (g *Grid) cursorNewline() {
	g.wrapPending = false
	g.CursorCol = 0
	g.CursorRow++
	// Check if we're at the bottom of the scroll region
	if g.CursorRow >= g.scrollBottom {
		g.scrollUpRegionWithBg(g.eraseBg)
		g.CursorRow = g.scrollBottom - 1
	} else if g.CursorRow >= g.Rows {
		g.scrollUpInternalWithBg(g.eraseBg)
		g.CursorRow = g.Rows - 1
	}
}

// scrollUpRegion scrolls only within the scroll region
func (g *Grid) scrollUpRegion() {
	g.scrollUpRegionWithBg(DefaultBg())
}

// scrollUpRegionWithBg scrolls only within the scroll region with BCE support.
// Uses row-pointer rotation: O(region rows) pointer moves, no cell copies.
func (g *Grid) scrollUpRegionWithBg(bg Color) {
	if g.scrollTop == 1 && g.scrollBottom == g.Rows {
		g.scrollUpInternalWithBg(bg)
		return
	}

	top := g.scrollTop - 1 // Convert to 0-based
	bottom := g.scrollBottom - 1

	leaving := g.rows[top]
	// Rotate region rows up.
	for row := top; row < bottom; row++ {
		g.rows[row] = g.rows[row+1]
	}
	if top == 0 {
		// Region anchored at the screen top: the leaving line enters scrollback
		// (preserving historical behavior); the bottom gets a fresh blank row.
		g.pushHistory(leaving)
		g.rows[bottom] = g.blankRow(bg)
	} else {
		// Mid-screen region: the leaving line is discarded; recycle it as the
		// new cleared bottom row.
		g.clearRow(leaving, bg)
		g.rows[bottom] = leaving
	}
}

// Newline moves cursor to the beginning of the next line
func (g *Grid) Newline() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cursorNewline()
}

// CarriageReturn moves cursor to the beginning of the current line
func (g *Grid) CarriageReturn() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.wrapPending = false
	g.CursorCol = 0
}

// Backspace moves cursor back one position, skipping continuation cells
func (g *Grid) Backspace() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.wrapPending = false
	if g.CursorCol > 0 {
		g.CursorCol--
		// If we landed on a continuation cell, move back one more
		if g.CursorCol > 0 {
			idx := g.index(g.CursorCol, g.CursorRow)
			if g.cellAt(idx).Width == CellWidthContinuation {
				g.CursorCol--
			}
		}
	}
}

// Tab moves the cursor to the next tab stop (honoring custom HTS/TBC stops).
func (g *Grid) Tab() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ensureTabStops()
	g.wrapPending = false
	next := -1
	for c := g.CursorCol + 1; c < g.Cols; c++ {
		if g.tabStops[c] {
			next = c
			break
		}
	}
	if next < 0 {
		next = g.Cols - 1
	}
	g.CursorCol = next
	// Check if we landed on a continuation cell
	if g.CursorCol > 0 {
		idx := g.index(g.CursorCol, g.CursorRow)
		if g.cellAt(idx).Width == CellWidthContinuation {
			g.CursorCol--
		}
	}
}

// MoveCursor moves the cursor by the given delta, handling wide cells
func (g *Grid) MoveCursor(dCol, dRow int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.wrapPending = false

	// Handle horizontal movement with wide cell awareness
	if dCol < 0 {
		// Moving left - skip continuation cells
		for i := 0; i > dCol && g.CursorCol > 0; i-- {
			g.CursorCol--
			// If we landed on a continuation cell, move back one more
			if g.CursorCol > 0 {
				idx := g.index(g.CursorCol, g.CursorRow)
				if g.cellAt(idx).Width == CellWidthContinuation {
					g.CursorCol--
				}
			}
		}
	} else if dCol > 0 {
		// Moving right - skip over wide characters properly
		for i := 0; i < dCol && g.CursorCol < g.Cols-1; i++ {
			idx := g.index(g.CursorCol, g.CursorRow)
			if g.cellAt(idx).Width == CellWidthWide {
				// Wide char - move by 2
				g.CursorCol += 2
			} else {
				g.CursorCol++
			}
		}
	}

	// Handle vertical movement
	g.CursorRow += dRow

	// Clamp to bounds
	if g.CursorCol < 0 {
		g.CursorCol = 0
	}
	if g.CursorCol >= g.Cols {
		g.CursorCol = g.Cols - 1
	}
	if g.CursorRow < 0 {
		g.CursorRow = 0
	}
	if g.CursorRow >= g.Rows {
		g.CursorRow = g.Rows - 1
	}
}

// SetCursorPos sets the cursor to an absolute position (1-based)
func (g *Grid) SetCursorPos(col, row int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.wrapPending = false
	g.CursorCol = col - 1
	g.CursorRow = row - 1

	// Clamp to bounds
	if g.CursorCol < 0 {
		g.CursorCol = 0
	}
	if g.CursorCol >= g.Cols {
		g.CursorCol = g.Cols - 1
	}
	if g.CursorRow < 0 {
		g.CursorRow = 0
	}
	if g.CursorRow >= g.Rows {
		g.CursorRow = g.Rows - 1
	}

	// After clamping, check if we landed on a continuation cell
	// If so, move left to the wide character start
	if g.CursorCol > 0 {
		idx := g.index(g.CursorCol, g.CursorRow)
		if g.cellAt(idx).Width == CellWidthContinuation {
			g.CursorCol--
		}
	}
}

// scrollUpInternal scrolls the grid up by one line (internal, no lock)
func (g *Grid) scrollUpInternal() {
	g.scrollUpInternalWithBg(DefaultBg())
}

// scrollUpInternalWithBg scrolls the grid up by one line with BCE support (internal, no lock)
func (g *Grid) scrollUpInternalWithBg(bg Color) {
	// The top row enters scrollback (pointer move; its style refs travel with
	// it). Rows shift up by pointer rotation; a fresh blank row fills the bottom.
	g.pushHistory(g.rows[0])
	for row := 0; row < g.Rows-1; row++ {
		g.rows[row] = g.rows[row+1]
	}
	g.rows[g.Rows-1] = g.blankRow(bg)
}

// ScrollUp scrolls the grid up by n lines within the scroll region
func (g *Grid) ScrollUp(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := 0; i < n; i++ {
		g.scrollUpRegion()
	}
}

// ScrollUpWithBg scrolls the grid up by n lines with BCE support
func (g *Grid) ScrollUpWithBg(n int, bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := 0; i < n; i++ {
		g.scrollUpRegionWithBg(bg)
	}
}

// scrollDownInternal scrolls the entire grid down by one line (internal, no lock)
func (g *Grid) scrollDownInternal() {
	g.scrollDownInternalWithBg(DefaultBg())
}

// scrollDownInternalWithBg scrolls the entire grid down by one line with BCE support (internal, no lock)
func (g *Grid) scrollDownInternalWithBg(bg Color) {
	// Scroll-down never enters scrollback; recycle the bottom row as the new
	// (cleared) top row via pointer rotation.
	leaving := g.rows[g.Rows-1]
	for row := g.Rows - 1; row >= 1; row-- {
		g.rows[row] = g.rows[row-1]
	}
	g.clearRow(leaving, bg)
	g.rows[0] = leaving
}

// scrollDownRegion scrolls only within the scroll region
func (g *Grid) scrollDownRegion() {
	g.scrollDownRegionWithBg(DefaultBg())
}

// scrollDownRegionWithBg scrolls only within the scroll region with BCE support
func (g *Grid) scrollDownRegionWithBg(bg Color) {
	if g.scrollTop == 1 && g.scrollBottom == g.Rows {
		g.scrollDownInternalWithBg(bg)
		return
	}

	top := g.scrollTop - 1 // Convert to 0-based
	bottom := g.scrollBottom - 1

	// Rotate region rows down; recycle the bottom row as the new cleared top.
	leaving := g.rows[bottom]
	for row := bottom; row > top; row-- {
		g.rows[row] = g.rows[row-1]
	}
	g.clearRow(leaving, bg)
	g.rows[top] = leaving
}

// ScrollDown scrolls the grid down by n lines within the scroll region
func (g *Grid) ScrollDown(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := 0; i < n; i++ {
		g.scrollDownRegion()
	}
}

// ScrollDownWithBg scrolls the grid down by n lines with BCE support
func (g *Grid) ScrollDownWithBg(n int, bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := 0; i < n; i++ {
		g.scrollDownRegionWithBg(bg)
	}
}

// ScrollViewUp scrolls the view up in scrollback
func (g *Grid) ScrollViewUp(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.scrollOffset += n
	if g.scrollOffset > len(g.history) {
		g.scrollOffset = len(g.history)
	}
}

// ScrollViewDown scrolls the view down in scrollback
func (g *Grid) ScrollViewDown(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.scrollOffset -= n
	if g.scrollOffset < 0 {
		g.scrollOffset = 0
	}
}

// ResetScrollOffset resets the scroll view to the bottom
func (g *Grid) ResetScrollOffset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.scrollOffset = 0
}

// GetScrollOffset returns the current scroll offset
func (g *Grid) GetScrollOffset() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.scrollOffset
}

// DisplayCell returns the cell at display position (accounting for scrollback)
func (g *Grid) DisplayCell(col, row int) Cell {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.displayCellLocked(col, row)
}

func (g *Grid) displayCellLocked(col, row int) Cell {
	if g.scrollOffset == 0 {
		if col < 0 || col >= g.Cols || row < 0 || row >= g.Rows {
			return NewCellWithBg(g.eraseBg)
		}
		return g.getCell(g.index(col, row))
	}

	// Calculate scrollback position within the unified history+active sequence.
	histRow := len(g.history) - g.scrollOffset + row
	if histRow < 0 {
		return NewCellWithBg(g.eraseBg)
	}
	if histRow < len(g.history) {
		r := g.history[histRow]
		if col >= 0 && col < len(r.cells) {
			return g.inflate(r.cells[col])
		}
		return NewCellWithBg(g.eraseBg)
	}

	gridRow := histRow - len(g.history)
	if gridRow >= g.Rows || col < 0 || col >= g.Cols {
		return NewCellWithBg(g.eraseBg)
	}
	return g.getCell(g.index(col, gridRow))
}

// VisibleText returns the visible grid as plain text.
func (g *Grid) VisibleText() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	lines := make([]string, g.Rows)
	for row := 0; row < g.Rows; row++ {
		var b strings.Builder
		b.Grow(g.Cols)
		for col := 0; col < g.Cols; col++ {
			cell := g.displayCellLocked(col, row)
			ch := cell.Char
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
			for _, cm := range cell.Combining {
				b.WriteRune(cm)
			}
		}
		lines[row] = strings.TrimRight(b.String(), " ")
	}

	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// SetSelection sets the selection bounds in display coordinates.
func (g *Grid) SetSelection(startCol, startRow, endCol, endRow int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Cols == 0 || g.Rows == 0 {
		return
	}

	startCol = clampInt(startCol, 0, g.Cols-1)
	endCol = clampInt(endCol, 0, g.Cols-1)
	startRow = clampInt(startRow, 0, g.Rows-1)
	endRow = clampInt(endRow, 0, g.Rows-1)

	g.selectionActive = true
	g.selectionStartCol = startCol
	g.selectionStartRow = startRow
	g.selectionEndCol = endCol
	g.selectionEndRow = endRow
	g.selectionScrollOffset = g.scrollOffset
}

// ClearSelection clears any active selection.
func (g *Grid) ClearSelection() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.selectionActive = false
}

// HasSelection returns whether a selection is active.
func (g *Grid) HasSelection() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.selectionActive
}

// IsSelected returns whether a display cell is within the current selection.
func (g *Grid) IsSelected(col, row int) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.isSelectedLocked(col, row)
}

func (g *Grid) isSelectedLocked(col, row int) bool {
	if !g.selectionActive || g.scrollOffset != g.selectionScrollOffset {
		return false
	}

	startCol, startRow := g.selectionStartCol, g.selectionStartRow
	endCol, endRow := g.selectionEndCol, g.selectionEndRow
	if endRow < startRow || (endRow == startRow && endCol < startCol) {
		startCol, endCol = endCol, startCol
		startRow, endRow = endRow, startRow
	}

	if row < startRow || row > endRow {
		return false
	}
	if startRow == endRow {
		return col >= startCol && col <= endCol
	}
	if row == startRow {
		return col >= startCol
	}
	if row == endRow {
		return col <= endCol
	}
	return true
}

// SelectedText returns the text within the current selection.
func (g *Grid) SelectedText() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.selectionActive || g.scrollOffset != g.selectionScrollOffset {
		return ""
	}

	startCol, startRow := g.selectionStartCol, g.selectionStartRow
	endCol, endRow := g.selectionEndCol, g.selectionEndRow
	if endRow < startRow || (endRow == startRow && endCol < startCol) {
		startCol, endCol = endCol, startCol
		startRow, endRow = endRow, startRow
	}

	var lines []string
	for row := startRow; row <= endRow; row++ {
		colStart := 0
		colEnd := g.Cols - 1
		if row == startRow {
			colStart = startCol
		}
		if row == endRow {
			colEnd = endCol
		}
		if colEnd < colStart {
			continue
		}

		var b strings.Builder
		b.Grow(colEnd - colStart + 1)
		for col := colStart; col <= colEnd; col++ {
			cell := g.displayCellLocked(col, row)
			ch := cell.Char
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
			for _, cm := range cell.Combining {
				b.WriteRune(cm)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}

	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
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

// ClearAll clears the entire grid
func (g *Grid) ClearAll() {
	g.ClearAllWithBg(g.eraseBg)
}

// ClearToEnd clears from cursor to end of screen
func (g *Grid) ClearToEnd() {
	g.ClearToEndWithBg(g.eraseBg)
}

// ClearToStart clears from start of screen to cursor
func (g *Grid) ClearToStart() {
	g.ClearToStartWithBg(g.eraseBg)
}

// ClearLine clears the current line
func (g *Grid) ClearLine() {
	g.ClearLineWithBg(g.eraseBg)
}

// ClearLineToEnd clears from cursor to end of line
func (g *Grid) ClearLineToEnd() {
	g.ClearLineToEndWithBg(g.eraseBg)
}

// ClearLineToStart clears from start of line to cursor
func (g *Grid) ClearLineToStart() {
	g.ClearLineToStartWithBg(g.eraseBg)
}

// ClearAllWithBg clears the entire grid with a specific background color (BCE)
func (g *Grid) ClearAllWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Save non-empty rows to scrollback before clearing. A saved row's *Row is
	// moved into history (refs travel with it) and replaced by a fresh blank;
	// empty rows are recycled in place.
	for row := 0; row < g.Rows; row++ {
		r := g.rows[row]
		hasContent := false
		for col := 0; col < len(r.cells); col++ {
			ch := r.cells[col].Char
			if ch != ' ' && ch != 0 {
				hasContent = true
				break
			}
		}
		if hasContent {
			g.pushHistory(r)
			g.rows[row] = g.blankRow(bg)
		} else {
			g.clearRow(r, bg)
		}
	}
}

// ClearToEndWithBg clears from cursor to end of screen with background color (BCE)
func (g *Grid) ClearToEndWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Clear rest of current line
	for col := g.CursorCol; col < g.Cols; col++ {
		g.putCell(g.index(col, g.CursorRow), NewCellWithBg(bg))
	}
	// Clear lines below
	for row := g.CursorRow + 1; row < g.Rows; row++ {
		for col := 0; col < g.Cols; col++ {
			g.putCell(g.index(col, row), NewCellWithBg(bg))
		}
	}
}

// ClearToStartWithBg clears from start of screen to cursor with background color (BCE)
func (g *Grid) ClearToStartWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Clear lines above
	for row := 0; row < g.CursorRow; row++ {
		for col := 0; col < g.Cols; col++ {
			g.putCell(g.index(col, row), NewCellWithBg(bg))
		}
	}
	// Clear start of current line
	for col := 0; col <= g.CursorCol; col++ {
		g.putCell(g.index(col, g.CursorRow), NewCellWithBg(bg))
	}
}

// ClearLineWithBg clears the current line with background color (BCE)
func (g *Grid) ClearLineWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for col := 0; col < g.Cols; col++ {
		g.putCell(g.index(col, g.CursorRow), NewCellWithBg(bg))
	}
}

// ClearLineToEndWithBg clears from cursor to end of line with background color (BCE)
func (g *Grid) ClearLineToEndWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for col := g.CursorCol; col < g.Cols; col++ {
		g.putCell(g.index(col, g.CursorRow), NewCellWithBg(bg))
	}
}

// ClearLineToStartWithBg clears from start of line to cursor with background color (BCE)
func (g *Grid) ClearLineToStartWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for col := 0; col <= g.CursorCol; col++ {
		g.putCell(g.index(col, g.CursorRow), NewCellWithBg(bg))
	}
}

// DeleteChars deletes n characters at cursor, shifting left
func (g *Grid) DeleteChars(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// If cursor is on a continuation cell, clear the wide char first
	if g.CursorCol > 0 {
		idx := g.index(g.CursorCol, g.CursorRow)
		if g.cellAt(idx).Width == CellWidthContinuation {
			// Clear the wide character (both cells)
			g.putCell(g.index(g.CursorCol-1, g.CursorRow), NewCellWithBg(g.eraseBg))
			g.putCell(idx, NewCellWithBg(g.eraseBg))
		}
	}

	// Check if the end of deletion range would break a wide character
	endPos := g.CursorCol + n
	if endPos < g.Cols {
		idx := g.index(endPos, g.CursorRow)
		if g.cellAt(idx).Width == CellWidthContinuation {
			// Would break a wide char - clear it first
			g.putCell(g.index(endPos-1, g.CursorRow), NewCellWithBg(g.eraseBg))
			g.putCell(idx, NewCellWithBg(g.eraseBg))
		}
	}

	// Now perform the shift (move transfers style ownership)
	for col := g.CursorCol; col < g.Cols-n; col++ {
		g.moveCell(g.index(col, g.CursorRow), g.index(col+n, g.CursorRow))
	}
	for col := g.Cols - n; col < g.Cols; col++ {
		g.putCell(g.index(col, g.CursorRow), NewCellWithBg(g.eraseBg))
	}
}

// InsertChars inserts n blank characters at cursor, shifting right
func (g *Grid) InsertChars(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// If cursor is on a continuation cell, clear the wide char first
	if g.CursorCol > 0 {
		idx := g.index(g.CursorCol, g.CursorRow)
		if g.cellAt(idx).Width == CellWidthContinuation {
			g.putCell(g.index(g.CursorCol-1, g.CursorRow), NewCellWithBg(g.eraseBg))
			g.putCell(idx, NewCellWithBg(g.eraseBg))
		}
	}

	// Check if shifting would break a wide character at the end
	// If the last cell that would be kept is a wide char start, it would lose its continuation
	if g.Cols-n >= 0 && g.Cols-n < g.Cols {
		idx := g.index(g.Cols-n, g.CursorRow)
		if idx >= 0 && idx < g.Cols*g.Rows && g.cellAt(idx).Width == CellWidthWide {
			g.putCell(idx, NewCellWithBg(g.eraseBg))
		}
	}

	// Shift right (move transfers style ownership)
	for col := g.Cols - 1; col >= g.CursorCol+n; col-- {
		g.moveCell(g.index(col, g.CursorRow), g.index(col-n, g.CursorRow))
	}
	// Clear inserted positions
	for col := g.CursorCol; col < g.CursorCol+n && col < g.Cols; col++ {
		g.putCell(g.index(col, g.CursorRow), NewCellWithBg(g.eraseBg))
	}
}

// DeleteLines deletes n lines at cursor within scroll region, shifting up
func (g *Grid) DeleteLines(n int) {
	g.DeleteLinesWithBg(n, DefaultBg())
}

// DeleteLinesWithBg deletes n lines at cursor with BCE support
func (g *Grid) DeleteLinesWithBg(n int, bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()

	top := g.scrollTop - 1 // Convert to 0-based
	bottom := g.scrollBottom - 1

	// Cursor must be within scroll region
	if g.CursorRow < top || g.CursorRow > bottom {
		return
	}

	// Clamp n to not exceed remaining lines in region
	if g.CursorRow+n > bottom+1 {
		n = bottom + 1 - g.CursorRow
	}

	// Shift lines up within the scroll region (move transfers style ownership)
	for row := g.CursorRow; row <= bottom-n; row++ {
		for col := 0; col < g.Cols; col++ {
			g.moveCell(g.index(col, row), g.index(col, row+n))
		}
	}

	// Clear bottom n lines of the scroll region with background color
	for row := bottom - n + 1; row <= bottom; row++ {
		for col := 0; col < g.Cols; col++ {
			g.putCell(g.index(col, row), NewCellWithBg(bg))
		}
	}
}

// InsertLines inserts n blank lines at cursor within scroll region, shifting down
func (g *Grid) InsertLines(n int) {
	g.InsertLinesWithBg(n, DefaultBg())
}

// InsertLinesWithBg inserts n blank lines at cursor with BCE support
func (g *Grid) InsertLinesWithBg(n int, bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()

	top := g.scrollTop - 1 // Convert to 0-based
	bottom := g.scrollBottom - 1

	// Cursor must be within scroll region
	if g.CursorRow < top || g.CursorRow > bottom {
		return
	}

	// Clamp n to not exceed remaining lines in region
	if g.CursorRow+n > bottom+1 {
		n = bottom + 1 - g.CursorRow
	}

	// Shift lines down within the scroll region (move transfers style ownership)
	for row := bottom; row >= g.CursorRow+n; row-- {
		for col := 0; col < g.Cols; col++ {
			g.moveCell(g.index(col, row), g.index(col, row-n))
		}
	}

	// Clear n lines at cursor position with background color
	for row := g.CursorRow; row < g.CursorRow+n && row <= bottom; row++ {
		for col := 0; col < g.Cols; col++ {
			g.putCell(g.index(col, row), NewCellWithBg(bg))
		}
	}
}

// Resize resizes the grid
func (g *Grid) Resize(cols, rows int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if cols == g.Cols && rows == g.Rows {
		return
	}
	g.wrapPending = false

	// The main screen reflows wrapped lines to the new width; the alternate
	// screen (no scrollback) clamps/truncates, since TUI apps repaint on resize.
	if g.maxScroll > 0 {
		g.reflow(cols, rows)
		return
	}

	// Track if scroll region was full-screen before resize
	wasFullScreen := (g.scrollTop == 1 && g.scrollBottom == g.Rows)
	oldScrollTop := g.scrollTop
	oldScrollBottom := g.scrollBottom

	// Clamp/truncate path (alternate screen): copy the overlap region.
	newRows := make([]*Row, rows)
	keepCols := min(cols, g.Cols)
	keepRows := min(rows, g.Rows)
	for row := 0; row < rows; row++ {
		nr := g.blankRow(g.eraseBg)
		if row < keepRows {
			src := g.rows[row]
			for col := 0; col < keepCols; col++ {
				g.styles.retain(src.cells[col].Style)
				g.styles.release(nr.cells[col].Style)
				nr.cells[col] = src.cells[col]
			}
		}
		newRows[row] = nr
	}
	// Release every old active row's references.
	for _, r := range g.rows {
		g.releaseRow(r)
	}

	g.rows = newRows
	oldRows := g.Rows
	g.Cols = cols
	g.Rows = rows
	g.tabStops = nil // rebuilt lazily at the new width

	// Smart scroll region handling
	if wasFullScreen {
		// Keep scroll region as full-screen after resize
		g.scrollTop = 1
		g.scrollBottom = rows
	} else {
		// Custom scroll region: preserve if still valid
		g.scrollTop = oldScrollTop
		g.scrollBottom = oldScrollBottom

		// Clamp scroll region to new bounds
		if g.scrollTop > rows {
			g.scrollTop = 1
		}
		if g.scrollBottom > rows {
			g.scrollBottom = rows
		}
		// If region becomes invalid, reset to full screen
		if g.scrollTop >= g.scrollBottom {
			g.scrollTop = 1
			g.scrollBottom = rows
		}
	}
	_ = oldRows // Suppress unused variable warning

	// Clamp cursor
	if g.CursorCol >= cols {
		g.CursorCol = cols - 1
	}
	if g.CursorRow >= rows {
		g.CursorRow = rows - 1
	}
}

// GetCursor returns the current cursor position
func (g *Grid) GetCursor() (col, row int) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.CursorCol, g.CursorRow
}

// AbsoluteCursorRow returns the cursor's row in absolute coordinates (rows ever
// scrolled out + current history + active offset), for anchoring image
// placements so they scroll with content.
func (g *Grid) AbsoluteCursorRow() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.scrolledOut + len(g.history) + g.CursorRow
}

// LinesScrolledOut returns the number of rows trimmed off the top of history.
func (g *Grid) LinesScrolledOut() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.scrolledOut
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// EraseChars erases n characters at cursor without moving cursor
func (g *Grid) EraseChars(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	startCol := g.CursorCol
	endCol := g.CursorCol + n
	if endCol > g.Cols {
		endCol = g.Cols
	}

	// If we start on a continuation cell, include the wide char start
	if startCol > 0 {
		idx := g.index(startCol, g.CursorRow)
		if g.cellAt(idx).Width == CellWidthContinuation {
			startCol--
		}
	}

	// If we end on a wide char start, include the continuation cell
	if endCol < g.Cols && endCol > 0 {
		idx := g.index(endCol-1, g.CursorRow)
		if g.cellAt(idx).Width == CellWidthWide {
			endCol++
		}
	}

	// Erase the range
	for col := startCol; col < endCol && col < g.Cols; col++ {
		g.putCell(g.index(col, g.CursorRow), NewCellWithBg(g.eraseBg))
	}
}

// RepeatChar repeats the last written character n times
func (g *Grid) RepeatChar(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := 0; i < n; i++ {
		if g.wrapPending {
			if g.autoWrap {
				g.cursorNewline()
			}
			g.wrapPending = false
		}
		if g.CursorCol >= g.Cols {
			if g.autoWrap {
				g.cursorNewline()
			} else {
				g.CursorCol = g.Cols - 1
			}
		}
		idx := g.index(g.CursorCol, g.CursorRow)
		g.putCell(idx, Cell{
			Char:           g.lastChar,
			Fg:             g.lastFg,
			Bg:             g.lastBg,
			UnderlineColor: g.lastUnderlineColor,
			Flags:          g.lastFlags,
			Width:          CellWidthNormal,
			UnderlineStyle: g.lastUnderlineStyle,
			Link:           g.lastLink,
		})
		g.CursorCol++
		if g.CursorCol >= g.Cols {
			if g.autoWrap {
				g.wrapPending = true
			}
			g.CursorCol = g.Cols - 1
		}
	}
}

// SetScrollRegion sets the scrolling region (1-based, inclusive)
func (g *Grid) SetScrollRegion(top, bottom int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if top < 1 {
		top = 1
	}
	if bottom > g.Rows {
		bottom = g.Rows
	}
	if top < bottom {
		g.scrollTop = top
		g.scrollBottom = bottom
	}
	// Move cursor to home position
	g.CursorCol = 0
	g.CursorRow = 0
}

// RestoreScrollRegion sets the scroll region without moving the cursor.
// Unlike SetScrollRegion (which resets cursor to 0,0 per DECSTBM spec),
// this preserves cursor position — used when restoring state during
// alternate screen exit.
func (g *Grid) RestoreScrollRegion(top, bottom int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if top < 1 {
		top = 1
	}
	if bottom > g.Rows {
		bottom = g.Rows
	}
	if top < bottom {
		g.scrollTop = top
		g.scrollBottom = bottom
	}
}

// ResetWrapPending clears the wrapPending flag.
// Used after restoring the main grid to avoid stale wrap state.
func (g *Grid) ResetWrapPending() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.wrapPending = false
}

// GetScrollRegion returns the current scroll region
func (g *Grid) GetScrollRegion() (top, bottom int) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.scrollTop, g.scrollBottom
}

// SetAutoWrap sets the auto-wrap mode (DECAWM ?7)
func (g *Grid) SetAutoWrap(enabled bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.autoWrap = enabled
}

// GetAutoWrap returns the current auto-wrap mode
func (g *Grid) GetAutoWrap() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.autoWrap
}

// SetEraseBackground sets the background color for BCE (Background Color Erase)
func (g *Grid) SetEraseBackground(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.eraseBg = bg
}

// GetEraseBackground returns the current BCE background color
func (g *Grid) GetEraseBackground() Color {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.eraseBg
}
