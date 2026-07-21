package parser

import (
	"encoding/base64"
	"fmt"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/images"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ParserState represents the current state of the ANSI parser
type ParserState int

const (
	StateGround ParserState = iota
	StateEscape
	StateCSI
	StateOSC
	StateOSCEscape // for handling ESC within OSC
	StateDCS       // Device Control String
	StateDCSEscape // ESC within DCS
	StateCharset
	StateHash
	StateAPC       // Application Program Command (ESC _ ... ST) — Kitty graphics
	StateAPCEscape // ESC within APC
)

// Charset represents a character set designation (G0/G1).
type Charset int

const (
	charsetASCII Charset = iota
	charsetLineDrawing
)

type charsetTarget int

const (
	charsetTargetNone charsetTarget = iota
	charsetTargetG0
	charsetTargetG1
)

// CursorStyle represents the rendered cursor style.
type CursorStyle int

const (
	CursorStyleBlock CursorStyle = iota
	CursorStyleUnderline
	CursorStyleBar
)

// DEC Special Graphics (line drawing) character mapping.
// Used when G0/G1 is designated via ESC ( 0 / ESC ) 0 and selected via SI/SO.
var decLineDrawing = map[rune]rune{
	'`': '◆', // U+25C6 Black Diamond
	'a': '▒', // U+2592 Medium Shade
	'f': '°', // U+00B0 Degree Sign
	'g': '±', // U+00B1 Plus-Minus Sign
	'j': '┘', // U+2518 Box Drawings Light Up And Left
	'k': '┐', // U+2510 Box Drawings Light Down And Left
	'l': '┌', // U+250C Box Drawings Light Down And Right
	'm': '└', // U+2514 Box Drawings Light Up And Right
	'n': '┼', // U+253C Box Drawings Light Vertical And Horizontal
	'o': '⎺', // U+23BA Horizontal Scan Line-1
	'p': '⎻', // U+23BB Horizontal Scan Line-3
	'q': '─', // U+2500 Box Drawings Light Horizontal
	'r': '⎼', // U+23BC Horizontal Scan Line-7
	's': '⎽', // U+23BD Horizontal Scan Line-9
	't': '├', // U+251C Box Drawings Light Vertical And Right
	'u': '┤', // U+2524 Box Drawings Light Vertical And Left
	'v': '┴', // U+2534 Box Drawings Light Up And Horizontal
	'w': '┬', // U+252C Box Drawings Light Down And Horizontal
	'x': '│', // U+2502 Box Drawings Light Vertical
	'y': '≤', // U+2264 Less-Than Or Equal To
	'z': '≥', // U+2265 Greater-Than Or Equal To
	'{': 'π', // U+03C0 Greek Small Letter Pi
	'|': '≠', // U+2260 Not Equal To
	'}': '£', // U+00A3 Pound Sign
	'~': '·', // U+00B7 Middle Dot
}

// CursorState holds complete cursor state for save/restore
type CursorState struct {
	col   int
	row   int
	fg    grid.Color
	bg    grid.Color
	flags grid.CellFlags
}

// Accumulation caps for escape-sequence payloads. An unterminated sequence in
// binary/hostile output must not buffer the rest of the stream forever (xterm
// caps these too). Oversized sequences are discarded, not executed truncated.
// OSC/APC are generous to fit OSC 52 clipboard and Kitty graphics payloads.
const (
	maxCSILen = 1024
	maxOSCLen = 8 << 20
	maxDCSLen = 64 << 10
	maxAPCLen = 8 << 20
)

// Terminal handles ANSI escape sequence parsing and state
type Terminal struct {
	Grid            *grid.Grid
	state           ParserState
	csiBuf          []byte
	paramScratch    []int
	subScratch      []int
	oscParams       []byte
	dcsParams       []byte
	currentFg       grid.Color
	currentBg       grid.Color
	currentFlags    grid.CellFlags
	appCursorKeys   bool
	cursorVisible   bool
	alternateScreen bool
	savedMainGrid   *grid.Grid
	lastWorkingDir  string
	responseWriter  func([]byte)
	mu              sync.Mutex
	// UTF-8 decoding state
	utf8Buf       [4]byte
	utf8Len       int
	utf8Remaining int
	// Per-screen cursor state (fixes shared cursor bug)
	savedMainCursor      CursorState
	savedAlternateCursor CursorState
	// Per-screen scroll region state
	savedMainScrollTop    int
	savedMainScrollBottom int
	// Character set handling (DEC line drawing)
	charsetG0      Charset
	charsetG1      Charset
	activeCharset  int // 0=G0, 1=G1
	charsetPending charsetTarget
	// Origin mode (DECOM ?6)
	originMode bool
	// Insert/replace mode (IRM, mode 4) and line-feed/new-line mode (LNM, mode 20)
	insertMode bool
	lnmMode    bool
	// Kitty keyboard protocol: per-screen stack of enhancement flag sets.
	kittyStack    []uint8
	altKittyStack []uint8
	// Kitty graphics: per-screen image stores, APC accumulation, and chunked-
	// transmission assembly state.
	images       *images.Store
	altImages    *images.Store
	apcBuf       []byte
	pendingKitty *kittyTransmit
	// Dynamic colors: OSC 10/11/12 foreground/background/cursor and OSC 4 palette
	// overrides. Zero value (ColorDefault) means "not overridden".
	fgColor         grid.Color
	bgColor         grid.Color
	cursorColor     grid.Color
	paletteOverride map[int]grid.Color
	// Cursor style (DECSCUSR) and whether it blinks
	cursorStyle    CursorStyle
	cursorBlinking bool
	// Bracketed paste mode (?2004)
	bracketedPaste bool
	// Window title (OSC 0/2) and icon name (OSC 0/1)
	windowTitle string
	iconName    string
	// Mouse tracking modes
	mouseMode    int  // 0=off, 1000=normal, 1002=button, 1003=any
	mouseSGRMode bool // ?1006 - SGR extended coordinates
	// Saved terminal modes for alternate screen restore
	savedMainAppCursorKeys  bool
	savedMainBracketedPaste bool
	savedMainMouseMode      int
	savedMainMouseSGRMode   bool
	// OSC 8 hyperlink + extended underline (SGR 4:n / 58) pending attributes
	currentLinkID         uint16
	currentUnderlineStyle uint8
	currentUnderlineColor grid.Color
	// Focus reporting (?1004)
	focusReporting bool
	// Synchronized output (?2026)
	syncActive   bool
	syncDeadline time.Time
	// OSC 52 clipboard access (nil-guarded, wired by the host)
	clipboardWriter func(string)
	clipboardReader func() string
	// Reusable render snapshot buffer (double-buffered across frames).
	snapPrev *grid.Snapshot
	// batchGrid is the grid whose Grid.mu Process currently holds (via
	// LockBatch) for the duration of a chunk. It tracks the lock across
	// alternate-screen swaps; nil outside Process. Guarded by mu.
	batchGrid *grid.Grid
}

// NewTerminal creates a new terminal parser
func NewTerminal(cols, rows int) *Terminal {
	return &Terminal{
		Grid:                  grid.NewGrid(cols, rows),
		state:                 StateGround,
		currentFg:             grid.DefaultFg(),
		currentBg:             grid.DefaultBg(),
		currentFlags:          0,
		cursorVisible:         true,
		savedMainScrollTop:    1,
		savedMainScrollBottom: rows,
		charsetG0:             charsetASCII,
		charsetG1:             charsetASCII,
		activeCharset:         0,
		charsetPending:        charsetTargetNone,
		cursorStyle:           CursorStyleBlock,
		cursorBlinking:        true, // default cursor blinks (DECSCUSR 0)
		images:                images.NewStore(256 << 20),
		altImages:             images.NewStore(256 << 20),
	}
}

// Process processes incoming bytes from the PTY.
//
// LOCK ORDER: t.mu is held for the whole chunk, and beneath it Grid.mu is held
// for the whole chunk too (via Grid.LockBatch; every grid call below uses an
// exported XxxLocked variant — a locking public method would self-deadlock).
// Terminal.mu is always acquired before Grid.mu (see Snapshot); never the
// reverse. Grid.mu is a leaf lock: grid code never calls out of its package.
// The batch lock follows the grid across alternate-screen swaps (batchGrid).
// Worst-case hold time is bounded by one chunk (the PTY read buffer), so
// Grid.mu-only callers (selection, scrollbar) see at most one extra chunk of
// wait, while the renderer already waits on t.mu for the same span.
func (t *Terminal) Process(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.batchGrid = t.Grid
	t.batchGrid.LockBatch()
	defer func() {
		t.batchGrid.UnlockBatch()
		t.batchGrid = nil
	}()

	for i := 0; i < len(data); {
		b := data[i]
		// Fast path: a run of printable ASCII in ground state with plain
		// charset and no insert mode is written through the grid's batch path,
		// byte-direct (no []rune conversion). Everything else falls back to
		// the per-byte state machine.
		if b >= 0x20 && b < 0x7f && t.state == StateGround && t.utf8Remaining == 0 &&
			!t.insertMode && !t.activeCharsetMapped() {
			j := i + 1
			for j < len(data) && data[j] >= 0x20 && data[j] < 0x7f {
				j++
			}
			t.Grid.WriteBytesLocked(data[i:j], t.currentFg, t.currentBg, t.currentFlags,
				t.currentLinkID, t.currentUnderlineStyle, t.currentUnderlineColor)
			i = j
			continue
		}
		t.processByte(b)
		i++
	}
}

// activeCharsetMapped reports whether the active charset remaps printable
// ASCII (DEC line drawing); when it does, the batch fast path is skipped.
func (t *Terminal) activeCharsetMapped() bool {
	if t.activeCharset == 1 {
		return t.charsetG1 == charsetLineDrawing
	}
	return t.charsetG0 == charsetLineDrawing
}

// processByte processes a single byte
func (t *Terminal) processByte(b byte) {
	switch t.state {
	case StateGround:
		t.processGround(b)
	case StateEscape:
		t.processEscape(b)
	case StateCSI:
		t.processCSI(b)
	case StateOSC:
		t.processOSC(b)
	case StateOSCEscape:
		t.processOSCEscape(b)
	case StateDCS:
		t.processDCS(b)
	case StateDCSEscape:
		t.processDCSEscape(b)
	case StateAPC:
		t.processAPC(b)
	case StateAPCEscape:
		t.processAPCEscape(b)
	case StateCharset:
		// Character set designation - consume the designator byte
		t.setCharset(b)
		t.state = StateGround
	case StateHash:
		// DEC special sequences. ESC # 8 is DECALN (fill screen with 'E').
		if b == '8' {
			t.Grid.FillScreenLocked('E')
		}
		t.state = StateGround
	}
}

// processGround handles bytes in ground state
func (t *Terminal) processGround(b byte) {
	// If we're in the middle of a UTF-8 sequence, continue it
	if t.utf8Remaining > 0 {
		if b&0xC0 == 0x80 { // Valid continuation byte
			t.utf8Buf[t.utf8Len] = b
			t.utf8Len++
			t.utf8Remaining--
			if t.utf8Remaining == 0 {
				// Complete UTF-8 sequence - decode and write
				r := t.mapCharsetRune(decodeUTF8(t.utf8Buf[:t.utf8Len]))
				t.printRune(r)
				t.utf8Len = 0
			}
		} else {
			// Invalid continuation - discard and process this byte normally
			t.utf8Len = 0
			t.utf8Remaining = 0
			t.processGround(b)
		}
		return
	}

	switch b {
	case 0x1b: // ESC
		t.state = StateEscape
	// NOTE: In a UTF-8 terminal, bytes 0x80-0x9F are UTF-8 continuation bytes,
	// NOT 8-bit C1 controls. We therefore do NOT treat 0x9b/0x9d/0x90/0x9c as
	// CSI/OSC/DCS/ST here; C1 controls only arrive via their ESC-prefixed forms.
	case 0x07: // BEL
		// Bell - ignore
	case 0x08: // BS
		t.Grid.BackspaceLocked()
	case 0x09: // HT (Tab)
		t.Grid.TabLocked()
	case 0x0e: // SO (Shift Out) - select G1
		t.activeCharset = 1
	case 0x0f: // SI (Shift In) - select G0
		t.activeCharset = 0
	case 0x0a, 0x0b, 0x0c: // LF, VT, FF
		if t.lnmMode { // LNM: LF also performs CR
			t.Grid.CarriageReturnLocked()
		}
		t.Grid.NewlineLocked()
		// Scroll position preserved - reset happens on user input instead
	case 0x0d: // CR
		t.Grid.CarriageReturnLocked()
	default:
		if b >= 0x20 && b < 0x7f {
			// ASCII printable character
			r := t.mapCharsetRune(rune(b))
			t.printRune(r)
		} else if b >= 0xC0 && b < 0xE0 {
			// Start of 2-byte UTF-8 sequence
			t.utf8Buf[0] = b
			t.utf8Len = 1
			t.utf8Remaining = 1
		} else if b >= 0xE0 && b < 0xF0 {
			// Start of 3-byte UTF-8 sequence
			t.utf8Buf[0] = b
			t.utf8Len = 1
			t.utf8Remaining = 2
		} else if b >= 0xF0 && b < 0xF8 {
			// Start of 4-byte UTF-8 sequence
			t.utf8Buf[0] = b
			t.utf8Len = 1
			t.utf8Remaining = 3
		}
		// Ignore other bytes (control characters, invalid UTF-8 start bytes)
	}
}

// printRune writes a printable rune at the cursor, honoring insert mode (IRM):
// when insert mode is active, existing cells shift right to make room.
func (t *Terminal) printRune(r rune) {
	if t.insertMode {
		if w := grid.RuneWidth(r); w > 0 {
			t.Grid.InsertCharsLocked(w)
		}
	}
	t.Grid.WriteCharLocked(r, t.currentFg, t.currentBg, t.currentFlags, t.currentLinkID, t.currentUnderlineStyle, t.currentUnderlineColor)
}

// decodeUTF8 decodes a UTF-8 byte sequence to a rune
func decodeUTF8(buf []byte) rune {
	if len(buf) == 0 {
		return 0xFFFD // Replacement character
	}

	var r rune
	switch len(buf) {
	case 1:
		return rune(buf[0])
	case 2:
		if buf[0]&0xE0 != 0xC0 {
			return 0xFFFD
		}
		r = rune(buf[0]&0x1F)<<6 | rune(buf[1]&0x3F)
		if r < 0x80 { // overlong
			return 0xFFFD
		}
	case 3:
		if buf[0]&0xF0 != 0xE0 {
			return 0xFFFD
		}
		r = rune(buf[0]&0x0F)<<12 | rune(buf[1]&0x3F)<<6 | rune(buf[2]&0x3F)
		if r < 0x800 { // overlong
			return 0xFFFD
		}
	case 4:
		if buf[0]&0xF8 != 0xF0 {
			return 0xFFFD
		}
		r = rune(buf[0]&0x07)<<18 | rune(buf[1]&0x3F)<<12 | rune(buf[2]&0x3F)<<6 | rune(buf[3]&0x3F)
		if r < 0x10000 { // overlong
			return 0xFFFD
		}
	default:
		return 0xFFFD
	}
	// Reject surrogate halves and out-of-range code points.
	if r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF) {
		return 0xFFFD
	}
	return r
}

