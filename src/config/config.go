package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// OllamaConfig holds local AI chat settings.
type OllamaConfig struct {
	Enabled         bool   `toml:"enabled"`
	URL             string `toml:"url"`
	Model           string `toml:"model"`
	ThinkingMode    bool   `toml:"thinking_mode"`    // Enable thinking/reasoning mode for supported models
	ThinkingBudget  int    `toml:"thinking_budget"`  // Max tokens for thinking (0 = no limit)
	ShowThinking    bool   `toml:"show_thinking"`    // Show thinking content in UI (collapsible)
	ExtendedTimeout int    `toml:"extended_timeout"` // Extended timeout in seconds for thinking models (0 = default 300s)
	Tools           bool   `toml:"tools"`            // Let the model call read-only tools (web search, file/dir reads, read-only commands)
}

// ShellConfig holds shell-specific settings
type ShellConfig struct {
	// Path to shell binary (empty = system default)
	Path string `toml:"path"`
	// SourceRC whether to source user's rc files (.bashrc, .zshrc, etc.)
	SourceRC bool `toml:"source_rc"`
	// Paths are extra directories prepended to PATH for every shell.
	Paths []string `toml:"paths"`
	// AdditionalEnv extra environment variables
	AdditionalEnv map[string]string `toml:"env"`
}

// CustomCommand represents a user-defined command
type CustomCommand struct {
	Name        string `toml:"name"`
	Command     string `toml:"command"`
	Description string `toml:"description"`
}

// AppearanceConfig holds visual settings
type AppearanceConfig struct {
	CursorStyle       string  `toml:"cursor_style"`        // "block", "underline", "bar"
	CursorBlink       bool    `toml:"cursor_blink"`        // Whether cursor blinks
	PanelWidthPercent float32 `toml:"panel_width_percent"` // Width of side panels (25-50)
	FauxBold          bool    `toml:"faux_bold"`           // Synthesize bold text (offset double-draw)
	FauxItalic        bool    `toml:"faux_italic"`         // Synthesize italic text (shear)
	Undercurl         bool    `toml:"undercurl"`           // Render curly/styled underlines (SGR 4:n)
}

// Config holds the terminal configuration
type Config struct {
	Shell      ShellConfig       `toml:"shell"`
	Prompt     PromptConfig      `toml:"prompt"`
	Scripts    ScriptsConfig     `toml:"scripts"`
	WebSearch  WebSearchConfig   `toml:"web_search"`
	Ollama     OllamaConfig      `toml:"ollama"`
	Appearance AppearanceConfig  `toml:"appearance"`
	Commands   []CustomCommand   `toml:"commands"`
	Aliases    map[string]string `toml:"aliases"`
	Exports    map[string]string `toml:"exports"`
	Theme      string            `toml:"theme"`
	FontSize   float32           `toml:"font_size"`
	// AllowClipboardRead lets applications read the system clipboard via
	// OSC 52 queries. Off by default: clipboard read is a data-exfiltration
	// vector, so kitty and wezterm gate it behind an opt-in too.
	AllowClipboardRead bool `toml:"allow_clipboard_read"`
	// RestoreSession reopens the previous run's tabs, splits, and working
	// directories at startup. Only the layout is restored — shells are fresh.
	RestoreSession bool `toml:"restore_session"`
}

// sha256 of the pre-fix default VCS-detect script (broken ahead/behind
// parsing), kept so LoadConfig can migrate old configs to defaultVCSDetect
// without embedding the full 80-line legacy script.
const defaultVCSDetectLegacyHash = "8478bf11ed2c355fd863f6a8257eb42b2899cc5e01b928010457a5cf6af10d6c"

const defaultLanguageDetect = `# Detect project language
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
`

const defaultVCSDetect = `# Detect VCS (Git + Ivaldi)
_vcs=""
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    _branch=$(git branch --show-current 2>/dev/null || echo "?")

    _ahead=0
    _behind=0
    if git rev-parse --abbrev-ref @{upstream} >/dev/null 2>&1; then
        _counts=$(git rev-list --left-right --count HEAD...@{upstream} 2>/dev/null)
        read -r _behind _ahead <<<"$_counts"
        case "$_behind" in
            (''|*[!0-9]*) _behind=0 ;;
        esac
        case "$_ahead" in
            (''|*[!0-9]*) _ahead=0 ;;
        esac
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
`

