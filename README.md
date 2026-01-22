# Raven Terminal

A GPU-accelerated terminal emulator written in Go using OpenGL for rendering. Raven Terminal features smooth font rendering, tab support, and Nerd Font icon compatibility.

## Features

- **GPU-Accelerated Rendering**: Uses OpenGL 4.1 for smooth, hardware-accelerated text rendering
- **Tab Support**: Multiple terminal sessions in a single window with a left-side tab bar
- **Split Panes**: Divide your terminal into multiple panes
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

### Quick Install (from GitHub)

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

See [Installation Guide](docs/installation.md) for detailed instructions including uninstallation.

## Usage

After installation, launch from:
- **Application Menu**: Search for "Raven Terminal"
- **Command Line**: `raven-terminal`

Press `Ctrl+Shift+K` to show the keybindings help panel, or `Ctrl+Shift+S` to open settings.

## Documentation

- [Installation](docs/installation.md) - Installation, uninstallation, and troubleshooting
- [Keybindings](docs/keybindings.md) - Complete keybinding reference
- [Settings](docs/settings.md) - Configuration options and built-in commands
- [Split Panes](docs/splits.md) - Split pane usage and navigation
- [Architecture](docs/ARCHITECTURE.md) - Project structure and internal architecture
- [Icon](docs/icon.md) - Application icon customization

## License

MIT License - see [LICENSE](LICENSE) for details.