// setCursorPos applies origin mode if enabled, then clamps to bounds.
func (t *Terminal) setCursorPos(col, row int) {
	if t.originMode {
		top, bottom := t.Grid.GetScrollRegionLocked()
		row = top + row - 1
		if row < top {
			row = top
		} else if row > bottom {
			row = bottom
		}
	}
	t.Grid.SetCursorPosLocked(col, row)
}

// moveCursor moves the cursor and clamps to the scroll region if origin mode is enabled.
func (t *Terminal) moveCursor(dCol, dRow int) {
	if !t.originMode {
		t.Grid.MoveCursorLocked(dCol, dRow)
		return
	}
	col, row := t.Grid.GetCursorLocked()
	col += dCol
	row += dRow
	if col < 0 {
		col = 0
	} else if col >= t.Grid.Cols {
		col = t.Grid.Cols - 1
	}
	top, bottom := t.Grid.GetScrollRegionLocked()
	top--
	bottom--
	if row < top {
		row = top
	} else if row > bottom {
		row = bottom
	}
	t.Grid.SetCursorPosLocked(col+1, row+1)
}

// mapCharsetRune applies DEC line drawing mapping if a graphics charset is active.
func (t *Terminal) mapCharsetRune(r rune) rune {
	var cs Charset
	if t.activeCharset == 1 {
		cs = t.charsetG1
	} else {
		cs = t.charsetG0
	}
	if cs == charsetLineDrawing {
		if mapped, ok := decLineDrawing[r]; ok {
			return mapped
		}
	}
	return r
}

