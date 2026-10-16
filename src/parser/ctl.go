package parser

import (
	"fmt"

	"github.com/javanhut/RavenTerminal/src/grid"
)

// Version is reported by XTVERSION; override with
// -ldflags "-X github.com/javanhut/RavenTerminal/src/parser.Version=x.y.z".
var Version = "1.0.0"

const (
	defaultCellW  = 8
	defaultCellH  = 16
	maxTitleStack = 10
)

// savedCursor is the DECSC/DECRC state.
type savedCursor struct {
	valid       bool
	col, row    int
	wrapPending bool
	fg, bg      grid.Color
	flags       grid.CellFlags
	ulStyle     uint8
	ulColor     grid.Color
	link        uint16
	g           [4]Charset
	gl, ss      int
	originMode  bool
	autoWrap    bool
}

// ctlState holds the control-sequence state owned by the CSI/ESC/DCS layer.
type ctlState struct {
	g      [4]Charset // G0..G3 designations
	gl     int        // set locked into GL (0..3)
	ss     int        // pending single shift (2 or 3), 0 if none
	escBuf []byte     // ESC intermediates

	appKeypad       bool
	modifyOtherKeys int
	reverseVideo    bool
	mode1048        bool
	lrmm            bool // DECLRMM ?69
	lrSet           bool
	left, right     int // 0-based inclusive left/right margins (when lrSet)
	mouseExt        int // 0, 1005, 1006, 1015 or 1016
	altScrollOff    bool
	metaNoEsc       bool
	altNoEsc        bool
	graphemeOff     bool
	colorReports    bool // ?2031
	inBandResize    bool // ?2048
	sixelDisplay    bool // ?80 DECSDM
	sixelCurRight   bool // ?8452
	cursorParam     int  // last DECSCUSR Ps (for DECRQSS)
	dark            bool
	cellW, cellH    int
	xtsave          map[int]bool
	titleStack      []string
	tabs            []bool

	dcsHdrDone bool
	dcsSixel   bool

	savedMain, savedAlt savedCursor
}

func (c *ctlState) init(cols int) {
	*c = ctlState{dark: c.dark, cellW: c.cellW, cellH: c.cellH}
	c.resetTabs(cols)
}

// Tab stops cover at least minTabCols columns, like xterm's fixed table, so
// stops (and a TBC 3) survive a shrink and a later grow.
const minTabCols = 1024

func (c *ctlState) resetTabs(cols int) {
	c.tabs = make([]bool, max(cols, minTabCols))
	for i := 8; i < len(c.tabs); i += 8 {
		c.tabs[i] = true
	}
}

// resizeTabs keeps custom stops; columns beyond the table get default stops.
func (c *ctlState) resizeTabs(cols int) {
	for i := len(c.tabs); i < cols; i++ {
		c.tabs = append(c.tabs, i%8 == 0)
	}
}

func (t *Terminal) cellSize() (int, int) {
	w, h := t.ctl.cellW, t.ctl.cellH
	if w <= 0 || h <= 0 {
		return defaultCellW, defaultCellH
	}
	return w, h
}

// SetCellPixelSize records the cell size in pixels (XTWINOPS 14t/16t, sixel
// and kitty sizing, SGR-pixel mouse). With ?2048 it emits a resize report.
func (t *Terminal) SetCellPixelSize(w, h int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if w == t.ctl.cellW && h == t.ctl.cellH {
		return
	}
	t.ctl.cellW, t.ctl.cellH = w, h
	t.reportInBandSize()
}

func (t *Terminal) reportInBandSize() {
	if !t.ctl.inBandResize || t.responseWriter == nil {
		return
	}
	cw, ch := t.cellSize()
	g := t.Grid
	t.responseWriter(fmt.Appendf(nil, "\x1b[48;%d;%d;%d;%dt", g.Rows, g.Cols, g.Rows*ch, g.Cols*cw))
}

