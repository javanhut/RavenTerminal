package parser

import (
	"bytes"
	"testing"
)

func TestDecideMouse(t *testing.T) {
	sgr1000 := MouseContext{Mode: 1000, SGR: true}
	sgr1002 := MouseContext{Mode: 1002, SGR: true}
	sgr1003 := MouseContext{Mode: 1003, SGR: true}
	x10_1000 := MouseContext{Mode: 1000, SGR: false}

	cases := []struct {
		name        string
		ctx         MouseContext
		kind        MouseEventKind
		button      int
		col, row    int
		held        int
		cellChanged bool
		wantKind    MouseActionKind
		wantBytes   []byte
	}{
		{name: "shift forces local even with mode on",
			ctx: MouseContext{Mode: 1003, SGR: true, Shift: true}, kind: MousePress, button: 0, col: 4, row: 2,
			wantKind: MouseActionLocal},
		{name: "press with no mode is local",
			ctx: MouseContext{}, kind: MousePress, button: 0, col: 4, row: 2,
			wantKind: MouseActionLocal},
		{name: "SGR left press",
			ctx: sgr1000, kind: MousePress, button: 0, col: 4, row: 2,
			wantKind: MouseActionSend, wantBytes: []byte("\x1b[<0;5;3M")},
		{name: "SGR right press",
			ctx: sgr1000, kind: MousePress, button: 2, col: 0, row: 0,
			wantKind: MouseActionSend, wantBytes: []byte("\x1b[<2;1;1M")},
		{name: "SGR left release",
			ctx: sgr1000, kind: MouseRelease, button: 0, col: 4, row: 2,
			wantKind: MouseActionSend, wantBytes: []byte("\x1b[<0;5;3m")},
		{name: "X10 left press",
			ctx: x10_1000, kind: MousePress, button: 0, col: 4, row: 2,
			wantKind: MouseActionSend, wantBytes: []byte{0x1b, '[', 'M', 32, 37, 35}},
		{name: "X10 release encodes button 3",
			ctx: x10_1000, kind: MouseRelease, button: 0, col: 4, row: 2,
			wantKind: MouseActionSend, wantBytes: []byte{0x1b, '[', 'M', 35, 37, 35}},
		{name: "motion in 1000 is local (hover ok, nothing reported)",
			ctx: sgr1000, kind: MouseMotion, held: 0, cellChanged: true,
			wantKind: MouseActionLocal},
		{name: "1002 drag motion reported with +32",
			ctx: sgr1002, kind: MouseMotion, col: 4, row: 2, held: 0, cellChanged: true,
			wantKind: MouseActionSend, wantBytes: []byte("\x1b[<32;5;3M")},
		{name: "1002 motion same cell throttled",
			ctx: sgr1002, kind: MouseMotion, col: 4, row: 2, held: 0, cellChanged: false,
			wantKind: MouseActionIgnore},
		{name: "1002 motion with no button is local",
			ctx: sgr1002, kind: MouseMotion, col: 4, row: 2, held: -1, cellChanged: true,
			wantKind: MouseActionLocal},
		{name: "1003 any-motion reported as button 3 + 32",
			ctx: sgr1003, kind: MouseMotion, col: 4, row: 2, held: -1, cellChanged: true,
			wantKind: MouseActionSend, wantBytes: []byte("\x1b[<35;5;3M")},
		{name: "1003 motion same cell throttled",
			ctx: sgr1003, kind: MouseMotion, col: 4, row: 2, held: -1, cellChanged: false,
			wantKind: MouseActionIgnore},
		{name: "wheel up with mode is button 64",
			ctx: sgr1000, kind: MouseWheelUp, col: 4, row: 2,
			wantKind: MouseActionSend, wantBytes: []byte("\x1b[<64;5;3M")},
		{name: "wheel down with mode is button 65",
			ctx: sgr1000, kind: MouseWheelDown, col: 4, row: 2,
			wantKind: MouseActionSend, wantBytes: []byte("\x1b[<65;5;3M")},
		{name: "X10 wheel up",
			ctx: x10_1000, kind: MouseWheelUp, col: 4, row: 2,
			wantKind: MouseActionSend, wantBytes: []byte{0x1b, '[', 'M', 96, 37, 35}},
		{name: "X10 press at coordinate limit (col 222 -> x 223 -> byte 255)",
			ctx: x10_1000, kind: MousePress, button: 0, col: 222, row: 2,
			wantKind: MouseActionSend, wantBytes: []byte{0x1b, '[', 'M', 32, 255, 35}},
		{name: "X10 press one past limit clamps to 223",
			ctx: x10_1000, kind: MousePress, button: 0, col: 223, row: 2,
			wantKind: MouseActionSend, wantBytes: []byte{0x1b, '[', 'M', 32, 255, 35}},
		{name: "X10 press far past limit clamps both coordinates",
			ctx: x10_1000, kind: MousePress, button: 0, col: 299, row: 400,
			wantKind: MouseActionSend, wantBytes: []byte{0x1b, '[', 'M', 32, 255, 255}},
		{name: "wheel no mode on alt screen sends CSI arrows x3",
			ctx: MouseContext{AltScreen: true}, kind: MouseWheelUp,
			wantKind: MouseActionSend, wantBytes: []byte("\x1b[A\x1b[A\x1b[A")},
		{name: "wheel no mode on alt screen app-cursor sends SS3 arrows x3",
			ctx: MouseContext{AltScreen: true, AppCursorKeys: true}, kind: MouseWheelDown,
			wantKind: MouseActionSend, wantBytes: []byte("\x1bOB\x1bOB\x1bOB")},
		{name: "wheel no mode main screen is local scrollback",
			ctx: MouseContext{}, kind: MouseWheelUp,
			wantKind: MouseActionLocal},
		{name: "shift wheel with mode is local scrollback",
			ctx: MouseContext{Mode: 1000, SGR: true, Shift: true}, kind: MouseWheelUp,
			wantKind: MouseActionLocal},
	}

	for _, c := range cases {
		got := DecideMouse(c.ctx, c.kind, c.button, c.col, c.row, c.held, c.cellChanged)
		if got.Kind != c.wantKind {
			t.Errorf("%s: kind = %v, want %v", c.name, got.Kind, c.wantKind)
			continue
		}
		if !bytes.Equal(got.Bytes, c.wantBytes) {
			t.Errorf("%s: bytes = %q, want %q", c.name, got.Bytes, c.wantBytes)
		}
	}
}

func TestMouseStateAccessor(t *testing.T) {
	term := NewTerminal(20, 5)
	mode, sgr, alt, appCur := term.MouseState()
	if mode != 0 || sgr || alt || appCur {
		t.Fatalf("default MouseState = (%d,%v,%v,%v), want (0,false,false,false)", mode, sgr, alt, appCur)
	}
	term.Process([]byte("\x1b[?1002h\x1b[?1006h\x1b[?1049h\x1b[?1h"))
	mode, sgr, alt, appCur = term.MouseState()
	if mode != 1002 || !sgr || !alt || !appCur {
		t.Fatalf("MouseState = (%d,%v,%v,%v), want (1002,true,true,true)", mode, sgr, alt, appCur)
	}
}