// setCharset applies a charset designation byte to the pending target.
func (t *Terminal) setCharset(designator byte) {
	if t.charsetPending == charsetTargetNone {
		return
	}

	cs := charsetASCII
	switch designator {
	case '0':
		cs = charsetLineDrawing
	case 'B':
		cs = charsetASCII
	}

	switch t.charsetPending {
	case charsetTargetG0:
		t.charsetG0 = cs
	case charsetTargetG1:
		t.charsetG1 = cs
	}
	t.charsetPending = charsetTargetNone
}

func (t *Terminal) setCursorStyle(params []int) {
	p := 0
	if len(params) > 0 {
		p = params[0]
	}
	// Odd/default values blink; even values are steady (DECSCUSR).
	t.cursorBlinking = (p == 0 || p == 1 || p == 3 || p == 5)
	switch p {
	case 0, 1, 2: // Default/blink/steady block
		t.cursorStyle = CursorStyleBlock
	case 3, 4: // Blink/steady underline
		t.cursorStyle = CursorStyleUnderline
	case 5, 6: // Blink/steady bar
		t.cursorStyle = CursorStyleBar
	}
}

// processEscape handles bytes in escape state
func (t *Terminal) processEscape(b byte) {
	switch b {
	case '[': // CSI
		t.state = StateCSI
		t.csiBuf = t.csiBuf[:0]
	case ']': // OSC
		t.state = StateOSC
		t.oscParams = t.oscParams[:0]
	case 'P': // DCS - Device Control String
		t.state = StateDCS
		t.dcsParams = t.dcsParams[:0]
	case '7': // DECSC - Save cursor
		t.saveCursor()
		t.state = StateGround
	case '8': // DECRC - Restore cursor
		t.restoreCursor()
		t.state = StateGround
	case 'c': // RIS - Reset
		t.reset()
		t.state = StateGround
	case 'D': // IND - Index (down, respects scroll region, with BCE)
		_, row := t.Grid.GetCursorLocked()
		_, bottom := t.Grid.GetScrollRegionLocked()
		if row == bottom-1 { // At bottom of scroll region (0-based vs 1-based)
			t.Grid.ScrollUpWithBgLocked(1, t.currentBg)
		} else {
			t.Grid.MoveCursorLocked(0, 1)
		}
		t.state = StateGround
	case 'M': // RI - Reverse index (up, respects scroll region, with BCE)
		_, row := t.Grid.GetCursorLocked()
		top, _ := t.Grid.GetScrollRegionLocked()
		if row == top-1 { // At top of scroll region (0-based vs 1-based)
			t.Grid.ScrollDownWithBgLocked(1, t.currentBg)
		} else if row > 0 {
			t.Grid.MoveCursorLocked(0, -1)
		}
		t.state = StateGround
	case 'E': // NEL - Next line
		t.Grid.CarriageReturnLocked()
		t.Grid.NewlineLocked()
		t.state = StateGround
	case '(', ')', '*', '+': // Character set designation - need to consume next byte
		switch b {
		case '(':
			t.charsetPending = charsetTargetG0
		case ')':
			t.charsetPending = charsetTargetG1
		default:
			t.charsetPending = charsetTargetNone
		}
		t.state = StateCharset
	case '=': // DECKPAM - Application keypad mode
		t.state = StateGround
	case '>': // DECKPNM - Normal keypad mode
		t.state = StateGround
	case '#': // DEC line drawing - need to consume next byte
		t.state = StateHash
	case 'H': // HTS - set tab stop at cursor
		t.Grid.SetTabStopLocked()
		t.state = StateGround
	case '_': // APC - Application Program Command (Kitty graphics)
		t.state = StateAPC
		t.apcBuf = t.apcBuf[:0]
	default:
		t.state = StateGround
	}
}

// processCSI handles bytes in CSI state
func (t *Terminal) processCSI(b byte) {
	switch {
	case b >= 0x30 && b <= 0x3f:
		// Parameter byte
		if len(t.csiBuf) < maxCSILen {
			t.csiBuf = append(t.csiBuf, b)
		}
	case b >= 0x20 && b <= 0x2f:
		// Intermediate byte
		if len(t.csiBuf) < maxCSILen {
			t.csiBuf = append(t.csiBuf, b)
		}
	case b >= 0x40 && b <= 0x7e:
		// Final byte (skip execution if the params overflowed the cap)
		if len(t.csiBuf) < maxCSILen {
			t.executeCSI(b)
		}
		t.csiBuf = t.csiBuf[:0] // Clear params after execution
		t.state = StateGround
	case b == 0x1b: // ESC aborts the sequence and starts a new one
		t.csiBuf = t.csiBuf[:0]
		t.state = StateEscape
	case b == 0x18 || b == 0x1a: // CAN/SUB abort
		t.csiBuf = t.csiBuf[:0]
		t.state = StateGround
	case b < 0x20:
		// C0 controls embedded in a CSI sequence execute immediately without
		// aborting it (VT behavior; curses output relies on this).
		t.processGround(b)
	default:
		t.csiBuf = t.csiBuf[:0] // Clear params on abort
		t.state = StateGround
	}
}

// csiHasPrefix reports whether the accumulated CSI bytes start with c.
func (t *Terminal) csiHasPrefix(c byte) bool {
	return len(t.csiBuf) > 0 && t.csiBuf[0] == c
}

// csiHasSuffix reports whether the accumulated CSI bytes end with c.
func (t *Terminal) csiHasSuffix(c byte) bool {
	return len(t.csiBuf) > 0 && t.csiBuf[len(t.csiBuf)-1] == c
}

