package hdrctl

// Crop & Merge > Remove hairs (hairpage.go on Windows): a box drawn around
// a hair is searched for the line, which is healed with nearby film
// texture. Loading, removing and saving run on goroutines.

import (
	"fmt"
	"image"
	"path/filepath"
	"strconv"
	"strings"

	"basetool/hdrscan"
)

const hairStatusText = "No automatic mode: over a whole frame the line detector also finds real lines (branches, wires, edges), so it only runs inside a box you draw."

// HairModes are the mode combo's items.
var HairModes = hairModes

// Hair is the Remove hairs page (hairUI on Windows).
type Hair struct {
	c *Ctl

	Path    string // the TIFF LineEdit
	PathSeq int    // bumps when the page sets Path
	Mode    int    // 0 remove, 1 zoom
	Mask    bool   // Show mask
	Status  string // status TextEdit
	Seq     int    // bumps when the preview changes

	src   string // file the pixels came from (or were last saved to)
	ti    hdrscan.TIFFInfo
	baseN int // hair-removal selections already recorded in the loaded file's Software tag
	w, h  int
	pix   []uint16
	lut   *[65536]uint8
	edits []*hdrscan.HairEdit
	saved int // len(edits) at the last save
	vp    image.Rectangle
	busy  bool
}

func (hu *Hair) say(s string) {
	hu.Status = s
	hu.c.say("Remove hairs: " + strings.ReplaceAll(s, "\n", " | "))
}

// Buttons: Undo, Save, Fit and Browse enabled (updateButtons).
func (hu *Hair) Buttons() (undo, save, fit, load bool) {
	return !hu.busy && len(hu.edits) > 0, !hu.busy && len(hu.edits) != hu.saved, hu.pix != nil, !hu.busy
}

// Busy reports a load, removal or save in progress ("Working...").
func (hu *Hair) Busy() bool { return hu.busy }

// Loaded reports an image.
func (hu *Hair) Loaded() bool { return hu.pix != nil }

// Size is the image size.
func (hu *Hair) Size() (int, int) { return hu.w, hu.h }

// Viewport is the image area shown.
func (hu *Hair) Viewport() image.Rectangle { return hu.vp }

// Edits are the boxes used so far (green with Show mask).
func (hu *Hair) Edits() []image.Rectangle {
	var rs []image.Rectangle
	for _, e := range hu.edits {
		rs = append(rs, e.Rect)
	}
	return rs
}

// Preview renders the viewport at dw x dh (hairPreview).
func (hu *Hair) Preview(dw, dh int) *image.RGBA {
	if hu.pix == nil {
		return nil
	}
	return hairPreview(hu.pix, hu.w, hu.h, hu.vp, dw, dh, hu.lut, hu.edits, hu.Mask)
}

// unsavedOK asks before unsaved removals are dropped; next runs if they
// may be (the Windows Yes/No box, here OK/Cancel).
func (hu *Hair) unsavedOK(next func()) {
	if len(hu.edits) == hu.saved {
		next()
		return
	}
	hu.c.Host.Confirm("Remove hairs", fmt.Sprintf("%d hair removal(s) in %s are not saved.\nDiscard them?", len(hu.edits)-hu.saved, filepath.Base(hu.src)), func(ok bool) {
		if ok {
			next()
		}
	})
}

// Open loads a 16-bit TIFF (browsed, typed, dropped or the last HDR).
func (hu *Hair) Open(path string) {
	path = strings.TrimSpace(path)
	if hu.busy || path == "" {
		return
	}
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".tif" && ext != ".tiff" {
		hu.c.Host.Message("Remove hairs", "Open a 16-bit TIFF (an HDR from this program or a converted frame).")
		return
	}
	hu.unsavedOK(func() { hu.open(path) })
}

