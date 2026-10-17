package grid

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// checkRows asserts the invariants the scroll and erase fast paths rely on,
// over every active and history row:
//   - Row.blankTail: every cell at or past used() is blankStored;
//   - style refcounts: each live style's refs equals the number of cells
//     holding it, and no cell holds a freed id.
func checkRows(t *testing.T, g *Grid, step int, op string) {
	t.Helper()
	held := map[StyleID]uint32{}
	for _, rows := range [][]*Row{g.history, g.rows} {
		for i, r := range rows {
			if u := r.used(); u < 0 || u > len(r.cells) {
				t.Fatalf("step %d (%s): row %d used()=%d out of [0,%d]", step, op, i, u, len(r.cells))
			}
			for c := r.used(); c < len(r.cells); c++ {
				if r.cells[c] != blankStored {
					t.Fatalf("step %d (%s): row %d col %d is %+v inside the claimed blank tail (used=%d)",
						step, op, i, c, r.cells[c], r.used())
				}
			}
			for _, sc := range r.cells {
				if sc.Style != 0 {
					held[sc.Style]++
				}
			}
		}
	}
	for id, n := range held {
		if int(id) >= len(g.styles.entries) || g.styles.entries[id] == nil {
			t.Fatalf("step %d (%s): %d cells hold freed style %d", step, op, n, id)
		}
		if e := g.styles.entries[id]; e.refs != n {
			t.Fatalf("step %d (%s): style %d refs=%d, cells holding it=%d", step, op, id, e.refs, n)
		}
	}
	for id, e := range g.styles.entries {
		if e != nil && held[StyleID(id)] == 0 {
			t.Fatalf("step %d (%s): style %d live with refs=%d but no cell holds it", step, op, id, e.refs)
		}
	}
}

// TestBlankTailRandomOps drives two grids through the same random mix of
// writes, erases, scrolls, insert/delete, rect ops, line sizes and resizes.
// Grid a writes ASCII through WriteBytesLocked (the bulk run path); grid b
// writes the same text one rune at a time. They must stay identical, and both
// must keep the blank-tail and refcount invariants after every operation.
func TestBlankTailRandomOps(t *testing.T) {
	for seed := range uint64(40) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, 7))
			a, b := newGrid(12, 6, 20), newGrid(12, 6, 20)
			color := func() Color {
				if rng.IntN(3) == 0 {
					return IndexedColor(uint8(rng.IntN(4)))
				}
				return DefaultBg()
			}
			for step := range 600 {
				op, apply := randomOp(rng, color, a.Cols, a.Rows)
				switch op {
				case "bytes":
					n := rng.IntN(2 * a.Cols)
					bs := make([]byte, n)
					for i := range bs {
						bs[i] = byte(0x20 + rng.IntN(0x5f))
					}
					fg, bg := color(), color()
					a.WriteBytesLocked(bs, fg, bg, 0, 0, 0, Color{})
					writeSeq(b, []rune(string(bs)), fg, bg, 0, 0, 0, Color{})
				default:
					apply(a)
					apply(b)
				}
				checkRows(t, a, step, op)
				checkRows(t, b, step, op)
				gridsEqual(t, a, b)
			}
		})
	}
}

// randomOp picks one grid operation. "bytes" is handled by the caller, since
// it is applied differently to the two grids.
func randomOp(rng *rand.Rand, color func() Color, cols, rows int) (string, func(*Grid)) {
	n := 1 + rng.IntN(4)
	c, r := 1+rng.IntN(cols), 1+rng.IntN(rows)
	bg := color()
	switch rng.IntN(24) {
	case 0, 1, 2, 3, 4:
		return "bytes", nil
	case 5:
		rs := []rune{'世', 'a', '́', '界', 'x', '🙂'}
		k := rng.IntN(len(rs))
		fg := color()
		return "runes", func(g *Grid) { g.WriteRunesLocked(rs[k:], fg, bg, 0, 0, 0, Color{}) }
	case 6, 7:
		return "crlf", func(g *Grid) { g.CarriageReturnLocked(); g.NewlineLocked() }
	case 8:
		return "lf", func(g *Grid) { g.NewlineLocked() }
	case 9:
		return "cup", func(g *Grid) { g.SetCursorPosLocked(c, r) }
	case 10:
		return "el", func(g *Grid) {
			switch n % 3 {
			case 0:
				g.ClearLineToEndWithBgLocked(bg)
			case 1:
				g.ClearLineToStartWithBgLocked(bg)
			default:
				g.ClearLineWithBgLocked(bg)
			}
		}
	case 11:
		return "ed", func(g *Grid) {
			switch n % 3 {
			case 0:
				g.ClearToEndWithBgLocked(bg)
			case 1:
				g.ClearToStartWithBgLocked(bg)
			default:
				g.ClearAllWithBgLocked(bg)
			}
		}
	case 12:
		return "ich", func(g *Grid) { g.InsertCharsLocked(n) }
	case 13:
		return "dch", func(g *Grid) { g.DeleteCharsLocked(n) }
	case 14:
		return "ech", func(g *Grid) { g.EraseCharsLocked(n) }
	case 15:
		return "il/dl", func(g *Grid) {
			if n%2 == 0 {
				g.InsertLinesWithBgLocked(n, bg)
			} else {
				g.DeleteLinesWithBgLocked(n, bg)
			}
		}
	case 16:
		return "su/sd", func(g *Grid) {
			if n%2 == 0 {
				g.ScrollUpWithBgLocked(n, bg)
			} else {
				g.ScrollDownWithBgLocked(n, bg)
			}
		}
	case 17:
		top, bot := min(r, rows-1), max(r+n, 2)
		return "decstbm", func(g *Grid) {
			if n == 4 {
				g.SetScrollRegionLocked(1, g.Rows)
			} else {
				g.SetScrollRegionLocked(top, min(bot, g.Rows))
			}
		}
	case 18:
		left, right := min(c, cols-1), cols
		return "rect", func(g *Grid) {
			switch n {
			case 1:
				g.ScrollRectUpLocked(1, g.Rows, left, right, 1, bg)
			case 2:
				g.ScrollRectDownLocked(1, g.Rows, left, right, 1, bg)
			case 3:
				g.ScrollRectLeftLocked(1, g.Rows, left, right, 1, bg)
			default:
				g.ScrollRectRightLocked(1, g.Rows, left, right, 1, bg)
			}
		}
	case 19:
		sizes := []LineSize{LineSingle, LineDoubleWidth, LineDoubleTop, LineSingle}
		return "linesize", func(g *Grid) { g.SetLineSizeLocked(sizes[n-1]) }
	case 20:
		return "erasebg", func(g *Grid) { g.SetEraseBackgroundLocked(bg) }
	case 21:
		nc, nr := 4+rng.IntN(14), 2+rng.IntN(8)
		return "resize", func(g *Grid) { g.Resize(nc, nr) }
	case 22:
		return "rep/tab/bs", func(g *Grid) {
			switch n {
			case 1:
				g.RepeatCharLocked(n + 2)
			case 2:
				g.TabLocked()
			case 3:
				g.BackspaceLocked()
			default:
				g.SetAutoWrapLocked(!g.GetAutoWrapLocked())
			}
		}
	default:
		return "setcell", func(g *Grid) {
			g.SetCell(min(c-1, g.Cols-1), min(r-1, g.Rows-1), NewCellWithBg(bg))
		}
	}
}
