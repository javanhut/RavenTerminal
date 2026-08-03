package grid

import "testing"

// texts renders each match back to its selected string, which is how the caller
// actually consumes a hit — it exercises the absolute coordinates rather than
// trusting the raw numbers.
func matchTexts(g *Grid, ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		g.SelectMatch(m)
		out[i] = g.SelectedText()
	}
	g.ClearSelection()
	return out
}

func TestSearchFindsHitsAcrossHistoryAndScreen(t *testing.T) {
	g := NewGrid(20, 3)
	// Only the last three lines stay on screen; the rest is scrollback.
	writeLines(g, "alpha one", "beta", "alpha two", "gamma", "alpha three")

	got := matchTexts(g, g.Search("alpha"))
	if len(got) != 3 {
		t.Fatalf("found %d hits (%q), want 3 spanning history and screen", len(got), got)
	}
	for i, s := range got {
		if s != "alpha" {
			t.Fatalf("hit %d = %q, want %q", i, s, "alpha")
		}
	}
}

func TestSearchSmartCase(t *testing.T) {
	g := NewGrid(20, 4)
	writeLines(g, "Error: nope", "error: also")

	if n := len(g.Search("error")); n != 2 {
		t.Fatalf("lowercase query matched %d, want 2 (case-insensitive)", n)
	}
	hits := g.Search("Error")
	if len(hits) != 1 {
		t.Fatalf("capitalized query matched %d, want 1 (case-sensitive)", len(hits))
	}
	if got := matchTexts(g, hits)[0]; got != "Error" {
		t.Fatalf("case-sensitive hit = %q, want %q", got, "Error")
	}
}

// A match broken across the right margin must still be found: terminal output
// wraps constantly, and a per-row search would silently miss every long path.
func TestSearchSpansSoftWrap(t *testing.T) {
	g := NewGrid(5, 4)
	writeStr(g, "abcdefgh") // wraps to "abcde" + "fgh"

	hits := g.Search("defg")
	if len(hits) != 1 {
		t.Fatalf("found %d hits, want 1 across the wrap boundary", len(hits))
	}
	if hits[0].StartAbsRow == hits[0].EndAbsRow {
		t.Fatal("match did not span rows; the wrap join collapsed to one row")
	}
	if got := matchTexts(g, hits)[0]; got != "defg" {
		t.Fatalf("wrapped hit = %q, want %q", got, "defg")
	}
}

// Trailing blank padding must not bridge two hard lines into one haystack, or
// queries would match text that is not adjacent on screen.
func TestSearchDoesNotJoinHardLines(t *testing.T) {
	g := NewGrid(10, 4)
	writeLines(g, "foo", "bar")
	if n := len(g.Search("foobar")); n != 0 {
		t.Fatalf("matched %d across a hard newline, want 0", n)
	}
}

func TestSearchOverlappingAndEmpty(t *testing.T) {
	g := NewGrid(20, 3)
	writeStr(g, "aaaa")
	// Non-overlapping scan: "aa" in "aaaa" is two hits, not three.
	if n := len(g.Search("aa")); n != 2 {
		t.Fatalf("overlapping query matched %d, want 2 non-overlapping", n)
	}
	if got := g.Search(""); got != nil {
		t.Fatalf("empty query returned %v, want nil", got)
	}
}

func TestScrollToAbsRowRevealsAndLeavesVisibleRowsAlone(t *testing.T) {
	g := NewGrid(10, 3)
	writeLines(g, "L1", "L2", "L3", "L4", "L5", "L6") // history: L1..L3

	hits := g.Search("L1")
	if len(hits) != 1 {
		t.Fatalf("found %d hits for L1, want 1", len(hits))
	}
	g.ScrollToAbsRow(hits[0].StartAbsRow)
	if g.GetScrollOffset() == 0 {
		t.Fatal("scrollback hit did not scroll the view")
	}
	g.SelectMatch(hits[0])
	if got := g.SelectedText(); got != "L1" {
		t.Fatalf("after scrolling, hit reads %q, want %q", got, "L1")
	}

	// An already-visible row must not move the viewport, so stepping between
	// nearby matches doesn't jitter.
	before := g.GetScrollOffset()
	g.ScrollToAbsRow(g.viewTopAbsLocked() + 1)
	if g.GetScrollOffset() != before {
		t.Fatalf("visible row moved the view: offset %d -> %d", before, g.GetScrollOffset())
	}
}
