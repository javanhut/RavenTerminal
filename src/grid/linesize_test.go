package grid

import "testing"

func TestLineSizeHalvesUsableColumns(t *testing.T) {
	g := newGrid(10, 4, 100)
	g.SetLineSizeLocked(LineDoubleWidth)
	if g.LineSize(0) != LineDoubleWidth {
		t.Fatalf("size = %v", g.LineSize(0))
	}
	writeStr(g, "abcdefg")
	if got := line(g, 0); got != "abcde" {
		t.Fatalf("row 0 = %q, want wrap at cols/2", got)
	}
	if got := line(g, 1); got != "fg" {
		t.Fatalf("row 1 = %q", got)
	}
	if g.LineSize(1) != LineSingle {
		t.Fatal("wrapped-onto row inherited the size")
	}
}

func TestLineSizeClampsCursor(t *testing.T) {
	g := newGrid(10, 4, 100)
	g.SetCursorLocked(8, 0)
	g.SetLineSizeLocked(LineDoubleTop)
	if c, _ := g.GetCursor(); c != 4 {
		t.Fatalf("cursor col = %d after DECDHL, want 4", c)
	}
	g.SetCursorLocked(9, 0)
	if c, _ := g.GetCursor(); c != 4 {
		t.Fatalf("CUP past half: col %d", c)
	}
	g.SetCursorPos(10, 1)
	if c, _ := g.GetCursor(); c != 4 {
		t.Fatalf("SetCursorPos past half: col %d", c)
	}
	g.MoveCursor(5, 0)
	if c, _ := g.GetCursor(); c != 4 {
		t.Fatalf("MoveCursor past half: col %d", c)
	}
	g.SetCursorLocked(9, 1)
	if c, _ := g.GetCursor(); c != 9 {
		t.Fatalf("single-width row clamped: col %d", c)
	}
	g.SetCursorLocked(0, 0)
	writeStr(g, "abcd")
	g.Tab()
	if c, _ := g.GetCursor(); c != 4 {
		t.Fatalf("tab past half: col %d", c)
	}
}

func TestLineSizeSurvivesScroll(t *testing.T) {
	g := newGrid(10, 3, 100)
	g.SetCursorLocked(0, 1)
	g.SetLineSizeLocked(LineDoubleWidth)
	g.SetCursorLocked(0, 2)
	g.Newline() // scrolls: row 1 moves to row 0
	if g.LineSize(0) != LineDoubleWidth || g.LineSize(1) != LineSingle || g.LineSize(2) != LineSingle {
		t.Fatalf("sizes after scroll = %v %v %v", g.LineSize(0), g.LineSize(1), g.LineSize(2))
	}
	g.Newline() // into scrollback
	g.ScrollViewUp(1)
	s := g.Snapshot(nil)
	if s.LineSize(0) != LineDoubleWidth || s.LineSize(1) != LineSingle {
		t.Fatalf("scrollback snapshot sizes = %v %v", s.LineSize(0), s.LineSize(1))
	}
}

func TestLineSizeInSnapshot(t *testing.T) {
	g := newGrid(10, 3, 100)
	s := g.Snapshot(nil)
	g.SetCursorLocked(0, 1)
	g.SetLineSizeLocked(LineDoubleBottom)
	s = g.Snapshot(s) // incremental path
	if s.LineSize(1) != LineDoubleBottom || s.LineSize(0) != LineSingle {
		t.Fatalf("snapshot sizes = %v %v", s.LineSize(0), s.LineSize(1))
	}
	if !s.Dirty {
		t.Fatal("size change did not dirty the snapshot")
	}
	g.SetLineSizeLocked(LineSingle)
	s = g.Snapshot(s)
	if s.LineSize(1) != LineSingle {
		t.Fatal("reset not reflected")
	}
	if s.LineSize(-1) != LineSingle || s.LineSize(99) != LineSingle {
		t.Fatal("out of range should be single")
	}
}

