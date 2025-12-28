# Split Panes

Raven Terminal supports splitting the terminal window into multiple panes within a single tab. This allows you to work with multiple terminal sessions side by side or stacked vertically.

## Overview

Each tab can contain up to 4 panes. Panes share the available terminal space and each runs an independent shell session.

## Keybindings

| Keybinding | Action |
|------------|--------|
| Ctrl+Shift+V | Split vertically (side by side) |
| Ctrl+Shift+H | Split horizontally (stacked) |
| Ctrl+Shift+W | Close current pane |
| Shift+Tab | Cycle to next pane |
| Ctrl+Shift+] | Focus next pane |
| Ctrl+Shift+[ | Focus previous pane |

## Split Types

### Vertical Split (Ctrl+Shift+V)

Creates a new pane to the right of the current pane. All panes are arranged side by side with equal width.

```
+---------------+---------------+
|               |               |
|    Pane 1     |    Pane 2     |
|               |               |
+---------------+---------------+
```

### Horizontal Split (Ctrl+Shift+H)

Creates a new pane below the current pane. All panes are stacked vertically with equal height.

```
+-------------------------------+
|            Pane 1             |
+-------------------------------+
|            Pane 2             |
+-------------------------------+
```

## Active Pane Indicator

The currently active pane is indicated by a highlighted border. Only the active pane receives keyboard input.

## Limitations

- Maximum of 4 panes per tab
- All panes in a tab share the same split direction (either all vertical or all horizontal)
- Closing the last pane in a tab is not allowed (use Ctrl+Shift+X to close the entire tab instead)

## Usage Tips

- Use vertical splits when comparing files or monitoring multiple processes
- Use horizontal splits when you need wider terminal output
- Combine with tabs to organize different workspaces
