package keybindings

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

const (
	ctrl  = glfw.ModControl
	shift = glfw.ModShift
	alt   = glfw.ModAlt
	super = glfw.ModSuper
	numLk = glfw.ModNumLock
	rel   = glfw.Action(100)
)

type kcase struct {
	name   string
	key    glfw.Key
	mods   glfw.ModifierKey
	r      rune
	action glfw.Action
	mode   Mode
	want   string
}

func runCases(t *testing.T, cases []kcase) {
	t.Helper()
	for _, c := range cases {
		act := c.action
		switch act {
		case 0:
			act = glfw.Press
		case rel:
			act = glfw.Release
		}
		res := Translate(Event{Key: c.key, Mods: c.mods, Action: act, Rune: c.r}, c.mode)
		got := string(res.Data)
		if c.want == "" {
			if res.Action == ActionInput || got != "" {
				t.Errorf("%s: got %v %q, want nothing", c.name, res.Action, got)
			}
			continue
		}
		if res.Action != ActionInput || got != c.want {
			t.Errorf("%s: got %v %q, want %q", c.name, res.Action, got, c.want)
		}
	}
}

func linux(t *testing.T) {
	prev := isMacOS
	isMacOS = false
	t.Cleanup(func() { isMacOS = prev })
}

func TestAppChordsNoLongerStolen(t *testing.T) {
	linux(t)
	runCases(t, []kcase{
		{name: "Ctrl+Q", key: glfw.KeyQ, mods: ctrl, r: 'q', want: "\x11"},
		{name: "Ctrl+R", key: glfw.KeyR, mods: ctrl, r: 'r', want: "\x12"},
		{name: "Shift+Enter", key: glfw.KeyEnter, mods: shift, want: "\r"},
		{name: "Shift+PgUp alt screen", key: glfw.KeyPageUp, mods: shift, mode: Mode{AltScreen: true}, want: "\x1b[5;2~"},
		{name: "Shift+PgDn mouse app", key: glfw.KeyPageDown, mods: shift, mode: Mode{MouseReporting: true}, want: "\x1b[6;2~"},
		{name: "Shift+Up alt screen", key: glfw.KeyUp, mods: shift, mode: Mode{AltScreen: true}, want: "\x1b[1;2A"},
		{name: "Shift+Down kitty", key: glfw.KeyDown, mods: shift, mode: Mode{KittyFlags: 1}, want: "\x1b[1;2B"},
	})
	actions := []struct {
		name string
		key  glfw.Key
		mods glfw.ModifierKey
		mode Mode
		want KeyAction
	}{
		{"Ctrl+Shift+Q", glfw.KeyQ, ctrl | shift, Mode{}, ActionExit},
		{"Super+Q", glfw.KeyQ, super, Mode{}, ActionExit},
		{"Ctrl+Shift+R", glfw.KeyR, ctrl | shift, Mode{}, ActionToggleResizeMode},
		{"Super+R", glfw.KeyR, super, Mode{}, ActionToggleResizeMode},
		{"Super+Enter", glfw.KeyEnter, super, Mode{}, ActionToggleFullscreen},
		{"Ctrl+Shift+Enter", glfw.KeyEnter, ctrl | shift, Mode{}, ActionToggleFullscreen},
		{"F11", glfw.KeyF11, 0, Mode{}, ActionToggleFullscreen},
		{"Shift+PgUp main", glfw.KeyPageUp, shift, Mode{}, ActionScrollUp},
		{"Shift+PgDn main", glfw.KeyPageDown, shift, Mode{}, ActionScrollDown},
		{"Shift+Up main", glfw.KeyUp, shift, Mode{}, ActionScrollUpLine},
		{"Shift+Down main", glfw.KeyDown, shift, Mode{}, ActionScrollDownLine},
		{"Super+T in kitty mode", glfw.KeyT, super, Mode{KittyFlags: 31}, ActionNewTab},
	}
	for _, c := range actions {
		if got := Translate(Event{Key: c.key, Mods: c.mods, Action: glfw.Press}, c.mode).Action; got != c.want {
			t.Errorf("%s = %v, want %v", c.name, got, c.want)
		}
	}
	if got := Translate(Event{Key: glfw.KeyQ, Mods: ctrl | shift, Action: glfw.Release}, Mode{}).Action; got != ActionNone {
		t.Errorf("release of Ctrl+Shift+Q = %v, want none", got)
	}
}

