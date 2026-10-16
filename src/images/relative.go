package images

import "slices"

// MaxChainDepth caps chains of relative placements (kitty P/Q).
const MaxChainDepth = 8

// findReal returns the real placement with the given image and placement id.
func (s *Store) findReal(id ImageID, pid uint32) *Placement {
	for _, p := range s.placements {
		if p.ImageID == id && p.PlacementID == pid {
			return p
		}
	}
	return nil
}

// PlaceRelative records a placement positioned relative to its parent
// (ParentImage/ParentPlacement) offset by RelCol/RelRow cells. It fails with
// ENOPARENT when the parent is not a real placement, ECYCLE when the chain
// would loop back to p, and ETOODEEP past MaxChainDepth.
func (s *Store) PlaceRelative(p *Placement) error {
	parent := s.findReal(p.ParentImage, p.ParentPlacement)
	if parent == nil {
		return errorf("ENOPARENT", "parent placement not found")
	}
	depth := 1
	for q := parent; q != nil; {
		if q.ImageID == p.ImageID && q.PlacementID == p.PlacementID {
			return errorf("ECYCLE", "relative placement chain forms a cycle")
		}
		if q.ParentImage == 0 {
			break
		}
		if depth++; depth > MaxChainDepth {
			return errorf("ETOODEEP", "relative placement chain too deep")
		}
		q = s.findReal(q.ParentImage, q.ParentPlacement)
	}
	p.AnchorCol = parent.AnchorCol + p.RelCol
	p.AnchorAbsRow = parent.AnchorAbsRow + p.RelRow
	s.relative = true
	s.Place(p)
	return nil
}

// reposition moves the descendants of parent to follow it. Moved children
// are copies: placements already handed out are never mutated.
func (s *Store) reposition(parent *Placement) {
	list := s.placements
	cloned := false
	queue := []*Placement{parent}
	for steps := 0; len(queue) > 0 && steps <= len(list); steps++ {
		par := queue[0]
		queue = queue[1:]
		for i, c := range list {
			if c.ParentImage == 0 || c == par || c.ParentImage != par.ImageID || c.ParentPlacement != par.PlacementID {
				continue
			}
			if !cloned {
				list, cloned = slices.Clone(list), true
			}
			nc := *c
			nc.AnchorCol = par.AnchorCol + nc.RelCol
			nc.AnchorAbsRow = par.AnchorAbsRow + nc.RelRow
			list[i] = &nc
			queue = append(queue, &nc)
		}
	}
	s.placements = list
}

// orphans returns the relative placements whose parent is gone.
func (s *Store) orphans() func(*Placement) bool {
	if !s.relative {
		return func(*Placement) bool { return false }
	}
	return func(p *Placement) bool {
		return p.ParentImage != 0 && s.findReal(p.ParentImage, p.ParentPlacement) == nil
	}
}
