package parser

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javanhut/RavenTerminal/src/grid"
)

func within(t *testing.T, d time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("did not finish within %v", d)
	}
}

func onScreenPlacements(term *Terminal) int {
	n := 0
	_, row := term.Grid.GetCursor()
	top := term.Grid.AbsoluteCursorRow() - row
	for _, p := range term.Images().Placements() {
		r0 := p.AnchorAbsRow - top
		if !p.Virtual && r0 < term.Grid.Rows && r0+p.Rows > 0 {
			n++
		}
	}
	return n
}

func TestEDClearsImagesOnScreen(t *testing.T) {
	for name, img := range map[string]string{
		"kitty": string(apc("a=T,f=32,s=1,v=1,r=3,q=2;" + b64(rgba(1, 1)))),
		"sixel": sixelImage(10, 6),
	} {
		term := NewTerminal(20, 6)
		term.SetCellPixelSize(10, 12)
		feed(term, "$ show\r\n"+img+"\x1b[2;1H$ clear\r\n")
		if n := onScreenPlacements(term); n != 1 {
			t.Fatalf("%s: setup placed %d images", name, n)
		}
		feed(term, "\x1b[H\x1b[2J\x1b[3J")
		if n := onScreenPlacements(term); n != 0 {
			t.Fatalf("%s: %d placements survive ED 2", name, n)
		}
	}
}

func TestEraseOpsClearPendingWrap(t *testing.T) {
	for _, seq := range []string{"\x1b[K", "\x1b[1K", "\x1b[2K", "\x1b[J", "\x1b[X", "\x1b[@", "\x1b[P"} {
		term := NewTerminal(10, 3)
		feed(term, "0123456789"+seq+"X")
		if c := term.Grid.GetCell(9, 0); c.Char != 'X' {
			t.Fatalf("%q: X not at col 9 row 0 (got %q)", seq, c.Char)
		}
		if got := lineText(term, 1); got != "" {
			t.Fatalf("%q: X wrapped to row 1: %q", seq, got)
		}
	}
}

func TestAltScreenStartsWithoutImages(t *testing.T) {
	term := NewTerminal(20, 6)
	feed(term, "\x1b[?1049h")
	term.Process(apc("a=T,f=32,s=1,v=1,i=3,q=2;" + b64(rgba(1, 1))))
	feed(term, "\x1b[?1049l\x1b[?1049h")
	if n := len(term.Images().Placements()); n != 0 {
		t.Fatalf("stale alt placements: %d", n)
	}
	if _, ok := term.Images().Get(3); ok {
		t.Fatal("stale alt image")
	}
}

func TestAltScreenImagesScrollWithText(t *testing.T) {
	term := NewTerminal(20, 4)
	feed(term, "\x1b[?1049h")
	term.Process(apc("a=T,f=32,s=1,v=1,C=1,q=2;" + b64(rgba(1, 1))))
	before := term.Grid.AbsoluteCursorRow()
	feed(term, "\x1b[4;1H\n\n\n")
	_, row := term.Grid.GetCursor()
	top := term.Grid.AbsoluteCursorRow() - row
	if top-before != 3 {
		t.Fatalf("alt screen top advanced %d rows, want 3", top-before)
	}
	if p := term.Images().Placements()[0]; p.AnchorAbsRow-top != -3 {
		t.Fatalf("placement on-screen row %d, want -3", p.AnchorAbsRow-top)
	}
}

func TestSixelIDsOutsideKittySpace(t *testing.T) {
	term := NewTerminal(40, 10)
	term.SetCellPixelSize(10, 12)
	feed(term, sixelImage(4, 1))
	ps := term.Images().Placements()
	if len(ps) != 1 {
		t.Fatalf("placements %d", len(ps))
	}
	sid := ps[0].ImageID
	if sid <= 0xffffffff {
		t.Fatalf("sixel id %d inside kitty id space", sid)
	}
	term.Process(apc("a=t,f=32,s=7,v=1,i=1,q=2;" + b64(rgba(7, 1))))
	if img, _ := term.Images().Get(sid); img == nil || img.Width != 4 {
		t.Fatal("kitty i=1 clobbered the sixel image")
	}
	term.Process(apc("a=d,d=I,i=1,q=2"))
	if _, ok := term.Images().Get(sid); !ok {
		t.Fatal("kitty d=I,i=1 deleted the sixel")
	}
}

func TestStringAbortedByEscape(t *testing.T) {
	for _, intro := range []string{"\x1bPzz", "\x1b_Gzz", "\x1b^zz", "\x1bXzz"} {
		term := NewTerminal(20, 2)
		feed(term, intro+"\x1b[31mHello")
		if got := lineText(term, 0); got != "Hello" {
			t.Fatalf("%q swallowed following output: %q", intro, got)
		}
		if c := term.Grid.GetCell(0, 0); c.Fg.Type != grid.ColorIndexed || c.Fg.Index != 1 {
			t.Fatalf("%q: SGR after aborted string lost: %+v", intro, c.Fg)
		}
	}
}

