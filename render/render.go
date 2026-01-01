package render

import (
	"fmt"
	"github.com/javanhut/RavenTerminal/aipanel"
	"github.com/javanhut/RavenTerminal/fonts"
	"github.com/javanhut/RavenTerminal/grid"
	"github.com/javanhut/RavenTerminal/menu"
	"github.com/javanhut/RavenTerminal/searchpanel"
	"github.com/javanhut/RavenTerminal/tab"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"github.com/go-gl/gl/v4.1-core/gl"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
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
}

// Glyph contains information about a rendered glyph
type Glyph struct {
	X, Y          float32 // Position in atlas (normalized 0-1)
	Width, Height float32 // Size in atlas (normalized 0-1)
	PixelWidth    int     // Actual pixel width
	PixelHeight   int     // Actual pixel height
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
	currentFont     string

	// Font data
	glyphs    map[rune]Glyph
	fontAtlas uint32
	atlasSize int

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

	// Help panel scroll state
	helpScrollOffset int

	// Hover underline state for URLs
	hoverGrid     *grid.Grid
	hoverRow      int
	hoverStartCol int
	hoverEndCol   int
	hoverActive   bool
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
		tabBarWidth:     135.0,
		currentFont:     fonts.DefaultFontName(),
		glyphs:          make(map[rune]Glyph),
		atlasSize:       512, // Larger atlas for Nerd Font icons
	}

	if err := r.initGL(); err != nil {
		return nil, err
	}

	if err := r.loadFont(); err != nil {
		return nil, err
	}

	// Store base cell dimensions for UI elements
	r.baseCellWidth = r.cellWidth
	r.baseCellHeight = r.cellHeight

	return r, nil
}

// loadFont loads the current embedded font and creates a glyph atlas
func (r *Renderer) loadFont() error {
	return r.loadFontData(fonts.DefaultFont())
}

// loadFontData loads font from byte data and creates a glyph atlas
func (r *Renderer) loadFontData(fontData []byte) error {
	parsedFont, err := opentype.Parse(fontData)
	if err != nil {
		return fmt.Errorf("failed to parse font: %w", err)
	}

	// Create font face with desired size
	face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    float64(r.fontSize),
		DPI:     96,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return fmt.Errorf("failed to create font face: %w", err)
	}
	defer face.Close()

	// Get font metrics
	metrics := face.Metrics()
	r.cellHeight = float32((metrics.Ascent + metrics.Descent).Ceil())

	// Calculate cell width from 'M' character
	advance, _ := face.GlyphAdvance('M')
	r.cellWidth = float32(advance.Ceil())

	// Create atlas image (RGBA for anti-aliasing)
	atlas := image.NewRGBA(image.Rect(0, 0, r.atlasSize, r.atlasSize))
	// Fill with transparent
	draw.Draw(atlas, atlas.Bounds(), image.Transparent, image.Point{}, draw.Src)

	// Drawer for rendering text
	drawer := &font.Drawer{
		Dst:  atlas,
		Src:  image.White,
		Face: face,
	}

	// Character ranges to render (ASCII + Extended + Nerd Font icons)
	charRanges := []struct{ start, end rune }{
		{32, 126},        // Printable ASCII
		{160, 255},       // Extended Latin-1
		{0x2500, 0x257F}, // Box Drawing
		{0x2580, 0x259F}, // Block Elements
		{0x25A0, 0x25FF}, // Geometric Shapes
		{0x2600, 0x26FF}, // Miscellaneous Symbols
		{0x2700, 0x27BF}, // Dingbats
		{0xE0A0, 0xE0D4}, // Powerline symbols
		{0xE200, 0xE2A9}, // Pomicons
		{0xE5FA, 0xE6B5}, // Seti-UI + Custom
		{0xE700, 0xE7C5}, // Devicons
		{0xEA60, 0xEC1E}, // Codicons
		{0xED00, 0xEFC1}, // Font Logos
		{0xF000, 0xF2E0}, // Font Awesome
		{0xF300, 0xF372}, // Font Awesome Extension
		{0xF400, 0xF533}, // Octicons
		{0xF500, 0xFD46}, // Material Design Icons
	}

	x, y := 0, metrics.Ascent.Ceil()
	charHeight := int(r.cellHeight)
	charWidth := int(r.cellWidth)

	for _, cr := range charRanges {
		for c := cr.start; c <= cr.end; c++ {
			// Check if we need to wrap to next row
			if x+charWidth > r.atlasSize {
				x = 0
				y += charHeight
			}
			if y+charHeight > r.atlasSize {
				break // Atlas full
			}

			// Check if glyph exists in font
			_, hasGlyph := face.GlyphAdvance(c)
			if !hasGlyph {
				continue
			}

			// Render glyph
			drawer.Dot = fixed.P(x, y)
			drawer.DrawString(string(c))

			// Store glyph info (normalized coordinates)
			r.glyphs[c] = Glyph{
				X:           float32(x) / float32(r.atlasSize),
				Y:           float32(y-metrics.Ascent.Ceil()) / float32(r.atlasSize),
				Width:       float32(charWidth) / float32(r.atlasSize),
				Height:      float32(charHeight) / float32(r.atlasSize),
				PixelWidth:  charWidth,
				PixelHeight: charHeight,
			}

			x += charWidth
		}
	}

	// Convert RGBA to single-channel alpha for OpenGL
	alphaAtlas := make([]byte, r.atlasSize*r.atlasSize)
	for i := 0; i < r.atlasSize*r.atlasSize; i++ {
		// Use the alpha channel for anti-aliased edges
		alphaAtlas[i] = atlas.Pix[i*4+3]
	}

	// Create OpenGL texture
	gl.GenTextures(1, &r.fontAtlas)
	gl.BindTexture(gl.TEXTURE_2D, r.fontAtlas)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RED, int32(r.atlasSize), int32(r.atlasSize), 0,
		gl.RED, gl.UNSIGNED_BYTE, gl.Ptr(alphaAtlas))

	// Use LINEAR filtering for smooth scaling (anti-aliasing)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)

	gl.BindTexture(gl.TEXTURE_2D, 0)

	return nil
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
}

