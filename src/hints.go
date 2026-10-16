package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/tab"
)

type hintKind int

const (
	hintURL hintKind = iota
	hintPath
	hintHash
	hintIP
)

// hintMatch is one pickable string on screen, in viewport cells.
type hintMatch struct {
	Text           string
	Kind           hintKind
	Row, Col       int
	EndRow, EndCol int
}

var hintPatterns = []struct {
	kind hintKind
	re   *regexp.Regexp
}{
	{hintURL, regexp.MustCompile("(?:https?|ftp|file|ssh|git)://[^\\s<>\"'`]+")},
	{hintPath, regexp.MustCompile(`(?:~|\.\.?)?/[\w.+~@%-]+(?:/[\w.+~@%-]+)*/?(?::\d+(?::\d+)?)?`)},
	{hintPath, regexp.MustCompile(`[\w.+@-]+(?:/[\w.+@%-]+)+/?(?::\d+(?::\d+)?)?`)},
	{hintPath, regexp.MustCompile(`[\w.-]+\.[A-Za-z]\w*:\d+(?::\d+)?`)},
	{hintIP, regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d{1,5})?\b`)},
	{hintIP, regexp.MustCompile(`\b(?:[0-9a-fA-F]{1,4}:){3,7}[0-9a-fA-F]{1,4}\b`)},
	{hintHash, regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)},
}

func isPathRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_./~-+@%", r)
}

// trimURL drops trailing punctuation and unbalanced closing brackets.
func trimURL(s string) string {
	for s != "" {
		last := s[len(s)-1]
		if strings.IndexByte(".,;:!?'\"", last) >= 0 {
			s = s[:len(s)-1]
			continue
		}
		if open := map[byte]byte{')': '(', ']': '[', '}': '{', '>': '<'}[last]; open != 0 &&
			strings.Count(s, string(open)) < strings.Count(s, string(last)) {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

func isHash(s string) bool {
	return strings.ContainsAny(s, "0123456789") && strings.ContainsAny(s, "abcdef")
}

type hintSpan struct {
	start, end int // rune indices, end exclusive
	kind       hintKind
}

// findHintSpans returns non-overlapping matches in a logical line.
func findHintSpans(line string) []hintSpan {
	var spans []hintSpan
	for _, p := range hintPatterns {
		for _, loc := range p.re.FindAllStringIndex(line, -1) {
			text := line[loc[0]:loc[1]]
			if p.kind == hintURL {
				text = trimURL(text)
			}
			if p.kind == hintHash && !isHash(text) {
				continue
			}
			if p.kind == hintPath {
				if r, _ := utf8.DecodeLastRuneInString(line[:loc[0]]); loc[0] > 0 && isPathRune(r) {
					continue
				}
				text = strings.TrimRight(text, ".")
				if !strings.Contains(text, "/") && !strings.Contains(text, ":") {
					continue
				}
			}
			start := utf8.RuneCountInString(line[:loc[0]])
			spans = append(spans, hintSpan{start, start + utf8.RuneCountInString(text), p.kind})
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})
	out := spans[:0]
	end := -1
	for _, s := range spans {
		if s.start >= end && s.end > s.start {
			out = append(out, s)
			end = s.end
		}
	}
	return out
}

// collectHints scans the visible rows, joining soft-wrapped lines.
func collectHints(g *grid.Grid) []hintMatch {
	top := g.ViewTopAbs()
	var out []hintMatch
	var runes []rune
	var at []cellPos
	flush := func() {
		line := string(runes)
		for _, s := range findHintSpans(line) {
			out = append(out, hintMatch{
				Text: string(runes[s.start:s.end]), Kind: s.kind,
				Row: at[s.start].row, Col: at[s.start].col,
				EndRow: at[s.end-1].row, EndCol: at[s.end-1].col,
			})
		}
		runes, at = runes[:0], at[:0]
	}
	for row := 0; row < g.Rows; row++ {
		for col, c := range g.RowCells(top + row) {
			if c.Width == grid.CellWidthContinuation {
				continue
			}
			ch := c.Char
			if ch == 0 {
				ch = ' '
			}
			runes = append(runes, ch)
			at = append(at, cellPos{col: col, row: row})
		}
		if !g.RowSoftWrapped(top+row) || row == g.Rows-1 {
			flush()
		}
	}
	return out
}

const hintAlphabet = "asdfghjklqwertyuiopzxcvbnm"

// hintLabels returns n distinct, prefix-free labels of equal length.
func hintLabels(n int) []string {
	k := 1
	for total := len(hintAlphabet); total < n; total *= len(hintAlphabet) {
		k++
	}
	out := make([]string, 0, n)
	idx := make([]int, k)
	for len(out) < n {
		b := make([]byte, k)
		for i, v := range idx {
			b[i] = hintAlphabet[v]
		}
		out = append(out, string(b))
		for i := k - 1; i >= 0; i-- {
			if idx[i]++; idx[i] < len(hintAlphabet) {
				break
			}
			idx[i] = 0
		}
	}
	return out
}

// hintState is the hints overlay: matches, their labels and the typed prefix.
type hintState struct {
	pane    *tab.Pane
	matches []hintMatch
	labels  []string
	typed   string
	open    bool
}

func newHintState(pane *tab.Pane, matches []hintMatch) *hintState {
	labels := hintLabels(len(matches))
	hs := &hintState{pane: pane, matches: matches, labels: make([]string, len(matches))}
	for i := range matches {
		hs.labels[len(matches)-1-i] = labels[i]
	}
	return hs
}

// input takes one typed rune; Shift (an uppercase label) asks to open.
// Runes that continue no label are ignored.
func (hs *hintState) input(r rune) (hintMatch, bool, bool) {
	lower := unicode.ToLower(r)
	if !strings.ContainsRune(hintAlphabet, lower) {
		return hintMatch{}, false, false
	}
	next := hs.typed + string(lower)
	prefix := false
	for i, l := range hs.labels {
		if l == next {
			return hs.matches[i], hs.open || unicode.IsUpper(r), true
		}
		prefix = prefix || strings.HasPrefix(l, next)
	}
	if prefix {
		hs.typed = next
		hs.open = hs.open || unicode.IsUpper(r)
	}
	return hintMatch{}, false, false
}

func (hs *hintState) backspace() {
	if hs.typed != "" {
		hs.typed = hs.typed[:len(hs.typed)-1]
	}
}

var hintLineRef = regexp.MustCompile(`:\d+(?::\d+)?$`)

// hintOpenTarget is what Shift+label opens: the URL, or the path resolved
// against the pane's directory. Hashes and addresses are inserted instead.
func hintOpenTarget(m hintMatch, cwd string) string {
	switch m.Kind {
	case hintURL:
		return m.Text
	case hintPath:
		p := hintLineRef.ReplaceAllString(m.Text, "")
		if p == "~" || strings.HasPrefix(p, "~/") {
			home, _ := os.UserHomeDir()
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		} else if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		return filepath.Clean(p)
	}
	return ""
}
