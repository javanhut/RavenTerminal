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
	MenuCommandEdit
	MenuAliases
	MenuAliasEdit
)

// MenuItem represents a menu item
type MenuItem struct {
	Label    string
	Value    string
	Action   MenuAction
	Disabled bool
}

// MenuAction represents what happens when a menu item is selected
type MenuAction int

const (
	ActionNone MenuAction = iota
	ActionOpenShellMenu
	ActionOpenPromptMenu
	ActionOpenPromptStyleMenu
	ActionOpenScriptsMenu
	ActionOpenCommandsMenu
	ActionOpenAliasesMenu
	ActionSelectShell
	ActionSelectPromptStyle
	ActionToggleSourceRC
	ActionTogglePath
	ActionToggleUsername
	ActionToggleHostname
	ActionToggleLanguage
	ActionToggleVCS
	ActionAddCommand
	ActionEditCommand
	ActionDeleteCommand
	ActionAddAlias
	ActionEditAlias
	ActionDeleteAlias
	ActionSaveAndClose
	ActionCancel
	ActionBack
)

// Menu manages the configuration menu
type Menu struct {
	State         MenuState
	Config        *config.Config
	SelectedIndex int
	Items         []MenuItem
	ScrollOffset  int

	// For editing
	EditingField int // 0 = name, 1 = value, 2 = description
	EditBuffer   string
	EditIndex    int // Index of item being edited (-1 for new)

	// Input mode
	InputMode     bool
	InputPrompt   string
	InputBuffer   string
	InputCallback func(string)

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
		State:         MenuClosed,
		Config:        cfg,
		SelectedIndex: 0,
		Items:         []MenuItem{},
		EditIndex:     -1,
	}
}

// Open opens the menu
func (m *Menu) Open() {
	m.State = MenuMain
	m.SelectedIndex = 0
	m.ScrollOffset = 0
	m.buildMainMenu()
}

// Close closes the menu
func (m *Menu) Close() {
	m.State = MenuClosed
	m.InputMode = false
	m.StatusMessage = ""
}

// IsOpen returns true if the menu is open
func (m *Menu) IsOpen() bool {
	return m.State != MenuClosed
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
		{Label: "Shell: " + currentShell, Action: ActionOpenShellMenu},
		{Label: "Source RC Files: " + sourceRC, Action: ActionToggleSourceRC},
		{Label: "Prompt Settings (Style: " + promptStyle + ")", Action: ActionOpenPromptMenu},
		{Label: "Scripts", Action: ActionOpenScriptsMenu},
		{Label: "Commands (" + itoa(len(m.Config.Commands)) + ")", Action: ActionOpenCommandsMenu},
		{Label: "Aliases (" + itoa(len(m.Config.Aliases)) + ")", Action: ActionOpenAliasesMenu},
		{Label: "", Action: ActionNone, Disabled: true},
		{Label: "Save and Close", Action: ActionSaveAndClose},
		{Label: "Cancel", Action: ActionCancel},
	}
}

// buildShellMenu builds the shell selection menu
func (m *Menu) buildShellMenu() {
	shells := config.GetAvailableShells()
	m.Items = []MenuItem{
		{Label: "(System Default)", Value: "", Action: ActionSelectShell},
	}
	for _, shell := range shells {
		m.Items = append(m.Items, MenuItem{
			Label:  shell,
			Value:  shell,
			Action: ActionSelectShell,
		})
	}
	m.Items = append(m.Items, MenuItem{Label: "", Action: ActionNone, Disabled: true})
	m.Items = append(m.Items, MenuItem{Label: "Back", Action: ActionBack})
}

// boolToStatus returns a status string for a boolean
func boolToStatus(b bool) string {
	if b {
		return "[ON]"
	}
	return "[OFF]"
}

// buildPromptMenu builds the prompt settings menu
func (m *Menu) buildPromptMenu() {
	p := m.Config.Prompt
	style := p.Style
	if style == "" {
		style = "full"
	}

	m.Items = []MenuItem{
		{Label: "Prompt Style: " + style, Action: ActionOpenPromptStyleMenu},
		{Label: "", Action: ActionNone, Disabled: true},
		{Label: "Show Path " + boolToStatus(p.ShowPath), Action: ActionTogglePath},
		{Label: "Show Username " + boolToStatus(p.ShowUsername), Action: ActionToggleUsername},
		{Label: "Show Hostname " + boolToStatus(p.ShowHostname), Action: ActionToggleHostname},
		{Label: "Show Programming Language " + boolToStatus(p.ShowLanguage), Action: ActionToggleLanguage},
		{Label: "Show VCS (Git/Ivaldi) " + boolToStatus(p.ShowVCS), Action: ActionToggleVCS},
		{Label: "", Action: ActionNone, Disabled: true},
		{Label: "Back", Action: ActionBack},
	}
}

