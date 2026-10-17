package parser

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javanhut/RavenTerminal/src/images"
)

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func rgba(w, h int) []byte {
	p := make([]byte, w*h*4)
	for i := range p {
		p[i] = byte(i)
	}
	return p
}

func TestKittyChunkedUsesFirstChunkControls(t *testing.T) {
	term := NewTerminal(40, 10)
	term.SetCellPixelSize(10, 20)
	out := captureResponses(term)
	enc := b64(rgba(20, 40))
	half := len(enc) / 2
	half -= half % 4
	term.Process(apc("a=T,f=32,s=20,v=40,i=9,q=1,m=1;" + enc[:half]))
	term.Process(apc("m=0;" + enc[half:]))
	if _, ok := term.Images().Get(9); !ok {
		t.Fatal("image not stored")
	}
	ps := term.Images().Placements()
	if len(ps) != 1 {
		t.Fatalf("a=T from first chunk not honoured: %d placements", len(ps))
	}
	if ps[0].Cols != 2 || ps[0].Rows != 2 {
		t.Fatalf("placement size %dx%d, want 2x2", ps[0].Cols, ps[0].Rows)
	}
	if len(*out) != 0 {
		t.Fatalf("q=1 from first chunk not honoured: %q", *out)
	}
	// Cursor advanced right by cols, down by rows-1.
	expectCursor(t, term, 2, 1)
}

func TestKittyNoCursorMoveC1(t *testing.T) {
	term := NewTerminal(40, 10)
	term.SetCellPixelSize(10, 20)
	term.Process(apc("a=T,f=32,s=20,v=40,i=1,C=1,q=2;" + b64(rgba(20, 40))))
	expectCursor(t, term, 0, 0)
}

func TestKittyNoReplyWithoutID(t *testing.T) {
	term := NewTerminal(40, 10)
	out := captureResponses(term)
	term.Process(apc("a=t,f=32,s=1,v=1;" + b64(rgba(1, 1))))
	if len(*out) != 0 {
		t.Fatalf("reply without i: %q", *out)
	}
}

func TestKittyQueryTrueResult(t *testing.T) {
	term := NewTerminal(40, 10)
	out := captureResponses(term)
	term.Process(apc("a=q,i=31,s=1,v=1,f=24;" + b64([]byte{1, 2, 3})))
	if string(*out) != "\x1b_Gi=31;OK\x1b\\" {
		t.Fatalf("good query = %q", *out)
	}
	if _, ok := term.Images().Get(31); ok {
		t.Fatal("query must not store")
	}
	*out = nil
	term.Process(apc("a=q,i=32,s=2,v=2,f=24;" + b64([]byte{1, 2, 3})))
	if !strings.HasPrefix(string(*out), "\x1b_Gi=32;E") {
		t.Fatalf("bad query = %q", *out)
	}
}

func TestKittyDeleteLowerKeepsData(t *testing.T) {
	term := NewTerminal(40, 10)
	term.Process(apc("a=T,f=32,s=1,v=1,i=5,q=2;" + b64(rgba(1, 1))))
	term.Process(apc("a=d,d=i,i=5"))
	if len(term.Images().Placements()) != 0 {
		t.Fatal("placements remain")
	}
	if _, ok := term.Images().Get(5); !ok {
		t.Fatal("d=i freed data")
	}
	term.Process(apc("a=d,d=I,i=5"))
	if _, ok := term.Images().Get(5); ok {
		t.Fatal("d=I kept data")
	}
}

func TestKittyZlibAndPlacementOptions(t *testing.T) {
	term := NewTerminal(40, 10)
	term.SetCellPixelSize(10, 20)
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write(rgba(4, 4))
	zw.Close()
	term.Process(apc("a=t,f=32,o=z,s=4,v=4,i=2,q=2;" + b64(buf.Bytes())))
	img, ok := term.Images().Get(2)
	if !ok || img.Width != 4 || len(img.Pixels) != 64 {
		t.Fatal("zlib image not decoded")
	}
	term.Process(apc("a=p,i=2,p=7,x=1,y=1,w=2,h=2,X=3,Y=4,z=-5,q=2"))
	ps := term.Images().Placements()
	if len(ps) != 1 {
		t.Fatal("no placement")
	}
	p := ps[0]
	if p.SrcX != 1 || p.SrcY != 1 || p.SrcW != 2 || p.SrcH != 2 || p.OffsetX != 3 || p.OffsetY != 4 || p.ZIndex != -5 || p.PlacementID != 7 {
		t.Fatalf("placement %+v", p)
	}
	// Same image+placement id replaces.
	term.Process(apc("a=p,i=2,p=7,c=3,r=2,q=2"))
	ps = term.Images().Placements()
	if len(ps) != 1 || ps[0].Cols != 3 || ps[0].Rows != 2 {
		t.Fatalf("replace failed: %d %+v", len(ps), ps[0])
	}
}

