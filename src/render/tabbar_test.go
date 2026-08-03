package render

import (
	"testing"
	"time"

	"github.com/javanhut/RavenTerminal/src/tab"
)

func TestTabDropSlotUsesMidpointBetweenTabCenters(t *testing.T) {
	g := tabBarGeom{
		topPad: 12,
		boxH:   30,
		gap:    6,
	}

	// Tab centers are 27, 63, and 99, so their boundaries are 45 and 81.
	tests := []struct {
		name string
		y    float32
		want int
	}{
		{name: "above tabs clamps first", y: -100, want: 0},
		{name: "first center", y: 27, want: 0},
		{name: "just before first boundary", y: 44.9, want: 0},
		{name: "first boundary", y: 45, want: 1},
		{name: "second center", y: 63, want: 1},
		{name: "just before second boundary", y: 80.9, want: 1},
		{name: "second boundary", y: 81, want: 2},
		{name: "below tabs clamps last", y: 1000, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tabDropSlot(tt.y, 3, g); got != tt.want {
				t.Fatalf("tabDropSlot(%v) = %d, want %d", tt.y, got, tt.want)
			}
		})
	}
}

// Chips must compress so a full MaxTabs strip plus the "+" button still fits
// the bar; otherwise tabs past the fold are unclickable.
func TestTabBarGeomFitsMaxTabs(t *testing.T) {
	r := &Renderer{
		baseFontSize: 16, fontSize: 16,
		cellWidth: 8, cellHeight: 18,
		tabBarWidth: 200,
		barViewH:    768,
	}
	const count = 32
	g := r.tabBarGeom(count)
	bottom := g.topPad + float32(count)*(g.boxH+g.gap) + g.plusH
	if bottom > r.barViewH {
		t.Fatalf("%d chips + button reach %v, past the %v bar", count, bottom, r.barViewH)
	}
	if g.cellH > g.boxH {
		t.Fatalf("text height %v overflows chip height %v", g.cellH, g.boxH)
	}
	// Few tabs keep the uncompressed geometry.
	if small := r.tabBarGeom(3); small.boxH != r.cellHeight+14 {
		t.Fatalf("3 tabs: boxH = %v, want uncompressed %v", small.boxH, r.cellHeight+14)
	}
}

func TestTabDropSlotEmpty(t *testing.T) {
	if got := tabDropSlot(100, 0, tabBarGeom{}); got != 0 {
		t.Fatalf("tabDropSlot() = %d, want 0", got)
	}
}

// A reorder must slide: the swapped chips leave their old slots gradually and
// keep requesting frames until they arrive. Without the uiDirty latch the
// event-driven main loop stops drawing and the slide freezes mid-flight.
func TestAnimateTabSlotsSlidesAndKeepsRequestingFrames(t *testing.T) {
	a, b := &tab.Tab{}, &tab.Tab{}
	r := &Renderer{}

	// First sight of both tabs settles them in place — nothing to slide from.
	if got := r.animateTabSlots([]*tab.Tab{a, b}); got[0] != 0 || got[1] != 1 {
		t.Fatalf("first frame = %v, want [0 1]", got)
	}
	r.uiDirty = false

	// Swap them, then advance a frame at a time. tabSlotAt is rewound by a
	// fixed step so the ease is deterministic instead of wall-clock dependent.
	step := func() []float32 {
		r.tabSlotAt = r.tabSlotAt.Add(-16 * time.Millisecond)
		return r.animateTabSlots([]*tab.Tab{b, a})
	}

	got := step()
	if got[0] <= 0 || got[0] >= 1 {
		t.Fatalf("chip b at %v after one frame; want strictly between its old slot 1 and new slot 0", got[0])
	}
	if !r.uiDirty {
		t.Fatal("mid-slide frame did not latch uiDirty, so no further frame is scheduled")
	}

	// It must actually converge, not merely approach forever.
	for i := 0; i < 200 && (got[0] != 0 || got[1] != 1); i++ {
		got = step()
	}
	if got[0] != 0 || got[1] != 1 {
		t.Fatalf("slots never settled: %v", got)
	}
	r.uiDirty = false
	if step(); r.uiDirty {
		t.Fatal("settled bar still latches uiDirty, so the app never returns to idle")
	}
}

// The dragged chip tracks the pointer, so it must not lag behind into an ease.
func TestAnimateTabSlotsSnapsDraggedChip(t *testing.T) {
	a, b := &tab.Tab{}, &tab.Tab{}
	r := &Renderer{}
	r.animateTabSlots([]*tab.Tab{a, b})

	r.dragTab = b
	r.tabSlotAt = r.tabSlotAt.Add(-16 * time.Millisecond)
	got := r.animateTabSlots([]*tab.Tab{b, a})
	if got[0] != 0 {
		t.Fatalf("dragged chip at %v, want 0 (snapped to cursor slot)", got[0])
	}
	if got[1] == 1 {
		t.Fatal("displaced chip snapped too; the swap would be invisible")
	}
}
