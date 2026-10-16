package keybindings

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func TestCtrlTabReachesApplication(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()
	for _, mac := range []bool{true, false} {
		isMacOS = mac
		res := Translate(Event{Key: glfw.KeyTab, Mods: glfw.ModControl, Action: glfw.Press}, Mode{KittyFlags: 1})
		if res.Action != ActionInput || string(res.Data) != "\x1b[9;5u" {
			t.Errorf("mac=%v kitty Ctrl+Tab = %v %q, want CSI 9;5u", mac, res.Action, res.Data)
		}
		if res := TranslateKey(glfw.KeyTab, glfw.ModControl, false); res.Action != ActionInput || len(res.Data) == 0 {
			t.Errorf("mac=%v legacy Ctrl+Tab = %v %q, want it sent to the app", mac, res.Action, res.Data)
		}
	}
}

func TestLeaderChordsMatchChecklist(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()
	isMacOS = false
	cs, super := glfw.ModControl|glfw.ModShift, glfw.ModSuper
	cases := []struct {
		name string
		key  glfw.Key
		mods glfw.ModifierKey
		want KeyAction
	}{
		{"Ctrl+Shift+H hints", glfw.KeyH, cs, ActionHints},
		{"Ctrl+Shift+P palette", glfw.KeyP, cs, ActionCommandPalette},
		{"Ctrl+Shift+V paste", glfw.KeyV, cs, ActionPaste},
		{"Ctrl+Shift+D split vertical", glfw.KeyD, cs, ActionSplitVertical},
		{"Ctrl+Shift+Alt+D split horizontal", glfw.KeyD, cs | glfw.ModAlt, ActionSplitHorizontal},
		{"Ctrl+Shift+Tab next tab", glfw.KeyTab, cs, ActionNextTab},
		{"Ctrl+Shift+Alt+Tab prev tab", glfw.KeyTab, cs | glfw.ModAlt, ActionPrevTab},
		{"Super+Tab next tab", glfw.KeyTab, super, ActionNextTab},
		{"Super+Shift+Tab prev tab", glfw.KeyTab, super | glfw.ModShift, ActionPrevTab},
	}
	for _, c := range cases {
		if got := TranslateKey(c.key, c.mods, false).Action; got != c.want {
			t.Errorf("%s = %v, want %v", c.name, got, c.want)
		}
	}
}
