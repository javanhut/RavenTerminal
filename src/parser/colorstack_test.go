package parser

import (
	"strings"
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

func TestXTPushPopColorsRestoresDynamicColors(t *testing.T) {
	term := NewTerminal(10, 2)
	feed(term, "\x1b]4;1;#112233\x1b\\\x1b]10;#445566\x1b\\")
	feed(term, "\x1b[#P")
	feed(term, "\x1b]4;1;#aaaaaa\x1b\\\x1b]10;#bbbbbb\x1b\\\x1b]11;#cccccc\x1b\\\x1b]4;2;#dddddd\x1b\\")
	feed(term, "\x1b[#Q")
	if c, ok := term.PaletteOverride(1); !ok || c != grid.RGBColor(0x11, 0x22, 0x33) {
		t.Fatalf("palette 1 = %+v %v", c, ok)
	}
	if _, ok := term.PaletteOverride(2); ok {
		t.Fatal("palette 2 override survived the pop")
	}
	if c := term.ForegroundColor(); c != grid.RGBColor(0x44, 0x55, 0x66) {
		t.Fatalf("fg = %+v", c)
	}
	if c := term.BackgroundColor(); c.Type != grid.ColorDefault {
		t.Fatalf("bg = %+v, want default", c)
	}
}

func TestXTPushColorsIsASnapshot(t *testing.T) {
	term := NewTerminal(10, 2)
	feed(term, "\x1b]4;1;#112233\x1b\\\x1b[#P\x1b]4;1;#aaaaaa\x1b\\\x1b[#P\x1b]4;1;#bbbbbb\x1b\\")
	feed(term, "\x1b[#Q")
	if c, _ := term.PaletteOverride(1); c != grid.RGBColor(0xaa, 0xaa, 0xaa) {
		t.Fatalf("first pop = %+v", c)
	}
	feed(term, "\x1b[#Q")
	if c, _ := term.PaletteOverride(1); c != grid.RGBColor(0x11, 0x22, 0x33) {
		t.Fatalf("second pop = %+v", c)
	}
	feed(term, "\x1b[#Q") // empty stack: no-op
	if c, _ := term.PaletteOverride(1); c != grid.RGBColor(0x11, 0x22, 0x33) {
		t.Fatalf("pop on empty stack changed colours: %+v", c)
	}
}

func TestXTReportColors(t *testing.T) {
	term := NewTerminal(10, 2)
	if got := reply(term, "\x1b[#R"); got != "\x1b[0;0#Q" {
		t.Fatalf("empty report = %q", got)
	}
	feed(term, "\x1b[#P\x1b[#P")
	if got := reply(term, "\x1b[#R"); got != "\x1b[2;2#Q" {
		t.Fatalf("after two pushes = %q", got)
	}
	feed(term, "\x1b[#Q")
	if got := reply(term, "\x1b[#R"); got != "\x1b[1;2#Q" {
		t.Fatalf("after pop = %q", got)
	}
}

func TestXTPushColorsSlots(t *testing.T) {
	term := NewTerminal(10, 2)
	feed(term, "\x1b]11;#010203\x1b\\\x1b[3#P\x1b]11;#040506\x1b\\")
	if got := reply(term, "\x1b[#R"); got != "\x1b[3;3#Q" {
		t.Fatalf("slot push report = %q", got)
	}
	feed(term, "\x1b[3#Q")
	if c := term.BackgroundColor(); c != grid.RGBColor(1, 2, 3) {
		t.Fatalf("slot pop bg = %+v", c)
	}
	if got := reply(term, "\x1b[#R"); got != "\x1b[2;3#Q" {
		t.Fatalf("after slot pop = %q", got)
	}
	feed(term, "\x1b[11#P\x1b[2#Q") // out-of-range slot ignored; empty slot 2 restores nothing
	if c := term.BackgroundColor(); c != grid.RGBColor(1, 2, 3) {
		t.Fatalf("empty-slot pop changed bg: %+v", c)
	}
	for range 12 {
		feed(term, "\x1b[#P")
	}
	if got := reply(term, "\x1b[#R"); !strings.HasPrefix(got, "\x1b[10;10") {
		t.Fatalf("stack not capped at 10: %q", got)
	}
}

func TestOSCResetsStillGoToThemeAfterPush(t *testing.T) {
	term := NewTerminal(10, 2)
	feed(term, "\x1b]4;1;#112233\x1b\\\x1b]10;#445566\x1b\\\x1b[#P")
	feed(term, "\x1b]104\x1b\\\x1b]110\x1b\\")
	if _, ok := term.PaletteOverride(1); ok {
		t.Fatal("OSC 104 did not reset to the theme")
	}
	if c := term.ForegroundColor(); c.Type != grid.ColorDefault {
		t.Fatal("OSC 110 did not reset to the theme")
	}
	feed(term, "\x1b]113\x07\x1b]119\x07") // other dynamic resets are accepted
	feed(term, "\x1b[#Q")
	if c, _ := term.PaletteOverride(1); c != grid.RGBColor(0x11, 0x22, 0x33) {
		t.Fatal("pop after reset did not restore")
	}
	feed(term, "\x1b[#P\x1bc")
	if got := reply(term, "\x1b[#R"); got != "\x1b[0;0#Q" {
		t.Fatalf("RIS kept the colour stack: %q", got)
	}
}
