package render

import "math"

// FingerprintModal is what the sudo fingerprint modal shows. The app fills it
// from the pane's fingerprint.Watcher; render stays ignorant of where it came
// from.
type FingerprintModal struct {
	Title    string
	Subtitle string
	// Tone picks the accent: waiting, matched or missed.
	Tone FingerprintTone
	// Tries is how many readings this sudo has used up so far (retries and
	// misses); the progress dots light one per try.
	Tries int
}

type FingerprintTone int

const (
	FingerprintWaiting FingerprintTone = iota
	FingerprintMatched
	FingerprintMissed
)

// FingerprintHit is what a click on the modal landed on.
type FingerprintHit int

const (
	FingerprintHitNone     FingerprintHit = iota // outside the card
	FingerprintHitCard                           // on the card, but not a control
	FingerprintHitClose                          // the × in the corner
	FingerprintHitPassword                       // "Use Password"
	FingerprintHitSettings                       // "Authentication Settings"
)

// Nerd Font icons, present in every embedded font (drawn from the embedded
// one regardless, see labelIcon).
const (
	fingerprintGlyph = '\U000F0237' // nf-md-fingerprint
	iconClose        = '\U000F0156' // nf-md-close
	iconLock         = '\U000F0341' // nf-md-lock_outline
	iconSecurityKey  = '\U000F129F' // nf-md-usb_flash_drive_outline
	iconCog          = '\U000F08BB' // nf-md-cog_outline
)

// The modal's strings that do not depend on state.
const (
	fpPasswordLabel = "Use Password"
	fpKeyLabel      = "Use Security Key"
	fpRememberLabel = "Remember this device"
	fpRememberHint  = "Skip this for faster sign-ins."
	fpSettingsLabel = "Authentication Settings"
)

// fpDots is how many progress dots sit under the subtitle.
const fpDots = 5

func rgb(hex uint32, a float32) [4]float32 {
	return [4]float32{
		float32(hex>>16&0xff) / 255,
		float32(hex>>8&0xff) / 255,
		float32(hex&0xff) / 255,
		a,
	}
}

// The modal keeps its own palette rather than the terminal theme's: it is a
// system prompt, and reads the same whichever theme the pane uses.
var (
	fpBackdrop    = rgb(0x05060f, 0.62)
	fpCardTop     = rgb(0x181b37, 0.97)
	fpCardBottom  = rgb(0x0d0f22, 0.97)
	fpBorderTop   = rgb(0x6a6fd0, 0.75)
	fpBorderBot   = rgb(0x3a3f86, 0.55)
	fpTitle       = rgb(0xf3f4ff, 1)
	fpBody        = rgb(0xb6bcdd, 1)
	fpLabel       = rgb(0xe7e9f7, 1)
	fpMuted       = rgb(0x9197b8, 1)
	fpLine        = rgb(0x3a3f66, 0.8)
	fpTrack       = rgb(0x272b4a, 1)
	fpRingInside  = rgb(0x0a0c1e, 0.6)
	fpButtonFill  = rgb(0x151934, 1)
	fpButtonEdge  = rgb(0x363b68, 1)
	fpIcon        = rgb(0xcfd2ff, 1)
	fpDotIdle     = rgb(0x3b4062, 1)
	fpBlue        = rgb(0x5d6bff, 1)
	fpCyan        = rgb(0x3fd4ff, 1)
	fpViolet      = rgb(0xa78bfa, 1)
	fpViolet2     = rgb(0x7466f0, 0.9)
	fpPrintTop    = rgb(0xb18cff, 1)
	fpPrintBottom = rgb(0x4f7bff, 1)
	fpGreen       = rgb(0x34d399, 1)
	fpGreenDeep   = rgb(0x10b981, 1)
	fpRed         = rgb(0xf87171, 1)
	fpRedDeep     = rgb(0xdc2626, 1)
)

// dim fades a color for a control that has nothing behind it yet.
func dim(c [4]float32) [4]float32 { return withAlpha(c, c[3]*0.38) }

// fpRect is a hit box in framebuffer pixels.
type fpRect struct{ x, y, w, h float32 }

