package render

import (
	"fmt"

	"github.com/go-gl/gl/v4.1-core/gl"
)

// Batched rendering. The terminal grid is drawn by accumulating all background
// rectangles into one buffer and all glyph quads into another, then issuing a
// single draw call each — replacing the previous one-draw-call-per-cell loop.
//
// Both batches use per-vertex color (so a whole frame's varied colors fit in
// one draw). The rect batch is also reused for selection highlights; underlines,
// strikethrough, block elements, and the cursor remain immediate draws (a small
// minority of cells), drawn after the batches flush.

// rectBatch accumulates colored rectangles: 6 vertices/rect, 6 floats/vertex
// (x, y, r, g, b, a).
type rectBatch struct {
	verts []float32
}

func (b *rectBatch) reset() { b.verts = b.verts[:0] }

func (b *rectBatch) addRect(x, y, w, h float32, c [4]float32) {
	x2, y2 := x+w, y+h
	b.verts = append(b.verts,
		x, y, c[0], c[1], c[2], c[3],
		x2, y, c[0], c[1], c[2], c[3],
		x2, y2, c[0], c[1], c[2], c[3],
		x, y, c[0], c[1], c[2], c[3],
		x2, y2, c[0], c[1], c[2], c[3],
		x, y2, c[0], c[1], c[2], c[3],
	)
}

// glyphBatch accumulates textured glyph quads: 6 vertices/quad, 8 floats/vertex
// (x, y, u, v, r, g, b, a).
type glyphBatch struct {
	verts []float32
}

func (b *glyphBatch) reset() { b.verts = b.verts[:0] }

// addGlyph appends a quad covering screen rect [x,y2]..[x2,y] (y is the cell
// baseline-bottom; the glyph extends upward by its pixel height) with the given
// atlas UV rect and color. shear slants the top edge (faux italic).
func (b *glyphBatch) addGlyph(x, y, w, h, tx, ty, tw, th float32, c [4]float32, shear float32) {
	yTop := y - h
	x2 := x + w
	b.verts = append(b.verts,
		x+shear, yTop, tx, ty, c[0], c[1], c[2], c[3],
		x2+shear, yTop, tx+tw, ty, c[0], c[1], c[2], c[3],
		x2, y, tx+tw, ty+th, c[0], c[1], c[2], c[3],
		x+shear, yTop, tx, ty, c[0], c[1], c[2], c[3],
		x2, y, tx+tw, ty+th, c[0], c[1], c[2], c[3],
		x, y, tx, ty+th, c[0], c[1], c[2], c[3],
	)
}

// isBlockElement reports whether a rune is a block/quadrant element that the
// renderer draws as geometry (U+2580–U+259F) rather than as a font glyph.
func isBlockElement(c rune) bool { return c >= 0x2580 && c <= 0x259F }

// initBatches creates the two batch shader programs and their VAOs/VBOs.
func (r *Renderer) initBatches() error {
	rectVert := `
		#version 410 core
		layout (location = 0) in vec2 aPos;
		layout (location = 1) in vec4 aColor;
		uniform mat4 projection;
		out vec4 vColor;
		void main() {
			gl_Position = projection * vec4(aPos, 0.0, 1.0);
			vColor = aColor;
		}
	` + "\x00"
	rectFrag := `
		#version 410 core
		in vec4 vColor;
		out vec4 FragColor;
		void main() { FragColor = vColor; }
	` + "\x00"

	var err error
	r.rectBatchProgram, err = createProgram(rectVert, rectFrag)
	if err != nil {
		return fmt.Errorf("rect batch shader: %w", err)
	}
	r.rectBatchProjLoc = gl.GetUniformLocation(r.rectBatchProgram, gl.Str("projection\x00"))

	glyphVert := `
		#version 410 core
		layout (location = 0) in vec2 aPos;
		layout (location = 1) in vec2 aTex;
		layout (location = 2) in vec4 aColor;
		uniform mat4 projection;
		out vec2 TexCoords;
		out vec4 vColor;
		void main() {
			gl_Position = projection * vec4(aPos, 0.0, 1.0);
			TexCoords = aTex;
			vColor = aColor;
		}
	` + "\x00"
	glyphFrag := `
		#version 410 core
		in vec2 TexCoords;
		in vec4 vColor;
		out vec4 FragColor;
		uniform sampler2D text;
		void main() {
			float a = texture(text, TexCoords).r;
			FragColor = vec4(vColor.rgb, vColor.a * a);
		}
	` + "\x00"
	r.glyphBatchProgram, err = createProgram(glyphVert, glyphFrag)
	if err != nil {
		return fmt.Errorf("glyph batch shader: %w", err)
	}
	r.glyphBatchProjLoc = gl.GetUniformLocation(r.glyphBatchProgram, gl.Str("projection\x00"))
	r.glyphBatchTexLoc = gl.GetUniformLocation(r.glyphBatchProgram, gl.Str("text\x00"))

	// Rect batch VAO/VBO: vec2 pos + vec4 color, stride 6 floats.
	gl.GenVertexArrays(1, &r.rectBatchVAO)
	gl.GenBuffers(1, &r.rectBatchVBO)
	gl.BindVertexArray(r.rectBatchVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.rectBatchVBO)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 6*4, 0)
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointerWithOffset(1, 4, gl.FLOAT, false, 6*4, 2*4)
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)

	// Glyph batch VAO/VBO: vec2 pos + vec2 tex + vec4 color, stride 8 floats.
	gl.GenVertexArrays(1, &r.glyphBatchVAO)
	gl.GenBuffers(1, &r.glyphBatchVBO)
	gl.BindVertexArray(r.glyphBatchVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.glyphBatchVBO)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 8*4, 0)
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointerWithOffset(1, 2, gl.FLOAT, false, 8*4, 2*4)
	gl.EnableVertexAttribArray(2)
	gl.VertexAttribPointerWithOffset(2, 4, gl.FLOAT, false, 8*4, 4*4)
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)

	return nil
}

