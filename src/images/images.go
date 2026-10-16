// Package images defines a protocol-neutral model for inline terminal images
// (Kitty graphics and Sixel) plus a small store. Decoded pixels live in Image;
// each on-screen instance is a Placement anchored to an absolute scrollback row
// so it scrolls with the content using the same arithmetic as the grid.
package images

import "slices"

// ImageID identifies a stored image.
type ImageID uint64

// Image is an immutable RGBA pixel payload.
type Image struct {
	ID     ImageID
	Number uint32 // Kitty image number (I=), 0 if none
	Width  int    // pixels
	Height int    // pixels
	Pixels []byte // RGBA, len == 4*Width*Height
	seq    uint64
}

// Placement is one on-screen instance of an Image.
type Placement struct {
	ImageID     ImageID
	PlacementID uint32
	// AnchorAbsRow is the absolute row (scrolled-out + history + active index)
	// of the placement's top-left cell, so it scrolls with content/history.
	AnchorAbsRow int
	AnchorCol    int
	Cols, Rows   int // covered area in terminal cells
	ZIndex       int // <0 draws under text, >=0 over text
	// Source rectangle in image pixels; SrcW/SrcH == 0 means "to the edge".
	SrcX, SrcY, SrcW, SrcH int
	// Pixel offset of the image inside the anchor cell.
	OffsetX, OffsetY int
	// Virtual placements (Kitty U=1) are not drawn directly; cells holding
	// U+10EEEE placeholders reference them.
	Virtual bool
}

// Per-store caps on image and placement counts.
const (
	MaxImages     = 1024
	MaxPlacements = 4096
)

// internalBase starts ids for terminal-created images (sixel), above the
// 32-bit range Kitty clients address with i=.
const internalBase ImageID = 1 << 32

// Store holds images and their placements for one screen (main or alt).
// Placement slices are copy-on-write so a slice handed to the renderer is
// never mutated afterwards.
type Store struct {
	images     map[ImageID]*Image
	placements []*Placement
	virtuals   []*Placement
	nextID     ImageID
	nextInt    ImageID
	seq        uint64
	totalBytes int
	maxBytes   int
}

// NewStore creates an image store with a total pixel-memory budget in bytes.
func NewStore(maxBytes int) *Store {
	return &Store{
		images:   make(map[ImageID]*Image),
		maxBytes: maxBytes,
	}
}

// Add stores an image, assigning an id if id==0, and enforces the byte budget by
// evicting the least-recently-added images. Returns the stored image.
func (s *Store) Add(id ImageID, width, height int, pixels []byte) *Image {
	if id == 0 {
		s.nextID++
		for s.images[s.nextID] != nil {
			s.nextID++
		}
		id = s.nextID
	} else if id >= s.nextID && id < internalBase {
		s.nextID = id
	}
	return s.put(id, width, height, pixels)
}

// AddInternal stores a terminal-created image under an id outside the Kitty
// client id space, so clients can neither replace nor delete it by i=.
func (s *Store) AddInternal(width, height int, pixels []byte) *Image {
	s.nextInt++
	return s.put(internalBase+s.nextInt, width, height, pixels)
}

func (s *Store) put(id ImageID, width, height int, pixels []byte) *Image {
	if old, ok := s.images[id]; ok {
		s.totalBytes -= cap(old.Pixels)
	}
	s.seq++
	img := &Image{ID: id, Width: width, Height: height, Pixels: pixels, seq: s.seq}
	s.images[id] = img
	s.totalBytes += cap(pixels)
	s.evictIfNeeded(id)
	return img
}

// Get returns a stored image by id.
func (s *Store) Get(id ImageID) (*Image, bool) {
	img, ok := s.images[id]
	return img, ok
}

// ByNumber returns the newest image carrying the Kitty image number n.
func (s *Store) ByNumber(n uint32) (*Image, bool) {
	var best *Image
	for _, img := range s.images {
		if img.Number == n && (best == nil || img.ID > best.ID) {
			best = img
		}
	}
	return best, best != nil
}

// ImageIDs returns every stored image id.
func (s *Store) ImageIDs() []ImageID {
	ids := make([]ImageID, 0, len(s.images))
	for id := range s.images {
		ids = append(ids, id)
	}
	return ids
}

