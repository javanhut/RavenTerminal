package render

import "testing"

// TestIndexedColorStandard16 pins the exact RGBA of every standard palette
// entry (guards the table hoist refactor).
func TestIndexedColorStandard16(t *testing.T) {
	want := [][4]float32{
		{0.043, 0.059, 0.078, 1.0},
		{0.820, 0.412, 0.412, 1.0},
		{0.498, 0.737, 0.549, 1.0},
		{0.843, 0.729, 0.490, 1.0},
		{0.533, 0.643, 0.831, 1.0},
		{0.773, 0.525, 0.753, 1.0},
		{0.498, 0.773, 0.784, 1.0},
		{0.831, 0.847, 0.871, 1.0},
		{0.294, 0.322, 0.388, 1.0},
		{0.878, 0.478, 0.478, 1.0},
		{0.604, 0.843, 0.659, 1.0},
		{0.906, 0.788, 0.545, 1.0},
		{0.647, 0.749, 0.941, 1.0},
		{0.847, 0.627, 0.831, 1.0},
		{0.604, 0.843, 0.863, 1.0},
		{0.945, 0.953, 0.961, 1.0},
	}
	for i, w := range want {
		if got := indexedColor(uint8(i)); got != w {
			t.Errorf("indexedColor(%d) = %v, want %v", i, got, w)
		}
	}
}

// TestIndexedColorCube checks the 6x6x6 color cube formula (indices 16-231).
func TestIndexedColorCube(t *testing.T) {
	for idx := 16; idx < 232; idx++ {
		i := idx - 16
		r := float32((i/36)%6) * 51 / 255
		g := float32((i/6)%6) * 51 / 255
		b := float32(i%6) * 51 / 255
		want := [4]float32{r, g, b, 1.0}
		if got := indexedColor(uint8(idx)); got != want {
			t.Fatalf("indexedColor(%d) = %v, want %v", idx, got, want)
		}
	}
	// Spot-check corners.
	if got := indexedColor(16); got != ([4]float32{0, 0, 0, 1}) {
		t.Errorf("indexedColor(16) = %v, want black", got)
	}
	if got := indexedColor(231); got != ([4]float32{1, 1, 1, 1}) {
		t.Errorf("indexedColor(231) = %v, want white", got)
	}
}

// TestIndexedColorGrayscale checks the grayscale ramp (indices 232-255).
func TestIndexedColorGrayscale(t *testing.T) {
	for idx := 232; idx < 256; idx++ {
		v := float32(idx-232) * 10 / 255
		want := [4]float32{v, v, v, 1.0}
		if got := indexedColor(uint8(idx)); got != want {
			t.Fatalf("indexedColor(%d) = %v, want %v", idx, got, want)
		}
	}
}
