package render

import (
	"testing"
	"time"

	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/images"
	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/tab"
)

func oneCellSnap(c grid.Cell) *grid.Snapshot {
	return &grid.Snapshot{Cols: 1, Rows: 1, Cells: []grid.Cell{c}}
}

func textCell(ch rune, flags grid.CellFlags) grid.Cell {
	c := grid.NewCell()
	c.Char = ch
	c.Flags = flags
	return c
}

// --- colours ---

func testTheme() Theme {
	var th Theme
	th.Background = [4]float32{0.1, 0.1, 0.1, 0.9}
	th.Foreground = [4]float32{0.9, 0.9, 0.9, 1}
	th.Cursor = [4]float32{1, 0.5, 0, 1}
	return th
}

func TestXtermCubeAndGray(t *testing.T) {
	if got := indexedColor(196); got != ([4]float32{1, 0, 0, 1}) {
		t.Fatalf("196 = %v", got)
	}
	if got := indexedColor(16 + 36*1 + 6*2 + 3); got != ([4]float32{95.0 / 255, 135.0 / 255, 175.0 / 255, 1}) {
		t.Fatalf("cube = %v", got)
	}
	if got := indexedColor(232); got != ([4]float32{8.0 / 255, 8.0 / 255, 8.0 / 255, 1}) {
		t.Fatalf("232 = %v", got)
	}
}

func TestResolvePaneColorsKeepsThemeExactly(t *testing.T) {
	th := testTheme()
	fg, bg, cur, pal16 := themeDefaultColors(th)
	var pal [256][3]uint8
	for i := range 16 {
		pal[i] = [3]uint8{pal16[i].R, pal16[i].G, pal16[i].B}
	}
	pal[196] = [3]uint8{255, 0, 0}
	pc := resolvePaneColors(th, pal, rgb8(fg), rgb8(bg), rgb8(cur), false)
	if pc.bg != th.Background || pc.fg != th.Foreground || pc.cursor != th.Cursor {
		t.Fatalf("defaults drifted: %+v", pc)
	}
	if pc.palette[1] != standard16[1] {
		t.Fatalf("palette[1] = %v", pc.palette[1])
	}
	if pc.palette[196] != ([4]float32{1, 0, 0, 1}) {
		t.Fatalf("palette[196] = %v", pc.palette[196])
	}
}

func TestResolvePaneColorsOverridesAndReverse(t *testing.T) {
	th := testTheme()
	var pal [256][3]uint8
	pc := resolvePaneColors(th, pal, [3]uint8{0, 0, 255}, [3]uint8{255, 0, 0}, [3]uint8{0, 255, 0}, false)
	if pc.bg != ([4]float32{1, 0, 0, 0.9}) || pc.fg != ([4]float32{0, 0, 1, 1}) || pc.cursor != ([4]float32{0, 1, 0, 1}) {
		t.Fatalf("overrides = %+v", pc)
	}
	rev := resolvePaneColors(th, pal, [3]uint8{0, 0, 255}, [3]uint8{255, 0, 0}, [3]uint8{0, 255, 0}, true)
	if rev.bg != ([4]float32{0, 0, 1, 0.9}) || rev.fg != ([4]float32{1, 0, 0, 1}) {
		t.Fatalf("reverse = %+v", rev)
	}
}

func TestColorToRGBAUsesPaneColors(t *testing.T) {
	r := newBenchRenderer()
	pc := paneColors{fg: [4]float32{0, 1, 0, 1}, bg: [4]float32{0, 0, 1, 1}}
	pc.palette[42] = [4]float32{0.5, 0.5, 0.5, 1}
	r.pc = &pc
	if got := r.colorToRGBA(grid.Color{Type: grid.ColorIndexed, Index: 42}, false); got != pc.palette[42] {
		t.Fatalf("indexed = %v", got)
	}
	if got := r.colorToRGBA(grid.DefaultBg(), true); got != pc.bg {
		t.Fatalf("default bg = %v", got)
	}
	if bg, ok := r.paneBackground(); !ok || bg != pc.bg {
		t.Fatalf("pane background = %v %v", bg, ok)
	}
	r.pc = nil
	if _, ok := r.paneBackground(); ok {
		t.Fatal("theme background should not need a pane rect")
	}
}

// --- attributes ---

