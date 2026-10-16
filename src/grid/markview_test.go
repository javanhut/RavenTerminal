package grid

import "testing"

func TestPromptMarksRange(t *testing.T) {
	g := NewGrid(10, 4)
	for i := range 6 {
		g.LockBatch()
		g.SetMarkLocked(MarkPrompt)
		if i%2 == 1 {
			g.SetExitCodeLocked(g.AbsoluteCursorRowLocked(), 2)
		}
		g.UnlockBatch()
		writeLine(g, "$ x")
		g.CarriageReturn()
		g.Newline()
	}
	first, last := g.AbsBounds()
	if first != 0 || last != 6 {
		t.Fatalf("bounds = %d..%d, want 0..6", first, last)
	}
	marks := g.PromptMarks(first, last, nil)
	if len(marks) != 6 {
		t.Fatalf("marks = %+v", marks)
	}
	if m := marks[1]; m.AbsRow != 1 || !m.HasExit || m.Exit != 2 {
		t.Fatalf("mark 1 = %+v", m)
	}
	if m := marks[0]; m.HasExit {
		t.Fatalf("mark 0 has exit: %+v", m)
	}
	if got := g.PromptMarks(2, 3, marks[:0]); len(got) != 2 || got[0].AbsRow != 2 {
		t.Fatalf("sub-range = %+v", got)
	}
}
