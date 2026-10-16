package render

import (
	"testing"
	"time"

	"github.com/javanhut/RavenTerminal/src/parser"
)

func TestImageAnimationSchedulesFrames(t *testing.T) {
	r := newBenchRenderer()
	r.beginEffectsFrame()
	r.prepareImages(&parser.RenderFrame{}, 0, 0, 100, 100)
	if _, ok := r.NextAnimationFrame(time.Now()); ok {
		t.Fatal("no animation, yet a frame was scheduled")
	}
	due := time.Now().Add(30 * time.Millisecond)
	r.prepareImages(&parser.RenderFrame{NextAnimFrame: due}, 0, 0, 100, 100)
	wait, ok := r.NextAnimationFrame(time.Now())
	if !ok || wait > 30*time.Millisecond {
		t.Fatalf("next frame in %v (%v)", wait, ok)
	}
}