func TestBlinkHidesGlyphUnlessReducedMotion(t *testing.T) {
	r := newBenchRenderer()
	snap := oneCellSnap(textCell('A', grid.FlagBlink))
	r.blinkOff = true
	r.buildGridBatches(snap, 0, 0, 100, 100, false)
	if len(r.gridGlyphs.verts) != 0 {
		t.Fatal("blink-off phase still drew the glyph")
	}
	if !r.sawBlink {
		t.Fatal("blink cell not noticed, so no animation frame is scheduled")
	}
	r.reduceMotion = true
	r.buildGridBatches(snap, 0, 0, 100, 100, false)
	if len(r.gridGlyphs.verts) == 0 {
		t.Fatal("reduced motion must keep blinking text steady")
	}
}

func TestBlinkVisible(t *testing.T) {
	iv := 500 * time.Millisecond
	if !blinkVisible(100*time.Millisecond, iv) || blinkVisible(600*time.Millisecond, iv) || !blinkVisible(1100*time.Millisecond, iv) {
		t.Fatal("blink phase wrong")
	}
}

func TestOverlineNeedsDecoration(t *testing.T) {
	r := newBenchRenderer()
	r.buildGridBatches(oneCellSnap(textCell(' ', grid.FlagOverline)), 0, 0, 100, 100, false)
	if len(r.pass2) != 1 {
		t.Fatalf("overline cell not queued for decoration: %d", len(r.pass2))
	}
}

func TestUnderlineStyleFor(t *testing.T) {
	c := textCell('a', grid.FlagUnderline)
	c.UnderlineStyle = 2
	if got := underlineStyleFor(c, false, false); got != 2 {
		t.Fatalf("double underline lost without undercurl: %d", got)
	}
	c.UnderlineStyle = 3
	if underlineStyleFor(c, false, false) != 1 || underlineStyleFor(c, false, true) != 3 {
		t.Fatal("curly underline toggle wrong")
	}
	if underlineStyleFor(c, true, true) != 1 {
		t.Fatal("hover underline must be solid")
	}
}

// --- images ---

func near(a, b float32) bool { return a-b < 0.001 && b-a < 0.001 }

func quadEq(q, w imgQuad) bool {
	return near(q.x0, w.x0) && near(q.y0, w.y0) && near(q.x1, w.x1) && near(q.y1, w.y1) &&
		near(q.u0, w.u0) && near(q.v0, w.v0) && near(q.u1, w.u1) && near(q.v1, w.v1)
}

func TestPlacementQuad(t *testing.T) {
	img := &images.Image{Width: 20, Height: 20}
	p := &images.Placement{AnchorAbsRow: 12, AnchorCol: 2, Cols: 2, Rows: 1}
	cases := []struct {
		name   string
		p      images.Placement
		cw, ch float32
		want   imgQuad
	}{
		{"natural", *p, 10, 20, imgQuad{20, 40, 40, 60, 0, 0, 1, 1}},
		{"zoomed", *p, 20, 40, imgQuad{40, 80, 80, 120, 0, 0, 1, 1}},
		{"stretched c/r", images.Placement{AnchorAbsRow: 12, AnchorCol: 2, Cols: 4, Rows: 2}, 10, 20, imgQuad{20, 40, 60, 80, 0, 0, 1, 1}},
		{"source rect", images.Placement{AnchorAbsRow: 12, Cols: 1, Rows: 1, SrcX: 10, SrcW: 10, SrcY: 5, SrcH: 10}, 10, 20, imgQuad{0, 40, 10, 50, 0.5, 0.25, 1, 0.75}},
		{"offset", images.Placement{AnchorAbsRow: 12, Cols: 3, Rows: 2, OffsetX: 3, OffsetY: 4}, 10, 20, imgQuad{3, 44, 23, 64, 0, 0, 1, 1}},
	}
	for _, tc := range cases {
		q, ok := placementQuad(&tc.p, img, 10, tc.cw, tc.ch, 10, 20)
		if !ok || !quadEq(q, tc.want) {
			t.Errorf("%s: got %+v ok=%v, want %+v", tc.name, q, ok, tc.want)
		}
	}
}

func TestClipQuad(t *testing.T) {
	q, ok := clipQuad(imgQuad{0, -10, 20, 10, 0, 0, 1, 1}, 100, 100)
	if !ok || !quadEq(q, imgQuad{0, 0, 20, 10, 0, 0.5, 1, 1}) {
		t.Fatalf("top clip = %+v", q)
	}
	q, ok = clipQuad(imgQuad{90, 0, 110, 10, 0, 0, 1, 1}, 100, 100)
	if !ok || !quadEq(q, imgQuad{90, 0, 100, 10, 0, 0, 0.5, 1}) {
		t.Fatalf("right clip = %+v", q)
	}
	if _, ok := clipQuad(imgQuad{0, 120, 20, 140, 0, 0, 1, 1}, 100, 100); ok {
		t.Fatal("off-pane quad should be dropped")
	}
}

