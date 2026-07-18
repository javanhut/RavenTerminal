package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestListOllamaModelsAsyncDoesNotBlock verifies the dispatch returns
// immediately even when the server is slow: the network wait must never
// happen on the caller's (GLFW main) thread.
func TestListOllamaModelsAsyncDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Write([]byte(`{"models":[{"name":"llama3"}]}`))
	}))
	defer srv.Close()
	defer close(release)

	results := make(chan ollamaModelsResponse, 1)
	start := time.Now()
	listOllamaModelsAsync(srv.URL, 5*time.Second, results)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("dispatch blocked for %v", elapsed)
	}
	select {
	case <-results:
		t.Fatal("result delivered before server responded")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestListOllamaModelsAsyncDeliversModels verifies the fetched model list
// lands on the channel.
func TestListOllamaModelsAsyncDeliversModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[{"name":"llama3"},{"name":"mistral"}]}`))
	}))
	defer srv.Close()

	results := make(chan ollamaModelsResponse, 1)
	listOllamaModelsAsync(srv.URL, 5*time.Second, results)
	select {
	case resp := <-results:
		if resp.err != nil {
			t.Fatalf("unexpected error: %v", resp.err)
		}
		if len(resp.models) != 2 || resp.models[0] != "llama3" || resp.models[1] != "mistral" {
			t.Fatalf("unexpected models: %v", resp.models)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no result delivered")
	}
}

// TestListOllamaModelsAsyncDeliversError verifies an unreachable endpoint
// produces an error on the channel instead of hanging.
func TestListOllamaModelsAsyncDeliversError(t *testing.T) {
	results := make(chan ollamaModelsResponse, 1)
	listOllamaModelsAsync("http://127.0.0.1:1", 500*time.Millisecond, results)
	select {
	case resp := <-results:
		if resp.err == nil {
			t.Fatal("expected an error for unreachable endpoint")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no result delivered")
	}
}
