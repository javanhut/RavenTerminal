package parser

import "fmt"

// Kitty keyboard protocol enhancement flags.
const (
	KittyDisambiguate    uint8 = 1 << 0 // disambiguate escape codes
	KittyReportEvents    uint8 = 1 << 1 // report event types (press/repeat/release)
	KittyReportAltKeys   uint8 = 1 << 2 // report alternate keys
	KittyReportAllKeys   uint8 = 1 << 3 // report all keys as escape codes
	KittyReportAssocText uint8 = 1 << 4 // report associated text
)

const kittyMaxStack = 32

// KittyKeyboardFlags returns the active flag set (top of stack; 0 = legacy).
func (t *Terminal) KittyKeyboardFlags() uint8 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.kittyTop()
}

// kittyTop returns the current flags without locking (caller holds t.mu).
func (t *Terminal) kittyTop() uint8 {
	if n := len(t.kittyStack); n > 0 {
		return t.kittyStack[n-1]
	}
	return 0
}

// kittyPush pushes a new flag set (CSI > flags u). Caller holds t.mu.
func (t *Terminal) kittyPush(flags uint8) {
	if len(t.kittyStack) >= kittyMaxStack {
		// Drop the oldest to bound growth.
		t.kittyStack = t.kittyStack[1:]
	}
	t.kittyStack = append(t.kittyStack, flags)
}

// kittyPop pops n flag sets (CSI < n u). Caller holds t.mu.
func (t *Terminal) kittyPop(n int) {
	if n < 1 {
		n = 1
	}
	if n > len(t.kittyStack) {
		n = len(t.kittyStack)
	}
	t.kittyStack = t.kittyStack[:len(t.kittyStack)-n]
}

// kittySet sets the current flags (CSI = flags ; mode u): mode 1=set all,
// 2=set bits, 3=clear bits. Caller holds t.mu.
func (t *Terminal) kittySet(flags uint8, mode int) {
	cur := t.kittyTop()
	var next uint8
	switch mode {
	case 2:
		next = cur | flags
	case 3:
		next = cur &^ flags
	default: // 1 = replace
		next = flags
	}
	if len(t.kittyStack) == 0 {
		t.kittyStack = append(t.kittyStack, next)
	} else {
		t.kittyStack[len(t.kittyStack)-1] = next
	}
}

// kittyQuery replies with the current flags (CSI ? flags u). Caller holds t.mu.
func (t *Terminal) kittyQuery() {
	if t.responseWriter == nil {
		return
	}
	t.responseWriter([]byte(fmt.Sprintf("\x1b[?%du", t.kittyTop())))
}
