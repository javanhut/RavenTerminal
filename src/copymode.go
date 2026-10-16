package main

import (
	"unicode"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/tab"
)

type visualKind int

const (
	visualNone visualKind = iota
	visualChar
	visualLine
	visualBlock
)

// copyMode is a vi-style cursor over a pane's scrollback.
type copyMode struct {
	g                    *grid.Grid
	pane                 *tab.Pane
	abs, col             int
	visual               visualKind
	anchorAbs, anchorCol int
	pendingG             bool
	searching, back      bool
	query, lastQuery     string
	lastBack             bool
}

// copyResult tells the caller to leave copy mode and what was yanked.
type copyResult struct {
	exit bool
	yank string
}

func newCopyMode(g *grid.Grid) *copyMode {
	col, _ := g.GetCursor()
	c := &copyMode{g: g, abs: g.AbsoluteCursorRow(), col: col}
	c.clamp()
	return c
}

func (c *copyMode) bounds() (int, int) { return c.g.AbsBounds() }

func (c *copyMode) runes(abs int) []rune {
	cells := c.g.RowCells(abs)
	out := make([]rune, c.g.Cols)
	for i := range out {
		out[i] = ' '
	}
	for i, cell := range cells {
		if i >= len(out) {
			break
		}
		switch {
		case cell.Width == grid.CellWidthContinuation:
			out[i] = 0
		case cell.Char != 0:
			out[i] = cell.Char
		}
	}
	return out
}

func (c *copyMode) clamp() {
	first, last := c.bounds()
	c.abs = min(max(c.abs, first), last)
	c.col = min(max(c.col, 0), c.g.Cols-1)
	for c.col > 0 && c.runes(c.abs)[c.col] == 0 {
		c.col--
	}
}

// sync keeps the cursor on screen and the selection in step with visual mode.
func (c *copyMode) sync() {
	c.clamp()
	top := c.g.ViewTopAbs()
	if c.abs < top {
		c.g.ScrollViewToAbs(c.abs)
	} else if c.abs >= top+c.g.Rows {
		c.g.ScrollViewToAbs(c.abs - c.g.Rows + 1)
	}
	switch c.visual {
	case visualNone:
		c.g.ClearSelection()
	case visualChar:
		c.g.SetSelectionAbs(c.anchorAbs, c.anchorCol, c.abs, c.col, false)
	case visualLine:
		c.g.SetSelectionAbs(min(c.anchorAbs, c.abs), 0, max(c.anchorAbs, c.abs), c.g.Cols-1, false)
	case visualBlock:
		c.g.SetSelectionAbs(c.anchorAbs, c.anchorCol, c.abs, c.col, true)
	}
}

// viewRow is the cursor's viewport row, or -1 when off screen.
func (c *copyMode) viewRow() int {
	r := c.abs - c.g.ViewTopAbs()
	if r < 0 || r >= c.g.Rows {
		return -1
	}
	return r
}

func runeClass(r rune) int {
	switch {
	case r == ' ' || r == 0 || r == '\t':
		return 0
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return 1
	}
	return 2
}

type cpos struct{ abs, col int }

func (c *copyMode) step(p cpos, dir int) (cpos, bool, bool) {
	first, last := c.bounds()
	for {
		p.col += dir
		crossed := false
		if p.col >= c.g.Cols {
			if p.abs >= last {
				return p, false, false
			}
			p.abs, p.col, crossed = p.abs+1, 0, true
		} else if p.col < 0 {
			if p.abs <= first {
				return p, false, false
			}
			p.abs, p.col, crossed = p.abs-1, c.g.Cols-1, true
		}
		if c.runes(p.abs)[p.col] != 0 || crossed {
			return p, true, crossed
		}
	}
}

func (c *copyMode) class(p cpos) int { return runeClass(c.runes(p.abs)[p.col]) }

