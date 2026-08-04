package render

import (
	"fmt"
	"github.com/javanhut/RavenTerminal/src/aipanel"
	"github.com/javanhut/RavenTerminal/src/assets/fonts"
	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/menu"
	"github.com/javanhut/RavenTerminal/src/parser"
	"github.com/javanhut/RavenTerminal/src/searchpanel"
	"github.com/javanhut/RavenTerminal/src/tab"
	"image/color"
	"image/draw"
	"math"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/go-gl/gl/v4.1-core/gl"
	gtfont "github.com/go-text/typesetting/font"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// Minimum grid dimensions enforced by CalculateGridSize. Below this the
// terminal is no longer functional; the window-level minimum in
// window.NewWindow is sized to keep us above this floor in practice.
const (
	minGridCols = 10
	minGridRows = 3
)

// Theme colors
type Theme struct {
	Background [4]float32
	Foreground [4]float32
	Cursor     [4]float32
	TabBar     [4]float32
	TabActive  [4]float32
	Selection  [4]float32
}

// DefaultTheme returns the default color theme
func DefaultTheme() Theme {
	return ThemeByName("raven-blue")
}

// ThemeByName returns a theme for a known theme name.
func ThemeByName(name string) Theme {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "crow-black":
		return Theme{
			Background: [4]float32{0.020, 0.020, 0.020, 1.0}, // #050505
			Foreground: [4]float32{0.902, 0.902, 0.902, 1.0}, // #e6e6e6
			Cursor:     [4]float32{0.965, 0.965, 0.965, 1.0}, // #f6f6f6
			TabBar:     [4]float32{0.000, 0.000, 0.000, 1.0}, // #000000
			TabActive:  [4]float32{0.702, 0.702, 0.702, 1.0}, // #b3b3b3
			Selection:  [4]float32{0.702, 0.702, 0.702, 0.35},
		}
	case "magpie-black-white-grey", "magpie-black-and-white-grey":
		return Theme{
			Background: [4]float32{0.067, 0.067, 0.067, 1.0}, // #111111
			Foreground: [4]float32{0.961, 0.961, 0.961, 1.0}, // #f5f5f5
			Cursor:     [4]float32{1.000, 1.000, 1.000, 1.0}, // #ffffff
			TabBar:     [4]float32{0.039, 0.039, 0.039, 1.0}, // #0a0a0a
			TabActive:  [4]float32{0.816, 0.816, 0.816, 1.0}, // #d0d0d0
			Selection:  [4]float32{0.816, 0.816, 0.816, 0.35},
		}
	case "ghostty":
		// Matches Ghostty's out-of-the-box look: One Dark-style #282C34
		// background with white text (cursor defaults to the foreground).
		return Theme{
			Background: [4]float32{0.157, 0.173, 0.204, 1.0}, // #282C34
			Foreground: [4]float32{1.000, 1.000, 1.000, 1.0}, // #FFFFFF
			Cursor:     [4]float32{1.000, 1.000, 1.000, 1.0}, // #FFFFFF (= fg)
			TabBar:     [4]float32{0.129, 0.141, 0.169, 1.0}, // #21242B
			TabActive:  [4]float32{0.380, 0.686, 0.937, 1.0}, // #61AFEF (One Dark blue)
			Selection:  [4]float32{0.380, 0.686, 0.937, 0.35},
		}
	case "catppuccin-mocha", "catppuccin", "catpuccin":
		return Theme{
			Background: [4]float32{0.118, 0.118, 0.180, 1.0}, // #1e1e2e
			Foreground: [4]float32{0.804, 0.839, 0.957, 1.0}, // #cdd6f4
			Cursor:     [4]float32{0.961, 0.761, 0.906, 1.0}, // #f5c2e7
			TabBar:     [4]float32{0.094, 0.094, 0.145, 1.0}, // #181825
			TabActive:  [4]float32{0.537, 0.706, 0.980, 1.0}, // #89b4fa
			Selection:  [4]float32{0.537, 0.706, 0.980, 0.35},
		}
	case "raven-blue":
		fallthrough
	default:
		return Theme{
			Background: [4]float32{0.051, 0.063, 0.102, 1.0}, // #0d101a
			Foreground: [4]float32{0.910, 0.929, 0.969, 1.0}, // #e8edf7
			Cursor:     [4]float32{0.635, 0.878, 0.780, 1.0}, // #a2e0c7
			TabBar:     [4]float32{0.039, 0.047, 0.078, 1.0}, // #0a0c14
			TabActive:  [4]float32{0.455, 0.714, 1.0, 1.0},   // #74b6ff
			Selection:  [4]float32{0.455, 0.714, 1.0, 0.35},
		}
	}
}

// SetThemeByName applies a named theme to the renderer.
func (r *Renderer) SetThemeByName(name string) {
	r.theme = ThemeByName(name)
	r.themeGen++
	r.uiDirty = true
}

// Glyph contains information about a rendered glyph
type Glyph struct {
	X, Y          float32 // Position in atlas (normalized 0-1)
	Width, Height float32 // Size in atlas (normalized 0-1)
	PixelWidth    int     // Actual pixel width (0 for inkless glyphs, e.g. space)
	PixelHeight   int     // Actual pixel height
	// Bitmap placement relative to the pen position: OffsetX from the cell
	// origin, OffsetY from the baseline (negative above it). Recording the
	// bearings here lets wide glyphs (Nerd Font icons) render unclipped
	// instead of being cut off at the cell boundary.
	OffsetX float32
	OffsetY float32
}

// asciiGlyphSlot is the per-rune resolution cache entry for ASCII runes.
type asciiGlyphSlot struct {
	g        Glyph
	ok       bool
	resolved bool
}

// Renderer handles OpenGL rendering with smooth fonts
type Renderer struct {
	theme           Theme
	cellWidth       float32 // Current cell dimensions (may be zoomed)
	cellHeight      float32
	fontSize        float32 // Current font size
	baseFontSize    float32 // Base font size (16.0)
	baseCellWidth   float32 // Cell dimensions at base font size (for UI)
	defaultFontSize float32 // Default font size for reset
	baseCellHeight  float32
	paddingTop      float32
	paddingBottom   float32
	tabBarWidth     float32
	barViewH        float32 // height of the last rendered frame (tab chip fitting)
	currentFont     string

	// Font data. The atlas is dynamic: glyphs are rasterized on demand at
	// their natural ink bounds and shelf-packed into a growable RED texture.
	glyphs      map[rune]Glyph
	glyphMisses map[rune]bool // runes no font in the chain covers (negative cache)
	// asciiCache is a flat array in front of the glyphs map for runes < 128
	// (the overwhelming majority of terminal content); neither fallback table
	// has ASCII keys, so resolution for them is exactly ensureGlyph. Cleared
	// wherever the glyph cache is invalidated (font reload, atlas grow,
	// system-fallback load).
	asciiCache     [128]asciiGlyphSlot
	fontAtlas      uint32
	atlasSize      int
	atlasPix       []byte    // CPU-side mirror of the RED atlas (for grow/repack)
	shelfX, shelfY int       // shelf packer cursor
	shelfH         int       // height of the current shelf
	atlasAscent    int       // baseline offset from the cell top (font ascent)
	face           font.Face // current rasterization face (kept open)
	// Font fallback chain (see fallback.go): tried in order when the active
	// font misses a glyph, before the substitution tables and the '?' resort.
	fallbackFonts         []fallbackFont
	fallbackFaces         []font.Face
	systemFallbacksLoaded bool

	// OpenGL resources
	quadVAO     uint32
	quadVBO     uint32
	program     uint32
	fontProgram uint32
	fontVAO     uint32
	fontVBO     uint32

	// Uniforms
	colorLoc    int32
	projLoc     int32
	texColorLoc int32
	texProjLoc  int32
	texLoc      int32

	// Batched-rendering resources (per-vertex color). Used by renderGridAt to
	// draw all cell backgrounds in one call and all glyphs in one call.
	rectBatchProgram  uint32
	rectBatchVAO      uint32
	rectBatchVBO      uint32
	rectBatchProjLoc  int32
	rectBatchCap      int // current VBO capacity in floats
	glyphBatchProgram uint32
	glyphBatchVAO     uint32
	glyphBatchVBO     uint32
	glyphBatchProjLoc int32
	glyphBatchTexLoc  int32
	glyphBatchCap     int
	// Reusable per-frame batch buffers (avoid re-allocation each frame).
	gridRects  rectBatch
	gridGlyphs glyphBatch
	// UI batch state (see batch.go): drawRect/drawChar* accumulate here and
	// flush on kind/projection change or before any non-batched draw.
	uiRects    rectBatch
	uiGlyphs   glyphBatch
	uiKind     int
	uiProj     [16]float32
	uiAtlasGen uint64
	// pass2 collects, during the batched pass, the minority of cells that need
	// immediate decoration draws (block elements, underline, strikethrough),
	// so pass 2 doesn't re-scan the whole grid.
	pass2 []pass2Item
	// atlasGen increments whenever the glyph atlas grows and re-packs
	// (invalidating all previously recorded UVs); renderGridAt uses it to
	// detect a mid-pass repack and re-record the batch.
	atlasGen uint64

	// Color emoji: system color-emoji font decoded to per-glyph RGBA textures,
	// drawn with a dedicated non-tinting shader (see coloremoji.go). colorDraws
	// collects emoji quads during the batched grid pass to draw after the
	// monochrome glyph batch flushes.
	colorFace     *gtfont.Face
	colorFontFile *os.File
	colorGlyphs   map[rune]colorGlyph
	colorProgram  uint32
	colorProjLoc  int32
	colorTexLoc   int32
	colorAlphaLoc int32
	colorDraws    []colorDrawItem

	// Help panel scroll state and cached section content
	helpScrollOffset int
	helpSections     []helpSection

	// Search-preview wrap cache: rebuilt only when the preview content,
	// width, or theme changes instead of every frame. themeGen bumps on
	// SetThemeByName so a theme switch invalidates the styled colors.
	themeGen         uint64
	previewWrapKey   previewWrapKey
	previewWrapLines []styledLine

	// Hover underline state for URLs
	hoverGrid     *grid.Grid
	hoverRow      int
	hoverStartCol int
	hoverEndCol   int
	hoverActive   bool

	// Text styling options (from config); default on
	fauxBold   bool
	fauxItalic bool
	undercurl  bool

	// tabBarVisible controls whether the left tab bar is shown and reserves layout
	// width. It is hidden when there is a single tab so a lone tab uses the full window.
	tabBarVisible bool

	// contentScale is the window's HiDPI content scale (framebuffer pixels per
	// logical point). Fonts are rasterized at 96*contentScale DPI and all
	// renderer geometry stays in framebuffer pixels; the hit-testing helpers
	// (PaneRectFor, CellSize, HitTestPane, HitTestTabBar) convert to/from the
	// logical coordinates that GLFW cursor callbacks deliver.
	contentScale float32

	// uiDirty latches renderer-side state changes (hover underline, theme, tab
	// bar visibility, font size) that must force a redraw; consumed once per
	// frame by ConsumeUIDirty.
	uiDirty bool

	// Tab-chip slide animation. tabSlot holds each tab's *animated* slot
	// position (a float index into the chip stack) so a reorder slides instead
	// of snapping. Keyed by tab pointer; stale entries are pruned each frame,
	// so closed tabs cannot accumulate. tabSlotAt timestamps the last frame so
	// the ease is wall-clock based rather than per-frame (the main loop's frame
	// rate varies with load). dragTab is the tab currently under the cursor in
	// a drag-reorder — it snaps to its slot instead of easing, so the chip
	// tracks the pointer while the displaced chips visibly slide around it.
	tabSlot   map[*tab.Tab]float32
	tabSlotAt time.Time
	dragTab   *tab.Tab
}

// pass2Item marks a cell needing immediate decoration draws after the batches
// flush, with its position and already-resolved fg color.
type pass2Item struct {
	col, row int
	x, y     float32
	fg       [4]float32
	hovered  bool
}

// SetTabBarVisible shows or hides the left tab bar (and its reserved layout width).
func (r *Renderer) SetTabBarVisible(visible bool) {
	if r.tabBarVisible != visible {
		r.uiDirty = true
	}
	r.tabBarVisible = visible
}

// TabBarVisible reports whether the tab bar is currently shown.
func (r *Renderer) TabBarVisible() bool {
	return r.tabBarVisible
}

// TabBarWidthLogical returns the tab bar's width in the logical cursor
// coordinates GLFW callbacks deliver (0 when the bar is hidden). Callers that
// need to know whether the pointer has left the strip use this rather than
// reaching for the framebuffer-space width.
func (r *Renderer) TabBarWidthLogical() float64 {
	if !r.tabBarVisible {
		return 0
	}
	return float64(r.tabBarWidth / r.hidpiScale())
}

// layoutTabBarWidth is the horizontal space the tab bar reserves: the bar width when
// visible, otherwise 0 so the terminal grid spans the full window.
func (r *Renderer) layoutTabBarWidth() float32 {
	if r.tabBarVisible {
		return r.tabBarWidth
	}
	return 0
}

// layoutMargins is the single source of truth for the pane content area inside
// the window framebuffer. All callers that need to convert between grid
// columns/rows and pane pixels (CalculateGridSize, renderPanes, paneRects) must
// route through this helper so that the grid is sized for exactly the same
// region that panes are painted into. Otherwise cells underflow or overflow
// the pane rect, leaving unpainted strips at small window sizes.
//
// The horizontal margin is split symmetrically: 5px left (between the tab bar
// and the first column) and 5px right (between the last column and the window
// edge). The vertical margin uses the configured top/bottom padding.
func (r *Renderer) layoutMargins(width, height int) (baseX, baseY, availW, availH float32) {
	tabBarW := r.layoutTabBarWidth()
	baseX = tabBarW + 5
	baseY = r.paddingTop
	availW = float32(width) - tabBarW - 10
	availH = float32(height) - r.paddingTop - r.paddingBottom
	if availW < 1 {
		availW = 1
	}
	if availH < 1 {
		availH = 1
	}
	return
}

// SetTextStyleOptions configures synthesized bold/italic and styled underlines.
func (r *Renderer) SetTextStyleOptions(fauxBold, fauxItalic, undercurl bool) {
	r.fauxBold = fauxBold
	r.fauxItalic = fauxItalic
	r.undercurl = undercurl
	r.uiDirty = true
}

type paneRect struct {
	pane   *tab.Pane
	x      float32
	y      float32
	width  float32
	height float32
}

// NewRenderer creates a new renderer with smooth font rendering
func NewRenderer() (*Renderer, error) {
	r := &Renderer{
		theme:           DefaultTheme(),
		fontSize:        defaultFontSize,
		baseFontSize:    defaultFontSize, // Fixed UI font size
		defaultFontSize: defaultFontSize,
		paddingTop:      12.0,
		paddingBottom:   12.0,
		tabBarWidth:     200.0,
		currentFont:     fonts.DefaultFontName(),
		contentScale:    1,
		glyphs:          make(map[rune]Glyph),
		glyphMisses:     make(map[rune]bool),
		// atlasSize calculated dynamically in loadFontData based on glyph count
		fauxBold:   true,
		fauxItalic: true,
		undercurl:  true,
	}

	if err := r.initGL(); err != nil {
		return nil, err
	}

	if err := r.loadFont(); err != nil {
		return nil, err
	}

	// Load the system color-emoji font (optional; nil if none is available).
	r.loadColorFont()

	// Store base cell dimensions for UI elements
	r.baseCellWidth = r.cellWidth
	r.baseCellHeight = r.cellHeight

	return r, nil
}

// loadFont loads the current embedded font and creates a glyph atlas
func (r *Renderer) loadFont() error {
	return r.loadFontData(fonts.DefaultFont())
}

// loadFontData parses a font, builds the rasterization face, and initializes an
// empty dynamic glyph atlas. Glyphs are rasterized lazily by ensureGlyph; this
// replaces the old startup bake of ~4000 fixed-range glyphs.
func (r *Renderer) loadFontData(fontData []byte) error {
	parsedFont, err := opentype.Parse(fontData)
	if err != nil {
		return fmt.Errorf("failed to parse font: %w", err)
	}

	face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    float64(r.fontSize),
		DPI:     r.fontDPI(),
		Hinting: font.HintingFull,
	})
	if err != nil {
		return fmt.Errorf("failed to create font face: %w", err)
	}

	// Close any previously-open face before replacing it.
	if r.face != nil {
		r.face.Close()
	}
	r.face = face

	metrics := face.Metrics()
	r.cellHeight = float32((metrics.Ascent + metrics.Descent).Ceil())
	advance, _ := face.GlyphAdvance('M')
	r.cellWidth = float32(advance.Ceil())
	r.atlasAscent = metrics.Ascent.Ceil()

	// Rebuild the fallback faces at the (possibly new) font size.
	r.rebuildFallbackFaces()

	// Reset the glyph cache and (re)create an empty atlas texture.
	r.glyphs = make(map[rune]Glyph)
	r.glyphMisses = make(map[rune]bool)
	r.asciiCache = [128]asciiGlyphSlot{}
	if r.fontAtlas != 0 {
		gl.DeleteTextures(1, &r.fontAtlas)
		r.fontAtlas = 0
	}
	r.initAtlas(atlasInitialSize)
	r.uiDirty = true // glyph metrics changed; force a redraw
	return nil
}

// fontDPI is the rasterization DPI: 96 scaled by the HiDPI content scale, so
// glyphs cover the right number of framebuffer pixels on 2x displays.
func (r *Renderer) fontDPI() float64 {
	if r.contentScale > 0 {
		return 96 * float64(r.contentScale)
	}
	return 96
}

// hidpiScale is the content scale with a zero-guard, for logical<->framebuffer
// coordinate conversion in the hit-testing helpers.
func (r *Renderer) hidpiScale() float32 {
	if r.contentScale > 0 {
		return r.contentScale
	}
	return 1
}

