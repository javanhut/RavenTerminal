package main

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/parser"
)

type captureWriter struct{ data []byte }

func (w *captureWriter) Write(p []byte) error {
	w.data = append(w.data, p...)
	return nil
}

// writePaste must normalize newlines to CR, and only when the application has
// enabled bracketed paste (?2004) wrap the text in \x1b[200~ ... \x1b[201~,
// stripping any embedded end marker first (paste-injection guard).
func TestWritePaste(t *testing.T) {
	t.Run("plain paste no markers", func(t *testing.T) {
		term := parser.NewTerminal(20, 5)
		w := &captureWriter{}
		writePaste(term, w, "hello\r\nworld\n!")
		if got, want := string(w.data), "hello\rworld\r!"; got != want {
			t.Fatalf("plain paste = %q, want %q", got, want)
		}
	})

	t.Run("bracketed paste wraps", func(t *testing.T) {
		term := parser.NewTerminal(20, 5)
		term.Process([]byte("\x1b[?2004h"))
		w := &captureWriter{}
		writePaste(term, w, "hello\nworld")
		if got, want := string(w.data), "\x1b[200~hello\rworld\x1b[201~"; got != want {
			t.Fatalf("bracketed paste = %q, want %q", got, want)
		}
	})

	t.Run("embedded end marker stripped", func(t *testing.T) {
		term := parser.NewTerminal(20, 5)
		term.Process([]byte("\x1b[?2004h"))
		w := &captureWriter{}
		writePaste(term, w, "evil\x1b[201~rm -rf /")
		if got, want := string(w.data), "\x1b[200~evilrm -rf /\x1b[201~"; got != want {
			t.Fatalf("injection paste = %q, want %q", got, want)
		}
	})
}
