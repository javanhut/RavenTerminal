package keybindings

import (
	"strconv"
	"unicode"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// Kitty progressive-enhancement flags (CSI > flags u).
const (
	kittyDisambiguate = 1
	kittyEvents       = 2
	kittyAlternates   = 4
	kittyAllKeys      = 8
	kittyText         = 16
)

// Kitty modifier bits (the parameter is 1 + this bitmask).
const (
	KMShift    uint8 = 1 << 0
	KMAlt      uint8 = 1 << 1
	KMCtrl     uint8 = 1 << 2
	KMSuper    uint8 = 1 << 3
	KMHyper    uint8 = 1 << 4
	KMMeta     uint8 = 1 << 5
	KMCapsLock uint8 = 1 << 6
	KMNumLock  uint8 = 1 << 7
)

// Kitty event types.
const (
	KEvPress   = 1
	KEvRepeat  = 2
	KEvRelease = 3
)

type kittyFunc struct {
	num   int
	final byte
}

var kittyFuncKeys = map[glfw.Key]kittyFunc{
	glfw.KeyEscape: {27, 'u'}, glfw.KeyEnter: {13, 'u'}, glfw.KeyTab: {9, 'u'}, glfw.KeyBackspace: {127, 'u'},
	glfw.KeyInsert: {2, '~'}, glfw.KeyDelete: {3, '~'}, glfw.KeyPageUp: {5, '~'}, glfw.KeyPageDown: {6, '~'},
	glfw.KeyLeft: {1, 'D'}, glfw.KeyRight: {1, 'C'}, glfw.KeyUp: {1, 'A'}, glfw.KeyDown: {1, 'B'},
	glfw.KeyHome: {1, 'H'}, glfw.KeyEnd: {1, 'F'},
	glfw.KeyPrintScreen: {57361, 'u'}, glfw.KeyPause: {57362, 'u'}, glfw.KeyMenu: {57363, 'u'},
	glfw.KeyF1: {1, 'P'}, glfw.KeyF2: {1, 'Q'}, glfw.KeyF3: {13, '~'}, glfw.KeyF4: {1, 'S'},
	glfw.KeyF5: {15, '~'}, glfw.KeyF6: {17, '~'}, glfw.KeyF7: {18, '~'}, glfw.KeyF8: {19, '~'},
	glfw.KeyF9: {20, '~'}, glfw.KeyF10: {21, '~'}, glfw.KeyF11: {23, '~'}, glfw.KeyF12: {24, '~'},
	glfw.KeyKPEnter: {57414, 'u'},
	// Keypad digits with Num Lock off.
	glfw.KeyKP0: {57425, 'u'}, glfw.KeyKP1: {57424, 'u'}, glfw.KeyKP2: {57420, 'u'},
	glfw.KeyKP3: {57422, 'u'}, glfw.KeyKP4: {57417, 'u'}, glfw.KeyKP5: {57427, 'u'},
	glfw.KeyKP6: {57418, 'u'}, glfw.KeyKP7: {57423, 'u'}, glfw.KeyKP8: {57419, 'u'},
	glfw.KeyKP9: {57421, 'u'}, glfw.KeyKPDecimal: {57426, 'u'},
}

// Keypad keys that produce text: kitty code and the text.
var kittyKPText = map[glfw.Key][2]rune{
	glfw.KeyKPDivide: {57410, '/'}, glfw.KeyKPMultiply: {57411, '*'}, glfw.KeyKPSubtract: {57412, '-'},
	glfw.KeyKPAdd: {57413, '+'}, glfw.KeyKPEqual: {57415, '='},
}

type kittyModKey struct {
	code int
	bit  uint8
}

var kittyModKeys = map[glfw.Key]kittyModKey{
	glfw.KeyLeftShift: {57441, KMShift}, glfw.KeyLeftControl: {57442, KMCtrl},
	glfw.KeyLeftAlt: {57443, KMAlt}, glfw.KeyLeftSuper: {57444, KMSuper},
	glfw.KeyRightShift: {57447, KMShift}, glfw.KeyRightControl: {57448, KMCtrl},
	glfw.KeyRightAlt: {57449, KMAlt}, glfw.KeyRightSuper: {57450, KMSuper},
	glfw.KeyCapsLock: {57358, 0}, glfw.KeyScrollLock: {57359, 0}, glfw.KeyNumLock: {57360, 0},
}

// Modifier keys known by keysym rather than position: a Caps Lock remapped
// to Hyper reports as Hyper, and AltGr as ISO_Level3_Shift rather than
// Right Alt. These win over kittyModKeys.
var kittyModKeysyms = map[uint32]kittyModKey{
	0xffed: {57445, KMHyper}, 0xffee: {57451, KMHyper}, // Hyper_L, Hyper_R
	0xffe7: {57446, KMMeta}, 0xffe8: {57452, KMMeta}, // Meta_L, Meta_R
	0xfe03: {57453, 0}, 0xfe11: {57454, 0}, // ISO_Level3_Shift, ISO_Level5_Shift
}

// Media keys, which GLFW reports as KeyUnknown, by XF86 keysym.
var kittyMediaKeysyms = map[uint32]int{
	0x1008ff14: 57428, // XF86AudioPlay
	0x1008ff31: 57429, // XF86AudioPause
	0x100810a4: 57430, // XF86MediaPlayPause
	0x1008ff15: 57432, // XF86AudioStop
	0x1008ff97: 57433, // XF86AudioForward
	0x1008ff3e: 57434, // XF86AudioRewind
	0x1008ff17: 57435, // XF86AudioNext
	0x1008ff16: 57436, // XF86AudioPrev
	0x1008ff1c: 57437, // XF86AudioRecord
	0x1008ff11: 57438, // XF86AudioLowerVolume
	0x1008ff13: 57439, // XF86AudioRaiseVolume
	0x1008ff12: 57440, // XF86AudioMute
}

func kittyMods(mods glfw.ModifierKey, locks bool) uint8 {
	var m uint8
	if mods&glfw.ModShift != 0 {
		m |= KMShift
	}
	if mods&glfw.ModAlt != 0 {
		m |= KMAlt
	}
	if mods&glfw.ModControl != 0 {
		m |= KMCtrl
	}
	if mods&glfw.ModSuper != 0 {
		m |= KMSuper
	}
	if mods&glfw.ModHyper != 0 {
		m |= KMHyper
	}
	if mods&glfw.ModMeta != 0 {
		m |= KMMeta
	}
	if locks {
		if mods&glfw.ModCapsLock != 0 {
			m |= KMCapsLock
		}
		if mods&glfw.ModNumLock != 0 {
			m |= KMNumLock
		}
	}
	return m
}

// kittyCSI builds CSI code[:shifted[:base]] [; mods[:event] [; text]] final.
// Zero alternates are omitted; a letter-final key with nothing else to say
// drops its implicit 1 (CSI A).
func kittyCSI(code int, shifted, base rune, mods uint8, event int, text []rune, final byte) []byte {
	out := []byte{0x1b, '['}
	modsField := mods != 0 || event != KEvPress || len(text) > 0
	if final == 'u' || final == '~' || code != 1 || modsField {
		out = strconv.AppendInt(out, int64(code), 10)
	}
	if shifted != 0 || base != 0 {
		out = append(out, ':')
		if shifted != 0 {
			out = strconv.AppendInt(out, int64(shifted), 10)
		}
		if base != 0 {
			out = append(out, ':')
			out = strconv.AppendInt(out, int64(base), 10)
		}
	}
	if modsField {
		out = append(out, ';')
		out = strconv.AppendInt(out, int64(mods)+1, 10)
		if event != KEvPress {
			out = append(out, ':')
			out = strconv.AppendInt(out, int64(event), 10)
		}
	}
	if len(text) > 0 {
		out = append(out, ';')
		for i, r := range text {
			if i > 0 {
				out = append(out, ':')
			}
			out = strconv.AppendInt(out, int64(r), 10)
		}
	}
	return append(out, final)
}

// encodeKitty encodes a key per the kitty keyboard protocol. nil means the
// key is sent as plain text by the char callback, or not reported at all.
func encodeKitty(ev Event, m Mode) []byte {
	f := m.KittyFlags
	all := f&kittyAllKeys != 0
	event := KEvPress
	if f&kittyEvents != 0 {
		switch ev.Action {
		case glfw.Repeat:
			event = KEvRepeat
		case glfw.Release:
			event = KEvRelease
		}
	} else if ev.Action == glfw.Release {
		return nil
	}
	// Lock state is only reported when every key is an escape code, so
	// text keys stay backward compatible.
	km := kittyMods(ev.Mods, all)
	real := km &^ (KMCapsLock | KMNumLock)
	key := ev.Key

	mk, ok := kittyModKeysyms[ev.Keysym]
	if !ok {
		mk, ok = kittyModKeys[key]
	}
	if ok {
		if !all {
			return nil
		}
		if event == KEvRelease {
			km &^= mk.bit
		} else {
			km |= mk.bit
		}
		return kittyCSI(mk.code, 0, 0, km, event, nil, 'u')
	}
	if key >= glfw.KeyF13 && key <= glfw.KeyF25 {
		return kittyCSI(57376+int(key-glfw.KeyF13), 0, 0, km, event, nil, 'u')
	}
	if code, ok := kittyMediaKeysyms[ev.Keysym]; ok && key == glfw.KeyUnknown {
		return kittyCSI(code, 0, 0, km, event, nil, 'u')
	}

	numLock := ev.Mods&glfw.ModNumLock != 0
	var code, text rune
	var shifted, base rune
	switch {
	case isKPNumeric(key) && numLock:
		if key == glfw.KeyKPDecimal {
			code, text = 57409, '.'
		} else {
			code, text = 57399+rune(key-glfw.KeyKP0), '0'+rune(key-glfw.KeyKP0)
		}
	case kittyKPText[key][0] != 0:
		code, text = kittyKPText[key][0], kittyKPText[key][1]
	default:
		if fk, ok := kittyFuncKeys[key]; ok {
			if !all && (key == glfw.KeyEnter || key == glfw.KeyTab || key == glfw.KeyBackspace) {
				// Kept legacy so a shell stays usable if a program dies
				// without popping its flags; no release events either.
				if event == KEvRelease {
					return nil
				}
				if real == 0 {
					return []byte{byte(fk.num)}
				}
			}
			return kittyCSI(fk.num, 0, 0, km, event, nil, fk.final)
		}
		us := usRune(key)
		c := ev.Rune
		if c == 0 {
			c = us
		}
		if c == 0 {
			return nil
		}
		code = unicode.ToLower(c)
		text = code
		if km&KMShift != 0 {
			text = ev.Shifted
			if text == 0 {
				text = shiftedRune(code, us)
			}
			if f&kittyAlternates != 0 && text != code {
				shifted = text
			}
		}
		if ev.Mods&glfw.ModCapsLock != 0 && km&KMShift == 0 {
			text = unicode.ToUpper(text)
		}
		if f&kittyAlternates != 0 && us != 0 && us != code {
			base = us
		}
	}

	producesText := real&(KMAlt|KMCtrl|KMSuper|KMHyper|KMMeta) == 0
	if !all && producesText && event != KEvRelease {
		return nil
	}
	var textField []rune
	if all && f&kittyText != 0 && producesText && event != KEvRelease {
		textField = []rune{text}
	}
	return kittyCSI(int(code), shifted, base, km, event, textField, 'u')
}