// SetDarkMode sets the host colour scheme; with ?2031 a change is reported.
func (t *Terminal) SetDarkMode(dark bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if dark == t.ctl.dark {
		return
	}
	t.ctl.dark = dark
	if t.ctl.colorReports {
		t.reportColorScheme()
	}
}

func (t *Terminal) reportColorScheme() {
	if t.responseWriter == nil {
		return
	}
	v := 2
	if t.ctl.dark {
		v = 1
	}
	t.responseWriter(fmt.Appendf(nil, "\x1b[?997;%dn", v))
}

func (t *Terminal) locked(f func() bool) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return f()
}

// AppKeypad reports application keypad mode (DECKPAM / DECNKM).
func (t *Terminal) AppKeypad() bool { return t.locked(func() bool { return t.ctl.appKeypad }) }

// ModifyOtherKeys returns the xterm modifyOtherKeys level (0, 1 or 2).
func (t *Terminal) ModifyOtherKeys() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ctl.modifyOtherKeys
}

// MetaSendsEscape reports ?1036 (default on).
func (t *Terminal) MetaSendsEscape() bool { return t.locked(func() bool { return !t.ctl.metaNoEsc }) }

// AltSendsEscape reports ?1039 (default on).
func (t *Terminal) AltSendsEscape() bool { return t.locked(func() bool { return !t.ctl.altNoEsc }) }

// AlternateScroll reports ?1007: wheel sends arrows on the alternate screen.
func (t *Terminal) AlternateScroll() bool {
	return t.locked(func() bool { return !t.ctl.altScrollOff })
}

// GraphemeClustering reports ?2027 (default on).
func (t *Terminal) GraphemeClustering() bool {
	return t.locked(func() bool { return !t.ctl.graphemeOff })
}

// ReverseVideo reports DECSCNM (?5).
func (t *Terminal) ReverseVideo() bool { return t.locked(func() bool { return t.ctl.reverseVideo }) }

// IsAlternateScreen reports whether the alternate screen is active.
func (t *Terminal) IsAlternateScreen() bool {
	return t.locked(func() bool { return t.alternateScreen })
}

// MouseEncoding returns the active extended mouse encoding (0, 1005, 1006, 1015, 1016).
func (t *Terminal) MouseEncoding() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ctl.mouseExt
}

func (t *Terminal) mouseSGR() bool { return t.ctl.mouseExt == 1006 || t.ctl.mouseExt == 1016 }

// --- margins and cursor helpers ---

func (t *Terminal) lrActive() bool { return t.ctl.lrmm && t.ctl.lrSet }

// margins returns the 0-based left/right margins.
func (t *Terminal) margins() (int, int) {
	if t.lrActive() {
		return t.ctl.left, t.ctl.right
	}
	return 0, t.Grid.Cols - 1
}

// region returns the 0-based top/bottom margins.
func (t *Terminal) region() (int, int) {
	top, bottom := t.Grid.GetScrollRegionLocked()
	return top - 1, bottom - 1
}

func (t *Terminal) resetLRMargins() {
	t.ctl.lrSet = false
	t.ctl.left, t.ctl.right = 0, 0
}

// setCursorPos implements CUP/HVP (1-based) with origin mode.
func (t *Terminal) setCursorPos(col, row int) {
	col--
	row--
	if t.originMode {
		top, bottom := t.region()
		left, right := t.margins()
		row = min(max(row+top, top), bottom)
		col = min(max(col+left, left), right)
	}
	t.Grid.SetCursorLocked(col, row)
}

// moveCursor moves relative to the cursor; CUU/CUD stop at the scroll margins
// and CUF/CUB at the left/right margins when the cursor is inside them.
func (t *Terminal) moveCursor(dCol, dRow int) {
	col, row := t.Grid.GetCursorLocked()
	if dRow != 0 {
		top, bottom := t.region()
		lo, hi := 0, t.Grid.Rows-1
		if row >= top && row <= bottom {
			lo, hi = top, bottom
		}
		row = min(max(row+dRow, lo), hi)
	}
	if dCol != 0 {
		left, right := t.margins()
		lo, hi := 0, t.Grid.Cols-1
		if col >= left && col <= right {
			lo, hi = left, right
		}
		col = min(max(col+dCol, lo), hi)
	}
	t.Grid.SetCursorLocked(col, row)
}

