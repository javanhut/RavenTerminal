package render

import (
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/parser"
)

// paneColors is one pane's effective palette: theme defaults with the
// application's OSC 4/10/11/12 overrides and DECSCNM applied.
type paneColors struct {
	palette        [256][4]float32
	fg, bg, cursor [4]float32
}

func rgb8(c grid.Color) [3]uint8 { return [3]uint8{c.R, c.G, c.B} }

func to8(c [4]float32) [3]uint8 {
	q := func(v float32) uint8 { return uint8(clampUnit(v)*255 + 0.5) }
	return [3]uint8{q(c[0]), q(c[1]), q(c[2])}
}

func toGridColor(c [4]float32) grid.Color {
	v := to8(c)
	return grid.Color{Type: grid.ColorRGB, R: v[0], G: v[1], B: v[2]}
}

// matchTheme returns the theme's exact float colour when the 8-bit value is
// unchanged (keeping alpha and avoiding rounding), else the 8-bit colour.
func matchTheme(v [3]uint8, theme [4]float32, alpha float32) [4]float32 {
	if to8(theme) == v {
		return theme
	}
	return [4]float32{float32(v[0]) / 255, float32(v[1]) / 255, float32(v[2]) / 255, alpha}
}

func (t Theme) ansi16() *[16][4]float32 {
	if t.ANSI != nil {
		return t.ANSI
	}
	return &standard16
}

// themeDefaultColors converts a theme into the defaults the terminal answers
// OSC 4/10/11/12 queries with.
func themeDefaultColors(t Theme) (fg, bg, cursor grid.Color, palette [16]grid.Color) {
	for i, c := range t.ansi16() {
		palette[i] = toGridColor(c)
	}
	return toGridColor(t.Foreground), toGridColor(t.Background), toGridColor(t.Cursor), palette
}

func resolvePaneColors(t Theme, pal [256][3]uint8, fg, bg, cursor [3]uint8, reverse bool) paneColors {
	var pc paneColors
	ansi := t.ansi16()
	for i := range pal {
		if i < 16 {
			pc.palette[i] = matchTheme(pal[i], ansi[i], 1)
		} else {
			pc.palette[i] = matchTheme(pal[i], indexedColor(uint8(i)), 1)
		}
	}
	if reverse {
		fg, bg = bg, fg
	}
	pc.fg = matchTheme(fg, t.Foreground, 1)
	pc.bg = matchTheme(bg, t.Background, t.Background[3])
	pc.cursor = matchTheme(cursor, t.Cursor, 1)
	return pc
}

// applyPaneColors loads a pane's effective colours for the grid pass, first
// seeding the terminal with the current theme's defaults when needed.
func (r *Renderer) applyPaneColors(term *parser.Terminal, reverse bool) {
	if r.themedTerms == nil || len(r.themedTerms) > 512 {
		r.themedTerms = make(map[*parser.Terminal]uint64)
	}
	if gen, ok := r.themedTerms[term]; !ok || gen != r.themeGen {
		fg, bg, cur, pal := themeDefaultColors(r.theme)
		term.SetDefaultColors(fg, bg, cur, pal)
		r.themedTerms[term] = r.themeGen
	}
	pal, fg, bg, cur := term.EffectiveColors()
	r.paneCols = resolvePaneColors(r.theme, pal, fg, bg, cur, reverse)
	r.pc = &r.paneCols
}

// paneBackground reports the pane's background when it differs from the
// window clear colour (OSC 11 or reverse video), so it needs its own rect.
func (r *Renderer) paneBackground() ([4]float32, bool) {
	if r.pc == nil || r.pc.bg == r.theme.Background {
		return r.theme.Background, false
	}
	return r.pc.bg, true
}

func (r *Renderer) defaultFg() [4]float32 {
	if r.pc != nil {
		return r.pc.fg
	}
	return r.theme.Foreground
}

func (r *Renderer) defaultBg() [4]float32 {
	if r.pc != nil {
		return r.pc.bg
	}
	return r.theme.Background
}

func (r *Renderer) cursorColor() [4]float32 {
	if r.pc != nil {
		return r.pc.cursor
	}
	return r.theme.Cursor
}
