package parser

import (
	"strings"
	"testing"
)

// sixelImage builds a w x (6*bands) sixel with every pixel set.
func sixelImage(w, bands int) string {
	var b strings.Builder
	b.WriteString("\x1bP0;1;0q\"1;1;")
	b.WriteString(itoa(w) + ";" + itoa(bands*6))
	b.WriteString("#1;2;100;0;0#1")
	for i := range bands {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString("!" + itoa(w) + "~")
	}
	b.WriteString("\x1b\\")
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func TestSixelPlacedAtCursor(t *testing.T) {
	term := NewTerminal(40, 10)
	term.SetCellPixelSize(10, 12)
	feed(term, "\x1b[2;3H")
	feed(term, sixelImage(25, 4)) // 25x24 px -> 3x2 cells
	ps := term.Images().Placements()
	if len(ps) != 1 {
		t.Fatalf("placements = %d", len(ps))
	}
	p := ps[0]
	if p.AnchorCol != 2 || p.AnchorAbsRow != term.Grid.AbsoluteCursorRow()-2 || p.Cols != 3 || p.Rows != 2 {
		t.Fatalf("placement %+v (abs cursor %d)", p, term.Grid.AbsoluteCursorRow())
	}
	img, ok := term.Images().Get(p.ImageID)
	if !ok || img.Width != 25 || img.Height != 24 {
		t.Fatal("image not stored")
	}
	// Cursor moves to the line below the image, at the image's left column.
	expectCursor(t, term, 2, 3)
	if got := lineText(term, 1); got != "" {
		t.Fatalf("sixel data leaked to screen: %q", got)
	}
}

func TestSixelScrollsAtBottom(t *testing.T) {
	term := NewTerminal(20, 4)
	term.SetCellPixelSize(10, 10)
	feed(term, "top\x1b[3;1H")
	feed(term, sixelImage(10, 5)) // 30px -> 3 rows
	if got := lineText(term, 0); got == "top" {
		t.Fatal("screen did not scroll to fit the image")
	}
	expectCursor(t, term, 0, 3)
}

func TestSixelDisplayModeNoScroll(t *testing.T) {
	term := NewTerminal(20, 4)
	term.SetCellPixelSize(10, 10)
	feed(term, "\x1b[?80h\x1b[3;5H")
	feed(term, sixelImage(10, 2))
	ps := term.Images().Placements()
	if len(ps) != 1 || ps[0].AnchorCol != 0 || ps[0].AnchorAbsRow != term.Grid.AbsoluteCursorRow()-2 {
		t.Fatalf("DECSDM placement %+v", ps)
	}
	expectCursor(t, term, 4, 2)
}

func TestSixelLargePayloadAccepted(t *testing.T) {
	term := NewTerminal(200, 50)
	term.SetCellPixelSize(10, 20)
	feed(term, "\x1bP0;1q#1;2;100;0;0#1"+strings.Repeat("~", 100000)+"\x1b\\")
	ps := term.Images().Placements()
	if len(ps) != 1 {
		t.Fatalf("large sixel dropped: %d", len(ps))
	}
}
