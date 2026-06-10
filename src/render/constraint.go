package render

// Glyph placement constraint, modeled on Ghostty: icon/symbol glyphs are
// scaled down to fit the cell span available to them and centered in it,
// while regular text renders at its natural bearings (small overhang, e.g.
// from italics, is allowed rather than clipped).

// isCellExact reports whether a rune must be rasterized clipped to exactly
// the cell box so it tiles seamlessly with neighboring cells and background
// rectangles (TUI borders, powerline separators).
func isCellExact(c rune) bool {
	return (c >= 0x2500 && c <= 0x257F) || // box drawing
		(c >= 0xE0B0 && c <= 0xE0D4) // powerline separators and extras
}

// isIconRune reports whether a rune is an icon/symbol that gets scale-to-fit
// constraint instead of natural placement. Covers the Nerd Fonts private-use
// areas (file/folder/git icons as used by nvim-tree, lsd, starship, ...) and
// the common symbol blocks.
func isIconRune(c rune) bool {
	switch {
	case c >= 0xE000 && c <= 0xF8FF: // private use area (Nerd Fonts)
		return !isCellExact(c) // powerline stays cell-exact
	case c >= 0xF0000 && c <= 0xFFFFD: // supplementary PUA-A (Nerd Fonts 3 material icons)
		return true
	case c >= 0x2600 && c <= 0x27BF: // miscellaneous symbols, dingbats
		return true
	case c >= 0x2B00 && c <= 0x2BFF: // miscellaneous symbols and arrows
		return true
	case c >= 0x1F000 && c <= 0x1FAFF: // pictographs rendered monochrome
		return true
	}
	return false
}

// glyphQuad computes the screen rectangle for glyph g anchored at the cell
// whose top-left corner is (cellX, cellTop), with span horizontal cells
// available (2 when an icon is followed by a blank cell, matching how Nerd
// Font icons are typically spaced in TUIs). Returns the quad top-left and
// size in pixels.
func (r *Renderer) glyphQuad(char rune, g Glyph, cellX, cellTop float32, span int) (x, yTop, w, h float32) {
	w = float32(g.PixelWidth)
	h = float32(g.PixelHeight)
	baseline := cellTop + float32(r.atlasAscent)
	x = cellX + g.OffsetX
	yTop = baseline + g.OffsetY
	if !isIconRune(char) {
		return
	}

	// Icons: scale down to fit the available span, then center. Horizontal
	// centering uses the cells the glyph actually needs, so a one-cell icon
	// stays in its own cell instead of drifting toward the next.
	maxW := float32(span) * r.cellWidth
	if scale := min(1, maxW/w, r.cellHeight/h); scale < 1 {
		w *= scale
		h *= scale
	}
	needCells := float32(int((w-0.001)/r.cellWidth) + 1)
	if needCells > float32(span) {
		needCells = float32(span)
	}
	x = cellX + (needCells*r.cellWidth-w)/2
	if x < cellX {
		x = cellX
	}
	yTop = cellTop + (r.cellHeight-h)/2
	return
}
