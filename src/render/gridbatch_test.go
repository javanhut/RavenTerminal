package render

import (
	"math/rand"
	"testing"

	"github.com/javanhut/RavenTerminal/src/grid"
)

// newBenchRenderer builds a GL-free Renderer with a pre-warmed glyph cache:
// every rune the workloads use resolves from the map without rasterization.
func newBenchRenderer() *Renderer {
	r := &Renderer{
		cellWidth:   9,
		cellHeight:  18,
		atlasAscent: 14,
		glyphs:      make(map[rune]Glyph),
		glyphMisses: make(map[rune]bool),
		fauxBold:    true,
		fauxItalic:  true,
	}
	r.theme.Background = [4]float32{0.1, 0.1, 0.12, 1}
	r.theme.Foreground = [4]float32{0.9, 0.9, 0.9, 1}
	r.theme.Selection = [4]float32{0.3, 0.4, 0.6, 1}
	fake := Glyph{X: 0.1, Y: 0.1, Width: 0.01, Height: 0.02, PixelWidth: 9, PixelHeight: 14, OffsetX: 0, OffsetY: -11}
	for c := rune(0); c < 128; c++ {
		r.glyphs[c] = fake
	}
	for _, c := range []rune{'世', '─', '│', '┌', '🙂'} {
		r.glyphs[c] = fake
	}
	return r
}

// makeSnapshot builds a deterministic pseudo-random snapshot exercising plain
// text, SGR colors, attribute flags, wide chars, block elements and blanks.
func makeSnapshot(rng *rand.Rand, cols, rows int, styled bool) *grid.Snapshot {
	snap := &grid.Snapshot{Cols: cols, Rows: rows, Cells: make([]grid.Cell, cols*rows)}
	plainRunes := []rune("The quick brown fox jumps over the lazy dog 0123456789")
	blocks := []rune{'▀', '▄', '█', '▌', '▖'}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			cell := grid.NewCell()
			roll := rng.Intn(100)
			switch {
			case roll < 5:
				cell.Char = ' '
			case roll < 8:
				cell.Char = blocks[rng.Intn(len(blocks))]
			case roll < 10 && col+1 < cols:
				cell.Char = '世'
				cell.Width = grid.CellWidthWide
				snap.Cells[row*cols+col] = cell
				col++
				cont := grid.NewCell()
				cont.Char = 0
				cont.Width = grid.CellWidthContinuation
				snap.Cells[row*cols+col] = cont
				continue
			default:
				cell.Char = plainRunes[rng.Intn(len(plainRunes))]
			}
			if styled {
				switch rng.Intn(4) {
				case 0: // default colors
				case 1:
					cell.Fg = grid.Color{Type: grid.ColorIndexed, Index: uint8(rng.Intn(256))}
					cell.Bg = grid.Color{Type: grid.ColorIndexed, Index: uint8(rng.Intn(256))}
				case 2:
					cell.Fg = grid.Color{Type: grid.ColorRGB, R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256))}
				case 3:
					cell.Bg = grid.Color{Type: grid.ColorIndexed, Index: uint8(rng.Intn(16))}
				}
				for _, f := range []grid.CellFlags{grid.FlagBold, grid.FlagItalic, grid.FlagDim, grid.FlagInverse, grid.FlagUnderline, grid.FlagStrikethrough, grid.FlagHidden} {
					if rng.Intn(16) == 0 {
						cell.Flags |= f
					}
				}
			}
			snap.Cells[row*cols+col] = cell
		}
	}
	return snap
}