// executeCSI executes a CSI sequence
func (t *Terminal) executeCSI(final byte) {
	if final == 'm' { // SGR - Select graphic rendition
		// Parsed in a single pass here; the generic parseParams is skipped so
		// SGR sequences are not parsed twice.
		t.executeSGR(t.parseSGRParams())
		return
	}

	params := t.parseParams()

	switch final {
	case 'A': // CUU - Cursor up
		n := t.getParam(params, 0, 1)
		t.moveCursor(0, -n)
	case 'B': // CUD - Cursor down
		n := t.getParam(params, 0, 1)
		t.moveCursor(0, n)
	case 'C': // CUF - Cursor forward
		n := t.getParam(params, 0, 1)
		t.moveCursor(n, 0)
	case 'D': // CUB - Cursor back
		n := t.getParam(params, 0, 1)
		t.moveCursor(-n, 0)
	case 'E': // CNL - Cursor next line
		n := t.getParam(params, 0, 1)
		t.Grid.CarriageReturnLocked()
		t.moveCursor(0, n)
	case 'F': // CPL - Cursor previous line
		n := t.getParam(params, 0, 1)
		t.Grid.CarriageReturnLocked()
		t.moveCursor(0, -n)
	case 'G': // CHA - Cursor horizontal absolute
		n := t.getParam(params, 0, 1)
		_, row := t.Grid.GetCursorLocked()
		t.Grid.SetCursorPosLocked(n, row+1)
	case 'H', 'f': // CUP - Cursor position
		row := t.getParam(params, 0, 1)
		col := t.getParam(params, 1, 1)
		t.setCursorPos(col, row)
	case 'J': // ED - Erase in display (with BCE support)
		n := t.getParam(params, 0, 0)
		switch n {
		case 0:
			t.Grid.ClearToEndWithBgLocked(t.currentBg)
		case 1:
			t.Grid.ClearToStartWithBgLocked(t.currentBg)
		case 2:
			t.Grid.ClearAllWithBgLocked(t.currentBg)
		case 3: // ED 3 erases saved lines (scrollback) only, not the screen
			t.Grid.ClearScrollbackLocked()
		}
	case 'K': // EL - Erase in line (with BCE support)
		n := t.getParam(params, 0, 0)
		switch n {
		case 0:
			t.Grid.ClearLineToEndWithBgLocked(t.currentBg)
		case 1:
			t.Grid.ClearLineToStartWithBgLocked(t.currentBg)
		case 2:
			t.Grid.ClearLineWithBgLocked(t.currentBg)
		}
	case 'L': // IL - Insert lines (with BCE support)
		n := t.getParam(params, 0, 1)
		t.Grid.InsertLinesWithBgLocked(n, t.currentBg)
	case 'M': // DL - Delete lines (with BCE support)
		n := t.getParam(params, 0, 1)
		t.Grid.DeleteLinesWithBgLocked(n, t.currentBg)
	case 'P': // DCH - Delete characters
		n := t.getParam(params, 0, 1)
		t.Grid.DeleteCharsLocked(n)
	case '@': // ICH - Insert characters
		n := t.getParam(params, 0, 1)
		t.Grid.InsertCharsLocked(n)
	case 'S': // SU - Scroll up (with BCE support)
		n := t.getParam(params, 0, 1)
		t.Grid.ScrollUpWithBgLocked(n, t.currentBg)
	case 'T': // SD - Scroll down (with BCE support)
		n := t.getParam(params, 0, 1)
		t.Grid.ScrollDownWithBgLocked(n, t.currentBg)
	case 'X': // ECH - Erase character (erase n chars at cursor without moving)
		n := t.getParam(params, 0, 1)
		t.Grid.EraseCharsLocked(n)
	case 'd': // VPA - Vertical position absolute
		n := t.getParam(params, 0, 1)
		col, _ := t.Grid.GetCursorLocked()
		t.setCursorPos(col+1, n)
	case 'b': // REP - Repeat preceding character
		n := t.getParam(params, 0, 1)
		t.Grid.RepeatCharLocked(n)
	case 'h': // SM - Set mode
		t.setMode(params, true)
	case 'l': // RM - Reset mode
		t.setMode(params, false)
	case 'r': // DECSTBM - Set scrolling region
		top := t.getParam(params, 0, 1)
		bottom := t.getParam(params, 1, t.Grid.Rows)
		t.Grid.SetScrollRegionLocked(top, bottom)
		if t.originMode {
			t.setCursorPos(1, 1)
		} else {
			t.Grid.SetCursorPosLocked(1, 1)
		}
	case 's': // SCP - Save cursor position
		t.saveCursor()
	case 'u':
		// Kitty keyboard protocol uses CSI with a >/</=/? prefix and final 'u';
		// the unprefixed form is RCP (restore cursor position).
		switch {
		case t.csiHasPrefix('>'):
			t.kittyPush(uint8(t.getParam(params, 0, 0)))
		case t.csiHasPrefix('<'):
			t.kittyPop(t.getParam(params, 0, 1))
		case t.csiHasPrefix('='):
			t.kittySet(uint8(t.getParam(params, 0, 0)), t.getParam(params, 1, 1))
		case t.csiHasPrefix('?'):
			t.kittyQuery()
		default:
			t.restoreCursor()
		}
	case 'n': // DSR - Device status report (ignore for now)
		t.handleDSR(params)
	case 'c': // DA - Device attributes
		t.handleDA(params)
	case 'g': // TBC - Tab clear (0=at cursor, 3=all)
		t.Grid.ClearTabStopLocked(t.getParam(params, 0, 0))
	case 't': // Window manipulation: report text-area size in chars (18)
		if t.getParam(params, 0, 0) == 18 && t.responseWriter != nil {
			t.responseWriter(fmt.Appendf(nil, "\x1b[8;%d;%dt", t.Grid.Rows, t.Grid.Cols))
		}
	case 'q':
		if t.csiHasPrefix('>') {
			// XTVERSION (CSI > q): report terminal name/version as DCS > | text ST.
			if t.responseWriter != nil {
				t.responseWriter([]byte("\x1bP>|RavenTerminal\x1b\\"))
			}
		} else {
			// DECSCUSR (CSI Ps SP q) - set cursor style
			t.setCursorStyle(params)
		}
	case 'p':
		if t.csiHasSuffix('!') {
			// DECSTR - soft terminal reset
			t.softReset()
		} else if t.csiHasPrefix('?') && t.csiHasSuffix('$') {
			// DECRQM - request DEC private mode state
			t.handleDECRQM(params)
		}
	}
}

// sgrUnderlineStyleBase encodes a "4:n" underline sub-style as a single pseudo-param
// (base+n) so it survives executeSGR's int stream without colliding with real SGR codes
// (which top out at 107). Values: 0=none,1=solid,2=double,3=curly,4=dotted,5=dashed.
const sgrUnderlineStyleBase = 9000

// executeSGR handles SGR (Select Graphic Rendition) sequences
func (t *Terminal) executeSGR(params []int) {
	if len(params) == 0 {
		params = []int{0}
	}

	prevBg := t.currentBg
	i := 0
	for i < len(params) {
		p := params[i]
		switch {
		case p == 0: // Reset
			t.currentFg = grid.DefaultFg()
			t.currentBg = grid.DefaultBg()
			t.currentFlags = 0
			t.currentUnderlineStyle = 0
			t.currentUnderlineColor = grid.Color{}
		case p == 1: // Bold
			t.currentFlags |= grid.FlagBold
		case p == 2: // Dim/faint
			t.currentFlags |= grid.FlagDim
		case p == 3: // Italic
			t.currentFlags |= grid.FlagItalic
		case p == 4: // Underline (plain = solid)
			t.currentFlags |= grid.FlagUnderline
			t.currentUnderlineStyle = 1
		case p == 7: // Inverse
			t.currentFlags |= grid.FlagInverse
		case p == 8: // Hidden
			t.currentFlags |= grid.FlagHidden
		case p == 9: // Strikethrough
			t.currentFlags |= grid.FlagStrikethrough
		case p == 22: // Normal intensity (not bold, not dim)
			t.currentFlags &^= grid.FlagBold
			t.currentFlags &^= grid.FlagDim
		case p == 23: // Not italic
			t.currentFlags &^= grid.FlagItalic
		case p == 24: // Not underlined
			t.currentFlags &^= grid.FlagUnderline
			t.currentUnderlineStyle = 0
		case p == 27: // Not inverse
			t.currentFlags &^= grid.FlagInverse
		case p == 28: // Not hidden
			t.currentFlags &^= grid.FlagHidden
		case p == 29: // Not strikethrough
			t.currentFlags &^= grid.FlagStrikethrough
		case p >= 30 && p <= 37: // Standard foreground colors
			t.currentFg = grid.IndexedColor(uint8(p - 30))
		case p == 38: // Extended foreground color
			if i+1 < len(params) {
				if params[i+1] == 5 && i+2 < len(params) {
					// 256-color
					t.currentFg = grid.IndexedColor(uint8(params[i+2]))
					i += 2
				} else if params[i+1] == 2 && i+4 < len(params) {
					// RGB
					t.currentFg = grid.RGBColor(uint8(params[i+2]), uint8(params[i+3]), uint8(params[i+4]))
					i += 4
				}
			}
		case p == 39: // Default foreground
			t.currentFg = grid.DefaultFg()
		case p >= 40 && p <= 47: // Standard background colors
			t.currentBg = grid.IndexedColor(uint8(p - 40))
		case p == 48: // Extended background color
			if i+1 < len(params) {
				if params[i+1] == 5 && i+2 < len(params) {
					// 256-color
					t.currentBg = grid.IndexedColor(uint8(params[i+2]))
					i += 2
				} else if params[i+1] == 2 && i+4 < len(params) {
					// RGB
					t.currentBg = grid.RGBColor(uint8(params[i+2]), uint8(params[i+3]), uint8(params[i+4]))
					i += 4
				}
			}
		case p == 49: // Default background
			t.currentBg = grid.DefaultBg()
		case p == 58: // Set underline color (extended)
			if i+1 < len(params) {
				if params[i+1] == 5 && i+2 < len(params) {
					t.currentUnderlineColor = grid.IndexedColor(uint8(params[i+2]))
					i += 2
				} else if params[i+1] == 2 && i+4 < len(params) {
					t.currentUnderlineColor = grid.RGBColor(uint8(params[i+2]), uint8(params[i+3]), uint8(params[i+4]))
					i += 4
				}
			}
		case p == 59: // Default underline color (follow text color)
			t.currentUnderlineColor = grid.Color{}
		case p >= sgrUnderlineStyleBase && p <= sgrUnderlineStyleBase+5: // 4:n underline style
			style := uint8(p - sgrUnderlineStyleBase)
			if style == 0 {
				t.currentFlags &^= grid.FlagUnderline
				t.currentUnderlineStyle = 0
			} else {
				t.currentFlags |= grid.FlagUnderline
				t.currentUnderlineStyle = style
			}
		case p >= 90 && p <= 97: // Bright foreground colors
			t.currentFg = grid.IndexedColor(uint8(p - 90 + 8))
		case p >= 100 && p <= 107: // Bright background colors
			t.currentBg = grid.IndexedColor(uint8(p - 100 + 8))
		}
		i++
	}
	// Sync BCE erase background with current background, but only when it
	// actually changed — most SGR sequences touch attributes or foreground.
	if t.currentBg != prevBg {
		t.Grid.SetEraseBackgroundLocked(t.currentBg)
	}
}

