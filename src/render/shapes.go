package render

import (
	"fmt"
	"image"
	"math"

	"github.com/go-gl/gl/v4.1-core/gl"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
)

// Anti-aliased shapes and crisp sized labels for the modal UI.
//
// The rect and glyph batches only know axis-aligned boxes and cell-sized
// glyphs. A card with a hairline border, a ring with gradient arcs and a soft
// glow, and a title larger than the terminal font need more: a signed-distance
// shape shader, and labels rasterized at their own pixel size instead of
// scaled-up atlas bitmaps. Both are immediate draws, so each one flushes the
// UI batch first to keep paint order.

// shapeGrad picks how a shape's two colors are blended.
type shapeGrad int32

const (
	gradSolid      shapeGrad = iota // colorA only
	gradVertical                    // colorA at the top edge, colorB at the bottom
	gradHorizontal                  // colorA at the left edge, colorB at the right
	gradArc                         // colorA at the arc's start, colorB at its end
)

// shape is one signed-distance draw: a rounded box centred on (cx, cy), or a
// circle when radius equals the half size. stroke > 0 draws only an inner
// band of that width; soft widens the anti-aliased edge into a glow. sweep
// below a full turn masks the shape to an arc starting at start radians,
// measured clockwise from twelve o'clock.
type shape struct {
	cx, cy, hw, hh float32
	radius         float32
	stroke         float32
	soft           float32
	colorA, colorB [4]float32
	grad           shapeGrad
	start, sweep   float32
}

type shapeProgram struct {
	program uint32
	vao     uint32
	vbo     uint32

	proj, center, half, radius, stroke, soft int32
	colorA, colorB, grad, arc                int32
}

func (r *Renderer) initShapes() error {
	vert := `
		#version 410 core
		layout (location = 0) in vec2 aPos;
		uniform mat4 projection;
		uniform vec2 uCenter;
		out vec2 vLocal;
		void main() {
			gl_Position = projection * vec4(aPos, 0.0, 1.0);
			vLocal = aPos - uCenter;
		}
	` + "\x00"
	frag := `
		#version 410 core
		in vec2 vLocal;
		out vec4 FragColor;
		uniform vec2 uHalf;
		uniform float uRadius;
		uniform float uStroke;
		uniform float uSoft;
		uniform vec4 uColorA;
		uniform vec4 uColorB;
		uniform int uGrad;
		uniform vec2 uArc; // start, sweep (radians)
		const float TAU = 6.28318530718;
		void main() {
			vec2 p = vLocal;
			vec2 q = abs(p) - uHalf + uRadius;
			float d = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - uRadius;
			if (uStroke > 0.0) {
				d = abs(d + uStroke * 0.5) - uStroke * 0.5;
			}
			float soft = max(uSoft, 1.0);
			float alpha = clamp(0.5 - d / soft, 0.0, 1.0);

			float t = 0.0;
			if (uArc.y < TAU) {
				float ang = mod(atan(p.x, -p.y) - uArc.x + 2.0 * TAU, TAU);
				float r = length(p);
				float s = ang <= uArc.y
					? -min(ang, uArc.y - ang) * r
					: min(ang - uArc.y, TAU - ang) * r;
				alpha *= clamp(0.5 - s / soft, 0.0, 1.0);
				t = clamp(ang / uArc.y, 0.0, 1.0);
			}
			if (uGrad == 1) {
				t = clamp((p.y + uHalf.y) / (2.0 * uHalf.y), 0.0, 1.0);
			} else if (uGrad == 2) {
				t = clamp((p.x + uHalf.x) / (2.0 * uHalf.x), 0.0, 1.0);
			} else if (uGrad == 0) {
				t = 0.0;
			}
			vec4 c = mix(uColorA, uColorB, t);
			FragColor = vec4(c.rgb, c.a * alpha);
		}
	` + "\x00"
	prog, err := createProgram(vert, frag)
	if err != nil {
		return fmt.Errorf("shape shader: %w", err)
	}
	s := &r.shapes
	s.program = prog
	loc := func(name string) int32 { return gl.GetUniformLocation(prog, gl.Str(name+"\x00")) }
	s.proj, s.center, s.half = loc("projection"), loc("uCenter"), loc("uHalf")
	s.radius, s.stroke, s.soft = loc("uRadius"), loc("uStroke"), loc("uSoft")
	s.colorA, s.colorB, s.grad, s.arc = loc("uColorA"), loc("uColorB"), loc("uGrad"), loc("uArc")

	gl.GenVertexArrays(1, &s.vao)
	gl.GenBuffers(1, &s.vbo)
	gl.BindVertexArray(s.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, s.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, 12*4, nil, gl.DYNAMIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 2*4, 0)
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)
	return nil
}

