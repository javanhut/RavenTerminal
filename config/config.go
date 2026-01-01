package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// PromptConfig holds prompt customization settings
type PromptConfig struct {
	Style              string `toml:"style"` // "minimal", "simple", "full", "custom"
	ShowPath           bool   `toml:"show_path"`
	ShowUsername       bool   `toml:"show_username"`
	ShowHostname       bool   `toml:"show_hostname"`
	ShowLanguage       bool   `toml:"show_language"`
	ShowVCS            bool   `toml:"show_vcs"`
	CustomPromptScript string `toml:"custom_script"` // Custom script for prompt
}

// ScriptsConfig holds custom scripts configuration
type ScriptsConfig struct {
	// Init script runs once when shell starts
	Init string `toml:"init"`
	// PrePrompt runs before each prompt (like PROMPT_COMMAND)
	PrePrompt string `toml:"pre_prompt"`
	// LanguageDetect custom script for language detection (should echo result)
	LanguageDetect string `toml:"language_detect"`
	// VCSDetect custom script for VCS detection (should echo result)
	VCSDetect string `toml:"vcs_detect"`
}

// WebSearchConfig holds web search settings
type WebSearchConfig struct {
	Enabled bool `toml:"enabled"`
	// UseReaderProxy enables a text-only proxy fallback for JS-heavy pages.
	UseReaderProxy bool `toml:"use_reader_proxy"`
	// ReaderProxyURLs lists proxy base URLs to try for text extraction.
	ReaderProxyURLs []string `toml:"reader_proxy_urls"`
}

// ShellConfig holds shell-specific settings
type ShellConfig struct {
	// Path to shell binary (empty = system default)
	Path string `toml:"path"`
	// SourceRC whether to source user's rc files (.bashrc, .zshrc, etc.)
	SourceRC bool `toml:"source_rc"`
	// AdditionalEnv extra environment variables
	AdditionalEnv map[string]string `toml:"env"`
}

// CustomCommand represents a user-defined command
type CustomCommand struct {
	Name        string `toml:"name"`
	Command     string `toml:"command"`
	Description string `toml:"description"`
}

// Config holds the terminal configuration
type Config struct {
	Shell     ShellConfig       `toml:"shell"`
	Prompt    PromptConfig      `toml:"prompt"`
	Scripts   ScriptsConfig     `toml:"scripts"`
	WebSearch WebSearchConfig   `toml:"web_search"`
	Commands  []CustomCommand   `toml:"commands"`
	Aliases   map[string]string `toml:"aliases"`
	Exports   map[string]string `toml:"exports"`
	Theme     string            `toml:"theme"`
	FontSize  float32           `toml:"font_size"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Shell: ShellConfig{
			Path:          "",
			SourceRC:      true,
			AdditionalEnv: map[string]string{},
		},
		Prompt: PromptConfig{
			Style:        "full",
			ShowPath:     true,
			ShowUsername: true,
			ShowHostname: true,
			ShowLanguage: true,
			ShowVCS:      true,
		},
		Scripts: ScriptsConfig{
			Init:      "",
			PrePrompt: "",
			LanguageDetect: `# Detect project language
[ -f go.mod ] && echo "Go" && return 0
[ -f Cargo.toml ] && echo "Rust" && return 0
[ -f package.json ] && echo "JavaScript" && return 0
[ -f pyproject.toml ] && echo "Python" && return 0
[ -f requirements.txt ] && echo "Python" && return 0
[ -f Pipfile ] && echo "Python" && return 0
[ -f Gemfile ] && echo "Ruby" && return 0
[ -f pom.xml ] && echo "Java" && return 0
[ -f build.gradle ] && echo "Java" && return 0
[ -f CMakeLists.txt ] && echo "C/C++" && return 0
[ -f Makefile ] && echo "C/C++" && return 0
ls *.crl >/dev/null 2>&1 && echo "Carrion" && return 0
echo "None"
`,
			VCSDetect: `# Detect VCS (Git + Ivaldi)
_vcs=""
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    _branch=$(git branch --show-current 2>/dev/null || echo "?")

    _ahead=0
    _behind=0
    if git rev-parse --abbrev-ref @{upstream} >/dev/null 2>&1; then
        _counts=$(git rev-list --left-right --count HEAD...@{upstream} 2>/dev/null)
        _behind=${_counts%% *}
        _ahead=${_counts##* }
    fi

    _staged=0
    _unstaged=0
    _untracked=0
    while IFS= read -r _line; do
        case "${_line:0:2}" in
            "??") _untracked=$((_untracked + 1)) ;;
            *) 
                [ "${_line:0:1}" != " " ] && _staged=$((_staged + 1))
                [ "${_line:1:1}" != " " ] && _unstaged=$((_unstaged + 1))
                ;;
        esac
    done < <(git status --porcelain 2>/dev/null)

    _state=""
    [ "$_ahead" -gt 0 ] && _state="$_state ^$_ahead"
    [ "$_behind" -gt 0 ] && _state="$_state v$_behind"
    [ "$_staged" -gt 0 ] && _state="$_state +$_staged"
    [ "$_unstaged" -gt 0 ] && _state="$_state ~$_unstaged"
    [ "$_untracked" -gt 0 ] && _state="$_state ?$_untracked"

    if [ -n "$_state" ]; then
        _vcs="Git($_branch$_state)"
    else
        _vcs="Git($_branch)"
    fi
