package parser

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/sixel"
)

func (t *Terminal) respond(s string) {
	if t.responseWriter != nil {
		t.responseWriter([]byte(s))
	}
}

// handleDA1: VT220 with sixel (4) and ANSI colour (22).
func (t *Terminal) handleDA1() { t.respond("\x1b[?62;4;22c") }

func (t *Terminal) handleDA2() { t.respond("\x1b[>0;136;0c") }

// handleDA3 reports the unit ID in DECRPTUI format.
func (t *Terminal) handleDA3() { t.respond("\x1bP!|00000000\x1b\\") }

func (t *Terminal) handleXTVERSION() { t.respond("\x1bP>|RavenTerminal(" + Version + ")\x1b\\") }

func (t *Terminal) handleDSR(params []int, private bool) {
	if t.responseWriter == nil || len(params) == 0 {
		return
	}
	switch params[0] {
	case 5:
		if !private {
			t.respond("\x1b[0n")
		}
	case 6:
		col, row := t.Grid.GetCursorLocked()
		if t.originMode {
			top, _ := t.region()
			left, _ := t.margins()
			row, col = max(row-top, 0), max(col-left, 0)
		}
		if private {
			t.respond(fmt.Sprintf("\x1b[?%d;%dR", row+1, col+1))
		} else {
			t.respond(fmt.Sprintf("\x1b[%d;%dR", row+1, col+1))
		}
	case 15:
		if private {
			t.respond("\x1b[?13n") // no printer
		}
	case 25:
		if private {
			t.respond("\x1b[?20n") // UDKs unlocked
		}
	case 26:
		if private {
			t.respond("\x1b[?27;1;0;0n") // North American keyboard, ready
		}
	case 996:
		if private {
			t.reportColorScheme()
		}
	}
}

func (t *Terminal) handleXTWINOPS(params []int) {
	if len(params) == 0 {
		return
	}
	g := t.Grid
	cw, ch := t.cellSize()
	switch params[0] {
	case 14:
		t.respond(fmt.Sprintf("\x1b[4;%d;%dt", g.Rows*ch, g.Cols*cw))
	case 16:
		t.respond(fmt.Sprintf("\x1b[6;%d;%dt", ch, cw))
	case 18:
		t.respond(fmt.Sprintf("\x1b[8;%d;%dt", g.Rows, g.Cols))
	case 19:
		t.respond(fmt.Sprintf("\x1b[9;%d;%dt", g.Rows, g.Cols))
	case 20: // icon label: never echo host-controlled text back
		t.respond("\x1b]L\x1b\\")
	case 21: // window title: same
		t.respond("\x1b]l\x1b\\")
	case 22: // push title
		if which := t.getParam(params, 1, 0); which == 0 || which == 2 {
			if len(t.ctl.titleStack) >= maxTitleStack {
				t.ctl.titleStack = t.ctl.titleStack[1:]
			}
			t.ctl.titleStack = append(t.ctl.titleStack, t.windowTitle)
		}
	case 23: // pop title
		if which := t.getParam(params, 1, 0); which == 0 || which == 2 {
			if n := len(t.ctl.titleStack); n > 0 {
				title := t.ctl.titleStack[n-1]
				t.ctl.titleStack = t.ctl.titleStack[:n-1]
				t.setTitle(title)
			}
		}
	}
}

// handleXTSMGRAPHICS answers CSI ? Pi ; Pa ; Pv S for sixel colour registers
// (Pi=1) and geometry (Pi=2); ReGIS (Pi=3) is unsupported.
func (t *Terminal) handleXTSMGRAPHICS(params []int) {
	if len(params) < 2 {
		return
	}
	item, action := params[0], params[1]
	switch item {
	case 1:
		if action >= 1 && action <= 4 {
			t.respond(fmt.Sprintf("\x1b[?1;0;%dS", sixel.NumColorRegisters))
		} else {
			t.respond("\x1b[?1;2;0S")
		}
	case 2:
		cw, ch := t.cellSize()
		w, h := t.Grid.Cols*cw, t.Grid.Rows*ch
		if action == 4 {
			w, h = min(sixel.MaxWidth, w*4), min(sixel.MaxHeight, h*4)
		}
		if action >= 1 && action <= 4 {
			t.respond(fmt.Sprintf("\x1b[?2;0;%d;%dS", w, h))
		} else {
			t.respond("\x1b[?2;2;0S")
		}
	default:
		t.respond(fmt.Sprintf("\x1b[?%d;1;0S", item))
	}
}

