package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/javanhut/RavenTerminal/src/aitools"
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/ollama"
	"github.com/javanhut/RavenTerminal/src/searchpanel"
	"github.com/javanhut/RavenTerminal/src/websearch"
)

// 20 (not 8): DDG pagination needs a per-query vqd token and ignores GET
// offsets, so one bigger first page replaces a load-more key.
const maxSearchResults = 20
const maxChatMessages = 6

func (a *App) startPreview(result searchpanel.Result) {
	a.searchPanel.NavPush(result.URL, result.Title)
	a.searchPanel.Mode = searchpanel.ModePreview
	a.searchPanel.PreviewTitle = result.Title
	a.searchPanel.PreviewURL = result.URL
	a.searchPanel.PreviewLines = nil
	a.searchPanel.PreviewScroll = 0
	a.searchPanel.PreviewID++
	useReaderProxy := a.searchPanel.ProxyEnabled
	cacheKey := previewCacheKey(useReaderProxy, result.URL)
	if entry, ok := a.previewCache.get(cacheKey); ok {
		a.searchPanel.SetPreview(result.URL, result.Title, entry.lines, entry.links, nil)
		if entry.source == "proxy" {
			a.searchPanel.Status = "Source: reader proxy (cached)"
		} else {
			a.searchPanel.Status = "Source: direct HTML (cached)"
		}
		return
	}
	a.searchPanel.Status = "Loading preview..."
	a.searchPanel.StartLoading()
	previewID := a.searchPanel.PreviewID
	var proxyURLs []string
	if a.settingsMenu.Config != nil {
		proxyURLs = a.settingsMenu.Config.WebSearch.ReaderProxyURLs
	}
	go func(id int, url, title string, useProxy bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		lines, links, source, proxyErr, err := websearch.FetchText(ctx, url, 12000, useProxy, proxyURLs)
		a.previewResponses <- previewResponse{id: id, url: url, title: title, lines: lines, links: links, source: source, proxyErr: proxyErr, cacheKey: cacheKey, err: err}
	}(previewID, result.URL, result.Title, useReaderProxy)
}

// runSearch always performs a web search (no direct-URL sniffing); it is
// also the fallback when a heuristic direct-URL preview fails to load.
func (a *App) runSearch(query string) {
	a.searchPanel.Mode = searchpanel.ModeResults
	a.searchPanel.Results = nil
	a.searchPanel.Selected = 0
	a.searchPanel.ResultsScroll = 0
	a.searchPanel.ResetHistory()
	a.searchPanel.ResetNav()
	a.searchPanel.SearchID++
	if results, ok := a.searchCache.get(strings.TrimSpace(query)); ok {
		a.searchPanel.SetResults(query, results, nil)
		a.searchPanel.AddToHistory(query)
		config.SaveSearchHistory(a.searchPanel.History)
		a.searchPanel.Status = fmt.Sprintf("%d results (cached)", len(results))
		return
	}
	a.searchPanel.Status = "Searching..."
	a.searchPanel.StartLoading()
	searchID := a.searchPanel.SearchID
	go func(id int, q string) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		results, err := websearch.SearchDuckDuckGo(ctx, q, maxSearchResults)
		a.searchResponses <- searchResponse{id: id, query: q, results: results, err: err}
	}(searchID, query)
}

func (a *App) startSearch(query string) {
	// A query that looks like a URL skips the search and previews the
	// page directly. Schemeless matches are only a heuristic ("node.js"
	// is a search term, not a host), so remember the query and fall back
	// to a real search if that preview fetch fails.
	if u, ok := websearch.DirectURL(query); ok {
		a.startPreview(searchpanel.Result{Title: u, URL: u})
		q := strings.TrimSpace(query)
		if !strings.HasPrefix(q, "http://") && !strings.HasPrefix(q, "https://") {
			a.directFallbackQuery = query
			a.directFallbackID = a.searchPanel.PreviewID
		}
		return
	}
	a.runSearch(query)
}

