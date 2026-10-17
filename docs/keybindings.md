# Raven Terminal Keybindings

App shortcuts use a **leader** modifier instead of plain Ctrl, so the Control
key stays free for terminal control characters (Ctrl+C, Ctrl+D, ...), matching
iTerm2 / Terminal.app. No plain Ctrl, Alt or Shift chord is taken from the
running program (Ctrl+Q, Ctrl+R and Shift+Enter all reach it):

- **macOS**: the leader is **Cmd**.
- **Linux**: the leader is **Super** (same muscle memory as macOS) *or* the
  conventional **Ctrl+Shift** chords — both work.

"Leader+X" below means Cmd+X on macOS, and Super+X or Ctrl+Shift+X on Linux,
unless a row says otherwise.

## Reserved: Super+Ctrl on Linux

On Linux the terminal never acts on a **Super+Ctrl** chord — with or without
Shift — and never sends one to the shell. That layer belongs to the
compositor: Huginn (the RavenLinux desktop) keeps all of its window management
there and intercepts it before any client sees it. The full list is in
RavenGUI's `docs/integration.md`; the ones you will meet are:

| Chord | Huginn does |
|-------|-------------|
| Super+Ctrl+Space | open the application launcher |
| Super+Ctrl+A | open the pinned applications |
| Super+Ctrl+S | open quick settings |
| Super+Ctrl+E / T | open a terminal |
| Super+Ctrl+Q / X | close the focused window |
| Super+Ctrl+J / K | focus the next / previous window |
| Super+Ctrl+arrows | move the focused window between tiles |
| Super+Ctrl+1..9, Super+Ctrl+Shift+1..9 | go to / send the window to a workspace |
| Super+Ctrl+H | show the compositor's keybinding list |

Plain **Super** and **Super+Shift** are the terminal's, which is what makes the
leader layer below work. `Super+L` is also the compositor's (it locks the
session) and is not listed above because the terminal has no binding on it.

## General

