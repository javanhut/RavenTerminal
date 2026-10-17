package shell

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/javanhut/RavenTerminal/src/version"
)

func startCmd(t *testing.T, argv ...string) *PtySession {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p, err := NewPtySessionSized(80, 24, 640, 384, "", argv)
	if err != nil {
		t.Fatalf("start %v: %v", argv, err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func waitExited(t *testing.T, p *PtySession, d time.Duration) {
	t.Helper()
	select {
	case <-p.Exited():
	case <-time.After(d):
		t.Fatalf("process still alive after %v", d)
	}
}

// A child that never reads stdin must not stall the UI thread: Write queues
// and returns at once, even for a paste far larger than the PTY buffer.
func TestWriteNeverBlocks(t *testing.T) {
	p := startCmd(t, "sleep", "30")
	big := bytes.Repeat([]byte("x"), 1<<20)
	start := time.Now()
	for range 4 {
		if _, err := p.Write(big); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if el := time.Since(start); el > 50*time.Millisecond {
		t.Fatalf("Write blocked for %v", el)
	}
	start = time.Now()
	if err := p.Resize(100, 30, 800, 600); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if el := time.Since(start); el > 50*time.Millisecond {
		t.Fatalf("Resize blocked behind a stuck write for %v", el)
	}
	p.Close()
	select {
	case <-p.writerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("writer goroutine still stuck after Close")
	}
}

// Queued writes arrive in order at the child.
func TestWriteOrderPreserved(t *testing.T) {
	p := startCmd(t, "sh", "-c", "stty raw -echo; echo READY; head -c 6000 >/dev/null; read l; echo GOT:$l")
	var mu sync.Mutex
	var out bytes.Buffer
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			mu.Lock()
			out.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	waitFor := func(s string) {
		deadline := time.Now().Add(5 * time.Second)
		for {
			mu.Lock()
			ok := strings.Contains(out.String(), s)
			o := out.String()
			mu.Unlock()
			if ok {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %q, child output %q", s, o)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitFor("READY")
	payload := bytes.Repeat([]byte("a"), 6000)
	p.Write(payload[:3000])
	p.Write(payload[3000:])
	p.Write([]byte("hello\n"))
	waitFor("GOT:hello")
}

func TestWriteAfterExitDropped(t *testing.T) {
	p := startCmd(t, "true")
	waitExited(t, p, 3*time.Second)
	if n, err := p.Write([]byte("ls\n")); err != ErrExited || n != 0 {
		t.Fatalf("Write after exit = %d, %v; want 0, ErrExited", n, err)
	}
}

func TestExitCode(t *testing.T) {
	p := startCmd(t, "sh", "-c", "exit 7")
	waitExited(t, p, 3*time.Second)
	if c := p.ExitCode(); c != 7 {
		t.Fatalf("ExitCode = %d, want 7", c)
	}
}

func TestPixelWinsize(t *testing.T) {
	p := startCmd(t, "sleep", "30")
	ws, err := pty.GetsizeFull(p.pty)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Cols != 80 || ws.Rows != 24 || ws.X != 640 || ws.Y != 384 {
		t.Fatalf("initial winsize %+v, want 80x24 640x384px", ws)
	}
	if err := p.Resize(100, 30, 1000, 600); err != nil {
		t.Fatal(err)
	}
	cols, rows, pw, ph, err := p.Size()
	if err != nil || cols != 100 || rows != 30 || pw != 1000 || ph != 600 {
		t.Fatalf("Size = %d %d %d %d %v", cols, rows, pw, ph, err)
	}
}

// Close hangs up the process group and returns at once; a child that
// ignores SIGHUP is SIGKILLed shortly after.
func TestCloseKillsStubbornChild(t *testing.T) {
	p := startCmd(t, "sh", "-c", "trap '' HUP; while :; do sleep 1; done")
	time.Sleep(100 * time.Millisecond)
	pid := p.cmd.Process.Pid
	start := time.Now()
	p.Close()
	if el := time.Since(start); el > 50*time.Millisecond {
		t.Fatalf("Close blocked for %v", el)
	}
	waitExited(t, p, 3*time.Second)
	deadline := time.Now().Add(time.Second)
	for syscall.Kill(-pid, 0) != syscall.ESRCH {
		if time.Now().After(deadline) {
			t.Fatalf("process group %d still has members", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCloseHangsUpGroup(t *testing.T) {
	p := startCmd(t, "sh", "-c", "sleep 30 & sleep 30")
	time.Sleep(100 * time.Millisecond)
	pid := p.cmd.Process.Pid
	p.Close()
	waitExited(t, p, 900*time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(-pid, 0) != syscall.ESRCH {
		if time.Now().After(deadline) {
			t.Fatal("background child in the group survived Close")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWaitReapedReturnsAfterClose(t *testing.T) {
	p := startCmd(t, "sh", "-c", "trap '' HUP; while :; do sleep 1; done")
	time.Sleep(50 * time.Millisecond)
	p.Close()
	if !WaitReaped(3 * time.Second) {
		t.Fatal("WaitReaped timed out")
	}
	if !p.HasExited() {
		t.Fatal("process not exited after WaitReaped")
	}
}

// Settings reach a real session: [shell.env] variables, the home override
// (shell.home beats a HOME in [shell.env]) as both $HOME and the start
// directory, and the build version.
func TestSessionEnvFromSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	alt := filepath.Join(home, "alt")
	if err := os.MkdirAll(filepath.Join(home, ".config", "raven-terminal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(alt, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[shell]\nhome = \"" + alt + "\"\n\n[shell.env]\nRAVEN_TEST_VAR = \"from settings\"\nHOME = \"/nonexistent\"\n"
	if err := os.WriteFile(filepath.Join(home, ".config", "raven-terminal", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := NewPtySessionSized(80, 24, 640, 384, "", []string{"sh", "-c", `echo "[$RAVEN_TEST_VAR|$HOME|$(pwd)|$TERM_PROGRAM_VERSION]"`})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	var out bytes.Buffer
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "]") && time.Now().Before(deadline) {
		n, err := p.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	want := "[from settings|" + alt + "|" + alt + "|" + version.String() + "]"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output %q, want %q", out.String(), want)
	}
}
