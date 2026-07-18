# AI Tools (read-only)

When **AI Tools (read-only)** is enabled (Settings → AI Features, on by
default), the AI chat panel lets the model call a small set of built-in tools
while answering. Tool activity appears in the conversation as dim `⚙` lines.

The toolset is strictly read-only by construction — the registry in
`src/aitools` is the policy boundary, and nothing registered there can create,
modify, or delete anything. When you ask for something that would change state
(create files, push code, install software), the model is instructed to give
you the exact commands to run yourself instead.

Local file, directory, and command inspection is restricted to the active
terminal directory, including symlink resolution. Page fetching is restricted
to public HTTP(S) targets: localhost, link-local, and private-network addresses
are rejected, including redirects. Tool results are sent to the configured
Ollama model, so do not enable tools in a directory containing secrets you do
not want that model to process.

## Tools

| Tool | What it does |
| --- | --- |
| `web_search` | DuckDuckGo search; returns titles, URLs, snippets |
| `fetch_page` | Fetches a page's readable text (honors the Reader Proxy setting) |
| `read_file` | Reads a workspace text file (size/line capped, binary files rejected) |
| `list_dir` | Lists workspace directory entries |
| `run_command` | Runs ONE read-only command, no shell |

## run_command policy

Commands execute directly (no shell), so pipes, redirection, `&&`,
substitution, and globs are rejected outright. Only allowlisted binaries run:

`ls cat head tail grep rg find which file stat wc du df ps man pwd whoami
uname date uptime git ivaldi go` plus package-manager queries:
`brew apt apt-cache dpkg pacman dnf apk zypper port npm pip pip3
softwareupdate`

- `git`/`ivaldi`/`go` are further limited to read-only subcommands
  (`status`, `log`, `diff`, `show`, …; `go version`/`go env`).
- Package managers are limited to query verbs — `brew list/info/search/
  outdated/--prefix`, `dpkg -l/-L/-s/-S`, `pacman -Q*/-Ss/-Si`,
  `apt list/show/search/policy`, `npm ls/view/outdated`, `pip show/list`,
  `softwareupdate --list` — so the AI can tell you what's installed, where,
  and what's outdated. Install/upgrade/remove is always answered with
  instructions for you to run, never executed.
- Write-capable flags are blocked per binary (`find -delete/-exec`,
  `git --output`, `go env -w`, …).
- Pagers are forced off, stdin is closed, output is capped, and every tool
  call has a 20 second timeout.
- Local paths and command working directories are confined to the active
  pane's directory; absolute paths, traversal, and escaping symlinks are rejected.

Models that don't support tool calling fall back to a plain chat
automatically.

Tool definitions use the same shape as MCP tools (name / description /
JSON-schema parameters), so an external MCP client can reuse the same
execution path later.