func TestLayoutAwareCtrlAlt(t *testing.T) {
	linux(t)
	runCases(t, []kcase{
		{name: "AZERTY Ctrl+A on physical Q", key: glfw.KeyQ, mods: ctrl, r: 'a', want: "\x01"},
		{name: "Dvorak Ctrl+U on physical F", key: glfw.KeyF, mods: ctrl, r: 'u', want: "\x15"},
		{name: "Cyrillic Ctrl+C falls back to physical", key: glfw.KeyC, mods: ctrl, r: 'с', want: "\x03"},
		{name: "Ctrl+Alt+A", key: glfw.KeyA, mods: ctrl | alt, r: 'a', want: "\x1b\x01"},
		{name: "Alt+letter defers to char callback", key: glfw.KeyA, mods: alt, r: 'a', want: ""},
		{name: "plain letter defers to char callback", key: glfw.KeyA, r: 'a', want: ""},
	})
	if got := string(TranslateChar('é', alt, Mode{})); got != "\x1bé" {
		t.Errorf("Alt+é char = %q", got)
	}
	if got := string(TranslateChar('/', alt|shift, Mode{})); got != "\x1b/" {
		t.Errorf("Alt+Shift+7 (de) char = %q", got)
	}
	if got := string(TranslateChar('x', 0, Mode{})); got != "x" {
		t.Errorf("plain char = %q", got)
	}
}

func TestAltWithoutEscapePrefix(t *testing.T) {
	linux(t)
	noEsc := Mode{AltNoEscape: true}
	if got := string(TranslateChar('a', alt, noEsc)); got != "\u00e1" {
		t.Errorf("Alt+a with ?1036/?1039 off = %q, want 8-bit meta U+00E1", got)
	}
	if got := string(TranslateChar('é', alt, noEsc)); got != "é" {
		t.Errorf("Alt+é with ?1036/?1039 off = %q", got)
	}
	runCases(t, []kcase{
		{name: "Alt+Backspace no esc", key: glfw.KeyBackspace, mods: alt, mode: noEsc, want: "\x7f"},
		{name: "Ctrl+Alt+A no esc", key: glfw.KeyA, mods: ctrl | alt, r: 'a', mode: noEsc, want: "\x01"},
	})
}

