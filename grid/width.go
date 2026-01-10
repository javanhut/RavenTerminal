package grid

import (
	"unicode"

	"golang.org/x/text/width"
)

// RuneWidth returns the display width of a rune (0, 1, or 2 cells)
// 0 = zero-width (combining marks, null)
// 1 = normal single-width character
// 2 = wide character (CJK, emoji, etc.)
func RuneWidth(r rune) int {
	// Null character has zero width
	if r == '\x00' {
		return 0
	}

	// Non-printable characters have zero width
	if !unicode.IsPrint(r) {
		return 0
	}

	// Combining characters have zero width
	// Mn = Mark, Nonspacing
	// Me = Mark, Enclosing
	// Mc = Mark, Spacing Combining
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r) {
		return 0
	}

	// Use East Asian Width properties from x/text/width
	k := width.LookupRune(r)
	switch k.Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	default:
		return 1
	}
}

// StringWidth returns the total display width of a string
func StringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += RuneWidth(r)
	}
	return w
}
