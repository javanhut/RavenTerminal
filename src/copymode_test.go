package main

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/javanhut/RavenTerminal/src/grid"
)

func copyGrid() *grid.Grid {
	return termWith(20, 4, "alpha beta gamma\r\nline two\r\nthird line here\r\nfour\r\nfive\r\n$ ").GetGrid()
}

func feedRunes(c *copyMode, s string) copyResult {
	var r copyResult
	for _, ch := range s {
		r = c.rune(ch)
	}
	return r
}

func TestCopyModeMotionsAndYank(t *testing.T) {
	g := copyGrid()
	first, last := g.AbsBounds()
	c := newCopyMode(g)
	if c.abs != g.AbsoluteCursorRow() || c.col != 2 {
		t.Fatalf("copy mode starts at %d,%d, want the terminal cursor", c.abs, c.col)
	}
	feedRunes(c, "gg")
	if c.abs != first || c.col != 0 || g.ViewTopAbs() != first {
		t.Fatalf("gg -> %d,%d view %d, want top", c.abs, c.col, g.ViewTopAbs())
	}
	feedRunes(c, "w")
	if c.col != 6 {
		t.Fatalf("w -> col %d, want 6", c.col)
	}
	feedRunes(c, "e")
	if c.col != 9 {
		t.Fatalf("e -> col %d, want 9", c.col)
	}
	feedRunes(c, "b")
	if c.col != 6 {
		t.Fatalf("b -> col %d, want 6", c.col)
	}
	feedRunes(c, "$")
	if c.col != 15 {
		t.Fatalf("$ -> col %d, want 15", c.col)
	}
	feedRunes(c, "0v")
	feedRunes(c, "e")
	if got := g.SelectedText(); got != "alpha" {
		t.Fatalf("visual selection = %q", got)
	}
	r := feedRunes(c, "y")
	if !r.exit || r.yank != "alpha" {
		t.Fatalf("y = %+v", r)
	}
	if g.HasSelection() {
		t.Fatal("yank must leave copy mode with the selection cleared")
	}

	c = newCopyMode(g)
	feedRunes(c, "ggjVj")
	if r := feedRunes(c, "y"); r.yank != "line two\nthird line here" {
		t.Fatalf("V yank = %q", r.yank)
	}

	c = newCopyMode(g)
	feedRunes(c, "gg")
	c.key(glfw.KeyV, glfw.ModControl)
	feedRunes(c, "jjll")
	if r := feedRunes(c, "y"); r.yank != "alp\nlin\nthi" {
		t.Fatalf("block yank = %q", r.yank)
	}

	c = newCopyMode(g)
	feedRunes(c, "G")
	if c.abs != last {
		t.Fatalf("G -> %d, want %d", c.abs, last)
	}
	c.key(glfw.KeyU, glfw.ModControl)
	if c.abs != last-2 {
		t.Fatalf("Ctrl+u -> %d, want %d", c.abs, last-2)
	}
	c.key(glfw.KeyD, glfw.ModControl)
	if c.abs != last {
		t.Fatalf("Ctrl+d -> %d, want %d", c.abs, last)
	}
	feedRunes(c, "kkh")
	c.key(glfw.KeyUp, 0)
	if c.abs != last-3 {
		t.Fatalf("arrows -> %d", c.abs)
	}
}

func TestCopyModeSearchAndEscape(t *testing.T) {
	g := copyGrid()
	first, _ := g.AbsBounds()
	c := newCopyMode(g)
	feedRunes(c, "gg/line")
	if !c.searching || c.query != "line" {
		t.Fatalf("search prompt state %v %q", c.searching, c.query)
	}
	c.key(glfw.KeyEnter, 0)
	if c.searching || c.abs != first+1 || c.col != 0 {
		t.Fatalf("/line -> %d,%d", c.abs-first, c.col)
	}
	feedRunes(c, "n")
	if c.abs != first+2 || c.col != 6 {
		t.Fatalf("n -> %d,%d", c.abs-first, c.col)
	}
	feedRunes(c, "N")
	if c.abs != first+1 {
		t.Fatalf("N -> %d", c.abs-first)
	}
	feedRunes(c, "?alpha")
	c.key(glfw.KeyEnter, 0)
	if c.abs != first || c.col != 0 {
		t.Fatalf("?alpha -> %d,%d", c.abs-first, c.col)
	}
	feedRunes(c, "v")
	if r := c.key(glfw.KeyEscape, 0); r.exit || c.visual != visualNone || g.HasSelection() {
		t.Fatal("Esc in visual must only leave visual mode")
	}
	if r := c.key(glfw.KeyEscape, 0); !r.exit {
		t.Fatal("Esc must leave copy mode")
	}
	if r := feedRunes(newCopyMode(g), "q"); !r.exit {
		t.Fatal("q must leave copy mode")
	}
}
