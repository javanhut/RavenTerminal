package grid

// Style holds the visual attributes of a cell, separated from its content so
// that the many cells sharing the same attributes can reference a single
// interned copy. It is a comparable value type (usable as a map key).
type Style struct {
	Fg             Color
	Bg             Color
	UnderlineColor Color
	Flags          CellFlags
	UnderlineStyle uint8
}

// defaultStyle is the zero/default style (default fg+bg, no flags). It is never
// tracked in the styleSet — StyleID 0 always resolves to it.
var defaultStyle = Style{Fg: DefaultFg(), Bg: DefaultBg()}

// StyleID is a compact handle for an interned Style. ID 0 is reserved for the
// default style and is never reference-counted.
type StyleID uint32

// storedCell is the in-memory representation of a grid cell. It replaces the
// fat public Cell inside the active buffer: visual attributes are folded into a
// StyleID, shrinking the per-cell footprint. Grapheme indexes an (optional)
// combining-mark cluster (reserved here, populated in the grapheme phase).
type storedCell struct {
	Style    StyleID
	Grapheme uint32
	Char     rune
	Width    uint8
	Link     uint16
}

// blankStored is an empty cell with the default style. Matches the historical
// NewCell() (space, normal width) so display/clear semantics are unchanged.
var blankStored = storedCell{Char: ' ', Width: CellWidthNormal}

// styleEntry is one interned style plus its live reference count.
type styleEntry struct {
	style Style
	refs  uint32
}

// styleSet interns Styles into compact StyleIDs with reference counting, so a
// style's storage is freed once no cell references it. Mirrors the existing
// hyperlink interning. Not safe for concurrent use — callers hold Grid.mu.
type styleSet struct {
	byStyle map[Style]StyleID
	byID    map[StyleID]*styleEntry
	free    []StyleID // recycled ids
	nextID  StyleID
}

func newStyleSet() *styleSet {
	return &styleSet{
		byStyle: make(map[Style]StyleID),
		byID:    make(map[StyleID]*styleEntry),
		nextID:  0,
	}
}

// intern returns an id for st, taking ONE reference on the caller's behalf
// (the caller is expected to transfer it into a cell). The default style
// returns id 0 without tracking.
func (s *styleSet) intern(st Style) StyleID {
	if st == defaultStyle {
		return 0
	}
	if id, ok := s.byStyle[st]; ok {
		s.byID[id].refs++
		return id
	}
	var id StyleID
	if n := len(s.free); n > 0 {
		id = s.free[n-1]
		s.free = s.free[:n-1]
	} else {
		s.nextID++
		if s.nextID == 0 { // overflow guard (0 reserved)
			s.nextID = 1
		}
		id = s.nextID
	}
	s.byStyle[st] = id
	s.byID[id] = &styleEntry{style: st, refs: 1}
	return id
}

// styleRef is an interned style plus a direct pointer to its refcount entry.
// A run of cells sharing one style interns it ONCE and then takes a reference
// per cell through the pointer, so the per-cell cost is an increment instead of
// building a Style, comparing it against the default, and hashing it into two
// maps. entry is nil for the default style, which is never reference-counted.
type styleRef struct {
	style Style
	id    StyleID
	entry *styleEntry
}

// ref interns st and returns a handle holding one reference of its own. Pair it
// with done() when the run finishes, so a run that ends up writing no cells
// (all zero-width) doesn't strand the entry.
func (s *styleSet) ref(st Style) styleRef {
	id := s.intern(st)
	if id == 0 {
		return styleRef{style: st}
	}
	return styleRef{style: st, id: id, entry: s.byID[id]}
}

// take adds one reference on behalf of a cell about to store this style, and
// returns the id to store. This is the whole point of styleRef: no map access.
func (r *styleRef) take() StyleID {
	if r.entry != nil {
		r.entry.refs++
	}
	return r.id
}

// done releases the handle's own reference, freeing the style if the run wrote
// no cells that kept it alive.
func (s *styleSet) done(r styleRef) { s.release(r.id) }

// retain adds one reference to an existing id (no-op for the default style).
func (s *styleSet) retain(id StyleID) {
	if id == 0 {
		return
	}
	if e := s.byID[id]; e != nil {
		e.refs++
	}
}

// release drops one reference, freeing the id when it reaches zero.
func (s *styleSet) release(id StyleID) {
	if id == 0 {
		return
	}
	e := s.byID[id]
	if e == nil {
		return
	}
	e.refs--
	if e.refs == 0 {
		delete(s.byStyle, e.style)
		delete(s.byID, id)
		s.free = append(s.free, id)
	}
}

// resolve returns the Style for an id (default style for id 0 or unknown).
func (s *styleSet) resolve(id StyleID) Style {
	if id == 0 {
		return defaultStyle
	}
	if e := s.byID[id]; e != nil {
		return e.style
	}
	return defaultStyle
}

