package aipanel

import (
	"strings"
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

// Wrapped lines must be measured in display cells, not bytes: emoji are 4
// bytes but 2 cells, so byte-based wrapping pushed lines past the panel edge
// (or wrapped far too early) whenever a model reply contained one.
func TestWrapTextCountsCellsNotBytes(t *testing.T) {
	// 10 cells of emoji (5 runes x 2 cells) must wrap at a 6-cell limit
	// into ceil(10/6)-ish chunks, never split mid-rune.
	lines := wrapText("🎨🎨🎨🎨🎨", 6, "", "")
	for _, l := range lines {
		if grid.StringWidth(l) > 6 {
			t.Errorf("line %q is %d cells, limit 6", l, grid.StringWidth(l))
		}
		if strings.ContainsRune(l, '�') {
			t.Errorf("line %q split mid-rune", l)
		}
	}
}

func TestWrapTextPlainAsciiUnchanged(t *testing.T) {
	lines := wrapText("one two three four", 10, "AI: ", "    ")
	want := []string{"AI: one", "    two", "    three", "    four"}
	for i, l := range lines {
		if grid.StringWidth(l) > 10 {
			t.Errorf("line %q exceeds limit", l)
		}
		if i < len(want) && l != want[i] {
			t.Errorf("line %d = %q, want %q", i, l, want[i])
		}
	}
}

func TestWrapInputTextEmoji(t *testing.T) {
	for _, l := range wrapInputText("hello 😊 world this is a longer line 😊 with emoji", 12) {
		if grid.StringWidth(l) > 12 {
			t.Errorf("input line %q is %d cells, limit 12", l, grid.StringWidth(l))
		}
	}
}

func TestTruncateCells(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"emoji 🎨🎨🎨🎨", 9, "emoji ..."},
	}
	for _, c := range cases {
		got := truncateCells(c.in, c.max)
		if got != c.want {
			t.Errorf("truncateCells(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
		if grid.StringWidth(got) > c.max {
			t.Errorf("truncateCells(%q, %d) = %q exceeds max", c.in, c.max, got)
		}
	}
}

// The bullet continuation indent must match the bullet prefix's display width
// ("• " is 4 bytes but 2 cells).
func TestBulletContinuationIndent(t *testing.T) {
	msg := []Message{{Role: "assistant", Content: "- " + strings.Repeat("word ", 20)}}
	lines := BuildWrappedLines(msg, 20)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped bullet, got %d lines", len(lines))
	}
	first := lines[0].Text  // "AI: • word ..."
	second := lines[1].Text // continuation
	wantIndent := strings.Repeat(" ", len("AI: ")+grid.StringWidth("• "))
	if !strings.HasPrefix(second, wantIndent) || strings.HasPrefix(second, wantIndent+" ") {
		t.Errorf("continuation %q should be indented exactly %d cells (first line %q)",
			second, len(wantIndent), first)
	}
}

// panel_width_percent must actually control the panel width (it was once
// persisted by the menu but ignored here).
func TestLayoutWidthPercent(t *testing.T) {
	p := &Panel{}
	if got := p.Layout(2000, 1000, 10, 20).PanelWidth; got != 700 {
		t.Fatalf("default width = %v, want 700 (35%%)", got)
	}
	p.WidthPercent = 50
	if got := p.Layout(2000, 1000, 10, 20).PanelWidth; got != 1000 {
		t.Fatalf("width at 50%% = %v, want 1000", got)
	}
}
