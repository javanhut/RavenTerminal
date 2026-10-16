package parser

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/javanhut/RavenTerminal/src/images"
)

func pixB64(px ...[4]byte) string {
	var b []byte
	for _, p := range px {
		b = append(b, p[:]...)
	}
	return base64.StdEncoding.EncodeToString(b)
}

var (
	pRed   = [4]byte{255, 0, 0, 255}
	pGreen = [4]byte{0, 255, 0, 255}
	pBlue  = [4]byte{0, 0, 255, 255}
)

func animTerm(t *testing.T) *Terminal {
	t.Helper()
	term := NewTerminal(20, 5)
	term.Process(apc("a=T,f=32,s=2,v=1,i=1,q=2;" + pixB64(pRed, pRed)))
	return term
}

func TestKittyFrameLoadReplies(t *testing.T) {
	term := animTerm(t)
	got := reply(term, string(apc("a=f,i=1,f=32,s=1,v=1,x=1,c=1;"+pixB64(pGreen))))
	if got != "\x1b_Gi=1;OK\x1b\\" {
		t.Fatalf("a=f reply = %q", got)
	}
	store := term.Images()
	if store.FrameCount(1) != 2 {
		t.Fatalf("frames = %d", store.FrameCount(1))
	}
	f := store.FrameImage(1, 2)
	if [4]byte(f.Pixels[0:4]) != pRed || [4]byte(f.Pixels[4:8]) != pGreen {
		t.Fatalf("frame 2 = %v", f.Pixels)
	}
	if got := reply(term, string(apc("a=f,i=1,q=1,f=32,s=1,v=1;"+pixB64(pBlue)))); got != "" {
		t.Fatalf("q=1 OK reply = %q", got)
	}
	if got := reply(term, string(apc("a=f,i=9,f=32,s=1,v=1;"+pixB64(pBlue)))); !strings.HasPrefix(got, "\x1b_Gi=9;ENOENT:") {
		t.Fatalf("missing image reply = %q", got)
	}
	if got := reply(term, string(apc("a=f,i=1,f=32,s=2,v=1,x=1;"+pixB64(pBlue, pBlue)))); !strings.HasPrefix(got, "\x1b_Gi=1;EINVAL:") {
		t.Fatalf("out of bounds reply = %q", got)
	}
	if got := reply(term, string(apc("a=f,i=1,q=2,f=32,s=2,v=1,x=1;"+pixB64(pBlue, pBlue)))); got != "" {
		t.Fatalf("q=2 error reply = %q", got)
	}
	// Frame data defaults to the image size when s/v are omitted.
	if got := reply(term, string(apc("a=f,i=1,f=32;"+pixB64(pBlue, pGreen)))); got != "\x1b_Gi=1;OK\x1b\\" {
		t.Fatalf("full-size frame reply = %q", got)
	}
}

func TestKittyFrameChunked(t *testing.T) {
	term := animTerm(t)
	data := pixB64(pBlue, pGreen)
	got := reply(term, string(apc("a=f,i=1,f=32,s=2,v=1,m=1;"+data[:4]))+string(apc("m=0;"+data[4:])))
	if got != "\x1b_Gi=1;OK\x1b\\" || term.Images().FrameCount(1) != 2 {
		t.Fatalf("chunked frame: reply %q frames %d", got, term.Images().FrameCount(1))
	}
}