// liveCount reports how many distinct styles are currently interned. Test/debug.
func (s *styleSet) liveCount() int { return len(s.byID) }

// --- Grid <-> storedCell boundary -------------------------------------------

// styleOf extracts the Style attributes from a public Cell.
func styleOf(c Cell) Style {
	return Style{
		Fg:             c.Fg,
		Bg:             c.Bg,
		UnderlineColor: c.UnderlineColor,
		Flags:          c.Flags,
		UnderlineStyle: c.UnderlineStyle,
	}
}

// internGrapheme returns a compact id for a grapheme cluster (base rune plus
// combining/ZWJ codepoints), deduplicating repeats. A single-rune cluster
// returns 0 (meaning "use Cell.Char, no combining").
func (g *Grid) internGrapheme(cluster []rune) uint32 {
	if len(cluster) <= 1 {
		return 0
	}
	key := string(cluster)
	if g.graphemeMap == nil {
		g.graphemeMap = make(map[string]uint32)
		g.graphemes = make([][]rune, 1) // index 0 reserved for "none"
	}
	if id, ok := g.graphemeMap[key]; ok {
		return id
	}
	cp := make([]rune, len(cluster))
	copy(cp, cluster)
	id := uint32(len(g.graphemes))
	g.graphemes = append(g.graphemes, cp)
	g.graphemeMap[key] = id
	return id
}

// inflate resolves a storedCell back into the public Cell DTO. Callers outside
// the grid only ever see public Cells, so the interning is invisible to them.
func (g *Grid) inflate(sc storedCell) Cell {
	st := g.styles.resolve(sc.Style)
	c := Cell{
		Char:           sc.Char,
		Fg:             st.Fg,
		Bg:             st.Bg,
		UnderlineColor: st.UnderlineColor,
		Flags:          st.Flags,
		Width:          sc.Width,
		UnderlineStyle: st.UnderlineStyle,
		Link:           sc.Link,
	}
	if sc.Grapheme != 0 && int(sc.Grapheme) < len(g.graphemes) {
		if cl := g.graphemes[sc.Grapheme]; len(cl) > 0 {
			c.Char = cl[0]
			if len(cl) > 1 {
				c.Combining = cl[1:]
			}
		}
	}
	return c
}

// cellAt returns a pointer to the stored cell at a linear index. Linear indices
// (row*Cols+col, produced by index()) are decoded into the row-pointer buffer,
// so existing idx-based call sites keep working over the paged storage.
func (g *Grid) cellAt(idx int) *storedCell {
	return &g.rows[idx/g.Cols].cells[idx%g.Cols]
}

// putCell writes a public Cell at a linear index, maintaining style refcounts:
// the new style is interned (taking the cell's reference) and the previous
// cell's style is released. This is the single funnel for all cell writes.
func (g *Grid) putCell(idx int, c Cell) {
	p := g.cellAt(idx)
	old := p.Style
	var gid uint32
	if len(c.Combining) > 0 {
		cl := make([]rune, 0, 1+len(c.Combining))
		cl = append(cl, c.Char)
		cl = append(cl, c.Combining...)
		gid = g.internGrapheme(cl)
	}
	*p = storedCell{
		Style:    g.styles.intern(styleOf(c)),
		Grapheme: gid,
		Char:     c.Char,
		Width:    c.Width,
		Link:     c.Link,
	}
	g.styles.release(old)
	g.rows[idx/g.Cols].flags |= RowDirty
}

// putCellStyled writes a cell whose style has already been interned by the
// caller, who transfers one reference in via id. This is putCell's body minus
// the per-cell intern; the run-writing path uses it so the style is interned
// once per run rather than once per cell.
func (g *Grid) putCellStyled(idx int, char rune, id StyleID, width uint8, link uint16) {
	p := g.cellAt(idx)
	old := p.Style
	*p = storedCell{Style: id, Char: char, Width: width, Link: link}
	g.styles.release(old)
	g.rows[idx/g.Cols].flags |= RowDirty
}

// getCell reads and inflates the cell at a linear index.
func (g *Grid) getCell(idx int) Cell { return g.inflate(*g.cellAt(idx)) }

// moveCell moves the stored cell from src to dst, transferring style ownership
// (no intern/resolve churn) and leaving src blank. Used for shift loops within
// a row (whole-row moves use row-pointer rotation in page.go).
func (g *Grid) moveCell(dst, src int) {
	if dst == src {
		return
	}
	d := g.cellAt(dst)
	s := g.cellAt(src)
	g.styles.release(d.Style)
	*d = *s
	*s = blankStored
	g.rows[dst/g.Cols].flags |= RowDirty
	g.rows[src/g.Cols].flags |= RowDirty
}
