package ollama

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                         "",
		" localhost:11434/ ":       "http://localhost:11434",
		"https://example.com/api/": "https://example.com/api",
	}
	for input, want := range cases {
		if got := normalizeBaseURL(input); got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestClientListModels(t *testing.T) {
	client := NewClient("http://ollama.test", "")
	client.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"models":[{"name":"qwen"},{"model":"llama"},{"name":" "}]}`)),
			Request:    r,
		}, nil
	})

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"qwen", "llama"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
}

func TestExtractThinking(t *testing.T) {
	content, thinking := ExtractThinking("<think>first</think>Hello<think>second</think>")
	if content != "Hello" || thinking != "first\n\nsecond" {
		t.Fatalf("content=%q thinking=%q", content, thinking)
	}
}
