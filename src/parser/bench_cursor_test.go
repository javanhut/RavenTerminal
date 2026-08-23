package parser

import (
	"bytes"
	"fmt"
	"testing"
)

// Mirrors the three corpora from scripts/bench.sh so profiles line up with the
// terminal-level numbers that motivated them.
func corpusCursor(lines int) []byte {
	var b bytes.Buffer
	for i := range lines {
		fmt.Fprintf(&b, "\033[s\033[1;1Hline %d\033[u\033[K%d\n", i, i)
	}
	return b.Bytes()
}

func corpusPlain(lines int) []byte {
	var b bytes.Buffer
	for range lines {
		b.WriteString("The quick brown fox jumps over the lazy dog 0123456789\n")
	}
	return b.Bytes()
}

func corpusSGR(lines int) []byte {
	var b bytes.Buffer
	for i := range lines {
		fmt.Fprintf(&b, "\033[31mred\033[32mgreen\033[34mblue\033[0m %d\n", i)
	}
	return b.Bytes()
}

func benchCorpus(b *testing.B, data []byte) {
	// One terminal for the whole run, as in real use. Constructing it per
	// iteration would allocate a fresh grid and scrollback each time and the
	// profile would show the runtime scavenging that, not the parser working.
	// Streaming into one terminal also means scrollback reaches steady state,
	// which is the condition a terminal actually spends its time in.
	t := NewTerminal(100, 30)
	t.Process(data) // fill scrollback before measuring

	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t.Process(data)
	}
}

func BenchmarkCursor(b *testing.B) { benchCorpus(b, corpusCursor(30000)) }
func BenchmarkPlain(b *testing.B)  { benchCorpus(b, corpusPlain(100000)) }
func BenchmarkSGR(b *testing.B)    { benchCorpus(b, corpusSGR(50000)) }

// Isolates the three escapes the cursor corpus mixes, to attribute cost.
func BenchmarkCursorParts(b *testing.B) {
	parts := []struct {
		name string
		seq  string
	}{
		{"SaveRestore", "\033[s\033[u"},
		{"AbsPosition", "\033[1;1H"},
		{"EraseLine", "\033[K"},
		{"TextOnly", "line 12345"},
	}
	for _, p := range parts {
		b.Run(p.name, func(b *testing.B) {
			var buf bytes.Buffer
			for range 30000 {
				buf.WriteString(p.seq)
			}
			benchCorpus(b, buf.Bytes())
		})
	}
}
