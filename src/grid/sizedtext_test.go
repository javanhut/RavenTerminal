package grid

import "testing"

func putBlock(g *Grid, col, row int, st SizedText) {
	g.mu.Lock()
	g.PutSizedTextLocked(col, row, st)
	g.mu.Unlock()
}

func TestSizedTextOnEveryRowOfTheBlock(t *testing.T) {
	g := newGrid(20, 5, 100)
	putBlock(g, 3, 1, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	s := g.Snapshot(nil)
	for row, want := range []int{-1, 0, 1, -1} {
		got := s.SizedText(row)
		if want < 0 {
			if len(got) != 0 {
				t.Fatalf("row %d has %v", row, got)
			}
			continue
		}
		if len(got) != 1 || got[0].SubRow != want || got[0].Col != 3 || got[0].Text != "Hi" || got[0].Cells != 4 {
			t.Fatalf("row %d = %+v", row, got)
		}
	}
	if !s.Dirty {
		t.Fatal("block did not dirty the snapshot")
	}
}

func TestSizedTextDroppedWhenOverwritten(t *testing.T) {
	g := newGrid(20, 5, 100)
	putBlock(g, 2, 0, SizedText{Text: "Hi", Scale: 2, Cells: 4})
	putBlock(g, 10, 0, SizedText{Text: "Yo", Scale: 2, Cells: 4})
	g.SetCursorLocked(3, 1)
	writeStr(g, "x") // a lower cell of the first block
	s := g.Snapshot(nil)
	for row := range 2 {
		got := s.SizedText(row)
		if len(got) != 1 || got[0].Text != "Yo" {
			t.Fatalf("row %d after overwrite = %+v", row, got)
		}
	}
	g.SetCursorLocked(0, 0)
	g.ClearLineWithBg(DefaultBg()) // EL 2 on the top row erases the other block
	s = g.Snapshot(s)
	if len(s.SizedText(0)) != 0 || len(s.SizedText(1)) != 0 {
		t.Fatalf("EL kept the block: %+v %+v", s.SizedText(0), s.SizedText(1))
	}
}

func TestSizedTextErasedOnBlankRowFastPath(t *testing.T) {
	g := newGrid(20, 5, 100)
	putBlock(g, 0, 0, SizedText{Text: "A", Scale: 3, Cells: 3})
	g.SetCursorLocked(0, 2)
	g.ClearLineToEndWithBg(DefaultBg()) // the block's bottom row holds only blanks
	s := g.Snapshot(nil)
	for row := range 3 {
		if len(s.SizedText(row)) != 0 {
			t.Fatalf("row %d kept the block", row)
		}
	}
}

func TestSizedTextScrollsWithRows(t *testing.T) {
	g := newGrid(20, 3, 100)
	putBlock(g, 0, 1, SizedText{Text: "A", Scale: 2, Cells: 2})
	g.SetCursorLocked(0, 2)
	g.Newline()
	s := g.Snapshot(nil)
	if len(s.SizedText(0)) != 1 || s.SizedText(0)[0].SubRow != 0 || len(s.SizedText(1)) != 1 || len(s.SizedText(2)) != 0 {
		t.Fatalf("after scroll: %+v %+v %+v", s.SizedText(0), s.SizedText(1), s.SizedText(2))
	}
	g.Newline()
	g.ScrollViewUp(1)
	s = g.Snapshot(s)
	if len(s.SizedText(0)) != 1 || s.SizedText(0)[0].SubRow != 0 {
		t.Fatalf("scrollback row = %+v", s.SizedText(0))
	}
}

func TestSizedTextClearedWithRowsAndDECALN(t *testing.T) {
	g := newGrid(20, 3, 100)
	putBlock(g, 0, 0, SizedText{Text: "A", Scale: 2, Cells: 2})
	g.ClearAllWithBg(DefaultBg())
	s := g.Snapshot(nil)
	if len(s.SizedText(0)) != 0 || len(s.SizedText(1)) != 0 {
		t.Fatal("ED 2 kept the block on screen")
	}
	putBlock(g, 0, 0, SizedText{Text: "A", Scale: 2, Cells: 2})
	g.FillScreen('E')
	s = g.Snapshot(s)
	if len(s.SizedText(0)) != 0 {
		t.Fatal("DECALN kept the block")
	}
	putBlock(g, 0, 0, SizedText{Text: "A", Scale: 1, Cells: 1})
	g.SetCursorLocked(0, 0)
	g.DeleteChars(1)
	s = g.Snapshot(s)
	if len(s.SizedText(0)) != 0 {
		t.Fatal("DCH kept the block")
	}
}

func TestSizedTextSurvivesReflowWhenItFits(t *testing.T) {
	g := newGrid(20, 4, 100)
	writeStr(g, "ab")
	putBlock(g, 2, 0, SizedText{Text: "Z", Scale: 1, Cells: 2, Num: 1, Den: 2})
	g.Resize(10, 4)
	s := g.Snapshot(nil)
	if got := s.SizedText(0); len(got) != 1 || got[0].Col != 2 || got[0].Num != 1 {
		t.Fatalf("after reflow = %+v", got)
	}
	g.Resize(3, 4) // the block no longer fits on one row
	s = g.Snapshot(nil)
	for row := range s.Rows {
		if len(s.SizedText(row)) != 0 {
			t.Fatalf("split block kept on row %d", row)
		}
	}
}
