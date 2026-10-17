package images

import (
	"errors"
	"testing"
	"time"
)

// solid returns w*h RGBA pixels of one colour.
func solid(w, h int, c [4]byte) []byte {
	p := make([]byte, 4*w*h)
	for i := 0; i < len(p); i += 4 {
		copy(p[i:], c[:])
	}
	return p
}

func px(img *Image, x, y int) [4]byte {
	i := 4 * (y*img.Width + x)
	return [4]byte(img.Pixels[i : i+4])
}

func errCode(err error) string {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	if err != nil {
		return "?"
	}
	return ""
}

var (
	red   = [4]byte{255, 0, 0, 255}
	green = [4]byte{0, 255, 0, 255}
	blue  = [4]byte{0, 0, 255, 255}
	clear = [4]byte{}
)

func TestLoadFrameComposesOnBaseAndBackground(t *testing.T) {
	s := NewStore(0)
	img := s.Add(1, 2, 2, solid(2, 2, red))
	n, err := s.LoadFrame(1, FrameData{Pixels: solid(1, 1, green), W: 1, H: 1, X: 1, Y: 1, Base: 1})
	if err != nil || n != 2 {
		t.Fatalf("frame = %d, %v", n, err)
	}
	if s.FrameCount(1) != 2 {
		t.Fatalf("frames = %d", s.FrameCount(1))
	}
	f2 := s.FrameImage(1, 2)
	if px(f2, 0, 0) != red || px(f2, 1, 1) != green {
		t.Fatalf("frame 2 = %v", f2.Pixels)
	}
	// No base: the canvas is the Y background colour (default transparent).
	s.LoadFrame(1, FrameData{Pixels: solid(1, 1, green), W: 1, H: 1, Bg: 0x0000ffff})
	f3 := s.FrameImage(1, 3)
	if px(f3, 0, 0) != green || px(f3, 1, 1) != blue {
		t.Fatalf("frame 3 = %v", f3.Pixels)
	}
	if px(img, 0, 0) != red || px(s.FrameImage(1, 1), 1, 1) != red {
		t.Fatal("root frame changed")
	}
}

func TestLoadFrameBlendAndOverwrite(t *testing.T) {
	s := NewStore(0)
	s.Add(1, 1, 1, solid(1, 1, red))
	half := [4]byte{0, 0, 255, 128}
	s.LoadFrame(1, FrameData{Pixels: solid(1, 1, half), W: 1, H: 1, Base: 1})
	if got := px(s.FrameImage(1, 2), 0, 0); got[0] < 120 || got[0] > 135 || got[2] < 120 || got[2] > 135 || got[3] != 255 {
		t.Fatalf("alpha blend = %v", got)
	}
	s.LoadFrame(1, FrameData{Pixels: solid(1, 1, half), W: 1, H: 1, Base: 1, Overwrite: true})
	if got := px(s.FrameImage(1, 3), 0, 0); got != half {
		t.Fatalf("overwrite = %v", got)
	}
}

func TestLoadFrameEditsExistingFrame(t *testing.T) {
	s := NewStore(0)
	img := s.Add(1, 2, 1, solid(2, 1, red))
	n, err := s.LoadFrame(1, FrameData{Pixels: solid(1, 1, green), W: 1, H: 1, X: 1, Edit: 1})
	if err != nil || n != 1 {
		t.Fatalf("edit = %d, %v", n, err)
	}
	if s.FrameCount(1) != 1 {
		t.Fatal("editing frame 1 created a frame")
	}
	root := s.Display(1)
	if px(root, 0, 0) != red || px(root, 1, 0) != green {
		t.Fatalf("edited root = %v", root.Pixels)
	}
	if px(img, 1, 0) != red {
		t.Fatal("the handed-out image was mutated")
	}
}

func TestLoadFrameErrors(t *testing.T) {
	s := NewStore(0)
	if _, err := s.LoadFrame(5, FrameData{Pixels: solid(1, 1, red), W: 1, H: 1}); errCode(err) != "ENOENT" {
		t.Fatalf("missing image: %v", err)
	}
	s.Add(1, 2, 2, solid(2, 2, red))
	if _, err := s.LoadFrame(1, FrameData{Pixels: solid(2, 2, red), W: 2, H: 2, X: 1}); errCode(err) != "EINVAL" {
		t.Fatalf("out of bounds: %v", err)
	}
	if _, err := s.LoadFrame(1, FrameData{Pixels: solid(1, 1, red), W: 1, H: 1, Base: 4}); errCode(err) != "ENOENT" {
		t.Fatalf("missing base frame: %v", err)
	}
	if _, err := s.LoadFrame(1, FrameData{Pixels: solid(1, 1, red), W: 1, H: 1, Edit: 3}); errCode(err) != "ENOENT" {
		t.Fatalf("missing edited frame: %v", err)
	}
}

