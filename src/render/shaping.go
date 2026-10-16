package render

import (
	"bytes"
	"container/list"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// Text shaping (HarfBuzz via go-text) for ligatures and grapheme clusters.
// The grid stays strictly monospace: every shaped glyph is anchored to the
// cell its cluster starts in, so a ligature's glyphs span exactly the cells of
// the characters it replaces.

const (
	shapeCacheSize   = 4096
	clusterCacheSize = 1024
)

// lruCache is a small bounded LRU keyed by string.
type lruCache[V any] struct {
	cap int
	m   map[string]*list.Element
	l   *list.List
}

type lruEntry[V any] struct {
	key string
	val V
}

func newLRU[V any](capacity int) *lruCache[V] {
	return &lruCache[V]{cap: capacity, m: make(map[string]*list.Element), l: list.New()}
}

func newShapeCache(capacity int) *lruCache[[]shapedGlyph] { return newLRU[[]shapedGlyph](capacity) }

func (c *lruCache[V]) get(key string) (V, bool) {
	if e, ok := c.m[key]; ok {
		c.l.MoveToFront(e)
		return e.Value.(*lruEntry[V]).val, true
	}
	var zero V
	return zero, false
}

// getBytes is get without allocating a key string.
func (c *lruCache[V]) getBytes(key []byte) (V, bool) {
	if e, ok := c.m[string(key)]; ok {
		c.l.MoveToFront(e)
		return e.Value.(*lruEntry[V]).val, true
	}
	var zero V
	return zero, false
}

func (c *lruCache[V]) put(key string, v V) {
	if e, ok := c.m[key]; ok {
		e.Value.(*lruEntry[V]).val = v
		c.l.MoveToFront(e)
		return
	}
	c.m[key] = c.l.PushFront(&lruEntry[V]{key, v})
	for c.l.Len() > c.cap {
		old := c.l.Back()
		c.l.Remove(old)
		delete(c.m, old.Value.(*lruEntry[V]).key)
	}
}

func (c *lruCache[V]) len() int { return c.l.Len() }

func (c *lruCache[V]) reset() {
	clear(c.m)
	c.l.Init()
}

// shapedGlyph is one shaped glyph anchored to cell (index into the shaped
// text); dx/dy offset the pen from the cell origin on the baseline (y down).
type shapedGlyph struct {
	gid    uint32
	cell   int
	dx, dy float32
}

// shapeFace lazily parses a font for the shaper.
type shapeFace struct {
	face  *gtfont.Face
	tried bool
}

func (s *fontSrc) shapeFace() *gtfont.Face {
	if s == nil || s.data == nil {
		return nil
	}
	if !s.shaper.tried {
		s.shaper.tried = true
		if s.ttc {
			if faces, err := gtfont.ParseTTC(bytes.NewReader(s.data)); err == nil && len(faces) > 0 {
				s.shaper.face = faces[0]
			}
		} else if f, err := gtfont.ParseTTF(bytes.NewReader(s.data)); err == nil {
			s.shaper.face = f
		}
	}
	return s.shaper.face
}

// shapeRunes shapes text with face at ppem and anchors each glyph to the cell
// of its cluster.
func (r *Renderer) shapeRunes(face *gtfont.Face, text []rune, ppem fixed.Int26_6) []shapedGlyph {
	script := language.Common
	for _, c := range text {
		if s := language.LookupScript(c); s != language.Common && s != language.Inherited {
			script = s
			break
		}
	}
	out := r.shaper.Shape(shaping.Input{
		Text:      text,
		RunStart:  0,
		RunEnd:    len(text),
		Direction: di.DirectionLTR,
		Face:      face,
		Size:      ppem,
		Script:    script,
		Language:  language.NewLanguage("en"),
	})
	res := make([]shapedGlyph, 0, len(out.Glyphs))
	cluster, pen := -1, fixed.Int26_6(0)
	for _, g := range out.Glyphs {
		if g.ClusterIndex != cluster {
			cluster, pen = g.ClusterIndex, 0
		}
		res = append(res, shapedGlyph{
			gid:  uint32(g.GlyphID),
			cell: g.ClusterIndex,
			dx:   float32(pen+g.XOffset) / 64,
			dy:   -float32(g.YOffset) / 64,
		})
		pen += g.Advance
	}
	return res
}

// shapeWord shapes a run of single-cell ASCII characters in one style and
// returns its glyphs, or nil when shaping changes nothing (no ligature), so
// the caller draws nominal glyphs. Results are cached per (style, text).
func (r *Renderer) shapeWord(style uint8, text []rune) []shapedGlyph {
	if !r.ligatures || len(text) < 2 || !r.styleHasLigatures(style) {
		return nil
	}
	return r.shapeWordCached(style, text)
}

func (r *Renderer) shapeWordCached(style uint8, text []rune) []shapedGlyph {
	if r.wordShapes == nil {
		r.wordShapes = newShapeCache(shapeCacheSize)
	}
	key := append(r.keyBuf[:0], style)
	for _, c := range text {
		key = utf8.AppendRune(key, c)
	}
	r.keyBuf = key
	if v, ok := r.wordShapes.getBytes(key); ok {
		return v
	}
	r.shapeCalls++
	src, _ := r.styleFace(style)
	var out []shapedGlyph
	if face := src.shapeFace(); face != nil {
		out = r.shapeRunes(face, text, r.ppem())
		if len(out) == len(text) {
			same := true
			for i, g := range out {
				if g.cell != i || g.dx != 0 || g.dy != 0 || g.gid != uint32(src.gid(text[i])) {
					same = false
					break
				}
			}
			if same {
				out = nil
			}
		}
	}
	r.wordShapes.put(string(key), out)
	return out
}

var ligatureProbes = []string{"->", "!=", "==", "<=", "=>", "::", "&&", "||", "/*", "++", "</", "www", "<!--"}

// styleHasLigatures reports (and caches) whether a style's face substitutes
// any common programming ligature; faces without them skip shaping entirely.
func (r *Renderer) styleHasLigatures(style uint8) bool {
	if r.ligProbe[style] == 0 {
		r.ligProbe[style] = 1
		for _, p := range ligatureProbes {
			if r.shapeWordCached(style, []rune(p)) != nil {
				r.ligProbe[style] = 2
				break
			}
		}
	}
	return r.ligProbe[style] == 2
}