fi

_ivaldi_tl=""
_ivaldi_present=""
if command -v ivaldi >/dev/null 2>&1; then
    _ivaldi_raw="$(ivaldi whereami 2>/dev/null)"
    if [ -z "$_ivaldi_raw" ]; then
        _ivaldi_raw="$(ivaldi wai 2>/dev/null)"
    fi
    if [ -n "$_ivaldi_raw" ]; then
        _ivaldi_present="1"
    fi
    _ivaldi_tl=$(printf "%s\n" "$_ivaldi_raw" | awk -F: 'tolower($1) ~ /^[[:space:]]*timeline[[:space:]]*$/ {sub(/^[[:space:]]+/, "", $2); gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2; exit}')
fi
if [ -z "$_ivaldi_tl" ] && [ -f .ivaldi ]; then
    _ivaldi_present="1"
    _ivaldi_tl=$(awk -F: 'tolower($1) ~ /^[[:space:]]*timeline[[:space:]]*$/ {sub(/^[[:space:]]+/, "", $2); gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2; exit} NF{print; exit}' .ivaldi 2>/dev/null)
fi
if [ -z "$_ivaldi_tl" ] && [ -d .ivaldi ]; then
    _ivaldi_present="1"
    for _ivaldi_file in .ivaldi/timeline .ivaldi/whereami .ivaldi/wai; do
        if [ -f "$_ivaldi_file" ]; then
            _ivaldi_tl=$(awk -F: 'tolower($1) ~ /^[[:space:]]*timeline[[:space:]]*$/ {sub(/^[[:space:]]+/, "", $2); gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2; exit} NF{print; exit}' "$_ivaldi_file" 2>/dev/null)
            [ -n "$_ivaldi_tl" ] && break
        fi
    done
fi
if [ -n "$_ivaldi_tl" ] || [ -n "$_ivaldi_present" ]; then
    if [ -n "$_ivaldi_tl" ]; then
        _ivaldi_display="Ivaldi (tl: $_ivaldi_tl)"
    else
        _ivaldi_display="Ivaldi"
    fi
    if [ -n "$_vcs" ]; then
        _vcs="$_vcs | $_ivaldi_display"
    else
        _vcs="$_ivaldi_display"
    fi
fi

