package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/javanhut/RavenTerminal/src/config"
)

// darwinDefaultPath is the fallback PATH on macOS when the login shell can't be
// queried. Includes Homebrew (Apple Silicon + Intel) and the standard dirs.
const darwinDefaultPath = "/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/local/sbin:/usr/bin:/bin:/usr/sbin:/sbin"

// linuxDefaultPath mirrors the historical hardcoded prefix used on Linux.
const linuxDefaultPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

var (
	basePathCache   = map[string]string{}
	basePathCacheMu sync.Mutex
)

// resolveBasePath returns the base PATH the shell should start with, cached per
// shell binary. On macOS, GUI-launched apps inherit only a minimal PATH, so we
// ask the user's login shell for its real PATH (picking up path_helper, the
// user's profile, and Homebrew) — the same trick VS Code/JetBrains use. On
// other platforms we keep the historical default prefix plus the inherited PATH.
func resolveBasePath(shell string) string {
	basePathCacheMu.Lock()
	if cached, ok := basePathCache[shell]; ok {
		basePathCacheMu.Unlock()
		return cached
	}
	basePathCacheMu.Unlock()

	var base string
	if runtime.GOOS == "darwin" {
		base = darwinLoginPath(shell)
	} else {
		inherited := os.Getenv("PATH")
		if inherited != "" {
			base = linuxDefaultPath + ":" + inherited
		} else {
			base = linuxDefaultPath
		}
	}
	// Per-user bin dirs (claude, bun, cargo, ...) are normally added by login
	// rc files, but a custom shell (e.g. ravenshell) may not source any.
	if local := userLocalBinDirs(); len(local) > 0 {
		base = composePath(nil, base+":"+strings.Join(local, ":"))
	}

	basePathCacheMu.Lock()
	basePathCache[shell] = base
	basePathCacheMu.Unlock()
	return base
}

// darwinLoginPath runs a login shell non-interactively to capture its PATH,
// falling back to a sensible default (incl. Homebrew) on error or timeout.
func darwinLoginPath(shell string) string {
	fallback := darwinDefaultPath
	if inherited := os.Getenv("PATH"); inherited != "" {
		fallback = composePath(nil, darwinDefaultPath+":"+inherited)
	}
	probe := probeShellFor(shell)
	if probe == "" {
		return fallback
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// `-l -c` runs a login shell (sourcing /etc/zprofile -> path_helper and the
	// user's ~/.zprofile/~/.bash_profile/fish config -> Homebrew) and prints
	// PATH colon-joined. fish stores PATH as a list joined by spaces, so it needs
	// a different command to produce the colon-separated form.
	probeBase := probe[strings.LastIndex(probe, "/")+1:]
	var args []string
	if probeBase == "fish" {
		args = []string{"-l", "-c", "string join : $PATH"}
	} else {
		args = []string{"-l", "-c", "printf %s \"$PATH\""}
	}
	out, err := exec.CommandContext(ctx, probe, args...).Output()
	if err == nil {
		if p := strings.TrimSpace(string(out)); looksLikePathList(p) {
			return p
		}
	}
	return fallback
}

// probeShellFor returns a shell safe to query for the login PATH. A custom
// shell (e.g. ravenshell) may not implement `-l -c` or POSIX printf, and a
// usage error printed with exit 0 would be taken for the PATH — so anything
// outside the known set is probed via a standard shell instead (zsh first:
// its /etc/zprofile runs path_helper on macOS).
func probeShellFor(shell string) string {
	switch shell[strings.LastIndex(shell, "/")+1:] {
	case "bash", "zsh", "fish", "sh", "dash", "ksh":
		return shell
	}
	for _, s := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(s); err == nil {
			return s
		}
	}
	return ""
}

// looksLikePathList reports whether s plausibly is a colon-separated PATH:
// no newlines, and every entry an absolute path. Shell usage/error text
// captured by the probe must not be mistaken for a PATH.
func looksLikePathList(s string) bool {
	if s == "" || strings.ContainsAny(s, "\n\r") {
		return false
	}
	for entry := range strings.SplitSeq(s, ":") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if !strings.HasPrefix(entry, "/") {
			return false
		}
	}
	return true
}

