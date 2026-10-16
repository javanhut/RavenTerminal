package main

import (
	"strings"

	"github.com/javanhut/RavenTerminal/src/parser"
)

// quoteDroppedPaths shell-quotes dropped file paths, space separated with a
// trailing space so the next argument can follow. Paths with control
// characters use $'...' so a newline cannot run the command line.
func quoteDroppedPaths(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		if p == "" {
			continue
		}
		b.WriteString(quotePath(p))
		b.WriteByte(' ')
	}
	return b.String()
}

func quotePath(p string) string {
	safe, ctrl := true, false
	for _, r := range p {
		switch {
		case r < 0x20 || r == 0x7f:
			ctrl = true
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r >= 0x80:
		case strings.ContainsRune("/._-+,:@%=", r):
		default:
			safe = false
		}
	}
	switch {
	case ctrl:
		var b strings.Builder
		b.WriteString("$'")
		for _, r := range p {
			switch r {
			case '\n':
				b.WriteString(`\n`)
			case '\t':
				b.WriteString(`\t`)
			case '\r':
				b.WriteString(`\r`)
			case '\\', '\'':
				b.WriteByte('\\')
				b.WriteRune(r)
			default:
				if r < 0x20 || r == 0x7f {
					b.WriteString(`\x`)
					b.WriteByte("0123456789abcdef"[r>>4])
					b.WriteByte("0123456789abcdef"[r&15])
				} else {
					b.WriteRune(r)
				}
			}
		}
		b.WriteByte('\'')
		return b.String()
	case safe:
		return p
	}
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

// dropPaths pastes dropped files into a pane (bracketed when enabled).
func dropPaths(term *parser.Terminal, target interface{ Write([]byte) error }, paths []string) {
	if s := quoteDroppedPaths(paths); s != "" {
		writePaste(term, target, s)
	}
}