// --- DCS ---

// handleDCS dispatches a completed non-sixel DCS string.
func (t *Terminal) handleDCS(s string) {
	if t.responseWriter == nil {
		return
	}
	if caps, ok := strings.CutPrefix(s, "+q"); ok {
		t.handleXTGETTCAP(caps)
	} else if pt, ok := strings.CutPrefix(s, "$q"); ok {
		t.handleDECRQSS(pt)
	}
}

func (t *Terminal) handleDECRQSS(pt string) {
	var v string
	switch pt {
	case "m":
		v = t.sgrString() + "m"
	case "r":
		top, bottom := t.Grid.GetScrollRegionLocked()
		v = fmt.Sprintf("%d;%dr", top, bottom)
	case "s":
		left, right := t.margins()
		v = fmt.Sprintf("%d;%ds", left+1, right+1)
	case " q":
		v = strconv.Itoa(t.ctl.cursorParam) + " q"
	case "\"p":
		v = "62;1\"p"
	case "\"q":
		v = "0\"q"
	default:
		t.respond("\x1bP0$r\x1b\\")
		return
	}
	t.respond("\x1bP1$r" + v + "\x1b\\")
}

func sgrColorString(c grid.Color, base, bright, ext int) string {
	switch c.Type {
	case grid.ColorIndexed:
		switch {
		case c.Index < 8 && base > 0:
			return strconv.Itoa(base + int(c.Index))
		case c.Index < 16 && bright > 0:
			return strconv.Itoa(bright + int(c.Index) - 8)
		}
		return fmt.Sprintf("%d;5;%d", ext, c.Index)
	case grid.ColorRGB:
		return fmt.Sprintf("%d;2;%d;%d;%d", ext, c.R, c.G, c.B)
	}
	return ""
}

func (t *Terminal) sgrString() string {
	parts := []string{"0"}
	f := t.currentFlags
	add := func(flag grid.CellFlags, s string) {
		if f&flag != 0 {
			parts = append(parts, s)
		}
	}
	add(grid.FlagBold, "1")
	add(grid.FlagDim, "2")
	add(grid.FlagItalic, "3")
	if f&grid.FlagUnderline != 0 {
		if t.currentUnderlineStyle > 1 {
			parts = append(parts, "4:"+strconv.Itoa(int(t.currentUnderlineStyle)))
		} else {
			parts = append(parts, "4")
		}
	}
	add(grid.FlagBlink, "5")
	add(grid.FlagInverse, "7")
	add(grid.FlagHidden, "8")
	add(grid.FlagStrikethrough, "9")
	add(grid.FlagOverline, "53")
	for _, s := range []string{
		sgrColorString(t.currentFg, 30, 90, 38),
		sgrColorString(t.currentBg, 40, 100, 48),
		sgrColorString(t.currentUnderlineColor, 0, 0, 58),
	} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ";")
}

