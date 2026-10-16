package render

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/tab"
)

func TestPaletteBackgroundOpaque(t *testing.T) {
	r := &Renderer{theme: Theme{TabBar: [4]float32{0.1, 0.1, 0.1, 0.4}}}
	if bg := r.paletteBackground(); bg[3] != 1 {
		t.Fatalf("palette background alpha %v, want 1 so terminal text can't show through", bg[3])
	}
}

func TestCopyStatusSitsBottomLeft(t *testing.T) {
	x, y := copyStatusOrigin(100, 50, 24, 20)
	if x != 100 || y != 50+23*20 {
		t.Fatalf("copy status at %v,%v, want the pane's bottom-left (100,%v) clear of the bottom-right toast", x, y, 50+23*20)
	}
}

func TestTabTagsShowModes(t *testing.T) {
	tb := &tab.Tab{}
	if got := tabTags(tb); len(got) != 0 {
		t.Fatalf("plain tab tags %v, want none", got)
	}
	tb.ToggleBroadcast()
	if got := tabTags(tb); len(got) != 1 || got[0] != "BCAST" {
		t.Fatalf("broadcasting tab tags %v, want [BCAST]", got)
	}
}

func TestToastAvoidsCopyStatus(t *testing.T) {
	const w, h, bw, bh, m = 944, 979, 600, 40, 10
	x, y := toastOrigin(w, h, bw, bh, m, nil)
	if x != w-bw-m || y != h-bh-m {
		t.Fatalf("toast at %v,%v, want the bottom-right corner", x, y)
	}
	status := &uiBox{x: 575, y: 935, w: 150, h: 27}
	x, y = toastOrigin(w, h, bw, bh, m, status)
	if y+bh > status.y || x != w-bw-m {
		t.Fatalf("toast at %v,%v (h %v) overlaps the copy status at y %v", x, y, bh, status.y)
	}
	away := &uiBox{x: 0, y: 935, w: 150, h: 27}
	if x, y = toastOrigin(w, h, bw, bh, m, away); y != h-bh-m {
		t.Fatalf("toast moved to %v,%v for a status it doesn't touch", x, y)
	}
}
