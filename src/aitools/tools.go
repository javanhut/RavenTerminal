// Package aitools provides the built-in tools the AI chat panel can call:
// web lookups plus strictly read-only local inspection. The registry is the
// single policy boundary — nothing registered here may create, modify, or
// delete anything. Tools are defined in the same shape as MCP tool
// definitions (name / description / JSON-schema parameters) so an external
// MCP client could feed the same execution path later.
package aitools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/javanhut/RavenTerminal/src/websearch"
)

// Tool is one callable capability exposed to the model.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema for the arguments object
	Run         func(ctx context.Context, args map[string]any) (string, error)
}

// Config carries the pieces of app configuration the tools need.
type Config struct {
	UseReaderProxy bool     // route fetch_page through reader proxies
	ProxyURLs      []string // reader proxy bases (websearch config)
	WorkDir        string   // workspace root for local tools and run_command cwd
}

// Registry holds the available tools.
type Registry struct {
	cfg   Config
	tools []Tool
}

// Tool execution limits.
const (
	toolTimeout      = 20 * time.Second
	maxFetchChars    = 8000
	maxFileBytes     = 256 * 1024
	maxFileLines     = 400
	defaultFileLines = 200
	maxDirEntries    = 200
	maxSearchResults = 8
)

// NewRegistry builds the standard read-only toolset.
func NewRegistry(cfg Config) *Registry {
	if cfg.WorkDir == "" {
		cfg.WorkDir, _ = os.Getwd()
	}
	if absolute, err := filepath.Abs(cfg.WorkDir); err == nil {
		cfg.WorkDir = filepath.Clean(absolute)
	}
	r := &Registry{cfg: cfg}
	r.tools = []Tool{
		r.webSearchTool(),
		r.fetchPageTool(),
		r.readFileTool(),
		r.listDirTool(),
		r.runCommandTool(),
	}
	return r
}

// Tools returns the registered tools.
func (r *Registry) Tools() []Tool {
	return r.tools
}

// Execute runs the named tool with its own timeout under ctx.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	for _, t := range r.tools {
		if t.Name != name {
			continue
		}
		tctx, cancel := context.WithTimeout(ctx, toolTimeout)
		defer cancel()
		return t.Run(tctx, args)
	}
	return "", fmt.Errorf("unknown tool %q", name)
}

// SystemPrompt is the standing instruction sent with tool-enabled chats. It
// states the read-only contract so the model routes mutating requests into
// instructions for the user instead of attempting them.
func SystemPrompt() string {
	return strings.TrimSpace(fmt.Sprintf(`
You are the AI assistant inside Raven Terminal, running on %s/%s. You have
tools for searching the web, fetching pages, and inspecting the local machine
(reading files, listing directories, running read-only commands). Prefer this
platform's conventions (e.g. its package manager) when inspecting the system
or giving instructions.

Rules:
- You are strictly read-only. You cannot and must not create, modify, or
  delete files, push code, install software, or change any state. The
  run_command tool only accepts read-only commands.
- Local file, directory, and command inspection is limited to the active
  terminal directory. Never attempt to access secrets or send local content
  to a web tool.
- Web page fetching only permits public HTTP(S) destinations; localhost and
  private-network targets are blocked.
- Package questions: you can check whether something is installed and where,
  inspect versions, search for packages, and list outdated ones with the
  system package manager's query commands (e.g. brew list/info/search/
  outdated, brew --prefix, dpkg -l/-L, pacman -Q*/-Ss, apt list/show/search,
  npm ls/view, pip show/list, softwareupdate --list). Installing, upgrading,
  or removing packages changes state — give the user the exact commands to
  run themselves instead.
- When the user asks for something that would change state, do not attempt
  it; instead give clear step-by-step instructions (with exact commands)
  they can run themselves.
- Use tools when they help answer accurately (look things up rather than
  guessing). Keep answers concise and terminal-friendly.`, runtime.GOOS, runtime.GOARCH))
}

// argString extracts a string argument.
func argString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// argInt extracts an integer argument (JSON numbers decode as float64).
func argInt(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}

func (r *Registry) webSearchTool() Tool {
	return Tool{
		Name:        "web_search",
		Description: "Search the web (DuckDuckGo) and return result titles, URLs and snippets.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":       map[string]any{"type": "string", "description": "The search query"},
				"max_results": map[string]any{"type": "integer", "description": "Maximum results (1-8, default 5)"},
			},
			"required": []string{"query"},
		},
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			query := argString(args, "query")
			if query == "" {
				return "", fmt.Errorf("query is required")
			}
			n := min(max(argInt(args, "max_results", 5), 1), maxSearchResults)
			results, err := websearch.SearchDuckDuckGo(ctx, query, n)
			if err != nil {
				return "", err
			}
			if len(results) == 0 {
				return "No results.", nil
			}
			var b strings.Builder
			for i, res := range results {
				fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, strings.TrimSpace(res.Title), res.URL)
				if s := strings.TrimSpace(res.Snippet); s != "" {
					fmt.Fprintf(&b, "   %s\n", s)
				}
			}
			return b.String(), nil
		},
	}
}