// getDefaultLsAlias returns a platform-appropriate ls alias
func getDefaultLsAlias() string {
	switch runtime.GOOS {
	case "darwin":
		return "ls -GC" // BSD ls: -G for color, -C for columns
	default:
		return "ls --color=auto --group-directories-first -C" // GNU ls
	}
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Shell: ShellConfig{
			Path:          "",
			SourceRC:      true,
			Paths:         []string{},
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
			Init:           "",
			PrePrompt:      "",
			LanguageDetect: defaultLanguageDetect,
			VCSDetect:      defaultVCSDetect,
		},
		WebSearch: WebSearchConfig{
			Enabled:        false,
			UseReaderProxy: false,
			ReaderProxyURLs: []string{
				"https://r.jina.ai/",
			},
		},
		Ollama: OllamaConfig{
			Enabled:         false,
			URL:             "http://localhost:11434",
			Model:           "llama3",
			ThinkingMode:    false,
			ThinkingBudget:  0,    // No limit
			ShowThinking:    true, // Show thinking by default
			ExtendedTimeout: 600,  // 10 minutes for thinking models
			Tools:           true, // Read-only tools on by default
		},
		Appearance: AppearanceConfig{
			CursorStyle:       "block",
			CursorBlink:       true,
			PanelWidthPercent: 35.0,
			FauxBold:          true,
			FauxItalic:        true,
			Undercurl:         true,
		},
		Commands: []CustomCommand{},
		Aliases: map[string]string{
			"ls": getDefaultLsAlias(),
		},
		Exports:            map[string]string{},
		Theme:              "raven-blue",
		FontSize:           15.0,
		AllowClipboardRead: false, // opt-in: OSC 52 read leaks clipboard contents to apps
		RestoreSession:     false, // opt-in: reopening old tabs surprises users who expect a clean start
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

// GetSearchHistoryPath returns the path to the persisted search history file
func GetSearchHistoryPath() string {
	return filepath.Join(GetConfigDir(), "search_history.json")
}

// LoadSearchHistory loads persisted search queries (newest first). A missing
// or unreadable file just means no history.
func LoadSearchHistory() []string {
	data, err := os.ReadFile(GetSearchHistoryPath())
	if err != nil {
		return nil
	}
	var history []string
	if err := json.Unmarshal(data, &history); err != nil {
		return nil
	}
	return history
}

// SaveSearchHistory persists search queries best-effort; errors are ignored
// because losing history is harmless.
func SaveSearchHistory(history []string) {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return
	}
	_ = WriteFileAtomic(GetSearchHistoryPath(), data)
}

// WriteFileAtomic writes via a temp file in the same directory plus rename so
// a crash mid-write never leaves a truncated file behind — a truncated
// bookmarks/history file would load as empty and be overwritten on the next
// save, silently losing everything.
func WriteFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Bookmark is a saved page from the search panel preview.
type Bookmark struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

const maxBookmarks = 100

// GetBookmarksPath returns the path to the persisted bookmarks file
func GetBookmarksPath() string {
	return filepath.Join(GetConfigDir(), "bookmarks.json")
}

// LoadBookmarks loads persisted bookmarks (newest first). A missing or
// unreadable file just means no bookmarks.
func LoadBookmarks() []Bookmark {
	data, err := os.ReadFile(GetBookmarksPath())
	if err != nil {
		return nil
	}
	var bookmarks []Bookmark
	if err := json.Unmarshal(data, &bookmarks); err != nil {
		return nil
	}
	return bookmarks
}

// SaveBookmarks persists bookmarks best-effort; errors are ignored because
// losing bookmarks is harmless.
func SaveBookmarks(bookmarks []Bookmark) {
	data, err := json.MarshalIndent(bookmarks, "", "  ")
	if err != nil {
		return
	}
	_ = WriteFileAtomic(GetBookmarksPath(), data)
}