// SetContentScale applies a new HiDPI content scale: fonts are re-rasterized
// at 96*scale DPI (rebuilding the atlas via the existing font reload path) and
// cell sizes recomputed. The caller must re-fit the grid afterwards
// (CalculateGridSize + resize), since cell dimensions change.
func (r *Renderer) SetContentScale(scale float32) error {
	if scale <= 0 {
		scale = 1
	}
	if scale == r.hidpiScale() {
		r.contentScale = scale
		return nil
	}
	old := r.hidpiScale()
	r.contentScale = scale
	fontData, ok := fonts.GetFont(r.currentFont)
	if !ok {
		fontData = fonts.DefaultFont()
	}
	if err := r.loadFontData(fontData); err != nil {
		return err
	}
	// Base (UI) cell dimensions scale with the rasterization DPI too.
	r.baseCellWidth = r.baseCellWidth * scale / old
	r.baseCellHeight = r.baseCellHeight * scale / old
	return nil
}

// ContentScale returns the renderer's current HiDPI content scale.
func (r *Renderer) ContentScale() float32 { return r.hidpiScale() }

// ConsumeUIDirty reports whether renderer-side visual state (hover underline,
// theme, tab bar, font) changed since the last call, clearing the latch.
func (r *Renderer) ConsumeUIDirty() bool {
	d := r.uiDirty
	r.uiDirty = false
	return d
}

// initGL initializes OpenGL resources
func (r *Renderer) initGL() error {
	// Create quad shader program for colored rectangles
	vertShader := `
		#version 410 core
		layout (location = 0) in vec2 aPos;
		uniform mat4 projection;
		void main() {
			gl_Position = projection * vec4(aPos, 0.0, 1.0);
		}
	` + "\x00"

	fragShader := `
		#version 410 core
		out vec4 FragColor;
		uniform vec4 color;
		void main() {
			FragColor = color;
		}
	` + "\x00"

	var err error
	r.program, err = createProgram(vertShader, fragShader)
	if err != nil {
		return fmt.Errorf("failed to create quad shader: %w", err)
	}

	r.colorLoc = gl.GetUniformLocation(r.program, gl.Str("color\x00"))
	r.projLoc = gl.GetUniformLocation(r.program, gl.Str("projection\x00"))

	// Create text shader program with smooth alpha blending
	textVertShader := `
		#version 410 core
		layout (location = 0) in vec4 vertex; // <vec2 pos, vec2 tex>
		out vec2 TexCoords;
		uniform mat4 projection;
		void main() {
			gl_Position = projection * vec4(vertex.xy, 0.0, 1.0);
			TexCoords = vertex.zw;
		}
	` + "\x00"

	textFragShader := `
		#version 410 core
		in vec2 TexCoords;
		out vec4 FragColor;
		uniform sampler2D text;
		uniform vec4 textColor;
		void main() {
			float alpha = texture(text, TexCoords).r;
			FragColor = vec4(textColor.rgb, textColor.a * alpha);
		}
	` + "\x00"

	r.fontProgram, err = createProgram(textVertShader, textFragShader)
	if err != nil {
		return fmt.Errorf("failed to create text shader: %w", err)
	}

	r.texColorLoc = gl.GetUniformLocation(r.fontProgram, gl.Str("textColor\x00"))
	r.texProjLoc = gl.GetUniformLocation(r.fontProgram, gl.Str("projection\x00"))
	r.texLoc = gl.GetUniformLocation(r.fontProgram, gl.Str("text\x00"))

	// Create quad VAO/VBO
	gl.GenVertexArrays(1, &r.quadVAO)
	gl.GenBuffers(1, &r.quadVBO)
	gl.BindVertexArray(r.quadVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.quadVBO)
	gl.BufferData(gl.ARRAY_BUFFER, 6*2*4, nil, gl.DYNAMIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 2*4, 0)
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)

	// Create font VAO/VBO
	gl.GenVertexArrays(1, &r.fontVAO)
	gl.GenBuffers(1, &r.fontVBO)
	gl.BindVertexArray(r.fontVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.fontVBO)
	gl.BufferData(gl.ARRAY_BUFFER, 6*4*4, nil, gl.DYNAMIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 4, gl.FLOAT, false, 4*4, 0)
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)

	if err := r.initBatches(); err != nil {
		return err
	}

	if err := r.initColorEmoji(); err != nil {
		return err
	}

	return nil
}

// Render renders the terminal
func (r *Renderer) Render(tm *tab.TabManager, width, height int, cursorVisible bool) {
	r.RenderWithHelp(tm, width, height, cursorVisible, false)
}

// RenderWithHelp renders the terminal with optional help panel
func (r *Renderer) RenderWithHelp(tm *tab.TabManager, width, height int, cursorVisible bool, showHelp bool) {
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)

	// Clear background
	gl.ClearColor(r.theme.Background[0], r.theme.Background[1], r.theme.Background[2], r.theme.Background[3])
	gl.Clear(gl.COLOR_BUFFER_BIT)

	// Render tab bar
	r.renderTabBar(tm, width, height, proj)

	// Render terminal content with split pane support
	activeTab := tm.ActiveTab()
	if activeTab != nil {
		r.renderPanes(activeTab, width, height, proj, cursorVisible)
	}

	// Render help panel overlay if requested
	if showHelp {
		r.renderHelpPanel(width, height, proj)
	}
	r.uiFlush()
}

// RenderWithHelpAndPanels renders the terminal with optional help and overlay panels.
func (r *Renderer) RenderWithHelpAndPanels(tm *tab.TabManager, width, height int, cursorVisible bool, showHelp bool, searchPanel *searchpanel.Panel, aiPanel *aipanel.Panel) {
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)

	// Clear background
	gl.ClearColor(r.theme.Background[0], r.theme.Background[1], r.theme.Background[2], r.theme.Background[3])
	gl.Clear(gl.COLOR_BUFFER_BIT)

	// Render tab bar
	r.renderTabBar(tm, width, height, proj)

	// Render terminal content with split pane support
	activeTab := tm.ActiveTab()
	if activeTab != nil {
		r.renderPanes(activeTab, width, height, proj, cursorVisible)
	}

	if searchPanel != nil && searchPanel.Open {
		r.renderSearchPanel(searchPanel, width, height, proj)
	}
	if aiPanel != nil && aiPanel.Open {
		r.renderAIPanel(aiPanel, width, height, proj)
	}

	if showHelp {
		r.renderHelpPanel(width, height, proj)
	}
	r.uiFlush()
}

// Shared overlay-panel palette.
var (
	panelDimText   = [4]float32{0.58, 0.60, 0.66, 1.0}
	panelFaintText = [4]float32{0.42, 0.44, 0.50, 1.0}
	panelErrorText = [4]float32{0.9, 0.3, 0.3, 1.0}
	panelInputBg   = [4]float32{0.02, 0.02, 0.04, 1.0}
)

// panelFocusChord returns the platform chord that moves keyboard focus
// between an overlay panel and the terminal (the pane-cycle binding).
func panelFocusChord() string {
	if runtime.GOOS == "darwin" {
		return "Cmd+]"
	}
	return "Ctrl+Shift+]"
}

// panelCopyChord returns the platform chord bound to ActionCopy.
func panelCopyChord() string {
	if runtime.GOOS == "darwin" {
		return "Cmd+C"
	}
	return "Ctrl+Shift+C"
}

// drawPanelFrame draws the shared overlay-panel chrome: a rounded backdrop
// with a header strip and a border that brightens when the panel has keyboard
// focus, so it is obvious whether typing goes to the panel or the terminal.
func (r *Renderer) drawPanelFrame(x, y, w, h, lineHeight float32, focused bool, proj [16]float32) {
	radius := float32(8)
	borderColor := r.theme.TabActive
	borderWidth := float32(2)
	if !focused {
		borderColor = withAlpha(r.theme.TabActive, 0.3)
		borderWidth = 1
	}
	panelBg := [4]float32{0.05, 0.06, 0.08, 0.97}
	headerBg := [4]float32{0.09, 0.10, 0.14, 1.0}

	r.drawRoundedRect(x-borderWidth, y-borderWidth, w+2*borderWidth, h+2*borderWidth, radius+borderWidth, borderColor, proj)
	r.drawRoundedRect(x, y, w, h, radius, panelBg, proj)

	// Header strip: rounded along the panel top, square at its bottom edge.
	headerH := lineHeight * 1.7
	if headerH > h {
		headerH = h
	}
	r.drawRoundedRect(x, y, w, headerH, radius, headerBg, proj)
	r.drawRect(x, y+headerH/2, w, headerH/2, headerBg, proj)
	r.drawRect(x, y+headerH, w, 1, withAlpha(r.theme.TabActive, 0.25), proj)
}

// drawPanelInputBox draws a rounded input field whose border brightens when
// it will receive typed characters.
func (r *Renderer) drawPanelInputBox(x, y, w, h float32, active bool, proj [16]float32) {
	border := [4]float32{0.22, 0.24, 0.32, 1.0}
	if active {
		border = withAlpha(r.theme.TabActive, 0.85)
	}
	r.drawRoundedRect(x-1, y-1, w+2, h+2, 5, border, proj)
	r.drawRoundedRect(x, y, w, h, 4, panelInputBg, proj)
}

// drawPanelScrollbar draws a thin scroll indicator along the right edge of a
// scrollable region when its content overflows.
func (r *Renderer) drawPanelScrollbar(x, top, bottom float32, total, visible, scroll int, proj [16]float32) {
	if total <= visible || visible <= 0 {
		return
	}
	trackH := bottom - top
	if trackH <= 16 {
		return
	}
	r.drawRect(x, top, 3, trackH, [4]float32{1, 1, 1, 0.07}, proj)
	thumbH := trackH * float32(visible) / float32(total)
	if thumbH < 14 {
		thumbH = 14
	}
	pos := float32(scroll) / float32(total-visible)
	if pos < 0 {
		pos = 0
	}
	if pos > 1 {
		pos = 1
	}
	r.drawRoundedRect(x, top+pos*(trackH-thumbH), 3, thumbH, 1.5, withAlpha(r.theme.TabActive, 0.6), proj)
}

// drawTextRight draws text right-aligned so it ends at rightX.
func (r *Renderer) drawTextRight(rightX, y float32, text string, clr [4]float32, proj [16]float32) {
	r.drawText(rightX-float32(textCells(text))*r.cellWidth, y, text, clr, proj)
}

func (r *Renderer) renderSearchPanel(panel *searchpanel.Panel, width, height int, proj [16]float32) {
	layout := panel.Layout(width, height, r.cellWidth, r.cellHeight)

	r.drawPanelFrame(layout.PanelX, layout.PanelY, layout.PanelWidth, layout.PanelHeight, layout.LineHeight, panel.Focused, proj)

	maxChars := max(int(layout.ContentWidth/r.cellWidth)-2, 10)

	r.drawText(layout.ContentX, layout.HeaderY, "Web Search", r.theme.TabActive, proj)
	proxyBadge := "proxy off"
	proxyColor := panelFaintText
	if panel.ProxyEnabled {
		proxyBadge = "proxy on"
		proxyColor = r.theme.Cursor
	}
	r.drawTextRight(layout.ContentX+layout.ContentWidth, layout.HeaderY, proxyBadge, proxyColor, proj)

	r.drawText(layout.ContentX, layout.InputLabelY, "Query", panelDimText, proj)
	inputActive := panel.Focused && panel.Mode == searchpanel.ModeResults
	r.drawPanelInputBox(layout.ContentX, layout.InputBoxY, layout.ContentWidth, layout.LineHeight, inputActive, proj)

	inputText := truncateHeadToCells(panel.Query, maxChars)
	inputTextY := layout.InputBoxY + layout.LineHeight*0.75
	if inputText == "" {
		r.drawText(layout.ContentX+8, inputTextY, "Search the web...", panelFaintText, proj)
		if inputActive {
			r.drawInputCursor(layout.ContentX+8, inputTextY, proj)
		}
	} else {
		r.drawText(layout.ContentX+8, inputTextY, inputText, r.theme.Foreground, proj)
		if inputActive {
			r.drawInputCursor(layout.ContentX+8+float32(textCells(inputText))*r.cellWidth, inputTextY, proj)
		}
	}

	status := panel.Status
	statusColor := r.theme.Cursor
	if panel.Mode == searchpanel.ModePreview && panel.FindActive {
		matches := panel.FindMatchLines()
		cur := 0
		if panel.FindMatch >= 0 && panel.FindMatch < len(matches) {
			cur = panel.FindMatch + 1
		}
		status = fmt.Sprintf("find: %s (%d/%d)", panel.FindQuery, cur, len(matches))
	} else if panel.Loading {
		if status == "" {
			status = "Loading..."
		}
		status = panel.SpinnerFrame() + " " + status
	} else if strings.Contains(status, "failed") || strings.HasPrefix(status, "Failed") {
		statusColor = panelErrorText
	} else if status == "" {
		if panel.HistoryIndex >= 0 {
			status = fmt.Sprintf("history %d/%d", panel.HistoryIndex+1, len(panel.History))
			statusColor = panelDimText
		} else if panel.Mode == searchpanel.ModeResults && len(panel.Results) > 0 {
			noun := "results"
			if len(panel.Results) == 1 {
				noun = "result"
			}
			status = fmt.Sprintf("%d %s for \"%s\"", len(panel.Results), noun, panel.LastQuery)
			statusColor = panelDimText
		}
	}
	if status != "" {
		r.drawText(layout.ContentX, layout.StatusY, truncateToCells(status, maxChars), statusColor, proj)
	}

	if panel.Mode == searchpanel.ModePreview {
		r.renderSearchPreview(panel, layout, maxChars, proj)
	} else {
		r.renderSearchResults(panel, layout, maxChars, proj)
	}

	var footerText string
	switch {
	case !panel.Focused:
		footerText = fitToCells(maxChars, panelFocusChord()+": focus panel", "Focus: "+panelFocusChord())
	case panel.Mode == searchpanel.ModePreview:
		// Position indicator: first visible wrapped line / total, plus scroll
		// percentage. PreviewScroll is clamped here for display only.
		pos := ""
		if total := len(panel.PreviewWrapped); total > 0 {
			vis := max(layout.VisibleLines-1, 1)
			first := min(panel.PreviewScroll, max(total-vis, 0))
			pct := 100
			if maxScroll := total - vis; maxScroll > 0 {
				pct = first * 100 / maxScroll
			}
			pos = fmt.Sprintf("L%d/%d %d%% | ", first+1, total, pct)
		}
		if n := len(panel.Links); n > 0 {
			pos += fmt.Sprintf("[%d/%d links] ", panel.SelectedLink+1, n)
		}
		footerText = fitToCells(maxChars,
			pos+"Tab: links | /: find | Ctrl+A: to AI | Ctrl+B: bookmark | Ctrl+Y: URL | Esc: back",
			pos+"Tab: links | Enter: follow | Ctrl+Left/Right: back/fwd | /: find | Esc: back",
			pos+"Tab: links | /: find | n/N: next/prev | Esc: back | Ctrl+O: browser",
			pos+"/: find | Esc: back | Up/Down: scroll | Ctrl+O: browser",
			pos+"Esc: back | Up/Down: scroll",
			pos+"Esc: back",
			"Esc: back")
	case panel.ShowingBookmarks:
		// Bookmark view: Enter previews and Ctrl+B returns to the results,
		// regardless of QueryDirty (main.go checks ShowingBookmarks first).
		footerText = fitToCells(maxChars,
			"Enter: preview | Ctrl+B: back to results | Esc: close",
			"Enter: preview | Ctrl+B: back to results",
			"Enter: preview | Ctrl+B: back")
	case len(panel.Results) > 0 && !panel.QueryDirty:
		footerText = fitToCells(maxChars,
			fmt.Sprintf("%d/%d | Enter: preview | Ctrl+B: bookmarks | Ctrl+Y: URL | Ctrl+O: browser", panel.Selected+1, len(panel.Results)),
			fmt.Sprintf("%d/%d | Enter: preview | Ctrl+R: retry | Ctrl+O: browser", panel.Selected+1, len(panel.Results)),
			fmt.Sprintf("%d/%d | Enter: preview | Ctrl+O: browser", panel.Selected+1, len(panel.Results)),
			fmt.Sprintf("%d/%d | Enter: preview", panel.Selected+1, len(panel.Results)),
			"Enter: preview")
	default:
		footerText = fitToCells(maxChars,
			"Enter: search | Up/Down: history | Esc: close",
			"Enter: search | Up/Down: history",
			"Enter: search")
	}
	r.drawText(layout.ContentX, layout.FooterY, footerText, panelDimText, proj)
}

