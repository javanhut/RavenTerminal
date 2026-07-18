package websearch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/net/html"
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

// lite.duckduckgo.com serves results as table rows: a result-link anchor
// followed by a result-snippet cell.
const liteFixture = `<html><body><form><table>
<tr><td>1.</td><td><a rel="nofollow" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2F" class="result-link">The Go Programming Language</a></td></tr>
<tr><td>&nbsp;</td><td class="result-snippet">Go is an open source programming language.</td></tr>
<tr><td>2.</td><td><a rel="nofollow" href="https://example.com/x" class="result-link">Example
Domain</a></td></tr>
<tr><td>&nbsp;</td><td class="result-snippet">An   example
snippet.</td></tr>
</table></form></body></html>`

func TestParseLiteResults(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(liteFixture))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	results := parseLiteResults(doc, 8)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(results), results)
	}
	if results[0].Title != "The Go Programming Language" || results[0].URL != "https://go.dev/" {
		t.Errorf("result 0 = %+v (redirect not decoded?)", results[0])
	}
	if results[0].Snippet != "Go is an open source programming language." {
		t.Errorf("result 0 snippet = %q", results[0].Snippet)
	}
	if results[1].Title != "Example Domain" || results[1].Snippet != "An example snippet." {
		t.Errorf("result 1 = %+v (whitespace not collapsed?)", results[1])
	}

	// maxResults caps appends without stealing the next result's snippet.
	capped := parseLiteResults(doc, 1)
	if len(capped) != 1 || capped[0].Snippet != "Go is an open source programming language." {
		t.Errorf("capped = %+v", capped)
	}
}

func TestParseHTMLResults(t *testing.T) {
	const fixture = `<html><body>
<div class="result results_links"><h2><a class="result__a" href="/l/?uddg=https%3A%2F%2Fgo.dev%2Fdl%2F">Downloads</a></h2>
<a class="result__snippet">Download Go here.</a></div>
</body></html>`
	doc, err := html.Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	results := parseHTMLResults(doc, 8)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(results), results)
	}
	if results[0].URL != "https://go.dev/dl/" || results[0].Title != "Downloads" || results[0].Snippet != "Download Go here." {
		t.Errorf("result = %+v", results[0])
	}
}

// Headings and list items must come out prefixed with the structure markers
// the preview renderer keys off ("## " and "• "), in both extractors.
func TestExtractStructureMarkers(t *testing.T) {
	const fixture = `<html><body><article>
<h1>Main Title</h1>
<p>Intro text.</p>
<h3>Sub Heading</h3>
<ul><li>first item</li><li>second item</li></ul>
</article></body></html>`
	doc, err := html.Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	for name, extract := range map[string]func(*html.Node, int, *url.URL, *[]Link) string{
		"extractMainContent": extractMainContent,
		"extractText":        extractText,
	} {
		lines := splitLines(extract(doc, 8000, nil, nil))
		text := strings.Join(lines, "\n")
		for _, want := range []string{"## Main Title", "## Sub Heading", "• first item", "• second item"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: missing %q in:\n%s", name, want, text)
			}
		}
	}
}

// trimText must never cut in the middle of a multi-byte rune.
func TestTrimTextRuneBoundary(t *testing.T) {
	got := trimText(strings.Repeat("é", 10), 5) // "é" is 2 bytes; 5 lands mid-rune
	if !utf8.ValidString(got) {
		t.Errorf("trimText produced invalid UTF-8: %q", got)
	}
	if got != "éé..." {
		t.Errorf("trimText = %q, want %q", got, "éé...")
	}
}

func TestResolveLink(t *testing.T) {
	base, _ := url.Parse("https://example.com/docs/page.html")
	cases := []struct {
		href string
		want string
	}{
		{"https://go.dev/dl/", "https://go.dev/dl/"},
		{"/about", "https://example.com/about"},
		{"other.html", "https://example.com/docs/other.html"},
		{"//cdn.example.com/x", "https://cdn.example.com/x"},
		{"#section", ""},
		{"javascript:void(0)", ""},
		{"mailto:x@example.com", ""},
		{"", ""},
		{"ftp://example.com/f", ""},
	}
	for _, c := range cases {
		if got := resolveLink(base, c.href); got != c.want {
			t.Errorf("resolveLink(%q) = %q, want %q", c.href, got, c.want)
		}
	}
	if got := resolveLink(nil, "/relative"); got != "" {
		t.Errorf("resolveLink(nil base, relative) = %q, want empty", got)
	}
}

