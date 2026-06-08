package keybindings

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func TestEncodeKittyKeyPlain(t *testing.T) {
	// 'a' with no modifiers -> CSI 97 u
	got := string(EncodeKittyKey('a', 0, KEvPress, false))
	if got != "\x1b[97u" {
		t.Fatalf("plain a = %q, want %q", got, "\x1b[97u")
	}
}

func TestEncodeKittyKeyCtrl(t *testing.T) {
	// Ctrl+a -> CSI 97 ; 5 u  (mods 4 -> param 5)
	got := string(EncodeKittyKey('a', KMCtrl, KEvPress, false))
	if got != "\x1b[97;5u" {
		t.Fatalf("ctrl a = %q, want %q", got, "\x1b[97;5u")
	}
}

func TestEncodeKittyKeyRelease(t *testing.T) {
	// Release event with report-events on -> CSI 97 ; 1 : 3 u
	got := string(EncodeKittyKey('a', 0, KEvRelease, true))
	if got != "\x1b[97;1:3u" {
		t.Fatalf("release a = %q, want %q", got, "\x1b[97;1:3u")
	}
	// With report-events off, release is not annotated.
	got = string(EncodeKittyKey('a', 0, KEvRelease, false))
	if got != "\x1b[97u" {
		t.Fatalf("release a (no report) = %q, want %q", got, "\x1b[97u")
	}
}

func TestTranslateKeyKittyLetters(t *testing.T) {
	got := string(TranslateKeyKitty(glfw.KeyA, glfw.ModControl, KEvPress, 0x01))
	if got != "\x1b[97;5u" {
		t.Fatalf("Ctrl+A = %q, want %q", got, "\x1b[97;5u")
	}
}

func TestTranslateKeyKittyFunctional(t *testing.T) {
	// Escape -> CSI 27 u
	got := string(TranslateKeyKitty(glfw.KeyEscape, 0, KEvPress, 0x01))
	if got != "\x1b[27u" {
		t.Fatalf("Escape = %q, want %q", got, "\x1b[27u")
	}
}
