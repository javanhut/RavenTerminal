package menu

import (
	"github.com/javanhut/RavenTerminal/config"
)

// MenuState represents the current menu state
type MenuState int

const (
	MenuClosed MenuState = iota
	MenuMain
	MenuShellSelect
	MenuPromptSettings
	MenuPromptStyle
	MenuScripts
	MenuCommands
	MenuAliases
)

// InputState tracks what we're currently inputting
type InputState int

const (
	InputNone InputState = iota
	// Command input states
	InputCommandName
	InputCommandValue
	InputCommandDesc
	// Alias input states
	InputAliasName
	InputAliasValue
	// Script input states
	InputScriptInit
	InputScriptPrePrompt
	InputScriptLangDetect
	InputScriptVCSDetect
)

// MenuItem represents a menu item
type MenuItem struct {
	Label    string
	Value    string
	Disabled bool
}

// Menu manages the configuration menu
type Menu struct {
	State         MenuState
	Config        *config.Config
	SelectedIndex int
	Items         []MenuItem
	ScrollOffset  int

	// Input handling - simplified
	InputActive bool
	InputState  InputState
	InputBuffer string
	InputLabel  string

	// Pending values for multi-step input
	PendingName string
	PendingCmd  string

	// Edit tracking
	EditingIndex int    // -1 for new, >= 0 for existing
	EditingName  string // For alias editing

	// Messages
	StatusMessage string
}

// NewMenu creates a new menu instance
func NewMenu() *Menu {
	cfg, _ := config.Load()
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return &Menu{
		State:        MenuClosed,
		Config:       cfg,
		EditingIndex: -1,
	}
}

// Open opens the menu
func (m *Menu) Open() {
	m.State = MenuMain
	m.SelectedIndex = 0
	m.ScrollOffset = 0
	m.InputActive = false
	m.InputState = InputNone
	m.StatusMessage = ""
	m.buildMainMenu()
}

// Close closes the menu
func (m *Menu) Close() {
	m.State = MenuClosed
	m.InputActive = false
	m.InputState = InputNone
	m.StatusMessage = ""
}

// IsOpen returns true if the menu is open
func (m *Menu) IsOpen() bool {
	return m.State != MenuClosed
}

// InputMode returns true if currently accepting text input
func (m *Menu) InputMode() bool {
	return m.InputActive
}

// GetInputPrompt returns the current input prompt
func (m *Menu) GetInputPrompt() string {
	return m.InputLabel
}

// GetInputBuffer returns the current input buffer
func (m *Menu) GetInputBuffer() string {
	return m.InputBuffer
}

// buildMainMenu builds the main menu items
func (m *Menu) buildMainMenu() {
	currentShell := m.Config.Shell.Path
	if currentShell == "" {
		currentShell = "(system default)"
	}

	promptStyle := m.Config.Prompt.Style
	if promptStyle == "" {
		promptStyle = "full"
	}

	sourceRC := "OFF"
	if m.Config.Shell.SourceRC {
		sourceRC = "ON"
	}

	m.Items = []MenuItem{
		{Label: "Shell: " + currentShell},
		{Label: "Source RC Files: " + sourceRC},
		{Label: "Prompt Style: " + promptStyle},
		{Label: "Prompt Options..."},
		{Label: "Scripts..."},
		{Label: "Commands (" + itoa(len(m.Config.Commands)) + ")..."},
		{Label: "Aliases (" + itoa(len(m.Config.Aliases)) + ")..."},
		{Label: ""},
		{Label: "Save and Close"},
		{Label: "Cancel"},
	}
}

// buildShellMenu builds the shell selection menu
func (m *Menu) buildShellMenu() {
	shells := config.GetAvailableShells()
	m.Items = []MenuItem{
		{Label: "(System Default)", Value: ""},
	}
	for _, shell := range shells {
		m.Items = append(m.Items, MenuItem{Label: shell, Value: shell})
	}
	m.Items = append(m.Items, MenuItem{Label: ""})
	m.Items = append(m.Items, MenuItem{Label: "Back"})
}

