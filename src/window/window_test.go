package window

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func TestHintsTransparent(t *testing.T) {
	find := func(hs []intHint, h glfw.Hint) (int, bool) {
		for _, x := range hs {
			if x.hint == h {
				return x.value, true
			}
		}
		return 0, false
	}
	cfg := DefaultConfig()
	if v, ok := find(intHints(cfg), glfw.TransparentFramebuffer); !ok || v != glfw.False {
		t.Fatalf("opaque config: TransparentFramebuffer = %d (set %v), want False", v, ok)
	}
	cfg.Transparent = true
	if v, ok := find(intHints(cfg), glfw.TransparentFramebuffer); !ok || v != glfw.True {
		t.Fatalf("transparent config: TransparentFramebuffer = %d (set %v), want True", v, ok)
	}
}

func TestPickMonitor(t *testing.T) {
	left := rect{0, 0, 1920, 1080}
	right := rect{1920, 0, 2560, 1440}
	mons := []rect{left, right}
	cases := []struct {
		name string
		win  rect
		want int
	}{
		{"inside left", rect{100, 100, 800, 600}, 0},
		{"inside right", rect{2000, 100, 800, 600}, 1},
		{"center decides when spanning", rect{1500, 100, 900, 600}, 1},
		{"mostly left", rect{1300, 100, 900, 600}, 0},
		{"off-screen overlap wins", rect{-500, 1000, 800, 600}, 0},
		{"nowhere", rect{10000, 10000, 100, 100}, -1},
	}
	for _, c := range cases {
		if got := pickMonitor(c.win, mons); got != c.want {
			t.Errorf("%s: pickMonitor = %d, want %d", c.name, got, c.want)
		}
	}
	if got := pickMonitor(rect{0, 0, 10, 10}, nil); got != -1 {
		t.Errorf("no monitors: got %d", got)
	}
}
