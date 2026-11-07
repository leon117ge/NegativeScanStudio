package main

// v0.71 Develop preview: per-frame orientation (rotate / flip) and
// Resolve-style navigation (wheel zoom at the cursor, middle / Space+left
// drag pan, Fit, 100%). When the view is magnified past the preview proxy,
// the visible region is rendered from the full-resolution frame.

import (
	"image"
	"math"
	"path/filepath"
	"sync/atomic"
	"time"

	"basetool/develop"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

func (u *DevUI) curOrient() develop.Orient {
	if u.pPath == "" {
		return 0
	}
	return u.orient[filepath.Base(u.pPath)]
}

// orientMap is the preset form of the per-frame orientations.
func (u *DevUI) orientMap() map[string]int {
	if len(u.orient) == 0 {
		return nil
	}
	m := map[string]int{}
	for k, v := range u.orient {
		if v != 0 {
			m[k] = int(v)
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// frameDims is the oriented full-resolution frame shown by the preview:
// the proxy-covered part of the scan (pw0*pk x ph0*pk; at most pk-1
// pixels of the right / bottom edge are not shown in the preview).
func (u *DevUI) frameDims() (int, int) {
	return u.curOrient().Dims(u.pw0*u.pk, u.ph0*u.pk)
}

// applyOrient rebuilds the oriented proxy of the current frame and renders.
func (u *DevUI) applyOrient() {
	o := u.curOrient()
	u.pix, u.pw, u.ph = develop.OrientF32(u.pix0, u.pw0, u.ph0, o)
	iw, ih := u.frameDims()
	if !u.vw.Fit && (iw != u.vwDim[0] || ih != u.vwDim[1]) {
		// a frame of another size: keep the zoom, look at its centre
		u.vw.CX, u.vw.CY = float64(iw)/2, float64(ih)/2
	}
	u.vwDim = [2]int{iw, ih}
	if u.orientLbl != nil {
		u.orientLbl.SetText(o.String())
	}
	u.updateZoomLbl()
}

// orientOp rotates / flips the current frame (-1 = reset).
func (u *DevUI) orientOp(op int) {
	defer u.guard("orientation")
	if u.pPath == "" || u.pix0 == nil {
		u.status.SetText("Open a TIFF first.")
		return
	}
	if u.orient == nil {
		u.orient = map[string]develop.Orient{}
	}
	name := filepath.Base(u.pPath)
	o := u.orient[name]
	if op < 0 {
		o = 0
	} else {
		o = o.Apply(op)
	}
	if o == 0 {
		delete(u.orient, name)
	} else {
		u.orient[name] = o
	}
	u.vw.Fit = true
	u.applyOrient()
	u.log(name + ": orientation " + o.String() + " (preview and export; Save to roll folder keeps it)")
	u.changed()
}

// ---------- view transform ----------

func (u *DevUI) viewT() develop.View {
	v := u.vw
	cb := u.view.ClientBoundsPixels()
	v.VX, v.VY, v.VW, v.VH = cb.X, cb.Y, cb.Width, cb.Height
	v.Margin = px(8, dpiOf(u.view))
	v.IW, v.IH = u.frameDims()
	return v
}

func (u *DevUI) setView(v develop.View) {
	u.vw.Fit, u.vw.Zoom, u.vw.CX, u.vw.CY = v.Fit, v.Zoom, v.CX, v.CY
	u.updateZoomLbl()
	u.view.Invalidate()
	u.scheduleHi(120 * time.Millisecond)
}

func (u *DevUI) updateZoomLbl() {
	if u.zoomLbl == nil || u.view == nil {
		return
	}
	if u.pw0 == 0 {
		u.zoomLbl.SetText("Fit")
		return
	}
	u.zoomLbl.SetText(u.viewT().Label())
}

func (u *DevUI) zoomFit() {
	v := u.viewT()
	v.Fit = true
	u.setView(v)
}

func (u *DevUI) zoom100() {
	if u.pw0 == 0 {
		return
	}
	v := u.viewT()
	v.SetZoom(1)
	u.setView(v)
}

func rectF(x0, y0, x1, y1 float64) walk.Rectangle {
	X0, Y0 := int(math.Round(x0)), int(math.Round(y0))
	return walk.Rectangle{X: X0, Y: Y0, Width: int(math.Round(x1)) - X0, Height: int(math.Round(y1)) - Y0}
}

func (u *DevUI) paintView(c *walk.Canvas, _ walk.Rectangle) error {
	cb := u.view.ClientBoundsPixels()
	fill(c, walk.RGB(18, 18, 20), cb)
	u.fonts()
	if u.bmp == nil || u.pw == 0 {
		msg := "Drop an HDR TIFF from module 3 or a roll folder here, or use Open.\r\n\r\nThe negative is developed with the node chain on the right:\r\n1-3 are the three FilmScan DCTLs (exact port)."
		if u.font != nil {
			c.DrawTextPixels(msg, u.font, cDim, cb, walk.TextCenter|walk.TextVCenter|walk.TextWordbreak)
		}
		return nil
	}
	v := u.viewT()
	k := float64(u.pk)
	// proxy: only the visible proxy pixels, each mapped exactly
	x0, y0, x1, y1 := v.Visible()
	if x1 > x0 && y1 > y0 {
		px0, py0 := x0/u.pk, y0/u.pk
		px1, py1 := min(u.pw, (x1+u.pk-1)/u.pk), min(u.ph, (y1+u.pk-1)/u.pk)
		ax, ay := v.ToScreen(float64(px0)*k, float64(py0)*k)
		bx, by := v.ToScreen(float64(px1)*k, float64(py1)*k)
		if u.bmp.Size().Width == u.pw && u.bmp.Size().Height == u.ph {
			c.DrawBitmapPartWithOpacityPixels(u.bmp, rectF(ax, ay, bx, by), walk.Rectangle{X: px0, Y: py0, Width: px1 - px0, Height: py1 - py0}, 0xff)
		}
	}
	// full-resolution region on top (same content only)
	if u.hiBmp != nil && u.hiFor == atomic.LoadInt64(&u.content) {
		r := u.hiRect
		ax, ay := v.ToScreen(float64(r[0]), float64(r[1]))
		bx, by := v.ToScreen(float64(r[0]+r[2]), float64(r[1]+r[3]))
		sz := u.hiBmp.Size()
		c.DrawBitmapPartWithOpacityPixels(u.hiBmp, rectF(ax, ay, bx, by), walk.Rectangle{Width: sz.Width, Height: sz.Height}, 0xff)
	}
	ix0, iy0 := v.ToScreen(0, 0)
	ix1, iy1 := v.ToScreen(float64(v.IW), float64(v.IH))
	ir := rectF(math.Max(ix0, float64(cb.X)), math.Max(iy0, float64(cb.Y)), math.Min(ix1, float64(cb.X+cb.Width)), math.Min(iy1, float64(cb.Y+cb.Height)))
	if u.cmpCB.CurrentIndex() == 2 {
		sx, _ := v.ToScreen(float64(v.IW)/2, 0)
		x := int(math.Round(sx))
		if x >= cb.X && x < cb.X+cb.Width {
			fill(c, cAccent, walk.Rectangle{X: x, Y: ir.Y, Width: max(1, px(1, dpiOf(u.view))), Height: ir.Height})
			if u.font != nil {
				c.DrawTextPixels("Before", u.font, cText, walk.Rectangle{X: x - 126, Y: ir.Y + 4, Width: 120, Height: 20}, walk.TextRight)
				c.DrawTextPixels("After", u.font, cText, walk.Rectangle{X: x + 6, Y: ir.Y + 4, Width: 120, Height: 20}, walk.TextLeft)
			}
		}
	}
	if u.pick != 0 && u.fontB != nil {
		t := map[int]string{1: "Click the film base (unexposed rebate) to pick it", 2: "Click a neutral grey / white point"}[u.pick]
		c.DrawTextPixels(t, u.fontB, cAccent, walk.Rectangle{X: ir.X, Y: ir.Y + ir.Height - 24, Width: ir.Width, Height: 22}, walk.TextCenter)
	}
	return nil
}

// sample averages raw scan values in a 7x7 block of the (oriented) proxy
// around a click; the click goes through the view (zoom / pan) and the
// orientation, so it reads the scan pixels shown under the cursor.
func (u *DevUI) sample(x, y int) (r, g, b float32, ok bool) {
	if u.pw == 0 {
		return
	}
	v := u.viewT()
	ix, iy := v.ToImage(float64(x)+0.5, float64(y)+0.5)
	if ix < 0 || iy < 0 || ix >= float64(v.IW) || iy >= float64(v.IH) {
		return
	}
	px0, py0 := int(ix)/u.pk, int(iy)/u.pk
	var s [3]float64
	n := 0
	for yy := py0 - 3; yy <= py0+3; yy++ {
		for xx := px0 - 3; xx <= px0+3; xx++ {
			if xx < 0 || yy < 0 || xx >= u.pw || yy >= u.ph {
				continue
			}
			i := (yy*u.pw + xx) * 3
			s[0] += float64(u.pix[i])
			s[1] += float64(u.pix[i+1])
			s[2] += float64(u.pix[i+2])
			n++
		}
	}
	if n == 0 {
		return
	}
	return float32(s[0] / float64(n)), float32(s[1] / float64(n)), float32(s[2] / float64(n)), true
}

// ---------- full-resolution region ----------

// scheduleHi renders the visible region from the full-resolution frame
// after a short pause (view changes come in bursts).
func (u *DevUI) scheduleHi(d time.Duration) {
	if u.hiTimer != nil {
		u.hiTimer.Stop()
	}
	if d <= 0 {
		d = 30 * time.Millisecond
	}
	u.hiTimer = time.AfterFunc(d, func() { u.ui(u.renderHi) })
}

func (u *DevUI) renderHi() {
	if u.full0 == nil || u.pix == nil || u.view == nil {
		return
	}
	v := u.viewT()
	s := v.Scale()
	if s <= 1.02/float64(u.pk) { // the proxy is sharp enough
		atomic.AddInt64(&u.hgen, 1)
		return
	}
	x0, y0, x1, y1 := v.Visible()
	k := max(1, int(math.Floor(1/s)))
	x0, y0 = x0/k*k, y0/k*k
	n, m := (x1-x0+k-1)/k, (y1-y0+k-1)/k
	for x0+n*k > v.IW {
		n--
	}
	for y0+m*k > v.IH {
		m--
	}
	if n <= 0 || m <= 0 {
		return
	}
	gen := atomic.AddInt64(&u.hgen, 1)
	content := atomic.LoadInt64(&u.content)
	chain, cx, upto := u.chain.Clone(), u.cx, u.viewUpto()
	cmp, disp := u.cmpCB.CurrentIndex(), u.dispCB.CurrentIndex()
	full, sw, cw, ch, o, iw := u.full0, u.fW, u.pw0*u.pk, u.ph0*u.pk, u.curOrient(), v.IW
	go func() {
		defer u.guard("full-resolution preview")
		src := develop.RegionF32(full, sw, cw, ch, o, x0, y0, n, m, k)
		if atomic.LoadInt64(&u.hgen) != gen {
			return
		}
		prog := chain.Compile(cx, upto)
		out := make([]float32, len(src))
		copy(out, src)
		if cmp != 1 {
			prog.Run(out)
			if disp == 0 {
				if vf := develop.ViewFunc(prog.Domain); vf != nil {
					runPix(out, vf)
				}
			}
		}
		rgba := make([]byte, n*m*4)
		b8 := func(v float32) byte {
			if !(v > 0) {
				return 0
			}
			if v >= 1 {
				return 255
			}
			return byte(v*255 + 0.5)
		}
		for j := 0; j < m; j++ {
			for i := 0; i < n; i++ {
				p := (j*n + i) * 3
				r, g, b := out[p], out[p+1], out[p+2]
				if cmp == 1 || (cmp == 2 && x0+i*k+k/2 < iw/2) {
					r, g, b = develop.Encode(develop.DomRec709, src[p]), develop.Encode(develop.DomRec709, src[p+1]), develop.Encode(develop.DomRec709, src[p+2])
				}
				q := (j*n + i) * 4
				rgba[q], rgba[q+1], rgba[q+2], rgba[q+3] = b8(r), b8(g), b8(b), 255
			}
		}
		u.ui(func() {
			if atomic.LoadInt64(&u.hgen) != gen || atomic.LoadInt64(&u.content) != content {
				return
			}
			img := &image.RGBA{Pix: rgba, Stride: 4 * n, Rect: image.Rect(0, 0, n, m)}
			if bm, err := walk.NewBitmapFromImageForDPI(img, 96); err == nil {
				if u.hiBmp != nil {
					u.hiBmp.Dispose()
				}
				u.hiBmp, u.hiRect, u.hiFor = bm, [4]int{x0, y0, n * k, m * k}, content
			}
			u.view.Invalidate()
		})
	}()
}

// ---------- mouse ----------

func (u *DevUI) navInit() {
	u.vw.Fit = true
	u.view.MouseWheel().Attach(u.navWheel)
	u.view.SizeChanged().Attach(func() {
		u.updateZoomLbl()
		u.scheduleHi(150 * time.Millisecond)
	})
}

func (u *DevUI) navWheel(x, y int, b walk.MouseButton) {
	defer u.guard("zoom")
	if u.pw0 == 0 {
		return
	}
	delta := float64(int16(uint32(b) >> 16))
	// WM_MOUSEWHEEL carries screen coordinates
	pt := win.POINT{X: int32(x), Y: int32(y)}
	win.ScreenToClient(u.view.Handle(), &pt)
	v := u.viewT()
	v.ZoomAt(v.Scale()*math.Pow(1.25, delta/120), float64(pt.X), float64(pt.Y))
	u.setView(v)
}

// navDown starts a pan (middle button, or Space + left) or fits on a
// middle double-click; true = the click was used for navigation.
func (u *DevUI) navDown(x, y int, btn walk.MouseButton) bool {
	if u.view != nil {
		u.view.SetFocus() // wheel and Space go to the preview
	}
	if u.pw0 == 0 {
		return false
	}
	space := win.GetKeyState(win.VK_SPACE) < 0
	if btn == walk.MiddleButton {
		if time.Since(u.lastMid) < 500*time.Millisecond && max(x-u.lastMidX, u.lastMidX-x) < 6 && max(y-u.lastMidY, u.lastMidY-y) < 6 {
			u.lastMid = time.Time{}
			u.zoomFit()
			return true
		}
		u.lastMid, u.lastMidX, u.lastMidY = time.Now(), x, y
		win.SetCapture(u.view.Handle())
	} else if !(btn == walk.LeftButton && space) {
		return false
	}
	v := u.viewT()
	u.pan = true
	u.panIX, u.panIY = v.ToImage(float64(x), float64(y))
	u.view.SetCursor(walk.CursorSizeAll())
	return true
}

func (u *DevUI) navMove(x, y int, btn walk.MouseButton) {
	if !u.pan {
		return
	}
	if btn&(walk.MiddleButton|walk.LeftButton) == 0 {
		u.navUp(x, y, btn)
		return
	}
	v := u.viewT()
	v.PanTo(u.panIX, u.panIY, float64(x), float64(y))
	u.setView(v)
}

func (u *DevUI) navUp(x, y int, btn walk.MouseButton) {
	if !u.pan {
		return
	}
	u.pan = false
	win.ReleaseCapture()
	u.view.SetCursor(map[bool]walk.Cursor{true: walk.CursorCross(), false: walk.CursorArrow()}[u.pick != 0])
}
