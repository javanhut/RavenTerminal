package render

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/images"
	"github.com/javanhut/RavenTerminal/src/parser"
)

// A placement relative to a virtual (placeholder) parent is drawn at the
// placeholders' minimum column and row plus its own H/V offset, and not at
// all while no placeholder for the parent is on screen.
func TestRelativeToVirtualParentFollowsPlaceholders(t *testing.T) {
	term := parser.NewTerminal(20, 6)
	term.SetResponseWriter(func([]byte) {})
	const px = "AAAA/w=="
	apc := func(s string) []byte { return []byte("\x1b_G" + s + "\x1b\\") }
	term.Process(apc("f=32,s=1,v=1,i=1,a=t,q=2;" + px))
	term.Process(apc("f=32,s=1,v=1,i=2,a=t,q=2;" + px))
	term.Process(apc("a=p,U=1,i=1,p=7,c=2,r=2,q=2"))
	term.Process(apc("a=p,i=2,p=1,P=1,Q=7,H=1,V=1,q=2"))

	r := newBenchRenderer()
	var f parser.RenderFrame
	draw := func() (*images.Placement, bool) {
		term.RenderFrame(&f)
		r.paneImgs.frame = &f
		r.buildGridBatches(f.Snap, 0, 0, 1000, 1000, false)
		r.locateVirtualRoots()
		var child *images.Placement
		for _, p := range f.Placements {
			if p.ImageID == 2 {
				child = p
			}
		}
		if child == nil {
			t.Fatal("child placement missing")
		}
		return r.paneImgs.placedAt(child)
	}

	if _, ok := draw(); ok {
		t.Fatal("drawn with no placeholder on screen")
	}

	// A 2x2 block of placeholders for image 1 at rows 3-4, columns 5-6
	// (1-based), placement id 7 in the underline colour.
	ph := "\x1b[38;5;1m\x1b[58;5;7m"
	d := func(n int) string { return string(placeholderDiacritics[n]) }
	term.Process([]byte("\x1b[3;5H" + ph + "\U0010EEEE" + d(0) + d(0) + "\U0010EEEE" + d(0) + d(1)))
	term.Process([]byte("\x1b[4;5H" + ph + "\U0010EEEE" + d(1) + d(0) + "\U0010EEEE" + d(1) + d(1)))

	p, ok := draw()
	if !ok {
		t.Fatal("not drawn with placeholders on screen")
	}
	if wantCol, wantRow := 4+1, f.ViewTopAbs+2+1; p.AnchorCol != wantCol || p.AnchorAbsRow != wantRow {
		t.Fatalf("anchor (%d,%d), want (%d,%d)", p.AnchorCol, p.AnchorAbsRow, wantCol, wantRow)
	}
	for _, q := range f.Placements {
		if q == p {
			t.Fatal("drew the store's own placement instead of an anchored copy")
		}
	}
}
