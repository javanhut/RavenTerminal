package main

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/parser"
)

func TestPanePixel(t *testing.T) {
	// Pane at logical (10,20) size 100x50, content scale 2: framebuffer px.
	if x, y := panePixel(15.5, 30, 10, 20, 100, 50, 2); x != 11 || y != 20 {
		t.Fatalf("got %d,%d", x, y)
	}
	// Clamped to the pane.
	if x, y := panePixel(0, 500, 10, 20, 100, 50, 2); x != 0 || y != 99 {
		t.Fatalf("clamp got %d,%d", x, y)
	}
}

func TestSGRPixelsReportUsesPointerPixels(t *testing.T) {
	term := parser.NewTerminal(80, 24)
	term.Process([]byte("\x1b[?1000h\x1b[?1016h"))
	ctx := withPixels(term.MouseCtx(), 15.5, 30, 10, 20, 100, 50, 2)
	act := parser.DecideMouse(ctx, parser.MousePress, 0, 1, 1, -1, false)
	if got := string(act.Bytes); got != "\x1b[<0;12;21M" {
		t.Fatalf("got %q", got)
	}
}

func TestWheelSteps(t *testing.T) {
	cases := []struct {
		x, y float64
		want []wheelStep
	}{
		{0, 2, []wheelStep{{parser.MouseWheelUp, 2}}},
		{0, -0.4, []wheelStep{{parser.MouseWheelDown, 1}}},
		{1, 0, []wheelStep{{parser.MouseWheelLeft, 1}}},
		{-3, 1, []wheelStep{{parser.MouseWheelUp, 1}, {parser.MouseWheelRight, 3}}},
		{0, 0, nil},
	}
	for _, c := range cases {
		got := wheelSteps(c.x, c.y)
		if len(got) != len(c.want) {
			t.Fatalf("%v,%v: got %v", c.x, c.y, got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%v,%v: got %v want %v", c.x, c.y, got, c.want)
			}
		}
	}
	term := parser.NewTerminal(80, 24)
	term.Process([]byte("\x1b[?1000h\x1b[?1006h"))
	act := parser.DecideMouse(term.MouseCtx(), parser.MouseWheelRight, 0, 0, 0, -1, false)
	if got := string(act.Bytes); got != "\x1b[<67;1;1M" {
		t.Fatalf("wheel right = %q", got)
	}
}
