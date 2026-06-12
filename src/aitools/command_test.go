package aitools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReadOnlyAllows(t *testing.T) {
	allowed := [][]string{
		{"ls", "-la", "/tmp"},
		{"git", "status"},
		{"git", "log", "--oneline", "-5"},
		{"git", "diff", "HEAD~1"},
		{"grep", "-rn", "TODO", "."},
		{"find", ".", "-name", "*.go"},
		{"cat", "main.go"},
		{"ps", "aux"},
	}
	for _, argv := range allowed {
		if err := checkReadOnly(argv); err != nil {
			t.Errorf("checkReadOnly(%v) rejected a read-only command: %v", argv, err)
		}
	}
}

func TestCheckReadOnlyRejects(t *testing.T) {
	rejected := [][]string{
		{"rm", "-rf", "/tmp/x"},            // not allowlisted
		{"touch", "file"},                  // not allowlisted
		{"git", "push"},                    // mutating subcommand
		{"git", "commit", "-m", "x"},       // mutating subcommand
		{"git", "checkout", "main"},        // mutating subcommand
		{"git", "config", "user.name"},     // can write config
		{"git", "diff", "--output=f"},      // write-capable flag
		{"find", ".", "-delete"},           // destructive flag
		{"find", ".", "-exec", "rm", "{}"}, // arbitrary exec
		{"find", ".", "-fprint0", "out"},   // file-writing variant
		{"curl", "http://example.com"},     // network side effects, not listed
		{"sh", "-c", "ls"},                 // shell escape
	}
	for _, argv := range rejected {
		if err := checkReadOnly(argv); err == nil {
			t.Errorf("checkReadOnly(%v) allowed a non-read-only command", argv)
		}
	}
}

func TestSplitCommandRejectsShellFeatures(t *testing.T) {
	for _, cmd := range []string{
		"ls | grep foo",
		"cat a > b",
		"ls && rm x",
		"echo `whoami`",
		"echo $(id)",
		"cat < /etc/passwd",
		"ls; rm x",
	} {
		if _, err := splitCommand(cmd); err == nil {
			t.Errorf("splitCommand(%q) accepted shell metacharacters", cmd)
		}
	}
}

func TestSplitCommandQuotes(t *testing.T) {
	argv, err := splitCommand(`grep -rn "hello world" src`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"grep", "-rn", "hello world", "src"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

func TestRunCommandExecutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(Config{WorkDir: dir})
	out, err := r.Execute(context.Background(), "run_command", map[string]any{"command": "ls"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello.txt") {
		t.Errorf("ls output %q missing hello.txt", out)
	}
}

func TestReadFileTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(Config{WorkDir: dir})
	out, err := r.Execute(context.Background(), "read_file", map[string]any{"path": "f.txt", "start_line": float64(2), "max_lines": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "two") || strings.Contains(out, "one") {
		t.Errorf("read_file window wrong: %q", out)
	}
}

func TestListDirTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(Config{WorkDir: dir})
	out, err := r.Execute(context.Background(), "list_dir", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sub/") {
		t.Errorf("list_dir output %q missing sub/", out)
	}
}

func TestUnknownToolErrors(t *testing.T) {
	r := NewRegistry(Config{WorkDir: t.TempDir()})
	if _, err := r.Execute(context.Background(), "write_file", map[string]any{}); err == nil {
		t.Error("unknown tool should error")
	}
}

func TestGoSubcommands(t *testing.T) {
	if err := checkReadOnly([]string{"go", "version"}); err != nil {
		t.Errorf("go version should be allowed: %v", err)
	}
	for _, argv := range [][]string{
		{"go", "build", "./..."},
		{"go", "run", "main.go"},
		{"go", "env", "-w", "GOPATH=/x"},
	} {
		if err := checkReadOnly(argv); err == nil {
			t.Errorf("checkReadOnly(%v) should be rejected", argv)
		}
	}
}

func TestPackageManagerQueriesAllowed(t *testing.T) {
	allowed := [][]string{
		{"brew", "list"},
		{"brew", "info", "ripgrep"},
		{"brew", "search", "terminal"},
		{"brew", "outdated"},
		{"brew", "--prefix", "go"},
		{"apt", "list", "--installed"},
		{"apt", "show", "curl"},
		{"apt-cache", "search", "editor"},
		{"dpkg", "-l"},
		{"dpkg", "-L", "curl"},
		{"dpkg", "-S", "/usr/bin/curl"},
		{"pacman", "-Qi", "go"},
		{"pacman", "-Ss", "terminal"},
		{"pacman", "-Ql", "ripgrep"},
		{"dnf", "list", "installed"},
		{"apk", "info", "curl"},
		{"npm", "ls", "-g"},
		{"npm", "view", "react", "version"},
		{"pip", "show", "requests"},
		{"pip3", "list", "--outdated"},
		{"port", "installed"},
		{"softwareupdate", "--list"},
	}
	for _, argv := range allowed {
		if err := checkReadOnly(argv); err != nil {
			t.Errorf("checkReadOnly(%v) rejected a read-only package query: %v", argv, err)
		}
	}
}

func TestPackageManagerMutationsRejected(t *testing.T) {
	rejected := [][]string{
		{"brew", "install", "ripgrep"},
		{"brew", "upgrade"},
		{"brew", "uninstall", "go"},
		{"apt", "install", "curl"},
		{"apt", "update"},
		{"apt", "upgrade"},
		{"dpkg", "-i", "pkg.deb"},
		{"dpkg", "-r", "curl"},
		{"pacman", "-S", "go"},
		{"pacman", "-Syu"},
		{"pacman", "-R", "go"},
		{"pacman", "-U", "pkg.tar.zst"},
		{"dnf", "install", "curl"},
		{"npm", "install", "react"},
		{"npm", "update"},
		{"pip", "install", "requests"},
		{"pip", "uninstall", "requests"},
		{"softwareupdate", "--install", "--all"},
		{"softwareupdate", "-i", "-a"},
	}
	for _, argv := range rejected {
		if err := checkReadOnly(argv); err == nil {
			t.Errorf("checkReadOnly(%v) allowed a state-changing package command", argv)
		}
	}
}
