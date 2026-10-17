package parser

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/javanhut/RavenTerminal/src/images"
)

const (
	maxKittyData  = 128 << 20 // assembled / decompressed / file payload cap
	maxKittyPixel = 10000     // per-dimension cap
	maxKittyArea  = 4096 * 4096
	maxKittyCells = 10000 // per-dimension placement size cap
)

// kittyTransmit accumulates a chunked Kitty transmission (m=1 chunks). All
// controls come from the first chunk; later chunks only carry m (and q).
type kittyTransmit struct {
	ctrl map[string]string
	buf  bytes.Buffer
}

type kittyError struct{ code, msg string }

func (e *kittyError) Error() string { return e.code + ":" + e.msg }

func kerr(code, msg string) error { return &kittyError{code, msg} }

// processAPC accumulates APC payload bytes until a terminator (ST/BEL).
func (t *Terminal) processAPC(b byte) {
	switch b {
	case 0x1b: // ESC — possible start of ST
		t.state = StateAPCEscape
	case 0x07: // BEL. 0x9c is NOT a terminator in UTF-8 mode (see processOSC).
		t.handleAPC(t.apcBuf)
		t.state = StateGround
	case 0x18, 0x1a: // CAN/SUB abort
		t.apcBuf = t.apcBuf[:0]
		t.state = StateGround
	default:
		if len(t.apcBuf) < maxAPCLen {
			t.apcBuf = append(t.apcBuf, b)
		}
	}
}

// processAPCEscape handles the byte after ESC inside an APC string.
func (t *Terminal) processAPCEscape(b byte) {
	if b == '\\' { // ST (ESC \)
		t.handleAPC(t.apcBuf)
		t.state = StateGround
		return
	}
	// Not ST: the ESC aborts the string and starts a new sequence.
	t.apcBuf = t.apcBuf[:0]
	t.state = StateEscape
	t.processEscape(b)
}

// activeImages returns the image store for the current screen.
func (t *Terminal) activeImages() *images.Store {
	if t.alternateScreen {
		return t.altImages
	}
	return t.images
}

// handleAPC dispatches a Kitty graphics APC command ("G<controls>;<payload>").
func (t *Terminal) handleAPC(buf []byte) {
	if len(buf) == 0 || len(buf) >= maxAPCLen || buf[0] != 'G' {
		return
	}
	ctrlPart, payload, _ := bytes.Cut(buf[1:], []byte{';'})
	ctrl := parseKittyControls(string(ctrlPart))
	data := decodeB64(payload)

	if tr := t.pendingKitty; tr != nil {
		if tr.buf.Len()+len(data) > maxKittyData {
			t.pendingKitty = nil
			t.kittyReply(tr.ctrl, 0, kerr("EFBIG", "image data too large"))
			return
		}
		tr.buf.Write(data)
		if ctrl["m"] != "1" {
			t.pendingKitty = nil
			t.kittyFinishTransmit(tr.ctrl, tr.buf.Bytes())
		}
		return
	}

	switch ctrl["a"] {
	case "", "t", "T", "q", "f":
		if ctrl["m"] == "1" {
			t.pendingKitty = &kittyTransmit{ctrl: ctrl}
			t.pendingKitty.buf.Write(data)
			return
		}
		t.kittyFinishTransmit(ctrl, data)
	case "p":
		t.kittyPlace(ctrl)
	case "d":
		t.kittyDelete(ctrl)
	case "a":
		t.kittyAnimate(ctrl)
	case "c":
		t.kittyCompose(ctrl)
	default:
		t.kittyReply(ctrl, images.ImageID(ctrlInt(ctrl, "i", 0)), kerr("EINVAL", "unsupported action"))
	}
}

func decodeB64(p []byte) []byte {
	s := strings.TrimSpace(string(p))
	if out, err := base64.StdEncoding.DecodeString(s); err == nil {
		return out
	}
	out, _ := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
	return out
}

