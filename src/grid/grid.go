package grid

import (
	"strings"
	"sync"
	"unicode"
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
	Width          uint8  // 0=continuation cell, 1=normal width, 2=wide cell start
	UnderlineStyle uint8  // 0=default/solid, 1=solid, 2=double, 3=curly, 4=dotted, 5=dashed (SGR 4:n)
	Link           uint16 // OSC 8 hyperlink id (0=none); resolve via Grid.LinkURL
	Combining      []rune // codepoints after Char (combining marks / ZWJ cont.); nil for none
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
	history      []*Row // scrollback (oldest first), a window into histBuf
	histBuf      []*Row // backing store for history; see pushHistory
	maxScroll    int    // history cap in rows (0 = no scrollback, e.g. alt screen)
	freeRows     []*Row // recycled row allocations, all at current width
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
	lastChar  rune
	lastStyle Style
	lastLink  uint16

	// OSC 8 hyperlink interning: url <-> compact id stored on cells
	links      map[string]uint16
	linkURLs   map[uint16]string
	nextLinkID uint16

	// Grapheme interning: cluster (base + combining/ZWJ) <-> compact id.
	// Append-only; index 0 = none. Combining clusters are rare so this stays small.
	graphemes   [][]rune
	graphemeMap map[string]uint32

	// Selection state. Anchors are stored in ABSOLUTE buffer coordinates:
	// absRow = scrolledOut + index into the history+screen continuum, the same
	// coordinate space AbsoluteCursorRow uses for image anchoring. Absolute rows
	// are stable when lines scroll from the screen into history (a screen row
	// pushed by scrollUp keeps its index) and when scrollback is trimmed
	// (scrolledOut absorbs the shift), so a selection survives both view
	// scrolling and new output. Rule for active-screen rows: an anchor keeps its
	// absolute position, so mid-screen region scrolls (TUIs) move text under a
	// fixed selection rectangle rather than dragging the selection along; and
	// reflow invalidates absolute rows entirely, so Resize clears the selection.
	selectionActive bool
	selAnchorAbsRow int // where the drag started
	selAnchorCol    int
	selEndAbsRow    int // current drag end (may precede the anchor)
	selEndCol       int

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

	// lastSnap records the visible state captured by the most recent Snapshot,
	// so RedrawNeeded can cheaply peek for changes without clearing anything.
	lastSnap snapState

	// snapInvalid forces the next Snapshot to fully re-copy the visible region
	// instead of only dirty rows. Set by paths that change the row-to-display
	// mapping without marking every affected row dirty (view scrolling, reflow).
	snapInvalid bool
}

// snapState is the non-content visible state recorded at Snapshot time
// (content changes are tracked separately via per-row RowDirty flags).
type snapState struct {
	valid                bool
	cols, rows           int
	cursorCol, cursorRow int
	scrollOffset         int
	selActive            bool
	sSCol, sSRow         int
	sECol, sERow         int
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
	return g.InternLinkLocked(url)
}

// InternLinkLocked is InternLink without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) InternLinkLocked(url string) uint16 {
	if url == "" {
		return 0
	}
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

// LockBatch acquires g.mu (write) for a multi-operation batch, letting the
// caller run the exported XxxLocked variants without paying a lock/unlock per
// operation. The parser uses this to hold the grid lock across a whole PTY
// chunk instead of once per grid op. Pair every LockBatch with exactly one
// UnlockBatch; while the batch is held, ONLY XxxLocked methods may be called
// on this grid (the locking public methods would self-deadlock).
//
// LOCK ORDER: Grid.mu is a leaf lock — grid code never calls out of the
// package. Terminal.mu is always acquired before Grid.mu (see
// parser.Terminal.Process / Snapshot); never take Terminal.mu while holding
// Grid.mu.
func (g *Grid) LockBatch() {
	g.mu.Lock()
}

// UnlockBatch releases the lock acquired by LockBatch.
func (g *Grid) UnlockBatch() {
	g.mu.Unlock()
}

// WriteChar writes a character at the cursor position and advances
func (g *Grid) WriteChar(c rune, fg, bg Color, flags CellFlags, link uint16, ulStyle uint8, ulColor Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.WriteCharLocked(c, fg, bg, flags, link, ulStyle, ulColor)
}