// setMode handles setting/resetting terminal modes
func (t *Terminal) setMode(params []int, set bool) {
	// Check for private mode indicator
	private := t.csiHasPrefix('?')

	for _, p := range params {
		if !private {
			// ANSI (non-private) modes.
			switch p {
			case 4: // IRM - Insert/Replace mode
				t.insertMode = set
			case 20: // LNM - Line Feed/New Line mode
				t.lnmMode = set
			}
			continue
		}
		if private {
			switch p {
			case 1: // DECCKM - Application cursor keys
				t.appCursorKeys = set
			case 7: // DECAWM - Auto-wrap mode
				t.Grid.SetAutoWrapLocked(set)
			case 25: // DECTCEM - Text cursor enable
				t.cursorVisible = set
			case 6: // DECOM - Origin mode
				t.originMode = set
				if t.originMode {
					t.setCursorPos(1, 1)
				} else {
					t.Grid.SetCursorPosLocked(1, 1)
				}
			case 47, 1047: // Alternate screen buffer
				if set {
					t.enterAlternateScreen()
				} else {
					t.exitAlternateScreen()
				}
			case 1048: // Save/restore cursor (xterm)
				if set {
					t.saveCursor()
				} else {
					t.restoreCursor()
				}
			case 1049: // Alternate screen buffer with save/restore cursor
				if set {
					t.saveCursor()
					t.enterAlternateScreen()
				} else {
					t.exitAlternateScreen()
					t.restoreCursor()
				}
			case 2004: // Bracketed paste mode
				t.bracketedPaste = set
			case 1000: // Normal mouse tracking
				if set {
					t.mouseMode = 1000
				} else if t.mouseMode == 1000 {
					t.mouseMode = 0
				}
			case 1002: // Button-event tracking
				if set {
					t.mouseMode = 1002
				} else if t.mouseMode == 1002 {
					t.mouseMode = 0
				}
			case 1003: // Any-event tracking
				if set {
					t.mouseMode = 1003
				} else if t.mouseMode == 1003 {
					t.mouseMode = 0
				}
			case 1006: // SGR extended mode
				t.mouseSGRMode = set
			case 1004: // Focus reporting
				t.focusReporting = set
			case 2026: // Synchronized output
				t.syncActive = set
				if set {
					// Watchdog deadline: if the app never closes the sync, the
					// renderer resumes presenting after this elapses.
					t.syncDeadline = time.Now().Add(100 * time.Millisecond)
				}
			}
		}
	}
}

// relockBatchGrid moves Process's batch grid lock (see LockBatch) from the
// previous grid to t.Grid after an alternate-screen swap, so the XxxLocked
// calls below always run under the mutex of the grid they touch. The freshly
// swapped-in grid is only reachable via t.Grid, which t.mu guards, so taking
// its lock here is uncontended. No-op outside a batch.
func (t *Terminal) relockBatchGrid() {
	if t.batchGrid != nil && t.batchGrid != t.Grid {
		t.batchGrid.UnlockBatch()
		t.batchGrid = t.Grid
		t.batchGrid.LockBatch()
	}
}

// enterAlternateScreen switches to alternate screen buffer
func (t *Terminal) enterAlternateScreen() {
	if !t.alternateScreen {
		// Save main screen's scroll region
		t.savedMainScrollTop, t.savedMainScrollBottom = t.Grid.GetScrollRegionLocked()

		// Save terminal modes so they can be restored on exit
		t.savedMainAppCursorKeys = t.appCursorKeys
		t.savedMainBracketedPaste = t.bracketedPaste
		t.savedMainMouseMode = t.mouseMode
		t.savedMainMouseSGRMode = t.mouseSGRMode
		// Swap in the alt screen's own kitty-keyboard flag stack.
		t.kittyStack, t.altKittyStack = t.altKittyStack, t.kittyStack

		t.savedMainGrid = t.Grid
		// The alternate screen has no scrollback (standard VT behavior).
		t.Grid = grid.NewAltGrid(t.Grid.Cols, t.Grid.Rows)
		t.alternateScreen = true
		t.relockBatchGrid()
		// The snapshot buffer belongs to the old grid; force a fresh full copy.
		t.snapPrev = nil

		// Clear the alternate screen (standard behavior)
		t.Grid.ClearAllLocked()
		t.Grid.SetCursorPosLocked(1, 1)
	}
}

// exitAlternateScreen returns to main screen buffer with full state cleanup.
// Resets all terminal attributes so TUI app state doesn't leak into the main screen.
func (t *Terminal) exitAlternateScreen() {
	if t.alternateScreen && t.savedMainGrid != nil {
		t.Grid = t.savedMainGrid
		t.savedMainGrid = nil
		t.alternateScreen = false
		t.relockBatchGrid()
		// The snapshot buffer belongs to the alt grid; force a fresh full copy.
		t.snapPrev = nil

		// Restore scroll region without resetting cursor position
		t.Grid.RestoreScrollRegionLocked(t.savedMainScrollTop, t.savedMainScrollBottom)

		// Reset text attributes to defaults (prevent TUI colors leaking)
		t.currentFg = grid.DefaultFg()
		t.currentBg = grid.DefaultBg()
		t.currentFlags = 0

		// Reset BCE background on the restored grid
		t.Grid.SetEraseBackgroundLocked(grid.DefaultBg())

		// Reset charset state to ASCII defaults
		t.charsetG0 = charsetASCII
		t.charsetG1 = charsetASCII
		t.activeCharset = 0
		t.charsetPending = charsetTargetNone

		// Reset terminal modes
		t.originMode = false
		t.cursorStyle = CursorStyleBlock
		t.cursorVisible = true

		// Restore saved terminal modes from main screen
		t.appCursorKeys = t.savedMainAppCursorKeys
		t.bracketedPaste = t.savedMainBracketedPaste
		t.mouseMode = t.savedMainMouseMode
		t.mouseSGRMode = t.savedMainMouseSGRMode
		// Swap the main screen's kitty-keyboard flag stack back in.
		t.kittyStack, t.altKittyStack = t.altKittyStack, t.kittyStack

		// Clear stale wrap state from the restored grid
		t.Grid.ResetWrapPendingLocked()
	}
}

// processOSC handles OSC sequences (Operating System Command)
func (t *Terminal) processOSC(b byte) {
	// Terminators are BEL and ST in its 7-bit form (ESC \, via StateOSCEscape).
	// The 8-bit ST (0x9c) is NOT one: in a UTF-8 terminal 0x9c only occurs as a
	// continuation byte (e.g. U+2733 ✳ is e2 9c b3), so treating it as ST ends
	// the string early and spills the rest of the payload into the grid (the
	// same UTF-8 policy as the ground state, see processGround).
	if b == 0x07 {
		t.finishOSC()
	} else if b == 0x1b { // ESC - might be start of ST
		t.state = StateOSCEscape
	} else if len(t.oscParams) < maxOSCLen {
		t.oscParams = append(t.oscParams, b)
	}
}

