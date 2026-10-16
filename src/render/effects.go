package render

import (
	"time"
	"unicode/utf8"

	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/tab"
	"github.com/rivo/uniseg"
)

// Effects are the renderer's visual options (wired from config).
type Effects struct {
	// ReduceMotion keeps blinking text steady, stops the indeterminate
	// progress sweep and makes the visual bell a single unanimated flash.
	ReduceMotion bool
	// PromptMarks draws OSC 133 prompt marks in the pane gutter and, while
	// scrolled back, as ticks on the right edge.
	PromptMarks bool
	// BlinkInterval is the text blink half-period (default 530ms, the cursor rate).
	BlinkInterval time.Duration
}

// SetEffects applies visual options.
func (r *Renderer) SetEffects(e Effects) {
	r.reduceMotion = e.ReduceMotion
	r.promptMarks = e.PromptMarks
	r.blinkInterval = e.BlinkInterval
	r.uiDirty = true
}

const defaultBlinkInterval = 530 * time.Millisecond

func (r *Renderer) blinkIv() time.Duration {
	if r.blinkInterval > 0 {
		return r.blinkInterval
	}
	return defaultBlinkInterval
}

// blinkVisible reports the SGR 5 text phase at elapsed time t.
func blinkVisible(t, interval time.Duration) bool {
	return interval <= 0 || (t/interval)%2 == 0
}

// --- progress (OSC 9;4) ---

type progressKind int

const (
	progressNormal progressKind = iota
	progressError
	progressWarning
)

type progressSeg struct {
	x, w     float32
	kind     progressKind
	animated bool
}

const progressSweep = 1600 * time.Millisecond

// progressBar lays out the bar for an OSC 9;4 state across width w:
// 1 normal, 2 error, 3 indeterminate, 4 paused. Error/paused without a value
// fill the bar.
func progressBar(state, value int, w float32, elapsed time.Duration) (progressSeg, bool) {
	value = max(0, min(value, 100))
	switch state {
	case 1:
		return progressSeg{w: w * float32(value) / 100}, value > 0
	case 2, 4:
		kind := progressError
		if state == 4 {
			kind = progressWarning
		}
		if value == 0 {
			value = 100
		}
		return progressSeg{w: w * float32(value) / 100, kind: kind}, true
	case 3:
		seg := w / 4
		phase := float32(elapsed%progressSweep) / float32(progressSweep)
		x0 := phase*(w+seg) - seg
		x1 := min(x0+seg, w)
		x0 = max(x0, 0)
		if x1 <= x0 {
			x0, x1 = 0, min(seg/8, w)
		}
		return progressSeg{x: x0, w: x1 - x0, animated: true}, true
	}
	return progressSeg{}, false
}

func (r *Renderer) drawProgress(state, value int, x, y, w float32, proj [16]float32) {
	elapsed := time.Since(r.epoch)
	if r.reduceMotion && state == 3 {
		elapsed = progressSweep * 3 / 8
	}
	seg, ok := progressBar(state, value, w, elapsed)
	if !ok {
		return
	}
	if seg.animated && !r.reduceMotion {
		r.animUntil(time.Now().Add(time.Second / 30))
	}
	ansi := r.theme.ansi16()
	clr := r.theme.TabActive
	switch seg.kind {
	case progressError:
		clr = ansi[9]
	case progressWarning:
		clr = ansi[11]
	}
	h := max32(2, r.cellHeight/8)
	r.drawRect(x, y, w, h, withAlpha(r.theme.Foreground, 0.08), proj)
	r.drawRect(x+seg.x, y, seg.w, h, clr, proj)
}

// --- bell ---

const bellDuration = 150 * time.Millisecond

// BellActions maps the bell config ("visual", "attention", "none") to whether
// the pane flashes and whether the window should request attention.
func BellActions(mode string) (flash, attention bool) {
	switch mode {
	case "none":
		return false, false
	case "attention":
		return true, true
	}
	return true, false
}

// flashAlpha is the overlay opacity elapsed into a bell flash.
func flashAlpha(elapsed time.Duration, reduceMotion bool) float32 {
	if elapsed < 0 || elapsed >= bellDuration {
		return 0
	}
	if reduceMotion {
		return 0.12
	}
	return 0.25 * (1 - float32(elapsed)/float32(bellDuration))
}

// FlashPane starts a visual bell flash on a pane.
func (r *Renderer) FlashPane(p *tab.Pane) {
	if p == nil {
		return
	}
	if r.flashes == nil {
		r.flashes = map[*tab.Pane]time.Time{}
	}
	r.flashes[p] = time.Now()
	r.uiDirty = true
}

