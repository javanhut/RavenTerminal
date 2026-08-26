package render

import "testing"

func helpContentWidths() (longestKey, longestDesc, longestFull int) {
	longestFull = max(textCells(helpTitle), textCells(helpFooter))
	for _, section := range buildHelpSections() {
		longestFull = max(longestFull, textCells(section.title))
		for _, b := range section.bindings {
			longestKey = max(longestKey, textCells(b[0]))
			longestDesc = max(longestDesc, textCells(b[1]))
		}
	}
	return
}

// The help panel used to be capped at 700 raw pixels, which is only ~29
// columns on a HiDPI framebuffer: every binding spilled past the border.
func TestHelpPanelFitsContent(t *testing.T) {
	longestKey, longestDesc, longestFull := helpContentWidths()
	l := helpPanelLayout(3456, 24, longestKey, longestDesc, longestFull) // HiDPI cells

	if l.keyCells < longestKey || l.descCells < longestDesc || l.contentCells < longestFull {
		t.Errorf("panel clipped its own content: layout %+v, needs key=%d desc=%d full=%d",
			l, longestKey, longestDesc, longestFull)
	}
}

// Nothing may draw past the border at any window size or font size: every
// string the renderer draws is truncated to one of the layout's cell budgets.
func TestHelpPanelNeverOverflows(t *testing.T) {
	longestKey, longestDesc, longestFull := helpContentWidths()
	sections := buildHelpSections()

	for _, cellWidth := range []float32{7, 12, 24, 60} {
		for _, width := range []int{200, 400, 800, 1600, 3456} {
			l := helpPanelLayout(width, cellWidth, longestKey, longestDesc, longestFull)

			if l.panelWidth+l.marginX*2 > float32(width)+0.01 {
				t.Fatalf("cell %v width %d: panel %v overflows window", cellWidth, width, l.panelWidth)
			}
			used := int(l.keyColWidth/cellWidth) + l.descCells
			if used > l.contentCells {
				t.Fatalf("cell %v width %d: columns use %d of %d content cells (%+v)",
					cellWidth, width, used, l.contentCells, l)
			}

			// Full-width lines.
			for _, line := range []string{helpTitle, helpFooter} {
				if got := textCells(truncateToCells(line, l.contentCells)); got > l.contentCells {
					t.Fatalf("cell %v width %d: %q draws %d cells, budget %d", cellWidth, width, line, got, l.contentCells)
				}
			}
			// Two-column lines: indent + key must clear the description column.
			for _, section := range sections {
				if got := textCells(truncateToCells(section.title, l.contentCells)); got > l.contentCells {
					t.Fatalf("cell %v width %d: section %q draws %d cells, budget %d",
						cellWidth, width, section.title, got, l.contentCells)
				}
				for _, b := range section.bindings {
					key := textCells(truncateToCells(b[0], l.keyCells))
					if float32(key)*cellWidth+l.keyIndent > l.keyColWidth {
						t.Fatalf("cell %v width %d: key %q overruns the description column", cellWidth, width, b[0])
					}
					if got := textCells(truncateToCells(b[1], l.descCells)); got > l.descCells {
						t.Fatalf("cell %v width %d: desc %q draws %d cells, budget %d",
							cellWidth, width, b[1], got, l.descCells)
					}
				}
			}
		}
	}
}

func TestTruncateToCellsRespectsBudget(t *testing.T) {
	for _, max := range []int{0, 1, 2, 3, 4, 8} {
		if got := textCells(truncateToCells("Search / open result / send message", max)); got > max {
			t.Errorf("max %d: drew %d cells", max, got)
		}
	}
}
