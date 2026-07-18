package searchpanel

import (
	"fmt"
	"testing"

	"github.com/javanhut/RavenTerminal/src/websearch"
)

func TestFindMatchLines(t *testing.T) {
	lines := []string{"Hello World", "nothing here", "world of Go", "WORLD"}

	got := findMatchLines(lines, "world")
	want := []int{0, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("findMatchLines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("findMatchLines = %v, want %v", got, want)
		}
	}

	if got := findMatchLines(lines, ""); got != nil {
		t.Errorf("empty query matched %v, want none", got)
	}
	if got := findMatchLines(lines, "  "); got != nil {
		t.Errorf("blank query matched %v, want none", got)
	}
	if got := findMatchLines(nil, "x"); got != nil {
		t.Errorf("nil lines matched %v, want none", got)
	}
}

// A query whose words land on opposite sides of a wrap boundary must still
// match: the wrapped pair joined by the removed space is searched, reported
// on the line the match starts on (without double-counting the next line).
func TestFindMatchLinesAcrossWrapBoundary(t *testing.T) {
	lines := []string{"the quick", "brown fox jumps", "over the lazy dog"}

	got := findMatchLines(lines, "quick brown")
	if len(got) != 1 || got[0] != 0 {
		t.Errorf("findMatchLines(quick brown) = %v, want [0]", got)
	}

	got = findMatchLines(lines, "jumps over")
	if len(got) != 1 || got[0] != 1 {
		t.Errorf("findMatchLines(jumps over) = %v, want [1]", got)
	}

	// A whole-line match is not also reported by the previous line's pair.
	got = findMatchLines(lines, "brown fox")
	if len(got) != 1 || got[0] != 1 {
		t.Errorf("findMatchLines(brown fox) = %v, want [1]", got)
	}
}

func TestConfirmFindJumpsToFirstMatchFromScroll(t *testing.T) {
	p := New()
	p.PreviewWrapped = []string{"x", "match", "x", "x", "match", "x", "match"}
	p.FindActive = true
	p.FindEditing = true
	p.FindQuery = "match"
	p.PreviewScroll = 3

	p.ConfirmFind(2)
	if p.FindEditing {
		t.Error("ConfirmFind left FindEditing set")
	}
	// First match at or after scroll position 3 is line 4 (index 1 of matches).
	if p.FindMatch != 1 {
		t.Errorf("FindMatch = %d, want 1", p.FindMatch)
	}
	if p.PreviewScroll > 4 || p.PreviewScroll+2 <= 4 {
		t.Errorf("match line 4 not visible: scroll=%d visible=2", p.PreviewScroll)
	}

	// No match at or after scroll: wraps to the first match.
	p.PreviewScroll = 100
	p.ConfirmFind(2)
	if p.FindMatch != 0 {
		t.Errorf("FindMatch = %d, want 0 (wrapped)", p.FindMatch)
	}
	if p.PreviewScroll > 1 {
		t.Errorf("scroll = %d, match line 1 not visible", p.PreviewScroll)
	}

	// No matches at all.
	p.FindQuery = "absent"
	p.ConfirmFind(2)
	if p.FindMatch != -1 {
		t.Errorf("FindMatch = %d, want -1", p.FindMatch)
	}
}

func TestFindStepWrapsBothDirections(t *testing.T) {
	p := New()
	p.PreviewWrapped = []string{"match", "x", "match", "x", "match"}
	p.FindActive = true
	p.FindQuery = "match"
	p.FindMatch = 2 // last match (line 4)

	p.FindStep(1, 2)
	if p.FindMatch != 0 {
		t.Errorf("next from last: FindMatch = %d, want 0", p.FindMatch)
	}
	if p.PreviewScroll != 0 {
		t.Errorf("scroll = %d, want 0 (match line 0 visible)", p.PreviewScroll)
	}

	p.FindStep(-1, 2)
	if p.FindMatch != 2 {
		t.Errorf("prev from first: FindMatch = %d, want 2", p.FindMatch)
	}
	// Line 4 must be scrolled into a 2-line viewport.
	if p.PreviewScroll > 4 || p.PreviewScroll+2 <= 4 {
		t.Errorf("match line 4 not visible: scroll=%d visible=2", p.PreviewScroll)
	}

	// Unset match index starts at the first match.
	p.FindMatch = -1
	p.FindStep(1, 2)
	if p.FindMatch != 0 {
		t.Errorf("step from unset: FindMatch = %d, want 0", p.FindMatch)
	}

	// No matches leaves the index unset.
	p.FindQuery = "absent"
	p.FindStep(1, 2)
	if p.FindMatch != -1 {
		t.Errorf("step with no matches: FindMatch = %d, want -1", p.FindMatch)
	}
}

