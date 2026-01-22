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
	Char  rune
	Fg    Color
	Bg    Color
	Flags CellFlags
	Width uint8 // 0=continuation cell, 1=normal width, 2=wide cell start
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

// Grid represents the terminal grid buffer
type Grid struct {
	cells        []Cell
	Cols         int
	Rows         int
	CursorCol    int
	CursorRow    int
	scrollback   [][]Cell
	scrollOffset int
	mu           sync.RWMutex

	// Scroll region (1-based, inclusive)
	scrollTop    int
	scrollBottom int

	// Last written character for REP sequence
	lastChar  rune
	lastFg    Color
	lastBg    Color
	lastFlags CellFlags

	// Selection state (display coordinates)
	selectionActive       bool
	selectionStartCol     int
	selectionStartRow     int
	selectionEndCol       int
	selectionEndRow       int
	selectionScrollOffset int

	// Auto-wrap mode (DECAWM ?7) - default true
	autoWrap bool
}

// NewGrid creates a new grid with the given dimensions
func NewGrid(cols, rows int) *Grid {
	cells := make([]Cell, cols*rows)
	for i := range cells {
		cells[i] = NewCell()
	}
	return &Grid{
		cells:        cells,
		Cols:         cols,
		Rows:         rows,
		CursorCol:    0,
		CursorRow:    0,
		scrollback:   make([][]Cell, 0, MaxScrollback),
		scrollOffset: 0,
		scrollTop:    1,
		scrollBottom: rows,
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
		return NewCell()
	}
	return g.cells[g.index(col, row)]
}

// SetCell sets the cell at the given position
func (g *Grid) SetCell(col, row int, cell Cell) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if col < 0 || col >= g.Cols || row < 0 || row >= g.Rows {
		return
	}
	g.cells[g.index(col, row)] = cell
}

// WriteChar writes a character at the cursor position and advances
func (g *Grid) WriteChar(c rune, fg, bg Color, flags CellFlags) {
	g.mu.Lock()
	defer g.mu.Unlock()

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
		// Zero-width character (combining mark) - ignore for now
		// Future: could append to previous cell's char
		return
	}

	// Check if wide character fits on current line
	if charWidth == 2 && g.CursorCol >= g.Cols-1 {
		if g.autoWrap {
			// Wide char at last column - fill with space and wrap
			idx := g.index(g.CursorCol, g.CursorRow)
			g.cells[idx] = Cell{
				Char:  ' ',
				Fg:    g.lastFg,
				Bg:    g.lastBg,
				Width: CellWidthNormal,
			}
			g.cursorNewline()
		} else {
			// No auto-wrap: treat wide char as single width at last column
			charWidth = 1
		}
	}

	// Write the character to current cell
	idx := g.index(g.CursorCol, g.CursorRow)
	g.cells[idx] = Cell{
		Char:  c,
		Fg:    fg,
		Bg:    bg,
		Flags: flags,
		Width: uint8(charWidth),
	}
	g.CursorCol++

	// If wide character, write continuation cell
	if charWidth == 2 && g.CursorCol < g.Cols {
		contIdx := g.index(g.CursorCol, g.CursorRow)
		g.cells[contIdx] = Cell{
			Char:  ' ', // Placeholder for continuation
			Fg:    fg,
			Bg:    bg,
			Flags: flags,
			Width: CellWidthContinuation,
		}
		g.CursorCol++
	}

	// Save for REP sequence
	g.lastChar = c
	g.lastFg = fg
	g.lastBg = bg
	g.lastFlags = flags
}

// cursorNewline moves cursor to next line (internal, no lock)
func (g *Grid) cursorNewline() {
	g.CursorCol = 0
	g.CursorRow++
	// Check if we're at the bottom of the scroll region
	if g.CursorRow >= g.scrollBottom {
		g.scrollUpRegion()
		g.CursorRow = g.scrollBottom - 1
	} else if g.CursorRow >= g.Rows {
		g.scrollUpInternal()
		g.CursorRow = g.Rows - 1
	}
}

