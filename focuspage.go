package main

// Focus Check page (v0.97): live view with zoom, green-only or white light,
// temporary auto-exposure so film grain is readable, and a sharpness meter
// with peak hold. Prefers the Hikrobot monochrome camera; falls back to the
// existing tethered camera's live view.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"basetool/camera"
	"basetool/hdrscan"
	"basetool/hikcam"
	"basetool/rgblight"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type FocusUI struct {
	hdr   *HDRUI
	light *LightUI
	mono  *MonoUI

	active bool

	view                                        *walk.CustomWidget
	bmp                                         *walk.Bitmap
	srcW, srcH                                  int
	zoomCB                                      *walk.ComboBox
	greenBtn, whiteBtn, liveBtn, resetPeakBtn   *walk.PushButton
	srcLbl, expLbl, lightLbl, sharpLbl, peakLbl *walk.Label
	logTE                                       *walk.TextEdit
	meter                                       *walk.ProgressBar

	zoom         int     // 0 = fit, else 1/2/4
	cx, cy       float64 // pan centre, 0..1
	drag         bool
	dragX, dragY int

	loopOn  atomic.Bool
	loopRun atomic.Bool

	mu         sync.Mutex
	samples    []float64
	visW, visH int
	score      hdrscan.FocusScore
	peak       hdrscan.PeakHold
	aeWhy      string
	aeState    hdrscan.ExposureState

	savedHik       hikcam.Settings
	haveSavedHik   bool
	savedFuji      camera.Exposure
	haveSavedFuji  bool
	savedLight     rgblight.State
	haveSavedLight bool
	lightMode      int // 0 = green, 1 = white
}

var zoomItems = []string{"Fit", "100 %", "200 %", "400 %"}
var zoomVals = []int{0, 1, 2, 4}

func (u *FocusUI) say(s string) {
	if u.hdr != nil && u.hdr.log != nil {
		u.hdr.log.Printf("Focus check: %s", s)
	}
	if u.logTE != nil {
		tx := u.logTE.Text()
		if len(tx) > 20000 {
			u.logTE.SetText(tx[len(tx)-12000:])
		}
		u.logTE.AppendText(time.Now().Format("15:04:05  ") + s + "\r\n")
		u.logTE.ScrollToCaret()
	}
}

func (u *FocusUI) sourceName() string {
	if u.mono != nil && u.mono.cam != nil {
		return u.mono.cam.Info().Label()
	}
	if u.hdr != nil && u.hdr.dev != nil {
		inf := u.hdr.dev.Info()
		return strings.TrimSpace(inf.Manufacturer + " " + inf.Model)
	}
	return ""
}

func (u *FocusUI) enter() {
	if u.active {
		return
	}
	u.active = true
	u.cx, u.cy = 0.5, 0.5
	u.peak.Reset()
	// Snapshot exposures we may overwrite, then drive the light to green.
	if u.mono != nil && u.mono.cam != nil {
		u.savedHik = u.mono.cam.Settings()
		u.haveSavedHik = true
		u.aeState = hdrscan.ExposureState{ExposureUS: u.savedHik.ExposureUS, GainDB: u.savedHik.GainDB}
	} else {
		u.haveSavedHik = false
	}
	if u.hdr != nil && u.hdr.dev != nil {
		if e, err := u.hdr.dev.Exposure(); err == nil {
			u.savedFuji = e
			u.haveSavedFuji = true
			u.aeState = hdrscan.ExposureState{ExposureUS: float64(e.Shutter), GainDB: 0}
		}
	} else {
		u.haveSavedFuji = false
	}
	if u.light != nil && u.light.connected() {
		if st, err := u.light.cl.ReadState(); err == nil {
			u.savedLight = st
			u.haveSavedLight = true
		}
		u.setLight(0)
	} else {
		u.haveSavedLight = false
	}
	u.say("Entered Focus check: exposure will be adjusted temporarily so the visible region (including a zoomed crop) is readable. The previous capture exposure is restored when you leave this page.")
	u.updateLabels()
	u.startLoop()
}