// buildPromptStyleMenu builds the prompt style selection menu
func (m *Menu) buildPromptStyleMenu() {
	styles := []struct {
		name string
		desc string
	}{
		{"minimal", "Just the prompt symbol"},
		{"simple", "Path and prompt"},
		{"full", "Path, language, VCS, user info"},
		{"custom", "Use custom script"},
	}

	m.Items = []MenuItem{}
	for _, style := range styles {
		prefix := "  "
		if m.Config.Prompt.Style == style.name {
			prefix = "> "
		}
		m.Items = append(m.Items, MenuItem{
			Label:  prefix + style.name + " - " + style.desc,
			Value:  style.name,
			Action: ActionSelectPromptStyle,
		})
	}
	m.Items = append(m.Items, MenuItem{Label: "", Action: ActionNone, Disabled: true})
	m.Items = append(m.Items, MenuItem{Label: "Back", Action: ActionBack})
}

// buildScriptsMenu builds the scripts info menu
func (m *Menu) buildScriptsMenu() {
	configDir := config.GetConfigDir()
	m.Items = []MenuItem{
		{Label: "Scripts are configured in config.toml", Action: ActionNone, Disabled: true},
		{Label: "", Action: ActionNone, Disabled: true},
		{Label: "Config location:", Action: ActionNone, Disabled: true},
		{Label: "  " + configDir + "/config.toml", Action: ActionNone, Disabled: true},
		{Label: "", Action: ActionNone, Disabled: true},
		{Label: "Available script sections:", Action: ActionNone, Disabled: true},
		{Label: "  [scripts.init] - Runs on shell start", Action: ActionNone, Disabled: true},
		{Label: "  [scripts.pre_prompt] - Runs before prompt", Action: ActionNone, Disabled: true},
		{Label: "  [scripts.language_detect] - Language detection", Action: ActionNone, Disabled: true},
		{Label: "  [scripts.vcs_detect] - VCS detection", Action: ActionNone, Disabled: true},
		{Label: "", Action: ActionNone, Disabled: true},
		{Label: "Back", Action: ActionBack},
	}
}

// buildCommandsMenu builds the commands menu
func (m *Menu) buildCommandsMenu() {
	m.Items = []MenuItem{
		{Label: "+ Add New Command", Action: ActionAddCommand},
		{Label: "", Action: ActionNone, Disabled: true},
	}
	for i, cmd := range m.Config.Commands {
		m.Items = append(m.Items, MenuItem{
			Label:  cmd.Name + " - " + truncate(cmd.Command, 30),
			Value:  itoa(i),
			Action: ActionEditCommand,
		})
	}
	m.Items = append(m.Items, MenuItem{Label: "", Action: ActionNone, Disabled: true})
	m.Items = append(m.Items, MenuItem{Label: "Back", Action: ActionBack})
}

// buildAliasesMenu builds the aliases menu
func (m *Menu) buildAliasesMenu() {
	m.Items = []MenuItem{
		{Label: "+ Add New Alias", Action: ActionAddAlias},
		{Label: "", Action: ActionNone, Disabled: true},
	}
	for name, cmd := range m.Config.Aliases {
		m.Items = append(m.Items, MenuItem{
			Label:  name + " = " + truncate(cmd, 30),
			Value:  name,
			Action: ActionEditAlias,
		})
	}
	m.Items = append(m.Items, MenuItem{Label: "", Action: ActionNone, Disabled: true})
	m.Items = append(m.Items, MenuItem{Label: "Back", Action: ActionBack})
}

// MoveUp moves selection up
func (m *Menu) MoveUp() {
	if m.InputMode {
		return
	}
	for {
		m.SelectedIndex--
		if m.SelectedIndex < 0 {
			m.SelectedIndex = len(m.Items) - 1
		}
		if !m.Items[m.SelectedIndex].Disabled {
			break
		}
	}
	m.adjustScroll()
}

// MoveDown moves selection down
func (m *Menu) MoveDown() {
	if m.InputMode {
		return
	}
	for {
		m.SelectedIndex++
		if m.SelectedIndex >= len(m.Items) {
			m.SelectedIndex = 0
		}
		if !m.Items[m.SelectedIndex].Disabled {
			break
		}
	}
	m.adjustScroll()
}

