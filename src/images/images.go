// Package images defines a protocol-neutral model for inline terminal images
// (Kitty graphics and Sixel) plus a small store. Decoded pixels live in Image;
// each on-screen instance is a Placement anchored to an absolute scrollback row
// so it scrolls with the content using the same arithmetic as the grid.
package images

// ImageID identifies a stored image.
type ImageID uint64

// Image is an immutable RGBA pixel payload.
type Image struct {
	ID     ImageID
	Width  int    // pixels
	Height int    // pixels
	Pixels []byte // RGBA, len == 4*Width*Height
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
}

// Store holds images and their placements for one screen (main or alt).
type Store struct {
	images     map[ImageID]*Image
	placements []*Placement
	nextID     ImageID
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
		id = s.nextID
	} else if id >= s.nextID {
		s.nextID = id
	}
	if old, ok := s.images[id]; ok {
		s.totalBytes -= len(old.Pixels)
	}
	img := &Image{ID: id, Width: width, Height: height, Pixels: pixels}
	s.images[id] = img
	s.totalBytes += len(pixels)
	s.evictIfNeeded()
	return img
}

// Get returns a stored image by id.
func (s *Store) Get(id ImageID) (*Image, bool) {
	img, ok := s.images[id]
	return img, ok
}

// Place records a placement.
func (s *Store) Place(p *Placement) { s.placements = append(s.placements, p) }

// Placements returns the current placements (caller must not mutate).
func (s *Store) Placements() []*Placement { return s.placements }

// DeleteAll removes all images and placements.
func (s *Store) DeleteAll() {
	s.images = make(map[ImageID]*Image)
	s.placements = nil
	s.totalBytes = 0
}

// DeleteImage removes an image and any placements referencing it.
func (s *Store) DeleteImage(id ImageID) {
	if img, ok := s.images[id]; ok {
		s.totalBytes -= len(img.Pixels)
		delete(s.images, id)
	}
	s.dropPlacements(func(p *Placement) bool { return p.ImageID == id })
}

// ClearPlacements removes all placements but keeps stored images.
func (s *Store) ClearPlacements() { s.placements = nil }

// PrunePlacementsBelow removes placements whose anchor scrolled out of history
// (absolute row below the given trimmed threshold).
func (s *Store) PrunePlacementsBelow(minAbsRow int) {
	s.dropPlacements(func(p *Placement) bool { return p.AnchorAbsRow+p.Rows <= minAbsRow })
}

func (s *Store) dropPlacements(remove func(*Placement) bool) {
	kept := s.placements[:0]
	for _, p := range s.placements {
		if !remove(p) {
			kept = append(kept, p)
		}
	}
	s.placements = kept
}

func (s *Store) evictIfNeeded() {
	if s.maxBytes <= 0 {
		return
	}
	// Evict oldest images (lowest id) until within budget.
	for s.totalBytes > s.maxBytes && len(s.images) > 1 {
		var oldest ImageID
		first := true
		for id := range s.images {
			if first || id < oldest {
				oldest = id
				first = false
			}
		}
		s.DeleteImage(oldest)
	}
}
