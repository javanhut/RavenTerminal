# Raven Terminal Settings

## Shell Commands

The generated shell integration (bash, zsh, fish and ravenshell) defines:

| Command              | Description                                   |
|----------------------|-----------------------------------------------|
| `keybindings`        | Print the keybindings (`raven-terminal --keybindings`) |
| `list-fonts`         | Print the available fonts (`raven-terminal --list-fonts`) |
| `change-font <name>` | Switch the running terminal's font (OSC 7771); no argument lists fonts |

They are ordinary shell functions; the terminal does not intercept typed
input.

### Available Fonts

- `jetbrains` - JetBrains Mono Nerd Font
- `firacode` - Fira Code Nerd Font
- `hack` - Hack Nerd Font
- `ubuntu` - Ubuntu Mono Nerd Font

All four ship regular, bold, italic and bold-italic faces.

## Opening Settings

Press Leader+S (Super+S or Ctrl+Shift+S on Linux, Cmd+S on macOS) to open the settings menu.

## Settings Menu Navigation

| Key | Action |
|-----|--------|
| Up/Down Arrow | Navigate menu items |
| Enter | Select item / Confirm input / Toggle option |
| Escape | Go back / Cancel input / Close menu |
| Delete | Delete selected command or alias |

## Menu Structure

The settings menu is organized into the following sections:

### Main Menu
- **Shell**: Select which shell to use (bash, zsh, fish, etc.)
- **Source RC Files**: Toggle whether to source your shell's rc files (ON/OFF)
- **Theme**: Select a UI theme
- **Prompt Style**: Select prompt style (minimal, simple, full, custom)
- **Prompt Options**: Toggle individual prompt elements
- **Scripts**: Edit initialization and detection scripts
- **Web Search**: Toggle built-in web search panel (off by default)
- **Web Search Reader Proxy**: Toggle text-only proxy for search previews
- **Ollama Chat**: Toggle local AI chat panel (off by default)
- **Ollama URL**: Set the Ollama base URL (e.g., http://localhost:11434)
- **Ollama Model**: Set the Ollama model name (e.g., llama3)
- **Ollama Test Connection**: Verify the Ollama URL is reachable
- **Ollama Refresh Models**: Pull the available model list from `/api/tags`
- **Ollama Models**: Pick a model from the fetched list
- **Commands**: Add/edit/delete custom commands
- **Aliases**: Add/edit/delete shell aliases
- **Reload Config**: Reload settings from config.toml
- **Save and Close**: Save all changes to config.toml
- **Cancel**: Discard changes and close menu

### Adding Commands

1. Navigate to Commands
2. Select "+ Add New Command"
3. Enter the command name (e.g., "update")
4. Enter the command to run (e.g., "sudo pacman -Syu")
5. Optionally enter a description

To edit an existing command, select it from the list.

### Adding Aliases

1. Navigate to Aliases
2. Select "+ Add New Alias"
3. Enter the alias name (e.g., "ll")
4. Enter the command (e.g., "ls -la")

To edit an existing alias, select it from the list.

### Deleting Commands/Aliases

1. Navigate to Commands or Aliases
2. Select the item to delete
3. Press the Delete key

### Editing Scripts

Scripts can be edited directly from the menu:
1. Navigate to Scripts
2. Select the script to edit
3. The current value is shown in a multi-line text area
4. Press Enter to insert a new line
5. Press Ctrl+Enter to save, Escape to cancel

## Configuration File

Settings are stored in TOML format at `~/.config/raven-terminal/config.toml`.

On first run, a default configuration is created automatically.

## Configuration Options

### Theme

```toml
theme = "raven" # "raven" (follow the desktop), "raven-blue", "crow-black",
                # "magpie-black-white-grey", "catppuccin-mocha", "ghostty"
font_size = 15.0
```

Applications see the theme's colours through OSC 4/10/11/12 queries and get
dark/light change reports (mode 2031).

### Terminal Behaviour

```toml
scrollback_lines = 10000      # history per pane; 0 disables scrollback
bell = "visual"               # "visual" (flash, plus urgency when unfocused), "attention", "none"
notifications = true          # desktop notifications from apps (OSC 9, 99, 777)
notify_command_after = 10     # notify when a command that ran this many seconds
                              # finishes while unfocused (0 = never; needs shell integration)
allow_clipboard_read = false  # let apps read the clipboard via OSC 52 (writes always work)
allow_window_ops = false      # let apps resize the window (XTWINOPS CSI 8 t / CSI 4 t)
restore_session = false       # reopen the previous run's tabs, splits and directories
fingerprint_sudo = true       # show RavenLinux's sudo fingerprint prompt as a modal
```

### Appearance

```toml
[appearance]
cursor_style = "block"        # "block", "underline", "bar" (apps can change it with DECSCUSR)
cursor_blink = true
panel_width_percent = 35.0    # web search / AI panel width (25-50)
faux_bold = true              # bold text (real bold face, synthesized when missing)
faux_italic = true            # italic text (real italic face, synthesized when missing)
undercurl = true              # curly, dotted and dashed underlines (SGR 4:3..4:5) and SGR 58 colours
ligatures = true              # programming ligatures (=>, !=, ...)
text_gamma = 1.7              # extra gamma for text darker than the background (1 = off)
text_contrast = 1.0           # text coverage multiplier (1 = off)
marks_gutter = true           # mark each shell prompt (OSC 133) in the left gutter
reduced_motion = false        # steady bell tint instead of a fading flash, no blinking text, static progress bars
background_opacity = 1.0      # 0.1-1; below 1 the window is translucent
```

Turning `background_opacity` below 1 while the terminal is running applies to
windows opened afterwards (the transparent framebuffer is chosen when a window
is created); going back to 1 applies at once.

### Shell Settings

```toml
[shell]
path = ""           # Shell path (empty = system default)
home = ""           # HOME for new shells (empty = inherit the terminal's $HOME)
source_rc = true    # Whether to source .bashrc/.zshrc etc.

[shell.env]         # Additional environment variables
# MY_VAR = "value"
```

- **path**: Specify a shell path like `/usr/bin/zsh` or leave empty for system default
- **home**: The `HOME` new shells get, and the directory they start in when none is given. Empty keeps whatever `HOME` the terminal was launched with, falling back to your account's home if that is unset. `~/` means your account's home, so `~/profiles/work` gives shells a separate profile. Also under Settings > Basic Settings > Home.
- **source_rc**: When `true`, sources your shell's rc files (.bashrc, .zshrc, etc.)
- **[shell.env]**: Variables set in the environment of every new pane, before the shell starts, so `-e` commands and programs that never read shell init files see them too. They override the terminal's own defaults (`TERM`, `COLORTERM`, …). A `HOME` here is used when `home` above is empty, with the same `~/` expansion. Unlike `[exports]`, which the shell's init script runs, values are taken literally, with no `$VAR` expansion.

### Prompt Settings

```toml
[prompt]
style = "full"          # "minimal", "simple", "full", or "custom"
show_path = true        # Show current directory
show_username = true    # Show username
show_hostname = true    # Show hostname
show_language = true    # Show detected programming language
show_vcs = true         # Show VCS info (Git/Ivaldi)
custom_script = ""      # Custom prompt script (for style = "custom")
```

#### Prompt Styles

| Style | Description |
|-------|-------------|
| minimal | Just `>` |
| simple | Path and `>` |
| full | Path, language, VCS, username, hostname |
| custom | Uses your custom_script |

### Scripts Configuration

The scripts section allows you to customize how Raven Terminal detects project information:

```toml
[scripts]
# Runs once when shell starts
init = '''
export PATH="$HOME/.local/bin:$PATH"
'''

# Runs before each prompt (optional)
pre_prompt = ""

# Language detection script (should echo the result)
language_detect = '''
[ -f go.mod ] && echo "Go" && return 0
[ -f Cargo.toml ] && echo "Rust" && return 0
[ -f package.json ] && echo "JavaScript" && return 0
[ -f pyproject.toml ] && echo "Python" && return 0
[ -f requirements.txt ] && echo "Python" && return 0
[ -f Gemfile ] && echo "Ruby" && return 0
[ -f pom.xml ] && echo "Java" && return 0
[ -f CMakeLists.txt ] && echo "C/C++" && return 0
ls *.crl >/dev/null 2>&1 && echo "Carrion" && return 0
echo "None"
'''

# VCS detection script (should echo the result)
vcs_detect = '''
_vcs=""
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    _branch=$(git branch --show-current 2>/dev/null || echo "?")
    _vcs="Git($_branch)"
fi
if [ -d .ivaldi ] || [ -f .ivaldi ]; then
    [ -n "$_vcs" ] && _vcs="$_vcs+Ivaldi" || _vcs="Ivaldi"
fi
[ -z "$_vcs" ] && _vcs="None"
echo "$_vcs"
'''
```

### Web Search

```toml
[web_search]
enabled = false
use_reader_proxy = false
reader_proxy_urls = ["https://r.jina.ai/"]
```

- **enabled**: Allow Raven Terminal to make outbound web requests for the search panel
- **use_reader_proxy**: Use a text-only proxy fallback for JS-heavy pages
- **reader_proxy_urls**: Proxy base URLs to try in order (target URL appended)

Search history is persisted best-effort to
`~/.config/raven-terminal/search_history.json` next to the config file (last
100 queries, newest first). Bookmarks (Ctrl+B in the search panel preview) are
persisted the same way to `~/.config/raven-terminal/bookmarks.json` (last 100,
deduped by URL, newest first). Delete either file to clear it.

### Ollama Chat

```toml
[ollama]
enabled = false
url = "http://localhost:11434"
model = "llama3"
```

- **enabled**: Show the AI chat panel and allow local Ollama requests
- **url**: Base URL for the Ollama server
- **model**: Model name to load for quick questions

### Custom Commands

```toml
[[commands]]
name = "update"
command = "sudo pacman -Syu"
description = "Update system packages"

[[commands]]
name = "clean"
command = "sudo pacman -Sc"
description = "Clean package cache"
```

### Aliases

```toml
[aliases]
ls = "ls --color=auto -p -C"
ll = "ls -la"
gs = "git status"
gp = "git push"
```

## Example Configuration

```toml
theme = "raven-blue"

[shell]
path = "/usr/bin/zsh"
source_rc = true

[shell.env]
EDITOR = "nvim"

[prompt]
style = "full"
show_path = true
show_username = true
show_hostname = true
show_language = true
show_vcs = true

[scripts]
init = '''
export PATH="$HOME/.local/bin:$PATH"
'''

language_detect = '''
[ -f go.mod ] && echo "Go" && return 0
[ -f Cargo.toml ] && echo "Rust" && return 0
[ -f package.json ] && echo "JavaScript" && return 0
echo "None"
'''

vcs_detect = '''
_vcs=""
git rev-parse --is-inside-work-tree >/dev/null 2>&1 && _vcs="Git($(git branch --show-current 2>/dev/null))"
[ -d .ivaldi ] && { [ -n "$_vcs" ] && _vcs="$_vcs+Ivaldi" || _vcs="Ivaldi"; }
[ -z "$_vcs" ] && _vcs="None"
echo "$_vcs"
'''

[web_search]
enabled = false
use_reader_proxy = false
reader_proxy_urls = ["https://r.jina.ai/"]

[ollama]
enabled = false
url = "http://localhost:11434"
model = "llama3"

[[commands]]
name = "dev"
command = "cd ~/Development"
description = "Go to dev folder"

[aliases]
ll = "ls -la"
gs = "git status"
```

## Generated Scripts

Raven Terminal generates an init script at `~/.config/raven-terminal/scripts/init.sh` based on your configuration. This script is automatically sourced when you open a new terminal tab.

The generated script includes:
- Your custom init script
- Language detection function (`__raven_detect_lang`)
- VCS detection function (`__raven_detect_vcs`)
- Prompt function (`__raven_prompt`)
- Your aliases

## Applying Changes

`config.toml` is watched: saving it (from the settings menu or an editor)
applies theme, font size, text style, scrollback, bell, notifications, cursor
and panel settings to every open window without a restart. Shell, prompt,
script and alias changes are written to the generated init script and take
effect in new tabs.

## Migrating from Other Terminals

Since `source_rc = true` by default, your existing `.bashrc` or `.zshrc` will be sourced automatically. Raven Terminal adds its own prompt on top of your existing configuration.

To use only Raven Terminal's prompt, set `source_rc = false` and configure everything in the TOML file.
