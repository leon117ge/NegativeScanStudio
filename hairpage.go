package main

// "Remove hairs" sub-tab of HDR scan: a guided hair / fibre remover. The
// user draws a box around a hair on the preview; hdrscan.RemoveHair finds
// the line inside the box only and heals it with nearby film texture.
// Nothing outside the box changes, edits can be undone, and the result is
// written into the one <name>_HDR.tiff (see hdrscan.HairSavePath).

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"basetool/hdrscan"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type hairUI struct {
	u       *HDRUI
	pathEd  *walk.LineEdit
	view    *walk.CustomWidget
	modeCB  *walk.ComboBox
	maskCK  *walk.CheckBox
	undoBtn *walk.PushButton
	saveBtn *walk.PushButton
	fitBtn  *walk.PushButton
	loadBtn *walk.PushButton
	status  *walk.TextEdit

	src      string // file the pixels came from (or were last saved to)
	ti       hdrscan.TIFFInfo
	baseN    int // hair-removal selections already recorded in the loaded file's Software tag
	w, h     int
	pix      []uint16
	lut      *[65536]uint8
	edits    []*hdrscan.HairEdit
	saved    int // len(edits) at the last save
	vp       image.Rectangle
	bmp      *walk.Bitmap
	bmpKey   string
	busy     bool
	dragging bool
	dragA    walk.Point
	dragB    walk.Point
}

var hairModes = []string{"Drag a box around a hair: remove it", "Drag a box: zoom in"}

var hairNoteRE = regexp.MustCompile(`hair removal \((\d+) selection`)

func (hu *hairUI) say(s string) {
	if hu.status != nil {
		hu.status.SetText(strings.ReplaceAll(s, "\n", "\r\n"))
	}
	hu.u.say("Remove hairs: " + strings.ReplaceAll(s, "\n", " | "))
}

func (hu *hairUI) updateButtons() {
	if hu.undoBtn == nil {
		return
	}
	hu.undoBtn.SetEnabled(!hu.busy && len(hu.edits) > 0)
	hu.saveBtn.SetEnabled(!hu.busy && len(hu.edits) != hu.saved)
	hu.fitBtn.SetEnabled(hu.pix != nil)
	hu.loadBtn.SetEnabled(!hu.busy)
}

