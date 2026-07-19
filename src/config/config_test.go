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

// Save must round-trip through Load, write atomically (no stray temp file),
// and preserve an unparseable existing config as .bak instead of clobbering
// the only copy the user could still repair.
func TestSaveBacksUpBrokenConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := GetConfigPath()

	cfg := DefaultConfig()
	cfg.Theme = "test-theme"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load()
	if err != nil || loaded.Theme != "test-theme" {
		t.Fatalf("round-trip: theme=%q err=%v", loaded.Theme, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: %v", err)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("valid config should not be backed up")
	}

	// Corrupt the file, then save again: the broken original must survive as .bak.
	if err := os.WriteFile(path, []byte("theme = [broken\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load should fail on corrupt config")
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save over corrupt: %v", err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil || string(bak) != "theme = [broken\n" {
		t.Fatalf("backup missing or wrong: %q, %v", bak, err)
	}
	if loaded, err := Load(); err != nil || loaded.Theme != "test-theme" {
		t.Fatalf("post-backup round-trip: theme=%q err=%v", loaded.Theme, err)
	}
}