func (u *FocusUI) leave() {
	if !u.active {
		return
	}
	u.active = false
	u.stopLoop()
	u.restoreExposure()
	u.restoreLight()
	u.say("Left Focus check: previous capture exposure restored.")
}

func (u *FocusUI) restoreExposure() {
	if u.haveSavedHik && u.mono != nil && u.mono.cam != nil {
		if err := u.mono.cam.Apply(u.savedHik); err != nil {
			u.say("Could not restore Hikrobot exposure: " + err.Error())
		} else {
			u.say(fmt.Sprintf("Restored Hikrobot exposure: %.0f us, %.2f dB", u.savedHik.ExposureUS, u.savedHik.GainDB))
		}
	}
	if u.haveSavedFuji && u.hdr != nil && u.hdr.dev != nil {
		if _, err := u.hdr.dev.SetExposure(u.savedFuji); err != nil {
			u.say("Could not restore camera exposure: " + err.Error())
		} else {
			u.say("Restored camera exposure: " + u.savedFuji.String())
		}
	}
}

func (u *FocusUI) restoreLight() {
	if !u.haveSavedLight || u.light == nil || !u.light.connected() {
		return
	}
	if _, err := u.light.cl.SetAllConfirmed(u.savedLight.On, u.savedLight.Value); err != nil {
		u.say("Could not restore the light: " + err.Error())
		return
	}
	u.light.showState(u.savedLight, false)
}

func (u *FocusUI) setLight(mode int) {
	u.lightMode = mode
	if u.light == nil || !u.light.connected() {
		u.say("RGB light not connected - cannot switch green/white.")
		u.updateLabels()
		return
	}
	vals, _, ok := scanLightValues(u.hdr, u.light)
	if !ok {
		if cs := u.light.cl.CachedState(); cs != nil {
			vals = cs.Value
		} else {
			vals = [3]int{50, 50, 50}
		}
	}
	sw := &rgblight.ChannelSwitch{C: u.light.cl, Values: vals}
	var err error
	if mode == 0 {
		err = sw.Green()
		u.say("Light: green only (trichrome focus baseline).")
	} else {
		err = sw.White()
		u.say("Light: white (all channels) for white-light scanning.")
	}
	if err != nil {
		u.say("Light switch failed: " + err.Error())
	} else if cs := u.light.cl.CachedState(); cs != nil {
		u.light.showState(*cs, false)
	}
	u.updateLabels()
}

func (u *FocusUI) updateLabels() {
	if u.srcLbl != nil {
		n := u.sourceName()
		if n == "" {
			setStatusWide(u.srcLbl, "Camera: none connected (open the Hikrobot camera on Mono trichrome, or the Fuji body on Camera)")
		} else {
			setStatusWide(u.srcLbl, "Camera: "+n)
		}
	}
	if u.lightLbl != nil {
		mode := "green only"
		if u.lightMode != 0 {
			mode = "white"
		}
		if u.light == nil || !u.light.connected() {
			setStatusWide(u.lightLbl, "Light: not connected  (wanted: "+mode+")")
		} else {
			setStatusWide(u.lightLbl, "Light: "+mode)
		}
	}
	if u.expLbl != nil {
		setStatusWide(u.expLbl, fmt.Sprintf("Auto exposure: %.0f us, %.2f dB  (%s)", u.aeState.ExposureUS, u.aeState.GainDB, u.aeWhy))
	}
	if u.sharpLbl != nil {
		setStatusWide(u.sharpLbl, fmt.Sprintf("Sharpness: %.4f   Laplacian %.4f   grain %.4f", u.score.Combined, u.score.Laplacian, u.score.Grain))
	}
	if u.peakLbl != nil {
		if u.peak.Have {
			setStatusWide(u.peakLbl, fmt.Sprintf("Peak hold: %.4f", u.peak.Peak))
		} else {
			setStatusWide(u.peakLbl, "Peak hold: -")
		}
	}
	if u.meter != nil {
		// Map a typical score range onto 0..1000. The absolute scale is
		// arbitrary; the peak line is what the user watches while focusing.
		v := int(u.score.Combined * 20000)
		if v < 0 {
			v = 0
		}
		if v > 1000 {
			v = 1000
		}
		u.meter.SetRange(0, 1000)
		u.meter.SetValue(v)
	}
	if u.greenBtn != nil {
		u.greenBtn.SetEnabled(u.light != nil && u.light.connected())
		u.whiteBtn.SetEnabled(u.light != nil && u.light.connected())
	}
}

