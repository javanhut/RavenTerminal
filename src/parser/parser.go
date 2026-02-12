package parser

import (
	"fmt"
	"github.com/javanhut/RavenTerminal/src/grid"
	"net/url"
	"strconv"
	"strings"
	"sync"
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

// Terminal handles ANSI escape sequence parsing and state
type Terminal struct {
	Grid            *grid.Grid
	state           ParserState
	csiParams       string
	oscParams       string
	dcsParams       string
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
	utf8Buf       []byte
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
	// Cursor style (DECSCUSR)
	cursorStyle CursorStyle
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
	}
}

// Process processes incoming bytes from the PTY
func (t *Terminal) Process(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, b := range data {
		t.processByte(b)
	}
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
	case StateCharset:
		// Character set designation - consume the designator byte
		t.setCharset(b)
		t.state = StateGround
	case StateHash:
		// DEC special sequences like ESC # 8 (DECALN)
		t.state = StateGround
	}
}

// processGround handles bytes in ground state
func (t *Terminal) processGround(b byte) {
	// If we're in the middle of a UTF-8 sequence, continue it
	if t.utf8Remaining > 0 {
		if b&0xC0 == 0x80 { // Valid continuation byte
			t.utf8Buf = append(t.utf8Buf, b)
			t.utf8Remaining--
			if t.utf8Remaining == 0 {
				// Complete UTF-8 sequence - decode and write
				r := t.mapCharsetRune(decodeUTF8(t.utf8Buf))
				t.Grid.WriteChar(r, t.currentFg, t.currentBg, t.currentFlags)
				t.utf8Buf = nil
			}
		} else {
			// Invalid continuation - discard and process this byte normally
			t.utf8Buf = nil
			t.utf8Remaining = 0
			t.processGround(b)
		}
		return
	}

	switch b {
	case 0x1b: // ESC
		t.state = StateEscape
	case 0x9b: // CSI (8-bit C1)
		t.state = StateCSI
		t.csiParams = ""
	case 0x9d: // OSC (8-bit C1)
		t.state = StateOSC
		t.oscParams = ""
	case 0x90: // DCS (8-bit C1)
		t.state = StateDCS
		t.dcsParams = ""
	case 0x07: // BEL
		// Bell - ignore
	case 0x08: // BS
		t.Grid.Backspace()
	case 0x09: // HT (Tab)
		t.Grid.Tab()
	case 0x0e: // SO (Shift Out) - select G1
		t.activeCharset = 1
	case 0x0f: // SI (Shift In) - select G0
		t.activeCharset = 0
	case 0x0a, 0x0b, 0x0c: // LF, VT, FF
		t.Grid.Newline()
		// Scroll position preserved - reset happens on user input instead
	case 0x0d: // CR
		t.Grid.CarriageReturn()
	case 0x9c: // ST (String Terminator) - ignore in ground
		// No-op
	default:
		if b >= 0x20 && b < 0x7f {
			// ASCII printable character
			r := t.mapCharsetRune(rune(b))
			t.Grid.WriteChar(r, t.currentFg, t.currentBg, t.currentFlags)
		} else if b >= 0xC0 && b < 0xE0 {
			// Start of 2-byte UTF-8 sequence
			t.utf8Buf = []byte{b}
			t.utf8Remaining = 1
		} else if b >= 0xE0 && b < 0xF0 {
			// Start of 3-byte UTF-8 sequence
			t.utf8Buf = []byte{b}
			t.utf8Remaining = 2
		} else if b >= 0xF0 && b < 0xF8 {
			// Start of 4-byte UTF-8 sequence
			t.utf8Buf = []byte{b}
			t.utf8Remaining = 3
		}
		// Ignore other bytes (control characters, invalid UTF-8 start bytes)
	}
}

// decodeUTF8 decodes a UTF-8 byte sequence to a rune
func decodeUTF8(buf []byte) rune {
	if len(buf) == 0 {
		return 0xFFFD // Replacement character
	}

	switch len(buf) {
	case 1:
		return rune(buf[0])
	case 2:
		if buf[0]&0xE0 == 0xC0 {
			return rune(buf[0]&0x1F)<<6 | rune(buf[1]&0x3F)
		}
	case 3:
		if buf[0]&0xF0 == 0xE0 {
			return rune(buf[0]&0x0F)<<12 | rune(buf[1]&0x3F)<<6 | rune(buf[2]&0x3F)
		}
	case 4:
		if buf[0]&0xF8 == 0xF0 {
			return rune(buf[0]&0x07)<<18 | rune(buf[1]&0x3F)<<12 | rune(buf[2]&0x3F)<<6 | rune(buf[3]&0x3F)
		}
	}

	return 0xFFFD // Replacement character for invalid sequences
}

