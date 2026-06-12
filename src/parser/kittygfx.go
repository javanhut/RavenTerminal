package parser

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strconv"
	"strings"

	"github.com/javanhut/RavenTerminal/src/images"
)

// kittyTransmit accumulates a chunked Kitty image transmission (m=1 chunks).
type kittyTransmit struct {
	id     images.ImageID
	format int // 24=RGB, 32=RGBA, 100=PNG
	width  int
	height int
	buf    bytes.Buffer
}

// processAPC accumulates APC payload bytes until a terminator (ST/BEL).
func (t *Terminal) processAPC(b byte) {
	switch b {
	case 0x1b: // ESC — possible start of ST
		t.state = StateAPCEscape
	case 0x9c, 0x07: // ST (8-bit) or BEL
		t.handleAPC(t.apcBuf)
		t.state = StateGround
	default:
		t.apcBuf = append(t.apcBuf, b)
	}
}

// processAPCEscape handles the byte after ESC inside an APC string.
func (t *Terminal) processAPCEscape(b byte) {
	if b == '\\' { // ST (ESC \)
		t.handleAPC(t.apcBuf)
		t.state = StateGround
		return
	}
	// Not a terminator: the ESC was literal payload.
	t.apcBuf = append(t.apcBuf, 0x1b, b)
	t.state = StateAPC
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
	if len(buf) == 0 || buf[0] != 'G' {
		return // only the Kitty graphics APC is supported
	}
	body := buf[1:]
	var ctrlPart, payloadPart []byte
	if before, after, ok := bytes.Cut(body, []byte{';'}); ok {
		ctrlPart, payloadPart = before, after
	} else {
		ctrlPart = body
	}
	ctrl := parseKittyControls(string(ctrlPart))

	switch ctrl["a"] {
	case "", "t", "T": // transmit (and, for T, display)
		t.kittyTransmit(ctrl, payloadPart)
	case "p": // put/display an already-transmitted image
		t.kittyPlace(ctrl)
	case "d": // delete
		t.kittyDelete(ctrl)
	case "q": // query — answer OK without storing
		t.kittyReply(ctrl, "OK")
	}
}

// parseKittyControls parses comma-separated key=value control pairs.
func parseKittyControls(s string) map[string]string {
	m := make(map[string]string)
	for kv := range strings.SplitSeq(s, ",") {
		if kv == "" {
			continue
		}
		if before, after, ok := strings.Cut(kv, "="); ok {
			m[before] = after
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

// kittyTransmit handles transmit (and optional display) of image data, including
// chunked transmission (m=1 means more chunks follow).
func (t *Terminal) kittyTransmit(ctrl map[string]string, payload []byte) {
	// Begin or continue an assembly buffer. Format/size come from the first chunk.
	if t.pendingKitty == nil {
		t.pendingKitty = &kittyTransmit{
			id:     images.ImageID(ctrlInt(ctrl, "i", 0)),
			format: ctrlInt(ctrl, "f", 32),
			width:  ctrlInt(ctrl, "s", 0),
			height: ctrlInt(ctrl, "v", 0),
		}
	}
	if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(payload))); err == nil {
		t.pendingKitty.buf.Write(decoded)
	}

	if ctrl["m"] == "1" {
		return // more chunks to come
	}

	// Final chunk: build the image.
	tr := t.pendingKitty
	t.pendingKitty = nil
	img := decodeKittyImage(tr)
	if img == nil {
		t.kittyReply(ctrl, "EINVAL")
		return
	}
	stored := t.activeImages().Add(tr.id, img.Width, img.Height, img.Pixels)

	// a=T also displays the image at the cursor.
	if ctrl["a"] == "T" {
		t.placeImage(stored, ctrl)
	}
	t.kittyReply(ctrl, "OK")
}

// decodeKittyImage converts an assembled transmission into RGBA pixels.
func decodeKittyImage(tr *kittyTransmit) *images.Image {
	data := tr.buf.Bytes()
	switch tr.format {
	case 100: // PNG
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		b := img.Bounds()
		rgba := image.NewRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
		return &images.Image{Width: b.Dx(), Height: b.Dy(), Pixels: rgba.Pix}
	case 24: // RGB
		if tr.width*tr.height*3 != len(data) {
			return nil
		}
		pix := make([]byte, tr.width*tr.height*4)
		for i := 0; i < tr.width*tr.height; i++ {
			pix[i*4] = data[i*3]
			pix[i*4+1] = data[i*3+1]
			pix[i*4+2] = data[i*3+2]
			pix[i*4+3] = 255
		}
		return &images.Image{Width: tr.width, Height: tr.height, Pixels: pix}
	default: // 32 = RGBA
		if tr.width*tr.height*4 != len(data) {
			return nil
		}
		return &images.Image{Width: tr.width, Height: tr.height, Pixels: data}
	}
}

// kittyPlace displays an already-stored image (a=p).
func (t *Terminal) kittyPlace(ctrl map[string]string) {
	id := images.ImageID(ctrlInt(ctrl, "i", 0))
	if img, ok := t.activeImages().Get(id); ok {
		t.placeImage(img, ctrl)
		t.kittyReply(ctrl, "OK")
	} else {
		t.kittyReply(ctrl, "ENOENT")
	}
}

// placeImage anchors a placement at the cursor and advances the cursor past it.
func (t *Terminal) placeImage(img *images.Image, ctrl map[string]string) {
	cellW, cellH := 1, 1 // dimensions in cells are derived from pixel size below
	cols := ctrlInt(ctrl, "c", 0)
	rows := ctrlInt(ctrl, "r", 0)
	// Without explicit c/r, the caller cannot know cell pixel size here; default
	// to covering the image's natural cell span using a nominal 1px cell (the
	// renderer scales to the real pane cell size). Cols/rows are advisory.
	_ = cellW
	_ = cellH
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	col, _ := t.Grid.GetCursor()
	t.activeImages().Place(&images.Placement{
		ImageID:      img.ID,
		PlacementID:  uint32(ctrlInt(ctrl, "p", 0)),
		AnchorAbsRow: t.Grid.AbsoluteCursorRow(),
		AnchorCol:    col,
		Cols:         cols,
		Rows:         rows,
		ZIndex:       ctrlInt(ctrl, "z", 0),
	})
}

// kittyDelete handles a=d deletions. Lowercase d-values remove placements only;
// uppercase also free image data. Only the common forms are implemented.
func (t *Terminal) kittyDelete(ctrl map[string]string) {
	store := t.activeImages()
	switch ctrl["d"] {
	case "", "a", "A":
		if ctrl["d"] == "A" {
			store.DeleteAll()
		} else {
			store.ClearPlacements()
		}
	case "i", "I":
		id := images.ImageID(ctrlInt(ctrl, "i", 0))
		store.DeleteImage(id)
	}
}

// kittyReply sends the Kitty graphics response, honoring quiet mode (q=1 quells
// success, q=2 quells all).
func (t *Terminal) kittyReply(ctrl map[string]string, msg string) {
	if t.responseWriter == nil {
		return
	}
	q := ctrlInt(ctrl, "q", 0)
	if q >= 2 || (q == 1 && msg == "OK") {
		return
	}
	id := ctrlInt(ctrl, "i", 0)
	t.responseWriter([]byte("\x1b_Gi=" + strconv.Itoa(id) + ";" + msg + "\x1b\\"))
}

// Images returns the active screen's image store (for the renderer).
func (t *Terminal) Images() *images.Store {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.activeImages()
}