func TestFindEditingResetsMatch(t *testing.T) {
	p := New()
	p.StartFind()
	if !p.FindActive || !p.FindEditing {
		t.Fatal("StartFind did not enter find mode")
	}
	p.AppendFind('g')
	p.AppendFind('o')
	if p.FindQuery != "go" {
		t.Errorf("FindQuery = %q, want %q", p.FindQuery, "go")
	}
	p.FindMatch = 3
	p.FindBackspace()
	if p.FindQuery != "g" || p.FindMatch != -1 {
		t.Errorf("after backspace: query=%q match=%d", p.FindQuery, p.FindMatch)
	}
	p.ExitFind()
	if p.FindActive || p.FindEditing || p.FindQuery != "" || p.FindMatch != -1 {
		t.Errorf("ExitFind left state: %+v", p)
	}
}

func TestLoadHistoryTrimsToMax(t *testing.T) {
	p := New()
	entries := make([]string, maxHistorySize+10)
	for i := range entries {
		entries[i] = fmt.Sprintf("query %d", i)
	}
	p.LoadHistory(entries)
	if len(p.History) != maxHistorySize {
		t.Errorf("len(History) = %d, want %d", len(p.History), maxHistorySize)
	}
	if p.History[0] != "query 0" {
		t.Errorf("History[0] = %q, want newest-first order kept", p.History[0])
	}

	// Empty load keeps whatever is there.
	p.LoadHistory(nil)
	if len(p.History) != maxHistorySize {
		t.Errorf("LoadHistory(nil) changed history: len=%d", len(p.History))
	}
}

func TestNavStackBackForward(t *testing.T) {
	p := New()

	// First push: nothing to go back to.
	p.NavPush("https://a", "A")
	if _, ok := p.NavBack(); ok {
		t.Fatal("NavBack succeeded on the first page")
	}

	// A (scrolled to 5) -> B (scrolled to 9) -> C.
	p.PreviewScroll = 5
	p.NavPush("https://b", "B")
	p.PreviewScroll = 9
	p.NavPush("https://c", "C")
	p.PreviewScroll = 2

	entry, ok := p.NavBack()
	if !ok || entry.URL != "https://b" || entry.Scroll != 9 {
		t.Fatalf("NavBack = %+v, %v; want B at scroll 9", entry, ok)
	}
	entry, ok = p.NavBack()
	if !ok || entry.URL != "https://a" || entry.Scroll != 5 {
		t.Fatalf("NavBack = %+v, %v; want A at scroll 5", entry, ok)
	}
	if _, ok := p.NavBack(); ok {
		t.Fatal("NavBack past the first page succeeded")
	}

	entry, ok = p.NavForward()
	if !ok || entry.URL != "https://b" {
		t.Fatalf("NavForward = %+v, %v; want B", entry, ok)
	}
	// The restore flow re-pushes the same URL: must not duplicate.
	p.NavPush("https://b", "B")
	if len(p.NavStack) != 3 || p.NavPos != 1 {
		t.Fatalf("restore push changed stack: len=%d pos=%d", len(p.NavStack), p.NavPos)
	}

	// Navigating somewhere new from B truncates the forward entry (C).
	p.NavPush("https://d", "D")
	if len(p.NavStack) != 3 || p.NavStack[2].URL != "https://d" || p.NavPos != 2 {
		t.Fatalf("push after back kept forward entries: %+v pos=%d", p.NavStack, p.NavPos)
	}
	if _, ok := p.NavForward(); ok {
		t.Fatal("NavForward succeeded past the newest page")
	}

	p.ResetNav()
	if len(p.NavStack) != 0 || p.NavPos != -1 {
		t.Fatalf("ResetNav left state: %+v pos=%d", p.NavStack, p.NavPos)
	}
}