func (hu *Hair) open(path string) {
	hu.Path = path
	hu.PathSeq++
	hu.busy = true
	hu.Seq++
	hu.say("Loading " + path + " ...")
	go func() {
		p, err := hdrscan.Load(path)
		var ti hdrscan.TIFFInfo
		var lut *[65536]uint8
		if err == nil {
			ti, err = hdrscan.ReadTIFFInfo(path)
			lut = hairLUT(p.Pix, p.W, p.H)
		}
		hu.c.Host.Post(func() {
			hu.busy = false
			hu.Seq++
			if err != nil {
				hu.say("Could not open " + path + ": " + err.Error())
				return
			}
			hu.src, hu.ti, hu.w, hu.h, hu.pix, hu.lut = path, ti, p.W, p.H, p.Pix, lut
			hu.edits, hu.saved = nil, 0
			hu.baseN = 0
			if m := hairNoteRE.FindStringSubmatch(ti.Software); m != nil {
				hu.baseN, _ = strconv.Atoi(m[1])
			}
			hu.vp = image.Rect(0, 0, p.W, p.H)
			to, inPlace := hdrscan.HairSavePath(path)
			where := "Save writes into this file (it is an HDR output of this program)."
			if !inPlace {
				where = "Save writes a new file " + filepath.Base(to) + " (this file is not an _HDR.tiff and is never changed)."
			}
			hu.say(fmt.Sprintf("%s  %dx%d\n%s\nDrag a box around a hair (Zoom mode first if it is small on screen). Only pixels inside the box can change.", filepath.Base(path), p.W, p.H, where))
		})
	}()
}

// PathEdited is the TIFF field's OnEditingFinished.
func (hu *Hair) PathEdited(t string) {
	hu.Path = t
	if t = strings.TrimSpace(t); t != "" && t != hu.src {
		hu.Open(t)
	}
}

// Browse is "Browse...".
func (hu *Hair) Browse() {
	hu.c.Host.PickFile("HDR TIFF to clean", false, hu.Path, []string{"tif", "tiff"}, func(p string, ok bool) {
		if ok {
			hu.Open(p)
		}
	})
}

// LastHDR is "Last HDR": the newer of the last capture and the last merge.
func (hu *Hair) LastHDR() {
	var r *hdrscan.Result
	if hu.c.LastMerge != nil && hu.c.LastMerge.HDR != "" {
		r = hu.c.LastMerge
	}
	if hu.c.LastCap != nil && hu.c.LastCap.HDR != "" && (r == nil || newer(hu.c.LastCap.HDR, r.HDR)) {
		r = hu.c.LastCap
	}
	if r == nil {
		hu.c.Host.Message("Remove hairs", "No HDR made in this session yet. Use Browse... or drop a TIFF here.")
		return
	}
	hu.Open(r.HDR)
}

// DropFiles opens the first TIFF of files.
func (hu *Hair) DropFiles(files []string) {
	for _, f := range files {
		if ext := strings.ToLower(filepath.Ext(f)); ext == ".tif" || ext == ".tiff" {
			hu.Open(f)
			return
		}
	}
}

// Box is mouseUp with the drag already in image pixels: a box of at least
// 4 x 4 px removes a hair (Mode 0) or zooms in (Mode 1). viewW, viewH are
// the preview's size in px (the zoom keeps its aspect).
func (hu *Hair) Box(sel image.Rectangle, viewW, viewH int) {
	if hu.pix == nil || hu.busy {
		return
	}
	sel = sel.Canon()
	if sel.Dx() < 4 || sel.Dy() < 4 {
		return
	}
	if hu.Mode == 1 {
		hu.zoomTo(sel, viewW, viewH)
		return
	}
	hu.remove(sel)
}

// zoomTo shows sel (grown to the view's aspect, at least 64 px).
func (hu *Hair) zoomTo(sel image.Rectangle, cw, ch int) {
	if cw <= 0 || ch <= 0 {
		return
	}
	asp := float64(cw) / float64(ch)
	w, h := float64(max(sel.Dx(), 64)), float64(max(sel.Dy(), 64))
	if w/h < asp {
		w = h * asp
	} else {
		h = w / asp
	}
	w, h = min(w, float64(hu.w)), min(h, float64(hu.h))
	cx, cy := float64(sel.Min.X+sel.Max.X)/2, float64(sel.Min.Y+sel.Max.Y)/2
	x0 := int(max(0, min(float64(hu.w)-w, cx-w/2)))
	y0 := int(max(0, min(float64(hu.h)-h, cy-h/2)))
	hu.vp = image.Rect(x0, y0, x0+int(w), y0+int(h))
	hu.Seq++
}

