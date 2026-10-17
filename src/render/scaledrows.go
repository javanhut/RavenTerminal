package render

import (
	"math"

	"github.com/javanhut/RavenTerminal/src/grid"
)

// Scaled text: DECDWL/DECDHL rows and OSC 66 sized text are laid out at 1x
// into a scratch batch, then each quad is scaled and clipped into the grid
// batch, so they share the normal glyph pipeline and draw call.

// clipBox is an axis-aligned clip rectangle in pane pixels.
type clipBox struct{ x0, y0, x1, y1 float32 }

var noClip = clipBox{-math.MaxFloat32, -math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}

// appendScaled appends src's quads to dst, mapping a 1x point (x, y) to
// (nx + (x-ox)*sx, ny + (y-oy)*sy) and clipping to clip with the texture
// coordinates trimmed in proportion. Only position and texture coordinates
// change; every other vertex attribute is copied through.
func appendScaled(dst, src *glyphBatch, ox, oy, nx, ny, sx, sy float32, clip clipBox) {
	const n = glyphVertFloats
	const stride = 6 * n
	v := src.verts
	for i := 0; i+stride <= len(v); i += stride {
		q := v[i : i+stride]
		v0, v2, v5 := q[0:], q[2*n:], q[5*n:]
		x, x2 := nx+(v5[0]-ox)*sx, nx+(v2[0]-ox)*sx
		yTop, y := ny+(v0[1]-oy)*sy, ny+(v2[1]-oy)*sy
		shear := (v0[0] - v5[0]) * sx
		tx, ty, tw, th := v5[2], v0[3], v2[2]-v5[2], v2[3]-v0[3]
		if y <= yTop || x2 <= x {
			continue
		}
		if yTop < clip.y0 {
			f := (clip.y0 - yTop) / (y - yTop)
			ty += th * f
			th -= th * f
			shear *= 1 - f
			yTop = clip.y0
		}
		if y > clip.y1 {
			f := (y - clip.y1) / (y - yTop)
			th -= th * f
			x += shear * f
			x2 += shear * f
			shear *= 1 - f
			y = clip.y1
		}
		if x < clip.x0 {
			f := (clip.x0 - x) / (x2 - x)
			tx += tw * f
			tw -= tw * f
			x = clip.x0
		}
		if x2 > clip.x1 {
			tw -= tw * (x2 - clip.x1) / (x2 - x)
			x2 = clip.x1
		}
		if y <= yTop || x2 <= x {
			continue
		}
		at := len(dst.verts)
		dst.verts = append(dst.verts, q...)
		out := dst.verts[at:]
		set := func(k int, px, py, u, w float32) {
			out[k*n], out[k*n+1], out[k*n+2], out[k*n+3] = px, py, u, w
		}
		set(0, x+shear, yTop, tx, ty)
		set(1, x2+shear, yTop, tx+tw, ty)
		set(2, x2, y, tx+tw, ty+th)
		set(3, x+shear, yTop, tx, ty)
		set(4, x2, y, tx+tw, ty+th)
		set(5, x, y, tx, ty+th)
	}
}

// flushScaledRow moves the scratch glyphs of a DECDWL/DECDHL row, laid out at
// 1x from (ox, rowTop), into the grid batch at 2x. Double-height rows show the
// top or bottom half of the 2x text, clipped to the row.
func (r *Renderer) flushScaledRow(ls grid.LineSize, ox, rowTop, clipRight float32) {
	clip := noClip
	clip.x1 = clipRight
	ny, sy := rowTop, float32(1)
	switch ls {
	case grid.LineDoubleTop, grid.LineDoubleBottom:
		sy = 2
		clip.y0, clip.y1 = rowTop, rowTop+r.cellHeight
		if ls == grid.LineDoubleBottom {
			ny = rowTop - r.cellHeight
		}
	}
	appendScaled(&r.gridGlyphs, &r.scaledGlyphs, ox, rowTop, ox, ny, 2, sy, clip)
	r.scaledGlyphs.reset()
}

// inSized reports whether col is covered by one of a row's sized-text blocks.
func inSized(blocks []grid.SizedText, col int) bool {
	for _, b := range blocks {
		if col >= b.Col && col < b.Col+b.Cells {
			return true
		}
	}
	return false
}

// cellFg resolves a cell's text colour as the grid pass does.
func (r *Renderer) cellFg(cell grid.Cell) [4]float32 {
	if cell.Fg.Type == grid.ColorDefault && cell.Flags&(grid.FlagInverse|grid.FlagDim) == 0 {
		return r.defaultFg()
	}
	fg := r.colorToRGBA(cell.Fg, false)
	if cell.Flags&grid.FlagInverse != 0 {
		fg = r.colorToRGBA(cell.Bg, true)
	}
	if cell.Flags&grid.FlagDim != 0 {
		fg[3] /= 2
	}
	return fg
}

