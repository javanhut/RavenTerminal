package menu

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/javanhut/RavenTerminal/config"
)

var debugMenu = os.Getenv("RAVEN_DEBUG_MENU") == "1"

// MenuState represents the current menu state
type MenuState int

const (
	MenuClosed MenuState = iota
	MenuMain
	MenuShellSelect
	MenuThemeSelect
	MenuPromptSettings
	MenuPromptStyle
	MenuScripts
	MenuOllamaModels
	MenuCommands
	MenuAliases
	MenuExports
	MenuConfirmCommand
	MenuConfirmAlias
	MenuConfirmExport
	MenuConfirmDelete  // Confirmation before deleting items
	MenuCursorStyle    // Cursor style selection
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
	// Export input states
	InputExportName
	InputExportValue
	// Script input states
	InputScriptInit
	InputScriptPrePrompt
	InputScriptLangDetect
	InputScriptVCSDetect
	// Ollama input states
	InputOllamaURL
	InputOllamaModel
	// Font size input state
	InputFontSize
	// Panel width input state
	InputPanelWidth
)

// MenuItem represents a menu item
type MenuItem struct {
	Label    string
	Value    string
	Disabled bool
	IsHeader bool   // Section header (non-selectable, styled differently)
	IsToggle bool   // Toggle item (shows checkbox indicator)
	Toggled  bool   // Current toggle state
}

// Menu manages the configuration menu
type Menu struct {
	State         MenuState
	Config        *config.Config
	SelectedIndex int
	Items         []MenuItem
	ScrollOffset  int
	OllamaModels  []string

	// Position memory - preserve selection when navigating between menus
	savedIndex  map[MenuState]int
	savedScroll map[MenuState]int

	// Input handling - simplified
	InputActive bool
	InputState  InputState
	InputBuffer string
	InputLabel  string

	// Pending values for multi-step input
	PendingName     string
	PendingCmd      string
	PendingDesc     string
	PendingAliasCmd string
	PendingExport   string

	// Edit tracking
	EditingIndex      int    // -1 for new, >= 0 for existing
	EditingName       string // For alias editing
	EditingExportName string

	// Delete confirmation tracking
	DeleteType   string // "command", "alias", or "export"
	DeleteTarget string // Name or index of item to delete
	DeleteIndex  int    // Index for commands

	// Messages
	StatusMessage string

	// Optional hook for applying config without closing the menu
	OnConfigReload func(cfg *config.Config) error
	// Optional hook for applying updated init script to the active shell
	OnInitScriptUpdated func(initPath string) error
	// Optional hook for testing Ollama connectivity.
	OnOllamaTest func(url string) error
	// Optional hook for fetching Ollama models.
	OnOllamaFetchModels func(url string) ([]string, error)
	// Optional hook for pre-loading an Ollama model into memory.
	OnOllamaLoadModel func(url, model string)
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
		savedIndex:   make(map[MenuState]int),
		savedScroll:  make(map[MenuState]int),
	}
}

// savePosition stores the current position for the current state
func (m *Menu) savePosition() {
	m.savedIndex[m.State] = m.SelectedIndex
	m.savedScroll[m.State] = m.ScrollOffset
}

// restorePosition restores position for the given state, or resets to 0
func (m *Menu) restorePosition(state MenuState) {
	if idx, ok := m.savedIndex[state]; ok {
		m.SelectedIndex = idx
	} else {
		m.SelectedIndex = 0
	}
	if scroll, ok := m.savedScroll[state]; ok {
		m.ScrollOffset = scroll
	} else {
		m.ScrollOffset = 0
	}
}

// navigateTo transitions to a new menu state, saving current position
func (m *Menu) navigateTo(newState MenuState, buildFunc func()) {
	m.savePosition()
	m.State = newState
	buildFunc()
	m.restorePosition(newState)
	// Ensure selection is valid after rebuild
	if m.SelectedIndex >= len(m.Items) {
		m.SelectedIndex = 0
	}
	// Skip to first navigable item if current is not navigable
	if !m.isNavigable(m.SelectedIndex) {
		for i := 0; i < len(m.Items); i++ {
			if m.isNavigable(i) {
				m.SelectedIndex = i
				break
			}
		}
	}
	m.adjustScroll()
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
	m.debugf("open state=%s", m.stateName())
}

// Close closes the menu
func (m *Menu) Close() {
	m.State = MenuClosed
	m.InputActive = false
	m.InputState = InputNone
	m.StatusMessage = ""
	m.debugf("close")
}

// IsOpen returns true if the menu is open
func (m *Menu) IsOpen() bool {
	return m.State != MenuClosed
}

// InputMode returns true if currently accepting text input
func (m *Menu) InputMode() bool {
	return m.InputActive
}

