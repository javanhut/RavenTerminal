package render

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
)

// Font fallback chain, modeled on Ghostty's font cascade: when the active font
// has no glyph for a codepoint, the renderer tries the other embedded Nerd
// Fonts, then (lazily) a small set of system fonts with broad symbol/CJK
// coverage, then any installed font fontconfig finds for the codepoint, before
// resorting to the substitution tables and the final '?'.

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
		r.addFallbackFile(path)
	}
	// New faces may cover runes previously recorded as missing.
	r.glyphMisses = make(map[rune]bool)
	r.asciiCache = [128]asciiGlyphSlot{}
}

// addFallbackFile parses the font at path and appends it to the fallback
// chain, returning how many faces were added. Each path is loaded at most once;
// files that don't exist or fail to parse add nothing.
func (r *Renderer) addFallbackFile(path string) int {
	if r.fallbackPaths == nil {
		r.fallbackPaths = make(map[string]bool)
	}
	if r.fallbackPaths[path] {
		return 0
	}
	r.fallbackPaths[path] = true
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var f *opentype.Font
	if filepath.Ext(path) == ".ttc" {
		coll, err := opentype.ParseCollection(data)
		if err != nil {
			return 0
		}
		// The first face of a collection is the regular weight.
		if f, err = coll.Font(0); err != nil {
			return 0
		}
	} else if f, err = opentype.Parse(data); err != nil {
		return 0
	}
	r.fallbackFonts = append(r.fallbackFonts, fallbackFont{font: f})
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    float64(r.fontSize),
		DPI:     r.fontDPI(),
		Hinting: font.HintingFull,
	})
	if err != nil {
		return 0
	}
	r.fallbackFaces = append(r.fallbackFaces, face)
	return 1
}

// fontconfigMatch asks fontconfig for an installed font file covering char,
// or "" when there is none or fontconfig isn't available. fc-match always
// answers with its best match, which may still lack the glyph, so callers must
// check coverage. A variable so tests can stub it.
var fontconfigMatch = func(char rune) (string, error) {
	out, err := exec.Command("fc-match", "--format=%{file}", fmt.Sprintf(":charset=%x", char)).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// fontconfigFallback is the last link of the chain: when neither the embedded
// fonts nor the fixed system list cover char, ask fontconfig which installed
// font does and add that font to the chain. Each rune is looked up at most
// once per session, and a missing fc-match disables the lookup entirely.
func (r *Renderer) fontconfigFallback(char rune) font.Face {
	if r.fontconfigOff {
		return nil
	}
	if r.fontconfigTried == nil {
		r.fontconfigTried = make(map[rune]bool)
	}
	if r.fontconfigTried[char] {
		return nil
	}
	r.fontconfigTried[char] = true
	path, err := fontconfigMatch(char)
	if errors.Is(err, exec.ErrNotFound) {
		r.fontconfigOff = true
		return nil
	}
	// Color emoji have their own renderer (coloremoji.go); a monochrome face
	// parsed from a bitmap emoji font would claim the rune and draw nothing.
	if path == "" || strings.Contains(strings.ToLower(filepath.Base(path)), "emoji") {
		return nil
	}
	n := len(r.fallbackFaces)
	if r.addFallbackFile(path) == 0 {
		return nil
	}
	// The new face may cover runes previously recorded as missing.
	r.glyphMisses = make(map[rune]bool)
	r.asciiCache = [128]asciiGlyphSlot{}
	if f := r.fallbackFaces[n]; hasGlyph(f, char) {
		return f
	}
	return nil
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
// on first use), then whatever installed font fontconfig says covers it.
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
	return r.fontconfigFallback(char)
}

func hasGlyph(f font.Face, char rune) bool {
	_, ok := f.GlyphAdvance(char)
	return ok
}
