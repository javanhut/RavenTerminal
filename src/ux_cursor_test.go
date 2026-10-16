package main

import "testing"

func TestCopyModeHidesTerminalCursor(t *testing.T) {
	a := &App{}
	if !a.terminalCursorAllowed() {
		t.Fatal("terminal cursor hidden with no overlay open")
	}
	a.ux.copy = &copyMode{}
	if a.terminalCursorAllowed() {
		t.Fatal("terminal cursor drawn alongside the copy-mode cursor")
	}
}
