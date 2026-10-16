package keybindings

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// On macOS, Shift+Tab must reach terminal apps as back-tab (CSI Z) — TUIs
// like Claude Code bind it — and Cmd+Shift+Tab cycles tabs backwards.
func TestShiftTabMacOS(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()
	isMacOS = true

	res := TranslateKey(glfw.KeyTab, glfw.ModShift, false)
	if res.Action != ActionInput || string(res.Data) != "\x1b[Z" {
		t.Errorf("macOS Shift+Tab = %v %q, want back-tab CSI Z", res.Action, res.Data)
	}

	res = TranslateKey(glfw.KeyTab, glfw.ModShift|glfw.ModSuper, false)
	if res.Action != ActionPrevTab {
		t.Errorf("macOS Cmd+Shift+Tab = %v, want ActionPrevTab", res.Action)
	}
}

// Linux behaves the same: Shift+Tab is back-tab for the app (nvim binds
// it), Super+Shift+Tab cycles tabs backwards.
func TestShiftTabOtherPlatforms(t *testing.T) {
	prev := isMacOS
	defer func() { isMacOS = prev }()
	isMacOS = false

	res := TranslateKey(glfw.KeyTab, glfw.ModShift, false)
	if res.Action != ActionInput || string(res.Data) != "\x1b[Z" {
		t.Errorf("Shift+Tab = %v %q, want back-tab CSI Z", res.Action, res.Data)
	}

	res = TranslateKey(glfw.KeyTab, glfw.ModShift|glfw.ModSuper, false)
	if res.Action != ActionPrevTab {
		t.Errorf("Super+Shift+Tab = %v, want ActionPrevTab", res.Action)
	}
}
