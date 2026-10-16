package grid

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func cellText(g *Grid, col, row int) string { return g.DisplayCell(col, row).Text() }

// --- reflow -----------------------------------------------------------------

func TestResizeRowsOnlySkipsReflow(t *testing.T) {
	g := newGrid(10, 5, 100)
	writeStr(g, "abcdefghijkl") // soft-wraps
	r0, r1 := g.rows[0], g.rows[1]
	g.Resize(10, 8)
	if g.rows[0] != r0 || g.rows[1] != r1 {
		t.Fatal("rows-only grow rebuilt rows instead of keeping them")
	}
	if line(g, 0) != "abcdefghij" || line(g, 1) != "kl" {
		t.Fatalf("content changed: %q %q", line(g, 0), line(g, 1))
	}
	if c, r := g.GetCursor(); c != 2 || r != 1 {
		t.Fatalf("cursor = (%d,%d), want (2,1)", c, r)
	}
	g.Resize(10, 2)
	if line(g, 0) != "abcdefghij" || line(g, 1) != "kl" {
		t.Fatalf("shrink lost content: %q %q", line(g, 0), line(g, 1))
	}
	for range 6 {
		g.CarriageReturn()
		g.Newline()
	}
	g.Resize(10, 1)
	if c, r := g.GetCursor(); r != 0 || c != 0 {
		t.Fatalf("cursor = (%d,%d) after shrink to 1 row", c, r)
	}
}

func TestReflowWidePadNotContent(t *testing.T) {
	g := newGrid(10, 4, 100)
	writeStr(g, "abcdefghi中xy") // 中 does not fit at col 9: padded + wrapped
	if c, r := g.GetCursor(); c != 4 || r != 1 {
		t.Fatalf("precondition cursor (%d,%d)", c, r)
	}
	g.Resize(20, 4)
	if got := line(g, 0); got != "abcdefghi中 xy" { // 中's continuation cell renders as ' '
		t.Fatalf("rejoined line = %q (pad leaked into content)", got)
	}
	if g.DisplayCell(9, 0).Char != '中' {
		t.Fatalf("col 9 = %q, want 中", g.DisplayCell(9, 0).Char)
	}
	if c, r := g.GetCursor(); c != 13 || r != 0 {
		t.Fatalf("cursor = (%d,%d), want (13,0)", c, r)
	}
}

func TestReflowCursorWithPushedWideChar(t *testing.T) {
	g := newGrid(20, 4, 100)
	writeStr(g, "abcdefghi中xy") // cursor col 13
	g.Resize(10, 4)
	if g.DisplayCell(0, 1).Char != '中' {
		t.Fatalf("row1 col0 = %q, want 中", g.DisplayCell(0, 1).Char)
	}
	if c, r := g.GetCursor(); c != 4 || r != 1 {
		t.Fatalf("cursor = (%d,%d), want (4,1)", c, r)
	}
}

func TestReflowCursorOnBlankLineKeepsColumn(t *testing.T) {
	g := newGrid(20, 5, 100)
	writeStr(g, "hi")
	g.CarriageReturn()
	g.Newline()
	g.SetCursorPos(8, 2)
	g.Resize(15, 5)
	if c, r := g.GetCursor(); c != 7 || r != 1 {
		t.Fatalf("cursor = (%d,%d), want (7,1)", c, r)
	}
}

func TestReflowCursorBeyondContent(t *testing.T) {
	g := newGrid(20, 5, 100)
	writeStr(g, "abc")
	g.SetCursorPos(16, 1) // col 15, past the content
	g.Resize(10, 5)
	if c, r := g.GetCursor(); c != 5 || r != 1 {
		t.Fatalf("shrink cursor = (%d,%d), want (5,1)", c, r)
	}
	g.Resize(20, 5)
	if c, r := g.GetCursor(); c != 15 || r != 0 {
		t.Fatalf("grow cursor = (%d,%d), want (15,0)", c, r)
	}
	if line(g, 0) != "abc" {
		t.Fatalf("line = %q", line(g, 0))
	}
}

func TestReflowPreservesWrapPending(t *testing.T) {
	g := newGrid(10, 5, 100)
	writeStr(g, "abcdefghij") // wrap pending at col 9
	g.Resize(5, 5)
	writeStr(g, "X")
	if line(g, 0) != "abcde" || line(g, 1) != "fghij" || line(g, 2) != "X" {
		t.Fatalf("rows = %q %q %q", line(g, 0), line(g, 1), line(g, 2))
	}
}

