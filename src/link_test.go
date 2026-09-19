package main

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

func writeLinkStr(g *grid.Grid, s string, link uint16) {
	for _, r := range s {
		g.WriteChar(r, grid.DefaultFg(), grid.DefaultBg(), 0, link, 0, grid.Color{})
	}
}

func TestLinkAtCellFollowsSoftWrap(t *testing.T) {
	// 10 cols: "see https:" / "//example." / "com/a ok"
	g := grid.NewGrid(10, 4)
	writeLinkStr(g, "see https://example.com/a ok", 0)

	want := "https://example.com/a"
	wantSpan := linkSpan{cellPos{4, 0}, cellPos{4, 2}, true}
	for _, at := range []cellPos{{4, 0}, {9, 0}, {0, 1}, {5, 1}, {4, 2}} {
		got, span := linkAtCell(g, at.col, at.row)
		if got != want || span != wantSpan {
			t.Errorf("linkAtCell(%d,%d) = %q %+v, want %q %+v", at.col, at.row, got, span, want, wantSpan)
		}
	}
	for _, at := range []cellPos{{0, 0}, {7, 2}} {
		if got, span := linkAtCell(g, at.col, at.row); got != "" || span.ok {
			t.Errorf("linkAtCell(%d,%d) = %q %+v, want no link", at.col, at.row, got, span)
		}
	}
}

func TestLinkAtCellStopsAtHardNewline(t *testing.T) {
	g := grid.NewGrid(20, 3)
	writeLinkStr(g, "https://a.com", 0)
	g.CarriageReturn()
	g.Newline()
	writeLinkStr(g, "/more", 0)

	if got, _ := linkAtCell(g, 0, 0); got != "https://a.com" {
		t.Errorf("row 0 link = %q, want %q", got, "https://a.com")
	}
	if got, _ := linkAtCell(g, 1, 1); got != "" {
		t.Errorf("row 1 link = %q, want none", got)
	}
}

func TestLinkAtCellOSC8AcrossWrap(t *testing.T) {
	g := grid.NewGrid(6, 3)
	id := g.InternLink("https://example.com/")
	writeLinkStr(g, "ab", 0)
	writeLinkStr(g, "click here", id) // wraps after "clic"
	writeLinkStr(g, " x", 0)

	got, span := linkAtCell(g, 1, 1)
	wantSpan := linkSpan{cellPos{2, 0}, cellPos{5, 1}, true}
	if got != "https://example.com/" || span != wantSpan {
		t.Errorf("linkAtCell = %q %+v, want %q %+v", got, span, "https://example.com/", wantSpan)
	}
}