func (r *Renderer) renderAIPanel(panel *aipanel.Panel, width, height int, proj [16]float32) {
	layout := panel.Layout(width, height, r.cellWidth, r.cellHeight)

	r.drawPanelFrame(layout.PanelX, layout.PanelY, layout.PanelWidth, layout.PanelHeight, layout.LineHeight, panel.Focused, proj)

	maxChars := max(int(layout.ContentWidth/r.cellWidth)-2, 10)

	r.drawText(layout.ContentX, layout.HeaderY, "AI Chat", r.theme.TabActive, proj)
	if model := panel.LoadedModel; model != "" {
		avail := maxChars - len("AI Chat") - 2
		if avail > 3 {
			r.drawTextRight(layout.ContentX+layout.ContentWidth, layout.HeaderY,
				truncateToCells(model, avail), panelFaintText, proj)
		}
	}

	// Errors and one-off notices render in the status slot under the header;
	// the loading spinner instead joins the conversation flow below, next to
	// the message it belongs to.
	if status := panel.Status; status != "" && !panel.Loading {
		statusColor := r.theme.Cursor
		if strings.Contains(status, "failed") || strings.HasPrefix(status, "Missing") {
			statusColor = panelErrorText
		}
		r.drawText(layout.ContentX, layout.StatusY, truncateToCells(status, maxChars), statusColor, proj)
	}

	r.drawText(layout.ContentX, layout.InputLabelY, "Message", panelDimText, proj)
	r.drawPanelInputBox(layout.ContentX, layout.InputBoxY, layout.ContentWidth, layout.InputBoxH, panel.Focused, proj)

	// Wrap input text for multiline display
	inputLines := panel.WrapInput(maxChars - 2)
	panel.EnsureInputCursorVisible(layout.InputLines)

	// Draw visible input lines
	inputY := layout.InputBoxY + layout.LineHeight*0.75
	visibleInputLines := layout.InputLines
	inputStartLine := panel.InputScroll

	if panel.Input == "" {
		r.drawText(layout.ContentX+8, inputY, "Ask anything...", panelFaintText, proj)
		if panel.Focused {
			r.drawInputCursor(layout.ContentX+8, inputY, proj)
		}
	} else {
		for i := 0; i < visibleInputLines && inputStartLine+i < len(inputLines); i++ {
			lineText := inputLines[inputStartLine+i]
			r.drawText(layout.ContentX+8, inputY, lineText, r.theme.Foreground, proj)
			// Caret after the last line's text
			if panel.Focused && inputStartLine+i == len(inputLines)-1 {
				r.drawInputCursor(layout.ContentX+8+float32(textCells(lineText))*r.cellWidth, inputY, proj)
			}
			inputY += layout.LineHeight
		}
	}

	// Show scroll indicator if input has more lines
	if len(inputLines) > visibleInputLines {
		scrollIndicator := fmt.Sprintf("↕ %d/%d", panel.InputScroll+1, len(inputLines)-visibleInputLines+1)
		r.drawText(layout.ContentX+layout.ContentWidth-float32(len(scrollIndicator))*r.cellWidth-8,
			layout.InputBoxY+layout.InputBoxH-layout.LineHeight*0.3,
			scrollIndicator, [4]float32{0.5, 0.5, 0.5, 1.0}, proj)
	}

	lines := panel.WrappedForRender(maxChars)

	// While waiting on the model, the spinner rides at the end of the
	// conversation (where the reply will appear) rather than up in the header.
	if panel.Loading {
		status := panel.Status
		if status == "" {
			status = "Thinking..."
		}
		if len(lines) > 0 {
			lines = append(lines, aipanel.WrappedLine{Role: "", Text: ""})
		}
		lines = append(lines, aipanel.WrappedLine{
			Role: "thinking", Text: panel.SpinnerFrame() + " " + status, IsThinking: true,
		})
	}

	if len(lines) == 0 {
		r.drawText(layout.ContentX, layout.MessagesStart,
			truncateToCells("Ask a question to begin.", maxChars), panelDimText, proj)
		r.drawText(layout.ContentX, layout.MessagesStart+layout.LineHeight,
			fitToCells(maxChars,
				"Enter sends, Shift+Enter adds a newline.",
				"Enter sends, Shift+Enter: newline.",
				"Enter sends."), panelFaintText, proj)
	} else {
		visibleLines := layout.VisibleLines
		totalLines := len(lines)
		maxScroll := max(totalLines-visibleLines, 0)
		if panel.AutoScroll {
			panel.Scroll = maxScroll
			panel.AutoScroll = false
		}
		if panel.Scroll > maxScroll {
			panel.Scroll = maxScroll
		}
		if panel.Scroll < 0 {
			panel.Scroll = 0
		}

		startLine := panel.Scroll
		lineY := layout.MessagesStart
		// Chat-style anchoring: while the conversation is shorter than the
		// viewport, hug it to the input box instead of leaving a dead gap.
		panel.AnchorOffset = 0
		if totalLines < visibleLines {
			panel.AnchorOffset = visibleLines - totalLines
			lineY = layout.MessagesStart + float32(panel.AnchorOffset)*layout.LineHeight
		}
		codeColor := [4]float32{0.7, 0.8, 0.6, 1.0}           // Greenish for code
		headerColor := [4]float32{0.9, 0.7, 0.4, 1.0}         // Orange/gold for headers
		bulletColor := [4]float32{0.7, 0.7, 0.9, 1.0}         // Light blue for bullets
		thinkingColor := [4]float32{0.6, 0.5, 0.7, 0.85}      // Purple/dim for thinking
		thinkingHeaderColor := [4]float32{0.7, 0.5, 0.8, 1.0} // Brighter purple for thinking header
		// Compute selection range for highlight
		selStart, selEnd := panel.SelectionStart, panel.SelectionEnd
		if selEnd < selStart {
			selStart, selEnd = selEnd, selStart
		}

		for i := 0; i < visibleLines && startLine+i < totalLines; i++ {
			lineIdx := startLine + i
			line := lines[lineIdx]

			// Draw selection highlight
			if panel.SelectionActive && lineIdx >= selStart && lineIdx <= selEnd {
				selColor := [4]float32{r.theme.Selection[0], r.theme.Selection[1], r.theme.Selection[2], 0.3}
				r.drawRect(layout.ContentX, lineY-layout.LineHeight*0.75, layout.ContentWidth, layout.LineHeight, selColor, proj)
			}

			// Role accent bar in the left padding, continuous across each message.
			if line.Role != "" {
				barColor := withAlpha(r.theme.Foreground, 0.25)
				switch line.Role {
				case "user":
					barColor = withAlpha(r.theme.TabActive, 0.9)
				case "error":
					barColor = panelErrorText
				case "thinking":
					barColor = thinkingColor
				}
				r.drawRect(layout.ContentX-9, lineY-layout.LineHeight*0.75, 3, layout.LineHeight, barColor, proj)
			}

			if strings.TrimSpace(line.Text) != "" {
				color := r.theme.Foreground
				if line.IsThinking {
					// Thinking content uses purple/dim colors
					if line.IsHeader {
						color = thinkingHeaderColor
					} else {
						color = thinkingColor
					}
				} else if line.InCode {
					color = codeColor
				} else if line.IsHeader {
					color = headerColor
				} else if line.IsBullet {
					color = bulletColor
				} else {
					switch line.Role {
					case "user":
						color = r.theme.TabActive
					case "assistant":
						color = r.theme.Foreground
					case "error":
						color = [4]float32{0.9, 0.3, 0.3, 1.0} // Red for errors
					case "tool":
						color = panelDimText // tool activity notes stay quiet
					default:
						if line.Role != "" {
							color = r.theme.Cursor
						}
					}
				}
				r.drawText(layout.ContentX, lineY, line.Text, color, proj)
			}
			lineY += layout.LineHeight
		}

		r.drawPanelScrollbar(layout.PanelX+layout.PanelWidth-7,
			layout.MessagesStart-layout.LineHeight*0.75, layout.MessagesEnd,
			totalLines, visibleLines, panel.Scroll, proj)
	}

	var footerText string
	if !panel.Focused {
		footerText = fitToCells(maxChars, panelFocusChord()+": focus panel", "Focus: "+panelFocusChord())
	} else {
		full := "Enter: send | Shift+Enter: newline | " + panelCopyChord() + ": copy"
		if aipanel.HasThinkingContent(panel.Messages) {
			full += " | Ctrl+T: thinking"
		}
		footerText = fitToCells(maxChars, full,
			"Enter: send | Shift+Enter: newline | "+panelCopyChord()+": copy",
			"Enter: send | Shift+Enter: newline",
			"Enter sends")
	}
	r.drawText(layout.ContentX, layout.FooterY, footerText, panelDimText, proj)
}

// fitToCells returns the first candidate that fits within max display cells,
// falling back to a hard truncation of the last (shortest) candidate. Used for
// hint footers so narrow panels drop whole hints instead of cutting mid-word.
func fitToCells(max int, candidates ...string) string {
	for _, c := range candidates {
		if textCells(c) <= max {
			return c
		}
	}
	return truncateToCells(candidates[len(candidates)-1], max)
}

func (r *Renderer) renderSearchResults(panel *searchpanel.Panel, layout searchpanel.Layout, maxChars int, proj [16]float32) {
	if len(panel.Results) == 0 {
		if !panel.Loading {
			hint := "Type a query and press Enter to search."
			if strings.TrimSpace(panel.Query) != "" {
				if panel.QueryDirty {
					hint = "Press Enter to search."
				} else {
					hint = "No results."
				}
			}
			r.drawText(layout.ContentX, layout.ResultsStart, hint, panelFaintText, proj)
		}
		return
	}

	linesPerResult := panel.LinesPerResult()
	visibleLines := layout.VisibleLines

	for i, result := range panel.Results {
		startLine := i * linesPerResult
		if startLine+linesPerResult <= panel.ResultsScroll {
			continue
		}
		if startLine >= panel.ResultsScroll+visibleLines {
			break
		}

		drawLine := startLine - panel.ResultsScroll
		drawY := layout.ResultsStart + float32(drawLine)*layout.LineHeight

		if i == panel.Selected {
			highlightColor := [4]float32{0.10, 0.12, 0.19, 1.0}
			if panel.Focused {
				highlightColor = [4]float32{0.13, 0.16, 0.26, 1.0}
			}
			r.drawRoundedRect(layout.ContentX-8, drawY-layout.LineHeight+6, layout.ContentWidth+16, layout.LineHeight*2.2, 4, highlightColor, proj)
			r.drawRect(layout.ContentX-8, drawY-layout.LineHeight+6, 3, layout.LineHeight*2.2, r.theme.TabActive, proj)
		}

		title := truncateToCells(fmt.Sprintf("%d. %s", i+1, strings.TrimSpace(result.Title)), maxChars)
		titleColor := r.theme.TabActive
		if i != panel.Selected {
			titleColor = withAlpha(r.theme.TabActive, 0.8)
		}
		r.drawText(layout.ContentX, drawY, title, titleColor, proj)

		subLine := strings.TrimSpace(result.Snippet)
		if subLine == "" {
			subLine = strings.TrimSpace(result.URL)
		}
		r.drawText(layout.ContentX+12, drawY+layout.LineHeight, truncateToCells(subLine, maxChars), panelDimText, proj)
	}

	r.drawPanelScrollbar(layout.PanelX+layout.PanelWidth-7,
		layout.ResultsStart-layout.LineHeight*0.75, layout.ResultsEnd,
		panel.ResultsTotalLines(), visibleLines, panel.ResultsScroll, proj)
}

func (r *Renderer) renderSearchPreview(panel *searchpanel.Panel, layout searchpanel.Layout, maxChars int, proj [16]float32) {
	header := "Preview"
	if panel.PreviewTitle != "" {
		header = "Preview: " + panel.PreviewTitle
	}
	r.drawText(layout.ContentX, layout.ResultsStart, truncateToCells(header, maxChars), r.theme.TabActive, proj)

	// Re-wrap only when the content, width, or theme changed; the preview is
	// static text between navigations and this ran every frame.
	key := previewWrapKey{n: len(panel.PreviewLines), chars: maxChars, themeGen: r.themeGen}
	if key.n > 0 {
		key.src = &panel.PreviewLines[0]
	}
	rewrapped := key != r.previewWrapKey || r.previewWrapLines == nil
	if rewrapped {
		r.previewWrapLines = buildWrappedPreview(panel.PreviewLines, maxChars, r.theme)
		r.previewWrapKey = key
	}
	wrappedLines := r.previewWrapLines
	if rewrapped || panel.PreviewWrapped == nil || panel.PreviewWrapChars != maxChars {
		panel.PreviewWrapped = panel.PreviewWrapped[:0]
		panel.PreviewWrapChars = maxChars
		for _, line := range wrappedLines {
			panel.PreviewWrapped = append(panel.PreviewWrapped, line.text)
		}
	}

	visibleLines := max(layout.VisibleLines-1, 1)
	startLine := panel.PreviewScroll
	maxScroll := max(len(wrappedLines)-visibleLines, 0)
	if startLine > maxScroll {
		startLine = maxScroll
	}

	// Compute selection range for highlight
	selStart, selEnd := panel.SelectionStart, panel.SelectionEnd
	if selEnd < selStart {
		selStart, selEnd = selEnd, selStart
	}

	findNeedle := ""
	if panel.FindActive {
		findNeedle = strings.ToLower(strings.TrimSpace(panel.FindQuery))
	}

	// Resolve the selected link's marker to its exact line and offset so a
	// literal "[n]" elsewhere on the page (Wikipedia citations) is not
	// highlighted by mistake.
	linkMarker := ""
	markerLine, markerCol := -1, -1
	if panel.SelectedLink >= 0 && panel.SelectedLink < len(panel.Links) {
		linkMarker = fmt.Sprintf("[%d]", panel.SelectedLink+1)
		markerLine, markerCol = panel.SelectedLinkMarkerPos()
	}

	lineY := layout.ResultsStart + layout.LineHeight
	for i := 0; i < visibleLines && startLine+i < len(wrappedLines); i++ {
		lineIdx := startLine + i
		line := wrappedLines[lineIdx]

		// Draw selection highlight
		if panel.SelectionActive && lineIdx >= selStart && lineIdx <= selEnd {
			selColor := [4]float32{r.theme.Selection[0], r.theme.Selection[1], r.theme.Selection[2], 0.3}
			r.drawRect(layout.ContentX, lineY-layout.LineHeight*0.75, layout.ContentWidth, layout.LineHeight, selColor, proj)
		}

		// Highlight in-page find matches (same rect pattern as the selection).
		if findNeedle != "" {
			lower := strings.ToLower(line.text)
			hlColor := withAlpha(r.theme.Cursor, 0.35)
			for from := 0; ; {
				idx := strings.Index(lower[from:], findNeedle)
				if idx < 0 {
					break
				}
				matchStart := from + idx
				matchEnd := matchStart + len(findNeedle)
				x := layout.ContentX + float32(textCells(lower[:matchStart]))*r.cellWidth
				w := float32(textCells(lower[matchStart:matchEnd])) * r.cellWidth
				r.drawRect(x, lineY-layout.LineHeight*0.75, w, layout.LineHeight, hlColor, proj)
				from = matchEnd
			}
		}

		// Highlight the selected link's "[n]" marker (selection color, a bit
		// stronger than the mouse selection so it reads as a cursor).
		if linkMarker != "" && lineIdx == markerLine && markerCol >= 0 {
			selColor := [4]float32{r.theme.Selection[0], r.theme.Selection[1], r.theme.Selection[2], 0.55}
			x := layout.ContentX + float32(textCells(line.text[:markerCol]))*r.cellWidth
			w := float32(textCells(linkMarker)) * r.cellWidth
			r.drawRect(x, lineY-layout.LineHeight*0.75, w, layout.LineHeight, selColor, proj)
		}

		r.drawTextStyled(layout.ContentX, lineY, line.text, line.color, line.bold, proj)
		lineY += layout.LineHeight
	}

	r.drawPanelScrollbar(layout.PanelX+layout.PanelWidth-7,
		layout.ResultsStart+layout.LineHeight*0.25, layout.ResultsEnd,
		len(wrappedLines), visibleLines, startLine, proj)
}

type styledLine struct {
	text  string
	color [4]float32
	bold  bool
}

func buildWrappedPreview(lines []string, maxChars int, theme Theme) []styledLine {
	out := []styledLine{}
	inCode := false

	codeColor := [4]float32{0.7, 0.8, 0.6, 1.0}   // Greenish for code
	bulletColor := [4]float32{0.7, 0.7, 0.9, 1.0} // Light blue for bullets
	quoteColor := [4]float32{0.6, 0.7, 0.6, 1.0}  // Muted green for quotes

	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)

		// Toggle code block state; keep the fence line, dimmed.
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			out = append(out, styledLine{text: truncateToCells(trimmed, maxChars), color: panelDimText})
			continue
		}

		// Skip empty lines but add spacing
		if trimmed == "" {
			out = append(out, styledLine{text: "", color: theme.Foreground})
			continue
		}

		// Skip table separators
		if strings.HasPrefix(trimmed, "|--") || strings.HasPrefix(trimmed, "| --") ||
			strings.HasPrefix(trimmed, "|:") || strings.HasPrefix(trimmed, "| :") {
			continue
		}

		color := theme.Foreground
		prefix := ""
		indent := ""
		text := trimmed

		if inCode {
			// Code block - preserve as-is
			out = append(out, styledLine{text: truncateToCells(text, maxChars), color: codeColor})
			continue
		}

		// Handle headers
		if strings.HasPrefix(text, "#") {
			level := 0
			for level < len(text) && text[level] == '#' {
				level++
			}
			text = strings.TrimSpace(text[level:])
			if text == "" {
				continue
			}
			if level > 3 {
				level = 3
			}
			prefix = strings.Repeat("=", level) + " "
			text = stripInlineMarkdown(text)
			wrapped := wrapText(text, maxChars, prefix, "   ")
			for _, line := range wrapped {
				out = append(out, styledLine{text: line, color: theme.TabActive, bold: true})
			}
			continue
		}

		// Handle bullet points ("• " comes pre-marked from the extractors)
		if strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "* ") || strings.HasPrefix(text, "+ ") || strings.HasPrefix(text, "• ") {
			prefix = "• "
			if after, ok := strings.CutPrefix(text, "• "); ok {
				text = strings.TrimSpace(after)
			} else {
				text = strings.TrimSpace(text[2:])
			}
			indent = "  "
			text = stripInlineMarkdown(text)
			wrapped := wrapText(text, maxChars, prefix, indent)
			for _, line := range wrapped {
				out = append(out, styledLine{text: line, color: bulletColor})
			}
			continue
		}

		// Handle numbered lists
		if len(text) > 2 && text[0] >= '0' && text[0] <= '9' {
			dotIdx := strings.Index(text, ".")
			if dotIdx > 0 && dotIdx < 4 {
				prefix = text[:dotIdx+1] + " "
				text = strings.TrimSpace(text[dotIdx+1:])
				indent = strings.Repeat(" ", len(prefix))
				text = stripInlineMarkdown(text)
				wrapped := wrapText(text, maxChars, prefix, indent)
				for _, line := range wrapped {
					out = append(out, styledLine{text: line, color: bulletColor})
				}
				continue
			}
		}

		// Handle blockquotes
		if strings.HasPrefix(text, "> ") {
			prefix = "│ "
			text = strings.TrimSpace(text[2:])
			indent = "  "
			text = stripInlineMarkdown(text)
			wrapped := wrapText(text, maxChars, prefix, indent)
			for _, line := range wrapped {
				out = append(out, styledLine{text: line, color: quoteColor})
			}
			continue
		}

		// Handle table rows
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			cells := strings.Split(trimmed, "|")
			var cellTexts []string
			for _, cell := range cells {
				cell = strings.TrimSpace(cell)
				if cell != "" {
					cellTexts = append(cellTexts, stripInlineMarkdown(cell))
				}
			}
			if len(cellTexts) > 0 {
				text = strings.Join(cellTexts, " | ")
				wrapped := wrapText(text, maxChars, "", "")
				for _, line := range wrapped {
					out = append(out, styledLine{text: line, color: theme.Foreground})
				}
			}
			continue
		}

		// Regular text
		text = stripInlineMarkdown(text)
		wrapped := wrapText(text, maxChars, prefix, indent)
		for _, line := range wrapped {
			out = append(out, styledLine{text: line, color: color})
		}
	}
	return out
}