// buildPromptStyleMenu builds the prompt style selection menu
func (m *Menu) buildPromptStyleMenu() {
	styles := []string{"minimal", "simple", "full", "custom"}
	m.Items = []MenuItem{}
	for _, style := range styles {
		prefix := "  "
		if m.Config.Prompt.Style == style {
			prefix = "> "
		}
		m.Items = append(m.Items, MenuItem{Label: prefix + style, Value: style})
	}
	m.Items = append(m.Items, MenuItem{Label: ""})
	m.Items = append(m.Items, MenuItem{Label: "Back"})
}

// buildPromptSettingsMenu builds the prompt settings menu
func (m *Menu) buildPromptSettingsMenu() {
	p := m.Config.Prompt
	m.Items = []MenuItem{
		{Label: "Show Path: " + boolStr(p.ShowPath)},
		{Label: "Show Username: " + boolStr(p.ShowUsername)},
		{Label: "Show Hostname: " + boolStr(p.ShowHostname)},
		{Label: "Show Language: " + boolStr(p.ShowLanguage)},
		{Label: "Show VCS: " + boolStr(p.ShowVCS)},
		{Label: ""},
		{Label: "Back"},
	}
}

// buildScriptsMenu builds the scripts menu
func (m *Menu) buildScriptsMenu() {
	initStatus := scriptStatus(m.Config.Scripts.Init)
	prePromptStatus := scriptStatus(m.Config.Scripts.PrePrompt)
	langStatus := scriptStatus(m.Config.Scripts.LanguageDetect)
	vcsStatus := scriptStatus(m.Config.Scripts.VCSDetect)

	m.Items = []MenuItem{
		{Label: "Init Script: " + initStatus},
		{Label: "Pre-Prompt: " + prePromptStatus},
		{Label: "Language Detect: " + langStatus},
		{Label: "VCS Detect: " + vcsStatus},
		{Label: ""},
		{Label: "Back"},
	}
}

// buildCommandsMenu builds the commands menu
func (m *Menu) buildCommandsMenu() {
	m.Items = []MenuItem{
		{Label: "+ Add New Command"},
	}
	for i, cmd := range m.Config.Commands {
		m.Items = append(m.Items, MenuItem{
			Label: cmd.Name + " = " + truncate(cmd.Command, 25),
			Value: itoa(i),
		})
	}
	m.Items = append(m.Items, MenuItem{Label: ""})
	m.Items = append(m.Items, MenuItem{Label: "Back"})
}

// buildAliasesMenu builds the aliases menu
func (m *Menu) buildAliasesMenu() {
	m.Items = []MenuItem{
		{Label: "+ Add New Alias"},
	}
	for name, cmd := range m.Config.Aliases {
		m.Items = append(m.Items, MenuItem{
			Label: name + " = " + truncate(cmd, 25),
			Value: name,
		})
	}
	m.Items = append(m.Items, MenuItem{Label: ""})
	m.Items = append(m.Items, MenuItem{Label: "Back"})
}

// MoveUp moves selection up
func (m *Menu) MoveUp() {
	if m.InputActive {
		return
	}
	for {
		m.SelectedIndex--
		if m.SelectedIndex < 0 {
			m.SelectedIndex = len(m.Items) - 1
		}
		if m.Items[m.SelectedIndex].Label != "" {
			break
		}
	}
	m.adjustScroll()
}

// MoveDown moves selection down
func (m *Menu) MoveDown() {
	if m.InputActive {
		return
	}
	for {
		m.SelectedIndex++
		if m.SelectedIndex >= len(m.Items) {
			m.SelectedIndex = 0
		}
		if m.Items[m.SelectedIndex].Label != "" {
			break
		}
	}
	m.adjustScroll()
}

// adjustScroll adjusts scroll offset to keep selection visible
func (m *Menu) adjustScroll() {
	visibleItems := 12
	if m.SelectedIndex < m.ScrollOffset {
		m.ScrollOffset = m.SelectedIndex
	} else if m.SelectedIndex >= m.ScrollOffset+visibleItems {
		m.ScrollOffset = m.SelectedIndex - visibleItems + 1
	}
}

// Select handles selection of current item
func (m *Menu) Select() {
	if m.InputActive || m.SelectedIndex >= len(m.Items) {
		return
	}

	item := m.Items[m.SelectedIndex]
	if item.Label == "" {
		return
	}

	switch m.State {
	case MenuMain:
		m.handleMainSelect()
	case MenuShellSelect:
		m.handleShellSelect(item)
	case MenuPromptStyle:
		m.handlePromptStyleSelect(item)
	case MenuPromptSettings:
		m.handlePromptSettingsSelect()
	case MenuScripts:
		m.handleScriptsSelect()
	case MenuCommands:
		m.handleCommandsSelect(item)
	case MenuAliases:
		m.handleAliasesSelect(item)
	}
}