func (r *Renderer) renderSearchPanel(panel *searchpanel.Panel, width, height int, proj [16]float32) {
	layout := panel.Layout(width, height, r.cellWidth, r.cellHeight)

	panelBg := [4]float32{0.05, 0.06, 0.08, 0.95}
	borderColor := r.theme.TabActive
	borderWidth := float32(2)

	r.drawRect(layout.PanelX, layout.PanelY, layout.PanelWidth, layout.PanelHeight, panelBg, proj)
	r.drawRect(layout.PanelX, layout.PanelY, layout.PanelWidth, borderWidth, borderColor, proj)
	r.drawRect(layout.PanelX, layout.PanelY+layout.PanelHeight-borderWidth, layout.PanelWidth, borderWidth, borderColor, proj)
	r.drawRect(layout.PanelX, layout.PanelY, borderWidth, layout.PanelHeight, borderColor, proj)
	r.drawRect(layout.PanelX+layout.PanelWidth-borderWidth, layout.PanelY, borderWidth, layout.PanelHeight, borderColor, proj)

	maxChars := int(layout.ContentWidth/r.cellWidth) - 2
	if maxChars < 10 {
		maxChars = 10
	}

	r.drawText(layout.ContentX, layout.HeaderY, "Web Search", r.theme.TabActive, proj)

	r.drawText(layout.ContentX, layout.InputLabelY, "Query", r.theme.Foreground, proj)
	inputBoxColor := [4]float32{0.03, 0.03, 0.05, 1.0}
	r.drawRect(layout.ContentX, layout.InputBoxY, layout.ContentWidth, layout.LineHeight, inputBoxColor, proj)

	inputText := panel.Query
	if len(inputText) > maxChars {
		inputText = "..." + inputText[len(inputText)-maxChars+3:]
	}
	r.drawText(layout.ContentX+8, layout.InputBoxY+layout.LineHeight*0.75, inputText+"_", r.theme.TabActive, proj)

	status := panel.Status
	if status == "" && panel.Loading {
		status = "Loading..."
	}
	if status != "" {
		if len(status) > maxChars {
			status = status[:maxChars-3] + "..."
		}
		r.drawText(layout.ContentX, layout.StatusY, status, r.theme.Cursor, proj)
	}

	if panel.Mode == searchpanel.ModePreview {
		r.renderSearchPreview(panel, layout, maxChars, proj)
	} else {
		r.renderSearchResults(panel, layout, maxChars, proj)
	}

	footerText := "Enter: search/open | Esc: close | Up/Down: navigate | PgUp/PgDn: scroll"
	proxyState := "Proxy: off"
	if panel.ProxyEnabled {
		proxyState = "Proxy: on"
	}
	focusState := "Focus: terminal"
	if panel.Focused {
		focusState = "Focus: panel"
	}
	footerText = footerText + " | " + proxyState + " (Ctrl+Shift+R) | " + focusState + " (Ctrl+Shift+[ or ])"
	if panel.Mode == searchpanel.ModePreview {
		footerText = "Esc/Left: back | Up/Down: scroll | PgUp/PgDn: page"
		footerText = footerText + " | " + proxyState + " (Ctrl+Shift+R) | " + focusState + " (Ctrl+Shift+[ or ])"
	}
	if len(footerText) > maxChars {
		footerText = footerText[:maxChars-3] + "..."
	}
	r.drawText(layout.ContentX, layout.FooterY, footerText, [4]float32{0.6, 0.6, 0.6, 1.0}, proj)
}