// AddBookmark prepends b, deduping by URL and trimming to the max size.
func AddBookmark(bookmarks []Bookmark, b Bookmark) []Bookmark {
	for i, existing := range bookmarks {
		if existing.URL == b.URL {
			bookmarks = append(bookmarks[:i], bookmarks[i+1:]...)
			break
		}
	}
	bookmarks = append([]Bookmark{b}, bookmarks...)
	if len(bookmarks) > maxBookmarks {
		bookmarks = bookmarks[:maxBookmarks]
	}
	return bookmarks
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
	if fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.Scripts.VCSDetect))) == defaultVCSDetectLegacyHash {
		cfg.Scripts.VCSDetect = defaultVCSDetect
	}

	// Migrate ls alias to platform-appropriate default if it's the old GNU-specific value
	if lsAlias, ok := cfg.Aliases["ls"]; ok {
		if lsAlias == "ls --color=auto --group-directories-first -C" && runtime.GOOS == "darwin" {
			cfg.Aliases["ls"] = getDefaultLsAlias()
		}
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

	// If an existing file is present but unparseable, this save would clobber
	// a config the user could still repair by hand (callers fall back to
	// defaults when Load fails). Keep the original as .bak first.
	if _, err := os.Stat(configPath); err == nil {
		if _, err := toml.DecodeFile(configPath, DefaultConfig()); err != nil {
			_ = os.Rename(configPath, configPath+".bak")
		}
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	return WriteFileAtomic(configPath, buf.Bytes())
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
		"/usr/local/bin/ravenshell",
		"/opt/homebrew/bin/ravenshell",
		"/bin/sh",
		"/usr/bin/sh",
		"/bin/dash",
		"/usr/bin/dash",
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		possibleShells = append(possibleShells, filepath.Join(homeDir, ".local", "bin", "ravenshell"))
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

	// RavenShell may be installed somewhere else on PATH (e.g. ~/go/bin).
	if !seen["ravenshell"] {
		if path, err := exec.LookPath("ravenshell"); err == nil {
			shells = append(shells, path)
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
	var script strings.Builder
	script.WriteString("#!/bin/bash\n")
	script.WriteString("# Raven Terminal Init Script - Auto-generated\n")
	script.WriteString("# Do not edit directly - changes will be overwritten\n")
	script.WriteString("# Edit config.toml instead\n\n")

	// Source user's .bashrc if SourceRC is enabled
	if c.Shell.SourceRC {
		script.WriteString("# Source user's bashrc\n")
		script.WriteString("[ -f \"$HOME/.bashrc\" ] && source \"$HOME/.bashrc\"\n\n")
	}

	// Add user's init script
	if c.Scripts.Init != "" {
		script.WriteString("# User init script\n")
		script.WriteString(c.Scripts.Init + "\n\n")
	}

	// Add language detection function
	script.WriteString("# Language detection function\n")
	script.WriteString("__raven_detect_lang() {\n")
	if c.Scripts.LanguageDetect != "" {
		script.WriteString(c.Scripts.LanguageDetect)
	} else {
		script.WriteString("echo 'None'\n")
	}
	script.WriteString("}\n\n")

	// Add VCS detection function
	script.WriteString("# VCS detection function\n")
	script.WriteString("__raven_detect_vcs() {\n")
	if c.Scripts.VCSDetect != "" {
		script.WriteString(c.Scripts.VCSDetect)
	} else {
		script.WriteString("echo 'None'\n")
	}
	script.WriteString("}\n\n")

	// Add OSC 7 emission for cwd tracking
	script.WriteString("# Emit OSC 7 for current working directory\n")
	script.WriteString("__raven_emit_osc7() {\n")
	script.WriteString("    local _host\n")
	script.WriteString("    _host=\"${HOSTNAME:-$(hostname)}\"\n")
	script.WriteString("    printf '\\e]7;file://%s%s\\a' \"$_host\" \"$PWD\"\n")
	script.WriteString("}\n\n")

	// Add prompt building function based on style
	script.WriteString(c.buildPromptFunction())

	// Add PROMPT_COMMAND
	script.WriteString("\n# Set up prompt\n")
	script.WriteString("PROMPT_COMMAND='__raven_prompt'\n")

	// Add aliases
	if len(c.Aliases) > 0 {
		script.WriteString("\n# Aliases\n")
		for name, cmd := range c.Aliases {
			// zshQuote is plain POSIX single-quoting; a ' in the alias body
			// must not break out of the quotes.
			script.WriteString("alias " + name + "=" + zshQuote(cmd) + "\n")
		}
	}

	// Add exports
	if len(c.Exports) > 0 {
		script.WriteString("\n# Exports\n")
		for name, value := range c.Exports {
			script.WriteString("export " + name + "=\"" + bashDoubleQuote(value) + "\"\n")
		}
	}

	// Prepend user PATH directories (guarded so re-sourcing never duplicates an
	// entry). New shells also get these via the PTY env; this block lets the
	// active tab pick up edits immediately when the init script is re-sourced.
	if len(c.Shell.Paths) > 0 {
		script.WriteString("\n# Raven PATH additions\n")
		for _, dir := range c.Shell.Paths {
			d := bashDoubleQuote(dir)
			script.WriteString("case \":$PATH:\" in *\":" + d + ":\"*) ;; *) PATH=\"" + d + ":$PATH\";; esac\n")
		}
		script.WriteString("export PATH\n")
	}

	if err := os.WriteFile(initPath, []byte(script.String()), 0644); err != nil {
		return "", err
	}

	// Also write the fish-, rsh- and zsh-syntax variants; none of those shells
	// can source init.sh. A write failure must not disable the (already
	// written) bash init, so it is not propagated; each launch path checks its
	// file exists before using it.
	c.writeFishInitScript()
	c.writeRavenInitScript()
	c.writeZshInitScript()

	return initPath, nil
}

// getDistroName reads the distribution name from /etc/os-release
func getDistroName() string {
	opSys := runtime.GOOS
	var distro string
	switch opSys {
	case "linux":
		distro = "linux"
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			for line := range strings.SplitSeq(string(data), "\n") {
				if id, ok := strings.CutPrefix(line, "ID="); ok {
					distro = strings.Trim(id, "\"")
				}
			}
		}
	case "darwin":
		distro = "macos"
	case "windows":
		distro = "windows"
	}
	return distro
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
		script += `    case "$_status" in` + "\n"
		script += `        (''|*[!0-9]*) _status=0 ;;` + "\n"
		script += `    esac` + "\n"
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

// bashDoubleQuote escapes a value for a double-quoted bash/zsh string: $VAR
// expansion stays live (exports rely on it; the fish/rsh generators keep
// parity), but backticks and $( are escaped so a config value can never run
// a command. Not for fish, where both are already inert in double quotes.
func bashDoubleQuote(s string) string {
	s = escapeDoubleQuotes(s)
	s = strings.ReplaceAll(s, "`", "\\`")
	return strings.ReplaceAll(s, "$(", "\\$(")
}