func (r *Renderer) destroyShapes() {
	gl.DeleteVertexArrays(1, &r.shapes.vao)
	gl.DeleteBuffers(1, &r.shapes.vbo)
	gl.DeleteProgram(r.shapes.program)
	r.clearLabels()
}

// drawShape draws one shape immediately.
func (r *Renderer) drawShape(s shape, proj [16]float32) {
	r.uiFlush()
	if s.sweep <= 0 {
		s.sweep = 2 * math.Pi
	}
	if s.colorB == ([4]float32{}) {
		s.colorB = s.colorA
	}
	// The quad covers the shape plus its feathered edge.
	pad := max(s.soft, 1) + 1
	x0, y0 := s.cx-s.hw-pad, s.cy-s.hh-pad
	x1, y1 := s.cx+s.hw+pad, s.cy+s.hh+pad
	verts := [12]float32{x0, y0, x1, y0, x1, y1, x0, y0, x1, y1, x0, y1}

	p := &r.shapes
	gl.UseProgram(p.program)
	gl.UniformMatrix4fv(p.proj, 1, false, &proj[0])
	gl.Uniform2f(p.center, s.cx, s.cy)
	gl.Uniform2f(p.half, s.hw, s.hh)
	gl.Uniform1f(p.radius, min(s.radius, s.hw, s.hh))
	gl.Uniform1f(p.stroke, s.stroke)
	gl.Uniform1f(p.soft, s.soft)
	gl.Uniform4fv(p.colorA, 1, &s.colorA[0])
	gl.Uniform4fv(p.colorB, 1, &s.colorB[0])
	gl.Uniform1i(p.grad, int32(s.grad))
	gl.Uniform2f(p.arc, s.start, s.sweep)
	gl.BindVertexArray(p.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, p.vbo)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(verts)*4, gl.Ptr(&verts[0]))
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
}

// fillCircle and strokeCircle are the common shape cases.
func (r *Renderer) fillCircle(cx, cy, radius float32, clr [4]float32, proj [16]float32) {
	r.drawShape(shape{cx: cx, cy: cy, hw: radius, hh: radius, radius: radius, colorA: clr}, proj)
}

func (r *Renderer) strokeCircle(cx, cy, radius, width float32, clr [4]float32, proj [16]float32) {
	r.drawShape(shape{cx: cx, cy: cy, hw: radius, hh: radius, radius: radius, stroke: width, colorA: clr}, proj)
}

// labelFont says which font a label is rasterized from: the terminal's
// current font for words, the embedded Nerd Font for icons, which every
// embedded font carries but a user-chosen one may not.
type labelFont int

const (
	labelText labelFont = iota
	labelIcon
)

type labelKey struct {
	text string
	px   int
	font labelFont
}

// label is a string rasterized at its own pixel size into a RED texture.
// minX/minY place the ink box relative to the pen origin on the baseline.
type label struct {
	tex        uint32
	w, h       int
	minX, minY int
	advance    float32
	capHeight  float32
}

type faceKey struct {
	px   int
	font labelFont
}

// maxLabels bounds the cache: a live resize walks through many pixel sizes,
// and a modal only ever shows a dozen labels at once.
const maxLabels = 96

func (r *Renderer) labelFace(px int, which labelFont) font.Face {
	k := faceKey{px, which}
	if f, ok := r.labelFaces[k]; ok {
		return f
	}
	src := r.fontParsed
	if which == labelIcon || src == nil {
		if r.iconFont == nil {
			f, err := opentype.Parse(fonts.DefaultFont())
			if err != nil {
				return nil
			}
			r.iconFont = f
		}
		src = r.iconFont
	}
	face, err := opentype.NewFace(src, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	if r.labelFaces == nil {
		r.labelFaces = make(map[faceKey]font.Face)
	}
	r.labelFaces[k] = face
	return face
}

// measureLabel is the pen advance of text at px, without rasterizing it.
func (r *Renderer) measureLabel(text string, px float32, which labelFont) float32 {
	face := r.labelFace(int(math.Round(float64(px))), which)
	if face == nil {
		return float32(len([]rune(text))) * r.cellWidth * px / r.cellHeight
	}
	return float32(font.MeasureString(face, text)) / 64
}

func (r *Renderer) labelFor(text string, px float32, which labelFont) (*label, bool) {
	k := labelKey{text, int(math.Round(float64(px))), which}
	if l, ok := r.labels[k]; ok {
		return l, l.tex != 0
	}
	if len(r.labels) >= maxLabels {
		r.clearLabels()
	}
	if r.labels == nil {
		r.labels = make(map[labelKey]*label)
	}
	l := &label{}
	r.labels[k] = l
	face := r.labelFace(k.px, which)
	if face == nil || k.px <= 0 {
		return l, false
	}
	bounds, adv := font.BoundString(face, text)
	l.advance = float32(adv) / 64
	l.capHeight = float32(face.Metrics().CapHeight) / 64
	if l.capHeight <= 0 {
		l.capHeight = float32(face.Metrics().Ascent) / 64 * 0.7
	}
	const pad = 1
	l.minX = bounds.Min.X.Floor() - pad
	l.minY = bounds.Min.Y.Floor() - pad
	l.w = bounds.Max.X.Ceil() + pad - l.minX
	l.h = bounds.Max.Y.Ceil() + pad - l.minY
	if l.w <= 0 || l.h <= 0 {
		return l, false
	}
	img := image.NewAlpha(image.Rect(0, 0, l.w, l.h))
	d := font.Drawer{Dst: img, Src: image.White, Face: face, Dot: fixed.P(-l.minX, -l.minY)}
	d.DrawString(text)

	gl.GenTextures(1, &l.tex)
	gl.BindTexture(gl.TEXTURE_2D, l.tex)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RED, int32(l.w), int32(l.h), 0, gl.RED, gl.UNSIGNED_BYTE, gl.Ptr(img.Pix))
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	return l, true
}

