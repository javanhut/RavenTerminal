package fingerprint

import (
	"testing"
	"time"
)

func newAt(t0 time.Time) *Watcher {
	w := New()
	w.now = func() time.Time { return t0 }
	return w
}

func TestPromptThenMatch(t *testing.T) {
	t0 := time.Unix(1000, 0)
	w := newAt(t0)
	w.Feed([]byte("[sudo] \x1b[1mTouch the fingerprint sensor, or press Enter to type your password.\x1b[0m\r\n"))
	if p, _, ok := w.State(t0); p != Waiting || !ok {
		t.Fatalf("after prompt: phase=%v visible=%v", p, ok)
	}
	w.Feed([]byte("Fingerprint recognised.\r\n"))
	if p, _, ok := w.State(t0); p != Matched || !ok {
		t.Fatalf("after match: phase=%v visible=%v", p, ok)
	}
	if _, _, ok := w.State(t0.Add(MatchedLinger)); ok {
		t.Fatal("match verdict should expire")
	}
}

func TestSentenceSplitAcrossChunks(t *testing.T) {
	t0 := time.Unix(1000, 0)
	w := newAt(t0)
	s := string(msgPrompt)
	w.Feed([]byte("junk " + s[:10]))
	w.Feed([]byte(s[10:40]))
	w.Feed([]byte(s[40:]))
	if p, _, _ := w.State(t0); p != Waiting {
		t.Fatalf("split prompt not recognised: %v", p)
	}
}

func TestOrderWithinOneChunk(t *testing.T) {
	t0 := time.Unix(1000, 0)
	w := newAt(t0)
	w.Feed([]byte(string(msgPrompt) + "\r\n" + string(msgMissed) + "\r\n"))
	if p, _, _ := w.State(t0); p != Missed {
		t.Fatalf("want Missed, got %v", p)
	}
	w.Feed([]byte(string(msgPrompt) + "\r\n"))
	if p, _, _ := w.State(t0); p != Waiting {
		t.Fatalf("a second prompt should wait again, got %v", p)
	}
}

func TestMissedIsNotMatched(t *testing.T) {
	t0 := time.Unix(1000, 0)
	w := newAt(t0)
	w.Feed(msgPrompt)
	w.Feed(msgMissed)
	if p, _, _ := w.State(t0); p != Missed {
		t.Fatalf("'not recognised' must not read as recognised, got %v", p)
	}
}

func TestFallbackHides(t *testing.T) {
	t0 := time.Unix(1000, 0)
	w := newAt(t0)
	w.Feed(msgPrompt)
	w.Feed([]byte("\r\nNo fingerprint; using your password.\r\nPassword: "))
	if _, _, ok := w.State(t0); ok {
		t.Fatal("password fallback should hide the modal")
	}
}

func TestRetryHintOnlyWhileWaiting(t *testing.T) {
	t0 := time.Unix(1000, 0)
	w := newAt(t0)
	w.Feed([]byte("Try again.\n"))
	if _, _, ok := w.State(t0); ok {
		t.Fatal("a stray 'Try again.' must not raise the modal")
	}
	w.Feed(msgPrompt)
	w.Feed([]byte("\r\nCentre your finger on the sensor.\r\n"))
	if p, hint, _ := w.State(t0); p != Waiting || hint != "Centre your finger on the sensor." {
		t.Fatalf("phase=%v hint=%q", p, hint)
	}
	w.Feed([]byte("Wipe the sensor and try again.\r\n"))
	if _, hint, _ := w.State(t0); hint != "Wipe the sensor and try again." {
		t.Fatalf("hint=%q", hint)
	}
}

func TestNoDoubleMatchFromTail(t *testing.T) {
	t0 := time.Unix(1000, 0)
	w := newAt(t0)
	w.Feed(msgPrompt)
	w.Dismiss()
	w.Feed([]byte("x"))
	if _, _, ok := w.State(t0); ok {
		t.Fatal("a sentence already consumed must not match again")
	}
}

func TestWaitTimesOut(t *testing.T) {
	t0 := time.Unix(1000, 0)
	w := newAt(t0)
	w.Feed(msgPrompt)
	if _, _, ok := w.State(t0.Add(WaitTimeout)); ok {
		t.Fatal("an abandoned wait should stop showing")
	}
}

func TestTriesCountRetriesAndMisses(t *testing.T) {
	t0 := time.Unix(1000, 0)
	w := newAt(t0)
	w.Feed(msgPrompt)
	if n := w.Tries(); n != 0 {
		t.Fatalf("fresh check tries=%d", n)
	}
	w.Feed([]byte("Centre your finger on the sensor.\r\n"))
	w.Feed(msgMissed)
	if n := w.Tries(); n != 2 {
		t.Fatalf("after a retry and a miss tries=%d, want 2", n)
	}
	// The helper asking again right after a miss is the same sudo.
	w.Feed(msgPrompt)
	if n := w.Tries(); n != 2 {
		t.Fatalf("re-prompt after a miss reset tries to %d", n)
	}
	w.Dismiss()
	w.Feed(msgPrompt)
	if n := w.Tries(); n != 0 {
		t.Fatalf("new check kept tries=%d", n)
	}
}
