package main

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/keybindings"
)

// paletteEntry is one command palette row: an action, a theme, a font or a
// setting (Apply changes the config, which is then saved and applied).
type paletteEntry struct {
	Label, Detail string
	Action        keybindings.KeyAction
	Theme, Font   string
	Apply         func(*config.Config)
}

func settingEntries() []paletteEntry {
	toggle := func(label string, field func(*config.Config) *bool) paletteEntry {
		return paletteEntry{Label: "Toggle " + label, Detail: "setting", Apply: func(c *config.Config) {
			f := field(c)
			*f = !*f
		}}
	}
	out := []paletteEntry{
		toggle("ligatures", func(c *config.Config) *bool { return &c.Appearance.Ligatures }),
		toggle("marks gutter", func(c *config.Config) *bool { return &c.Appearance.MarksGutter }),
		toggle("reduced motion", func(c *config.Config) *bool { return &c.Appearance.ReducedMotion }),
		toggle("cursor blink", func(c *config.Config) *bool { return &c.Appearance.CursorBlink }),
		toggle("desktop notifications", func(c *config.Config) *bool { return &c.Notifications }),
		toggle("clipboard read (OSC 52)", func(c *config.Config) *bool { return &c.AllowClipboardRead }),
	}
	for _, v := range []string{"visual", "attention", "none"} {
		out = append(out, paletteEntry{Label: "Bell: " + v, Detail: "setting", Apply: func(c *config.Config) { c.Bell = v }})
	}
	for _, v := range []string{"block", "underline", "bar"} {
		out = append(out, paletteEntry{Label: "Cursor style: " + v, Detail: "setting", Apply: func(c *config.Config) { c.Appearance.CursorStyle = v }})
	}
	for _, s := range []int{0, 10, 30, 60} {
		label := "off"
		if s > 0 {
			label = strconv.Itoa(s) + "s"
		}
		out = append(out, paletteEntry{Label: "Notify after commands: " + label, Detail: "setting", Apply: func(c *config.Config) { c.NotifyCommandAfter = s }})
	}
	for _, n := range []int{1000, 10000, 100000} {
		out = append(out, paletteEntry{Label: "Scrollback: " + strconv.Itoa(n) + " lines", Detail: "setting", Apply: func(c *config.Config) { c.ScrollbackLines = n }})
	}
	return out
}

func paletteEntries(themes []config.ThemeOption, fontList []fonts.FontInfo) []paletteEntry {
	var out []paletteEntry
	for _, a := range keybindings.Actions() {
		out = append(out, paletteEntry{Label: a.Name, Detail: a.Chord, Action: a.Action})
	}
	for _, t := range themes {
		out = append(out, paletteEntry{Label: "Theme: " + t.Label, Detail: "setting", Theme: t.Name})
	}
	for _, f := range fontList {
		name := f.DisplayName
		if name == "" {
			name = f.Name
		}
		out = append(out, paletteEntry{Label: "Font: " + name, Detail: "setting", Font: f.Name})
	}
	return append(out, settingEntries()...)
}

// fuzzyTerm scores one query term as a subsequence of text, trying every
// start so a contiguous word match beats an early scattered one.
func fuzzyTerm(term, text []rune) (int, bool) {
	best, found := 0, false
	for start := range text {
		if text[start] != term[0] {
			continue
		}
		score, ti, prev := 0, 0, -2
		for i := start; i < len(text) && ti < len(term); i++ {
			if text[i] != term[ti] {
				continue
			}
			score++
			if i == prev+1 {
				score += 5
			}
			if ti == 0 && (i == 0 || !unicode.IsLetter(text[i-1]) && !unicode.IsDigit(text[i-1])) {
				score += 8
			}
			prev = i
			ti++
		}
		if ti == len(term) && (!found || score > best) {
			best, found = score, true
		}
	}
	return best, found
}

// fuzzyScore matches every whitespace-separated query term, case-insensitively.
func fuzzyScore(query, text string) (int, bool) {
	t := []rune(strings.ToLower(text))
	total := 0
	for _, term := range strings.Fields(strings.ToLower(query)) {
		s, ok := fuzzyTerm([]rune(term), t)
		if !ok {
			return 0, false
		}
		total += s
	}
	return total, true
}

func filterPalette(entries []paletteEntry, query string) []paletteEntry {
	if strings.TrimSpace(query) == "" {
		return entries
	}
	type scored struct {
		e paletteEntry
		s int
	}
	var hits []scored
	for _, e := range entries {
		if s, ok := fuzzyScore(query, e.Label); ok {
			hits = append(hits, scored{e, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	out := make([]paletteEntry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out
}

type paletteState struct {
	open       bool
	query      string
	sel        int
	all, shown []paletteEntry
}

func (p *paletteState) openWith(entries []paletteEntry) {
	*p = paletteState{open: true, all: entries, shown: entries}
}

func (p *paletteState) refilter() {
	p.shown = filterPalette(p.all, p.query)
	p.sel = 0
}

func (p *paletteState) typeRune(r rune) {
	p.query += string(r)
	p.refilter()
}

func (p *paletteState) backspace() {
	if q := []rune(p.query); len(q) > 0 {
		p.query = string(q[:len(q)-1])
		p.refilter()
	}
}

func (p *paletteState) move(d int) {
	p.sel = min(max(p.sel+d, 0), max(len(p.shown)-1, 0))
}

func (p *paletteState) choose() (paletteEntry, bool) {
	if p.sel < 0 || p.sel >= len(p.shown) {
		return paletteEntry{}, false
	}
	return p.shown[p.sel], true
}