func (m *Menu) handleMainSelect() {
	switch m.SelectedIndex {
	case 0: // Shell
		m.State = MenuShellSelect
		m.SelectedIndex = 0
		m.buildShellMenu()
	case 1: // Source RC
		m.Config.Shell.SourceRC = !m.Config.Shell.SourceRC
		m.buildMainMenu()
		m.StatusMessage = "Updated (restart tab to apply)"
	case 2: // Prompt Style
		m.State = MenuPromptStyle
		m.SelectedIndex = 0
		m.buildPromptStyleMenu()
	case 3: // Prompt Options
		m.State = MenuPromptSettings
		m.SelectedIndex = 0
		m.buildPromptSettingsMenu()
	case 4: // Scripts
		m.State = MenuScripts
		m.SelectedIndex = 0
		m.buildScriptsMenu()
	case 5: // Commands
		m.State = MenuCommands
		m.SelectedIndex = 0
		m.buildCommandsMenu()
	case 6: // Aliases
		m.State = MenuAliases
		m.SelectedIndex = 0
		m.buildAliasesMenu()
	case 8: // Save and Close
		if err := m.Config.Save(); err != nil {
			m.StatusMessage = "Error: " + err.Error()
		} else {
			m.Close()
		}
	case 9: // Cancel
		m.Config, _ = config.Load()
		m.Close()
	}
}

func (m *Menu) handleShellSelect(item MenuItem) {
	if item.Label == "Back" {
		m.goBack()
		return
	}
	m.Config.Shell.Path = item.Value
	m.StatusMessage = "Shell updated (restart tab to apply)"
	m.goBack()
}

func (m *Menu) handlePromptStyleSelect(item MenuItem) {
	if item.Label == "Back" {
		m.goBack()
		return
	}
	if item.Value != "" {
		m.Config.Prompt.Style = item.Value
		m.StatusMessage = "Style updated (restart tab to apply)"
	}
	m.goBack()
}

func (m *Menu) handlePromptSettingsSelect() {
	switch m.SelectedIndex {
	case 0:
		m.Config.Prompt.ShowPath = !m.Config.Prompt.ShowPath
	case 1:
		m.Config.Prompt.ShowUsername = !m.Config.Prompt.ShowUsername
	case 2:
		m.Config.Prompt.ShowHostname = !m.Config.Prompt.ShowHostname
	case 3:
		m.Config.Prompt.ShowLanguage = !m.Config.Prompt.ShowLanguage
	case 4:
		m.Config.Prompt.ShowVCS = !m.Config.Prompt.ShowVCS
	case 6:
		m.goBack()
		return
	}
	m.buildPromptSettingsMenu()
	m.StatusMessage = "Updated (restart tab to apply)"
}

func (m *Menu) handleScriptsSelect() {
	switch m.SelectedIndex {
	case 0: // Init
		m.startInputWithValue(InputScriptInit, "Init script (\\n for newlines):", escapeNewlines(m.Config.Scripts.Init))
	case 1: // Pre-Prompt
		m.startInputWithValue(InputScriptPrePrompt, "Pre-prompt script:", escapeNewlines(m.Config.Scripts.PrePrompt))
	case 2: // Language Detect
		m.startInputWithValue(InputScriptLangDetect, "Language detect script:", escapeNewlines(m.Config.Scripts.LanguageDetect))
	case 3: // VCS Detect
		m.startInputWithValue(InputScriptVCSDetect, "VCS detect script:", escapeNewlines(m.Config.Scripts.VCSDetect))
	case 5:
		m.goBack()
	}
}

func (m *Menu) handleCommandsSelect(item MenuItem) {
	if item.Label == "Back" {
		m.goBack()
		return
	}
	if m.SelectedIndex == 0 { // Add new
		m.EditingIndex = -1
		m.PendingName = ""
		m.PendingCmd = ""
		m.startInputWithValue(InputCommandName, "Command name:", "")
	} else if item.Value != "" { // Edit existing
		idx := atoi(item.Value)
		if idx >= 0 && idx < len(m.Config.Commands) {
			m.EditingIndex = idx
			m.PendingName = m.Config.Commands[idx].Name
			m.PendingCmd = m.Config.Commands[idx].Command
			m.startInputWithValue(InputCommandName, "Command name:", m.PendingName)
		}
	}
}

