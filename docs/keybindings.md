# Raven Terminal Keybindings

App shortcuts use a **leader** modifier instead of plain Ctrl, so the Control
key stays free for terminal control characters (Ctrl+C, Ctrl+D, ...), matching
iTerm2 / Terminal.app:

- **macOS**: the leader is **Cmd**.
- **Linux**: the leader is **Super** (same muscle memory as macOS) *or* the
  conventional **Ctrl+Shift** chords — both work.

"Leader+X" below means Cmd+X on macOS, and Super+X or Ctrl+Shift+X on Linux,
unless a row says otherwise.

## General

| Keybinding | Action |
|------------|--------|
| Ctrl+Q or Cmd/Super+Q | Exit terminal |
| Leader+C | Copy selection (Cmd+C / Super+C / Ctrl+Shift+C) |
| Cmd/Super+V (also Ctrl+Shift+P on Linux) | Paste clipboard |
| Shift+Enter | Toggle fullscreen mode |
| Leader+K | Show keybindings help panel |
| Leader+S | Open settings menu |
| Leader+F | Toggle web search panel |
| Leader+A | Toggle AI chat panel |
| Ctrl+R or Cmd/Super+R | Toggle pane resize mode |

## Zoom

| Keybinding | Action |
|------------|--------|
| Leader+= | Zoom in (increase font size) |
| Leader+- | Zoom out (decrease font size) |
| Leader+0 | Reset zoom to default |

## Tab Management

| Keybinding | Action |
|------------|--------|
| Leader+T | New tab |
| Leader+X | Close current tab |
| Leader+1..9 | Jump to tab 1-9 |
| Ctrl+Tab | Next tab |
| Ctrl+Shift+Tab | Previous tab |
| Cmd/Super+Shift+] | Next tab |
| Cmd/Super+Shift+[ | Previous tab |

## Split Panes

| Keybinding | Action |
|------------|--------|
| Cmd/Super+D (also Ctrl+Shift+V on Linux) | Split pane vertically (side by side) |
| Cmd/Super+Shift+D (also Ctrl+Shift+H on Linux) | Split pane horizontally (stacked) |
| Leader+W | Close current pane |
| Cmd/Super+] (also Ctrl+Shift+] on Linux) | Focus next pane |
| Cmd/Super+[ (also Ctrl+Shift+[ on Linux) | Focus previous pane |
| Cmd/Super+Shift+Tab | Cycle to next pane |

Note: macOS reserves Cmd+Shift+Tab for the app switcher unless remapped, and
tiling window managers often grab Super chords; the leader+] / leader+[
bindings always cycle panes.

## Scrolling

| Keybinding | Action |
|------------|--------|
| Mouse wheel up/down | Scroll 3 lines |
| Shift+Up | Scroll up 1 line |
| Shift+Down | Scroll down 1 line |
| Shift+PageUp | Scroll up 5 lines |
| Shift+PageDown | Scroll down 5 lines |

Scrolling is reset to the bottom when any input is typed.

## Mouse

| Action | Behavior |
|--------|----------|
| Left-click drag | Select text and copy to clipboard |
| Right-click | Copy selection if one exists, otherwise paste clipboard |
| Ctrl+Right-click | Open the URL under the pointer |

## Help Panel

While the help panel (Leader+K) is open:

| Keybinding | Action |
|------------|--------|
| Up / Down | Scroll one line |
| PageUp / PageDown | Scroll 5 lines |
| Home | Jump to top |
| Escape | Close the panel |

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

- **Ctrl+letter**: Sends the control character to the shell (Ctrl+C interrupts,
  Ctrl+D for EOF, Ctrl+L clears, etc.) — Ctrl is *not* used for copy/paste.
- **Ctrl+Space**: Sends NUL.
- **Alt+letter**: Sends ESC prefix followed by the letter.
- **Shift+Tab**: Always sends the reverse-tab sequence (CSI Z) to the running
  app (nvim, TUIs). Pane cycling uses Cmd/Super+Shift+Tab instead.
