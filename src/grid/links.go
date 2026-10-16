package grid

import "sync"

// LinkTable interns OSC 8 hyperlinks into compact cell ids. One table is
// shared by a terminal's main and alternate grids so ids stay valid across
// screen switches. When the 16-bit id space is exhausted the owner sweeps it
// with the set of ids still referenced by cells; live ids are never reused.
type LinkTable struct {
	mu    sync.Mutex
	byKey map[string]uint16
	urls  map[uint16]string
	keys  map[uint16]string
	free  []uint16
	next  uint16
	max   uint16
}

func NewLinkTable() *LinkTable { return newLinkTableCap(0xffff) }

func newLinkTableCap(n uint16) *LinkTable {
	return &LinkTable{byKey: map[string]uint16{}, urls: map[uint16]string{}, keys: map[uint16]string{}, max: n}
}

// Intern returns the id for (key, url); key is the OSC 8 id= parameter (""
// shares the id among every use of the same url). ok is false when the table
// is full and must be swept first.
func (lt *LinkTable) Intern(key, url string) (uint16, bool) {
	if url == "" {
		return 0, true
	}
	k := key + "\x00" + url
	lt.mu.Lock()
	defer lt.mu.Unlock()
	if id, ok := lt.byKey[k]; ok {
		return id, true
	}
	var id uint16
	if n := len(lt.free); n > 0 {
		id = lt.free[n-1]
		lt.free = lt.free[:n-1]
	} else if lt.next < lt.max {
		lt.next++
		id = lt.next
	} else {
		return 0, false
	}
	lt.byKey[k] = id
	lt.urls[id] = url
	lt.keys[id] = k
	return id, true
}

// URL resolves an id ("" if unknown).
func (lt *LinkTable) URL(id uint16) string {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	return lt.urls[id]
}

// Sweep frees every id not present in live.
func (lt *LinkTable) Sweep(live map[uint16]bool) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	for id, k := range lt.keys {
		if !live[id] {
			delete(lt.keys, id)
			delete(lt.urls, id)
			delete(lt.byKey, k)
			lt.free = append(lt.free, id)
		}
	}
}

// SetLinkTable makes the grid resolve and intern links through lt.
func (g *Grid) SetLinkTable(lt *LinkTable) {
	g.mu.Lock()
	g.linkTable = lt
	g.mu.Unlock()
}

// SetLinkTableLocked is SetLinkTable for a caller already holding g.mu.
func (g *Grid) SetLinkTableLocked(lt *LinkTable) { g.linkTable = lt }

func (g *Grid) links() *LinkTable {
	if g.linkTable == nil {
		g.linkTable = NewLinkTable()
	}
	return g.linkTable
}

// MarkLiveLinksLocked adds every link id referenced by the grid's cells
// (screen and scrollback) to live. Caller holds g.mu (or owns the grid).
func (g *Grid) MarkLiveLinksLocked(live map[uint16]bool) {
	live[g.lastLink] = true
	for _, rows := range [][]*Row{g.history, g.rows} {
		for _, r := range rows {
			for i := range r.cells {
				if id := r.cells[i].Link; id != 0 {
					live[id] = true
				}
			}
		}
	}
}