func (t *Terminal) setCol(col int) {
	_, row := t.Grid.GetCursorLocked()
	if t.originMode {
		left, right := t.margins()
		col = min(max(col+left, left), right)
	}
	t.Grid.SetCursorLocked(col, row)
}

func (t *Terminal) setRow(row int) {
	col, _ := t.Grid.GetCursorLocked()
	if t.originMode {
		top, bottom := t.region()
		row = min(max(row+top, top), bottom)
	}
	t.Grid.SetCursorLocked(col, row)
}

func (t *Terminal) home() { t.setCursorPos(1, 1) }

func (t *Terminal) carriageReturn() {
	if t.lrActive() {
		col, row := t.Grid.GetCursorLocked()
		if col >= t.ctl.left {
			t.Grid.SetCursorLocked(t.ctl.left, row)
			return
		}
	}
	t.Grid.CarriageReturnLocked()
}

func (t *Terminal) backspace() {
	col, row := t.Grid.GetCursorLocked()
	left, _ := t.margins()
	if col == left || col == 0 {
		t.Grid.SetCursorLocked(col, row)
		return
	}
	t.Grid.SetCursorLocked(col-1, row)
}

// index is LF/IND: down one row, scrolling the margin area at the bottom.
func (t *Terminal) index() {
	if !t.lrActive() {
		t.Grid.NewlineLocked()
		return
	}
	col, row := t.Grid.GetCursorLocked()
	top, bottom := t.region()
	if row == bottom {
		if col >= t.ctl.left && col <= t.ctl.right {
			t.Grid.ScrollRectUpLocked(top, bottom, t.ctl.left, t.ctl.right, 1, t.currentBg)
		}
		t.Grid.SetCursorLocked(col, row)
		return
	}
	t.Grid.SetCursorLocked(col, row+1)
}

// reverseIndex is RI: up one row, scrolling down at the top margin.
func (t *Terminal) reverseIndex() {
	col, row := t.Grid.GetCursorLocked()
	top, bottom := t.region()
	if row != top {
		t.Grid.SetCursorLocked(col, row-1)
		return
	}
	if t.lrActive() {
		if col >= t.ctl.left && col <= t.ctl.right {
			t.Grid.ScrollRectDownLocked(top, bottom, t.ctl.left, t.ctl.right, 1, t.currentBg)
		}
		return
	}
	t.Grid.ScrollDownWithBgLocked(1, t.currentBg)
}

func (t *Terminal) tab(n int) {
	col, row := t.Grid.GetCursorLocked()
	_, right := t.margins()
	limit := t.Grid.Cols - 1
	if col <= right {
		limit = right
	}
	for ; n > 0 && col < limit; n-- {
		col++
		for col < limit && (col >= len(t.ctl.tabs) || !t.ctl.tabs[col]) {
			col++
		}
	}
	t.Grid.SetCursorLocked(col, row)
}

func (t *Terminal) backTab(n int) {
	col, row := t.Grid.GetCursorLocked()
	left, _ := t.margins()
	limit := 0
	if col >= left {
		limit = left
	}
	for ; n > 0 && col > limit; n-- {
		col--
		for col > limit && (col >= len(t.ctl.tabs) || !t.ctl.tabs[col]) {
			col--
		}
	}
	t.Grid.SetCursorLocked(col, row)
}

func (t *Terminal) setTabStop() {
	if col, _ := t.Grid.GetCursorLocked(); col < len(t.ctl.tabs) {
		t.ctl.tabs[col] = true
	}
}

func (t *Terminal) clearTabStop(mode int) {
	switch mode {
	case 0:
		if col, _ := t.Grid.GetCursorLocked(); col < len(t.ctl.tabs) {
			t.ctl.tabs[col] = false
		}
	case 3:
		clear(t.ctl.tabs)
	}
}