func TestLineSizeClearedByED(t *testing.T) {
	g := newGrid(10, 4, 100)
	for r := range 4 {
		g.SetCursorLocked(0, r)
		g.SetLineSizeLocked(LineDoubleWidth)
	}
	g.SetCursorLocked(2, 1)
	g.ClearToEndWithBg(DefaultBg()) // ED 0: rows below reset, cursor row kept
	if g.LineSize(1) != LineDoubleWidth || g.LineSize(2) != LineSingle || g.LineSize(3) != LineSingle {
		t.Fatalf("ED 0 sizes = %v %v %v", g.LineSize(1), g.LineSize(2), g.LineSize(3))
	}
	g.ClearToStartWithBg(DefaultBg()) // ED 1: rows above reset
	if g.LineSize(0) != LineSingle || g.LineSize(1) != LineDoubleWidth {
		t.Fatalf("ED 1 sizes = %v %v", g.LineSize(0), g.LineSize(1))
	}
	g.ClearLineWithBg(DefaultBg()) // EL leaves the line attribute (xterm)
	if g.LineSize(1) != LineDoubleWidth {
		t.Fatal("EL 2 reset the line size")
	}
	g.ClearAllWithBg(DefaultBg()) // ED 2
	if g.LineSize(1) != LineSingle {
		t.Fatal("ED 2 kept the line size")
	}
}

func TestLineSizeResetByDECALNAndNewRows(t *testing.T) {
	g := newGrid(10, 4, 100)
	g.SetLineSizeLocked(LineDoubleWidth)
	g.FillScreen('E')
	if g.LineSize(0) != LineSingle {
		t.Fatal("DECALN kept the line size")
	}
	g.SetLineSizeLocked(LineDoubleWidth)
	g.InsertLines(1)
	if g.LineSize(0) != LineSingle || g.LineSize(1) != LineDoubleWidth {
		t.Fatalf("IL sizes = %v %v", g.LineSize(0), g.LineSize(1))
	}
}

func TestLineSizeSurvivesReflow(t *testing.T) {
	g := newGrid(20, 4, 100)
	writeStr(g, "plain line that wraps")
	g.CarriageReturn()
	g.Newline()
	g.SetLineSizeLocked(LineDoubleTop)
	writeStr(g, "BIG")
	g.CarriageReturn()
	g.Newline()
	g.SetLineSizeLocked(LineDoubleBottom)
	writeStr(g, "BIG")
	g.Resize(12, 6)
	var found []LineSize
	s := g.Snapshot(nil)
	for r := range s.Rows {
		if s.LineSize(r) != LineSingle {
			found = append(found, s.LineSize(r))
			if txt := line(g, r); txt != "BIG" {
				t.Fatalf("sized row %d = %q", r, txt)
			}
		}
	}
	if len(found) != 2 || found[0] != LineDoubleTop || found[1] != LineDoubleBottom {
		t.Fatalf("sizes after reflow = %v", found)
	}
	if c, _ := g.GetCursor(); c != 3 {
		t.Fatalf("cursor col = %d", c)
	}
	g.Resize(4, 6) // narrower than the sized content: truncated to cols/2, never split
	s = g.Snapshot(nil)
	n := 0
	for r := range s.Rows {
		if s.LineSize(r) != LineSingle {
			n++
			if txt := line(g, r); txt != "BI" {
				t.Fatalf("narrow sized row = %q", txt)
			}
		}
	}
	if n != 2 {
		t.Fatalf("sized rows after narrow reflow = %d", n)
	}
}

func TestLineSizeAltScreenResizeKeepsSize(t *testing.T) {
	g := NewAltGrid(10, 3)
	g.SetCursorLocked(0, 1)
	g.SetLineSizeLocked(LineDoubleWidth)
	g.Resize(12, 4)
	if g.LineSize(1) != LineDoubleWidth {
		t.Fatal("alt resize dropped the line size")
	}
}

func TestViewLineSizeFollowsScrollback(t *testing.T) {
	g := NewGrid(10, 3)
	g.mu.Lock()
	g.SetLineSizeLocked(LineDoubleWidth)
	g.mu.Unlock()
	if g.ViewLineSize(0) != LineDoubleWidth || g.ViewLineSize(1) != LineSingle {
		t.Fatalf("view sizes %v %v, want double then single", g.ViewLineSize(0), g.ViewLineSize(1))
	}
	for range 3 {
		g.Newline()
	}
	if g.ViewLineSize(0) != LineSingle {
		t.Fatal("the double row scrolled off but view row 0 still reports it")
	}
	g.ScrollViewToAbs(g.ViewTopAbs() - 3)
	if g.ViewLineSize(0) != LineDoubleWidth {
		t.Fatalf("scrolled back to it: view row 0 = %v, want double width", g.ViewLineSize(0))
	}
	if !LineDoubleTop.Doubled() || !LineDoubleWidth.Doubled() || LineSingle.Doubled() {
		t.Fatal("Doubled")
	}
}
