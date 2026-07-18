package main

import "testing"

func TestMemoCacheEvictsOldestInsertion(t *testing.T) {
	c := newMemoCache[int]()
	for i := range memoCacheMax + 2 {
		c.put(string(rune('a'+i)), i)
	}
	if len(c.entries) != memoCacheMax || len(c.order) != memoCacheMax {
		t.Fatalf("size = %d/%d, want %d", len(c.entries), len(c.order), memoCacheMax)
	}
	// The two oldest insertions are gone; the newest survive.
	for _, key := range []string{"a", "b"} {
		if _, ok := c.get(key); ok {
			t.Errorf("key %q should have been evicted", key)
		}
	}
	if v, ok := c.get(string(rune('a' + memoCacheMax + 1))); !ok || v != memoCacheMax+1 {
		t.Errorf("newest key missing: %v %v", v, ok)
	}
}

// Ctrl+R retry relies on delete making the next get a miss.
func TestMemoCacheDelete(t *testing.T) {
	c := newMemoCache[int]()
	c.put("a", 1)
	c.put("b", 2)
	c.delete("a")
	if _, ok := c.get("a"); ok {
		t.Error("deleted key still present")
	}
	if len(c.order) != 1 || c.order[0] != "b" {
		t.Errorf("order = %v, want [b]", c.order)
	}
	// Deleting a missing key is a no-op.
	c.delete("missing")
	if v, ok := c.get("b"); !ok || v != 2 {
		t.Errorf("get(b) = %v %v after no-op delete", v, ok)
	}
	// Re-insert after delete works and keeps entries/order consistent.
	c.put("a", 3)
	if v, ok := c.get("a"); !ok || v != 3 {
		t.Errorf("get(a) = %v %v after re-insert", v, ok)
	}
	if len(c.entries) != len(c.order) {
		t.Errorf("entries/order out of sync: %d vs %d", len(c.entries), len(c.order))
	}
}

func TestMemoCacheOverwriteDoesNotGrowOrder(t *testing.T) {
	c := newMemoCache[string]()
	c.put("k", "v1")
	c.put("k", "v2")
	if v, ok := c.get("k"); !ok || v != "v2" {
		t.Errorf("get = %q %v, want v2", v, ok)
	}
	if len(c.order) != 1 {
		t.Errorf("order len = %d, want 1", len(c.order))
	}
}
