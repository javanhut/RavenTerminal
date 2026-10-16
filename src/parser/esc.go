package parser

import (
	"bytes"

	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/images"
	"github.com/javanhut/RavenTerminal/src/sixel"
)

const maxSixelLen = 32 << 20

// processEscape handles the byte after ESC.
func (t *Terminal) processEscape(b byte) {
	t.state = StateGround
	switch {
	case b >= 0x20 && b <= 0x2f:
		t.ctl.escBuf = append(t.ctl.escBuf[:0], b)
		t.state = StateEscapeIntermediate
		return
	case b == 0x1b:
		t.state = StateEscape
		return
	case b == 0x18 || b == 0x1a:
		return
	case b < 0x20:
		t.state = StateEscape
		t.processGround(b)
		return
	}
	switch b {
	case '[':
		t.state = StateCSI
		t.csiBuf = t.csiBuf[:0]
	case ']':
		t.state = StateOSC
		t.oscParams = t.oscParams[:0]
	case 'P':
		t.state = StateDCS
		t.dcsParams = t.dcsParams[:0]
		t.ctl.dcsHdrDone, t.ctl.dcsSixel = false, false
	case '_':
		t.state = StateAPC
		t.apcBuf = t.apcBuf[:0]
	case '^', 'X': // PM, SOS: consumed and ignored
		t.state = StateIgnoreString
	case '7':
		t.saveCursor()
	case '8':
		t.restoreCursor()
	case 'c':
		t.reset()
	case 'D':
		t.index()
	case 'E':
		t.carriageReturn()
		t.index()
	case 'M':
		t.reverseIndex()
	case 'H':
		t.setTabStop()
	case '=':
		t.ctl.appKeypad = true
	case '>':
		t.ctl.appKeypad = false
	case 'N':
		t.ctl.ss = 2
	case 'O':
		t.ctl.ss = 3
	case 'n':
		t.ctl.gl = 2
	case 'o':
		t.ctl.gl = 3
	case 'Z': // DECID
		t.handleDA1()
	}
}

// processEscapeIntermediate collects ESC intermediates up to the final byte.
func (t *Terminal) processEscapeIntermediate(b byte) {
	switch {
	case b >= 0x20 && b <= 0x2f:
		if len(t.ctl.escBuf) < 4 {
			t.ctl.escBuf = append(t.ctl.escBuf, b)
		}
	case b >= 0x30 && b <= 0x7e:
		t.state = StateGround
		t.dispatchEscIntermediate(t.ctl.escBuf, b)
	case b == 0x1b:
		t.state = StateEscape
	case b == 0x18 || b == 0x1a:
		t.state = StateGround
	case b < 0x20:
		t.processGround(b)
	default:
		t.state = StateGround
	}
}

func (t *Terminal) dispatchEscIntermediate(inter []byte, final byte) {
	switch inter[0] {
	case '(', ')', '*', '+': // 94-character sets into G0..G3
		cs := charsetASCII
		if len(inter) == 1 {
			switch final {
			case '0':
				cs = charsetLineDrawing
			case 'A':
				cs = charsetUK
			}
		}
		t.ctl.g[inter[0]-'('] = cs
	case '-', '.', '/': // 96-character sets into G1..G3
		t.ctl.g[inter[0]-','] = charsetASCII
	case '#':
		if len(inter) != 1 {
			return
		}
		switch final {
		case '3':
			t.Grid.SetLineSizeLocked(grid.LineDoubleTop)
		case '4':
			t.Grid.SetLineSizeLocked(grid.LineDoubleBottom)
		case '5':
			t.Grid.SetLineSizeLocked(grid.LineSingle)
		case '6':
			t.Grid.SetLineSizeLocked(grid.LineDoubleWidth)
		}
		if final == '8' { // DECALN
			t.Grid.FillScreenLocked('E')
			t.Grid.RestoreScrollRegionLocked(1, t.Grid.Rows)
			t.resetLRMargins()
			t.originMode = false
			t.Grid.SetCursorLocked(0, 0)
		}
	}
}

// mapCharsetRune applies the active (or single-shifted) character set.
func (t *Terminal) mapCharsetRune(r rune) rune {
	cs := t.ctl.g[t.ctl.gl]
	if t.ctl.ss != 0 {
		cs = t.ctl.g[t.ctl.ss]
		t.ctl.ss = 0
	}
	switch cs {
	case charsetLineDrawing:
		if mapped, ok := decLineDrawing[r]; ok {
			return mapped
		}
	case charsetUK:
		if r == '#' {
			return '£'
		}
	}
	return r
}

