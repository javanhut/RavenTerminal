package keybindings

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// The Super (Cmd) layer must be identical on macOS and Linux so muscle
// memory transfers between machines.
func TestSuperLayerEquivalentAcrossPlatforms(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()

	super := glfw.ModSuper
	superShift := glfw.ModSuper | glfw.ModShift
	cases := []struct {
		name string
		key  glfw.Key
		mods glfw.ModifierKey
		want KeyAction
	}{
		{"Super+Q exit", glfw.KeyQ, super, ActionExit},
		{"Super+C copy", glfw.KeyC, super, ActionCopy},
		{"Super+V paste", glfw.KeyV, super, ActionPaste},
		{"Super+T new tab", glfw.KeyT, super, ActionNewTab},
		{"Super+X close tab", glfw.KeyX, super, ActionCloseTab},
		{"Super+W close pane", glfw.KeyW, super, ActionClosePane},
		{"Super+D split vertical", glfw.KeyD, super, ActionSplitVertical},
		{"Super+Shift+D split horizontal", glfw.KeyD, superShift, ActionSplitHorizontal},
		{"Super+K help", glfw.KeyK, super, ActionShowHelp},
		{"Super+Shift+] next tab", glfw.KeyRightBracket, superShift, ActionNextTab},
		{"Super+Shift+[ prev tab", glfw.KeyLeftBracket, superShift, ActionPrevTab},
		{"Super+] next pane", glfw.KeyRightBracket, super, ActionNextPane},
		{"Super+[ prev pane", glfw.KeyLeftBracket, super, ActionPrevPane},
		{"Super+Shift+Tab cycle panes", glfw.KeyTab, superShift, ActionNextPane},
		{"Super+= zoom in", glfw.KeyEqual, super, ActionZoomIn},
		{"Super+- zoom out", glfw.KeyMinus, super, ActionZoomOut},
		{"Super+0 zoom reset", glfw.Key0, super, ActionZoomReset},
		{"Super+S settings", glfw.KeyS, super, ActionOpenMenu},
		{"Super+F search panel", glfw.KeyF, super, ActionToggleSearchPanel},
		{"Super+A ai panel", glfw.KeyA, super, ActionToggleAIPanel},
		{"Super+R resize mode", glfw.KeyR, super, ActionToggleResizeMode},
		{"Super+1 select tab", glfw.Key1, super, ActionSelectTab},
	}

	for _, mac := range []bool{true, false} {
		isMacOS = mac
		for _, c := range cases {
			res := TranslateKey(c.key, c.mods, false)
			if res.Action != c.want {
				t.Errorf("mac=%v %s = %v, want %v", mac, c.name, res.Action, c.want)
			}
		}
	}
}

// Linux keeps its conventional Ctrl+Shift aliases alongside the Super layer.
func TestLinuxCtrlShiftAliases(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()
	isMacOS = false

	cs := glfw.ModControl | glfw.ModShift
	cases := []struct {
		name string
		key  glfw.Key
		want KeyAction
	}{
		{"Ctrl+Shift+C copy", glfw.KeyC, ActionCopy},
		{"Ctrl+Shift+P paste", glfw.KeyP, ActionPaste},
		{"Ctrl+Shift+T new tab", glfw.KeyT, ActionNewTab},
		{"Ctrl+Shift+V split vertical", glfw.KeyV, ActionSplitVertical},
		{"Ctrl+Shift+H split horizontal", glfw.KeyH, ActionSplitHorizontal},
		{"Ctrl+Shift+] next pane", glfw.KeyRightBracket, ActionNextPane},
		{"Ctrl+Shift+[ prev pane", glfw.KeyLeftBracket, ActionPrevPane},
		{"Ctrl+Shift+K help", glfw.KeyK, ActionShowHelp},
		{"Ctrl+Shift+S settings", glfw.KeyS, ActionOpenMenu},
	}
	for _, c := range cases {
		res := TranslateKey(c.key, cs, false)
		if res.Action != c.want {
			t.Errorf("%s = %v, want %v", c.name, res.Action, c.want)
		}
	}
}

// Plain-Ctrl control characters must keep flowing to the shell on both
// platforms (the leader layers must not capture them).
func TestControlCharsUntouched(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()
	for _, mac := range []bool{true, false} {
		isMacOS = mac
		res := TranslateKey(glfw.KeyC, glfw.ModControl, false)
		if res.Action != ActionInput || len(res.Data) != 1 || res.Data[0] != 0x03 {
			t.Errorf("mac=%v Ctrl+C = %v %v, want input 0x03", mac, res.Action, res.Data)
		}
	}
}
