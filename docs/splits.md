# Split Panes

Raven Terminal supports splitting the terminal window into multiple panes within a single tab. This allows you to work with multiple terminal sessions side by side or stacked vertically, with full support for nested splits.

## Overview

Each tab can contain up to 16 panes arranged in a tree structure. Panes can be split vertically or horizontally, and nested splits allow for complex layouts like vertical splits inside horizontal splits and vice versa.

## Keybindings

| Keybinding | Action |
|------------|--------|
| Ctrl+Shift+V | Split current pane vertically (side by side) |
| Ctrl+Shift+H | Split current pane horizontally (stacked) |
| Ctrl+Shift+W | Close current pane |
| Shift+Tab | Cycle to next pane |
| Ctrl+Shift+] | Focus next pane |
| Ctrl+Shift+[ | Focus previous pane |

## Split Types

### Vertical Split (Ctrl+Shift+V)

Creates a new pane to the right of the current pane. The current pane and the new pane share the space equally.

```
+---------------+---------------+
|               |               |
|    Pane 1     |    Pane 2     |
|               |               |
+---------------+---------------+
```

### Horizontal Split (Ctrl+Shift+H)

Creates a new pane below the current pane. The current pane and the new pane share the space equally.

```
+-------------------------------+
|            Pane 1             |
+-------------------------------+
|            Pane 2             |
+-------------------------------+
```

## Nested Splits

Raven Terminal supports nested splits, allowing you to create complex layouts. Each split operation applies to the currently active pane, subdividing its space.

### Example: Vertical Split Inside Horizontal Split

1. Start with a single pane
2. Split horizontally (Ctrl+Shift+H) - creates top and bottom panes
3. With the bottom pane active, split vertically (Ctrl+Shift+V)

Result:
```
+-------------------------------+
|            Pane 1             |
+---------------+---------------+
|    Pane 2     |    Pane 3     |
+---------------+---------------+
```

### Example: Complex Grid Layout

1. Split vertically (Ctrl+Shift+V) - creates left and right panes
2. Split the left pane horizontally (Ctrl+Shift+H)
3. Switch to the right pane and split horizontally (Ctrl+Shift+H)

Result:
```
+---------------+---------------+
|    Pane 1     |    Pane 3     |
+---------------+---------------+
|    Pane 2     |    Pane 4     |
+---------------+---------------+
```

## Active Pane Indicator

The currently active pane is indicated by a highlighted border (blue by default). Only the active pane receives keyboard input.

## Closing Panes

When you close a pane with Ctrl+Shift+W:
- The pane is removed from the layout
- The sibling pane expands to fill the available space
- Focus moves to the remaining sibling pane
- Closing the last pane in a tab is not allowed (use Ctrl+Shift+X to close the entire tab)

## Limitations

- Maximum of 16 panes per tab
- Pane sizes are divided equally among siblings
- Closing the last pane in a tab is not allowed

## Usage Tips

- Use vertical splits when comparing files or monitoring multiple processes side by side
- Use horizontal splits when you need wider terminal output (logs, tables)
- Combine nested splits for dashboard-style layouts
- Use Shift+Tab to quickly cycle through panes
- Combine with tabs to organize different workspaces