func TestReflowRemapsTrackedPoints(t *testing.T) {
	g := newGrid(10, 4, 100)
	writeStr(g, "abcdefghijklmno") // rows: abcdefghij / klmno
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "zz")
	saved := Point{Col: 3, Row: 1} // on 'n'
	g.ResizeTracking(5, 4, &saved)
	if got := g.DisplayCell(saved.Col, saved.Row).Char; got != 'n' {
		t.Fatalf("saved point -> (%d,%d) = %q, want 'n'", saved.Col, saved.Row, got)
	}
	g.ResizeTracking(20, 4, &saved)
	if saved != (Point{Col: 13, Row: 0}) {
		t.Fatalf("saved point after grow = %+v, want {13 0}", saved)
	}
}

func TestReflowKeepsTabStops(t *testing.T) {
	g := newGrid(20, 4, 100)
	g.SetCursorPos(4, 1)
	g.SetTabStop()
	g.Resize(30, 4)
	g.SetCursorPos(1, 1)
	g.Tab()
	if c, _ := g.GetCursor(); c != 3 {
		t.Fatalf("tab -> col %d, want custom stop 3", c)
	}
	g.Tab()
	if c, _ := g.GetCursor(); c != 8 {
		t.Fatalf("tab -> col %d, want 8", c)
	}
	g.SetCursorPos(21, 1)
	g.Tab()
	if c, _ := g.GetCursor(); c != 24 {
		t.Fatalf("tab in grown area -> col %d, want default stop 24", c)
	}
}

func TestReflowAllocationsBounded(t *testing.T) {
	g := newGrid(100, 30, 2000)
	for i := range 1500 {
		writeStr(g, strings.Repeat(string(rune('a'+i%26)), 30+i%150))
		g.CarriageReturn()
		g.Newline()
	}
	w := 100
	allocs := testing.AllocsPerRun(4, func() {
		if w == 100 {
			w = 70
		} else {
			w = 100
		}
		g.Resize(w, 30)
	})
	if allocs > 200 {
		t.Fatalf("reflow allocated %.0f times per resize, want O(1) (<=200)", allocs)
	}
}

func TestReflowStyleRefcountsBalanced(t *testing.T) {
	g := newGrid(10, 4, 100)
	for i := range 30 {
		g.WriteRunes([]rune(strings.Repeat("x", 3+i%17)), IndexedColor(uint8(1+i%5)), DefaultBg(), 0, 0, 0, Color{})
		g.CarriageReturn()
		g.Newline()
	}
	for _, w := range []int{7, 13, 4, 10} {
		g.Resize(w, 4)
	}
	counts := map[StyleID]uint32{}
	for _, r := range append(append([]*Row{}, g.history...), g.rows...) {
		for _, sc := range r.cells {
			if sc.Style != 0 {
				counts[sc.Style]++
			}
		}
	}
	for id, e := range g.styles.entries {
		if e == nil {
			continue
		}
		if e.refs != counts[StyleID(id)] {
			t.Fatalf("style %d refs=%d, cells=%d", id, e.refs, counts[StyleID(id)])
		}
	}
	for id, n := range counts {
		if e := g.styles.entries[id]; e == nil {
			t.Fatalf("style %d used by %d cells but freed", id, n)
		}
	}
}

// --- grapheme clustering ----------------------------------------------------

func TestGraphemeVS16Widens(t *testing.T) {
	g := NewGrid(10, 2)
	writeStr(g, "❤️x")
	c := g.GetCell(0, 0)
	if c.Width != CellWidthWide || c.Text() != "❤️" {
		t.Fatalf("cell0 = %q width %d", c.Text(), c.Width)
	}
	if g.GetCell(1, 0).Width != CellWidthContinuation {
		t.Fatal("cell1 not continuation")
	}
	if g.GetCell(2, 0).Char != 'x' {
		t.Fatalf("x landed at wrong col: %q", line(g, 0))
	}
}

func TestGraphemeVS15Narrows(t *testing.T) {
	g := NewGrid(10, 2)
	writeStr(g, "😀︎x")
	c := g.GetCell(0, 0)
	if c.Width != CellWidthNormal || c.Text() != "😀︎" {
		t.Fatalf("cell0 = %q width %d", c.Text(), c.Width)
	}
	if g.GetCell(1, 0).Char != 'x' {
		t.Fatalf("x at wrong col: cell1=%q", g.GetCell(1, 0).Char)
	}
}

