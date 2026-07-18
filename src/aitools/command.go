package aitools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The run_command tool executes a single binary directly (no shell), so there
// is no redirection, piping, substitution, or globbing. Safety comes from
// three layers: the metacharacter reject (these would be passed literally and
// only confuse the model, so fail fast with a clear message), the binary
// allowlist below, and per-binary argument blocklists for commands that have
// write-capable flags.

// readOnlyCommands maps an allowed binary to the arguments it must never
// receive (write-capable escape hatches).
var readOnlyCommands = map[string][]string{
	"ls":     nil,
	"pwd":    nil,
	"whoami": nil,
	"uname":  nil,
	"date":   nil,
	"uptime": nil,
	"df":     nil,
	"du":     nil,
	"wc":     nil,
	"file":   nil,
	"stat":   nil,
	"head":   nil,
	"tail":   nil, // -f would hang; the exec timeout bounds it
	"cat":    nil,
	"grep":   nil,
	"rg":     nil,
	"which":  nil,
	"ps":     nil,
	"man":    nil, // pager forced to cat via env below
	"find":   {"-delete", "-exec", "-execdir", "-ok", "-okdir", "-fprint", "-fprintf", "-fls"},
	"git":    {"-o", "--output", "--output-directory"},
	"ivaldi": nil,    // limited to read subcommands below, like git
	"go":     {"-w"}, // limited to read subcommands below; -w blocks `go env -w`

	// Package managers, restricted to query verbs (see readSubcommands and
	// the flag-based validators) so the AI can answer "is X installed,
	// where, what's outdated" — installing/upgrading stays with the user.
	"brew":           nil,
	"apt":            nil,
	"apt-cache":      nil,
	"dpkg":           nil,
	"pacman":         nil,
	"dnf":            nil,
	"apk":            nil,
	"zypper":         nil,
	"port":           nil,
	"npm":            nil,
	"pip":            nil,
	"pip3":           nil,
	"softwareupdate": {"-i", "--install", "-d", "--download"},
}

// readSubcommands restricts multi-verb tools to their read-only verbs.
var readSubcommands = map[string]map[string]bool{
	"git": {
		"status": true, "log": true, "diff": true, "show": true,
		"branch": true, "tag": true, "remote": true, "blame": true,
		"shortlog": true, "describe": true, "rev-parse": true,
		"ls-files": true,
	},
	"ivaldi": {
		"status": true, "log": true, "diff": true, "show": true,
	},
	"go": {
		"version": true, "env": true,
	},
	// Package-manager query verbs. Flag-style verbs (e.g. brew --prefix)
	// are listed alongside word verbs; checkReadOnly accepts either form.
	"brew": {
		"list": true, "ls": true, "info": true, "search": true,
		"outdated": true, "deps": true, "desc": true, "doctor": true,
		"--version": true, "--prefix": true, "--cellar": true, "--repository": true,
	},
	"apt": {
		"list": true, "show": true, "search": true, "policy": true,
		"depends": true, "rdepends": true,
	},
	"apt-cache": {
		"search": true, "show": true, "showpkg": true, "policy": true,
		"depends": true, "rdepends": true,
	},
	"dnf": {
		"list": true, "info": true, "search": true, "provides": true,
		"repoquery": true, "deplist": true,
	},
	"apk": {
		"info": true, "list": true, "search": true, "policy": true,
	},
	"zypper": {
		"search": true, "se": true, "info": true, "if": true,
	},
	"port": {
		"installed": true, "info": true, "search": true, "contents": true,
		"outdated": true,
	},
	"npm": {
		"ls": true, "list": true, "view": true, "info": true, "show": true,
		"outdated": true, "root": true, "prefix": true, "explain": true,
	},
	"pip": {
		"show": true, "list": true, "freeze": true, "check": true,
	},
	"pip3": {
		"show": true, "list": true, "freeze": true, "check": true,
	},
	"softwareupdate": {
		"-l": true, "--list": true,
	},
}

// flagVerbValidators handles package managers whose operations are encoded
// entirely in flags. Every flag argument must match the read-only pattern.
var flagVerbValidators = map[string]func(argv []string) error{
	// pacman: -Q* query operations are read-only; of the -S sync ops only
	// the explicit search/info/group/list forms are. Anything else (-S
	// install, -R remove, -U upgrade, -D database, -F with refresh) is not.
	"pacman": func(argv []string) error {
		sawOp := false
		for _, arg := range argv[1:] {
			if !strings.HasPrefix(arg, "-") {
				continue
			}
			switch {
			case strings.HasPrefix(arg, "-Q"):
				sawOp = true
			case arg == "-Ss" || arg == "-Si" || arg == "-Sg" || arg == "-Sl":
				sawOp = true
			case strings.HasPrefix(arg, "--query"):
				sawOp = true
			default:
				return fmt.Errorf("pacman %s is not a read-only query (use -Q*/-Ss/-Si); explain to the user how to run it themselves", arg)
			}
		}
		if !sawOp {
			return fmt.Errorf("pacman needs a query operation (-Q*, -Ss, -Si)")
		}
		return nil
	},
	// dpkg: only the listed query flags are read-only.
	"dpkg": func(argv []string) error {
		allowed := map[string]bool{
			"-l": true, "--list": true,
			"-L": true, "--listfiles": true,
			"-s": true, "--status": true,
			"-S": true, "--search": true,
			"-p": true, "--print-avail": true,
		}
		sawOp := false
		for _, arg := range argv[1:] {
			if !strings.HasPrefix(arg, "-") {
				continue
			}
			if !allowed[arg] {
				return fmt.Errorf("dpkg %s is not a read-only query (-l/-L/-s/-S); explain to the user how to run it themselves", arg)
			}
			sawOp = true
		}
		if !sawOp {
			return fmt.Errorf("dpkg needs a query flag (-l, -L, -s, -S)")
		}
		return nil
	},
}

