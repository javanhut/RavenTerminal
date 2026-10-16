package images

import (
	"testing"
	"time"
)

func TestImageCountCapped(t *testing.T) {
	s := NewStore(1 << 30)
	for range MaxImages * 2 {
		s.Add(0, 1, 1, make([]byte, 4))
	}
	if n := len(s.ImageIDs()); n > MaxImages {
		t.Fatalf("%d images stored, cap %d", n, MaxImages)
	}
}

func TestAddAccountsCapacity(t *testing.T) {
	s := NewStore(1 << 20)
	big := make([]byte, 1<<20)
	for range 10 {
		s.Add(0, 1, 1, big[:4])
	}
	if s.totalBytes > 1<<20 {
		t.Fatalf("budget %d exceeded: retained backing arrays not counted", s.totalBytes)
	}
}

func TestPlacementsCappedAndLinear(t *testing.T) {
	s := NewStore(1 << 20)
	s.Add(1, 1, 1, make([]byte, 4))
	start := time.Now()
	for range 50000 {
		s.Place(&Placement{ImageID: 1, Cols: 1, Rows: 1})
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("50k placements took %v", d)
	}
	if n := len(s.Placements()); n > MaxPlacements {
		t.Fatalf("%d placements, cap %d", n, MaxPlacements)
	}
	for range 50000 {
		s.Place(&Placement{ImageID: 1, Virtual: true})
	}
	if n := len(s.VirtualPlacements()); n > MaxPlacements {
		t.Fatalf("%d virtual placements, cap %d", n, MaxPlacements)
	}
}

func TestPlaceDoesNotMutateHandedOutSlice(t *testing.T) {
	s := NewStore(1 << 20)
	s.Place(&Placement{ImageID: 1, PlacementID: 1})
	s.Place(&Placement{ImageID: 2, PlacementID: 1})
	held := s.Placements()
	snap := append([]*Placement(nil), held...)
	s.Place(&Placement{ImageID: 1, PlacementID: 1, Cols: 9})
	s.Place(&Placement{ImageID: 3})
	for i := range held {
		if held[i] != snap[i] {
			t.Fatal("slice handed to renderer was mutated")
		}
	}
	if n := len(s.Placements()); n != 3 {
		t.Fatalf("placements %d, want 3", n)
	}
}

func TestAddInternalIDsOutsideClientRange(t *testing.T) {
	s := NewStore(1 << 20)
	a := s.AddInternal(1, 1, make([]byte, 4))
	b := s.AddInternal(1, 1, make([]byte, 4))
	if a.ID <= 0xffffffff || b.ID == a.ID {
		t.Fatalf("internal ids %d %d", a.ID, b.ID)
	}
	c := s.Add(0, 1, 1, make([]byte, 4))
	if c.ID > 0xffffffff {
		t.Fatalf("client auto id %d left uint32 range", c.ID)
	}
}
