package sixel

import (
	"strings"
	"testing"
)

func TestRasterAttributesDoNotAmplify(t *testing.T) {
	img, err := Decode(nil, []byte("\"1;1;8192;8192#0~"))
	if err != nil {
		t.Fatal(err)
	}
	if img.Width*img.Height > 1<<20 {
		t.Fatalf("raster-only canvas %dx%d allocated", img.Width, img.Height)
	}
	if img.Width < 1 || img.Height < 6 {
		t.Fatalf("drawn extent lost: %dx%d", img.Width, img.Height)
	}
}

func TestDrawnAreaCapped(t *testing.T) {
	data := "#1;2;100;0;0#1!8192~" + strings.Repeat("-!8192~", 1400)
	img, _ := Decode([]int{0, 1}, []byte(data))
	if img != nil && img.Width*img.Height > MaxPixels {
		t.Fatalf("decoded %dx%d past MaxPixels", img.Width, img.Height)
	}
}

func TestBlankAdvanceCannotInflateArea(t *testing.T) {
	data := "#1~" + strings.Repeat("-~", 3000) + "!8192?"
	img, err := Decode([]int{0, 1}, []byte(data))
	if err == nil && img.Width*img.Height > MaxPixels {
		t.Fatalf("decoded %dx%d past MaxPixels", img.Width, img.Height)
	}
}
