package grid

import (
	"unicode"

	"github.com/rivo/uniseg"
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

	// Private Use Area glyphs (nerd-font / powerline / devicon icons used by
	// prompts like starship) occupy a single cell. unicode.IsPrint reports
	// false for the Private Use category, so handle these before that check to
	// avoid treating them as zero-width and desyncing the cursor column.
	if unicode.Is(unicode.Co, r) {
		return 1
	}

	// Non-printable characters have zero width. Unicode space separators (Zs)
	// are excluded from this check: unicode.IsPrint admits only the ASCII
	// space, but NBSP and friends occupy a cell in every terminal. Dropping
	// them desyncs the cursor column from applications that emit them (e.g.
	// Claude Code separates its prompt from ghost text with U+00A0).
	if !unicode.IsPrint(r) && !unicode.Is(unicode.Zs, r) {
		return 0
	}

	// Combining characters have zero width
	// Mn = Mark, Nonspacing
	// Me = Mark, Enclosing
	// Mc = Mark, Spacing Combining
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r) {
		return 0
	}

	// Measure with the grapheme-aware width table (uniseg) so emoji are sized
	// the same way applications expect (emoji-presentation pictographs = 2),
	// matching ClusterWidth. This keeps the cursor column in sync with the TUI.
	return uniseg.StringWidth(string(r))
}

// StringWidth returns the total display width of a string
func StringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += RuneWidth(r)
	}
	return w
}

// ClusterWidth returns the display width (in cells) of a single grapheme
// cluster, using Unicode segmentation so that ZWJ sequences, VS16 emoji
// presentation, regional-indicator flags, and base+combining sequences are
// measured correctly (e.g. "👨‍👩‍👧"=2, "1️⃣"=2, "é"(decomposed)=1).
func ClusterWidth(cluster string) int {
	if cluster == "" {
		return 0
	}
	return uniseg.StringWidth(cluster)
}
