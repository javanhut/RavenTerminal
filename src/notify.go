package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// notification is one desktop notification. Tag (OSC 99 i=) makes a later
// notification with the same tag replace the earlier one.
type notification struct {
	Summary, Body string
	Urgency       int // 0 low, 1 normal, 2 critical
	Tag           string
}

const (
	notifyDest  = "org.freedesktop.Notifications"
	notifyPath  = "/org/freedesktop/Notifications"
	notifyIface = "org.freedesktop.Notifications"
)

// notifyArgs builds org.freedesktop.Notifications.Notify's arguments. The
// desktop-entry hint lets huginn show the app's icon and focus its window
// when the notification is clicked; "default" is the click action.
func notifyArgs(n notification, replaces uint32) []any {
	return []any{
		"Raven Terminal",
		replaces,
		"raven-terminal",
		n.Summary,
		bodyEscaper.Replace(n.Body),
		[]string{"default", "Show"},
		map[string]dbus.Variant{
			"desktop-entry": dbus.MakeVariant("raven-terminal"),
			"urgency":       dbus.MakeVariant(byte(min(max(n.Urgency, 0), 2))),
		},
		int32(-1),
	}
}

// bodyEscaper keeps bodies plain text on servers with body-markup.
var bodyEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// notifier sends notifications from one worker goroutine so the main loop
// never waits on D-Bus, and turns clicks into focus requests for the window
// that sent them.
type notifier struct {
	once    sync.Once
	queue   chan queuedNote
	mu      sync.Mutex
	conn    *dbus.Conn
	tags    map[string]uint32
	owners  map[uint32]any
	focus   chan any
	buckets map[any]*noteBucket // main thread only (send)
	now     func() time.Time
}

// Notifications come from program output, so each window gets a token
// bucket and the text is truncated before it reaches D-Bus.
const (
	notifyBurst      = 5
	notifyRefill     = 2 * time.Second
	maxNotifySummary = 256
	maxNotifyBody    = 1024
)

type noteBucket struct {
	tokens float64
	last   time.Time
}

func (nt *notifier) allow(owner any) bool {
	now := time.Now()
	if nt.now != nil {
		now = nt.now()
	}
	if nt.buckets == nil || len(nt.buckets) > 256 {
		nt.buckets = map[any]*noteBucket{}
	}
	b := nt.buckets[owner]
	if b == nil {
		b = &noteBucket{tokens: notifyBurst, last: now}
		nt.buckets[owner] = b
	}
	b.tokens = min(b.tokens+now.Sub(b.last).Seconds()/notifyRefill.Seconds(), notifyBurst)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func truncateRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

type queuedNote struct {
	n     notification
	owner any
}

var desktopNotifier = &notifier{tags: map[string]uint32{}, owners: map[uint32]any{}, focus: make(chan any, 8)}

func (nt *notifier) replaceID(tag string) uint32 {
	if tag == "" {
		return 0
	}
	nt.mu.Lock()
	defer nt.mu.Unlock()
	return nt.tags[tag]
}

func (nt *notifier) remember(tag string, id uint32, owner any) {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	if tag != "" {
		if len(nt.tags) >= 256 {
			clear(nt.tags)
		}
		nt.tags[tag] = id
	}
	if len(nt.owners) > 256 {
		clear(nt.owners)
	}
	nt.owners[id] = owner
}

func (nt *notifier) ownerOf(id uint32) any {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	return nt.owners[id]
}

// send queues n; when the queue is full or the owner is over its rate the
// notification is dropped.
func (nt *notifier) send(n notification, owner any) {
	nt.once.Do(func() {
		nt.queue = make(chan queuedNote, 16)
		goSafe("notifications", nt.run)
	})
	if !nt.allow(owner) {
		return
	}
	n.Summary = truncateRunes(n.Summary, maxNotifySummary)
	n.Body = truncateRunes(n.Body, maxNotifyBody)
	select {
	case nt.queue <- queuedNote{n, owner}:
	default:
	}
}

func (nt *notifier) run() {
	for q := range nt.queue {
		if nt.conn == nil && !nt.connect() {
			continue
		}
		var id uint32
		call := nt.conn.Object(notifyDest, notifyPath).Call(notifyIface+".Notify", 0, notifyArgs(q.n, nt.replaceID(q.n.Tag))...)
		if err := call.Store(&id); err != nil {
			log.Printf("raven: notification failed: %v", err)
			continue
		}
		nt.remember(q.n.Tag, id, q.owner)
	}
}

func (nt *notifier) connect() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("raven: no session bus for notifications: %v", err)
		return false
	}
	if err := conn.AddMatchSignal(dbus.WithMatchInterface(notifyIface), dbus.WithMatchMember("ActionInvoked")); err != nil {
		log.Printf("raven: notification clicks unavailable: %v", err)
	}
	sig := make(chan *dbus.Signal, 8)
	conn.Signal(sig)
	goSafe("notification actions", func() {
		for s := range sig {
			if s.Name != notifyIface+".ActionInvoked" || len(s.Body) < 2 {
				continue
			}
			id, _ := s.Body[0].(uint32)
			if owner := nt.ownerOf(id); owner != nil {
				select {
				case nt.focus <- owner:
					wakeUI()
				default:
				}
			}
		}
	})
	nt.conn = conn
	return true
}

// takeFocusRequests returns the windows whose notification was clicked.
func (nt *notifier) takeFocusRequests() []any {
	var out []any
	for {
		select {
		case o := <-nt.focus:
			out = append(out, o)
		default:
			return out
		}
	}
}
