package shell

import (
	"strings"
	"testing"
)

func TestLooksLikePathList(t *testing.T) {
	valid := []string{
		"/usr/bin:/bin",
		"/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
		"/single",
	}
	for _, p := range valid {
		if !looksLikePathList(p) {
			t.Errorf("looksLikePathList(%q) = false, want true", p)
		}
	}
	invalid := []string{
		"",
		"usage: printf format [arguments ...]\nerror: unknown operator: %",
		"command not found",
		"/usr/bin:not-absolute",
	}
	for _, p := range invalid {
		if looksLikePathList(p) {
			t.Errorf("looksLikePathList(%q) = true, want false", p)
		}
	}
}

func TestProbeShellForKnownShells(t *testing.T) {
	for _, shell := range []string{"/bin/bash", "/bin/zsh", "/opt/homebrew/bin/fish", "/bin/sh"} {
		if got := probeShellFor(shell); got != shell {
			t.Errorf("probeShellFor(%q) = %q, want the shell itself", shell, got)
		}
	}
}

func TestProbeShellForUnknownShellUsesStandardShell(t *testing.T) {
	got := probeShellFor("/opt/homebrew/bin/ravenshell")
	if got == "/opt/homebrew/bin/ravenshell" {
		t.Fatalf("probeShellFor must not probe an unknown shell directly")
	}
	if got != "" && !strings.HasPrefix(got, "/bin/") {
		t.Fatalf("probeShellFor = %q, want a standard /bin shell", got)
	}
}

func TestComposePathPrependAndDedup(t *testing.T) {
	got := composePath([]string{"/opt/homebrew/bin", "/usr/bin"}, "/usr/bin:/bin")
	want := "/opt/homebrew/bin:/usr/bin:/bin"
	if got != want {
		t.Fatalf("composePath = %q, want %q", got, want)
	}
}

func TestComposePathEmptyCustom(t *testing.T) {
	got := composePath(nil, "/usr/bin:/bin")
	if got != "/usr/bin:/bin" {
		t.Fatalf("composePath = %q, want unchanged base", got)
	}
}

func TestComposePathDedupesBase(t *testing.T) {
	got := composePath(nil, "/usr/bin:/bin:/usr/bin")
	if got != "/usr/bin:/bin" {
		t.Fatalf("composePath = %q, want deduped", got)
	}
}

func TestComposePathSkipsEmptyAndWhitespace(t *testing.T) {
	got := composePath([]string{"", "  /a/b  "}, "::/bin")
	if got != "/a/b:/bin" {
		t.Fatalf("composePath = %q, want %q", got, "/a/b:/bin")
	}
}

func TestComposePathCustomDirAlreadyInBaseMovesFirst(t *testing.T) {
	// A custom dir that also exists in base should appear once, in the custom
	// (prepended) position.
	got := composePath([]string{"/bin"}, "/usr/bin:/bin")
	if got != "/bin:/usr/bin" {
		t.Fatalf("composePath = %q, want %q", got, "/bin:/usr/bin")
	}
}
