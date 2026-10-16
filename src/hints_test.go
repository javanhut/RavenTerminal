package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javanhut/RavenTerminal/src/parser"
)

func termWith(cols, rows int, text string) *parser.Terminal {
	t := parser.NewTerminal(cols, rows)
	t.Process([]byte(text))
	return t
}

func TestCollectHintsFindsEveryKind(t *testing.T) {
	term := termWith(60, 6, strings.Join([]string{
		"see https://example.com/a?b=1). and /usr/lib/foo.so",
		"commit 3f2a9c1d merged; src/main.go:42:7 failed",
		"host 192.168.1.20:8080 and ./rel/path.txt ~/notes.md",
		"word defaced and plain text 12:30 ok",
	}, "\r\n"))
	got := collectHints(term.GetGrid())
	var texts []string
	for _, h := range got {
		texts = append(texts, h.Text)
	}
	want := []string{"https://example.com/a?b=1", "/usr/lib/foo.so", "3f2a9c1d", "src/main.go:42:7",
		"192.168.1.20:8080", "./rel/path.txt", "~/notes.md"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Fatalf("hints = %q\nwant    %q", texts, want)
	}
	if h := got[0]; h.Row != 0 || h.Col != 4 || h.EndRow != 0 || h.EndCol != 28 || h.Kind != hintURL {
		t.Fatalf("url hint = %+v", h)
	}
	if got[2].Kind != hintHash || got[4].Kind != hintIP || got[3].Kind != hintPath {
		t.Fatalf("kinds wrong: %+v", got)
	}
}

func TestCollectHintsJoinsSoftWrappedURL(t *testing.T) {
	url := "https://example.com/" + strings.Repeat("x", 30)
	got := collectHints(termWith(20, 5, "> "+url).GetGrid())
	if len(got) != 1 || got[0].Text != url || got[0].Row != 0 || got[0].EndRow != 2 {
		t.Fatalf("wrapped hint = %+v", got)
	}
}

func TestHintLabelsArePrefixFree(t *testing.T) {
	for _, n := range []int{1, 3, 26, 27, 200} {
		labels := hintLabels(n)
		if len(labels) != n {
			t.Fatalf("hintLabels(%d) gave %d", n, len(labels))
		}
		seen := map[string]bool{}
		for _, l := range labels {
			if seen[l] {
				t.Fatalf("duplicate label %q", l)
			}
			seen[l] = true
			for _, o := range labels {
				if o != l && strings.HasPrefix(o, l) {
					t.Fatalf("label %q is a prefix of %q", l, o)
				}
			}
		}
	}
	if l := hintLabels(3); l[0] != "a" {
		t.Fatalf("first label = %q, want a", l[0])
	}
}

func TestHintStateInput(t *testing.T) {
	matches := []hintMatch{{Text: "one"}, {Text: "two"}, {Text: "three"}}
	hs := newHintState(nil, matches)
	// The match nearest the bottom gets the first label.
	if hs.labels[2] != "a" {
		t.Fatalf("labels = %v", hs.labels)
	}
	if _, _, done := hs.input('z'); done {
		t.Fatal("an unknown label must be ignored")
	}
	m, open, done := hs.input(rune(strings.ToUpper(hs.labels[0])[0]))
	if !done || !open || m.Text != "one" {
		t.Fatalf("Shift+label = %+v open=%v done=%v", m, open, done)
	}
	hs = newHintState(nil, matches)
	m, open, done = hs.input('a')
	if !done || open || m.Text != "three" {
		t.Fatalf("label a = %+v open=%v done=%v", m, open, done)
	}
	many := make([]hintMatch, 40)
	hs = newHintState(nil, many)
	if _, _, done := hs.input(rune(hs.labels[5][0])); done || hs.typed == "" {
		t.Fatal("first letter of a two-letter label must wait for the second")
	}
	hs.backspace()
	if hs.typed != "" {
		t.Fatal("backspace must drop the typed prefix")
	}
}

func TestResolveHintTarget(t *testing.T) {
	home, _ := os.UserHomeDir()
	cwd := t.TempDir()
	cases := []struct{ in, want string }{
		{"https://x.org/a", "https://x.org/a"},
		{"src/main.go:42:7", filepath.Join(cwd, "src/main.go")},
		{"/usr/lib/foo.so", "/usr/lib/foo.so"},
		{"~/notes.md", filepath.Join(home, "notes.md")},
	}
	for _, c := range cases {
		kind := hintPath
		if strings.HasPrefix(c.in, "http") {
			kind = hintURL
		}
		if got := hintOpenTarget(hintMatch{Text: c.in, Kind: kind}, cwd); got != c.want {
			t.Errorf("hintOpenTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if hintOpenTarget(hintMatch{Text: "3f2a9c1d", Kind: hintHash}, cwd) != "" {
		t.Error("hashes are inserted, not opened")
	}
}