// UI batch layer. drawRect and drawChar*/drawText* (tab bar, panels, toasts,
// help, cursor, underline decorations) append here instead of issuing one
// draw call + one allocation per rect/char. Switching kind (rect<->glyph),
// changing projection, or hitting a non-batched draw (grid batches, color
// emoji) flushes, so paint order is exactly what immediate mode produced.
const (
	uiKindNone  = 0
	uiKindRect  = 1
	uiKindGlyph = 2
)

// uiEnsure prepares the UI batch of the given kind, flushing pending work of
// the other kind (or a different projection) first.
func (r *Renderer) uiEnsure(kind int, proj [16]float32) {
	if r.uiKind != kind || r.uiProj != proj {
		r.uiFlush()
		r.uiKind = kind
		r.uiProj = proj
		r.uiAtlasGen = r.atlasGen
	}
}

// uiFlush issues any pending UI batch. Must run before any draw that bypasses
// the UI batches and at the end of every top-level Render* entry point.
func (r *Renderer) uiFlush() {
	switch r.uiKind {
	case uiKindRect:
		r.flushRects(&r.uiRects, r.uiProj)
		r.uiRects.reset()
	case uiKindGlyph:
		r.flushGlyphs(&r.uiGlyphs, r.uiProj)
		r.uiGlyphs.reset()
	}
	r.uiKind = uiKindNone
}

// flushRects uploads and draws all accumulated rectangles in one call.
func (r *Renderer) flushRects(b *rectBatch, proj [16]float32) {
	if len(b.verts) == 0 {
		return
	}
	gl.UseProgram(r.rectBatchProgram)
	gl.UniformMatrix4fv(r.rectBatchProjLoc, 1, false, &proj[0])
	gl.BindVertexArray(r.rectBatchVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.rectBatchVBO)
	if len(b.verts) > r.rectBatchCap {
		gl.BufferData(gl.ARRAY_BUFFER, len(b.verts)*4, gl.Ptr(b.verts), gl.DYNAMIC_DRAW)
		r.rectBatchCap = len(b.verts)
	} else {
		gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(b.verts)*4, gl.Ptr(b.verts))
	}
	gl.DrawArrays(gl.TRIANGLES, 0, int32(len(b.verts)/6))
	gl.BindVertexArray(0)
}

// flushGlyphs uploads and draws all accumulated glyph quads in one call.
func (r *Renderer) flushGlyphs(b *glyphBatch, proj [16]float32) {
	if len(b.verts) == 0 {
		return
	}
	gl.UseProgram(r.glyphBatchProgram)
	gl.UniformMatrix4fv(r.glyphBatchProjLoc, 1, false, &proj[0])
	gl.Uniform1i(r.glyphBatchTexLoc, 0)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, r.fontAtlas)
	gl.BindVertexArray(r.glyphBatchVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.glyphBatchVBO)
	if len(b.verts) > r.glyphBatchCap {
		gl.BufferData(gl.ARRAY_BUFFER, len(b.verts)*4, gl.Ptr(b.verts), gl.DYNAMIC_DRAW)
		r.glyphBatchCap = len(b.verts)
	} else {
		gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(b.verts)*4, gl.Ptr(b.verts))
	}
	gl.DrawArrays(gl.TRIANGLES, 0, int32(len(b.verts)/8))
	gl.BindVertexArray(0)
}