func (r *Renderer) drawFlash(p *tab.Pane, x, y, w, h float32, proj [16]float32) {
	start, ok := r.flashes[p]
	if !ok {
		return
	}
	el := time.Since(start)
	a := flashAlpha(el, r.reduceMotion)
	if a <= 0 {
		delete(r.flashes, p)
		return
	}
	r.drawRect(x, y, w, h, withAlpha(r.theme.Foreground, a), proj)
}

// --- animation scheduling ---

func (r *Renderer) animUntil(t time.Time) {
	if r.nextAnim.IsZero() || t.Before(r.nextAnim) {
		r.nextAnim = t
	}
}

// NextAnimationFrame reports how long until a running effect (bell flash,
// indeterminate progress, blinking text) needs another frame; ok is false
// when nothing animates. The main loop should not sleep past it.
func (r *Renderer) NextAnimationFrame(now time.Time) (time.Duration, bool) {
	var next time.Time
	for p, start := range r.flashes {
		end := start.Add(bellDuration)
		if !now.Before(end) {
			if now.Sub(end) > time.Second {
				delete(r.flashes, p)
			}
			continue
		}
		t := now.Add(time.Second / 60)
		if end.Before(t) {
			t = end
		}
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	if !r.nextAnim.IsZero() && (next.IsZero() || r.nextAnim.Before(next)) {
		next = r.nextAnim
	}
	if next.IsZero() {
		return 0, false
	}
	return max(next.Sub(now), 0), true
}

// --- prompt marks ---

type markStatus int

const (
	markUnknown markStatus = iota
	markSuccess
	markFailure
)

type gutterMark struct {
	row    int
	status markStatus
}

type scrollTick struct {
	y      float32
	status markStatus
}

func statusOf(m grid.PromptMark) markStatus {
	switch {
	case !m.HasExit:
		return markUnknown
	case m.Exit == 0:
		return markSuccess
	}
	return markFailure
}

// gutterMarks returns the viewport rows of prompt marks.
func gutterMarks(marks []grid.PromptMark, viewTopAbs, rows int, dst []gutterMark) []gutterMark {
	dst = dst[:0]
	for _, m := range marks {
		if row := m.AbsRow - viewTopAbs; row >= 0 && row < rows {
			dst = append(dst, gutterMark{row: row, status: statusOf(m)})
		}
	}
	return dst
}

// scrollbarTicks positions every prompt mark along a track of height h
// covering absolute rows first..last.
func scrollbarTicks(marks []grid.PromptMark, first, last int, h float32, dst []scrollTick) []scrollTick {
	dst = dst[:0]
	total := float32(last - first + 1)
	if total <= 0 {
		return dst
	}
	for _, m := range marks {
		dst = append(dst, scrollTick{y: float32(m.AbsRow-first) / total * h, status: statusOf(m)})
	}
	return dst
}

func (r *Renderer) markColor(s markStatus) [4]float32 {
	ansi := r.theme.ansi16()
	switch s {
	case markSuccess:
		return ansi[2]
	case markFailure:
		return ansi[1]
	}
	return withAlpha(r.theme.Foreground, 0.45)
}

func (r *Renderer) drawPromptMarks(g *grid.Grid, viewTopAbs, rows, scrollOffset int, x, y, w, h float32, proj [16]float32) {
	if !r.promptMarks || g == nil {
		return
	}
	r.markBuf = g.PromptMarks(viewTopAbs, viewTopAbs+rows-1, r.markBuf[:0])
	r.gutterBuf = gutterMarks(r.markBuf, viewTopAbs, rows, r.gutterBuf)
	for _, m := range r.gutterBuf {
		r.drawRect(x-4, y+float32(m.row)*r.cellHeight+1, 2, r.cellHeight-2, r.markColor(m.status), proj)
	}
	if scrollOffset == 0 {
		return
	}
	first, last := g.AbsBounds()
	if r.tickKey != [3]any{g, first, last} {
		r.tickKey = [3]any{g, first, last}
		r.allMarks = g.PromptMarks(first, last, r.allMarks[:0])
	}
	r.tickBuf = scrollbarTicks(r.allMarks, first, last, h, r.tickBuf)
	for _, t := range r.tickBuf {
		r.drawRect(x+w-3, y+t.y, 3, 2, r.markColor(t.status), proj)
	}
	vy := float32(viewTopAbs-first) / float32(last-first+1) * h
	vh := float32(rows) / float32(last-first+1) * h
	r.drawRect(x+w-1, y+vy, 1, max32(vh, 2), withAlpha(r.theme.Foreground, 0.35), proj)
}

// --- IME preedit ---

// SetPreedit sets the input-method composition shown at the active pane's
// cursor; cursorBytes is the caret's byte offset (-1 hides it), "" ends it.
func (r *Renderer) SetPreedit(text string, cursorBytes int) {
	if text == r.preedit && cursorBytes == r.preeditCursor {
		return
	}
	r.preedit, r.preeditCursor = text, cursorBytes
	r.uiDirty = true
}

// preeditLayout returns the first column, width in cells and caret cell
// (relative to start, -1 hidden) of a preedit string drawn at cursorCol,
// shifted left so it stays inside cols.
func preeditLayout(text string, cursorBytes, cursorCol, cols int) (start, cells, caret int) {
	caret = -1
	for n, state := 0, -1; n < len(text); {
		cluster, _, _, st := uniseg.FirstGraphemeClusterInString(text[n:], state)
		w := textCells(cluster)
		if caret < 0 && cursorBytes >= n && cursorBytes < n+len(cluster) {
			caret = cells
			if cursorBytes > n {
				caret += w // inside a cluster: after it
			}
		}
		n, state, cells = n+len(cluster), st, cells+w
	}
	if cursorBytes == len(text) && cursorBytes >= 0 {
		caret = cells
	}
	start = cursorCol
	if start+cells > cols {
		start = max(cols-cells, 0)
	}
	return start, cells, caret
}

func (r *Renderer) drawPreeditText(px, py float32, text string, fg [4]float32, proj [16]float32) {
	r.drawText(px, py+r.cellHeight, text, fg, proj)
}

func (r *Renderer) drawPreedit(cursorCol, cursorRow, cols int, x, y float32, proj [16]float32) {
	if r.preedit == "" || !utf8.ValidString(r.preedit) {
		return
	}
	start, cells, caret := preeditLayout(r.preedit, r.preeditCursor, cursorCol, cols)
	px := x + float32(start)*r.cellWidth
	py := y + float32(cursorRow)*r.cellHeight
	fg := r.defaultFg()
	r.drawRect(px, py, float32(cells)*r.cellWidth, r.cellHeight, r.defaultBg(), proj)
	r.drawPreeditText(px, py, r.preedit, fg, proj)
	r.drawRect(px, py+r.cellHeight-2, float32(cells)*r.cellWidth, 1, fg, proj)
	if caret >= 0 {
		r.drawRect(px+float32(caret)*r.cellWidth, py, 2, r.cellHeight, r.cursorColor(), proj)
	}
}

// underlineStyleFor picks the SGR 4:n style to draw: hover links are solid,
// and with styled underlines disabled everything but SGR 21 double is solid.
func underlineStyleFor(c grid.Cell, hovered, undercurl bool) uint8 {
	if hovered || c.Flags&grid.FlagUnderline == 0 || c.UnderlineStyle == 0 {
		return 1
	}
	if c.UnderlineStyle != 2 && !undercurl {
		return 1
	}
	return c.UnderlineStyle
}

// beginEffectsFrame resets per-frame animation state before the panes draw.
func (r *Renderer) beginEffectsFrame() {
	now := time.Now()
	if r.epoch.IsZero() {
		r.epoch = now
	}
	r.nextAnim = time.Time{}
	r.blinkOff = !blinkVisible(now.Sub(r.epoch), r.blinkIv())
	if r.texCache != nil {
		r.texCache.beginFrame()
		r.texCache.sweepIdle(now, time.Minute)
	}
}

// paneEffects draws the per-pane overlays after the grid: prompt marks,
// progress bar, IME preedit and the bell flash.
func (r *Renderer) paneEffects(p *tab.Pane, active bool, x, y, w, h float32, proj [16]float32) {
	f := &r.frame
	if r.sawBlink {
		iv := r.blinkIv()
		el := time.Since(r.epoch)
		r.animUntil(r.epoch.Add((el/iv + 1) * iv))
	}
	r.drawPromptMarks(f.Grid, f.ViewTopAbs, f.Snap.Rows, f.Snap.ScrollOffset, x, y, w, h, proj)
	r.drawProgress(f.ProgressState, f.ProgressValue, x, y, w, proj)
	if active && f.Snap.ScrollOffset == 0 {
		r.drawPreedit(f.Snap.CursorCol, f.Snap.CursorRow, f.Snap.Cols, x, y, proj)
	}
	r.drawFlash(p, x, y, w, h, proj)
}
