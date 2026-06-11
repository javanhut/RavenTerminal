package keybindings

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// On macOS, Shift+Tab must reach terminal apps as back-tab (CSI Z) — TUIs
// like Claude Code bind it — and pane cycling moves to Cmd+Shift+Tab.
func TestShiftTabMacOS(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()
	isMacOS = true

	res := TranslateKey(glfw.KeyTab, glfw.ModShift, false)
	if res.Action != ActionInput || string(res.Data) != "\x1b[Z" {
		t.Errorf("macOS Shift+Tab = %v %q, want back-tab CSI Z", res.Action, res.Data)
	}

	res = TranslateKey(glfw.KeyTab, glfw.ModShift|glfw.ModSuper, false)
	if res.Action != ActionNextPane {
		t.Errorf("macOS Cmd+Shift+Tab = %v, want ActionNextPane", res.Action)
	}
}

// Linux behaves the same: Shift+Tab is back-tab for the app (nvim binds
// it), Super+Shift+Tab cycles panes.
func TestShiftTabOtherPlatforms(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()
	isMacOS = false

	res := TranslateKey(glfw.KeyTab, glfw.ModShift, false)
	if res.Action != ActionInput || string(res.Data) != "\x1b[Z" {
		t.Errorf("Shift+Tab = %v %q, want back-tab CSI Z", res.Action, res.Data)
	}

	res = TranslateKey(glfw.KeyTab, glfw.ModShift|glfw.ModSuper, false)
	if res.Action != ActionNextPane {
		t.Errorf("Super+Shift+Tab = %v, want ActionNextPane", res.Action)
	}
}

// Ctrl+Tab / Ctrl+Shift+Tab tab cycling is unaffected on both platforms.
func TestCtrlTabCyclingUnchanged(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()
	for _, mac := range []bool{true, false} {
		isMacOS = mac
		if res := TranslateKey(glfw.KeyTab, glfw.ModControl, false); res.Action != ActionNextTab {
			t.Errorf("mac=%v Ctrl+Tab = %v, want ActionNextTab", mac, res.Action)
		}
		if res := TranslateKey(glfw.KeyTab, glfw.ModControl|glfw.ModShift, false); res.Action != ActionPrevTab {
			t.Errorf("mac=%v Ctrl+Shift+Tab = %v, want ActionPrevTab", mac, res.Action)
		}
	}
}