// WriteCharLocked is WriteChar without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) WriteCharLocked(c rune, fg, bg Color, flags CellFlags, link uint16, ulStyle uint8, ulColor Color) {
	sr := g.styles.ref(Style{Fg: fg, Bg: bg, UnderlineColor: ulColor, Flags: flags, UnderlineStyle: ulStyle})
	g.writeCharLocked(c, &sr, link)
	g.styles.done(sr)
}

// WriteRunes writes a run of runes sharing the same attributes, taking g.mu
// ONCE for the whole run instead of once per rune. This is the parser's batch
// path for contiguous printable output (the cat-largefile hot path); all other
// public Grid methods keep their per-call locking.
//
// LOCK ORDER: the parser calls this while holding Terminal.mu. Terminal.mu is
// always acquired before Grid.mu (see Terminal.Snapshot / parser.Process);
// never take Terminal.mu while holding Grid.mu.
func (g *Grid) WriteRunes(rs []rune, fg, bg Color, flags CellFlags, link uint16, ulStyle uint8, ulColor Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.WriteRunesLocked(rs, fg, bg, flags, link, ulStyle, ulColor)
}

// WriteRunesLocked is WriteRunes without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) WriteRunesLocked(rs []rune, fg, bg Color, flags CellFlags, link uint16, ulStyle uint8, ulColor Color) {
	// Intern the shared style ONCE for the whole run; each cell then takes a
	// reference through the handle rather than re-deriving and re-hashing it.
	sr := g.styles.ref(Style{Fg: fg, Bg: bg, UnderlineColor: ulColor, Flags: flags, UnderlineStyle: ulStyle})
	for _, c := range rs {
		g.writeCharLocked(c, &sr, link)
	}
	g.styles.done(sr)
}