func TestKittyAnimationControlAndCompose(t *testing.T) {
	term := animTerm(t)
	term.Process(apc("a=f,i=1,f=32,s=2,v=1,z=-1,q=2;" + pixB64(pGreen, pGreen)))
	term.Process(apc("a=f,i=1,f=32,s=2,v=1,q=2;" + pixB64(pBlue, pBlue)))
	if got := reply(term, string(apc("a=a,i=1,c=3"))); got != "\x1b_Gi=1;OK\x1b\\" {
		t.Fatalf("a=a reply = %q", got)
	}
	if term.Images().CurrentFrame(1) != 3 {
		t.Fatal("c= did not select frame 3")
	}
	if got := reply(term, string(apc("a=a,i=1,c=9"))); !strings.Contains(got, "ENOENT") {
		t.Fatalf("missing frame reply = %q", got)
	}
	// a=c: copy frame 2's left pixel over frame 3's right pixel.
	if got := reply(term, string(apc("a=c,i=1,r=2,c=3,w=1,h=1,X=1"))); got != "\x1b_Gi=1;OK\x1b\\" {
		t.Fatalf("a=c reply = %q", got)
	}
	f3 := term.Images().FrameImage(1, 3)
	if [4]byte(f3.Pixels[0:4]) != pBlue || [4]byte(f3.Pixels[4:8]) != pGreen {
		t.Fatalf("composed frame = %v", f3.Pixels)
	}
	if got := reply(term, string(apc("a=c,i=1,r=2,c=3,w=2,h=1,X=1"))); !strings.Contains(got, "EINVAL") {
		t.Fatalf("bad compose reply = %q", got)
	}
	if got := reply(term, string(apc("a=a,i=1,s=3,v=1,r=1,z=20"))); got != "\x1b_Gi=1;OK\x1b\\" {
		t.Fatalf("run reply = %q", got)
	}
	if got := reply(term, string(apc("a=x,i=1"))); !strings.Contains(got, "EINVAL") {
		t.Fatalf("unknown action reply = %q", got)
	}
}

func TestKittyAnimationDrivesRenderFrame(t *testing.T) {
	term := animTerm(t)
	term.Process(apc("a=f,i=1,f=32,s=2,v=1,z=15,q=2;" + pixB64(pGreen, pGreen)))
	var f RenderFrame
	term.RenderFrame(&f)
	if !f.NextAnimFrame.IsZero() {
		t.Fatal("a stopped animation scheduled a frame")
	}
	term.Process(apc("a=a,i=1,s=3,r=1,z=15,q=2"))
	term.RenderFrame(&f)
	if f.NextAnimFrame.IsZero() || time.Until(f.NextAnimFrame) > 20*time.Millisecond {
		t.Fatalf("next frame = %v", time.Until(f.NextAnimFrame))
	}
	first := f.Images[1]
	deadline := time.Now().Add(2 * time.Second)
	for f.Images[1] == first && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		term.RenderFrame(&f)
	}
	if f.Images[1] == first {
		t.Fatal("the rendered image never advanced to another frame")
	}
	if !f.Snap.Dirty {
		t.Fatal("a frame change did not mark the frame dirty")
	}
}

func TestKittyRelativePlacementFollowsAndDies(t *testing.T) {
	term := NewTerminal(40, 10)
	px := "AAAA/w=="
	term.Process(apc("f=32,s=1,v=1,i=1,a=t,q=2;" + px))
	term.Process(apc("f=32,s=1,v=1,i=2,a=t,q=2;" + px))
	feed(term, "\x1b[3;5H")
	term.Process(apc("a=p,i=1,p=7,C=1,q=2"))
	if got := reply(term, string(apc("a=p,i=2,p=1,P=1,Q=7,H=2,V=1"))); got != "\x1b_Gi=2,p=1;OK\x1b\\" {
		t.Fatalf("relative reply = %q", got)
	}
	child := func() *images.Placement {
		for _, p := range term.Images().Placements() {
			if p.ImageID == 2 {
				return p
			}
		}
		return nil
	}
	feed(term, "\x1b[6;10H")
	term.Process(apc("a=p,i=1,p=7,C=1,q=2")) // move the parent
	if c := child(); c == nil || c.AnchorCol != 11 || c.AnchorAbsRow != 6 {
		t.Fatalf("child after parent moved = %+v", c)
	}
	if got := reply(term, string(apc("a=p,i=1,p=7,P=2,Q=1"))); !strings.Contains(got, "ECYCLE") {
		t.Fatalf("cycle reply = %q", got)
	}
	term.Process(apc("a=d,d=i,i=1"))
	if c := child(); c != nil {
		t.Fatal("deleting the parent kept the child")
	}
}