// Fit shows the whole image again.
func (hu *Hair) Fit() {
	if hu.pix == nil {
		return
	}
	hu.vp = image.Rect(0, 0, hu.w, hu.h)
	hu.Seq++
}

// SetMask is the Show mask check box.
func (hu *Hair) SetMask(on bool) {
	hu.Mask = on
	hu.Seq++
}

func (hu *Hair) remove(sel image.Rectangle) {
	hu.busy = true
	hu.Seq++
	pix, w, h := hu.pix, hu.w, hu.h
	go func() {
		e, err := hdrscan.RemoveHair(pix, w, h, sel, hdrscan.DefaultHairOptions())
		hu.c.Host.Post(func() {
			hu.busy = false
			hu.Seq++
			if err != nil {
				hu.say(fmt.Sprintf("Box (%d,%d)-(%d,%d): %s. Nothing changed.", sel.Min.X, sel.Min.Y, sel.Max.X, sel.Max.Y, err))
				return
			}
			e.Apply(hu.pix)
			hu.edits = append(hu.edits, e)
			hu.say(fmt.Sprintf("%s.\n%d removal(s) not saved yet. Red = changed pixels (Show mask). Undo takes the last one back.", e, len(hu.edits)-hu.saved))
		})
	}()
}

// Undo takes the last removal back.
func (hu *Hair) Undo() {
	if hu.busy || len(hu.edits) == 0 {
		return
	}
	e := hu.edits[len(hu.edits)-1]
	e.Undo(hu.pix)
	hu.edits = hu.edits[:len(hu.edits)-1]
	if hu.saved > len(hu.edits) {
		hu.saved = -1 // the file on disk has an edit that is now undone: saving is needed again
	}
	hu.Seq++
	hu.say(fmt.Sprintf("Undone: %s. %d removal(s) left.", e.Rect, len(hu.edits)))
}

// Save is "Save into _HDR.tiff" (asks first).
func (hu *Hair) Save() {
	if hu.busy || hu.pix == nil || len(hu.edits) == hu.saved {
		return
	}
	to, inPlace := hdrscan.HairSavePath(hu.src)
	n := hu.baseN + len(hu.edits)
	var q string
	if inPlace {
		q = fmt.Sprintf("Write %d hair removal(s) into\n%s\n\nThis HDR TIFF is replaced by the cleaned one (no second copy is kept; written to a temporary file first, so a failed save leaves it unchanged). Continue?", len(hu.edits), to)
	} else {
		q = fmt.Sprintf("%s is not an HDR output of this program and is not changed.\nSave the cleaned image as a new file\n%s ?", filepath.Base(hu.src), to)
	}
	hu.c.Host.Confirm("Remove hairs", q, func(ok bool) {
		if ok {
			hu.save(to, n)
		}
	})
}

func (hu *Hair) save(to string, n int) {
	if hu.busy {
		return
	}
	ti := hu.ti
	ti.Software = hdrscan.HairSoftware(ti.Software, appVersion, n)
	hu.busy = true
	hu.Seq++
	pix, w, h, k := hu.pix, hu.w, hu.h, len(hu.edits)
	go func() {
		err := hdrscan.SaveHairTIFF(to, w, h, pix, ti)
		hu.c.Host.Post(func() {
			hu.busy = false
			hu.Seq++
			if err != nil {
				hu.say("Save failed, nothing was replaced: " + err.Error())
				return
			}
			hu.src, hu.saved = to, k
			hu.Path = to
			hu.PathSeq++
			hu.say(fmt.Sprintf("Saved %s (%d hair removal(s) recorded in the TIFF Software tag).", to, n))
		})
	}()
}

// FitRect is fitRect: the aw x ah area fitted centred in a cw x ch box.
func FitRect(aw, ah, cw, ch int) image.Rectangle { return fitRect(aw, ah, cw, ch) }
