package aipanel

import (
	"fmt"
	"strings"
	"time"
)

// Spinner frames for loading animation
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerFrameRate = 100 * time.Millisecond

type Message struct {
	Role     string
	Content  string
	Thinking string // Thinking/reasoning content (for thinking models)
}

type WrappedLine struct {
	Role       string
	Text       string
	InCode     bool // Whether this line is inside a code block
	IsHeader   bool // Whether this is a header line
	IsBullet   bool // Whether this is a bullet point
	IsThinking bool // Whether this line is thinking content
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
	WasAtBottom  bool // Track if user was at bottom before new content
	WrapChars    int
	WrappedLines []WrappedLine
	RequestID    int
	ModelLoaded  bool
	LoadedURL    string
	LoadedModel  string
	LoadingStart time.Time

	// Thinking model support
	ShowThinking     bool // Whether to show thinking content
	ThinkingExpanded bool // Whether thinking sections are expanded
	ThinkingMode     bool // Whether thinking mode is enabled for requests

	// Multiline input support
	InputCursorPos int      // Cursor position in input string
	InputScroll    int      // Scroll offset for input area (in lines)
	InputLines     []string // Wrapped input lines for display
	InputWrapChars int      // Characters per line for wrapping
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
	InputBoxH     float32 // Height of input box (multiline)
	InputLines    int     // Number of visible lines in input box
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
	p.ThinkingExpanded = false
}

// ToggleThinkingExpanded toggles the expanded state of thinking content
func (p *Panel) ToggleThinkingExpanded() {
	p.ThinkingExpanded = !p.ThinkingExpanded
}

func (p *Panel) SetInput(text string) {
	p.Input = text
	p.InputCursorPos = len(text)
	p.updateInputLines()
}

func (p *Panel) AppendInput(char rune) {
	p.Input += string(char)
	p.InputCursorPos = len(p.Input)
	p.updateInputLines()
}

func (p *Panel) AppendNewline() {
	p.Input += "\n"
	p.InputCursorPos = len(p.Input)
	p.updateInputLines()
}

func (p *Panel) Backspace() {
	if p.Input == "" {
		return
	}
	runes := []rune(p.Input)
	p.Input = string(runes[:len(runes)-1])
	p.InputCursorPos = len(p.Input)
	p.updateInputLines()
}

func (p *Panel) ClearInput() {
	p.Input = ""
	p.InputCursorPos = 0
	p.InputScroll = 0
	p.InputLines = nil
}

// updateInputLines wraps the input text for display
func (p *Panel) updateInputLines() {
	if p.InputWrapChars <= 0 {
		p.InputWrapChars = 40 // Default
	}
	p.InputLines = wrapInputText(p.Input, p.InputWrapChars)
}

// WrapInput wraps input text and updates scroll position
func (p *Panel) WrapInput(maxChars int) []string {
	p.InputWrapChars = maxChars
	p.InputLines = wrapInputText(p.Input, maxChars)
	return p.InputLines
}

// ScrollInputUp scrolls the input area up
func (p *Panel) ScrollInputUp() {
	if p.InputScroll > 0 {
		p.InputScroll--
	}
}

// ScrollInputDown scrolls the input area down
func (p *Panel) ScrollInputDown(visibleLines int) {
	maxScroll := len(p.InputLines) - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.InputScroll < maxScroll {
		p.InputScroll++
	}
}

// EnsureInputCursorVisible scrolls to make the cursor visible
func (p *Panel) EnsureInputCursorVisible(visibleLines int) {
	if len(p.InputLines) == 0 {
		p.InputScroll = 0
		return
	}

	// Find which line the cursor is on
	cursorLine := len(p.InputLines) - 1 // Default to last line

	// Ensure cursor line is visible
	if cursorLine < p.InputScroll {
		p.InputScroll = cursorLine
	} else if cursorLine >= p.InputScroll+visibleLines {
		p.InputScroll = cursorLine - visibleLines + 1
	}

	maxScroll := len(p.InputLines) - visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.InputScroll > maxScroll {
		p.InputScroll = maxScroll
	}
}

// wrapInputText wraps text for the input area, preserving newlines
func wrapInputText(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = 40
	}

	var result []string
	lines := strings.Split(text, "\n")

	for _, line := range lines {
		if line == "" {
			result = append(result, "")
			continue
		}

		// Wrap long lines
		for len(line) > 0 {
			if len(line) <= maxChars {
				result = append(result, line)
				break
			}

			// Find a good break point
			breakAt := maxChars
			// Try to break at a space
			for i := maxChars - 1; i > maxChars/2; i-- {
				if line[i] == ' ' {
					breakAt = i + 1
					break
				}
			}

			result = append(result, line[:breakAt])
			line = line[breakAt:]
		}
	}

	if len(result) == 0 {
		result = append(result, "")
	}

	return result
}