func TestKittyImageNumbers(t *testing.T) {
	term := NewTerminal(40, 10)
	out := captureResponses(term)
	term.Process(apc("a=t,f=32,s=1,v=1,I=77;" + b64(rgba(1, 1))))
	r := string(*out)
	if !strings.HasPrefix(r, "\x1b_Gi=") || !strings.Contains(r, ",I=77;OK") {
		t.Fatalf("I= reply = %q", r)
	}
	*out = nil
	term.Process(apc("a=p,I=77"))
	if len(term.Images().Placements()) != 1 {
		t.Fatal("placement by number failed")
	}
	term.Process(apc("a=d,d=N,I=77"))
	if len(term.Images().Placements()) != 0 {
		t.Fatal("delete by number failed")
	}
}

func TestKittyDeleteSelectors(t *testing.T) {
	setup := func() *Terminal {
		term := NewTerminal(40, 10)
		term.SetCellPixelSize(10, 10)
		d := b64(rgba(10, 10))
		term.Process(apc("a=t,f=32,s=10,v=10,i=1,q=2;" + d))
		term.Process(apc("a=t,f=32,s=10,v=10,i=2,q=2;" + d))
		term.Process(apc("a=t,f=32,s=10,v=10,i=3,q=2;" + d))
		feed(term, "\x1b[1;1H")
		term.Process(apc("a=p,i=1,z=1,C=1,q=2")) // cell (1,1)
		feed(term, "\x1b[3;5H")
		term.Process(apc("a=p,i=2,z=2,C=1,q=2")) // cell (5,3)
		feed(term, "\x1b[6;10H")
		term.Process(apc("a=p,i=3,z=3,C=1,q=2")) // cell (10,6)
		return term
	}
	ids := func(term *Terminal) []images.ImageID {
		var r []images.ImageID
		for _, p := range term.Images().Placements() {
			r = append(r, p.ImageID)
		}
		return r
	}
	cases := []struct {
		cmd  string
		left int
	}{
		{"d=a", 0},
		{"d=p,x=5,y=3", 2},
		{"d=q,x=5,y=3,z=2", 2},
		{"d=q,x=5,y=3,z=9", 3},
		{"d=x,x=10", 2},
		{"d=y,y=1", 2},
		{"d=z,z=3", 2},
		{"d=r,x=1,y=2", 1},
		{"d=c", 2}, // cursor is at (10,6)
	}
	for _, tc := range cases {
		term := setup()
		term.Process(apc("a=d," + tc.cmd))
		if got := ids(term); len(got) != tc.left {
			t.Errorf("%s: remaining %v", tc.cmd, got)
		}
	}
	term := setup()
	term.Process(apc("a=d,d=R,x=1,y=2"))
	if _, ok := term.Images().Get(1); ok {
		t.Error("d=R kept data")
	}
	if _, ok := term.Images().Get(3); !ok {
		t.Error("d=R freed out-of-range image")
	}
}

func TestKittyFileTransmission(t *testing.T) {
	term := NewTerminal(40, 10)
	dir := t.TempDir()
	path := filepath.Join(dir, "img.rgba")
	os.WriteFile(path, rgba(2, 2), 0o600)
	term.Process(apc("a=t,t=f,f=32,s=2,v=2,i=4,q=2;" + b64([]byte(path))))
	if _, ok := term.Images().Get(4); !ok {
		t.Fatal("t=f not loaded")
	}
	// Non-regular files are rejected.
	out := captureResponses(term)
	term.Process(apc("a=t,t=f,f=32,s=1,v=1,i=6;" + b64([]byte("/dev/zero"))))
	if _, ok := term.Images().Get(6); ok || !strings.Contains(string(*out), "i=6;E") {
		t.Fatalf("/dev/zero accepted: %q", *out)
	}
}