func (b fpRect) contains(x, y float32) bool {
	return x >= b.x && x <= b.x+b.w && y >= b.y && y <= b.y+b.h
}

// fingerprintLayout places the card. Every length is the mockup's pixel value
// times u, which follows the cell height (so zoom and HiDPI carry over) and
// shrinks when the window is too small for the whole card.
type fingerprintLayout struct {
	u          float32
	x, y, w, h float32
	settingsW  float32
	close      fpRect
	password   fpRect
	settings   fpRect
}

const (
	fpBaseW       = 656
	fpBaseH       = 734
	fpTitlePx     = 30
	fpBodyPx      = 20
	fpLabelPx     = 16
	fpRememberPx  = 15
	fpSmallPx     = 13
	fpSettingsPx  = 14.5
	fpOrPx        = 17
	fpMinScale    = 0.5
	fpWindowInset = 24
)

func (r *Renderer) fingerprintLayout(m FingerprintModal, width, height int) fingerprintLayout {
	unit := r.cellHeight / 24
	measure := func(u float32) (w, settingsW float32) {
		settingsW = u*55 + r.measureLabel(fpSettingsLabel, fpSettingsPx*u, labelText) + u*20
		remember := max(r.measureLabel(fpRememberLabel, fpRememberPx*u, labelText),
			r.measureLabel(fpRememberHint, fpSmallPx*u, labelText))
		w = max(fpBaseW*u,
			u*72+remember+u*28+settingsW+u*30,
			r.measureLabel(m.Title, fpTitlePx*u, labelText)+u*80,
			r.measureLabel(m.Subtitle, fpBodyPx*u, labelText)+u*80)
		return w, settingsW
	}
	w, _ := measure(unit)
	inset := fpWindowInset * unit
	scale := min(1, (float32(width)-inset*2)/w, (float32(height)-inset*2)/(fpBaseH*unit))
	u := unit * max(scale, fpMinScale)
	w, settingsW := measure(u)

	l := fingerprintLayout{u: u, w: w, h: fpBaseH * u, settingsW: settingsW}
	l.x = float32(math.Round(float64((float32(width) - l.w) / 2)))
	l.y = float32(math.Round(float64((float32(height) - l.h) / 2)))
	l.close = fpRect{l.x + l.w - 54*u, l.y + 14*u, 40 * u, 40 * u}
	cx := l.x + l.w/2
	l.password = fpRect{cx - 133*u - 90*u, l.y + 500*u, 180 * u, 108 * u}
	l.settings = fpRect{l.x + l.w - 30*u - settingsW, l.y + 657*u, settingsW, 49 * u}
	return l
}

// FingerprintModalHit reports what a click at framebuffer (x, y) lands on.
func (r *Renderer) FingerprintModalHit(m FingerprintModal, width, height int, x, y float32) FingerprintHit {
	l := r.fingerprintLayout(m, width, height)
	switch {
	case l.close.contains(x, y):
		return FingerprintHitClose
	case l.password.contains(x, y):
		return FingerprintHitPassword
	case l.settings.contains(x, y):
		return FingerprintHitSettings
	case (fpRect{l.x, l.y, l.w, l.h}).contains(x, y):
		return FingerprintHitCard
	}
	return FingerprintHitNone
}

