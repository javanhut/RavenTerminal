package parser

import "testing"

func TestMouseEncodings(t *testing.T) {
	cases := []struct {
		name string
		ctx  MouseContext
		kind MouseEventKind
		btn  int
		want string
	}{
		{"sgr pixels press", MouseContext{Mode: 1000, Encoding: 1016, Pixels: true, PixelX: 123, PixelY: 45}, MousePress, 0, "\x1b[<0;124;46M"},
		{"sgr pixels release", MouseContext{Mode: 1000, Encoding: 1016, Pixels: true, PixelX: 9, PixelY: 9}, MouseRelease, 2, "\x1b[<2;10;10m"},
		{"urxvt", MouseContext{Mode: 1000, Encoding: 1015}, MousePress, 1, "\x1b[33;5;3M"},
		{"urxvt release", MouseContext{Mode: 1000, Encoding: 1015}, MouseRelease, 1, "\x1b[35;5;3M"},
		{"utf8 large", MouseContext{Mode: 1000, Encoding: 1005}, MousePress, 0, "\x1b[M " + string(rune(5+32)) + string(rune(3+32))},
		{"modifiers", MouseContext{Mode: 1000, SGR: true, Ctrl: true, Alt: true}, MousePress, 0, "\x1b[<24;5;3M"},
		{"wheel left", MouseContext{Mode: 1000, SGR: true}, MouseWheelLeft, 0, "\x1b[<66;5;3M"},
		{"wheel right", MouseContext{Mode: 1000, SGR: true}, MouseWheelRight, 0, "\x1b[<67;5;3M"},
		{"x10 press", MouseContext{Mode: 9, Ctrl: true}, MousePress, 0, "\x1b[M %#"},
	}
	for _, tc := range cases {
		got := DecideMouse(tc.ctx, tc.kind, tc.btn, 4, 2, -1, true)
		if string(got.Bytes) != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got.Bytes, tc.want)
		}
	}
	// Without exact pixels, SGR-pixels reports the cell's top-left pixel.
	if a := DecideMouse(MouseContext{Mode: 1000, Encoding: 1016, CellW: 10, CellH: 20}, MousePress, 0, 4, 2, -1, true); string(a.Bytes) != "\x1b[<0;41;41M" {
		t.Errorf("1016 cell fallback = %q", a.Bytes)
	}
	// X10 never reports releases or motion.
	if a := DecideMouse(MouseContext{Mode: 9}, MouseRelease, 0, 1, 1, -1, true); a.Kind == MouseActionSend {
		t.Error("x10 release sent")
	}
	// UTF-8 encoding handles coordinates beyond 223.
	a := DecideMouse(MouseContext{Mode: 1000, Encoding: 1005}, MousePress, 0, 299, 0, -1, true)
	if want := "\x1b[M " + string(rune(300+32)) + "!"; string(a.Bytes) != want {
		t.Errorf("utf8 wide = %q want %q", a.Bytes, want)
	}
	// Alternate scroll off: wheel on alt screen is local.
	if a := DecideMouse(MouseContext{AltScreen: true, NoAlternateScroll: true}, MouseWheelUp, 0, 1, 1, -1, true); a.Kind != MouseActionLocal {
		t.Error("alternate scroll disabled still translated")
	}
}

func TestMouseCtxFromModes(t *testing.T) {
	term := NewTerminal(10, 3)
	feed(term, "\x1b[?1003h\x1b[?1016h\x1b[?1007l\x1b[?1049h\x1b[?1h")
	ctx := term.MouseCtx()
	if ctx.Mode != 1003 || ctx.Encoding != 1016 || !ctx.NoAlternateScroll || !ctx.AltScreen || !ctx.AppCursorKeys {
		t.Fatalf("ctx = %+v", ctx)
	}
	term.SetCellPixelSize(9, 18)
	if ctx := term.MouseCtx(); ctx.CellW != 9 || ctx.CellH != 18 {
		t.Fatalf("cell size not in ctx: %+v", ctx)
	}
	feed(term, "\x1b[?1016l\x1b[?1015h")
	if ctx := term.MouseCtx(); ctx.Encoding != 1015 || ctx.SGR {
		t.Fatalf("ctx = %+v", ctx)
	}
	feed(term, "\x1b[?9h")
	if term.MouseCtx().Mode != 9 {
		t.Fatal("x10 mode")
	}
	// The legacy MouseState stays consistent.
	term2 := NewTerminal(10, 3)
	feed(term2, "\x1b[?1000h\x1b[?1016h")
	if _, sgr, _, _ := term2.MouseState(); !sgr {
		t.Fatal("1016 implies SGR framing")
	}
}