// parseKittyControls parses comma-separated key=value control pairs.
func parseKittyControls(s string) map[string]string {
	m := make(map[string]string)
	for kv := range strings.SplitSeq(s, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			m[k] = v
		}
	}
	return m
}

func ctrlInt(m map[string]string, key string, def int) int {
	if v, ok := m[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// kittyFinishTransmit loads, decodes and stores (or for a=q only validates)
// a complete transmission.
func (t *Terminal) kittyFinishTransmit(ctrl map[string]string, data []byte) {
	if ctrl["a"] == "f" {
		t.kittyFrame(ctrl, data)
		return
	}
	img, err := loadKittyImage(ctrl, data)
	if ctrl["a"] == "q" {
		t.kittyReply(ctrl, images.ImageID(ctrlInt(ctrl, "i", 0)), err)
		return
	}
	if err != nil {
		t.kittyReply(ctrl, images.ImageID(ctrlInt(ctrl, "i", 0)), err)
		return
	}
	store := t.activeImages()
	stored := store.Add(images.ImageID(ctrlInt(ctrl, "i", 0)), img.Width, img.Height, img.Pixels)
	stored.Number = uint32(ctrlInt(ctrl, "I", 0))
	if ctrl["a"] == "T" {
		err = t.placeImage(stored, ctrl)
	}
	t.kittyReply(ctrl, stored.ID, err)
}

func loadKittyImage(ctrl map[string]string, data []byte) (*images.Image, error) {
	switch medium := ctrl["t"]; medium {
	case "", "d":
	case "f", "t", "s":
		var err error
		if data, err = readKittyMedium(medium, string(data), ctrlInt(ctrl, "S", 0), ctrlInt(ctrl, "O", 0)); err != nil {
			return nil, err
		}
	default:
		return nil, kerr("EINVAL", "unknown transmission medium")
	}
	if ctrl["o"] == "z" {
		zr, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, kerr("EINVAL", "bad zlib data")
		}
		data, err = io.ReadAll(io.LimitReader(zr, maxKittyData+1))
		if err != nil || len(data) > maxKittyData {
			return nil, kerr("EINVAL", "bad zlib data")
		}
	}
	return decodeKittyImage(ctrlInt(ctrl, "f", 32), ctrlInt(ctrl, "s", 0), ctrlInt(ctrl, "v", 0), data)
}

// inKittyTempDir reports whether a resolved path lies in a directory t=t
// files may come from: /tmp, /dev/shm or the user's $TMPDIR.
func inKittyTempDir(real string) bool {
	for _, d := range []string{"/tmp", "/dev/shm", os.TempDir()} {
		if r, err := filepath.EvalSymlinks(d); err == nil && strings.HasPrefix(real, r+"/") {
			return true
		}
	}
	return false
}

// readKittyMedium reads image data from a file (t=f), a temporary file that is
// deleted afterwards (t=t, only under /tmp, /dev/shm or $TMPDIR and named with
// "tty-graphics-protocol"), or a POSIX shared-memory object (t=s). Only
// regular files are read, never from /proc, /sys or /dev (except /dev/shm).
func readKittyMedium(medium, path string, size, offset int) ([]byte, error) {
	if medium == "s" {
		name := strings.TrimPrefix(path, "/")
		if name == "" || strings.Contains(name, "/") || name == "." || name == ".." {
			return nil, kerr("EINVAL", "bad shared memory name")
		}
		path = "/dev/shm/" + name
	}
	if !filepath.IsAbs(path) {
		return nil, kerr("EINVAL", "path must be absolute")
	}
	clean := filepath.Clean(path)
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return nil, kerr("EBADF", "cannot open file")
	}
	inShm := strings.HasPrefix(real, "/dev/shm/")
	// Media that are deleted after reading are never a symlink themselves; a
	// symlinked parent such as macOS's /tmp -> /private/tmp is fine.
	if medium == "s" || medium == "t" {
		dir, err := filepath.EvalSymlinks(filepath.Dir(clean))
		if err != nil || real != filepath.Join(dir, filepath.Base(clean)) {
			return nil, kerr("EPERM", "refusing symlinked or out-of-place file")
		}
	}
	if medium == "s" && !inShm {
		return nil, kerr("EPERM", "refusing symlinked or out-of-place file")
	}
	for _, bad := range []string{"/proc/", "/sys/", "/dev/"} {
		if strings.HasPrefix(real, bad) && !inShm {
			return nil, kerr("EPERM", "refusing device or kernel file")
		}
	}
	if medium == "t" {
		if !inKittyTempDir(real) || !strings.Contains(filepath.Base(real), "tty-graphics-protocol") {
			return nil, kerr("EPERM", "temporary file outside temp dirs")
		}
	}
	// O_NOFOLLOW|O_NONBLOCK and an fstat close the swap-in race: a symlink or
	// FIFO put in place after the checks above is refused, never blocked on.
	f, err := os.OpenFile(real, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, kerr("EBADF", "cannot open file")
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, kerr("EBADF", "not a regular file")
	}
	if fi.Size() > maxKittyData {
		return nil, kerr("EFBIG", "file too large")
	}
	if offset > 0 {
		if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
			return nil, kerr("EINVAL", "bad offset")
		}
	}
	limit := int64(maxKittyData)
	if size > 0 {
		limit = int64(min(size, maxKittyData))
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, kerr("EBADF", "read failed")
	}
	if medium == "t" || medium == "s" {
		if cur, err := os.Lstat(real); err == nil && os.SameFile(cur, fi) {
			os.Remove(real)
		}
	}
	return data, nil
}

