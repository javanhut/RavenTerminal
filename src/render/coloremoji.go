package render

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"runtime"

	"github.com/go-gl/gl/v4.1-core/gl"
	gtfont "github.com/go-text/typesetting/font"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
)

// Color emoji. The bundled nerd fonts contain no emoji, so the system
// color-emoji font is loaded and each needed bitmap is decoded, scaled to the
// cell once, and packed into the RGBA atlas. Color glyphs are ordinary quads
// in the glyph batch (negative layer selects the color atlas), so emoji cost
// no extra texture binds or draw calls.

// colorGlyph identifies a color-emoji glyph; ok=false means none.
type colorGlyph struct {
	gid uint32
	ok  bool
}

type colorKey struct {
	gid  uint32
	side int
}

// colorEmojiFontPaths lists candidate system color-emoji fonts per platform,
// in preference order.
func colorEmojiFontPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/System/Library/Fonts/Apple Color Emoji.ttc"}
	case "windows":
		return []string{`C:\Windows\Fonts\seguiemj.ttf`}
	default: // linux and others
		return []string{
			"/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf",
			"/usr/share/fonts/noto/NotoColorEmoji.ttf",
			"/usr/share/fonts/google-noto-emoji/NotoColorEmoji.ttf",
			"/usr/share/fonts/NotoColorEmoji.ttf",
		}
	}
}

// loadColorFont opens the first available system color-emoji font. The file
// stays open and is read lazily (Apple Color Emoji is ~190MB). Failure is
// non-fatal: emoji then fall back to '?'.
func (r *Renderer) loadColorFont() {
	for _, path := range colorEmojiFontPaths() {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		faces, err := gtfont.ParseTTC(f)
		if err != nil || len(faces) == 0 {
			f.Close()
			continue
		}
		face := faces[0]
		// Request a large bitmap strike; the font snaps to its nearest size.
		face.SetPpem(128, 128)
		r.colorFontFile = f
		r.colorFace = face
		return
	}
}

// initColorEmoji compiles the straight-alpha RGBA texture shader (samples the
// texture directly rather than tinting an alpha channel). Called from initGL.
func (r *Renderer) initColorEmoji() error {
	vert := `
		#version 410 core
		layout (location = 0) in vec4 vertex; // <vec2 pos, vec2 tex>
		out vec2 TexCoords;
		uniform mat4 projection;
		void main() {
			gl_Position = projection * vec4(vertex.xy, 0.0, 1.0);
			TexCoords = vertex.zw;
		}
	` + "\x00"
	frag := `
		#version 410 core
		in vec2 TexCoords;
		out vec4 FragColor;
		uniform sampler2D tex;
		uniform float alpha;
		void main() {
			vec4 c = texture(tex, TexCoords);
			FragColor = vec4(c.rgb, c.a * alpha);
		}
	` + "\x00"
	prog, err := createProgram(vert, frag)
	if err != nil {
		return fmt.Errorf("color texture shader: %w", err)
	}
	r.colorProgram = prog
	r.colorProjLoc = gl.GetUniformLocation(prog, gl.Str("projection\x00"))
	r.colorTexLoc = gl.GetUniformLocation(prog, gl.Str("tex\x00"))
	r.colorAlphaLoc = gl.GetUniformLocation(prog, gl.Str("alpha\x00"))
	return nil
}

func (r *Renderer) hasColorBitmap(gid gtfont.GID) bool {
	bm, ok := r.colorFace.GlyphData(gid).(gtfont.GlyphBitmap)
	return ok && bm.Format == gtfont.PNG
}

// ensureColorGlyph reports whether the color font has a bitmap for ch.
func (r *Renderer) ensureColorGlyph(ch rune) (colorGlyph, bool) {
	if r.colorFace == nil {
		return colorGlyph{}, false
	}
	gid, seen := r.colorRunes[ch]
	if !seen {
		if g, ok := r.colorFace.NominalGlyph(ch); ok && r.hasColorBitmap(g) {
			gid = uint32(g)
		}
		r.colorRunes[ch] = gid
	}
	return colorGlyph{gid: gid, ok: gid != 0}, gid != 0
}

