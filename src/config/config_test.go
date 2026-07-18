package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Bookmark/history saves must be atomic: a crash mid-write must never leave a
// truncated file that a later load reads as empty (and then overwrites).
func TestWriteFileAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bookmarks.json")
	if err := writeFileAtomic(path, []byte("old")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	if err := writeFileAtomic(path, []byte("new")); err != nil {
		t.Fatalf("writeFileAtomic overwrite: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("read back %q, %v", data, err)
	}
	// No temp file left behind.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: %v", err)
	}
}

// Clipboard read (OSC 52 query) is a data-exfiltration vector, so it must be
// opt-in: off by default.
func TestAllowClipboardReadDefaultsFalse(t *testing.T) {
	if DefaultConfig().AllowClipboardRead {
		t.Fatal("AllowClipboardRead must default to false")
	}
}

func TestAddBookmark(t *testing.T) {
	var list []Bookmark
	list = AddBookmark(list, Bookmark{Title: "A", URL: "https://a.example"})
	list = AddBookmark(list, Bookmark{Title: "B", URL: "https://b.example"})
	if len(list) != 2 || list[0].URL != "https://b.example" {
		t.Fatalf("expected newest first, got %v", list)
	}

	// Re-adding the same URL dedupes and moves it to the front (title updates).
	list = AddBookmark(list, Bookmark{Title: "A2", URL: "https://a.example"})
	if len(list) != 2 {
		t.Fatalf("dedupe failed, got %d entries", len(list))
	}
	if list[0].URL != "https://a.example" || list[0].Title != "A2" {
		t.Fatalf("expected re-added bookmark first with new title, got %v", list[0])
	}
}

func TestAddBookmarkCap(t *testing.T) {
	var list []Bookmark
	for i := range maxBookmarks + 10 {
		list = AddBookmark(list, Bookmark{URL: fmt.Sprintf("https://x.example/%d", i)})
	}
	if len(list) != maxBookmarks {
		t.Fatalf("len = %d, want %d", len(list), maxBookmarks)
	}
	// Newest survives, oldest fell off.
	if list[0].URL != fmt.Sprintf("https://x.example/%d", maxBookmarks+9) {
		t.Fatalf("newest not first: %v", list[0])
	}
}