// alignOffset places a span of size used inside room: 0 start, 1 end, 2 centre.
func alignOffset(align int, room, used float32) float32 {
	switch align {
	case 1:
		return room - used
	case 2:
		return (room - used) / 2
	}
	return 0
}

// emitSizedText draws the part of each OSC 66 block that falls on the row at
// rowTop: the text is laid out at 1x, scaled by s (times n/d), aligned in the
// block of Cells x Scale cells and clipped to this row of it.
func (r *Renderer) emitSizedText(rowCells []grid.Cell, blocks []grid.SizedText, ox, rowTop, clipRight float32) {
	cw, ch := r.cellWidth, r.cellHeight
	saved := r.ligRow
	r.ligRow = r.ligRow[:0]
	defer func() { r.ligRow = saved }()
	for _, b := range blocks {
		if b.Col < 0 || b.Col >= len(rowCells) || b.Cells <= 0 {
			continue
		}
		base := rowCells[b.Col]
		if base.Flags&grid.FlagHidden != 0 {
			continue
		}
		cells := r.sizedCells[:0]
		for _, c := range b.Text {
			switch w := grid.RuneWidth(c); {
			case w <= 0:
				n := len(cells) - 1
				if n > 0 && cells[n].Width == grid.CellWidthContinuation {
					n--
				}
				if n >= 0 {
					cells[n].Combining = append(cells[n].Combining, c)
				}
			default:
				cell := grid.Cell{Char: c, Flags: base.Flags, Width: grid.CellWidthNormal}
				if w == 2 {
					cell.Width = grid.CellWidthWide
				}
				cells = append(cells, cell)
				if w == 2 {
					cells = append(cells, grid.Cell{Width: grid.CellWidthContinuation})
				}
			}
		}
		r.sizedCells = cells
		fg := r.cellFg(base)
		if base.Bg.Type != grid.ColorDefault {
			r.scaledGlyphs.setBg(r.colorToRGBA(base.Bg, true))
		} else {
			r.scaledGlyphs.clearBg()
		}
		for i := range cells {
			if cells[i].Width != grid.CellWidthContinuation && cells[i].Char != ' ' {
				r.emitCellGlyph(&r.scaledGlyphs, cells, i, float32(i)*cw, 0, math.MaxFloat32, fg)
			}
		}
		scale := float32(max(b.Scale, 1))
		if b.Num > 0 && b.Den > b.Num {
			scale *= float32(b.Num) / float32(b.Den)
		}
		blockW, blockH := float32(b.Cells)*cw, float32(max(b.Scale, 1))*ch
		bx, blockTop := ox+float32(b.Col)*cw, rowTop-float32(b.SubRow)*ch
		nx := bx + alignOffset(b.HAlign, blockW, float32(len(cells))*cw*scale)
		ny := blockTop + alignOffset(b.VAlign, blockH, ch*scale)
		clip := clipBox{bx, rowTop, min(bx+blockW, clipRight), rowTop + ch}
		appendScaled(&r.gridGlyphs, &r.scaledGlyphs, 0, 0, nx, ny, scale, scale, clip)
		r.scaledGlyphs.reset()
	}
}

// cursorRect is where the cursor is drawn: the box it fills and, for text
// under a block cursor, where the top of the (possibly 2x) glyph sits. A
// cursor on a double-height pair spans both rows.
type cursorRect struct {
	x, y, w, h, glyphTop float32
	size                 grid.LineSize
}

func cursorBox(snap *grid.Snapshot, col, row int, ox, oy, cw, ch float32) cursorRect {
	ls := snap.LineSize(row)
	rowTop := oy + float32(row)*ch
	b := cursorRect{x: ox + float32(col)*cw, y: rowTop, w: cw, h: ch, glyphTop: rowTop, size: ls}
	if !ls.Doubled() {
		return b
	}
	b.x += float32(col) * cw
	b.w = 2 * cw
	switch ls {
	case grid.LineDoubleTop:
		if snap.LineSize(row+1) == grid.LineDoubleBottom {
			b.h = 2 * ch
		}
	case grid.LineDoubleBottom:
		b.glyphTop = rowTop - ch
		if row > 0 && snap.LineSize(row-1) == grid.LineDoubleTop {
			b.y, b.h = rowTop-ch, 2*ch
		}
	}
	return b
}