// InputIsMultiline returns true when the active input supports newlines.
func (m *Menu) InputIsMultiline() bool {
	switch m.InputState {
	case InputScriptInit, InputScriptPrePrompt, InputScriptLangDetect, InputScriptVCSDetect:
		return true
	default:
		return false
	}
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

	themeLabel := config.ThemeLabel(m.Config.Theme)
	promptStyle := m.Config.Prompt.Style
	if promptStyle == "" {
		promptStyle = "full"
	}

	ollamaURL := m.Config.Ollama.URL
	if ollamaURL == "" {
		ollamaURL = "(not set)"
	}
	ollamaModel := m.Config.Ollama.Model
	if ollamaModel == "" {
		ollamaModel = "(not set)"
	}

	// Get appearance values with defaults
	cursorStyle := m.Config.Appearance.CursorStyle
	if cursorStyle == "" {
		cursorStyle = "block"
	}
	panelWidth := m.Config.Appearance.PanelWidthPercent
	if panelWidth == 0 {
		panelWidth = 35.0
	}

	m.Items = []MenuItem{
		// Shell & Environment
		{Label: "SHELL & ENVIRONMENT", IsHeader: true},
		{Label: "Shell: " + currentShell},
		{Label: "Source RC Files", IsToggle: true, Toggled: m.Config.Shell.SourceRC},
		{Label: "Scripts..."},
		{Label: "Commands (" + itoa(len(m.Config.Commands)) + ")..."},
		{Label: "Aliases (" + itoa(len(m.Config.Aliases)) + ")..."},
		{Label: "Exports (" + itoa(len(m.Config.Exports)) + ")..."},
		// Appearance
		{Label: "APPEARANCE", IsHeader: true},
		{Label: "Theme: " + themeLabel},
		{Label: "Font Size: " + formatFloat(m.Config.FontSize)},
		{Label: "Cursor Style: " + cursorStyle},
		{Label: "Cursor Blink", IsToggle: true, Toggled: m.Config.Appearance.CursorBlink},
		{Label: "Panel Width: " + formatFloat(panelWidth) + "%"},
		{Label: "Prompt Style: " + promptStyle},
		{Label: "Prompt Options..."},
		// AI Features
		{Label: "AI FEATURES", IsHeader: true},
		{Label: "Web Search", IsToggle: true, Toggled: m.Config.WebSearch.Enabled},
		{Label: "Reader Proxy", IsToggle: true, Toggled: m.Config.WebSearch.UseReaderProxy},
		{Label: "Ollama Chat", IsToggle: true, Toggled: m.Config.Ollama.Enabled},
		{Label: "Ollama URL: " + truncate(ollamaURL, 25)},
		{Label: "Ollama Model: " + truncate(ollamaModel, 25)},
		{Label: "Test Ollama Connection"},
		{Label: "Refresh Ollama Models"},
		{Label: "Ollama Models..."},
		{Label: "Thinking Mode", IsToggle: true, Toggled: m.Config.Ollama.ThinkingMode},
		{Label: "Show Thinking", IsToggle: true, Toggled: m.Config.Ollama.ShowThinking},
		// Actions
		{Label: "ACTIONS", IsHeader: true},
		{Label: "Reload Config"},
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

// buildThemeMenu builds the theme selection menu
func (m *Menu) buildThemeMenu() {
	options := config.ThemeOptions()
	m.Items = []MenuItem{}
	for _, opt := range options {
		prefix := "  "
		if m.Config.Theme == opt.Name {
			prefix = "> "
		}
		m.Items = append(m.Items, MenuItem{Label: prefix + opt.Label, Value: opt.Name})
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

// buildCursorStyleMenu builds the cursor style selection menu
func (m *Menu) buildCursorStyleMenu() {
	styles := []string{"block", "underline", "bar"}
	currentStyle := m.Config.Appearance.CursorStyle
	if currentStyle == "" {
		currentStyle = "block"
	}
	m.Items = []MenuItem{}
	for _, style := range styles {
		prefix := "  "
		if currentStyle == style {
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
		{Label: "Show Path", IsToggle: true, Toggled: p.ShowPath},
		{Label: "Show Username", IsToggle: true, Toggled: p.ShowUsername},
		{Label: "Show Hostname", IsToggle: true, Toggled: p.ShowHostname},
		{Label: "Show Language", IsToggle: true, Toggled: p.ShowLanguage},
		{Label: "Show VCS", IsToggle: true, Toggled: p.ShowVCS},
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

// buildExportsMenu builds the exports menu
func (m *Menu) buildExportsMenu() {
	m.Items = []MenuItem{
		{Label: "+ Add New Export"},
	}
	for name, value := range m.Config.Exports {
		m.Items = append(m.Items, MenuItem{
			Label: name + " = " + truncate(value, 25),
			Value: name,
		})
	}
	m.Items = append(m.Items, MenuItem{Label: ""})
	m.Items = append(m.Items, MenuItem{Label: "Back"})
}

// buildOllamaModelsMenu builds the Ollama models list menu.
func (m *Menu) buildOllamaModelsMenu() {
	m.Items = []MenuItem{}
	for _, model := range m.OllamaModels {
		prefix := "  "
		if m.Config.Ollama.Model == model {
			prefix = "> "
		}
		m.Items = append(m.Items, MenuItem{Label: prefix + model, Value: model})
	}
	if len(m.Items) == 0 {
		m.Items = append(m.Items, MenuItem{Label: "(no models loaded)"})
	}
	m.Items = append(m.Items, MenuItem{Label: ""})
	m.Items = append(m.Items, MenuItem{Label: "Back"})
}

// buildCommandConfirmMenu builds the command confirmation menu
func (m *Menu) buildCommandConfirmMenu() {
	label := "Save Command"
	if m.EditingIndex >= 0 {
		label = "Save Changes"
	}
	m.Items = []MenuItem{
		{Label: label, Value: "save"},
		{Label: "Cancel", Value: "cancel"},
		{Label: ""},
		{Label: "Name: " + m.PendingName, Disabled: true},
		{Label: "Command: " + m.PendingCmd, Disabled: true},
	}
	if m.PendingDesc != "" {
		m.Items = append(m.Items, MenuItem{Label: "Description: " + m.PendingDesc, Disabled: true})
	}
}

// buildAliasConfirmMenu builds the alias confirmation menu
func (m *Menu) buildAliasConfirmMenu() {
	label := "Save Alias"
	if m.EditingName != "" {
		label = "Save Changes"
	}
	m.Items = []MenuItem{
		{Label: label, Value: "save"},
		{Label: "Cancel", Value: "cancel"},
		{Label: ""},
		{Label: "Alias: " + m.PendingName, Disabled: true},
		{Label: "Command: " + m.PendingAliasCmd, Disabled: true},
	}
}

// buildExportConfirmMenu builds the export confirmation menu
func (m *Menu) buildExportConfirmMenu() {
	label := "Save Export"
	if m.EditingExportName != "" {
		label = "Save Changes"
	}
	m.Items = []MenuItem{
		{Label: label, Value: "save"},
		{Label: "Cancel", Value: "cancel"},
		{Label: ""},
		{Label: "Export: " + m.PendingName, Disabled: true},
		{Label: "Value: " + m.PendingExport, Disabled: true},
	}
}

// buildDeleteConfirmMenu builds the delete confirmation menu
func (m *Menu) buildDeleteConfirmMenu() {
	var typeLabel, itemLabel string
	switch m.DeleteType {
	case "command":
		typeLabel = "Command"
		if m.DeleteIndex >= 0 && m.DeleteIndex < len(m.Config.Commands) {
			cmd := m.Config.Commands[m.DeleteIndex]
			itemLabel = cmd.Name + " = " + truncate(cmd.Command, 30)
		}
	case "alias":
		typeLabel = "Alias"
		if val, ok := m.Config.Aliases[m.DeleteTarget]; ok {
			itemLabel = m.DeleteTarget + " = " + truncate(val, 30)
		}
	case "export":
		typeLabel = "Export"
		if val, ok := m.Config.Exports[m.DeleteTarget]; ok {
			itemLabel = m.DeleteTarget + " = " + truncate(val, 30)
		}
	}

	m.Items = []MenuItem{
		{Label: "DELETE " + typeLabel + "?", IsHeader: true},
		{Label: ""},
		{Label: itemLabel, Disabled: true},
		{Label: ""},
		{Label: "Yes, Delete", Value: "delete"},
		{Label: "Cancel", Value: "cancel"},
	}
}

// MoveUp moves selection up
func (m *Menu) MoveUp() {
	if m.InputActive {
		return
	}
	for i := 0; i < len(m.Items); i++ {
		m.SelectedIndex--
		if m.SelectedIndex < 0 {
			m.SelectedIndex = len(m.Items) - 1
		}
		if m.isNavigable(m.SelectedIndex) {
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
	for i := 0; i < len(m.Items); i++ {
		m.SelectedIndex++
		if m.SelectedIndex >= len(m.Items) {
			m.SelectedIndex = 0
		}
		if m.isNavigable(m.SelectedIndex) {
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

	if !m.isSelectable(m.SelectedIndex) {
		return
	}
	item := m.Items[m.SelectedIndex]
	m.debugf("select state=%s index=%d label=%q value=%q", m.stateName(), m.SelectedIndex, item.Label, item.Value)

	switch m.State {
	case MenuMain:
		m.handleMainSelect()
	case MenuShellSelect:
		m.handleShellSelect(item)
	case MenuThemeSelect:
		m.handleThemeSelect(item)
	case MenuPromptStyle:
		m.handlePromptStyleSelect(item)
	case MenuPromptSettings:
		m.handlePromptSettingsSelect()
	case MenuScripts:
		m.handleScriptsSelect()
	case MenuOllamaModels:
		m.handleOllamaModelsSelect(item)
	case MenuCommands:
		m.handleCommandsSelect(item)
	case MenuAliases:
		m.handleAliasesSelect(item)
	case MenuExports:
		m.handleExportsSelect(item)
	case MenuConfirmCommand:
		m.handleCommandConfirmSelect()
	case MenuConfirmAlias:
		m.handleAliasConfirmSelect()
	case MenuConfirmExport:
		m.handleExportConfirmSelect()
	case MenuConfirmDelete:
		m.handleDeleteConfirmSelect()
	case MenuCursorStyle:
		m.handleCursorStyleSelect(item)
	}
}

func (m *Menu) handleMainSelect() {
	// Menu indices after reorganization with category headers:
	// 0: SHELL & ENVIRONMENT (header)
	// 1: Shell, 2: Source RC, 3: Scripts, 4: Commands, 5: Aliases, 6: Exports
	// 7: APPEARANCE (header)
	// 8: Theme, 9: Font Size, 10: Cursor Style, 11: Cursor Blink, 12: Panel Width
	// 13: Prompt Style, 14: Prompt Options
	// 15: AI FEATURES (header)
	// 16: Web Search, 17: Reader Proxy, 18: Ollama Chat, 19: Ollama URL, 20: Ollama Model
	// 21: Test Ollama, 22: Refresh Models, 23: Ollama Models
	// 24: ACTIONS (header)
	// 25: Reload Config, 26: Save and Close, 27: Cancel

	switch m.SelectedIndex {
	case 1: // Shell
		m.navigateTo(MenuShellSelect, m.buildShellMenu)
	case 2: // Source RC
		m.Config.Shell.SourceRC = !m.Config.Shell.SourceRC
		m.buildMainMenu()
		m.StatusMessage = "Updated (restart tab to apply)"
	case 3: // Scripts
		m.navigateTo(MenuScripts, m.buildScriptsMenu)
	case 4: // Commands
		m.navigateTo(MenuCommands, m.buildCommandsMenu)
	case 5: // Aliases
		m.navigateTo(MenuAliases, m.buildAliasesMenu)
	case 6: // Exports
		m.navigateTo(MenuExports, m.buildExportsMenu)
	case 8: // Theme
		m.navigateTo(MenuThemeSelect, m.buildThemeMenu)
	case 9: // Font Size
		m.startInputWithValue(InputFontSize, "Font size (8-32):", formatFloat(m.Config.FontSize))
	case 10: // Cursor Style
		m.navigateTo(MenuCursorStyle, m.buildCursorStyleMenu)
	case 11: // Cursor Blink
		m.Config.Appearance.CursorBlink = !m.Config.Appearance.CursorBlink
		m.buildMainMenu()
		m.StatusMessage = "Updated (save to persist)"
	case 12: // Panel Width
		pw := m.Config.Appearance.PanelWidthPercent
		if pw == 0 {
			pw = 35.0
		}
		m.startInputWithValue(InputPanelWidth, "Panel width (25-50%):", formatFloat(pw))
	case 13: // Prompt Style
		m.navigateTo(MenuPromptStyle, m.buildPromptStyleMenu)
	case 14: // Prompt Options
		m.navigateTo(MenuPromptSettings, m.buildPromptSettingsMenu)
	case 16: // Web Search
		m.Config.WebSearch.Enabled = !m.Config.WebSearch.Enabled
		m.buildMainMenu()
		m.StatusMessage = "Updated (save to persist)"
	case 17: // Reader Proxy
		m.Config.WebSearch.UseReaderProxy = !m.Config.WebSearch.UseReaderProxy
		m.buildMainMenu()
		m.StatusMessage = "Updated (save to persist)"
	case 18: // Ollama Chat
		m.Config.Ollama.Enabled = !m.Config.Ollama.Enabled
		m.buildMainMenu()
		m.StatusMessage = "Updated (save to persist)"
	case 19: // Ollama URL
		m.startInputWithValue(InputOllamaURL, "Ollama base URL:", m.Config.Ollama.URL)
	case 20: // Ollama Model
		m.startInputWithValue(InputOllamaModel, "Ollama model name:", m.Config.Ollama.Model)
	case 21: // Test Ollama Connection
		if m.OnOllamaTest == nil {
			m.StatusMessage = "Ollama test unavailable"
			return
		}
		if err := m.OnOllamaTest(m.Config.Ollama.URL); err != nil {
			m.StatusMessage = "Ollama test failed: " + err.Error()
			return
		}
		m.StatusMessage = "Ollama connection OK"
	case 22: // Refresh Ollama Models
		if m.OnOllamaFetchModels == nil {
			m.StatusMessage = "Ollama fetch unavailable"
			return
		}
		models, err := m.OnOllamaFetchModels(m.Config.Ollama.URL)
		if err != nil {
			m.StatusMessage = "Model refresh failed: " + err.Error()
			return
		}
		m.OllamaModels = models
		if len(models) == 0 {
			m.StatusMessage = "No models found"
			return
		}
		m.StatusMessage = "Models loaded (" + itoa(len(models)) + ")"
	case 23: // Ollama Models
		m.navigateTo(MenuOllamaModels, m.buildOllamaModelsMenu)
	case 24: // Thinking Mode
		m.Config.Ollama.ThinkingMode = !m.Config.Ollama.ThinkingMode
		m.buildMainMenu()
		m.StatusMessage = "Updated (save to persist)"
	case 25: // Show Thinking
		m.Config.Ollama.ShowThinking = !m.Config.Ollama.ShowThinking
		m.buildMainMenu()
		m.StatusMessage = "Updated (save to persist)"
	case 27: // Reload Config
		cfg, err := config.Load()
		if err != nil {
			m.StatusMessage = "Failed to reload config"
			return
		}
		if _, err := cfg.WriteInitScript(); err != nil {
			m.StatusMessage = "Reloaded (init regen failed)"
		}
		if m.OnConfigReload != nil {
			if err := m.OnConfigReload(cfg); err != nil {
				if m.StatusMessage == "" {
					m.StatusMessage = "Reloaded (apply failed)"
				}
			}
		}
		m.Config = cfg
		m.buildMainMenu()
		if m.StatusMessage == "" {
			m.StatusMessage = "Config reloaded"
		}
	case 28: // Save and Close
		if !m.saveConfigWithInitScript("Saved") {
			m.buildMainMenu()
			return
		}
		if m.OnConfigReload != nil {
			if err := m.OnConfigReload(m.Config); err != nil {
				m.StatusMessage = "Saved (apply failed)"
				m.buildMainMenu()
				return
			}
		}
		m.Close()
	case 29: // Cancel
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

func (m *Menu) handleThemeSelect(item MenuItem) {
	if item.Label == "Back" {
		m.goBack()
		return
	}
	if item.Value != "" {
		m.Config.Theme = item.Value
		m.StatusMessage = "Theme updated (save to persist)"
	}
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

func (m *Menu) handleCursorStyleSelect(item MenuItem) {
	if item.Label == "Back" {
		m.goBack()
		return
	}
	if item.Value != "" {
		m.Config.Appearance.CursorStyle = item.Value
		m.StatusMessage = "Cursor style updated (save to persist)"
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
		m.startInputWithValue(InputScriptInit, "Init script (Ctrl+Enter to save):", m.Config.Scripts.Init)
	case 1: // Pre-Prompt
		m.startInputWithValue(InputScriptPrePrompt, "Pre-prompt script (Ctrl+Enter to save):", m.Config.Scripts.PrePrompt)
	case 2: // Language Detect
		m.startInputWithValue(InputScriptLangDetect, "Language detect script (Ctrl+Enter to save):", m.Config.Scripts.LanguageDetect)
	case 3: // VCS Detect
		m.startInputWithValue(InputScriptVCSDetect, "VCS detect script (Ctrl+Enter to save):", m.Config.Scripts.VCSDetect)
	case 5:
		m.goBack()
	}
}

func (m *Menu) handleOllamaModelsSelect(item MenuItem) {
	if item.Label == "Back" {
		m.goBack()
		return
	}
	if item.Value == "" {
		return
	}
	m.Config.Ollama.Model = item.Value
	m.StatusMessage = "Ollama model updated (save to persist)"
	// Pre-load the model into memory
	if m.OnOllamaLoadModel != nil && m.Config.Ollama.URL != "" {
		m.OnOllamaLoadModel(m.Config.Ollama.URL, item.Value)
	}
	m.goBack()
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

func (m *Menu) handleExportsSelect(item MenuItem) {
	if item.Label == "Back" {
		m.goBack()
		return
	}
	if m.SelectedIndex == 0 { // Add new
		m.EditingExportName = ""
		m.PendingName = ""
		m.PendingExport = ""
		m.startInputWithValue(InputExportName, "Export name:", "")
	} else if item.Value != "" { // Edit existing
		m.EditingExportName = item.Value
		m.PendingName = item.Value
		m.startInputWithValue(InputExportName, "Export name:", item.Value)
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

// HandlePaste appends clipboard text to the input buffer.
func (m *Menu) HandlePaste(text string) {
	if !m.InputActive || text == "" {
		return
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if !m.InputIsMultiline() {
		text = strings.ReplaceAll(text, "\n", " ")
	}
	m.InputBuffer += text
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
	m.debugf("input enter state=%s input_state=%s value=%q", m.stateName(), m.inputStateName(), value)

	switch m.InputState {
	case InputCommandName:
		if value == "" {
			m.InputState = InputNone
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
			m.InputState = InputNone
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
		m.PendingDesc = value
		m.State = MenuConfirmCommand
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildCommandConfirmMenu()
		m.SelectedIndex = m.firstSelectableIndex()
		m.debugf("confirm command name=%q cmd=%q desc=%q", m.PendingName, m.PendingCmd, m.PendingDesc)

	case InputAliasName:
		if value == "" {
			m.InputState = InputNone
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
			m.InputState = InputNone
			m.buildAliasesMenu()
			return false
		}
		m.PendingAliasCmd = value
		m.State = MenuConfirmAlias
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildAliasConfirmMenu()
		m.SelectedIndex = m.firstSelectableIndex()
		m.debugf("confirm alias name=%q cmd=%q", m.PendingName, m.PendingAliasCmd)

	case InputExportName:
		if value == "" {
			m.InputState = InputNone
			m.buildExportsMenu()
			return false
		}
		m.PendingName = value
		initialValue := ""
		if m.EditingExportName != "" {
			initialValue = m.Config.Exports[m.EditingExportName]
		}
		m.startInputWithValue(InputExportValue, "Value:", initialValue)

	case InputExportValue:
		if value == "" {
			m.InputState = InputNone
			m.buildExportsMenu()
			return false
		}
		m.PendingExport = value
		m.State = MenuConfirmExport
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildExportConfirmMenu()
		m.SelectedIndex = m.firstSelectableIndex()
		m.debugf("confirm export name=%q value=%q", m.PendingName, m.PendingExport)

	case InputScriptInit:
		m.Config.Scripts.Init = value
		m.StatusMessage = "Script updated"
		m.buildScriptsMenu()

	case InputScriptPrePrompt:
		m.Config.Scripts.PrePrompt = value
		m.StatusMessage = "Script updated"
		m.buildScriptsMenu()

	case InputScriptLangDetect:
		m.Config.Scripts.LanguageDetect = value
		m.StatusMessage = "Script updated"
		m.buildScriptsMenu()

	case InputScriptVCSDetect:
		m.Config.Scripts.VCSDetect = value
		m.StatusMessage = "Script updated"
		m.buildScriptsMenu()

	case InputOllamaURL:
		m.Config.Ollama.URL = strings.TrimSpace(value)
		m.OllamaModels = nil
		m.StatusMessage = "Ollama URL updated (save to persist)"
		m.buildMainMenu()

	case InputOllamaModel:
		m.Config.Ollama.Model = strings.TrimSpace(value)
		m.StatusMessage = "Ollama model updated (save to persist)"
		m.buildMainMenu()

	case InputFontSize:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 32)
		if err != nil {
			m.StatusMessage = "Invalid font size"
			m.buildMainMenu()
			break
		}
		m.Config.FontSize = float32(parsed)
		m.StatusMessage = "Font size updated (save to persist)"
		m.buildMainMenu()

	case InputPanelWidth:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 32)
		if err != nil {
			m.StatusMessage = "Invalid panel width"
			m.buildMainMenu()
			break
		}
		// Clamp to valid range
		pw := float32(parsed)
		if pw < 25 {
			pw = 25
		} else if pw > 50 {
			pw = 50
		}
		m.Config.Appearance.PanelWidthPercent = pw
		m.StatusMessage = "Panel width updated (save to persist)"
		m.buildMainMenu()
	}

	if !m.InputActive {
		m.InputState = InputNone
	}
	return false
}

// HandleEscape handles escape key
func (m *Menu) HandleEscape() {
	if m.InputActive {
		m.InputActive = false
		m.InputState = InputNone
		m.InputBuffer = ""
		m.debugf("escape input state=%s", m.stateName())
		// Rebuild current menu
		switch m.State {
		case MenuCommands:
			m.buildCommandsMenu()
		case MenuAliases:
			m.buildAliasesMenu()
		case MenuScripts:
			m.buildScriptsMenu()
		case MenuExports:
			m.buildExportsMenu()
		}
		return
	}
	m.debugf("escape state=%s", m.stateName())
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
			m.DeleteType = "command"
			m.DeleteIndex = idx
			m.DeleteTarget = ""
			m.savePosition()
			m.State = MenuConfirmDelete
			m.buildDeleteConfirmMenu()
			m.SelectedIndex = m.firstSelectableIndex()
			m.ScrollOffset = 0
		}
	case MenuAliases:
		if m.SelectedIndex > 0 {
			item := m.Items[m.SelectedIndex]
			if item.Value != "" {
				m.DeleteType = "alias"
				m.DeleteTarget = item.Value
				m.DeleteIndex = -1
				m.savePosition()
				m.State = MenuConfirmDelete
				m.buildDeleteConfirmMenu()
				m.SelectedIndex = m.firstSelectableIndex()
				m.ScrollOffset = 0
			}
		}
	case MenuExports:
		if m.SelectedIndex > 0 {
			item := m.Items[m.SelectedIndex]
			if item.Value != "" {
				m.DeleteType = "export"
				m.DeleteTarget = item.Value
				m.DeleteIndex = -1
				m.savePosition()
				m.State = MenuConfirmDelete
				m.buildDeleteConfirmMenu()
				m.SelectedIndex = m.firstSelectableIndex()
				m.ScrollOffset = 0
			}
		}
	}
}

// handleDeleteConfirmSelect handles selection in delete confirmation menu
func (m *Menu) handleDeleteConfirmSelect() {
	item := m.Items[m.SelectedIndex]
	switch item.Value {
	case "delete":
		// Actually perform the delete
		switch m.DeleteType {
		case "command":
			m.Config.RemoveCustomCommand(m.DeleteIndex)
			if m.saveConfig() {
				m.StatusMessage = "Command deleted"
			}
			m.navigateTo(MenuCommands, m.buildCommandsMenu)
		case "alias":
			m.Config.RemoveAlias(m.DeleteTarget)
			_ = m.saveConfigWithInitScript("Alias deleted")
			m.navigateTo(MenuAliases, m.buildAliasesMenu)
		case "export":
			m.Config.RemoveExport(m.DeleteTarget)
			_ = m.saveConfigWithInitScript("Export deleted")
			m.navigateTo(MenuExports, m.buildExportsMenu)
		}
		// Adjust selection if needed
		if m.SelectedIndex >= len(m.Items) {
			m.SelectedIndex = len(m.Items) - 1
		}
	case "cancel":
		// Go back without deleting
		switch m.DeleteType {
		case "command":
			m.navigateTo(MenuCommands, m.buildCommandsMenu)
		case "alias":
			m.navigateTo(MenuAliases, m.buildAliasesMenu)
		case "export":
			m.navigateTo(MenuExports, m.buildExportsMenu)
		}
	}
	// Clear delete tracking
	m.DeleteType = ""
	m.DeleteTarget = ""
	m.DeleteIndex = -1
}

// goBack goes back to previous menu
func (m *Menu) goBack() {
	switch m.State {
	case MenuShellSelect, MenuThemeSelect, MenuPromptStyle, MenuPromptSettings, MenuScripts, MenuOllamaModels, MenuCommands, MenuAliases, MenuExports, MenuCursorStyle:
		m.navigateTo(MenuMain, m.buildMainMenu)
		m.debugf("go back to main")
	case MenuConfirmCommand:
		m.clearPendingCommand()
		m.navigateTo(MenuCommands, m.buildCommandsMenu)
		m.debugf("go back to commands")
	case MenuConfirmAlias:
		m.clearPendingAlias()
		m.navigateTo(MenuAliases, m.buildAliasesMenu)
		m.debugf("go back to aliases")
	case MenuConfirmExport:
		m.clearPendingExport()
		m.navigateTo(MenuExports, m.buildExportsMenu)
		m.debugf("go back to exports")
	case MenuConfirmDelete:
		// Go back to the appropriate menu based on delete type
		switch m.DeleteType {
		case "command":
			m.navigateTo(MenuCommands, m.buildCommandsMenu)
		case "alias":
			m.navigateTo(MenuAliases, m.buildAliasesMenu)
		case "export":
			m.navigateTo(MenuExports, m.buildExportsMenu)
		default:
			m.navigateTo(MenuMain, m.buildMainMenu)
		}
		m.DeleteType = ""
		m.DeleteTarget = ""
		m.DeleteIndex = -1
		m.debugf("go back from delete confirm")
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
	case MenuThemeSelect:
		return "Select Theme"
	case MenuPromptStyle:
		return "Prompt Style"
	case MenuPromptSettings:
		return "Prompt Options"
	case MenuScripts:
		return "Scripts"
	case MenuOllamaModels:
		return "Ollama Models"
	case MenuCommands:
		return "Commands"
	case MenuAliases:
		return "Aliases"
	case MenuExports:
		return "Exports"
	case MenuConfirmCommand:
		return "Confirm Command"
	case MenuConfirmAlias:
		return "Confirm Alias"
	case MenuConfirmExport:
		return "Confirm Export"
	case MenuConfirmDelete:
		return "Confirm Delete"
	case MenuCursorStyle:
		return "Cursor Style"
	default:
		return "Settings"
	}
}

func (m *Menu) handleCommandConfirmSelect() {
	item := m.Items[m.SelectedIndex]
	m.debugf("confirm command select value=%q", item.Value)
	switch item.Value {
	case "save":
		if m.EditingIndex >= 0 {
			m.Config.Commands[m.EditingIndex].Name = m.PendingName
			m.Config.Commands[m.EditingIndex].Command = m.PendingCmd
			m.Config.Commands[m.EditingIndex].Description = m.PendingDesc
			if m.saveConfig() {
				m.StatusMessage = "Command updated"
			}
		} else {
			m.Config.AddCustomCommand(m.PendingName, m.PendingCmd, m.PendingDesc)
			if m.saveConfig() {
				m.StatusMessage = "Command added"
			}
		}
		m.clearPendingCommand()
		m.navigateTo(MenuCommands, m.buildCommandsMenu)
	case "cancel":
		m.clearPendingCommand()
		m.navigateTo(MenuCommands, m.buildCommandsMenu)
	}
}

func (m *Menu) handleAliasConfirmSelect() {
	item := m.Items[m.SelectedIndex]
	m.debugf("confirm alias select value=%q", item.Value)
	switch item.Value {
	case "save":
		if m.EditingName != "" && m.EditingName != m.PendingName {
			delete(m.Config.Aliases, m.EditingName)
		}
		m.Config.SetAlias(m.PendingName, m.PendingAliasCmd)
		_ = m.saveConfigWithInitScript("Alias saved")
		m.clearPendingAlias()
		m.navigateTo(MenuAliases, m.buildAliasesMenu)
	case "cancel":
		m.clearPendingAlias()
		m.navigateTo(MenuAliases, m.buildAliasesMenu)
	}
}

func (m *Menu) handleExportConfirmSelect() {
	item := m.Items[m.SelectedIndex]
	m.debugf("confirm export select value=%q", item.Value)
	switch item.Value {
	case "save":
		if m.EditingExportName != "" && m.EditingExportName != m.PendingName {
			delete(m.Config.Exports, m.EditingExportName)
		}
		m.Config.SetExport(m.PendingName, m.PendingExport)
		_ = m.saveConfigWithInitScript("Export saved")
		m.clearPendingExport()
		m.navigateTo(MenuExports, m.buildExportsMenu)
	case "cancel":
		m.clearPendingExport()
		m.navigateTo(MenuExports, m.buildExportsMenu)
	}
}

func (m *Menu) clearPendingCommand() {
	m.PendingName = ""
	m.PendingCmd = ""
	m.PendingDesc = ""
	m.EditingIndex = -1
}

func (m *Menu) clearPendingAlias() {
	m.PendingName = ""
	m.PendingAliasCmd = ""
	m.EditingName = ""
	m.EditingIndex = -1
}

func (m *Menu) clearPendingExport() {
	m.PendingName = ""
	m.PendingExport = ""
	m.EditingExportName = ""
	m.EditingIndex = -1
}

func (m *Menu) saveConfig() bool {
	if err := m.Config.Save(); err != nil {
		m.StatusMessage = "Error: " + err.Error()
		m.debugf("save error: %v", err)
		return false
	}
	m.debugf("save ok")
	return true
}

func (m *Menu) saveConfigWithInitScript(successMessage string) bool {
	if !m.saveConfig() {
		return false
	}
	initPath, err := m.Config.WriteInitScript()
	if err != nil {
		m.StatusMessage = successMessage + " (init regen failed)"
		return false
	}
	if m.OnInitScriptUpdated != nil {
		if err := m.OnInitScriptUpdated(initPath); err != nil {
			m.StatusMessage = successMessage + " (apply failed)"
			return false
		}
	}
	m.StatusMessage = successMessage
	return true
}

// Helper functions

func (m *Menu) isSelectable(index int) bool {
	if index < 0 || index >= len(m.Items) {
		return false
	}
	item := m.Items[index]
	return item.Label != "" && !item.Disabled && !item.IsHeader
}

func (m *Menu) isNavigable(index int) bool {
	if index < 0 || index >= len(m.Items) {
		return false
	}
	item := m.Items[index]
	// Headers are not navigable - skip them when moving cursor
	return item.Label != "" && !item.IsHeader
}

func (m *Menu) firstSelectableIndex() int {
	for i := range m.Items {
		if m.isSelectable(i) {
			return i
		}
	}
	return 0
}

func (m *Menu) debugf(format string, args ...interface{}) {
	if !debugMenu {
		return
	}
	log.Printf("menu: "+format, args...)
}

func (m *Menu) stateName() string {
	switch m.State {
	case MenuClosed:
		return "closed"
	case MenuMain:
		return "main"
	case MenuShellSelect:
		return "shell"
	case MenuThemeSelect:
		return "theme"
	case MenuPromptSettings:
		return "prompt_settings"
	case MenuPromptStyle:
		return "prompt_style"
	case MenuScripts:
		return "scripts"
	case MenuOllamaModels:
		return "ollama_models"
	case MenuCommands:
		return "commands"
	case MenuAliases:
		return "aliases"
	case MenuExports:
		return "exports"
	case MenuConfirmCommand:
		return "confirm_command"
	case MenuConfirmAlias:
		return "confirm_alias"
	case MenuConfirmExport:
		return "confirm_export"
	case MenuConfirmDelete:
		return "confirm_delete"
	case MenuCursorStyle:
		return "cursor_style"
	default:
		return "unknown"
	}
}

func (m *Menu) inputStateName() string {
	switch m.InputState {
	case InputNone:
		return "none"
	case InputCommandName:
		return "command_name"
	case InputCommandValue:
		return "command_value"
	case InputCommandDesc:
		return "command_desc"
	case InputAliasName:
		return "alias_name"
	case InputAliasValue:
		return "alias_value"
	case InputScriptInit:
		return "script_init"
	case InputScriptPrePrompt:
		return "script_pre_prompt"
	case InputScriptLangDetect:
		return "script_lang_detect"
	case InputScriptVCSDetect:
		return "script_vcs_detect"
	case InputExportName:
		return "export_name"
	case InputExportValue:
		return "export_value"
	case InputOllamaURL:
		return "ollama_url"
	case InputOllamaModel:
		return "ollama_model"
	case InputFontSize:
		return "font_size"
	case InputPanelWidth:
		return "panel_width"
	default:
		return "unknown"
	}
}

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

func formatFloat(f float32) string {
	return strconv.FormatFloat(float64(f), 'f', -1, 32)
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
