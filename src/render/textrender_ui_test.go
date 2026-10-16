package render

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func uiQuads(r *Renderer) [][]float32 {
	var q [][]float32
	v := r.uiGlyphs.verts
	for i := 0; i+6*glyphVertFloats <= len(v); i += 6 * glyphVertFloats {
		q = append(q, append([]float32(nil), v[i:i+6*glyphVertFloats]...))
	}
	return q
}

// Panel text goes through the grid's shaping path: ligatures come out as
// the same quads the grid draws for those characters.
func TestUITextShapesLigatures(t *testing.T) {
	r := newFontRenderer(t, "firacode")
	want := buildRow(r, oneRow(cellOf('-', 0), cellOf('>', 0)))
	var proj [16]float32
	r.uiGlyphs.reset()
	r.drawText(0, r.cellHeight, "->", r.defaultFg(), proj)
	got := uiQuads(r)
	if len(got) != len(want) {
		t.Fatalf("panel text drew %d quads, grid %d", len(got), len(want))
	}
	for i := range got {
		gx0, gx1, gu0, gu1 := quadX(got[i])
		wx0, wx1, wu0, wu1 := quadX(want[i])
		if gx0 != wx0 || gx1 != wx1 || gu0 != wu0 || gu1 != wu1 {
			t.Errorf("quad %d = [%v,%v] u[%v,%v], grid [%v,%v] u[%v,%v]", i, gx0, gx1, gu0, gu1, wx0, wx1, wu0, wu1)
		}
	}
}

// A base plus combining mark is one cluster: both glyphs drawn, one cell.
func TestUITextKeepsClusters(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	var proj [16]float32
	r.uiGlyphs.reset()
	r.drawText(0, r.cellHeight, "q́a", r.defaultFg(), proj)
	q := uiQuads(r)
	if len(q) != 3 {
		t.Fatalf("drew %d quads, want q, acute, a", len(q))
	}
	lone := buildRow(r, oneRow(cellOf('a', 0)))
	x0, _, _, _ := quadX(q[2])
	lx0, _, _, _ := quadX(lone[0])
	if x0 != lx0+r.cellWidth {
		t.Errorf("'a' after the cluster at x=%v, want one cell in (%v)", x0, lx0+r.cellWidth)
	}
	if n := textCells("q́a"); n != 2 {
		t.Errorf("textCells = %d, want 2", n)
	}
}

func TestUITextEmojiSequence(t *testing.T) {
	r := newEmojiRenderer(t)
	family := "\U0001F468‍\U0001F469‍\U0001F467"
	if n := textCells(family + "a"); n != 3 {
		t.Errorf("textCells(family+a) = %d, want 3", n)
	}
	var proj [16]float32
	r.uiGlyphs.reset()
	r.drawText(0, r.cellHeight, family+"a", r.defaultFg(), proj)
	q := uiQuads(r)
	if len(q) != 2 || q[0][8] >= 0 || q[1][8] < 0 {
		t.Fatalf("drew %d quads, want one colour family and the 'a'", len(q))
	}
	lone, _, _, _ := quadX(buildRow(r, oneRow(cellOf('a', 0)))[0])
	if x0, _, _, _ := quadX(q[1]); x0 != lone+2*r.cellWidth {
		t.Errorf("'a' at x=%v, want two cells in (%v)", x0, lone+2*r.cellWidth)
	}
}

// Bold panel text keeps the bold face even when terminal faux bold is off.
func TestUITextBold(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	r.fauxBold = false
	var proj [16]float32
	r.uiGlyphs.reset()
	r.drawTextStyled(0, r.cellHeight, "a", r.defaultFg(), true, proj)
	bold := uiQuads(r)
	r.uiGlyphs.reset()
	r.drawText(0, r.cellHeight, "a", r.defaultFg(), proj)
	reg := uiQuads(r)
	if len(bold) != 1 || len(reg) != 1 || bold[0][2] == reg[0][2] && bold[0][3] == reg[0][3] {
		t.Errorf("bold panel text drew the regular glyph")
	}
	if r.fauxBold {
		t.Errorf("drawing panel text changed the faux-bold setting")
	}
}

// Tab bar labels (scaled text) are shaped too.
func TestUIScaledTextShapes(t *testing.T) {
	r := newFontRenderer(t, "firacode")
	var proj [16]float32
	r.uiGlyphs.reset()
	r.drawTextScaled(0, r.cellHeight, "->", r.defaultFg(), proj, 1)
	got := uiQuads(r)
	want := buildRow(r, oneRow(cellOf('-', 0), cellOf('>', 0)))
	if len(got) != len(want) || got[0][2] != want[0][2] {
		t.Fatalf("scaled text drew %d quads, want the grid's %d ligature quads", len(got), len(want))
	}
	r.uiGlyphs.reset()
	r.drawTextScaled(0, 2*r.cellHeight, "->", r.defaultFg(), proj, 2)
	x0, x1, _, _ := quadX(uiQuads(r)[0])
	wx0, wx1, _, _ := quadX(want[0])
	if !approx(x0, 2*wx0) || !approx(x1, 2*wx1) {
		t.Errorf("2x quad [%v,%v], want [%v,%v]", x0, x1, 2*wx0, 2*wx1)
	}
}

