package images

import "time"

// Kitty graphics animation. Frame 1 is the image's own pixels; later frames
// are full canvases the size of the image. Frame pixels are never mutated
// once stored (edits replace the slice), so a frame handed to the renderer
// stays valid outside the terminal lock.

// Error is a kitty graphics protocol error: Code is the reply's error code.
type Error struct{ Code, Msg string }

func (e *Error) Error() string { return e.Code + ":" + e.Msg }

func errorf(code, msg string) error { return &Error{code, msg} }

// AnimState is an animation's playback state (a=a s=).
type AnimState int

const (
	AnimStopped AnimState = 1 // s=1
	AnimLoading AnimState = 2 // s=2: wait for more frames at the end
	AnimRunning AnimState = 3 // s=3: loop
)

const (
	defaultGap = 40 // ms, for frames sent without z
	maxFrames  = 1024
)

type frame struct {
	pixels []byte
	gap    int  // ms; 0 = gapless
	root   bool // pixels are the image's own (counted with it)
	view   *Image
}

type animation struct {
	frames    []*frame // frames[0] is the root frame
	state     AnimState
	loops     int // v: 1 = forever, n = n-1 loops back to the start
	loopsDone int
	current   int // 0-based
	shownAt   time.Time
	gen       uint64 // bumped whenever the displayed frame may change
}

// FrameData is one a=f transmission: W x H pixels placed at (X, Y).
type FrameData struct {
	Pixels    []byte
	W, H      int
	X, Y      int
	Base      int    // c: 1-based frame to start from (0 = Bg colour)
	Edit      int    // r: 1-based frame to edit (0 = new frame)
	Gap       int    // z: ms, 0 = default, <0 = gapless
	Overwrite bool   // X=1: replace pixels instead of alpha blending
	Bg        uint32 // Y: RGBA background for a new frame without a base
}

// AnimControl is an a=a command. Zero fields leave things unchanged.
type AnimControl struct {
	State    AnimState // s
	Loops    int       // v
	Current  int       // c: 1-based frame to show
	GapFrame int       // r: frame whose gap z sets
	Gap      int       // z
}

// Compose is an a=c command: copy a W x H rect at (X, Y) of frame Src onto
// frame Dst at (DstX, DstY). W/H 0 mean "to the edge".
type Compose struct {
	Src, Dst   int
	X, Y, W, H int
	DstX, DstY int
	Overwrite  bool // C=1
}

func (s *Store) anim(img *Image) *animation {
	if img.anim == nil {
		img.anim = &animation{
			frames: []*frame{{pixels: img.Pixels, gap: defaultGap, root: true, view: img}},
			state:  AnimStopped,
		}
	}
	return img.anim
}

func (s *Store) image(id ImageID) (*Image, error) {
	img, ok := s.images[id]
	if !ok {
		return nil, errorf("ENOENT", "image not found")
	}
	return img, nil
}

func (a *animation) frameAt(n int) (*frame, error) {
	if n < 1 || n > len(a.frames) {
		return nil, errorf("ENOENT", "no such frame")
	}
	return a.frames[n-1], nil
}

func gapOf(z int) int {
	switch {
	case z < 0:
		return 0
	case z == 0:
		return defaultGap
	}
	return z
}