// HTML anchors must produce inline "[n]" markers after their text and a
// matching Link list, with relative hrefs resolved against the page URL.
func TestExtractLinksFromHTML(t *testing.T) {
	const fixture = `<html><body><article>
<p>See <a href="https://go.dev/dl/">Downloads</a> and <a href="/docs">the docs</a>.</p>
<p>Ignore <a href="#top">top</a> and <a href="mailto:x@y.z">mail</a>.</p>
</article></body></html>`
	doc, err := html.Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	base, _ := url.Parse("https://example.com/page")
	var links []Link
	text := extractMainContent(doc, 8000, base, &links)
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2: %+v", len(links), links)
	}
	if links[0].Text != "Downloads" || links[0].URL != "https://go.dev/dl/" {
		t.Errorf("links[0] = %+v", links[0])
	}
	if links[1].Text != "the docs" || links[1].URL != "https://example.com/docs" {
		t.Errorf("links[1] = %+v", links[1])
	}
	for _, want := range []string{"Downloads [1]", "the docs [2]"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing marker %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[3]") {
		t.Errorf("fragment/mailto anchors were numbered:\n%s", text)
	}
}

// Reader-proxy markdown links must be rewritten to "text[n]" markers; images
// lose their syntax without being numbered.
func TestExtractMarkdownLinks(t *testing.T) {
	base, _ := url.Parse("https://example.com/post")
	lines := []string{
		"See [Go](https://go.dev) and [the docs](/docs \"Docs\").",
		"An image ![logo](logo.png) and [mail](mailto:x@y.z) stay plain.",
		"No links here.",
		"Unclosed [bracket stays as-is.",
	}
	out, links := extractMarkdownLinks(lines, base)
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2: %+v", len(links), links)
	}
	if links[0].Text != "Go" || links[0].URL != "https://go.dev" {
		t.Errorf("links[0] = %+v", links[0])
	}
	if links[1].Text != "the docs" || links[1].URL != "https://example.com/docs" {
		t.Errorf("links[1] = %+v", links[1])
	}
	if out[0] != "See Go[1] and the docs[2]." {
		t.Errorf("out[0] = %q", out[0])
	}
	if out[1] != "An image logo and mail stay plain." {
		t.Errorf("out[1] = %q", out[1])
	}
	if out[2] != lines[2] || out[3] != lines[3] {
		t.Errorf("plain lines changed: %q, %q", out[2], out[3])
	}
}

// Literal bracket text (Wikipedia citation "[1]", unfollowable fragment
// anchors) must not shadow a generated marker: Occ counts the identical "[n]"
// occurrences emitted before the marker so consumers can find the real one.
func TestLinkOccurrenceDisambiguatesLiteralBrackets(t *testing.T) {
	const fixture = `<html><body><article>
<p>Cited fact <a href="#cite_note-1">[1]</a> appears early.</p>
<p>Then <a href="https://go.dev/">Go</a> is linked.</p>
</article></body></html>`
	doc, err := html.Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	base, _ := url.Parse("https://example.com/page")
	var links []Link
	extractMainContent(doc, 8000, base, &links)
	if len(links) != 1 || links[0].URL != "https://go.dev/" {
		t.Fatalf("links = %+v, want just the Go link", links)
	}
	// The literal citation "[1]" precedes link 1's marker, so the marker is
	// the second occurrence.
	if links[0].Occ != 1 {
		t.Errorf("links[0].Occ = %d, want 1", links[0].Occ)
	}

	// Same in the markdown (reader proxy) path, across lines.
	mdLines := []string{
		"A literal [1] citation first.",
		"Then [Go](https://go.dev) linked.",
	}
	_, mdLinks := extractMarkdownLinks(mdLines, base)
	if len(mdLinks) != 1 || mdLinks[0].Occ != 1 {
		t.Fatalf("markdown links = %+v, want one link with Occ 1", mdLinks)
	}
}

func TestDirectURL(t *testing.T) {
	cases := []struct {
		query string
		want  string
		ok    bool
	}{
		{"https://go.dev/dl/", "https://go.dev/dl/", true},
		{"http://example.com", "http://example.com", true},
		{"example.com", "https://example.com", true},
		{"  example.com/path?q=1  ", "https://example.com/path?q=1", true},
		{"golang concurrency", "", false},
		{"golang", "", false},
		{"go 1.22 release", "", false},
		{"", "", false},
		// Dotted terms without a plausible TLD stay searches.
		{"3.14", "", false},
		{"go1.22.1", "", false},
		{"requirements.txt2", "", false},
		// IP literals still count as hosts.
		{"8.8.8.8", "https://8.8.8.8", true},
		// "node.js" still parses as a host (letters-only last label); the
		// panel falls back to a search when its preview fetch fails.
		{"node.js", "https://node.js", true},
	}
	for _, c := range cases {
		got, ok := DirectURL(c.query)
		if got != c.want || ok != c.ok {
			t.Errorf("DirectURL(%q) = (%q, %v), want (%q, %v)", c.query, got, ok, c.want, c.ok)
		}
	}
}

func TestErrorReason(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("search request failed: %w", context.DeadlineExceeded), "timeout"},
		{fmt.Errorf("search failed: %w", &StatusError{Code: 429}), "rate limited (429)"},
		{fmt.Errorf("preview failed: %w", &StatusError{Code: 503}), "HTTP 503"},
		{ErrNoResults, "no results (page layout may have changed)"},
		{errors.New("boom"), "boom"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := ErrorReason(c.err); got != c.want {
			t.Errorf("ErrorReason(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}