// userLocalBinDirs returns common per-user bin directories that exist on disk
// (native installers put tools like claude in ~/.local/bin; bun and cargo use
// their own homes).
func userLocalBinDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	var dirs []string
	for _, sub := range []string{".local/bin", ".bun/bin", ".cargo/bin", "go/bin"} {
		dir := home + "/" + sub
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// composePath prepends custom directories to base and de-duplicates entries,
// preserving first-seen order. Empty entries are skipped.
func composePath(custom []string, base string) string {
	seen := make(map[string]struct{})
	var out []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		seen[dir] = struct{}{}
		out = append(out, dir)
	}
	for _, d := range custom {
		add(d)
	}
	for d := range strings.SplitSeq(base, ":") {
		add(d)
	}
	return strings.Join(out, ":")
}

// fishInitIfPresent returns the fish init script path when the file exists,
// "" otherwise — so fish never starts with a "source: no such file" error
// when the fish-side write failed.
func fishInitIfPresent() string {
	path := config.FishInitPath()
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// ravenInitIfPresent returns the rsh init script path when the file exists,
// "" otherwise. RavenShell loads it itself via $RAVEN_INIT_SCRIPT.
func ravenInitIfPresent() string {
	path := config.RavenInitPath()
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// zshDotDirIfPresent returns the generated ZDOTDIR shim directory (see
// config/zshinit.go) when its .zshenv exists, "" otherwise — so zsh never
// starts with a ZDOTDIR pointing at a missing shim.
func zshDotDirIfPresent() string {
	dir := config.ZshDotDir()
	if _, err := os.Stat(dir + "/.zshenv"); err != nil {
		return ""
	}
	return dir
}

// zshInitEnv points ZDOTDIR at the generated shim dir so zsh loads Raven's
// init. Without sourceRC the shim itself skips the user's rc files
// (RAVEN_ZSH_NORC). A stale recursion guard inherited from a parent Raven
// zsh is cleared so the shim always applies. No-op when the shim is missing.
func zshInitEnv(env []string, sourceRC bool) []string {
	dir := zshDotDirIfPresent()
	if dir == "" {
		return env
	}
	env = removeEnv(env, "RAVEN_ZDOTDIR_DONE")
	env = replaceEnv(env, "ZDOTDIR", dir)
	if sourceRC {
		env = removeEnv(env, "RAVEN_ZSH_NORC")
	} else {
		env = replaceEnv(env, "RAVEN_ZSH_NORC", "1")
	}
	return env
}

// PtySession manages a pseudo-terminal connection to a shell
type PtySession struct {
	cmd       *exec.Cmd
	pty       *os.File
	mu        sync.Mutex
	exited    bool
	exitedMu  sync.Mutex
	shellName string // basename of the launched shell (e.g. "bash", "fish")

	// cwdMu guards a short-lived cache of the working directory used on
	// platforms without a /proc cwd link (macOS/BSD), where each lookup shells
	// out to lsof. The mutex is never held while lsof runs — refreshes happen
	// on a goroutine (cwdRefreshing marks one in flight) and callers get the
	// cached value immediately, so the render loop never blocks on lsof.
	cwdMu         sync.Mutex
	cwdCache      string
	cwdCachedAt   time.Time
	cwdRefreshing bool
}

// ShellName returns the basename of the shell this session runs (e.g.
// "bash", "zsh", "fish"). Callers use it to send shell-appropriate commands.
func (p *PtySession) ShellName() string {
	return p.shellName
}

// NewPtySession creates a new PTY session with a login shell
func NewPtySession(cols, rows uint16, startDir string) (*PtySession, error) {
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
			// Raven's init is injected via the ZDOTDIR shim (see zshInitEnv);
			// its .zshenv restores the user's rc files, so zsh still sources
			// .zshrc as usual.
			cmd = exec.Command(shell, "-i")
		case "fish":
			// Fish reads its own config, then -C sources the fish-syntax
			// init script (init.sh is bash and unusable for fish).
			if fishInit := fishInitIfPresent(); fishInit != "" {
				cmd = exec.Command(shell, "-i", "-C", "source "+config.FishQuote(fishInit))
			} else {
				cmd = exec.Command(shell, "-i")
			}
		case "ravenshell":
			// RavenShell reads ~/.ravenrc itself and loads the rsh-syntax
			// init script via $RAVEN_INIT_SCRIPT (set below).
			cmd = exec.Command(shell)
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
			if zshDotDirIfPresent() != "" {
				// The ZDOTDIR shim skips the user's rc files itself
				// (RAVEN_ZSH_NORC, set in zshInitEnv) while still loading
				// Raven's init; --no-rcs would skip the shim's .zshenv too.
				cmd = exec.Command(shell, "-i")
			} else {
				cmd = exec.Command(shell, "--no-rcs", "-i")
			}
		case "fish":
			if fishInit := fishInitIfPresent(); fishInit != "" {
				cmd = exec.Command(shell, "--no-config", "-i", "-C", "source "+config.FishQuote(fishInit))
			} else {
				cmd = exec.Command(shell, "--no-config", "-i")
			}
		case "ravenshell":
			// RavenShell has no --no-config equivalent; it always reads
			// ~/.ravenrc. The rsh init script still applies (set below).
			cmd = exec.Command(shell)
		default:
			cmd = exec.Command(shell, "-i")
		}
	}

	// Create new session
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// XDG runtime directory (Linux convention; not applicable on macOS).
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" && runtime.GOOS != "darwin" {
		xdgRuntimeDir = "/run/user/" + currentUser.Uid
	}

	// Build environment (inherit then override). PATH is resolved from the real
	// login-shell environment (fixing GUI-launched macOS PATH) with the user's
	// configured directories prepended.
	env := os.Environ()
	env = replaceEnv(env, "PATH", composePath(cfg.Shell.Paths, resolveBasePath(shell)))
	env = replaceEnv(env, "TERM", "xterm-256color")
	env = replaceEnv(env, "COLORTERM", "truecolor")
	env = replaceEnv(env, "TERM_PROGRAM", "RavenTerminal")
	env = replaceEnv(env, "TERM_PROGRAM_VERSION", "1.0")
	env = replaceEnv(env, "RAVEN_TERMINAL", "1")
	env = replaceEnv(env, "HOME", currentUser.HomeDir)
	env = replaceEnv(env, "USER", currentUser.Username)
	env = replaceEnv(env, "SHELL", shell)
	// NOTE: deliberately do NOT export COLUMNS/LINES. ncurses (use_env) and many
	// TUI apps prefer those env vars over the PTY's TIOCGWINSZ size, and env vars
	// never update on resize — exporting them freezes apps at the startup size.
	// The PTY winsize (set below and on every Resize) is the source of truth.
	env = removeEnv(env, "COLUMNS")
	env = removeEnv(env, "LINES")
	// Default the locale for UTF-8 rendering, but keep any value the user
	// already configured rather than clobbering it.
	if os.Getenv("LANG") == "" {
		env = replaceEnv(env, "LANG", "en_US.UTF-8")
	}
	if os.Getenv("LC_ALL") == "" {
		env = replaceEnv(env, "LC_ALL", "en_US.UTF-8")
	}
	if xdgRuntimeDir != "" {
		env = replaceEnv(env, "XDG_RUNTIME_DIR", xdgRuntimeDir)
	}
	env = replaceEnv(env, "LS_COLORS", "rs=0:di=38;5;110:ln=38;5;109:mh=38;5;109:pi=38;5;173:so=38;5;173:do=38;5;173:bd=38;5;180:cd=38;5;180:or=38;5;196:mi=38;5;196:su=38;5;160:sg=38;5;160:tw=38;5;110:ow=38;5;110:st=38;5;150:ex=38;5;114:fi=38;5;253:*.go=38;5;150:*.rs=38;5;179:*.js=38;5;178:*.ts=38;5;178:*.json=38;5;173:*.md=38;5;109:*.txt=38;5;245:*.png=38;5;176:*.jpg=38;5;176:*.jpeg=38;5;176:*.svg=38;5;176:*.zip=38;5;173:*.tar=38;5;173:*.gz=38;5;173:*.mp3=38;5;140:*.mp4=38;5;140")

	// Add display variables if present
	if display := os.Getenv("DISPLAY"); display != "" {
		env = replaceEnv(env, "DISPLAY", display)
	}
	if waylandDisplay := os.Getenv("WAYLAND_DISPLAY"); waylandDisplay != "" {
		env = replaceEnv(env, "WAYLAND_DISPLAY", waylandDisplay)
		env = replaceEnv(env, "XDG_SESSION_TYPE", "wayland")
	}

	// Add additional env from config
	for k, v := range cfg.Shell.AdditionalEnv {
		env = replaceEnv(env, k, v)
	}

	// For zsh, inject Raven's init via the standard ZDOTDIR shim.
	if shellBase == "zsh" {
		env = zshInitEnv(env, cfg.Shell.SourceRC)
	}

	// For bash without sourcing rc, we need to run the init script
	if shellBase == "bash" && !cfg.Shell.SourceRC && initScriptPath != "" {
		env = replaceEnv(env, "BASH_ENV", initScriptPath)
	}

	// RavenShell loads the rsh-syntax init script (prompt + detect functions)
	// from this variable after ~/.ravenrc.
	if shellBase == "ravenshell" {
		if ravenInit := ravenInitIfPresent(); ravenInit != "" {
			env = replaceEnv(env, "RAVEN_INIT_SCRIPT", ravenInit)
		}
	}

	cmd.Env = env
	if startDir != "" {
		if info, err := os.Stat(startDir); err == nil && info.IsDir() {
			cmd.Dir = startDir
		} else {
			cmd.Dir = currentUser.HomeDir
		}
	} else {
		cmd.Dir = currentUser.HomeDir
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, err
	}

	session := &PtySession{
		cmd:       cmd,
		pty:       ptmx,
		exited:    false,
		shellName: shellBase,
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

func replaceEnv(env []string, key, value string) []string {
	return append(removeEnv(env, key), key+"="+value)
}

func removeEnv(env []string, key string) []string {
	prefix := key + "="
	for i, e := range slices.Backward(env) {
		if strings.HasPrefix(e, prefix) {
			env = append(env[:i], env[i+1:]...)
		}
	}
	return env
}

// CurrentDir returns the shell process's working directory, or "" if it cannot
// be determined. Linux reads it cheaply from /proc on every call. Other
// platforms (macOS/BSD) have no such link, so the value is queried via lsof and
// cached briefly — this keeps CurrentDir safe to call from the render loop (it
// runs once per tab per frame) without spawning an lsof for every frame.
func (p *PtySession) CurrentDir() string {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return ""
	}
	pid := p.cmd.Process.Pid

	// Linux exposes the live cwd as a symlink; reading it is cheap.
	if path, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)); err == nil {
		return path
	}

	// Elsewhere, serve the cache and refresh it in the background at most
	// once per TTL. lsof takes 5-50ms; running it inline here (as this once
	// did) was a recurring per-pane hitch on the render loop.
	p.cwdMu.Lock()
	cached := p.cwdCache
	fresh := !p.cwdCachedAt.IsZero() && time.Since(p.cwdCachedAt) < cwdCacheTTL
	if fresh || p.cwdRefreshing {
		p.cwdMu.Unlock()
		return cached
	}
	p.cwdRefreshing = true
	p.cwdMu.Unlock()

	go func() {
		cwd := processCwd(pid)
		p.cwdMu.Lock()
		if cwd != "" {
			p.cwdCache = cwd
		}
		p.cwdCachedAt = time.Now()
		p.cwdRefreshing = false
		p.cwdMu.Unlock()
	}()
	return cached
}

