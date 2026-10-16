package fonts

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"sync"
)

//go:embed JetBrainsMonoNerdFont-Regular.ttf
var JetBrainsMono []byte

//go:embed FiraCodeNerdFont-Regular.ttf
var FiraCode []byte

//go:embed HackNerdFont-Regular.ttf
var Hack []byte

//go:embed UbuntuMonoNerdFont-Regular.ttf
var UbuntuMono []byte

// Bold/italic variants ship gzip-compressed and are inflated on first use.
//
//go:embed *-Bold.ttf.gz *-Italic.ttf.gz *-BoldItalic.ttf.gz
var variantFS embed.FS

var variantFiles = map[string]string{
	"jetbrains": "JetBrainsMonoNerdFont",
	"firacode":  "FiraCodeNerdFont",
	"hack":      "HackNerdFont",
	"ubuntu":    "UbuntuMonoNerdFont",
}

var (
	variantMu    sync.Mutex
	variantCache = map[string][]byte{}
)

// Variant returns the embedded bold/italic face of font name. ok is false when
// the family has no such face (Fira Code has no italic), so callers synthesize
// the style instead. Regular returns the embedded regular data.
func Variant(name string, bold, italic bool) ([]byte, bool) {
	if !bold && !italic {
		return GetFont(name)
	}
	base, ok := variantFiles[name]
	if !ok {
		return nil, false
	}
	style := "Bold"
	switch {
	case bold && italic:
		style = "BoldItalic"
	case italic:
		style = "Italic"
	}
	file := base + "-" + style + ".ttf.gz"
	variantMu.Lock()
	defer variantMu.Unlock()
	if data, ok := variantCache[file]; ok {
		return data, data != nil
	}
	data := inflate(file)
	variantCache[file] = data
	return data, data != nil
}

func inflate(file string) []byte {
	raw, err := variantFS.ReadFile(file)
	if err != nil {
		return nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		return nil
	}
	return data
}

// FontInfo describes an available font
type FontInfo struct {
	Name        string
	DisplayName string
	Data        []byte
}

// AvailableFonts returns all embedded fonts
func AvailableFonts() []FontInfo {
	return []FontInfo{
		{Name: "jetbrains", DisplayName: "JetBrains Mono Nerd Font", Data: JetBrainsMono},
		{Name: "firacode", DisplayName: "Fira Code Nerd Font", Data: FiraCode},
		{Name: "hack", DisplayName: "Hack Nerd Font", Data: Hack},
		{Name: "ubuntu", DisplayName: "Ubuntu Mono Nerd Font", Data: UbuntuMono},
	}
}

// GetFont returns the font data by name (case insensitive)
func GetFont(name string) ([]byte, bool) {
	for _, f := range AvailableFonts() {
		if f.Name == name {
			return f.Data, true
		}
	}
	return nil, false
}

// DefaultFont returns the default font (JetBrains Mono)
func DefaultFont() []byte {
	return JetBrainsMono
}

// DefaultFontName returns the default font name
func DefaultFontName() string {
	return "jetbrains"
}
