package render

import (
	"fmt"
	"math"

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

// glyphBatch accumulates textured glyph quads: 6 vertices/quad,
// glyphVertFloats floats/vertex (x, y, u, v, r, g, b, a, layer, bg). layer >= 0
// samples that page of the mono atlas tinted by the color; layer < 0 samples
// page -layer-1 of the color-emoji atlas untinted. bg is the luminance of the
// background behind the glyph (for gamma/contrast), negative for the theme's.
type glyphBatch struct {
	verts []float32
	bgY   float32
	bgSet bool
}

const glyphVertFloats = 10

func (b *glyphBatch) reset() { b.verts = b.verts[:0] }

// setBg sets the background later quads are composed against.
func (b *glyphBatch) setBg(c [4]float32) { b.bgY, b.bgSet = textLuma(c), true }

// clearBg makes later quads compose against the theme background.
func (b *glyphBatch) clearBg() { b.bgSet = false }

func textLuma(c [4]float32) float32 { return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2] }

// addGlyph appends a quad covering screen rect [x,y2]..[x2,y] (y is the cell
// baseline-bottom; the glyph extends upward by its pixel height) with the given
// atlas UV rect and color. shear slants the top edge (faux italic).
func (b *glyphBatch) addGlyph(x, y, w, h, tx, ty, tw, th float32, c [4]float32, shear float32) {
	b.addGlyphL(x, y, w, h, tx, ty, tw, th, c, shear, 0)
}

// addGlyphL is addGlyph for an explicit atlas layer.
func (b *glyphBatch) addGlyphL(x, y, w, h, tx, ty, tw, th float32, c [4]float32, shear, l float32) {
	yTop := y - h
	x2 := x + w
	bg := float32(-1)
	if b.bgSet {
		bg = b.bgY
	}
	b.verts = append(b.verts,
		x+shear, yTop, tx, ty, c[0], c[1], c[2], c[3], l, bg,
		x2+shear, yTop, tx+tw, ty, c[0], c[1], c[2], c[3], l, bg,
		x2, y, tx+tw, ty+th, c[0], c[1], c[2], c[3], l, bg,
		x+shear, yTop, tx, ty, c[0], c[1], c[2], c[3], l, bg,
		x2, y, tx+tw, ty+th, c[0], c[1], c[2], c[3], l, bg,
		x, y, tx, ty+th, c[0], c[1], c[2], c[3], l, bg,
	)
}

// isGeometryRune reports whether a rune is one the renderer draws as geometry
// rather than as a font glyph: block/quadrant elements (U+2580–U+259F) and
// Braille patterns (U+2800–U+28FF). Few fonts carry Braille — none of the
// embedded Nerd Fonts do — yet CLI spinners and charts lean on it heavily.
func isGeometryRune(c rune) bool {
	return (c >= 0x2580 && c <= 0x259F) || isBraille(c)
}

func isBraille(c rune) bool { return c >= 0x2800 && c <= 0x28FF }

