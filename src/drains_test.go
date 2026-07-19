package main

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javanhut/RavenTerminal/src/aipanel"
	"github.com/javanhut/RavenTerminal/src/config"
	"github.com/javanhut/RavenTerminal/src/menu"
	"github.com/javanhut/RavenTerminal/src/searchpanel"
	"github.com/javanhut/RavenTerminal/src/websearch"
)

// newDrainTestApp builds an App with only the fields the response drains
// touch — no window, renderer, or tab manager — so the tests run headless.
func newDrainTestApp() *App {
	return &App{
		searchPanel:           searchpanel.New(),
		aiPanel:               aipanel.New(),
		settingsMenu:          &menu.Menu{},
		searchCache:           newMemoCache[[]searchpanel.Result](),
		previewCache:          newMemoCache[cachedPreview](),
		searchResponses:       make(chan searchResponse, 4),
		previewResponses:      make(chan previewResponse, 4),
		aiResponses:           make(chan aiResponse, 4),
		modelLoadResponses:    make(chan modelLoadResponse, 2),
		ollamaTestResponses:   make(chan ollamaModelsResponse, 2),
		ollamaModelsResponses: make(chan ollamaModelsResponse, 2),
	}
}

func TestDrainSearchResponsesAppliesResults(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep SaveSearchHistory off the real config dir
	a := newDrainTestApp()
	a.searchPanel.SearchID = 1
	a.searchResponses <- searchResponse{id: 1, query: "raven", results: []websearch.Result{
		{Title: "Raven Terminal", URL: "https://example.com"},
		{Title: "Raven bird", URL: "https://example.org"},
	}}

	a.drainSearchResponses()

	if len(a.searchPanel.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(a.searchPanel.Results))
	}
	if a.searchPanel.Status != "2 results" {
		t.Errorf("status = %q, want %q", a.searchPanel.Status, "2 results")
	}
	if len(a.searchPanel.History) != 1 || a.searchPanel.History[0] != "raven" {
		t.Errorf("history = %v, want [raven]", a.searchPanel.History)
	}
	if _, ok := a.searchCache.get("raven"); !ok {
		t.Error("results not memoized")
	}
}

func TestDrainSearchResponsesIgnoresStaleID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := newDrainTestApp()
	a.searchPanel.SearchID = 2
	a.searchResponses <- searchResponse{id: 1, query: "old", results: []websearch.Result{
		{Title: "stale", URL: "https://example.com"},
	}}

	a.drainSearchResponses()

	if a.searchPanel.Results != nil {
		t.Errorf("stale response applied: %v", a.searchPanel.Results)
	}
	if _, ok := a.searchCache.get("old"); ok {
		t.Error("stale response memoized")
	}
}

func TestDrainPreviewResponsesAppliesPreview(t *testing.T) {
	a := newDrainTestApp()
	a.searchPanel.PreviewID = 5
	key := previewCacheKey(false, "https://example.com")
	a.previewResponses <- previewResponse{
		id: 5, url: "https://example.com", title: "Example",
		lines: []string{"first", "second"}, source: "direct", cacheKey: key,
	}

	a.drainPreviewResponses()

	if a.searchPanel.Status != "Source: direct HTML" {
		t.Errorf("status = %q, want %q", a.searchPanel.Status, "Source: direct HTML")
	}
	entry, ok := a.previewCache.get(key)
	if !ok {
		t.Fatal("preview not memoized")
	}
	if entry.source != "direct" || len(entry.lines) != 2 {
		t.Errorf("cache entry = %+v", entry)
	}
}

func TestDrainPreviewResponsesProxyAndProxyError(t *testing.T) {
	a := newDrainTestApp()
	a.searchPanel.PreviewID = 1
	a.previewResponses <- previewResponse{id: 1, url: "u1", title: "t", source: "proxy", cacheKey: previewCacheKey(true, "u1")}
	a.previewResponses <- previewResponse{id: 1, url: "u2", title: "t", source: "direct", proxyErr: "timeout", cacheKey: previewCacheKey(false, "u2")}

	a.drainPreviewResponses()

	// Both responses are for the live PreviewID, so the second one wins.
	if a.searchPanel.Status != "Proxy failed: timeout" {
		t.Errorf("status = %q, want %q", a.searchPanel.Status, "Proxy failed: timeout")
	}
	if _, ok := a.previewCache.get(previewCacheKey(true, "u1")); !ok {
		t.Error("proxy preview not memoized under the proxy key")
	}
}

