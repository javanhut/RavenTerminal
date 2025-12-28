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
	// Load config
	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	// Get shell path
	shell := findShell(cfg)

	// Get user info
	currentUser, err := user.Current()
	if err != nil {
		return nil, err
	}

	// Determine shell type
	shellBase := shell
	if idx := strings.LastIndex(shell, "/"); idx >= 0 {
		shellBase = shell[idx+1:]
	}

	// Write the init script
	initScriptPath, err := cfg.WriteInitScript()
	if err != nil {
		// Non-fatal, continue without init script
		initScriptPath = ""
	}

	// Build shell command based on config
	var cmd *exec.Cmd
	if cfg.Shell.SourceRC {
		// Source user's rc files - run as interactive login shell
		switch shellBase {
		case "bash":
			if initScriptPath != "" {
				// Use --rcfile to source our init script (which can source .bashrc)
				cmd = exec.Command(shell, "--rcfile", initScriptPath)
			} else {
				// Fall back to interactive shell
				cmd = exec.Command(shell, "-i")
			}
		case "zsh":
			// Zsh will source .zshrc automatically
			cmd = exec.Command(shell, "-i")
		case "fish":
			cmd = exec.Command(shell, "-i")
		default:
			cmd = exec.Command(shell, "-i")
		}
	} else {
		// Don't source rc files
		switch shellBase {
		case "bash":
			if initScriptPath != "" {
				cmd = exec.Command(shell, "--noprofile", "--rcfile", initScriptPath)
			} else {
				cmd = exec.Command(shell, "--noprofile", "--norc", "-i")
			}
		case "zsh":
			cmd = exec.Command(shell, "--no-rcs", "-i")
		case "fish":
			cmd = exec.Command(shell, "--no-config", "-i")
		default:
			cmd = exec.Command(shell, "-i")
		}
	}

	// Create new session
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// XDG runtime directory
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" {
		xdgRuntimeDir = "/run/user/" + currentUser.Uid
	}

	// Build environment
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:" + os.Getenv("PATH"),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"RAVEN_TERMINAL=1",
		"HOME=" + currentUser.HomeDir,
		"USER=" + currentUser.Username,
		"SHELL=" + shell,
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"XDG_RUNTIME_DIR=" + xdgRuntimeDir,
		"LS_COLORS=di=01;34:fi=0:ln=01;36:ex=01;32:*.crl=01;35",
	}

	// Add display variables if present
	if display := os.Getenv("DISPLAY"); display != "" {
		env = append(env, "DISPLAY="+display)
	}
	if waylandDisplay := os.Getenv("WAYLAND_DISPLAY"); waylandDisplay != "" {
		env = append(env, "WAYLAND_DISPLAY="+waylandDisplay)
		env = append(env, "XDG_SESSION_TYPE=wayland")
	}

	// Add additional env from config
	for k, v := range cfg.Shell.AdditionalEnv {
		env = append(env, k+"="+v)
	}

	// For zsh, set up custom init by prepending to .zshrc
	if shellBase == "zsh" && initScriptPath != "" {
		// Create a custom ZDOTDIR to source our init script
		env = append(env, "RAVEN_INIT_SCRIPT="+initScriptPath)
		// Zsh will source the script via .zshenv or we use precmd
	}

	// For bash without sourcing rc, we need to run the init script
	if shellBase == "bash" && !cfg.Shell.SourceRC && initScriptPath != "" {
		env = append(env, "BASH_ENV="+initScriptPath)
	}

	cmd.Env = env
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

// findShell finds the shell to use based on config
func findShell(cfg *config.Config) string {
	// Check config for user-selected shell
	if cfg.Shell.Path != "" {
		if _, err := os.Stat(cfg.Shell.Path); err == nil {
			return cfg.Shell.Path
		}
	}

	// Get shell from /etc/passwd
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
