package grid

import (
	"testing"
	"time"
)

func TestInsertCharsBlanksSplitWideChar(t *testing.T) {
	g := NewGrid(10, 2)
	writeStr(g, "abcdefgh中")
	g.SetCursorPos(1, 1)
	g.InsertChars(1)
	if c := g.GetCell(9, 0); c.Width == CellWidthWide {
		t.Fatalf("orphan wide start at last column: %+v", c)
	}
	if c := g.GetCell(8, 0); c.Char != 'h' {
		t.Fatalf("col 8 = %q, want h", c.Char)
	}
}

func TestKeycapEmojiIsWide(t *testing.T) {
	for _, s := range []string{"1️⃣", "#️⃣", "*️", "7️"} {
		g := NewGrid(10, 2)
		writeStr(g, s)
		if col, _ := g.GetCursor(); col != 2 {
			t.Fatalf("%q: cursor col %d, want 2", s, col)
		}
		if w := ClusterWidth(s); w != 2 {
			t.Fatalf("ClusterWidth(%q) = %d, want 2", s, w)
		}
	}
	if w := ClusterWidth("1⃣"); w != 1 {
		t.Fatalf("text-presentation keycap width %d, want 1", w)
	}
}

func TestAltGridScrollAdvancesAbsoluteRows(t *testing.T) {
	g := NewAltGrid(4, 2)
	g.SetCursorPos(1, 2)
	before := g.AbsoluteCursorRow()
	for range 3 {
		g.Newline()
	}
	if got := g.AbsoluteCursorRow() - before; got != 3 {
		t.Fatalf("absolute row advanced %d, want 3", got)
	}
}

func TestHugeScrollCountsBounded(t *testing.T) {
	g := NewGrid(10, 4)
	writeStr(g, "abc")
	done := make(chan struct{})
	go func() {
		g.ScrollUp(1 << 62)
		g.ScrollDown(1 << 62)
		g.ScrollUpWithBg(1<<62, DefaultBg())
		g.ScrollDownWithBg(1<<62, DefaultBg())
		g.RepeatChar(1 << 62)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unbounded scroll/repeat loop")
	}
}