func TestSplitLayersByZ(t *testing.T) {
	ps := []*images.Placement{{ZIndex: 5}, {ZIndex: -1}, {ZIndex: -2000000000}, {ZIndex: 0}, {ZIndex: -3}, {Virtual: true}}
	below, under, over := splitLayers(ps, nil, nil, nil)
	if len(below) != 1 || len(under) != 2 || len(over) != 2 {
		t.Fatalf("layers = %d/%d/%d", len(below), len(under), len(over))
	}
	if under[0].ZIndex != -3 || over[0].ZIndex != 0 {
		t.Fatalf("not z-sorted: %d %d", under[0].ZIndex, over[0].ZIndex)
	}
}

func TestDiacriticTable(t *testing.T) {
	if len(placeholderDiacritics) != 297 {
		t.Fatalf("table has %d entries, want 297", len(placeholderDiacritics))
	}
	for i := 1; i < len(placeholderDiacritics); i++ {
		if placeholderDiacritics[i] <= placeholderDiacritics[i-1] {
			t.Fatalf("table not sorted at %d", i)
		}
	}
	if diacriticIndex(0x0305) != 0 || diacriticIndex(0x030D) != 1 || diacriticIndex(0x1D244) != 296 || diacriticIndex('a') != -1 {
		t.Fatal("diacriticIndex wrong")
	}
}

func phCell(id uint32, diacritics ...rune) grid.Cell {
	c := grid.NewCell()
	c.Char = placeholderRune
	c.Fg = grid.Color{Type: grid.ColorRGB, R: uint8(id >> 16), G: uint8(id >> 8), B: uint8(id)}
	c.Combining = diacritics
	return c
}

func TestDecodePlaceholderInheritance(t *testing.T) {
	d := placeholderDiacritics
	first := decodePlaceholder(phCell(7, d[2], d[0]), placeholderInfo{})
	if first.id != 7 || first.row != 2 || first.col != 0 {
		t.Fatalf("first = %+v", first)
	}
	next := decodePlaceholder(phCell(7), first)
	if next.id != 7 || next.row != 2 || next.col != 1 {
		t.Fatalf("inherited = %+v", next)
	}
	rowOnly := decodePlaceholder(phCell(7, d[2]), next)
	if rowOnly.col != 2 {
		t.Fatalf("row-only = %+v", rowOnly)
	}
	msb := decodePlaceholder(phCell(7, d[0], d[0], d[1]), placeholderInfo{})
	if msb.id != 7|1<<24 {
		t.Fatalf("msb id = %#x", msb.id)
	}
	ul := phCell(7, d[0], d[0])
	ul.UnderlineColor = grid.Color{Type: grid.ColorIndexed, Index: 9}
	if p := decodePlaceholder(ul, placeholderInfo{}); p.pid != 9 {
		t.Fatalf("pid = %d", p.pid)
	}
	other := decodePlaceholder(phCell(8), first)
	if other.col != 0 || other.row != 0 {
		t.Fatalf("different image must not inherit: %+v", other)
	}
}

func TestPlaceholderTile(t *testing.T) {
	p := &images.Placement{Cols: 2, Rows: 1, Virtual: true}
	wide := &images.Image{Width: 20, Height: 10}
	q, ok := placeholderTile(p, wide, 0, 1, 10, 10)
	if !ok || !quadEq(q, imgQuad{0, 0, 10, 10, 0.5, 0, 1, 1}) {
		t.Fatalf("tile 1 = %+v", q)
	}
	square := &images.Image{Width: 10, Height: 10}
	q, ok = placeholderTile(p, square, 0, 0, 10, 10)
	if !ok || !quadEq(q, imgQuad{5, 0, 10, 10, 0, 0, 0.5, 1}) {
		t.Fatalf("letterboxed tile = %+v", q)
	}
	if _, ok := placeholderTile(p, square, 1, 0, 10, 10); ok {
		t.Fatal("row outside placement must not draw")
	}
}

