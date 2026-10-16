package tab

import "testing"

func TestPrimaryMailbox(t *testing.T) {
	_, _ = DrainPrimary()
	QueuePrimary("one")
	QueuePrimary("two")
	if got, ok := DrainPrimary(); !ok || got != "two" {
		t.Fatalf("DrainPrimary = %q, %v", got, ok)
	}
	if _, ok := DrainPrimary(); ok {
		t.Fatal("mailbox not cleared")
	}
}