func stripInlineMarkdown(text string) string {
	// Remove bold/italic markers
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "__", "")
	text = strings.ReplaceAll(text, "*", "")
	text = strings.ReplaceAll(text, "_", "")
	text = strings.ReplaceAll(text, "`", "")

	// Convert links [text](url) to just text
	for {
		start := strings.Index(text, "[")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], "](")
		if end == -1 {
			break
		}
		end += start
		urlEnd := strings.Index(text[end:], ")")
		if urlEnd == -1 {
			break
		}
		urlEnd += end
		linkText := text[start+1 : end]
		text = text[:start] + linkText + text[urlEnd+1:]
	}

	return strings.TrimSpace(text)
}

// wrapText wraps text into lines of at most maxChars display cells. Width is
// measured in cells (not bytes) so multibyte and wide runes wrap correctly.
func wrapText(text string, maxChars int, prefix, indent string) []string {
	if maxChars <= 0 {
		return []string{prefix + text}
	}
	if prefix == "" && indent == "" && textCells(text) <= maxChars {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{strings.TrimRight(prefix, " ")}
	}

	lines := []string{}
	line := prefix
	lineLimit := max(maxChars, 4)

	for _, word := range words {
		if line == "" {
			line = indent
		}
		next := line
		if next != "" && !strings.HasSuffix(next, " ") {
			next += " "
		}
		next += word

		if textCells(next) <= lineLimit {
			line = next
			continue
		}

		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRight(line, " "))
			line = indent + word
			continue
		}

		// Hard wrap a single word wider than the line, by cells.
		rs := []rune(word)
		budget := max(lineLimit-textCells(indent), 1)
		for len(rs) > 0 {
			if textCells(string(rs)) <= budget {
				lines = append(lines, indent+string(rs))
				break
			}
			n := 0
			w := 0
			for i, c := range rs {
				cw := grid.RuneWidth(c)
				if w+cw > budget {
					break
				}
				w += cw
				n = i + 1
			}
			if n < 1 {
				n = 1
			}
			lines = append(lines, indent+string(rs[:n]))
			rs = rs[n:]
		}
		line = ""
	}

	if strings.TrimSpace(line) != "" {
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

// previewWrapKey fingerprints the inputs of buildWrappedPreview: the preview
// lines slice is replaced wholesale on navigation (never mutated in place),
// so its first-element pointer plus length captures content changes.
type previewWrapKey struct {
	src      *string
	n        int
	chars    int
	themeGen uint64
}

// helpSection is one titled group of keybinding rows in the help panel.
type helpSection struct {
	title    string
	bindings [][2]string
}

// getHelpSections returns all keybinding sections for the help panel. The
// content is static per process, so it is built once and cached (the help
// renderer asks for it several times per frame).
func (r *Renderer) getHelpSections() []helpSection {
	if r.helpSections != nil {
		return r.helpSections
	}
	r.helpSections = buildHelpSections()
	return r.helpSections
}

func buildHelpSections() []helpSection {
	// Platform-aware modifier labels. The Super (Cmd) layer is identical on
	// every platform; Linux shows its conventional Ctrl+Shift chords with
	// the Super alias where the letters differ.
	isMac := runtime.GOOS == "darwin"
	mod := "Ctrl+Shift"
	exitKey := "Ctrl+Q"
	pasteKey := "Super+V / Ctrl+Shift+P"
	splitV := "Super+D / Ctrl+Shift+V"
	splitH := "Super+Shift+D / Ctrl+Shift+H"
	nextTab := "Ctrl+Tab / Super+Shift+]"
	prevTab := "Ctrl+Shift+Tab / Super+Shift+["
	cyclePanes := "Super+Shift+Tab"
	// Find stays on the Super layer on Linux too: Ctrl+Shift+F is already the
	// web-search panel there, so there is no Ctrl+Shift chord left for it.
	findKey := "Super+Shift+F"
	if isMac {
		mod = "Cmd"
		exitKey = "Cmd+Q"
		pasteKey = "Cmd+V"
		splitV = "Cmd+D"
		splitH = "Cmd+Shift+D"
		nextTab = "Cmd+Shift+]"
		prevTab = "Cmd+Shift+["
		cyclePanes = "Cmd+Shift+Tab"
		findKey = "Cmd+Shift+F"
	}

	return []helpSection{
		{
			title: "General",
			bindings: [][2]string{
				{exitKey, "Exit terminal"},
				{mod + "+C", "Copy visible screen"},
				{pasteKey, "Paste clipboard"},
				{"Shift+Enter", "Toggle fullscreen"},
				{mod + "+K", "Show/hide help"},
				{mod + "+S", "Open settings"},
				{mod + "+F", "Toggle web search"},
				{findKey, "Find in scrollback"},
				{mod + "+A", "Toggle AI chat"},
				{mod + "++", "Zoom in"},
				{mod + "+-", "Zoom out"},
				{mod + "+0", "Reset zoom"},
			},
		},
		{
			title: "Tab Management",
			bindings: [][2]string{
				{mod + "+T", "New tab"},
				{mod + "+X", "Close current tab"},
				{mod + "+1..9", "Jump to tab N"},
				{nextTab, "Next tab"},
				{prevTab, "Previous tab"},
				{"Drag chip", "Reorder tabs"},
				{"Drag chip right", "Tear tab into a new window"},
			},
		},
		{
			title: "Split Panes",
			bindings: [][2]string{
				{splitV, "Split vertical"},
				{splitH, "Split horizontal"},
				{mod + "+W", "Close pane"},
				{cyclePanes, "Cycle panes"},
				{mod + "+]", "Next pane"},
				{mod + "+[", "Previous pane"},
				{mod + "+[ or ]", "Cycle overlay panel (when open)"},
				{"Ctrl+R", "Toggle resize mode"},
				{"Arrow Keys", "Resize active pane"},
			},
		},
		{
			title: "Web Search / AI Chat",
			bindings: [][2]string{
				{"Enter", "Search / open result / send message"},
				{"Shift+Enter", "Newline in AI message"},
				{"Up/Down", "Select result, history, or scroll"},
				{"Ctrl+O", "Open result in browser"},
				{"Ctrl+Shift+R", "Toggle reader proxy"},
				{"Ctrl+T", "Expand/collapse AI thinking"},
				{"Ctrl+U", "Clear input"},
				{"Esc", "Back / close panel"},
			},
		},
		{
			title: "Scrolling",
			bindings: [][2]string{
				{"Mouse Wheel", "Scroll 3 lines"},
				{"Shift+Up", "Scroll up 1 line"},
				{"Shift+Down", "Scroll down 1 line"},
				{"Shift+PageUp", "Scroll up 5 lines"},
				{"Shift+PageDown", "Scroll down 5 lines"},
			},
		},
		{
			title: "Text Navigation",
			bindings: [][2]string{
				{"Home", "Beginning of line"},
				{"End", "End of line"},
				{"PageUp", "Page up"},
				{"PageDown", "Page down"},
				{"Insert", "Toggle insert mode"},
				{"Delete", "Delete character"},
			},
		},
		{
			title: "Modifier Keys",
			bindings: [][2]string{
				{"Ctrl+letter", "Control character"},
				{"Alt+letter", "ESC + letter"},
				{"Ctrl+D", "End of input"},
				{"Ctrl+Z", "Suspend process"},
				{"Ctrl+L", "Clear screen"},
			},
		},
		{
			title: "Function Keys",
			bindings: [][2]string{
				{"F1-F12", "Passed to app"},
			},
		},
	}
}

// getTotalHelpLines calculates total lines needed for help content
func (r *Renderer) getTotalHelpLines() int {
	sections := r.getHelpSections()
	total := 0
	for _, section := range sections {
		total += 1 + len(section.bindings) + 1 // title + bindings + spacing
	}
	return total
}

// ScrollHelpUp scrolls the help panel up
func (r *Renderer) ScrollHelpUp() {
	if r.helpScrollOffset > 0 {
		r.helpScrollOffset--
	}
}

// ScrollHelpDown scrolls the help panel down
func (r *Renderer) ScrollHelpDown() {
	// Estimate visible lines based on default panel size
	visibleLines := 20
	maxScroll := max(r.getTotalHelpLines()-visibleLines, 0)
	if r.helpScrollOffset < maxScroll {
		r.helpScrollOffset++
	}
}

// ResetHelpScroll resets the help scroll position
func (r *Renderer) ResetHelpScroll() {
	r.helpScrollOffset = 0
}

// renderHelpPanel renders the keybindings help overlay
func (r *Renderer) renderHelpPanel(width, height int, proj [16]float32) {
	// Panel dimensions - dynamically sized based on window
	// Use 80% of window size for the panel
	panelWidth := float32(width) * 0.80
	panelHeight := float32(height) * 0.85

	// Set reasonable min/max constraints
	if panelWidth < 350 {
		panelWidth = 350
	}
	if panelWidth > 700 {
		panelWidth = 700
	}
	if panelHeight < 250 {
		panelHeight = 250
	}
	if panelHeight > 800 {
		panelHeight = 800
	}

	// Center the panel in the window
	panelX := (float32(width) - panelWidth) / 2
	panelY := (float32(height) - panelHeight) / 2

	// Draw semi-transparent background overlay over entire window
	overlayColor := [4]float32{0.0, 0.0, 0.0, 0.75}
	r.drawRect(0, 0, float32(width), float32(height), overlayColor, proj)

	// Draw panel background
	panelBg := [4]float32{0.06, 0.07, 0.10, 1.0}
	r.drawRect(panelX, panelY, panelWidth, panelHeight, panelBg, proj)

	// Draw panel border
	borderColor := r.theme.TabActive
	borderWidth := float32(3)
	r.drawRect(panelX, panelY, panelWidth, borderWidth, borderColor, proj)
	r.drawRect(panelX, panelY+panelHeight-borderWidth, panelWidth, borderWidth, borderColor, proj)
	r.drawRect(panelX, panelY, borderWidth, panelHeight, borderColor, proj)
	r.drawRect(panelX+panelWidth-borderWidth, panelY, borderWidth, panelHeight, borderColor, proj)

	// Content positioning with dynamic margins
	marginX := panelWidth * 0.05
	if marginX < 20 {
		marginX = 20
	}
	contentX := panelX + marginX
	contentWidth := panelWidth - marginX*2 - 25 // Leave room for scrollbar

	lineHeight := r.cellHeight * 1.5
	headerY := panelY + 40
	contentStartY := headerY + lineHeight*2
	footerHeight := float32(50)
	contentEndY := panelY + panelHeight - footerHeight
	visibleHeight := contentEndY - contentStartY
	visibleLines := int(visibleHeight / lineHeight)

	// Calculate column positions - size the key column to the longest key
	// label so dual-chord labels ("Super+D / Ctrl+Shift+V") never overlap
	// their descriptions.
	longestKey := 0
	for _, section := range r.getHelpSections() {
		for _, binding := range section.bindings {
			if w := textCells(binding[0]); w > longestKey {
				longestKey = w
			}
		}
	}
	keyColWidth := r.cellWidth * float32(longestKey+3)
	descColX := contentX + keyColWidth

	// Title (fixed, doesn't scroll)
	r.drawText(contentX, headerY, "Keybindings Help", r.theme.TabActive, proj)

	// Draw a separator line under the title
	separatorY := headerY + lineHeight*0.8
	r.drawRect(contentX, separatorY, contentWidth, 1, r.theme.Foreground, proj)

	// Scroll indicators
	totalLines := r.getTotalHelpLines()
	maxScroll := max(totalLines-visibleLines, 0)

	// Draw scroll indicator on right side
	scrollBarX := panelX + panelWidth - 18
	scrollBarHeight := contentEndY - contentStartY
	scrollBarY := contentStartY

	if maxScroll > 0 {
		// Scroll track
		trackColor := [4]float32{0.12, 0.13, 0.18, 1.0}
		r.drawRect(scrollBarX, scrollBarY, 8, scrollBarHeight, trackColor, proj)

		// Scroll thumb - size proportional to visible content
		scrollThumbHeight := scrollBarHeight * float32(visibleLines) / float32(totalLines)
		if scrollThumbHeight < 30 {
			scrollThumbHeight = 30
		}
		scrollThumbY := scrollBarY + (scrollBarHeight-scrollThumbHeight)*float32(r.helpScrollOffset)/float32(maxScroll)

		r.drawRect(scrollBarX, scrollThumbY, 8, scrollThumbHeight, r.theme.TabActive, proj)
	}

	// Draw content with clipping
	sections := r.getHelpSections()
	currentLine := 0

	for _, section := range sections {
		// Section title
		if currentLine >= r.helpScrollOffset && currentLine < r.helpScrollOffset+visibleLines {
			drawY := contentStartY + float32(currentLine-r.helpScrollOffset)*lineHeight
			if drawY+lineHeight <= contentEndY {
				r.drawText(contentX, drawY, section.title, r.theme.TabActive, proj)
			}
		}
		currentLine++

		// Bindings
		for _, binding := range section.bindings {
			if currentLine >= r.helpScrollOffset && currentLine < r.helpScrollOffset+visibleLines {
				drawY := contentStartY + float32(currentLine-r.helpScrollOffset)*lineHeight
				if drawY+lineHeight <= contentEndY {
					r.drawText(contentX+15, drawY, binding[0], r.theme.Cursor, proj)
					r.drawText(descColX, drawY, binding[1], r.theme.Foreground, proj)
				}
			}
			currentLine++
		}

		// Spacing after section
		currentLine++
	}

	// Footer separator and text (fixed, doesn't scroll)
	// Position text first, then put separator above it
	footerY := panelY + panelHeight - 20
	footerText := "Up/Down: scroll | Esc: close"
	r.drawText(contentX, footerY, footerText, [4]float32{0.5, 0.5, 0.5, 1.0}, proj)

	// Separator line above the footer text
	footerSepY := footerY - r.cellHeight - 8
	r.drawRect(contentX, footerSepY, contentWidth, 1, r.theme.Foreground, proj)
}

// RenderWithMenu renders the terminal with optional menu overlay
func (r *Renderer) RenderWithMenu(tm *tab.TabManager, width, height int, cursorVisible bool, m *menu.Menu) {
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)

	// Clear background
	gl.ClearColor(r.theme.Background[0], r.theme.Background[1], r.theme.Background[2], r.theme.Background[3])
	gl.Clear(gl.COLOR_BUFFER_BIT)

	// Render tab bar
	r.renderTabBar(tm, width, height, proj)

	// Render terminal content with split pane support
	activeTab := tm.ActiveTab()
	if activeTab != nil {
		r.renderPanes(activeTab, width, height, proj, cursorVisible)
	}

	// Render menu overlay if open
	if m != nil && m.IsOpen() {
		r.renderMenu(m, width, height, proj)
	}
	r.uiFlush()
}

