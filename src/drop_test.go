package main

import (
	"testing"

	"github.com/javanhut/RavenTerminal/src/parser"
)

func TestQuoteDroppedPaths(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"/plain/file.txt"}, "/plain/file.txt "},
		{[]string{"/tmp/a b", "/x/it's"}, `'/tmp/a b' '/x/it'\''s' `},
		{[]string{"/a/$HOME;rm", "/b/*.go"}, `'/a/$HOME;rm' '/b/*.go' `},
		{[]string{"/n\nl"}, `$'/n\nl' `},
		{[]string{"/tab\tq'x"}, `$'/tab\tq\'x' `},
		{[]string{"~user"}, `'~user' `},
		{nil, ""},
		{[]string{""}, ""},
	}
	for _, c := range cases {
		if got := quoteDroppedPaths(c.in); got != c.want {
			t.Errorf("quoteDroppedPaths(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDropPasteIsBracketedWhenEnabled(t *testing.T) {
	term := parser.NewTerminal(20, 5)
	term.Process([]byte("\x1b[?2004h"))
	w := &captureWriter{}
	dropPaths(term, w, []string{"/tmp/a b"})
	if got, want := string(w.data), "\x1b[200~'/tmp/a b' \x1b[201~"; got != want {
		t.Fatalf("drop = %q, want %q", got, want)
	}
	w = &captureWriter{}
	dropPaths(parser.NewTerminal(20, 5), w, []string{"/x"})
	if got := string(w.data); got != "/x " {
		t.Fatalf("plain drop = %q", got)
	}
}