// setCursorPos applies origin mode if enabled, then clamps to bounds.
func (t *Terminal) setCursorPos(col, row int) {
	if t.originMode {
		top, bottom := t.Grid.GetScrollRegion()
		row = top + row - 1
		if row < top {
			row = top
		} else if row > bottom {
			row = bottom
		}
	}
	t.Grid.SetCursorPos(col, row)
}

// moveCursor moves the cursor and clamps to the scroll region if origin mode is enabled.
func (t *Terminal) moveCursor(dCol, dRow int) {
	if !t.originMode {
		t.Grid.MoveCursor(dCol, dRow)
		return
	}
	col, row := t.Grid.GetCursor()
	col += dCol
	row += dRow
	if col < 0 {
		col = 0
	} else if col >= t.Grid.Cols {
		col = t.Grid.Cols - 1
	}
	top, bottom := t.Grid.GetScrollRegion()
	top--
	bottom--
	if row < top {
		row = top
	} else if row > bottom {
		row = bottom
	}
	t.Grid.SetCursorPos(col+1, row+1)
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
		t.csiParams = ""
	case ']': // OSC
		t.state = StateOSC
		t.oscParams = ""
	case 'P': // DCS - Device Control String
		t.state = StateDCS
		t.dcsParams = ""
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
		_, row := t.Grid.GetCursor()
		_, bottom := t.Grid.GetScrollRegion()
		if row == bottom-1 { // At bottom of scroll region (0-based vs 1-based)
			t.Grid.ScrollUpWithBg(1, t.currentBg)
		} else {
			t.Grid.MoveCursor(0, 1)
		}
		t.state = StateGround
	case 'M': // RI - Reverse index (up, respects scroll region, with BCE)
		_, row := t.Grid.GetCursor()
		top, _ := t.Grid.GetScrollRegion()
		if row == top-1 { // At top of scroll region (0-based vs 1-based)
			t.Grid.ScrollDownWithBg(1, t.currentBg)
		} else if row > 0 {
			t.Grid.MoveCursor(0, -1)
		}
		t.state = StateGround
	case 'E': // NEL - Next line
		t.Grid.CarriageReturn()
		t.Grid.Newline()
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
	default:
		t.state = StateGround
	}
}

// processCSI handles bytes in CSI state
func (t *Terminal) processCSI(b byte) {
	if b >= 0x30 && b <= 0x3f {
		// Parameter byte
		t.csiParams += string(b)
	} else if b >= 0x20 && b <= 0x2f {
		// Intermediate byte
		t.csiParams += string(b)
	} else if b >= 0x40 && b <= 0x7e {
		// Final byte
		t.executeCSI(b)
		t.csiParams = "" // Clear params after execution
		t.state = StateGround
	} else {
		t.csiParams = "" // Clear params on abort
		t.state = StateGround
	}
}

