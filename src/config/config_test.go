package config

import "testing"

// Clipboard read (OSC 52 query) is a data-exfiltration vector, so it must be
// opt-in: off by default.
func TestAllowClipboardReadDefaultsFalse(t *testing.T) {
	if DefaultConfig().AllowClipboardRead {
		t.Fatal("AllowClipboardRead must default to false")
	}
}
