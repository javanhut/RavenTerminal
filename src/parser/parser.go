package parser

import (
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/images"
	"math"
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
	StateEscapeIntermediate
	StateIgnoreString       // SOS / PM payload
	StateIgnoreStringEscape // ESC within SOS / PM
)

// Charset represents a character set designation (G0..G3).
type Charset int

const (
	charsetASCII Charset = iota
	charsetLineDrawing
	charsetUK
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
	// Per-screen scroll region state
	savedMainScrollTop    int
	savedMainScrollBottom int
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
	cursorStyle CursorStyle
	// defaultCursorStyle is the host's configured style, used for DECSCUSR 0
	// and restored by DECSTR/RIS.
	defaultCursorStyle CursorStyle
	cursorBlinking     bool
	// Bracketed paste mode (?2004)
	bracketedPaste bool
	// Window title (OSC 0/2) and icon name (OSC 0/1)
	windowTitle string
	iconName    string
	// Mouse tracking modes
	mouseMode int // 0=off, 9=X10, 1000=normal, 1002=button, 1003=any
	// Saved terminal modes for alternate screen restore
	savedMainAppCursorKeys  bool
	savedMainBracketedPaste bool
	savedMainMouseMode      int
	savedMainMouseExt       int
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
	// OSC-owned state (osc.go): events, colours, links, marks, notifications.
	osc oscState
	// Control-sequence state (ctl.go): charsets, modes, margins, tab stops.
	ctl ctlState
}

// NewTerminal creates a new terminal parser
func NewTerminal(cols, rows int) *Terminal {
	t := &Terminal{
		Grid:                  grid.NewGrid(cols, rows),
		state:                 StateGround,
		currentFg:             grid.DefaultFg(),
		currentBg:             grid.DefaultBg(),
		currentFlags:          0,
		cursorVisible:         true,
		savedMainScrollTop:    1,
		savedMainScrollBottom: rows,
		cursorStyle:           CursorStyleBlock,
		cursorBlinking:        true, // default cursor blinks (DECSCUSR 0)
		images:                images.NewStore(256 << 20),
		altImages:             images.NewStore(256 << 20),
	}
	t.ctl.dark = true
	t.ctl.init(cols)
	return t
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
			t.plainPrint() {
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
	case StateEscapeIntermediate:
		t.processEscapeIntermediate(b)
	case StateIgnoreString:
		t.processIgnoreString(b)
	case StateIgnoreStringEscape:
		t.processIgnoreStringEscape(b)
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
			// Truncated sequence: replacement character, then this byte.
			t.utf8Len = 0
			t.utf8Remaining = 0
			t.printRune(0xFFFD)
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
		t.pushEvent(Event{Kind: EventBell})
	case 0x08: // BS
		t.backspace()
	case 0x09: // HT
		t.tab(1)
	case 0x0e: // SO (LS1)
		t.ctl.gl = 1
	case 0x0f: // SI (LS0)
		t.ctl.gl = 0
	case 0x0a, 0x0b, 0x0c: // LF, VT, FF
		if t.lnmMode { // LNM: LF also performs CR
			t.carriageReturn()
		}
		t.index()
	case 0x0d: // CR
		t.carriageReturn()
	default:
		if b >= 0x20 && b < 0x7f {
			// ASCII printable character
			r := t.mapCharsetRune(rune(b))
			t.printRune(r)
		} else if b >= 0xC2 && b < 0xE0 {
			// Start of 2-byte UTF-8 sequence
			t.utf8Buf[0] = b
			t.utf8Len = 1
			t.utf8Remaining = 1
		} else if b >= 0xE0 && b < 0xF0 {
			// Start of 3-byte UTF-8 sequence
			t.utf8Buf[0] = b
			t.utf8Len = 1
			t.utf8Remaining = 2
		} else if b >= 0xF0 && b < 0xF5 {
			// Start of 4-byte UTF-8 sequence
			t.utf8Buf[0] = b
			t.utf8Len = 1
			t.utf8Remaining = 3
		} else if b >= 0x80 {
			// Stray continuation or invalid lead byte.
			t.printRune(0xFFFD)
		}
	}
}

// printRune writes a printable rune at the cursor, honoring insert mode (IRM):
// when insert mode is active, existing cells shift right to make room.
func (t *Terminal) printRune(r rune) {
	if t.lrActive() {
		t.printInMargins(r)
		return
	}
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
		t.savedMainMouseExt = t.ctl.mouseExt
		// Swap in the alt screen's own kitty-keyboard flag stack.
		t.kittyStack, t.altKittyStack = t.altKittyStack, t.kittyStack
		t.swapPointerStacks(true)

		t.savedMainGrid = t.Grid
		// The alternate screen has no scrollback (standard VT behavior).
		t.Grid = grid.NewAltGrid(t.Grid.Cols, t.Grid.Rows)
		t.Grid.SetLinkTableLocked(t.linkTable())
		t.alternateScreen = true
		t.relockBatchGrid()
		t.Grid.SetGraphemeClusteringLocked(!t.ctl.graphemeOff)
		// The snapshot buffer belongs to the old grid; force a fresh full copy.
		t.snapPrev = nil

		// Clear the alternate screen and its image layer (standard behavior)
		t.Grid.ClearAllLocked()
		t.altImages.DeleteAll()
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
		t.swapPointerStacks(false)
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
		t.ctl.g = [4]Charset{}
		t.ctl.gl, t.ctl.ss = 0, 0

		// Reset terminal modes
		t.originMode = false
		t.cursorVisible = true

		// Restore saved terminal modes from main screen
		t.appCursorKeys = t.savedMainAppCursorKeys
		t.bracketedPaste = t.savedMainBracketedPaste
		t.mouseMode = t.savedMainMouseMode
		t.ctl.mouseExt = t.savedMainMouseExt
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
		t.osc.bel = true
		t.finishOSC()
	} else if b == 0x1b { // ESC - might be start of ST
		t.state = StateOSCEscape
	} else if b == 0x18 || b == 0x1a { // CAN/SUB abort the string
		t.oscParams = t.oscParams[:0]
		t.state = StateGround
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
		t.osc.bel = false
		t.finishOSC()
	} else {
		// Not ST, ESC starts new sequence
		t.oscParams = t.oscParams[:0]
		t.state = StateEscape
		t.processEscape(b)
	}
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
	return t.mouseSGR()
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
	return encodeMouse(t.ctl.mouseExt, button, x, y, pressed)
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

// SetDefaultCursorStyle sets the configured cursor style. It takes effect
// at once unless the application has chosen a style with DECSCUSR.
func (t *Terminal) SetDefaultCursorStyle(s CursorStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.defaultCursorStyle = s
	if t.ctl.cursorParam == 0 {
		t.cursorStyle = s
	}
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
	s := t.Grid.Snapshot(t.snapPrev)
	s.CursorVisible = t.cursorVisible
	t.snapPrev = s
	return s
}