func (m *Menu) handleAliasesSelect(item MenuItem) {
	if item.Label == "Back" {
		m.goBack()
		return
	}
	if m.SelectedIndex == 0 { // Add new
		m.EditingIndex = -1
		m.EditingName = ""
		m.PendingName = ""
		m.startInputWithValue(InputAliasName, "Alias name:", "")
	} else if item.Value != "" { // Edit existing
		m.EditingName = item.Value
		m.PendingName = item.Value
		m.startInputWithValue(InputAliasName, "Alias name:", item.Value)
	}
}

// startInput begins input mode with optional initial value
func (m *Menu) startInput(state InputState, label string) {
	m.InputActive = true
	m.InputState = state
	m.InputLabel = label
	// Don't clear InputBuffer here - caller may set it after
}

// startInputWithValue begins input mode with an initial value
func (m *Menu) startInputWithValue(state InputState, label string, initialValue string) {
	m.InputActive = true
	m.InputState = state
	m.InputLabel = label
	m.InputBuffer = initialValue
}

// HandleChar handles character input
func (m *Menu) HandleChar(char rune) {
	if !m.InputActive {
		return
	}
	m.InputBuffer += string(char)
}

// HandleBackspace handles backspace
func (m *Menu) HandleBackspace() {
	if !m.InputActive || len(m.InputBuffer) == 0 {
		return
	}
	// Remove last character (handle UTF-8)
	runes := []rune(m.InputBuffer)
	m.InputBuffer = string(runes[:len(runes)-1])
}

// HandleEnter handles enter key - returns true if menu should close
func (m *Menu) HandleEnter() bool {
	if !m.InputActive {
		return false
	}

	value := m.InputBuffer
	m.InputActive = false

	switch m.InputState {
	case InputCommandName:
		if value == "" {
			m.buildCommandsMenu()
			return false
		}
		m.PendingName = value
		initialCmd := ""
		if m.EditingIndex >= 0 {
			initialCmd = m.Config.Commands[m.EditingIndex].Command
		}
		m.startInputWithValue(InputCommandValue, "Command to run:", initialCmd)

	case InputCommandValue:
		if value == "" {
			m.buildCommandsMenu()
			return false
		}
		m.PendingCmd = value
		initialDesc := ""
		if m.EditingIndex >= 0 {
			initialDesc = m.Config.Commands[m.EditingIndex].Description
		}
		m.startInputWithValue(InputCommandDesc, "Description (optional):", initialDesc)

	case InputCommandDesc:
		if m.EditingIndex >= 0 {
			// Update existing
			m.Config.Commands[m.EditingIndex].Name = m.PendingName
			m.Config.Commands[m.EditingIndex].Command = m.PendingCmd
			m.Config.Commands[m.EditingIndex].Description = value
			m.StatusMessage = "Command updated"
		} else {
			// Add new
			m.Config.AddCustomCommand(m.PendingName, m.PendingCmd, value)
			m.StatusMessage = "Command added"
		}
		m.PendingName = ""
		m.PendingCmd = ""
		m.EditingIndex = -1
		m.buildCommandsMenu()

	case InputAliasName:
		if value == "" {
			m.buildAliasesMenu()
			return false
		}
		m.PendingName = value
		initialCmd := ""
		if m.EditingName != "" {
			initialCmd = m.Config.Aliases[m.EditingName]
		}
		m.startInputWithValue(InputAliasValue, "Command:", initialCmd)

	case InputAliasValue:
		if value == "" {
			m.buildAliasesMenu()
			return false
		}
		// If editing, remove old alias first
		if m.EditingName != "" && m.EditingName != m.PendingName {
			delete(m.Config.Aliases, m.EditingName)
		}
		m.Config.SetAlias(m.PendingName, value)
		m.StatusMessage = "Alias saved"
		m.PendingName = ""
		m.EditingName = ""
		m.EditingIndex = -1
		m.buildAliasesMenu()

	case InputScriptInit:
		m.Config.Scripts.Init = unescapeNewlines(value)
		m.StatusMessage = "Script updated"
		m.buildScriptsMenu()

	case InputScriptPrePrompt:
		m.Config.Scripts.PrePrompt = unescapeNewlines(value)
		m.StatusMessage = "Script updated"
		m.buildScriptsMenu()

	case InputScriptLangDetect:
		m.Config.Scripts.LanguageDetect = unescapeNewlines(value)
		m.StatusMessage = "Script updated"
		m.buildScriptsMenu()

	case InputScriptVCSDetect:
		m.Config.Scripts.VCSDetect = unescapeNewlines(value)
		m.StatusMessage = "Script updated"
		m.buildScriptsMenu()
	}

	m.InputState = InputNone
	return false
}

