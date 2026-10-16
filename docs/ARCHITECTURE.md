# Raven Terminal Architecture

This document describes the internal architecture and project structure of Raven Terminal.

## Project Structure

```
RavenTerminal/
├── src/                    # Main source code (package main: main.go, app.go, ...)
│   ├── aipanel/            # AI chat panel (Ollama integration UI)
│   ├── aitools/            # Tools the AI chat panel can call
│   ├── assets/             # Embedded fonts and icons
│   ├── commands/           # Built-in terminal commands
│   ├── config/             # Config, themes, desktop.toml glass, shell init scripts
│   ├── fingerprint/        # RavenLinux sudo fingerprint prompt detection
│   ├── grid/               # Terminal grid, scrollback, reflow
│   ├── images/             # Inline image model (Kitty graphics, Sixel)
│   ├── keybindings/        # Keyboard input encoding and bindings
│   ├── menu/               # Settings menu UI
│   ├── ollama/             # Ollama AI backend integration
│   ├── parser/             # VT/ANSI escape sequence parser
│   ├── render/             # OpenGL 4.1 renderer
│   ├── searchpanel/        # Web search panel UI
│   ├── shell/              # PTY/shell handling, TERM selection
│   ├── sixel/              # DECSIXEL decoder
│   ├── tab/                # Tabs and split panes
│   ├── websearch/          # Web search backend
│   └── window/             # GLFW window management
├── third_party/glfw/       # Vendored GLFW Go bindings (X11 + Wayland)
├── terminfo/               # xterm-raven terminfo source
├── docs/                   # Documentation
├── scripts/                # Installation scripts
├── lazy.toml               # Build interface (imlazy)
├── go.mod                  # Go module definition
└── README.md               # Project overview
```

## Core Components

### Renderer (`src/render/`)

The OpenGL 4.1 renderer is responsible for all visual output:

- **GPU-accelerated text rendering** using glyph atlases
- **Font management** with embedded Nerd Font support
- **Color handling** for 256-color and true-color modes
- **Cursor rendering** with configurable styles
- **Selection highlighting** for copy operations

### Parser (`src/parser/`)

The ANSI escape sequence parser interprets terminal control codes:

- **CSI sequences** for cursor movement, colors, and screen control
- **OSC sequences** for window titles and clipboard operations
- **SGR codes** for text styling (bold, italic, colors)
- **DEC private modes** for terminal behavior control

### Shell (`src/shell/`)

PTY (pseudo-terminal) management:

- **Process spawning** with proper terminal setup
- **Input/output handling** between the terminal and shell
- **Signal forwarding** (SIGWINCH for resize, etc.)
- **Environment configuration**

### Grid (`src/grid/`)

Terminal buffer and grid management:

- **Cell storage** with attributes (color, style)
- **Scrollback buffer** with configurable history
- **Line wrapping** and cursor positioning
- **Dirty region tracking** for efficient rendering

## Feature Modules

### Tab Management (`src/tab/`)

Multi-tab support with:

- Tab creation, switching, and closing
- Independent shell processes per tab
- Visual tab bar with close buttons
- Split pane support within tabs

### AI Panel (`src/aipanel/`, `src/ollama/`)

Ollama AI integration:

- Chat panel UI with conversation history
- Backend communication with local Ollama instance
- Response streaming and rendering
- Context-aware prompts

### Search Panel (`src/searchpanel/`, `src/websearch/`)

Web search integration:

- Search panel UI
- Backend search implementation
- Result rendering

### Keybindings (`src/keybindings/`)

Keyboard input handling:

- Configurable key mappings
- Mode-based input (normal, resize, search)
- Special key handling (function keys, modifiers)

### Settings Menu (`src/menu/`)

In-terminal settings UI:

- Font selection
- Theme configuration
- Keybinding display
- Configuration persistence

### Window Management (`src/window/`)

GLFW window handling:

- Window creation and lifecycle
- Event processing (input, resize)
- Fullscreen toggle
- Multi-monitor support

## Shell Integration and TERM

`src/config` generates init scripts for bash (`init.sh`), zsh (ZDOTDIR shim),
fish (`init.fish`) and RavenShell (`init.rsh`). They are rewritten atomically
and only when their content changes. Each one reports the working directory
(OSC 7) and marks prompts and commands with OSC 133 (A prompt start, B prompt
end, C command start, D;status command end); fish 4+ and RavenShell (which
exports `RAVENSHELL_OSC133=1`) emit the marks natively. They also define
`list-fonts` and `keybindings` (`raven-terminal --list-fonts`/`--keybindings`)
and `change-font <name>`, which asks the terminal over `OSC 7771 ;
change-font=<name> ST`.

Child shells get `TERM=xterm-raven` when that terminfo entry resolves
(`$TERMINFO`, `~/.terminfo`, `$TERMINFO_DIRS`, `/usr/share/terminfo`,
`/usr/lib/terminfo`, `/etc/terminfo`), else `xterm-256color`.

## Configuration System

Configuration is handled by `src/config/`:

- **TOML configuration file** at `~/.config/raven-terminal/config.toml`
- **Theme management** with built-in and custom themes
- **Runtime configuration** changes via settings menu
- **Sensible defaults** when no config exists

## Build System

Tasks are defined in `lazy.toml` and run with imlazy (`imlazy list` shows
them all). The Makefile is only a compatibility shim.

```bash
imlazy build          # Build the binary (backend=auto|wayland|x11)
imlazy test           # go test ./...
imlazy check          # gofmt + go vet + tests
imlazy deps           # Install system dependencies
imlazy terminfo       # Compile terminfo/xterm-raven.ti into ~/.terminfo
imlazy install-local  # Install to ~/.local/bin/
imlazy install        # Install system-wide
imlazy clean          # Remove build artifacts
```

Scripts in `scripts/`:

- `install.sh` - Full installation with desktop integration
- `uninstall.sh` - Clean removal of all installed files
- `remote-install.sh` - One-liner installation from GitHub
- `remote-uninstall.sh` - One-liner uninstallation
- `raven-terminal-wrapper.sh` - Desktop launcher wrapper

## Dependencies

### Build Dependencies

- Go 1.25+
- OpenGL 4.1 development libraries
- X11/Wayland development libraries
- pkg-config

### Runtime Dependencies

- OpenGL 4.1 compatible graphics driver
- X11 or Wayland display server

### Go Dependencies (managed by go.mod)

- `github.com/go-gl/gl` - OpenGL bindings
- `github.com/go-gl/glfw` - Window management (vendored in `third_party/glfw`)
- `github.com/creack/pty` - PTY handling
- `github.com/BurntSushi/toml` - Configuration parsing
- `golang.org/x/image` - Font rendering support
- `github.com/go-text/typesetting` - Text shaping
- `github.com/rivo/uniseg` - Grapheme clusters and widths
- `github.com/srwiley/oksvg`, `rasterx` - SVG icon rasterising