// finishOSC dispatches a completed OSC (unless it overflowed the cap) and
// returns to ground state.
func (t *Terminal) finishOSC() {
	if len(t.oscParams) < maxOSCLen {
		t.handleOSC(string(t.oscParams))
	}
	t.oscParams = t.oscParams[:0]
	t.state = StateGround
}

// processOSCEscape handles bytes after ESC in OSC state
func (t *Terminal) processOSCEscape(b byte) {
	if b == 0x5c { // Backslash completes ST (ESC \)
		t.finishOSC()
	} else {
		// Not ST, ESC starts new sequence
		t.oscParams = t.oscParams[:0]
		t.state = StateEscape
		t.processEscape(b)
	}
}

// processDCS handles Device Control String sequences
func (t *Terminal) processDCS(b byte) {
	if b == 0x1b { // ESC - might be start of ST
		t.state = StateDCSEscape
	} else if b == 0x07 { // BEL is non-standard but common. 0x9c is NOT a
		// terminator: in UTF-8 mode it only occurs as a continuation byte (see
		// processOSC).
		t.finishDCS()
	} else if len(t.dcsParams) < maxDCSLen {
		t.dcsParams = append(t.dcsParams, b)
	}
}

// finishDCS dispatches a completed DCS (unless it overflowed the cap) and
// returns to ground state.
func (t *Terminal) finishDCS() {
	if len(t.dcsParams) < maxDCSLen {
		t.handleDCS(string(t.dcsParams))
	}
	t.dcsParams = t.dcsParams[:0]
	t.state = StateGround
}

// processDCSEscape handles bytes after ESC in DCS state
func (t *Terminal) processDCSEscape(b byte) {
	if b == 0x5c { // Backslash completes ST (ESC \)
		t.finishDCS()
	} else {
		// Not ST, treat as part of DCS
		if len(t.dcsParams) < maxDCSLen {
			t.dcsParams = append(t.dcsParams, 0x1b, b)
		}
		t.state = StateDCS
	}
}

// handleDCS handles DCS sequences like XTGETTCAP
func (t *Terminal) handleDCS(params string) {
	if t.responseWriter == nil {
		return
	}
	// Handle XTGETTCAP requests (DCS + q Pt ST)
	// These request terminfo capabilities
	if after, ok := strings.CutPrefix(params, "+q"); ok {
		caps := after
		t.handleXTGETTCAP(caps)
	}
	// Handle DECRQSS and other DCS sequences as needed
}

// handleXTGETTCAP responds to XTGETTCAP capability queries
func (t *Terminal) handleXTGETTCAP(hexCaps string) {
	if t.responseWriter == nil {
		return
	}
	// Capabilities are hex-encoded, separated by semicolons
	// Common queries: 524742 (RGB), 536574757020 (Setxxx)
	// Respond with DCS 1 + r <cap>=<value> ST for supported caps
	// Respond with DCS 0 + r ST for unsupported caps

	// For simplicity, report that we support common capabilities
	// RGB support (for truecolor)
	if hexCaps == "524742" { // "RGB" in hex
		// DCS 1 + r 524742 ST (capability supported)
		t.responseWriter([]byte("\x1bP1+r524742\x1b\\"))
		return
	}

	// For unknown capabilities, report not supported
	t.responseWriter([]byte("\x1bP0+r\x1b\\"))
}

func (t *Terminal) handleOSC(params string) {
	parts := strings.SplitN(params, ";", 2)
	if len(parts) < 1 {
		return
	}

	code := parts[0]
	value := ""
	if len(parts) > 1 {
		value = parts[1]
	}

	switch code {
	case "0": // Set icon name and window title
		t.iconName = value
		t.windowTitle = value
	case "1": // Set icon name
		t.iconName = value
	case "2": // Set window title
		t.windowTitle = value
	case "4": // Set/query a palette entry: 4;index;spec (or index;?)
		t.handleOSC4(value)
	case "10": // Set/query default foreground
		t.handleColorOSC("10", value, &t.fgColor)
	case "11": // Set/query default background
		t.handleColorOSC("11", value, &t.bgColor)
	case "12": // Set/query cursor color
		t.handleColorOSC("12", value, &t.cursorColor)
	case "7": // Working directory
		path := parseOSC7Path(value)
		if path != "" {
			t.lastWorkingDir = path
		}
	case "8": // Hyperlink: OSC 8 ; params ; URI
		// value is "params;URI"; an empty URI closes the current hyperlink.
		uri := ""
		if _, after, ok := strings.Cut(value, ";"); ok {
			uri = after
		}
		if uri == "" {
			t.currentLinkID = 0
		} else {
			t.currentLinkID = t.Grid.InternLinkLocked(uri)
		}
	case "52": // Clipboard access: OSC 52 ; Pc ; Pd
		t.handleOSC52(value)
	}
}

// handleOSC52 implements OSC 52 clipboard set/query.
// value is "Pc;Pd" where Pd is base64 data, or "?" to query the clipboard.
func (t *Terminal) handleOSC52(value string) {
	parts := strings.SplitN(value, ";", 2)
	if len(parts) < 2 {
		return
	}
	selection := parts[0] // c=clipboard, p=primary, etc. (treated uniformly)
	data := parts[1]
	if data == "?" {
		// Query: respond with the current clipboard contents, base64-encoded.
		reader := t.clipboardReader
		if reader == nil {
			defaultClipboardReaderMu.Lock()
			reader = defaultClipboardReader
			defaultClipboardReaderMu.Unlock()
		}
		writer := t.responseWriter
		if reader == nil || writer == nil {
			return
		}
		// The reader may block (the host answers from its main thread, which
		// also needs Terminal.mu to render), and Process holds t.mu — so the
		// query is answered from a goroutine, off the lock. A single writer
		// call keeps the reply sequence contiguous on the PTY.
		go func() {
			enc := base64.StdEncoding.EncodeToString([]byte(reader()))
			writer([]byte("\x1b]52;" + selection + ";" + enc + "\x1b\\"))
		}()
		return
	}
	// Set: decode base64 and write to the system clipboard.
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil || t.clipboardWriter == nil {
		return // ignore malformed data silently
	}
	t.clipboardWriter(string(decoded))
}

// handleColorOSC implements OSC 10/11/12 dynamic color set and query. A value
// of "?" queries the current color and replies with the X11 rgb form.
func (t *Terminal) handleColorOSC(code, value string, target *grid.Color) {
	if value == "?" {
		if t.responseWriter == nil || target.Type != grid.ColorRGB {
			return
		}
		t.responseWriter([]byte("\x1b]" + code + ";" + formatXColor(*target) + "\x1b\\"))
		return
	}
	if c, ok := parseXColor(value); ok {
		*target = c
	}
}

// handleOSC4 implements OSC 4 palette set/query: "index;spec" (set) or
// "index;?" (query). Only a single index;value pair is handled per call.
func (t *Terminal) handleOSC4(value string) {
	parts := strings.SplitN(value, ";", 2)
	if len(parts) != 2 {
		return
	}
	idx, err := strconv.Atoi(parts[0])
	if err != nil || idx < 0 || idx > 255 {
		return
	}
	spec := parts[1]
	if spec == "?" {
		if t.responseWriter == nil {
			return
		}
		if t.paletteOverride != nil {
			if c, ok := t.paletteOverride[idx]; ok && c.Type == grid.ColorRGB {
				t.responseWriter(fmt.Appendf(nil, "\x1b]4;%d;%s\x1b\\", idx, formatXColor(c)))
			}
		}
		return
	}
	if c, ok := parseXColor(spec); ok {
		if t.paletteOverride == nil {
			t.paletteOverride = make(map[int]grid.Color)
		}
		t.paletteOverride[idx] = c
	}
}

// ForegroundColor / BackgroundColor / CursorColor expose any OSC-set colors
// (Type == ColorRGB when set; ColorDefault otherwise). PaletteOverride returns
// any OSC 4 palette entry overrides. The renderer consults these.
func (t *Terminal) ForegroundColor() grid.Color { t.mu.Lock(); defer t.mu.Unlock(); return t.fgColor }
func (t *Terminal) BackgroundColor() grid.Color { t.mu.Lock(); defer t.mu.Unlock(); return t.bgColor }
func (t *Terminal) CursorColor() grid.Color     { t.mu.Lock(); defer t.mu.Unlock(); return t.cursorColor }

