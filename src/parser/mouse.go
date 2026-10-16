package parser

import (
	"fmt"
	"unicode/utf8"
)

// Mouse reporting decision logic. DecideMouse is a pure function mapping a GUI
// mouse event plus the terminal's reporting state to what the frontend should
// do: forward encoded bytes to the PTY, handle the event locally (selection /
// URL hover / scrollback), or swallow it. The GLFW callbacks in main.go are
// thin translators around it.

// MouseEventKind classifies the GUI event being decided.
type MouseEventKind int

const (
	MousePress MouseEventKind = iota
	MouseRelease
	MouseMotion
	MouseWheelUp
	MouseWheelDown
	MouseWheelLeft
	MouseWheelRight
)

// MouseContext is a snapshot of the terminal state relevant to mouse routing.
type MouseContext struct {
	Mode          int  // 0=off, 9=X10, 1000=normal, 1002=button-event, 1003=any-event
	SGR           bool // ?1006 extended coordinates (implied by Encoding 1006/1016)
	Encoding      int  // 0 legacy, 1005 UTF-8, 1006 SGR, 1015 urxvt, 1016 SGR-pixels
	Shift         bool // shift held: universal "bypass app, act locally" convention
	Alt, Ctrl     bool // modifier bits reported to the app (8 / 16)
	AltScreen     bool // alternate screen active
	AppCursorKeys bool // DECCKM: SS3 vs CSI arrow encoding
	// NoAlternateScroll disables wheel-to-arrow translation (?1007 reset).
	NoAlternateScroll bool
	// PixelX/PixelY are the 0-based pointer position in pixels within the
	// pane (valid when Pixels is set), used by SGR-pixels (?1016). Without
	// them the cell's top-left pixel is derived from CellW/CellH.
	Pixels         bool
	PixelX, PixelY int
	CellW, CellH   int
}

// MouseActionKind is what the frontend should do with the event.
type MouseActionKind int

const (
	MouseActionLocal  MouseActionKind = iota // selection / URL / scrollback as today
	MouseActionSend                          // write Bytes to the pane's PTY
	MouseActionIgnore                        // swallow (e.g. unmoved motion)
)

// MouseAction is DecideMouse's verdict.
type MouseAction struct {
	Kind  MouseActionKind
	Bytes []byte
}

func (ctx MouseContext) encoding() int {
	if ctx.Encoding != 0 {
		return ctx.Encoding
	}
	if ctx.SGR {
		return 1006
	}
	return 0
}

func (ctx MouseContext) send(button, col, row int, pressed bool) MouseAction {
	enc := ctx.encoding()
	if ctx.Mode != 9 {
		if ctx.Alt {
			button |= 8
		}
		if ctx.Ctrl {
			button |= 16
		}
	}
	x, y := col+1, row+1
	if enc == 1016 {
		if ctx.Pixels {
			x, y = ctx.PixelX+1, ctx.PixelY+1
		} else {
			cw, ch := ctx.CellW, ctx.CellH
			if cw <= 0 || ch <= 0 {
				cw, ch = defaultCellW, defaultCellH
			}
			x, y = col*cw+1, row*ch+1
		}
	}
	return MouseAction{Kind: MouseActionSend, Bytes: encodeMouse(enc, button, x, y, pressed)}
}