[ -z "$_vcs" ] && _vcs="None"
echo "$_vcs"
`,
		},
		WebSearch: WebSearchConfig{
			Enabled:        false,
			UseReaderProxy: false,
			ReaderProxyURLs: []string{
				"https://r.jina.ai/",
			},
		},
		Commands: []CustomCommand{},
		Aliases: map[string]string{
			"ls": "ls --color=auto --group-directories-first -C",
		},
		Exports:  map[string]string{},
		Theme:    "raven-blue",
		FontSize: 15.0,
	}
}

// GetConfigDir returns the config directory path
func GetConfigDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".config/raven-terminal"
	}
	return filepath.Join(homeDir, ".config", "raven-terminal")
}

// GetConfigPath returns the path to the config file
func GetConfigPath() string {
	return filepath.Join(GetConfigDir(), "config.toml")
}

// GetScriptsDir returns the path to the scripts directory
func GetScriptsDir() string {
	return filepath.Join(GetConfigDir(), "scripts")
}

// Load loads the configuration from disk
func Load() (*Config, error) {
	configPath := GetConfigPath()

	// Ensure config directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, err
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config
		cfg := DefaultConfig()
		if err := cfg.Save(); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	// Load existing config
	cfg := DefaultConfig()
	if _, err := toml.DecodeFile(configPath, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save saves the configuration to disk
func (c *Config) Save() error {
	configPath := GetConfigPath()

	// Ensure config directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	// Create scripts directory
	scriptsDir := GetScriptsDir()
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		return err
	}

	// Write config file
	f, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	return encoder.Encode(c)
}

// GetAvailableShells returns a list of available shells on the system
func GetAvailableShells() []string {
	shells := []string{}
	possibleShells := []string{
		"/bin/bash",
		"/usr/bin/bash",
		"/bin/zsh",
		"/usr/bin/zsh",
		"/bin/fish",
		"/usr/bin/fish",
		"/bin/sh",
		"/usr/bin/sh",
		"/bin/dash",
		"/usr/bin/dash",
	}

	seen := make(map[string]bool)
	for _, shell := range possibleShells {
		if _, err := os.Stat(shell); err == nil {
			base := filepath.Base(shell)
			if !seen[base] {
				seen[base] = true
				shells = append(shells, shell)
			}
		}
	}
	return shells
}

// WriteInitScript writes the init script to the scripts directory
func (c *Config) WriteInitScript() (string, error) {
	scriptsDir := GetScriptsDir()
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		return "", err
	}

	initPath := filepath.Join(scriptsDir, "init.sh")

	// Build the init script content
	script := "#!/bin/bash\n"
	script += "# Raven Terminal Init Script - Auto-generated\n"
	script += "# Do not edit directly - changes will be overwritten\n"
	script += "# Edit config.toml instead\n\n"

	// Source user's .bashrc if SourceRC is enabled
	if c.Shell.SourceRC {
		script += "# Source user's bashrc\n"
		script += "[ -f \"$HOME/.bashrc\" ] && source \"$HOME/.bashrc\"\n\n"
	}

	// Add user's init script
	if c.Scripts.Init != "" {
		script += "# User init script\n"
		script += c.Scripts.Init + "\n\n"
	}

	// Add language detection function
	script += "# Language detection function\n"
	script += "__raven_detect_lang() {\n"
	if c.Scripts.LanguageDetect != "" {
		script += c.Scripts.LanguageDetect
	} else {
		script += "echo 'None'\n"
	}
	script += "}\n\n"

	// Add VCS detection function
	script += "# VCS detection function\n"
	script += "__raven_detect_vcs() {\n"
	if c.Scripts.VCSDetect != "" {
		script += c.Scripts.VCSDetect
	} else {
		script += "echo 'None'\n"
	}
	script += "}\n\n"

	// Add OSC 7 emission for cwd tracking
	script += "# Emit OSC 7 for current working directory\n"
	script += "__raven_emit_osc7() {\n"
	script += "    local _host\n"
	script += "    _host=\"${HOSTNAME:-$(hostname)}\"\n"
	script += "    printf '\\e]7;file://%s%s\\a' \"$_host\" \"$PWD\"\n"
	script += "}\n\n"

	// Add prompt building function based on style
	script += c.buildPromptFunction()

	// Add PROMPT_COMMAND
	script += "\n# Set up prompt\n"
	script += "PROMPT_COMMAND='__raven_prompt'\n"

	// Add aliases
	if len(c.Aliases) > 0 {
		script += "\n# Aliases\n"
		for name, cmd := range c.Aliases {
			script += "alias " + name + "='" + cmd + "'\n"
		}
	}

	// Add exports
	if len(c.Exports) > 0 {
		script += "\n# Exports\n"
		for name, value := range c.Exports {
			script += "export " + name + "=\"" + escapeDoubleQuotes(value) + "\"\n"
		}
	}

	if err := os.WriteFile(initPath, []byte(script), 0644); err != nil {
		return "", err
	}

	return initPath, nil
}

// getDistroName reads the distribution name from /etc/os-release
func getDistroName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "linux"
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "ID=") {
			id := strings.TrimPrefix(line, "ID=")
			id = strings.Trim(id, "\"")
			return id
		}
	}
	return "linux"
}

// buildPromptFunction builds the __raven_prompt function based on config
func (c *Config) buildPromptFunction() string {
	// Colors
	cyan := `\e[0;36m`
	green := `\e[0;32m`
	yellow := `\e[0;33m`
	magenta := `\e[0;35m`
	blue := `\e[0;34m`
	red := `\e[0;31m`
	dim := `\e[0;90m`
	reset := `\e[0m`

	distro := getDistroName()

	script := "# Prompt function\n"
	script += "__raven_prompt() {\n"

	switch c.Prompt.Style {
	case "minimal":
		script += `    PS1="> "` + "\n"

	case "simple":
		script += `    PS1="\[` + cyan + `\]\w\[` + reset + `\] > "` + "\n"

	case "custom":
		if c.Prompt.CustomPromptScript != "" {
			script += "    " + c.Prompt.CustomPromptScript + "\n"
		} else {
			script += `    PS1="> "` + "\n"
		}

	case "full":
		fallthrough
	default:
		script += `    local _status=$?` + "\n"
		// Build line 1
		script += `    local _line1=""` + "\n"
		if c.Prompt.ShowPath {
			script += `    _line1="\[` + cyan + `\]\w\[` + reset + `\]"` + "\n"
		}
		if c.Prompt.ShowLanguage {
			script += `    if [ -n "$_line1" ]; then` + "\n"
			script += `        _line1="$_line1 \[` + dim + `\] | \[` + blue + `\]Lang:\[` + reset + `\] \[` + yellow + `\]$(__raven_detect_lang)\[` + reset + `\]"` + "\n"
			script += `    else` + "\n"
			script += `        _line1="\[` + blue + `\]Lang:\[` + reset + `\] \[` + yellow + `\]$(__raven_detect_lang)\[` + reset + `\]"` + "\n"
			script += `    fi` + "\n"
		}
		if c.Prompt.ShowVCS {
			script += `    if [ -n "$_line1" ]; then` + "\n"
			script += `        _line1="$_line1 \[` + dim + `\] | \[` + blue + `\]VCS:\[` + reset + `\] \[` + magenta + `\]$(__raven_detect_vcs)\[` + reset + `\]"` + "\n"
			script += `    else` + "\n"
			script += `        _line1="\[` + blue + `\]VCS:\[` + reset + `\] \[` + magenta + `\]$(__raven_detect_vcs)\[` + reset + `\]"` + "\n"
			script += `    fi` + "\n"
		}

		// Build line 2
		script += `    local _line2=""` + "\n"
		if c.Prompt.ShowUsername || c.Prompt.ShowHostname {
			script += `    _line2="["` + "\n"
			if c.Prompt.ShowUsername {
				script += `    _line2="$_line2\[` + green + `\]\u\[` + reset + `\]"` + "\n"
			}
			if c.Prompt.ShowUsername && c.Prompt.ShowHostname {
				script += `    _line2="$_line2@"` + "\n"
			}
			if c.Prompt.ShowHostname {
				// Use distro name instead of hostname
				script += `    _line2="$_line2\[` + yellow + `\]` + distro + `\[` + reset + `\]"` + "\n"
			}
			script += `    _line2="$_line2] "` + "\n"
		}
		script += `    if [ $_status -ne 0 ]; then` + "\n"
		script += `        _line2="$_line2\[` + red + `\]err:$_status\[` + reset + `\] "` + "\n"
		script += `    fi` + "\n"
		script += `    _line2="$_line2\[` + dim + `\]>\[` + reset + `\] "` + "\n"

		// Combine
		script += `    PS1="$_line1\n$_line2"` + "\n"
	}

	script += "    __raven_emit_osc7\n"
	script += "}\n"
	return script
}

// Backward compatibility functions

// AddCustomCommand adds a new custom command
func (c *Config) AddCustomCommand(name, command, description string) {
	c.Commands = append(c.Commands, CustomCommand{
		Name:        name,
		Command:     command,
		Description: description,
	})
}

// RemoveCustomCommand removes a custom command by index
func (c *Config) RemoveCustomCommand(index int) {
	if index >= 0 && index < len(c.Commands) {
		c.Commands = append(c.Commands[:index], c.Commands[index+1:]...)
	}
}

// SetAlias sets an alias
func (c *Config) SetAlias(name, command string) {
	if c.Aliases == nil {
		c.Aliases = make(map[string]string)
	}
	c.Aliases[name] = command
}

// RemoveAlias removes an alias
func (c *Config) RemoveAlias(name string) {
	delete(c.Aliases, name)
}

// SetExport sets an export value
func (c *Config) SetExport(name, value string) {
	if c.Exports == nil {
		c.Exports = make(map[string]string)
	}
	c.Exports[name] = value
}

// RemoveExport removes an export
func (c *Config) RemoveExport(name string) {
	delete(c.Exports, name)
}

func escapeDoubleQuotes(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "\"", "\\\"")
}
