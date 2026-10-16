package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/javanhut/RavenTerminal/src/tab"
	"github.com/javanhut/RavenTerminal/src/window"
)

// IPC: one JSON object per line each way over a unix socket.
// Request {"cmd": "...", "args": [...]}; response {"ok": bool, "error": "...", "data": ...}.

const ipcSocketEnv = "RAVEN_TERMINAL_SOCKET"

type ipcRequest struct {
	Cmd  string   `json:"cmd"`
	Args []string `json:"args,omitempty"`
}

type ipcResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}

type ipcCommand struct {
	Name   string
	ID     int
	Dir    string
	Extent string
	Text   string
	Cwd    string
	Argv   []string
}

type ipcPane struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Cwd    string `json:"cwd"`
	Active bool   `json:"active"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
	Exited bool   `json:"exited"`
}

type ipcTab struct {
	Index     int       `json:"index"`
	Title     string    `json:"title"`
	Active    bool      `json:"active"`
	Zoomed    bool      `json:"zoomed"`
	Broadcast bool      `json:"broadcast"`
	Panes     []ipcPane `json:"panes"`
}

type ipcWindow struct {
	ID      int      `json:"id"`
	Focused bool     `json:"focused"`
	Tabs    []ipcTab `json:"tabs"`
}

func ipcUsage() string {
	return `usage: raven-terminal @ [--to SOCKET] <command> [args]
  ls                                   windows, tabs and panes as JSON
  new-tab [--cwd DIR] [CMD ARGS...]    open a tab (shell when no command)
  new-window                           open a window
  split [--id N] h|v                   split a pane: h stacked, v side by side
  focus-pane left|right|up|down|N      focus a neighbour or the pane with id N
  send-text [--id N] TEXT...           type text (\n \r \t \e \xHH escapes)
  get-text [--id N] [--extent screen|all|last-output|selection]
  set-title [--id N] TITLE...          fix the tab title ("" via set-title -)
  close-pane [--id N]
  get-user-vars [--id N]               OSC 1337 SetUserVar values