func (r *Renderer) renderAIPanel(panel *aipanel.Panel, width, height int, proj [16]float32) {
	layout := panel.Layout(width, height, r.cellWidth, r.cellHeight)

	panelBg := [4]float32{0.05, 0.06, 0.08, 0.95}
	borderColor := r.theme.TabActive
	borderWidth := float32(2)

	r.drawRect(layout.PanelX, layout.PanelY, layout.PanelWidth, layout.PanelHeight, panelBg, proj)
	r.drawRect(layout.PanelX, layout.PanelY, layout.PanelWidth, borderWidth, borderColor, proj)
	r.drawRect(layout.PanelX, layout.PanelY+layout.PanelHeight-borderWidth, layout.PanelWidth, borderWidth, borderColor, proj)
	r.drawRect(layout.PanelX, layout.PanelY, borderWidth, layout.PanelHeight, borderColor, proj)
	r.drawRect(layout.PanelX+layout.PanelWidth-borderWidth, layout.PanelY, borderWidth, layout.PanelHeight, borderColor, proj)

	maxChars := int(layout.ContentWidth/r.cellWidth) - 2
	if maxChars < 10 {
		maxChars = 10
	}

	r.drawText(layout.ContentX, layout.HeaderY, "AI Chat", r.theme.TabActive, proj)

	status := panel.Status
	if status == "" && panel.Loading {
		status = "Thinking..."
	}
	if status != "" {
		if len(status) > maxChars {
			status = status[:maxChars-3] + "..."
		}
		r.drawText(layout.ContentX, layout.StatusY, status, r.theme.Cursor, proj)
	}

	r.drawText(layout.ContentX, layout.InputLabelY, "Ask", r.theme.Foreground, proj)
	inputBoxColor := [4]float32{0.03, 0.03, 0.05, 1.0}
	r.drawRect(layout.ContentX, layout.InputBoxY, layout.ContentWidth, layout.LineHeight, inputBoxColor, proj)

	inputText := panel.Input
	if len(inputText) > maxChars {
		inputText = "..." + inputText[len(inputText)-maxChars+3:]
	}
	r.drawText(layout.ContentX+8, layout.InputBoxY+layout.LineHeight*0.75, inputText+"_", r.theme.TabActive, proj)

	lines := aipanel.BuildWrappedLines(panel.Messages, maxChars)
	panel.WrapChars = maxChars
	panel.WrappedLines = lines

	if len(lines) == 0 && !panel.Loading {
		r.drawText(layout.ContentX, layout.MessagesStart, "Ask a quick question to begin.", [4]float32{0.6, 0.6, 0.6, 1.0}, proj)
	} else {
		visibleLines := layout.VisibleLines
		totalLines := len(lines)
		maxScroll := totalLines - visibleLines
		if maxScroll < 0 {
			maxScroll = 0
		}
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
		for i := 0; i < visibleLines && startLine+i < totalLines; i++ {
			line := lines[startLine+i]
			if strings.TrimSpace(line.Text) != "" {
				color := r.theme.Foreground
				switch line.Role {
				case "user":
					color = r.theme.TabActive
				case "assistant":
					color = r.theme.Foreground
				case "error":
					color = [4]float32{0.9, 0.3, 0.3, 1.0} // Red for errors
				default:
					if line.Role != "" {
						color = r.theme.Cursor
					}
				}
				r.drawText(layout.ContentX, lineY, line.Text, color, proj)
			}
			lineY += layout.LineHeight
		}
	}

	footerText := "Enter: send | Esc: close | Up/Down: scroll | PgUp/PgDn: page | Ctrl+U: clear"
	focusState := "Focus: terminal"
	if panel.Focused {
		focusState = "Focus: panel"
	}
	footerText = footerText + " | " + focusState + " (Ctrl+Shift+[ or ])"
	if len(footerText) > maxChars {
		footerText = footerText[:maxChars-3] + "..."
	}
	r.drawText(layout.ContentX, layout.FooterY, footerText, [4]float32{0.6, 0.6, 0.6, 1.0}, proj)
}

func (r *Renderer) renderSearchResults(panel *searchpanel.Panel, layout searchpanel.Layout, maxChars int, proj [16]float32) {
	if len(panel.Results) == 0 {
		if !panel.Loading && strings.TrimSpace(panel.Query) != "" {
			r.drawText(layout.ContentX, layout.ResultsStart, "No results.", [4]float32{0.6, 0.6, 0.6, 1.0}, proj)
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
			highlightColor := [4]float32{0.12, 0.14, 0.22, 1.0}
			r.drawRect(layout.ContentX, drawY-layout.LineHeight+6, layout.ContentWidth, layout.LineHeight*2.2, highlightColor, proj)
		}

		title := strings.TrimSpace(result.Title)
		if len(title) > maxChars {
			title = title[:maxChars-3] + "..."
		}
		r.drawText(layout.ContentX, drawY, title, r.theme.TabActive, proj)

		subLine := strings.TrimSpace(result.Snippet)
		if subLine == "" {
			subLine = strings.TrimSpace(result.URL)
		}
		if len(subLine) > maxChars {
			subLine = subLine[:maxChars-3] + "..."
		}
		r.drawText(layout.ContentX+12, drawY+layout.LineHeight, subLine, r.theme.Foreground, proj)
	}
}