func TestLegacyEncodings(t *testing.T) {
	linux(t)
	ss3 := "\x1bO"
	runCases(t, []kcase{
		{name: "Ctrl+[", key: glfw.KeyLeftBracket, mods: ctrl, r: '[', want: "\x1b"},
		{name: "Ctrl+\\", key: glfw.KeyBackslash, mods: ctrl, r: '\\', want: "\x1c"},
		{name: "Ctrl+]", key: glfw.KeyRightBracket, mods: ctrl, r: ']', want: "\x1d"},
		{name: "Ctrl+/", key: glfw.KeySlash, mods: ctrl, r: '/', want: "\x1f"},
		{name: "Ctrl+2", key: glfw.Key2, mods: ctrl, r: '2', want: "\x00"},
		{name: "Ctrl+3", key: glfw.Key3, mods: ctrl, r: '3', want: "\x1b"},
		{name: "Ctrl+4", key: glfw.Key4, mods: ctrl, r: '4', want: "\x1c"},
		{name: "Ctrl+5", key: glfw.Key5, mods: ctrl, r: '5', want: "\x1d"},
		{name: "Ctrl+6", key: glfw.Key6, mods: ctrl, r: '6', want: "\x1e"},
		{name: "Ctrl+7", key: glfw.Key7, mods: ctrl, r: '7', want: "\x1f"},
		{name: "Ctrl+8", key: glfw.Key8, mods: ctrl, r: '8', want: "\x7f"},
		{name: "Ctrl+Space", key: glfw.KeySpace, mods: ctrl, r: ' ', want: "\x00"},
		{name: "Ctrl+1 unmapped", key: glfw.Key1, mods: ctrl, r: '1', want: ""},
		{name: "Backspace", key: glfw.KeyBackspace, want: "\x7f"},
		{name: "Alt+Backspace", key: glfw.KeyBackspace, mods: alt, want: "\x1b\x7f"},
		{name: "Ctrl+Backspace", key: glfw.KeyBackspace, mods: ctrl, want: "\x08"},
		{name: "Ctrl+Alt+Backspace", key: glfw.KeyBackspace, mods: ctrl | alt, want: "\x1b\x08"},
		{name: "Alt+Enter", key: glfw.KeyEnter, mods: alt, want: "\x1b\r"},
		{name: "Alt+Tab", key: glfw.KeyTab, mods: alt, want: "\x1b\t"},
		{name: "Shift+Tab", key: glfw.KeyTab, mods: shift, want: "\x1b[Z"},
		{name: "Esc", key: glfw.KeyEscape, want: "\x1b"},
		{name: "Alt+Esc", key: glfw.KeyEscape, mods: alt, want: "\x1b\x1b"},
		{name: "Up", key: glfw.KeyUp, want: "\x1b[A"},
		{name: "Up DECCKM", key: glfw.KeyUp, mode: Mode{AppCursor: true}, want: ss3 + "A"},
		{name: "Ctrl+Up", key: glfw.KeyUp, mods: ctrl, want: "\x1b[1;5A"},
		{name: "Ctrl+Up DECCKM", key: glfw.KeyUp, mods: ctrl, mode: Mode{AppCursor: true}, want: "\x1b[1;5A"},
		{name: "Alt+Left", key: glfw.KeyLeft, mods: alt, want: "\x1b[1;3D"},
		{name: "Ctrl+Shift+Right", key: glfw.KeyRight, mods: ctrl | shift, want: "\x1b[1;6C"},
		{name: "Home", key: glfw.KeyHome, want: "\x1b[H"},
		{name: "End", key: glfw.KeyEnd, want: "\x1b[F"},
		{name: "Home DECCKM", key: glfw.KeyHome, mode: Mode{AppCursor: true}, want: ss3 + "H"},
		{name: "End DECCKM", key: glfw.KeyEnd, mode: Mode{AppCursor: true}, want: ss3 + "F"},
		{name: "Ctrl+Home", key: glfw.KeyHome, mods: ctrl, want: "\x1b[1;5H"},
		{name: "Shift+End", key: glfw.KeyEnd, mods: shift, want: "\x1b[1;2F"},
		{name: "Insert", key: glfw.KeyInsert, want: "\x1b[2~"},
		{name: "Ctrl+Delete", key: glfw.KeyDelete, mods: ctrl, want: "\x1b[3;5~"},
		{name: "Ctrl+PgUp", key: glfw.KeyPageUp, mods: ctrl, want: "\x1b[5;5~"},
		{name: "Alt+PgDn", key: glfw.KeyPageDown, mods: alt, want: "\x1b[6;3~"},
		{name: "F1", key: glfw.KeyF1, want: ss3 + "P"},
		{name: "F3", key: glfw.KeyF3, want: ss3 + "R"},
		{name: "Ctrl+F1", key: glfw.KeyF1, mods: ctrl, want: "\x1b[1;5P"},
		{name: "Shift+F4", key: glfw.KeyF4, mods: shift, want: "\x1b[1;2S"},
		{name: "F5", key: glfw.KeyF5, want: "\x1b[15~"},
		{name: "Ctrl+F5", key: glfw.KeyF5, mods: ctrl, want: "\x1b[15;5~"},
		{name: "Alt+F12", key: glfw.KeyF12, mods: alt, want: "\x1b[24;3~"},
		{name: "Shift+F11", key: glfw.KeyF11, mods: shift, want: "\x1b[23;2~"},
		{name: "F13", key: glfw.KeyF13, want: "\x1b[1;2P"},
		{name: "KP1 app keypad", key: glfw.KeyKP1, mods: numLk, mode: Mode{AppKeypad: true}, want: ss3 + "q"},
		{name: "KP0 app keypad", key: glfw.KeyKP0, mods: numLk, mode: Mode{AppKeypad: true}, want: ss3 + "p"},
		{name: "KP9 app keypad", key: glfw.KeyKP9, mods: numLk, mode: Mode{AppKeypad: true}, want: ss3 + "y"},
		{name: "KP* app keypad", key: glfw.KeyKPMultiply, mode: Mode{AppKeypad: true}, want: ss3 + "j"},
		{name: "KP+ app keypad", key: glfw.KeyKPAdd, mode: Mode{AppKeypad: true}, want: ss3 + "k"},
		{name: "KP- app keypad", key: glfw.KeyKPSubtract, mode: Mode{AppKeypad: true}, want: ss3 + "m"},
		{name: "KP. app keypad", key: glfw.KeyKPDecimal, mods: numLk, mode: Mode{AppKeypad: true}, want: ss3 + "n"},
		{name: "KP/ app keypad", key: glfw.KeyKPDivide, mode: Mode{AppKeypad: true}, want: ss3 + "o"},
		{name: "KP= app keypad", key: glfw.KeyKPEqual, mode: Mode{AppKeypad: true}, want: ss3 + "X"},
		{name: "KPEnter app keypad", key: glfw.KeyKPEnter, mode: Mode{AppKeypad: true}, want: ss3 + "M"},
		{name: "KPEnter", key: glfw.KeyKPEnter, want: "\r"},
		{name: "KP1 numlock text", key: glfw.KeyKP1, mods: numLk, want: ""},
		{name: "KP1 no numlock = End", key: glfw.KeyKP1, want: "\x1b[F"},
		{name: "KP8 no numlock = Up", key: glfw.KeyKP8, want: "\x1b[A"},
		{name: "KP9 no numlock = PgUp", key: glfw.KeyKP9, want: "\x1b[5~"},
		{name: "KP. no numlock = Delete", key: glfw.KeyKPDecimal, want: "\x1b[3~"},
		{name: "mOK1 Ctrl+;", key: glfw.KeySemicolon, mods: ctrl, r: ';', mode: Mode{ModifyOtherKeys: 1}, want: "\x1b[27;5;59~"},
		{name: "mOK1 Ctrl+1", key: glfw.Key1, mods: ctrl, r: '1', mode: Mode{ModifyOtherKeys: 1}, want: "\x1b[27;5;49~"},
		{name: "mOK1 Shift+Enter", key: glfw.KeyEnter, mods: shift, mode: Mode{ModifyOtherKeys: 1}, want: "\x1b[27;2;13~"},
		{name: "mOK1 Ctrl+Enter", key: glfw.KeyEnter, mods: ctrl, mode: Mode{ModifyOtherKeys: 1}, want: "\x1b[27;5;13~"},
		{name: "mOK1 Ctrl+A unchanged", key: glfw.KeyA, mods: ctrl, r: 'a', mode: Mode{ModifyOtherKeys: 1}, want: "\x01"},
		{name: "mOK1 Alt+Enter unchanged", key: glfw.KeyEnter, mods: alt, mode: Mode{ModifyOtherKeys: 1}, want: "\x1b\r"},
		{name: "mOK2 Ctrl+A", key: glfw.KeyA, mods: ctrl, r: 'a', mode: Mode{ModifyOtherKeys: 2}, want: "\x1b[27;5;97~"},
		{name: "mOK2 Alt+A", key: glfw.KeyA, mods: alt, r: 'a', mode: Mode{ModifyOtherKeys: 2}, want: "\x1b[27;3;97~"},
		{name: "mOK2 Ctrl+Shift+Y", key: glfw.KeyY, mods: ctrl | shift, r: 'y', mode: Mode{ModifyOtherKeys: 2}, want: "\x1b[27;6;89~"},
		{name: "mOK2 Shift+A is text", key: glfw.KeyA, mods: shift, r: 'a', mode: Mode{ModifyOtherKeys: 2}, want: ""},
		{name: "mOK2 Ctrl+Backspace", key: glfw.KeyBackspace, mods: ctrl, mode: Mode{ModifyOtherKeys: 2}, want: "\x1b[27;5;127~"},
		{name: "mOK2 Shift+Tab", key: glfw.KeyTab, mods: shift, mode: Mode{ModifyOtherKeys: 2}, want: "\x1b[Z"},
		{name: "repeat encodes like press", key: glfw.KeyUp, action: glfw.Repeat, want: "\x1b[A"},
		{name: "release sends nothing", key: glfw.KeyUp, action: rel, want: ""},
	})
}