func (u *FocusUI) startLoop() {
	if u.loopRun.Load() {
		u.loopOn.Store(true)
		return
	}
	if u.sourceName() == "" {
		u.say("No camera to preview.")
		u.updateLabels()
		return
	}
	u.loopOn.Store(true)
	u.loopRun.Store(true)
	go u.loop()
}

func (u *FocusUI) stopLoop() {
	u.loopOn.Store(false)
	for i := 0; i < 40 && u.loopRun.Load(); i++ {
		time.Sleep(50 * time.Millisecond)
	}
}

func (u *FocusUI) loop() {
	defer u.loopRun.Store(false)
	cfg := hdrscan.DefaultAutoExp()
	for u.loopOn.Load() && u.active {
		img, pix, w, h, err := u.grabFrame()
		if err != nil {
			u.ui(func() { u.say("Live: " + err.Error()) })
			time.Sleep(400 * time.Millisecond)
			continue
		}
		x0, y0, x1, y1 := u.visibleRect(w, h, 640, 400)
		samples := hdrscan.RegionSamples(pix, w, h, x0, y0, x1, y1, 2)
		crop, cw, ch := hdrscan.CropGray(hdrscan.Gray16ToFloat64(pix), w, h, x0, y0, x1, y1)
		score := hdrscan.MeasureFocus(crop, cw, ch)
		state, why := hdrscan.AdjustExposure(samples, u.aeState, cfg)
		u.applyAE(state)
		u.mu.Lock()
		u.samples = samples
		u.visW, u.visH = cw, ch
		u.score = score
		u.peak.Update(score.Combined)
		u.aeWhy = why
		u.aeState = state
		u.mu.Unlock()
		u.ui(func() {
			u.showImage(img)
			u.updateLabels()
		})
		time.Sleep(80 * time.Millisecond)
	}
}

func (u *FocusUI) grabFrame() (image.Image, []uint16, int, int, error) {
	if u.mono != nil && u.mono.cam != nil {
		cam := u.mono.cam
		_ = cam.StartGrab()
		fr, err := cam.Grab(1500 * time.Millisecond)
		if err != nil {
			return nil, nil, 0, 0, err
		}
		return grayImage(fr), fr.Pix, fr.W, fr.H, nil
	}
	if u.hdr != nil && u.hdr.dev != nil {
		jpg, err := u.hdr.dev.LiveView()
		if err != nil {
			return nil, nil, 0, 0, err
		}
		im, err := jpeg.Decode(bytes.NewReader(jpg))
		if err != nil {
			return nil, nil, 0, 0, err
		}
		g := imageToGray16(im)
		b := g.Bounds()
		return g, gray16Pix(g), b.Dx(), b.Dy(), nil
	}
	return nil, nil, 0, 0, fmt.Errorf("no camera")
}