func TestDrainPreviewResponsesIgnoresStaleID(t *testing.T) {
	a := newDrainTestApp()
	a.searchPanel.PreviewID = 9
	a.previewResponses <- previewResponse{id: 1, url: "https://example.com", title: "stale", source: "direct", cacheKey: previewCacheKey(false, "https://example.com")}

	a.drainPreviewResponses()

	if a.searchPanel.Status != "" {
		t.Errorf("stale preview changed status to %q", a.searchPanel.Status)
	}
	if _, ok := a.previewCache.get(previewCacheKey(false, "https://example.com")); ok {
		t.Error("stale preview memoized")
	}
}

func TestDrainAIResponsesStreamsTokens(t *testing.T) {
	a := newDrainTestApp()
	a.aiPanel.RequestID = 1
	a.aiPanel.Loading = true
	a.aiResponses <- aiResponse{id: 1, token: "Hello"}
	a.aiResponses <- aiResponse{id: 1, token: " world"}
	a.aiResponses <- aiResponse{id: 1, thinking: "thought", done: true, loaded: true}

	a.drainAIResponses()

	if a.aiPanel.Loading {
		t.Error("Loading still true after final response")
	}
	if a.aiPanel.Status != "" {
		t.Errorf("status = %q, want empty", a.aiPanel.Status)
	}
	if len(a.aiPanel.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(a.aiPanel.Messages))
	}
	msg := a.aiPanel.Messages[0]
	if msg.Role != "assistant" || msg.Content != "Hello world" {
		t.Errorf("message = %q %q", msg.Role, msg.Content)
	}
	if msg.Thinking != "thought" {
		t.Errorf("thinking = %q, want %q", msg.Thinking, "thought")
	}
	// loaded=true but no config loaded: the nil-Config guard must hold.
	if a.aiPanel.ModelLoaded {
		t.Error("ModelLoaded set without a config")
	}
}

func TestDrainAIResponsesLoadedSyncsConfig(t *testing.T) {
	a := newDrainTestApp()
	a.settingsMenu.Config = &config.Config{
		Ollama: config.OllamaConfig{URL: "http://localhost:11434", Model: "llama3"},
	}
	a.aiPanel.RequestID = 1
	a.aiPanel.Loading = true
	a.aiResponses <- aiResponse{id: 1, done: true, loaded: true}

	a.drainAIResponses()

	if !a.aiPanel.ModelLoaded {
		t.Fatal("ModelLoaded not set")
	}
	if a.aiPanel.LoadedURL != "http://localhost:11434" || a.aiPanel.LoadedModel != "llama3" {
		t.Errorf("loaded = %q %q", a.aiPanel.LoadedURL, a.aiPanel.LoadedModel)
	}
}

func TestDrainAIResponsesError(t *testing.T) {
	a := newDrainTestApp()
	a.aiPanel.RequestID = 1
	a.aiPanel.Loading = true
	a.aiResponses <- aiResponse{id: 1, done: true, err: errors.New("boom")}

	a.drainAIResponses()

	if a.aiPanel.Loading {
		t.Error("Loading still true after error")
	}
	if a.aiPanel.Status != "Error occurred" {
		t.Errorf("status = %q, want %q", a.aiPanel.Status, "Error occurred")
	}
	last := a.aiPanel.Messages[len(a.aiPanel.Messages)-1]
	if last.Role != "error" || last.Content != "boom" {
		t.Errorf("error message = %q %q", last.Role, last.Content)
	}
}

func TestDrainAIResponsesToolNoteKeepsLoading(t *testing.T) {
	a := newDrainTestApp()
	a.aiPanel.RequestID = 1
	a.aiPanel.Loading = true
	a.aiResponses <- aiResponse{id: 1, toolNote: "web_search: ravens"}

	a.drainAIResponses()

	if !a.aiPanel.Loading {
		t.Error("tool note stopped loading")
	}
	if a.aiPanel.Status != "Running tools..." {
		t.Errorf("status = %q, want %q", a.aiPanel.Status, "Running tools...")
	}
	last := a.aiPanel.Messages[len(a.aiPanel.Messages)-1]
	if last.Role != "tool" || last.Content != "web_search: ravens" {
		t.Errorf("tool message = %q %q", last.Role, last.Content)
	}
}

func TestDrainModelLoadResponses(t *testing.T) {
	a := newDrainTestApp()
	a.modelLoadResponses <- modelLoadResponse{url: "http://x", model: "llama3"}
	a.modelLoadResponses <- modelLoadResponse{err: errors.New("boom")}

	a.drainModelLoadResponses()

	// The error response lands second and wins.
	if a.aiPanel.ModelLoaded {
		t.Error("ModelLoaded true after failed load")
	}
	if a.aiPanel.Status != "Load failed" {
		t.Errorf("status = %q, want %q", a.aiPanel.Status, "Load failed")
	}
	last := a.aiPanel.Messages[len(a.aiPanel.Messages)-1]
	if last.Role != "error" || !strings.Contains(last.Content, "boom") {
		t.Errorf("error message = %q %q", last.Role, last.Content)
	}
}