// PaletteOverride returns the OSC 4 color override for an index, if any.
func (t *Terminal) PaletteOverride(idx int) (grid.Color, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.paletteOverride == nil {
		return grid.Color{}, false
	}
	c, ok := t.paletteOverride[idx]
	return c, ok
}

func parseOSC7Path(value string) string {
	if strings.HasPrefix(value, "file://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return ""
		}
		if parsed.Path == "" {
			return ""
		}
		path, err := url.PathUnescape(parsed.Path)
		if err != nil {
			return ""
		}
		return path
	}
	if strings.HasPrefix(value, "/") {
		return value
	}
	return ""
}

// WorkingDir returns the last known working directory from OSC 7.
func (t *Terminal) WorkingDir() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastWorkingDir
}

// BracketedPasteEnabled returns whether bracketed paste mode is enabled (?2004)
func (t *Terminal) BracketedPasteEnabled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.bracketedPaste
}

// FocusReportingEnabled returns whether focus event reporting is enabled (?1004)
func (t *Terminal) FocusReportingEnabled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.focusReporting
}

// SyncActive reports whether the app is holding a synchronized-output frame (?2026).
// It applies a watchdog: if the hold has exceeded its deadline, it is cleared so the
// renderer resumes presenting (guarantees liveness even if the app never closes it).
func (t *Terminal) SyncActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.syncActive {
		return false
	}
	if time.Now().After(t.syncDeadline) {
		t.syncActive = false
		return false
	}
	return true
}

// SetClipboardWriter sets the callback used to write OSC 52 clipboard data.
func (t *Terminal) SetClipboardWriter(writer func(string)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clipboardWriter = writer
}

// SetClipboardReader sets the callback used to answer OSC 52 clipboard queries.
func (t *Terminal) SetClipboardReader(reader func() string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clipboardReader = reader
}

// defaultClipboardReader answers OSC 52 clipboard queries for any terminal
// without its own reader. Process-wide so terminals created after wiring
// (new tabs/panes) are covered too.
var (
	defaultClipboardReaderMu sync.Mutex
	defaultClipboardReader   func() string
)

// SetDefaultClipboardReader sets the process-wide fallback OSC 52 clipboard
// reader. The reader is invoked on parser (PTY reader) goroutines.
func SetDefaultClipboardReader(reader func() string) {
	defaultClipboardReaderMu.Lock()
	defer defaultClipboardReaderMu.Unlock()
	defaultClipboardReader = reader
}

// GetWindowTitle returns the current window title (set via OSC 0/2)
func (t *Terminal) GetWindowTitle() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.windowTitle
}

// GetMouseMode returns the current mouse tracking mode (0=off, 1000/1002/1003)
func (t *Terminal) GetMouseMode() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mouseMode
}

// MouseSGREnabled returns whether SGR extended mouse mode is enabled (?1006)
func (t *Terminal) MouseSGREnabled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mouseSGRMode
}

// EncodeMouseEvent returns the escape sequence for a mouse event
// button: 0=left, 1=middle, 2=right, 3=release, 64=scroll up, 65=scroll down
// x, y: 1-based coordinates
// pressed: true for press, false for release
func (t *Terminal) EncodeMouseEvent(button int, x, y int, pressed bool) []byte {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.mouseMode == 0 {
		return nil
	}
	return encodeMouseBytes(t.mouseSGRMode, button, x, y, pressed)
}

// atoiBytes parses a decimal integer exactly like strconv.Atoi: an optional
// leading sign followed by digits only; ok is false on empty input, stray
// characters, or overflow (callers treat !ok as 0, matching the old
// "n, _ := strconv.Atoi(...)" behavior).
func atoiBytes(s []byte) (n int, ok bool) {
	if len(s) == 0 {
		return 0, false
	}
	i := 0
	neg := false
	if s[0] == '+' || s[0] == '-' {
		neg = s[0] == '-'
		i = 1
		if len(s) == 1 {
			return 0, false
		}
	}
	// Accumulate negatively so MinInt is representable, mirroring Atoi.
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		d := int(c - '0')
		if n < (math.MinInt+d)/10 {
			return 0, false // overflow
		}
		n = n*10 - d
	}
	if !neg {
		if n < -math.MaxInt {
			return 0, false // overflow
		}
		n = -n
	}
	return n, true
}

// parseSGRParams parses CSI parameters for SGR sequences, properly expanding
// colon sub-parameters for extended color sequences (38, 48, 58) per ISO 8613-6.
// Modern apps like Neovim use "38:2:R:G:B" instead of "38;2;R;G;B".
// The result reuses t.paramScratch and must be consumed before the next parse.
func (t *Terminal) parseSGRParams() []int {
	s := t.csiBuf
	// Remove private/prefix indicators, one byte each in this order.
	for _, p := range [...]byte{'?', '>', '!'} {
		if len(s) > 0 && s[0] == p {
			s = s[1:]
		}
	}
	if len(s) == 0 {
		return nil
	}
	params := t.paramScratch[:0]
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ';' {
			params = t.appendSGRPart(params, s[start:i])
			start = i + 1
		}
	}
	t.paramScratch = params
	return params
}

// appendSGRPart parses one ';'-separated SGR parameter (possibly carrying
// ':'-separated sub-parameters) and appends the resulting code(s) to params.
func (t *Terminal) appendSGRPart(params []int, part []byte) []int {
	hasColon := false
	for _, c := range part {
		if c == ':' {
			hasColon = true
			break
		}
	}
	if !hasColon {
		n, _ := atoiBytes(part)
		return append(params, n)
	}

	subs := t.subScratch[:0]
	var second []byte // raw bytes of subparts[1], for the "2"/"5" checks below
	idx := 0
	start := 0
	for k := 0; k <= len(part); k++ {
		if k == len(part) || part[k] == ':' {
			sp := part[start:k]
			if idx == 1 {
				second = sp
			}
			n, _ := atoiBytes(sp)
			subs = append(subs, n)
			idx++
			start = k + 1
		}
	}

	first := subs[0]
	if first == 38 || first == 48 || first == 58 {
		// Expand colon sub-params for extended color sequences.
		// The ITU T.416 RGB form carries a colorspace ID slot
		// ("38:2::R:G:B"); drop it, and truncate any trailing
		// sub-params (tolerance etc.) so executeSGR sees exactly
		// the semicolon stream (38;2;R;G;B / 38;5;n) with no
		// leftovers that would run as spurious SGR codes.
		if len(subs) > 1 && len(second) == 1 && second[0] == '2' {
			if len(subs) >= 6 {
				subs = append(subs[:2], subs[3:]...)
			}
			if len(subs) > 5 {
				subs = subs[:5]
			}
		} else if len(subs) > 1 && len(second) == 1 && second[0] == '5' && len(subs) > 3 {
			subs = subs[:3]
		}
		t.subScratch = subs
		return append(params, subs...)
	} else if first == 4 && len(subs) > 1 {
		// Underline style "4:n" (curly/dotted/dashed/etc) - encode the sub-style
		t.subScratch = subs
		return append(params, sgrUnderlineStyleBase+subs[1])
	}
	// For other colon codes, keep first value only
	t.subScratch = subs
	return append(params, first)
}

// parseParams parses CSI parameters, reusing t.paramScratch; the result must
// be consumed before the next parse.
func (t *Terminal) parseParams() []int {
	s := t.csiBuf
	// Remove private/prefix indicators (?, >, <, =, !), one byte each in this
	// order (chained, matching the old strings.TrimPrefix sequence).
	for _, p := range [...]byte{'?', '>', '<', '=', '!'} {
		if len(s) > 0 && s[0] == p {
			s = s[1:]
		}
	}
	// Strip trailing intermediate bytes (0x20-0x2f), e.g. the space in the
	// DECSCUSR sequence "CSI Ps SP q", so the parameter parses cleanly.
	for len(s) > 0 && s[len(s)-1] >= 0x20 && s[len(s)-1] <= 0x2f {
		s = s[:len(s)-1]
	}

	if len(s) == 0 {
		return nil
	}

	params := t.paramScratch[:0]
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ';' {
			part := s[start:i]
			// Handle sub-parameters (colon-separated) by taking the first one
			for j, c := range part {
				if c == ':' {
					part = part[:j]
					break
				}
			}
			n, _ := atoiBytes(part)
			params = append(params, n)
			start = i + 1
		}
	}
	t.paramScratch = params
	return params
}