// renderMenu renders the settings menu overlay
func (r *Renderer) renderMenu(m *menu.Menu, width, height int, proj [16]float32) {
	// Fixed panel dimensions - use percentage of window but with sensible limits
	panelWidth := float32(width) * 0.75
	panelHeight := float32(height) * 0.80

	// Minimum size to fit content, but never larger than the window minus
	// chrome so the panel never overflows/overlays its own padding at small
	// window sizes.
	minWidth := float32(450)
	minHeight := float32(350)
	if w := float32(width) - 20; minWidth > w {
		minWidth = w
	}
	if h := float32(height) - 20; minHeight > h {
		minHeight = h
	}
	if minWidth < 1 {
		minWidth = 1
	}
	if minHeight < 1 {
		minHeight = 1
	}
	if panelWidth < minWidth {
		panelWidth = minWidth
	}
	if panelHeight < minHeight {
		panelHeight = minHeight
	}

	// Don't exceed window size
	if panelWidth > float32(width)-20 {
		panelWidth = float32(width) - 20
	}
	if panelHeight > float32(height)-20 {
		panelHeight = float32(height) - 20
	}

	// Center the panel
	panelX := (float32(width) - panelWidth) / 2
	panelY := (float32(height) - panelHeight) / 2

	// Draw semi-transparent overlay
	overlayColor := [4]float32{0.0, 0.0, 0.0, 0.8}
	r.drawRect(0, 0, float32(width), float32(height), overlayColor, proj)

	// Draw panel background
	panelBg := [4]float32{0.06, 0.07, 0.10, 1.0}
	r.drawRect(panelX, panelY, panelWidth, panelHeight, panelBg, proj)

	// Draw panel border
	borderColor := r.theme.TabActive
	borderThickness := float32(2)
	r.drawRect(panelX, panelY, panelWidth, borderThickness, borderColor, proj)
	r.drawRect(panelX, panelY+panelHeight-borderThickness, panelWidth, borderThickness, borderColor, proj)
	r.drawRect(panelX, panelY, borderThickness, panelHeight, borderColor, proj)
	r.drawRect(panelX+panelWidth-borderThickness, panelY, borderThickness, panelHeight, borderColor, proj)

	// Match the side-panel title inset so switching between Settings and the
	// Web/AI panels does not make the header text jump horizontally.
	marginX := float32(18)
	contentX := panelX + marginX
	contentWidth := panelWidth - marginX*2

	lineHeight := r.cellHeight * 1.5
	// drawText takes the bottom of a cell box, not its top.  A fixed pixel
	// offset here used to put the title almost against the upper border when
	// the font (or framebuffer scale) grew.  Use the same font-relative header
	// baseline as the Web Search and AI Chat panels instead.
	panelHeaderLineHeight := r.cellHeight * 1.35
	headerY := panelY + panelHeaderLineHeight*1.2
	separatorY := headerY + lineHeight*0.5

	// Calculate footer area height
	inputIsMultiline := m.InputMode() && m.InputIsMultiline()
	inputLines := 1
	if inputIsMultiline {
		inputLines = 6
	}
	footerHeight := float32(60)
	if m.InputMode() {
		footerHeight = lineHeight*float32(inputLines+2) + 40
	}
	if m.StatusMessage != "" {
		footerHeight += lineHeight
	}

	// Menu items area
	contentStartY := separatorY + lineHeight*0.8
	contentEndY := panelY + panelHeight - footerHeight
	visibleHeight := contentEndY - contentStartY
	visibleItems := max(int(visibleHeight/lineHeight), 1)

	// Report the real viewport size so the menu scrolls to keep the selection visible
	// at any window size (fullscreen or windowed). This also re-clamps the scroll offset.
	m.SetVisibleCount(visibleItems)

	totalItems := len(m.Items)
	maxScroll := max(totalItems-visibleItems, 0)

	scrollBarWidth := float32(8)
	scrollBarPadding := float32(8)
	if maxScroll > 0 {
		contentWidth -= scrollBarWidth + scrollBarPadding
	}

	// Calculate max characters that fit in content width (for truncation)
	maxChars := max(
		// -3 for "> " prefix
		int(contentWidth/r.cellWidth)-3, 10)

	// Title
	r.drawText(contentX, headerY, m.GetTitle(), r.theme.TabActive, proj)

	// Separator under title
	r.drawRect(contentX, separatorY, contentWidth, 1, r.theme.Foreground, proj)

	// Draw menu items
	itemIndex := 0
	headerColor := [4]float32{0.5, 0.5, 0.6, 1.0}    // Dim color for headers
	toggleOnColor := [4]float32{0.3, 0.8, 0.4, 1.0}  // Green for enabled toggles
	toggleOffColor := [4]float32{0.5, 0.5, 0.5, 1.0} // Gray for disabled toggles

	for i, item := range m.Items {
		if i < m.ScrollOffset {
			continue
		}
		if itemIndex >= visibleItems {
			break
		}

		y := contentStartY + float32(itemIndex)*lineHeight

		// Empty items are separators - still count them for spacing
		if item.Label == "" {
			itemIndex++
			continue
		}

		// Section headers - styled differently, not selectable
		if item.IsHeader {
			r.drawText(contentX+5, y, item.Label, headerColor, proj)
			itemIndex++
			continue
		}

		// Build display label with toggle indicator if needed
		label := item.Label
		if item.IsToggle {
			if item.Toggled {
				label = "[x] " + label
			} else {
				label = "[ ] " + label
			}
		}

		// Truncate label to fit
		if len(label) > maxChars {
			label = label[:maxChars-3] + "..."
		}

		// Highlight selected item
		if i == m.SelectedIndex {
			highlightColor := [4]float32{0.15, 0.17, 0.25, 1.0}
			r.drawRect(contentX, y-lineHeight+8, contentWidth, lineHeight, highlightColor, proj)
			r.drawText(contentX+5, y, ">", r.theme.TabActive, proj)
			if item.IsToggle {
				// Color the checkbox based on state
				checkColor := toggleOffColor
				if item.Toggled {
					checkColor = toggleOnColor
				}
				checkboxEnd := r.cellWidth*4 + 5
				r.drawText(contentX+r.cellWidth*2+5, y, label[:4], checkColor, proj)
				r.drawText(contentX+r.cellWidth*2+5+checkboxEnd, y, label[4:], r.theme.TabActive, proj)
			} else {
				r.drawText(contentX+r.cellWidth*2+5, y, label, r.theme.TabActive, proj)
			}
		} else {
			if item.IsToggle {
				// Color the checkbox based on state
				checkColor := toggleOffColor
				if item.Toggled {
					checkColor = toggleOnColor
				}
				checkboxEnd := r.cellWidth*4 + 5
				r.drawText(contentX+r.cellWidth*2+5, y, label[:4], checkColor, proj)
				r.drawText(contentX+r.cellWidth*2+5+checkboxEnd, y, label[4:], r.theme.Foreground, proj)
			} else {
				r.drawText(contentX+r.cellWidth*2+5, y, label, r.theme.Foreground, proj)
			}
		}
		itemIndex++
	}

	// Footer area - positioned from bottom
	footerTextY := panelY + panelHeight - 20
	footerSepY := footerTextY - lineHeight

	// Input mode - draw input box
	if m.InputMode() {
		inputText := m.GetInputBuffer()
		prompt := m.GetInputPrompt()
		if len(prompt) > maxChars {
			prompt = prompt[:maxChars-3] + "..."
		}
		caretLine, caretCol := m.InputCursorLineCol()
		maxInputChars := maxChars - 2
		textX := contentX + 8

		if inputIsMultiline {
			textAreaHeight := lineHeight * float32(inputLines)
			inputAreaY := footerSepY - textAreaHeight - lineHeight*0.8

			// Input prompt
			r.drawText(contentX+5, inputAreaY, prompt, r.theme.Foreground, proj)

			// Text area background
			textBoxY := inputAreaY + lineHeight*0.3
			r.drawRect(contentX, textBoxY, contentWidth, textAreaHeight, [4]float32{0.03, 0.03, 0.05, 1.0}, proj)

			lines := strings.Split(inputText, "\n")
			// Scroll the window of lines onto the caret rather than pinning it
			// to the end, so editing higher up in a script stays visible.
			start := max(caretLine-inputLines+1, 0)
			end := min(start+inputLines, len(lines))

			lineY := textBoxY + lineHeight*0.75
			for i, line := range lines[start:end] {
				// Only the caret's line scrolls horizontally; the rest are cut
				// at the field width.
				col := 0
				if start+i == caretLine {
					col = caretCol
				}
				vis, visCol := inputWindow([]rune(line), col, maxInputChars)
				r.drawText(textX, lineY, string(vis), r.theme.TabActive, proj)
				if start+i == caretLine {
					r.drawInputCaret(textX, lineY, visCol, proj)
				}
				lineY += lineHeight
			}
		} else {
			inputAreaY := footerSepY - lineHeight*2

			// Input prompt
			r.drawText(contentX+5, inputAreaY, prompt, r.theme.Foreground, proj)

			// Input box background
			inputBoxY := inputAreaY + lineHeight*0.3
			r.drawRect(contentX, inputBoxY, contentWidth, lineHeight, [4]float32{0.03, 0.03, 0.05, 1.0}, proj)

			baselineY := inputBoxY + lineHeight*0.75
			vis, visCol := inputWindow([]rune(inputText), caretCol, maxInputChars)
			r.drawText(textX, baselineY, string(vis), r.theme.TabActive, proj)
			r.drawInputCaret(textX, baselineY, visCol, proj)
		}
	}

	// Status message
	if m.StatusMessage != "" {
		statusY := footerSepY - lineHeight*0.3
		status := m.StatusMessage
		if len(status) > maxChars {
			status = status[:maxChars-3] + "..."
		}
		r.drawText(contentX, statusY, status, r.theme.Cursor, proj)
		footerSepY = statusY - lineHeight*0.5
	}

	// Footer separator
	r.drawRect(contentX, footerSepY, contentWidth, 1, [4]float32{0.3, 0.3, 0.4, 1.0}, proj)

	// Footer help text - truncate if needed
	var footerText string
	if m.InputMode() {
		if inputIsMultiline {
			footerText = "Arrows: move | Home/End | Del | Enter: newline | Ctrl+Enter: save | Esc: cancel"
		} else {
			footerText = "Arrows: move | Home/End | Del | Enter: confirm | Esc: cancel"
		}
	} else {
		footerText = "Up/Down | Enter | Del | Esc"
	}
	r.drawText(contentX, footerTextY, footerText, [4]float32{0.5, 0.5, 0.5, 1.0}, proj)

	if maxScroll > 0 {
		scrollBarX := contentX + contentWidth + scrollBarPadding
		scrollBarHeight := contentEndY - contentStartY
		scrollBarY := contentStartY

		trackColor := [4]float32{0.12, 0.13, 0.18, 1.0}
		r.drawRect(scrollBarX, scrollBarY, scrollBarWidth, scrollBarHeight, trackColor, proj)

		scrollThumbHeight := scrollBarHeight * float32(visibleItems) / float32(totalItems)
		if scrollThumbHeight < 24 {
			scrollThumbHeight = 24
		}
		if scrollThumbHeight > scrollBarHeight {
			scrollThumbHeight = scrollBarHeight
		}
		scrollThumbY := scrollBarY
		if maxScroll > 0 {
			scrollThumbY = scrollBarY + (scrollBarHeight-scrollThumbHeight)*float32(m.ScrollOffset)/float32(maxScroll)
		}
		r.drawRect(scrollBarX, scrollThumbY, scrollBarWidth, scrollThumbHeight, r.theme.TabActive, proj)
	}
}

// inputWindow returns the slice of line that fits in a width-cell field with
// the caret at column col kept on screen, plus the caret's column within that
// slice. The window is derived from the caret alone (no scroll state to keep
// in sync): once the caret passes the right edge it rides there, and moving
// back left pulls the earlier text into view again.
func inputWindow(line []rune, col, width int) ([]rune, int) {
	width = max(width, 1)
	off := max(col-width+1, 0)
	return line[off:min(off+width, len(line))], col - off
}

// drawInputCaret draws the settings-input caret as a bar between characters, so
// it reads as an insertion point rather than a character of the value.
func (r *Renderer) drawInputCaret(textX, baselineY float32, col int, proj [16]float32) {
	x := textX + float32(col)*r.cellWidth
	r.drawRect(x, baselineY-r.cellHeight*0.8, 2, r.cellHeight, r.theme.Cursor, proj)
}

// renderPanes renders all panes in a tab using the nested layout system
func (r *Renderer) renderPanes(t *tab.Tab, width, height int, proj [16]float32, cursorVisible bool) {
	layouts := t.GetPaneLayouts()
	if len(layouts) == 0 {
		return
	}

	// Calculate available area (after tab bar) — shared with CalculateGridSize
	// and paneRects via layoutMargins so cells tile exactly into the pane rect.
	baseX, baseY, availableWidth, availableHeight := r.layoutMargins(width, height)

	// Get active pane for highlighting
	activePane := t.GetActivePane()
	separatorWidth := float32(2)

	// First pass: draw separators between panes
	if len(layouts) > 1 {
		r.drawPaneSeparators(layouts, baseX, baseY, availableWidth, availableHeight, separatorWidth, proj)
	}

	// Second pass: render each pane
	for _, layout := range layouts {
		// Convert normalized coordinates to screen coordinates
		offsetX := baseX + layout.X*availableWidth
		offsetY := baseY + layout.Y*availableHeight
		paneWidth := layout.Width * availableWidth
		paneHeight := layout.Height * availableHeight

		// Adjust for separators (small inset to avoid overlap)
		if len(layouts) > 1 {
			if layout.X > 0 {
				offsetX += separatorWidth / 2
				paneWidth -= separatorWidth / 2
			}
			if layout.X+layout.Width < 1.0 {
				paneWidth -= separatorWidth / 2
			}
			if layout.Y > 0 {
				offsetY += separatorWidth / 2
				paneHeight -= separatorWidth / 2
			}
			if layout.Y+layout.Height < 1.0 {
				paneHeight -= separatorWidth / 2
			}
		}

		// Draw active pane indicator (subtle border)
		isActive := layout.Pane == activePane
		if isActive && len(layouts) > 1 {
			borderColor := r.theme.TabActive
			borderWidth := float32(2)
			// Top border
			r.drawRect(offsetX, offsetY, paneWidth, borderWidth, borderColor, proj)
			// Bottom border
			r.drawRect(offsetX, offsetY+paneHeight-borderWidth, paneWidth, borderWidth, borderColor, proj)
			// Left border
			r.drawRect(offsetX, offsetY, borderWidth, paneHeight, borderColor, proj)
			// Right border
			r.drawRect(offsetX+paneWidth-borderWidth, offsetY, borderWidth, paneHeight, borderColor, proj)
		}

		// Render the pane's grid
		showCursor := cursorVisible && isActive
		cursorStyle := parser.CursorStyleBlock
		term := layout.Pane.Terminal
		if showCursor && term != nil {
			// CursorStyle takes Terminal.mu; skip the lock when no cursor
			// will be drawn this frame.
			cursorStyle = term.CursorStyle()
		}
		// One locked snapshot per pane per frame. The grid pointer is used
		// only for hover-URL identity matching, so GetGrid (another
		// Terminal.mu acquisition) is only needed while a hover is active.
		var g *grid.Grid
		if r.hoverActive {
			g = term.GetGrid()
		}
		snap := term.Snapshot()
		r.renderGridAt(snap, g, offsetX, offsetY, paneWidth, paneHeight, proj, showCursor, cursorStyle)
	}
}

func (r *Renderer) paneRects(t *tab.Tab, width, height int) []paneRect {
	if t == nil {
		return nil
	}
	layouts := t.GetPaneLayouts()
	if len(layouts) == 0 {
		return nil
	}

	baseX, baseY, availableWidth, availableHeight := r.layoutMargins(width, height)
	separatorWidth := float32(2)

	rects := make([]paneRect, 0, len(layouts))
	for _, layout := range layouts {
		offsetX := baseX + layout.X*availableWidth
		offsetY := baseY + layout.Y*availableHeight
		paneWidth := layout.Width * availableWidth
		paneHeight := layout.Height * availableHeight

		if len(layouts) > 1 {
			if layout.X > 0 {
				offsetX += separatorWidth / 2
				paneWidth -= separatorWidth / 2
			}
			if layout.X+layout.Width < 1.0 {
				paneWidth -= separatorWidth / 2
			}
			if layout.Y > 0 {
				offsetY += separatorWidth / 2
				paneHeight -= separatorWidth / 2
			}
			if layout.Y+layout.Height < 1.0 {
				paneHeight -= separatorWidth / 2
			}
		}

		rects = append(rects, paneRect{
			pane:   layout.Pane,
			x:      offsetX,
			y:      offsetY,
			width:  paneWidth,
			height: paneHeight,
		})
	}

	return rects
}

// HitTestPane returns the pane and cell position for a screen coordinate.
func (r *Renderer) HitTestPane(t *tab.Tab, x, y float64, width, height int) (*tab.Pane, int, int, bool) {
	// x,y arrive in logical (cursor callback) coordinates; pane rects are in
	// framebuffer pixels, so scale the point up on HiDPI displays.
	s := r.hidpiScale()
	fx := float32(x) * s
	fy := float32(y) * s
	for _, rect := range r.paneRects(t, width, height) {
		if fx < rect.x || fx >= rect.x+rect.width || fy < rect.y || fy >= rect.y+rect.height {
			continue
		}
		g := rect.pane.Terminal.GetGrid()
		col := int((fx - rect.x) / r.cellWidth)
		row := int((fy - rect.y) / r.cellHeight)
		col = clampInt(col, 0, g.Cols-1)
		row = clampInt(row, 0, g.Rows-1)
		return rect.pane, col, row, true
	}
	return nil, 0, 0, false
}

// PaneRectFor returns the rect for a specific pane in LOGICAL (cursor
// callback) coordinates, for hit-testing against GLFW cursor positions. width/
// height are the framebuffer size; internally rects are framebuffer pixels and
// are divided by the content scale so callers can compare them directly with
// GetCursorPos values on HiDPI displays.
func (r *Renderer) PaneRectFor(t *tab.Tab, pane *tab.Pane, width, height int) (float32, float32, float32, float32, bool) {
	if pane == nil {
		return 0, 0, 0, 0, false
	}
	s := r.hidpiScale()
	for _, rect := range r.paneRects(t, width, height) {
		if rect.pane == pane {
			return rect.x / s, rect.y / s, rect.width / s, rect.height / s, true
		}
	}
	return 0, 0, 0, 0, false
}

// CellSize returns the cell dimensions in LOGICAL (cursor callback)
// coordinates, matching PaneRectFor, for pixel->cell hit-testing math.
func (r *Renderer) CellSize() (float32, float32) {
	s := r.hidpiScale()
	return r.cellWidth / s, r.cellHeight / s
}

// drawPaneSeparators draws separator lines between panes
func (r *Renderer) drawPaneSeparators(layouts []tab.PaneLayout, baseX, baseY, availableWidth, availableHeight, separatorWidth float32, proj [16]float32) {
	// Track edges where separators should be drawn
	type edge struct {
		x1, y1, x2, y2 float32
		vertical       bool
	}
	var edges []edge

	// Find edges between panes
	for i, layout1 := range layouts {
		for j, layout2 := range layouts {
			if i >= j {
				continue
			}

			// Check for vertical separator (layout1 to the left of layout2)
			if almostEqual(layout1.X+layout1.Width, layout2.X) {
				// They share a vertical edge
				overlapY1 := max32(layout1.Y, layout2.Y)
				overlapY2 := min32(layout1.Y+layout1.Height, layout2.Y+layout2.Height)
				if overlapY1 < overlapY2 {
					edges = append(edges, edge{
						x1:       layout1.X + layout1.Width,
						y1:       overlapY1,
						x2:       layout1.X + layout1.Width,
						y2:       overlapY2,
						vertical: true,
					})
				}
			}

			// Check for horizontal separator (layout1 above layout2)
			if almostEqual(layout1.Y+layout1.Height, layout2.Y) {
				// They share a horizontal edge
				overlapX1 := max32(layout1.X, layout2.X)
				overlapX2 := min32(layout1.X+layout1.Width, layout2.X+layout2.Width)
				if overlapX1 < overlapX2 {
					edges = append(edges, edge{
						x1:       overlapX1,
						y1:       layout1.Y + layout1.Height,
						x2:       overlapX2,
						y2:       layout1.Y + layout1.Height,
						vertical: false,
					})
				}
			}
		}
	}

	// Draw the separator lines
	for _, e := range edges {
		if e.vertical {
			x := baseX + e.x1*availableWidth - separatorWidth/2
			y := baseY + e.y1*availableHeight
			h := (e.y2 - e.y1) * availableHeight
			r.drawRect(x, y, separatorWidth, h, r.theme.Foreground, proj)
		} else {
			x := baseX + e.x1*availableWidth
			y := baseY + e.y1*availableHeight - separatorWidth/2
			w := (e.x2 - e.x1) * availableWidth
			r.drawRect(x, y, w, separatorWidth, r.theme.Foreground, proj)
		}
	}
}