// executeCSI executes a CSI sequence
func (t *Terminal) executeCSI(final byte) {
	params := t.parseParams(t.csiParams)

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
		t.Grid.CarriageReturn()
		t.moveCursor(0, n)
	case 'F': // CPL - Cursor previous line
		n := t.getParam(params, 0, 1)
		t.Grid.CarriageReturn()
		t.moveCursor(0, -n)
	case 'G': // CHA - Cursor horizontal absolute
		n := t.getParam(params, 0, 1)
		_, row := t.Grid.GetCursor()
		t.Grid.SetCursorPos(n, row+1)
	case 'H', 'f': // CUP - Cursor position
		row := t.getParam(params, 0, 1)
		col := t.getParam(params, 1, 1)
		t.setCursorPos(col, row)
	case 'J': // ED - Erase in display (with BCE support)
		n := t.getParam(params, 0, 0)
		switch n {
		case 0:
			t.Grid.ClearToEndWithBg(t.currentBg)
		case 1:
			t.Grid.ClearToStartWithBg(t.currentBg)
		case 2, 3:
			t.Grid.ClearAllWithBg(t.currentBg)
		}
	case 'K': // EL - Erase in line (with BCE support)
		n := t.getParam(params, 0, 0)
		switch n {
		case 0:
			t.Grid.ClearLineToEndWithBg(t.currentBg)
		case 1:
			t.Grid.ClearLineToStartWithBg(t.currentBg)
		case 2:
			t.Grid.ClearLineWithBg(t.currentBg)
		}
	case 'L': // IL - Insert lines (with BCE support)
		n := t.getParam(params, 0, 1)
		t.Grid.InsertLinesWithBg(n, t.currentBg)
	case 'M': // DL - Delete lines (with BCE support)
		n := t.getParam(params, 0, 1)
		t.Grid.DeleteLinesWithBg(n, t.currentBg)
	case 'P': // DCH - Delete characters
		n := t.getParam(params, 0, 1)
		t.Grid.DeleteChars(n)
	case '@': // ICH - Insert characters
		n := t.getParam(params, 0, 1)
		t.Grid.InsertChars(n)
	case 'S': // SU - Scroll up (with BCE support)
		n := t.getParam(params, 0, 1)
		t.Grid.ScrollUpWithBg(n, t.currentBg)
	case 'T': // SD - Scroll down (with BCE support)
		n := t.getParam(params, 0, 1)
		t.Grid.ScrollDownWithBg(n, t.currentBg)
	case 'X': // ECH - Erase character (erase n chars at cursor without moving)
		n := t.getParam(params, 0, 1)
		t.Grid.EraseChars(n)
	case 'd': // VPA - Vertical position absolute
		n := t.getParam(params, 0, 1)
		col, _ := t.Grid.GetCursor()
		t.setCursorPos(col+1, n)
	case 'b': // REP - Repeat preceding character
		n := t.getParam(params, 0, 1)
		t.Grid.RepeatChar(n)
	case 'm': // SGR - Select graphic rendition
		sgrParams := t.parseSGRParams(t.csiParams)
		t.executeSGR(sgrParams)
	case 'h': // SM - Set mode
		t.setMode(params, true)
	case 'l': // RM - Reset mode
		t.setMode(params, false)
	case 'r': // DECSTBM - Set scrolling region
		top := t.getParam(params, 0, 1)
		bottom := t.getParam(params, 1, t.Grid.Rows)
		t.Grid.SetScrollRegion(top, bottom)
		if t.originMode {
			t.setCursorPos(1, 1)
		} else {
			t.Grid.SetCursorPos(1, 1)
		}
	case 's': // SCP - Save cursor position
		t.saveCursor()
	case 'u': // RCP - Restore cursor position
		t.restoreCursor()
	case 'n': // DSR - Device status report (ignore for now)
		t.handleDSR(params)
	case 'c': // DA - Device attributes
		t.handleDA(params)
	case 't': // Window manipulation (ignore)
	case 'q': // DECSCUSR - Set cursor style (ignore for now)
		t.setCursorStyle(params)
	}
}

// executeSGR handles SGR (Select Graphic Rendition) sequences
func (t *Terminal) executeSGR(params []int) {
	if len(params) == 0 {
		params = []int{0}
	}

	i := 0
	for i < len(params) {
		p := params[i]
		switch {
		case p == 0: // Reset
			t.currentFg = grid.DefaultFg()
			t.currentBg = grid.DefaultBg()
			t.currentFlags = 0
		case p == 1: // Bold
			t.currentFlags |= grid.FlagBold
		case p == 2: // Dim/faint
			t.currentFlags |= grid.FlagDim
		case p == 3: // Italic
			t.currentFlags |= grid.FlagItalic
		case p == 4: // Underline
			t.currentFlags |= grid.FlagUnderline
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
		case p >= 90 && p <= 97: // Bright foreground colors
			t.currentFg = grid.IndexedColor(uint8(p - 90 + 8))
		case p >= 100 && p <= 107: // Bright background colors
			t.currentBg = grid.IndexedColor(uint8(p - 100 + 8))
		}
		i++
	}
	// Sync BCE erase background with current background
	t.Grid.SetEraseBackground(t.currentBg)
}

