package parser

import (
	"strings"
	"testing"
)

func TestRenderFrameImagesAnchorAndPrune(t *testing.T) {
	term := NewTerminal(20, 5)
	term.SetCellPixelSize(10, 20)
	term.SetMaxScrollback(3)
	term.Process([]byte("\r\n"))
	term.Process(apc("a=T,f=32,s=20,v=20,i=7,q=2;" + b64(rgba(20, 20))))

	var f RenderFrame
	term.RenderFrame(&f)
	if f.Snap == nil || f.Grid == nil {
		t.Fatal("frame missing snapshot or grid")
	}
	if len(f.Placements) != 1 {
		t.Fatalf("placements = %d, want 1", len(f.Placements))
	}
	p := f.Placements[0]
	if got := p.AnchorAbsRow - f.ViewTopAbs; got != 1 {
		t.Fatalf("placement view row = %d, want 1", got)
	}
	if img := f.Images[p.ImageID]; img == nil || img.Width != 20 {
		t.Fatalf("frame image = %+v", img)
	}
	if f.ImageStore == nil || f.CellW != 10 || f.CellH != 20 {
		t.Fatalf("store=%v cell=%dx%d", f.ImageStore, f.CellW, f.CellH)
	}

	term.Process([]byte(strings.Repeat("x\r\n", 20)))
	term.RenderFrame(&f)
	if f.MinAbs <= p.AnchorAbsRow {
		t.Fatalf("MinAbs = %d, want past anchor %d", f.MinAbs, p.AnchorAbsRow)
	}
	if len(f.Placements) != 0 || len(term.Images().Placements()) != 0 {
		t.Fatalf("trimmed placement not pruned: %d", len(f.Placements))
	}
}

func TestRenderFrameModes(t *testing.T) {
	term := NewTerminal(10, 3)
	term.Process([]byte("\x1b[?5h\x1b]9;4;1;40\x07"))
	var f RenderFrame
	term.RenderFrame(&f)
	if !f.Reverse {
		t.Fatal("DECSCNM not reported")
	}
	if f.ProgressState != 1 || f.ProgressValue != 40 {
		t.Fatalf("progress = %d,%d", f.ProgressState, f.ProgressValue)
	}
	term.Process([]byte("\x1b[?1049h"))
	term.RenderFrame(&f)
	if !f.AltScreen || len(f.Images) != 0 {
		t.Fatalf("alt=%v images=%d", f.AltScreen, len(f.Images))
	}
}

func TestRenderFrameConcurrent(t *testing.T) {
	term := NewTerminal(20, 5)
	term.SetMaxScrollback(10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 50 {
			term.Process(apc("a=T,f=32,s=2,v=2,q=2;" + b64(rgba(2, 2))))
			term.Process([]byte("line\r\n"))
			if i%10 == 0 {
				term.Process(apc("a=d,d=A,q=2"))
			}
		}
	}()
	var f RenderFrame
	for {
		term.RenderFrame(&f)
		for _, p := range f.Placements {
			if img := f.Images[p.ImageID]; img != nil {
				_ = len(img.Pixels) + p.AnchorAbsRow - f.ViewTopAbs
			}
		}
		select {
		case <-done:
			return
		default:
		}
	}
}
