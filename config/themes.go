package config

// ThemeOption describes an available UI theme.
type ThemeOption struct {
	Name  string
	Label string
}

// ThemeOptions lists the available themes for the UI.
func ThemeOptions() []ThemeOption {
	return []ThemeOption{
		{Name: "raven-blue", Label: "Raven Blue"},
		{Name: "crow-black", Label: "Crow Black"},
		{Name: "magpie-black-white-grey", Label: "Magpie Black/White/Grey"},
		{Name: "catppuccin-mocha", Label: "Catppuccin Mocha"},
	}
}

// ThemeLabel returns the display label for a theme name.
func ThemeLabel(name string) string {
	for _, opt := range ThemeOptions() {
		if opt.Name == name {
			return opt.Label
		}
	}
	if name == "" {
		return "Raven Blue"
	}
	return name
}
