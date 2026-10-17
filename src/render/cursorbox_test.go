package render

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

func TestCursorBox(t *testing.T) {
	const ox, oy, cw, ch = 10, 20, 8, 16
	sizes := func(s ...grid.LineSize) *grid.Snapshot { return &grid.Snapshot{LineSizes: s} }
	cases := []struct {
		name       string
		snap       *grid.Snapshot
		col, row   int
		x, y, w, h float32
		glyphTop   float32
	}{
		{"single", sizes(grid.LineSingle, grid.LineSingle), 3, 1, ox + 3*cw, oy + ch, cw, ch, oy + ch},
		{"double width", sizes(grid.LineDoubleWidth), 3, 0, ox + 6*cw, oy, 2 * cw, ch, oy},
		{"top of pair", sizes(grid.LineSingle, grid.LineDoubleTop, grid.LineDoubleBottom), 2, 1, ox + 4*cw, oy + ch, 2 * cw, 2 * ch, oy + ch},
		{"bottom of pair", sizes(grid.LineSingle, grid.LineDoubleTop, grid.LineDoubleBottom), 2, 2, ox + 4*cw, oy + ch, 2 * cw, 2 * ch, oy + ch},
		{"lone top", sizes(grid.LineDoubleTop, grid.LineSingle), 1, 0, ox + 2*cw, oy, 2 * cw, ch, oy},
		{"lone bottom", sizes(grid.LineSingle, grid.LineDoubleBottom), 1, 1, ox + 2*cw, oy + ch, 2 * cw, ch, oy},
	}
	for _, tc := range cases {
		b := cursorBox(tc.snap, tc.col, tc.row, ox, oy, cw, ch)
		if b.x != tc.x || b.y != tc.y || b.w != tc.w || b.h != tc.h || b.glyphTop != tc.glyphTop {
			t.Errorf("%s: got %+v, want x=%v y=%v w=%v h=%v glyphTop=%v", tc.name, b, tc.x, tc.y, tc.w, tc.h, tc.glyphTop)
		}
	}
}