// setMode handles setting/resetting terminal modes
func (t *Terminal) setMode(params []int, set bool) {
	// Check for private mode indicator
	private := strings.HasPrefix(t.csiParams, "?")

	for _, p := range params {
		if private {
			switch p {
			case 1: // DECCKM - Application cursor keys
				t.appCursorKeys = set
			case 7: // DECAWM - Auto-wrap mode
				t.Grid.SetAutoWrap(set)
			case 25: // DECTCEM - Text cursor enable
				t.cursorVisible = set
			case 6: // DECOM - Origin mode
				t.originMode = set
				if t.originMode {
					t.setCursorPos(1, 1)
				} else {
					t.Grid.SetCursorPos(1, 1)
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
			}
		}
	}
}

// enterAlternateScreen switches to alternate screen buffer
func (t *Terminal) enterAlternateScreen() {
	if !t.alternateScreen {
		// Save main screen's scroll region
		t.savedMainScrollTop, t.savedMainScrollBottom = t.Grid.GetScrollRegion()

		// Save terminal modes so they can be restored on exit
		t.savedMainAppCursorKeys = t.appCursorKeys
		t.savedMainBracketedPaste = t.bracketedPaste
		t.savedMainMouseMode = t.mouseMode
		t.savedMainMouseSGRMode = t.mouseSGRMode

		t.savedMainGrid = t.Grid
		t.Grid = grid.NewGrid(t.Grid.Cols, t.Grid.Rows)
		t.alternateScreen = true

		// Clear the alternate screen (standard behavior)
		t.Grid.ClearAll()
		t.Grid.SetCursorPos(1, 1)
	}
}

// exitAlternateScreen returns to main screen buffer with full state cleanup.
// Resets all terminal attributes so TUI app state doesn't leak into the main screen.
func (t *Terminal) exitAlternateScreen() {
	if t.alternateScreen && t.savedMainGrid != nil {
		t.Grid = t.savedMainGrid
		t.savedMainGrid = nil
		t.alternateScreen = false

		// Restore scroll region without resetting cursor position
		t.Grid.RestoreScrollRegion(t.savedMainScrollTop, t.savedMainScrollBottom)

		// Reset text attributes to defaults (prevent TUI colors leaking)
		t.currentFg = grid.DefaultFg()
		t.currentBg = grid.DefaultBg()
		t.currentFlags = 0

		// Reset BCE background on the restored grid
		t.Grid.SetEraseBackground(grid.DefaultBg())

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

		// Clear stale wrap state from the restored grid
		t.Grid.ResetWrapPending()
	}
}

// processOSC handles OSC sequences (Operating System Command)
func (t *Terminal) processOSC(b byte) {
	if b == 0x07 { // BEL terminates OSC
		t.handleOSC(t.oscParams)
		t.oscParams = ""
		t.state = StateGround
	} else if b == 0x9c { // ST (8-bit)
		t.handleOSC(t.oscParams)
		t.oscParams = ""
		t.state = StateGround
	} else if b == 0x1b { // ESC - might be start of ST
		t.state = StateOSCEscape
	} else {
		t.oscParams += string(b)
	}
}

// processOSCEscape handles bytes after ESC in OSC state
func (t *Terminal) processOSCEscape(b byte) {
	if b == 0x5c { // Backslash completes ST (ESC \)
		t.handleOSC(t.oscParams)
		t.oscParams = ""
		t.state = StateGround
	} else {
		// Not ST, ESC starts new sequence
		t.oscParams = ""
		t.state = StateEscape
		t.processEscape(b)
	}
}

// processDCS handles Device Control String sequences
func (t *Terminal) processDCS(b byte) {
	if b == 0x1b { // ESC - might be start of ST
		t.state = StateDCSEscape
	} else if b == 0x9c { // ST (8-bit)
		t.handleDCS(t.dcsParams)
		t.dcsParams = ""
		t.state = StateGround
	} else if b == 0x07 { // BEL also terminates (non-standard but common)
		t.handleDCS(t.dcsParams)
		t.dcsParams = ""
		t.state = StateGround
	} else {
		t.dcsParams += string(b)
	}
}

// processDCSEscape handles bytes after ESC in DCS state
func (t *Terminal) processDCSEscape(b byte) {
	if b == 0x5c { // Backslash completes ST (ESC \)
		t.handleDCS(t.dcsParams)
		t.dcsParams = ""
		t.state = StateGround
	} else {
		// Not ST, treat as part of DCS
		t.dcsParams += "\x1b" + string(b)
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
	if strings.HasPrefix(params, "+q") {
		caps := strings.TrimPrefix(params, "+q")
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
	case "4": // Query/set color palette
		// We don't support dynamic palette changes
		// Just ignore - no response needed for set operations
	case "7": // Working directory
		path := parseOSC7Path(value)
		if path != "" {
			t.lastWorkingDir = path
		}
	}
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

	if t.mouseSGRMode {
		// SGR format: CSI < button ; x ; y M (press) or m (release)
		suffix := 'M'
		if !pressed {
			suffix = 'm'
		}
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", button, x, y, suffix))
	}

	// X10/Normal format: CSI M Cb Cx Cy (all values + 32)
	// Only reports press, not release (except button 3 which is release)
	if !pressed && button != 3 {
		return nil // X10 doesn't report most releases
	}
	cb := byte(button + 32)
	cx := byte(x + 32)
	cy := byte(y + 32)
	// Clamp to valid range (max 223 for coordinates)
	if cx > 255 {
		cx = 255
	}
	if cy > 255 {
		cy = 255
	}
	return []byte{0x1b, '[', 'M', cb, cx, cy}
}

// parseSGRParams parses CSI parameters for SGR sequences, properly expanding
// colon sub-parameters for extended color sequences (38, 48, 58) per ISO 8613-6.
// Modern apps like Neovim use "38:2:R:G:B" instead of "38;2;R;G;B".
func (t *Terminal) parseSGRParams(s string) []int {
	s = strings.TrimPrefix(s, "?")
	s = strings.TrimPrefix(s, ">")
	s = strings.TrimPrefix(s, "!")
	if s == "" {
		return nil
	}
	var params []int
	parts := strings.Split(s, ";")
	for _, part := range parts {
		if strings.Contains(part, ":") {
			subparts := strings.Split(part, ":")
			first, _ := strconv.Atoi(subparts[0])
			if first == 38 || first == 48 || first == 58 {
				// Expand colon sub-params for extended color sequences
				for _, sp := range subparts {
					n, _ := strconv.Atoi(sp)
					params = append(params, n)
				}
			} else {
				// For other codes (e.g. 4:3 underline style), keep first value only
				params = append(params, first)
			}
		} else {
			n, _ := strconv.Atoi(part)
			params = append(params, n)
		}
	}
	return params
}

// parseParams parses CSI parameters
func (t *Terminal) parseParams(s string) []int {
	// Remove private mode indicator
	s = strings.TrimPrefix(s, "?")
	s = strings.TrimPrefix(s, ">")
	s = strings.TrimPrefix(s, "!")

	if s == "" {
		return nil
	}

	parts := strings.Split(s, ";")
	params := make([]int, len(parts))
	for i, part := range parts {
		// Handle sub-parameters (colon-separated) by taking the first one
		if idx := strings.Index(part, ":"); idx >= 0 {
			part = part[:idx]
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			params[i] = 0
		} else {
			params[i] = n
		}
	}
	return params
}

// getParam gets a parameter with a default value
func (t *Terminal) getParam(params []int, index, defaultVal int) int {
	if index < len(params) && params[index] > 0 {
		return params[index]
	}
	return defaultVal
}

// reset resets the terminal state
func (t *Terminal) reset() {
	t.Grid.ClearAll()
	t.Grid.SetCursorPos(1, 1)
	t.currentFg = grid.DefaultFg()
	t.currentBg = grid.DefaultBg()
	t.currentFlags = 0
	t.Grid.SetEraseBackground(grid.DefaultBg())
	t.appCursorKeys = false
	t.cursorVisible = true
	t.exitAlternateScreen()
	t.charsetG0 = charsetASCII
	t.charsetG1 = charsetASCII
	t.activeCharset = 0
	t.charsetPending = charsetTargetNone
	t.originMode = false
	t.cursorStyle = CursorStyleBlock
}

// Resize resizes the terminal
func (t *Terminal) Resize(cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Grid.Resize(cols, rows)
	if t.savedMainGrid != nil {
		t.savedMainGrid.Resize(cols, rows)
	}
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

func (t *Terminal) handleDSR(params []int) {
	if t.responseWriter == nil {
		return
	}
	code := t.getParam(params, 0, 0)
	switch code {
	case 5: // Status report
		t.responseWriter([]byte("\x1b[0n"))
	case 6: // Cursor position report
		col, row := t.Grid.GetCursor()
		if t.originMode {
			top, _ := t.Grid.GetScrollRegion()
			row = row - (top - 1)
			if row < 0 {
				row = 0
			}
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
	if strings.HasPrefix(t.csiParams, ">") {
		// Secondary DA: report as xterm version 136
		// Format: ESC[>Pp;Pv;Pc c where Pp=terminal type, Pv=version, Pc=ROM cartridge
		t.responseWriter([]byte("\x1b[>0;136;0c"))
	} else {
		// Primary DA: report as VT220 with various features
		// 62 = VT220, 22 = ANSI color, 29 = ANSI text locator
		// This tells applications we support:
		// - VT220 features (62)
		// - 132 columns (1)
		// - Printer port (2)
		// - Sixel graphics (4)
		// - Selective erase (6)
		// - User-defined keys (8)
		// - National replacement charsets (9)
		// - Technical character set (15)
		// - Windowing capability (18)
		// - Horizontal scrolling (21)
		// - ANSI color (22)
		// - Greek (23)
		// - Turkish (24)
		t.responseWriter([]byte("\x1b[?62;22c"))
	}
}

// saveCursor saves current cursor state to appropriate screen's slot
func (t *Terminal) saveCursor() {
	col, row := t.Grid.GetCursor()
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

	t.Grid.SetCursorPos(col+1, row+1)
	t.currentFg = state.fg
	t.currentBg = state.bg
	t.currentFlags = state.flags
}