// almostEqual checks if two floats are nearly equal
func almostEqual(a, b float32) bool {
	const epsilon = 0.001
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}

// max32 returns the larger of two float32 values
func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// min32 returns the smaller of two float32 values
func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// nextPowerOf2 returns the smallest power of 2 >= n
func nextPowerOf2(n int) int {
	if n <= 0 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	return n + 1
}

// tabBarGeom holds the shared geometry for the vertical tab bar so rendering and
// hit-testing stay in sync.
type tabBarGeom struct {
	boxX, boxW float32
	topPad     float32
	boxH       float32
	gap        float32
	plusH      float32
	scale      float32
	cellH      float32
}

// tabBarGeom computes the chip geometry for count tabs. Chips are compressed
// (and their text scaled down with them) so that every tab plus the "+" button
// always fits the bar height — that is what lets MaxTabs exceed what a
// fixed-height chip stack could show. barViewH is the height of the last
// rendered frame; it is zero before the first frame, which skips compression.
func (r *Renderer) tabBarGeom(count int) tabBarGeom {
	scale := r.baseFontSize / r.fontSize
	cellH := r.cellHeight * scale
	const sidePad = 10.0
	g := tabBarGeom{
		boxX:   sidePad,
		boxW:   r.tabBarWidth - 2*sidePad,
		topPad: 12,
		boxH:   cellH + 14,
		// Gutter between chips. Wide enough that a chip sliding into an
		// adjacent slot reads as movement rather than a jump; compressed
		// proportionally with the chips when many tabs are open.
		gap:   12,
		scale: scale,
		cellH: cellH,
	}
	g.plusH = g.boxH * 0.8

	avail := r.barViewH - 2*g.topPad
	need := float32(count)*(g.boxH+g.gap) + g.plusH
	if count > 0 && avail > 0 && need > avail {
		k := avail / need
		g.boxH *= k
		g.gap *= k
		g.plusH *= k
		// Keep the label inside its chip: shrink the text with the chip once
		// the chip is no longer taller than a line of text.
		if fit := g.boxH - 4; fit < g.cellH {
			g.scale *= fit / g.cellH
			g.cellH = fit
		}
	}
	return g
}

// SetTabDrag marks the tab currently being drag-reordered (nil when no drag is
// in progress). The dragged chip snaps to its slot so it stays under the
// cursor; every other chip eases, which is what makes the swap visible.
func (r *Renderer) SetTabDrag(t *tab.Tab) {
	r.dragTab = t
}

// tabSlideTau is the time constant of the chip slide. ~70ms reads as a
// deliberate swap without feeling sluggish when reordering several tabs
// quickly.
const tabSlideTau = 0.07

// animateTabSlots advances each chip's animated slot position toward its real
// index and returns the positions to draw at, parallel to tabs. Movement is an
// exponential ease evaluated against wall-clock delta, so it is frame-rate
// independent. While any chip is still in motion it latches uiDirty, which is
// what schedules the next frame — the main loop is event-driven and would
// otherwise stop drawing mid-slide.
func (r *Renderer) animateTabSlots(tabs []*tab.Tab) []float32 {
	now := time.Now()
	dt := now.Sub(r.tabSlotAt).Seconds()
	r.tabSlotAt = now
	// A long gap means the bar has not drawn for a while (idle terminal, or
	// first frame): settle immediately rather than replaying a stale slide.
	if dt <= 0 || dt > 0.25 {
		dt = 0
	}
	k := float32(1 - math.Exp(-dt/tabSlideTau))

	next := make(map[*tab.Tab]float32, len(tabs))
	slots := make([]float32, len(tabs))
	for i, t := range tabs {
		target := float32(i)
		cur, seen := r.tabSlot[t]
		switch {
		case !seen || t == r.dragTab:
			// A tab drawn for the first time has no previous position to slide
			// from, and the dragged chip must track the pointer.
			cur = target
		default:
			cur += (target - cur) * k
			if d := target - cur; d > -0.01 && d < 0.01 {
				cur = target
			} else {
				r.uiDirty = true // still moving: ask for another frame
			}
		}
		next[t] = cur
		slots[i] = cur
	}
	// Rebuilding the map drops entries for closed tabs.
	r.tabSlot = next
	return slots
}

// renderTabBar renders the left tab bar as a vertical stack of tab "chips": each shows
// the working-directory label and a ⌘N shortcut badge, with a rounded pill highlight on
// the active tab and a "+" button at the bottom to open a new tab. It is only shown when
// there is more than one tab (see SetTabBarVisible), so a single tab uses full width.
func (r *Renderer) renderTabBar(tm *tab.TabManager, width, height int, proj [16]float32) {
	if !r.tabBarVisible {
		return
	}

	barW := r.tabBarWidth
	r.barViewH = float32(height)

	// Tab bar background + a hairline right edge.
	r.drawRect(0, 0, barW, float32(height), r.theme.TabBar, proj)
	r.drawRect(barW-1, 0, 1, float32(height), withAlpha(r.theme.Foreground, 0.12), proj)

	tabs := tm.GetTabs()
	g := r.tabBarGeom(len(tabs))
	activeIdx := tm.ActiveIndex()
	isMac := runtime.GOOS == "darwin"
	home := cachedHomeDir
	charW := r.cellWidth * g.scale
	slots := r.animateTabSlots(tabs)

	for i, t := range tabs {
		boxY := g.topPad + slots[i]*(g.boxH+g.gap)
		active := i == activeIdx
		baseline := boxY + g.boxH*0.5 + g.cellH*0.30

		if active {
			// Rounded pill highlight for the active tab.
			r.drawRoundedRect(g.boxX, boxY, g.boxW, g.boxH, 9, lighten(r.theme.TabBar, 0.11), proj)
			// Accent strip on the leading edge.
			r.drawRoundedRect(g.boxX, boxY+3, 3, g.boxH-6, 1.5, r.theme.TabActive, proj)
		} else if i > 0 {
			// Hairline separator between inactive tabs.
			r.drawRect(g.boxX+10, boxY-g.gap*0.5, g.boxW-20, 1, withAlpha(r.theme.Foreground, 0.10), proj)
		}

		// ⌘N badge, right-aligned — only tabs 1..9 have a Cmd/primary shortcut.
		labelRight := g.boxX + g.boxW - 12
		if t.ID() <= 9 {
			badge := fmt.Sprintf("%d", t.ID())
			if isMac {
				badge = "⌘" + badge
			}
			badgeW := float32(len([]rune(badge))) * charW
			badgeX := g.boxX + g.boxW - badgeW - 12
			badgeClr := withAlpha(r.theme.Foreground, 0.45)
			if active {
				badgeClr = withAlpha(r.theme.TabActive, 0.9)
			}
			r.drawTextScaled(badgeX, baseline, badge, badgeClr, proj, g.scale)
			labelRight = badgeX - 6
		}

		// Directory label, truncated so it never collides with the badge.
		label := tabLabel(t, home)
		textClr := r.theme.Foreground
		if !active {
			textClr = withAlpha(textClr, 0.6)
		}
		maxLabelW := labelRight - (g.boxX + 14)
		label = truncateToWidth(label, maxLabelW, charW)
		r.drawTextScaled(g.boxX+14, baseline, label, textClr, proj, g.scale)
	}

	// "+" new-tab button below the last tab.
	plusY := g.topPad + float32(len(tabs))*(g.boxH+g.gap)
	r.drawRoundedRect(g.boxX, plusY, g.boxW, g.plusH, 9, lighten(r.theme.TabBar, 0.05), proj)
	plusBaseline := plusY + g.plusH*0.5 + g.cellH*0.30
	r.drawTextScaled(g.boxX+g.boxW*0.5-charW*0.5, plusBaseline, "+", withAlpha(r.theme.Foreground, 0.55), proj, g.scale)
}

// HitTestTabBar maps a click in the tab bar to a tab index, or the "+" button. ok is
// false when the bar is hidden or the point is outside it.
func (r *Renderer) HitTestTabBar(tm *tab.TabManager, x, y float64) (index int, newTab bool, ok bool) {
	if !r.tabBarVisible {
		return 0, false, false
	}
	// x,y are logical cursor coordinates; tab bar geometry is framebuffer px.
	s := r.hidpiScale()
	fx, fy := float32(x)*s, float32(y)*s
	if fx < 0 || fx > r.tabBarWidth {
		return 0, false, false
	}
	n := tm.TabCount()
	g := r.tabBarGeom(n)
	for i := range n {
		boxY := g.topPad + float32(i)*(g.boxH+g.gap)
		if fx >= g.boxX && fx <= g.boxX+g.boxW && fy >= boxY && fy <= boxY+g.boxH {
			return i, false, true
		}
	}
	plusY := g.topPad + float32(n)*(g.boxH+g.gap)
	if fx >= g.boxX && fx <= g.boxX+g.boxW && fy >= plusY && fy <= plusY+g.plusH {
		return 0, true, true
	}
	return 0, false, false
}

// tabDropSlot maps a framebuffer Y coordinate to the nearest tab-chip center.
// The half-gap between adjacent chips is therefore the swap boundary.
func tabDropSlot(y float32, count int, g tabBarGeom) int {
	if count <= 0 {
		return 0
	}
	pitch := g.boxH + g.gap
	firstCenter := g.topPad + g.boxH*0.5
	slot := int(math.Floor(float64((y-firstCenter)/pitch) + 0.5))
	if slot < 0 {
		return 0
	}
	if slot >= count {
		return count - 1
	}
	return slot
}

// TabDropIndex maps a logical cursor Y to the nearest tab slot, clamped to
// [0, TabCount-1]. Used while drag-reordering tabs; returns 0 when the bar is
// hidden or empty.
func (r *Renderer) TabDropIndex(tm *tab.TabManager, y float64) int {
	n := tm.TabCount()
	if !r.tabBarVisible || n == 0 {
		return 0
	}
	fy := float32(y) * r.hidpiScale()
	return tabDropSlot(fy, n, r.tabBarGeom(n))
}

// tabLabel returns a compact directory-based label for a tab (e.g. "~/D/RavenTerminal").
func tabLabel(t *tab.Tab, home string) string {
	dir := t.ActiveDir()
	if dir == "" {
		return fmt.Sprintf("Tab %d", t.ID())
	}
	return abbreviatePath(dir, home)
}

// abbreviatePath shortens a path for display: the home prefix becomes "~", and every
// intermediate component is collapsed to its first character, keeping the last full
// (e.g. "/Users/me/Development/RavenTerminal" -> "~/D/RavenTerminal").
func abbreviatePath(p, home string) string {
	if home != "" {
		if p == home {
			return "~"
		}
		if strings.HasPrefix(p, home+"/") {
			p = "~" + p[len(home):]
		}
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		if i == 0 || i == len(parts)-1 {
			continue
		}
		if len(parts[i]) > 1 {
			parts[i] = parts[i][:1]
		}
	}
	return strings.Join(parts, "/")
}

// truncateToWidth shortens s with an ellipsis so it fits within maxW pixels.
func truncateToWidth(s string, maxW, charW float32) string {
	if maxW <= 0 || charW <= 0 {
		return ""
	}
	runes := []rune(s)
	maxChars := int(maxW / charW)
	if maxChars <= 0 {
		return ""
	}
	if len(runes) <= maxChars {
		return s
	}
	if maxChars == 1 {
		return "…"
	}
	return string(runes[:maxChars-1]) + "…"
}

// drawRoundedRect draws a filled rectangle with rounded corners of the given radius,
// reusing drawRect for the body and per-row strips for the rounded caps.
func (r *Renderer) drawRoundedRect(x, y, w, h, radius float32, clr [4]float32, proj [16]float32) {
	if radius <= 0 {
		r.drawRect(x, y, w, h, clr, proj)
		return
	}
	if radius > w/2 {
		radius = w / 2
	}
	if radius > h/2 {
		radius = h / 2
	}
	// Body between the rounded caps.
	r.drawRect(x, y+radius, w, h-2*radius, clr, proj)
	// Rounded top/bottom caps: 1px strips inset along a circular arc.
	steps := int(radius)
	for i := range steps {
		dy := radius - float32(i) - 0.5
		dx := radius - float32(math.Sqrt(float64(radius*radius-dy*dy)))
		stripX := x + dx
		stripW := w - 2*dx
		r.drawRect(stripX, y+float32(i), stripW, 1, clr, proj)     // top cap
		r.drawRect(stripX, y+h-float32(i)-1, stripW, 1, clr, proj) // bottom cap
	}
}

// withAlpha returns a copy of a color with its alpha replaced.
func withAlpha(c [4]float32, a float32) [4]float32 {
	c[3] = a
	return c
}

// lighten returns a copy of a color with its RGB channels raised by amt (clamped).
func lighten(c [4]float32, amt float32) [4]float32 {
	c[0] = clampUnit(c[0] + amt)
	c[1] = clampUnit(c[1] + amt)
	c[2] = clampUnit(c[2] + amt)
	return c
}

