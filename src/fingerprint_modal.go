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
		tries := p.Fingerprint.Tries()
		switch phase {
		case fingerprint.Waiting:
			sub := hint
			if sub == "" {
				sub = "Place your finger on the sensor to continue."
			}
			s.modal = render.FingerprintModal{
				Title:    "Scan your fingerprint",
				Subtitle: sub,
				Tone:     render.FingerprintWaiting,
				Tries:    tries,
			}
		case fingerprint.Matched:
			s.modal = render.FingerprintModal{
				Title:    "Fingerprint recognised",
				Subtitle: "Continuing with sudo.",
				Tone:     render.FingerprintMatched,
				Tries:    tries,
			}
		case fingerprint.Missed:
			s.modal = render.FingerprintModal{
				Title:    "Fingerprint not recognised",
				Subtitle: "Try again, or use your password.",
				Tone:     render.FingerprintMissed,
				Tries:    tries,
			}
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
		fingerprintFallback(s)
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

// fingerprintFallback takes the modal down and, while the helper is still
// waiting, sends it the Enter that switches sudo to the password prompt.
func fingerprintFallback(s fingerprintState) {
	if s.phase == fingerprint.Waiting {
		_ = s.pane.Write([]byte("\r"))
	}
	s.pane.Fingerprint.Dismiss()
}

// fingerprintClick handles a mouse press while the modal is up and reports
// whether it consumed it; the modal is modal, so every press on it or on the
// dimmed window around it is. (x, y) are in framebuffer pixels.
//
// Authentication Settings only hides the modal: the helper keeps watching the
// sensor while the settings menu is open, and a finger still gets through.
func (a *App) fingerprintClick(width, height int, x, y float32) bool {
	s := a.fingerprintModal(time.Now())
	if !s.visible {
		return false
	}
	switch a.renderer.FingerprintModalHit(s.modal, width, height, x, y) {
	case render.FingerprintHitClose, render.FingerprintHitPassword:
		fingerprintFallback(s)
	case render.FingerprintHitSettings:
		s.pane.Fingerprint.Dismiss()
		a.searchPanel.Open = false
		a.aiPanel.Open = false
		a.settingsMenu.Open()
	}
	return true
}