// writeCharLocked is WriteChar's body; the caller must hold g.mu and pass a
// style handle it owns (see styleSet.ref). Each cell written takes its own
// reference from sr; the caller releases the handle when its run is done.
func (g *Grid) writeCharLocked(c rune, sr *styleRef, link uint16) {
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

	// Get character width. Printable ASCII (0x20-0x7e, the overwhelming
	// majority of terminal output) is always one cell; short-circuit it here
	// because RuneWidth can't inline (its fallback calls uniseg). Everything
	// else — controls, DEL, non-ASCII — goes through RuneWidth as before.
	charWidth := 1
	if c < 0x20 || c >= 0x7f {
		charWidth = RuneWidth(c)
		if charWidth == 0 {
			// Zero-width codepoint (combining mark / ZWJ continuation): attach
			// it to the most recently written cell instead of dropping it.
			g.appendCombining(c)
			return
		}
	}

	// Check if wide character fits on current line
	if charWidth == 2 && g.CursorCol >= g.Cols-1 {
		if g.autoWrap {
			// Wide char at last column - fill with space and wrap. The pad
			// takes the incoming character's style (not the previous one, as
			// g.lastStyle would give) so its background matches the cell it
			// stands in for; otherwise full-screen redraws leave stale
			// colors at the right edge.
			g.putCellStyledRow(g.rows[g.CursorRow], g.CursorCol, ' ', sr.take(), CellWidthNormal, link)
			// The line continues onto the next row: mark it soft-wrapped so
			// reflow can rejoin it on resize.
			g.rows[g.CursorRow].flags |= RowSoftWrapped
			g.cursorNewline()
		} else {
			// No auto-wrap: treat wide char as single width at last column
			charWidth = 1
		}
	}

	// Write the character to current cell. The cursor row/col are already known,
	// so index the row's cell slice directly (as fillSpan does) instead of
	// decoding a linear index with per-cell div/mod in cellAt/putCellStyled.
	row := g.rows[g.CursorRow]
	g.putCellStyledRow(row, g.CursorCol, c, sr.take(), uint8(charWidth), link)
	g.CursorCol++

	// If wide character, write continuation cell
	if charWidth == 2 && g.CursorCol < g.Cols {
		g.putCellStyledRow(row, g.CursorCol, ' ', sr.take(), CellWidthContinuation, link)
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
	g.lastStyle = sr.style
	g.lastLink = link
}

// WriteBytesLocked is WriteRunesLocked for a run of printable ASCII bytes
// (0x20-0x7e), skipping the caller's byte->[]rune conversion pass: each byte
// stores directly as rune(b). Callers must guarantee every byte is printable
// ASCII (width is then always 1); the parser's fast path does. The caller must
// hold g.mu (see LockBatch).
func (g *Grid) WriteBytesLocked(bs []byte, fg, bg Color, flags CellFlags, link uint16, ulStyle uint8, ulColor Color) {
	// Intern the shared style ONCE for the whole run, as in WriteRunesLocked.
	sr := g.styles.ref(Style{Fg: fg, Bg: bg, UnderlineColor: ulColor, Flags: flags, UnderlineStyle: ulStyle})
	for _, b := range bs {
		g.writeByteLocked(b, &sr, link)
	}
	g.styles.done(sr)
}

// writeByteLocked is writeCharLocked specialized for a printable ASCII byte
// (0x20-0x7e): width is always 1, so the width lookup, combining-mark, and
// wide-char branches are provably unreachable and dropped. The wrap,
// wrap-pending, dirty, and REP bookkeeping match writeCharLocked exactly.
// The caller must hold g.mu and pass a style handle it owns (see styleSet.ref).
func (g *Grid) writeByteLocked(b byte, sr *styleRef, link uint16) {
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

	row := g.rows[g.CursorRow]
	g.putCellStyledRow(row, g.CursorCol, rune(b), sr.take(), CellWidthNormal, link)
	g.CursorCol++

	// If we advanced past the last column, set wrap pending (DECAWM behavior)
	if g.CursorCol >= g.Cols {
		if g.autoWrap {
			g.wrapPending = true
		}
		g.CursorCol = g.Cols - 1
	}

	// Save for REP sequence
	g.lastChar = rune(b)
	g.lastStyle = sr.style
	g.lastLink = link
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
// cursorIndex moves the cursor down one row with line-feed/IND semantics:
// the column is PRESERVED. Resetting it is carriage return's job — raw-mode
// TUIs (nvim) send bare LF as a cheap "cursor down" after positioning, and
// homing the column here scatters their writes across column 1. The PTY's
// ONLCR translation hides that for cooked-mode shells, which is why the bug
// only showed inside full-screen apps.
func (g *Grid) cursorIndex() {
	g.wrapPending = false
	g.CursorRow++
	// Scroll only when the cursor crosses the bottom margin from inside the
	// scroll region. A cursor positioned below the region (possible when a TUI
	// keeps a status area outside the margins) moves down without scrolling and
	// stops at the last screen row.
	if g.CursorRow == g.scrollBottom {
		g.scrollUpRegionWithBg(g.eraseBg)
		g.CursorRow = g.scrollBottom - 1
	} else if g.CursorRow >= g.Rows {
		g.CursorRow = g.Rows - 1
	}
}

// cursorNewline wraps to the start of the next row (autowrap continuation:
// the only newline that legitimately resets the column).
func (g *Grid) cursorNewline() {
	g.CursorCol = 0
	g.cursorIndex()
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
	// Rotate region rows up. The moved rows land at new display positions, so
	// they must be re-copied by the next snapshot even though their cell
	// contents are untouched.
	for row := top; row < bottom; row++ {
		g.rows[row] = g.rows[row+1]
		g.rows[row].flags |= RowDirty
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
	g.NewlineLocked()
}

// NewlineLocked is Newline without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) NewlineLocked() {
	g.cursorIndex()
}

// CarriageReturn moves cursor to the beginning of the current line
func (g *Grid) CarriageReturn() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.CarriageReturnLocked()
}

// CarriageReturnLocked is CarriageReturn without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) CarriageReturnLocked() {
	g.wrapPending = false
	g.CursorCol = 0
}

// Backspace moves cursor back one position, skipping continuation cells
func (g *Grid) Backspace() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.BackspaceLocked()
}

// BackspaceLocked is Backspace without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) BackspaceLocked() {
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
	g.TabLocked()
}

// TabLocked is Tab without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) TabLocked() {
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
	g.MoveCursorLocked(dCol, dRow)
}

// MoveCursorLocked is MoveCursor without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) MoveCursorLocked(dCol, dRow int) {
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
	g.SetCursorPosLocked(col, row)
}