func (u *FocusUI) applyAE(st hdrscan.ExposureState) {
	if u.mono != nil && u.mono.cam != nil {
		cur := u.mono.cam.Settings()
		if math.Abs(cur.ExposureUS-st.ExposureUS)/math.Max(cur.ExposureUS, 1) < 0.04 && math.Abs(cur.GainDB-st.GainDB) < 0.15 {
			return
		}
		cur.ExposureUS = st.ExposureUS
		cur.GainDB = st.GainDB
		_ = u.mono.cam.Apply(cur)
		return
	}
	if u.hdr == nil || u.hdr.dev == nil {
		return
	}
	// Map the suggested microseconds onto the nearest Fujifilm shutter step.
	us := uint32(math.Max(1, st.ExposureUS))
	i := camera.ShutterIndex(us)
	next := camera.Exposure{ISO: 160, Shutter: camera.Shutters[i].Value}
	if u.haveSavedFuji {
		next.ISO = u.savedFuji.ISO
	}
	cur, err := u.hdr.dev.Exposure()
	if err == nil && cur.Shutter == next.Shutter && cur.ISO == next.ISO {
		return
	}
	_, _ = u.hdr.dev.SetExposure(next)
}

func (u *FocusUI) ui(f func()) {
	if u.hdr != nil {
		u.hdr.ui(f)
	}
}

func (u *FocusUI) showImage(img image.Image) {
	if img == nil || u.view == nil {
		return
	}
	b := img.Bounds()
	u.srcW, u.srcH = b.Dx(), b.Dy()
	bmp, err := walk.NewBitmapFromImage(img)
	if err != nil {
		return
	}
	if u.bmp != nil {
		u.bmp.Dispose()
	}
	u.bmp = bmp
	u.view.Invalidate()
}

func (u *FocusUI) zoomFactor() int {
	i := 0
	if u.zoomCB != nil {
		i = u.zoomCB.CurrentIndex()
	}
	if i < 0 || i >= len(zoomVals) {
		return 0
	}
	return zoomVals[i]
}

func (u *FocusUI) visibleRect(imgW, imgH, widgetW, widgetH int) (x0, y0, x1, y1 int) {
	if imgW <= 0 || imgH <= 0 {
		return 0, 0, 0, 0
	}
	z := u.zoomFactor()
	if z == 0 {
		return 0, 0, imgW, imgH
	}
	// At N00 %, N image pixels cover one widget pixel, so the visible
	// source size is widget/N. We use a stand-in widget size when called
	// from the grab loop (the real widget size is used when painting).
	if u.view != nil {
		cb := u.view.ClientBoundsPixels()
		if cb.Width > 8 && cb.Height > 8 {
			widgetW, widgetH = cb.Width, cb.Height
		}
	}
	vw := widgetW / z
	vh := widgetH / z
	if vw < 16 {
		vw = 16
	}
	if vh < 16 {
		vh = 16
	}
	if vw > imgW {
		vw = imgW
	}
	if vh > imgH {
		vh = imgH
	}
	cx := int(u.cx * float64(imgW))
	cy := int(u.cy * float64(imgH))
	x0 = cx - vw/2
	y0 = cy - vh/2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x0+vw > imgW {
		x0 = imgW - vw
	}
	if y0+vh > imgH {
		y0 = imgH - vh
	}
	return x0, y0, x0 + vw, y0 + vh
}

