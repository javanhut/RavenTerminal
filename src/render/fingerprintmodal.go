package render

// FingerprintModal is what the sudo fingerprint modal shows. The app fills it
// from the pane's fingerprint.Watcher; render stays ignorant of where it came
// from.
type FingerprintModal struct {
	Title    string
	Subtitle string
	Footer   string
	// Tone picks the accent: waiting, matched or missed.
	Tone FingerprintTone
}

type FingerprintTone int

const (
	FingerprintWaiting FingerprintTone = iota
	FingerprintMatched
	FingerprintMissed
)

// fingerprintGlyph is nf-md-fingerprint, present in every embedded Nerd Font.
const fingerprintGlyph = '\U000F0237'

var fingerprintMatchedColor = [4]float32{0.3, 0.8, 0.4, 1.0}

// DrawFingerprintModal draws a centred card over a dimmed window: a large
// fingerprint, what the helper is waiting for, and how to get to the password
// instead. Drawn last in a frame, like the toast, so it sits above panels.
func (r *Renderer) DrawFingerprintModal(m FingerprintModal, width, height int) {
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)
	cw, ch := r.cellWidth, r.cellHeight

	accent := r.theme.TabActive
	switch m.Tone {
	case FingerprintMatched:
		accent = fingerprintMatchedColor
	case FingerprintMissed:
		accent = panelErrorText
	}

	r.drawRect(0, 0, float32(width), float32(height), withAlpha(r.theme.Background, 0.55), proj)

	const iconScale = 3
	margin := cw * 2
	cardW := min(cw*48, float32(width)-margin*2)
	if cardW < cw*12 {
		cardW = float32(width) // too narrow for margins: use the whole width
	}
	padY := ch * 0.9
	// Rows with nothing to say are left out, so a bare verdict gets a
	// compact card rather than a tall one with blank lines.
	cardH := padY + ch*iconScale + ch*0.6 + ch + padY
	if m.Subtitle != "" {
		cardH += ch*0.3 + ch
	}
	if m.Footer != "" {
		cardH += ch*0.9 + ch
	}
	x := (float32(width) - cardW) / 2
	y := (float32(height) - cardH) / 2

	border := float32(2)
	r.drawRoundedRect(x-border, y-border, cardW+border*2, cardH+border*2, ch*0.6+border, withAlpha(accent, 0.9), proj)
	r.drawRoundedRect(x, y, cardW, cardH, ch*0.6, withAlpha(r.theme.TabBar, 0.98), proj)

	maxChars := int((cardW - cw*2) / cw)
	centered := func(baseline float32, text string, clr [4]float32) {
		runes := []rune(text)
		if len(runes) > maxChars && maxChars > 3 {
			runes = append(runes[:maxChars-3], []rune("...")...)
		}
		tx := x + (cardW-float32(len(runes))*cw)/2
		r.drawText(tx, baseline, string(runes), clr, proj)
	}

	cursor := y + padY + ch*iconScale
	if g, ok := r.lookupGlyph(fingerprintGlyph); ok && g.PixelWidth > 0 {
		gw := float32(g.PixelWidth) * iconScale
		gx := x + (cardW-gw)/2 - g.OffsetX*iconScale
		r.drawCharScaled(gx, cursor, fingerprintGlyph, accent, proj, iconScale)
	}
	cursor += ch*0.6 + ch
	centered(cursor, m.Title, r.theme.Foreground)
	if m.Subtitle != "" {
		cursor += ch*0.3 + ch
		centered(cursor, m.Subtitle, withAlpha(r.theme.Foreground, 0.75))
	}
	if m.Footer != "" {
		cursor += ch*0.9 + ch
		centered(cursor, m.Footer, withAlpha(r.theme.Foreground, 0.5))
	}

	r.uiFlush()
}
