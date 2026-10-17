package main

import (
	"bytes"
	"image"
	"image/png"
	"math/bits"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/javanhut/RavenTerminal/src/keybindings"
	"github.com/javanhut/RavenTerminal/src/tab"
)

func TestParseIPC(t *testing.T) {
	ok := []struct {
		req  ipcRequest
		want ipcCommand
	}{
		{ipcRequest{Cmd: "ls"}, ipcCommand{Name: "ls"}},
		{ipcRequest{Cmd: "split", Args: []string{"v"}}, ipcCommand{Name: "split", Dir: "v"}},
		{ipcRequest{Cmd: "split", Args: []string{"--id", "4", "h"}}, ipcCommand{Name: "split", Dir: "h", ID: 4}},
		{ipcRequest{Cmd: "focus-pane", Args: []string{"left"}}, ipcCommand{Name: "focus-pane", Dir: "left"}},
		{ipcRequest{Cmd: "focus-pane", Args: []string{"7"}}, ipcCommand{Name: "focus-pane", ID: 7}},
		{ipcRequest{Cmd: "get-text", Args: []string{"--extent", "last-output"}}, ipcCommand{Name: "get-text", Extent: "last-output"}},
		{ipcRequest{Cmd: "get-text"}, ipcCommand{Name: "get-text", Extent: "screen"}},
		{ipcRequest{Cmd: "send-text", Args: []string{"--id", "3", `hi\n`, `x\e`}}, ipcCommand{Name: "send-text", ID: 3, Text: "hi\n x\x1b"}},
		{ipcRequest{Cmd: "set-title", Args: []string{"my", "title"}}, ipcCommand{Name: "set-title", Text: "my title"}},
		{ipcRequest{Cmd: "new-tab", Args: []string{"--cwd", "/tmp", "htop", "-d", "5"}}, ipcCommand{Name: "new-tab", Cwd: "/tmp", Argv: []string{"htop", "-d", "5"}}},
		{ipcRequest{Cmd: "close-pane"}, ipcCommand{Name: "close-pane"}},
		{ipcRequest{Cmd: "get-user-vars"}, ipcCommand{Name: "get-user-vars"}},
		{ipcRequest{Cmd: "new-window"}, ipcCommand{Name: "new-window"}},
	}
	for _, c := range ok {
		got, err := parseIPC(c.req)
		if err != nil {
			t.Errorf("%v: %v", c.req, err)
			continue
		}
		if got.Name != c.want.Name || got.Dir != c.want.Dir || got.ID != c.want.ID || got.Extent != c.want.Extent ||
			got.Text != c.want.Text || got.Cwd != c.want.Cwd || strings.Join(got.Argv, " ") != strings.Join(c.want.Argv, " ") {
			t.Errorf("%v = %+v, want %+v", c.req, got, c.want)
		}
	}
	for _, bad := range []ipcRequest{
		{Cmd: "bogus"}, {Cmd: "split", Args: []string{"x"}}, {Cmd: "split"}, {Cmd: "focus-pane"},
		{Cmd: "get-text", Args: []string{"--extent", "nope"}}, {Cmd: "set-title"}, {Cmd: "send-text", Args: []string{"--id", "z", "a"}},
	} {
		if _, err := parseIPC(bad); err == nil {
			t.Errorf("%v must be rejected", bad)
		}
	}
}

