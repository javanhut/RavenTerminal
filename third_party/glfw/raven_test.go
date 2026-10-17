//go:build linux

package glfw

import (
	"os"
	"runtime"
	"testing"
	"time"
	"unicode"
)

func init() { runtime.LockOSThread() }

var (
	mainQueue = make(chan func())
	displayOK bool
)

// onMain runs f on the locked main OS thread, where GLFW must be driven.
func onMain(f func()) {
	done := make(chan struct{})
	mainQueue <- func() { defer close(done); f() }
	<-done
}

func TestMain(m *testing.M) {
	displayOK = Init() == nil && glfwInitialized()
	code := make(chan int, 1)
	go func() { code <- m.Run() }()
	for {
		select {
		case f := <-mainQueue:
			f()
		case c := <-code:
			if displayOK {
				Terminate()
			}
			os.Exit(c)
		}
	}
}

func needDisplay(t *testing.T) {
	t.Helper()
	if !displayOK {
		t.Skip("no display available")
	}
}

func newTestWindow(t *testing.T, visible bool, hints map[Hint]int) *Window {
	t.Helper()
	var w *Window
	var err error
	onMain(func() {
		DefaultWindowHints()
		if !visible {
			WindowHint(ClientAPI, NoAPI)
		}
		if visible {
			WindowHint(Visible, True)
		} else {
			WindowHint(Visible, False)
		}
		for h, v := range hints {
			WindowHint(h, v)
		}
		w, err = CreateWindow(320, 200, "raven-glfw-test", nil, nil)
	})
	if err != nil {
		t.Fatalf("CreateWindow: %v", err)
	}
	t.Cleanup(func() { onMain(w.Destroy) })
	return w
}

// present swaps a visible window's buffers so a Wayland surface gets mapped.
func present(w *Window) func() bool {
	return func() bool {
		w.MakeContextCurrent()
		w.SwapBuffers()
		return false
	}
}

// pump processes events on the main thread until cond holds or timeout.
func pump(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok := false
		onMain(func() {
			WaitEventsTimeout(0.02)
			ok = cond()
		})
		if ok {
			return true
		}
	}
	return false
}

func TestScaledSize(t *testing.T) {
	cases := []struct{ logical, scale120, want int }{
		{1000, 120, 1000},
		{1000, 150, 1250},
		{801, 180, 1202},
		{800, 144, 960},
		{333, 126, 350},
		{1, 240, 2},
		{0, 150, 0},
	}
	for _, c := range cases {
		if got := scaledSize(c.logical, c.scale120); got != c.want {
			t.Errorf("scaledSize(%d, %d) = %d, want %d", c.logical, c.scale120, got, c.want)
		}
	}
	if got := scaleFromWire(150); got != 1.25 {
		t.Errorf("scaleFromWire(150) = %v, want 1.25", got)
	}
	if got := scaleFromWire(0); got != 1 {
		t.Errorf("scaleFromWire(0) = %v, want 1", got)
	}
}

func TestTakeEnvToken(t *testing.T) {
	os.Setenv("RAVEN_TEST_TOKEN", "tok-123")
	if got := takeEnv("RAVEN_TEST_TOKEN"); got != "tok-123" {
		t.Fatalf("takeEnv = %q, want tok-123", got)
	}
	if got := takeEnv("RAVEN_TEST_TOKEN"); got != "" {
		t.Fatalf("token not consumed: second takeEnv = %q", got)
	}
	os.Setenv("RAVEN_TEST_TOKEN", "")
	if got := takeEnv("RAVEN_TEST_TOKEN"); got != "" {
		t.Fatalf("empty token returned %q", got)
	}
}