func (p *Panel) AddMessage(role, content string) {
	cleaned := strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if cleaned == "" {
		return
	}
	p.Messages = append(p.Messages, Message{Role: role, Content: cleaned})
	p.AutoScroll = true
}

// AddMessageWithThinking adds a message with both content and thinking
func (p *Panel) AddMessageWithThinking(role, content, thinking string) {
	cleaned := strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	thinkingCleaned := strings.TrimSpace(strings.ReplaceAll(thinking, "\r\n", "\n"))
	if cleaned == "" && thinkingCleaned == "" {
		return
	}
	p.Messages = append(p.Messages, Message{
		Role:     role,
		Content:  cleaned,
		Thinking: thinkingCleaned,
	})
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

// StartLoading marks the panel as loading with timestamp
func (p *Panel) StartLoading() {
	p.Loading = true
	p.LoadingStart = time.Now()
}

// SpinnerFrame returns the current spinner character based on elapsed time
func (p *Panel) SpinnerFrame() string {
	if !p.Loading {
		return ""
	}
	elapsed := time.Since(p.LoadingStart)
	frameCount := int(elapsed / spinnerFrameRate)
	return spinnerFrames[frameCount%len(spinnerFrames)]
}

// GetLastAssistantMessage returns the last assistant message content
func (p *Panel) GetLastAssistantMessage() string {
	for i := len(p.Messages) - 1; i >= 0; i-- {
		if p.Messages[i].Role == "assistant" {
			return p.Messages[i].Content
		}
	}
	return ""
}

// IsAtBottom returns true if scroll is at the bottom of content
func (p *Panel) IsAtBottom(visibleLines int) bool {
	if len(p.WrappedLines) <= visibleLines {
		return true
	}
	maxScroll := len(p.WrappedLines) - visibleLines
	return p.Scroll >= maxScroll-1
}

// SaveScrollPosition saves whether user is at bottom before content changes
func (p *Panel) SaveScrollPosition(visibleLines int) {
	p.WasAtBottom = p.IsAtBottom(visibleLines)
}

// RestoreScrollPosition scrolls to bottom if user was at bottom before content changed
func (p *Panel) RestoreScrollPosition(visibleLines int) {
	if p.AutoScroll || p.WasAtBottom {
		maxScroll := len(p.WrappedLines) - visibleLines
		if maxScroll < 0 {
			maxScroll = 0
		}
		p.Scroll = maxScroll
		p.AutoScroll = false
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
	statusY := headerY + lineHeight*1.1
	footerY := panelY + panelHeight - lineHeight*0.6

	// Multiline input box - 4 lines tall
	inputVisibleLines := 4
	inputBoxH := lineHeight * float32(inputVisibleLines)
	inputLabelY := footerY - inputBoxH - lineHeight*1.0
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
		InputBoxH:     inputBoxH,
		InputLines:    inputVisibleLines,
		FooterY:       footerY,
		VisibleLines:  visibleLines,
	}
}

func BuildWrappedLines(messages []Message, maxChars int) []WrappedLine {
	return BuildWrappedLinesWithThinking(messages, maxChars, true, false)
}

// BuildWrappedLinesWithThinking builds wrapped lines with optional thinking content
func BuildWrappedLinesWithThinking(messages []Message, maxChars int, showThinking, expanded bool) []WrappedLine {
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

		// Render thinking content first if present and enabled
		if showThinking && message.Thinking != "" {
			thinkingLines := buildThinkingLines(message.Thinking, maxChars, expanded)
			lines = append(lines, thinkingLines...)
			if len(thinkingLines) > 0 {
				lines = append(lines, WrappedLine{Role: "", Text: ""})
			}
		}

		// Split content by lines first to handle code blocks
		contentLines := strings.Split(message.Content, "\n")
		inCode := false
		isFirstLine := true

		for _, contentLine := range contentLines {
			trimmed := strings.TrimSpace(contentLine)

			// Toggle code block state
			if strings.HasPrefix(trimmed, "```") {
				inCode = !inCode
				continue // Skip the ``` markers entirely
			}

			linePrefix := indent
			if isFirstLine {
				linePrefix = prefix
				isFirstLine = false
			}

			if inCode {
				// In code block: preserve line as-is (with indent only)
				codeLine := linePrefix + contentLine
				if len(codeLine) > maxChars {
					codeLine = codeLine[:maxChars-3] + "..."
				}
				lines = append(lines, WrappedLine{Role: role, Text: codeLine, InCode: true})
				continue
			}

			// Skip empty lines
			if trimmed == "" {
				lines = append(lines, WrappedLine{Role: role, Text: "", InCode: false})
				continue
			}

			// Skip table separators
			if strings.HasPrefix(trimmed, "|--") || strings.HasPrefix(trimmed, "| --") ||
				strings.HasPrefix(trimmed, "|:") || strings.HasPrefix(trimmed, "| :") {
				continue
			}

			// Handle headers
			if strings.HasPrefix(trimmed, "#") {
				level := 0
				for level < len(trimmed) && trimmed[level] == '#' {
					level++
				}
				headerText := strings.TrimSpace(trimmed[level:])
				headerText = stripMarkdownFormatting(headerText)
				if headerText != "" {
					headerPrefix := strings.Repeat("=", min(level, 3)) + " "
					text := linePrefix + headerPrefix + headerText
					if len(text) > maxChars {
						text = text[:maxChars-3] + "..."
					}
					lines = append(lines, WrappedLine{Role: role, Text: text, IsHeader: true})
				}
				continue
			}

			// Handle bullet points
			isBullet := false
			bulletPrefix := ""
			text := trimmed
			if strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "+ ") {
				isBullet = true
				bulletPrefix = "• "
				text = strings.TrimSpace(trimmed[2:])
			} else if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
				// Numbered list
				dotIdx := strings.Index(trimmed, ".")
				if dotIdx > 0 && dotIdx < 4 {
					isBullet = true
					bulletPrefix = trimmed[:dotIdx+1] + " "
					text = strings.TrimSpace(trimmed[dotIdx+1:])
				}
			}

			// Handle table rows - convert to readable format
			if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
				cells := strings.Split(trimmed, "|")
				var cellTexts []string
				for _, cell := range cells {
					cell = strings.TrimSpace(cell)
					if cell != "" {
						cellTexts = append(cellTexts, stripMarkdownFormatting(cell))
					}
				}
				if len(cellTexts) > 0 {
					text = strings.Join(cellTexts, " | ")
					wrapped := wrapText(text, maxChars, linePrefix, indent)
					for _, wline := range wrapped {
						lines = append(lines, WrappedLine{Role: role, Text: wline, InCode: false})
					}
				}
				continue
			}

			// Strip markdown formatting from regular text
			text = stripMarkdownFormatting(text)

			// Wrap the text
			fullPrefix := linePrefix
			if isBullet {
				fullPrefix = linePrefix + bulletPrefix
			}
			bulletIndent := indent + strings.Repeat(" ", len(bulletPrefix))

			wrapped := wrapText(text, maxChars, fullPrefix, bulletIndent)
			for _, wline := range wrapped {
				lines = append(lines, WrappedLine{Role: role, Text: wline, IsBullet: isBullet})
			}
		}

		if i < len(messages)-1 {
			lines = append(lines, WrappedLine{Role: "", Text: ""})
		}
	}
	return lines
}

