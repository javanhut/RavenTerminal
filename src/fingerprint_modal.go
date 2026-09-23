package main

import (
	"time"

	"github.com/javanhut/RavenTerminal/src/fingerprint"
	"github.com/javanhut/RavenTerminal/src/render"
	"github.com/javanhut/RavenTerminal/src/tab"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// fingerprintState is the modal for the pane currently showing one, if any.
type fingerprintState struct {
	pane    *tab.Pane
	phase   fingerprint.Phase
	modal   render.FingerprintModal
	visible bool
}

// fingerprintModal finds a pane in the active tab whose sudo is waiting on the
// fingerprint reader. The modal is off when the user turned it off in
// Settings; the helper's text is still in the pane either way.
func (a *App) fingerprintModal(now time.Time) fingerprintState {
	if a.settingsMenu.Config != nil && !a.settingsMenu.Config.FingerprintSudo {
		return fingerprintState{}
	}
	activeTab := a.tabManager.ActiveTab()
	if activeTab == nil {
		return fingerprintState{}
	}
	for _, p := range activeTab.GetPanes() {
		if p == nil || p.Fingerprint == nil {
			continue
		}
		phase, hint, ok := p.Fingerprint.State(now)
		if !ok {
			continue
		}
		s := fingerprintState{pane: p, phase: phase, visible: true}
		switch phase {
		case fingerprint.Waiting:
			sub := hint
			if sub == "" {
				sub = "sudo is asking to confirm it's you"
			}
			s.modal = render.FingerprintModal{
				Title:    "Touch the fingerprint sensor",
				Subtitle: sub,
				Footer:   "Enter or Esc: type your password instead",
				Tone:     render.FingerprintWaiting,
			}
		case fingerprint.Matched:
			s.modal = render.FingerprintModal{Title: "Fingerprint recognised", Tone: render.FingerprintMatched}
		case fingerprint.Missed:
			s.modal = render.FingerprintModal{Title: "Fingerprint not recognised", Tone: render.FingerprintMissed}
		}
		return s
	}
	return fingerprintState{}
}

// fingerprintKey handles a key press while the modal is up and reports whether
// it consumed the key. Escape stands in for the Enter the helper asks for,
// since Escape is what dismisses a modal; Enter and Ctrl+C reach the pane as
// usual and just take the modal down with them.
//
// Only a Waiting helper is sent anything. After a verdict the helper may
// already have handed over to sudo's own password prompt, where a stray Enter
// would submit an empty password and spend one of sudo's attempts.
func (a *App) fingerprintKey(key glfw.Key, mods glfw.ModifierKey) bool {
	s := a.fingerprintModal(time.Now())
	if !s.visible {
		return false
	}
	switch {
	case key == glfw.KeyEscape:
		if s.phase == fingerprint.Waiting {
			_ = s.pane.Write([]byte("\r"))
		}
		s.pane.Fingerprint.Dismiss()
		return true
	case key == glfw.KeyEnter, key == glfw.KeyKPEnter,
		key == glfw.KeyC && mods&glfw.ModControl != 0:
		s.pane.Fingerprint.Dismiss()
	case s.phase != fingerprint.Waiting:
		// A verdict is only on screen to be read; typing past it means the
		// person has moved on.
		s.pane.Fingerprint.Dismiss()
	}
	return false
}