// cwdCacheTTL bounds how often CurrentDir spawns lsof on platforms without a
// /proc cwd link: the render loop calls it once per tab per frame, so the
// cache limits lsof to at most one subprocess per TTL per pane.
const cwdCacheTTL = 5 * time.Second

// processCwd returns the working directory of the given pid using lsof, or ""
// on any failure. It is used on platforms without a /proc cwd link (macOS/BSD).
func processCwd(pid int) string {
	// Prefer the absolute path; GUI apps may launch with a stripped-down PATH.
	lsof := "/usr/sbin/lsof"
	if _, err := os.Stat(lsof); err != nil {
		lsof = "lsof"
	}
	// -d cwd selects the cwd "file"; -F n yields machine-readable lines where the
	// path is the line prefixed with 'n'.
	out, err := exec.Command(lsof, "-a", "-d", "cwd", "-F", "n", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "n"); ok {
			return after
		}
	}
	return ""
}

// findShell finds the shell to use based on config
func findShell(cfg *config.Config) string {
	// Check config for user-selected shell
	if cfg.Shell.Path != "" {
		if _, err := os.Stat(cfg.Shell.Path); err == nil {
			return cfg.Shell.Path
		}
	}

	// Prefer $SHELL — valid on macOS (where /etc/passwd does not list users) and
	// honored on Linux too.
	if envShell := os.Getenv("SHELL"); envShell != "" {
		if _, err := os.Stat(envShell); err == nil {
			return envShell
		}
	}

	// Linux fallback: read the user's shell from /etc/passwd.
	if currentUser, err := user.Current(); err == nil {
		shell := getUserShell(currentUser.Username)
		if shell != "" {
			if _, err := os.Stat(shell); err == nil {
				return shell
			}
		}
	}

	// Fallback to common shells (zsh first — macOS default).
	shells := []string{"/bin/zsh", "/usr/bin/zsh", "/bin/bash", "/usr/bin/bash", "/bin/sh"}
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
	for line := range strings.SplitSeq(string(data), "\n") {
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
