package grid

import "testing"

func TestStyleInternDedup(t *testing.T) {
	s := newStyleSet()
	a := Style{Fg: IndexedColor(1)}
	id1 := s.intern(a)
	id2 := s.intern(a)
	if id1 != id2 {
		t.Fatalf("intern of equal styles gave different ids: %d vs %d", id1, id2)
	}
	if s.liveCount() != 1 {
		t.Fatalf("liveCount = %d, want 1", s.liveCount())
	}
	// Two references now; releasing once keeps it alive.
	s.release(id1)
	if s.liveCount() != 1 {
		t.Fatalf("after one release liveCount = %d, want 1", s.liveCount())
	}
	s.release(id2)
	if s.liveCount() != 0 {
		t.Fatalf("after both releases liveCount = %d, want 0", s.liveCount())
	}
}

func TestStyleDefaultIsZero(t *testing.T) {
	s := newStyleSet()
	if id := s.intern(defaultStyle); id != 0 {
		t.Fatalf("default style id = %d, want 0", id)
	}
	if s.liveCount() != 0 {
		t.Fatalf("default style must not be tracked; liveCount = %d", s.liveCount())
	}
	// release/retain on 0 are no-ops
	s.release(0)
	s.retain(0)
	if got := s.resolve(0); got != defaultStyle {
		t.Fatalf("resolve(0) = %+v, want default", got)
	}
}

func TestStyleRecycle(t *testing.T) {
	s := newStyleSet()
	id := s.intern(Style{Fg: IndexedColor(1)})
	s.release(id) // freed, id recycled
	id2 := s.intern(Style{Fg: IndexedColor(2)})
	if id2 != id {
		t.Fatalf("expected recycled id %d, got %d", id, id2)
	}
}

// TestAltGridClearReleasesStyles is the load-bearing balance invariant on a
// no-scrollback (alt) grid: clearing to default must release every reference
// since there is no history to retain them.
func TestAltGridClearReleasesStyles(t *testing.T) {
	g := NewAltGrid(10, 3)
	g.WriteChar('a', IndexedColor(1), DefaultBg(), 0, 0, 0, Color{})
	g.WriteChar('b', IndexedColor(2), DefaultBg(), 0, 0, 0, Color{})
	g.WriteChar('c', IndexedColor(1), DefaultBg(), 0, 0, 0, Color{})
	if g.styles.liveCount() != 2 {
		t.Fatalf("liveCount = %d, want 2 distinct styles", g.styles.liveCount())
	}
	g.ClearAll()
	if g.styles.liveCount() != 0 {
		t.Fatalf("after alt ClearAll liveCount = %d, want 0", g.styles.liveCount())
	}
}

// TestHistoryRetainsStyles verifies that on a normal grid a styled row scrolled
// off-screen retains its style references in history (scrollback preserves
// styled content), while an alt grid (no scrollback) releases immediately.
func TestHistoryRetainsStyles(t *testing.T) {
	g := NewGrid(4, 2)
	g.WriteChar('x', IndexedColor(5), DefaultBg(), 0, 0, 0, Color{})
	g.ScrollUpWithBg(1, DefaultBg())
	g.ScrollUpWithBg(1, DefaultBg())
	if g.styles.liveCount() != 1 {
		t.Fatalf("after scroll-off into history liveCount = %d, want 1 (retained)", g.styles.liveCount())
	}

	a := NewAltGrid(4, 2)
	a.WriteChar('x', IndexedColor(5), DefaultBg(), 0, 0, 0, Color{})
	a.ScrollUpWithBg(1, DefaultBg())
	a.ScrollUpWithBg(1, DefaultBg())
	if a.styles.liveCount() != 0 {
		t.Fatalf("alt grid scroll-off liveCount = %d, want 0 (no scrollback)", a.styles.liveCount())
	}
}

// TestHistoryTrimReleasesStyles verifies that trimming history past its cap
// releases the dropped rows' style references.
func TestHistoryTrimReleasesStyles(t *testing.T) {
	g := newGrid(4, 1, 2) // tiny scrollback cap of 2 rows
	// Each distinct color produces a distinct style; scroll each into history.
	for i := 0; i < 5; i++ {
		g.WriteChar('x', IndexedColor(uint8(10+i)), DefaultBg(), 0, 0, 0, Color{})
		g.ScrollUpWithBg(1, DefaultBg())
	}
	// History holds at most 2 rows; only those styles (plus none active) remain.
	if lc := g.styles.liveCount(); lc > 2 {
		t.Fatalf("after trim liveCount = %d, want <= 2 (history cap)", lc)
	}
}

// TestReflowPreservesStyles verifies the main grid reflows (rather than
// truncates) on resize, so styled content survives a shrink (reflowed into
// scrollback) and refcounts stay balanced.
func TestReflowPreservesStyles(t *testing.T) {
	g := NewGrid(10, 4)
	g.WriteChar('a', IndexedColor(1), DefaultBg(), 0, 0, 0, Color{})
	g.WriteChar('b', RGBColor(9, 8, 7), DefaultBg(), 0, 0, 0, Color{})
	if g.styles.liveCount() != 2 {
		t.Fatalf("liveCount before = %d, want 2", g.styles.liveCount())
	}
	g.Resize(6, 3)
	if g.styles.liveCount() != 2 {
		t.Fatalf("after reflow to 6x3 liveCount = %d, want 2 (preserved)", g.styles.liveCount())
	}
	g.Resize(1, 1) // content reflows into scrollback, not truncated
	if lc := g.styles.liveCount(); lc != 2 {
		t.Fatalf("after reflow to 1x1 liveCount = %d, want 2 (preserved in history)", lc)
	}
}

// TestAltResizeTruncates verifies the alternate screen clamps/truncates on
// resize (no scrollback), dropping styles that fall outside the new bounds.
func TestAltResizeTruncates(t *testing.T) {
	g := NewAltGrid(10, 4)
	g.WriteChar('a', IndexedColor(1), DefaultBg(), 0, 0, 0, Color{})
	g.WriteChar('b', RGBColor(9, 8, 7), DefaultBg(), 0, 0, 0, Color{})
	if g.styles.liveCount() != 2 {
		t.Fatalf("liveCount before = %d, want 2", g.styles.liveCount())
	}
	g.Resize(1, 1) // truncates: only the 'a' cell remains
	if lc := g.styles.liveCount(); lc != 1 {
		t.Fatalf("after alt shrink to 1x1 liveCount = %d, want 1", lc)
	}
}