// Place records a placement. A placement with a non-zero PlacementID replaces
// the existing one with the same image and placement id.
func (s *Store) Place(p *Placement) {
	list := &s.placements
	if p.Virtual {
		list = &s.virtuals
	}
	cur := *list
	if p.PlacementID != 0 {
		if i := slices.IndexFunc(cur, func(q *Placement) bool {
			return q.ImageID == p.ImageID && q.PlacementID == p.PlacementID
		}); i >= 0 {
			cur = slices.Concat(cur[:i], cur[i+1:])
		}
	}
	if len(cur) >= MaxPlacements {
		cur = cur[len(cur)-MaxPlacements+1:]
	}
	// Appending past len never touches elements a caller already holds.
	*list = append(cur, p)
}

// Placements returns the current real placements (caller must not mutate).
func (s *Store) Placements() []*Placement { return s.placements }

// VirtualPlacements returns the Kitty unicode-placeholder placements.
func (s *Store) VirtualPlacements() []*Placement { return s.virtuals }

// VirtualPlacement finds the virtual placement for an image; pid 0 matches the
// most recent one.
func (s *Store) VirtualPlacement(id ImageID, pid uint32) (*Placement, bool) {
	for i := len(s.virtuals) - 1; i >= 0; i-- {
		p := s.virtuals[i]
		if p.ImageID == id && (pid == 0 || p.PlacementID == pid) {
			return p, true
		}
	}
	return nil, false
}

// DeleteAll removes all images and placements.
func (s *Store) DeleteAll() {
	s.images = make(map[ImageID]*Image)
	s.placements = nil
	s.virtuals = nil
	s.totalBytes = 0
}

// DeleteImage removes an image and any placements referencing it.
func (s *Store) DeleteImage(id ImageID) {
	if img, ok := s.images[id]; ok {
		s.totalBytes -= cap(img.Pixels)
		delete(s.images, id)
	}
	s.dropPlacements(func(p *Placement) bool { return p.ImageID == id })
}

// DeletePlacements removes the placements (real and virtual) matching remove.
// With free, images that lost a placement here, plus the extra candidates,
// are freed once nothing references them any more.
func (s *Store) DeletePlacements(remove func(*Placement) bool, free bool, candidates ...ImageID) {
	touched := map[ImageID]bool{}
	for _, id := range candidates {
		touched[id] = true
	}
	s.dropPlacements(func(p *Placement) bool {
		if remove(p) {
			touched[p.ImageID] = true
			return true
		}
		return false
	})
	if !free {
		return
	}
	for id := range touched {
		if !s.referenced(id) {
			s.DeleteImage(id)
		}
	}
}

func (s *Store) referenced(id ImageID) bool {
	for _, p := range s.placements {
		if p.ImageID == id {
			return true
		}
	}
	for _, p := range s.virtuals {
		if p.ImageID == id {
			return true
		}
	}
	return false
}

// ClearPlacements removes all placements but keeps stored images.
func (s *Store) ClearPlacements() {
	s.placements = nil
	s.virtuals = nil
}

// PrunePlacementsBelow removes placements whose anchor scrolled out of history
// (absolute row below the given trimmed threshold).
func (s *Store) PrunePlacementsBelow(minAbsRow int) {
	kept := make([]*Placement, 0, len(s.placements))
	for _, p := range s.placements {
		if p.AnchorAbsRow+p.Rows > minAbsRow {
			kept = append(kept, p)
		}
	}
	s.placements = kept
}

func (s *Store) dropPlacements(remove func(*Placement) bool) {
	filter := func(in []*Placement) []*Placement {
		kept := make([]*Placement, 0, len(in))
		for _, p := range in {
			if !remove(p) {
				kept = append(kept, p)
			}
		}
		return kept
	}
	s.placements = filter(s.placements)
	s.virtuals = filter(s.virtuals)
}

func (s *Store) evictIfNeeded(keep ImageID) {
	// Evict the least recently added images until within budget.
	for (s.maxBytes > 0 && s.totalBytes > s.maxBytes || len(s.images) > MaxImages) && len(s.images) > 1 {
		var oldest *Image
		for id, img := range s.images {
			if id != keep && (oldest == nil || img.seq < oldest.seq) {
				oldest = img
			}
		}
		s.DeleteImage(oldest.ID)
	}
}
