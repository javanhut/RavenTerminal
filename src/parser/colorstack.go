package parser

import (
	"maps"
	"strconv"

	"github.com/javanhut/RavenTerminal/src/grid"
)

// maxColorStack is xterm's limit on saved palettes (XTPUSHCOLORS).
const maxColorStack = 10

// colorSlot is one XTPUSHCOLORS entry: the dynamic colours and palette
// overrides in force when it was pushed.
type colorSlot struct {
	set            bool
	fg, bg, cursor grid.Color
	palette        map[int]grid.Color
}

// colorStack holds the saved palettes. used is the current position, last the
// highest slot ever filled, as XTREPORTCOLORS reports them.
type colorStack struct {
	slots      [maxColorStack]colorSlot
	used, last int
}

// pushColors implements XTPUSHCOLORS (CSI Ps # P): save into slot Ps, or the
// next slot when Ps is 0.
func (t *Terminal) pushColors(slot int) {
	cs := &t.osc.colors
	if slot == 0 {
		slot = cs.used + 1
	}
	if slot < 1 || slot > maxColorStack {
		return
	}
	cs.slots[slot-1] = colorSlot{set: true, fg: t.fgColor, bg: t.bgColor, cursor: t.cursorColor,
		palette: maps.Clone(t.paletteOverride)}
	cs.used, cs.last = slot, max(cs.last, slot)
}

// popColors implements XTPOPCOLORS (CSI Ps # Q): restore slot Ps, or the
// current one when Ps is 0, and drop the stack below it.
func (t *Terminal) popColors(slot int) {
	cs := &t.osc.colors
	if slot == 0 {
		slot = cs.used
	}
	if slot < 1 || slot > maxColorStack || !cs.slots[slot-1].set {
		return
	}
	s := cs.slots[slot-1]
	t.fgColor, t.bgColor, t.cursorColor = s.fg, s.bg, s.cursor
	t.paletteOverride = maps.Clone(s.palette)
	cs.used = slot - 1
	t.Grid.MarkScreenDirtyLocked()
}

// reportColors implements XTREPORTCOLORS (CSI # R).
func (t *Terminal) reportColors() {
	cs := &t.osc.colors
	t.reply("\x1b[" + strconv.Itoa(cs.used) + ";" + strconv.Itoa(cs.last) + "#Q")
}
