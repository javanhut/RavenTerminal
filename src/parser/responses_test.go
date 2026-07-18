package parser

import (
	"testing"
	"time"
)

// TestDeviceAttributesReplies covers DA1/DA2 (byte-identical regression) and
// tertiary DA (CSI = c), which must reply in DECRPTUI format.
func TestDeviceAttributesReplies(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"DA1", "\x1b[c", "\x1b[?62;22c"},
		{"DA1 explicit zero", "\x1b[0c", "\x1b[?62;22c"},
		{"DA2", "\x1b[>c", "\x1b[>0;136;0c"},
		{"DA3", "\x1b[=c", "\x1bP!|00000000\x1b\\"},
		{"DA3 explicit zero", "\x1b[=0c", "\x1bP!|00000000\x1b\\"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			term := NewTerminal(10, 3)
			out := captureResponses(term)
			term.Process([]byte(tc.input))
			if string(*out) != tc.want {
				t.Fatalf("reply = %q, want %q", string(*out), tc.want)
			}
		})
	}
}

// TestXTVersion covers CSI > q (XTVERSION): reply with DCS > | name ST, and
// never treat it as DECSCUSR.
func TestXTVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"bare", "\x1b[>q"},
		{"explicit zero", "\x1b[>0q"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			term := NewTerminal(10, 3)
			out := captureResponses(term)
			term.Process([]byte(tc.input))
			if string(*out) != "\x1bP>|RavenTerminal\x1b\\" {
				t.Fatalf("XTVERSION reply = %q, want %q", string(*out), "\x1bP>|RavenTerminal\x1b\\")
			}
		})
	}
}

// TestDECSCUSRUnchanged verifies plain CSI q / CSI Ps SP q still sets the
// cursor style and produces no response bytes.
func TestDECSCUSRUnchanged(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantStyle CursorStyle
		wantBlink bool
	}{
		{"steady bar", "\x1b[6 q", CursorStyleBar, false},
		{"blinking underline", "\x1b[3 q", CursorStyleUnderline, true},
		{"default", "\x1b[0 q", CursorStyleBlock, true},
		{"no space intermediate", "\x1b[2q", CursorStyleBlock, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			term := NewTerminal(10, 3)
			out := captureResponses(term)
			term.Process([]byte(tc.input))
			if got := term.CursorStyle(); got != tc.wantStyle {
				t.Fatalf("cursor style = %v, want %v", got, tc.wantStyle)
			}
			if got := term.CursorBlinking(); got != tc.wantBlink {
				t.Fatalf("cursor blinking = %v, want %v", got, tc.wantBlink)
			}
			if len(*out) != 0 {
				t.Fatalf("DECSCUSR produced response %q, want none", string(*out))
			}
		})
	}
}

// TestOSC52Read covers the clipboard query path: silent with no reader wired,
// base64 reply when one is.
func TestOSC52Read(t *testing.T) {
	t.Run("nil reader", func(t *testing.T) {
		term := NewTerminal(10, 3)
		out := captureResponses(term)
		term.Process([]byte("\x1b]52;c;?\x07"))
		if len(*out) != 0 {
			t.Fatalf("OSC 52 query with nil reader replied %q, want nothing", string(*out))
		}
	})
	t.Run("fake reader", func(t *testing.T) {
		term := NewTerminal(10, 3)
		responses := make(chan []byte, 1)
		term.SetResponseWriter(func(b []byte) { responses <- append([]byte(nil), b...) })
		term.SetClipboardReader(func() string { return "hello" })
		term.Process([]byte("\x1b]52;c;?\x07"))
		want := "\x1b]52;c;aGVsbG8=\x1b\\"
		select {
		case got := <-responses:
			if string(got) != want {
				t.Fatalf("OSC 52 query reply = %q, want %q", string(got), want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no OSC 52 reply within 2s")
		}
	})
	// The clipboard reader may block (the real one waits on the main thread's
	// clipboard access, which in turn may need Terminal.mu to render). Process
	// must therefore never invoke it while holding Terminal.mu: it must return
	// before the reader completes, and the reader must be able to use the
	// terminal's locking API.
	t.Run("reader runs off the terminal lock", func(t *testing.T) {
		term := NewTerminal(10, 3)
		responses := make(chan []byte, 1)
		term.SetResponseWriter(func(b []byte) { responses <- append([]byte(nil), b...) })
		release := make(chan struct{})
		term.SetClipboardReader(func() string {
			term.GetGrid() // deadlocks if the reader is called under Terminal.mu
			<-release
			return "hi"
		})
		done := make(chan struct{})
		go func() {
			term.Process([]byte("\x1b]52;c;?\x07"))
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Process blocked on the clipboard reader")
		}
		close(release)
		select {
		case got := <-responses:
			if want := "\x1b]52;c;aGk=\x1b\\"; string(got) != want {
				t.Fatalf("OSC 52 reply = %q, want %q", string(got), want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no OSC 52 reply after reader returned")
		}
	})
	// SetDefaultClipboardReader(nil) must unwire the process-wide fallback so
	// allow_clipboard_read can be turned off at runtime.
	t.Run("default reader cleared", func(t *testing.T) {
		SetDefaultClipboardReader(func() string { return "secret" })
		SetDefaultClipboardReader(nil)
		term := NewTerminal(10, 3)
		out := captureResponses(term)
		term.Process([]byte("\x1b]52;c;?\x07"))
		time.Sleep(50 * time.Millisecond) // reply, if any, is async
		if len(*out) != 0 {
			t.Fatalf("OSC 52 query after clearing default reader replied %q, want nothing", string(*out))
		}
	})
}