// socketTempDir is a temp dir short enough to hold a unix socket: macOS's
// per-test t.TempDir paths overflow its 104-byte socket path limit.
func socketTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func uxIsolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func mustIPC(t *testing.T, apps []*App, cmd string, args ...string) ipcResponse {
	t.Helper()
	c, err := parseIPC(ipcRequest{Cmd: cmd, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	resp := dispatchIPC(apps, c)
	if !resp.OK {
		t.Fatalf("%s %v: %s", cmd, args, resp.Error)
	}
	return resp
}

func waitText(t *testing.T, apps []*App, want string, args ...string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := mustIPC(t, apps, "get-text", args...).Data.(string)
		if strings.Contains(got, want) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("text never contained %q:\n%s", want, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestIPCDispatch(t *testing.T) {
	uxIsolateHome(t)
	tm, err := tab.NewTabManagerIn(80, 24, "", []string{"cat"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tm.CloseAll)
	apps := []*App{{tabManager: tm}}

	wins := mustIPC(t, apps, "ls").Data.([]ipcWindow)
	if len(wins) != 1 || len(wins[0].Tabs) != 1 || len(wins[0].Tabs[0].Panes) != 1 || !wins[0].Tabs[0].Panes[0].Active {
		t.Fatalf("ls = %+v", wins)
	}
	firstID := wins[0].Tabs[0].Panes[0].ID

	mustIPC(t, apps, "send-text", `hello-ipc\r`)
	waitText(t, apps, "hello-ipc")
	waitText(t, apps, "hello-ipc", "--extent", "all")

	newID := mustIPC(t, apps, "split", "v").Data.(int)
	if newID == firstID {
		t.Fatal("split must report the new pane's id")
	}
	if n := len(mustIPC(t, apps, "ls").Data.([]ipcWindow)[0].Tabs[0].Panes); n != 2 {
		t.Fatalf("after split: %d panes", n)
	}
	mustIPC(t, apps, "focus-pane", "left")
	if tm.ActiveTab().GetActivePane() != ipcPaneByID(apps, firstID) {
		t.Fatal("focus-pane left did not focus the first pane")
	}
	c, _ := parseIPC(ipcRequest{Cmd: "focus-pane", Args: []string{"left"}})
	if dispatchIPC(apps, c).OK {
		t.Fatal("focus-pane with no neighbour must fail")
	}
	mustIPC(t, apps, "focus-pane", itoa(newID))
	if tm.ActiveTab().GetActivePane() != ipcPaneByID(apps, newID) {
		t.Fatal("focus-pane by id failed")
	}
	mustIPC(t, apps, "send-text", "--id", itoa(firstID), `to-first\r`)
	waitText(t, apps, "to-first", "--id", itoa(firstID))

	mustIPC(t, apps, "set-title", "Build", "box")
	if tm.ActiveTab().Title() != "Build box" {
		t.Fatalf("title = %q", tm.ActiveTab().Title())
	}
	if _, ok := mustIPC(t, apps, "get-user-vars").Data.(map[string]string); !ok {
		t.Fatal("get-user-vars must return a map")
	}
	mustIPC(t, apps, "close-pane")
	if tm.ActiveTab().PaneCount() != 1 {
		t.Fatal("close-pane did not close")
	}
	mustIPC(t, apps, "new-tab", "sh", "-c", "echo from-new-tab; sleep 5")
	if tm.TabCount() != 2 {
		t.Fatal("new-tab did not open a tab")
	}
	waitText(t, apps, "from-new-tab")
}

func TestIPCSocketRoundTrip(t *testing.T) {
	dir := socketTempDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("RAVEN_TERMINAL_SOCKET", "")
	path, stop, err := startIPCServerWith(func(req ipcRequest) ipcResponse {
		if req.Cmd == "fail" {
			return ipcResponse{Error: "boom"}
		}
		return ipcResponse{OK: true, Data: req.Cmd + ":" + strings.Join(req.Args, ",")}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if filepath.Dir(path) != filepath.Join(dir, "raven-terminal") || filepath.Base(path) != itoa(os.Getpid())+".sock" {
		t.Fatalf("socket path = %s", path)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("socket perm: %v %v", err, st)
	}
	if os.Getenv("RAVEN_TERMINAL_SOCKET") != path {
		t.Fatal("RAVEN_TERMINAL_SOCKET not exported")
	}
	var out, errb bytes.Buffer
	if code := ipcClientMain([]string{"echo", "a", "b"}, &out, &errb); code != 0 || out.String() != "echo:a,b\n" {
		t.Fatalf("client code %d out %q err %q", code, out.String(), errb.String())
	}
	out.Reset()
	if code := ipcClientMain([]string{"fail"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "boom") {
		t.Fatalf("error reply: code %d err %q", code, errb.String())
	}
	if code := ipcClientMain(nil, &out, &errb); code != 2 {
		t.Fatal("no command must be a usage error")
	}
	stop()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("stop must remove the socket")
	}
}

// leaderMods is what Leader stands for on this platform: Cmd on macOS,
// Ctrl+Shift (Ctrl+Alt once Shift is in the chord) elsewhere.
func leaderMods(shift bool) glfw.ModifierKey {
	switch {
	case runtime.GOOS == "darwin" && shift:
		return glfw.ModSuper | glfw.ModShift
	case runtime.GOOS == "darwin":
		return glfw.ModSuper
	case shift:
		return glfw.ModControl | glfw.ModShift | glfw.ModAlt
	}
	return glfw.ModControl | glfw.ModShift
}

func TestParseChord(t *testing.T) {
	ok := []struct {
		in   string
		key  glfw.Key
		mods glfw.ModifierKey
	}{
		{"ctrl+shift+space", glfw.KeySpace, glfw.ModControl | glfw.ModShift},
		{"escape", glfw.KeyEscape, 0},
		{"Esc", glfw.KeyEscape, 0},
		{"j", glfw.KeyJ, 0},
		{"J", glfw.KeyJ, 0},
		{"Leader+H", glfw.KeyH, leaderMods(false)},
		{"Leader+Shift+Tab", glfw.KeyTab, leaderMods(true)},
		{"Leader+=", glfw.KeyEqual, leaderMods(false)},
		{"Leader+-", glfw.KeyMinus, leaderMods(false)},
		{"Super+Shift+]", glfw.KeyRightBracket, glfw.ModSuper | glfw.ModShift},
		{"shift+page_up", glfw.KeyPageUp, glfw.ModShift},
		{"F11", glfw.KeyF11, 0},
		{"alt+enter", glfw.KeyEnter, glfw.ModAlt},
		{"ctrl", glfw.KeyLeftControl, 0},
		{"3", glfw.Key3, 0},
	}
	for _, c := range ok {
		got, err := parseChord(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got.Key != c.key || got.Mods != c.mods {
			t.Errorf("%q = %+v, want key %v mods %v", c.in, got, c.key, c.mods)
		}
	}
	for _, bad := range []string{"", "ctrl+", "hyper+x", "nosuchkey", "ctrl+a+b", "+"} {
		if _, err := parseChord(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestChordSteps(t *testing.T) {
	ch := func(s string) keyChord {
		c, err := parseChord(s)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	cs := glfw.ModControl | glfw.ModShift
	want := []keyStep{
		{Key: glfw.KeyLeftControl, Action: glfw.Press},
		{Key: glfw.KeyLeftShift, Action: glfw.Press, Mods: glfw.ModControl},
		{Key: glfw.KeySpace, Action: glfw.Press, Mods: cs},
		{Key: glfw.KeySpace, Action: glfw.Release, Mods: cs},
		{Key: glfw.KeyLeftShift, Action: glfw.Release, Mods: cs},
		{Key: glfw.KeyLeftControl, Action: glfw.Release, Mods: glfw.ModControl},
	}
	if got := ch("ctrl+shift+space").steps(); !slices.Equal(got, want) {
		t.Errorf("ctrl+shift+space steps:\n got %+v\nwant %+v", got, want)
	}
	want = []keyStep{{Key: glfw.KeyJ, Action: glfw.Press}, {Char: 'j'}, {Key: glfw.KeyJ, Action: glfw.Release}}
	if got := ch("j").steps(); !slices.Equal(got, want) {
		t.Errorf("j steps: %+v", got)
	}
	chars := func(s string) string {
		var b strings.Builder
		for _, st := range ch(s).steps() {
			if st.Char != 0 {
				b.WriteRune(st.Char)
			}
		}
		return b.String()
	}
	// Cmd chords type nothing on macOS; elsewhere GLFW still delivers the char.
	superT := "t"
	if runtime.GOOS == "darwin" {
		superT = ""
	}
	for in, out := range map[string]string{"shift+1": "!", "shift+j": "J", "alt+x": "x", "ctrl+c": "", "space": " ",
		"escape": "", "shift+/": "?", "super+t": superT} {
		if got := chars(in); got != out {
			t.Errorf("%s types %q, want %q", in, got, out)
		}
	}
}

// Every palette action must resolve to a chord that really triggers it.
func TestIPCActionChords(t *testing.T) {
	for _, info := range ipcActions() {
		c, err := actionChord(info)
		if err != nil {
			t.Errorf("%s: %v", info.Name, err)
			continue
		}
		if got := keybindings.TranslateKey(c.Key, c.Mods, false).Action; got != info.Action {
			t.Errorf("%s: chord %q gives action %v, want %v", info.Name, info.Chord, got, info.Action)
		}
	}
	for name, want := range map[string]keybindings.KeyAction{
		"hints": keybindings.ActionHints, "copy mode": keybindings.ActionCopyMode, "copy-mode": keybindings.ActionCopyMode,
		"palette": keybindings.ActionCommandPalette, "zoom": keybindings.ActionTogglePaneZoom,
		"broadcast": keybindings.ActionToggleBroadcast, "Pane resize mode": keybindings.ActionToggleResizeMode,
		"find in scrollback": keybindings.ActionFindInScrollback,
	} {
		info, err := lookupAction(name)
		if err != nil || info.Action != want {
			t.Errorf("lookupAction(%q) = %v, %v; want %v", name, info.Action, err, want)
		}
	}
	for _, bad := range []string{"copy", "nope", ""} {
		if _, err := lookupAction(bad); err == nil {
			t.Errorf("lookupAction(%q) must fail", bad)
		}
	}
}

func TestParseIPCHarness(t *testing.T) {
	c, err := parseIPC(ipcRequest{Cmd: "screenshot", Args: []string{"--id", "2", "/tmp/x.png"}})
	if err != nil || c.Path != "/tmp/x.png" || c.ID != 2 {
		t.Fatalf("screenshot = %+v, %v", c, err)
	}
	c, err = parseIPC(ipcRequest{Cmd: "send-key", Args: []string{"ctrl+shift+space"}})
	if err != nil || len(c.Steps) != 6 || c.Steps[2].Key != glfw.KeySpace {
		t.Fatalf("send-key = %+v, %v", c, err)
	}
	c, err = parseIPC(ipcRequest{Cmd: "action", Args: []string{"copy", "mode"}})
	if err != nil || len(c.Steps) == 0 {
		t.Fatalf("action copy mode = %+v, %v", c, err)
	}
	if k := c.Steps[len(c.Steps)/2-1]; keybindings.TranslateKey(k.Key, k.Mods, false).Action != keybindings.ActionCopyMode {
		t.Fatalf("action copy mode presses %+v", k)
	}
	if _, err := parseIPC(ipcRequest{Cmd: "state"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []ipcRequest{
		{Cmd: "screenshot"}, {Cmd: "screenshot", Args: []string{"rel.png"}}, {Cmd: "screenshot", Args: []string{"/tmp/x.jpg"}},
		{Cmd: "screenshot", Args: []string{"/a.png", "/b.png"}}, {Cmd: "send-key"}, {Cmd: "send-key", Args: []string{"ctrl+nope"}},
		{Cmd: "action"}, {Cmd: "action", Args: []string{"copy"}}, {Cmd: "state", Args: []string{"x"}},
	} {
		if _, err := parseIPC(bad); err == nil {
			t.Errorf("%v must be rejected", bad)
		}
	}
}

func TestFrameImage(t *testing.T) {
	// 3x2 framebuffer, bottom row first as glReadPixels returns it.
	px := func(v byte) []byte { return []byte{v, v, v, 7} }
	var pix []byte
	for _, v := range []byte{10, 11, 12, 20, 21, 22} {
		pix = append(pix, px(v)...)
	}
	img := frameImage(pix, 3, 2, image.Rect(0, 0, 3, 2))
	if img.Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatalf("bounds %v", img.Bounds())
	}
	if c := img.NRGBAAt(0, 0); c.R != 20 || c.A != 255 {
		t.Fatalf("top-left = %v, want the last row read, opaque", c)
	}
	if c := img.NRGBAAt(2, 1); c.R != 12 {
		t.Fatalf("bottom-right = %v", c)
	}
	img = frameImage(pix, 3, 2, image.Rect(1, 1, 3, 2))
	if img.Bounds() != image.Rect(0, 0, 2, 1) || img.NRGBAAt(0, 0).R != 11 || img.NRGBAAt(1, 0).R != 12 {
		t.Fatalf("crop = %v %v", img.Bounds(), img.Pix)
	}
	if img := frameImage(pix, 3, 2, image.Rect(-5, -5, 50, 50)); img.Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatalf("crop must clamp to the frame, got %v", img.Bounds())
	}
}

func TestIPCHarnessDispatch(t *testing.T) {
	uxIsolateHome(t)
	tm, err := tab.NewTabManagerIn(80, 24, "", []string{"cat"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tm.CloseAll)
	a := &App{tabManager: tm}
	apps := []*App{a}
	id := mustIPC(t, apps, "ls").Data.([]ipcWindow)[0].Tabs[0].Panes[0].ID

	st := mustIPC(t, apps, "state").Data.(ipcState)
	if st.FocusedPane != id || st.Modes != (ipcModes{}) || st.Pointer != "default" {
		t.Fatalf("idle state = %+v", st)
	}
	a.ux.copy = &copyMode{}
	a.resizeMode = true
	tm.ActiveTab().ToggleBroadcast()
	ipcNotePreedit(a, "nihon", 3)
	a.pointerCSS = "text"
	st = mustIPC(t, apps, "state").Data.(ipcState)
	if !st.Modes.Copy || !st.Modes.Resize || !st.Modes.Broadcast || st.Modes.Hints || st.Modes.Palette {
		t.Fatalf("modes = %+v", st.Modes)
	}
	if st.Preedit.Text != "nihon" || st.Preedit.Caret != 3 || st.Pointer != "text" {
		t.Fatalf("state = %+v", st)
	}

	var sent []keyStep
	var sentTo *App
	old := ipcInjectKeys
	ipcInjectKeys = func(app *App, steps []keyStep) { sentTo, sent = app, append(sent, steps...) }
	t.Cleanup(func() { ipcInjectKeys = old })
	mustIPC(t, apps, "send-key", "j")
	if sentTo != a || len(sent) != 3 || sent[1].Char != 'j' {
		t.Fatalf("send-key injected %+v into %p", sent, sentTo)
	}
	sent = nil
	mustIPC(t, apps, "action", "hints")
	held := bits.OnesCount(uint(leaderMods(false)))
	if len(sent) != 2*held+2 || keybindings.TranslateKey(sent[held].Key, sent[held].Mods, false).Action != keybindings.ActionHints {
		t.Fatalf("action hints injected %+v", sent)
	}

	dir := t.TempDir()
	call := ipcCall{reply: make(chan ipcResponse, 1)}
	call.cmd, _ = parseIPC(ipcRequest{Cmd: "screenshot", Args: []string{filepath.Join(dir, "full.png")}})
	if err := queueScreenshot(apps, call); err != nil {
		t.Fatal(err)
	}
	pcall := ipcCall{reply: make(chan ipcResponse, 1)}
	pcall.cmd, _ = parseIPC(ipcRequest{Cmd: "screenshot", Args: []string{"--id", itoa(id), filepath.Join(dir, "pane.png")}})
	if err := queueScreenshot(apps, pcall); err != nil {
		t.Fatal(err)
	}
	if !a.eventRedraw {
		t.Fatal("a queued screenshot must force a frame")
	}
	bad := ipcCall{reply: make(chan ipcResponse, 1)}
	bad.cmd, _ = parseIPC(ipcRequest{Cmd: "screenshot", Args: []string{"--id", "999", filepath.Join(dir, "x.png")}})
	if queueScreenshot(apps, bad) == nil {
		t.Fatal("screenshot of an unknown pane must fail")
	}

	pix := make([]byte, 4*4*2)
	for i := range pix {
		pix[i] = byte(i)
	}
	a.deliverShots(pix, 4, 2, func(p *tab.Pane) (image.Rectangle, bool) {
		if p != ipcPaneByID(apps, id) {
			t.Errorf("rect asked for the wrong pane")
		}
		return image.Rect(2, 0, 4, 1), true
	})
	for name, want := range map[string]image.Rectangle{"full.png": image.Rect(0, 0, 4, 2), "pane.png": image.Rect(0, 0, 2, 1)} {
		c := call
		if name == "pane.png" {
			c = pcall
		}
		select {
		case r := <-c.reply:
			if !r.OK || r.Data != filepath.Join(dir, name) {
				t.Fatalf("%s reply %+v", name, r)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: no reply", name)
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil || img.Bounds() != want {
			t.Fatalf("%s: %v bounds %v", name, err, img.Bounds())
		}
	}
	if len(ipcWinFor(a).shots) != 0 {
		t.Fatal("delivered shots must be cleared")
	}
}

func TestIPCClientAbsolutizesScreenshot(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", socketTempDir(t))
	t.Setenv("RAVEN_TERMINAL_SOCKET", "")
	_, stop, err := startIPCServerWith(func(req ipcRequest) ipcResponse {
		return ipcResponse{OK: true, Data: strings.Join(req.Args, ",")}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	wd, _ := os.Getwd()
	var out, errb bytes.Buffer
	if code := ipcClientMain([]string{"screenshot", "--id", "3", "shot.png"}, &out, &errb); code != 0 ||
		out.String() != "--id,3,"+filepath.Join(wd, "shot.png")+"\n" {
		t.Fatalf("code %d out %q err %q", code, out.String(), errb.String())
	}
}

func TestParseIPCSendMouse(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want ipcMouse
	}{
		{[]string{"10", "20.5"}, ipcMouse{X: 10, Y: 20.5}},
		{[]string{"10", "20", "left"}, ipcMouse{X: 10, Y: 20, Button: glfw.MouseButtonLeft, Press: true, Release: true}},
		{[]string{"1", "2", "middle"}, ipcMouse{X: 1, Y: 2, Button: glfw.MouseButtonMiddle, Press: true, Release: true}},
		{[]string{"1", "2", "right:press"}, ipcMouse{X: 1, Y: 2, Button: glfw.MouseButtonRight, Press: true}},
		{[]string{"1", "2", "left:release"}, ipcMouse{X: 1, Y: 2, Button: glfw.MouseButtonLeft, Release: true}},
		{[]string{"1", "2", "ctrl+left"}, ipcMouse{X: 1, Y: 2, Button: glfw.MouseButtonLeft, Press: true, Release: true, Mods: glfw.ModControl}},
		{[]string{"1", "2", "Shift+Alt+right:press"}, ipcMouse{X: 1, Y: 2, Button: glfw.MouseButtonRight, Press: true, Mods: glfw.ModShift | glfw.ModAlt}},
	} {
		c, err := parseIPC(ipcRequest{Cmd: "send-mouse", Args: tc.args})
		if err != nil || c.Mouse != tc.want {
			t.Errorf("send-mouse %v = %+v, %v; want %+v", tc.args, c.Mouse, err, tc.want)
		}
	}
	for _, bad := range [][]string{nil, {"1"}, {"x", "2"}, {"1", "2", "thumb"}, {"1", "2", "left:hold"}, {"1", "2", "left", "x"}, {"-1", "2"}, {"1", "2", "hyper+left"}, {"1", "2", "ctrl+"}, {"1", "2", "ctrl+shift"}} {
		if _, err := parseIPC(ipcRequest{Cmd: "send-mouse", Args: bad}); err == nil {
			t.Errorf("send-mouse %v must be rejected", bad)
		}
	}
}

func TestIPCSendMouseDispatch(t *testing.T) {
	uxIsolateHome(t)
	tm, err := tab.NewTabManagerIn(80, 24, "", []string{"cat"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tm.CloseAll)
	a := &App{tabManager: tm}
	var got []ipcMouse
	var to *App
	old := ipcInjectMouse
	ipcInjectMouse = func(app *App, m ipcMouse) { to, got = app, append(got, m) }
	t.Cleanup(func() { ipcInjectMouse = old })
	mustIPC(t, []*App{a}, "send-mouse", "30", "40", "left")
	if to != a || len(got) != 1 || got[0].X != 30 || got[0].Button != glfw.MouseButtonLeft || !a.eventRedraw {
		t.Fatalf("send-mouse injected %+v into %p (redraw %v)", got, to, a.eventRedraw)
	}
}

// Button and wheel handlers must see the position the cursor callback was
// last given, not ask GLFW again, so injected motion and presses agree.
func TestCursorPosFollowsCallback(t *testing.T) {
	a := &App{lastCursorX: 12.5, lastCursorY: 99, haveCursorPos: true}
	if x, y := a.cursorPos(); x != 12.5 || y != 99 {
		t.Fatalf("cursorPos = %v,%v, want the last callback position", x, y)
	}
}

// Injected modifiers count as held for the code that polls key state, so a
// send-mouse ctrl+ or shift+ behaves like a physical chord.
func TestIPCModsCountAsHeld(t *testing.T) {
	a := &App{}
	if a.modDown(nil, glfw.ModControl) {
		t.Fatal("ctrl reported held with nothing pressed")
	}
	a.ipcMods = glfw.ModControl | glfw.ModShift
	if !a.modDown(nil, glfw.ModControl) || !a.modDown(nil, glfw.ModShift) || a.modDown(nil, glfw.ModAlt) {
		t.Fatalf("modDown ignores injected mods %v", a.ipcMods)
	}
}