const maxCommandOutput = 16 * 1024

func (r *Registry) runCommandTool() Tool {
	allowed := make([]string, 0, len(readOnlyCommands))
	for name := range readOnlyCommands {
		allowed = append(allowed, name)
	}
	return Tool{
		Name: "run_command",
		Description: "Run a single READ-ONLY command and return its output. Allowed binaries: " +
			strings.Join(sortedCopy(allowed), ", ") +
			". No shell features (no pipes, redirection, &&, globs). For git/ivaldi only read subcommands (status, log, diff, show, ...) are allowed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "The command line, e.g. \"git status\" or \"ls -la src\""},
			},
			"required": []string{"command"},
		},
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			command := argString(args, "command")
			if command == "" {
				return "", fmt.Errorf("command is required")
			}
			argv, err := splitCommand(command)
			if err != nil {
				return "", err
			}
			if err := checkReadOnly(argv); err != nil {
				return "", err
			}
			return r.execReadOnly(ctx, argv)
		},
	}
}

// splitCommand tokenizes a command line, honoring single/double quotes, and
// rejects shell metacharacters outright (there is no shell to interpret them,
// so accepting them would silently change meaning).
func splitCommand(command string) ([]string, error) {
	if strings.ContainsAny(command, "|&;<>`$\n\\") {
		return nil, fmt.Errorf("shell features (pipes, redirection, substitution) are not supported; run a single read-only command")
	}
	var argv []string
	var cur strings.Builder
	inQuote := rune(0)
	flush := func() {
		if cur.Len() > 0 {
			argv = append(argv, cur.String())
			cur.Reset()
		}
	}
	for _, c := range command {
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			inQuote = c
		case c == ' ' || c == '\t':
			flush()
		default:
			cur.WriteRune(c)
		}
	}
	if inQuote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return argv, nil
}

// checkReadOnly enforces the allowlist and per-binary argument blocklists.
func checkReadOnly(argv []string) error {
	bin := filepath.Base(argv[0])
	blocked, ok := readOnlyCommands[bin]
	if !ok {
		return fmt.Errorf("%q is not on the read-only allowlist; explain to the user how to run it themselves", bin)
	}
	for _, arg := range argv[1:] {
		for _, bad := range blocked {
			// Prefix match so variants can't slip through ("-fprint0",
			// "--output=x"). Blocklist entries are chosen so no legitimate
			// read-only flag shares their prefix.
			if strings.HasPrefix(arg, bad) {
				return fmt.Errorf("argument %q is not allowed for %s (write-capable)", arg, bin)
			}
		}
	}
	if validate, ok := flagVerbValidators[bin]; ok {
		return validate(argv)
	}
	if subs, ok := readSubcommands[bin]; ok {
		// The verb is normally the first non-flag argument (git --no-pager
		// log). Some tools use flag-style verbs (brew --prefix,
		// softwareupdate -l), so the first argument is accepted as-is too.
		sub := ""
		for _, arg := range argv[1:] {
			if !strings.HasPrefix(arg, "-") {
				sub = arg
				break
			}
		}
		if !subs[sub] && !(len(argv) > 1 && subs[argv[1]]) {
			return fmt.Errorf("%s %s is not a read-only subcommand; explain to the user how to run it themselves", bin, sub)
		}
	}
	return nil
}

// execReadOnly runs argv with no shell, a closed stdin, pagers disabled, and
// capped combined output.
func (r *Registry) execReadOnly(ctx context.Context, argv []string) (string, error) {
	if err := r.checkCommandWorkspace(argv); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = r.cfg.WorkDir
	cmd.Stdin = nil // exec gives the child /dev/null; nothing can prompt
	cmd.Env = append(os.Environ(),
		"PAGER=cat", "GIT_PAGER=cat", "MANPAGER=cat",
		"GIT_TERMINAL_PROMPT=0", "TERM=dumb",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	text := out.String()
	if len(text) > maxCommandOutput {
		text = text[:maxCommandOutput] + "\n... (output truncated)"
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// A non-zero exit is information, not a tool failure: `which rg`
			// exiting 1 means "not installed", grep exiting 1 means "no
			// matches". Report it as content so the model can reason on it.
			if strings.TrimSpace(text) == "" {
				return fmt.Sprintf("(no output, %v)", err), nil
			}
			return fmt.Sprintf("%s\n(exit: %v)", text, err), nil
		}
		// Couldn't run at all (binary missing, timeout, ...).
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "(no output)", nil
	}
	return text, nil
}

// checkCommandWorkspace prevents otherwise read-only utilities from being
// used to inspect paths outside the active terminal directory.
func (r *Registry) checkCommandWorkspace(argv []string) error {
	for _, arg := range argv[1:] {
		candidate := arg
		if _, value, ok := strings.Cut(arg, "="); ok {
			candidate = value
		}
		if candidate == "" || strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
			continue
		}
		if strings.HasPrefix(candidate, "file:") {
			return fmt.Errorf("file URLs are outside the active terminal directory")
		}
		clean := filepath.Clean(candidate)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("path %q is outside the active terminal directory", candidate)
		}
		// If an argument names an existing path, resolve symlinks and enforce
		// the same workspace boundary used by read_file/list_dir.
		path := filepath.Join(r.cfg.WorkDir, candidate)
		if _, err := os.Lstat(path); err == nil {
			if _, err := r.workspacePath(candidate); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