// referenceBuildGridBatches is a verbatim copy of the original renderGridAt
// pass-1 per-cell loop (before the optimization), kept here so tests can
// assert the optimized buildGridBatches produces identical batches.
func referenceBuildGridBatches(r *Renderer, snap *grid.Snapshot, offsetX, offsetY, paneWidth, paneHeight float32, hoverRow int, rb *rectBatch, gb *glyphBatch, p2 []pass2Item) ([]float32, []float32, []pass2Item) {
	cols := snap.Cols
	rows := snap.Rows
	rb.reset()
	gb.reset()
	p2 = p2[:0]
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			cell := snap.Cells[row*cols+col]
			x := offsetX + float32(col)*r.cellWidth
			y := offsetY + float32(row)*r.cellHeight
			if x+r.cellWidth > offsetX+paneWidth || y+r.cellHeight > offsetY+paneHeight {
				continue
			}

			bgColor := r.colorToRGBA(cell.Bg, true)
			if cell.Flags&grid.FlagInverse != 0 {
				bgColor = r.colorToRGBA(cell.Fg, false)
			}
			if bgColor != r.theme.Background {
				rb.addRect(x, y, r.cellWidth+0.5, r.cellHeight, bgColor)
			}
			if snap.Selected(col, row) {
				rb.addRect(x, y, r.cellWidth+0.5, r.cellHeight, r.theme.Selection)
			}

			if cell.Width == grid.CellWidthContinuation {
				continue
			}

			hidden := cell.Flags&grid.FlagHidden != 0
			isBlock := isBlockElement(cell.Char)
			needGlyph := !hidden && cell.Char != ' ' && cell.Char != 0 && !isBlock
			hovered := row == hoverRow && col >= r.hoverStartCol && col <= r.hoverEndCol
			needDecor := !hidden && (isBlock || hovered ||
				cell.Flags&(grid.FlagUnderline|grid.FlagStrikethrough) != 0)
			if !needGlyph && !needDecor {
				continue
			}

			fgColor := r.colorToRGBA(cell.Fg, false)
			if cell.Flags&grid.FlagInverse != 0 {
				fgColor = r.colorToRGBA(cell.Bg, true)
			}
			if cell.Flags&grid.FlagDim != 0 {
				fgColor[3] = fgColor[3] / 2
			}
			if needDecor {
				p2 = append(p2, pass2Item{col: col, row: row, x: x, y: y, fg: fgColor, hovered: hovered})
			}

			if needGlyph {
				// Old resolveGlyph without the ASCII cache: ensureGlyph plus
				// the two fallback tables on miss.
				g, ok := r.ensureGlyph(cell.Char)
				if !ok {
					if fb, has := boxDrawingFallbacks[cell.Char]; has {
						g, ok = r.ensureGlyph(fb)
					}
					if !ok {
						if fb, has := unicodeFallbacks[cell.Char]; has {
							g, ok = r.ensureGlyph(fb)
						}
					}
				}
				if !ok {
					if cg, isColor := r.ensureColorGlyph(cell.Char); isColor {
						span := 1
						if cell.Width == grid.CellWidthWide {
							span = 2
						}
						_ = cg
						_ = span
					} else {
						g, ok = r.ensureGlyph('?')
					}
				}
				if ok && g.PixelWidth > 0 {
					span := 1
					if cell.Width == grid.CellWidthWide {
						span = 2
					} else if isIconRune(cell.Char) && col+1 < cols {
						next := snap.Cells[row*cols+col+1]
						if next.Char == ' ' || next.Char == 0 {
							span = 2
						}
					}
					if span == 2 && x+2*r.cellWidth > offsetX+paneWidth {
						span = 1
					}
					gx, gyTop, gw, gh := r.glyphQuad(cell.Char, g, x, y, span)
					var shear float32
					if r.fauxItalic && cell.Flags&grid.FlagItalic != 0 {
						shear = gh * 0.2
					}
					gb.addGlyph(gx, gyTop+gh, gw, gh, g.X, g.Y, g.Width, g.Height, fgColor, shear)
					if r.fauxBold && cell.Flags&grid.FlagBold != 0 {
						gb.addGlyph(gx+1, gyTop+gh, gw, gh, g.X, g.Y, g.Width, g.Height, fgColor, shear)
					}
				}
			}
		}
	}
	return rb.verts, gb.verts, p2
}

