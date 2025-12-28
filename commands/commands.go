package commands

import (
	"fmt"
	"github.com/javanhut/RavenTerminal/fonts"
	"strings"
)

// CommandResult represents the result of executing a terminal command
type CommandResult struct {
	Handled bool   // Whether the command was handled
	Output  string // Output to display in terminal
}

// FontChanger interface for changing fonts
type FontChanger interface {
	ChangeFont(name string) error
	CurrentFont() string
	GetAvailableFonts() []fonts.FontInfo
}

// HandleCommand checks if input is a terminal command and handles it
// Returns (handled, output) - if handled is true, don't send to shell
func HandleCommand(input string, fontChanger FontChanger) CommandResult {
	input = strings.TrimSpace(input)

	// Check for keybindings command
	if input == "keybindings" || input == "raven-keybindings" {
		return CommandResult{
			Handled: true,
			Output:  getKeybindingsHelp(),
		}
	}

	// Check for change-font command
	if strings.HasPrefix(input, "change-font ") || strings.HasPrefix(input, "change-font\t") {
		args := strings.TrimPrefix(input, "change-font ")
		args = strings.TrimSpace(args)
		return handleChangeFont(args, fontChanger)
	}

	// Check for change-font without args (list fonts)
	if input == "change-font" {
		return handleListFonts(fontChanger)
	}

	// Check for list-fonts command
	if input == "list-fonts" || input == "fonts" {
		return handleListFonts(fontChanger)
	}

	return CommandResult{Handled: false}
}

func getKeybindingsHelp() string {
	return `
Raven Terminal - Keybindings
============================

General:
  Ctrl+Q          Exit terminal
  Ctrl+Shift+Q    Force exit

Tabs:
  Ctrl+Shift+T    New tab
  Ctrl+Shift+W    Close current tab
  Ctrl+Tab        Next tab
  Ctrl+Shift+Tab  Previous tab

Scrolling:
  Shift+PageUp    Scroll up
  Shift+PageDown  Scroll down

Terminal Commands:
  keybindings     Show this help
  change-font     List available fonts
  change-font <name>  Change font (e.g., change-font firacode)
  list-fonts      List available fonts

`
}

func handleChangeFont(fontName string, fontChanger FontChanger) CommandResult {
	if fontName == "" {
		return handleListFonts(fontChanger)
	}

	fontName = strings.ToLower(strings.TrimSpace(fontName))

	err := fontChanger.ChangeFont(fontName)
	if err != nil {
		availableFonts := fontChanger.GetAvailableFonts()
		var names []string
		for _, f := range availableFonts {
			names = append(names, f.Name)
		}
		return CommandResult{
			Handled: true,
			Output:  fmt.Sprintf("\nError: %v\nAvailable fonts: %s\n\n", err, strings.Join(names, ", ")),
		}
	}

	return CommandResult{
		Handled: true,
		Output:  fmt.Sprintf("\nFont changed to: %s\n\n", fontName),
	}
}

func handleListFonts(fontChanger FontChanger) CommandResult {
	availableFonts := fontChanger.GetAvailableFonts()
	currentFont := fontChanger.CurrentFont()

	var sb strings.Builder
	sb.WriteString("\nAvailable Fonts:\n")
	sb.WriteString("================\n")

	for _, f := range availableFonts {
		marker := "  "
		if f.Name == currentFont {
			marker = "> "
		}
		sb.WriteString(fmt.Sprintf("%s%s (%s)\n", marker, f.Name, f.DisplayName))
	}

	sb.WriteString("\nUsage: change-font <name>\n")
	sb.WriteString("Example: change-font firacode\n\n")

	return CommandResult{
		Handled: true,
		Output:  sb.String(),
	}
}