func (r *Registry) fetchPageTool() Tool {
	return Tool{
		Name:        "fetch_page",
		Description: "Fetch a public web page and return its readable text content. Local and private-network URLs are blocked.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "The http(s) URL to fetch"},
			},
			"required": []string{"url"},
		},
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			pageURL := argString(args, "url")
			if pageURL == "" {
				return "", fmt.Errorf("url is required")
			}
			if err := websearch.ValidatePublicHTTPURL(ctx, pageURL); err != nil {
				return "", err
			}
			lines, _, _, _, err := websearch.FetchText(ctx, pageURL, maxFetchChars, r.cfg.UseReaderProxy, r.cfg.ProxyURLs)
			if err != nil {
				return "", err
			}
			text := strings.TrimSpace(strings.Join(lines, "\n"))
			if text == "" {
				return "Page had no extractable text.", nil
			}
			return text, nil
		},
	}
}

// resolvePath expands ~ and resolves relative paths against the work dir.
func (r *Registry) resolvePath(p string) string {
	if p == "" {
		return r.cfg.WorkDir
	}
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.cfg.WorkDir, p)
	}
	return filepath.Clean(p)
}

// workspacePath resolves an existing path and ensures symlinks cannot escape
// the active terminal directory exposed to the model.
func (r *Registry) workspacePath(p string) (string, error) {
	path := r.resolvePath(p)
	root := r.cfg.WorkDir
	if evaluated, err := filepath.EvalSymlinks(root); err == nil {
		root = evaluated
	}
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, evaluated)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside the active terminal directory")
	}
	return evaluated, nil
}

func (r *Registry) readFileTool() Tool {
	return Tool{
		Name:        "read_file",
		Description: "Read a text file within the active terminal directory (read-only). Returns numbered lines.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string", "description": "File path (~ allowed; relative paths resolve against the terminal's directory)"},
				"start_line": map[string]any{"type": "integer", "description": "1-based first line to return (default 1)"},
				"max_lines":  map[string]any{"type": "integer", "description": "Maximum lines to return (default 200, cap 400)"},
			},
			"required": []string{"path"},
		},
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			path := argString(args, "path")
			if path == "" {
				return "", fmt.Errorf("path is required")
			}
			path, err := r.workspacePath(path)
			if err != nil {
				return "", err
			}
			info, err := os.Stat(path)
			if err != nil {
				return "", err
			}
			if info.IsDir() {
				return "", fmt.Errorf("%s is a directory (use list_dir)", path)
			}
			if info.Size() > maxFileBytes {
				return "", fmt.Errorf("file is %d bytes, over the %d byte read limit", info.Size(), maxFileBytes)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			if isBinary(data) {
				return "", fmt.Errorf("%s looks like a binary file", path)
			}

			start := max(argInt(args, "start_line", 1), 1)
			limit := min(max(argInt(args, "max_lines", defaultFileLines), 1), maxFileLines)

			lines := strings.Split(string(data), "\n")
			if start > len(lines) {
				return "", fmt.Errorf("start_line %d is past end of file (%d lines)", start, len(lines))
			}
			end := min(start-1+limit, len(lines))
			var b strings.Builder
			for i := start - 1; i < end; i++ {
				fmt.Fprintf(&b, "%5d\t%s\n", i+1, lines[i])
			}
			if end < len(lines) {
				fmt.Fprintf(&b, "... (%d more lines)\n", len(lines)-end)
			}
			return b.String(), nil
		},
	}
}

// isBinary reports whether data looks like binary content (NUL byte in the
// first KB).
func isBinary(data []byte) bool {
	probe := data
	if len(probe) > 1024 {
		probe = probe[:1024]
	}
	return slices.Contains(probe, 0)
}

func (r *Registry) listDirTool() Tool {
	return Tool{
		Name:        "list_dir",
		Description: "List entries within the active terminal directory (read-only).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory path (default: the terminal's directory)"},
			},
		},
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			path, err := r.workspacePath(argString(args, "path"))
			if err != nil {
				return "", err
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return "", err
			}
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].IsDir() != entries[j].IsDir() {
					return entries[i].IsDir()
				}
				return entries[i].Name() < entries[j].Name()
			})
			var b strings.Builder
			fmt.Fprintf(&b, "%s:\n", path)
			for i, e := range entries {
				if i >= maxDirEntries {
					fmt.Fprintf(&b, "... (%d more entries)\n", len(entries)-i)
					break
				}
				if e.IsDir() {
					fmt.Fprintf(&b, "  %s/\n", e.Name())
					continue
				}
				size := int64(0)
				if info, err := e.Info(); err == nil {
					size = info.Size()
				}
				fmt.Fprintf(&b, "  %s (%d bytes)\n", e.Name(), size)
			}
			if len(entries) == 0 {
				b.WriteString("  (empty)\n")
			}
			return b.String(), nil
		},
	}
}