// SetCursorPosLocked is SetCursorPos without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) SetCursorPosLocked(col, row int) {
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
		// Moved to a new display row: force re-copy at the next snapshot.
		g.rows[row].flags |= RowDirty
	}
	g.rows[g.Rows-1] = g.blankRow(bg)
}

// ScrollUp scrolls the grid up by n lines within the scroll region
func (g *Grid) ScrollUp(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for range n {
		g.scrollUpRegion()
	}
}

// ScrollUpWithBg scrolls the grid up by n lines with BCE support
func (g *Grid) ScrollUpWithBg(n int, bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ScrollUpWithBgLocked(n, bg)
}

// ScrollUpWithBgLocked is ScrollUpWithBg without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ScrollUpWithBgLocked(n int, bg Color) {
	for range n {
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
		// Moved to a new display row: force re-copy at the next snapshot.
		g.rows[row].flags |= RowDirty
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
		// Moved to a new display row: force re-copy at the next snapshot.
		g.rows[row].flags |= RowDirty
	}
	g.clearRow(leaving, bg)
	g.rows[top] = leaving
}

// ScrollDown scrolls the grid down by n lines within the scroll region
func (g *Grid) ScrollDown(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for range n {
		g.scrollDownRegion()
	}
}

// ScrollDownWithBg scrolls the grid down by n lines with BCE support
func (g *Grid) ScrollDownWithBg(n int, bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ScrollDownWithBgLocked(n, bg)
}

// ScrollDownWithBgLocked is ScrollDownWithBg without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ScrollDownWithBgLocked(n int, bg Color) {
	for range n {
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
	g.snapInvalid = true
}

// ScrollViewDown scrolls the view down in scrollback
func (g *Grid) ScrollViewDown(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.scrollOffset -= n
	if g.scrollOffset < 0 {
		g.scrollOffset = 0
	}
	g.snapInvalid = true
}

// ResetScrollOffset resets the scroll view to the bottom
func (g *Grid) ResetScrollOffset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.scrollOffset = 0
	g.snapInvalid = true
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

// viewTopAbsLocked returns the absolute row displayed on viewport row 0.
func (g *Grid) viewTopAbsLocked() int {
	return g.scrolledOut + len(g.history) - g.scrollOffset
}

// rowAtAbsLocked resolves an absolute row to its *Row (nil if it scrolled out
// of history or lies beyond the active screen).
func (g *Grid) rowAtAbsLocked(abs int) *Row {
	idx := abs - g.scrolledOut
	if idx < 0 {
		return nil
	}
	if idx < len(g.history) {
		return g.history[idx]
	}
	idx -= len(g.history)
	if idx >= 0 && idx < len(g.rows) {
		return g.rows[idx]
	}
	return nil
}

// normalizedSelectionLocked returns the absolute selection range ordered so
// that (sRow,sCol) <= (eRow,eCol).
func (g *Grid) normalizedSelectionLocked() (sRow, sCol, eRow, eCol int) {
	sRow, sCol = g.selAnchorAbsRow, g.selAnchorCol
	eRow, eCol = g.selEndAbsRow, g.selEndCol
	if eRow < sRow || (eRow == sRow && eCol < sCol) {
		sRow, eRow = eRow, sRow
		sCol, eCol = eCol, sCol
	}
	return
}

// selectionViewLocked projects the absolute selection onto the current
// viewport, normalized and clamped to the visible rows. It is derived state:
// computing it on demand (rather than caching it) is what keeps the highlight
// in lockstep with SelectedText, whatever moved underneath it — view
// scrolling, new output, or scrollback trimming. active is false when there is
// no selection or it lies entirely off-screen.
func (g *Grid) selectionViewLocked() (active bool, sCol, sRow, eCol, eRow int) {
	if !g.selectionActive {
		return false, 0, 0, 0, 0
	}
	sAbs, sC, eAbs, eC := g.normalizedSelectionLocked()
	viewTop := g.viewTopAbsLocked()
	sR, eR := sAbs-viewTop, eAbs-viewTop
	if eR < 0 || sR >= g.Rows {
		return false, 0, 0, 0, 0
	}
	if sR < 0 {
		sR, sC = 0, 0
	}
	if eR >= g.Rows {
		eR, eC = g.Rows-1, g.Cols-1
	}
	return true, sC, sR, eC, eR
}

