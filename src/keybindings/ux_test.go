package keybindings

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func TestUXLeaderActions(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()
	isMacOS = false

	super, cs := glfw.ModSuper, glfw.ModControl|glfw.ModShift
	superShift, csAlt := super|glfw.ModShift, cs|glfw.ModAlt
	cases := []struct {
		name string
		key  glfw.Key
		mods glfw.ModifierKey
		want KeyAction
	}{
		{"Super+Up prev prompt", glfw.KeyUp, super, ActionPrevPrompt},
		{"Ctrl+Shift+Up prev prompt", glfw.KeyUp, cs, ActionPrevPrompt},
		{"Super+Down next prompt", glfw.KeyDown, super, ActionNextPrompt},
		{"Ctrl+Shift+Down next prompt", glfw.KeyDown, cs, ActionNextPrompt},
		{"Super+Shift+C copy last output", glfw.KeyC, superShift, ActionCopyLastOutput},
		{"Ctrl+Shift+Alt+C copy last output", glfw.KeyC, csAlt, ActionCopyLastOutput},
		{"Super+H hints", glfw.KeyH, super, ActionHints},
		{"Super+E hints", glfw.KeyE, super, ActionHints},
		{"Ctrl+Shift+E hints", glfw.KeyE, cs, ActionHints},
		{"Super+Space copy mode", glfw.KeySpace, super, ActionCopyMode},
		{"Ctrl+Shift+Space copy mode", glfw.KeySpace, cs, ActionCopyMode},
		{"Super+Shift+V copy mode", glfw.KeyV, superShift, ActionCopyMode},
		{"Ctrl+Shift+Alt+V copy mode", glfw.KeyV, csAlt, ActionCopyMode},
		{"Super+P palette", glfw.KeyP, super, ActionCommandPalette},
		{"Super+Shift+P palette", glfw.KeyP, superShift, ActionCommandPalette},
		{"Ctrl+Shift+Alt+P palette", glfw.KeyP, csAlt, ActionCommandPalette},
		{"Super+Z zoom pane", glfw.KeyZ, super, ActionTogglePaneZoom},
		{"Ctrl+Shift+Z zoom pane", glfw.KeyZ, cs, ActionTogglePaneZoom},
		{"Super+B broadcast", glfw.KeyB, super, ActionToggleBroadcast},
		{"Ctrl+Shift+B broadcast", glfw.KeyB, cs, ActionToggleBroadcast},
		{"Super+Shift+H pager", glfw.KeyH, superShift, ActionScrollbackPager},
		{"Ctrl+Shift+Alt+H pager", glfw.KeyH, csAlt, ActionScrollbackPager},
		// Existing chords keep their meaning.
		{"Ctrl+Shift+C copy", glfw.KeyC, cs, ActionCopy},
		{"Super+C copy", glfw.KeyC, super, ActionCopy},
		{"Ctrl+Shift+V paste", glfw.KeyV, cs, ActionPaste},
		{"Super+V paste", glfw.KeyV, super, ActionPaste},
		{"Ctrl+Shift+H hints", glfw.KeyH, cs, ActionHints},
		{"Ctrl+Shift+P palette", glfw.KeyP, cs, ActionCommandPalette},
		{"Super+Shift+F find", glfw.KeyF, superShift, ActionFindInScrollback},
	}
	for _, c := range cases {
		if got := TranslateKey(c.key, c.mods, false).Action; got != c.want {
			t.Errorf("%s = %v, want %v", c.name, got, c.want)
		}
	}
	// Plain Ctrl and plain Shift stay with the application.
	for _, k := range []glfw.Key{glfw.KeyUp, glfw.KeyB, glfw.KeyZ, glfw.KeyE, glfw.KeyP} {
		if got := TranslateKey(k, glfw.ModControl, false).Action; got != ActionInput && got != ActionNone {
			t.Errorf("Ctrl+%v = %v, want it sent to the app", k, got)
		}
	}
	if got := TranslateKey(glfw.KeyUp, glfw.ModShift, false).Action; got != ActionScrollUpLine {
		t.Errorf("Shift+Up = %v, want ScrollUpLine", got)
	}
}

func TestActionTableCoversPaletteActions(t *testing.T) {
	seen := map[KeyAction]bool{}
	for _, a := range Actions() {
		if a.Name == "" || a.Chord == "" {
			t.Errorf("action %v missing name or chord: %+v", a.Action, a)
		}
		if seen[a.Action] {
			t.Errorf("action %v listed twice", a.Action)
		}
		seen[a.Action] = true
	}
	for _, want := range []KeyAction{ActionNewTab, ActionSplitVertical, ActionCopyMode, ActionHints,
		ActionTogglePaneZoom, ActionToggleBroadcast, ActionScrollbackPager, ActionCopyLastOutput,
		ActionPrevPrompt, ActionNextPrompt, ActionFindInScrollback, ActionOpenMenu} {
		if !seen[want] {
			t.Errorf("Actions() lacks %v", want)
		}
	}
	if seen[ActionCommandPalette] || seen[ActionInput] || seen[ActionNone] || seen[ActionSelectTab] {
		t.Error("Actions() must not list the palette itself, input or parameterised actions")
	}
}
