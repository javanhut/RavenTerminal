package grid

import "testing"

// pushHistory recycles its backing array by sliding the window back to the
// front once it reaches the end. Push well past that point and assert the
// window still holds exactly the newest maxScroll rows, in order.
func TestPushHistoryWrapsWithoutLosingRows(t *testing.T) {
	const cap_ = 4
	g := newGrid(2, 2, cap_)

	// Enough pushes to force several compactions (each happens roughly every
	// maxScroll pushes), so a stale-window bug can't hide behind a single lap.
	const total = 200
	for i := range total {
		r := g.blankRow(DefaultBg())
		r.cells[0].Char = rune('a' + i%26) // tag the row so order is checkable
		g.pushHistory(r)

		if len(g.history) > cap_ {
			t.Fatalf("push %d: history=%d exceeds cap %d", i, len(g.history), cap_)
		}
	}

	if len(g.history) != cap_ {
		t.Fatalf("history=%d, want %d", len(g.history), cap_)
	}
	// The window must be the newest cap_ rows, oldest first.
	for j := range cap_ {
		want := rune('a' + (total-cap_+j)%26)
		if got := g.history[j].cells[0].Char; got != want {
			t.Errorf("history[%d]=%q, want %q", j, got, want)
		}
	}
	if want := total - cap_; g.scrolledOut != want {
		t.Errorf("scrolledOut=%d, want %d", g.scrolledOut, want)
	}
}
