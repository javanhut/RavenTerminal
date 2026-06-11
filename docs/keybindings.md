# Raven Terminal Keybindings

## General

| Keybinding | Action |
|------------|--------|
| Ctrl+Q | Exit terminal |
| Ctrl+C | Copy visible screen |
| Ctrl+P | Paste clipboard |
| Shift+Enter | Toggle fullscreen mode |
| Ctrl+Shift+K | Show/hide keybindings help panel |
| Ctrl+Shift+P | Open settings menu |
| Ctrl+Shift+F | Toggle web search panel |
| Ctrl+Shift+A | Toggle AI chat panel |
| Ctrl+Shift+[ | Previous pane or overlay panel in cycle (when open) |
| Ctrl+Shift+] | Next pane or overlay panel in cycle (when open) |

## Zoom

| Keybinding | Action |
|------------|--------|
| Ctrl+Shift++ | Zoom in (increase font size) |
| Ctrl+Shift+- | Zoom out (decrease font size) |
| Ctrl+Shift+0 | Reset zoom to default |

## Tab Management

| Keybinding | Action |
|------------|--------|
| Ctrl+Shift+T | New tab |
| Ctrl+Shift+X | Close current tab |
| Ctrl+Tab | Next tab |
| Ctrl+Shift+Tab | Previous tab |

## Split Panes

| Keybinding | Action |
|------------|--------|
| Super+D (Cmd+D on macOS) or Ctrl+Shift+V | Split pane vertically (side by side) |
| Super+Shift+D (Cmd+Shift+D on macOS) or Ctrl+Shift+H | Split pane horizontally (stacked) |
| Ctrl+Shift+W | Close current pane |
| Super+Shift+Tab (Cmd+Shift+Tab on macOS) | Cycle to next pane |
| Super+] (Cmd+] on macOS) or Ctrl+Shift+] | Focus next pane |
| Super+[ (Cmd+[ on macOS) or Ctrl+Shift+[ | Focus previous pane |

## Scrolling

| Keybinding | Action |
|------------|--------|
| Mouse wheel up | Scroll up 3 lines |
| Mouse wheel down | Scroll down 3 lines |
| Shift+Up | Scroll up 1 line |
| Shift+Down | Scroll down 1 line |
| Shift+PageUp | Scroll up 5 lines |
| Shift+PageDown | Scroll down 5 lines |

Scrolling is reset to the bottom when any input is typed.

## Mouse

| Action | Behavior |
|--------|----------|
| Left-click drag | Select text and copy to clipboard |
| Right-click | Copy selection or paste clipboard |

## Text Navigation

| Keybinding | Action |
|------------|--------|
| Home | Move to beginning of line |
| End | Move to end of line |
| PageUp | Page up (in applications) |
| PageDown | Page down (in applications) |
| Insert | Toggle insert mode |
| Delete | Delete character |

## Function Keys

F1-F12 are passed through to the running application.

## Modifier Keys

- **Ctrl+letter**: Sends control character (Ctrl+D for EOF, Ctrl+L for clear, etc.)
- **Alt+letter**: Sends ESC prefix followed by the letter
- **Shift+Tab**: Always sends the reverse-tab sequence (CSI Z) to the running app (nvim, TUIs). Pane cycling uses Super+Shift+Tab (Cmd+Shift+Tab on macOS). Note macOS reserves Cmd+Shift+Tab for the app switcher and tiling WMs often grab Super chords; the leader+] / leader+[ bindings always cycle panes.
