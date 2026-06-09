package grid

import (
	"strings"
	"testing"
)

// allText returns every non-empty line of history+active joined by newlines.
func allText(g *Grid) string {
	var lines []string
	// history
	for _, r := range g.history {
		lines = append(lines, rowText(g, r))
	}
	for _, r := range g.rows {
		lines = append(lines, rowText(g, r))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func rowText(g *Grid, r *Row) string {
	var b strings.Builder
	for _, sc := range r.cells {
		c := g.inflate(sc)
		ch := c.Char
		if ch == 0 {
			ch = ' '
		}
		b.WriteRune(ch)
	}
	return strings.TrimRight(b.String(), " ")
}

func TestReflowNarrowThenWide(t *testing.T) {
	g := NewGrid(10, 4)
	// 14 chars auto-wrap across two rows at width 10.
	writeStr(g, "abcdefghijklmn")
	// Shrink to width 5: should rewrap to "abcde","fghij","klmn".
	g.Resize(5, 4)
	got := strings.ReplaceAll(allText(g), "\n", "|")
	if !strings.Contains(got, "abcde") || !strings.Contains(got, "fghij") || !strings.Contains(got, "klmn") {
		t.Fatalf("after shrink, segments missing: %q", got)
	}
	// Grow back to width 20: the line should rejoin to a single row.
	g.Resize(20, 4)
	joined := allText(g)
	if !strings.Contains(joined, "abcdefghijklmn") {
		t.Fatalf("after widen, line did not rejoin: %q", joined)
	}
}

func TestReflowHardNewlinesPreserved(t *testing.T) {
	g := NewGrid(10, 5)
	writeStr(g, "foo")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "bar")
	g.Resize(4, 5)
	txt := allText(g)
	if !strings.Contains(txt, "foo") || !strings.Contains(txt, "bar") {
		t.Fatalf("hard lines lost on reflow: %q", txt)
	}
	// foo and bar must remain on separate logical lines (not joined).
	if strings.Contains(txt, "foobar") {
		t.Fatalf("hard newline incorrectly joined: %q", txt)
	}
}

func TestReflowWideCharNotSplit(t *testing.T) {
	g := NewGrid(6, 3)
	// Two wide chars then ASCII; reflow to a width that would split a wide char.
	g.WriteChar('世', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	g.WriteChar('界', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
	g.Resize(3, 3) // width 3: one wide char (2 cells) + 1 col
	// No cell should be a lone continuation without its wide-char start before it.
	for _, r := range append(append([]*Row{}, g.history...), g.rows...) {
		for i, sc := range r.cells {
			if sc.Width == CellWidthContinuation {
				if i == 0 || r.cells[i-1].Width != CellWidthWide {
					t.Fatalf("orphaned continuation cell at col %d after reflow", i)
				}
			}
		}
	}
}

func TestReflowCursorStaysOnContent(t *testing.T) {
	g := NewGrid(10, 3)
	writeStr(g, "hello") // cursor at col 5, row 0
	g.Resize(3, 3)
	// Cursor should remain within bounds and on the last logical line.
	col, row := g.GetCursor()
	if col < 0 || col >= 3 || row < 0 || row >= 3 {
		t.Fatalf("cursor out of bounds after reflow: (%d,%d)", col, row)
	}
}

// TestReflowGrowShrinkKeepsPromptAtTop reproduces the "ghost prompt" bug: with
// a two-row prompt at the top of an otherwise blank screen, growing then
// shrinking the window must not push the prompt into scrollback (shrink) and
// then pull the stale copy back in above a redrawn prompt (grow). The blank
// area under the cursor is unused space, not content.
func TestReflowGrowShrinkKeepsPromptAtTop(t *testing.T) {
	g := NewGrid(20, 10)
	writeStr(g, "~")
	g.CarriageReturn()
	g.Newline()
	writeStr(g, "> ") // cursor at (2,1)

	g.Resize(20, 30) // grow
	if col, row := g.GetCursor(); col != 2 || row != 1 {
		t.Fatalf("after grow: cursor = (%d,%d), want (2,1)", col, row)
	}
	if len(g.history) != 0 {
		t.Fatalf("after grow: history has %d rows, want 0", len(g.history))
	}

	g.Resize(20, 10) // shrink back
	if col, row := g.GetCursor(); col != 2 || row != 1 {
		t.Fatalf("after shrink: cursor = (%d,%d), want (2,1)", col, row)
	}
	if len(g.history) != 0 {
		t.Fatalf("after shrink: history has %d rows, want 0 (prompt pushed to scrollback)", len(g.history))
	}
	if got := line(g, 0); got != "~" {
		t.Fatalf("after shrink: row 0 = %q, want %q", got, "~")
	}
	// Exactly one copy of the prompt may exist across history+active.
	if txt := allText(g); strings.Count(txt, "~") != 1 {
		t.Fatalf("prompt duplicated after resize cycle: %q", txt)
	}
}

// TestReflowShrinkAfterClearKeepsPromptAtTop: with scrollback present (e.g.
// after `clear`) and the prompt at the top of the screen, shrinking must keep
// the prompt at the top rather than pulling history back into view or pushing
// the prompt out.
func TestReflowShrinkAfterClearKeepsPromptAtTop(t *testing.T) {
	g := NewGrid(20, 10)
	for i := 0; i < 30; i++ {
		writeStr(g, "old")
		g.CarriageReturn()
		g.Newline()
	}
	g.ClearAll() // push content to history, blank screen
	g.SetCursorPos(1, 1)
	writeStr(g, "> ") // prompt redrawn at top, cursor (2,0)
	hist := len(g.history)

	g.Resize(20, 6) // shrink
	if got := line(g, 0); got != ">" {
		t.Fatalf("after shrink: row 0 = %q, want %q", got, ">")
	}
	if col, row := g.GetCursor(); col != 2 || row != 0 {
		t.Fatalf("after shrink: cursor = (%d,%d), want (2,0)", col, row)
	}
	if len(g.history) != hist {
		t.Fatalf("after shrink: history = %d rows, want %d (boundary must not move)", len(g.history), hist)
	}
}