func (c *copyMode) wordForward() {
	p := cpos{c.abs, c.col}
	cls := c.class(p)
	for {
		n, ok, crossed := c.step(p, 1)
		if !ok {
			break
		}
		p = n
		if crossed || c.class(p) != cls {
			break
		}
	}
	for c.class(p) == 0 {
		n, ok, _ := c.step(p, 1)
		if !ok {
			break
		}
		p = n
	}
	c.abs, c.col = p.abs, p.col
}

func (c *copyMode) wordEnd() {
	p, ok, _ := c.step(cpos{c.abs, c.col}, 1)
	if !ok {
		return
	}
	for c.class(p) == 0 {
		if p, ok, _ = c.step(p, 1); !ok {
			return
		}
	}
	cls := c.class(p)
	for {
		n, ok, crossed := c.step(p, 1)
		if !ok || crossed || c.class(n) != cls {
			break
		}
		p = n
	}
	c.abs, c.col = p.abs, p.col
}

func (c *copyMode) wordBack() {
	p, ok, _ := c.step(cpos{c.abs, c.col}, -1)
	if !ok {
		return
	}
	for c.class(p) == 0 {
		if p, ok, _ = c.step(p, -1); !ok {
			return
		}
	}
	cls := c.class(p)
	for {
		n, ok, crossed := c.step(p, -1)
		if !ok || crossed || c.class(n) != cls {
			break
		}
		p = n
	}
	c.abs, c.col = p.abs, p.col
}

func (c *copyMode) lineEnd() {
	rs := c.runes(c.abs)
	c.col = 0
	for i := len(rs) - 1; i >= 0; i-- {
		if rs[i] != ' ' && rs[i] != 0 {
			c.col = i
			return
		}
	}
}

func (c *copyMode) firstNonBlank() {
	for i, r := range c.runes(c.abs) {
		if r != ' ' && r != 0 {
			c.col = i
			return
		}
	}
	c.col = 0
}

// search jumps to the next match of q after (or before, when back) the cursor,
// wrapping around the buffer.
func (c *copyMode) search(q string, back bool) {
	if q == "" {
		return
	}
	c.lastQuery, c.lastBack = q, back
	ms := c.g.Search(q)
	if len(ms) == 0 {
		return
	}
	after := func(m grid.Match) bool {
		return m.StartAbsRow > c.abs || (m.StartAbsRow == c.abs && m.StartCol > c.col)
	}
	pick := ms[0]
	if back {
		pick = ms[len(ms)-1]
		for i := len(ms) - 1; i >= 0; i-- {
			if m := ms[i]; m.StartAbsRow < c.abs || (m.StartAbsRow == c.abs && m.StartCol < c.col) {
				pick = m
				break
			}
		}
	} else {
		for _, m := range ms {
			if after(m) {
				pick = m
				break
			}
		}
	}
	c.abs, c.col = pick.StartAbsRow, pick.StartCol
}

func (c *copyMode) toggleVisual(v visualKind) {
	if c.visual == v {
		c.visual = visualNone
		return
	}
	if c.visual == visualNone {
		c.anchorAbs, c.anchorCol = c.abs, c.col
	}
	c.visual = v
}

func (c *copyMode) yank() copyResult {
	var text string
	if c.visual == visualNone {
		text = c.g.TextRange(c.abs, 0, c.abs, c.g.Cols-1, false)
	} else {
		text = c.g.SelectedText()
	}
	c.leave()
	return copyResult{exit: true, yank: text}
}

func (c *copyMode) leave() {
	c.visual = visualNone
	c.g.ClearSelection()
	c.g.ResetScrollOffset()
}

