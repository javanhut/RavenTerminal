package grid

import "testing"

// fillReflowGrid builds a 10k-row x 200-col buffer of mixed-length lines.
func fillReflowGrid(b *testing.B) *Grid {
	g := newGrid(200, 50, 10000)
	line := []rune("the quick brown fox jumps over the lazy dog 0123456789 ")
	for i := range 10050 {
		n := 40 + (i*37)%400 // some lines soft-wrap past 200 cols
		rs := make([]rune, n)
		for j := range rs {
			rs[j] = line[(i+j)%len(line)]
		}
		g.WriteRunes(rs, IndexedColor(uint8(i%8)), DefaultBg(), 0, 0, 0, Color{})
		g.CarriageReturn()
		g.Newline()
	}
	return g
}

func BenchmarkReflow10kx200(b *testing.B) {
	g := fillReflowGrid(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			g.Resize(150, 50)
		} else {
			g.Resize(200, 50)
		}
	}
}

func BenchmarkResizeRowsOnly(b *testing.B) {
	g := fillReflowGrid(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			g.Resize(200, 40)
		} else {
			g.Resize(200, 50)
		}
	}
}
