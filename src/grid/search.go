package grid

import (
	"slices"
	"unicode"
)

// Match is one search hit, in ABSOLUTE buffer coordinates (the same space
// selection anchors use — see the Grid.selectionActive docs). Absolute rows
// stay valid as new output scrolls the buffer, so a result list survives the
// shell writing more lines underneath it. A match may span rows when the text
// it matched was soft-wrapped across the right margin.
type Match struct {
	StartAbsRow, StartCol int
	EndAbsRow, EndCol     int
}

// MaxMatches bounds a single search so a one-character query against a full
// 10k-row scrollback cannot allocate without limit. Hitting the cap truncates
// the result list; the caller reports it.
// ponytail: fixed cap, not a streaming/lazy cursor. Raise it if anyone ever
// legitimately needs more than this many hits at once.
const MaxMatches = 5000

// Search returns every occurrence of query across scrollback and the active
// screen, ordered top to bottom. Matching is smart-case: an all-lowercase
// query matches case-insensitively, and a query containing any uppercase rune
// matches exactly — the same rule vim and ripgrep use.
//
// Soft-wrapped rows are joined into their logical line before matching, so a
// path or command broken across the right margin is still found as one string;
// the returned Match then spans from one row to the next.
func (g *Grid) Search(query string) []Match {
	needle := []rune(query)
	if len(needle) == 0 {
		return nil
	}
	fold := !hasUpper(needle)
	if fold {
		for i, r := range needle {
			needle[i] = unicode.ToLower(r)
		}
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	var (
		matches []Match
		hay     []rune // current logical line, soft-wrap joined
		at      []cellPos
	)
	// flush searches the accumulated logical line and resets the accumulator.
	flush := func() {
		// Trailing blanks pad every row out to Cols; dropping them keeps the
		// haystack proportional to real content rather than to the grid width.
		n := len(hay)
		for n > 0 && hay[n-1] == ' ' {
			n--
		}
		for i := 0; i+len(needle) <= n; i++ {
			if !runesEqualAt(hay, i, needle, fold) {
				continue
			}
			s, e := at[i], at[i+len(needle)-1]
			matches = append(matches, Match{
				StartAbsRow: s.absRow, StartCol: s.col,
				EndAbsRow: e.absRow, EndCol: e.col,
			})
			if len(matches) >= MaxMatches {
				return
			}
			// Overlapping hits ("aa" in "aaa") would double-report the same
			// text; advance past the one just taken.
			i += len(needle) - 1
		}
		hay, at = hay[:0], at[:0]
	}

	first := g.scrolledOut
	last := first + len(g.history) + len(g.rows) // exclusive
	for abs := first; abs < last && len(matches) < MaxMatches; abs++ {
		row := g.rowAtAbsLocked(abs)
		if row == nil {
			continue
		}
		for col, sc := range row.cells {
			// A continuation cell repeats its wide neighbour; counting it would
			// insert a phantom rune between the base char and the next one.
			if sc.Width == CellWidthContinuation {
				continue
			}
			ch := sc.Char
			if ch == 0 {
				ch = ' '
			}
			hay = append(hay, ch)
			at = append(at, cellPos{absRow: abs, col: col})
		}
		// Keep accumulating only while the line genuinely continues; the final
		// row of the buffer has no successor to wrap into.
		if row.flags&RowSoftWrapped != 0 && abs+1 < last {
			continue
		}
		flush()
	}
	return matches
}

// cellPos maps a rune in the search haystack back to the cell it came from.
type cellPos struct {
	absRow int
	col    int
}

func hasUpper(rs []rune) bool {
	return slices.ContainsFunc(rs, unicode.IsUpper)
}

// runesEqualAt reports whether needle occurs in hay at offset i. hay is not
// pre-folded (it is rebuilt per logical line and most lines hold no match), so
// folding happens per compared rune instead.
func runesEqualAt(hay []rune, i int, needle []rune, fold bool) bool {
	for j, want := range needle {
		got := hay[i+j]
		if fold {
			got = unicode.ToLower(got)
		}
		if got != want {
			return false
		}
	}
	return true
}

// SelectMatch highlights a match by pointing the selection at it, reusing the
// selection's absolute anchoring so the highlight tracks the text as output
// scrolls it. SelectedText then yields the matched line range, so the existing
// copy binding works on a search hit.
func (g *Grid) SelectMatch(m Match) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Cols == 0 || g.Rows == 0 {
		return
	}
	g.selectionActive = true
	g.selAnchorAbsRow, g.selAnchorCol = m.StartAbsRow, clampInt(m.StartCol, 0, g.Cols-1)
	g.selEndAbsRow, g.selEndCol = m.EndAbsRow, clampInt(m.EndCol, 0, g.Cols-1)
}

// ScrollToAbsRow scrolls the view so the given absolute row is visible, resting
// it about a third of the way down the viewport so the surrounding context —
// especially the lines that follow a hit — stays on screen. Rows already
// visible are left alone, so stepping between nearby matches does not jitter
// the viewport.
func (g *Grid) ScrollToAbsRow(abs int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Rows == 0 {
		return
	}
	top := g.viewTopAbsLocked()
	if abs >= top && abs < top+g.Rows {
		return
	}
	offset := g.scrolledOut + len(g.history) - abs + g.Rows/3
	g.scrollOffset = clampInt(offset, 0, len(g.history))
	g.snapInvalid = true
}