// LoadFrame implements a=f: it creates a frame (or edits frame Edit) and
// returns its 1-based number.
func (s *Store) LoadFrame(id ImageID, fd FrameData) (int, error) {
	img, err := s.image(id)
	if err != nil {
		return 0, err
	}
	if fd.X < 0 || fd.Y < 0 || fd.W <= 0 || fd.H <= 0 || fd.X+fd.W > img.Width || fd.Y+fd.H > img.Height ||
		len(fd.Pixels) < 4*fd.W*fd.H {
		return 0, errorf("EINVAL", "frame data out of bounds")
	}
	a := s.anim(img)
	if fd.Edit == 0 && len(a.frames) >= maxFrames {
		return 0, errorf("EINVAL", "too many frames")
	}
	canvas := make([]byte, 4*img.Width*img.Height)
	var old *frame
	switch {
	case fd.Edit > 0:
		if old, err = a.frameAt(fd.Edit); err != nil {
			return 0, err
		}
		copy(canvas, old.pixels)
	case fd.Base > 0:
		base, err := a.frameAt(fd.Base)
		if err != nil {
			return 0, err
		}
		copy(canvas, base.pixels)
	default:
		bg := [4]byte{byte(fd.Bg >> 24), byte(fd.Bg >> 16), byte(fd.Bg >> 8), byte(fd.Bg)}
		if bg != [4]byte{} {
			for i := 0; i < len(canvas); i += 4 {
				copy(canvas[i:], bg[:])
			}
		}
	}
	blit(canvas, img.Width, fd.Pixels, fd.W, 0, 0, fd.W, fd.H, fd.X, fd.Y, !fd.Overwrite)
	if old != nil {
		n := fd.Edit
		gap := old.gap
		if fd.Gap != 0 {
			gap = gapOf(fd.Gap)
		}
		s.replaceFrame(img, n, &frame{pixels: canvas, gap: gap})
		return n, nil
	}
	a.frames = append(a.frames, &frame{pixels: canvas, gap: gapOf(fd.Gap)})
	img.extraBytes += len(canvas)
	s.totalBytes += len(canvas)
	s.evictIfNeeded(id)
	return len(a.frames), nil
}

// replaceFrame swaps frame n (1-based) for f, keeping the byte budget.
func (s *Store) replaceFrame(img *Image, n int, f *frame) {
	a := img.anim
	old := a.frames[n-1]
	if !old.root {
		img.extraBytes -= len(old.pixels)
		s.totalBytes -= len(old.pixels)
	}
	a.frames[n-1] = f
	a.gen++
	img.extraBytes += len(f.pixels)
	s.totalBytes += len(f.pixels)
}

// ComposeFrames implements a=c.
func (s *Store) ComposeFrames(id ImageID, c Compose) error {
	img, err := s.image(id)
	if err != nil {
		return err
	}
	a := s.anim(img)
	src, err := a.frameAt(c.Src)
	if err != nil {
		return err
	}
	dst, err := a.frameAt(c.Dst)
	if err != nil {
		return err
	}
	w, h := c.W, c.H
	if w == 0 {
		w = img.Width - c.X
	}
	if h == 0 {
		h = img.Height - c.Y
	}
	if c.X < 0 || c.Y < 0 || c.DstX < 0 || c.DstY < 0 || w <= 0 || h <= 0 ||
		c.X+w > img.Width || c.Y+h > img.Height || c.DstX+w > img.Width || c.DstY+h > img.Height {
		return errorf("EINVAL", "rectangle out of bounds")
	}
	if c.Src == c.Dst && c.X < c.DstX+w && c.DstX < c.X+w && c.Y < c.DstY+h && c.DstY < c.Y+h {
		return errorf("EINVAL", "source and destination overlap")
	}
	canvas := make([]byte, len(dst.pixels))
	copy(canvas, dst.pixels)
	blit(canvas, img.Width, src.pixels, img.Width, c.X, c.Y, w, h, c.DstX, c.DstY, !c.Overwrite)
	s.replaceFrame(img, c.Dst, &frame{pixels: canvas, gap: dst.gap})
	return nil
}

// blit copies a w x h rect at (sx, sy) of src (row stride srcW pixels) onto
// dst (stride dstW) at (dx, dy), alpha blending straight RGBA when blend.
func blit(dst []byte, dstW int, src []byte, srcW, sx, sy, w, h, dx, dy int, blend bool) {
	for y := range h {
		si := 4 * ((sy+y)*srcW + sx)
		di := 4 * ((dy+y)*dstW + dx)
		if !blend {
			copy(dst[di:di+4*w], src[si:si+4*w])
			continue
		}
		for x := 0; x < 4*w; x += 4 {
			s, d := src[si+x:si+x+4], dst[di+x:di+x+4]
			sa := uint32(s[3])
			if sa == 255 {
				copy(d, s)
				continue
			}
			if sa == 0 {
				continue
			}
			da := uint32(d[3]) * (255 - sa) / 255
			oa := sa + da
			for k := range 3 {
				d[k] = byte((uint32(s[k])*sa + uint32(d[k])*da) / oa)
			}
			d[3] = byte(oa)
		}
	}
}

