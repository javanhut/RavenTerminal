package images

import "testing"

func TestStoreAddGet(t *testing.T) {
	s := NewStore(0)
	img := s.Add(0, 2, 2, make([]byte, 16))
	if img.ID == 0 {
		t.Fatal("expected auto-assigned id")
	}
	got, ok := s.Get(img.ID)
	if !ok || got != img {
		t.Fatal("Get did not return the stored image")
	}
}

func TestStoreEviction(t *testing.T) {
	s := NewStore(16) // room for ~1 image of 16 bytes
	a := s.Add(0, 2, 2, make([]byte, 16))
	s.Add(0, 2, 2, make([]byte, 16)) // exceeds budget -> evict oldest (a)
	if _, ok := s.Get(a.ID); ok {
		t.Fatal("expected oldest image to be evicted")
	}
}

func TestStoreDeleteImageDropsPlacements(t *testing.T) {
	s := NewStore(0)
	img := s.Add(0, 1, 1, make([]byte, 4))
	s.Place(&Placement{ImageID: img.ID, Rows: 1, Cols: 1})
	s.Place(&Placement{ImageID: 999, Rows: 1, Cols: 1})
	s.DeleteImage(img.ID)
	if len(s.Placements()) != 1 {
		t.Fatalf("placements = %d, want 1 (only the unrelated one kept)", len(s.Placements()))
	}
}

func TestStorePruneBelow(t *testing.T) {
	s := NewStore(0)
	s.Place(&Placement{AnchorAbsRow: 0, Rows: 2})  // occupies rows 0-1
	s.Place(&Placement{AnchorAbsRow: 10, Rows: 2}) // rows 10-11
	s.PrunePlacementsBelow(5)                      // drop anything fully below row 5
	if len(s.Placements()) != 1 {
		t.Fatalf("placements = %d, want 1 after prune", len(s.Placements()))
	}
}