func TestGraphemeClusters(t *testing.T) {
	cases := []struct {
		in        string
		cluster   string
		width     uint8
		cursorCol int
	}{
		{"🇺🇸", "🇺🇸", 2, 2},
		{"👍🏽", "👍🏽", 2, 2},
		{"👨‍👩‍👧", "👨‍👩‍👧", 2, 2},
		{"é", "é", 1, 1},
		{"कि", "कि", 2, 2},
	}
	for _, tc := range cases {
		g := NewGrid(10, 2)
		writeStr(g, tc.in)
		c := g.GetCell(0, 0)
		if c.Text() != tc.cluster || c.Width != tc.width {
			t.Errorf("%q: cell0 = %q width %d, want %q width %d", tc.in, c.Text(), c.Width, tc.cluster, tc.width)
		}
		if col, _ := g.GetCursor(); col != tc.cursorCol {
			t.Errorf("%q: cursor col %d, want %d", tc.in, col, tc.cursorCol)
		}
	}
}

func TestGraphemeThirdRegionalIndicatorStartsNewCluster(t *testing.T) {
	g := NewGrid(10, 2)
	writeStr(g, "🇺🇸🇩")
	if g.GetCell(0, 0).Text() != "🇺🇸" || g.GetCell(2, 0).Text() != "🇩" {
		t.Fatalf("cells = %q %q", g.GetCell(0, 0).Text(), g.GetCell(2, 0).Text())
	}
}

func TestGraphemeWidenAtLastColumnWraps(t *testing.T) {
	g := NewGrid(5, 3)
	writeStr(g, "abcd❤️")
	if line(g, 0) != "abcd" {
		t.Fatalf("row0 = %q", line(g, 0))
	}
	c := g.GetCell(0, 1)
	if c.Text() != "❤️" || c.Width != CellWidthWide {
		t.Fatalf("row1 col0 = %q width %d", c.Text(), c.Width)
	}
	if col, row := g.GetCursor(); col != 2 || row != 1 {
		t.Fatalf("cursor (%d,%d), want (2,1)", col, row)
	}
}

func TestGraphemeCombiningAcrossWrapPending(t *testing.T) {
	g := NewGrid(5, 3)
	writeStr(g, "abcdéf")
	if g.GetCell(4, 0).Text() != "é" {
		t.Fatalf("cell(4,0) = %q", g.GetCell(4, 0).Text())
	}
	if g.GetCell(0, 1).Char != 'f' {
		t.Fatalf("f not on next row: %q", line(g, 1))
	}
}

func TestGraphemeModeOffLegacy(t *testing.T) {
	g := NewGrid(10, 2)
	g.SetGraphemeClustering(false)
	writeStr(g, "🇺🇸")
	if col, _ := g.GetCursor(); col != 4 {
		t.Fatalf("legacy RI pair cursor col %d, want 4", col)
	}
	if g.GraphemeClustering() {
		t.Fatal("mode reports on")
	}
}

func TestGraphemeClusterCap(t *testing.T) {
	g := NewGrid(10, 2)
	writeStr(g, "a"+strings.Repeat("́", 100)+"b")
	c := g.GetCell(0, 0)
	if n := len([]rune(c.Text())); n > MaxClusterRunes {
		t.Fatalf("cluster holds %d runes, cap %d", n, MaxClusterRunes)
	}
	if g.GetCell(1, 0).Char != 'b' {
		t.Fatal("b misplaced")
	}
}

