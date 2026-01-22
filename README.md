# Raven Terminal

A GPU-accelerated terminal emulator written in Go using OpenGL for rendering. Raven Terminal features smooth font rendering, tab support, and Nerd Font icon compatibility.

## Features

- **GPU-Accelerated Rendering**: Uses OpenGL 4.1 for smooth, hardware-accelerated text rendering
- **Tab Support**: Multiple terminal sessions in a single window with a left-side tab bar
- **Nerd Font Support**: Built-in support for Nerd Font icons (Powerline, Devicons, Font Awesome, etc.)
- **Multiple Fonts**: Bundled with popular monospace fonts (FiraCode, Hack, JetBrains Mono, Ubuntu Mono)
- **Scrollback Buffer**: Scroll through terminal history with mouse wheel or keyboard
- **Fullscreen Mode**: Toggle fullscreen with Shift+Enter
- **256 Color Support**: Full indexed and RGB color support

## Requirements

- Go 1.21 or later
- OpenGL 4.1 compatible graphics driver
- Linux (X11/Wayland)

## Installation

### One-liner Install (from GitHub)

```bash
curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-install.sh | bash
```

For system-wide install:

```bash
curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-install.sh | bash -s -- --global
```

### From Source

```bash
git clone https://github.com/javanhut/RavenTerminal.git
cd RavenTerminal
make deps           # Install system dependencies
make                # Build
make install-local  # Install to ~/.local/bin/
```

See [Installation Guide](docs/installation.md) for detailed instructions.

## Uninstallation

### One-liner Uninstall

```bash
curl -sSL https://raw.githubusercontent.com/javanhut/RavenTerminal/main/scripts/remote-uninstall.sh | bash
```

### Local Uninstall

```bash
./scripts/uninstall.sh --user      # Remove user installation
./scripts/uninstall.sh --global    # Remove system-wide installation
./scripts/uninstall.sh --all --config  # Remove everything including config
```

## Usage

After installation, launch from:
- **Application Menu**: Search for "Raven Terminal"
- **Command Line**: `raven-terminal`

## Keybindings

### General

| Keybinding       | Action                  |
|------------------|-------------------------|
| Ctrl+Q           | Exit terminal           |
| Ctrl+Shift+C     | Copy selection          |
| Ctrl+Shift+P     | Paste clipboard         |
| Shift+Enter      | Toggle fullscreen mode  |
| Ctrl+Shift+K     | Show/hide help panel    |
| Ctrl+Shift+S     | Open settings menu      |

### Zoom

| Keybinding       | Action              |
|------------------|---------------------|
| Ctrl+Shift++     | Zoom in             |
| Ctrl+Shift+-     | Zoom out            |
| Ctrl+Shift+0     | Reset zoom          |

### Tab Management

| Keybinding       | Action              |
|------------------|---------------------|
| Ctrl+Shift+T     | New tab             |
| Ctrl+Shift+X     | Close current tab   |
| Ctrl+Tab         | Next tab            |
| Ctrl+Shift+Tab   | Previous tab        |

### Split Panes

| Keybinding       | Action                    |
|------------------|---------------------------|
| Ctrl+Shift+V     | Split vertical            |
| Ctrl+Shift+H     | Split horizontal          |
| Ctrl+Shift+W     | Close current pane        |
| Ctrl+Shift+]     | Next pane                 |
| Ctrl+Shift+[     | Previous pane             |
| Shift+Tab        | Cycle panes               |
| Ctrl+R           | Toggle resize mode        |

### Scrolling

| Keybinding       | Action              |
|------------------|---------------------|
| Mouse wheel up   | Scroll up 3 lines   |
| Mouse wheel down | Scroll down 3 lines |
| Shift+Up         | Scroll up 1 line    |
| Shift+Down       | Scroll down 1 line  |
| Shift+PageUp     | Scroll up 5 lines   |
| Shift+PageDown   | Scroll down 5 lines |

### Mouse

| Action           | Behavior                          |
|------------------|-----------------------------------|
| Left-click drag  | Select text and copy to clipboard |
| Right-click      | Copy selection or paste clipboard |

## Built-in Commands

Raven Terminal includes several built-in commands:

| Command              | Description                |
|----------------------|----------------------------|
| `keybindings`        | Show keybinding help       |
| `list-fonts`         | List available fonts       |
| `change-font <name>` | Change to specified font   |

Command aliases:
- `raven-keybindings` - Alias for `keybindings`
- `fonts` - Alias for `list-fonts`

### Available Fonts

- `firacode` - FiraCode Nerd Font
- `hack` - Hack Nerd Font
- `jetbrainsmono` - JetBrains Mono Nerd Font
- `ubuntumono` - Ubuntu Mono Nerd Font

## Documentation

Additional documentation is available in the [docs/](docs/) directory:

- [Architecture](docs/ARCHITECTURE.md) - Project structure and internal architecture
- [Installation](docs/installation.md) - Installation and uninstallation guide
- [Keybindings](docs/keybindings.md) - Complete keybinding reference
- [Split Panes](docs/splits.md) - Split pane usage
- [Settings](docs/settings.md) - Settings menu and configuration
- [Icon](docs/icon.md) - Application icon customization

## License

MIT License - see [LICENSE](LICENSE) for details.