// rune handles a typed character.
func (c *copyMode) rune(r rune) copyResult {
	if c.searching {
		c.query += string(r)
		return copyResult{}
	}
	if c.pendingG {
		c.pendingG = false
		if r == 'g' {
			c.abs, _ = c.bounds()
			c.col = 0
		}
		c.sync()
		return copyResult{}
	}
	half := max(c.g.Rows/2, 1)
	switch r {
	case 'h':
		if p, ok, crossed := c.step(cpos{c.abs, c.col}, -1); ok && !crossed {
			c.col = p.col
		}
	case 'l':
		if p, ok, crossed := c.step(cpos{c.abs, c.col}, 1); ok && !crossed {
			c.col = p.col
		}
	case 'j':
		c.abs++
	case 'k':
		c.abs--
	case 'w', 'W':
		c.wordForward()
	case 'b', 'B':
		c.wordBack()
	case 'e', 'E':
		c.wordEnd()
	case '0':
		c.col = 0
	case '^':
		c.firstNonBlank()
	case '$':
		c.lineEnd()
	case 'g':
		c.pendingG = true
		return copyResult{}
	case 'G':
		_, c.abs = c.bounds()
	case 'H':
		c.abs = c.g.ViewTopAbs()
	case 'M':
		c.abs = c.g.ViewTopAbs() + c.g.Rows/2
	case 'L':
		c.abs = c.g.ViewTopAbs() + c.g.Rows - 1
	case 'u':
		c.abs -= half
	case 'd':
		c.abs += half
	case '/', '?':
		c.searching, c.back, c.query = true, r == '?', ""
		return copyResult{}
	case 'n':
		c.search(c.lastQuery, c.lastBack)
	case 'N':
		c.search(c.lastQuery, !c.lastBack)
	case 'v':
		c.toggleVisual(visualChar)
	case 'V':
		c.toggleVisual(visualLine)
	case 'y':
		return c.yank()
	case 'q':
		c.leave()
		return copyResult{exit: true}
	default:
		return copyResult{}
	}
	c.sync()
	return copyResult{}
}

// key handles keys that do not arrive as text: Escape, Enter, arrows and Ctrl chords.
func (c *copyMode) key(k glfw.Key, mods glfw.ModifierKey) copyResult {
	if c.searching {
		switch k {
		case glfw.KeyEscape:
			c.searching = false
		case glfw.KeyEnter, glfw.KeyKPEnter:
			c.searching = false
			c.search(c.query, c.back)
			c.sync()
		case glfw.KeyBackspace:
			if q := []rune(c.query); len(q) > 0 {
				c.query = string(q[:len(q)-1])
			}
		}
		return copyResult{}
	}
	if mods&glfw.ModControl != 0 {
		page := max(c.g.Rows-1, 1)
		switch k {
		case glfw.KeyU:
			c.abs -= max(c.g.Rows/2, 1)
		case glfw.KeyD:
			c.abs += max(c.g.Rows/2, 1)
		case glfw.KeyB:
			c.abs -= page
		case glfw.KeyF:
			c.abs += page
		case glfw.KeyV:
			c.toggleVisual(visualBlock)
		case glfw.KeyC:
			c.leave()
			return copyResult{exit: true}
		default:
			return copyResult{}
		}
		c.sync()
		return copyResult{}
	}
	switch k {
	case glfw.KeyEscape:
		if c.visual != visualNone {
			c.visual = visualNone
			c.sync()
			return copyResult{}
		}
		c.leave()
		return copyResult{exit: true}
	case glfw.KeyEnter, glfw.KeyKPEnter:
		return c.yank()
	case glfw.KeyLeft:
		return c.rune('h')
	case glfw.KeyRight:
		return c.rune('l')
	case glfw.KeyUp:
		return c.rune('k')
	case glfw.KeyDown:
		return c.rune('j')
	case glfw.KeyHome:
		return c.rune('0')
	case glfw.KeyEnd:
		return c.rune('$')
	case glfw.KeyPageUp:
		return c.key(glfw.KeyB, glfw.ModControl)
	case glfw.KeyPageDown:
		return c.key(glfw.KeyF, glfw.ModControl)
	}
	return copyResult{}
}

// status is the mode line shown under the copy cursor.
func (c *copyMode) status() string {
	switch {
	case c.searching && c.back:
		return "?" + c.query
	case c.searching:
		return "/" + c.query
	case c.visual == visualChar:
		return "-- VISUAL --"
	case c.visual == visualLine:
		return "-- VISUAL LINE --"
	case c.visual == visualBlock:
		return "-- VISUAL BLOCK --"
	}
	return "-- COPY --"
}