func TestComposeFrames(t *testing.T) {
	s := NewStore(0)
	s.Add(1, 2, 2, solid(2, 2, red))
	s.LoadFrame(1, FrameData{Pixels: solid(2, 2, green), W: 2, H: 2, Overwrite: true})
	// Copy a 1x1 rect from frame 2 (green) at (0,0) onto frame 1 at (1,1).
	if err := s.ComposeFrames(1, Compose{Src: 2, Dst: 1, W: 1, H: 1, DstX: 1, DstY: 1, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	f1 := s.FrameImage(1, 1)
	if px(f1, 0, 0) != red || px(f1, 1, 1) != green {
		t.Fatalf("composed = %v", f1.Pixels)
	}
	if err := s.ComposeFrames(1, Compose{Src: 3, Dst: 1}); errCode(err) != "ENOENT" {
		t.Fatalf("missing source: %v", err)
	}
	if err := s.ComposeFrames(1, Compose{Src: 2, Dst: 1, W: 2, H: 2, DstX: 1}); errCode(err) != "EINVAL" {
		t.Fatalf("out of bounds: %v", err)
	}
	if err := s.ComposeFrames(1, Compose{Src: 1, Dst: 1, W: 2, H: 1, DstX: 0, DstY: 0}); errCode(err) != "EINVAL" {
		t.Fatalf("overlapping self-compose: %v", err)
	}
}

func TestAnimationPlayback(t *testing.T) {
	s := NewStore(0)
	s.Add(1, 1, 1, solid(1, 1, red))
	s.LoadFrame(1, FrameData{Pixels: solid(1, 1, green), W: 1, H: 1, Gap: 100, Overwrite: true})
	s.LoadFrame(1, FrameData{Pixels: solid(1, 1, blue), W: 1, H: 1, Gap: 50, Overwrite: true})
	s.Place(&Placement{ImageID: 1, Cols: 1, Rows: 1})
	t0 := time.Unix(1000, 0)
	if next := s.Tick(t0); !next.IsZero() {
		t.Fatal("a stopped animation scheduled a frame")
	}
	s.ControlAnimation(1, AnimControl{State: AnimRunning, Gap: 200, GapFrame: 1}, t0)
	if s.CurrentFrame(1) != 1 {
		t.Fatalf("start frame = %d", s.CurrentFrame(1))
	}
	if next := s.Tick(t0); !next.Equal(t0.Add(200 * time.Millisecond)) {
		t.Fatalf("first deadline = %v", next.Sub(t0))
	}
	steps := []struct {
		at    time.Duration
		frame int
		color [4]byte
	}{{199, 1, red}, {200, 2, green}, {300, 3, blue}, {349, 3, blue}, {350, 1, red}, {600, 2, green}}
	for _, st := range steps {
		s.Tick(t0.Add(st.at * time.Millisecond))
		if f := s.CurrentFrame(1); f != st.frame {
			t.Fatalf("at %dms frame = %d, want %d", st.at, f, st.frame)
		}
		if got := px(s.Display(1), 0, 0); got != st.color {
			t.Fatalf("at %dms display = %v", st.at, got)
		}
	}
	s.ControlAnimation(1, AnimControl{State: AnimStopped}, t0)
	s.Tick(t0.Add(10 * time.Second))
	if s.CurrentFrame(1) != 2 {
		t.Fatal("stopped animation advanced")
	}
	s.ControlAnimation(1, AnimControl{Current: 3}, t0)
	if s.CurrentFrame(1) != 3 || px(s.Display(1), 0, 0) != blue {
		t.Fatal("c= did not select the frame")
	}
}

func TestAnimationLoopCountAndLoading(t *testing.T) {
	s := NewStore(0)
	s.Add(1, 1, 1, solid(1, 1, red))
	s.LoadFrame(1, FrameData{Pixels: solid(1, 1, green), W: 1, H: 1, Gap: 10})
	s.Place(&Placement{ImageID: 1, Cols: 1, Rows: 1})
	t0 := time.Unix(1000, 0)
	// v=2: one loop back to the start, then stop on the last frame.
	s.ControlAnimation(1, AnimControl{State: AnimRunning, Loops: 2, Gap: 10, GapFrame: 1}, t0)
	for ms := 0; ms <= 1000; ms += 5 {
		s.Tick(t0.Add(time.Duration(ms) * time.Millisecond))
	}
	if s.CurrentFrame(1) != 2 || !s.Tick(t0.Add(2*time.Second)).IsZero() {
		t.Fatalf("finite loop did not stop on the last frame: %d", s.CurrentFrame(1))
	}
	// Loading mode waits on the last frame for more data instead of looping.
	s.ControlAnimation(1, AnimControl{State: AnimLoading, Current: 1}, t0)
	s.Tick(t0.Add(100 * time.Millisecond))
	if s.CurrentFrame(1) != 2 {
		t.Fatalf("loading frame = %d", s.CurrentFrame(1))
	}
	s.LoadFrame(1, FrameData{Pixels: solid(1, 1, blue), W: 1, H: 1, Gap: 10})
	s.Tick(t0.Add(200 * time.Millisecond))
	if s.CurrentFrame(1) != 3 {
		t.Fatalf("loading did not continue onto the new frame: %d", s.CurrentFrame(1))
	}
}

func TestAnimationGaplessFramesAndUnplacedImages(t *testing.T) {
	s := NewStore(0)
	s.Add(1, 1, 1, solid(1, 1, red))
	s.LoadFrame(1, FrameData{Pixels: solid(1, 1, green), W: 1, H: 1, Gap: -1})
	s.LoadFrame(1, FrameData{Pixels: solid(1, 1, blue), W: 1, H: 1})
	t0 := time.Unix(1000, 0)
	s.ControlAnimation(1, AnimControl{State: AnimRunning}, t0)
	if !s.Tick(t0).IsZero() {
		t.Fatal("an image with no placement scheduled frames")
	}
	s.Place(&Placement{ImageID: 1, Cols: 1, Rows: 1})
	s.Tick(t0.Add(40 * time.Millisecond)) // the root and frame 2 are gapless, so frame 3 shows
	if s.CurrentFrame(1) != 3 {
		t.Fatalf("gapless frame was shown: %d", s.CurrentFrame(1))
	}
	if errCode(s.ControlAnimation(9, AnimControl{State: AnimRunning}, t0)) != "ENOENT" {
		t.Fatal("control of a missing image did not fail")
	}
	if errCode(s.ControlAnimation(1, AnimControl{Current: 7}, t0)) != "ENOENT" {
		t.Fatal("selecting a missing frame did not fail")
	}
}

func TestFramesCountAgainstBudgetAndDieWithImage(t *testing.T) {
	s := NewStore(0)
	s.Add(1, 2, 2, solid(2, 2, red))
	s.LoadFrame(1, FrameData{Pixels: solid(2, 2, green), W: 2, H: 2})
	if s.totalBytes != 32 {
		t.Fatalf("bytes = %d, want 32", s.totalBytes)
	}
	s.DeleteImage(1)
	if s.totalBytes != 0 {
		t.Fatalf("bytes after delete = %d", s.totalBytes)
	}
}

// The kitty spec: "The root frame defaults to zero gap", so a running
// animation skips it until the client gives it one with a=a r=1 z=....
func TestAnimationRootFrameDefaultsGapless(t *testing.T) {
	s := NewStore(0)
	s.Add(1, 1, 1, solid(1, 1, red))
	s.LoadFrame(1, FrameData{Pixels: solid(1, 1, green), W: 1, H: 1, Gap: 50})
	s.Place(&Placement{ImageID: 1, Cols: 1, Rows: 1})
	t0 := time.Unix(1000, 0)
	s.ControlAnimation(1, AnimControl{State: AnimRunning}, t0)
	s.Tick(t0)
	if s.CurrentFrame(1) != 2 {
		t.Fatalf("root frame was shown with no gap set: frame %d", s.CurrentFrame(1))
	}

	s.ControlAnimation(1, AnimControl{GapFrame: 1, Gap: 30, Current: 1}, t0)
	s.Tick(t0.Add(10 * time.Millisecond))
	if s.CurrentFrame(1) != 1 {
		t.Fatalf("root with a 30ms gap left after 10ms: frame %d", s.CurrentFrame(1))
	}
}
