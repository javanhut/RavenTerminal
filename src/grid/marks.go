package grid

// MarkKind is a set of OSC 133 semantic-prompt marks carried by a row. Marks
// live on the Row, so they travel into scrollback with it and are merged onto
// the first row of the logical line on reflow.
type MarkKind uint8

const (
	MarkPrompt     MarkKind = 1 << iota // OSC 133;A: a prompt starts on this row
	MarkInput                           // OSC 133;B: command input starts on this row
	MarkOutput                          // OSC 133;C at column 0: output starts on this row
	MarkOutputNext                      // OSC 133;C mid-row: output starts on the next row
	MarkEnd                             // OSC 133;D at column 0: output ended before this row
	MarkEndMid                          // OSC 133;D mid-row: output ends on this row
	markHasExit
)

// SetMarkLocked records an OSC 133 mark on the cursor row. Output and End
// marks are resolved against the cursor column. Caller holds g.mu.
func (g *Grid) SetMarkLocked(kind MarkKind) {
	if g.CursorRow < 0 || g.CursorRow >= len(g.rows) {
		return
	}
	mid := g.CursorCol > 0 || g.wrapPending
	switch {
	case kind == MarkOutput && mid:
		kind = MarkOutputNext
	case kind == MarkEnd && mid:
		kind = MarkEndMid
	}
	g.rows[g.CursorRow].mark |= kind
}

// SetExitCodeLocked stores a command's exit status on an absolute row
// (normally the row of its prompt). Caller holds g.mu.
func (g *Grid) SetExitCodeLocked(absRow, code int) {
	if r := g.rowAtAbsLocked(absRow); r != nil {
		r.mark |= markHasExit
		r.exit = int32(code)
	}
}

// ExitCodeAt returns the exit status recorded on an absolute row.
func (g *Grid) ExitCodeAt(absRow int) (int, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if r := g.rowAtAbsLocked(absRow); r != nil && r.mark&markHasExit != 0 {
		return int(r.exit), true
	}
	return 0, false
}

// RowMarkAt returns the marks on an absolute row.
func (g *Grid) RowMarkAt(absRow int) MarkKind {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.markAt(absRow) &^ markHasExit
}

func (g *Grid) markAt(abs int) MarkKind {
	if r := g.rowAtAbsLocked(abs); r != nil {
		return r.mark
	}
	return 0
}

func (g *Grid) lastAbsLocked() int { return g.scrolledOut + len(g.history) + len(g.rows) - 1 }

// PrevPrompt returns the absolute row of the closest prompt strictly above
// fromAbsRow, or -1.
func (g *Grid) PrevPrompt(fromAbsRow int) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for abs := min(fromAbsRow-1, g.lastAbsLocked()); abs >= g.scrolledOut; abs-- {
		if g.markAt(abs)&MarkPrompt != 0 {
			return abs
		}
	}
	return -1
}

// NextPrompt returns the absolute row of the closest prompt strictly below
// fromAbsRow, or -1.
func (g *Grid) NextPrompt(fromAbsRow int) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for abs := max(fromAbsRow+1, g.scrolledOut); abs <= g.lastAbsLocked(); abs++ {
		if g.markAt(abs)&MarkPrompt != 0 {
			return abs
		}
	}
	return -1
}

// outputEndLocked finds the last output row of a command whose output starts
// at start: the row before the next end/prompt mark, or up to the cursor when
// the command is still running.
func (g *Grid) outputEndLocked(start int) int {
	for abs := start; abs <= g.lastAbsLocked(); abs++ {
		m := g.markAt(abs)
		if m&MarkEndMid != 0 {
			return abs
		}
		if m&MarkEnd != 0 || (m&MarkPrompt != 0 && (abs > start || m&MarkOutput != 0)) {
			return abs - 1
		}
	}
	end := g.scrolledOut + len(g.history) + g.CursorRow
	if g.CursorCol == 0 && !g.wrapPending {
		end--
	}
	return end
}

func outputStart(abs int, m MarkKind) (int, bool) {
	switch {
	case m&MarkOutput != 0:
		return abs, true
	case m&MarkOutputNext != 0:
		return abs + 1, true
	}
	return 0, false
}

// LastOutputRange returns the absolute row range of the most recent command's
// output (inclusive). ok is false when there is none or it was empty.
func (g *Grid) LastOutputRange() (startAbsRow, endAbsRow int, ok bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for abs := g.lastAbsLocked(); abs >= g.scrolledOut; abs-- {
		if start, found := outputStart(abs, g.markAt(abs)); found {
			end := g.outputEndLocked(start)
			return start, end, end >= start
		}
	}
	return 0, 0, false
}

// CommandOutputAt returns the output range (inclusive absolute rows) of the
// command whose output contains absRow.
func (g *Grid) CommandOutputAt(absRow int) (start, end int, ok bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if absRow < g.scrolledOut || absRow > g.lastAbsLocked() {
		return 0, 0, false
	}
	start = -1
	for abs := absRow; abs >= g.scrolledOut; abs-- {
		m := g.markAt(abs)
		if s, found := outputStart(abs, m); found {
			if s > absRow {
				return 0, 0, false
			}
			start = s
			break
		}
		if m&(MarkPrompt|MarkInput) != 0 || (m&MarkEnd != 0) || (m&MarkEndMid != 0 && abs < absRow) {
			return 0, 0, false
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	end = g.outputEndLocked(start)
	return start, end, absRow <= end
}
