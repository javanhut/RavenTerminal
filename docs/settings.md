# Raven Terminal Settings

## Opening Settings

Press `Ctrl+Shift+P` to open the settings menu.

## Settings Menu Navigation

| Key | Action |
|-----|--------|
| Up/Down Arrow | Navigate menu items |
| Enter | Select item / Confirm input |
| Escape | Go back / Cancel input / Close menu |
| Delete | Delete selected command or alias |

## Configuration Options

### Shell Selection

Select your preferred shell from the list of available shells detected on your system. The shell selection takes effect when you open a new tab.

Available shells are detected from common locations:
- `/bin/bash`, `/usr/bin/bash`
- `/bin/zsh`, `/usr/bin/zsh`
- `/bin/fish`, `/usr/bin/fish`
- `/bin/sh`, `/usr/bin/sh`
- `/bin/dash`, `/usr/bin/dash`
- `/bin/tcsh`, `/usr/bin/tcsh`
- `/bin/ksh`, `/usr/bin/ksh`

### Custom Commands

Add frequently used commands with a name and optional description. Custom commands can be quickly accessed and executed.

To add a command:
1. Select "Custom Commands" from the main menu
2. Select "+ Add New Command"
3. Enter a name for the command
4. Enter the command to execute
5. Optionally enter a description

To edit or delete a command:
1. Select the command from the list
2. Choose to edit name, command, description, or delete

### Aliases

Create shell aliases for shorter command invocations.

To add an alias:
1. Select "Aliases" from the main menu
2. Select "+ Add New Alias"
3. Enter the alias name
4. Enter the command it expands to

To edit or delete an alias:
1. Select the alias from the list
2. Choose to edit the command or delete the alias

## Configuration File

Settings are stored in `~/.config/raven-terminal/config.json`.

Example configuration:
```json
{
  "shell": "/usr/bin/zsh",
  "custom_commands": [
    {
      "name": "update",
      "command": "sudo apt update && sudo apt upgrade -y",
      "description": "Update system packages"
    }
  ],
  "aliases": {
    "ll": "ls -la",
    "gs": "git status"
  }
}
```

## Saving Changes

- Select "Save and Close" to save your changes and close the menu
- Select "Cancel" to discard changes and close the menu
- Press Escape to go back to the previous menu level

Changes to shell selection require opening a new tab to take effect.
