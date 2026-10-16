package parser

import "sync"

// EventKind identifies a host-facing event raised by terminal output.
type EventKind int

const (
	EventBell EventKind = iota
	EventTitle
	EventNotify
	EventProgress
	EventPointerShape
	EventUserVar
	EventRavenCommand
	EventCommandFinished
)

// Event is one queued host-facing event. Fields used per kind:
// Title: Title. Notify: Title, Body, ID, Urgency (0 low, 1 normal, 2 critical).
// Progress: ProgressState (0 none, 1 value, 2 error, 3 indeterminate, 4 paused),
// ProgressValue (0..100). PointerShape: Shape (CSS name, "" = default).
// UserVar / RavenCommand: Name, Value. CommandFinished: ExitCode, Value
// (duration in milliseconds, "" if unknown), Body (command line if reported).
type Event struct {
	Kind                                  EventKind
	Title, Body, ID, Shape, Name, Value   string
	Urgency, ProgressState, ProgressValue int
	ExitCode                              int
}

const maxEvents = 256

type eventQueue struct {
	mu     sync.Mutex
	events []Event
}

func (t *Terminal) pushEvent(e Event) {
	q := &t.osc.events
	q.mu.Lock()
	if len(q.events) >= maxEvents {
		copy(q.events, q.events[1:])
		q.events = q.events[:maxEvents-1]
	}
	q.events = append(q.events, e)
	q.mu.Unlock()
}

// DrainEvents returns and clears the events queued since the last drain. Safe
// to call from any goroutine; it does not wait on Process.
func (t *Terminal) DrainEvents() []Event {
	q := &t.osc.events
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.events) == 0 {
		return nil
	}
	out := q.events
	q.events = nil
	return out
}
