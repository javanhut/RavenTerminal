package render

import (
	"testing"
)

func fillBytes(n int, v byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = v
	}
	return b
}

func TestAtlasPacksThenAddsPages(t *testing.T) {
	a := newGlyphAtlas(64, 1, 2)
	seen := map[[3]int]bool{}
	for i := range 4 {
		l, x, y, ok := a.alloc(32, 32)
		if !ok || l != 0 {
			t.Fatalf("alloc %d = layer %d ok=%v, want layer 0", i, l, ok)
		}
		if seen[[3]int{l, x, y}] {
			t.Fatalf("alloc %d reused region (%d,%d)", i, x, y)
		}
		seen[[3]int{l, x, y}] = true
	}
	l, x, y, ok := a.alloc(32, 32)
	if !ok || l != 1 || x != 0 || y != 0 {
		t.Fatalf("5th alloc = (%d,%d,%d,%v), want a fresh page 1 at origin", l, x, y, ok)
	}
	if len(a.pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(a.pages))
	}
	if _, _, _, ok := a.alloc(65, 10); ok {
		t.Fatalf("oversized alloc succeeded")
	}
}

func TestAtlasEvictsLeastRecentlyUsedPage(t *testing.T) {
	a := newGlyphAtlas(64, 1, 2)
	var evicted []int
	a.onEvict = func(layer int) { evicted = append(evicted, layer) }
	for i := range 8 {
		l, x, y, ok := a.alloc(32, 32)
		if !ok {
			t.Fatalf("alloc %d failed before the atlas was full", i)
		}
		a.write(l, x, y, 32, 32, fillBytes(32*32, byte(10+l)))
	}
	if len(evicted) != 0 {
		t.Fatalf("evicted %v before the atlas was full", evicted)
	}
	a.tick()
	a.touch(0) // page 0 used this frame: page 1 is least recently used
	l, x, y, ok := a.alloc(32, 32)
	if !ok || l != 1 || x != 0 || y != 0 {
		t.Fatalf("alloc after full = (%d,%d,%d,%v), want the LRU page 1 recycled", l, x, y, ok)
	}
	if len(evicted) != 1 || evicted[0] != 1 {
		t.Fatalf("evicted = %v, want [1]", evicted)
	}
	if a.pages[1].pix[63*64+63] != 0 {
		t.Errorf("recycled page kept stale pixels")
	}
	if a.pages[0].pix[0] != 10 || a.pages[0].pix[63*64+63] != 10 {
		t.Errorf("eviction disturbed the live page")
	}
	if !a.evictedInFrame() {
		// page 1 was last used in the previous frame, so UVs recorded this
		// frame are still valid.
	} else {
		t.Errorf("evicting a page not used this frame flagged the frame stale")
	}
}

func TestAtlasEvictingPageInUseFlagsFrame(t *testing.T) {
	a := newGlyphAtlas(32, 1, 1)
	a.alloc(32, 32)
	a.touch(0)
	if _, _, _, ok := a.alloc(32, 32); !ok {
		t.Fatalf("full single-page atlas must recycle, not fail")
	}
	if !a.evictedInFrame() {
		t.Errorf("recycling a page drawn from this frame must flag the frame")
	}
	a.tick()
	if a.evictedInFrame() {
		t.Errorf("tick must clear the stale flag")
	}
}

func TestAtlasDirtyRowsAndRGBA(t *testing.T) {
	a := newGlyphAtlas(16, 4, 1)
	l, x, y, _ := a.alloc(2, 3)
	a.write(l, x, y, 2, 3, fillBytes(2*3*4, 7))
	p := a.pages[0]
	if p.dirtyY0 != 0 || p.dirtyY1 != 3 {
		t.Fatalf("dirty rows = [%d,%d), want [0,3)", p.dirtyY0, p.dirtyY1)
	}
	if p.pix[(2*16+1)*4+3] != 7 || p.pix[(0*16+2)*4] != 0 {
		t.Errorf("RGBA write landed at the wrong place")
	}
	l, x, y, _ = a.alloc(4, 4)
	a.write(l, x, y, 4, 4, fillBytes(64, 1))
	if p.dirtyY0 != 0 || p.dirtyY1 != 4 {
		t.Errorf("dirty rows = [%d,%d), want [0,4)", p.dirtyY0, p.dirtyY1)
	}
}