// printInMargins writes r honouring DECSLRM: text wraps at the right margin
// back to the left margin, scrolling only the margin rectangle.
func (t *Terminal) printInMargins(r rune) {
	w := grid.RuneWidth(r)
	if w == 0 {
		t.Grid.WriteCharLocked(r, t.currentFg, t.currentBg, t.currentFlags, t.currentLinkID, t.currentUnderlineStyle, t.currentUnderlineColor)
		return
	}
	left, right := t.ctl.left, t.ctl.right
	autoWrap := t.Grid.GetAutoWrapLocked()
	col, row := t.Grid.GetCursorLocked()
	if col < left || col > right {
		t.writeRune(r)
		return
	}
	if t.Grid.WrapPendingLocked() || (w == 2 && col == right && right > left) {
		if autoWrap {
			t.Grid.SetCursorLocked(left, row)
			t.index()
		} else {
			t.Grid.SetCursorLocked(col, row)
		}
		col, row = t.Grid.GetCursorLocked()
	}
	if t.insertMode {
		t.Grid.ScrollRectRightLocked(row, row, col, right, w, t.Grid.EraseBgLocked())
	}
	t.Grid.WriteCharLocked(r, t.currentFg, t.currentBg, t.currentFlags, t.currentLinkID, t.currentUnderlineStyle, t.currentUnderlineColor)
	if ncol, _ := t.Grid.GetCursorLocked(); ncol > right || t.Grid.WrapPendingLocked() {
		t.Grid.SetCursorLocked(right, row)
		t.Grid.SetWrapPendingLocked(autoWrap)
	}
}

func (t *Terminal) writeRune(r rune) {
	t.Grid.WriteCharLocked(r, t.currentFg, t.currentBg, t.currentFlags, t.currentLinkID, t.currentUnderlineStyle, t.currentUnderlineColor)
}

// --- DECSC / DECRC ---

func (t *Terminal) saveCursor() {
	col, row := t.Grid.GetCursorLocked()
	s := savedCursor{
		valid: true, col: col, row: row, wrapPending: t.Grid.WrapPendingLocked(),
		fg: t.currentFg, bg: t.currentBg, flags: t.currentFlags,
		ulStyle: t.currentUnderlineStyle, ulColor: t.currentUnderlineColor, link: t.currentLinkID,
		g: t.ctl.g, gl: t.ctl.gl, ss: t.ctl.ss,
		originMode: t.originMode, autoWrap: t.Grid.GetAutoWrapLocked(),
	}
	if t.alternateScreen {
		t.ctl.savedAlt = s
	} else {
		t.ctl.savedMain = s
	}
}

func (t *Terminal) restoreCursor() {
	s := t.ctl.savedMain
	if t.alternateScreen {
		s = t.ctl.savedAlt
	}
	if !s.valid {
		s = savedCursor{fg: grid.DefaultFg(), bg: grid.DefaultBg(), autoWrap: true}
	}
	t.Grid.SetCursorLocked(s.col, s.row)
	if s.wrapPending && s.col == t.Grid.Cols-1 {
		t.Grid.SetWrapPendingLocked(true)
	}
	prevBg := t.currentBg
	t.currentFg, t.currentBg, t.currentFlags = s.fg, s.bg, s.flags
	t.currentUnderlineStyle, t.currentUnderlineColor, t.currentLinkID = s.ulStyle, s.ulColor, s.link
	if t.currentBg != prevBg {
		t.Grid.SetEraseBackgroundLocked(t.currentBg)
	}
	t.ctl.g, t.ctl.gl, t.ctl.ss = s.g, s.gl, s.ss
	t.originMode = s.originMode
	t.Grid.SetAutoWrapLocked(s.autoWrap)
}

// --- resets ---

func (t *Terminal) resetAttrs() {
	t.currentFg = grid.DefaultFg()
	t.currentBg = grid.DefaultBg()
	t.currentFlags = 0
	t.currentUnderlineStyle = 0
	t.currentUnderlineColor = grid.Color{}
	t.currentLinkID = 0
	t.Grid.SetEraseBackgroundLocked(grid.DefaultBg())
}