// plainPrint reports whether printable ASCII can take the batch fast path.
func (t *Terminal) plainPrint() bool {
	return !t.insertMode && t.ctl.ss == 0 && t.ctl.g[t.ctl.gl] == charsetASCII && !t.lrActive()
}

// processIgnoreString swallows SOS/PM payloads until ST; CAN/SUB abort.
func (t *Terminal) processIgnoreString(b byte) {
	switch b {
	case 0x1b:
		t.state = StateIgnoreStringEscape
	case 0x18, 0x1a:
		t.state = StateGround
	}
}

func (t *Terminal) processIgnoreStringEscape(b byte) {
	if b == '\\' {
		t.state = StateGround
		return
	}
	t.state = StateEscape
	t.processEscape(b)
}

// --- DCS ---

func (t *Terminal) dcsLimit() int {
	if t.ctl.dcsSixel {
		return maxSixelLen
	}
	return maxDCSLen
}

// processDCS accumulates a Device Control String. Sixel data (final 'q' with
// no intermediates) gets a larger cap than query strings.
func (t *Terminal) processDCS(b byte) {
	switch b {
	case 0x1b:
		t.state = StateDCSEscape
		return
	case 0x07: // BEL is non-standard but common
		t.finishDCS()
		return
	case 0x18, 0x1a:
		t.dcsParams = t.dcsParams[:0]
		t.state = StateGround
		return
	}
	if !t.ctl.dcsHdrDone && b >= 0x40 && b <= 0x7e {
		t.ctl.dcsHdrDone = true
		hdrOK := true
		for _, c := range t.dcsParams {
			if c < '0' || c > ';' {
				hdrOK = false
			}
		}
		t.ctl.dcsSixel = b == 'q' && hdrOK
	}
	if len(t.dcsParams) < t.dcsLimit() {
		t.dcsParams = append(t.dcsParams, b)
	}
}

// processDCSEscape handles bytes after ESC in DCS state.
func (t *Terminal) processDCSEscape(b byte) {
	if b == '\\' {
		t.finishDCS()
		return
	}
	// Any other ESC aborts the string and starts a new sequence.
	t.dcsParams = t.dcsParams[:0]
	t.state = StateEscape
	t.processEscape(b)
}

// finishDCS dispatches a completed DCS (unless it overflowed) and returns to
// ground state.
func (t *Terminal) finishDCS() {
	t.state = StateGround
	if len(t.dcsParams) < t.dcsLimit() {
		if t.ctl.dcsSixel {
			t.handleSixel(t.dcsParams)
		} else {
			t.handleDCS(string(t.dcsParams))
		}
	}
	if cap(t.dcsParams) > maxDCSLen {
		t.dcsParams = nil
	} else {
		t.dcsParams = t.dcsParams[:0]
	}
}

// handleSixel decodes a sixel image and places it at the cursor. In sixel
// scrolling mode (DECSDM reset, the default) the cursor ends on the line below
// the image, scrolling as needed; with ?8452 it ends right of the image. With
// DECSDM set the image is drawn at the top-left and the cursor stays put.
func (t *Terminal) handleSixel(buf []byte) {
	before, after, ok := bytes.Cut(buf, []byte{'q'})
	if !ok {
		return
	}
	var params []int
	for p := range bytes.SplitSeq(before, []byte{';'}) {
		n, _ := atoiBytes(p)
		params = append(params, n)
	}
	img, err := sixel.Decode(params, after)
	if err != nil || img.Width == 0 || img.Height == 0 {
		return
	}
	cw, ch := t.cellSize()
	cols, rows := (img.Width+cw-1)/cw, (img.Height+ch-1)/ch
	store := t.activeImages()
	stored := store.AddInternal(img.Width, img.Height, img.Pixels)
	col, row := t.Grid.GetCursorLocked()
	if t.ctl.sixelDisplay {
		store.Place(&images.Placement{ImageID: stored.ID, AnchorAbsRow: t.Grid.AbsoluteCursorRowLocked() - row, Cols: cols, Rows: rows})
		return
	}
	store.Place(&images.Placement{ImageID: stored.ID, AnchorAbsRow: t.Grid.AbsoluteCursorRowLocked(), AnchorCol: col, Cols: cols, Rows: rows})
	if t.ctl.sixelCurRight {
		for range rows - 1 {
			t.index()
		}
		_, r := t.Grid.GetCursorLocked()
		t.Grid.SetCursorLocked(col+cols, r)
		return
	}
	for range rows {
		t.index()
	}
	_, r := t.Grid.GetCursorLocked()
	t.Grid.SetCursorLocked(col, r)
}