func (r *Renderer) renderSearchPreview(panel *searchpanel.Panel, layout searchpanel.Layout, maxChars int, proj [16]float32) {
	header := "Preview"
	if panel.PreviewTitle != "" {
		header = "Preview: " + panel.PreviewTitle
	}
	if len(header) > maxChars {
		header = header[:maxChars-3] + "..."
	}
	r.drawText(layout.ContentX, layout.ResultsStart, header, r.theme.TabActive, proj)

	wrappedLines := buildWrappedPreview(panel.PreviewLines, maxChars, r.theme)
	panel.PreviewWrapped = nil
	panel.PreviewWrapChars = maxChars
	for _, line := range wrappedLines {
		panel.PreviewWrapped = append(panel.PreviewWrapped, line.text)
	}

	visibleLines := layout.VisibleLines - 1
	if visibleLines < 1 {
		visibleLines = 1
	}
	startLine := panel.PreviewScroll
	maxScroll := len(wrappedLines) - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if startLine > maxScroll {
		startLine = maxScroll
	}

	lineY := layout.ResultsStart + layout.LineHeight
	for i := 0; i < visibleLines && startLine+i < len(wrappedLines); i++ {
		line := wrappedLines[startLine+i]
		r.drawText(layout.ContentX, lineY, line.text, line.color, proj)
		lineY += layout.LineHeight
	}
}

type styledLine struct {
	text  string
	color [4]float32
}

func buildWrappedPreview(lines []string, maxChars int, theme Theme) []styledLine {
	out := []styledLine{}
	inCode := false

	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}

		color := theme.Foreground
		prefix := ""
		indent := ""
		text := trimmed

		if inCode {
			color = theme.Cursor
		} else if strings.HasPrefix(text, "#") {
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
			color = theme.TabActive
		} else if strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "* ") || strings.HasPrefix(text, "+ ") {
			prefix = text[:2]
			text = strings.TrimSpace(text[2:])
			indent = "  "
		} else if strings.HasPrefix(text, "> ") {
			prefix = "> "
			text = strings.TrimSpace(text[2:])
			indent = "  "
		}

		text = stripInlineMarkdown(text)
		wrapped := wrapText(text, maxChars, prefix, indent)
		for _, line := range wrapped {
			out = append(out, styledLine{text: line, color: color})
		}
	}
	return out
}

func stripInlineMarkdown(text string) string {
	text = strings.ReplaceAll(text, "`", "")
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "__", "")
	text = strings.ReplaceAll(text, "*", "")
	text = strings.ReplaceAll(text, "_", "")
	return strings.TrimSpace(text)
}