// ControlAnimation implements a=a.
func (s *Store) ControlAnimation(id ImageID, c AnimControl, now time.Time) error {
	img, err := s.image(id)
	if err != nil {
		return err
	}
	a := s.anim(img)
	if c.GapFrame > 0 && c.Gap != 0 {
		f, err := a.frameAt(c.GapFrame)
		if err != nil {
			return err
		}
		f.gap = gapOf(c.Gap)
	}
	if c.Current > 0 {
		if _, err := a.frameAt(c.Current); err != nil {
			return err
		}
		a.current, a.shownAt = c.Current-1, now
		a.gen++
	}
	if c.Loops > 0 {
		a.loops, a.loopsDone = c.Loops, 0
	}
	if c.State >= AnimStopped && c.State <= AnimRunning {
		if a.state == AnimStopped && c.State != AnimStopped {
			a.loopsDone, a.shownAt = 0, now
		}
		a.state = c.State
	}
	return nil
}

// advance moves the animation to the frame showing at now and returns when
// the next frame is due (zero when nothing is scheduled).
func (a *animation) advance(now time.Time) time.Time {
	if a.state == AnimStopped || len(a.frames) < 2 {
		return time.Time{}
	}
	moving := false
	for _, f := range a.frames {
		moving = moving || f.gap > 0
	}
	if !moving {
		return time.Time{}
	}
	if a.shownAt.IsZero() {
		a.shownAt = now
	}
	for range 4 * len(a.frames) {
		end := a.shownAt.Add(time.Duration(a.frames[a.current].gap) * time.Millisecond)
		if now.Before(end) {
			return end
		}
		a.gen++
		if a.current == len(a.frames)-1 {
			if a.state == AnimLoading {
				return time.Time{}
			}
			if a.loops > 1 {
				if a.loopsDone >= a.loops-1 {
					a.state = AnimStopped
					return time.Time{}
				}
				a.loopsDone++
			}
			a.current = 0
		} else {
			a.current++
		}
		a.shownAt = end
	}
	// Far behind (the window slept): resume from now.
	a.shownAt = now
	return now.Add(time.Duration(max(a.frames[a.current].gap, 1)) * time.Millisecond)
}

// Tick advances every animation to now and returns the earliest time a placed
// animated image needs its next frame (zero if none).
func (s *Store) Tick(now time.Time) time.Time {
	var next time.Time
	var placed map[ImageID]bool
	for id, img := range s.images {
		if img.anim == nil || img.anim.state == AnimStopped {
			continue
		}
		if placed == nil {
			placed = map[ImageID]bool{}
			for _, p := range s.placements {
				placed[p.ImageID] = true
			}
			for _, p := range s.virtuals {
				placed[p.ImageID] = true
			}
		}
		if !placed[id] {
			continue
		}
		if t := img.anim.advance(now); !t.IsZero() && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	return next
}

// AnimGeneration changes whenever the frame displayed for some image may
// have changed, so callers can tell when to redraw.
func (s *Store) AnimGeneration() uint64 {
	var g uint64
	for _, img := range s.images {
		if img.anim != nil {
			g += img.anim.gen
		}
	}
	return g
}

// FrameCount returns how many frames an image has (0 if it does not exist).
func (s *Store) FrameCount(id ImageID) int {
	img, ok := s.images[id]
	if !ok {
		return 0
	}
	if img.anim == nil {
		return 1
	}
	return len(img.anim.frames)
}

// CurrentFrame returns the 1-based frame an image is showing.
func (s *Store) CurrentFrame(id ImageID) int {
	img, ok := s.images[id]
	if !ok {
		return 0
	}
	if img.anim == nil {
		return 1
	}
	return img.anim.current + 1
}

// FrameImage returns frame n (1-based) of an image as an Image, or nil.
func (s *Store) FrameImage(id ImageID, n int) *Image {
	img, ok := s.images[id]
	if !ok {
		return nil
	}
	if img.anim == nil {
		if n == 1 {
			return img
		}
		return nil
	}
	f, err := img.anim.frameAt(n)
	if err != nil {
		return nil
	}
	if f.view == nil {
		f.view = &Image{ID: img.ID, Number: img.Number, Width: img.Width, Height: img.Height, Pixels: f.pixels}
	}
	return f.view
}

// Display returns the image as it should be drawn now: its current frame.
func (s *Store) Display(id ImageID) *Image {
	return s.FrameImage(id, s.CurrentFrame(id))
}
