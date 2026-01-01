package aipanel

import "strings"

type Message struct {
	Role    string
	Content string
}

type WrappedLine struct {
	Role string
	Text string
}

type Panel struct {
	Open         bool
	Enabled      bool
	Focused      bool
	Input        string
	Status       string
	Loading      bool
	Messages     []Message
	Scroll       int
	AutoScroll   bool
	WrapChars    int
	WrappedLines []WrappedLine
	RequestID    int
	ModelLoaded  bool
	LoadedURL    string
	LoadedModel  string
}

type Layout struct {
	PanelX        float32
	PanelY        float32
	PanelWidth    float32
	PanelHeight   float32
	ContentX      float32
	ContentWidth  float32
	LineHeight    float32
	HeaderY       float32
	StatusY       float32
	MessagesStart float32
	MessagesEnd   float32
	InputLabelY   float32
	InputBoxY     float32
	FooterY       float32
	VisibleLines  int
}

func New() *Panel {
	return &Panel{}
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
		p.Reset()
	}
}

func (p *Panel) Reset() {
	p.Input = ""
	p.Status = ""
	p.Loading = false
	p.Messages = nil
	p.Scroll = 0
	p.AutoScroll = false
	p.WrapChars = 0
	p.WrappedLines = nil
	p.RequestID = 0
	p.ModelLoaded = false
	p.LoadedURL = ""
	p.LoadedModel = ""
}

func (p *Panel) SetInput(text string) {
	p.Input = text
}

func (p *Panel) AppendInput(char rune) {
	p.Input += string(char)
}

func (p *Panel) Backspace() {
	if p.Input == "" {
		return
	}
	runes := []rune(p.Input)
	p.Input = string(runes[:len(runes)-1])
}

func (p *Panel) ClearInput() {
	p.Input = ""
}

func (p *Panel) AddMessage(role, content string) {
	cleaned := strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if cleaned == "" {
		return
	}
	p.Messages = append(p.Messages, Message{Role: role, Content: cleaned})
	p.AutoScroll = true
}

// AppendToLastMessage appends content to the last message if it matches the given role.
// If no message exists or the last message is a different role, creates a new message.
func (p *Panel) AppendToLastMessage(role, content string) {
	if len(p.Messages) > 0 && p.Messages[len(p.Messages)-1].Role == role {
		p.Messages[len(p.Messages)-1].Content += content
	} else {
		p.Messages = append(p.Messages, Message{Role: role, Content: content})
	}
	p.AutoScroll = true
}

func (p *Panel) TrimMessages(maxMessages int) {
	if maxMessages <= 0 {
		return
	}
	if len(p.Messages) <= maxMessages {
		return
	}
	p.Messages = append([]Message{}, p.Messages[len(p.Messages)-maxMessages:]...)
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
	statusY := headerY + lineHeight*1.1
	footerY := panelY + panelHeight - lineHeight*0.6
	inputLabelY := footerY - lineHeight*1.9
	inputBoxY := inputLabelY + lineHeight*0.35
	messagesStart := statusY + lineHeight*1.0
	messagesEnd := inputLabelY - lineHeight*0.6

	visibleLines := int((messagesEnd - messagesStart) / lineHeight)
	if visibleLines < 1 {
		visibleLines = 1
	}

	return Layout{
		PanelX:        panelX,
		PanelY:        panelY,
		PanelWidth:    panelWidth,
		PanelHeight:   panelHeight,
		ContentX:      contentX,
		ContentWidth:  contentWidth,
		LineHeight:    lineHeight,
		HeaderY:       headerY,
		StatusY:       statusY,
		MessagesStart: messagesStart,
		MessagesEnd:   messagesEnd,
		InputLabelY:   inputLabelY,
		InputBoxY:     inputBoxY,
		FooterY:       footerY,
		VisibleLines:  visibleLines,
	}
}

func BuildWrappedLines(messages []Message, maxChars int) []WrappedLine {
	lines := []WrappedLine{}
	for i, message := range messages {
		role := strings.TrimSpace(message.Role)
		prefix := "AI: "
		if role == "user" {
			prefix = "You: "
		} else if role == "error" {
			prefix = "Error: "
		} else if role != "" && role != "assistant" {
			prefix = role + ": "
		}
		indent := strings.Repeat(" ", len(prefix))
		content := strings.ReplaceAll(message.Content, "\n", " ")
		wrapped := wrapText(content, maxChars, prefix, indent)
		for _, line := range wrapped {
			lines = append(lines, WrappedLine{Role: role, Text: line})
		}
		if i < len(messages)-1 {
			lines = append(lines, WrappedLine{Role: "", Text: ""})
		}
	}
	return lines
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