// brailleDots returns the raised dots of a Braille pattern as (x, y, size)
// squares relative to the cell's top-left corner. The low 8 bits of the
// codepoint are the dots: bits 0–2 run down the left column, bits 3–5 down the
// right, and bits 6 and 7 are the bottom row (left, right). Positions are
// pixel-snapped so every dot in a pattern renders at the same size.
func brailleDots(c rune, cellW, cellH float32) [][3]float32 {
	bits := uint8(c - 0x2800)
	if bits == 0 {
		return nil
	}
	// col, row for each bit, in bit order.
	layout := [8][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {1, 1}, {1, 2}, {0, 3}, {1, 3}}
	size := float32(math.Floor(float64(min(cellW/2, cellH/4) * 0.6)))
	if size < 1 {
		size = 1
	}
	dots := make([][3]float32, 0, 8)
	for bit, pos := range layout {
		if bits&(1<<bit) == 0 {
			continue
		}
		cx := cellW * float32(2*pos[0]+1) / 4
		cy := cellH * float32(2*pos[1]+1) / 8
		dots = append(dots, [3]float32{
			float32(math.Round(float64(cx - size/2))),
			float32(math.Round(float64(cy - size/2))),
			size,
		})
	}
	return dots
}

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
		layout (location = 3) in float aLayer;
		layout (location = 4) in float aBg;
		uniform mat4 projection;
		out vec2 TexCoords;
		out vec4 vColor;
		flat out float vLayer;
		flat out float vBg;
		void main() {
			gl_Position = projection * vec4(aPos, 0.0, 1.0);
			TexCoords = aTex;
			vColor = aColor;
			vLayer = aLayer;
			vBg = aBg;
		}
	` + "\x00"
	// Mono coverage gets kitty-style composition: an extra gamma that grows
	// as the text gets darker than the background it is drawn on (sRGB
	// blending otherwise thins dark-on-light text), times a contrast factor.
	// The background is the cell's own (vBg), or the theme's when negative.
	glyphFrag := `
		#version 410 core
		in vec2 TexCoords;
		in vec4 vColor;
		flat in float vLayer;
		flat in float vBg;
		out vec4 FragColor;
		uniform sampler2DArray text;
		uniform sampler2DArray emoji;
		uniform vec3 bgColor;
		uniform float textGamma;
		uniform float textContrast;
		const vec3 Y = vec3(0.2126, 0.7152, 0.0722);
		void main() {
			if (vLayer < -0.5) {
				vec4 c = texture(emoji, vec3(TexCoords, -vLayer - 1.0));
				FragColor = vec4(c.a > 0.0 ? c.rgb / c.a : c.rgb, c.a * vColor.a);
				return;
			}
			float a = texture(text, vec3(TexCoords, vLayer)).r;
			float bgY = vBg < 0.0 ? dot(bgColor, Y) : vBg;
			float t = clamp((1.0 - dot(vColor.rgb, Y) + bgY) * 0.5, 0.0, 1.0);
			a = clamp(mix(a, pow(a, 1.0 / textGamma), t) * textContrast, 0.0, 1.0);
			FragColor = vec4(vColor.rgb, vColor.a * a);
		}
	` + "\x00"
	r.glyphBatchProgram, err = createProgram(glyphVert, glyphFrag)
	if err != nil {
		return fmt.Errorf("glyph batch shader: %w", err)
	}
	r.glyphBatchProjLoc = gl.GetUniformLocation(r.glyphBatchProgram, gl.Str("projection\x00"))
	r.glyphBatchTexLoc = gl.GetUniformLocation(r.glyphBatchProgram, gl.Str("text\x00"))
	r.glyphEmojiLoc = gl.GetUniformLocation(r.glyphBatchProgram, gl.Str("emoji\x00"))
	r.glyphBgLoc = gl.GetUniformLocation(r.glyphBatchProgram, gl.Str("bgColor\x00"))
	r.glyphGammaLoc = gl.GetUniformLocation(r.glyphBatchProgram, gl.Str("textGamma\x00"))
	r.glyphContrastLoc = gl.GetUniformLocation(r.glyphBatchProgram, gl.Str("textContrast\x00"))

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

	// Glyph batch VAO/VBO: vec2 pos + vec2 tex + vec4 color + float layer +
	// float background luminance.
	gl.GenVertexArrays(1, &r.glyphBatchVAO)
	gl.GenBuffers(1, &r.glyphBatchVBO)
	gl.BindVertexArray(r.glyphBatchVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.glyphBatchVBO)
	const stride = glyphVertFloats * 4
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, stride, 0)
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointerWithOffset(1, 2, gl.FLOAT, false, stride, 2*4)
	gl.EnableVertexAttribArray(2)
	gl.VertexAttribPointerWithOffset(2, 4, gl.FLOAT, false, stride, 4*4)
	gl.EnableVertexAttribArray(3)
	gl.VertexAttribPointerWithOffset(3, 1, gl.FLOAT, false, stride, 8*4)
	gl.EnableVertexAttribArray(4)
	gl.VertexAttribPointerWithOffset(4, 1, gl.FLOAT, false, stride, 9*4)
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
		if r.uiAtlasGen == r.atlasGen {
			r.flushGlyphs(&r.uiGlyphs, r.uiProj)
		}
		r.uiGlyphs.reset()
	}
	r.uiKind = uiKindNone
}

// uploadStream replaces the bound ARRAY_BUFFER's contents, orphaning the old
// storage first so the driver never stalls on a draw still reading it.
func uploadStream(verts []float32, capFloats *int) {
	if len(verts) > *capFloats {
		*capFloats = len(verts) + len(verts)/2
	}
	gl.BufferData(gl.ARRAY_BUFFER, *capFloats*4, nil, gl.STREAM_DRAW)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(verts)*4, gl.Ptr(verts))
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
	uploadStream(b.verts, &r.rectBatchCap)
	gl.DrawArrays(gl.TRIANGLES, 0, int32(len(b.verts)/6))
	gl.BindVertexArray(0)
}

// flushGlyphs uploads pending atlas pixels, then draws all accumulated glyph
// quads (text and color emoji) in one call.
func (r *Renderer) flushGlyphs(b *glyphBatch, proj [16]float32) {
	if len(b.verts) == 0 {
		return
	}
	mono := r.atlas.sync()
	emoji := r.colorAtlas.sync()
	gl.UseProgram(r.glyphBatchProgram)
	gl.UniformMatrix4fv(r.glyphBatchProjLoc, 1, false, &proj[0])
	gl.Uniform1i(r.glyphBatchTexLoc, 0)
	gl.Uniform1i(r.glyphEmojiLoc, 1)
	bg := r.theme.Background
	gl.Uniform3f(r.glyphBgLoc, bg[0], bg[1], bg[2])
	gl.Uniform1f(r.glyphGammaLoc, r.textGamma)
	gl.Uniform1f(r.glyphContrastLoc, r.textContrast)
	gl.ActiveTexture(gl.TEXTURE1)
	gl.BindTexture(gl.TEXTURE_2D_ARRAY, emoji)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D_ARRAY, mono)
	gl.BindVertexArray(r.glyphBatchVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.glyphBatchVBO)
	uploadStream(b.verts, &r.glyphBatchCap)
	gl.DrawArrays(gl.TRIANGLES, 0, int32(len(b.verts)/glyphVertFloats))
	gl.BindVertexArray(0)
	gl.BindTexture(gl.TEXTURE_2D_ARRAY, 0)
	r.atlas.tick()
	r.colorAtlas.tick()
}