func TestKittyKeyboard(t *testing.T) {
	linux(t)
	k := func(f uint8) Mode { return Mode{KittyFlags: f} }
	runCases(t, []kcase{
		// flag 1: disambiguate
		{name: "1 a is text", key: glfw.KeyA, r: 'a', mode: k(1), want: ""},
		{name: "1 shift+a is text", key: glfw.KeyA, mods: shift, r: 'a', mode: k(1), want: ""},
		{name: "1 ctrl+a", key: glfw.KeyA, mods: ctrl, r: 'a', mode: k(1), want: "\x1b[97;5u"},
		{name: "1 ctrl+q", key: glfw.KeyQ, mods: ctrl, r: 'q', mode: k(1), want: "\x1b[113;5u"},
		{name: "1 alt+a", key: glfw.KeyA, mods: alt, r: 'a', mode: k(1), want: "\x1b[97;3u"},
		{name: "1 shift+alt+a", key: glfw.KeyA, mods: shift | alt, r: 'a', mode: k(1), want: "\x1b[97;4u"},
		{name: "1 ctrl+space", key: glfw.KeySpace, mods: ctrl, r: ' ', mode: k(1), want: "\x1b[32;5u"},
		{name: "1 esc", key: glfw.KeyEscape, mode: k(1), want: "\x1b[27u"},
		{name: "1 enter legacy", key: glfw.KeyEnter, mode: k(1), want: "\r"},
		{name: "1 tab legacy", key: glfw.KeyTab, mode: k(1), want: "\t"},
		{name: "1 backspace legacy", key: glfw.KeyBackspace, mode: k(1), want: "\x7f"},
		{name: "1 shift+enter", key: glfw.KeyEnter, mods: shift, mode: k(1), want: "\x1b[13;2u"},
		{name: "1 shift+tab", key: glfw.KeyTab, mods: shift, mode: k(1), want: "\x1b[9;2u"},
		{name: "1 ctrl+backspace", key: glfw.KeyBackspace, mods: ctrl, mode: k(1), want: "\x1b[127;5u"},
		{name: "1 up", key: glfw.KeyUp, mode: k(1), want: "\x1b[A"},
		{name: "1 up DECCKM still CSI", key: glfw.KeyUp, mode: Mode{KittyFlags: 1, AppCursor: true}, want: "\x1b[A"},
		{name: "1 ctrl+up", key: glfw.KeyUp, mods: ctrl, mode: k(1), want: "\x1b[1;5A"},
		{name: "1 home", key: glfw.KeyHome, mode: k(1), want: "\x1b[H"},
		{name: "1 end", key: glfw.KeyEnd, mode: k(1), want: "\x1b[F"},
		{name: "1 insert", key: glfw.KeyInsert, mode: k(1), want: "\x1b[2~"},
		{name: "1 ctrl+delete", key: glfw.KeyDelete, mods: ctrl, mode: k(1), want: "\x1b[3;5~"},
		{name: "1 pgup", key: glfw.KeyPageUp, mode: k(1), want: "\x1b[5~"},
		{name: "1 pgdn", key: glfw.KeyPageDown, mode: k(1), want: "\x1b[6~"},
		{name: "1 F1", key: glfw.KeyF1, mode: k(1), want: "\x1b[P"},
		{name: "1 F2", key: glfw.KeyF2, mode: k(1), want: "\x1b[Q"},
		{name: "1 F3", key: glfw.KeyF3, mode: k(1), want: "\x1b[13~"},
		{name: "1 ctrl+F3", key: glfw.KeyF3, mods: ctrl, mode: k(1), want: "\x1b[13;5~"},
		{name: "1 F4", key: glfw.KeyF4, mode: k(1), want: "\x1b[S"},
		{name: "1 shift+F1", key: glfw.KeyF1, mods: shift, mode: k(1), want: "\x1b[1;2P"},
		{name: "1 F5", key: glfw.KeyF5, mode: k(1), want: "\x1b[15~"},
		{name: "1 F6", key: glfw.KeyF6, mode: k(1), want: "\x1b[17~"},
		{name: "1 F12", key: glfw.KeyF12, mode: k(1), want: "\x1b[24~"},
		{name: "1 F13", key: glfw.KeyF13, mode: k(1), want: "\x1b[57376u"},
		{name: "1 F25", key: glfw.KeyF25, mode: k(1), want: "\x1b[57388u"},
		{name: "1 print screen", key: glfw.KeyPrintScreen, mode: k(1), want: "\x1b[57361u"},
		{name: "1 pause", key: glfw.KeyPause, mode: k(1), want: "\x1b[57362u"},
		{name: "1 menu", key: glfw.KeyMenu, mode: k(1), want: "\x1b[57363u"},
		{name: "1 KP enter", key: glfw.KeyKPEnter, mode: k(1), want: "\x1b[57414u"},
		{name: "1 KP1 numlock is text", key: glfw.KeyKP1, mods: numLk, mode: k(1), want: ""},
		{name: "1 KP1 no numlock = KP_END", key: glfw.KeyKP1, mode: k(1), want: "\x1b[57424u"},
		{name: "1 KP5 no numlock = KP_BEGIN", key: glfw.KeyKP5, mode: k(1), want: "\x1b[57427u"},
		{name: "1 ctrl+KP+", key: glfw.KeyKPAdd, mods: ctrl, mode: k(1), want: "\x1b[57413;5u"},
		{name: "1 shift press is not reported", key: glfw.KeyLeftShift, mods: shift, mode: k(1), want: ""},
		{name: "1 release not reported", key: glfw.KeyA, mods: ctrl, r: 'a', action: rel, mode: k(1), want: ""},
		{name: "1 numlock ignored on text key", key: glfw.KeyA, mods: ctrl | numLk, r: 'a', mode: k(1), want: "\x1b[97;5u"},
		// flag 2: event types
		{name: "3 press of a is text", key: glfw.KeyA, r: 'a', mode: k(3), want: ""},
		{name: "3 release a", key: glfw.KeyA, r: 'a', action: rel, mode: k(3), want: "\x1b[97;1:3u"},
		{name: "3 release shift+a", key: glfw.KeyA, mods: shift, r: 'a', action: rel, mode: k(3), want: "\x1b[97;2:3u"},
		{name: "3 repeat ctrl+a", key: glfw.KeyA, mods: ctrl, r: 'a', action: glfw.Repeat, mode: k(3), want: "\x1b[97;5:2u"},
		{name: "3 release up", key: glfw.KeyUp, action: rel, mode: k(3), want: "\x1b[1;1:3A"},
		{name: "3 repeat F5", key: glfw.KeyF5, action: glfw.Repeat, mode: k(3), want: "\x1b[15;1:2~"},
		{name: "3 release esc", key: glfw.KeyEscape, action: rel, mode: k(3), want: "\x1b[27;1:3u"},
		{name: "3 release enter not reported", key: glfw.KeyEnter, action: rel, mode: k(3), want: ""},
		{name: "3 release tab not reported", key: glfw.KeyTab, action: rel, mode: k(3), want: ""},
		{name: "3 repeat enter legacy", key: glfw.KeyEnter, action: glfw.Repeat, mode: k(3), want: "\r"},
		{name: "1 repeat reported as press", key: glfw.KeyA, mods: ctrl, r: 'a', action: glfw.Repeat, mode: k(1), want: "\x1b[97;5u"},
		// flag 4: alternate keys
		{name: "5 shift+alt+a", key: glfw.KeyA, mods: shift | alt, r: 'a', mode: k(5), want: "\x1b[97:65;4u"},
		{name: "5 ctrl+a no alternates", key: glfw.KeyA, mods: ctrl, r: 'a', mode: k(5), want: "\x1b[97;5u"},
		{name: "5 cyrillic ctrl+es base key", key: glfw.KeyC, mods: ctrl, r: 'с', mode: k(5), want: "\x1b[1089::99;5u"},
		{name: "5 shift+alt+1", key: glfw.Key1, mods: shift | alt, r: '1', mode: k(5), want: "\x1b[49:33;4u"},
		// flag 8: all keys as escape codes
		{name: "8 a", key: glfw.KeyA, r: 'a', mode: k(8), want: "\x1b[97u"},
		{name: "8 shift+a", key: glfw.KeyA, mods: shift, r: 'a', mode: k(8), want: "\x1b[97;2u"},
		{name: "8 enter", key: glfw.KeyEnter, mode: k(8), want: "\x1b[13u"},
		{name: "8 tab", key: glfw.KeyTab, mode: k(8), want: "\x1b[9u"},
		{name: "8 backspace", key: glfw.KeyBackspace, mode: k(8), want: "\x1b[127u"},
		{name: "8 space", key: glfw.KeySpace, r: ' ', mode: k(8), want: "\x1b[32u"},
		{name: "8 up", key: glfw.KeyUp, mode: k(8), want: "\x1b[A"},
		{name: "8 left shift press", key: glfw.KeyLeftShift, mode: k(8), want: "\x1b[57441;2u"},
		{name: "8 left ctrl press", key: glfw.KeyLeftControl, mods: ctrl, mode: k(8), want: "\x1b[57442;5u"},
		{name: "8 left alt", key: glfw.KeyLeftAlt, mode: k(8), want: "\x1b[57443;3u"},
		{name: "8 left super", key: glfw.KeyLeftSuper, mode: k(8), want: "\x1b[57444;9u"},
		{name: "8 right shift", key: glfw.KeyRightShift, mode: k(8), want: "\x1b[57447;2u"},
		{name: "8 right ctrl", key: glfw.KeyRightControl, mode: k(8), want: "\x1b[57448;5u"},
		{name: "8 right alt", key: glfw.KeyRightAlt, mode: k(8), want: "\x1b[57449;3u"},
		{name: "8 right super", key: glfw.KeyRightSuper, mode: k(8), want: "\x1b[57450;9u"},
		{name: "8 caps lock", key: glfw.KeyCapsLock, mode: k(8), want: "\x1b[57358u"},
		{name: "8 scroll lock", key: glfw.KeyScrollLock, mode: k(8), want: "\x1b[57359u"},
		{name: "8 num lock", key: glfw.KeyNumLock, mode: k(8), want: "\x1b[57360u"},
		{name: "8 numlock bit on text", key: glfw.KeyA, mods: numLk, r: 'a', mode: k(8), want: "\x1b[97;129u"},
		{name: "8 capslock bit", key: glfw.KeyA, mods: glfw.ModCapsLock, r: 'a', mode: k(8), want: "\x1b[97;65u"},
		{name: "8 KP1 numlock", key: glfw.KeyKP1, mods: numLk, mode: k(8), want: "\x1b[57400;129u"},
		{name: "10 left shift release", key: glfw.KeyLeftShift, mods: shift, action: rel, mode: k(10), want: "\x1b[57441;1:3u"},
		{name: "10 enter release", key: glfw.KeyEnter, action: rel, mode: k(10), want: "\x1b[13;1:3u"},
		{name: "10 release a", key: glfw.KeyA, r: 'a', action: rel, mode: k(10), want: "\x1b[97;1:3u"},
		// flag 16: associated text
		{name: "24 a text", key: glfw.KeyA, r: 'a', mode: k(24), want: "\x1b[97;1;97u"},
		{name: "24 shift+a text", key: glfw.KeyA, mods: shift, r: 'a', mode: k(24), want: "\x1b[97;2;65u"},
		{name: "24 capslock a text", key: glfw.KeyA, mods: glfw.ModCapsLock, r: 'a', mode: k(24), want: "\x1b[97;65;65u"},
		{name: "24 ctrl+a no text", key: glfw.KeyA, mods: ctrl, r: 'a', mode: k(24), want: "\x1b[97;5u"},
		{name: "26 release no text", key: glfw.KeyA, r: 'a', action: rel, mode: k(26), want: "\x1b[97;1:3u"},
		{name: "28 shift+a all", key: glfw.KeyA, mods: shift, r: 'a', mode: k(28), want: "\x1b[97:65;2;65u"},
		{name: "24 KP1 text", key: glfw.KeyKP1, mods: numLk, mode: k(24), want: "\x1b[57400;129;49u"},
		{name: "24 enter has no text", key: glfw.KeyEnter, mode: k(24), want: "\x1b[13u"},
		{name: "16 alone is not reporting text", key: glfw.KeyA, r: 'a', mode: Mode{KittyFlags: 17}, want: ""},
		// flags without 1 or 8 keep legacy bytes
		{name: "2 only ctrl+a legacy", key: glfw.KeyA, mods: ctrl, r: 'a', mode: k(2), want: "\x01"},
		{name: "kitty ctrl+q not exit", key: glfw.KeyQ, mods: ctrl, r: 'q', mode: k(31), want: "\x1b[113;5u"},
	})
}

func TestTranslateKeyWrapperDetectsShortcuts(t *testing.T) {
	linux(t)
	if got := TranslateKey(glfw.KeyF, super|shift, false).Action; got != ActionFindInScrollback {
		t.Fatalf("Super+Shift+F = %v", got)
	}
	if got := TranslateKey(glfw.KeyUp, 0, true); string(got.Data) != "\x1bOA" {
		t.Fatalf("Up app cursor = %q", got.Data)
	}
}