// AbsRowForViewRow translates a viewport row to an absolute buffer row.
func (g *Grid) AbsRowForViewRow(row int) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.viewTopAbsLocked() + row
}

// StartSelection anchors a new selection at a viewport cell.
func (g *Grid) StartSelection(col, row int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Cols == 0 || g.Rows == 0 {
		return
	}
	col = clampInt(col, 0, g.Cols-1)
	row = clampInt(row, 0, g.Rows-1)
	abs := g.viewTopAbsLocked() + row
	g.selectionActive = true
	g.selAnchorAbsRow, g.selAnchorCol = abs, col
	g.selEndAbsRow, g.selEndCol = abs, col
}

// ExtendSelection moves the selection end to a viewport cell, keeping the
// anchor (which may now live in scrollback) fixed.
func (g *Grid) ExtendSelection(col, row int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.selectionActive || g.Cols == 0 || g.Rows == 0 {
		return
	}
	col = clampInt(col, 0, g.Cols-1)
	row = clampInt(row, 0, g.Rows-1)
	g.selEndAbsRow = g.viewTopAbsLocked() + row
	g.selEndCol = col
}

// SetSelection sets the selection bounds in display (viewport) coordinates.
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

	viewTop := g.viewTopAbsLocked()
	g.selectionActive = true
	g.selAnchorAbsRow, g.selAnchorCol = viewTop+startRow, startCol
	g.selEndAbsRow, g.selEndCol = viewTop+endRow, endCol
}

// isWordRune reports whether a rune belongs to a double-click word:
// unicode letters/digits plus _ - . / ~.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) ||
		r == '_' || r == '-' || r == '.' || r == '/' || r == '~'
}

// SelectWordAt selects the word under a viewport cell (double-click). A cell
// holding a non-word character selects just that cell.
// ponytail: word expansion stays within the display row; crossing soft-wrap
// boundaries can be added if wrapped-URL selection ever matters.
func (g *Grid) SelectWordAt(col, row int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Cols == 0 || g.Rows == 0 {
		return
	}
	col = clampInt(col, 0, g.Cols-1)
	row = clampInt(row, 0, g.Rows-1)
	abs := g.viewTopAbsLocked() + row
	r := g.rowAtAbsLocked(abs)
	if r == nil || len(r.cells) == 0 {
		return
	}
	if col >= len(r.cells) {
		col = len(r.cells) - 1
	}
	// charAt resolves a column to its rune, mapping a continuation cell to its
	// wide-char base.
	charAt := func(c int) rune {
		if r.cells[c].Width == CellWidthContinuation && c > 0 {
			c--
		}
		ch := r.cells[c].Char
		if ch == 0 {
			ch = ' '
		}
		return ch
	}
	start, end := col, col
	if isWordRune(charAt(col)) {
		for start > 0 && isWordRune(charAt(start-1)) {
			start--
		}
		for end+1 < len(r.cells) && isWordRune(charAt(end+1)) {
			end++
		}
	}
	// Snap to wide-char cell boundaries so the highlight covers whole glyphs.
	if r.cells[start].Width == CellWidthContinuation && start > 0 {
		start--
	}
	if r.cells[end].Width == CellWidthWide && end+1 < len(r.cells) {
		end++
	}
	g.selectionActive = true
	g.selAnchorAbsRow, g.selAnchorCol = abs, start
	g.selEndAbsRow, g.selEndCol = abs, end
}

// SelectLineAt selects the full logical line (soft-wrap-joined) containing a
// viewport cell (triple-click).
func (g *Grid) SelectLineAt(col, row int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Cols == 0 || g.Rows == 0 {
		return
	}
	_ = col
	row = clampInt(row, 0, g.Rows-1)
	abs := g.viewTopAbsLocked() + row
	if g.rowAtAbsLocked(abs) == nil {
		return
	}
	first, last := abs, abs
	for {
		prev := g.rowAtAbsLocked(first - 1)
		if prev == nil || prev.flags&RowSoftWrapped == 0 {
			break
		}
		first--
	}
	for {
		cur := g.rowAtAbsLocked(last)
		if cur == nil || cur.flags&RowSoftWrapped == 0 || g.rowAtAbsLocked(last+1) == nil {
			break
		}
		last++
	}
	g.selectionActive = true
	g.selAnchorAbsRow, g.selAnchorCol = first, 0
	g.selEndAbsRow, g.selEndCol = last, g.Cols-1
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
// Cheap: a couple of compares after translating the viewport row to absolute.
func (g *Grid) IsSelected(col, row int) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.isSelectedLocked(col, row)
}