func TestRISKeepsHostColorScheme(t *testing.T) {
	term := NewTerminal(10, 2)
	term.SetDarkMode(false)
	feed(term, "\x1bc")
	if got := reply(term, "\x1b[?996n"); got != "\x1b[?997;2n" {
		t.Fatalf("after RIS 996n = %q, want light", got)
	}
	if fresh := reply(NewTerminal(10, 2), "\x1b[?996n"); fresh != "\x1b[?997;1n" {
		t.Fatalf("new terminal 996n = %q, want dark", fresh)
	}
}

func TestOSC7PercentDecodedOnce(t *testing.T) {
	for in, want := range map[string]string{
		"file://host/tmp/100%25":   "/tmp/100%",
		"file://host/tmp/a%2520b":  "/tmp/a%20b",
		"file://host/tmp/a%20b":    "/tmp/a b",
		"file://host/home/u/x%23y": "/home/u/x#y",
	} {
		term := NewTerminal(10, 2)
		feed(term, "\x1b]7;"+in+"\x07")
		if got := term.WorkingDir(); got != want {
			t.Fatalf("%s -> %q, want %q", in, got, want)
		}
	}
}

func TestAltScreenKeepsCursorStyle(t *testing.T) {
	term := NewTerminal(10, 2)
	feed(term, "\x1b[6 q\x1b[?1049h\x1b[?1049l")
	if s := term.CursorStyle(); s != CursorStyleBar {
		t.Fatalf("cursor style %v after alt screen, want bar", s)
	}
	if got := reply(term, "\x1bP$q q\x1b\\"); !strings.Contains(got, "6 q") {
		t.Fatalf("DECRQSS = %q", got)
	}
}

func assertNoInjectedControls(t *testing.T, reply []byte, intro, term string) {
	t.Helper()
	s := string(reply)
	if !strings.HasPrefix(s, intro) || !strings.HasSuffix(s, term) {
		t.Fatalf("reply framing %q", s)
	}
	for _, c := range []byte(s[len(intro) : len(s)-len(term)]) {
		if c < 0x20 || c == 0x7f {
			t.Fatalf("control byte %#x echoed into PTY: %q", c, s)
		}
	}
}

func TestXTGETTCAPNeverEchoesRawName(t *testing.T) {
	term := NewTerminal(10, 2)
	out := captureResponses(term)
	feed(term, "\x1bP+q\r\x15$'\\164\\157\\165\\143\\150' $'\\57\\164\\155\\160\\57\\160'\r\x1b\\")
	assertNoInjectedControls(t, *out, "\x1bP0+r", "\x1b\\")
	if strings.Contains(string(*out), "$'") {
		t.Fatalf("raw request echoed: %q", *out)
	}
	*out = nil
	feed(term, "\x1bP+q7a7a\x1b\\") // unknown "zz": hex name echoed, uppercased
	if string(*out) != "\x1bP0+r7A7A\x1b\\" {
		t.Fatalf("unknown cap reply %q", *out)
	}
}

func TestOSC99QueryRejectsBadID(t *testing.T) {
	term := NewTerminal(10, 2)
	out := captureResponses(term)
	feed(term, "\x1b]99;i=\r\x15touch /tmp/pwned\r:p=?;\x07")
	if len(*out) != 0 {
		t.Fatalf("bad id echoed: %q", *out)
	}
	feed(term, "\x1b]99;i=ok-1:p=?;\x07")
	assertNoInjectedControls(t, *out, "\x1b]99;", "\x07")
	if !strings.Contains(string(*out), "i=ok-1") {
		t.Fatalf("valid id query reply %q", *out)
	}
}

func TestHugeScrollAndRepeatCountsAreBounded(t *testing.T) {
	for _, seq := range []string{
		"\x1b[9223372036854775807S", "\x1b[9223372036854775807T", "\x1b[300000000S",
		"x\x1b[2000000000b", "x\x1b[9223372036854775807b",
	} {
		term := NewTerminal(20, 5)
		feed(term, "abc\r\n")
		within(t, time.Second, func() { feed(term, seq) })
		if seq[0] != 'x' {
			for r := range 5 {
				if got := lineText(term, r); got != "" {
					t.Fatalf("%q: row %d = %q, want blank", seq, r, got)
				}
			}
		}
	}
}

func TestKittyHugePlacementSizeBounded(t *testing.T) {
	for _, ctl := range []string{"r=2000000000", "h=2000000000", "Y=2000000000", "r=9223372036854775807"} {
		term := NewTerminal(20, 5)
		term.SetCellPixelSize(10, 20)
		within(t, time.Second, func() {
			term.Process(apc("a=T,f=24,s=1,v=1,q=2," + ctl + ";AAAA"))
		})
	}
	term := NewTerminal(20, 5)
	term.Process(apc("a=T,f=24,s=1,v=1,q=2,c=9223372036854775807;AAAA"))
	if col, _ := term.Grid.GetCursor(); col != 19 {
		t.Fatalf("c=MaxInt cursor col %d, want clamped to last column", col)
	}
}

