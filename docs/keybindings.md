# Raven Terminal Keybindings

App shortcuts use a **leader** modifier instead of plain Ctrl, so the Control
key stays free for terminal control characters (Ctrl+C, Ctrl+D, ...), matching
iTerm2 / Terminal.app:

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
tiling window managers often grab Super chords (on RavenLinux they do not —
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

Scrolling is reset to the bottom when any input is typed.

## Mouse

| Action | Behavior |
|--------|----------|
| Left-click drag | Select text and copy to clipboard |
| Right-click | Copy selection if one exists, otherwise paste clipboard |
| Ctrl+Right-click | Open the URL under the pointer |

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

Note: while the search panel is focused, Ctrl+R retries the search instead of
toggling pane resize mode. Results and previewed pages are cached for the
session, so revisiting them is instant; Ctrl+R evicts the cached copy and
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

F1-F12 are passed through to the running application.

## Modifier Keys

- **Ctrl+letter**: Sends the control character to the shell (Ctrl+C interrupts,
  Ctrl+D for EOF, Ctrl+L clears, etc.) — Ctrl is *not* used for copy/paste.
- **Ctrl+Space**: Sends NUL.
- **Alt+letter**: Sends ESC prefix followed by the letter.
- **Shift+Tab**: Always sends the reverse-tab sequence (CSI Z) to the running
  app (nvim, TUIs). Pane cycling uses Cmd/Super+Shift+Tab instead.
