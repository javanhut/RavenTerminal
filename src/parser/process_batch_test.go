package parser

import "testing"

// TestProcessChunkingEquivalence: Process's printable-run fast path must be
// invisible to chunk boundaries — the same byte stream fed whole, byte-by-byte,
// or in odd-sized chunks yields identical screen contents and cursor state.
func TestProcessChunkingEquivalence(t *testing.T) {
	streams := []struct {
		name string
		data string
	}{
		{"plain", "hello world, plain printable run"},
		{"sgr-split", "red:\x1b[31mRED\x1b[0m done"},
		{"newlines", "line one\r\nline two\r\nline three"},
		{"utf8", "caf\xc3\xa9 \xe4\xb8\x96\xe7\x95\x8c ok"},
		// Invalid lead byte followed by printables, then a lone continuation
		// byte: if the fast path ran while utf8Remaining > 0, the stale state
		// would swallow \xa9 as a continuation of \xc3 and print an extra é.
		{"utf8-invalid", "\xc3A\xa9Z"},
		// \r puts existing text to the RIGHT of the cursor, so IRM insert
		// (shift) and overwrite produce different screens.
		{"insert-mode", "abc\r\x1b[4hXY\x1b[4lZ"},
		{"linedraw", "\x1b(0lqk\x1b(Bplain"},
		{"wrap", "0123456789012345678901234567890123456789"},
	}
	chunkSizes := []int{1, 3, 7}
	// Absolute expectations per stream: chunking equivalence alone would pass
	// if the fast path were equally wrong in both runs (e.g. bypassing the
	// line-drawing charset), so pin the actual rendered text where mapping
	// matters.
	wantText := map[string]string{
		"linedraw":     "┌─┐plain",
		"utf8-invalid": "AZ",
		"insert-mode":  "XYZbc",
	}
	for _, s := range streams {
		t.Run(s.name, func(t *testing.T) {
			whole := NewTerminal(20, 5)
			whole.Process([]byte(s.data))
			want := whole.Grid.VisibleText()
			wCol, wRow := whole.Grid.GetCursor()
			if abs, ok := wantText[s.name]; ok && want != abs {
				t.Fatalf("whole-stream text = %q, want %q", want, abs)
			}

			for _, sz := range chunkSizes {
				chunked := NewTerminal(20, 5)
				data := []byte(s.data)
				for i := 0; i < len(data); i += sz {
					end := min(i+sz, len(data))
					chunked.Process(data[i:end])
				}
				if got := chunked.Grid.VisibleText(); got != want {
					t.Errorf("chunk size %d: text %q, want %q", sz, got, want)
				}
				cCol, cRow := chunked.Grid.GetCursor()
				if cCol != wCol || cRow != wRow {
					t.Errorf("chunk size %d: cursor (%d,%d), want (%d,%d)", sz, cCol, cRow, wCol, wRow)
				}
			}
		})
	}
}