| Keybinding | Action |
|------------|--------|
| Leader+Q | Exit terminal |
| Leader+C | Copy selection (Cmd+C / Super+C / Ctrl+Shift+C) |
| Leader+V | Paste clipboard |
| Leader+Enter or F11 | Toggle fullscreen mode |
| Leader+K | Show keybindings help panel |
| Leader+S | Open settings menu |
| Leader+F | Toggle web search panel |
| Leader+A | Toggle AI chat panel |
| Leader+R | Toggle pane resize mode (arrows resize, Esc exits) |

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
| Leader+Tab | Next tab |
| Leader+Shift+Tab | Previous tab |
| Cmd/Super+Shift+] | Next tab |
| Cmd/Super+Shift+[ | Previous tab |

## Split Panes

| Keybinding | Action |
|------------|--------|
| Leader+D | Split pane vertically (side by side) |
| Leader+Shift+D | Split pane horizontally (stacked) |
| Leader+W | Close current pane |
| Cmd/Super+] (also Ctrl+Shift+] on Linux) | Focus next pane |
| Cmd/Super+[ (also Ctrl+Shift+[ on Linux) | Focus previous pane |

Plain Ctrl+Tab and Ctrl+Shift+Tab without the leader go to the program.
Note: tiling window managers often grab Super chords (on RavenLinux they do not —
Huginn leaves Super and Super+Shift alone, see "Reserved" above); the
leader+] / leader+[ bindings always cycle panes.

## Scrolling

| Keybinding | Action |
|------------|--------|
| Mouse wheel up/down | Scroll 3 lines |
| Shift+Up | Scroll up 1 line |
| Shift+Down | Scroll down 1 line |
| Shift+PageUp | Scroll up 5 lines |
| Shift+PageDown | Scroll down 5 lines |

Shift+Up/Down/PageUp/PageDown scroll only on the main screen while the
program has not enabled mouse reporting or the kitty keyboard protocol. In
full-screen programs (vim, less, htop, ...) they are sent to the program as
xterm-style modified keys.

Scrolling is reset to the bottom when any input is typed.

## Mouse

| Action | Behavior |
|--------|----------|
| Left-click drag | Select text; copies it to the clipboard and the primary selection |
| Alt+left-click drag | Rectangular (block) selection |
| Leader+left-click on command output | Select and copy that command's whole output (needs shell integration) |
| Middle-click | Paste the primary selection (when the program has not enabled mouse reporting) |
| Right-click | Copy selection if one exists, otherwise paste clipboard |
| Ctrl+Right-click | Open the URL under the pointer |

When a program enables mouse reporting, clicks, drags, motion and both wheel
axes (horizontal scrolling as buttons 6/7) go to the program; hold Shift to
select text instead. With SGR-pixels (mode 1016) positions are reported in
exact pixels within the pane. A pointer shape requested with OSC 22 applies
over the program's pane only.

## Shell Integration, Overlays and Panes

"Leader+Shift+X" means Super+Shift+X, or Ctrl+Shift+Alt+X with the Ctrl+Shift
leader (Ctrl+Shift+X alone is already the plain leader chord).

| Keybinding | Action |
|------------|--------|
| Leader+Up / Leader+Down | Jump to the previous / next shell prompt (OSC 133 marks) |
| Leader+Shift+C | Copy the last command's output |
| Leader+H (also Leader+E) | Hints: label URLs, paths, `file:line:col`, git hashes and IPs on screen |
| Leader+Space (also Leader+Shift+V) | Copy mode: vi keys over the scrollback |
| Leader+P (also Leader+Shift+P) | Command palette: fuzzy search every action, setting, theme and font |
| Leader+Z | Zoom the focused pane to fill the tab (again to restore the split) |
| Leader+B | Broadcast typing and pastes to every pane in the tab (orange frame) |
| Leader+Shift+H | Open the scrollback, with colours, in `less -R` in a new tab |

With the Ctrl+Shift leader, Leader+H is Ctrl+Shift+H, Leader+P is
Ctrl+Shift+P and Leader+V (paste) is Ctrl+Shift+V.

**Hints**: type a label to copy its text; type it with Shift (an uppercase
label) to open it instead — URLs in the browser, existing files and folders
with `raven-open` — or, for hashes and addresses, to type the text into the
pane. Esc cancels, Backspace undoes a typed letter.

**Copy mode**: `h j k l` / arrows move, `w b e` move by word, `0 ^ $` jump
within the line, `gg G` go to the top / bottom of the scrollback, `H M L`
to the top / middle / bottom of the screen, Ctrl+U / Ctrl+D half a page,
Ctrl+B / Ctrl+F (PageUp / PageDown) a page. `/` and `?` search forward /
backward (Enter runs, Esc cancels), `n N` repeat. `v` starts a selection,
`V` a line selection, Ctrl+V a block selection. `y` or Enter copies the
selection (the current line when nothing is selected) and leaves; Esc
leaves visual mode, then copy mode; `q` leaves.

**Command palette**: type to filter (words can come in any order), Up / Down
or Ctrl+P / Ctrl+N to move, Enter to run, Esc to close.

## Remote control

Every instance listens on `$XDG_RUNTIME_DIR/raven-terminal/<pid>.sock` (mode
0600) and exports its path to shells as `RAVEN_TERMINAL_SOCKET`. The protocol
is one JSON object per line: `{"cmd": "split", "args": ["v"]}` answered by
`{"ok": true, "data": ...}` or `{"ok": false, "error": "..."}`.

```
raven-terminal @ ls                               # windows, tabs, panes (JSON)
raven-terminal @ new-tab [--cwd DIR] [CMD ARGS]   # prints the new pane id
raven-terminal @ new-window
raven-terminal @ split [--id N] h|v               # h stacked, v side by side
raven-terminal @ focus-pane left|right|up|down|N  # fails when there is no neighbour
raven-terminal @ send-text [--id N] 'make\r'      # \n \r \t \e \xHH escapes
raven-terminal @ get-text [--id N] [--extent screen|all|last-output|selection]
raven-terminal @ set-title [--id N] TITLE          # "-" returns to the program's title
raven-terminal @ close-pane [--id N]
raven-terminal @ get-user-vars [--id N]            # OSC 1337 SetUserVar values
raven-terminal @ screenshot [--id N] shot.png      # next frame as PNG; --id crops to that pane
raven-terminal @ action hints                      # run a keybinding action by (part of) its name
raven-terminal @ send-key ctrl+shift+space         # press and release a chord (Leader+H, escape, j)
raven-terminal @ state                             # overlay modes, focus, size, pointer, IME preedit
raven-terminal @ send-mouse 300 60 ctrl+left:press # move the pointer (window coords); click, press or release, modifiers held
```

`action` takes any unambiguous part of a name from the help list or the
palette (`hints`, `copy mode`, `palette`, `zoom`, `broadcast`, `resize`) and
presses that action's first chord. `send-key` and `action` go through the
same key and char callbacks as a real keyboard (and `send-mouse` through the
cursor and button callbacks), modifiers included, so
overlays, keybindings and kitty keyboard encoding all apply. A relative
screenshot path is resolved against the caller's directory.

These four exist so a window can be checked without a human. Run the
terminal inside a nested huginn with its own HOME, XDG dirs, short
`XDG_RUNTIME_DIR` (socket paths must fit in 108 bytes) and a private bus:

```
WAYLAND_DISPLAY=/run/user/1000/wayland-1 vblank_mode=0 \
  dbus-run-session -- huginn --backend winit    # logs "huginn is up socket=wayland-N"
WAYLAND_DISPLAY=wayland-N raven-terminal &
raven-terminal @ action copy mode && raven-terminal @ screenshot copy.png
```

`vblank_mode=0` stops the nested compositor blocking on the host's frame
callbacks when its window is not on screen. IME preedit and commit can be
driven with `ime-probe` from RavenGUI (`ime-probe preedit TEXT wait 1000
commit TEXT`).

Without `--id` a command acts on the focused pane of the focused window.
`focus-pane` with a direction exits non-zero when there is no pane that way,
which is what editor split-navigation plugins (smart-splits.nvim style) need
to hand the move over to the terminal. Outside the terminal, `@` finds the
newest running instance, or use `raven-terminal @ --to SOCKET ...`.

## Web Search Panel

While the search panel (Leader+F) is open and focused (typed characters go to
the query field):

| Keybinding | Action |
|------------|--------|
| Enter | Run search (or open preview of the selected result; in preview, follow the selected link) |
| Up / Down | Move selection, browse query history, or scroll preview |
| Left / Right | Back to results / open preview |
| Tab / Shift+Tab | (Preview) Cycle through the page's numbered `[n]` links; Enter follows |
| Ctrl+Left / Ctrl+Right | (Preview) Go back / forward through visited pages (restores scroll position) |
| Ctrl+R | Retry: re-run the last search, or re-fetch the current preview (evicts the cached copy, so it always hits the network) |
| Ctrl+Shift+R | Toggle reader proxy (reloads the preview) |
| Ctrl+O | Open the selected result / previewed page in the browser |
| Ctrl+Y | Copy the previewed page's / selected result's URL to the clipboard |
| Ctrl+A | (Preview) Send the page text — or the mouse selection, if one is active — to the AI panel for a summary |
| Ctrl+I | (Preview, mouse selection active) Insert the selected text into the shell |
| Ctrl+B | (Preview) Bookmark the current page; (Results) toggle showing bookmarks as the results list |
| Ctrl+U | Clear the query (while editing a find term, clears the find term instead) |
| / | (Preview) Start in-page find: type the term, Enter jumps to the first match |
| n / N | (Preview, after Enter confirms a find) Jump to next / previous match |
| Escape | Exit find, clear a mouse selection, back to results, then close the panel |

Note: while the search panel is focused, Ctrl+R retries the search. Results
and previewed pages are cached for the session, so revisiting them is instant; Ctrl+R evicts the cached copy and
re-fetches. In preview mode, matches of the find term are highlighted and the
status line shows `find: <term> (current/total)`; the footer shows the scroll
position as `L<line>/<total> <percent>%`. Search history persists across
sessions (last 100 queries, stored in
`~/.config/raven-terminal/search_history.json`).

Links extracted from a previewed page are marked inline as `[n]`; the footer
shows `[selected/total links]`. Tab selects the next link (highlighting its
marker and scrolling it into view), Enter opens it, and Ctrl+Left / Ctrl+Right
walk the back/forward history — going back past the first page returns to the
results list. Typing a URL as the query (explicit `http(s)://`, or a dotted
host with no spaces and a plausible TLD, e.g. `go.dev/dl`) skips the search
and previews that page directly; if a schemeless guess fails to load (e.g.
`node.js`), the query falls back to a normal search.

A mouse drag selection in the preview is copied to the clipboard on release
and stays highlighted, so Ctrl+A (send to AI) and Ctrl+I (insert into shell)
can act on it; a plain click only focuses the panel and never selects. Esc, a
new click, page load, or click outside the panel clears the selection.
Bookmarks are `{title, url}` pairs persisted to
`~/.config/raven-terminal/bookmarks.json` (deduped by URL, newest first, last
100). In the bookmark list (status shows `Bookmarks`), Enter previews the
selected bookmark; Ctrl+B again, typing, or a new search returns to the normal
results.

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

F1-F10 and F12 are passed through to the running application (F13-F24 as
Shift+F1-F12). Plain F11 toggles fullscreen; modified F11 is passed through.

## Modifier Keys

Keys are encoded like xterm, following the active keyboard layout:

- **Ctrl+letter**: the control character of the letter your layout puts on
  that key (AZERTY Ctrl+A is ^A). On non-Latin layouts the physical key's
  Latin letter is used, so Ctrl+C still interrupts. Ctrl is *not* used for
  copy/paste.
- **Ctrl+Space / Ctrl+2**: NUL. **Ctrl+[ / Ctrl+3**: ESC. **Ctrl+\ / Ctrl+4**:
  FS. **Ctrl+] / Ctrl+5**: GS. **Ctrl+6**: RS. **Ctrl+/ / Ctrl+7**: US.
  **Ctrl+8**: DEL.
- **Alt+key**: ESC followed by the character your layout produces.
- **Backspace** sends DEL, **Ctrl+Backspace** ^H, **Alt+Backspace** ESC DEL.
- **Shift+Tab**: reverse tab (CSI Z). Ctrl+Tab is sent to the program too; tab cycling uses Leader+Tab.
- **Modified arrows, Home/End, Insert/Delete, PageUp/PageDown, F1-F12** send
  `CSI 1;<mod> X` / `CSI <n>;<mod> ~` (mod = 1 + Shift 1, Alt 2, Ctrl 4,
  Super 8). Home/End follow cursor-key application mode (SS3 H/F).
- **Keypad**: in application keypad mode (DECKPAM) the keypad sends SS3
  codes (`SS3 p`..`SS3 y` for 0-9, `SS3 M` for Enter, ...). With Num Lock off
  the keypad acts as the navigation keys printed on it.

## Program-requested keyboard modes

- **modifyOtherKeys** (`CSI > 4 ; 1 m` / `CSI > 4 ; 2 m`): chords without a
  distinct legacy code (Ctrl+;, Ctrl+1, Shift+Enter, Ctrl+Enter, and at level
  2 every Ctrl/Alt chord) are sent as `CSI 27 ; <mod> ; <code> ~`.
- **Kitty keyboard protocol** (`CSI > <flags> u`): all five enhancements are
  supported: disambiguate (1), report press/repeat/release (2), alternate
  keys (4, shifted and base-layout key), report all keys as escape codes (8,
  including modifier keys and Caps/Num Lock state) and associated text (16).
  Leader shortcuts keep working while a program has the protocol enabled.
