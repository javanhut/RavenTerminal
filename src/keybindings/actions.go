package keybindings

// ActionInfo describes an app action for the command palette and help.
type ActionInfo struct {
	Action KeyAction
	Name   string
	Chord  string
}

// Actions lists every action the command palette can run, with its default chord.
func Actions() []ActionInfo {
	return []ActionInfo{
		{ActionNewTab, "New tab", "Leader+T"},
		{ActionCloseTab, "Close tab", "Leader+X"},
		{ActionNextTab, "Next tab", "Leader+Tab / Super+Shift+]"},
		{ActionPrevTab, "Previous tab", "Leader+Shift+Tab / Super+Shift+["},
		{ActionSplitVertical, "Split pane vertically", "Leader+D"},
		{ActionSplitHorizontal, "Split pane horizontally", "Leader+Shift+D"},
		{ActionClosePane, "Close pane", "Leader+W"},
		{ActionNextPane, "Next pane", "Leader+]"},
		{ActionPrevPane, "Previous pane", "Leader+["},
		{ActionTogglePaneZoom, "Toggle pane zoom", "Leader+Z"},
		{ActionToggleBroadcast, "Toggle broadcast input to all panes", "Leader+B"},
		{ActionToggleResizeMode, "Pane resize mode", "Leader+R"},
		{ActionCopy, "Copy selection", "Leader+C"},
		{ActionPaste, "Paste clipboard", "Leader+V"},
		{ActionCopyLastOutput, "Copy last command output", "Leader+Shift+C"},
		{ActionPrevPrompt, "Jump to previous prompt", "Leader+Up"},
		{ActionNextPrompt, "Jump to next prompt", "Leader+Down"},
		{ActionHints, "Hints: pick a URL, path or hash on screen", "Leader+H / Leader+E"},
		{ActionCopyMode, "Copy mode (vi keys over scrollback)", "Leader+Space / Leader+Shift+V"},
		{ActionFindInScrollback, "Find in scrollback", "Super+Shift+F"},
		{ActionScrollbackPager, "Open scrollback in pager", "Leader+Shift+H"},
		{ActionScrollUp, "Scroll up a page", "Shift+PageUp"},
		{ActionScrollDown, "Scroll down a page", "Shift+PageDown"},
		{ActionZoomIn, "Increase font size", "Leader+="},
		{ActionZoomOut, "Decrease font size", "Leader+-"},
		{ActionZoomReset, "Reset font size", "Leader+0"},
		{ActionToggleFullscreen, "Toggle fullscreen", "Leader+Enter / F11"},
		{ActionShowHelp, "Show keybindings", "Leader+K"},
		{ActionOpenMenu, "Open settings", "Leader+S"},
		{ActionToggleSearchPanel, "Toggle web search panel", "Leader+F"},
		{ActionToggleAIPanel, "Toggle AI panel", "Leader+A"},
		{ActionExit, "Quit", "Leader+Q"},
	}
}