func (u *FocusUI) paint(c *walk.Canvas, _ walk.Rectangle) error {
	if u.view == nil {
		return nil
	}
	cb := u.view.ClientBoundsPixels()
	bg, _ := walk.NewSolidColorBrush(walk.RGB(16, 16, 18))
	if bg != nil {
		defer bg.Dispose()
		c.FillRectanglePixels(bg, cb)
	}
	if u.bmp == nil {
		f, _ := walk.NewFont("Segoe UI", 10, 0)
		if f != nil {
			defer f.Dispose()
			c.DrawTextPixels("No live image.\r\nConnect the Hikrobot camera on Mono trichrome, or the scan camera on Camera, then open this page.",
				f, walk.RGB(128, 128, 136), cb, walk.TextCenter|walk.TextWordbreak)
		}
		return nil
	}
	sz := u.bmp.Size()
	z := u.zoomFactor()
	var src, dst walk.Rectangle
	if z == 0 {
		dst = letterbox(cb, sz.Width, sz.Height)
		src = walk.Rectangle{Width: sz.Width, Height: sz.Height}
	} else {
		x0, y0, x1, y1 := u.visibleRect(sz.Width, sz.Height, cb.Width, cb.Height)
		src = walk.Rectangle{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
		dw, dh := src.Width*z, src.Height*z
		dst = walk.Rectangle{X: cb.X + (cb.Width-dw)/2, Y: cb.Y + (cb.Height-dh)/2, Width: dw, Height: dh}
	}
	_ = src
	// walk.DrawImageStretchedPixels draws the whole bitmap into dst; for a
	// zoomed crop we stretch the full bitmap into a larger rectangle offset
	// so the visible crop lands in the widget.
	if z == 0 {
		c.DrawImageStretchedPixels(u.bmp, dst)
	} else {
		full := walk.Rectangle{
			X:      dst.X - src.X*z,
			Y:      dst.Y - src.Y*z,
			Width:  sz.Width * z,
			Height: sz.Height * z,
		}
		c.DrawImageStretchedPixels(u.bmp, full)
	}
	// Crosshair at the centre to mark the focus sample.
	penB, _ := walk.NewSolidColorBrush(walk.RGB(80, 220, 120))
	if penB != nil {
		defer penB.Dispose()
		pen, _ := walk.NewGeometricPen(walk.PenSolid, 1, penB)
		if pen != nil {
			defer pen.Dispose()
			cx := cb.X + cb.Width/2
			cy := cb.Y + cb.Height/2
			c.DrawLinePixels(pen, walk.Point{X: cx - 18, Y: cy}, walk.Point{X: cx + 18, Y: cy})
			c.DrawLinePixels(pen, walk.Point{X: cx, Y: cy - 18}, walk.Point{X: cx, Y: cy + 18})
		}
	}
	return nil
}

func (u *FocusUI) click(x, y int, b walk.MouseButton) {
	if b != walk.LeftButton || u.srcW == 0 || u.view == nil {
		return
	}
	cb := u.view.ClientBoundsPixels()
	z := u.zoomFactor()
	var ix, iy float64
	if z == 0 {
		ir := letterbox(cb, u.srcW, u.srcH)
		if ir.Width <= 0 || ir.Height <= 0 {
			return
		}
		ix = float64(x-ir.X) / float64(ir.Width)
		iy = float64(y-ir.Y) / float64(ir.Height)
		if u.zoomCB != nil {
			u.zoomCB.SetCurrentIndex(2) // 200 %
		}
	} else {
		x0, y0, x1, y1 := u.visibleRect(u.srcW, u.srcH, cb.Width, cb.Height)
		ix = (float64(x0) + float64(x-cb.X)/float64(z)) / float64(u.srcW)
		iy = (float64(y0) + float64(y-cb.Y)/float64(z)) / float64(u.srcH)
		_ = x1
		_ = y1
	}
	if ix < 0 {
		ix = 0
	}
	if iy < 0 {
		iy = 0
	}
	if ix > 1 {
		ix = 1
	}
	if iy > 1 {
		iy = 1
	}
	u.cx, u.cy = ix, iy
	u.drag = true
	u.dragX, u.dragY = x, y
	if u.view != nil {
		u.view.Invalidate()
	}
}

func (u *FocusUI) move(x, y int, b walk.MouseButton) {
	if !u.drag || b != walk.LeftButton || u.zoomFactor() == 0 || u.srcW == 0 {
		return
	}
	z := float64(u.zoomFactor())
	u.cx -= float64(x-u.dragX) / z / float64(u.srcW)
	u.cy -= float64(y-u.dragY) / z / float64(u.srcH)
	if u.cx < 0 {
		u.cx = 0
	}
	if u.cy < 0 {
		u.cy = 0
	}
	if u.cx > 1 {
		u.cx = 1
	}
	if u.cy > 1 {
		u.cy = 1
	}
	u.dragX, u.dragY = x, y
	if u.view != nil {
		u.view.Invalidate()
	}
}

func (u *FocusUI) up(_ int, _ int, _ walk.MouseButton) { u.drag = false }

func imageToGray16(src image.Image) *image.Gray16 {
	b := src.Bounds()
	dst := image.NewGray16(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}

func gray16Pix(g *image.Gray16) []uint16 {
	n := g.Bounds().Dx() * g.Bounds().Dy()
	out := make([]uint16, n)
	i := 0
	for y := g.Bounds().Min.Y; y < g.Bounds().Max.Y; y++ {
		for x := g.Bounds().Min.X; x < g.Bounds().Max.X; x++ {
			r, _, _, _ := g.At(x, y).RGBA()
			out[i] = uint16(r)
			i++
		}
	}
	return out
}

func (u *FocusUI) Page() TabPage {
	grey := walk.RGB(110, 110, 110)
	bold := Font{Family: "Segoe UI", PointSize: 9, Bold: true}
	return TabPage{
		Title:  " Focus check ",
		Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
		Children: []Widget{
			Composite{StretchFactor: 3, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				CustomWidget{
					AssignTo:            &u.view,
					Paint:               u.paint,
					PaintMode:           PaintBuffered,
					InvalidatesOnResize: true,
					MinSize:             Size{Width: 420, Height: 280},
					StretchFactor:       4,
					OnMouseDown:         u.click,
					OnMouseMove:         u.move,
					OnMouseUp:           u.up,
				},
				Label{Text: "Click to zoom on a region (jumps to 200 %). Drag to pan when zoomed. Green crosshair: the area used for exposure and the sharpness meter.", TextColor: grey},
			}},
			Composite{StretchFactor: 2, MinSize: Size{Width: 340}, MaxSize: Size{Width: 420}, Layout: VBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
				Card{
					Title:  "Focus check",
					Icon:   iconHDR,
					Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 4, Right: 8, Bottom: 6}},
					Children: []Widget{
						Label{AssignTo: &u.srcLbl, Text: "Camera: -", Font: bold},
						Label{AssignTo: &u.lightLbl, Text: "Light: -"},
						Label{AssignTo: &u.expLbl, Text: "Auto exposure: -"},
						Composite{Layout: Grid{Columns: 2, MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Zoom", MinSize: Size{Width: 80}},
							ComboBox{AssignTo: &u.zoomCB, Model: zoomItems, CurrentIndex: 0, OnCurrentIndexChanged: func() {
								if u.view != nil {
									u.view.Invalidate()
								}
							}},
						}},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
							PushButton{AssignTo: &u.greenBtn, Text: "Green only", OnClicked: func() { u.setLight(0) },
								ToolTipText: "Trichrome focus baseline: only the green LED at the scan brightness."},
							PushButton{AssignTo: &u.whiteBtn, Text: "White", OnClicked: func() { u.setLight(1) },
								ToolTipText: "All three LEDs on, for focusing a white-light scan."},
						}},
						Label{Text: "Sharpness (zoomed region)", Font: bold},
						ProgressBar{AssignTo: &u.meter, MaxSize: Size{Height: 14}},
						Label{AssignTo: &u.sharpLbl, Text: "Sharpness: -"},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
							Label{AssignTo: &u.peakLbl, Text: "Peak hold: -", StretchFactor: 1},
							PushButton{AssignTo: &u.resetPeakBtn, Text: "Reset peak", OnClicked: func() {
								u.peak.Reset()
								u.updateLabels()
							}},
						}},
						Label{Text: "While this page is open the camera exposure is adjusted automatically so the visible (or zoomed) region is neither clipped nor too dark, and film grain can be judged. The capture exposure used on the scan pages is restored when you leave.", TextColor: grey},
						TextEdit{AssignTo: &u.logTE, ReadOnly: true, VScroll: true, MinSize: Size{Height: 120}, StretchFactor: 1},
					},
				},
				VSpacer{},
			}},
		},
	}
}

func (u *FocusUI) AfterCreate() {
	u.say(appVersion + " Focus check ready. Green is the focus baseline for trichrome; white is for white-light scans.")
	u.updateLabels()
}

var _ = color.Gray{}
