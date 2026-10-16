package grid

// PromptMark is an OSC 133 prompt row with its command's exit status.
type PromptMark struct {
	AbsRow  int
	Exit    int
	HasExit bool
}

// AbsBounds returns the first and last absolute rows held in the buffer.
func (g *Grid) AbsBounds() (first, last int) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.scrolledOut, g.lastAbsLocked()
}

// PromptMarks appends to dst the prompt rows in [fromAbs, toAbs].
func (g *Grid) PromptMarks(fromAbs, toAbs int, dst []PromptMark) []PromptMark {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for abs := max(fromAbs, g.scrolledOut); abs <= min(toAbs, g.lastAbsLocked()); abs++ {
		r := g.rowAtAbsLocked(abs)
		if r == nil || r.mark&MarkPrompt == 0 {
			continue
		}
		dst = append(dst, PromptMark{AbsRow: abs, Exit: int(r.exit), HasExit: r.mark&markHasExit != 0})
	}
	return dst
}
