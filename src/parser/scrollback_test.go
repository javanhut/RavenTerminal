package parser

import (
	"strings"
	"testing"
)

func TestSetMaxScrollbackForwards(t *testing.T) {
	term := NewTerminal(10, 3)
	term.SetMaxScrollback(5)
	term.Process([]byte(strings.Repeat("x\r\n", 20)))
	if n := term.Grid.ScrollbackLen(); n != 5 {
		t.Fatalf("scrollback = %d, want 5", n)
	}
	term.Process([]byte("\x1b[?1049h"))
	term.SetMaxScrollback(2)
	term.Process([]byte("\x1b[?1049l"))
	if n := term.Grid.ScrollbackLen(); n != 2 {
		t.Fatalf("main scrollback after alt-screen change = %d, want 2", n)
	}
}

func TestDECSCRemappedByReflow(t *testing.T) {
	term := NewTerminal(10, 4)
	term.Process([]byte("abcdefghijklmno\r\nzz\x1b[2;4H\x1b7\x1b[H"))
	term.Resize(5, 4)
	term.Process([]byte("\x1b8X"))
	if got := term.Grid.VisibleText(); !strings.Contains(got, "klmXo") {
		t.Fatalf("DECRC after reflow wrote at wrong cell:\n%s", got)
	}
}

func TestAltScreenSavedCursorRemappedByReflow(t *testing.T) {
	term := NewTerminal(10, 4)
	term.Process([]byte("abcdefghijklmno\r\nzz\x1b[2;4H\x1b[?1049h"))
	term.Resize(5, 4)
	term.Process([]byte("\x1b[?1049lX"))
	if got := term.Grid.VisibleText(); !strings.Contains(got, "klmXo") {
		t.Fatalf("1049 restore after reflow wrote at wrong cell:\n%s", got)
	}
}

func TestMode2027DrivesGrid(t *testing.T) {
	term := NewTerminal(10, 2)
	term.Process([]byte("🇺🇸"))
	if c, _ := term.Grid.GetCursor(); c != 2 {
		t.Fatalf("mode on: cursor %d, want 2", c)
	}
	term.Process([]byte("\r\x1b[?2027l\x1b[?1049h🇺🇸"))
	if c, _ := term.Grid.GetCursor(); c != 4 {
		t.Fatalf("mode off on alt screen: cursor %d, want 4", c)
	}
}