func (g *Grid) isSelectedLocked(col, row int) bool {
	if !g.selectionActive {
		return false
	}
	sRow, sCol, eRow, eCol := g.normalizedSelectionLocked()
	abs := g.viewTopAbsLocked() + row
	if abs < sRow || abs > eRow {
		return false
	}
	if sRow == eRow {
		return col >= sCol && col <= eCol
	}
	if abs == sRow {
		return col >= sCol
	}
	if abs == eRow {
		return col <= eCol
	}
	return true
}

// SelectedText returns the text within the current selection, walking absolute
// rows across history and the active screen. Soft-wrapped rows are joined
// without a newline; trailing spaces are trimmed only at hard line ends.
// Continuation cells of wide characters are skipped.
func (g *Grid) SelectedText() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.selectionActive {
		return ""
	}
	sRow, sCol, eRow, eCol := g.normalizedSelectionLocked()

	var out strings.Builder
	var line strings.Builder
	for abs := sRow; abs <= eRow; abs++ {
		r := g.rowAtAbsLocked(abs)
		if r == nil {
			continue
		}
		colStart := 0
		colEnd := len(r.cells) - 1
		if abs == sRow {
			colStart = clampInt(sCol, 0, len(r.cells)-1)
		}
		if abs == eRow {
			colEnd = clampInt(eCol, 0, len(r.cells)-1)
		}
		for col := colStart; col <= colEnd; col++ {
			if r.cells[col].Width == CellWidthContinuation {
				continue
			}
			cell := g.inflate(r.cells[col])
			ch := cell.Char
			if ch == 0 {
				ch = ' '
			}
			line.WriteRune(ch)
			for _, cm := range cell.Combining {
				line.WriteRune(cm)
			}
		}
		softWrapped := r.flags&RowSoftWrapped != 0 && abs < eRow
		if !softWrapped {
			// Hard line end (or end of selection): trim padding, flush.
			out.WriteString(strings.TrimRight(line.String(), " "))
			line.Reset()
			if abs < eRow {
				out.WriteByte('\n')
			}
		}
	}
	return strings.TrimRight(out.String(), "\n")
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
	g.ClearAllWithBgLocked(g.eraseBg)
}

// ClearAllLocked is ClearAll without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ClearAllLocked() {
	g.ClearAllWithBgLocked(g.eraseBg)
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

// ClearScrollback drops all scrollback history (ED 3 / CSI 3 J "erase saved
// lines"). The visible screen is untouched.
func (g *Grid) ClearScrollback() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ClearScrollbackLocked()
}

// ClearScrollbackLocked is ClearScrollback without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ClearScrollbackLocked() {
	for i, r := range g.history {
		g.releaseRow(r)
		g.history[i] = nil
	}
	// Keep absolute-row accounting consistent: the rows "left the top".
	g.scrolledOut += len(g.history)
	g.history = nil
	g.scrollOffset = 0
	g.snapInvalid = true
}

// ClearAllWithBg clears the entire grid with a specific background color (BCE)
func (g *Grid) ClearAllWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ClearAllWithBgLocked(bg)
}

// ClearAllWithBgLocked is ClearAllWithBg without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ClearAllWithBgLocked(bg Color) {

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
	g.ClearToEndWithBgLocked(bg)
}

// ClearToEndWithBgLocked is ClearToEndWithBg without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ClearToEndWithBgLocked(bg Color) {
	// Clear rest of current line
	g.fillSpan(g.CursorRow, g.CursorCol, g.Cols, bg)
	// Clear lines below
	for row := g.CursorRow + 1; row < g.Rows; row++ {
		g.fillSpan(row, 0, g.Cols, bg)
	}
}

// ClearToStartWithBg clears from start of screen to cursor with background color (BCE)
func (g *Grid) ClearToStartWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ClearToStartWithBgLocked(bg)
}