// decodeKittyImage converts raw transmitted data into RGBA pixels.
func decodeKittyImage(format, width, height int, data []byte) (*images.Image, error) {
	switch format {
	case 100:
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, kerr("EBADPNG", "cannot decode PNG")
		}
		if cfg.Width > maxKittyPixel || cfg.Height > maxKittyPixel || cfg.Width*cfg.Height > maxKittyArea {
			return nil, kerr("EFBIG", "image too large")
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, kerr("EBADPNG", "cannot decode PNG")
		}
		b := img.Bounds()
		rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
		return &images.Image{Width: b.Dx(), Height: b.Dy(), Pixels: rgba.Pix}, nil
	case 24, 32:
		if width <= 0 || height <= 0 {
			return nil, kerr("EINVAL", "bad image dimensions")
		}
		if width > maxKittyPixel || height > maxKittyPixel || width*height > maxKittyArea {
			return nil, kerr("EFBIG", "image too large")
		}
		bpp := format / 8
		if len(data) < width*height*bpp {
			return nil, kerr("ENODATA", "insufficient image data")
		}
		pix := make([]byte, width*height*4)
		if bpp == 4 { // copy so the pixels never pin the whole payload
			copy(pix, data)
			return &images.Image{Width: width, Height: height, Pixels: pix}, nil
		}
		for i := 0; i < width*height; i++ {
			copy(pix[i*4:i*4+3], data[i*3:i*3+3])
			pix[i*4+3] = 255
		}
		return &images.Image{Width: width, Height: height, Pixels: pix}, nil
	}
	return nil, kerr("EINVAL", "unknown format")
}

// lookupKittyImage resolves i= (id) or I= (number).
func (t *Terminal) lookupKittyImage(ctrl map[string]string) (*images.Image, bool) {
	store := t.activeImages()
	if id := ctrlInt(ctrl, "i", 0); id != 0 {
		return store.Get(images.ImageID(id))
	}
	if n := ctrlInt(ctrl, "I", 0); n != 0 {
		return store.ByNumber(uint32(n))
	}
	return nil, false
}

