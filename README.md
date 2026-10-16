# Raven Terminal

A GPU-accelerated terminal emulator written in Go. Rendering is OpenGL 4.1
through GLFW; the VT parser, grid, tabs, splits, and PTY handling are all
implemented in-tree — there is no external terminal backend. Runs on Linux
(X11/Wayland) and macOS.

## Features

**Rendering**
- OpenGL 4.1 GPU-accelerated text rendering with glyph atlases; redraws only when something changed
- Embedded Nerd Fonts with real bold/italic faces, switchable at runtime: JetBrains Mono, Fira Code, Hack, Ubuntu Mono
- Programming ligatures, grapheme clusters (ZWJ emoji, flags, combining marks, mode 2027), color emoji and system-font fallback
- 256-color and 24-bit color, curly/dotted/dashed and colored underlines, overline, blink, reverse video (DECSCNM)
- Themes, including `raven` (follows the RavenLinux desktop); translucent background (`background_opacity`)
- Visual bell, OSC 9;4 progress bars and OSC 133 prompt marks drawn per pane, with a reduced-motion option

**Window management**
- Tabs with a left-side tab bar and per-tab shell processes
- New tabs open next to the tab they were opened from, not at the end of the strip
- Drag a tab chip to reorder, or drag it clear of the strip to tear it into its own window — the running shells move with it
- Multiple windows, each with its own tabs, splits, and panels
- Split panes: vertical or horizontal, nestable, up to 16 panes per tab, with a resize mode
- Optional session restore: reopen the previous run's tabs, splits, and working directories
- Fullscreen toggle and font zoom
- Leader-key shortcuts: Cmd on macOS, Super or Ctrl+Shift on Linux — plain Ctrl stays free for shell control characters, and Super+Ctrl is left to the compositor
- Pane zoom, broadcast input to every pane, and a command palette over every action, theme and font
- Remote control over a Unix socket: `raven-terminal @ ls | new-tab | split | focus-pane | send-text | get-text ...`
- CLI: `-e CMD`, `--hold`, `-d DIR`, `-T TITLE`, `--class`/`--app-id`; the window title follows OSC 0/2

**Terminal**
- Scrollback with reflow on resize, mouse-wheel and keyboard scrolling
- Find in scrollback (Cmd/Super+Shift+F) with smart-case matching across soft-wrapped lines
- Hints mode (URLs, paths, `file:line`, hashes, IPs), vi copy mode with block selection, scrollback in a pager
- Mouse: drag to select-and-copy (also the primary selection), Alt+drag block selection, middle-click paste, Ctrl+right-click to open URLs
- Mouse reporting for applications: X10/normal/button/any-motion, SGR, SGR-pixels (1016), urxvt and UTF-8 encodings, horizontal wheel
- Clickable OSC 8 hyperlinks, underlined on hover
- Keyboard: kitty keyboard protocol (all flags), modifyOtherKeys, xterm modified keys, IME composition (preedit) at the cursor
- Inline images via Kitty graphics (source rectangles, offsets, relative and Unicode-placeholder placements) and Sixel, anchored to scrollback
- Synchronized output (2026), in-band resize (2048), color-scheme reports (2031), left/right margins (DECSLRM), bracketed paste, focus reports
- Queries answered truthfully: DA1/DA2/DA3, XTVERSION, DECRQM, DECRQSS, XTGETTCAP (matching the bundled `xterm-raven` terminfo), OSC 4/10/11/12 colors, OSC 22 pointer shapes
- OSC 52 clipboard and primary selection (reading is opt-in), OSC 7 working directory, OSC 1337 SetUserVar
- Desktop notifications (OSC 9, 99, 777) and a notification when a long command finishes in the background
- Drag and drop files onto a pane to paste their shell-quoted paths
- RavenLinux sudo fingerprint prompt shown as a modal (Settings > Basic > Show sudo fingerprint modal; Esc, × or Use Password falls back to the password; Authentication Settings opens the settings menu). The terminal only presents the prompt: sudo asks for a finger when RavenLinux's Settings > Security > Approve sudo is on

**Shell integration**
- In-terminal settings menu persisted to TOML
- Customizable prompt (minimal / simple / full / custom) with language and VCS detection
- Custom commands, shell aliases, and init / pre-prompt scripts
- Shell integration for bash, zsh, fish and ravenshell with OSC 133 marks: jump between prompts, copy the last command's output, select a command's output with Leader+click
- Shell commands: `keybindings`, `list-fonts`, `change-font <name>` (also `raven-terminal --keybindings` / `--list-fonts`)
- `config.toml` reloads live

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

App shortcuts use a leader key — Cmd on macOS, Super or Ctrl+Shift on Linux —
so plain Ctrl, Alt and Shift chords always reach the running program.
Leader+Shift is Super+Shift, or Ctrl+Shift+Alt with the Ctrl+Shift leader:

| Shortcut | Action |
| --- | --- |
| Leader+K | Keybindings help panel |
| Leader+S | Settings menu |
| Leader+T / Leader+X | New / close tab |
| Leader+D / Leader+Shift+D | Split pane vertically / horizontally |
| Leader+W | Close pane |
| Leader+] / Leader+[ | Next / previous pane |
| Leader+Tab / Leader+Shift+Tab | Next / previous tab |
| Leader+C / Leader+V | Copy / paste |
| Leader+P | Command palette (actions, settings, themes, fonts) |
| Leader+H | Hints: pick a URL, path or hash on screen |
| Leader+Space | Copy mode (vi keys over the scrollback) |
| Leader+Up / Leader+Down | Previous / next shell prompt |
| Leader+Shift+C | Copy the last command's output |
| Leader+Z / Leader+B | Zoom pane / broadcast input to all panes |
| Leader+Shift+H | Scrollback in a pager |
| Middle-click / Alt+drag | Paste PRIMARY / block selection |
| Leader+click on output | Select a command's whole output |
| Leader+F | Toggle web search panel (when enabled) |
| Leader+A | Toggle AI chat panel (when enabled) |
| Leader+Enter / F11 | Toggle fullscreen |
| Leader+R | Pane resize mode |
| Leader+Q | Quit |
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
