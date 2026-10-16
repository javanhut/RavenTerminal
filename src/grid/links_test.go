package grid

import (
	"strconv"
	"testing"
)

func TestLinkTableSweepKeepsLiveIDs(t *testing.T) {
	lt := newLinkTableCap(4)
	g := NewGrid(10, 2)
	g.SetLinkTable(lt)
	keep := g.InternLink("http://keep")
	g.WriteChar('k', DefaultFg(), DefaultBg(), 0, keep, 0, Color{})
	seen := map[uint16]bool{}
	for i := range 20 {
		id, ok := lt.Intern("", "http://"+strconv.Itoa(i))
		if !ok {
			live := map[uint16]bool{}
			g.MarkLiveLinksLocked(live)
			lt.Sweep(live)
			id, ok = lt.Intern("", "http://"+strconv.Itoa(i))
		}
		if !ok || id == 0 || id == keep {
			t.Fatalf("intern %d: id=%d ok=%v keep=%d", i, id, ok, keep)
		}
		seen[id] = true
	}
	if g.LinkURL(keep) != "http://keep" {
		t.Fatalf("live id recycled: %q", g.LinkURL(keep))
	}
	if len(seen) > 3 {
		t.Fatalf("ids outside cap: %v", seen)
	}
}

func TestLinkTableKeyedByID(t *testing.T) {
	lt := NewLinkTable()
	a, _ := lt.Intern("a", "http://x")
	b, _ := lt.Intern("b", "http://x")
	a2, _ := lt.Intern("a", "http://x")
	if a == b || a != a2 || lt.URL(b) != "http://x" {
		t.Fatalf("a=%d b=%d a2=%d", a, b, a2)
	}
}
