package window

import "testing"

// Background goroutines wake the UI as results arrive, which can happen
// before the first window initialises GLFW.
func TestPostEmptyEventBeforeInit(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PostEmptyEvent before glfw.Init panicked: %v", r)
		}
	}()
	PostEmptyEvent()
}
