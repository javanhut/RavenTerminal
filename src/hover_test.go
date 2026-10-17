package main

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/menu"
)

func TestHoverRecheckOnContentChange(t *testing.T) {
	a := &App{settingsMenu: menu.NewMenu(), haveCursorPos: true}
	if !a.hoverRecheckNeeded(true) {
		t.Fatal("pane content changed under the pointer but the hover was not re-checked")
	}
	if a.hoverRecheckNeeded(false) {
		t.Fatal("hover re-checked with nothing changed")
	}
	a.showHelp = true
	if a.hoverRecheckNeeded(true) {
		t.Fatal("hover re-checked under the help overlay")
	}
	a.showHelp = false
	a.haveCursorPos = false
	if a.hoverRecheckNeeded(true) {
		t.Fatal("hover re-checked with no known pointer position")
	}
}
