package render

import (
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
)

// newTestRenderer builds a Renderer with a real font face and fallback chain
// but no GL resources, for testing the glyph-resolution logic.
func newTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	r := &Renderer{
		fontSize:    15,
		currentFont: fonts.DefaultFontName(),
		glyphs:      make(map[rune]Glyph),
		glyphMisses: make(map[rune]bool),
	}
	parsed, err := opentype.Parse(fonts.DefaultFont())
	if err != nil {
		t.Fatalf("parse default font: %v", err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size: float64(r.fontSize), DPI: 96, Hinting: font.HintingFull,
	})
	if err != nil {
		t.Fatalf("create face: %v", err)
	}
	r.face = face
	metrics := face.Metrics()
	r.cellHeight = float32((metrics.Ascent + metrics.Descent).Ceil())
	advance, _ := face.GlyphAdvance('M')
	r.cellWidth = float32(advance.Ceil())
	r.atlasAscent = metrics.Ascent.Ceil()
	r.rebuildFallbackFaces()
	return r
}

// The nvim-tree symlink arrow (U+279B) is not in JetBrains Mono but is in the
// embedded Hack fallback; the chain must find it instead of degrading to '?'.
func TestFallbackChainCoversSymlinkArrow(t *testing.T) {
	r := newTestRenderer(t)
	for _, c := range []rune{'➛', '→', 'a'} {
		if r.faceFor(c) == nil {
			t.Errorf("faceFor(%q) = nil, want a fallback face", string(c))
		}
	}
	// The primary face must win when it covers the rune.
	if got := r.faceFor('a'); got != r.face {
		t.Errorf("faceFor('a') did not return the primary face")
	}
	if r.faceFor('➛') == r.face {
		t.Errorf("faceFor('➛') returned the primary face, which lacks the glyph")
	}
}

// Every unicodeFallbacks target must itself be renderable — an ASCII byte or a
// rune the fallback chain covers. A substitution that points at another missing
// glyph would still degrade to '?'. Guards the ⏵/⏴ (Claude Code auto-mode)
// entries, which map to ▶/◀ rather than plain '>'/'<'.
func TestUnicodeFallbackTargetsRenderable(t *testing.T) {
	r := newTestRenderer(t)
	for src, dst := range unicodeFallbacks {
		if dst < 128 {
			continue // ASCII always renders
		}
		if r.faceFor(dst) == nil {
			t.Errorf("unicodeFallbacks[%q]=%q has no covering face; would render as '?'", string(src), string(dst))
		}
	}
}

func TestRuneClassification(t *testing.T) {
	cases := []struct {
		c         rune
		icon      bool
		cellExact bool
	}{
		{0xE5FF, true, false},  // nvim-tree folder icon (PUA)
		{0xF0219, true, false}, // Nerd Fonts 3 material icon (PUA-A)
		{'➛', true, false},     // dingbat arrow
		{0xE0B0, false, true},  // powerline separator
		{'─', false, true},     // box drawing
		{'a', false, false},
		{'→', false, false}, // plain arrows render as text
	}
	for _, tc := range cases {
		if got := isIconRune(tc.c); got != tc.icon {
			t.Errorf("isIconRune(%#x) = %v, want %v", tc.c, got, tc.icon)
		}
		if got := isCellExact(tc.c); got != tc.cellExact {
			t.Errorf("isCellExact(%#x) = %v, want %v", tc.c, got, tc.cellExact)
		}
	}
}

func TestGlyphQuadIconConstraint(t *testing.T) {
	r := &Renderer{cellWidth: 10, cellHeight: 20, atlasAscent: 16}
	icon := rune(0xE5FF)

	// A 1.8-cell-wide icon with a blank cell after it (span 2) keeps its
	// natural size and is centered across the two cells.
	g := Glyph{PixelWidth: 18, PixelHeight: 16}
	x, yTop, w, h := r.glyphQuad(icon, g, 100, 50, 2)
	if w != 18 || h != 16 {
		t.Errorf("span-2 icon was scaled: w=%v h=%v, want 18x16", w, h)
	}
	if x != 101 { // 100 + (2*10-18)/2
		t.Errorf("span-2 icon x = %v, want 101", x)
	}
	if yTop != 52 { // 50 + (20-16)/2
		t.Errorf("span-2 icon yTop = %v, want 52", yTop)
	}

	// The same icon with no blank cell available (span 1) must scale down to
	// fit one cell instead of being clipped or overlapping the next glyph.
	x, _, w, h = r.glyphQuad(icon, g, 100, 50, 1)
	if w > r.cellWidth {
		t.Errorf("span-1 icon width %v exceeds cell width %v", w, r.cellWidth)
	}
	if x < 100 || x+w > 110 {
		t.Errorf("span-1 icon [%v,%v] not within its cell [100,110]", x, x+w)
	}

	// A one-cell icon in a 2-cell span stays centered in its own cell.
	g = Glyph{PixelWidth: 8, PixelHeight: 14}
	x, _, w, _ = r.glyphQuad(icon, g, 100, 50, 2)
	if x != 101 { // 100 + (10-8)/2
		t.Errorf("small icon x = %v, want 101 (centered in own cell)", x)
	}
	if w != 8 {
		t.Errorf("small icon was scaled: w=%v, want 8", w)
	}

	// Regular text uses natural bearings, no constraint.
	g = Glyph{PixelWidth: 9, PixelHeight: 12, OffsetX: 1, OffsetY: -11}
	x, yTop, w, h = r.glyphQuad('a', g, 100, 50, 1)
	if x != 101 || yTop != 55 || w != 9 || h != 12 { // yTop = 50+16-11
		t.Errorf("text quad = (%v,%v,%v,%v), want (101,55,9,12)", x, yTop, w, h)
	}
}