func TestDrainOllamaMenuResponses(t *testing.T) {
	a := newDrainTestApp()
	a.ollamaTestResponses <- ollamaModelsResponse{err: errors.New("boom")}
	a.drainOllamaMenuResponses()
	if a.settingsMenu.StatusMessage != "Ollama test failed: boom" {
		t.Errorf("status = %q", a.settingsMenu.StatusMessage)
	}

	a.ollamaTestResponses <- ollamaModelsResponse{}
	a.drainOllamaMenuResponses()
	if a.settingsMenu.StatusMessage != "Ollama connection OK" {
		t.Errorf("status = %q", a.settingsMenu.StatusMessage)
	}

	a.ollamaModelsResponses <- ollamaModelsResponse{models: []string{"llama3", "mistral"}}
	a.drainOllamaMenuResponses()
	if len(a.settingsMenu.OllamaModels) != 2 {
		t.Errorf("models = %v", a.settingsMenu.OllamaModels)
	}
	if a.settingsMenu.StatusMessage != "Models loaded (2)" {
		t.Errorf("status = %q", a.settingsMenu.StatusMessage)
	}

	a.ollamaModelsResponses <- ollamaModelsResponse{}
	a.drainOllamaMenuResponses()
	if a.settingsMenu.StatusMessage != "No models found" {
		t.Errorf("status = %q", a.settingsMenu.StatusMessage)
	}
}

// TestDrainsConcurrentProducers mirrors the production wiring: producer
// goroutines (PTY readers, HTTP fetches) only ever talk to the main loop
// through the response channels, while a single goroutine — the GLFW thread
// in production, the test here — drains them. Run under -race to prove no
// unsynchronized state crosses that boundary.
func TestDrainsConcurrentProducers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := newDrainTestApp()
	a.searchPanel.SearchID = 1
	a.searchPanel.PreviewID = 1
	a.aiPanel.RequestID = 1
	a.aiPanel.Loading = true

	const tokens = 50
	previewKey := previewCacheKey(false, "https://example.com")

	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		a.searchResponses <- searchResponse{id: 1, query: "q", results: []websearch.Result{{Title: "t", URL: "u"}}}
	}()
	go func() {
		defer wg.Done()
		a.previewResponses <- previewResponse{id: 1, url: "https://example.com", title: "t", lines: []string{"l"}, source: "direct", cacheKey: previewKey}
	}()
	go func() {
		defer wg.Done()
		for range tokens {
			a.aiResponses <- aiResponse{id: 1, token: "x"}
		}
		a.aiResponses <- aiResponse{id: 1, done: true}
	}()
	go func() {
		defer wg.Done()
		a.ollamaTestResponses <- ollamaModelsResponse{}
	}()
	go func() {
		defer wg.Done()
		a.ollamaModelsResponses <- ollamaModelsResponse{models: []string{"llama3"}}
	}()

	// Drain until every producer's terminal state is observable. Channel
	// buffers are smaller than `tokens`, so the producers can only finish
	// while this loop is draining.
	previewCached := func() bool {
		_, ok := a.previewCache.get(previewKey)
		return ok
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		a.drainSearchResponses()
		a.drainPreviewResponses()
		a.drainAIResponses()
		a.drainModelLoadResponses()
		a.drainOllamaMenuResponses()
		if a.searchPanel.Results != nil && previewCached() && !a.aiPanel.Loading &&
			a.settingsMenu.OllamaModels != nil && a.settingsMenu.StatusMessage != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("drains never caught up: results=%v preview=%v loading=%v models=%v status=%q",
				a.searchPanel.Results != nil, previewCached(), a.aiPanel.Loading,
				a.settingsMenu.OllamaModels, a.settingsMenu.StatusMessage)
		}
	}
	wg.Wait()

	if len(a.searchPanel.Results) != 1 {
		t.Errorf("results = %d, want 1", len(a.searchPanel.Results))
	}
	if len(a.aiPanel.Messages) != 1 || a.aiPanel.Messages[0].Content != strings.Repeat("x", tokens) {
		t.Errorf("streamed message = %+v", a.aiPanel.Messages)
	}
	if len(a.settingsMenu.OllamaModels) != 1 || a.settingsMenu.OllamaModels[0] != "llama3" {
		t.Errorf("models = %v", a.settingsMenu.OllamaModels)
	}
}