func TestKittySharedMemoryRefusesSymlink(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(victim, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "raven-fixer-link-" + strings.ReplaceAll(t.Name(), "/", "_")
	link := "/dev/shm/" + name
	os.Remove(link)
	if err := os.Symlink(victim, link); err != nil {
		t.Skip("no /dev/shm:", err)
	}
	defer os.Remove(link)
	term := NewTerminal(20, 5)
	out := captureResponses(term)
	term.Process(apc("a=t,t=s,f=24,s=1,v=1,i=7;" + base64.StdEncoding.EncodeToString([]byte(name))))
	if !strings.Contains(string(*out), "EPERM") {
		t.Fatalf("reply %q, want EPERM", *out)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("symlink target deleted")
	}
}

func TestKittySharedMemoryRegularFile(t *testing.T) {
	name := "raven-fixer-shm-" + strings.ReplaceAll(t.Name(), "/", "_")
	path := "/dev/shm/" + name
	if err := os.WriteFile(path, []byte{1, 2, 3}, 0o600); err != nil {
		t.Skip("no /dev/shm:", err)
	}
	defer os.Remove(path)
	term := NewTerminal(20, 5)
	out := captureResponses(term)
	term.Process(apc("a=t,t=s,f=24,s=1,v=1,i=7;" + base64.StdEncoding.EncodeToString([]byte(name))))
	if !strings.Contains(string(*out), "OK") {
		t.Fatalf("reply %q", *out)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("shm object not removed after read")
	}
}

func TestKittyRGBAPixelsDoNotPinPayload(t *testing.T) {
	term := NewTerminal(20, 5)
	payload := make([]byte, 1<<20)
	term.Process(apc("a=t,f=32,s=1,v=1,i=4,q=2;" + b64(payload)))
	img, ok := term.Images().Get(4)
	if !ok {
		t.Fatal("image not stored")
	}
	if cap(img.Pixels) != 4 {
		t.Fatalf("pixels retain %d bytes of payload", cap(img.Pixels))
	}
}

func hugePNG(w, h uint32) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	b := buf.Bytes()
	// IHDR data starts at 16; CRC covers type+data (bytes 12..29).
	binary.BigEndian.PutUint32(b[16:], w)
	binary.BigEndian.PutUint32(b[20:], h)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

func TestKittyPNGAreaCapped(t *testing.T) {
	term := NewTerminal(20, 5)
	out := captureResponses(term)
	term.Process(apc("a=t,f=100,i=2;" + b64(hugePNG(9000, 9000))))
	if !strings.Contains(string(*out), "EFBIG") {
		t.Fatalf("reply %q, want EFBIG", *out)
	}
	*out = nil
	term.Process(apc("a=t,f=32,s=9000,v=9000,i=3;" + b64(rgba(1, 1))))
	if !strings.Contains(string(*out), "EFBIG") {
		t.Fatalf("raw reply %q, want EFBIG", *out)
	}
}

func TestLinkSweepKeepsSavedCursorLink(t *testing.T) {
	term := NewTerminal(20, 3)
	feed(term, "\x1b]8;;http://a.example\x07\x1b7\x1b]8;;\x07")
	var b strings.Builder
	for i := range 0x10000 {
		b.WriteString("\x1b]8;;u")
		b.WriteString(itoa(i))
		b.WriteString("\x07")
	}
	b.WriteString("\x1b]8;;\x07")
	feed(term, b.String())
	feed(term, "\x1b8Z")
	_, row := term.Grid.GetCursor()
	c := term.Grid.GetCell(0, row)
	if got := term.Grid.LinkURL(c.Link); got != "http://a.example" {
		t.Fatalf("restored link resolves to %q", got)
	}
}

func TestDefaultCursorStyle(t *testing.T) {
	term := NewTerminal(10, 2)
	term.SetDefaultCursorStyle(CursorStyleBar)
	if s := term.CursorStyle(); s != CursorStyleBar {
		t.Fatalf("default not applied: %v", s)
	}
	feed(term, "\x1b[2 q")
	if term.CursorStyle() != CursorStyleBlock {
		t.Fatal("DECSCUSR 2 ignored")
	}
	feed(term, "\x1b[0 q")
	if term.CursorStyle() != CursorStyleBar {
		t.Fatal("DECSCUSR 0 must restore the configured default")
	}
	feed(term, "\x1b[4 q\x1b[!p")
	if term.CursorStyle() != CursorStyleBar {
		t.Fatal("DECSTR must restore the configured default")
	}
	feed(term, "\x1b[4 q\x1bc")
	if term.CursorStyle() != CursorStyleBar {
		t.Fatal("RIS must restore the configured default")
	}
	feed(term, "\x1b[4 q")
	term.SetDefaultCursorStyle(CursorStyleBlock)
	if term.CursorStyle() != CursorStyleUnderline {
		t.Fatal("changing the default must not override an app's DECSCUSR")
	}
}