// DecideMouse routes a mouse event. button and heldButton use terminal codes
// (0=left, 1=middle, 2=right); heldButton is -1 when no button is down. col and
// row are 0-based cells (encoded 1-based). cellChanged reports whether the cell
// under the cursor differs from the last reported one (motion throttle).
func DecideMouse(ctx MouseContext, kind MouseEventKind, button, col, row, heldButton int, cellChanged bool) MouseAction {
	if ctx.Shift {
		return MouseAction{Kind: MouseActionLocal}
	}
	sgr := ctx.encoding() == 1006 || ctx.encoding() == 1016

	switch kind {
	case MousePress:
		if ctx.Mode == 0 {
			return MouseAction{Kind: MouseActionLocal}
		}
		return ctx.send(button, col, row, true)

	case MouseRelease:
		if ctx.Mode == 0 {
			return MouseAction{Kind: MouseActionLocal}
		}
		if ctx.Mode == 9 {
			return MouseAction{Kind: MouseActionIgnore} // X10 reports presses only
		}
		if !sgr {
			button = 3 // legacy encodings report release as button 3
		}
		return ctx.send(button, col, row, false)

	case MouseMotion:
		switch ctx.Mode {
		case 1003:
			if !cellChanged && ctx.encoding() != 1016 {
				return MouseAction{Kind: MouseActionIgnore}
			}
			b := heldButton
			if b < 0 {
				b = 3 // no button
			}
			return ctx.send(b+32, col, row, true)
		case 1002:
			if heldButton < 0 {
				return MouseAction{Kind: MouseActionLocal} // hover: not reported
			}
			if !cellChanged && ctx.encoding() != 1016 {
				return MouseAction{Kind: MouseActionIgnore}
			}
			return ctx.send(heldButton+32, col, row, true)
		default:
			// Mode 0, 9 or 1000: motion is never reported.
			return MouseAction{Kind: MouseActionLocal}
		}

	case MouseWheelUp, MouseWheelDown, MouseWheelLeft, MouseWheelRight:
		btn := 64 + int(kind-MouseWheelUp)
		if ctx.Mode > 0 {
			return ctx.send(btn, col, row, true)
		}
		if ctx.AltScreen && !ctx.NoAlternateScroll && (kind == MouseWheelUp || kind == MouseWheelDown) {
			// No mouse mode but a full-screen app: translate the wheel to 3x
			// arrow keys so less/vim scroll (?1007).
			arrow := byte('A')
			if kind == MouseWheelDown {
				arrow = 'B'
			}
			prefix := []byte{0x1b, '['}
			if ctx.AppCursorKeys {
				prefix = []byte{0x1b, 'O'}
			}
			seq := append(append([]byte{}, prefix...), arrow)
			var out []byte
			for range 3 {
				out = append(out, seq...)
			}
			return MouseAction{Kind: MouseActionSend, Bytes: out}
		}
		return MouseAction{Kind: MouseActionLocal}
	}
	return MouseAction{Kind: MouseActionLocal}
}

// encodeMouse encodes a report; x, y are 1-based (pixels for 1016).
func encodeMouse(enc, button, x, y int, pressed bool) []byte {
	switch enc {
	case 1006, 1016:
		suffix := 'M'
		if !pressed {
			suffix = 'm'
		}
		return fmt.Appendf(nil, "\x1b[<%d;%d;%d%c", button, x, y, suffix)
	case 1015:
		return fmt.Appendf(nil, "\x1b[%d;%d;%dM", button+32, x, y)
	case 1005:
		out := []byte{0x1b, '[', 'M'}
		for _, v := range []int{button, min(x, 2015), min(y, 2015)} {
			out = utf8.AppendRune(out, rune(v+32))
		}
		return out
	}
	// Legacy: CSI M Cb Cx Cy (all values + 32). Coordinates clamp at 223
	// (byte 255) like xterm so they never wrap into control bytes.
	if !pressed && button&3 != 3 {
		return nil
	}
	return []byte{0x1b, '[', 'M', byte(button + 32), byte(min(x, 223) + 32), byte(min(y, 223) + 32)}
}

// MouseState returns, under one lock, the terminal state a frontend needs to
// route a mouse event: tracking mode, SGR flag, alt-screen, and DECCKM.
func (t *Terminal) MouseState() (mode int, sgr, altScreen, appCursorKeys bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mouseMode, t.mouseSGR(), t.alternateScreen, t.appCursorKeys
}

// MouseCtx returns the full mouse-routing state; callers fill in Shift, Alt,
// Ctrl and, when known, Pixels/PixelX/PixelY.
func (t *Terminal) MouseCtx() MouseContext {
	t.mu.Lock()
	defer t.mu.Unlock()
	ctx := MouseContext{
		Mode:              t.mouseMode,
		SGR:               t.mouseSGR(),
		Encoding:          t.ctl.mouseExt,
		AltScreen:         t.alternateScreen,
		AppCursorKeys:     t.appCursorKeys,
		NoAlternateScroll: t.ctl.altScrollOff,
	}
	ctx.CellW, ctx.CellH = t.cellSize()
	return ctx
}
