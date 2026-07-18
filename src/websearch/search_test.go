package websearch

import (
	"context"
	"testing"
)

// DuckDuckGo result links are protocol-relative redirects
// (//duckduckgo.com/l/?uddg=<escaped target>); normalizeURL must decode them
// to the real destination instead of returning the redirect.
func TestNormalizeURLDecodesRedirects(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdl%2F&rut=abc", "https://go.dev/dl/"},
		{"https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage", "https://example.com/page"},
		{"/l/?uddg=https%3A%2F%2Fexample.com", "https://example.com"},
		{"https://example.com/direct", "https://example.com/direct"},
		{"//cdn.example.com/x", "https://cdn.example.com/x"},
	}
	for _, c := range cases {
		if got := normalizeURL(c.in); got != c.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidatePublicHTTPURLBlocksPrivateTargets(t *testing.T) {
	for _, target := range []string{
		"http://localhost:8080",
		"http://127.0.0.1",
		"http://10.0.0.1",
		"http://169.254.169.254/latest/meta-data",
		"http://[::1]",
		"file:///etc/passwd",
	} {
		if err := ValidatePublicHTTPURL(context.Background(), target); err == nil {
			t.Errorf("ValidatePublicHTTPURL(%q) allowed a private target", target)
		}
	}
	if err := ValidatePublicHTTPURL(context.Background(), "https://8.8.8.8"); err != nil {
		t.Errorf("public target rejected: %v", err)
	}
}

func TestCollapseSpace(t *testing.T) {
	if got := collapseSpace("  a\n\t b   c "); got != "a b c" {
		t.Errorf("collapseSpace = %q", got)
	}
}