func TestSetPreviewAppliesPendingScroll(t *testing.T) {
	p := New()
	p.PendingScroll = 7
	p.SetPreview("https://a", "A", []string{"line"}, nil, nil)
	if p.PreviewScroll != 7 || p.PendingScroll != 0 {
		t.Errorf("scroll=%d pending=%d, want 7 and 0", p.PreviewScroll, p.PendingScroll)
	}
	if p.SelectedLink != -1 {
		t.Errorf("SelectedLink = %d, want -1", p.SelectedLink)
	}
	// Ordinary navigation (no pending) starts at the top.
	p.SetPreview("https://b", "B", []string{"line"}, nil, nil)
	if p.PreviewScroll != 0 {
		t.Errorf("scroll = %d, want 0", p.PreviewScroll)
	}
	// A fresh NavPush cancels a stale pending restore.
	p.PendingScroll = 4
	p.NavPush("https://c", "C")
	if p.PendingScroll != 0 {
		t.Errorf("NavPush kept PendingScroll = %d", p.PendingScroll)
	}
}

func TestLinkMarkerPos(t *testing.T) {
	lines := []string{"intro", "see docs[1] here", "and [12] elsewhere", "ref[2]"}
	cases := []struct {
		n, occ   int
		wantLine int
		wantCol  int
	}{
		{1, 0, 1, 8}, // "[1]" must not match inside "[12]"
		{12, 0, 2, 4},
		{2, 0, 3, 3},
		{99, 0, -1, -1},
	}
	for _, c := range cases {
		line, col := linkMarkerPos(lines, c.n, c.occ)
		if line != c.wantLine || col != c.wantCol {
			t.Errorf("linkMarkerPos(%d, %d) = (%d, %d), want (%d, %d)", c.n, c.occ, line, col, c.wantLine, c.wantCol)
		}
	}

	// A literal citation "[1]" earlier in the page is skipped when the link's
	// marker is a later occurrence.
	cited := []string{"a fact [1] cited", "the link[1] itself", "[1] again"}
	if line, col := linkMarkerPos(cited, 1, 1); line != 1 || col != 8 {
		t.Errorf("occ 1 = (%d, %d), want (1, 8)", line, col)
	}
	if line, _ := linkMarkerPos(cited, 1, 2); line != 2 {
		t.Errorf("occ 2 line = %d, want 2", line)
	}
	if line, col := linkMarkerPos(cited, 1, 3); line != -1 || col != -1 {
		t.Errorf("missing occ = (%d, %d), want (-1, -1)", line, col)
	}
}

func TestCycleLinkWrapsAndScrolls(t *testing.T) {
	p := New()
	p.Links = []websearch.Link{
		{Text: "a", URL: "https://a"},
		{Text: "b", URL: "https://b"},
		{Text: "c", URL: "https://c"},
	}
	p.PreviewWrapped = []string{"x", "a[1]", "x", "x", "b[2]", "x", "x", "c[3]"}

	p.CycleLink(1, 3)
	if p.SelectedLink != 0 || p.PreviewScroll != 0 {
		t.Fatalf("first Tab: sel=%d scroll=%d", p.SelectedLink, p.PreviewScroll)
	}
	p.CycleLink(1, 3)
	if p.SelectedLink != 1 || p.PreviewScroll != 2 {
		t.Fatalf("second Tab: sel=%d scroll=%d, want marker line 4 visible", p.SelectedLink, p.PreviewScroll)
	}
	p.CycleLink(1, 3)
	p.CycleLink(1, 3) // wraps to the first link, scrolls back up
	if p.SelectedLink != 0 || p.PreviewScroll != 1 {
		t.Fatalf("wrap: sel=%d scroll=%d", p.SelectedLink, p.PreviewScroll)
	}
	// Shift+Tab from none selects the last link.
	q := New()
	q.Links = p.Links
	q.PreviewWrapped = p.PreviewWrapped
	q.CycleLink(-1, 3)
	if q.SelectedLink != 2 {
		t.Fatalf("reverse from none: sel=%d, want 2", q.SelectedLink)
	}

	if link, ok := p.SelectedLinkTarget(); !ok || link.URL != "https://a" {
		t.Errorf("SelectedLinkTarget = %+v, %v", link, ok)
	}
	empty := New()
	empty.CycleLink(1, 3)
	if _, ok := empty.SelectedLinkTarget(); ok || empty.SelectedLink != -1 {
		t.Errorf("empty links: sel=%d", empty.SelectedLink)
	}
}