// termcaps is the XTGETTCAP table, matching the xterm-raven terminfo entry.
// A nil value marks a boolean capability.
var termcaps = map[string]*string{
	"TN":      new("xterm-raven"),
	"name":    new("xterm-raven"),
	"Co":      new("256"),
	"colors":  new("256"),
	"it":      new("8"),
	"RGB":     nil,
	"Tc":      nil,
	"Su":      nil,
	"am":      nil,
	"bce":     nil,
	"ccc":     nil,
	"km":      nil,
	"mir":     nil,
	"msgr":    nil,
	"xenl":    nil,
	"XF":      nil,
	"fullkbd": nil,
	"XT":      nil,
	"hs":      nil,
	"tsl":     new("\x1b]2;"),
	"fsl":     new("\x07"),
	"dsl":     new("\x1b]2;\x1b\\"),
	"kxIN":    new("\x1b[I"),
	"kxOUT":   new("\x1b[O"),
	"Smulx":   new("\x1b[4:%p1%dm"),
	"Setulc":  new("\x1b[58:2::%p1%{65536}%/%d:%p1%{256}%/%{255}%&%d:%p1%{255}%&%dm"),
	"Sync":    new("\x1b[?2026%?%p1%{1}%-%tl%eh%;"),
	"Ms":      new("\x1b]52;%p1%s;%p2%s\x07"),
	"Ss":      new("\x1b[%p1%d q"),
	"Se":      new("\x1b[2 q"),
	"Cs":      new("\x1b]12;%p1%s\x07"),
	"Cr":      new("\x1b]112\x07"),
	"setrgbf": new("\x1b[38:2:%p1%d:%p2%d:%p3%dm"),
	"setrgbb": new("\x1b[48:2:%p1%d:%p2%d:%p3%dm"),
	"smxx":    new("\x1b[9m"),
	"rmxx":    new("\x1b[29m"),
	"Smol":    new("\x1b[53m"),
	"BE":      new("\x1b[?2004h"),
	"BD":      new("\x1b[?2004l"),
	"PS":      new("\x1b[200~"),
	"PE":      new("\x1b[201~"),
	"fe":      new("\x1b[?1004h"),
	"fd":      new("\x1b[?1004l"),
	"smcup":   new("\x1b[?1049h\x1b[22;0;0t"),
	"rmcup":   new("\x1b[?1049l\x1b[23;0;0t"),
	"smkx":    new("\x1b[?1h\x1b="),
	"rmkx":    new("\x1b[?1l\x1b>"),
	"sitm":    new("\x1b[3m"),
	"ritm":    new("\x1b[23m"),
	"kmous":   new("\x1b[<"),
	"kcuu1":   new("\x1bOA"),
	"kcud1":   new("\x1bOB"),
	"kcuf1":   new("\x1bOC"),
	"kcub1":   new("\x1bOD"),
	"khome":   new("\x1bOH"),
	"kend":    new("\x1bOF"),
	"kbs":     new("\x7f"),
	"kcbt":    new("\x1b[Z"),
	"kent":    new("\x1bOM"),
	"kich1":   new("\x1b[2~"),
	"kdch1":   new("\x1b[3~"),
	"kpp":     new("\x1b[5~"),
	"knp":     new("\x1b[6~"),
	"kDC":     new("\x1b[3;2~"),
	"kEND":    new("\x1b[1;2F"),
	"kHOM":    new("\x1b[1;2H"),
	"kLFT":    new("\x1b[1;2D"),
	"kRIT":    new("\x1b[1;2C"),
	"kri":     new("\x1b[1;2A"),
	"kind":    new("\x1b[1;2B"),
	"kf1":     new("\x1bOP"),
	"kf2":     new("\x1bOQ"),
	"kf3":     new("\x1bOR"),
	"kf4":     new("\x1bOS"),
	"kf5":     new("\x1b[15~"),
	"kf6":     new("\x1b[17~"),
	"kf7":     new("\x1b[18~"),
	"kf8":     new("\x1b[19~"),
	"kf9":     new("\x1b[20~"),
	"kf10":    new("\x1b[21~"),
	"kf11":    new("\x1b[23~"),
	"kf12":    new("\x1b[24~"),
}

//go:fix inline
func sp(s string) *string { return new(s) }

func (t *Terminal) termcap(name string) (string, bool, bool) {
	switch name {
	case "cols":
		return strconv.Itoa(t.Grid.Cols), true, true
	case "lines":
		return strconv.Itoa(t.Grid.Rows), true, true
	}
	v, ok := termcaps[name]
	if !ok {
		return "", false, false
	}
	if v == nil {
		return "", false, true
	}
	return *v, true, true
}

// handleXTGETTCAP answers DCS + q Pt ST. Names are hex-encoded and separated
// by ';'. Known caps are answered together in one DCS 1 + r reply; the first
// unknown or malformed name ends the request with DCS 0 + r name ST.
func (t *Terminal) handleXTGETTCAP(req string) {
	var ok []string
	for raw := range strings.SplitSeq(req, ";") {
		nameB, err := hex.DecodeString(raw)
		name := string(nameB)
		var val string
		var hasVal, known bool
		if err == nil {
			val, hasVal, known = t.termcap(name)
		}
		if !known {
			if len(ok) > 0 {
				t.respond("\x1bP1+r" + strings.Join(ok, ";") + "\x1b\\")
			}
			if err != nil || len(raw) > 256 {
				raw = "" // never echo a malformed name into the PTY
			}
			t.respond("\x1bP0+r" + strings.ToUpper(raw) + "\x1b\\")
			return
		}
		entry := strings.ToUpper(hex.EncodeToString(nameB))
		if hasVal {
			entry += "=" + strings.ToUpper(hex.EncodeToString([]byte(val)))
		}
		ok = append(ok, entry)
	}
	if len(ok) > 0 {
		t.respond("\x1bP1+r" + strings.Join(ok, ";") + "\x1b\\")
	}
}