// scrollUpRegion scrolls only within the scroll region
func (g *Grid) scrollUpRegion() {
	if g.scrollTop == 1 && g.scrollBottom == g.Rows {
		g.scrollUpInternal()
		return
	}

	top := g.scrollTop - 1 // Convert to 0-based
	bottom := g.scrollBottom - 1

	// Shift rows up within region
	for row := top; row < bottom; row++ {
		for col := 0; col < g.Cols; col++ {
			g.cells[g.index(col, row)] = g.cells[g.index(col, row+1)]
		}
	}

	// Clear bottom row of region
	for col := 0; col < g.Cols; col++ {
		g.cells[g.index(col, bottom)] = NewCell()
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
	g.CursorCol = 0
}

// Backspace moves cursor back one position, skipping continuation cells
func (g *Grid) Backspace() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.CursorCol > 0 {
		g.CursorCol--
		// If we landed on a continuation cell, move back one more
		if g.CursorCol > 0 {
			idx := g.index(g.CursorCol, g.CursorRow)
			if g.cells[idx].Width == CellWidthContinuation {
				g.CursorCol--
			}
		}
	}
}

// Tab moves cursor to next tab stop (8 columns)
func (g *Grid) Tab() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.CursorCol = ((g.CursorCol / 8) + 1) * 8
	if g.CursorCol >= g.Cols {
		g.CursorCol = g.Cols - 1
	}
	// Check if we landed on a continuation cell
	if g.CursorCol > 0 {
		idx := g.index(g.CursorCol, g.CursorRow)
		if g.cells[idx].Width == CellWidthContinuation {
			g.CursorCol--
		}
	}
}

// MoveCursor moves the cursor by the given delta, handling wide cells
func (g *Grid) MoveCursor(dCol, dRow int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Handle horizontal movement with wide cell awareness
	if dCol < 0 {
		// Moving left - skip continuation cells
		for i := 0; i > dCol && g.CursorCol > 0; i-- {
			g.CursorCol--
			// If we landed on a continuation cell, move back one more
			if g.CursorCol > 0 {
				idx := g.index(g.CursorCol, g.CursorRow)
				if g.cells[idx].Width == CellWidthContinuation {
					g.CursorCol--
				}
			}
		}
	} else if dCol > 0 {
		// Moving right - skip over wide characters properly
		for i := 0; i < dCol && g.CursorCol < g.Cols-1; i++ {
			idx := g.index(g.CursorCol, g.CursorRow)
			if g.cells[idx].Width == CellWidthWide {
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
		if g.cells[idx].Width == CellWidthContinuation {
			g.CursorCol--
		}
	}
}

// scrollUpInternal scrolls the grid up by one line (internal, no lock)
func (g *Grid) scrollUpInternal() {
	// Save top row to scrollback
	topRow := make([]Cell, g.Cols)
	copy(topRow, g.cells[0:g.Cols])
	g.scrollback = append(g.scrollback, topRow)

	// Trim scrollback if too large
	if len(g.scrollback) > MaxScrollback {
		g.scrollback = g.scrollback[1:]
	}

	// Shift rows up
	copy(g.cells, g.cells[g.Cols:])

	// Clear bottom row
	for i := (g.Rows - 1) * g.Cols; i < g.Rows*g.Cols; i++ {
		g.cells[i] = NewCell()
	}
}

// ScrollUp scrolls the grid up by n lines
func (g *Grid) ScrollUp(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := 0; i < n; i++ {
		g.scrollUpInternal()
	}
}

// ScrollDown scrolls the grid down by n lines
func (g *Grid) ScrollDown(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := 0; i < n; i++ {
		// Shift rows down
		copy(g.cells[g.Cols:], g.cells[:len(g.cells)-g.Cols])

		// Clear top row
		for j := 0; j < g.Cols; j++ {
			g.cells[j] = NewCell()
		}
	}
}

// ScrollViewUp scrolls the view up in scrollback
func (g *Grid) ScrollViewUp(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.scrollOffset += n
	if g.scrollOffset > len(g.scrollback) {
		g.scrollOffset = len(g.scrollback)
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
			return NewCell()
		}
		return g.cells[g.index(col, row)]
	}

	// Calculate scrollback position
	scrollbackRow := len(g.scrollback) - g.scrollOffset + row
	if scrollbackRow < 0 {
		return NewCell()
	}
	if scrollbackRow < len(g.scrollback) {
		if col < len(g.scrollback[scrollbackRow]) {
			return g.scrollback[scrollbackRow][col]
		}
		return NewCell()
	}

	gridRow := scrollbackRow - len(g.scrollback)
	if gridRow >= g.Rows || col >= g.Cols {
		return NewCell()
	}
	return g.cells[g.index(col, gridRow)]
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
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := range g.cells {
		g.cells[i] = NewCell()
	}
}

// ClearToEnd clears from cursor to end of screen
func (g *Grid) ClearToEnd() {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Clear rest of current line
	for col := g.CursorCol; col < g.Cols; col++ {
		g.cells[g.index(col, g.CursorRow)] = NewCell()
	}
	// Clear lines below
	for row := g.CursorRow + 1; row < g.Rows; row++ {
		for col := 0; col < g.Cols; col++ {
			g.cells[g.index(col, row)] = NewCell()
		}
	}
}

// ClearToStart clears from start of screen to cursor
func (g *Grid) ClearToStart() {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Clear lines above
	for row := 0; row < g.CursorRow; row++ {
		for col := 0; col < g.Cols; col++ {
			g.cells[g.index(col, row)] = NewCell()
		}
	}
	// Clear start of current line
	for col := 0; col <= g.CursorCol; col++ {
		g.cells[g.index(col, g.CursorRow)] = NewCell()
	}
}

// ClearLine clears the current line
func (g *Grid) ClearLine() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for col := 0; col < g.Cols; col++ {
		g.cells[g.index(col, g.CursorRow)] = NewCell()
	}
}