// kittyPlace displays an already-stored image (a=p).
func (t *Terminal) kittyPlace(ctrl map[string]string) {
	img, ok := t.lookupKittyImage(ctrl)
	if !ok {
		t.kittyReply(ctrl, images.ImageID(ctrlInt(ctrl, "i", 0)), kerr("ENOENT", "image not found"))
		return
	}
	t.kittyReply(ctrl, img.ID, t.placeImage(img, ctrl))
}

// placeImage anchors a placement at the cursor. Without c/r the size in cells
// follows from the (source-rect) pixel size and the cell size. Real
// placements move the cursor right past the image on its last row unless C=1.
// Relative placements (P/Q) anchor at the parent placement offset by H/V
// cells and never move the cursor.
func (t *Terminal) placeImage(img *images.Image, ctrl map[string]string) error {
	cw, ch := t.cellSize()
	px := func(k string) int { return min(max(ctrlInt(ctrl, k, 0), 0), maxKittyPixel) }
	sx, sy, sw, sh := px("x"), px("y"), px("w"), px("h")
	pw, ph := sw, sh
	if pw == 0 {
		pw = max(img.Width-sx, 0)
	}
	if ph == 0 {
		ph = max(img.Height-sy, 0)
	}
	ox, oy := px("X"), px("Y")
	cols, rows := ctrlInt(ctrl, "c", 0), ctrlInt(ctrl, "r", 0)
	if cols <= 0 {
		cols = max((pw+ox+cw-1)/cw, 1)
	}
	if rows <= 0 {
		rows = max((ph+oy+ch-1)/ch, 1)
	}
	cols, rows = min(cols, maxKittyCells), min(rows, maxKittyCells)
	virtual := ctrl["U"] == "1"
	col, _ := t.Grid.GetCursorLocked()
	p := &images.Placement{
		ImageID:      img.ID,
		PlacementID:  uint32(ctrlInt(ctrl, "p", 0)),
		AnchorAbsRow: t.Grid.AbsoluteCursorRowLocked(),
		AnchorCol:    col,
		Cols:         cols,
		Rows:         rows,
		ZIndex:       ctrlInt(ctrl, "z", 0),
		SrcX:         sx, SrcY: sy, SrcW: sw, SrcH: sh,
		OffsetX: ox, OffsetY: oy,
		Virtual: virtual,
	}
	relative := false
	if pid := ctrlInt(ctrl, "P", 0); pid > 0 && virtual {
		// Kitty spec: "If a client attempts to make a virtual placement
		// relative the terminal must respond with the EINVAL error code."
		return kerr("EINVAL", "virtual placements cannot be relative")
	} else if pid > 0 {
		p.ParentImage, p.ParentPlacement = images.ImageID(pid), uint32(ctrlInt(ctrl, "Q", 0))
		p.RelCol, p.RelRow = ctrlInt(ctrl, "H", 0), ctrlInt(ctrl, "V", 0)
		if err := t.activeImages().PlaceRelative(p); err != nil {
			return err
		}
		relative = true
	} else {
		t.activeImages().Place(p)
	}
	t.Grid.MarkScreenDirtyLocked()
	if virtual || relative || ctrl["C"] == "1" {
		return nil
	}
	// Past a full screen of index() only blank rows would scroll by.
	for range min(rows-1, t.Grid.Rows) {
		t.index()
	}
	_, row := t.Grid.GetCursorLocked()
	t.Grid.SetCursorLocked(min(col+cols, t.Grid.Cols-1), row)
	return nil
}