func TestPreeditEdit(t *testing.T) {
	buf := []rune{}
	steps := []struct {
		first, chg int
		ins        string
		want       string
	}{
		{0, 0, "にほ", "にほ"},
		{2, 0, "ん", "にほん"},
		{0, 3, "日本", "日本"},
		{1, 1, "", "日"},
		{5, 9, "x", "日x"}, // out-of-range edits clamp
		{0, 2, "", ""},
	}
	for i, s := range steps {
		buf = preeditEdit(buf, s.first, s.chg, []rune(s.ins))
		if string(buf) != s.want {
			t.Fatalf("step %d: got %q, want %q", i, string(buf), s.want)
		}
	}
}

func TestUTF8ByteOffset(t *testing.T) {
	cases := []struct {
		s     string
		runes int
		want  int
	}{
		{"abc", 2, 2},
		{"日本語", 2, 6},
		{"a🙂b", 2, 5},
		{"ab", 9, 2},
	}
	for _, c := range cases {
		if got := utf8ByteOffset(c.s, c.runes); got != c.want {
			t.Errorf("utf8ByteOffset(%q, %d) = %d, want %d", c.s, c.runes, got, c.want)
		}
	}
}

func TestPostEmptyEventWakesWait(t *testing.T) {
	needDisplay(t)
	onMain(func() { PollEvents() })
	go func() {
		time.Sleep(50 * time.Millisecond)
		PostEmptyEvent()
	}()
	var elapsed time.Duration
	onMain(func() {
		start := time.Now()
		WaitEventsTimeout(5)
		elapsed = time.Since(start)
	})
	if elapsed > 2*time.Second {
		t.Fatalf("WaitEventsTimeout not woken by PostEmptyEvent (took %v)", elapsed)
	}
}

func TestPreeditCallbackDispatch(t *testing.T) {
	needDisplay(t)
	w := newTestWindow(t, false, nil)
	var gotText string
	gotCursor := -2
	calls := 0
	onMain(func() {
		w.SetPreeditCallback(func(_ *Window, text string, cursor int) {
			calls++
			gotText, gotCursor = text, cursor
		})
		inputPreedit(w, "日本", 3)
	})
	if calls != 1 || gotText != "日本" || gotCursor != 3 {
		t.Fatalf("preedit callback got (%q, %d) after %d calls", gotText, gotCursor, calls)
	}
	onMain(func() {
		w.SetPreeditCallback(nil)
		inputPreedit(w, "x", 1)
	})
	if calls != 1 {
		t.Fatalf("preedit callback still called after unset")
	}
}

func TestCommitStringDeliversRunes(t *testing.T) {
	needDisplay(t)
	w := newTestWindow(t, false, nil)
	var got []rune
	onMain(func() {
		w.SetCharCallback(func(_ *Window, r rune) { got = append(got, r) })
		inputCommit(w, "aé日🙂")
	})
	if string(got) != "aé日🙂" {
		t.Fatalf("commit delivered %q", string(got))
	}
}

func TestPrimarySelectionRoundtrip(t *testing.T) {
	needDisplay(t)
	w := newTestWindow(t, false, nil)
	_ = w
	const want = "raven primary ✓ selection"
	onMain(func() { SetPrimarySelectionString(want) })
	var got string
	pump(func() bool { got = GetPrimarySelectionString(); return got == want }, time.Second)
	if got != want {
		t.Fatalf("primary selection = %q, want %q", got, want)
	}
	onMain(func() { SetClipboardString("clipboard-other") })
	onMain(func() { got = GetPrimarySelectionString() })
	if got != want {
		t.Fatalf("setting clipboard clobbered primary: %q", got)
	}
}

func TestCurrentMonitorTracksSurface(t *testing.T) {
	needDisplay(t)
	w := newTestWindow(t, true, nil)
	var m *Monitor
	ok := pump(func() bool { present(w)(); m = w.CurrentMonitor(); return m != nil }, 3*time.Second)
	if !ok {
		t.Fatal("CurrentMonitor stayed nil for a visible window")
	}
	found := false
	onMain(func() {
		for _, mm := range GetMonitors() {
			if mm.data == m.data {
				found = true
			}
		}
	})
	if !found {
		t.Fatal("CurrentMonitor returned a monitor not in GetMonitors")
	}
}

