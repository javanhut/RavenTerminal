package grid

import "testing"

func BenchmarkWriteChar(b *testing.B) {
	g := NewGrid(80, 24)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.WriteChar('x', DefaultFg(), DefaultBg(), 0, 0, 0, Color{})
		if g.CursorCol >= g.Cols-1 {
			g.SetCursorPos(1, 1)
		}
	}
}

func BenchmarkScroll(b *testing.B) {
	g := NewGrid(80, 24)
	// fill with content
	for range 24 {
		writeStr(g, "the quick brown fox jumps over the lazy dog 0123456789")
		g.CarriageReturn()
		g.Newline()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.ScrollUpWithBg(1, DefaultBg())
	}
}

func BenchmarkResize(b *testing.B) {
	g := NewGrid(80, 24)
	for range 24 {
		writeStr(g, "the quick brown fox jumps over the lazy dog")
		g.CarriageReturn()
		g.Newline()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			g.Resize(100, 30)
		} else {
			g.Resize(80, 24)
		}
	}
}

// BenchmarkReadFrame approximates the renderer's per-frame per-cell read cost
// (the pattern Snapshot will replace).
func BenchmarkReadFrame(b *testing.B) {
	g := NewGrid(80, 24)
	for range 24 {
		writeStr(g, "the quick brown fox jumps over the lazy dog 0123456789")
		g.CarriageReturn()
		g.Newline()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for row := 0; row < g.Rows; row++ {
			for col := 0; col < g.Cols; col++ {
				_ = g.DisplayCell(col, row)
			}
		}
	}
}