// ClearLineToEnd clears from cursor to end of line
func (g *Grid) ClearLineToEnd() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for col := g.CursorCol; col < g.Cols; col++ {
		g.cells[g.index(col, g.CursorRow)] = NewCell()
	}
}

// ClearLineToStart clears from start of line to cursor
func (g *Grid) ClearLineToStart() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for col := 0; col <= g.CursorCol; col++ {
		g.cells[g.index(col, g.CursorRow)] = NewCell()
	}
}

// DeleteChars deletes n characters at cursor, shifting left
func (g *Grid) DeleteChars(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// If cursor is on a continuation cell, clear the wide char first
	if g.CursorCol > 0 {
		idx := g.index(g.CursorCol, g.CursorRow)
		if g.cells[idx].Width == CellWidthContinuation {
			// Clear the wide character (both cells)
			g.cells[g.index(g.CursorCol-1, g.CursorRow)] = NewCell()
			g.cells[idx] = NewCell()
		}
	}

	// Check if the end of deletion range would break a wide character
	endPos := g.CursorCol + n
	if endPos < g.Cols {
		idx := g.index(endPos, g.CursorRow)
		if g.cells[idx].Width == CellWidthContinuation {
			// Would break a wide char - clear it first
			g.cells[g.index(endPos-1, g.CursorRow)] = NewCell()
			g.cells[idx] = NewCell()
		}
	}

	// Now perform the shift
	for col := g.CursorCol; col < g.Cols-n; col++ {
		g.cells[g.index(col, g.CursorRow)] = g.cells[g.index(col+n, g.CursorRow)]
	}
	for col := g.Cols - n; col < g.Cols; col++ {
		g.cells[g.index(col, g.CursorRow)] = NewCell()
	}
}

