package parser

import "fmt"

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
)

// MouseContext is a snapshot of the terminal state relevant to mouse routing.
type MouseContext struct {
	Mode          int  // 0=off, 1000=normal, 1002=button-event, 1003=any-event
	SGR           bool // ?1006 extended coordinates
	Shift         bool // shift held: universal "bypass app, act locally" convention
	AltScreen     bool // alternate screen active
	AppCursorKeys bool // DECCKM: SS3 vs CSI arrow encoding
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

// DecideMouse routes a mouse event. button and heldButton use terminal codes
// (0=left, 1=middle, 2=right); heldButton is -1 when no button is down. col and
// row are 0-based cells (encoded 1-based). cellChanged reports whether the cell
// under the cursor differs from the last reported one (motion throttle).
// ponytail: modifier bits (ctrl=16/alt=8) are not encoded into button codes;
// add them here if an app is found that needs them.
func DecideMouse(ctx MouseContext, kind MouseEventKind, button, col, row, heldButton int, cellChanged bool) MouseAction {
	if ctx.Shift {
		return MouseAction{Kind: MouseActionLocal}
	}
	x, y := col+1, row+1

	switch kind {
	case MousePress:
		if ctx.Mode == 0 {
			return MouseAction{Kind: MouseActionLocal}
		}
		return MouseAction{Kind: MouseActionSend, Bytes: encodeMouseBytes(ctx.SGR, button, x, y, true)}

	case MouseRelease:
		if ctx.Mode == 0 {
			return MouseAction{Kind: MouseActionLocal}
		}
		if !ctx.SGR {
			button = 3 // X10/normal encoding reports release as button 3
		}
		return MouseAction{Kind: MouseActionSend, Bytes: encodeMouseBytes(ctx.SGR, button, x, y, false)}

	case MouseMotion:
		switch ctx.Mode {
		case 1003:
			if !cellChanged {
				return MouseAction{Kind: MouseActionIgnore}
			}
			b := heldButton
			if b < 0 {
				b = 3 // no button
			}
			return MouseAction{Kind: MouseActionSend, Bytes: encodeMouseBytes(ctx.SGR, b+32, x, y, true)}
		case 1002:
			if heldButton < 0 {
				return MouseAction{Kind: MouseActionLocal} // hover: not reported
			}
			if !cellChanged {
				return MouseAction{Kind: MouseActionIgnore}
			}
			return MouseAction{Kind: MouseActionSend, Bytes: encodeMouseBytes(ctx.SGR, heldButton+32, x, y, true)}
		default:
			// Mode 0 or 1000: motion is never reported; local hover is harmless
			// (a reported press means no local drag is in progress).
			return MouseAction{Kind: MouseActionLocal}
		}

	case MouseWheelUp, MouseWheelDown:
		btn := 64
		arrow := byte('A')
		if kind == MouseWheelDown {
			btn = 65
			arrow = 'B'
		}
		if ctx.Mode > 0 {
			return MouseAction{Kind: MouseActionSend, Bytes: encodeMouseBytes(ctx.SGR, btn, x, y, true)}
		}
		if ctx.AltScreen {
			// No mouse mode but a full-screen app: translate the wheel to 3x
			// arrow keys so less/vim scroll.
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

// encodeMouseBytes is the pure encoding core shared with EncodeMouseEvent.
// x, y are 1-based.
func encodeMouseBytes(sgr bool, button, x, y int, pressed bool) []byte {
	if sgr {
		// SGR format: CSI < button ; x ; y M (press) or m (release)
		suffix := 'M'
		if !pressed {
			suffix = 'm'
		}
		return fmt.Appendf(nil, "\x1b[<%d;%d;%d%c", button, x, y, suffix)
	}

	// X10/Normal format: CSI M Cb Cx Cy (all values + 32)
	// Only reports press, not release (except button 3 which is release)
	if !pressed && button != 3 {
		return nil // X10 doesn't report most releases
	}
	// Clamp coordinates to 223 (encoded byte 255) like xterm: byte(x+32)
	// would otherwise wrap modulo 256 and emit control bytes mid-sequence.
	if x > 223 {
		x = 223
	}
	if y > 223 {
		y = 223
	}
	return []byte{0x1b, '[', 'M', byte(button + 32), byte(x + 32), byte(y + 32)}
}

// MouseState returns, under one lock, the terminal state a frontend needs to
// route a mouse event: tracking mode, SGR flag, alt-screen, and DECCKM.
func (t *Terminal) MouseState() (mode int, sgr, altScreen, appCursorKeys bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mouseMode, t.mouseSGRMode, t.alternateScreen, t.appCursorKeys
}