// stripMarkdownFormatting removes markdown formatting from text
func stripMarkdownFormatting(text string) string {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

// buildThinkingLines formats thinking content for display
func buildThinkingLines(thinking string, maxChars int, expanded bool) []WrappedLine {
	var lines []WrappedLine

	if thinking == "" {
		return lines
	}

	// Count thinking lines for summary
	thinkingContentLines := strings.Split(thinking, "\n")
	lineCount := 0
	for _, l := range thinkingContentLines {
		if strings.TrimSpace(l) != "" {
			lineCount++
		}
	}

	if !expanded {
		// Collapsed view - just show a summary line
		summary := "💭 Thinking"
		if lineCount > 0 {
			summary = fmt.Sprintf("💭 Thinking (%d lines) [Ctrl+T to expand]", lineCount)
		}
		lines = append(lines, WrappedLine{
			Role:       "thinking",
			Text:       summary,
			IsThinking: true,
			IsHeader:   true,
		})
		return lines
	}

	// Expanded view - show header and content
	lines = append(lines, WrappedLine{
		Role:       "thinking",
		Text:       "💭 Thinking [Ctrl+T to collapse]",
		IsThinking: true,
		IsHeader:   true,
	})
	lines = append(lines, WrappedLine{
		Role:       "thinking",
		Text:       strings.Repeat("─", min(maxChars-4, 40)),
		IsThinking: true,
	})

	// Process thinking content
	thinkingPrefix := "  "
	thinkingIndent := "  "

	for _, contentLine := range thinkingContentLines {
		trimmed := strings.TrimSpace(contentLine)
		if trimmed == "" {
			lines = append(lines, WrappedLine{Role: "thinking", Text: "", IsThinking: true})
			continue
		}

		// Strip any markdown formatting
		text := stripMarkdownFormatting(trimmed)

		// Wrap the text
		wrapped := wrapText(text, maxChars-4, thinkingPrefix, thinkingIndent)
		for _, wline := range wrapped {
			lines = append(lines, WrappedLine{
				Role:       "thinking",
				Text:       wline,
				IsThinking: true,
			})
		}
	}

	// Add closing line
	lines = append(lines, WrappedLine{
		Role:       "thinking",
		Text:       strings.Repeat("─", min(maxChars-4, 40)),
		IsThinking: true,
	})

	return lines
}

// HasThinkingContent returns true if any message has thinking content
func HasThinkingContent(messages []Message) bool {
	for _, m := range messages {
		if m.Thinking != "" {
			return true
		}
	}
	return false
}