// InsertChars inserts n blank characters at cursor, shifting right
func (g *Grid) InsertChars(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// If cursor is on a continuation cell, clear the wide char first
	if g.CursorCol > 0 {
		idx := g.index(g.CursorCol, g.CursorRow)
		if g.cells[idx].Width == CellWidthContinuation {
			g.cells[g.index(g.CursorCol-1, g.CursorRow)] = NewCell()
			g.cells[idx] = NewCell()
		}
	}

	// Check if shifting would break a wide character at the end
	// If the last cell that would be kept is a wide char start, it would lose its continuation
	if g.Cols-n >= 0 && g.Cols-n < g.Cols {
		idx := g.index(g.Cols-n, g.CursorRow)
		if idx >= 0 && idx < len(g.cells) && g.cells[idx].Width == CellWidthWide {
			g.cells[idx] = NewCell()
		}
	}

	// Shift right
	for col := g.Cols - 1; col >= g.CursorCol+n; col-- {
		g.cells[g.index(col, g.CursorRow)] = g.cells[g.index(col-n, g.CursorRow)]
	}
	// Clear inserted positions
	for col := g.CursorCol; col < g.CursorCol+n && col < g.Cols; col++ {
		g.cells[g.index(col, g.CursorRow)] = NewCell()
	}
}

// DeleteLines deletes n lines at cursor, shifting up
func (g *Grid) DeleteLines(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for row := g.CursorRow; row < g.Rows-n; row++ {
		for col := 0; col < g.Cols; col++ {
			g.cells[g.index(col, row)] = g.cells[g.index(col, row+n)]
		}
	}
	for row := g.Rows - n; row < g.Rows; row++ {
		for col := 0; col < g.Cols; col++ {
			g.cells[g.index(col, row)] = NewCell()
		}
	}
}

// InsertLines inserts n blank lines at cursor, shifting down
func (g *Grid) InsertLines(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for row := g.Rows - 1; row >= g.CursorRow+n; row-- {
		for col := 0; col < g.Cols; col++ {
			g.cells[g.index(col, row)] = g.cells[g.index(col, row-n)]
		}
	}
	for row := g.CursorRow; row < g.CursorRow+n && row < g.Rows; row++ {
		for col := 0; col < g.Cols; col++ {
			g.cells[g.index(col, row)] = NewCell()
		}
	}
}

// Resize resizes the grid
func (g *Grid) Resize(cols, rows int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	newCells := make([]Cell, cols*rows)
	for i := range newCells {
		newCells[i] = NewCell()
	}

	// Copy existing cells
	for row := 0; row < min(rows, g.Rows); row++ {
		for col := 0; col < min(cols, g.Cols); col++ {
			newCells[row*cols+col] = g.cells[row*g.Cols+col]
		}
	}

	g.cells = newCells
	g.Cols = cols
	g.Rows = rows

	// Reset scroll region to full screen
	g.scrollTop = 1
	g.scrollBottom = rows

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
		if g.cells[idx].Width == CellWidthContinuation {
			startCol--
		}
	}

	// If we end on a wide char start, include the continuation cell
	if endCol < g.Cols && endCol > 0 {
		idx := g.index(endCol-1, g.CursorRow)
		if g.cells[idx].Width == CellWidthWide {
			endCol++
		}
	}

	// Erase the range
	for col := startCol; col < endCol && col < g.Cols; col++ {
		g.cells[g.index(col, g.CursorRow)] = NewCell()
	}
}

// RepeatChar repeats the last written character n times
func (g *Grid) RepeatChar(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := 0; i < n; i++ {
		if g.CursorCol >= g.Cols {
			g.cursorNewline()
		}
		idx := g.index(g.CursorCol, g.CursorRow)
		g.cells[idx] = Cell{
			Char:  g.lastChar,
			Fg:    g.lastFg,
			Bg:    g.lastBg,
			Flags: g.lastFlags,
			Width: CellWidthNormal,
		}
		g.CursorCol++
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