func (a *App) startAIChat(prompt string) {
	if a.settingsMenu.Config == nil {
		a.aiPanel.Status = "Missing config"
		return
	}
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return
	}

	cfg := a.settingsMenu.Config.Ollama
	if a.aiPanel.LoadedURL != cfg.URL || a.aiPanel.LoadedModel != cfg.Model {
		a.aiPanel.ModelLoaded = false
	}

	a.aiPanel.AddMessage("user", trimmed)
	a.aiPanel.TrimMessages(maxChatMessages)
	a.aiPanel.ClearInput()
	if !a.aiPanel.ModelLoaded {
		a.aiPanel.Status = "Loading model..."
	} else {
		a.aiPanel.Status = "Thinking..."
	}
	a.aiPanel.StartLoading()
	a.aiPanel.RequestID++
	requestID := a.aiPanel.RequestID
	needLoad := !a.aiPanel.ModelLoaded

	// History for the API: only the actual conversation. Tool-activity
	// notes and error lines are panel UI, not chat turns.
	messages := make([]ollama.Message, 0, len(a.aiPanel.Messages)+1)
	toolsEnabled := a.settingsMenu.Config.Ollama.Tools
	if toolsEnabled {
		messages = append(messages, ollama.Message{Role: "system", Content: aitools.SystemPrompt()})
	}
	for _, msg := range a.aiPanel.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		messages = append(messages, ollama.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Tool execution context (working dir follows the active pane).
	workDir := ""
	if at := a.tabManager.ActiveTab(); at != nil {
		if pane := at.GetActivePane(); pane != nil {
			workDir = pane.CurrentDir()
		}
	}
	toolCfg := aitools.Config{
		UseReaderProxy: a.settingsMenu.Config.WebSearch.UseReaderProxy,
		ProxyURLs:      a.settingsMenu.Config.WebSearch.ReaderProxyURLs,
		WorkDir:        workDir,
	}

	// Configure timeout based on thinking mode
	timeout := 180 * time.Second
	if cfg.ThinkingMode && cfg.ExtendedTimeout > 0 {
		timeout = time.Duration(cfg.ExtendedTimeout) * time.Second
	}

	go func(id int, baseURL, model string, messages []ollama.Message, loadModel bool, thinkingEnabled bool, thinkingBudget int, useTools bool, toolCfg aitools.Config) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		client := ollama.NewClient(baseURL, model)
		// Configure thinking mode
		client.Thinking = ollama.ThinkingOptions{
			Enabled: thinkingEnabled,
			Budget:  thinkingBudget,
		}

		loadSuccess := false
		if loadModel {
			a.aiResponses <- aiResponse{id: id, token: "", done: false} // Signal streaming start
			if err := client.LoadModel(ctx); err != nil {
				a.aiResponses <- aiResponse{id: id, err: err, done: true}
				return
			}
			loadSuccess = true
			// Signal: model loaded, now thinking
			a.aiResponses <- aiResponse{id: id, token: "", done: false, loaded: true}
		}

		var toolDefs []ollama.ToolDef
		var registry *aitools.Registry
		if useTools {
			registry = aitools.NewRegistry(toolCfg)
			for _, t := range registry.Tools() {
				toolDefs = append(toolDefs, ollama.ToolDef{
					Type: "function",
					Function: ollama.ToolDefFunction{
						Name:        t.Name,
						Description: t.Description,
						Parameters:  t.Parameters,
					},
				})
			}
		}

		onToken := func(token string) {
			a.aiResponses <- aiResponse{id: id, token: token, done: false}
		}

		// Agent loop: stream a response; if the model calls tools, run
		// them (read-only by construction) and continue with the results
		// until it answers in plain text or the round budget runs out.
		const maxToolRounds = 6
		var result ollama.ChatResult
		var err error
		for round := 0; ; round++ {
			result, err = client.ChatStreamWithTools(ctx, messages, toolDefs, onToken, nil)
			if err != nil && len(toolDefs) > 0 && strings.Contains(strings.ToLower(err.Error()), "does not support tools") {
				// Model can't do tool calls; degrade to a plain chat
				// rather than failing the whole conversation.
				toolDefs = nil
				result, err = client.ChatStreamWithTools(ctx, messages, nil, onToken, nil)
			}
			if err != nil || len(result.ToolCalls) == 0 || round >= maxToolRounds {
				break
			}

			messages = append(messages, ollama.Message{
				Role:      "assistant",
				Content:   result.Content,
				ToolCalls: result.ToolCalls,
			})
			for _, call := range result.ToolCalls {
				name := call.Function.Name
				a.aiResponses <- aiResponse{id: id, toolNote: name + summarizeToolArgs(call.Function.Arguments)}
				output, terr := registry.Execute(ctx, name, call.Function.Arguments)
				if terr != nil {
					output = "Error: " + terr.Error()
				}
				messages = append(messages, ollama.Message{
					Role:     "tool",
					Content:  output,
					ToolName: name,
				})
			}
		}
		a.aiResponses <- aiResponse{id: id, thinking: result.Thinking, err: err, done: true, loaded: loadSuccess}
	}(requestID, cfg.URL, cfg.Model, messages, needLoad, cfg.ThinkingMode, cfg.ThinkingBudget, toolsEnabled, toolCfg)
}
