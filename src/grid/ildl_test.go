package grid

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// TestInsertDeleteLinesMatchModel checks IL/DL against a plain model of the
// screen as a list of line strings: lines inside the scroll region shift by
// n from the cursor row, the vacated lines are blank, and everything outside
// the region and above the cursor is untouched.
func TestInsertDeleteLinesMatchModel(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for iter := range 3000 {
		cols, rows := 3+rng.IntN(6), 2+rng.IntN(8)
		g := newGrid(cols, rows, 10)
		model := make([]string, rows)
		for r := range rows {
			model[r] = fmt.Sprintf("%c%d", 'a'+r, iter%10)
			g.SetCursorPosLocked(1, r+1)
			g.WriteBytesLocked([]byte(model[r]), DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
		}
		top := 1 + rng.IntN(rows)
		bottom := top + rng.IntN(rows-top+1)
		g.SetScrollRegionLocked(top, bottom)
		top, bottom = g.GetScrollRegionLocked() // DECSTBM may refuse the request
		cur := 1 + rng.IntN(rows)
		g.SetCursorPosLocked(1, cur)
		n := 1 + rng.IntN(rows+1)
		insert := rng.IntN(2) == 0

		want := slices.Clone(model)
		if t0, b0, c0 := top-1, bottom-1, cur-1; c0 >= t0 && c0 <= b0 {
			span := want[c0 : b0+1]
			k := min(n, len(span))
			if insert {
				copy(span[k:], slices.Clone(span[:len(span)-k]))
				for i := range k {
					span[i] = ""
				}
			} else {
				copy(span, slices.Clone(span[k:]))
				for i := len(span) - k; i < len(span); i++ {
					span[i] = ""
				}
			}
		}
		if insert {
			g.InsertLinesWithBgLocked(n, DefaultBg())
		} else {
			g.DeleteLinesWithBgLocked(n, DefaultBg())
		}
		for r := range rows {
			if got := line(g, r); got != want[r] {
				t.Fatalf("iter %d: %dx%d region %d-%d cursor %d insert=%v n=%d: row %d = %q, want %q",
					iter, cols, rows, top, bottom, cur, insert, n, r, got, want[r])
			}
		}
		checkRows(t, g, iter, "il/dl")
	}
}
