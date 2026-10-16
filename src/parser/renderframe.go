package parser

import (
	"time"

	"github.com/javanhut/RavenTerminal/src/grid"
	"github.com/javanhut/RavenTerminal/src/images"
)

// RenderFrame is everything the renderer needs from one pane per frame, taken
// under a single Terminal.mu acquisition so the snapshot, image anchors and
// modes agree with each other.
type RenderFrame struct {
	Snap       *grid.Snapshot
	Grid       *grid.Grid
	ViewTopAbs int // absolute row shown on viewport row 0
	MinAbs     int // first absolute row still held in the buffer
	AltScreen  bool
	Reverse    bool // DECSCNM
	CellW      int  // cell pixel size the terminal sized images with
	CellH      int

	ProgressState, ProgressValue int

	// ImageStore identifies the active store (identity only: never call its
	// methods outside the terminal lock). Placements and Virtuals are
	// copy-on-write slices; Images holds every stored image.
	ImageStore *images.Store
	Placements []*images.Placement
	Virtuals   []*images.Placement
	Images     map[images.ImageID]*images.Image
	// NextAnimFrame is when a placed kitty animation next changes frame
	// (zero when none is running); Images already hold the current frames.
	NextAnimFrame time.Time
}

// RenderFrame fills f, reusing its Images map. Placements whose rows were
// trimmed out of scrollback are pruned from the store here.
func (t *Terminal) RenderFrame(f *RenderFrame) {
	t.mu.Lock()
	defer t.mu.Unlock()
	store := t.activeImages()
	f.NextAnimFrame = store.Tick(time.Now())
	if gen := store.AnimGeneration(); store != t.animStore || gen != t.animGen {
		t.animStore, t.animGen = store, gen
		t.Grid.MarkScreenDirty()
	}
	s := t.Grid.Snapshot(t.snapPrev)
	s.CursorVisible = t.cursorVisible
	t.snapPrev = s

	f.Snap, f.Grid = s, t.Grid
	f.ViewTopAbs = t.Grid.AbsRowForViewRow(0)
	f.MinAbs = f.ViewTopAbs + s.ScrollOffset - t.Grid.ScrollbackLen()
	f.AltScreen = t.alternateScreen
	f.Reverse = t.ctl.reverseVideo
	f.CellW, f.CellH = t.cellSize()
	f.ProgressState, f.ProgressValue = t.osc.progressState, t.osc.progressValue

	store.PrunePlacementsBelow(f.MinAbs)
	f.ImageStore = store
	f.Placements = store.Placements()
	f.Virtuals = store.VirtualPlacements()
	clear(f.Images)
	ids := store.ImageIDs()
	if len(ids) > 0 && f.Images == nil {
		f.Images = make(map[images.ImageID]*images.Image, len(ids))
	}
	for _, id := range ids {
		if img := store.Display(id); img != nil {
			f.Images[id] = img
		}
	}
}
