package grid

import "testing"

func writeLine(g *Grid, s string) {
	for _, r := range s {
		g.WriteChar(r, DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	}
}

func TestMarksSurviveReflow(t *testing.T) {
	g := NewGrid(10, 6)
	g.LockBatch()
	g.SetMarkLocked(MarkPrompt)
	g.UnlockBatch()
	writeLine(g, "$ echo abcdefghij")
	g.CarriageReturn()
	g.Newline()
	g.LockBatch()
	g.SetMarkLocked(MarkOutput)
	g.UnlockBatch()
	writeLine(g, "out")
	g.CarriageReturn()
	g.Newline()
	g.LockBatch()
	g.SetMarkLocked(MarkEnd)
	g.SetMarkLocked(MarkPrompt)
	g.UnlockBatch()
	writeLine(g, "$ ")
	// prompt row 0 wraps onto row 1; output on row 2; next prompt row 3
	if p := g.NextPrompt(0); p != 3 {
		t.Fatalf("NextPrompt before reflow = %d", p)
	}
	g.Resize(20, 6)
	if p := g.NextPrompt(0); p != 2 {
		t.Fatalf("NextPrompt after reflow = %d, want 2", p)
	}
	if p := g.PrevPrompt(2); p != 0 {
		t.Fatalf("PrevPrompt after reflow = %d, want 0", p)
	}
	if s, e, ok := g.LastOutputRange(); !ok || s != 1 || e != 1 {
		t.Fatalf("LastOutputRange = %d,%d,%v", s, e, ok)
	}
}

func TestMarksClearedOnRowReuse(t *testing.T) {
	g := NewGrid(10, 3)
	g.LockBatch()
	g.SetMarkLocked(MarkPrompt)
	g.ClearAllLocked()
	g.UnlockBatch()
	if p := g.NextPrompt(-1); p != -1 {
		t.Fatalf("mark survived screen clear: %d", p)
	}
}

func TestOutputWithoutTrailingNewline(t *testing.T) {
	g := NewGrid(10, 4)
	g.LockBatch()
	g.SetMarkLocked(MarkOutput)
	g.WriteCharLocked('x', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	g.SetMarkLocked(MarkEnd)
	g.UnlockBatch()
	if s, e, ok := g.LastOutputRange(); !ok || s != 0 || e != 0 {
		t.Fatalf("LastOutputRange = %d,%d,%v want 0,0,true", s, e, ok)
	}
}