// DrawFingerprintModal draws the card over a dimmed window: a fingerprint in
// a progress ring, what the helper is waiting for, and the ways out. Drawn
// last in a frame, like the toast, so it sits above panels.
func (r *Renderer) DrawFingerprintModal(m FingerprintModal, width, height int) {
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)
	l := r.fingerprintLayout(m, width, height)
	u := l.u
	X := func(v float32) float32 { return l.x + v*u }
	Y := func(v float32) float32 { return l.y + v*u }
	cx := l.x + l.w/2

	r.drawRect(0, 0, float32(width), float32(height), fpBackdrop, proj)

	// Card: a soft drop shadow, a vertical gradient fill and a hairline
	// border that is brighter along the top.
	radius := 22 * u
	r.drawShape(shape{cx: cx, cy: l.y + l.h/2 + 18*u, hw: l.w / 2, hh: l.h / 2, radius: radius,
		soft: 60 * u, colorA: rgb(0x000000, 0.55)}, proj)
	r.drawShape(shape{cx: cx, cy: l.y + l.h/2, hw: l.w / 2, hh: l.h / 2, radius: radius,
		colorA: fpCardTop, colorB: fpCardBottom, grad: gradVertical}, proj)
	r.drawShape(shape{cx: cx, cy: l.y + l.h/2, hw: l.w / 2, hh: l.h / 2, radius: radius,
		stroke: max(1.25*u, 1), colorA: fpBorderTop, colorB: fpBorderBot, grad: gradVertical}, proj)

	r.drawIcon(l.close.x+l.close.w/2, l.close.y+l.close.h/2, iconClose, 22*u, withAlpha(fpBody, 0.9), withAlpha(fpBody, 0.9), proj)

	r.drawFingerprintRing(m.Tone, cx, Y(179), u, proj)

	// Title, subtitle and the progress dots.
	r.drawLabel(cx, Y(334), m.Title, fpTitlePx*u, labelText, alignCenter, fpTitle, fpTitle, proj)
	if m.Subtitle != "" {
		r.drawLabel(cx, Y(372), m.Subtitle, fpBodyPx*u, labelText, alignCenter, fpBody, fpBody, proj)
	}
	r.drawFingerprintDots(m, cx, Y(426), u, proj)

	// "or" between two rules.
	lineY := float32(math.Round(float64(Y(474))))
	lineH := max(u, 1)
	gap := 28 * u
	r.drawRect(X(80), lineY, cx-gap-X(80), lineH, fpLine, proj)
	r.drawRect(cx+gap, lineY, l.x+l.w-80*u-(cx+gap), lineH, fpLine, proj)
	r.drawLabel(cx, lineY, "or", fpOrPx*u, labelText, alignCenter, fpBody, fpBody, proj)

	// The two alternatives, split by a short vertical rule. Only the password
	// is wired up: raven-finger-auth has no security-key path, so that one is
	// drawn disabled.
	r.drawRect(float32(math.Round(float64(cx))), Y(518), lineH, 55*u, fpLine, proj)
	r.drawFingerprintOption(cx-133*u, Y(539), u, iconLock, fpPasswordLabel, false, proj)
	r.drawFingerprintOption(cx+133*u, Y(539), u, iconSecurityKey, fpKeyLabel, true, proj)

	// Remember this device: disabled for the same reason.
	box := 26 * u
	r.drawShape(shape{cx: X(44), cy: Y(678), hw: box / 2, hh: box / 2, radius: 6 * u,
		stroke: max(1.5*u, 1), colorA: dim(fpIcon)}, proj)
	r.drawLabel(X(72), Y(668), fpRememberLabel, fpRememberPx*u, labelText, alignLeft, dim(fpLabel), dim(fpLabel), proj)
	r.drawLabel(X(72), Y(694), fpRememberHint, fpSmallPx*u, labelText, alignLeft, dim(fpMuted), dim(fpMuted), proj)

	// Authentication Settings button.
	s := l.settings
	r.drawShape(shape{cx: s.x + s.w/2, cy: s.y + s.h/2, hw: s.w / 2, hh: s.h / 2, radius: 8 * u,
		colorA: fpButtonFill}, proj)
	r.drawShape(shape{cx: s.x + s.w/2, cy: s.y + s.h/2, hw: s.w / 2, hh: s.h / 2, radius: 8 * u,
		stroke: max(u, 1), colorA: fpButtonEdge}, proj)
	r.drawIcon(s.x+29*u, s.y+s.h/2, iconCog, 22*u, fpIcon, fpIcon, proj)
	r.drawLabel(s.x+55*u, s.y+s.h/2, fpSettingsLabel, fpSettingsPx*u, labelText, alignLeft, fpLabel, fpLabel, proj)

	r.uiFlush()
}