// TestBuildGridBatchesMatchesReference pins the optimized batch builder to the
// original per-cell loop: identical vertex bytes and pass-2 items across
// randomized contents, styles, hover ranges and pane clipping.
func TestBuildGridBatchesMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	geometries := [][2]int{{107, 30}, {55, 16}, {147, 41}, {1, 1}, {3, 2}}
	for _, styled := range []bool{false, true} {
		for _, geo := range geometries {
			cols, rows := geo[0], geo[1]
			snap := makeSnapshot(rng, cols, rows, styled)
			cw, ch := float32(9), float32(18)
			clips := []struct{ offX, offY, w, h float32 }{
				{0, 0, float32(cols) * cw, float32(rows) * ch},         // exact fit
				{10, 20, float32(cols)*cw - 4.5, float32(rows)*ch - 1}, // clip right/bottom
				{0, 0, float32(cols)*cw + 100, float32(rows)*ch + 50},  // oversized pane
				{7, 3, 0.5 * cw, 1.5 * ch},                             // tiny pane
			}
			for ci, clip := range clips {
				r := newBenchRenderer()
				hoverRow := -1
				if ci%2 == 1 {
					r.hoverActive = true
					r.hoverStartCol, r.hoverEndCol = 1, 3
					hoverRow = rows / 2
				}
				var rb rectBatch
				var gb glyphBatch
				wantRects, wantGlyphs, wantP2 := referenceBuildGridBatches(r, snap, clip.offX, clip.offY, clip.w, clip.h, hoverRow, &rb, &gb, nil)
				r.buildGridBatches(snap, clip.offX, clip.offY, clip.w, clip.h, hoverRow)

				if len(r.colorDraws) != 0 {
					t.Fatalf("styled=%v geo=%v clip=%d: unexpected color draws", styled, geo, ci)
				}
				if !floatsEqual(r.gridRects.verts, wantRects) {
					t.Fatalf("styled=%v geo=%v clip=%d: rect verts differ (got %d floats, want %d)",
						styled, geo, ci, len(r.gridRects.verts), len(wantRects))
				}
				if !floatsEqual(r.gridGlyphs.verts, wantGlyphs) {
					t.Fatalf("styled=%v geo=%v clip=%d: glyph verts differ (got %d floats, want %d)",
						styled, geo, ci, len(r.gridGlyphs.verts), len(wantGlyphs))
				}
				if len(r.pass2) != len(wantP2) {
					t.Fatalf("styled=%v geo=%v clip=%d: pass2 len %d, want %d", styled, geo, ci, len(r.pass2), len(wantP2))
				}
				for i := range wantP2 {
					if r.pass2[i] != wantP2[i] {
						t.Fatalf("styled=%v geo=%v clip=%d: pass2[%d] = %+v, want %+v", styled, geo, ci, i, r.pass2[i], wantP2[i])
					}
				}
			}
		}
	}
}

// TestFitCellsMatchesClipPredicate verifies the hoisted binary search returns
// exactly the per-cell clip decision for every cell.
func TestFitCellsMatchesClipPredicate(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 2000; i++ {
		offset := float32(rng.Intn(40))
		sz := float32(rng.Intn(20)+1) + float32(rng.Intn(100))/100
		n := rng.Intn(300)
		limit := offset + float32(rng.Intn(int(sz)*n+2))
		got := fitCells(offset, sz, n, limit)
		want := 0
		for c := 0; c < n; c++ {
			if offset+float32(c)*sz+sz > limit {
				break
			}
			want++
		}
		if got != want {
			t.Fatalf("fitCells(%v,%v,%d,%v) = %d, want %d", offset, sz, n, limit, got, want)
		}
	}
}

func floatsEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func benchGrid(b *testing.B, cols, rows int, styled bool, optimized bool) {
	rng := rand.New(rand.NewSource(1))
	snap := makeSnapshot(rng, cols, rows, styled)
	r := newBenchRenderer()
	w := float32(cols) * r.cellWidth
	h := float32(rows) * r.cellHeight
	var rb rectBatch
	var gb glyphBatch
	var p2 []pass2Item
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if optimized {
			r.buildGridBatches(snap, 0, 0, w, h, -1)
		} else {
			_, _, p2 = referenceBuildGridBatches(r, snap, 0, 0, w, h, -1, &rb, &gb, p2)
		}
	}
}

// The "reference" variants measure the pre-optimization per-cell loop (kept
// for the equivalence test); the plain ones measure the optimized builder.
func BenchmarkBuildGridBatches(b *testing.B) {
	for _, geo := range [][2]int{{107, 30}, {147, 41}} {
		for _, styled := range []bool{false, true} {
			name := geoName(geo, styled, true)
			b.Run(name, func(b *testing.B) { benchGrid(b, geo[0], geo[1], styled, true) })
			b.Run(geoName(geo, styled, false), func(b *testing.B) { benchGrid(b, geo[0], geo[1], styled, false) })
		}
	}
}

func geoName(geo [2]int, styled, optimized bool) string {
	s := "plain"
	if styled {
		s = "sgr"
	}
	o := "optimized"
	if !optimized {
		o = "reference"
	}
	return s + "_" + itoa(geo[0]) + "x" + itoa(geo[1]) + "/" + o
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
