package parser

import (
	"time"

	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/images"
)

// csiSplit splits the accumulated CSI bytes into private marker, parameter
// bytes and a single intermediate. ok is false for malformed sequences
// (markers inside the parameters, parameters after an intermediate, or more
// than one intermediate), which are ignored.
func (t *Terminal) csiSplit() (marker byte, params []byte, inter byte, ok bool) {
	s := t.csiBuf
	if len(s) > 0 && s[0] >= '<' && s[0] <= '?' {
		marker = s[0]
		s = s[1:]
	}
	i := 0
	for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3f {
		if s[i] >= '<' {
			return 0, nil, 0, false
		}
		i++
	}
	switch len(s) - i {
	case 0:
	case 1:
		inter = s[i]
		if inter < 0x20 || inter > 0x2f {
			return 0, nil, 0, false
		}
	default:
		return 0, nil, 0, false
	}
	return marker, s[:i], inter, true
}

// csiHasPrefix reports whether the accumulated CSI bytes start with c.
func (t *Terminal) csiHasPrefix(c byte) bool {
	return len(t.csiBuf) > 0 && t.csiBuf[0] == c
}

// parseParams parses ';'-separated parameters (colon sub-parameters keep only
// the first value), reusing t.paramScratch.
func (t *Terminal) parseParams(s []byte) []int {
	if len(s) == 0 {
		return nil
	}
	params := t.paramScratch[:0]
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ';' {
			part := s[start:i]
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

// getParam gets a parameter with a default value (0 means default).
func (t *Terminal) getParam(params []int, index, defaultVal int) int {
	if index < len(params) && params[index] > 0 {
		return params[index]
	}
	return defaultVal
}

func (t *Terminal) executeCSI(final byte) {
	marker, ps, inter, ok := t.csiSplit()
	if !ok {
		return
	}
	if final == 'm' && marker == 0 && inter == 0 {
		t.executeSGR(ps)
		return
	}
	params := t.parseParams(ps)
	switch marker {
	case 0:
		if inter == 0 {
			t.csiPlain(final, params)
		} else {
			t.csiIntermediate(inter, final, params)
		}
	case '?':
		switch {
		case inter == 0:
			t.csiPrivate(final, params)
		case inter == '$' && final == 'p':
			t.handleDECRQM(params, true)
		}
	case '>':
		if inter == 0 {
			t.csiGreater(final, params)
		}
	case '<':
		if inter == 0 && final == 'u' {
			t.kittyPop(t.getParam(params, 0, 1))
		}
	case '=':
		if inter == 0 {
			switch final {
			case 'c':
				t.handleDA3()
			case 'u':
				t.kittySet(uint8(t.getParam(params, 0, 0)), t.getParam(params, 1, 1))
			}
		}
	}
}

func (t *Terminal) csiPlain(final byte, params []int) {
	g := t.Grid
	n := t.getParam(params, 0, 1)
	switch final {
	case 'A': // CUU
		t.moveCursor(0, -n)
	case 'B', 'e': // CUD, VPR
		t.moveCursor(0, n)
	case 'C', 'a': // CUF, HPR
		t.moveCursor(n, 0)
	case 'D': // CUB
		t.moveCursor(-n, 0)
	case 'E': // CNL
		t.moveCursor(0, n)
		t.carriageReturn()
	case 'F': // CPL
		t.moveCursor(0, -n)
		t.carriageReturn()
	case 'G', '`': // CHA, HPA
		t.setCol(n - 1)
	case 'd': // VPA
		t.setRow(n - 1)
	case 'H', 'f': // CUP, HVP
		t.setCursorPos(t.getParam(params, 1, 1), n)
	case 'I': // CHT
		t.tab(n)
	case 'Z': // CBT
		t.backTab(n)
	case 'J': // ED
		g.ResetWrapPendingLocked()
		switch mode := t.getParam(params, 0, 0); mode {
		case 0:
			g.ClearToEndWithBgLocked(t.currentBg)
		case 1:
			g.ClearToStartWithBgLocked(t.currentBg)
		case 2, 3:
			t.dropScreenImages()
			if mode == 2 {
				g.ClearAllWithBgLocked(t.currentBg)
			} else { // erases saved lines (scrollback) only
				g.ClearScrollbackLocked()
			}
		}
	case 'K': // EL
		g.ResetWrapPendingLocked()
		switch t.getParam(params, 0, 0) {
		case 0:
			g.ClearLineToEndWithBgLocked(t.currentBg)
		case 1:
			g.ClearLineToStartWithBgLocked(t.currentBg)
		case 2:
			g.ClearLineWithBgLocked(t.currentBg)
		}
	case 'L', 'M': // IL, DL
		t.insertDeleteLines(n, final == 'L')
	case '@', 'P': // ICH, DCH
		g.ResetWrapPendingLocked()
		col, row := g.GetCursorLocked()
		if t.lrActive() {
			if col >= t.ctl.left && col <= t.ctl.right {
				if final == '@' {
					g.ScrollRectRightLocked(row, row, col, t.ctl.right, n, g.EraseBgLocked())
				} else {
					g.ScrollRectLeftLocked(row, row, col, t.ctl.right, n, g.EraseBgLocked())
				}
			}
			return
		}
		if final == '@' {
			g.InsertCharsLocked(n)
		} else {
			g.DeleteCharsLocked(n)
		}
	case 'S': // SU
		t.scrollUp(n)
	case 'T': // SD; the 5-parameter form is mouse highlight tracking
		if len(params) <= 1 {
			t.scrollDown(n)
		}
	case 'X': // ECH
		g.ResetWrapPendingLocked()
		g.EraseCharsLocked(n)
	case 'b': // REP
		g.RepeatCharLocked(n)
	case 'c': // DA1
		if t.getParam(params, 0, 0) == 0 {
			t.handleDA1()
		}
	case 'g': // TBC
		t.clearTabStop(t.getParam(params, 0, 0))
	case 'h', 'l': // SM, RM
		for _, p := range params {
			t.setAnsiMode(p, final == 'h')
		}
	case 'n':
		t.handleDSR(params, false)
	case 'r': // DECSTBM
		top := t.getParam(params, 0, 1)
		bottom := min(t.getParam(params, 1, g.Rows), g.Rows)
		if top < bottom {
			g.RestoreScrollRegionLocked(top, bottom)
			t.home()
		}
	case 's':
		if t.ctl.lrmm { // DECSLRM
			left := t.getParam(params, 0, 1) - 1
			right := min(t.getParam(params, 1, g.Cols), g.Cols) - 1
			if left < right {
				t.ctl.left, t.ctl.right = left, right
				t.ctl.lrSet = left > 0 || right < g.Cols-1
				t.home()
			}
		} else if len(params) == 0 { // SCOSC
			t.saveCursor()
		}
	case 'u': // SCORC
		if len(params) == 0 {
			t.restoreCursor()
		}
	case 't':
		t.handleXTWINOPS(params)
	}
}

func (t *Terminal) insertDeleteLines(n int, insert bool) {
	g := t.Grid
	col, row := g.GetCursorLocked()
	top, bottom := t.region()
	if row < top || row > bottom {
		return
	}
	left, right := t.margins()
	if t.lrActive() {
		if col < left || col > right {
			return
		}
		if insert {
			g.ScrollRectDownLocked(row, bottom, left, right, n, t.currentBg)
		} else {
			g.ScrollRectUpLocked(row, bottom, left, right, n, t.currentBg)
		}
	} else if insert {
		g.InsertLinesWithBgLocked(n, t.currentBg)
	} else {
		g.DeleteLinesWithBgLocked(n, t.currentBg)
	}
	g.SetCursorLocked(left, row)
}

// dropScreenImages deletes the real placements overlapping the visible
// screen: images are part of what ED 2/3 erase.
func (t *Terminal) dropScreenImages() {
	_, row := t.Grid.GetCursorLocked()
	top, rows := t.Grid.AbsoluteCursorRowLocked()-row, t.Grid.Rows
	t.activeImages().DeletePlacements(func(p *images.Placement) bool {
		r0 := p.AnchorAbsRow - top
		return !p.Virtual && r0 < rows && r0+p.Rows > 0
	}, false)
	t.Grid.MarkScreenDirtyLocked()
}

func (t *Terminal) scrollUp(n int) {
	if t.lrActive() {
		top, bottom := t.region()
		t.Grid.ScrollRectUpLocked(top, bottom, t.ctl.left, t.ctl.right, n, t.currentBg)
		return
	}
	t.Grid.ScrollUpWithBgLocked(n, t.currentBg)
}

func (t *Terminal) scrollDown(n int) {
	if t.lrActive() {
		top, bottom := t.region()
		t.Grid.ScrollRectDownLocked(top, bottom, t.ctl.left, t.ctl.right, n, t.currentBg)
		return
	}
	t.Grid.ScrollDownWithBgLocked(n, t.currentBg)
}

func (t *Terminal) csiIntermediate(inter, final byte, params []int) {
	n := t.getParam(params, 0, 1)
	switch {
	case inter == ' ' && final == 'q': // DECSCUSR
		t.setCursorStyle(t.getParam(params, 0, 0))
	case inter == ' ' && (final == '@' || final == 'A'): // SL, SR
		top, bottom := t.region()
		left, right := t.margins()
		if final == '@' {
			t.Grid.ScrollRectLeftLocked(top, bottom, left, right, n, t.Grid.EraseBgLocked())
		} else {
			t.Grid.ScrollRectRightLocked(top, bottom, left, right, n, t.Grid.EraseBgLocked())
		}
	case inter == '!' && final == 'p': // DECSTR
		t.softReset()
	case inter == '$' && final == 'p': // DECRQM (ANSI)
		t.handleDECRQM(params, false)
	case inter == '#' && final == 'P': // XTPUSHCOLORS
		t.pushColors(t.getParam(params, 0, 0))
	case inter == '#' && final == 'Q': // XTPOPCOLORS
		t.popColors(t.getParam(params, 0, 0))
	case inter == '#' && final == 'R': // XTREPORTCOLORS
		t.reportColors()
	}
}

func (t *Terminal) csiPrivate(final byte, params []int) {
	switch final {
	case 'h', 'l':
		for _, p := range params {
			t.setPrivateMode(p, final == 'h')
		}
	case 'J': // DECSED (no protected cells: same as ED)
		t.csiPlain('J', params)
	case 'K': // DECSEL
		t.csiPlain('K', params)
	case 'n':
		t.handleDSR(params, true)
	case 's': // XTSAVE
		if t.ctl.xtsave == nil {
			t.ctl.xtsave = map[int]bool{}
		}
		for _, p := range params {
			if st := t.decModeState(p); st == 1 || st == 2 {
				t.ctl.xtsave[p] = st == 1
			}
		}
	case 'r': // XTRESTORE
		for _, p := range params {
			if v, ok := t.ctl.xtsave[p]; ok {
				t.setPrivateMode(p, v)
			}
		}
	case 'S': // XTSMGRAPHICS
		t.handleXTSMGRAPHICS(params)
	case 'u':
		t.kittyQuery()
	case 'm': // XTQMODKEYS
		if t.getParam(params, 0, 0) == 4 && t.responseWriter != nil {
			t.responseWriter([]byte("\x1b[>4;" + itoa10(t.ctl.modifyOtherKeys) + "m"))
		}
	}
}

func (t *Terminal) csiGreater(final byte, params []int) {
	switch final {
	case 'c':
		t.handleDA2()
	case 'q':
		t.handleXTVERSION()
	case 'u':
		t.kittyPush(uint8(t.getParam(params, 0, 0)))
	case 'm': // XTMODKEYS
		if len(params) > 0 && params[0] == 4 {
			if len(params) > 1 {
				t.ctl.modifyOtherKeys = min(max(params[1], 0), 2)
			} else {
				t.ctl.modifyOtherKeys = 0
			}
		}
	case 'n': // XTMODKEYS disable
		if len(params) > 0 && params[0] == 4 {
			t.ctl.modifyOtherKeys = 0
		}
	}
}

func itoa10(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (t *Terminal) setCursorStyle(p int) {
	if p > 6 {
		return
	}
	t.ctl.cursorParam = p
	// Odd/default values blink; even values are steady.
	t.cursorBlinking = p == 0 || p%2 == 1
	switch p {
	case 0:
		t.cursorStyle = t.defaultCursorStyle
	case 1, 2:
		t.cursorStyle = CursorStyleBlock
	case 3, 4:
		t.cursorStyle = CursorStyleUnderline
	case 5, 6:
		t.cursorStyle = CursorStyleBar
	}
}

// --- SGR ---

// executeSGR parses and applies an SGR parameter string. Colon sub-parameters
// belong to their parameter (38:2::r:g:b, 4:3); semicolon forms of 38/48/58
// consume the following parameters.
func (t *Terminal) executeSGR(s []byte) {
	vals := t.paramScratch[:0]
	lens := t.subScratch[:0]
	if len(s) > 0 {
		start, n := 0, 0
		for i := 0; i <= len(s); i++ {
			if i == len(s) || s[i] == ';' || s[i] == ':' {
				v, _ := atoiBytes(s[start:i])
				vals = append(vals, v)
				n++
				if i == len(s) || s[i] == ';' {
					lens = append(lens, n)
					n = 0
				}
				start = i + 1
			}
		}
	} else {
		vals = append(vals, 0)
		lens = append(lens, 1)
	}
	t.paramScratch, t.subScratch = vals, lens

	prevBg := t.currentBg
	off := 0
	for i := 0; i < len(lens); i++ {
		code, n := vals[off], lens[i]
		sub := vals[off+1 : off+n]
		off += n
		switch code {
		case 38, 48, 58:
			var c grid.Color
			var ok bool
			if n > 1 {
				c, ok = sgrColor(sub, true)
			} else {
				// Semicolon form: gather the following plain parameters.
				var tail []int
				j, o := i+1, off
				for j < len(lens) && lens[j] == 1 && len(tail) < 4 {
					tail = append(tail, vals[o])
					o++
					j++
				}
				var used int
				c, ok, used = sgrColorSemi(tail)
				for k := 0; k < used; k++ {
					off += lens[i+1]
					i++
				}
			}
			if ok {
				switch code {
				case 38:
					t.currentFg = c
				case 48:
					t.currentBg = c
				default:
					t.currentUnderlineColor = c
				}
			}
		case 4:
			style := 1
			if n > 1 {
				style = sub[0]
			}
			switch {
			case style == 0:
				t.currentFlags &^= grid.FlagUnderline
				t.currentUnderlineStyle = 0
			case style <= 5:
				t.currentFlags |= grid.FlagUnderline
				t.currentUnderlineStyle = uint8(style)
			}
		default:
			t.applySGR(code)
		}
	}
	if t.currentBg != prevBg {
		t.Grid.SetEraseBackgroundLocked(t.currentBg)
	}
}

func sgrColor(sub []int, colon bool) (grid.Color, bool) {
	if len(sub) == 0 {
		return grid.Color{}, false
	}
	switch sub[0] {
	case 5:
		if len(sub) >= 2 && sub[1] >= 0 && sub[1] <= 255 {
			return grid.IndexedColor(uint8(sub[1])), true
		}
	case 2:
		rgb := sub[1:]
		if colon && len(rgb) >= 4 {
			rgb = rgb[1:] // colour-space id slot
		}
		if len(rgb) >= 3 {
			return grid.RGBColor(uint8(rgb[0]), uint8(rgb[1]), uint8(rgb[2])), true
		}
	}
	return grid.Color{}, false
}

// sgrColorSemi decodes "5;n" / "2;r;g;b" and reports how many params it used.
func sgrColorSemi(tail []int) (grid.Color, bool, int) {
	if len(tail) == 0 {
		return grid.Color{}, false, 0
	}
	switch tail[0] {
	case 5:
		if len(tail) >= 2 {
			c, ok := sgrColor(tail[:2], false)
			return c, ok, 2
		}
		return grid.Color{}, false, len(tail)
	case 2:
		if len(tail) >= 4 {
			c, ok := sgrColor(tail[:4], false)
			return c, ok, 4
		}
		return grid.Color{}, false, len(tail)
	}
	return grid.Color{}, false, 1
}

func (t *Terminal) applySGR(p int) {
	switch {
	case p == 0:
		t.currentFg = grid.DefaultFg()
		t.currentBg = grid.DefaultBg()
		t.currentFlags = 0
		t.currentUnderlineStyle = 0
		t.currentUnderlineColor = grid.Color{}
	case p == 1:
		t.currentFlags |= grid.FlagBold
	case p == 2:
		t.currentFlags |= grid.FlagDim
	case p == 3:
		t.currentFlags |= grid.FlagItalic
	case p == 5 || p == 6:
		t.currentFlags |= grid.FlagBlink
	case p == 7:
		t.currentFlags |= grid.FlagInverse
	case p == 8:
		t.currentFlags |= grid.FlagHidden
	case p == 9:
		t.currentFlags |= grid.FlagStrikethrough
	case p == 21:
		t.currentFlags |= grid.FlagUnderline
		t.currentUnderlineStyle = 2
	case p == 22:
		t.currentFlags &^= grid.FlagBold | grid.FlagDim
	case p == 23:
		t.currentFlags &^= grid.FlagItalic
	case p == 24:
		t.currentFlags &^= grid.FlagUnderline
		t.currentUnderlineStyle = 0
	case p == 25:
		t.currentFlags &^= grid.FlagBlink
	case p == 27:
		t.currentFlags &^= grid.FlagInverse
	case p == 28:
		t.currentFlags &^= grid.FlagHidden
	case p == 29:
		t.currentFlags &^= grid.FlagStrikethrough
	case p >= 30 && p <= 37:
		t.currentFg = grid.IndexedColor(uint8(p - 30))
	case p == 39:
		t.currentFg = grid.DefaultFg()
	case p >= 40 && p <= 47:
		t.currentBg = grid.IndexedColor(uint8(p - 40))
	case p == 49:
		t.currentBg = grid.DefaultBg()
	case p == 53:
		t.currentFlags |= grid.FlagOverline
	case p == 55:
		t.currentFlags &^= grid.FlagOverline
	case p == 59:
		t.currentUnderlineColor = grid.Color{}
	case p >= 90 && p <= 97:
		t.currentFg = grid.IndexedColor(uint8(p - 90 + 8))
	case p >= 100 && p <= 107:
		t.currentBg = grid.IndexedColor(uint8(p - 100 + 8))
	}
}

// --- modes ---

func (t *Terminal) setAnsiMode(p int, set bool) {
	switch p {
	case 4: // IRM
		t.insertMode = set
	case 20: // LNM
		t.lnmMode = set
	}
}

func (t *Terminal) setMouseMode(mode int, set bool) {
	if set {
		t.mouseMode = mode
	} else if t.mouseMode == mode {
		t.mouseMode = 0
	}
}

func (t *Terminal) setPrivateMode(p int, set bool) {
	switch p {
	case 1: // DECCKM
		t.appCursorKeys = set
	case 5: // DECSCNM
		t.ctl.reverseVideo = set
	case 6: // DECOM
		t.originMode = set
		t.home()
	case 7: // DECAWM
		t.Grid.SetAutoWrapLocked(set)
	case 9, 1000, 1002, 1003:
		t.setMouseMode(p, set)
	case 12: // cursor blink
		t.cursorBlinking = set
	case 25: // DECTCEM
		t.cursorVisible = set
	case 47, 1047:
		if set {
			t.enterAlternateScreen()
		} else {
			t.exitAlternateScreen()
		}
	case 66: // DECNKM
		t.ctl.appKeypad = set
	case 69: // DECLRMM
		t.ctl.lrmm = set
		if !set {
			t.resetLRMargins()
		} else {
			// VT510: every line in page memory goes single width, so margin
			// operations never meet a double-size line. Both screens are page
			// memory.
			t.Grid.FlattenLineSizesLocked()
			if t.savedMainGrid != nil {
				t.savedMainGrid.FlattenLineSizes()
			}
		}
	case 80: // DECSDM
		t.ctl.sixelDisplay = set
	case 1004:
		t.focusReporting = set
	case 1005, 1006, 1015, 1016:
		if set {
			t.ctl.mouseExt = p
		} else if t.ctl.mouseExt == p {
			t.ctl.mouseExt = 0
		}
	case 1007:
		t.ctl.altScrollOff = !set
	case 1036:
		t.ctl.metaNoEsc = !set
	case 1039:
		t.ctl.altNoEsc = !set
	case 1048:
		t.ctl.mode1048 = set
		if set {
			t.saveCursor()
		} else {
			t.restoreCursor()
		}
	case 1049:
		if set {
			t.saveCursor()
			t.enterAlternateScreen()
		} else {
			t.exitAlternateScreen()
			t.restoreCursor()
		}
	case 2004:
		t.bracketedPaste = set
	case 2026:
		t.syncActive = set
		if set {
			// Watchdog: if the app never closes the sync, rendering resumes.
			t.syncDeadline = time.Now().Add(100 * time.Millisecond)
		}
	case 2027:
		t.ctl.graphemeOff = !set
		t.syncGraphemeMode()
	case 2031:
		t.ctl.colorReports = set
	case 2048:
		t.ctl.inBandResize = set
		if set {
			t.reportInBandSize()
		}
	case 8452:
		t.ctl.sixelCurRight = set
	}
}

// decModeState returns the DECRPM value for a DEC private mode:
// 0 not recognised, 1 set, 2 reset.
func (t *Terminal) decModeState(p int) int {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 2
	}
	switch p {
	case 1:
		return b(t.appCursorKeys)
	case 5:
		return b(t.ctl.reverseVideo)
	case 6:
		return b(t.originMode)
	case 7:
		return b(t.Grid.GetAutoWrapLocked())
	case 9, 1000, 1002, 1003:
		return b(t.mouseMode == p)
	case 12:
		return b(t.cursorBlinking)
	case 25:
		return b(t.cursorVisible)
	case 47, 1047, 1049:
		return b(t.alternateScreen)
	case 66:
		return b(t.ctl.appKeypad)
	case 69:
		return b(t.ctl.lrmm)
	case 80:
		return b(t.ctl.sixelDisplay)
	case 1004:
		return b(t.focusReporting)
	case 1005, 1006, 1015, 1016:
		return b(t.ctl.mouseExt == p)
	case 1007:
		return b(!t.ctl.altScrollOff)
	case 1036:
		return b(!t.ctl.metaNoEsc)
	case 1039:
		return b(!t.ctl.altNoEsc)
	case 1048:
		return b(t.ctl.mode1048)
	case 2004:
		return b(t.bracketedPaste)
	case 2026:
		return b(t.syncActive)
	case 2027:
		return b(!t.ctl.graphemeOff)
	case 2031:
		return b(t.ctl.colorReports)
	case 2048:
		return b(t.ctl.inBandResize)
	case 8452:
		return b(t.ctl.sixelCurRight)
	}
	return 0
}

func (t *Terminal) ansiModeState(p int) int {
	switch p {
	case 4:
		if t.insertMode {
			return 1
		}
		return 2
	case 20:
		if t.lnmMode {
			return 1
		}
		return 2
	case 2: // KAM: keyboard never locked
		return 4
	case 12: // SRM: never local echo
		return 3
	}
	return 0
}

// handleDECRQM answers CSI [?] Ps $ p with CSI [?] Ps ; Pm $ y.
func (t *Terminal) handleDECRQM(params []int, private bool) {
	if t.responseWriter == nil {
		return
	}
	mode := t.getParam(params, 0, 0)
	if private {
		t.responseWriter([]byte("\x1b[?" + itoa10(mode) + ";" + itoa10(t.decModeState(mode)) + "$y"))
		return
	}
	t.responseWriter([]byte("\x1b[" + itoa10(mode) + ";" + itoa10(t.ansiModeState(mode)) + "$y"))
}