// ClearToStartWithBgLocked is ClearToStartWithBg without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ClearToStartWithBgLocked(bg Color) {
	// Clear lines above
	for row := 0; row < g.CursorRow; row++ {
		g.fillSpan(row, 0, g.Cols, bg)
	}
	// Clear start of current line
	g.fillSpan(g.CursorRow, 0, g.CursorCol+1, bg)
}

// ClearLineWithBg clears the current line with background color (BCE)
func (g *Grid) ClearLineWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ClearLineWithBgLocked(bg)
}

// ClearLineWithBgLocked is ClearLineWithBg without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ClearLineWithBgLocked(bg Color) {
	g.fillSpan(g.CursorRow, 0, g.Cols, bg)
}

// ClearLineToEndWithBg clears from cursor to end of line with background color (BCE)
func (g *Grid) ClearLineToEndWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ClearLineToEndWithBgLocked(bg)
}

// ClearLineToEndWithBgLocked is ClearLineToEndWithBg without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ClearLineToEndWithBgLocked(bg Color) {
	g.fillSpan(g.CursorRow, g.CursorCol, g.Cols, bg)
}

// ClearLineToStartWithBg clears from start of line to cursor with background color (BCE)
func (g *Grid) ClearLineToStartWithBg(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ClearLineToStartWithBgLocked(bg)
}

// ClearLineToStartWithBgLocked is ClearLineToStartWithBg without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ClearLineToStartWithBgLocked(bg Color) {
	g.fillSpan(g.CursorRow, 0, g.CursorCol+1, bg)
}

// DeleteChars deletes n characters at cursor, shifting left
func (g *Grid) DeleteChars(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.DeleteCharsLocked(n)
}

// DeleteCharsLocked is DeleteChars without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) DeleteCharsLocked(n int) {

	// Clamp to the space right of the cursor; an oversized count would index
	// negative columns in the shift/clear loops below.
	if n > g.Cols-g.CursorCol {
		n = g.Cols - g.CursorCol
	}
	if n <= 0 {
		return
	}

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
	g.fillSpan(g.CursorRow, g.Cols-n, g.Cols, g.eraseBg)
}

// InsertChars inserts n blank characters at cursor, shifting right
func (g *Grid) InsertChars(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.InsertCharsLocked(n)
}

// InsertCharsLocked is InsertChars without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) InsertCharsLocked(n int) {

	if n > g.Cols-g.CursorCol {
		n = g.Cols - g.CursorCol
	}
	if n <= 0 {
		return
	}

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
	g.fillSpan(g.CursorRow, g.CursorCol, g.CursorCol+n, g.eraseBg)
}

// DeleteLines deletes n lines at cursor within scroll region, shifting up
func (g *Grid) DeleteLines(n int) {
	g.DeleteLinesWithBg(n, DefaultBg())
}

// DeleteLinesWithBg deletes n lines at cursor with BCE support
func (g *Grid) DeleteLinesWithBg(n int, bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.DeleteLinesWithBgLocked(n, bg)
}

// DeleteLinesWithBgLocked is DeleteLinesWithBg without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) DeleteLinesWithBgLocked(n int, bg Color) {

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
		g.fillSpan(row, 0, g.Cols, bg)
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
	g.InsertLinesWithBgLocked(n, bg)
}

// InsertLinesWithBgLocked is InsertLinesWithBg without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) InsertLinesWithBgLocked(n int, bg Color) {

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
		g.fillSpan(row, 0, g.Cols, bg)
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
	// Reflow rebuilds the history+screen continuum, invalidating absolute
	// selection anchors; clearing on resize is the documented rule.
	g.selectionActive = false

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

	// Clamp/truncate path (alternate screen): copy the overlap region. keepCols/
	// keepRows are computed from the OLD dimensions; new rows are allocated at
	// the NEW width (g.Cols isn't updated until below, so we must pass cols
	// explicitly — otherwise grown rows would be too short and overflow cellAt).
	newRows := make([]*Row, rows)
	keepCols := min(cols, g.Cols)
	keepRows := min(rows, g.Rows)
	for row := range rows {
		nr := g.blankRowN(cols, g.eraseBg)
		if row < keepRows && keepCols > 0 {
			src := g.rows[row]
			// nr is uniformly filled by blankRowN, so the refs on the
			// overwritten prefix drop in one call; the copied-in source
			// styles are retained per run of equal ids.
			g.styles.releaseN(nr.cells[0].Style, keepCols)
			for col := 0; col < keepCols; {
				run := col + 1
				for run < keepCols && src.cells[run].Style == src.cells[col].Style {
					run++
				}
				g.styles.retainN(src.cells[col].Style, run-col)
				copy(nr.cells[col:run], src.cells[col:run])
				col = run
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
	return g.GetCursorLocked()
}

// GetCursorLocked is GetCursor without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) GetCursorLocked() (col, row int) {
	return g.CursorCol, g.CursorRow
}

// AbsoluteCursorRow returns the cursor's row in absolute coordinates (rows ever
// scrolled out + current history + active offset), for anchoring image
// placements so they scroll with content.
func (g *Grid) AbsoluteCursorRow() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.AbsoluteCursorRowLocked()
}

