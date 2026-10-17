package images

import "testing"

func find(s *Store, id ImageID, pid uint32) *Placement {
	for _, p := range s.Placements() {
		if p.ImageID == id && p.PlacementID == pid {
			return p
		}
	}
	return nil
}

func TestRelativePlacementChain(t *testing.T) {
	s := NewStore(0)
	s.Place(&Placement{ImageID: 1, PlacementID: 1, AnchorAbsRow: 10, AnchorCol: 5, Cols: 2, Rows: 2})
	if err := s.PlaceRelative(&Placement{ImageID: 2, PlacementID: 1, ParentImage: 1, ParentPlacement: 1, RelCol: 3, RelRow: -2, Cols: 1, Rows: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceRelative(&Placement{ImageID: 3, PlacementID: 7, ParentImage: 2, ParentPlacement: 1, RelCol: -1, RelRow: 1, Cols: 1, Rows: 1}); err != nil {
		t.Fatal(err)
	}
	c, g := find(s, 2, 1), find(s, 3, 7)
	if c.AnchorCol != 8 || c.AnchorAbsRow != 8 || g.AnchorCol != 7 || g.AnchorAbsRow != 9 {
		t.Fatalf("child (%d,%d) grandchild (%d,%d)", c.AnchorCol, c.AnchorAbsRow, g.AnchorCol, g.AnchorAbsRow)
	}
	// Moving the parent moves the whole chain, without mutating handed-out placements.
	old := s.Placements()
	s.Place(&Placement{ImageID: 1, PlacementID: 1, AnchorAbsRow: 20, AnchorCol: 0, Cols: 2, Rows: 2})
	c, g = find(s, 2, 1), find(s, 3, 7)
	if c.AnchorCol != 3 || c.AnchorAbsRow != 18 || g.AnchorCol != 2 || g.AnchorAbsRow != 19 {
		t.Fatalf("after move: child (%d,%d) grandchild (%d,%d)", c.AnchorCol, c.AnchorAbsRow, g.AnchorCol, g.AnchorAbsRow)
	}
	for _, p := range old {
		if p.ImageID == 2 && p.AnchorCol != 8 {
			t.Fatal("a handed-out placement was mutated")
		}
	}
}

func TestRelativePlacementErrors(t *testing.T) {
	s := NewStore(0)
	err := s.PlaceRelative(&Placement{ImageID: 2, PlacementID: 1, ParentImage: 1, ParentPlacement: 1, Cols: 1, Rows: 1})
	if errCode(err) != "ENOPARENT" {
		t.Fatalf("missing parent: %v", err)
	}
	s.Place(&Placement{ImageID: 1, PlacementID: 1, Cols: 1, Rows: 1})
	s.PlaceRelative(&Placement{ImageID: 2, PlacementID: 1, ParentImage: 1, ParentPlacement: 1, Cols: 1, Rows: 1})
	// Re-placing 1/1 relative to its own child would close a loop.
	if err := s.PlaceRelative(&Placement{ImageID: 1, PlacementID: 1, ParentImage: 2, ParentPlacement: 1, Cols: 1, Rows: 1}); errCode(err) != "ECYCLE" {
		t.Fatalf("cycle: %v", err)
	}
	if err := s.PlaceRelative(&Placement{ImageID: 2, PlacementID: 1, ParentImage: 2, ParentPlacement: 1, Cols: 1, Rows: 1}); errCode(err) != "ECYCLE" {
		t.Fatalf("self parent: %v", err)
	}
	if find(s, 1, 1).ParentImage != 0 {
		t.Fatal("a rejected placement replaced the old one")
	}
}

// The kitty spec allows a virtual placement as a parent; the chain is then
// positioned from the root's placeholder cells, which the store can't see.
func TestRelativePlacementVirtualParent(t *testing.T) {
	s := NewStore(0)
	s.Place(&Placement{ImageID: 9, PlacementID: 1, Cols: 4, Rows: 2, Virtual: true})
	if err := s.PlaceRelative(&Placement{ImageID: 3, PlacementID: 1, ParentImage: 9, ParentPlacement: 1, RelCol: 2, RelRow: -1, Cols: 1, Rows: 1}); err != nil {
		t.Fatalf("virtual parent: %v", err)
	}
	if err := s.PlaceRelative(&Placement{ImageID: 4, PlacementID: 1, ParentImage: 3, ParentPlacement: 1, RelCol: 1, RelRow: 3, Cols: 1, Rows: 1}); err != nil {
		t.Fatalf("grandchild: %v", err)
	}
	child, grand := find(s, 3, 1), find(s, 4, 1)
	if child == nil || grand == nil || !child.VirtualRoot || !grand.VirtualRoot {
		t.Fatalf("child %+v grandchild %+v: want both VirtualRoot", child, grand)
	}

	origin := func(v *Placement) (int, int, bool) {
		if v.ImageID != 9 {
			t.Fatalf("origin asked for %+v", v)
		}
		return 10, 100, true
	}
	if c, r, ok := VirtualAnchor(child, s.Placements(), s.VirtualPlacements(), origin); !ok || c != 12 || r != 99 {
		t.Fatalf("child anchor (%d,%d,%v), want (12,99)", c, r, ok)
	}
	if c, r, ok := VirtualAnchor(grand, s.Placements(), s.VirtualPlacements(), origin); !ok || c != 13 || r != 102 {
		t.Fatalf("grandchild anchor (%d,%d,%v), want (13,102)", c, r, ok)
	}
	offscreen := func(*Placement) (int, int, bool) { return 0, 0, false }
	if _, _, ok := VirtualAnchor(child, s.Placements(), s.VirtualPlacements(), offscreen); ok {
		t.Fatal("resolved with no placeholder on screen")
	}

	// The stored anchor means nothing for these, so scrollback pruning
	// must not drop them.
	s.PrunePlacementsBelow(1 << 30)
	if find(s, 3, 1) == nil || find(s, 4, 1) == nil {
		t.Fatal("pruned a placement anchored to a virtual root")
	}

	// Deleting the virtual root takes the whole chain with it.
	s.DeletePlacements(func(p *Placement) bool { return p.ImageID == 9 }, false)
	if len(s.Placements()) != 0 {
		t.Fatalf("orphans left: %+v", s.Placements())
	}
}

func TestRelativePlacementDepthLimit(t *testing.T) {
	s := NewStore(0)
	s.Place(&Placement{ImageID: 1, PlacementID: 1, Cols: 1, Rows: 1})
	for i := 2; i <= MaxChainDepth+1; i++ {
		if err := s.PlaceRelative(&Placement{ImageID: ImageID(i), PlacementID: 1, ParentImage: ImageID(i - 1), ParentPlacement: 1, Cols: 1, Rows: 1}); err != nil {
			t.Fatalf("depth %d: %v", i-1, err)
		}
	}
	err := s.PlaceRelative(&Placement{ImageID: 100, PlacementID: 1, ParentImage: MaxChainDepth + 1, ParentPlacement: 1, Cols: 1, Rows: 1})
	if errCode(err) != "ETOODEEP" {
		t.Fatalf("too deep: %v", err)
	}
}

func TestRelativePlacementsDeletedWithParent(t *testing.T) {
	s := NewStore(0)
	s.Add(1, 1, 1, make([]byte, 4))
	s.Add(2, 1, 1, make([]byte, 4))
	s.Place(&Placement{ImageID: 1, PlacementID: 1, AnchorAbsRow: 5, Cols: 1, Rows: 1})
	s.PlaceRelative(&Placement{ImageID: 2, PlacementID: 1, ParentImage: 1, ParentPlacement: 1, Cols: 1, Rows: 1})
	s.PlaceRelative(&Placement{ImageID: 3, PlacementID: 1, ParentImage: 2, ParentPlacement: 1, Cols: 1, Rows: 1})
	s.Place(&Placement{ImageID: 4, PlacementID: 1, Cols: 1, Rows: 1})
	s.DeletePlacements(func(p *Placement) bool { return p.ImageID == 1 }, true)
	if len(s.Placements()) != 1 || s.Placements()[0].ImageID != 4 {
		t.Fatalf("placements after parent delete = %+v", s.Placements())
	}
	if _, ok := s.Get(2); ok {
		t.Fatal("a child's image freed by the cascade was kept")
	}
	s.Place(&Placement{ImageID: 1, PlacementID: 1, AnchorAbsRow: 5, Cols: 1, Rows: 1})
	s.PlaceRelative(&Placement{ImageID: 5, PlacementID: 1, ParentImage: 1, ParentPlacement: 1, RelRow: 50, Cols: 1, Rows: 1})
	s.PrunePlacementsBelow(10) // the parent scrolls out; its child goes too
	if find(s, 5, 1) != nil {
		t.Fatal("pruning the parent kept the child")
	}
	s.Place(&Placement{ImageID: 1, PlacementID: 1, Cols: 1, Rows: 1})
	s.PlaceRelative(&Placement{ImageID: 5, PlacementID: 1, ParentImage: 1, ParentPlacement: 1, Cols: 1, Rows: 1})
	s.DeleteImage(1)
	if find(s, 5, 1) != nil {
		t.Fatal("deleting the parent image kept the child")
	}
}
