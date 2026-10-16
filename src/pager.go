package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/javanhut/RavenTerminal/src/grid"
)

func sgrColor(b []string, c grid.Color, base int) []string {
	switch c.Type {
	case grid.ColorIndexed:
		switch {
		case c.Index < 8:
			return append(b, strconv.Itoa(base+int(c.Index)))
		case c.Index < 16:
			return append(b, strconv.Itoa(base+60+int(c.Index)-8))
		}
		return append(b, strconv.Itoa(base+8), "5", strconv.Itoa(int(c.Index)))
	case grid.ColorRGB:
		return append(b, strconv.Itoa(base+8), "2", strconv.Itoa(int(c.R)), strconv.Itoa(int(c.G)), strconv.Itoa(int(c.B)))
	}
	return b
}

var sgrFlags = []struct {
	f grid.CellFlags
	p string
}{
	{grid.FlagBold, "1"}, {grid.FlagDim, "2"}, {grid.FlagItalic, "3"}, {grid.FlagUnderline, "4"},
	{grid.FlagBlink, "5"}, {grid.FlagInverse, "7"}, {grid.FlagHidden, "8"}, {grid.FlagStrikethrough, "9"},
	{grid.FlagOverline, "53"},
}

// sgrParams is the SGR parameter list for a cell's style ("" = default).
func sgrParams(c grid.Cell) string {
	var b []string
	for _, f := range sgrFlags {
		if c.Flags&f.f != 0 {
			b = append(b, f.p)
		}
	}
	b = sgrColor(b, c.Fg, 30)
	b = sgrColor(b, c.Bg, 40)
	return strings.Join(b, ";")
}

// scrollbackANSI dumps the whole buffer as text with SGR colours, joining
// soft-wrapped rows and dropping trailing blank lines.
func scrollbackANSI(g *grid.Grid) string {
	first, last := g.AbsBounds()
	var lines []string
	var line strings.Builder
	for abs := first; abs <= last; abs++ {
		cells := g.RowCells(abs)
		end := len(cells)
		for end > 0 && (cells[end-1].Char == ' ' || cells[end-1].Char == 0) && sgrParams(cells[end-1]) == "" {
			end--
		}
		cur := ""
		for _, c := range cells[:end] {
			if c.Width == grid.CellWidthContinuation {
				continue
			}
			if p := sgrParams(c); p != cur {
				switch {
				case p == "":
					line.WriteString("\x1b[0m")
				case cur == "":
					line.WriteString("\x1b[" + p + "m")
				default:
					line.WriteString("\x1b[0;" + p + "m")
				}
				cur = p
			}
			ch := c.Char
			if ch == 0 {
				ch = ' '
			}
			line.WriteRune(ch)
			for _, cm := range c.Combining {
				line.WriteRune(cm)
			}
		}
		if cur != "" {
			line.WriteString("\x1b[0m")
		}
		if g.RowSoftWrapped(abs) && abs < last {
			continue
		}
		lines = append(lines, line.String())
		line.Reset()
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// writeScrollbackFile stores the dump in a private temp file.
func writeScrollbackFile(content string) (string, error) {
	f, err := os.CreateTemp("", "raven-scrollback-*.txt")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}

// pagerCommand opens the dump in less at the end and deletes it afterwards,
// also when the tab is closed while less is running.
func pagerCommand(path string) []string {
	return []string{"sh", "-c", `trap 'rm -f -- "$1"' EXIT HUP INT TERM; less -R +G -- "$1"`, "raven-pager", path}
}