// softReset implements DECSTR (CSI ! p): reset modes and attributes without
// clearing the screen. TUI apps (vim among them) issue this on startup/exit.
func (t *Terminal) softReset() {
	t.resetAttrs()
	t.originMode = false
	t.insertMode = false
	t.appCursorKeys = false
	t.cursorVisible = true
	t.cursorStyle = t.defaultCursorStyle
	t.cursorBlinking = true
	t.ctl.cursorParam = 0
	t.ctl.appKeypad = false
	t.Grid.SetAutoWrapLocked(true)
	// Margins reset to the full screen; DECSTR does not move the cursor.
	t.Grid.RestoreScrollRegionLocked(1, t.Grid.Rows)
	t.resetLRMargins()
	t.ctl.g = [4]Charset{}
	t.ctl.gl, t.ctl.ss = 0, 0
	t.ctl.savedMain, t.ctl.savedAlt = savedCursor{}, savedCursor{}
	t.ctl.resetTabs(t.Grid.Cols)
}

// reset implements RIS: leave the alternate screen first, then reset every
// mode, margin, charset, colour, title and image to its power-on state.
func (t *Terminal) reset() {
	t.exitAlternateScreen()
	g := t.Grid
	t.resetAttrs()
	g.ClearAllLocked()
	g.RestoreScrollRegionLocked(1, g.Rows)
	g.SetAutoWrapLocked(true)
	g.SetCursorLocked(0, 0)
	t.ctl.init(g.Cols)
	t.syncGraphemeMode()
	t.appCursorKeys = false
	t.cursorVisible = true
	t.originMode = false
	t.insertMode = false
	t.lnmMode = false
	t.kittyStack = nil
	t.altKittyStack = nil
	t.cursorStyle = t.defaultCursorStyle
	t.cursorBlinking = true
	t.focusReporting = false
	t.syncActive = false
	t.bracketedPaste = false
	t.mouseMode = 0
	t.savedMainAppCursorKeys, t.savedMainBracketedPaste = false, false
	t.savedMainMouseMode, t.savedMainMouseExt = 0, 0
	t.fgColor, t.bgColor, t.cursorColor = grid.Color{}, grid.Color{}, grid.Color{}
	t.paletteOverride = nil
	t.iconName = ""
	t.setTitle("")
	t.resetOSC()
	t.images.DeleteAll()
	t.altImages.DeleteAll()
	t.pendingKitty = nil
	t.resetOSC()
}

// Resize resizes the terminal
func (t *Terminal) Resize(cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.savedMainGrid != nil {
		// Resizing while on the alternate screen (e.g. inside vim): keep the
		// saved main-screen scroll region in sync with the new height, otherwise
		// exiting the alternate screen restores a stale region sized for the old
		// grid and the shell scrolls inside a partial window.
		oldRows := t.savedMainGrid.Rows
		wasFull := t.savedMainScrollTop == 1 && t.savedMainScrollBottom == oldRows
		t.resizeMainGrid(t.savedMainGrid, cols, rows)
		if wasFull {
			t.savedMainScrollTop, t.savedMainScrollBottom = 1, rows
		} else {
			if t.savedMainScrollBottom > rows {
				t.savedMainScrollBottom = rows
			}
			if t.savedMainScrollTop >= t.savedMainScrollBottom {
				t.savedMainScrollTop, t.savedMainScrollBottom = 1, rows
			}
		}
	}
	changed := cols != t.Grid.Cols || rows != t.Grid.Rows
	if t.alternateScreen {
		t.Grid.Resize(cols, rows)
	} else {
		t.resizeMainGrid(t.Grid, cols, rows)
	}
	t.ctl.resizeTabs(cols)
	if t.ctl.lrSet && t.ctl.right >= cols {
		t.resetLRMargins()
	}
	if changed {
		t.reportInBandSize()
	}
}
