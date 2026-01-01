package parser

import (
	"fmt"
	"github.com/javanhut/RavenTerminal/grid"
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
	StateCharset
	StateHash
)

// Terminal handles ANSI escape sequence parsing and state
type Terminal struct {
	Grid            *grid.Grid
	state           ParserState
	csiParams       string
	oscParams       string
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
}

// NewTerminal creates a new terminal parser
func NewTerminal(cols, rows int) *Terminal {
	return &Terminal{
		Grid:          grid.NewGrid(cols, rows),
		state:         StateGround,
		currentFg:     grid.DefaultFg(),
		currentBg:     grid.DefaultBg(),
		currentFlags:  0,
		cursorVisible: true,
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
	case StateCharset:
		// Character set designation - consume the designator byte and ignore
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
				r := decodeUTF8(t.utf8Buf)
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
	case 0x07: // BEL
		// Bell - ignore
	case 0x08: // BS
		t.Grid.Backspace()
	case 0x09: // HT (Tab)
		t.Grid.Tab()
	case 0x0a, 0x0b, 0x0c: // LF, VT, FF
		t.Grid.Newline()
		t.Grid.ResetScrollOffset()
	case 0x0d: // CR
		t.Grid.CarriageReturn()
	default:
		if b >= 0x20 && b < 0x7f {
			// ASCII printable character
			t.Grid.WriteChar(rune(b), t.currentFg, t.currentBg, t.currentFlags)
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

// processEscape handles bytes in escape state
func (t *Terminal) processEscape(b byte) {
	switch b {
	case '[': // CSI
		t.state = StateCSI
		t.csiParams = ""
	case ']': // OSC
		t.state = StateOSC
		t.oscParams = ""
	case '7': // DECSC - Save cursor
		t.Grid.SaveCursor()
		t.state = StateGround
	case '8': // DECRC - Restore cursor
		t.Grid.RestoreCursor()
		t.state = StateGround
	case 'c': // RIS - Reset
		t.reset()
		t.state = StateGround
	case 'D': // IND - Index (down)
		t.Grid.MoveCursor(0, 1)
		t.state = StateGround
	case 'M': // RI - Reverse index (up)
		col, row := t.Grid.GetCursor()
		if row == 0 {
			t.Grid.ScrollDown(1)
		} else {
			t.Grid.MoveCursor(0, -1)
		}
		_ = col
		t.state = StateGround
	case 'E': // NEL - Next line
		t.Grid.CarriageReturn()
		t.Grid.Newline()
		t.state = StateGround
	case '(', ')', '*', '+': // Character set designation - need to consume next byte
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
		t.state = StateGround
	} else {
		t.state = StateGround
	}
}

// executeCSI executes a CSI sequence
func (t *Terminal) executeCSI(final byte) {
	params := t.parseParams(t.csiParams)

	switch final {
	case 'A': // CUU - Cursor up
		n := t.getParam(params, 0, 1)
		t.Grid.MoveCursor(0, -n)
	case 'B': // CUD - Cursor down
		n := t.getParam(params, 0, 1)
		t.Grid.MoveCursor(0, n)
	case 'C': // CUF - Cursor forward
		n := t.getParam(params, 0, 1)
		t.Grid.MoveCursor(n, 0)
	case 'D': // CUB - Cursor back
		n := t.getParam(params, 0, 1)
		t.Grid.MoveCursor(-n, 0)
	case 'E': // CNL - Cursor next line
		n := t.getParam(params, 0, 1)
		t.Grid.CarriageReturn()
		t.Grid.MoveCursor(0, n)
	case 'F': // CPL - Cursor previous line
		n := t.getParam(params, 0, 1)
		t.Grid.CarriageReturn()
		t.Grid.MoveCursor(0, -n)
	case 'G': // CHA - Cursor horizontal absolute
		n := t.getParam(params, 0, 1)
		_, row := t.Grid.GetCursor()
		t.Grid.SetCursorPos(n, row+1)
	case 'H', 'f': // CUP - Cursor position
		row := t.getParam(params, 0, 1)
		col := t.getParam(params, 1, 1)
		t.Grid.SetCursorPos(col, row)
	case 'J': // ED - Erase in display
		n := t.getParam(params, 0, 0)
		switch n {
		case 0:
			t.Grid.ClearToEnd()
		case 1:
			t.Grid.ClearToStart()
		case 2, 3:
			t.Grid.ClearAll()
		}
	case 'K': // EL - Erase in line
		n := t.getParam(params, 0, 0)
		switch n {
		case 0:
			t.Grid.ClearLineToEnd()
		case 1:
			t.Grid.ClearLineToStart()
		case 2:
			t.Grid.ClearLine()
		}
	case 'L': // IL - Insert lines
		n := t.getParam(params, 0, 1)
		t.Grid.InsertLines(n)
	case 'M': // DL - Delete lines
		n := t.getParam(params, 0, 1)
		t.Grid.DeleteLines(n)
	case 'P': // DCH - Delete characters
		n := t.getParam(params, 0, 1)
		t.Grid.DeleteChars(n)
	case '@': // ICH - Insert characters
		n := t.getParam(params, 0, 1)
		t.Grid.InsertChars(n)
	case 'S': // SU - Scroll up
		n := t.getParam(params, 0, 1)
		t.Grid.ScrollUp(n)
	case 'T': // SD - Scroll down
		n := t.getParam(params, 0, 1)
		t.Grid.ScrollDown(n)
	case 'X': // ECH - Erase character (erase n chars at cursor without moving)
		n := t.getParam(params, 0, 1)
		t.Grid.EraseChars(n)
	case 'd': // VPA - Vertical position absolute
		n := t.getParam(params, 0, 1)
		col, _ := t.Grid.GetCursor()
		t.Grid.SetCursorPos(col+1, n)
	case 'b': // REP - Repeat preceding character
		n := t.getParam(params, 0, 1)
		t.Grid.RepeatChar(n)
	case 'm': // SGR - Select graphic rendition
		t.executeSGR(params)
	case 'h': // SM - Set mode
		t.setMode(params, true)
	case 'l': // RM - Reset mode
		t.setMode(params, false)
	case 'r': // DECSTBM - Set scrolling region
		top := t.getParam(params, 0, 1)
		bottom := t.getParam(params, 1, t.Grid.Rows)
		t.Grid.SetScrollRegion(top, bottom)
	case 's': // SCP - Save cursor position
		t.Grid.SaveCursor()
	case 'u': // RCP - Restore cursor position
		t.Grid.RestoreCursor()
	case 'n': // DSR - Device status report (ignore for now)
		t.handleDSR(params)
	case 'c': // DA - Device attributes (ignore for now)
	case 't': // Window manipulation (ignore)
	case 'q': // DECSCUSR - Set cursor style (ignore for now)
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
		case p == 22: // Normal intensity
			t.currentFlags &^= grid.FlagBold
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
			case 25: // DECTCEM - Text cursor enable
				t.cursorVisible = set
			case 47, 1047: // Alternate screen buffer
				if set {
					t.enterAlternateScreen()
				} else {
					t.exitAlternateScreen()
				}
			case 1049: // Alternate screen buffer with save/restore cursor
				if set {
					t.Grid.SaveCursor()
					t.enterAlternateScreen()
				} else {
					t.exitAlternateScreen()
					t.Grid.RestoreCursor()
				}
			}
		}
	}
}

// enterAlternateScreen switches to alternate screen buffer
func (t *Terminal) enterAlternateScreen() {
	if !t.alternateScreen {
		t.savedMainGrid = t.Grid
		t.Grid = grid.NewGrid(t.Grid.Cols, t.Grid.Rows)
		t.alternateScreen = true
	}
}

// exitAlternateScreen returns to main screen buffer
func (t *Terminal) exitAlternateScreen() {
	if t.alternateScreen && t.savedMainGrid != nil {
		t.Grid = t.savedMainGrid
		t.savedMainGrid = nil
		t.alternateScreen = false
	}
}

// processOSC handles OSC sequences (Operating System Command)
func (t *Terminal) processOSC(b byte) {
	if b == 0x07 || b == 0x1b { // BEL or ESC terminates OSC
		t.handleOSC(t.oscParams)
		t.oscParams = ""
		t.state = StateGround
	} else {
		t.oscParams += string(b)
	}
}

func (t *Terminal) handleOSC(params string) {
	if strings.HasPrefix(params, "7;") {
		path := parseOSC7Path(strings.TrimPrefix(params, "7;"))
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
	t.appCursorKeys = false
	t.cursorVisible = true
	t.exitAlternateScreen()
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
		response := fmt.Sprintf("\x1b[%d;%dR", row+1, col+1)
		t.responseWriter([]byte(response))
	}
}
