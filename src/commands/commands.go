// Package commands holds the text behind the shell-side helper commands
// (keybindings, list-fonts, change-font). The shell functions call
// `raven-terminal --keybindings` / `--list-fonts` and emit OSC 7771 for
// change-font, so nothing here intercepts typed input.
package commands

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/javanhut/RavenTerminal/src/assets/fonts"
	"github.com/javanhut/RavenTerminal/src/keybindings"
)

// FontChanger is implemented by the renderer.
type FontChanger interface {
	ChangeFont(name string) error
	CurrentFont() string
	GetAvailableFonts() []fonts.FontInfo
}

// KeybindingsHelp returns the keybinding reference printed by
// `raven-terminal --keybindings`, built from the same action table as the
// command palette.
func KeybindingsHelp() string {
	leader := "Super or Ctrl+Shift; Leader+Shift = Super+Shift or Ctrl+Shift+Alt"
	if runtime.GOOS == "darwin" {
		leader = "Cmd"
	}
	var b strings.Builder
	b.WriteString("Raven Terminal - Keybindings\n============================\n")
	fmt.Fprintf(&b, "Leader = %s.\nPlain Ctrl/Alt chords always go to the running program.\n\nKeys:\n", leader)
	rows := append(keybindings.Actions(),
		keybindings.ActionInfo{Name: "Command palette (actions, settings, themes, fonts)", Chord: "Leader+P"},
		keybindings.ActionInfo{Name: "Jump to tab N", Chord: "Leader+1..9"},
		keybindings.ActionInfo{Name: "Scroll 1 line (main screen)", Chord: "Shift+Up / Down"},
	)
	for _, a := range rows {
		fmt.Fprintf(&b, "  %-34s %s\n", a.Chord, a.Name)
	}
	b.WriteString(`
Mouse:
  Drag                               Select text (copies to clipboard and PRIMARY)
  Alt+drag                           Rectangular (block) selection
  Leader+click on command output     Select and copy that output (shell integration)
  Middle-click                       Paste the PRIMARY selection
  Right-click                        Copy selection or paste clipboard
  Ctrl+Right-click                   Open the URL under the pointer
  Mouse wheel                        Scroll 3 lines

Shell commands:
  keybindings                        Show this help
  list-fonts                         List available fonts
  change-font <name>                 Change font (e.g. change-font firacode)
`)
	return b.String()
}

// FontList renders the font list printed by `raven-terminal --list-fonts`,
// marking the current font.
func FontList(available []fonts.FontInfo, current string) string {
	var sb strings.Builder
	sb.WriteString("Available Fonts:\n================\n")
	for _, f := range available {
		marker := "  "
		if f.Name == current {
			marker = "> "
		}
		fmt.Fprintf(&sb, "%s%s (%s)\n", marker, f.Name, f.DisplayName)
	}
	sb.WriteString("\nUsage: change-font <name>\nExample: change-font firacode\n")
	return sb.String()
}

// ChangeFont switches the font and returns a user-facing message. An empty
// name returns the font list.
func ChangeFont(fc FontChanger, name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return FontList(fc.GetAvailableFonts(), fc.CurrentFont()), nil
	}
	if name == fc.CurrentFont() {
		return fmt.Sprintf("Font is already: %s\n", name), nil
	}
	if err := fc.ChangeFont(name); err != nil {
		var names []string
		for _, f := range fc.GetAvailableFonts() {
			names = append(names, f.Name)
		}
		return fmt.Sprintf("Error: %v\nAvailable fonts: %s\n", err, strings.Join(names, ", ")), err
	}
	return fmt.Sprintf("Font changed to: %s\n", name), nil
}
