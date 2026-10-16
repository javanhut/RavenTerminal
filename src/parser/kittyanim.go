package parser

import (
	"strconv"
	"time"

	"github.com/javanhut/RavenTerminal/src/images"
)

// kittyFrame handles a=f: frame data for an animation. The data is decoded
// like a transmission (s/v default to the image size) and composed at x,y
// onto a new frame (based on frame c, or the Y colour) or onto frame r.
func (t *Terminal) kittyFrame(ctrl map[string]string, data []byte) {
	img, ok := t.lookupKittyImage(ctrl)
	if !ok {
		t.kittyReply(ctrl, images.ImageID(ctrlInt(ctrl, "i", 0)), kerr("ENOENT", "image not found"))
		return
	}
	if ctrl["f"] != "100" {
		for k, v := range map[string]int{"s": img.Width, "v": img.Height} {
			if _, ok := ctrl[k]; !ok {
				ctrl[k] = strconv.Itoa(v)
			}
		}
	}
	fr, err := loadKittyImage(ctrl, data)
	if err != nil {
		t.kittyReply(ctrl, img.ID, err)
		return
	}
	bg, _ := strconv.ParseUint(ctrl["Y"], 10, 32)
	_, err = t.activeImages().LoadFrame(img.ID, images.FrameData{
		Pixels: fr.Pixels, W: fr.Width, H: fr.Height,
		X: ctrlInt(ctrl, "x", 0), Y: ctrlInt(ctrl, "y", 0),
		Base: ctrlInt(ctrl, "c", 0), Edit: ctrlInt(ctrl, "r", 0),
		Gap: ctrlInt(ctrl, "z", 0), Overwrite: ctrl["X"] == "1", Bg: uint32(bg),
	})
	t.Grid.MarkScreenDirtyLocked()
	t.kittyReply(ctrl, img.ID, err)
}

// kittyAnimate handles a=a: s (1 stop, 2 loading, 3 run), v (loops),
// c (current frame), and z (gap) for frame r.
func (t *Terminal) kittyAnimate(ctrl map[string]string) {
	img, ok := t.lookupKittyImage(ctrl)
	if !ok {
		t.kittyReply(ctrl, images.ImageID(ctrlInt(ctrl, "i", 0)), kerr("ENOENT", "image not found"))
		return
	}
	err := t.activeImages().ControlAnimation(img.ID, images.AnimControl{
		State: images.AnimState(ctrlInt(ctrl, "s", 0)), Loops: ctrlInt(ctrl, "v", 0),
		Current: ctrlInt(ctrl, "c", 0), GapFrame: ctrlInt(ctrl, "r", 0), Gap: ctrlInt(ctrl, "z", 0),
	}, time.Now())
	t.Grid.MarkScreenDirtyLocked()
	t.kittyReply(ctrl, img.ID, err)
}

// kittyCompose handles a=c: copy a w x h rect at x,y of frame r onto frame c
// at X,Y, alpha blended unless C=1.
func (t *Terminal) kittyCompose(ctrl map[string]string) {
	img, ok := t.lookupKittyImage(ctrl)
	if !ok {
		t.kittyReply(ctrl, images.ImageID(ctrlInt(ctrl, "i", 0)), kerr("ENOENT", "image not found"))
		return
	}
	err := t.activeImages().ComposeFrames(img.ID, images.Compose{
		Src: ctrlInt(ctrl, "r", 0), Dst: ctrlInt(ctrl, "c", 0),
		X: ctrlInt(ctrl, "x", 0), Y: ctrlInt(ctrl, "y", 0), W: ctrlInt(ctrl, "w", 0), H: ctrlInt(ctrl, "h", 0),
		DstX: ctrlInt(ctrl, "X", 0), DstY: ctrlInt(ctrl, "Y", 0), Overwrite: ctrl["C"] == "1",
	})
	t.Grid.MarkScreenDirtyLocked()
	t.kittyReply(ctrl, img.ID, err)
}
