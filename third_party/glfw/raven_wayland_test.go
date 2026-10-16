//go:build linux && wayland

package glfw

import "testing"

func TestPostEmptyEventDoesNotLeakCallbacks(t *testing.T) {
	needDisplay(t)
	var before, after uint32
	onMain(func() {
		PollEvents()
		before = waylandNextProxyID()
		for i := 0; i < 500; i++ {
			PostEmptyEvent()
		}
		PollEvents()
		after = waylandNextProxyID()
	})
	t.Logf("proxy ids before %d after %d", before, after)
	if after > before+20 {
		t.Fatalf("PostEmptyEvent leaked %d protocol objects", after-before)
	}
}

func TestCursorShapeMapping(t *testing.T) {
	cases := map[StandardCursor]int{
		ArrowCursor:      1,  // default
		IBeamCursor:      9,  // text
		CrosshairCursor:  8,  // crosshair
		HandCursor:       4,  // pointer
		HResizeCursor:    26, // ew-resize
		VResizeCursor:    27, // ns-resize
		ResizeNWSECursor: 29, // nwse-resize
		ResizeNESWCursor: 28, // nesw-resize
		ResizeAllCursor:  32, // all-scroll
		NotAllowedCursor: 15, // not-allowed
	}
	for shape, want := range cases {
		if got := cursorShapeFor(shape); got != want {
			t.Errorf("cursorShapeFor(%#x) = %d, want %d", shape, got, want)
		}
	}
}
