package main

import (
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestNotifyCallArgs(t *testing.T) {
	args := notifyArgs(notification{Summary: "Build", Body: "done", Urgency: 2}, 7)
	if len(args) != 8 {
		t.Fatalf("Notify takes 8 args, got %d", len(args))
	}
	if args[0] != "Raven Terminal" || args[1] != uint32(7) || args[2] != "raven-terminal" || args[3] != "Build" || args[4] != "done" {
		t.Fatalf("head args = %#v", args[:5])
	}
	if !reflect.DeepEqual(args[5], []string{"default", "Show"}) {
		t.Fatalf("actions = %#v", args[5])
	}
	hints, ok := args[6].(map[string]dbus.Variant)
	if !ok {
		t.Fatalf("hints type %T", args[6])
	}
	if hints["desktop-entry"].Value() != "raven-terminal" || hints["urgency"].Value() != byte(2) {
		t.Fatalf("hints = %#v", hints)
	}
	if args[7] != int32(-1) {
		t.Fatalf("timeout = %#v", args[7])
	}
	// Bodies are plain text; servers with body-markup must not parse them.
	if b := notifyArgs(notification{Summary: "s", Body: "a<b> & c"}, 0)[4]; b != "a&lt;b&gt; &amp; c" {
		t.Fatalf("escaped body = %q", b)
	}
	// Urgency is clamped to the spec's 0..2.
	if h := notifyArgs(notification{Summary: "x", Urgency: 9}, 0)[6].(map[string]dbus.Variant); h["urgency"].Value() != byte(2) {
		t.Fatalf("clamped urgency = %v", h["urgency"])
	}
}

func TestNotifierReplacesByTag(t *testing.T) {
	n := &notifier{tags: map[string]uint32{}, owners: map[uint32]any{}}
	n.remember("x1", 41, "app")
	if got := n.replaceID("x1"); got != 41 {
		t.Fatalf("replaceID = %d", got)
	}
	if got := n.replaceID(""); got != 0 {
		t.Fatalf("untagged replaceID = %d", got)
	}
	if got := n.ownerOf(41); got != "app" {
		t.Fatalf("owner = %v", got)
	}
}
