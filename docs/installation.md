# Raven Terminal Installation Guide

## Prerequisites

### Required
- Go 1.21 or later
- OpenGL 4.1 compatible graphics driver

### Build Dependencies

**Arch Linux:**
```bash
sudo pacman -S go base-devel libx11 libxcursor libxrandr libxinerama libxi mesa
```

**Ubuntu/Debian:**
```bash
sudo apt install golang build-essential libgl1-mesa-dev xorg-dev
```

**Fedora:**
```bash
sudo dnf install golang mesa-libGL-devel libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel
```

## Installation

### Quick Install (User)

```bash
./scripts/install.sh
```

This installs to `~/.local/bin/` and creates a desktop entry for your user.

### System-wide Install

```bash
./scripts/install.sh --global
```

This installs to `/usr/local/bin/` and creates a system-wide desktop entry.
Requires sudo privileges.

### Build Only

```bash
./scripts/install.sh --build-only
```

This only builds the binary without installing it.

## Installation Options

| Option | Description |
|--------|-------------|
| `-u, --user` | Install for current user only (default) |
| `-g, --global` | Install system-wide (requires sudo) |
| `-b, --build-only` | Only build the binary, don't install |
| `-s, --skip-build` | Skip building, use existing binary |
| `-v, --verbose` | Show verbose output |
| `-h, --help` | Show help message |

## Installation Locations

### User Installation (`--user`)
| Component | Location |
|-----------|----------|
| Binary | `~/.local/bin/raven-terminal` |
| Desktop Entry | `~/.local/share/applications/raven-terminal.desktop` |
| Icon | `~/.local/share/icons/hicolor/scalable/apps/raven-terminal.svg` |
| Config | `~/.config/raven-terminal/config.json` |

### Global Installation (`--global`)
| Component | Location |
|-----------|----------|
| Binary | `/usr/local/bin/raven-terminal` |
| Desktop Entry | `/usr/share/applications/raven-terminal.desktop` |
| Icon | `/usr/share/icons/hicolor/scalable/apps/raven-terminal.svg` |
| Config | `~/.config/raven-terminal/config.json` (per-user) |

## Uninstallation

### Remove User Installation

```bash
./scripts/uninstall.sh --user
```

### Remove Global Installation

```bash
./scripts/uninstall.sh --global
```

### Remove Everything (Including Config)

```bash
./scripts/uninstall.sh --all --config
```

## Uninstall Options

| Option | Description |
|--------|-------------|
| `-u, --user` | Remove user installation |
| `-g, --global` | Remove global installation (requires sudo) |
| `-a, --all` | Remove from both user and global locations |
| `-c, --config` | Also remove configuration files |
| `-f, --force` | Don't ask for confirmation |
| `-v, --verbose` | Show verbose output |
| `-h, --help` | Show help message |

## PATH Configuration

For user installations, ensure `~/.local/bin` is in your PATH. Add this to your `~/.bashrc` or `~/.zshrc`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Launching Raven Terminal

After installation:

1. **From Application Menu**: Search for "Raven Terminal"
2. **From Command Line**: Run `raven-terminal`
3. **Set as Default Terminal**: Configure your desktop environment to use `/usr/local/bin/raven-terminal` or `~/.local/bin/raven-terminal`

## Troubleshooting

### Application doesn't appear in menu
```bash
# Update desktop database
update-desktop-database ~/.local/share/applications  # user install
sudo update-desktop-database /usr/share/applications  # global install
```

### Icon not showing
```bash
# Update icon cache
gtk-update-icon-cache -f -t ~/.local/share/icons/hicolor  # user install
sudo gtk-update-icon-cache -f -t /usr/share/icons/hicolor  # global install
```

### OpenGL errors
Ensure you have proper graphics drivers installed. For NVIDIA:
```bash
# Arch
sudo pacman -S nvidia nvidia-utils

# Ubuntu
sudo apt install nvidia-driver-xxx  # replace xxx with version
```

### Missing shared libraries
```bash
# Check what's missing
ldd $(which raven-terminal) | grep "not found"
```

## Manual Installation

If you prefer manual installation:

```bash
# Build
go build -o raven-terminal .

# Install binary
sudo cp raven-terminal /usr/local/bin/
# or
cp raven-terminal ~/.local/bin/

# Install icon
sudo cp assets/raven_terminal_icon.svg /usr/share/icons/hicolor/scalable/apps/raven-terminal.svg

# Create desktop file (see scripts/install.sh for template)
```
