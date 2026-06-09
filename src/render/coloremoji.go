package render

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"runtime"

	"github.com/go-gl/gl/v4.1-core/gl"
	gtfont "github.com/go-text/typesetting/font"
)

// Color-emoji rendering. The monochrome glyph atlas (gl.RED, tinted with the
// cell's foreground color) cannot represent multi-color emoji, and the bundled
// nerd fonts contain no emoji at all. To render real color emoji (e.g. starship
// language symbols like the Go gopher), we load the system color-emoji font,
// decode each needed glyph's bitmap, and upload it as its own RGBA texture drawn
// with a dedicated non-tinting shader. Emoji on screen are few, so a standalone
// texture per unique glyph is simpler than packing an RGBA atlas and is plenty
// fast.

// colorGlyph is a decoded color-emoji bitmap uploaded as an RGBA texture.
// ok=false records a cached miss (rune absent from the color font).
type colorGlyph struct {
	tex  uint32
	w, h int // native bitmap pixel size
	ok   bool
}

// colorDrawItem is a deferred color-emoji quad, collected during the batched
// grid pass and drawn after the monochrome glyph batch flushes.
type colorDrawItem struct {
	x, yTop float32
	span    int // column span (2 for wide emoji, 1 otherwise)
	cg      colorGlyph
	alpha   float32
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

// loadColorFont opens the first available system color-emoji font. The font file
// stays open and is read lazily (Apple Color Emoji is ~190MB; we never load it
// fully into memory). Failure is non-fatal: emoji then fall back to '?'.
func (r *Renderer) loadColorFont() {
	for _, path := range colorEmojiFontPaths() {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		faces, err := gtfont.ParseTTC(f) // *os.File satisfies font.Resource (ReaderAt)
		if err != nil || len(faces) == 0 {
			f.Close()
			continue
		}
		face := faces[0]
		// Request a large bitmap strike; the font snaps to its nearest available
		// size (160px for Apple Color Emoji). The quad is scaled to the cell at
		// draw time, so this is independent of font size / zoom.
		face.SetPpem(128, 128)
		r.colorFontFile = f
		r.colorFace = face
		r.colorGlyphs = make(map[rune]colorGlyph)
		return
	}
}

// initColorEmoji compiles the RGBA color-glyph shader (samples the texture
// directly rather than tinting an alpha channel). Called from initGL.
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
		return fmt.Errorf("color emoji shader: %w", err)
	}
	r.colorProgram = prog
	r.colorProjLoc = gl.GetUniformLocation(prog, gl.Str("projection\x00"))
	r.colorTexLoc = gl.GetUniformLocation(prog, gl.Str("tex\x00"))
	r.colorAlphaLoc = gl.GetUniformLocation(prog, gl.Str("alpha\x00"))
	return nil
}

// ensureColorGlyph returns (and lazily uploads) the color texture for ch, or
// ok=false if no color-emoji font provides it. Hits and misses are both cached.
func (r *Renderer) ensureColorGlyph(ch rune) (colorGlyph, bool) {
	if r.colorFace == nil {
		return colorGlyph{}, false
	}
	if cg, seen := r.colorGlyphs[ch]; seen {
		return cg, cg.ok
	}

	miss := colorGlyph{}
	gid, ok := r.colorFace.NominalGlyph(ch)
	if !ok {
		r.colorGlyphs[ch] = miss
		return miss, false
	}
	bm, ok := r.colorFace.GlyphData(gid).(gtfont.GlyphBitmap)
	if !ok || bm.Format != gtfont.PNG {
		r.colorGlyphs[ch] = miss
		return miss, false
	}
	src, err := png.Decode(bytes.NewReader(bm.Data))
	if err != nil {
		r.colorGlyphs[ch] = miss
		return miss, false
	}

	// Normalize to RGBA (Apple's emoji decode as NRGBA).
	rgba, isRGBA := src.(*image.RGBA)
	if !isRGBA {
		b := src.Bounds()
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	}
	w, h := rgba.Bounds().Dx(), rgba.Bounds().Dy()

	var tex uint32
	gl.GenTextures(1, &tex)
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, int32(w), int32(h), 0,
		gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(rgba.Pix))
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.BindTexture(gl.TEXTURE_2D, 0)

	cg := colorGlyph{tex: tex, w: w, h: h, ok: true}
	r.colorGlyphs[ch] = cg
	return cg, true
}

// drawColorGlyph draws a color-emoji texture as a square fitted to the cell
// height and centered over the cell's column span.
func (r *Renderer) drawColorGlyph(x, yTop float32, spanCols int, cg colorGlyph, alpha float32, proj [16]float32) {
	spanW := float32(spanCols) * r.cellWidth
	side := r.cellHeight
	if side > spanW {
		side = spanW
	}
	dx := x + (spanW-side)/2
	dy := yTop + (r.cellHeight-side)/2
	x2 := dx + side
	y2 := dy + side

	verts := []float32{
		dx, dy, 0, 0,
		x2, dy, 1, 0,
		x2, y2, 1, 1,
		dx, dy, 0, 0,
		x2, y2, 1, 1,
		dx, y2, 0, 1,
	}

	gl.UseProgram(r.colorProgram)
	gl.UniformMatrix4fv(r.colorProjLoc, 1, false, &proj[0])
	gl.Uniform1i(r.colorTexLoc, 0)
	gl.Uniform1f(r.colorAlphaLoc, alpha)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, cg.tex)
	gl.BindVertexArray(r.fontVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.fontVBO)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(verts)*4, gl.Ptr(verts))
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
}

// destroyColorEmoji releases the color-glyph textures, shader, and font handle.
func (r *Renderer) destroyColorEmoji() {
	for ch, cg := range r.colorGlyphs {
		if cg.ok && cg.tex != 0 {
			gl.DeleteTextures(1, &cg.tex)
		}
		delete(r.colorGlyphs, ch)
	}
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