// clearLabels drops every cached label texture and face; the font changed,
// or the cache filled up.
func (r *Renderer) clearLabels() {
	for _, l := range r.labels {
		if l.tex != 0 {
			gl.DeleteTextures(1, &l.tex)
		}
	}
	r.labels = nil
	for _, f := range r.labelFaces {
		f.Close()
	}
	r.labelFaces = nil
}

// labelAlign is where a label sits relative to the x it is drawn at.
type labelAlign int

const (
	alignLeft labelAlign = iota
	alignCenter
)

// drawLabel draws text at px size with its cap height centred on cy. top and
// bottom tint the ink as a vertical gradient (pass the same color for none).
func (r *Renderer) drawLabel(x, cy float32, text string, px float32, which labelFont, align labelAlign, top, bottom [4]float32, proj [16]float32) {
	l, ok := r.labelFor(text, px, which)
	if !ok {
		return
	}
	penX := x
	if align == alignCenter {
		penX = x - l.advance/2
	}
	baseline := cy + l.capHeight/2
	// Whole pixels keep the rasterized ink crisp.
	x0 := float32(math.Round(float64(penX))) + float32(l.minX)
	y0 := float32(math.Round(float64(baseline))) + float32(l.minY)
	r.drawLabelQuad(l, x0, y0, float32(l.w), float32(l.h), top, bottom, proj)
}

// drawIcon draws one glyph from the icon font with its ink box centred on
// (cx, cy), sized so the ink is size pixels tall.
func (r *Renderer) drawIcon(cx, cy float32, icon rune, size float32, top, bottom [4]float32, proj [16]float32) {
	l, ok := r.labelFor(string(icon), size, labelIcon)
	if !ok {
		return
	}
	// The ink box is the glyph plus a pixel of padding each side.
	w, h := float32(l.w), float32(l.h)
	x0 := float32(math.Round(float64(cx - w/2)))
	y0 := float32(math.Round(float64(cy - h/2)))
	r.drawLabelQuad(l, x0, y0, w, h, top, bottom, proj)
}

func (r *Renderer) drawLabelQuad(l *label, x, y, w, h float32, top, bottom [4]float32, proj [16]float32) {
	r.uiFlush()
	x2, y2 := x+w, y+h
	verts := []float32{
		x, y, 0, 0, top[0], top[1], top[2], top[3],
		x2, y, 1, 0, top[0], top[1], top[2], top[3],
		x2, y2, 1, 1, bottom[0], bottom[1], bottom[2], bottom[3],
		x, y, 0, 0, top[0], top[1], top[2], top[3],
		x2, y2, 1, 1, bottom[0], bottom[1], bottom[2], bottom[3],
		x, y2, 0, 1, bottom[0], bottom[1], bottom[2], bottom[3],
	}
	gl.UseProgram(r.glyphBatchProgram)
	gl.UniformMatrix4fv(r.glyphBatchProjLoc, 1, false, &proj[0])
	gl.Uniform1i(r.glyphBatchTexLoc, 0)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, l.tex)
	gl.BindVertexArray(r.glyphBatchVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.glyphBatchVBO)
	if len(verts) > r.glyphBatchCap {
		gl.BufferData(gl.ARRAY_BUFFER, len(verts)*4, gl.Ptr(verts), gl.DYNAMIC_DRAW)
		r.glyphBatchCap = len(verts)
	} else {
		gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(verts)*4, gl.Ptr(verts))
	}
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
}