// adjustScroll adjusts scroll offset to keep selection visible
func (m *Menu) adjustScroll() {
	visibleItems := 15
	if m.SelectedIndex < m.ScrollOffset {
		m.ScrollOffset = m.SelectedIndex
	} else if m.SelectedIndex >= m.ScrollOffset+visibleItems {
		m.ScrollOffset = m.SelectedIndex - visibleItems + 1
	}
}

// Select handles selection of current item
func (m *Menu) Select() {
	if m.InputMode {
		return
	}
	if m.SelectedIndex >= len(m.Items) {
		return
	}

	item := m.Items[m.SelectedIndex]
	if item.Disabled {
		return
	}

	switch item.Action {
	case ActionOpenShellMenu:
		m.State = MenuShellSelect
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildShellMenu()

	case ActionToggleSourceRC:
		m.Config.Shell.SourceRC = !m.Config.Shell.SourceRC
		m.buildMainMenu()
		m.StatusMessage = "Setting updated (restart tab to apply)"

	case ActionOpenPromptMenu:
		m.State = MenuPromptSettings
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildPromptMenu()

	case ActionOpenPromptStyleMenu:
		m.State = MenuPromptStyle
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildPromptStyleMenu()

	case ActionOpenScriptsMenu:
		m.State = MenuScripts
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildScriptsMenu()

	case ActionSelectPromptStyle:
		m.Config.Prompt.Style = item.Value
		m.State = MenuPromptSettings
		m.SelectedIndex = 0
		m.buildPromptMenu()
		m.StatusMessage = "Prompt style updated (restart tab to apply)"

	case ActionTogglePath:
		m.Config.Prompt.ShowPath = !m.Config.Prompt.ShowPath
		m.buildPromptMenu()
		m.StatusMessage = "Setting updated (restart tab to apply)"

	case ActionToggleUsername:
		m.Config.Prompt.ShowUsername = !m.Config.Prompt.ShowUsername
		m.buildPromptMenu()
		m.StatusMessage = "Setting updated (restart tab to apply)"

	case ActionToggleHostname:
		m.Config.Prompt.ShowHostname = !m.Config.Prompt.ShowHostname
		m.buildPromptMenu()
		m.StatusMessage = "Setting updated (restart tab to apply)"

	case ActionToggleLanguage:
		m.Config.Prompt.ShowLanguage = !m.Config.Prompt.ShowLanguage
		m.buildPromptMenu()
		m.StatusMessage = "Setting updated (restart tab to apply)"

	case ActionToggleVCS:
		m.Config.Prompt.ShowVCS = !m.Config.Prompt.ShowVCS
		m.buildPromptMenu()
		m.StatusMessage = "Setting updated (restart tab to apply)"

	case ActionOpenCommandsMenu:
		m.State = MenuCommands
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildCommandsMenu()

	case ActionOpenAliasesMenu:
		m.State = MenuAliases
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildAliasesMenu()

	case ActionSelectShell:
		m.Config.Shell.Path = item.Value
		m.State = MenuMain
		m.SelectedIndex = 0
		m.buildMainMenu()
		m.StatusMessage = "Shell updated (restart tab to apply)"

	case ActionAddCommand:
		m.startInput("Command name:", func(name string) {
			if name != "" {
				m.startInput("Command:", func(cmd string) {
					if cmd != "" {
						m.startInput("Description (optional):", func(desc string) {
							m.Config.AddCustomCommand(name, cmd, desc)
							m.buildCommandsMenu()
							m.StatusMessage = "Command added"
						})
					}
				})
			}
		})

	case ActionEditCommand:
		idx := atoi(item.Value)
		m.showCommandOptions(idx)

	case ActionAddAlias:
		m.startInput("Alias name:", func(name string) {
			if name != "" {
				m.startInput("Command:", func(cmd string) {
					if cmd != "" {
						m.Config.SetAlias(name, cmd)
						m.buildAliasesMenu()
						m.StatusMessage = "Alias added"
					}
				})
			}
		})

	case ActionEditAlias:
		m.showAliasOptions(item.Value)

	case ActionSaveAndClose:
		if err := m.Config.Save(); err != nil {
			m.StatusMessage = "Error saving: " + err.Error()
		} else {
			m.Close()
		}

	case ActionCancel:
		// Reload config to discard changes
		m.Config, _ = config.Load()
		m.Close()

	case ActionBack:
		m.goBack()
	}
}