// AbsoluteCursorRowLocked is AbsoluteCursorRow without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) AbsoluteCursorRowLocked() int {
	return g.scrolledOut + len(g.history) + g.CursorRow
}

// LinesScrolledOut returns the number of rows trimmed off the top of history.
func (g *Grid) LinesScrolledOut() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.scrolledOut
}

// EraseChars erases n characters at cursor without moving cursor
func (g *Grid) EraseChars(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.EraseCharsLocked(n)
}

// EraseCharsLocked is EraseChars without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) EraseCharsLocked(n int) {

	startCol := g.CursorCol
	endCol := min(g.CursorCol+n, g.Cols)

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
	g.fillSpan(g.CursorRow, startCol, endCol, g.eraseBg)
}

// RepeatChar repeats the last written character n times. Reuses the normal
// write path so wide chars get their continuation cells and wrap/soft-wrap
// behave exactly as if the app had re-sent the character.
func (g *Grid) RepeatChar(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.RepeatCharLocked(n)
}

// RepeatCharLocked is RepeatChar without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) RepeatCharLocked(n int) {
	sr := g.styles.ref(g.lastStyle)
	for range n {
		g.writeCharLocked(g.lastChar, &sr, g.lastLink)
	}
	g.styles.done(sr)
}

// SetScrollRegion sets the scrolling region (1-based, inclusive)
func (g *Grid) SetScrollRegion(top, bottom int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.SetScrollRegionLocked(top, bottom)
}

// SetScrollRegionLocked is SetScrollRegion without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) SetScrollRegionLocked(top, bottom int) {
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
	g.RestoreScrollRegionLocked(top, bottom)
}

// RestoreScrollRegionLocked is RestoreScrollRegion without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) RestoreScrollRegionLocked(top, bottom int) {
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
	g.ResetWrapPendingLocked()
}

// ResetWrapPendingLocked is ResetWrapPending without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) ResetWrapPendingLocked() {
	g.wrapPending = false
}

// GetScrollRegion returns the current scroll region
func (g *Grid) GetScrollRegion() (top, bottom int) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.GetScrollRegionLocked()
}

// GetScrollRegionLocked is GetScrollRegion without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) GetScrollRegionLocked() (top, bottom int) {
	return g.scrollTop, g.scrollBottom
}

// SetAutoWrap sets the auto-wrap mode (DECAWM ?7)
func (g *Grid) SetAutoWrap(enabled bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.SetAutoWrapLocked(enabled)
}

// SetAutoWrapLocked is SetAutoWrap without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) SetAutoWrapLocked(enabled bool) {
	g.autoWrap = enabled
}

// GetAutoWrap returns the current auto-wrap mode
func (g *Grid) GetAutoWrap() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.GetAutoWrapLocked()
}

// GetAutoWrapLocked is GetAutoWrap without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) GetAutoWrapLocked() bool {
	return g.autoWrap
}

// SetEraseBackground sets the background color for BCE (Background Color Erase)
func (g *Grid) SetEraseBackground(bg Color) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.SetEraseBackgroundLocked(bg)
}

// SetEraseBackgroundLocked is SetEraseBackground without locking; the caller must hold g.mu
// (see LockBatch).
func (g *Grid) SetEraseBackgroundLocked(bg Color) {
	g.eraseBg = bg
}

// GetEraseBackground returns the current BCE background color
func (g *Grid) GetEraseBackground() Color {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.eraseBg
}