func wrapText(text string, maxChars int, prefix, indent string) []string {
	if maxChars <= 0 {
		return []string{prefix + text}
	}
	if prefix == "" && indent == "" && len(text) <= maxChars {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{strings.TrimRight(prefix, " ")}
	}

	lines := []string{}
	line := prefix
	lineLimit := maxChars
	if lineLimit < 4 {
		lineLimit = 4
	}

	for _, word := range words {
		if line == "" {
			line = prefix
		}
		next := line
		if next != "" && !strings.HasSuffix(next, " ") {
			next += " "
		}
		next += word

		if len(next) <= lineLimit {
			line = next
			continue
		}

		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRight(line, " "))
			line = indent + word
			continue
		}

		// Hard wrap long word
		for len(word) > 0 {
			limit := lineLimit
			if len(word) <= limit {
				lines = append(lines, indent+word)
				word = ""
				break
			}
			lines = append(lines, indent+word[:limit])
			word = word[limit:]
		}
		line = ""
	}

	if strings.TrimSpace(line) != "" {
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

// getHelpSections returns all keybinding sections for the help panel
func (r *Renderer) getHelpSections() []struct {
	title    string
	bindings [][2]string
} {
	return []struct {
		title    string
		bindings [][2]string
	}{
		{
			title: "General",
			bindings: [][2]string{
				{"Ctrl+Q", "Exit terminal"},
				{"Ctrl+Shift+C", "Copy visible screen"},
				{"Ctrl+Shift+P", "Paste clipboard"},
				{"Shift+Enter", "Toggle fullscreen"},
				{"Ctrl+Shift+K", "Show/hide help"},
				{"Ctrl+Shift+S", "Open settings"},
				{"Ctrl+Shift+F", "Toggle web search"},
				{"Ctrl+Shift+A", "Toggle AI chat"},
				{"Ctrl+Shift++", "Zoom in"},
				{"Ctrl+Shift+-", "Zoom out"},
				{"Ctrl+Shift+0", "Reset zoom"},
			},
		},
		{
			title: "Tab Management",
			bindings: [][2]string{
				{"Ctrl+Shift+T", "New tab"},
				{"Ctrl+Shift+X", "Close current tab"},
				{"Ctrl+Tab", "Next tab"},
				{"Ctrl+Shift+Tab", "Previous tab"},
			},
		},
		{
			title: "Split Panes",
			bindings: [][2]string{
				{"Ctrl+Shift+V", "Split vertical"},
				{"Ctrl+Shift+H", "Split horizontal"},
				{"Ctrl+Shift+W", "Close pane"},
				{"Shift+Tab", "Cycle panes"},
				{"Ctrl+Shift+]", "Next pane"},
				{"Ctrl+Shift+[", "Previous pane"},
				{"Ctrl+Shift+[ or ]", "Cycle overlay panel (when open)"},
				{"Ctrl+R", "Toggle resize mode"},
				{"Arrow Keys", "Resize active pane"},
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
	maxScroll := r.getTotalHelpLines() - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
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

	// Calculate column positions - fixed key column width to prevent overlap
	// Longest key is "Ctrl+Shift+Tab" or "Shift+PageDown" which needs ~15 chars
	keyColWidth := r.cellWidth * 18 // 18 characters worth of space
	descColX := contentX + keyColWidth

	// Title (fixed, doesn't scroll)
	r.drawText(contentX, headerY, "Keybindings Help", r.theme.TabActive, proj)

	// Draw a separator line under the title
	separatorY := headerY + lineHeight*0.8
	r.drawRect(contentX, separatorY, contentWidth, 1, r.theme.Foreground, proj)

	// Scroll indicators
	totalLines := r.getTotalHelpLines()
	maxScroll := totalLines - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}

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
}

// renderMenu renders the settings menu overlay
func (r *Renderer) renderMenu(m *menu.Menu, width, height int, proj [16]float32) {
	// Fixed panel dimensions - use percentage of window but with sensible limits
	panelWidth := float32(width) * 0.75
	panelHeight := float32(height) * 0.80

	// Minimum size to fit content
	minWidth := float32(450)
	minHeight := float32(350)
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

	// Content area with margins
	marginX := float32(20)
	contentX := panelX + marginX
	contentWidth := panelWidth - marginX*2

	lineHeight := r.cellHeight * 1.5
	headerY := panelY + 35
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
	visibleItems := int(visibleHeight / lineHeight)
	if visibleItems < 1 {
		visibleItems = 1
	}

	totalItems := len(m.Items)
	maxScroll := totalItems - visibleItems
	if maxScroll < 0 {
		maxScroll = 0
	}

	scrollBarWidth := float32(8)
	scrollBarPadding := float32(8)
	if maxScroll > 0 {
		contentWidth -= scrollBarWidth + scrollBarPadding
	}

	// Calculate max characters that fit in content width (for truncation)
	maxChars := int(contentWidth/r.cellWidth) - 3 // -3 for "> " prefix
	if maxChars < 10 {
		maxChars = 10
	}

	// Title
	r.drawText(contentX, headerY, m.GetTitle(), r.theme.TabActive, proj)

	// Separator under title
	r.drawRect(contentX, separatorY, contentWidth, 1, r.theme.Foreground, proj)

	// Draw menu items
	itemIndex := 0
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

		// Truncate label to fit
		label := item.Label
		if len(label) > maxChars {
			label = label[:maxChars-3] + "..."
		}

		// Highlight selected item
		if i == m.SelectedIndex {
			highlightColor := [4]float32{0.15, 0.17, 0.25, 1.0}
			r.drawRect(contentX, y-lineHeight+8, contentWidth, lineHeight, highlightColor, proj)
			r.drawText(contentX+5, y, ">", r.theme.TabActive, proj)
			r.drawText(contentX+r.cellWidth*2+5, y, label, r.theme.TabActive, proj)
		} else {
			r.drawText(contentX+r.cellWidth*2+5, y, label, r.theme.Foreground, proj)
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

		if inputIsMultiline {
			textAreaHeight := lineHeight * float32(inputLines)
			inputAreaY := footerSepY - textAreaHeight - lineHeight*0.8

			// Input prompt
			r.drawText(contentX+5, inputAreaY, prompt, r.theme.Foreground, proj)

			// Text area background
			textBoxY := inputAreaY + lineHeight*0.3
			r.drawRect(contentX, textBoxY, contentWidth, textAreaHeight, [4]float32{0.03, 0.03, 0.05, 1.0}, proj)

			lines := strings.Split(inputText, "\n")
			if len(lines) == 0 {
				lines = []string{""}
			}
			start := 0
			if len(lines) > inputLines {
				start = len(lines) - inputLines
			}
			visibleLines := lines[start:]

			lineY := textBoxY + lineHeight*0.75
			for i, line := range visibleLines {
				cursor := ""
				if i == len(visibleLines)-1 {
					cursor = "_"
				}
				maxInputChars := maxChars - 2
				availableChars := maxInputChars - len(cursor)
				displayLine := line
				if availableChars <= 0 {
					displayLine = ""
				} else if len(displayLine) > availableChars {
					if availableChars > 3 {
						displayLine = "..." + displayLine[len(displayLine)-(availableChars-3):]
					} else {
						displayLine = displayLine[len(displayLine)-availableChars:]
					}
				}
				r.drawText(contentX+8, lineY, displayLine+cursor, r.theme.TabActive, proj)
				lineY += lineHeight
			}
		} else {
			inputAreaY := footerSepY - lineHeight*2

			// Input prompt
			r.drawText(contentX+5, inputAreaY, prompt, r.theme.Foreground, proj)

			// Input box background
			inputBoxY := inputAreaY + lineHeight*0.3
			r.drawRect(contentX, inputBoxY, contentWidth, lineHeight, [4]float32{0.03, 0.03, 0.05, 1.0}, proj)

			// Input text with cursor - truncate from left if too long
			maxInputChars := maxChars - 2
			if len(inputText) > maxInputChars {
				inputText = "..." + inputText[len(inputText)-maxInputChars+3:]
			}
			r.drawText(contentX+8, inputBoxY+lineHeight*0.75, inputText+"_", r.theme.TabActive, proj)
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
			footerText = "Enter: newline | Ctrl+Enter: confirm | Esc: cancel"
		} else {
			footerText = "Enter: confirm | Esc: cancel"
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

// renderPanes renders all panes in a tab using the nested layout system
func (r *Renderer) renderPanes(t *tab.Tab, width, height int, proj [16]float32, cursorVisible bool) {
	layouts := t.GetPaneLayouts()
	if len(layouts) == 0 {
		return
	}

	// Calculate available area (after tab bar)
	baseX := r.tabBarWidth + 5
	baseY := r.paddingTop
	availableWidth := float32(width) - r.tabBarWidth - 5
	availableHeight := float32(height) - r.paddingTop - r.paddingBottom

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
		r.renderGridAt(layout.Pane.Terminal.Grid, offsetX, offsetY, paneWidth, paneHeight, proj, showCursor)
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

	baseX := r.tabBarWidth + 5
	baseY := r.paddingTop
	availableWidth := float32(width) - r.tabBarWidth - 5
	availableHeight := float32(height) - r.paddingTop - r.paddingBottom
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
	fx := float32(x)
	fy := float32(y)
	for _, rect := range r.paneRects(t, width, height) {
		if fx < rect.x || fx >= rect.x+rect.width || fy < rect.y || fy >= rect.y+rect.height {
			continue
		}
		g := rect.pane.Terminal.Grid
		col := int((fx - rect.x) / r.cellWidth)
		row := int((fy - rect.y) / r.cellHeight)
		col = clampInt(col, 0, g.Cols-1)
		row = clampInt(row, 0, g.Rows-1)
		return rect.pane, col, row, true
	}
	return nil, 0, 0, false
}

// PaneRectFor returns the screen rect for a specific pane.
func (r *Renderer) PaneRectFor(t *tab.Tab, pane *tab.Pane, width, height int) (float32, float32, float32, float32, bool) {
	if pane == nil {
		return 0, 0, 0, 0, false
	}
	for _, rect := range r.paneRects(t, width, height) {
		if rect.pane == pane {
			return rect.x, rect.y, rect.width, rect.height, true
		}
	}
	return 0, 0, 0, 0, false
}

// CellSize returns the current render cell dimensions.
func (r *Renderer) CellSize() (float32, float32) {
	return r.cellWidth, r.cellHeight
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

// renderTabBar renders the left tab bar
func (r *Renderer) renderTabBar(tm *tab.TabManager, width, height int, proj [16]float32) {
	// Draw tab bar background
	r.drawRect(0, 0, r.tabBarWidth, float32(height), r.theme.TabBar, proj)

	// Draw separator line
	r.drawRect(r.tabBarWidth-2, 0, 2, float32(height), r.theme.Foreground, proj)

	// Calculate scale to render at base size regardless of zoom
	scale := r.baseFontSize / r.fontSize
	cellH := r.cellHeight * scale

	// Draw header
	header := fmt.Sprintf("RT %d/%d", tm.ActiveIndex()+1, tm.TabCount())
	r.drawTextScaled(10, cellH, header, r.theme.TabActive, proj, scale)

	// Draw tabs
	tabs := tm.GetTabs()
	activeIdx := tm.ActiveIndex()
	for i, t := range tabs {
		y := cellH*2 + float32(i)*cellH*1.2
		prefix := "  "
		clr := r.theme.Foreground
		if i == activeIdx {
			prefix = "> "
			clr = r.theme.TabActive
		}
		text := fmt.Sprintf("%sTab %d", prefix, t.ID())
		r.drawTextScaled(10, y, text, clr, proj, scale)
	}
}

// renderGrid renders the terminal grid (backward compatible wrapper)
func (r *Renderer) renderGrid(g *grid.Grid, width, height int, proj [16]float32, cursorVisible bool) {
	offsetX := r.tabBarWidth + 5
	offsetY := r.paddingTop
	availableWidth := float32(width) - r.tabBarWidth - 10
	availableHeight := float32(height) - r.paddingTop - r.paddingBottom
	r.renderGridAt(g, offsetX, offsetY, availableWidth, availableHeight, proj, cursorVisible)
}

// renderGridAt renders the terminal grid at a specific position
func (r *Renderer) renderGridAt(g *grid.Grid, offsetX, offsetY, paneWidth, paneHeight float32, proj [16]float32, cursorVisible bool) {
	cols := g.Cols
	rows := g.Rows

	// Render cells
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			cell := g.DisplayCell(col, row)
			x := offsetX + float32(col)*r.cellWidth
			y := offsetY + float32(row)*r.cellHeight

			// Skip if outside pane bounds
			if x+r.cellWidth > offsetX+paneWidth || y+r.cellHeight > offsetY+paneHeight {
				continue
			}

			// Draw background if not default
			bgColor := r.colorToRGBA(cell.Bg, true)
			if cell.Flags&grid.FlagInverse != 0 {
				bgColor, _ = r.colorToRGBA(cell.Fg, false), r.colorToRGBA(cell.Bg, true)
			}
			if bgColor != r.theme.Background {
				r.drawRect(x, y, r.cellWidth, r.cellHeight, bgColor, proj)
			}

			// Draw selection highlight
			if g.IsSelected(col, row) {
				r.drawRect(x, y, r.cellWidth, r.cellHeight, r.theme.Selection, proj)
			}

			// Draw character
			fgColor := r.colorToRGBA(cell.Fg, false)
			if cell.Flags&grid.FlagInverse != 0 {
				fgColor = r.colorToRGBA(cell.Bg, true)
			}
			if cell.Char != ' ' && cell.Char != 0 {
				r.drawChar(x, y+r.cellHeight, cell.Char, fgColor, proj)
			}

			// Draw underline for ANSI styling or hovered URL
			drawUnderline := cell.Flags&grid.FlagUnderline != 0
			if r.hoverActive && r.hoverGrid == g && row == r.hoverRow && col >= r.hoverStartCol && col <= r.hoverEndCol {
				drawUnderline = true
			}
			if drawUnderline && cell.Char != ' ' && cell.Char != 0 {
				underlineY := y + r.cellHeight - 1
				r.drawRect(x, underlineY, r.cellWidth, 1, fgColor, proj)
			}
		}
	}

	// Draw cursor
	if cursorVisible && g.GetScrollOffset() == 0 {
		cursorCol, cursorRow := g.GetCursor()
		cursorX := offsetX + float32(cursorCol)*r.cellWidth
		cursorY := offsetY + float32(cursorRow)*r.cellHeight

		// Only draw cursor if within pane bounds
		if cursorX+r.cellWidth <= offsetX+paneWidth && cursorY+r.cellHeight <= offsetY+paneHeight {
			r.drawRect(cursorX, cursorY, r.cellWidth, r.cellHeight, r.theme.Cursor, proj)

			// Redraw character under cursor in inverse
			cell := g.DisplayCell(cursorCol, cursorRow)
			if cell.Char != ' ' && cell.Char != 0 {
				r.drawChar(cursorX, cursorY+r.cellHeight, cell.Char, r.theme.Background, proj)
			}
		}
	}
}

// SetHoverURL sets the hover underline range for a grid.
func (r *Renderer) SetHoverURL(g *grid.Grid, row, startCol, endCol int) {
	if g == nil || row < 0 || startCol < 0 || endCol < startCol {
		r.ClearHoverURL()
		return
	}
	r.hoverGrid = g
	r.hoverRow = row
	r.hoverStartCol = startCol
	r.hoverEndCol = endCol
	r.hoverActive = true
}

// ClearHoverURL clears any active hover underline.
func (r *Renderer) ClearHoverURL() {
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

// drawRect draws a colored rectangle
func (r *Renderer) drawRect(x, y, w, h float32, clr [4]float32, proj [16]float32) {
	vertices := []float32{
		x, y,
		x + w, y,
		x + w, y + h,
		x, y,
		x + w, y + h,
		x, y + h,
	}

	gl.UseProgram(r.program)
	gl.UniformMatrix4fv(r.projLoc, 1, false, &proj[0])
	gl.Uniform4fv(r.colorLoc, 1, &clr[0])

	gl.BindVertexArray(r.quadVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.quadVBO)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(vertices)*4, gl.Ptr(vertices))
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
}

// drawChar draws a single character using the font atlas
func (r *Renderer) drawChar(x, y float32, char rune, clr [4]float32, proj [16]float32) {
	glyph, ok := r.glyphs[char]
	if !ok {
		// Fallback to '?' for unknown characters
		glyph, ok = r.glyphs['?']
		if !ok {
			return
		}
	}

	// Calculate screen coordinates
	w := float32(glyph.PixelWidth)
	h := float32(glyph.PixelHeight)

	// Texture coordinates
	tx := glyph.X
	ty := glyph.Y
	tw := glyph.Width
	th := glyph.Height

	vertices := []float32{
		x, y - h, tx, ty,
		x + w, y - h, tx + tw, ty,
		x + w, y, tx + tw, ty + th,
		x, y - h, tx, ty,
		x + w, y, tx + tw, ty + th,
		x, y, tx, ty + th,
	}

	gl.UseProgram(r.fontProgram)
	gl.UniformMatrix4fv(r.texProjLoc, 1, false, &proj[0])
	gl.Uniform4fv(r.texColorLoc, 1, &clr[0])
	gl.Uniform1i(r.texLoc, 0)

	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, r.fontAtlas)

	gl.BindVertexArray(r.fontVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.fontVBO)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(vertices)*4, gl.Ptr(vertices))
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
}

// drawText draws a string of text
func (r *Renderer) drawText(x, y float32, text string, clr [4]float32, proj [16]float32) {
	for _, char := range text {
		r.drawChar(x, y, char, clr, proj)
		x += r.cellWidth
	}
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
	glyph, ok := r.glyphs[char]
	if !ok {
		glyph, ok = r.glyphs['?']
		if !ok {
			return
		}
	}

	// Calculate screen coordinates with scale
	w := float32(glyph.PixelWidth) * scale
	h := float32(glyph.PixelHeight) * scale

	// Texture coordinates
	tx := glyph.X
	ty := glyph.Y
	tw := glyph.Width
	th := glyph.Height

	vertices := []float32{
		x, y - h, tx, ty,
		x + w, y - h, tx + tw, ty,
		x + w, y, tx + tw, ty + th,
		x, y - h, tx, ty,
		x + w, y, tx + tw, ty + th,
		x, y, tx, ty + th,
	}

	gl.UseProgram(r.fontProgram)
	gl.UniformMatrix4fv(r.texProjLoc, 1, false, &proj[0])
	gl.Uniform4fv(r.texColorLoc, 1, &clr[0])
	gl.Uniform1i(r.texLoc, 0)

	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, r.fontAtlas)

	gl.BindVertexArray(r.fontVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.fontVBO)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(vertices)*4, gl.Ptr(vertices))
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
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

// indexedColor returns the RGB color for an indexed color (0-255)
func indexedColor(index uint8) [4]float32 {
	// Standard 16 colors
	standard := [][4]float32{
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

	if index < 16 {
		return standard[index]
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

// TabBarWidth returns the tab bar width
func (r *Renderer) TabBarWidth() float32 {
	return r.tabBarWidth
}

// CalculateGridSize calculates the number of columns and rows that fit
func (r *Renderer) CalculateGridSize(width, height int) (cols, rows int) {
	availableWidth := float32(width) - r.tabBarWidth - 10
	availableHeight := float32(height) - r.paddingTop - r.paddingBottom
	cols = int(availableWidth / r.cellWidth)
	rows = int(availableHeight / r.cellHeight)
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return
}

// ChangeFont changes the current font by name
func (r *Renderer) ChangeFont(name string) error {
	fontData, ok := fonts.GetFont(name)
	if !ok {
		return fmt.Errorf("font '%s' not found", name)
	}

	// Delete old texture
	if r.fontAtlas != 0 {
		gl.DeleteTextures(1, &r.fontAtlas)
	}

	// Clear old glyphs
	r.glyphs = make(map[rune]Glyph)

	// Load new font
	if err := r.loadFontData(fontData); err != nil {
		return err
	}

	r.currentFont = name
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

	// Delete old texture
	if r.fontAtlas != 0 {
		gl.DeleteTextures(1, &r.fontAtlas)
	}

	// Clear old glyphs
	r.glyphs = make(map[rune]Glyph)

	// Reload font with new size
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
	gl.DeleteTextures(1, &r.fontAtlas)
}

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
		return 0, err
	}

	program := gl.CreateProgram()
	gl.AttachShader(program, vertexShader)
	gl.AttachShader(program, fragmentShader)
	gl.LinkProgram(program)

	var status int32
	gl.GetProgramiv(program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetProgramiv(program, gl.INFO_LOG_LENGTH, &logLength)
		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetProgramInfoLog(program, logLength, nil, gl.Str(log))
		return 0, fmt.Errorf("failed to link program: %v", log)
	}

	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

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
		return 0, fmt.Errorf("failed to compile shader: %v", log)
	}

	return shader, nil
}

// Ensure imports are used
var _ = color.White
var _ = draw.Draw