func clustersOf(s string) []string {
	var out []string
	for state := -1; s != ""; {
		var c string
		c, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		out = append(out, c)
	}
	return out
}

// Truncation measures and cuts whole grapheme clusters, as they are drawn.
func TestTruncationKeepsClusters(t *testing.T) {
	family := "\U0001F468‍\U0001F469‍\U0001F467"
	s := "ab" + family + "é日本x"
	if n := textCells(s); n != 10 {
		t.Fatalf("textCells = %d, want 10", n)
	}
	all := clustersOf(s)
	whole := func(part string, tail bool) bool {
		p := clustersOf(part)
		if len(p) > len(all) {
			return false
		}
		off := 0
		if tail {
			off = len(all) - len(p)
		}
		for i := range p {
			if p[i] != all[off+i] {
				return false
			}
		}
		return true
	}
	for limit := 1; limit <= 11; limit++ {
		got := truncateToCells(s, limit)
		if textCells(got) > limit || !whole(strings.TrimSuffix(got, "..."), false) {
			t.Errorf("truncateToCells(%d) = %q", limit, got)
		}
		if limit >= 4 {
			got = truncateHeadToCells(s, limit)
			if textCells(got) > limit || !whole(strings.TrimPrefix(got, "..."), true) {
				t.Errorf("truncateHeadToCells(%d) = %q", limit, got)
			}
		}
		got = truncateToWidth(s, float32(limit)*10, 10)
		if textCells(got) > limit || !whole(strings.TrimSuffix(got, "…"), false) {
			t.Errorf("truncateToWidth(%d cells) = %q", limit, got)
		}
	}
}

// Preedit caret and width count grapheme clusters: a decomposed é is one
// cell, and a caret inside a cluster moves to its end.
func TestPreeditLayoutClusters(t *testing.T) {
	text := "xééz"
	if _, cells, caret := preeditLayout(text, 4, 0, 80); cells != 4 || caret != 2 {
		t.Fatalf("cells %d caret %d, want 4 cells and the caret after the first é (2)", cells, caret)
	}
	if _, _, caret := preeditLayout(text, 2, 0, 80); caret != 2 {
		t.Fatalf("caret inside e+acute at cell %d, want the cluster's end (2)", caret)
	}
	family := "\U0001F468‍\U0001F469‍\U0001F467"
	if _, cells, caret := preeditLayout(family+"a", len(family), 0, 80); cells != 3 || caret != 2 {
		t.Fatalf("ZWJ family: cells %d caret %d, want 3 and 2", cells, caret)
	}
}

// The preedit draws through the shaping path: the acute is one quad with
// its base, and the 'z' lands one cell after the cluster.
func TestPreeditTextShaped(t *testing.T) {
	r := newFontRenderer(t, "jetbrains")
	var proj [16]float32
	r.uiGlyphs.reset()
	r.drawPreeditText(0, 0, "q\u0301z", r.defaultFg(), proj)
	q := uiQuads(r)
	if len(q) != 3 {
		t.Fatalf("drew %d quads, want q, acute, z", len(q))
	}
	lone, _, _, _ := quadX(buildRow(r, oneRow(cellOf('z', 0)))[0])
	if x0, _, _, _ := quadX(q[2]); x0 != lone+r.cellWidth {
		t.Errorf("'z' at x=%v, want one cell in (%v)", x0, lone+r.cellWidth)
	}
}

// Hint labels are shaped UI text too; typed characters are dimmed.
func TestHintLabelShaped(t *testing.T) {
	r := newFontRenderer(t, "firacode")
	var proj [16]float32
	r.uiGlyphs.reset()
	r.drawHintLabel(0, 0, "->", 0, proj)
	got := uiQuads(r)
	want := buildRow(r, oneRow(cellOf('-', 0), cellOf('>', 0)))
	if len(got) != len(want) {
		t.Fatalf("label drew %d quads, grid %d", len(got), len(want))
	}
	for i := range got {
		gx0, _, gu0, _ := quadX(got[i])
		wx0, _, wu0, _ := quadX(want[i])
		if gx0 != wx0 || gu0 != wu0 {
			t.Errorf("quad %d at x=%v u=%v, grid x=%v u=%v", i, gx0, gu0, wx0, wu0)
		}
	}
	r.uiGlyphs.reset()
	r.drawHintLabel(0, 0, "jk", 1, proj)
	got = uiQuads(r)
	if len(got) != 2 {
		t.Fatalf("label jk drew %d quads", len(got))
	}
	if a0, a1 := got[0][7], got[1][7]; a0 >= a1 {
		t.Errorf("typed part alpha %v, rest %v: typed must be dimmer", a0, a1)
	}
	if x0, _, _, _ := quadX(got[1]); x0 < r.cellWidth {
		t.Errorf("untyped 'k' at x=%v, want in the second cell", x0)
	}
}
