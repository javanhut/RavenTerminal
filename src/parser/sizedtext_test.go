package parser

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

func sizedAt(term *Terminal, row int) []grid.SizedText {
	return term.Snapshot().SizedText(row)
}

func TestOSC66RecordsScaledBlock(t *testing.T) {
	term := NewTerminal(20, 5)
	feed(term, "a")
	term.Process(osc("66;s=2;Hi"))
	if col, row := term.Grid.GetCursor(); col != 5 || row != 0 {
		t.Fatalf("cursor = (%d,%d), want (5,0)", col, row)
	}
	top, bot := sizedAt(term, 0), sizedAt(term, 1)
	want := grid.SizedText{Col: 1, Cells: 4, Scale: 2, Text: "Hi"}
	if len(top) != 1 || top[0] != want {
		t.Fatalf("top = %+v", top)
	}
	want.SubRow = 1
	if len(bot) != 1 || bot[0] != want {
		t.Fatalf("bottom = %+v", bot)
	}
}

func TestOSC66FractionalAndAlignment(t *testing.T) {
	term := NewTerminal(20, 5)
	term.Process(osc("66;n=1:d=2:w=1;ab"))
	got := sizedAt(term, 0)
	if len(got) != 1 || got[0].Cells != 1 || got[0].Num != 1 || got[0].Den != 2 || got[0].Text != "ab" {
		t.Fatalf("fractional = %+v", got)
	}
	if col, _ := term.Grid.GetCursor(); col != 1 {
		t.Fatalf("cursor col = %d", col)
	}
	term.Process(osc("66;s=3:n=2:d=3:v=2:h=1:w=2;X"))
	got = sizedAt(term, 0)
	if len(got) != 2 || got[1].Scale != 3 || got[1].Cells != 6 || got[1].VAlign != 2 || got[1].HAlign != 1 || got[1].Num != 2 || got[1].Den != 3 {
		t.Fatalf("aligned = %+v", got)
	}
	term.Process(osc("66;n=3:d=2;Y")) // n >= d: no fractional scale
	got = sizedAt(term, 0)
	if g := got[len(got)-1]; g.Num != 0 || g.Den != 0 || g.Text != "Y" {
		t.Fatalf("invalid fraction kept: %+v", g)
	}
	term.Process(osc("66;v=7:h=9;Z")) // out-of-range alignment clamps to 0
	got = sizedAt(term, 0)
	if g := got[len(got)-1]; g.VAlign != 0 || g.HAlign != 0 {
		t.Fatalf("bad alignment kept: %+v", g)
	}
}

func TestOSC66WrapsWholeBlockAndScrolls(t *testing.T) {
	term := NewTerminal(10, 3)
	feed(term, "\x1b[3;9H")
	term.Process(osc("66;s=2;Hi"))
	// The block does not fit after col 8: it moves to a new line, which at
	// the bottom scrolls twice to make room for both rows.
	if col, row := term.Grid.GetCursor(); col != 4 || row != 1 {
		t.Fatalf("cursor = (%d,%d), want (4,1)", col, row)
	}
	if top := sizedAt(term, 1); len(top) != 1 || top[0].Col != 0 || top[0].SubRow != 0 {
		t.Fatalf("top row = %+v", top)
	}
	if bot := sizedAt(term, 2); len(bot) != 1 || bot[0].SubRow != 1 {
		t.Fatalf("bottom row = %+v", bot)
	}
	term2 := NewTerminal(10, 3)
	feed(term2, "abcdefghij") // wrap pending at the last column
	term2.Process(osc("66;;Q"))
	if top := sizedAt(term2, 1); len(top) != 1 || top[0].Col != 0 {
		t.Fatalf("pending wrap: block on row 1 = %+v, row 0 = %+v", top, sizedAt(term2, 0))
	}
}

func TestOSC66OverwriteDropsBlock(t *testing.T) {
	term := NewTerminal(20, 5)
	term.Process(osc("66;s=2;Hi"))
	feed(term, "\x1b[2;2Hx")
	if len(sizedAt(term, 0)) != 0 || len(sizedAt(term, 1)) != 0 {
		t.Fatal("overwriting a block cell kept the block")
	}
}
