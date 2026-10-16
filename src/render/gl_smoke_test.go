//go:build glsmoke

package render

import (
	"image"
	"image/png"
	"os"
	"runtime"
	"testing"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/parser"
)

// TestGLSmokeText renders shaped text, bold/italic, a cluster and color emoji
// through the real shaders into an offscreen framebuffer:
//
//	go test -tags glsmoke -run GLSmoke ./src/render/
func TestGLSmokeText(t *testing.T) {
	runtime.LockOSThread()
	if err := glfw.Init(); err != nil {
		t.Skipf("no display: %v", err)
	}
	defer glfw.Terminate()
	glfw.WindowHint(glfw.Visible, glfw.False)
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	win, err := glfw.CreateWindow(64, 64, "smoke", nil, nil)
	if err != nil {
		t.Skipf("no GL context: %v", err)
	}
	defer win.Destroy()
	win.MakeContextCurrent()
	if err := gl.Init(); err != nil {
		t.Fatal(err)
	}
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer r.Destroy()

	const W, H = 640, 120
	var fbo, tex uint32
	gl.GenFramebuffers(1, &fbo)
	gl.BindFramebuffer(gl.FRAMEBUFFER, fbo)
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, W, H, 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0)
	gl.Viewport(0, 0, W, H)
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)

	line := []rune("a -> b != c  bold italic ")
	cells := make([]grid.Cell, 0, 64)
	for i, c := range line {
		cell := grid.NewCell()
		cell.Char = c
		if i >= 13 && i < 17 {
			cell.Flags = grid.FlagBold
		}
		if i >= 18 {
			cell.Flags = grid.FlagItalic
		}
		cells = append(cells, cell)
	}
	q := grid.NewCell()
	q.Char, q.Combining = 'q', []rune{0x301}
	cells = append(cells, q)
	e := grid.NewCell()
	e.Char, e.Combining, e.Width = 0x1F44D, []rune{0x1F3FD}, grid.CellWidthWide
	cont := grid.NewCell()
	cont.Char, cont.Width = 0, grid.CellWidthContinuation
	cells = append(cells, e, cont)
	snap := &grid.Snapshot{Cols: len(cells), Rows: 1, Cells: cells}

	// Tiny pages force page growth (texture re-allocation) and eviction.
	r.initAtlases(64, 3, 64, 2)
	proj := orthoMatrix(0, W, H, 0, -1, 1)
	for frame := range 3 {
		gl.ClearColor(0, 0, 0, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT)
		r.renderGridAt(snap, nil, 0, 0, W, H, proj, false, parser.CursorStyleBlock)
		r.uiFlush()
		if e := gl.GetError(); e != gl.NO_ERROR {
			t.Fatalf("frame %d: GL error 0x%x", frame, e)
		}
	}
	pix := make([]byte, W*H*4)
	gl.ReadPixels(0, 0, W, H, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(pix))
	ink, colored := 0, 0
	for i := 0; i < len(pix); i += 4 {
		rr, gg, bb := int(pix[i]), int(pix[i+1]), int(pix[i+2])
		if rr+gg+bb > 150 {
			ink++
		}
		if max(rr, gg, bb)-min(rr, gg, bb) > 60 {
			colored++
		}
	}
	if ink < 200 {
		t.Errorf("only %d lit pixels: text did not render", ink)
	}
	if r.colorFace != nil && colored < 30 {
		t.Errorf("only %d colored pixels: emoji did not render", colored)
	}
	if len(r.atlas.pages) < 2 || r.atlas.texLayers != len(r.atlas.pages) {
		t.Errorf("atlas pages %d, texture layers %d", len(r.atlas.pages), r.atlas.texLayers)
	}
	if out := os.Getenv("SMOKE_PNG"); out != "" {
		img := image.NewRGBA(image.Rect(0, 0, W, H))
		for y := range H {
			copy(img.Pix[y*W*4:(y+1)*W*4], pix[(H-1-y)*W*4:(H-y)*W*4])
		}
		if f, err := os.Create(out); err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}
	t.Logf("lit=%d colored=%d monoPages=%d", ink, colored, len(r.atlas.pages))
}
