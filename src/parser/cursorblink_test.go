package parser

import "testing"

func TestEffectiveBlink(t *testing.T) {
	cases := []struct {
		cfg, style, focus, typed, want bool
	}{
		{true, true, true, false, true},   // all conditions to blink
		{false, true, true, false, false}, // config off
		{true, false, true, false, false}, // steady style
		{true, true, false, false, false}, // unfocused
		{true, true, true, true, false},   // just typed -> solid
	}
	for _, c := range cases {
		if got := EffectiveBlink(c.cfg, c.style, c.focus, c.typed); got != c.want {
			t.Errorf("EffectiveBlink(%v,%v,%v,%v) = %v, want %v",
				c.cfg, c.style, c.focus, c.typed, got, c.want)
		}
	}
}

func TestDECSCUSRBlinkFlag(t *testing.T) {
	cases := []struct {
		seq       string
		wantStyle CursorStyle
		wantBlink bool
	}{
		{"\x1b[1 q", CursorStyleBlock, true},      // blink block
		{"\x1b[2 q", CursorStyleBlock, false},     // steady block
		{"\x1b[3 q", CursorStyleUnderline, true},  // blink underline
		{"\x1b[4 q", CursorStyleUnderline, false}, // steady underline
		{"\x1b[5 q", CursorStyleBar, true},        // blink bar
		{"\x1b[6 q", CursorStyleBar, false},       // steady bar
	}
	for _, c := range cases {
		term := NewTerminal(10, 1)
		term.Process([]byte(c.seq))
		if term.CursorStyle() != c.wantStyle {
			t.Errorf("%q: style = %v, want %v", c.seq, term.CursorStyle(), c.wantStyle)
		}
		if term.CursorBlinking() != c.wantBlink {
			t.Errorf("%q: blinking = %v, want %v", c.seq, term.CursorBlinking(), c.wantBlink)
		}
	}
}

func TestOSC12CursorColor(t *testing.T) {
	term := NewTerminal(10, 1)
	out := captureResponses(term)
	term.Process([]byte("\x1b]12;#00ff00\x1b\\")) // set cursor color green
	if c := term.CursorColor(); c.Type != 2 /*ColorRGB*/ || c.G != 0xff {
		t.Fatalf("cursor color not set: %+v", c)
	}
	term.Process([]byte("\x1b]12;?\x1b\\")) // query
	if want := "\x1b]12;rgb:0000/ffff/0000\x1b\\"; string(*out) != want {
		t.Fatalf("OSC 12 query reply = %q, want %q", string(*out), want)
	}
}
