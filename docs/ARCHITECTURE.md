# Raven Terminal Architecture

This document describes the internal architecture and project structure of Raven Terminal.

## Project Structure

```
RavenTerminal/
├── src/                    # Main source code
│   ├── main.go             # Application entry point
│   ├── aipanel/            # AI chat panel (Ollama integration UI)
│   ├── assets/             # Embedded assets
│   │   ├── fonts/          # Bundled Nerd Fonts (FiraCode, Hack, JetBrains, Ubuntu)
│   │   └── *.svg           # Application icons
│   ├── commands/           # Built-in terminal commands
│   ├── config/             # Configuration and theme management
│   ├── grid/               # Terminal grid/buffer management
│   ├── keybindings/        # Keyboard input handling
│   ├── menu/               # Settings menu UI
│   ├── ollama/             # Ollama AI backend integration
│   ├── parser/             # ANSI escape sequence parser
│   ├── render/             # OpenGL 4.1 renderer
│   ├── searchpanel/        # Web search panel UI
│   ├── shell/              # PTY/shell handling
│   ├── tab/                # Tab management
│   ├── websearch/          # Web search backend
│   └── window/             # GLFW window management
├── docs/                   # Documentation
├── scripts/                # Build and installation scripts
├── Makefile                # Build automation
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

## Configuration System

Configuration is handled by `src/config/`:

- **TOML configuration file** at `~/.config/raven-terminal/config.toml`
- **Theme management** with built-in and custom themes
- **Runtime configuration** changes via settings menu
- **Sensible defaults** when no config exists

## Build System

The project uses a Makefile for build automation:

```bash
make              # Build the binary
make deps         # Install system dependencies (distro-detected)
make install-local # Install to ~/.local/bin/
make install      # Install system-wide
make clean        # Remove build artifacts
```

Scripts in `scripts/`:

- `install.sh` - Full installation with desktop integration
- `uninstall.sh` - Clean removal of all installed files
- `remote-install.sh` - One-liner installation from GitHub
- `remote-uninstall.sh` - One-liner uninstallation
- `raven-terminal-wrapper.sh` - Desktop launcher wrapper

## Dependencies

### Build Dependencies

- Go 1.21+
- OpenGL 4.1 development libraries
- X11/Wayland development libraries
- pkg-config

### Runtime Dependencies

- OpenGL 4.1 compatible graphics driver
- X11 or Wayland display server

### Go Dependencies (managed by go.mod)

- `github.com/go-gl/gl` - OpenGL bindings
- `github.com/go-gl/glfw` - Window management
- `github.com/creack/pty` - PTY handling
- `github.com/pelletier/go-toml` - Configuration parsing
- `golang.org/x/image` - Font rendering support
