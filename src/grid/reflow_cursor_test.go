package grid

import (
	"strings"
	"testing"
)

// TestReflowCursorPreservedAfterHistoryTrim proves the cursor follows its
// content even when reflow produces more than maxScroll history rows and the
// front of history is trimmed. With the off-by-drop bug the cursor was parked
// on the last active row (clamped) instead of its true interior row.
//
// Setup: width 10, 40 cells (one soft-wrapped logical line) with a unique '@'
// marker at logical offset 30. Cursor is placed on the marker before resizing
// to width 5. At width 5 the line is 8 segments; the active region is the last
// 3 (segments 5,6,7). The marker at offset 30 lands in segment 6 -> active row
// 1. History becomes 5 rows, capped to maxScroll=2, so drop=3.
//
//	buggy:   CursorRow = cursorNewAbsRow(6) - postTrimActiveStart(2) = 4 -> clamp 2
//	correct: CursorRow = cursorNewAbsRow(6) - preTrimActiveStart(5)  = 1
func TestReflowCursorPreservedAfterHistoryTrim(t *testing.T) {
	g := newGrid(10, 3, 2) // small cap => trim fires without thousands of lines

	s := []rune(strings.Repeat("x", 40))
	s[30] = '@'
	writeStr(g, string(s))

	// Place the cursor ON the marker (history=1 row, marker at active row 2 col 0)
	// so it is NOT on the last active row after reflow.
	g.CursorRow, g.CursorCol = 2, 0
	if got := g.DisplayCell(g.CursorCol, g.CursorRow).Char; got != '@' {
		t.Fatalf("precondition: cell under cursor = %q, want '@'", got)
	}

	g.Resize(5, 3) // 40 cells / 5 = 8 rows >> cap 2 => front trim

	col, row := g.GetCursor()
	if col < 0 || col >= g.Cols || row < 0 || row >= g.Rows {
		t.Fatalf("cursor out of bounds: (%d,%d) on %dx%d", col, row, g.Cols, g.Rows)
	}
	if row != 1 {
		t.Fatalf("CursorRow = %d, want 1 (off-by-drop bug parks it on row 2)", row)
	}
	if !strings.ContainsRune(line(g, row), '@') {
		t.Fatalf("cursor row %d = %q does not contain marker '@'", row, line(g, row))
	}
	if got := g.DisplayCell(col, row).Char; got != '@' {
		t.Fatalf("cell under cursor = %q, want '@'", got)
	}
}

// TestReflowTrimShapeAndRefcount checks structural invariants and style
// refcount balance across a >maxScroll front-trim: len(rows)==Rows,
// len(history)<=maxScroll, and no style leak/double-free.
func TestReflowTrimShapeAndRefcount(t *testing.T) {
	g := newGrid(10, 3, 2)
	writeStr(g, strings.Repeat("a", 50)) // long soft-wrapped line, default style
	before := g.styles.liveCount()

	g.Resize(5, 3) // 50 cells / 5 = 10 rows >> cap 2 => front trim

	if len(g.rows) != g.Rows {
		t.Fatalf("len(g.rows)=%d, want Rows=%d", len(g.rows), g.Rows)
	}
	if len(g.history) > g.maxScroll {
		t.Fatalf("history=%d exceeds cap %d", len(g.history), g.maxScroll)
	}
	// No leak / no double-free: default-style content is untracked (id 0), so
	// liveCount must not have grown and must stay sane.
	if lc := g.styles.liveCount(); lc > before {
		t.Fatalf("liveCount=%d after trim grew past before=%d (leak)", lc, before)
	}
	// Every surviving cell's style id must still resolve (not freed/dangling).
	for _, r := range append(append([]*Row{}, g.history...), g.rows...) {
		for _, sc := range r.cells {
			_ = g.styles.resolve(sc.Style)
		}
	}
}

// TestReflowCursorSimpleGrowShrink is a regression guard for the non-trim path:
// the cursor tracks the end of "hello" across shrink then grow.
func TestReflowCursorSimpleGrowShrink(t *testing.T) {
	g := newGrid(10, 4, 1000) // big cap: trim path not exercised
	writeStr(g, "hello")      // cursor at (5,0)

	g.Resize(3, 4) // shrink
	c, r := g.GetCursor()
	if c < 0 || c >= 3 || r < 0 || r >= 4 {
		t.Fatalf("shrink: cursor oob (%d,%d)", c, r)
	}

	g.Resize(20, 4) // grow back, line rejoins
	c, r = g.GetCursor()
	if c < 0 || c >= 20 || r < 0 || r >= 4 {
		t.Fatalf("grow: cursor oob (%d,%d)", c, r)
	}
	if c != 5 || r != 0 {
		t.Fatalf("cursor = (%d,%d) after grow, want (5,0) at end of 'hello'", c, r)
	}
}

// TestReflowCursorNonNegative documents the lower-bound guards under an extreme
// narrow shrink that also trims history.
func TestReflowCursorNonNegative(t *testing.T) {
	g := newGrid(8, 3, 2)
	writeStr(g, strings.Repeat("q", 40))

	g.Resize(2, 2) // extreme narrow shrink + trim

	c, r := g.GetCursor()
	if c < 0 || r < 0 {
		t.Fatalf("cursor went negative after reflow: (%d,%d)", c, r)
	}
	if len(g.rows) != g.Rows {
		t.Fatalf("len(g.rows)=%d != Rows=%d", len(g.rows), g.Rows)
	}
}