func TestTexCacheLifecycle(t *testing.T) {
	uploads, frees := 0, map[uint32]bool{}
	next := uint32(0)
	c := newTexCache(1000, func(*images.Image) uint32 { uploads++; next++; return next }, func(id uint32) { frees[id] = true })
	store := &images.Store{}
	a := &images.Image{ID: 1, Width: 10, Height: 10, Pixels: make([]byte, 400)}
	b := &images.Image{ID: 2, Width: 10, Height: 10, Pixels: make([]byte, 400)}
	c.beginFrame()
	ta := c.get(a, store)
	if c.get(a, store) != ta || uploads != 1 {
		t.Fatal("texture not cached")
	}
	tb := c.get(b, store)
	c.sweepStore(store, map[images.ImageID]*images.Image{1: a})
	if !frees[tb] || frees[ta] {
		t.Fatalf("deleted image texture not freed: %v", frees)
	}
	// Replaced image (same id, new pixels) gets a new texture.
	a2 := &images.Image{ID: 1, Width: 10, Height: 10, Pixels: make([]byte, 400)}
	c.sweepStore(store, map[images.ImageID]*images.Image{1: a2})
	if !frees[ta] {
		t.Fatal("replaced image texture not freed")
	}
	// Budget: LRU eviction skips textures used this frame.
	c.beginFrame()
	x := &images.Image{ID: 3, Pixels: make([]byte, 600)}
	y := &images.Image{ID: 4, Pixels: make([]byte, 600)}
	tx := c.get(x, store)
	c.beginFrame()
	ty := c.get(y, store)
	if !frees[tx] || frees[ty] || c.bytes != 600 {
		t.Fatalf("LRU eviction wrong: frees=%v bytes=%d", frees, c.bytes)
	}
	c.freeAll()
	if !frees[ty] || c.bytes != 0 || len(c.entries) != 0 {
		t.Fatal("freeAll left textures")
	}
}

// --- progress, bell, marks, preedit ---

func TestProgressBar(t *testing.T) {
	if _, ok := progressBar(0, 50, 200, 0); ok {
		t.Fatal("state 0 must hide the bar")
	}
	b, ok := progressBar(1, 40, 200, 0)
	if !ok || b.x != 0 || b.w != 80 || b.kind != progressNormal || b.animated {
		t.Fatalf("normal = %+v", b)
	}
	if b, _ := progressBar(1, 250, 200, 0); b.w != 200 {
		t.Fatalf("value not clamped: %+v", b)
	}
	if b, _ := progressBar(2, 0, 200, 0); b.kind != progressError || b.w != 200 {
		t.Fatalf("error = %+v", b)
	}
	if b, _ := progressBar(4, 50, 200, 0); b.kind != progressWarning || b.w != 100 {
		t.Fatalf("paused = %+v", b)
	}
	b0, _ := progressBar(3, 0, 200, 0)
	b1, _ := progressBar(3, 0, 200, 600*time.Millisecond)
	if !b0.animated || b0.x == b1.x || b1.w <= 0 || b1.x < 0 || b1.x+b1.w > 200 {
		t.Fatalf("indeterminate = %+v -> %+v", b0, b1)
	}
}

func TestBellModes(t *testing.T) {
	if f, a := BellActions("visual"); !f || a {
		t.Fatal("visual")
	}
	if f, a := BellActions("attention"); !f || !a {
		t.Fatal("attention")
	}
	if f, a := BellActions("none"); f || a {
		t.Fatal("none")
	}
	if f, _ := BellActions(""); !f {
		t.Fatal("default should flash")
	}
}

func TestFlashAlpha(t *testing.T) {
	if a := flashAlpha(0, false); a <= 0 {
		t.Fatal("flash not visible at start")
	}
	if flashAlpha(50*time.Millisecond, false) >= flashAlpha(0, false) {
		t.Fatal("flash does not fade")
	}
	if flashAlpha(bellDuration, false) != 0 || flashAlpha(bellDuration, true) != 0 {
		t.Fatal("flash does not end")
	}
	if flashAlpha(0, true) != flashAlpha(bellDuration/2, true) {
		t.Fatal("reduced motion flash must not animate")
	}
}

func TestBellFlashSchedulesFrames(t *testing.T) {
	r := newBenchRenderer()
	r.FlashPane(nil)
	if _, ok := r.NextAnimationFrame(time.Now()); ok {
		t.Fatal("nil pane flashed")
	}
	r.flashes = map[*tab.Pane]time.Time{{}: time.Now()}
	if d, ok := r.NextAnimationFrame(time.Now()); !ok || d > bellDuration {
		t.Fatalf("flash does not request frames: %v %v", d, ok)
	}
	if _, ok := r.NextAnimationFrame(time.Now().Add(time.Second)); ok {
		t.Fatal("finished flash still requests frames")
	}
}

