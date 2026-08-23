# Raven Terminal

A GPU-accelerated terminal emulator written in Go. Rendering is OpenGL 4.1
through GLFW; the VT parser, grid, tabs, splits, and PTY handling are all
implemented in-tree — there is no external terminal backend. Runs on Linux
(X11/Wayland) and macOS.

## Features

**Rendering**
- OpenGL 4.1 GPU-accelerated text rendering with glyph atlases
- Embedded Nerd Fonts, switchable at runtime: FiraCode, Hack, JetBrains Mono, Ubuntu Mono
- Color emoji and system-font fallback for missing glyphs
- 256-color and 24-bit true-color support
- Built-in themes: `raven-blue`, `crow-black`, `magpie-black-white-grey`, `catppuccin-mocha`

**Window management**
- Tabs with a left-side tab bar and per-tab shell processes
- New tabs open next to the tab they were opened from, not at the end of the strip
- Drag a tab chip to reorder, or drag it clear of the strip to tear it into its own window — the running shells move with it
- Multiple windows, each with its own tabs, splits, and panels
- Split panes: vertical or horizontal, nestable, up to 16 panes per tab, with a resize mode
- Optional session restore: reopen the previous run's tabs, splits, and working directories
- Fullscreen toggle and font zoom
- Leader-key shortcuts: Cmd on macOS, Super or Ctrl+Shift on Linux — plain Ctrl stays free for shell control characters

**Terminal**
- Scrollback buffer with mouse-wheel and keyboard scrolling
- Find in scrollback (Cmd/Super+Shift+F) with smart-case matching across soft-wrapped lines
- Mouse: drag to select-and-copy, right-click to paste, Ctrl+right-click to open URLs
- Clickable OSC 8 hyperlinks, underlined on hover
- Kitty keyboard protocol
- Inline images via Kitty graphics and Sixel, anchored to scrollback so they scroll with content

**Shell integration**
- In-terminal settings menu persisted to TOML
- Customizable prompt (minimal / simple / full / custom) with language and VCS detection
- Custom commands, shell aliases, and init / pre-prompt scripts
- Built-in commands: `keybindings`, `list-fonts`, `change-font <name>`

**Web search panel** (optional, off by default)
- DuckDuckGo search with in-terminal page previews
- Reader-proxy mode for JS-heavy pages
- Bookmarks, persistent query history, in-page find, numbered link following
- Send a page or selection to the AI panel, or insert it into the shell

**AI chat panel** (optional, off by default)
- Chat with a local [Ollama](https://ollama.com) model
- Strictly read-only toolset: `web_search`, `fetch_page`, `read_file`, `list_dir`, `run_command` (allowlisted binaries, no shell)
- Workspace-scoped: file and command access is confined to the active pane's directory; page fetching rejects private-network targets

## Requirements

- Go 1.25 or later (build only)
- OpenGL 4.1 compatible graphics driver
- Linux (X11 or Wayland) or macOS

### Compatibility

| Platform | Status | Notes |
| --- | --- | --- |
| Linux | Supported | X11/Wayland through GLFW |
| macOS | Supported | OpenGL 4.1; Homebrew setup available |
| Windows | Not supported | PTY and install integration are Unix-oriented |

## Installation

### Quick install (from GitHub)

```bash
curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-install.sh | bash
```

System-wide install:

```bash
curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-install.sh | bash -s -- --global
```

### From source

```bash
git clone https://github.com/javanhut/RavenTerminal.git
cd RavenTerminal
make deps           # install build dependencies (distro-detected)
make                # build ./raven-terminal
make install-local  # install to ~/.local/bin/
```

Remove it with `make uninstall`, or `make purge` to also delete config and
caches. See [docs/installation.md](docs/installation.md) for the full guide
including remote uninstallation.

## Usage

Launch `raven-terminal` from your application menu or the command line.

App shortcuts use a leader key — Cmd on macOS, Super or Ctrl+Shift on Linux:

| Shortcut | Action |
| --- | --- |
| Leader+K | Keybindings help panel |
| Leader+S | Settings menu |
| Leader+T / Leader+X | New / close tab |
| Leader+D / Leader+Shift+D | Split pane vertically / horizontally |
| Leader+W | Close pane |
| Leader+F | Toggle web search panel (when enabled) |
| Leader+A | Toggle AI chat panel (when enabled) |
| Shift+Enter | Toggle fullscreen |
| Leader+= / Leader+- / Leader+0 | Zoom in / out / reset |

The full reference is in [docs/keybindings.md](docs/keybindings.md).

## Configuration

Configuration lives at `~/.config/raven-terminal/config.toml` and is created
with defaults on first run. Everything — theme, shell, prompt, scripts, web
search, Ollama, custom commands, aliases — is editable from the settings menu
(Leader+S) or by hand. See [docs/settings.md](docs/settings.md).

## Development

```bash
make check   # gofmt + go vet + go test (matches CI)
go test -race ./src ./src/grid ./src/parser ./src/aitools ./src/shell ./src/websearch ./src/ollama ./src/tab
```

CI additionally runs `govulncheck`. A `lazy.toml` for the `imlazy` task
runner is included: `imlazy ci` runs check + race + vuln in one step.

See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.

## Documentation

- [Installation](docs/installation.md) — install, uninstall, troubleshooting
- [Keybindings](docs/keybindings.md) — complete keybinding reference
- [Settings](docs/settings.md) — configuration options and built-in commands
- [Split panes](docs/splits.md) — split pane usage and navigation
- [Architecture](docs/ARCHITECTURE.md) — project structure and internals
- [AI tools and privacy](docs/ai-tools.md) — tool capabilities and boundaries
- [Icon](docs/icon.md) — application icon customization

## License

MIT — see [LICENSE](LICENSE) for details.