// HandleEscape handles escape key
func (m *Menu) HandleEscape() {
	if m.InputActive {
		m.InputActive = false
		m.InputState = InputNone
		m.InputBuffer = ""
		// Rebuild current menu
		switch m.State {
		case MenuCommands:
			m.buildCommandsMenu()
		case MenuAliases:
			m.buildAliasesMenu()
		case MenuScripts:
			m.buildScriptsMenu()
		}
		return
	}
	m.goBack()
}

// HandleDelete handles delete key for removing items
func (m *Menu) HandleDelete() {
	if m.InputActive {
		return
	}

	switch m.State {
	case MenuCommands:
		if m.SelectedIndex > 0 && m.SelectedIndex <= len(m.Config.Commands) {
			idx := m.SelectedIndex - 1 // Offset for "Add New" item
			m.Config.RemoveCustomCommand(idx)
			m.StatusMessage = "Command deleted"
			m.buildCommandsMenu()
			if m.SelectedIndex >= len(m.Items) {
				m.SelectedIndex = len(m.Items) - 1
			}
		}
	case MenuAliases:
		if m.SelectedIndex > 0 {
			item := m.Items[m.SelectedIndex]
			if item.Value != "" {
				m.Config.RemoveAlias(item.Value)
				m.StatusMessage = "Alias deleted"
				m.buildAliasesMenu()
				if m.SelectedIndex >= len(m.Items) {
					m.SelectedIndex = len(m.Items) - 1
				}
			}
		}
	}
}

// goBack goes back to previous menu
func (m *Menu) goBack() {
	switch m.State {
	case MenuShellSelect, MenuPromptStyle, MenuPromptSettings, MenuScripts, MenuCommands, MenuAliases:
		m.State = MenuMain
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildMainMenu()
	default:
		m.Close()
	}
}

// GetTitle returns the current menu title
func (m *Menu) GetTitle() string {
	switch m.State {
	case MenuMain:
		return "Settings"
	case MenuShellSelect:
		return "Select Shell"
	case MenuPromptStyle:
		return "Prompt Style"
	case MenuPromptSettings:
		return "Prompt Options"
	case MenuScripts:
		return "Scripts"
	case MenuCommands:
		return "Commands"
	case MenuAliases:
		return "Aliases"
	default:
		return "Settings"
	}
}

// Helper functions

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	result := ""
	negative := i < 0
	if negative {
		i = -i
	}
	for i > 0 {
		result = string(rune('0'+i%10)) + result
		i /= 10
	}
	if negative {
		result = "-" + result
	}
	return result
}

func atoi(s string) int {
	result := 0
	negative := false
	for i, c := range s {
		if i == 0 && c == '-' {
			negative = true
			continue
		}
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		} else {
			break
		}
	}
	if negative {
		return -result
	}
	return result
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func boolStr(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func scriptStatus(s string) string {
	if s == "" {
		return "(not set)"
	}
	// Count lines
	lines := 1
	for _, c := range s {
		if c == '\n' {
			lines++
		}
	}
	if lines == 1 {
		return truncate(s, 20)
	}
	return itoa(lines) + " lines"
}

func escapeNewlines(s string) string {
	result := ""
	for _, c := range s {
		if c == '\n' {
			result += "\\n"
		} else {
			result += string(c)
		}
	}
	return result
}

func unescapeNewlines(s string) string {
	result := ""
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '\\' && s[i+1] == 'n' {
			result += "\n"
			i += 2
		} else {
			result += string(s[i])
			i++
		}
	}
	return result
}
