package shell

import "testing"

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