func (hu *hairUI) unsavedOK() bool {
	if len(hu.edits) == hu.saved {
		return true
	}
	return walk.MsgBox(hu.u.win(), "Remove hairs", fmt.Sprintf("%d hair removal(s) in %s are not saved.\r\nDiscard them?", len(hu.edits)-hu.saved, filepath.Base(hu.src)),
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes
}

// Open loads a 16-bit TIFF (dropped, browsed or the last HDR).
func (hu *hairUI) Open(path string) {
	path = strings.TrimSpace(path)
	if hu.busy || path == "" {
		return
	}
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".tif" && ext != ".tiff" {
		walk.MsgBox(hu.u.win(), "Remove hairs", "Open a 16-bit TIFF (an HDR from this program or a converted frame).", walk.MsgBoxIconInformation)
		return
	}
	if !hu.unsavedOK() {
		return
	}
	hu.pathEd.SetText(path)
	hu.busy = true
	hu.updateButtons()
	hu.say("Loading " + path + " ...")
	go func() {
		p, err := hdrscan.Load(path)
		var ti hdrscan.TIFFInfo
		var lut *[65536]uint8
		if err == nil {
			ti, err = hdrscan.ReadTIFFInfo(path)
			lut = hairLUT(p.Pix, p.W, p.H)
		}
		hu.u.ui(func() {
			hu.busy = false
			if err != nil {
				hu.updateButtons()
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
			hu.redraw()
			hu.updateButtons()
			to, inPlace := hdrscan.HairSavePath(path)
			where := "Save writes into this file (it is an HDR output of this program)."
			if !inPlace {
				where = "Save writes a new file " + filepath.Base(to) + " (this file is not an _HDR.tiff and is never changed)."
			}
			hu.say(fmt.Sprintf("%s  %dx%d\n%s\nDrag a box around a hair (Zoom mode first if it is small on screen). Only pixels inside the box can change.", filepath.Base(path), p.W, p.H, where))
		})
	}()
}

func (hu *hairUI) lastHDR() {
	var r *hdrscan.Result
	if hu.u.lastMerge != nil && hu.u.lastMerge.HDR != "" {
		r = hu.u.lastMerge
	}
	if hu.u.lastCap != nil && hu.u.lastCap.HDR != "" && (r == nil || newer(hu.u.lastCap.HDR, r.HDR)) {
		r = hu.u.lastCap
	}
	if r == nil {
		walk.MsgBox(hu.u.win(), "Remove hairs", "No HDR made in this session yet. Use Browse... or drop a TIFF here.", walk.MsgBoxIconInformation)
		return
	}
	hu.Open(r.HDR)
}

func newer(a, b string) bool {
	sa, ea := os.Stat(a)
	sb, eb := os.Stat(b)
	return ea == nil && (eb != nil || sa.ModTime().After(sb.ModTime()))
}

// imageRect: where the viewport is drawn in the widget (pixels).
func (hu *hairUI) imageRect() walk.Rectangle {
	cb := hu.view.ClientBoundsPixels()
	r := fitRect(hu.vp.Dx(), hu.vp.Dy(), cb.Width, cb.Height)
	return walk.Rectangle{X: cb.X + r.Min.X, Y: cb.Y + r.Min.Y, Width: r.Dx(), Height: r.Dy()}
}

func (hu *hairUI) redraw() {
	if hu.view == nil {
		return
	}
	if hu.pix != nil {
		ir := hu.imageRect()
		img := hairPreview(hu.pix, hu.w, hu.h, hu.vp, ir.Width, ir.Height, hu.lut, hu.edits, hu.maskCK.Checked())
		if bmp, err := walk.NewBitmapFromImage(img); err == nil {
			if hu.bmp != nil {
				hu.bmp.Dispose()
			}
			hu.bmp = bmp
		}
	}
	hu.view.Invalidate()
}

func (hu *hairUI) toImage(p walk.Point) image.Point {
	ir := hu.imageRect()
	if ir.Width <= 0 || ir.Height <= 0 {
		return hu.vp.Min
	}
	x := hu.vp.Min.X + int(float64(p.X-ir.X)*float64(hu.vp.Dx())/float64(ir.Width))
	y := hu.vp.Min.Y + int(float64(p.Y-ir.Y)*float64(hu.vp.Dy())/float64(ir.Height))
	return image.Pt(max(hu.vp.Min.X, min(hu.vp.Max.X, x)), max(hu.vp.Min.Y, min(hu.vp.Max.Y, y)))
}

func (hu *hairUI) toView(p image.Point) walk.Point {
	ir := hu.imageRect()
	return walk.Point{X: ir.X + int(float64(p.X-hu.vp.Min.X)*float64(ir.Width)/float64(hu.vp.Dx())),
		Y: ir.Y + int(float64(p.Y-hu.vp.Min.Y)*float64(ir.Height)/float64(hu.vp.Dy()))}
}

func (hu *hairUI) paint(c *walk.Canvas, _ walk.Rectangle) error {
	cb := hu.view.ClientBoundsPixels()
	bg, _ := walk.NewSolidColorBrush(walk.RGB(22, 22, 25))
	if bg != nil {
		c.FillRectanglePixels(bg, cb)
		bg.Dispose()
	}
	if hu.pix == nil || hu.bmp == nil {
		if f, _ := walk.NewFont("Segoe UI", 10, 0); f != nil {
			c.DrawTextPixels("No image.\r\nOpen an HDR TIFF (Browse..., Last HDR, or drop it here),\r\nthen drag a box around a hair.", f, walk.RGB(128, 128, 136), cb, walk.TextCenter|walk.TextVCenter|walk.TextWordbreak)
			f.Dispose()
		}
		return nil
	}
	ir := hu.imageRect()
	c.DrawImageStretchedPixels(hu.bmp, ir)
	rect := func(a, b walk.Point, col walk.Color) {
		br, _ := walk.NewSolidColorBrush(col)
		if br == nil {
			return
		}
		defer br.Dispose()
		pen, _ := walk.NewGeometricPen(walk.PenSolid, 2, br)
		if pen == nil {
			return
		}
		defer pen.Dispose()
		c.DrawRectanglePixels(pen, walk.Rectangle{X: min(a.X, b.X), Y: min(a.Y, b.Y), Width: max(a.X, b.X) - min(a.X, b.X), Height: max(a.Y, b.Y) - min(a.Y, b.Y)})
	}
	if hu.maskCK.Checked() {
		for _, e := range hu.edits {
			rect(hu.toView(e.Rect.Min), hu.toView(e.Rect.Max), walk.RGB(60, 220, 120))
		}
	}
	if hu.dragging {
		col := walk.RGB(255, 170, 40)
		if hu.modeCB.CurrentIndex() == 1 {
			col = walk.RGB(90, 170, 255)
		}
		rect(hu.dragA, hu.dragB, col)
	}
	if hu.busy {
		if f, _ := walk.NewFont("Segoe UI", 10, walk.FontBold); f != nil {
			c.DrawTextPixels("Working...", f, walk.RGB(255, 170, 40), walk.Rectangle{X: ir.X + 8, Y: ir.Y + 6, Width: 200, Height: 20}, walk.TextLeft|walk.TextSingleLine)
			f.Dispose()
		}
	}
	return nil
}

func (hu *hairUI) mouseDown(x, y int, b walk.MouseButton) {
	if b != walk.LeftButton || hu.pix == nil || hu.busy {
		return
	}
	hu.dragging = true
	hu.dragA, hu.dragB = walk.Point{X: x, Y: y}, walk.Point{X: x, Y: y}
}

func (hu *hairUI) mouseMove(x, y int, _ walk.MouseButton) {
	if !hu.dragging {
		return
	}
	hu.dragB = walk.Point{X: x, Y: y}
	hu.view.Invalidate()
}

func (hu *hairUI) mouseUp(x, y int, _ walk.MouseButton) {
	if !hu.dragging {
		return
	}
	hu.dragging = false
	hu.dragB = walk.Point{X: x, Y: y}
	a, b := hu.toImage(hu.dragA), hu.toImage(hu.dragB)
	sel := image.Rectangle{Min: a, Max: b}.Canon()
	hu.view.Invalidate()
	if sel.Dx() < 4 || sel.Dy() < 4 {
		return
	}
	if hu.modeCB.CurrentIndex() == 1 {
		hu.zoomTo(sel)
		return
	}
	hu.remove(sel)
}

// zoomTo shows sel (grown to the widget's aspect, at least 64 px).
func (hu *hairUI) zoomTo(sel image.Rectangle) {
	cb := hu.view.ClientBoundsPixels()
	if cb.Width <= 0 || cb.Height <= 0 {
		return
	}
	asp := float64(cb.Width) / float64(cb.Height)
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
	hu.redraw()
}

func (hu *hairUI) fit() {
	if hu.pix == nil {
		return
	}
	hu.vp = image.Rect(0, 0, hu.w, hu.h)
	hu.redraw()
}

func (hu *hairUI) remove(sel image.Rectangle) {
	hu.busy = true
	hu.updateButtons()
	hu.view.Invalidate()
	pix, w, h := hu.pix, hu.w, hu.h
	go func() {
		e, err := hdrscan.RemoveHair(pix, w, h, sel, hdrscan.DefaultHairOptions())
		hu.u.ui(func() {
			hu.busy = false
			if err != nil {
				hu.updateButtons()
				hu.view.Invalidate()
				hu.say(fmt.Sprintf("Box (%d,%d)-(%d,%d): %s. Nothing changed.", sel.Min.X, sel.Min.Y, sel.Max.X, sel.Max.Y, err))
				return
			}
			e.Apply(hu.pix)
			hu.edits = append(hu.edits, e)
			hu.redraw()
			hu.updateButtons()
			hu.say(fmt.Sprintf("%s.\n%d removal(s) not saved yet. Red = changed pixels (Show mask). Undo takes the last one back.", e, len(hu.edits)-hu.saved))
		})
	}()
}

func (hu *hairUI) undo() {
	if hu.busy || len(hu.edits) == 0 {
		return
	}
	e := hu.edits[len(hu.edits)-1]
	e.Undo(hu.pix)
	hu.edits = hu.edits[:len(hu.edits)-1]
	if hu.saved > len(hu.edits) {
		hu.saved = -1 // the file on disk has an edit that is now undone: saving is needed again
	}
	hu.redraw()
	hu.updateButtons()
	hu.say(fmt.Sprintf("Undone: %s. %d removal(s) left.", e.Rect, len(hu.edits)))
}

func (hu *hairUI) saveFile() {
	if hu.busy || hu.pix == nil || len(hu.edits) == hu.saved {
		return
	}
	to, inPlace := hdrscan.HairSavePath(hu.src)
	n := hu.baseN + len(hu.edits)
	var q string
	if inPlace {
		q = fmt.Sprintf("Write %d hair removal(s) into\r\n%s\r\n\r\nThis HDR TIFF is replaced by the cleaned one (no second copy is kept; written to a temporary file first, so a failed save leaves it unchanged). Continue?", len(hu.edits), to)
	} else {
		q = fmt.Sprintf("%s is not an HDR output of this program and is not changed.\r\nSave the cleaned image as a new file\r\n%s ?", filepath.Base(hu.src), to)
	}
	if walk.MsgBox(hu.u.win(), "Remove hairs", q, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	ti := hu.ti
	ti.Software = hdrscan.HairSoftware(ti.Software, appVersion, n)
	hu.busy = true
	hu.updateButtons()
	pix, w, h, k := hu.pix, hu.w, hu.h, len(hu.edits)
	go func() {
		err := hdrscan.SaveHairTIFF(to, w, h, pix, ti)
		hu.u.ui(func() {
			hu.busy = false
			if err != nil {
				hu.updateButtons()
				hu.say("Save failed, nothing was replaced: " + err.Error())
				return
			}
			hu.src, hu.saved = to, k
			hu.pathEd.SetText(to)
			hu.updateButtons()
			hu.say(fmt.Sprintf("Saved %s (%d hair removal(s) recorded in the TIFF Software tag).", to, n))
		})
	}()
}

// DropFiles opens the first TIFF dropped on the Remove hairs sub-tab.
func (hu *hairUI) DropFiles(files []string) {
	for _, f := range files {
		if ext := strings.ToLower(filepath.Ext(f)); ext == ".tif" || ext == ".tiff" {
			hu.Open(f)
			return
		}
	}
}

func (hu *hairUI) page(grey walk.Color) TabPage {
	browse := func() {
		dlg := new(walk.FileDialog)
		dlg.Title = "HDR TIFF to clean"
		dlg.Filter = "16-bit TIFF (*.tif;*.tiff)|*.tif;*.tiff|All files (*.*)|*.*"
		dlg.FilePath = hu.pathEd.Text()
		if ok, _ := dlg.ShowOpen(hu.u.win()); ok {
			hu.Open(dlg.FilePath)
		}
	}
	return TabPage{
		Title:  " Remove hairs ",
		Layout: VBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}, Spacing: 8},
		Children: []Widget{
			Label{Text: "Remove a hair or fibre by hand: draw a box around it. Only that box is searched and only the line found in it is healed\nwith film texture from beside it - shadows and image detail outside the line stay as they are. Undo takes the last box back.", TextColor: grey},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				Label{Text: "TIFF"},
				LineEdit{AssignTo: &hu.pathEd, StretchFactor: 1, OnEditingFinished: func() {
					if t := strings.TrimSpace(hu.pathEd.Text()); t != "" && t != hu.src {
						hu.Open(t)
					}
				}},
				PushButton{AssignTo: &hu.loadBtn, Text: "Browse...", OnClicked: browse},
				PushButton{Text: "Last HDR", OnClicked: hu.lastHDR, ToolTipText: "Open the HDR TIFF made last in this session (Camera capture or Merge files)."},
			}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				ComboBox{AssignTo: &hu.modeCB, Model: hairModes, CurrentIndex: 0, MinSize: Size{Width: 260}},
				PushButton{AssignTo: &hu.fitBtn, Text: "Fit", Enabled: false, OnClicked: hu.fit, ToolTipText: "Show the whole image again."},
				CheckBox{AssignTo: &hu.maskCK, Text: "Show mask", Checked: true, OnCheckedChanged: hu.redraw, ToolTipText: "Changed pixels in red, the boxes used in green."},
				PushButton{AssignTo: &hu.undoBtn, Text: "Undo", Enabled: false, OnClicked: hu.undo},
				HSpacer{},
				PushButton{AssignTo: &hu.saveBtn, Text: "Save into _HDR.tiff", Enabled: false, Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true}, OnClicked: hu.saveFile,
					ToolTipText: "An <name>_HDR.tiff from this program is updated in place (asks first). Any other TIFF is never changed: a new <name>_HDR.tiff is written next to it."},
			}},
			CustomWidget{
				AssignTo:            &hu.view,
				Paint:               hu.paint,
				PaintMode:           PaintBuffered,
				InvalidatesOnResize: true,
				MinSize:             Size{Width: 480, Height: 360},
				StretchFactor:       10,
				OnMouseDown:         hu.mouseDown,
				OnMouseMove:         hu.mouseMove,
				OnMouseUp:           hu.mouseUp,
				OnSizeChanged:       hu.redraw,
			},
			TextEdit{AssignTo: &hu.status, ReadOnly: true, VScroll: true, MinSize: Size{Height: 56},
				Text: "No automatic mode: over a whole frame the line detector also finds real lines (branches, wires, edges), so it only runs inside a box you draw."},
		},
	}
}