// showCommandOptions shows options for editing/deleting a command
func (m *Menu) showCommandOptions(idx int) {
	if idx < 0 || idx >= len(m.Config.Commands) {
		return
	}
	cmd := m.Config.Commands[idx]
	m.Items = []MenuItem{
		{Label: "Edit Name: " + cmd.Name, Value: itoa(idx) + ":name", Action: ActionEditCommand},
		{Label: "Edit Command: " + truncate(cmd.Command, 25), Value: itoa(idx) + ":cmd", Action: ActionEditCommand},
		{Label: "Edit Description: " + truncate(cmd.Description, 20), Value: itoa(idx) + ":desc", Action: ActionEditCommand},
		{Label: "", Action: ActionNone, Disabled: true},
		{Label: "Delete Command", Value: itoa(idx), Action: ActionDeleteCommand},
		{Label: "Back", Action: ActionBack},
	}
	m.State = MenuCommandEdit
	m.SelectedIndex = 0
	m.EditIndex = idx
}

// showAliasOptions shows options for editing/deleting an alias
func (m *Menu) showAliasOptions(name string) {
	cmd := m.Config.Aliases[name]
	m.Items = []MenuItem{
		{Label: "Alias: " + name, Action: ActionNone, Disabled: true},
		{Label: "Command: " + truncate(cmd, 30), Value: name, Action: ActionEditAlias},
		{Label: "", Action: ActionNone, Disabled: true},
		{Label: "Delete Alias", Value: name, Action: ActionDeleteAlias},
		{Label: "Back", Action: ActionBack},
	}
	m.State = MenuAliasEdit
	m.SelectedIndex = 1
}

// goBack goes back to previous menu
func (m *Menu) goBack() {
	switch m.State {
	case MenuShellSelect, MenuCommands, MenuAliases, MenuPromptSettings, MenuScripts:
		m.State = MenuMain
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildMainMenu()
	case MenuPromptStyle:
		m.State = MenuPromptSettings
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildPromptMenu()
	case MenuCommandEdit:
		m.State = MenuCommands
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildCommandsMenu()
	case MenuAliasEdit:
		m.State = MenuAliases
		m.SelectedIndex = 0
		m.ScrollOffset = 0
		m.buildAliasesMenu()
	default:
		m.Close()
	}
}

// startInput starts input mode
func (m *Menu) startInput(prompt string, callback func(string)) {
	m.InputMode = true
	m.InputPrompt = prompt
	m.InputBuffer = ""
	m.InputCallback = callback
}

// HandleInputChar handles character input in input mode
func (m *Menu) HandleInputChar(char rune) {
	if !m.InputMode {
		return
	}
	m.InputBuffer += string(char)
}

// HandleInputBackspace handles backspace in input mode
func (m *Menu) HandleInputBackspace() {
	if !m.InputMode || len(m.InputBuffer) == 0 {
		return
	}
	m.InputBuffer = m.InputBuffer[:len(m.InputBuffer)-1]
}

// HandleInputEnter handles enter in input mode
func (m *Menu) HandleInputEnter() {
	if !m.InputMode {
		return
	}
	m.InputMode = false
	if m.InputCallback != nil {
		m.InputCallback(m.InputBuffer)
	}
	m.InputBuffer = ""
	m.InputPrompt = ""
	m.InputCallback = nil
}

// HandleInputEscape handles escape in input mode
func (m *Menu) HandleInputEscape() {
	if !m.InputMode {
		m.goBack()
		return
	}
	m.InputMode = false
	m.InputBuffer = ""
	m.InputPrompt = ""
	m.InputCallback = nil
}

// DeleteSelected handles deletion of selected item
func (m *Menu) DeleteSelected() {
	if m.InputMode {
		return
	}
	if m.SelectedIndex >= len(m.Items) {
		return
	}

	item := m.Items[m.SelectedIndex]

	switch item.Action {
	case ActionDeleteCommand:
		idx := atoi(item.Value)
		m.Config.RemoveCustomCommand(idx)
		m.State = MenuCommands
		m.SelectedIndex = 0
		m.buildCommandsMenu()
		m.StatusMessage = "Command deleted"

	case ActionDeleteAlias:
		m.Config.RemoveAlias(item.Value)
		m.State = MenuAliases
		m.SelectedIndex = 0
		m.buildAliasesMenu()
		m.StatusMessage = "Alias deleted"
	}
}

// GetTitle returns the current menu title
func (m *Menu) GetTitle() string {
	switch m.State {
	case MenuMain:
		return "Raven Terminal Settings"
	case MenuShellSelect:
		return "Select Shell"
	case MenuPromptSettings:
		return "Prompt Settings"
	case MenuPromptStyle:
		return "Select Prompt Style"
	case MenuScripts:
		return "Scripts Configuration"
	case MenuCommands:
		return "Custom Commands"
	case MenuCommandEdit:
		return "Edit Command"
	case MenuAliases:
		return "Aliases"
	case MenuAliasEdit:
		return "Edit Alias"
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
