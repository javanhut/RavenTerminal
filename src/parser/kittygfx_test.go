package parser

import (
	"encoding/base64"
	"testing"
)

func apc(payload string) []byte {
	return []byte("\x1b_G" + payload + "\x1b\\")
}

func TestKittyTransmitRGBA(t *testing.T) {
	term := NewTerminal(20, 5)
	// 2x2 RGBA = 16 bytes.
	raw := make([]byte, 16)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	b64 := base64.StdEncoding.EncodeToString(raw)
	term.Process(apc("f=32,s=2,v=2,i=7,a=t;" + b64))

	img, ok := term.Images().Get(7)
	if !ok {
		t.Fatal("image id 7 not stored")
	}
	if img.Width != 2 || img.Height != 2 {
		t.Fatalf("dims = %dx%d, want 2x2", img.Width, img.Height)
	}
	if len(img.Pixels) != 16 || img.Pixels[0] != 1 {
		t.Fatalf("pixels wrong: len=%d first=%d", len(img.Pixels), img.Pixels[0])
	}
}

func TestKittyTransmitRGB(t *testing.T) {
	term := NewTerminal(20, 5)
	// 1x1 RGB = 3 bytes -> expanded to RGBA with alpha 255.
	b64 := base64.StdEncoding.EncodeToString([]byte{10, 20, 30})
	term.Process(apc("f=24,s=1,v=1,i=3,a=t;" + b64))
	img, ok := term.Images().Get(3)
	if !ok {
		t.Fatal("RGB image not stored")
	}
	if len(img.Pixels) != 4 || img.Pixels[3] != 255 {
		t.Fatalf("RGB->RGBA expansion wrong: %v", img.Pixels)
	}
}

func TestKittyChunkedTransmit(t *testing.T) {
	term := NewTerminal(20, 5)
	raw := make([]byte, 16) // 2x2 RGBA
	for i := range raw {
		raw[i] = byte(i)
	}
	half1 := base64.StdEncoding.EncodeToString(raw[:8])
	half2 := base64.StdEncoding.EncodeToString(raw[8:])
	term.Process(apc("f=32,s=2,v=2,i=9,a=t,m=1;" + half1)) // more chunks
	term.Process(apc("m=0;" + half2))                       // final chunk
	img, ok := term.Images().Get(9)
	if !ok {
		t.Fatal("chunked image not assembled")
	}
	if len(img.Pixels) != 16 {
		t.Fatalf("chunked pixels len = %d, want 16", len(img.Pixels))
	}
}

func TestKittyDisplayCreatesPlacement(t *testing.T) {
	term := NewTerminal(20, 5)
	b64 := base64.StdEncoding.EncodeToString(make([]byte, 4))
	term.Process(apc("f=32,s=1,v=1,i=1,a=T,c=3,r=2;" + b64)) // transmit + display
	pls := term.Images().Placements()
	if len(pls) != 1 {
		t.Fatalf("placements = %d, want 1", len(pls))
	}
	if pls[0].Cols != 3 || pls[0].Rows != 2 {
		t.Fatalf("placement cover = %dx%d, want 3x2", pls[0].Cols, pls[0].Rows)
	}
}

func TestKittyDelete(t *testing.T) {
	term := NewTerminal(20, 5)
	b64 := base64.StdEncoding.EncodeToString(make([]byte, 4))
	term.Process(apc("f=32,s=1,v=1,i=5,a=T;" + b64))
	term.Process(apc("a=d,d=A")) // delete all
	if len(term.Images().Placements()) != 0 {
		t.Fatal("placements not cleared after delete-all")
	}
	if _, ok := term.Images().Get(5); ok {
		t.Fatal("image not freed after d=A")
	}
}

func TestKittyQueryReply(t *testing.T) {
	term := NewTerminal(20, 5)
	out := captureResponses(term)
	term.Process(apc("i=42,a=q;")) // query
	if want := "\x1b_Gi=42;OK\x1b\\"; string(*out) != want {
		t.Fatalf("query reply = %q, want %q", string(*out), want)
	}
}

func TestKittyQuietSuppressesOK(t *testing.T) {
	term := NewTerminal(20, 5)
	out := captureResponses(term)
	b64 := base64.StdEncoding.EncodeToString(make([]byte, 4))
	term.Process(apc("f=32,s=1,v=1,i=1,a=t,q=1;" + b64)) // quiet success
	if len(*out) != 0 {
		t.Fatalf("q=1 should suppress OK reply, got %q", string(*out))
	}
}
