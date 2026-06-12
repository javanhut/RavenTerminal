package parser

import "testing"

func TestKittyPushPopQuery(t *testing.T) {
	term := NewTerminal(10, 1)
	out := captureResponses(term)

	if term.KittyKeyboardFlags() != 0 {
		t.Fatal("flags should start at 0 (legacy)")
	}
	term.Process([]byte("\x1b[>5u")) // push flags 5
	if term.KittyKeyboardFlags() != 5 {
		t.Fatalf("after push flags = %d, want 5", term.KittyKeyboardFlags())
	}
	term.Process([]byte("\x1b[?u")) // query
	if string(*out) != "\x1b[?5u" {
		t.Fatalf("query reply = %q, want %q", string(*out), "\x1b[?5u")
	}
	term.Process([]byte("\x1b[<1u")) // pop 1
	if term.KittyKeyboardFlags() != 0 {
		t.Fatalf("after pop flags = %d, want 0", term.KittyKeyboardFlags())
	}
}

func TestKittySetModes(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b[>1u"))   // push flags 1
	term.Process([]byte("\x1b[=6;2u")) // set bits: add 6 -> 7
	if term.KittyKeyboardFlags() != 7 {
		t.Fatalf("set-bits flags = %d, want 7", term.KittyKeyboardFlags())
	}
	term.Process([]byte("\x1b[=1;3u")) // clear bits: remove 1 -> 6
	if term.KittyKeyboardFlags() != 6 {
		t.Fatalf("clear-bits flags = %d, want 6", term.KittyKeyboardFlags())
	}
	term.Process([]byte("\x1b[=4;1u")) // replace -> 4
	if term.KittyKeyboardFlags() != 4 {
		t.Fatalf("replace flags = %d, want 4", term.KittyKeyboardFlags())
	}
}

func TestKittyStackResetOnRIS(t *testing.T) {
	term := NewTerminal(10, 1)
	term.Process([]byte("\x1b[>9u"))
	term.Process([]byte("\x1bc")) // RIS
	if term.KittyKeyboardFlags() != 0 {
		t.Fatalf("flags after RIS = %d, want 0", term.KittyKeyboardFlags())
	}
}

func TestUnprefixedURestoresCursor(t *testing.T) {
	// The unprefixed CSI u must remain RCP (restore cursor), not kitty.
	term := NewTerminal(10, 5)
	term.Process([]byte("\x1b[3;4H")) // move to row3 col4
	term.Process([]byte("\x1b[s"))    // save cursor
	term.Process([]byte("\x1b[1;1H")) // move home
	term.Process([]byte("\x1b[u"))    // restore cursor
	col, row := term.Grid.GetCursor()
	if col != 3 || row != 2 {
		t.Fatalf("RCP restored to (%d,%d), want (3,2)", col, row)
	}
}