// drawFingerprintRing draws the ring around the fingerprint: a dim track with
// two glowing arcs while waiting, one closed ring for a verdict.
func (r *Renderer) drawFingerprintRing(tone FingerprintTone, cx, cy, u float32, proj [16]float32) {
	const deg = math.Pi / 180
	ringR := 118 * u
	stroke := 5 * u

	r.fillCircle(cx, cy, ringR, fpRingInside, proj)
	r.strokeCircle(cx, cy, ringR, stroke, fpTrack, proj)

	type arc struct {
		start, sweep float32
		a, b         [4]float32
	}
	var arcs []arc
	printTop, printBottom := fpPrintTop, fpPrintBottom
	switch tone {
	case FingerprintMatched:
		arcs = []arc{{0, 359.9 * deg, fpGreen, fpGreenDeep}}
		printTop, printBottom = fpGreen, fpGreenDeep
	case FingerprintMissed:
		arcs = []arc{{5 * deg, 160 * deg, fpRed, fpRedDeep}, {200 * deg, 55 * deg, fpRedDeep, withAlpha(fpRed, 0.7)}}
		printTop, printBottom = fpRed, fpRedDeep
	default:
		arcs = []arc{{5 * deg, 160 * deg, fpBlue, fpCyan}, {200 * deg, 55 * deg, fpViolet, fpViolet2}}
	}
	for _, a := range arcs {
		glow := shape{cx: cx, cy: cy, hw: ringR + 5*u, hh: ringR + 5*u, radius: ringR + 5*u,
			stroke: stroke + 10*u, soft: 16 * u, colorA: withAlpha(a.a, 0.35), colorB: withAlpha(a.b, 0.35),
			grad: gradArc, start: a.start, sweep: a.sweep}
		r.drawShape(glow, proj)
		r.drawShape(shape{cx: cx, cy: cy, hw: ringR, hh: ringR, radius: ringR, stroke: stroke,
			colorA: a.a, colorB: a.b, grad: gradArc, start: a.start, sweep: a.sweep}, proj)
	}

	r.drawShape(shape{cx: cx, cy: cy, hw: 40 * u, hh: 40 * u, radius: 40 * u, soft: 70 * u,
		colorA: withAlpha(printBottom, 0.14)}, proj)
	r.drawIcon(cx, cy, fingerprintGlyph, 136*u, printTop, printBottom, proj)
}

// drawFingerprintDots lights one dot per reading so far: the current one
// while waiting, all of them once the finger matched.
func (r *Renderer) drawFingerprintDots(m FingerprintModal, cx, cy, u float32, proj [16]float32) {
	lit, active := min(m.Tries+1, fpDots), fpViolet
	switch m.Tone {
	case FingerprintMatched:
		lit, active = fpDots, fpGreen
	case FingerprintMissed:
		lit, active = min(max(m.Tries, 1), fpDots), fpRed
	}
	spacing := 29 * u
	x0 := cx - spacing*float32(fpDots-1)/2
	for i := range fpDots {
		x := x0 + spacing*float32(i)
		if i < lit {
			r.drawShape(shape{cx: x, cy: cy, hw: 6 * u, hh: 6 * u, radius: 6 * u, soft: 10 * u,
				colorA: withAlpha(active, 0.45)}, proj)
			r.fillCircle(x, cy, 6*u, active, proj)
			continue
		}
		r.fillCircle(x, cy, 5.5*u, fpDotIdle, proj)
	}
}

// drawFingerprintOption draws one round alternative button and its label.
func (r *Renderer) drawFingerprintOption(cx, cy, u float32, icon rune, text string, disabled bool, proj [16]float32) {
	fill, edge, ink, label := fpButtonFill, fpButtonEdge, fpIcon, fpLabel
	if disabled {
		fill, edge, ink, label = dim(fill), dim(edge), dim(ink), dim(label)
	}
	r.fillCircle(cx, cy, 33*u, fill, proj)
	r.strokeCircle(cx, cy, 33*u, max(1.25*u, 1), edge, proj)
	r.drawIcon(cx, cy, icon, 28*u, ink, ink, proj)
	r.drawLabel(cx, cy+53*u, text, fpLabelPx*u, labelText, alignCenter, label, label, proj)
}
