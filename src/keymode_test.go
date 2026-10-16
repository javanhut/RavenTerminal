package main

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/javanhut/RavenTerminal/src/keybindings"
	"github.com/javanhut/RavenTerminal/src/parser"
)

// The key encoder must see the modes a program set through escape sequences.
func TestKeyModeForFollowsTerminal(t *testing.T) {
	term := parser.NewTerminal(20, 5)
	if m := keyModeFor(term); m != (keybindings.Mode{}) {
		t.Fatalf("fresh terminal mode = %+v, want zero", m)
	}
	term.Process([]byte("\x1b[?1h\x1b=\x1b[?1049h\x1b[?1000h\x1b[>4;2m\x1b[>11u\x1b[?1036l\x1b[?1039l"))
	want := keybindings.Mode{AppCursor: true, AppKeypad: true, AltScreen: true, MouseReporting: true,
		ModifyOtherKeys: 2, KittyFlags: 11, AltNoEscape: true}
	if m := keyModeFor(term); m != want {
		t.Fatalf("mode = %+v, want %+v", m, want)
	}

	// End to end: in the alternate screen Shift+PgUp is the program's key.
	term2 := parser.NewTerminal(20, 5)
	term2.Process([]byte("\x1b[?1049h"))
	res := keybindings.Translate(keybindings.Event{Key: glfw.KeyPageUp, Mods: glfw.ModShift, Action: glfw.Press}, keyModeFor(term2))
	if res.Action != keybindings.ActionInput || string(res.Data) != "\x1b[5;2~" {
		t.Fatalf("alt-screen Shift+PgUp = %v %q", res.Action, res.Data)
	}
	term2.Process([]byte("\x1b[?1049l"))
	if res := keybindings.Translate(keybindings.Event{Key: glfw.KeyPageUp, Mods: glfw.ModShift, Action: glfw.Press}, keyModeFor(term2)); res.Action != keybindings.ActionScrollUp {
		t.Fatalf("main-screen Shift+PgUp = %v", res.Action)
	}
}
