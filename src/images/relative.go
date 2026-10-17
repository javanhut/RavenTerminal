package images

import "slices"

// MaxChainDepth caps chains of relative placements (kitty P/Q).
const MaxChainDepth = 8

// findIn returns the placement in list with the given image and placement id.
func findIn(list []*Placement, id ImageID, pid uint32) *Placement {
	for _, p := range list {
		if p.ImageID == id && p.PlacementID == pid {
			return p
		}
	}
	return nil
}

// findParent returns the placement a relative placement may hang from: a
// real one or, per the kitty spec, a virtual (Unicode placeholder) one.
func (s *Store) findParent(id ImageID, pid uint32) *Placement {
	if p := findIn(s.placements, id, pid); p != nil {
		return p
	}
	return findIn(s.virtuals, id, pid)
}

// PlaceRelative records a placement positioned relative to its parent
// (ParentImage/ParentPlacement) offset by RelCol/RelRow cells. It fails with
// ENOPARENT when the parent placement doesn't exist, ECYCLE when the chain
// would loop back to p, and ETOODEEP past MaxChainDepth. A parent may be
// virtual; p is then positioned at draw time (VirtualRoot).
func (s *Store) PlaceRelative(p *Placement) error {
	parent := s.findParent(p.ParentImage, p.ParentPlacement)
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
		q = s.findParent(q.ParentImage, q.ParentPlacement)
	}
	p.AnchorCol = parent.AnchorCol + p.RelCol
	p.AnchorAbsRow = parent.AnchorAbsRow + p.RelRow
	p.VirtualRoot = parent.Virtual || parent.VirtualRoot
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
			nc.VirtualRoot = par.Virtual || par.VirtualRoot
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
		return p.ParentImage != 0 && s.findParent(p.ParentImage, p.ParentPlacement) == nil
	}
}

// VirtualAnchor resolves where a VirtualRoot placement sits: the RelCol/RelRow
// offsets along its chain, summed onto origin's position for the virtual
// root. Per the kitty spec that position is the minimum column and minimum
// row of the placeholder cells showing the root, which only the renderer
// sees. ok is false when the chain is broken or origin doesn't know the root
// (none of its placeholders is on screen); such a placement isn't drawn.
func VirtualAnchor(p *Placement, real, virtuals []*Placement, origin func(*Placement) (col, absRow int, ok bool)) (col, absRow int, ok bool) {
	col, absRow = p.RelCol, p.RelRow
	q := p
	for range MaxChainDepth + 1 {
		// Same precedence as findParent: a real placement wins.
		if parent := findIn(real, q.ParentImage, q.ParentPlacement); parent != nil {
			if parent.ParentImage == 0 {
				return 0, 0, false // rooted at a real placement after all
			}
			col, absRow = col+parent.RelCol, absRow+parent.RelRow
			q = parent
			continue
		}
		v := findIn(virtuals, q.ParentImage, q.ParentPlacement)
		if v == nil {
			return 0, 0, false
		}
		oc, or, ok := origin(v)
		return oc + col, or + absRow, ok
	}
	return 0, 0, false
}
