package main

import (
	"math"

	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/tab"
)

// panePixel converts a logical cursor position to a 0-based framebuffer
// pixel offset inside a pane rect (logical coordinates), clamped to it.
func panePixel(x, y float64, rectX, rectY, rectW, rectH, scale float32) (int, int) {
	px := int((float32(x) - rectX) * scale)
	py := int((float32(y) - rectY) * scale)
	return clampInt(px, 0, int(rectW*scale)-1), clampInt(py, 0, int(rectH*scale)-1)
}

// withPixels fills the pointer's pixel position for SGR-pixels (?1016).
func withPixels(ctx parser.MouseContext, x, y float64, rectX, rectY, rectW, rectH, scale float32) parser.MouseContext {
	ctx.Pixels = true
	ctx.PixelX, ctx.PixelY = panePixel(x, y, rectX, rectY, rectW, rectH, scale)
	return ctx
}

// mouseCtxAt is mouseCtxFor plus the pointer's pixel position in the pane.
func (a *App) mouseCtxAt(t *tab.Tab, pane *tab.Pane, shift bool, x, y float64) parser.MouseContext {
	ctx := mouseCtxFor(pane, shift)
	width, height := a.win.GetFramebufferSize()
	if rx, ry, rw, rh, ok := a.renderer.PaneRectFor(t, pane, width, height); ok {
		ctx = withPixels(ctx, x, y, rx, ry, rw, rh, a.renderer.ContentScale())
	}
	return ctx
}

type wheelStep struct {
	kind  parser.MouseEventKind
	count int
}

// wheelSteps turns GLFW scroll offsets into wheel button events: positive y
// is up, positive x is left (X11 buttons 6/7).
func wheelSteps(xoff, yoff float64) []wheelStep {
	var out []wheelStep
	add := func(v float64, pos, neg parser.MouseEventKind) {
		if v == 0 {
			return
		}
		k := pos
		if v < 0 {
			k = neg
		}
		out = append(out, wheelStep{k, max(int(math.Abs(v)), 1)})
	}
	add(yoff, parser.MouseWheelUp, parser.MouseWheelDown)
	add(xoff, parser.MouseWheelLeft, parser.MouseWheelRight)
	return out
}
