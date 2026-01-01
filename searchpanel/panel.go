package searchpanel

import "strings"

type Mode int

const (
	ModeResults Mode = iota
	ModePreview
)

const (
	linesPerResult = 3
)

type Result struct {
	Title   string
	URL     string
	Snippet string
}

type Panel struct {
	Open             bool
	Enabled          bool
	Query            string
	LastQuery        string
	QueryDirty       bool
	Results          []Result
	Selected         int
	ResultsScroll    int
	Mode             Mode
	ProxyEnabled     bool
	Focused          bool
	PreviewTitle     string
	PreviewURL       string
	PreviewLines     []string
	PreviewWrapped   []string
	PreviewWrapChars int
	PreviewScroll    int
	Status           string
	Loading          bool
	SearchID         int
	PreviewID        int
}

type Layout struct {
	PanelX       float32
	PanelY       float32
	PanelWidth   float32
	PanelHeight  float32
	ContentX     float32
	ContentWidth float32
	LineHeight   float32
	HeaderY      float32
	InputLabelY  float32
	InputBoxY    float32
	StatusY      float32
	ResultsStart float32
	ResultsEnd   float32
	FooterY      float32
	VisibleLines int
}

func New() *Panel {
	return &Panel{
		Mode:     ModeResults,
		Selected: 0,
	}
}

func (p *Panel) Toggle() {
	p.Open = !p.Open
	if p.Open {
		p.Focused = true
	}
}

func (p *Panel) SetEnabled(enabled bool) {
	p.Enabled = enabled
	if !enabled {
		p.Open = false
	}
}

func (p *Panel) SetQuery(text string) {
	p.Query = text
	p.QueryDirty = p.Query != p.LastQuery
	if p.Mode == ModePreview {
		p.Mode = ModeResults
		p.PreviewLines = nil
		p.PreviewScroll = 0
	}
}

func (p *Panel) AppendQuery(char rune) {
	p.SetQuery(p.Query + string(char))
}

func (p *Panel) Backspace() {
	if p.Query == "" {
		return
	}
	runes := []rune(p.Query)
	p.SetQuery(string(runes[:len(runes)-1]))
}

func (p *Panel) ClearQuery() {
	p.SetQuery("")
}

func (p *Panel) SetResults(query string, results []Result, err error) {
	p.Loading = false
	if err != nil {
		p.Status = "Search failed"
		p.Results = nil
		p.Selected = 0
		p.ResultsScroll = 0
		return
	}

	p.Status = ""
	p.Results = results
	p.Selected = 0
	p.ResultsScroll = 0
	p.LastQuery = query
	p.QueryDirty = p.Query != p.LastQuery
}

func (p *Panel) SetPreview(url, title string, lines []string, err error) {
	p.Loading = false
	p.Mode = ModePreview
	p.PreviewScroll = 0
	p.PreviewWrapped = nil
	p.PreviewWrapChars = 0
	if err != nil {
		p.Status = "Preview failed"
		p.PreviewLines = []string{"Failed to load preview."}
		return
	}
	p.Status = ""
	p.PreviewURL = url
	p.PreviewTitle = title
	p.PreviewLines = lines
}

func (p *Panel) ResultCount() int {
	return len(p.Results)
}

func (p *Panel) ResultsTotalLines() int {
	return len(p.Results) * linesPerResult
}

func (p *Panel) LinesPerResult() int {
	return linesPerResult
}

func (p *Panel) MoveSelection(delta int, visibleLines int) {
	if len(p.Results) == 0 {
		return
	}
	p.Selected += delta
	if p.Selected < 0 {
		p.Selected = 0
	}
	if p.Selected >= len(p.Results) {
		p.Selected = len(p.Results) - 1
	}
	p.ensureSelectionVisible(visibleLines)
}

func (p *Panel) ScrollResults(delta int, visibleLines int) {
	if len(p.Results) == 0 {
		return
	}
	p.ResultsScroll += delta
	maxScroll := p.ResultsTotalLines() - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.ResultsScroll < 0 {
		p.ResultsScroll = 0
	}
	if p.ResultsScroll > maxScroll {
		p.ResultsScroll = maxScroll
	}
}

func (p *Panel) ScrollPreview(delta int, visibleLines int) {
	totalLines := len(p.PreviewLines)
	if len(p.PreviewWrapped) > 0 && p.PreviewWrapChars > 0 {
		totalLines = len(p.PreviewWrapped)
	}
	if totalLines == 0 {
		return
	}
	p.PreviewScroll += delta
	maxScroll := totalLines - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.PreviewScroll < 0 {
		p.PreviewScroll = 0
	}
	if p.PreviewScroll > maxScroll {
		p.PreviewScroll = maxScroll
	}
}

func (p *Panel) ensureSelectionVisible(visibleLines int) {
	if visibleLines <= 0 {
		return
	}
	startLine := p.Selected * linesPerResult
	endLine := startLine + linesPerResult - 1

	if startLine < p.ResultsScroll {
		p.ResultsScroll = startLine
	}
	if endLine >= p.ResultsScroll+visibleLines {
		p.ResultsScroll = endLine - visibleLines + 1
	}

	maxScroll := p.ResultsTotalLines() - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.ResultsScroll > maxScroll {
		p.ResultsScroll = maxScroll
	}
	if p.ResultsScroll < 0 {
		p.ResultsScroll = 0
	}
}

func (p *Panel) Layout(width, height int, cellWidth, cellHeight float32) Layout {
	panelWidth := float32(width) * 0.35
	minPanelWidth := float32(340)
	if cellWidth > 0 {
		wideMin := cellWidth * 32
		if wideMin > minPanelWidth {
			minPanelWidth = wideMin
		}
	}
	if panelWidth < minPanelWidth {
		panelWidth = minPanelWidth
	}
	if panelWidth > 560 {
		panelWidth = 560
	}
	maxWidth := float32(width) - 20
	if panelWidth > maxWidth {
		panelWidth = maxWidth
	}

	panelHeight := float32(height) - 30
	if panelHeight < 240 {
		panelHeight = 240
	}
	if panelHeight > float32(height)-20 {
		panelHeight = float32(height) - 20
	}

	panelX := float32(width) - panelWidth - 10
	panelY := float32(10)

	lineHeight := cellHeight * 1.35
	contentX := panelX + 18
	contentWidth := panelWidth - 36
	headerY := panelY + lineHeight*1.2
	inputLabelY := headerY + lineHeight*1.1
	inputBoxY := inputLabelY + lineHeight*0.35
	statusY := inputBoxY + lineHeight*1.3
	resultsStart := statusY + lineHeight*1.1
	footerY := panelY + panelHeight - lineHeight*0.6
	resultsEnd := footerY - lineHeight*1.2

	visibleLines := int((resultsEnd - resultsStart) / lineHeight)
	if visibleLines < 1 {
		visibleLines = 1
	}

	return Layout{
		PanelX:       panelX,
		PanelY:       panelY,
		PanelWidth:   panelWidth,
		PanelHeight:  panelHeight,
		ContentX:     contentX,
		ContentWidth: contentWidth,
		LineHeight:   lineHeight,
		HeaderY:      headerY,
		InputLabelY:  inputLabelY,
		InputBoxY:    inputBoxY,
		StatusY:      statusY,
		ResultsStart: resultsStart,
		ResultsEnd:   resultsEnd,
		FooterY:      footerY,
		VisibleLines: visibleLines,
	}
}

func (p *Panel) ResultTitle(idx int) string {
	if idx < 0 || idx >= len(p.Results) {
		return ""
	}
	return strings.TrimSpace(p.Results[idx].Title)
}