// kittyDelete handles a=d. Lowercase selectors delete placements only;
// uppercase also free image data no longer referenced.
func (t *Terminal) kittyDelete(ctrl map[string]string) {
	d := ctrl["d"]
	if d == "" {
		d = "a"
	}
	free := d != strings.ToLower(d)
	store := t.activeImages()
	curCol, curRow := t.Grid.GetCursorLocked()
	screenTop := t.Grid.AbsoluteCursorRowLocked() - curRow
	hit := func(p *images.Placement, col, row int) bool {
		r0 := p.AnchorAbsRow - screenTop
		return (col < 0 || col >= p.AnchorCol && col < p.AnchorCol+p.Cols) &&
			(row < 0 || row >= r0 && row < r0+p.Rows)
	}
	x, y, z := ctrlInt(ctrl, "x", 0), ctrlInt(ctrl, "y", 0), ctrlInt(ctrl, "z", 0)
	var match func(*images.Placement) bool
	var candidates []images.ImageID
	switch strings.ToLower(d) {
	case "a":
		rows := t.Grid.Rows
		match = func(p *images.Placement) bool {
			r0 := p.AnchorAbsRow - screenTop
			return !p.Virtual && r0 < rows && r0+p.Rows > 0
		}
	case "i", "n":
		img, ok := t.lookupKittyImage(ctrl)
		if !ok {
			return
		}
		pid := uint32(ctrlInt(ctrl, "p", 0))
		match = func(p *images.Placement) bool {
			return p.ImageID == img.ID && (pid == 0 || p.PlacementID == pid)
		}
		if pid == 0 {
			candidates = append(candidates, img.ID)
		}
	case "c":
		match = func(p *images.Placement) bool { return !p.Virtual && hit(p, curCol, curRow) }
	case "p":
		match = func(p *images.Placement) bool { return !p.Virtual && hit(p, x-1, y-1) }
	case "q":
		match = func(p *images.Placement) bool { return !p.Virtual && p.ZIndex == z && hit(p, x-1, y-1) }
	case "x":
		match = func(p *images.Placement) bool { return !p.Virtual && hit(p, x-1, -1) }
	case "y":
		match = func(p *images.Placement) bool { return !p.Virtual && hit(p, -1, y-1) }
	case "z":
		match = func(p *images.Placement) bool { return !p.Virtual && p.ZIndex == z }
	case "r":
		lo, hi := images.ImageID(x), images.ImageID(y)
		match = func(p *images.Placement) bool { return p.ImageID >= lo && p.ImageID <= hi }
		for _, id := range store.ImageIDs() {
			if id >= lo && id <= hi {
				candidates = append(candidates, id)
			}
		}
	default:
		return
	}
	store.DeletePlacements(match, free, candidates...)
	t.Grid.MarkScreenDirtyLocked()
}

// kittyReply answers a command that named an image (i= or I=), or any a=q.
// q=1 suppresses OK replies, q=2 all replies.
func (t *Terminal) kittyReply(ctrl map[string]string, id images.ImageID, err error) {
	if t.responseWriter == nil {
		return
	}
	_, hasI := ctrl["i"]
	num := ctrlInt(ctrl, "I", 0)
	if !hasI && num == 0 && ctrl["a"] != "q" {
		return
	}
	q := ctrlInt(ctrl, "q", 0)
	if q >= 2 || (q == 1 && err == nil) {
		return
	}
	msg := "OK"
	if err != nil {
		if ke, ok := errors.AsType[*kittyError](err); ok {
			msg = ke.Error()
		} else if ie, ok := errors.AsType[*images.Error](err); ok {
			msg = ie.Error()
		} else {
			msg = "EINVAL:" + err.Error()
		}
	}
	var b strings.Builder
	b.WriteString("\x1b_Gi=" + strconv.FormatUint(uint64(id), 10))
	if num != 0 {
		b.WriteString(",I=" + strconv.Itoa(num))
	}
	if p := ctrlInt(ctrl, "p", 0); p != 0 {
		b.WriteString(",p=" + strconv.Itoa(p))
	}
	b.WriteString(";" + msg + "\x1b\\")
	t.responseWriter([]byte(b.String()))
}

// Images returns the active screen's image store (for the renderer).
func (t *Terminal) Images() *images.Store {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.activeImages()
}
