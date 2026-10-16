package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	dir := t.TempDir()
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
