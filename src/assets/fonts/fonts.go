package fonts

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/binary"
	"hash/crc32"
	"io"
	"sync"
)

// All styles are Nerd Fonts v3.5.1 release files. To update them, unpack the
// release archives into one directory and run (here):
//
//	go run gendelta.go -src <dir> JetBrainsMonoNerdFont FiraCodeNerdFont HackNerdFont UbuntuMonoNerdFont

//go:embed JetBrainsMonoNerdFont-Regular.ttf
var JetBrainsMono []byte

//go:embed FiraCodeNerdFont-Regular.ttf
var FiraCode []byte

//go:embed HackNerdFont-Regular.ttf
var Hack []byte

//go:embed UbuntuMonoNerdFont-Regular.ttf
var UbuntuMono []byte

// Bold/italic variants ship as gzipped deltas against the family's regular
// face (they share most tables and every Nerd Font icon outline), rebuilt on
// first use. gendelta.go writes them.
//
//go:embed *.delta.gz
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
	file := base + "-" + style + ".delta.gz"
	variantMu.Lock()
	defer variantMu.Unlock()
	if data, ok := variantCache[file]; ok {
		return data, data != nil
	}
	regular, _ := GetFont(name)
	data := applyDelta(regular, inflate(file))
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

// applyDelta rebuilds a file from base and a delta: uvarint length, CRC-32
// of the result, then repeated (uvarint literal length, literals, varint copy
// offset relative to the end of the previous copy, uvarint copy length). It
// returns nil for a malformed delta or a checksum mismatch.
func applyDelta(base, d []byte) []byte {
	size, n := binary.Uvarint(d)
	if n <= 0 || len(d) < n+4 || size > 64<<20 {
		return nil
	}
	sum := binary.LittleEndian.Uint32(d[n:])
	d = d[n+4:]
	out := make([]byte, 0, size)
	last := 0
	for len(d) > 0 {
		lit, n := binary.Uvarint(d)
		if n <= 0 || uint64(len(d)-n) < lit {
			return nil
		}
		out = append(out, d[n:n+int(lit)]...)
		if d = d[n+int(lit):]; len(d) == 0 {
			break
		}
		off, n := binary.Varint(d)
		if n <= 0 {
			return nil
		}
		d = d[n:]
		length, n := binary.Uvarint(d)
		if n <= 0 {
			return nil
		}
		d = d[n:]
		start := int64(last) + off
		if start < 0 || length > uint64(len(base)) || start > int64(len(base))-int64(length) {
			return nil
		}
		out = append(out, base[start:start+int64(length)]...)
		last = int(start) + int(length)
	}
	if uint64(len(out)) != size || crc32.ChecksumIEEE(out) != sum {
		return nil
	}
	return out
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