func TestGutterMarks(t *testing.T) {
	marks := []grid.PromptMark{{AbsRow: 9}, {AbsRow: 10, HasExit: true}, {AbsRow: 12, HasExit: true, Exit: 1}, {AbsRow: 40}}
	got := gutterMarks(marks, 10, 5, nil)
	if len(got) != 2 || got[0].row != 0 || got[0].status != markSuccess || got[1].row != 2 || got[1].status != markFailure {
		t.Fatalf("gutter = %+v", got)
	}
	ticks := scrollbarTicks(marks, 0, 99, 200, nil)
	if len(ticks) != 4 || !near(ticks[1].y, 20) || ticks[2].status != markFailure || ticks[0].status != markUnknown {
		t.Fatalf("ticks = %+v", ticks)
	}
}

func TestPreeditLayout(t *testing.T) {
	start, cells, caret := preeditLayout("日本", 3, 5, 80)
	if start != 5 || cells != 4 || caret != 2 {
		t.Fatalf("layout = %d %d %d", start, cells, caret)
	}
	start, _, _ = preeditLayout("hello", -1, 78, 80)
	if start != 75 {
		t.Fatalf("overflow not shifted: %d", start)
	}
	if _, cells, _ := preeditLayout("", 0, 0, 80); cells != 0 {
		t.Fatal("empty preedit")
	}
}

func TestSetPreeditLatchesRedraw(t *testing.T) {
	r := newBenchRenderer()
	r.SetPreedit("か", 3)
	if !r.ConsumeUIDirty() || r.preedit != "か" || r.preeditCursor != 3 {
		t.Fatal("SetPreedit not stored or not redrawn")
	}
	r.SetPreedit("か", 3)
	if r.ConsumeUIDirty() {
		t.Fatal("unchanged preedit forced a redraw")
	}
}

func TestPlaceholderCellsSkipGlyphs(t *testing.T) {
	r := newBenchRenderer()
	r.glyphs[placeholderRune] = r.glyphs['a']
	r.buildGridBatches(oneCellSnap(phCell(7, placeholderDiacritics[0], placeholderDiacritics[0])), 0, 0, 100, 100, false)
	if len(r.gridGlyphs.verts) != 0 {
		t.Fatal("placeholder drew a glyph")
	}
	if len(r.placeholders) != 1 || r.placeholders[0].info.id != 7 {
		t.Fatalf("placeholders = %+v", r.placeholders)
	}
}

func TestPlaceholdersFromParser(t *testing.T) {
	term := parser.NewTerminal(10, 2)
	term.Process([]byte("\x1b[38;2;0;0;7m\U0010EEEE̅̅\U0010EEEE\U0010EEEE"))
	r := newBenchRenderer()
	r.buildGridBatches(term.Snapshot(), 0, 0, 1000, 1000, false)
	if len(r.placeholders) != 3 {
		t.Fatalf("placeholders = %+v", r.placeholders)
	}
	for i, ph := range r.placeholders {
		if ph.info.id != 7 || ph.info.row != 0 || ph.info.col != i {
			t.Fatalf("placeholder %d = %+v", i, ph.info)
		}
	}
}

func TestApplyPaneColorsFromTerminal(t *testing.T) {
	r := newBenchRenderer()
	r.theme = testTheme()
	term := parser.NewTerminal(10, 2)
	r.applyPaneColors(term, false)
	if _, ok := r.paneBackground(); ok {
		t.Fatal("theme bg should not need a pane rect")
	}
	if pal, _, bg, _ := term.EffectiveColors(); bg != to8(r.theme.Background) || pal[1] != to8(standard16[1]) {
		t.Fatalf("terminal not seeded with theme defaults: bg=%v", bg)
	}
	term.Process([]byte("\x1b]11;rgb:ff/00/00\x07\x1b]4;1;rgb:00/ff/00\x07"))
	r.applyPaneColors(term, false)
	if bg, ok := r.paneBackground(); !ok || bg != ([4]float32{1, 0, 0, r.theme.Background[3]}) {
		t.Fatalf("OSC 11 bg = %v %v", bg, ok)
	}
	if got := r.colorToRGBA(grid.Color{Type: grid.ColorIndexed, Index: 1}, false); got != ([4]float32{0, 1, 0, 1}) {
		t.Fatalf("OSC 4 palette = %v", got)
	}
	r.applyPaneColors(term, true)
	if r.defaultFg() != ([4]float32{1, 0, 0, 1}) {
		t.Fatalf("reverse fg = %v", r.defaultFg())
	}
}
