package tab

import "sync"

// primaryOut is the OSC 52 primary-selection counterpart of clipboardOut.
var primaryOut struct {
	mu   sync.Mutex
	text string
	has  bool
}

// QueuePrimary stores text to be set as the primary selection by the main thread.
func QueuePrimary(text string) {
	primaryOut.mu.Lock()
	primaryOut.text, primaryOut.has = text, true
	primaryOut.mu.Unlock()
}

// DrainPrimary returns and clears any pending OSC 52 primary-selection write.
func DrainPrimary() (string, bool) {
	primaryOut.mu.Lock()
	defer primaryOut.mu.Unlock()
	if !primaryOut.has {
		return "", false
	}
	primaryOut.has = false
	return primaryOut.text, true
}