// getParam gets a parameter with a default value
func (t *Terminal) getParam(params []int, index, defaultVal int) int {
	if index < len(params) && params[index] > 0 {
		return params[index]
	}
	return defaultVal
}

// softReset implements DECSTR (CSI ! p): reset modes and attributes without
// clearing the screen. TUI apps (vim among them) issue this on startup/exit.
func (t *Terminal) softReset() {
	t.currentFg = grid.DefaultFg()
	t.currentBg = grid.DefaultBg()
	t.currentFlags = 0
	t.currentUnderlineStyle = 0
	t.currentUnderlineColor = grid.Color{}
	t.currentLinkID = 0
	t.Grid.SetEraseBackgroundLocked(grid.DefaultBg())
	t.originMode = false
	t.insertMode = false
	t.appCursorKeys = false
	t.cursorVisible = true
	t.cursorStyle = CursorStyleBlock
	t.cursorBlinking = true
	t.Grid.SetAutoWrapLocked(true)
	// Margins reset to the full screen; DECSTR does not move the cursor.
	t.Grid.RestoreScrollRegionLocked(1, t.Grid.Rows)
	t.charsetG0 = charsetASCII
	t.charsetG1 = charsetASCII
	t.activeCharset = 0
	t.charsetPending = charsetTargetNone
}

// handleDECRQM responds to DECRQM (CSI ? Ps $ p) private-mode state requests
// with CSI ? Ps ; Pm $ y where Pm is 1=set, 2=reset, 0=not recognized.
func (t *Terminal) handleDECRQM(params []int) {
	if t.responseWriter == nil {
		return
	}
	mode := t.getParam(params, 0, 0)
	report := func(set bool) int {
		if set {
			return 1
		}
		return 2
	}
	state := 0 // not recognized
	switch mode {
	case 1:
		state = report(t.appCursorKeys)
	case 6:
		state = report(t.originMode)
	case 7:
		state = report(t.Grid.GetAutoWrapLocked())
	case 25:
		state = report(t.cursorVisible)
	case 47, 1047, 1049:
		state = report(t.alternateScreen)
	case 1000, 1002, 1003:
		state = report(t.mouseMode == mode)
	case 1004:
		state = report(t.focusReporting)
	case 1006:
		state = report(t.mouseSGRMode)
	case 2004:
		state = report(t.bracketedPaste)
	case 2026:
		state = report(t.syncActive)
	}
	t.responseWriter(fmt.Appendf(nil, "\x1b[?%d;%d$y", mode, state))
}

// reset resets the terminal state
func (t *Terminal) reset() {
	t.Grid.ClearAllLocked()
	t.Grid.SetCursorPosLocked(1, 1)
	t.currentFg = grid.DefaultFg()
	t.currentBg = grid.DefaultBg()
	t.currentFlags = 0
	t.Grid.SetEraseBackgroundLocked(grid.DefaultBg())
	t.appCursorKeys = false
	t.cursorVisible = true
	t.exitAlternateScreen()
	t.charsetG0 = charsetASCII
	t.charsetG1 = charsetASCII
	t.activeCharset = 0
	t.charsetPending = charsetTargetNone
	t.originMode = false
	t.insertMode = false
	t.lnmMode = false
	t.kittyStack = nil
	t.altKittyStack = nil
	t.cursorStyle = CursorStyleBlock
	t.cursorBlinking = true
	t.currentLinkID = 0
	t.currentUnderlineStyle = 0
	t.currentUnderlineColor = grid.Color{}
	t.focusReporting = false
	t.syncActive = false
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
		t.savedMainGrid.Resize(cols, rows)
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
	t.Grid.Resize(cols, rows)
}

// IsCursorVisible returns whether the cursor should be visible
func (t *Terminal) IsCursorVisible() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cursorVisible
}

// CursorStyle returns the current cursor style.
func (t *Terminal) CursorStyle() CursorStyle {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cursorStyle
}

// CursorBlinking reports whether the cursor style requests blinking (DECSCUSR).
func (t *Terminal) CursorBlinking() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cursorBlinking
}

// AppCursorKeys returns whether application cursor keys mode is enabled
func (t *Terminal) AppCursorKeys() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.appCursorKeys
}

// SetResponseWriter sets a callback used to write responses back to the PTY.
func (t *Terminal) SetResponseWriter(writer func([]byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.responseWriter = writer
}

// GetGrid returns the current grid with thread-safe access.
// Use this from render and main goroutines instead of accessing Terminal.Grid directly.
func (t *Terminal) GetGrid() *grid.Grid {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Grid
}

// Snapshot produces a fully-resolved copy of the visible grid for the renderer,
// taken under the terminal lock so the active/alternate grid pointer cannot
// swap mid-copy. The renderer consumes the returned value lock-free. The buffer
// is reused across frames to avoid per-frame allocation.
func (t *Terminal) Snapshot() *grid.Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Refresh the selection's viewport projection so the highlight tracks view
	// scrolling and content scrolling in (selection anchors are absolute).
	t.Grid.SyncSelectionView()
	s := t.Grid.Snapshot(t.snapPrev)
	s.CursorVisible = t.cursorVisible
	t.snapPrev = s
	return s
}

func (t *Terminal) handleDSR(params []int) {
	if t.responseWriter == nil {
		return
	}
	code := t.getParam(params, 0, 0)
	switch code {
	case 5: // Status report
		t.responseWriter([]byte("\x1b[0n"))
	case 6: // Cursor position report
		col, row := t.Grid.GetCursorLocked()
		if t.originMode {
			top, _ := t.Grid.GetScrollRegionLocked()
			row = max(row-(top-1), 0)
		}
		response := fmt.Sprintf("\x1b[%d;%dR", row+1, col+1)
		t.responseWriter([]byte(response))
	}
}

// handleDA handles Device Attributes queries (ESC[c or ESC[>c)
func (t *Terminal) handleDA(params []int) {
	if t.responseWriter == nil {
		return
	}
	// Check for secondary DA (ESC[>c)
	if t.csiHasPrefix('>') {
		// Secondary DA: report as xterm version 136
		// Format: ESC[>Pp;Pv;Pc c where Pp=terminal type, Pv=version, Pc=ROM cartridge
		t.responseWriter([]byte("\x1b[>0;136;0c"))
	} else if t.csiHasPrefix('=') {
		// Tertiary DA (ESC[=c): report unit ID in DECRPTUI format.
		t.responseWriter([]byte("\x1bP!|00000000\x1b\\"))
	} else {
		// Primary DA: report as VT220 with various features
		// 62 = VT220, 22 = ANSI color, 29 = ANSI text locator
		// The response advertises exactly what we implement:
		// - VT220 features (62)
		// - ANSI color (22)
		// NOTE: do not advertise Sixel (4) or other features we don't render.
		t.responseWriter([]byte("\x1b[?62;22c"))
	}
}

// saveCursor saves current cursor state to appropriate screen's slot
func (t *Terminal) saveCursor() {
	col, row := t.Grid.GetCursorLocked()
	state := CursorState{
		col:   col,
		row:   row,
		fg:    t.currentFg,
		bg:    t.currentBg,
		flags: t.currentFlags,
	}
	if t.alternateScreen {
		t.savedAlternateCursor = state
	} else {
		t.savedMainCursor = state
	}
}

// restoreCursor restores cursor state with bounds checking
func (t *Terminal) restoreCursor() {
	var state CursorState
	if t.alternateScreen {
		state = t.savedAlternateCursor
	} else {
		state = t.savedMainCursor
	}

	// Clamp to current grid bounds
	col, row := state.col, state.row
	if col < 0 {
		col = 0
	} else if col >= t.Grid.Cols {
		col = t.Grid.Cols - 1
	}
	if row < 0 {
		row = 0
	} else if row >= t.Grid.Rows {
		row = t.Grid.Rows - 1
	}

	t.Grid.SetCursorPosLocked(col+1, row+1)
	t.currentFg = state.fg
	t.currentBg = state.bg
	t.currentFlags = state.flags
}