func TestGraphemeTableBounded(t *testing.T) {
	g := newGrid(80, 24, 100)
	for i := range 1_000_000 / 2 {
		g.WriteChar(rune('a'+i%26), DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
		g.WriteChar(rune(0x300+i%112), DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
		g.WriteChar(rune(0x300+(i/112)%112), DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	}
	live := (100 + 24) * 80
	if n := len(g.graphemes); n > 4*live+8192 {
		t.Fatalf("grapheme table has %d entries for <=%d live cells", n, live)
	}
	if n := len(g.graphemeMap); n > 4*live+8192 {
		t.Fatalf("grapheme map has %d entries", n)
	}
	// Live clusters must still resolve correctly after compaction.
	for _, r := range append(append([]*Row{}, g.history...), g.rows...) {
		for _, sc := range r.cells {
			if sc.Grapheme != 0 {
				if cl := g.graphemes[sc.Grapheme]; len(cl) == 0 || cl[0] != sc.Char {
					t.Fatalf("cell %q resolves to %q", sc.Char, string(cl))
				}
			}
		}
	}
}

func TestGraphemeMarkFloodMemory(t *testing.T) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	g := NewGrid(80, 24)
	g.WriteChar('a', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	for i := range 1_000_000 {
		g.WriteChar(rune(0x300+i%112), DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if grow := int64(after.HeapAlloc) - int64(before.HeapAlloc); grow > 8<<20 {
		t.Fatalf("heap grew %d bytes for 1e6 combining marks", grow)
	}
	runtime.KeepAlive(g)
}

// --- hyperlinks -------------------------------------------------------------

func TestLinkIDWrapNoStaleReuse(t *testing.T) {
	g := NewGrid(10, 2)
	first := g.InternLink("https://first.example/")
	g.WriteChar('L', DefaultFg(), DefaultBg(), 0, first, 0, Color{})
	seen := map[uint16]string{}
	for i := range 70000 {
		url := fmt.Sprintf("https://x.example/%d", i)
		id := g.InternLink(url)
		if id == 0 {
			t.Fatalf("intern %d returned 0", i)
		}
		if id == first {
			t.Fatalf("live id %d reused for %s", id, url)
		}
		if g.LinkURL(id) != url {
			t.Fatalf("id %d resolves to %q, want %q", id, g.LinkURL(id), url)
		}
		seen[id] = url
	}
	if got := g.LinkURL(g.GetCell(0, 0).Link); got != "https://first.example/" {
		t.Fatalf("on-screen link resolves to %q", got)
	}
}

// --- scrollback -------------------------------------------------------------

func TestSetMaxScrollback(t *testing.T) {
	g := NewGrid(10, 3)
	if g.MaxScrollbackRows() != 10000 {
		t.Fatalf("default scrollback %d", g.MaxScrollbackRows())
	}
	for i := range 103 {
		writeStr(g, fmt.Sprint(i))
		g.CarriageReturn()
		g.Newline()
	}
	if g.ScrollbackLen() != 101 {
		t.Fatalf("history %d, want 101", g.ScrollbackLen())
	}
	out := g.LinesScrolledOut()
	g.SetMaxScrollback(50)
	if g.ScrollbackLen() != 50 || g.LinesScrolledOut() != out+51 {
		t.Fatalf("after trim: history %d scrolledOut %d", g.ScrollbackLen(), g.LinesScrolledOut())
	}
	if rowText(g, g.history[0]) != "51" {
		t.Fatalf("oldest kept row %q, want 51", rowText(g, g.history[0]))
	}
	g.SetMaxScrollback(5_000_000)
	if g.MaxScrollbackRows() != MaxScrollbackLimit {
		t.Fatalf("cap not clamped: %d", g.MaxScrollbackRows())
	}
	g.SetMaxScrollback(0)
	if g.ScrollbackLen() != 0 {
		t.Fatal("scrollback 0 kept history")
	}
	g.SetCursorPos(1, 1)
	writeStr(g, "abcdefgh")
	g.Resize(5, 3) // still reflows (not clamps) with scrollback disabled
	if !strings.Contains(g.VisibleText(), "fgh") {
		t.Fatalf("reflow disabled with zero scrollback: %q", g.VisibleText())
	}
}

func TestLargeScrollbackLazyAndCompact(t *testing.T) {
	g := NewGrid(200, 5)
	g.SetMaxScrollback(MaxScrollbackLimit)
	for i := range 2000 {
		writeStr(g, "x")
		if i%3 == 0 {
			writeStr(g, "yz")
		}
		g.CarriageReturn()
		g.Newline()
	}
	if c := cap(g.histBuf); c > 16384 {
		t.Fatalf("history buffer preallocated %d slots", c)
	}
	cells := 0
	for _, r := range g.history {
		cells += cap(r.cells)
	}
	if cells > 2000*8 {
		t.Fatalf("history holds %d cells for 2000 short lines (not compacted)", cells)
	}
	g.ScrollViewUp(5)
	if got := g.DisplayCell(0, 0).Char; got != 'x' {
		t.Fatalf("scrolled view cell = %q", got)
	}
	if got := g.DisplayCell(150, 0); got.Char != ' ' || got.Bg.Type != ColorDefault {
		t.Fatalf("trimmed tail cell = %+v", got)
	}
}

func TestShortHistoryRowsSelection(t *testing.T) {
	g := NewGrid(20, 3)
	writeStr(g, "one")
	g.CarriageReturn()
	g.Newline()
	g.CarriageReturn()
	g.Newline() // blank line
	writeStr(g, "three")
	for range 3 {
		g.CarriageReturn()
		g.Newline()
	}
	g.SetEraseBackground(IndexedColor(4))
	g.ScrollViewUp(g.ScrollbackLen())
	g.SetSelection(0, 0, 19, 2)
	if got := g.SelectedText(); got != "one\n\nthree" {
		t.Fatalf("selected %q", got)
	}
	g.SelectWordAt(15, 0)
	if got := g.SelectedText(); got != "" {
		t.Fatalf("word beyond short row selected %q", got)
	}
	if bg := g.DisplayCell(10, 0).Bg; bg.Type != ColorDefault {
		t.Fatalf("history tail took erase bg %+v", bg)
	}
}

// --- wide-char cursor movement (xterm) --------------------------------------

func TestWideCursorMovesByColumns(t *testing.T) {
	g := NewGrid(10, 2)
	writeStr(g, "中文x") // cols 0-1, 2-3, 4
	g.Backspace()      // col 4 -> 3 (continuation of 文)
	if c, _ := g.GetCursor(); c != 4 {
		t.Fatalf("BS from 5 -> %d, want 4", c)
	}
	g.Backspace()
	if c, _ := g.GetCursor(); c != 3 {
		t.Fatalf("BS onto continuation -> %d, want 3", c)
	}
	g.MoveCursor(-2, 0)
	if c, _ := g.GetCursor(); c != 1 {
		t.Fatalf("CUB 2 -> %d, want 1", c)
	}
	g.MoveCursor(1, 0)
	if c, _ := g.GetCursor(); c != 2 {
		t.Fatalf("CUF 1 from 1 -> %d, want 2", c)
	}
	g.SetCursorPos(2, 1)
	if c, _ := g.GetCursor(); c != 1 {
		t.Fatalf("CUP to continuation -> %d, want 1", c)
	}
	g.SetCursorPos(1, 1)
	g.SetTabStop()
	g.ClearTabStop(3)
	g.SetCursorPos(4, 1)
	g.SetTabStop()
	g.SetCursorPos(1, 1)
	g.Tab()
	if c, _ := g.GetCursor(); c != 3 {
		t.Fatalf("HT onto continuation -> %d, want 3", c)
	}
}

func TestOverwriteHalfWideClearsOrphan(t *testing.T) {
	g := NewGrid(10, 2)
	writeStr(g, "中文")
	g.SetCursorPos(2, 1) // continuation of 中
	writeStr(g, "a")
	if c := g.GetCell(0, 0); c.Char != ' ' || c.Width != CellWidthNormal {
		t.Fatalf("orphaned base = %q w%d", c.Char, c.Width)
	}
	if c := g.GetCell(1, 0); c.Char != 'a' || c.Width != CellWidthNormal {
		t.Fatalf("cell1 = %q w%d", c.Char, c.Width)
	}
	g.SetCursorPos(3, 1) // base of 文
	writeStr(g, "b")
	if c := g.GetCell(3, 0); c.Char != ' ' || c.Width != CellWidthNormal {
		t.Fatalf("orphaned continuation = %q w%d", c.Char, c.Width)
	}
	// Wide char written over [4,5] where 5 is a wide base: its tail is cleared.
	g2 := NewGrid(10, 2)
	writeStr(g2, "ab中")   // 中 at 2-3
	g2.SetCursorPos(2, 1) // col 1
	writeStr(g2, "文")     // occupies 1-2, orphaning col 3
	if c := g2.GetCell(3, 0); c.Width != CellWidthNormal || c.Char != ' ' {
		t.Fatalf("orphan after wide overwrite = %q w%d", c.Char, c.Width)
	}
	// ASCII byte fast path also clears orphans.
	g3 := NewGrid(10, 2)
	writeStr(g3, "中")
	g3.SetCursorPos(1, 1)
	g3.LockBatch()
	g3.WriteBytesLocked([]byte("z"), DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	g3.UnlockBatch()
	if c := g3.GetCell(1, 0); c.Width != CellWidthNormal || c.Char != ' ' {
		t.Fatalf("byte path orphan = %q w%d", c.Char, c.Width)
	}
}

// --- snapshot ---------------------------------------------------------------

func TestSnapshotReuseAllocFree(t *testing.T) {
	g := NewGrid(80, 24)
	writeStr(g, "hello é world")
	s := g.Snapshot(nil)
	allocs := testing.AllocsPerRun(20, func() {
		writeStr(g, "x")
		s = g.Snapshot(s)
	})
	if allocs != 0 {
		t.Fatalf("snapshot reuse allocated %.1f times", allocs)
	}
}

func TestCellText(t *testing.T) {
	if (Cell{Char: 'a'}).Text() != "a" || (Cell{}).Text() != " " ||
		(Cell{Char: 'e', Combining: []rune{0x301}}).Text() != "é" {
		t.Fatal("Cell.Text")
	}
}