func TestKittyTempFileDeleted(t *testing.T) {
	term := NewTerminal(40, 10)
	f, err := os.CreateTemp("/tmp", "tty-graphics-protocol-*")
	if err != nil {
		t.Skip("no /tmp")
	}
	f.Write(rgba(1, 1))
	f.Close()
	term.Process(apc("a=t,t=t,f=32,s=1,v=1,i=8,q=2;" + b64([]byte(f.Name()))))
	if _, ok := term.Images().Get(8); !ok {
		t.Fatal("t=t not loaded")
	}
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		os.Remove(f.Name())
		t.Fatal("temp file not deleted")
	}
	// Temp files outside /tmp and /dev/shm are refused and left alone.
	path := filepath.Join(t.TempDir(), "keep")
	if strings.HasPrefix(path, "/tmp/") {
		home, _ := os.UserHomeDir()
		dir, err := os.MkdirTemp(home, ".raven-kitty-test")
		if err != nil {
			t.Skip("no home dir")
		}
		defer os.RemoveAll(dir)
		path = filepath.Join(dir, "keep")
	}
	os.WriteFile(path, rgba(1, 1), 0o600)
	term.Process(apc("a=t,t=t,f=32,s=1,v=1,i=9,q=2;" + b64([]byte(path))))
	if _, ok := term.Images().Get(9); ok {
		t.Fatal("t=t outside temp dirs accepted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("refused temp file was deleted")
	}
}

// macOS clients write t=t files to $TMPDIR, and its /tmp is a symlink to
// /private/tmp: a temp dir reached through a symlinked parent is accepted,
// a symlink as the file itself is not.
func TestKittyTempFileInSymlinkedTMPDIR(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "private")
	os.Mkdir(real, 0o700)
	link := filepath.Join(base, "tmp")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	t.Setenv("TMPDIR", link)
	term := NewTerminal(40, 10)
	path := filepath.Join(link, "tty-graphics-protocol-a")
	os.WriteFile(path, rgba(1, 1), 0o600)
	term.Process(apc("a=t,t=t,f=32,s=1,v=1,i=11,q=2;" + b64([]byte(path))))
	if _, ok := term.Images().Get(11); !ok {
		t.Fatal("t=t in $TMPDIR via a symlinked dir not loaded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("temp file not deleted")
	}

	target := filepath.Join(real, "tty-graphics-protocol-target")
	os.WriteFile(target, rgba(1, 1), 0o600)
	alias := filepath.Join(link, "tty-graphics-protocol-alias")
	os.Symlink(target, alias)
	term.Process(apc("a=t,t=t,f=32,s=1,v=1,i=12,q=2;" + b64([]byte(alias))))
	if _, ok := term.Images().Get(12); ok {
		t.Fatal("t=t followed a symlinked file")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("symlink target was deleted")
	}
}

func TestKittySharedMemory(t *testing.T) {
	if _, err := os.Stat("/dev/shm"); err != nil {
		t.Skip("no /dev/shm")
	}
	name := "raven-kitty-test-shm"
	os.WriteFile("/dev/shm/"+name, rgba(1, 1), 0o600)
	defer os.Remove("/dev/shm/" + name)
	term := NewTerminal(40, 10)
	term.Process(apc("a=t,t=s,f=32,s=1,v=1,i=11,q=2;" + b64([]byte(name))))
	if _, ok := term.Images().Get(11); !ok {
		t.Fatal("t=s not loaded")
	}
	if _, err := os.Stat("/dev/shm/" + name); !os.IsNotExist(err) {
		t.Fatal("shm object not unlinked")
	}
	term.Process(apc("a=t,t=s,f=32,s=1,v=1,i=12,q=2;" + b64([]byte("../etc/passwd"))))
	if _, ok := term.Images().Get(12); ok {
		t.Fatal("path traversal accepted")
	}
}

func TestKittyVirtualPlacement(t *testing.T) {
	term := NewTerminal(40, 10)
	term.Process(apc("a=T,U=1,f=32,s=1,v=1,i=21,c=4,r=2,q=2;" + b64(rgba(1, 1))))
	expectCursor(t, term, 0, 0)
	v, ok := term.Images().VirtualPlacement(21, 0)
	if !ok || v.Cols != 4 || v.Rows != 2 || !v.Virtual {
		t.Fatalf("virtual placement = %+v %v", v, ok)
	}
	for _, p := range term.Images().Placements() {
		if p.Virtual {
			t.Fatal("virtual placement listed with real placements")
		}
	}
}

func TestRISClearsImages(t *testing.T) {
	term := NewTerminal(40, 10)
	term.Process(apc("a=T,f=32,s=1,v=1,i=5,q=2;" + b64(rgba(1, 1))))
	feed(term, "\x1bc")
	if _, ok := term.Images().Get(5); ok || len(term.Images().Placements()) != 0 {
		t.Fatal("RIS kept images")
	}
}
