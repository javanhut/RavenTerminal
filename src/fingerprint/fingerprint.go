// Package fingerprint recognises RavenLinux's sudo fingerprint prompt in a
// pane's output so the terminal can show it as a modal.
//
// The prompt comes from raven-finger-auth, which PAM runs ahead of pam_unix
// for sudo (see RavenGUI/docs/fingerprint.md). It writes plain sentences to
// sudo's /dev/tty — which is the pane's PTY — and only when the account turned
// "Approve sudo" on in Settings > Security, so seeing the prompt already means
// the system setting is on. Nothing here talks to the sensor or to PAM: the
// modal is a view of text the helper printed anyway, and the only input it ever
// sends back is the Enter the helper itself asks for to fall back to the
// password. A program that prints the same sentences can raise the modal, and
// that is all it can do.
package fingerprint

import (
	"bytes"
	"sync"
	"time"
)

// Phase is where a sudo fingerprint check stands.
type Phase int

const (
	Idle    Phase = iota // no check in progress
	Waiting              // the helper is watching the sensor
	Matched              // the finger was recognised; sudo proceeds
	Missed               // a clean read that matched no enrolled finger
)

// Linger is how long a verdict stays on screen before the modal goes away.
const (
	MatchedLinger = 800 * time.Millisecond
	MissedLinger  = 1500 * time.Millisecond
	// WaitTimeout bounds a Waiting phase whose verdict never arrived (sudo
	// killed from elsewhere, helper crashed), so the modal cannot outlive it.
	WaitTimeout = 2 * time.Minute
)

// The helper's sentences, verbatim from raven-finger-auth. They are matched
// as bytes wherever they appear, so a prefix, colour codes around them, or a
// chunk boundary through the middle do not matter.
var (
	msgPrompt   = []byte("Touch the fingerprint sensor, or press Enter to type your password.")
	msgMatched  = []byte("Fingerprint recognised.")
	msgMissed   = []byte("Fingerprint not recognised.")
	msgFallback = []byte("No fingerprint; using your password.")
)

// retries are the helper's advice for a reading it could not use. They are
// not verdicts, and "Try again." is generic enough that they only count while
// a check is Waiting.
var retries = [][]byte{
	[]byte("Wipe the sensor and try again."),
	[]byte("Cover more of the sensor with your finger."),
	[]byte("Centre your finger on the sensor."),
	[]byte("Try again."),
}

type rule struct {
	msg   []byte
	apply func(w *Watcher, now time.Time)
}

var rules []rule

// keep is how many trailing bytes survive between chunks: enough for the
// longest sentence to straddle a boundary, never enough to match one twice.
var keep int

func init() {
	rules = []rule{
		{msgPrompt, func(w *Watcher, now time.Time) {
			// A prompt straight after a miss is the helper asking again
			// within the same sudo, so the count carries on.
			if w.phase != Missed || now.Sub(w.changed) >= MissedLinger {
				w.tries = 0
			}
			w.set(Waiting, "", now)
		}},
		{msgMatched, func(w *Watcher, now time.Time) { w.set(Matched, "", now) }},
		{msgMissed, func(w *Watcher, now time.Time) { w.tries++; w.set(Missed, "", now) }},
		{msgFallback, func(w *Watcher, now time.Time) { w.set(Idle, "", now) }},
	}
	for _, r := range retries {
		hint := string(r)
		rules = append(rules, rule{r, func(w *Watcher, now time.Time) {
			if w.phase == Waiting {
				w.hint = hint
				w.tries++
			}
		}})
	}
	for _, r := range rules {
		keep = max(keep, len(r.msg)-1)
	}
}

// Watcher follows one pane's output. Feed is called from the PTY reader
// goroutine and State from the render thread, so it locks.
type Watcher struct {
	mu      sync.Mutex
	phase   Phase
	hint    string
	tries   int // readings used up in this check: retries and misses
	changed time.Time
	tail    []byte
	scratch []byte
	now     func() time.Time
}

// New returns an idle watcher.
func New() *Watcher { return &Watcher{now: time.Now} }

func (w *Watcher) set(p Phase, hint string, now time.Time) {
	w.phase, w.hint, w.changed = p, hint, now
}

// Feed scans a chunk of PTY output for the helper's sentences and applies
// them in the order they appear.
func (w *Watcher) Feed(data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// scratch is reused so the reader loop does not allocate per chunk.
	w.scratch = append(append(w.scratch[:0], w.tail...), data...)
	buf := w.scratch
	// Every sentence ends in '.', so a chunk without one cannot complete a
	// match and only needs the tail kept.
	if bytes.IndexByte(data, '.') >= 0 {
		now := w.now()
		for {
			at, which := -1, -1
			for i, r := range rules {
				if j := bytes.Index(buf, r.msg); j >= 0 && (at < 0 || j < at) {
					at, which = j, i
				}
			}
			if which < 0 {
				break
			}
			rules[which].apply(w, now)
			buf = buf[at+len(rules[which].msg):]
		}
	}
	if len(buf) > keep {
		buf = buf[len(buf)-keep:]
	}
	w.tail = append(w.tail[:0], buf...)
}

// Dismiss hides the modal: the person pressed Enter, Escape or Ctrl+C, each
// of which ends the helper's wait from the terminal's side.
func (w *Watcher) Dismiss() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.set(Idle, "", w.now())
}

// State is what the modal should show at now: the phase, the helper's latest
// advice, and whether to draw anything at all. Verdicts expire on their own,
// and so does a wait that never got one.
func (w *Watcher) State(now time.Time) (phase Phase, hint string, visible bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	age := now.Sub(w.changed)
	switch w.phase {
	case Waiting:
		visible = age < WaitTimeout
	case Matched:
		visible = age < MatchedLinger
	case Missed:
		visible = age < MissedLinger
	}
	if !visible {
		return Idle, "", false
	}
	return w.phase, w.hint, true
}

// Tries is how many readings the current check has used up: each retry the
// helper asked for, and each miss.
func (w *Watcher) Tries() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tries
}
