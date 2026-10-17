//go:build linux && wayland

package glfw

import (
	"fmt"
	"testing"
)

func TestPostEmptyEventDoesNotLeakCallbacks(t *testing.T) {
	needDisplay(t)
	var before, after uint32
	onMain(func() {
		PollEvents()
		before = waylandNextProxyID()
		for i := 0; i < 500; i++ {
			PostEmptyEvent()
		}
		PollEvents()
		after = waylandNextProxyID()
	})
	t.Logf("proxy ids before %d after %d", before, after)
	if after > before+20 {
		t.Fatalf("PostEmptyEvent leaked %d protocol objects", after-before)
	}
}

func TestCursorShapeMapping(t *testing.T) {
	cases := map[StandardCursor]int{
		ArrowCursor:      1,  // default
		IBeamCursor:      9,  // text
		CrosshairCursor:  8,  // crosshair
		HandCursor:       4,  // pointer
		HResizeCursor:    26, // ew-resize
		VResizeCursor:    27, // ns-resize
		ResizeNWSECursor: 29, // nwse-resize
		ResizeNESWCursor: 28, // nesw-resize
		ResizeAllCursor:  32, // all-scroll
		NotAllowedCursor: 15, // not-allowed
	}
	for shape, want := range cases {
		if got := cursorShapeFor(shape); got != want {
			t.Errorf("cursorShapeFor(%#x) = %d, want %d", shape, got, want)
		}
	}
}

// Evdev scancodes, as the Wayland key callback reports them.
const (
	evKey1         = 2
	evKey7         = 8
	evKeyMinus     = 12
	evKeyA         = 30
	evKeyC         = 46
	evKeyVolumeUp  = 115
	evKeyPlayPause = 164
)

func withKeymap(t *testing.T, layout, options, text string) {
	t.Helper()
	needDisplay(t)
	var restore func()
	var err error
	onMain(func() { restore, err = useTestKeymap(layout, options, text) })
	if err != nil {
		t.Skipf("xkeyboard-config data unavailable: %v", err)
	}
	t.Cleanup(func() { onMain(restore) })
}

func symbolsOn(scancode int, depressed, locked, group uint32) (s KeySymbols, ok bool) {
	onMain(func() {
		testModifiers(depressed, locked, group)
		s, ok = GetScancodeSymbols(scancode)
	})
	return
}

func TestScancodeSymbolsFollowLayout(t *testing.T) {
	cases := []struct {
		name, layout string
		scancode     int
		group        uint32
		want         KeySymbols
	}{
		{"us 1", "us", evKey1, 0, KeySymbols{Keysym: '1', Rune: '1', Shifted: '!'}},
		{"de 7", "de", evKey7, 0, KeySymbols{Keysym: '7', Rune: '7', Shifted: '/'}},
		{"de ß", "de", evKeyMinus, 0, KeySymbols{Keysym: 0xdf, Rune: 'ß', Shifted: '?'}},
		{"fr 1", "fr", evKey1, 0, KeySymbols{Keysym: '&', Rune: '&', Shifted: '1'}},
		{"us,ru group 2 c", "us,ru", evKeyC, 1, KeySymbols{Keysym: 0x6d3, Rune: 'с', Shifted: 'С'}},
		{"play/pause has no text", "us", evKeyPlayPause, 0, KeySymbols{Keysym: 0x1008ff14}},
		{"volume up", "us", evKeyVolumeUp, 0, KeySymbols{Keysym: 0x1008ff13}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withKeymap(t, c.layout, "", "")
			got, ok := symbolsOn(c.scancode, 0, 0, c.group)
			if !ok || got != c.want {
				t.Fatalf("GetScancodeSymbols(%d) = %+v, %v; want %+v", c.scancode, got, ok, c.want)
			}
		})
	}
}

// The lookup ignores the live modifier state: Shift held or Caps Lock on
// must not change what is reported as the unshifted key.
func TestScancodeSymbolsIgnoreLiveModifiers(t *testing.T) {
	withKeymap(t, "us", "", "")
	var shiftMask, lockMask uint32
	onMain(func() { shiftMask, lockMask = testModMask("Shift"), testModMask("Lock") })
	want := KeySymbols{Keysym: 'a', Rune: 'a', Shifted: 'A'}
	if got, ok := symbolsOn(evKeyA, shiftMask, lockMask, 0); !ok || got != want {
		t.Fatalf("with Shift+Caps held = %+v, %v; want %+v", got, ok, want)
	}
}

const ownModifierKeymap = `xkb_keymap {
	xkb_keycodes { include "evdev+aliases(qwerty)" };
	xkb_types { include "complete" };
	xkb_compat { include "complete" };
	xkb_symbols {
		include "pc+us+inet(evdev)"
		replace key <CAPS> { [ %s ] };
		modifier_map Mod3 { <CAPS> };
	};
};`

func TestHyperMetaReportedOnlyOnOwnModifier(t *testing.T) {
	t.Run("stock keymap shares them", func(t *testing.T) {
		withKeymap(t, "us", "", "")
		var hyper, meta uint32
		var mods ModifierKey
		onMain(func() {
			hyper, meta = testHyperMetaMasks()
			mods = testModifiers(testModMask("Mod4")|testModMask("Mod1"), 0, 0)
		})
		if hyper != 0 || meta != 0 {
			t.Errorf("hyper %#x meta %#x, want both 0", hyper, meta)
		}
		if mods != ModSuper|ModAlt {
			t.Errorf("Super+Alt held = %#x, want only Super|Alt", mods)
		}
	})
	for _, c := range []struct {
		sym  string
		want ModifierKey
	}{{"Hyper_L", ModHyper}, {"Meta_L", ModMeta}} {
		t.Run(c.sym+" on Mod3", func(t *testing.T) {
			withKeymap(t, "", "", fmt.Sprintf(ownModifierKeymap, c.sym))
			var mod3 uint32
			var held, super ModifierKey
			onMain(func() {
				mod3 = testModMask("Mod3")
				held = testModifiers(mod3, 0, 0)
				super = testModifiers(testModMask("Mod4"), 0, 0)
			})
			if held != c.want {
				t.Errorf("Mod3 held = %#x, want %#x", held, c.want)
			}
			if super != ModSuper {
				t.Errorf("Mod4 held = %#x, want only Super", super)
			}
		})
	}
}
