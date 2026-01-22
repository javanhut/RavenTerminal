package fonts

import (
	_ "embed"
)

//go:embed JetBrainsMonoNerdFont-Regular.ttf
var JetBrainsMono []byte

//go:embed FiraCodeNerdFont-Regular.ttf
var FiraCode []byte

//go:embed HackNerdFont-Regular.ttf
var Hack []byte

//go:embed UbuntuMonoNerdFont-Regular.ttf
var UbuntuMono []byte

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