// colorGlyphAt returns the atlas entry for a color glyph scaled to fit span
// cells, decoding and uploading it on first use.
func (r *Renderer) colorGlyphAt(gid uint32, span int) (Glyph, bool) {
	side := int(min(r.cellHeight, float32(span)*r.cellWidth))
	if side <= 0 || r.colorFace == nil {
		return Glyph{}, false
	}
	key := colorKey{gid, side}
	if g, ok := r.colorGlyphs[key]; ok {
		return g, true
	}
	bm, ok := r.colorFace.GlyphData(gtfont.GID(gid)).(gtfont.GlyphBitmap)
	if !ok || bm.Format != gtfont.PNG {
		return Glyph{}, false
	}
	src, err := png.Decode(bytes.NewReader(bm.Data))
	if err != nil {
		return Glyph{}, false
	}
	b := src.Bounds()
	scale := float32(side) / float32(max(b.Dx(), b.Dy()))
	w, h := max(1, int(float32(b.Dx())*scale+0.5)), max(1, int(float32(b.Dy())*scale+0.5))
	const pad = 1
	// Premultiplied, so LINEAR filtering never bleeds dark fringes; the
	// shader un-premultiplies.
	dst := image.NewRGBA(image.Rect(0, 0, w+2*pad, h+2*pad))
	xdraw.CatmullRom.Scale(dst, image.Rect(pad, pad, pad+w, pad+h), src, b, xdraw.Src, nil)
	layer, px, py, ok := r.colorAtlas.alloc(w+2*pad, h+2*pad)
	if !ok {
		return Glyph{}, false
	}
	r.colorAtlas.write(layer, px, py, w+2*pad, h+2*pad, dst.Pix)
	size := float32(r.colorAtlas.size)
	g := Glyph{
		X:           float32(px+pad) / size,
		Y:           float32(py+pad) / size,
		Width:       float32(w) / size,
		Height:      float32(h) / size,
		PixelWidth:  w,
		PixelHeight: h,
		Layer:       uint16(layer),
		Color:       true,
	}
	r.colorGlyphs[key] = g
	return g, true
}

// colorCluster shapes an emoji sequence with the color font and returns its
// glyph when the font has one (ZWJ sequences, flags and keycaps are
// ligatures there). A sequence the font lacks draws its first emoji.
func (r *Renderer) colorCluster(text []rune, span int) []placedGlyph {
	if r.colorFace == nil {
		return nil
	}
	for _, sg := range r.shapeRunes(r.colorFace, text, fixed.I(128)) {
		if sg.gid == 0 || !r.hasColorBitmap(gtfont.GID(sg.gid)) {
			continue
		}
		if g, ok := r.colorGlyphAt(sg.gid, span); ok {
			return []placedGlyph{{g: g}}
		}
		return nil
	}
	return nil
}

// drawColorGlyph draws a color glyph fitted to the cell height and centered
// over its column span, batched with the UI glyphs.
func (r *Renderer) drawColorGlyph(x, yTop float32, spanCols int, cg colorGlyph, alpha float32, proj [16]float32) {
	g, ok := r.colorGlyphAt(cg.gid, max(1, spanCols))
	if !ok {
		return
	}
	r.uiEnsure(uiKindGlyph, proj)
	if r.uiAtlasGen != r.atlasGen {
		r.uiGlyphs.reset()
		r.uiAtlasGen = r.atlasGen
	}
	r.emitColor(&r.uiGlyphs, g, x, yTop, max(1, spanCols), alpha)
}

// destroyColorEmoji releases the RGBA shader and the color font handle.
func (r *Renderer) destroyColorEmoji() {
	if r.colorProgram != 0 {
		gl.DeleteProgram(r.colorProgram)
		r.colorProgram = 0
	}
	if r.colorFontFile != nil {
		r.colorFontFile.Close()
		r.colorFontFile = nil
	}
	r.colorFace = nil
}