`
}

// unescapeIPC expands \n \r \t \e \\ \xHH and \0.
func unescapeIPC(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'e':
			b.WriteByte(0x1b)
		case '0':
			b.WriteByte(0)
		case 'x':
			if i+2 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
					b.WriteByte(byte(v))
					i += 2
					continue
				}
			}
			b.WriteString(`\x`)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func parseIPC(req ipcRequest) (ipcCommand, error) {
	c := ipcCommand{Name: req.Cmd}
	flags := map[string]bool{}
	switch req.Cmd {
	case "ls", "new-window":
	case "new-tab":
		flags["--cwd"] = true
	case "split", "send-text", "set-title", "close-pane", "get-user-vars":
		flags["--id"] = true
	case "get-text":
		flags["--id"], flags["--extent"] = true, true
		c.Extent = "screen"
	case "focus-pane":
	default:
		return c, fmt.Errorf("unknown command %q", req.Cmd)
	}
	args := req.Args
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") || len(pos) > 0 {
			pos = append(pos, args[i:]...)
			break
		}
		if !flags[a] {
			return c, fmt.Errorf("%s: unknown option %s", req.Cmd, a)
		}
		if i+1 >= len(args) {
			return c, fmt.Errorf("%s: %s needs a value", req.Cmd, a)
		}
		i++
		switch a {
		case "--id":
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return c, fmt.Errorf("%s: bad pane id %q", req.Cmd, args[i])
			}
			c.ID = n
		case "--extent":
			switch args[i] {
			case "screen", "all", "last-output", "selection":
				c.Extent = args[i]
			default:
				return c, fmt.Errorf("get-text: unknown extent %q", args[i])
			}
		case "--cwd":
			c.Cwd = args[i]
		}
	}
	switch req.Cmd {
	case "ls", "new-window", "close-pane", "get-user-vars", "get-text":
		if len(pos) > 0 {
			return c, fmt.Errorf("%s takes no arguments", req.Cmd)
		}
	case "new-tab":
		c.Argv = pos
	case "split":
		if len(pos) != 1 {
			return c, errors.New("split needs h or v")
		}
		switch pos[0] {
		case "h", "horizontal":
			c.Dir = "h"
		case "v", "vertical":
			c.Dir = "v"
		default:
			return c, fmt.Errorf("split: %q is not h or v", pos[0])
		}
	case "focus-pane":
		if len(pos) != 1 {
			return c, errors.New("focus-pane needs a direction or pane id")
		}
		switch pos[0] {
		case "left", "right", "up", "down":
			c.Dir = pos[0]
		default:
			n, err := strconv.Atoi(pos[0])
			if err != nil || n <= 0 {
				return c, fmt.Errorf("focus-pane: %q is not a direction or id", pos[0])
			}
			c.ID = n
		}
	case "send-text":
		if len(pos) == 0 {
			return c, errors.New("send-text needs text")
		}
		c.Text = unescapeIPC(strings.Join(pos, " "))
	case "set-title":
		if len(pos) == 0 {
			return c, errors.New("set-title needs a title")
		}
		if c.Text = strings.Join(pos, " "); c.Text == "-" {
			c.Text = ""
		}
	}
	return c, nil
}

// Pane ids are process-wide and stable for a pane's life. Main thread only.
var (
	ipcIDs    = map[*tab.Pane]int{}
	ipcNextID = 1
)

type ipcLoc struct {
	app *App
	tab int
	t   *tab.Tab
}

// ipcWalk visits every live pane, assigning ids and forgetting dead ones.
func ipcWalk(apps []*App, fn func(id int, p *tab.Pane, loc ipcLoc)) {
	live := map[*tab.Pane]bool{}
	for _, a := range apps {
		for ti, t := range a.tabManager.GetTabs() {
			for _, p := range t.GetPanes() {
				live[p] = true
				id, ok := ipcIDs[p]
				if !ok {
					id = ipcNextID
					ipcNextID++
					ipcIDs[p] = id
				}
				if fn != nil {
					fn(id, p, ipcLoc{a, ti, t})
				}
			}
		}
	}
	for p := range ipcIDs {
		if !live[p] {
			delete(ipcIDs, p)
		}
	}
}

func ipcPaneByID(apps []*App, id int) *tab.Pane {
	p, _ := ipcFind(apps, id)
	return p
}

func ipcFind(apps []*App, id int) (*tab.Pane, ipcLoc) {
	var found *tab.Pane
	var at ipcLoc
	ipcWalk(apps, func(pid int, p *tab.Pane, loc ipcLoc) {
		if pid == id {
			found, at = p, loc
		}
	})
	return found, at
}

func ipcID(apps []*App, p *tab.Pane) int {
	ipcWalk(apps, nil)
	return ipcIDs[p]
}

func focusedApp(apps []*App) *App {
	for _, a := range apps {
		if a.windowFocused {
			return a
		}
	}
	return apps[0]
}

// ipcTarget resolves --id, or the focused window's active pane.
func ipcTarget(apps []*App, c ipcCommand) (*tab.Pane, ipcLoc, error) {
	if c.ID != 0 {
		p, loc := ipcFind(apps, c.ID)
		if p == nil {
			return nil, loc, fmt.Errorf("no pane with id %d", c.ID)
		}
		return p, loc, nil
	}
	a := focusedApp(apps)
	t := a.tabManager.ActiveTab()
	if t == nil || t.GetActivePane() == nil {
		return nil, ipcLoc{}, errors.New("no active pane")
	}
	return t.GetActivePane(), ipcLoc{a, a.tabManager.ActiveIndex(), t}, nil
}

func ipcErr(err error) ipcResponse { return ipcResponse{Error: err.Error()} }

// dispatchIPC runs a parsed command against the windows. Main thread only.
func dispatchIPC(apps []*App, c ipcCommand) ipcResponse {
	if len(apps) == 0 {
		return ipcResponse{Error: "no windows"}
	}
	if c.Name == "ls" {
		var wins []ipcWindow
		ipcWalk(apps, nil)
		for wi, a := range apps {
			w := ipcWindow{ID: wi + 1, Focused: a.windowFocused}
			for ti, t := range a.tabManager.GetTabs() {
				it := ipcTab{Index: ti + 1, Title: t.Title(), Active: ti == a.tabManager.ActiveIndex(),
					Zoomed: t.Zoomed(), Broadcast: t.Broadcasting()}
				active := t.GetActivePane()
				for _, p := range t.GetPanes() {
					g := p.Terminal.GetGrid()
					it.Panes = append(it.Panes, ipcPane{ID: ipcIDs[p], Title: p.Terminal.Title(), Cwd: p.CurrentDir(),
						Active: p == active, Cols: g.Cols, Rows: g.Rows, Exited: p.HasExited()})
				}
				w.Tabs = append(w.Tabs, it)
			}
			wins = append(wins, w)
		}
		return ipcResponse{OK: true, Data: wins}
	}
	if c.Name == "new-tab" {
		a := focusedApp(apps)
		n := a.tabManager.TabCount()
		if err := a.tabManager.NewTabWithCommand(c.Argv, c.Cwd); err != nil {
			return ipcErr(err)
		}
		if a.tabManager.TabCount() == n {
			return ipcResponse{Error: "tab limit reached"}
		}
		return ipcResponse{OK: true, Data: ipcID(apps, a.tabManager.ActiveTab().GetActivePane())}
	}
	if c.Name == "focus-pane" && c.Dir != "" {
		t := focusedApp(apps).tabManager.ActiveTab()
		if t == nil || !t.FocusDirection(c.Dir) {
			return ipcResponse{Error: "no pane " + c.Dir}
		}
		return ipcResponse{OK: true, Data: ipcID(apps, t.GetActivePane())}
	}

	p, loc, err := ipcTarget(apps, c)
	if err != nil {
		return ipcErr(err)
	}
	switch c.Name {
	case "focus-pane":
		loc.app.tabManager.SelectTab(loc.tab)
		loc.t.SetActivePane(p)
		if loc.app.win != nil {
			loc.app.win.GLFW().Focus()
		}
		return ipcResponse{OK: true, Data: c.ID}
	case "split":
		loc.t.SetActivePane(p)
		if c.Dir == "h" {
			err = loc.t.SplitHorizontal()
		} else {
			err = loc.t.SplitVertical()
		}
		if err != nil {
			return ipcErr(err)
		}
		np := loc.t.GetActivePane()
		if np == p {
			return ipcResponse{Error: "pane limit reached"}
		}
		return ipcResponse{OK: true, Data: ipcID(apps, np)}
	case "send-text":
		if err := p.Write([]byte(c.Text)); err != nil {
			return ipcErr(err)
		}
		p.Terminal.GetGrid().ResetScrollOffset()
		return ipcResponse{OK: true}
	case "get-text":
		g := p.Terminal.GetGrid()
		switch c.Extent {
		case "all":
			first, last := g.AbsBounds()
			return ipcResponse{OK: true, Data: g.TextRange(first, 0, last, g.Cols-1, false)}
		case "last-output":
			s, e, ok := g.LastOutputRange()
			if !ok {
				return ipcResponse{Error: "no command output (shell integration marks missing?)"}
			}
			return ipcResponse{OK: true, Data: g.TextRange(s, 0, e, g.Cols-1, false)}
		case "selection":
			return ipcResponse{OK: true, Data: g.SelectedText()}
		}
		top := g.ViewTopAbs()
		return ipcResponse{OK: true, Data: g.TextRange(top, 0, top+g.Rows-1, g.Cols-1, false)}
	case "set-title":
		loc.t.SetTitle(c.Text)
		return ipcResponse{OK: true}
	case "close-pane":
		if loc.t.PaneCount() > 1 {
			loc.t.SetActivePane(p)
			loc.t.ClosePane()
		} else {
			loc.app.tabManager.SelectTab(loc.tab)
			loc.app.tabManager.CloseCurrentTab()
		}
		return ipcResponse{OK: true}
	case "get-user-vars":
		vars := p.Terminal.UserVars()
		if vars == nil {
			vars = map[string]string{}
		}
		return ipcResponse{OK: true, Data: vars}
	}
	return ipcResponse{Error: "unsupported command " + c.Name}
}

type ipcCall struct {
	cmd   ipcCommand
	reply chan ipcResponse
}

var ipcCalls = make(chan ipcCall, 64)

// ipcMainHandler hands a request to the main loop and waits for its answer.
func ipcMainHandler(req ipcRequest) ipcResponse {
	c, err := parseIPC(req)
	if err != nil {
		return ipcErr(err)
	}
	call := ipcCall{cmd: c, reply: make(chan ipcResponse, 1)}
	select {
	case ipcCalls <- call:
	case <-time.After(5 * time.Second):
		return ipcResponse{Error: "terminal busy"}
	}
	window.PostEmptyEvent()
	select {
	case r := <-call.reply:
		return r
	case <-time.After(10 * time.Second):
		return ipcResponse{Error: "timed out"}
	}
}

// serveIPC answers queued requests on the main loop; new-window adds a window.
func serveIPC(apps []*App) []*App {
	for {
		select {
		case call := <-ipcCalls:
			if call.cmd.Name == "new-window" {
				a, err := newAppWith(func(cols, rows uint16) (*tab.TabManager, error) {
					return tab.NewTabManager(cols, rows)
				})
				if err != nil {
					call.reply <- ipcErr(err)
					continue
				}
				a.registerCallbacks()
				a.win.GLFW().Focus()
				apps = append(apps, a)
				call.reply <- ipcResponse{OK: true, Data: len(apps)}
				continue
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						call.reply <- ipcResponse{Error: fmt.Sprint("internal error: ", r)}
					}
				}()
				call.reply <- dispatchIPC(apps, call.cmd)
			}()
		default:
			return apps
		}
	}
}

func ipcSocketDir() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = filepath.Join(os.TempDir(), "raven-terminal-"+strconv.Itoa(os.Getuid()))
	}
	return filepath.Join(base, "raven-terminal")
}

// secureDir requires path to be a real directory owned by this user that no
// one else can write to; with fix our own directory is tightened to 0700.
func secureDir(path string, fix bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s: not a directory owned by this user", path)
	}
	if fix && fi.Mode().Perm()&0o077 != 0 {
		return os.Chmod(path, 0o700)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s: writable by other users", path)
	}
	return nil
}

// checkIPCDir verifies the socket directory and its parent before anything
// in them is trusted.
func checkIPCDir(dir string, fix bool) error {
	if err := secureDir(filepath.Dir(dir), false); err != nil {
		return err
	}
	return secureDir(dir, fix)
}

// startIPCServerWith listens on <runtime dir>/raven-terminal/<pid>.sock and
// exports its path to child processes.
func startIPCServerWith(handle func(ipcRequest) ipcResponse) (string, func(), error) {
	dir := ipcSocketDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	if err := checkIPCDir(dir, true); err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, strconv.Itoa(os.Getpid())+".sock")
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return "", nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return "", nil, err
	}
	os.Setenv(ipcSocketEnv, path)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveIPCConn(conn, handle)
		}
	}()
	var once sync.Once
	return path, func() {
		once.Do(func() {
			ln.Close()
			os.Remove(path)
		})
	}, nil
}

func serveIPCConn(conn net.Conn, handle func(ipcRequest) ipcResponse) {
	defer conn.Close()
	defer recoverLog("ipc connection")
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	enc := json.NewEncoder(conn)
	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		if !sc.Scan() {
			return
		}
		var req ipcRequest
		resp := ipcResponse{Error: "bad request"}
		if err := json.Unmarshal(sc.Bytes(), &req); err == nil {
			resp = handle(req)
		}
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

// findIPCSocket picks the newest socket that accepts a connection.
func findIPCSocket() string {
	dir := ipcSocketDir()
	if checkIPCDir(dir, false) != nil {
		return ""
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.sock"))
	paths = slices.DeleteFunc(paths, func(p string) bool {
		fi, err := os.Lstat(p)
		if err != nil {
			return true
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		return !ok || int(st.Uid) != os.Getuid()
	})
	sort.Slice(paths, func(i, j int) bool {
		a, _ := os.Stat(paths[i])
		b, _ := os.Stat(paths[j])
		return a != nil && b != nil && a.ModTime().After(b.ModTime())
	})
	for _, p := range paths {
		if c, err := net.DialTimeout("unix", p, time.Second); err == nil {
			c.Close()
			return p
		}
	}
	return ""
}

// ipcClientMain implements `raven-terminal @ <cmd> [args]`.
func ipcClientMain(args []string, stdout, stderr io.Writer) int {
	path := os.Getenv(ipcSocketEnv)
	if len(args) >= 2 && args[0] == "--to" {
		path, args = args[1], args[2:]
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stderr, ipcUsage())
		return 2
	}
	if path == "" {
		if path = findIPCSocket(); path == "" {
			fmt.Fprintln(stderr, "raven-terminal @: no running terminal found ($"+ipcSocketEnv+" unset)")
			return 1
		}
	}
	conn, err := net.DialTimeout("unix", path, 3*time.Second)
	if err != nil {
		fmt.Fprintf(stderr, "raven-terminal @: %v\n", err)
		return 1
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := json.NewEncoder(conn).Encode(ipcRequest{Cmd: args[0], Args: args[1:]}); err != nil {
		fmt.Fprintf(stderr, "raven-terminal @: %v\n", err)
		return 1
	}
	var resp ipcResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		fmt.Fprintf(stderr, "raven-terminal @: %v\n", err)
		return 1
	}
	if !resp.OK {
		fmt.Fprintf(stderr, "raven-terminal @ %s: %s\n", args[0], resp.Error)
		return 1
	}
	switch d := resp.Data.(type) {
	case nil:
	case string:
		fmt.Fprint(stdout, d)
		if !strings.HasSuffix(d, "\n") {
			fmt.Fprintln(stdout)
		}
	default:
		out, _ := json.MarshalIndent(d, "", "  ")
		fmt.Fprintf(stdout, "%s\n", out)
	}
	return 0
}

// startIPCServer starts the app's socket; failures only disable remote control.
func startIPCServer() func() {
	_, stop, err := startIPCServerWith(ipcMainHandler)
	if err != nil {
		log.Printf("remote control disabled: %v", err)
		return func() {}
	}
	return stop
}

func itoa(n int) string { return strconv.Itoa(n) }
