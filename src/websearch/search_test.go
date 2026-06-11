package websearch

import "testing"

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

func TestCollapseSpace(t *testing.T) {
	if got := collapseSpace("  a\n\t b   c "); got != "a b c" {
		t.Errorf("collapseSpace = %q", got)
	}
}
