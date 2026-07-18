package render

import (
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
)

// Font fallback chain, modeled on Ghostty's font cascade: when the active font
// has no glyph for a codepoint, the renderer tries the other embedded Nerd
// Fonts, then (lazily) a small set of system fonts with broad symbol/CJK
// coverage, before resorting to the substitution tables and the final '?'.

// fallbackFont is a parsed font available for glyph fallback. Fonts are parsed
// once; faces are rebuilt from them whenever the font size changes.
type fallbackFont struct {
	name string // embedded font name ("" for system fonts), used to skip the active font
	font *opentype.Font
}

// initEmbeddedFallbacks parses all embedded fonts once. Called lazily from
// rebuildFallbackFaces.
func (r *Renderer) initEmbeddedFallbacks() {
	if r.fallbackFonts != nil {
		return
	}
	r.fallbackFonts = make([]fallbackFont, 0, 8)
	for _, fi := range fonts.AvailableFonts() {
		f, err := opentype.Parse(fi.Data)
		if err != nil {
			continue
		}
		r.fallbackFonts = append(r.fallbackFonts, fallbackFont{name: fi.Name, font: f})
	}
}

// rebuildFallbackFaces (re)creates fallback faces at the current font size,
// skipping the active font (it is always tried first via r.face).
func (r *Renderer) rebuildFallbackFaces() {
	r.initEmbeddedFallbacks()
	for _, f := range r.fallbackFaces {
		f.Close()
	}
	r.fallbackFaces = r.fallbackFaces[:0]
	for _, ff := range r.fallbackFonts {
		if ff.name != "" && ff.name == r.currentFont {
			continue
		}
		face, err := opentype.NewFace(ff.font, &opentype.FaceOptions{
			Size:    float64(r.fontSize),
			DPI:     r.fontDPI(),
			Hinting: font.HintingFull,
		})
		if err != nil {
			continue
		}
		r.fallbackFaces = append(r.fallbackFaces, face)
	}
}

// loadSystemFallbacks parses system fonts with broad coverage and appends them
// to the fallback chain. Called at most once, on the first glyph that misses
// every embedded font, so the cost (parsing a few MB of fonts) is only paid
// when actually needed.
func (r *Renderer) loadSystemFallbacks() {
	if r.systemFallbacksLoaded {
		return
	}
	r.systemFallbacksLoaded = true
	for _, path := range systemFallbackPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var parsed []*opentype.Font
		if filepath.Ext(path) == ".ttc" {
			coll, err := opentype.ParseCollection(data)
			if err != nil {
				continue
			}
			// The first face of a collection is the regular weight.
			f, err := coll.Font(0)
			if err != nil {
				continue
			}
			parsed = append(parsed, f)
		} else {
			f, err := opentype.Parse(data)
			if err != nil {
				continue
			}
			parsed = append(parsed, f)
		}
		for _, f := range parsed {
			r.fallbackFonts = append(r.fallbackFonts, fallbackFont{font: f})
			face, err := opentype.NewFace(f, &opentype.FaceOptions{
				Size:    float64(r.fontSize),
				DPI:     r.fontDPI(),
				Hinting: font.HintingFull,
			})
			if err != nil {
				continue
			}
			r.fallbackFaces = append(r.fallbackFaces, face)
		}
	}
	// New faces may cover runes previously recorded as missing.
	r.glyphMisses = make(map[rune]bool)
}

// systemFallbackPaths returns candidate system fonts in priority order; files
// that don't exist or fail to parse are skipped.
func systemFallbackPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/Menlo.ttc",                      // symbols, box drawing
			"/System/Library/Fonts/Apple Symbols.ttf",              // wide symbol coverage
			"/System/Library/Fonts/Supplemental/Arial Unicode.ttf", // CJK + broad BMP
			"/System/Library/Fonts/Supplemental/Zapf Dingbats.ttf", // dingbat arrows
			"/System/Library/Fonts/Supplemental/Apple Symbols.ttf", // alternate location
		}
	case "windows":
		return []string{
			`C:\Windows\Fonts\consola.ttf`,  // Consolas
			`C:\Windows\Fonts\seguisym.ttf`, // Segoe UI Symbol
			`C:\Windows\Fonts\msgothic.ttc`, // CJK
		}
	default: // linux and friends
		return []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
			"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
			"/usr/share/fonts/dejavu/DejaVuSansMono.ttf",
			"/usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansSymbols-Regular.ttf",
			"/usr/share/fonts/truetype/noto/NotoSansSymbols-Regular.ttf",
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		}
	}
}

// faceFor returns the first face in the fallback chain that has a glyph for
// char: the active font, then embedded fallbacks, then system fonts (loaded
// on first use).
func (r *Renderer) faceFor(char rune) font.Face {
	if r.face != nil {
		if _, ok := r.face.GlyphAdvance(char); ok {
			return r.face
		}
	}
	for _, f := range r.fallbackFaces {
		if _, ok := f.GlyphAdvance(char); ok {
			return f
		}
	}
	if !r.systemFallbacksLoaded {
		n := len(r.fallbackFaces)
		r.loadSystemFallbacks()
		for _, f := range r.fallbackFaces[n:] {
			if _, ok := f.GlyphAdvance(char); ok {
				return f
			}
		}
	}
	return nil
}