func TestSelectedPreviewText(t *testing.T) {
	p := New()
	p.PreviewWrapped = []string{"line0", "line1", "line2", "line3"}

	if got := p.SelectedPreviewText(); got != "" {
		t.Errorf("inactive selection returned %q", got)
	}

	p.SelectionActive = true
	p.SelectionStart = 1
	p.SelectionEnd = 2
	if got := p.SelectedPreviewText(); got != "line1\nline2" {
		t.Errorf("SelectedPreviewText = %q, want %q", got, "line1\nline2")
	}

	// Reversed drag (end above start) normalizes.
	p.SelectionStart = 3
	p.SelectionEnd = 0
	if got := p.SelectedPreviewText(); got != "line0\nline1\nline2\nline3" {
		t.Errorf("reversed selection = %q", got)
	}

	// Out-of-range indices clamp instead of panicking.
	p.SelectionStart = -5
	p.SelectionEnd = 99
	if got := p.SelectedPreviewText(); got != "line0\nline1\nline2\nline3" {
		t.Errorf("clamped selection = %q", got)
	}

	// Fully outside the wrapped lines yields nothing.
	p.SelectionStart = 10
	p.SelectionEnd = 20
	if got := p.SelectedPreviewText(); got != "" {
		t.Errorf("out-of-range selection = %q, want empty", got)
	}
}

func TestShowHideBookmarks(t *testing.T) {
	p := New()
	p.SetResults("go", []Result{{Title: "R", URL: "https://r.example"}}, nil)
	p.Selected = 0
	p.Status = "1 results"

	marks := []Result{{Title: "B1", URL: "https://b1.example"}, {Title: "B2", URL: "https://b2.example"}}
	p.ShowBookmarks(marks)
	if !p.ShowingBookmarks || p.Status != "Bookmarks" {
		t.Fatalf("ShowBookmarks: showing=%v status=%q", p.ShowingBookmarks, p.Status)
	}
	if len(p.Results) != 2 || p.Results[0].URL != "https://b1.example" {
		t.Fatalf("bookmarks not shown as results: %v", p.Results)
	}

	p.HideBookmarks()
	if p.ShowingBookmarks {
		t.Fatal("HideBookmarks left ShowingBookmarks set")
	}
	if len(p.Results) != 1 || p.Results[0].URL != "https://r.example" {
		t.Fatalf("normal results not restored: %v", p.Results)
	}
	if p.Status != "1 results" {
		t.Errorf("status not restored: %q", p.Status)
	}

	// HideBookmarks outside bookmark view is a no-op.
	p.HideBookmarks()
	if len(p.Results) != 1 {
		t.Fatalf("no-op hide changed results: %v", p.Results)
	}
}

func TestBookmarkViewExitsOnQueryEditAndNewResults(t *testing.T) {
	p := New()
	p.SetResults("go", []Result{{Title: "R", URL: "https://r.example"}}, nil)
	p.ShowBookmarks([]Result{{Title: "B", URL: "https://b.example"}})

	// Typing a character restores the normal results.
	p.AppendQuery('x')
	if p.ShowingBookmarks {
		t.Fatal("query edit did not leave bookmark view")
	}
	if len(p.Results) != 1 || p.Results[0].URL != "https://r.example" {
		t.Fatalf("results not restored on query edit: %v", p.Results)
	}

	// A completed search clears bookmark-view state without restoring.
	p.ShowBookmarks([]Result{{Title: "B", URL: "https://b.example"}})
	p.SetResults("new", []Result{{Title: "N", URL: "https://n.example"}}, nil)
	if p.ShowingBookmarks {
		t.Fatal("SetResults left ShowingBookmarks set")
	}
	if len(p.Results) != 1 || p.Results[0].URL != "https://n.example" {
		t.Fatalf("new results not shown: %v", p.Results)
	}
}

func TestSetPreviewClearsSelection(t *testing.T) {
	p := New()
	p.SelectionActive = true
	p.SelectionDragging = true
	p.SetPreview("https://x.example", "X", []string{"text"}, nil, nil)
	if p.SelectionActive || p.SelectionDragging {
		t.Fatal("SetPreview did not clear the mouse selection")
	}
}
