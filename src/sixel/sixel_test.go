package sixel

import "testing"

func TestDecodeSolidColumn(t *testing.T) {
	// Define color register 1 as pure red, select it, write one sixel column with
	// all 6 bits set (0x7E = 0x3F + 0x3F = all bits).
	data := []byte("#1;2;100;0;0#1~")
	img, err := Decode(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 1 || img.Height != 6 {
		t.Fatalf("dims = %dx%d, want 1x6", img.Width, img.Height)
	}
	// Every one of the 6 pixels should be red (255,0,0,255).
	for y := range 6 {
		o := (y*img.Width + 0) * 4
		if img.Pixels[o] != 255 || img.Pixels[o+1] != 0 || img.Pixels[o+2] != 0 || img.Pixels[o+3] != 255 {
			t.Fatalf("pixel (0,%d) = %v, want red", y, img.Pixels[o:o+4])
		}
	}
}

func TestDecodeRepeat(t *testing.T) {
	// Repeat a full column 5 times -> width 5.
	data := []byte("#1;2;0;100;0!5~")
	img, err := Decode(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 5 || img.Height != 6 {
		t.Fatalf("dims = %dx%d, want 5x6", img.Width, img.Height)
	}
	// Pixel (3,0) should be green.
	o := (0*img.Width + 3) * 4
	if img.Pixels[o+1] != 255 {
		t.Fatalf("repeated pixel not green: %v", img.Pixels[o:o+4])
	}
}

func TestDecodeBands(t *testing.T) {
	// Two bands separated by '-': second band starts at y=6.
	data := []byte("#1;2;100;100;100~-~")
	img, err := Decode(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	if img.Height != 12 {
		t.Fatalf("height = %d, want 12 (two bands)", img.Height)
	}
}

func TestDecodeCarriageReturn(t *testing.T) {
	// '$' returns to x=0 in the same band; overwrite with a second color.
	data := []byte("#1;2;100;0;0~$#2;2;0;0;100~")
	img, err := Decode(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 1 {
		t.Fatalf("width = %d, want 1 (CR reused column 0)", img.Width)
	}
	// Column 0 should now be blue (second write).
	if img.Pixels[2] != 255 {
		t.Fatalf("pixel after CR overwrite = %v, want blue", img.Pixels[0:4])
	}
}

func TestDecodeEmpty(t *testing.T) {
	img, err := Decode(nil, []byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 0 || img.Height != 0 {
		t.Fatalf("empty sixel should be 0x0, got %dx%d", img.Width, img.Height)
	}
}
