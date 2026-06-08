package keybindings

import (
	"strconv"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// Kitty keyboard modifier bits (param value is 1 + this bitmask).
const (
	KMShift uint8 = 1 << 0
	KMAlt   uint8 = 1 << 1
	KMCtrl  uint8 = 1 << 2
	KMSuper uint8 = 1 << 3
)

// Kitty event types.
const (
	KEvPress   = 1
	KEvRepeat  = 2
	KEvRelease = 3
)

// kittyMods converts GLFW modifiers to the kitty modifier bitmask.
func kittyMods(mods glfw.ModifierKey) uint8 {
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
	return m
}

// EncodeKittyKey builds a CSI-u key event:
//
//	CSI <key> [ ; <1+mods> [ : <event> ] ] u
//
// The modifiers section is emitted when any modifier is set or when an event
// type other than press must be reported (requires the report-events flag).
// reportEvents controls whether non-press events are encoded.
func EncodeKittyKey(key rune, mods uint8, event int, reportEvents bool) []byte {
	out := []byte{0x1b, '['}
	out = append(out, []byte(strconv.Itoa(int(key)))...)

	needEvent := reportEvents && event != KEvPress
	if mods != 0 || needEvent {
		out = append(out, ';')
		out = append(out, []byte(strconv.Itoa(int(mods)+1))...)
		if needEvent {
			out = append(out, ':')
			out = append(out, []byte(strconv.Itoa(event))...)
		}
	}
	out = append(out, 'u')
	return out
}

// kittyFunctionalKey maps a GLFW key to its kitty CSI-u code point, returning
// ok=false for keys that should be encoded by their text codepoint instead.
func kittyFunctionalKey(key glfw.Key) (rune, bool) {
	switch key {
	case glfw.KeyEscape:
		return 27, true
	case glfw.KeyEnter:
		return 13, true
	case glfw.KeyTab:
		return 9, true
	case glfw.KeyBackspace:
		return 127, true
	case glfw.KeyInsert:
		return 2, true // placeholder functional codes; see kitty spec for full table
	case glfw.KeyDelete:
		return 3, true
	case glfw.KeyHome:
		return 7, true
	case glfw.KeyEnd:
		return 8, true
	case glfw.KeyPageUp:
		return 5, true
	case glfw.KeyPageDown:
		return 6, true
	}
	return 0, false
}

// TranslateKeyKitty encodes a key event in the Kitty keyboard protocol. It
// returns nil when the key has no kitty encoding (caller should fall back to
// legacy or the text/char callback). event is one of KEv*.
func TranslateKeyKitty(key glfw.Key, mods glfw.ModifierKey, event int, flags uint8) []byte {
	km := kittyMods(mods)
	reportEvents := flags&0x02 != 0 // KittyReportEvents

	if code, ok := kittyFunctionalKey(key); ok {
		return EncodeKittyKey(code, km, event, reportEvents)
	}
	// Printable keys: GLFW key constants for letters/digits map to ASCII.
	if r := asciiForKey(key); r != 0 {
		return EncodeKittyKey(r, km, event, reportEvents)
	}
	return nil
}

// asciiForKey returns the base (lowercase/unshifted) ASCII rune for a GLFW key,
// or 0 if it isn't a simple printable key.
func asciiForKey(key glfw.Key) rune {
	switch {
	case key >= glfw.KeyA && key <= glfw.KeyZ:
		return rune('a' + (key - glfw.KeyA))
	case key >= glfw.Key0 && key <= glfw.Key9:
		return rune('0' + (key - glfw.Key0))
	case key == glfw.KeySpace:
		return ' '
	}
	return 0
}
