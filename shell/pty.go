package shell

import (
	"io"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/javanhut/RavenTerminal/config"
)

// getDistroName reads the distribution name from /etc/os-release
func getDistroName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "linux"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			id := strings.TrimPrefix(line, "ID=")
			id = strings.Trim(id, "\"")
			return id
		}
	}
	return "linux"
}

// PtySession manages a pseudo-terminal connection to a shell
type PtySession struct {
	cmd      *exec.Cmd
	pty      *os.File
	mu       sync.Mutex
	exited   bool
	exitedMu sync.Mutex
}

// NewPtySession creates a new PTY session with a login shell
func NewPtySession(cols, rows uint16) (*PtySession, error) {
	shell := findShellFromConfig()

	// Get user info from system, not environment
	currentUser, err := user.Current()
	if err != nil {
		return nil, err
	}

	// Run without user profiles/rc so our PS1 and clean env stay in effect
	cmd := exec.Command(shell, "--noprofile", "--norc")

	// Create new session - critical for independence from parent terminal
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// Get distro name for prompt
	distro := getDistroName()

	// Custom PS1: friendly two-line prompt with basic system info and ASCII-only symbols.
	// The path is trimmed to avoid wrapping: show ~, and if longer than 50 chars, keep the tail with an ellipsis.
	ps1 := "\\[\\e[0;36m\\]$(p=$PWD; home=$HOME; case \"$p\" in \"$home\"*) p=\"~${p#$home}\";; esac; max=50; if [ ${#p} -gt $max ]; then p=\"...${p: -$max}\"; fi; printf %s \"$p\")/\\[\\e[0m\\] PackageManager: \\[\\e[0;33m\\]$(if [ -f go.mod ]; then echo \"Go $(go version 2>/dev/null | awk '{print $3}')\"; elif [ -f Cargo.toml ]; then echo \"Cargo $(cargo --version 2>/dev/null | awk '{print $2}')\"; elif [ -f package.json ]; then echo \"Node $(node --version 2>/dev/null)\"; elif [ -f pyproject.toml ] || [ -f requirements.txt ] || [ -f Pipfile ]; then echo \"Python $(python3 --version 2>/dev/null | awk '{print $2}')\"; elif compgen -G \"*.cpp\" >/dev/null || compgen -G \"*.cc\" >/dev/null || compgen -G \"*.cxx\" >/dev/null; then echo \"C++ $(c++ --version 2>/dev/null | head -n1 | awk '{print $NF}')\"; elif compgen -G \"*.crl\" >/dev/null; then echo \"Carrion\"; else echo \"None\"; fi)\\[\\e[0m\\]   VCS: \\[\\e[0;32m\\]$(if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then echo \"Git\"; elif [ -e .ivaldi ]; then echo \"Ivaldi\"; else echo \"None\"; fi)\\[\\e[0m\\]\n[\\u@" + distro + "] > "

	// XDG runtime directory (required for Wayland apps like vem)
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" {
		xdgRuntimeDir = "/run/user/" + currentUser.Uid
	}

	// Clean environment - don't inherit from parent terminal
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"RAVEN_TERMINAL=1",
		"PROMPT_COMMAND=if [ -z \"$__RAVEN_LS_DEFINED\" ]; then __RAVEN_LS_DEFINED=1; alias ls='ls --color=auto -p'; fi",
		"LS_COLORS=di=01;34:fi=0:ln=01;36:ex=01;32:*.crl=01;35",
		"HOME=" + currentUser.HomeDir,
		"USER=" + currentUser.Username,
		"SHELL=" + shell,
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"PS1=" + ps1,
		"XDG_RUNTIME_DIR=" + xdgRuntimeDir,
		"XDG_SESSION_TYPE=wayland",
		"WAYLAND_DISPLAY=" + os.Getenv("WAYLAND_DISPLAY"),
	}

	// Start in home directory
	cmd.Dir = currentUser.HomeDir

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, err
	}

	session := &PtySession{
		cmd:    cmd,
		pty:    ptmx,
		exited: false,
	}

	// Monitor for process exit
	go func() {
		cmd.Wait()
		session.exitedMu.Lock()
		session.exited = true
		session.exitedMu.Unlock()
	}()

	return session, nil
}

// findShellFromConfig finds the shell from config or falls back to system default
func findShellFromConfig() string {
	// Check config for user-selected shell
	cfg, err := config.Load()
	if err == nil && cfg.Shell != "" {
		if _, err := os.Stat(cfg.Shell); err == nil {
			return cfg.Shell
		}
	}
	// Fall back to system default
	return findShell()
}

// findShell finds the default shell from system user database
func findShell() string {
	// Get shell from /etc/passwd, not environment variable
	currentUser, err := user.Current()
	if err == nil {
		shell := getUserShell(currentUser.Username)
		if shell != "" {
			if _, err := os.Stat(shell); err == nil {
				return shell
			}
		}
	}

	// Fallback to common shells
	shells := []string{"/bin/bash", "/usr/bin/bash", "/bin/zsh", "/usr/bin/zsh", "/bin/sh"}
	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}
	return "/bin/sh"
}

// getUserShell reads the user's shell from /etc/passwd
func getUserShell(username string) string {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[0] == username {
			return fields[6]
		}
	}
	return ""
}

// Read reads from the PTY
func (p *PtySession) Read(buf []byte) (int, error) {
	return p.pty.Read(buf)
}

// Write writes to the PTY
func (p *PtySession) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pty.Write(data)
}

// Resize resizes the PTY
func (p *PtySession) Resize(cols, rows uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return pty.Setsize(p.pty, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
}

// HasExited returns true if the shell process has exited
func (p *PtySession) HasExited() bool {
	p.exitedMu.Lock()
	defer p.exitedMu.Unlock()
	return p.exited
}

// Close closes the PTY session
func (p *PtySession) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	return p.pty.Close()
}

// Reader returns an io.Reader for the PTY
func (p *PtySession) Reader() io.Reader {
	return p.pty
}

// Writer returns an io.Writer for the PTY
func (p *PtySession) Writer() io.Writer {
	return p.pty
}