func TestFramebufferMatchesContentScale(t *testing.T) {
	needDisplay(t)
	w := newTestWindow(t, true, nil)
	pump(func() bool { present(w)(); return w.CurrentMonitor() != nil }, 2*time.Second)
	pump(func() bool { present(w)(); return false }, 200*time.Millisecond)
	onMain(func() {
		ww, wh := w.GetSize()
		fw, fh := w.GetFramebufferSize()
		sx, sy := w.GetContentScale()
		if sx <= 0 || sx != sy {
			t.Fatalf("content scale %v,%v", sx, sy)
		}
		t.Logf("window %dx%d framebuffer %dx%d scale %v", ww, wh, fw, fh, sx)
		s120 := int(sx*120 + 0.5)
		if fw != scaledSize(ww, s120) || fh != scaledSize(wh, s120) {
			t.Fatalf("framebuffer %dx%d does not match %dx%d at scale %v", fw, fh, ww, wh, sx)
		}
	})
}

func TestStandardCursorsAllShapes(t *testing.T) {
	needDisplay(t)
	w := newTestWindow(t, false, nil)
	shapes := []StandardCursor{
		ArrowCursor, IBeamCursor, CrosshairCursor, HandCursor, HResizeCursor, VResizeCursor,
		ResizeNWSECursor, ResizeNESWCursor, ResizeAllCursor, NotAllowedCursor,
	}
	onMain(func() {
		for _, s := range shapes {
			c := CreateStandardCursor(s)
			if c == nil || c.data == nil {
				t.Fatalf("CreateStandardCursor(%#x) failed", s)
			}
			w.SetCursor(c)
			w.SetCursor(nil)
			c.Destroy()
		}
	})
}

func TestTransparentFramebufferHonoured(t *testing.T) {
	needDisplay(t)
	var w *Window
	var err error
	onMain(func() {
		DefaultWindowHints()
		WindowHint(Visible, False)
		WindowHint(TransparentFramebuffer, True)
		w, err = CreateWindow(64, 64, "raven-transparent", nil, nil)
	})
	if err != nil {
		t.Skipf("no GL context available: %v", err)
	}
	defer onMain(w.Destroy)
	onMain(func() {
		if w.GetAttrib(TransparentFramebuffer) != True {
			t.Fatal("TransparentFramebuffer hint not honoured")
		}
	})
}

func TestIMECursorRectStored(t *testing.T) {
	needDisplay(t)
	w := newTestWindow(t, false, nil)
	onMain(func() {
		w.SetIMECursorRect(10, 20, 8, 16)
		x, y, cw, ch := imeCursorRect(w)
		if x != 10 || y != 20 || cw != 8 || ch != 16 {
			t.Fatalf("IME rect = %d,%d %dx%d", x, y, cw, ch)
		}
	})
}

// Against the live keymap, whatever it is: a letter key's Shift character
// is its capital, and out-of-range scancodes fail cleanly.
func TestScancodeSymbolsLiveKeymap(t *testing.T) {
	needDisplay(t)
	var syms KeySymbols
	var ok, badOK bool
	onMain(func() {
		syms, ok = GetScancodeSymbols(GetKeyScancode(KeyA))
		_, badOK = GetScancodeSymbols(-1)
	})
	if badOK {
		t.Error("scancode -1 reported symbols")
	}
	if !ok {
		t.Skip("no keymap received yet")
	}
	if syms.Keysym == 0 {
		t.Fatalf("KeyA has no keysym: %+v", syms)
	}
	if unicode.IsLetter(syms.Rune) && syms.Shifted != unicode.ToUpper(syms.Rune) {
		t.Errorf("KeyA = %+v, want Shifted = upper(Rune)", syms)
	}
}