// clampUnit clamps a color channel to the [0,1] range.
func clampUnit(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// renderGridAt draws a terminal grid from a pre-resolved Snapshot (a single
// locked copy of the visible region), so the per-cell loop is lock-free. The
// grid pointer g is used only for hover-URL identity matching.
func (r *Renderer) renderGridAt(snap *grid.Snapshot, g *grid.Grid, offsetX, offsetY, paneWidth, paneHeight float32, proj [16]float32, cursorVisible bool, cursorStyle parser.CursorStyle) {
	cols := snap.Cols

	// Hover underline applies only when the hover target is this grid.
	hoverRow := -1
	if r.hoverActive && r.hoverGrid == g {
		hoverRow = r.hoverRow
	}

	// Pass 1: accumulate all backgrounds/selection and all glyphs into two
	// batches, then flush each in a single draw call. Glyphs are rasterized on
	// demand inside the loop; if that grows the atlas mid-pass (growing
	// re-packs every glyph, invalidating UVs already recorded in the batch),
	// the pass is re-run once — by then every needed glyph is cached, so the
	// rerun records consistent UVs. This replaces the old always-on full-grid
	// glyph warm loop with a rare retry.
	for attempt := 0; ; attempt++ {
		startGen := r.atlasGen
		r.buildGridBatches(snap, offsetX, offsetY, paneWidth, paneHeight, hoverRow)
		if r.atlasGen == startGen || attempt > 0 {
			break
		}
	}
	r.uiFlush() // anything batched earlier (tab bar, chrome) lands under grid content
	r.flushRects(&r.gridRects, proj)
	r.flushGlyphs(&r.gridGlyphs, proj)

	// Color emoji: drawn after the monochrome batch (each is its own RGBA
	// texture, so they can't share the alpha-atlas draw call).
	for i := range r.colorDraws {
		cd := &r.colorDraws[i]
		r.drawColorGlyph(cd.x, cd.yTop, cd.span, cd.cg, cd.alpha, proj)
	}

	// Pass 2: immediate draws for the minority cases — block elements,
	// underlines, strikethrough (drawn over the batched glyphs) — visiting
	// only the cells pass 1 marked instead of re-scanning the whole grid.
	// Items are never hidden cells (filtered in pass 1).
	for i := range r.pass2 {
		it := &r.pass2[i]
		cell := snap.Cells[it.row*cols+it.col]
		if isBlockElement(cell.Char) {
			r.drawBlockElement(it.x, it.y, cell.Char, it.fg, proj)
		}
		if cell.Flags&grid.FlagUnderline != 0 || it.hovered {
			ulColor := it.fg
			style := uint8(1)
			if cell.Flags&grid.FlagUnderline != 0 && !it.hovered {
				if cell.UnderlineColor.Type != grid.ColorDefault {
					ulColor = r.colorToRGBA(cell.UnderlineColor, false)
				}
				if cell.UnderlineStyle != 0 {
					style = cell.UnderlineStyle
				}
				if !r.undercurl {
					style = 1
				}
			}
			r.drawUnderlineStyled(it.x, it.y, ulColor, proj, style)
		}
		if cell.Flags&grid.FlagStrikethrough != 0 {
			r.drawRect(it.x, it.y+r.cellHeight/2, r.cellWidth, 1, it.fg, proj)
		}
	}

	// Draw cursor
	if cursorVisible && snap.ScrollOffset == 0 {
		cursorCol, cursorRow := snap.CursorCol, snap.CursorRow
		cursorX := offsetX + float32(cursorCol)*r.cellWidth
		cursorY := offsetY + float32(cursorRow)*r.cellHeight

		// Only draw cursor if within pane bounds
		if cursorX+r.cellWidth <= offsetX+paneWidth && cursorY+r.cellHeight <= offsetY+paneHeight {
			cell := snap.At(cursorCol, cursorRow)
			switch cursorStyle {
			case parser.CursorStyleUnderline:
				h := r.cellHeight / 6
				if h < 1 {
					h = 1
				}
				r.drawRect(cursorX, cursorY+r.cellHeight-h, r.cellWidth, h, r.theme.Cursor, proj)
			case parser.CursorStyleBar:
				w := r.cellWidth / 6
				if w < 1 {
					w = 1
				}
				r.drawRect(cursorX, cursorY, w, r.cellHeight, r.theme.Cursor, proj)
			default:
				r.drawRect(cursorX, cursorY, r.cellWidth, r.cellHeight, r.theme.Cursor, proj)
				// Redraw character under cursor in inverse
				if cell.Char != ' ' && cell.Char != 0 && cell.Flags&grid.FlagHidden == 0 {
					if !r.drawBlockElement(cursorX, cursorY, cell.Char, r.theme.Background, proj) {
						if _, ok := r.resolveGlyph(cell.Char); ok {
							r.drawChar(cursorX, cursorY+r.cellHeight, cell.Char, r.theme.Background, proj)
						} else if cg, ok := r.ensureColorGlyph(cell.Char); ok {
							span := 1
							if cell.Width == grid.CellWidthWide {
								span = 2
							}
							r.drawColorGlyph(cursorX, cursorY, span, cg, 1.0, proj)
						} else {
							r.drawChar(cursorX, cursorY+r.cellHeight, cell.Char, r.theme.Background, proj)
						}
					}
				}
			}
		}
	}
}

// fitCells returns how many leading cells of size sz, laid out from offset,
// satisfy offset+float32(i)*sz+sz <= limit. This is exactly the clip predicate
// renderGridAt historically applied per cell, hoisted out of the loop: for
// sz > 0 the left side is non-decreasing in i (float add/multiply are
// monotonic), so a binary search over the identical expression yields the
// same set of visible cells.
func fitCells(offset, sz float32, n int, limit float32) int {
	if sz <= 0 || n <= 0 {
		return 0
	}
	lo, hi := 0, n
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if offset+float32(mid)*sz+sz > limit {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// buildGridBatches fills gridRects, gridGlyphs, colorDraws and pass2 from the
// snapshot — everything pass 1 of renderGridAt records. It performs no GL
// calls of its own, but a glyph cache miss may rasterize (and grow the
// atlas), so the caller re-runs it when atlasGen changes mid-pass.
func (r *Renderer) buildGridBatches(snap *grid.Snapshot, offsetX, offsetY, paneWidth, paneHeight float32, hoverRow int) {
	cols := snap.Cols

	r.gridRects.reset()
	r.gridGlyphs.reset()
	r.colorDraws = r.colorDraws[:0]
	r.pass2 = r.pass2[:0]

	// Hoist the pane-clip check: cells past these limits produce no output.
	maxCol := fitCells(offsetX, r.cellWidth, cols, offsetX+paneWidth)
	maxRow := fitCells(offsetY, r.cellHeight, snap.Rows, offsetY+paneHeight)

	selActive := snap.SelActive
	selColor := r.theme.Selection
	bgTheme := r.theme.Background
	fgTheme := r.theme.Foreground
	rectW := r.cellWidth + 0.5

	for row := range maxRow {
		rowCells := snap.Cells[row*cols : row*cols+cols]
		y := offsetY + float32(row)*r.cellHeight
		rowHovered := row == hoverRow
		for col := range maxCol {
			cell := rowCells[col]
			x := offsetX + float32(col)*r.cellWidth

			inverse := cell.Flags&grid.FlagInverse != 0
			// Fast path: a default background without inverse is exactly the
			// theme background, which draws no rect — skip the color math.
			if cell.Bg.Type != grid.ColorDefault || inverse {
				bgColor := r.colorToRGBA(cell.Bg, true)
				if inverse {
					bgColor = r.colorToRGBA(cell.Fg, false)
				}
				if bgColor != bgTheme {
					r.gridRects.addRect(x, y, rectW, r.cellHeight, bgColor)
				}
			}
			if selActive && snap.Selected(col, row) {
				r.gridRects.addRect(x, y, rectW, r.cellHeight, selColor)
			}

			if cell.Width == grid.CellWidthContinuation {
				continue
			}

			hidden := cell.Flags&grid.FlagHidden != 0
			isBlock := isBlockElement(cell.Char)
			// Block-element chars and the cursor cell are drawn immediately
			// in pass 2; everything else is a batched glyph. A glyph
			// missing from the monochrome font is tried as a color emoji
			// before the '?' fallback.
			needGlyph := !hidden && cell.Char != ' ' && cell.Char != 0 && !isBlock
			hovered := rowHovered && col >= r.hoverStartCol && col <= r.hoverEndCol
			needDecor := !hidden && (isBlock || hovered ||
				cell.Flags&(grid.FlagUnderline|grid.FlagStrikethrough) != 0)
			if !needGlyph && !needDecor {
				continue
			}

			// Resolve the fg color exactly once for both the glyph and the
			// pass-2 decorations. Fast path: default fg without inverse or
			// dim is exactly the theme foreground.
			var fgColor [4]float32
			if cell.Fg.Type == grid.ColorDefault && cell.Flags&(grid.FlagInverse|grid.FlagDim) == 0 {
				fgColor = fgTheme
			} else {
				fgColor = r.colorToRGBA(cell.Fg, false)
				if inverse {
					fgColor = r.colorToRGBA(cell.Bg, true)
				}
				if cell.Flags&grid.FlagDim != 0 {
					fgColor[3] = fgColor[3] / 2
				}
			}
			if needDecor {
				r.pass2 = append(r.pass2, pass2Item{
					col: col, row: row, x: x, y: y, fg: fgColor, hovered: hovered,
				})
			}

			if needGlyph {
				g, ok := r.resolveGlyph(cell.Char)
				if !ok {
					if cg, isColor := r.ensureColorGlyph(cell.Char); isColor {
						span := 1
						if cell.Width == grid.CellWidthWide {
							span = 2
						}
						r.colorDraws = append(r.colorDraws, colorDrawItem{
							x: x, yTop: y, span: span, cg: cg, alpha: fgColor[3],
						})
					} else {
						g, ok = r.ensureGlyph('?')
					}
				}
				if ok && g.PixelWidth > 0 {
					// Available span: wide chars own two cells; icons may
					// also use a following blank cell (Ghostty-style), which
					// is how Nerd Font icons are typically spaced in TUIs.
					span := 1
					if cell.Width == grid.CellWidthWide {
						span = 2
					} else if isIconRune(cell.Char) && col+1 < cols {
						next := rowCells[col+1]
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
					r.gridGlyphs.addGlyph(gx, gyTop+gh, gw, gh,
						g.X, g.Y, g.Width, g.Height, fgColor, shear)
					if r.fauxBold && cell.Flags&grid.FlagBold != 0 {
						r.gridGlyphs.addGlyph(gx+1, gyTop+gh, gw, gh,
							g.X, g.Y, g.Width, g.Height, fgColor, shear)
					}
				}
			}
		}
	}
}

// SetHoverURL sets the hover underline range for a grid. Latches a redraw
// only when the hover state actually changes (it is called on every mouse
// move).
func (r *Renderer) SetHoverURL(g *grid.Grid, row, startCol, endCol int) {
	if g == nil || row < 0 || startCol < 0 || endCol < startCol {
		r.ClearHoverURL()
		return
	}
	if r.hoverActive && r.hoverGrid == g && r.hoverRow == row &&
		r.hoverStartCol == startCol && r.hoverEndCol == endCol {
		return
	}
	r.hoverGrid = g
	r.hoverRow = row
	r.hoverStartCol = startCol
	r.hoverEndCol = endCol
	r.hoverActive = true
	r.uiDirty = true
}

// ClearHoverURL clears any active hover underline.
func (r *Renderer) ClearHoverURL() {
	if r.hoverActive {
		r.uiDirty = true
	}
	r.hoverGrid = nil
	r.hoverActive = false
}

// DrawToast renders a small notification overlay.
func (r *Renderer) DrawToast(message string, width, height int) {
	if strings.TrimSpace(message) == "" {
		return
	}

	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)

	paddingX := r.cellWidth * 0.8
	paddingY := r.cellHeight * 0.35
	runes := []rune(message)
	textWidth := float32(len(runes)) * r.cellWidth
	boxW := textWidth + paddingX*2
	boxH := r.cellHeight + paddingY*2
	margin := r.cellWidth * 0.8

	maxWidth := float32(width) - margin*2
	if boxW > maxWidth {
		maxChars := int((maxWidth - paddingX*2) / r.cellWidth)
		if maxChars > 3 {
			message = string(runes[:maxChars-3]) + "..."
			runes = []rune(message)
			textWidth = float32(len(runes)) * r.cellWidth
			boxW = textWidth + paddingX*2
		} else {
			return
		}
	}

	x := float32(width) - boxW - margin
	y := float32(height) - boxH - margin
	bg := r.theme.TabBar
	bg[3] = 0.85

	r.drawRect(x, y, boxW, boxH, bg, proj)
	r.drawText(x+paddingX, y+boxH-paddingY, message, r.theme.Foreground, proj)
}

// DrawFindBar renders the scrollback find prompt: a bar pinned to the bottom
// of the window showing the query and a match counter. It is drawn on the left
// so it does not collide with the toast, which occupies the bottom-right.
// status is the right-aligned counter text (e.g. "3/17" or "no matches").
func (r *Renderer) DrawFindBar(query, status string, width, height int) {
	proj := orthoMatrix(0, float32(width), float32(height), 0, -1, 1)

	paddingX := r.cellWidth * 0.8
	paddingY := r.cellHeight * 0.35
	boxH := r.cellHeight + paddingY*2
	margin := r.cellWidth * 0.8

	// A block cursor after the query marks the bar as the input focus; without
	// it an empty prompt looks inert.
	text := "find: " + query + "█"
	boxW := float32(len([]rune(text))+len([]rune(status))+2)*r.cellWidth + paddingX*2
	if maxW := float32(width) - margin*2; boxW > maxW {
		boxW = maxW
	}

	x := margin
	y := float32(height) - boxH - margin
	bg := r.theme.TabBar
	bg[3] = 0.95
	r.drawRect(x, y, boxW, boxH, bg, proj)
	r.drawRect(x, y, 3, boxH, r.theme.TabActive, proj)

	baseline := y + boxH - paddingY
	r.drawText(x+paddingX, baseline, text, r.theme.Foreground, proj)
	if status != "" {
		statusX := x + boxW - paddingX - float32(len([]rune(status)))*r.cellWidth
		r.drawText(statusX, baseline, status, withAlpha(r.theme.Foreground, 0.6), proj)
	}
}

// drawRect draws a colored rectangle
func (r *Renderer) drawRect(x, y, w, h float32, clr [4]float32, proj [16]float32) {
	r.uiEnsure(uiKindRect, proj)
	r.uiRects.addRect(x, y, w, h, clr)
}

// drawUnderlineStyled draws an underline across a cell in the given SGR 4:n style:
// 1=solid, 2=double, 3=curly (undercurl), 4=dotted, 5=dashed. Unknown styles draw solid.
func (r *Renderer) drawUnderlineStyled(x, y float32, clr [4]float32, proj [16]float32, style uint8) {
	w := r.cellWidth
	baseY := y + r.cellHeight - 1
	switch style {
	case 2: // double underline
		r.drawRect(x, baseY, w, 1, clr, proj)
		if baseY-2 >= y {
			r.drawRect(x, baseY-2, w, 1, clr, proj)
		}
	case 3: // curly / undercurl
		r.drawUndercurl(x, baseY, w, clr, proj)
	case 4: // dotted
		for dx := 0; dx < int(w); dx += 2 {
			r.drawRect(x+float32(dx), baseY, 1, 1, clr, proj)
		}
	case 5: // dashed
		for dx := 0; dx < int(w); dx += 4 {
			seg := float32(2)
			if float32(dx)+seg > w {
				seg = w - float32(dx)
			}
			r.drawRect(x+float32(dx), baseY, seg, 1, clr, proj)
		}
	default: // solid
		r.drawRect(x, baseY, w, 1, clr, proj)
	}
}

// drawUndercurl approximates a sine wave with 1px segments (triangle wave) so editors'
// curly diagnostic underlines (SGR 4:3) render without needing a custom shader.
func (r *Renderer) drawUndercurl(x, baseY, w float32, clr [4]float32, proj [16]float32) {
	const period = 4 // pixels per wave
	const amp = 1    // peak offset in pixels
	for dx := 0; dx < int(w); dx++ {
		phase := dx % period
		var off float32
		// triangle wave: 0 -> +amp -> 0 -> -amp across one period
		switch {
		case phase < period/2:
			off = float32(amp) - float32(phase)*(2*amp)/float32(period/2)
		default:
			off = -float32(amp) + float32(phase-period/2)*(2*amp)/float32(period/2)
		}
		r.drawRect(x+float32(dx), baseY+off, 1, 1, clr, proj)
	}
}

// boxDrawingFallbacks maps rounded corners and other box chars to simpler equivalents
var boxDrawingFallbacks = map[rune]rune{
	'╭': '┌',  // U+256D -> U+250C (rounded to square corner)
	'╮': '┐',  // U+256E -> U+2510
	'╯': '┘',  // U+256F -> U+2518
	'╰': '└',  // U+2570 -> U+2514
	'╱': '/',  // U+2571 -> ASCII slash
	'╲': '\\', // U+2572 -> ASCII backslash
	'╳': 'X',  // U+2573 -> ASCII X
}

// unicodeFallbacks maps common Unicode characters to ASCII equivalents
var unicodeFallbacks = map[rune]rune{
	'\u2010': '-',  // HYPHEN
	'\u2011': '-',  // NON-BREAKING HYPHEN
	'\u2012': '-',  // FIGURE DASH
	'\u2013': '-',  // EN DASH
	'\u2014': '-',  // EM DASH
	'\u2015': '-',  // HORIZONTAL BAR
	'\u2212': '-',  // MINUS SIGN
	'\u2018': '\'', // LEFT SINGLE QUOTATION
	'\u2019': '\'', // RIGHT SINGLE QUOTATION
	'\u201C': '"',  // LEFT DOUBLE QUOTATION
	'\u201D': '"',  // RIGHT DOUBLE QUOTATION
	'\u2026': '.',  // HORIZONTAL ELLIPSIS
	'\u00B7': '.',  // MIDDLE DOT
	'\u2022': '*',  // BULLET
	'\u2023': '>',  // TRIANGULAR BULLET
	'\u25CF': '*',  // BLACK CIRCLE
	// Dingbat arrows (e.g. nvim-tree's symlink arrow U+279B) degrade to the
	// plain rightwards arrow, which every embedded font except Ubuntu Mono
	// has; these only apply when no font in the fallback chain covers them.
	'\u2794': '\u2192', // HEAVY WIDE-HEADED RIGHTWARDS ARROW
	'\u2799': '\u2192', // HEAVY RIGHTWARDS ARROW
	'\u279B': '\u2192', // DRAFTING POINT RIGHTWARDS ARROW
	'\u279C': '\u2192', // HEAVY ROUND-TIPPED RIGHTWARDS ARROW
	'\u279D': '\u2192', // TRIANGLE-HEADED RIGHTWARDS ARROW
	'\u279E': '\u2192', // HEAVY TRIANGLE-HEADED RIGHTWARDS ARROW
	'\u27A1': '\u2192', // BLACK RIGHTWARDS ARROW
	'\u27A4': '\u2192', // BLACK RIGHTWARDS ARROWHEAD
	'\u2192': '>',      // RIGHTWARDS ARROW (Ubuntu Mono lacks even this)
	'\u2190': '<',      // LEFTWARDS ARROW
	// Media-control triangles (Unicode 7.0). Claude Code's "auto mode" prompt
	// uses U+23F5 U+23F5 (\u23f5\u23f5); no embedded or macOS system font covers these,
	// so they hit the '?' resort and render as "??". Map to the plain black
	// triangles U+25B6/U+25C0, which the fallback fonts (Menlo, Apple Symbols)
	// do have, so they render as real \u25b6/\u25c0 instead.
	'\u23f5': '\u25b6', // BLACK MEDIUM RIGHT-POINTING TRIANGLE -> \u25b6
	'\u23f4': '\u25c0', // BLACK MEDIUM LEFT-POINTING TRIANGLE  -> \u25c0
	'\u276E': '<',      // HEAVY LEFT-POINTING ANGLE QUOTATION MARK
	'\u276F': '>',      // HEAVY RIGHT-POINTING ANGLE QUOTATION MARK
	'\u2713': 'v',      // CHECK MARK
	'\u2717': 'x',      // BALLOT X
}

var quadrantBlockMasks = map[rune]uint8{
	'\u2596': 0b0100, // Quadrant lower left
	'\u2597': 0b1000, // Quadrant lower right
	'\u2598': 0b0001, // Quadrant upper left
	'\u2599': 0b1101, // Quadrant upper left and lower left and lower right
	'\u259A': 0b1001, // Quadrant upper left and lower right
	'\u259B': 0b0111, // Quadrant upper left and upper right and lower left
	'\u259C': 0b1011, // Quadrant upper left and upper right and lower right
	'\u259D': 0b0010, // Quadrant upper right
	'\u259E': 0b0110, // Quadrant upper right and lower left
	'\u259F': 0b1110, // Quadrant upper right and lower left and lower right
}

// drawBlockElement renders block element characters as geometry to avoid seams.
func (r *Renderer) drawBlockElement(x, y float32, char rune, clr [4]float32, proj [16]float32) bool {
	switch char {
	case '\u2588': // Full block
		r.drawRect(x, y, r.cellWidth, r.cellHeight, clr, proj)
		return true
	case '\u2580': // Upper half block
		r.drawRect(x, y, r.cellWidth, r.cellHeight/2, clr, proj)
		return true
	case '\u2584': // Lower half block
		r.drawRect(x, y+r.cellHeight/2, r.cellWidth, r.cellHeight/2, clr, proj)
		return true
	case '\u258C': // Left half block
		r.drawRect(x, y, r.cellWidth/2, r.cellHeight, clr, proj)
		return true
	case '\u2590': // Right half block
		r.drawRect(x+r.cellWidth/2, y, r.cellWidth/2, r.cellHeight, clr, proj)
		return true
	case '\u2591', '\u2592', '\u2593': // Light/medium/dark shade
		shade := clr
		switch char {
		case '\u2591':
			shade[3] *= 0.25
		case '\u2592':
			shade[3] *= 0.5
		case '\u2593':
			shade[3] *= 0.75
		}
		r.drawRect(x, y, r.cellWidth, r.cellHeight, shade, proj)
		return true
	case '\u2594': // Upper one eighth block
		r.drawRect(x, y, r.cellWidth, r.cellHeight/8, clr, proj)
		return true
	case '\u2595': // Right one eighth block
		r.drawRect(x+r.cellWidth*7/8, y, r.cellWidth/8, r.cellHeight, clr, proj)
		return true
	}

	if char >= '\u2581' && char <= '\u2587' {
		// Lower 1/8..7/8 blocks
		n := float32(char - '\u2580')
		h := r.cellHeight * n / 8
		r.drawRect(x, y+r.cellHeight-h, r.cellWidth, h, clr, proj)
		return true
	}

	if char >= '\u2589' && char <= '\u258F' {
		// Left 7/8..1/8 blocks
		n := float32('\u2590' - char)
		w := r.cellWidth * n / 8
		r.drawRect(x, y, w, r.cellHeight, clr, proj)
		return true
	}

	if mask, ok := quadrantBlockMasks[char]; ok {
		hw := r.cellWidth / 2
		hh := r.cellHeight / 2
		if mask&0b0001 != 0 {
			r.drawRect(x, y, hw, hh, clr, proj)
		}
		if mask&0b0010 != 0 {
			r.drawRect(x+hw, y, hw, hh, clr, proj)
		}
		if mask&0b0100 != 0 {
			r.drawRect(x, y+hh, hw, hh, clr, proj)
		}
		if mask&0b1000 != 0 {
			r.drawRect(x+hw, y+hh, hw, hh, clr, proj)
		}
		return true
	}

	return false
}

// resolveGlyph resolves a rune to a monochrome glyph, applying box-drawing and
// unicode-to-ASCII fallbacks, but WITHOUT the '?' last resort — so callers can
// try the color-emoji path before giving up on a missing glyph.
func (r *Renderer) resolveGlyph(char rune) (Glyph, bool) {
	// ASCII fast path: neither fallback table has keys < 128, so resolution
	// is exactly ensureGlyph; serve it from the flat cache instead of the map.
	if char >= 0 && char < 128 {
		slot := &r.asciiCache[char]
		if !slot.resolved {
			slot.g, slot.ok = r.ensureGlyph(char)
			slot.resolved = true
		}
		return slot.g, slot.ok
	}
	if glyph, ok := r.ensureGlyph(char); ok {
		return glyph, true
	}
	if fallback, has := boxDrawingFallbacks[char]; has {
		if glyph, ok := r.ensureGlyph(fallback); ok {
			return glyph, true
		}
	}
	if fallback, has := unicodeFallbacks[char]; has {
		if glyph, ok := r.ensureGlyph(fallback); ok {
			return glyph, true
		}
	}
	return Glyph{}, false
}

// lookupGlyph resolves a rune to a monochrome glyph, falling back to '?' when
// the font (and its box-drawing/unicode fallbacks) has no glyph. Callers that
// want to try color emoji first should use resolveGlyph + ensureColorGlyph.
func (r *Renderer) lookupGlyph(char rune) (Glyph, bool) {
	if glyph, ok := r.resolveGlyph(char); ok {
		return glyph, true
	}
	return r.ensureGlyph('?')
}

func (r *Renderer) drawChar(x, y float32, char rune, clr [4]float32, proj [16]float32) {
	r.drawCharStyled(x, y, char, clr, proj, false, false)
}

// drawCharStyled draws a glyph with optional synthesized bold (offset double-draw)
// and italic (horizontal shear). With bold=false, italic=false it is identical to a
// plain drawChar.
func (r *Renderer) drawCharStyled(x, y float32, char rune, clr [4]float32, proj [16]float32, bold, italic bool) {
	glyph, ok := r.resolveGlyph(char)
	if !ok {
		// Try the color-emoji path (same as the grid renderer) before the
		// '?' fallback, so emoji in panel text render instead of degrading.
		if cg, isColor := r.ensureColorGlyph(char); isColor {
			r.drawColorGlyph(x, y-r.cellHeight, grid.RuneWidth(char), cg, clr[3], proj)
			return
		}
		glyph, ok = r.ensureGlyph('?')
	}
	if !ok || glyph.PixelWidth == 0 {
		return
	}

	// y is the cell bottom; place the glyph at its bearings relative to the
	// cell box (with icon constraint applied).
	gx, gyTop, w, h := r.glyphQuad(char, glyph, x, y-r.cellHeight, 1)
	gy := gyTop + h

	// Texture coordinates
	tx := glyph.X
	ty := glyph.Y
	tw := glyph.Width
	th := glyph.Height

	// Faux italic: slant the glyph by shifting its top edge to the right.
	var shear float32
	if italic {
		shear = h * 0.2
	}

	r.uiEnsure(uiKindGlyph, proj)
	// resolveGlyph above may have grown (re-packed) the atlas, invalidating
	// UVs already recorded in this batch. Drop them rather than draw garbage;
	// the affected chars reappear next frame from the now-warm atlas.
	if r.uiAtlasGen != r.atlasGen {
		r.uiGlyphs.reset()
		r.uiAtlasGen = r.atlasGen
	}
	r.uiGlyphs.addGlyph(gx, gy, w, h, tx, ty, tw, th, clr, shear)
	// Faux bold: a second quad offset by 1px in x thickens the strokes.
	if bold {
		r.uiGlyphs.addGlyph(gx+1, gy, w, h, tx, ty, tw, th, clr, shear)
	}
}

// drawText draws a string of text
func (r *Renderer) drawText(x, y float32, text string, clr [4]float32, proj [16]float32) {
	r.drawTextStyled(x, y, text, clr, false, proj)
}

// drawTextStyled draws a string with optional faux-bold, advancing per rune
// width exactly like drawText.
func (r *Renderer) drawTextStyled(x, y float32, text string, clr [4]float32, bold bool, proj [16]float32) {
	for _, char := range text {
		w := grid.RuneWidth(char)
		if w == 0 {
			// Combining marks / variation selectors have no cell of their own
			// in panel text; skip rather than draw the '?' fallback glyph.
			continue
		}
		r.drawCharStyled(x, y, char, clr, proj, bold, false)
		x += r.cellWidth * float32(w)
	}
}

// textCells returns the number of terminal cells the string occupies when
// drawn with drawText (wide runes such as emoji take two).
func textCells(s string) int {
	return grid.StringWidth(s)
}

// truncateToCells truncates s so it occupies at most max cells, appending an
// ellipsis when something was cut. Safe for multibyte/wide runes (never slices
// mid-rune, counts display cells rather than bytes).
func truncateToCells(s string, max int) string {
	if max <= 0 || textCells(s) <= max {
		return s
	}
	budget := max - 3 // room for "..."
	w := 0
	for i, char := range s {
		cw := grid.RuneWidth(char)
		if w+cw > budget {
			return s[:i] + "..."
		}
		w += cw
	}
	return s
}

// truncateHeadToCells keeps the tail of s so it occupies at most max cells,
// prefixing an ellipsis when something was cut (used for input fields that
// scroll horizontally).
func truncateHeadToCells(s string, max int) string {
	if max <= 0 || textCells(s) <= max {
		return s
	}
	budget := max - 3
	runes := []rune(s)
	w := 0
	for i := len(runes) - 1; i >= 0; i-- {
		cw := grid.RuneWidth(runes[i])
		if w+cw > budget {
			return "..." + string(runes[i+1:])
		}
		w += cw
	}
	return s
}

// drawInputCursor draws a thin caret bar at x, sized to one text line. Using a
// bar (instead of an underscore glyph) keeps it visible on top of placeholder
// text without the two glyphs smudging into each other.
func (r *Renderer) drawInputCursor(x, textY float32, proj [16]float32) {
	// textY matches the y passed to drawText, which treats it as the cell
	// bottom; the bar spans that same cell box.
	r.drawRect(x, textY-r.cellHeight, 2, r.cellHeight, r.theme.TabActive, proj)
}

// drawTextScaled draws text at a specific scale relative to current font
func (r *Renderer) drawTextScaled(x, y float32, text string, clr [4]float32, proj [16]float32, scale float32) {
	for _, char := range text {
		r.drawCharScaled(x, y, char, clr, proj, scale)
		x += r.cellWidth * scale
	}
}

// drawCharScaled draws a character at a specific scale
func (r *Renderer) drawCharScaled(x, y float32, char rune, clr [4]float32, proj [16]float32, scale float32) {
	glyph, ok := r.lookupGlyph(char)
	if !ok || glyph.PixelWidth == 0 {
		return
	}

	// Calculate screen coordinates with scale; y is the (scaled) cell bottom,
	// so the baseline sits ascent-from-top within the scaled cell box.
	w := float32(glyph.PixelWidth) * scale
	h := float32(glyph.PixelHeight) * scale
	baseline := y - r.cellHeight*scale + float32(r.atlasAscent)*scale
	gx := x + glyph.OffsetX*scale
	gyTop := baseline + glyph.OffsetY*scale
	gy := gyTop + h

	// Texture coordinates
	tx := glyph.X
	ty := glyph.Y
	tw := glyph.Width
	th := glyph.Height

	r.uiEnsure(uiKindGlyph, proj)
	r.uiGlyphs.addGlyph(gx, gy, w, h, tx, ty, tw, th, clr, 0)
}

// colorToRGBA converts a grid.Color to RGBA
func (r *Renderer) colorToRGBA(c grid.Color, isBackground bool) [4]float32 {
	switch c.Type {
	case grid.ColorDefault:
		if isBackground {
			return r.theme.Background
		}
		return r.theme.Foreground
	case grid.ColorIndexed:
		return indexedColor(c.Index)
	case grid.ColorRGB:
		return [4]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255, 1.0}
	}
	return r.theme.Foreground
}

// standard16 is the standard ANSI palette (hoisted to package level so
// indexedColor doesn't rebuild the table literal on every call).
var standard16 = [16][4]float32{
	{0.043, 0.059, 0.078, 1.0}, // 0: Black
	{0.820, 0.412, 0.412, 1.0}, // 1: Red
	{0.498, 0.737, 0.549, 1.0}, // 2: Green
	{0.843, 0.729, 0.490, 1.0}, // 3: Yellow
	{0.533, 0.643, 0.831, 1.0}, // 4: Blue
	{0.773, 0.525, 0.753, 1.0}, // 5: Magenta
	{0.498, 0.773, 0.784, 1.0}, // 6: Cyan
	{0.831, 0.847, 0.871, 1.0}, // 7: White
	{0.294, 0.322, 0.388, 1.0}, // 8: Bright Black
	{0.878, 0.478, 0.478, 1.0}, // 9: Bright Red
	{0.604, 0.843, 0.659, 1.0}, // 10: Bright Green
	{0.906, 0.788, 0.545, 1.0}, // 11: Bright Yellow
	{0.647, 0.749, 0.941, 1.0}, // 12: Bright Blue
	{0.847, 0.627, 0.831, 1.0}, // 13: Bright Magenta
	{0.604, 0.843, 0.863, 1.0}, // 14: Bright Cyan
	{0.945, 0.953, 0.961, 1.0}, // 15: Bright White
}

// indexedColor returns the RGB color for an indexed color (0-255)
func indexedColor(index uint8) [4]float32 {
	if index < 16 {
		return standard16[index]
	}

	// 216 color cube (indices 16-231)
	if index < 232 {
		idx := index - 16
		red := (idx / 36) % 6
		green := (idx / 6) % 6
		blue := idx % 6
		return [4]float32{
			float32(red) * 51 / 255,
			float32(green) * 51 / 255,
			float32(blue) * 51 / 255,
			1.0,
		}
	}

	// Grayscale (indices 232-255)
	gray := float32(index-232) * 10 / 255
	return [4]float32{gray, gray, gray, 1.0}
}

// CellDimensions returns the cell width and height
func (r *Renderer) CellDimensions() (float32, float32) {
	return r.cellWidth, r.cellHeight
}

// TabBarWidth returns the tab bar width currently reserved for layout (0 when hidden).
func (r *Renderer) TabBarWidth() float32 {
	return r.layoutTabBarWidth()
}

// CalculateGridSize calculates the number of columns and rows that fit
func (r *Renderer) CalculateGridSize(width, height int) (cols, rows int) {
	_, _, availW, availH := r.layoutMargins(width, height)
	cols = int(availW / r.cellWidth)
	rows = int(availH / r.cellHeight)
	// Usable floor: below this the terminal is no longer functional and
	// layout degenerates. The window-level minimum (see window.NewWindow)
	// normally prevents us from ever reaching it, but clamp here too so a
	// transient tiny frame can't collapse the grid to a 1x1 cell.
	if cols < minGridCols {
		cols = minGridCols
	}
	if rows < minGridRows {
		rows = minGridRows
	}
	return
}

// ChangeFont changes the current font by name
func (r *Renderer) ChangeFont(name string) error {
	fontData, ok := fonts.GetFont(name)
	if !ok {
		return fmt.Errorf("font '%s' not found", name)
	}

	// Set the name first so the fallback chain rebuild excludes the new
	// active font; restore it if the load fails.
	prev := r.currentFont
	r.currentFont = name

	// loadFontData replaces the face, deletes the old atlas, and resets the cache.
	if err := r.loadFontData(fontData); err != nil {
		r.currentFont = prev
		return err
	}
	return nil
}

// CurrentFont returns the current font name
func (r *Renderer) CurrentFont() string {
	return r.currentFont
}

// GetAvailableFonts returns all available font names
func (r *Renderer) GetAvailableFonts() []fonts.FontInfo {
	return fonts.AvailableFonts()
}

// Default font size for reset
const defaultFontSize = 15.0
const minFontSize = 8.0
const maxFontSize = 32.0
const zoomStep = 2.0

// ZoomIn increases the font size
func (r *Renderer) ZoomIn() error {
	newSize := r.fontSize + zoomStep
	if newSize > maxFontSize {
		newSize = maxFontSize
	}
	return r.setFontSize(newSize)
}

// ZoomOut decreases the font size
func (r *Renderer) ZoomOut() error {
	newSize := r.fontSize - zoomStep
	if newSize < minFontSize {
		newSize = minFontSize
	}
	return r.setFontSize(newSize)
}

// ZoomReset resets the font size to default
func (r *Renderer) ZoomReset() error {
	return r.setFontSize(r.defaultFontSize)
}

// setFontSize changes the font size and reloads the font
func (r *Renderer) setFontSize(size float32) error {
	if size == r.fontSize {
		return nil
	}

	r.fontSize = size

	// Reload at the new size (loadFontData rebuilds face + atlas + cache).
	fontData, ok := fonts.GetFont(r.currentFont)
	if !ok {
		fontData = fonts.DefaultFont()
	}

	return r.loadFontData(fontData)
}

// SetDefaultFontSize sets the default font size and applies it.
func (r *Renderer) SetDefaultFontSize(size float32) error {
	size = clampFontSize(size)
	r.defaultFontSize = size
	return r.setFontSize(size)
}

// SetFontSize sets the current font size without changing the default.
func (r *Renderer) SetFontSize(size float32) error {
	return r.setFontSize(clampFontSize(size))
}

// GetFontSize returns the current font size
func (r *Renderer) GetFontSize() float32 {
	return r.fontSize
}

func clampFontSize(size float32) float32 {
	if size < minFontSize {
		return minFontSize
	}
	if size > maxFontSize {
		return maxFontSize
	}
	return size
}

// Destroy cleans up renderer resources
func (r *Renderer) Destroy() {
	gl.DeleteVertexArrays(1, &r.quadVAO)
	gl.DeleteBuffers(1, &r.quadVBO)
	gl.DeleteVertexArrays(1, &r.fontVAO)
	gl.DeleteBuffers(1, &r.fontVBO)
	gl.DeleteProgram(r.program)
	gl.DeleteProgram(r.fontProgram)
	gl.DeleteVertexArrays(1, &r.rectBatchVAO)
	gl.DeleteBuffers(1, &r.rectBatchVBO)
	gl.DeleteProgram(r.rectBatchProgram)
	gl.DeleteVertexArrays(1, &r.glyphBatchVAO)
	gl.DeleteBuffers(1, &r.glyphBatchVBO)
	gl.DeleteProgram(r.glyphBatchProgram)
	gl.DeleteTextures(1, &r.fontAtlas)
	if r.face != nil {
		r.face.Close()
	}
	for _, f := range r.fallbackFaces {
		f.Close()
	}
	r.destroyColorEmoji()
}

// cachedHomeDir avoids an os.UserHomeDir syscall per tab-bar frame; the home
// directory cannot change within a process.
var cachedHomeDir, _ = os.UserHomeDir()

// orthoMatrix creates an orthographic projection matrix
func orthoMatrix(left, right, bottom, top, near, far float32) [16]float32 {
	return [16]float32{
		2 / (right - left), 0, 0, 0,
		0, 2 / (top - bottom), 0, 0,
		0, 0, -2 / (far - near), 0,
		-(right + left) / (right - left), -(top + bottom) / (top - bottom), -(far + near) / (far - near), 1,
	}
}

// createProgram creates a shader program from vertex and fragment shader sources
func createProgram(vertexSource, fragmentSource string) (uint32, error) {
	vertexShader, err := compileShader(vertexSource, gl.VERTEX_SHADER)
	if err != nil {
		return 0, err
	}

	fragmentShader, err := compileShader(fragmentSource, gl.FRAGMENT_SHADER)
	if err != nil {
		gl.DeleteShader(vertexShader)
		return 0, err
	}

	program := gl.CreateProgram()
	gl.AttachShader(program, vertexShader)
	gl.AttachShader(program, fragmentShader)
	gl.LinkProgram(program)
	// Shaders are no longer needed once linked (or failed); freeing them here
	// covers the error path too.
	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

	var status int32
	gl.GetProgramiv(program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetProgramiv(program, gl.INFO_LOG_LENGTH, &logLength)
		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetProgramInfoLog(program, logLength, nil, gl.Str(log))
		gl.DeleteProgram(program)
		return 0, fmt.Errorf("failed to link program: %v", log)
	}

	return program, nil
}

// compileShader compiles a shader from source
func compileShader(source string, shaderType uint32) (uint32, error) {
	shader := gl.CreateShader(shaderType)

	csources, free := gl.Strs(source)
	gl.ShaderSource(shader, 1, csources, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLength)
		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))
		gl.DeleteShader(shader)
		return 0, fmt.Errorf("failed to compile shader: %v", log)
	}

	return shader, nil
}

// Ensure imports are used
var _ = color.White
var _ = draw.Draw
