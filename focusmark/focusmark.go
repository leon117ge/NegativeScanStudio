// Package focusmark draws a display-only focus / field-flatness overlay:
// a high-contrast crosshair at the optical centre of the sensor image,
// plus small brackets at the four corners and edge midpoints (10% inset).
package focusmark

import (
	"image"
	"image/color"
	"math"
)

// Inset is the fraction of width/height from each edge for corner and
// edge-midpoint marks (10%).
const Inset = 0.10

// Colours: a 1-pixel light line with a 1-pixel dark outline stays visible
// on both dark base and bright film.
var (
	Inner = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	Outer = color.RGBA{R: 0, G: 0, B: 0, A: 255}
)

const (
	centerArm = 28 // screen pixels
	centerGap = 4
	centerBox = 7
	cornerArm = 14
	edgeArm   = 10
)

// View maps sensor-image pixels onto the widget rectangle where that
// image (or a zoomed crop of it) is drawn.
type View struct {
	ImgW, ImgH             int
	SrcX, SrcY, SrcW, SrcH int
	DstX, DstY, DstW, DstH int
}

// Fit returns a letterboxed full-frame view of the image in a widget.
func Fit(widgetX, widgetY, widgetW, widgetH, imgW, imgH int) View {
	dx, dy, dw, dh := letterbox(widgetX, widgetY, widgetW, widgetH, imgW, imgH)
	return View{
		ImgW: imgW, ImgH: imgH,
		SrcX: 0, SrcY: 0, SrcW: imgW, SrcH: imgH,
		DstX: dx, DstY: dy, DstW: dw, DstH: dh,
	}
}

func letterbox(x, y, cw, ch, iw, ih int) (dx, dy, dw, dh int) {
	if iw <= 0 || ih <= 0 || cw <= 0 || ch <= 0 {
		return x, y, cw, ch
	}
	s := float64(cw) / float64(iw)
	if sy := float64(ch) / float64(ih); sy < s {
		s = sy
	}
	dw = int(float64(iw) * s)
	dh = int(float64(ih) * s)
	return x + (cw-dw)/2, y + (ch-dh)/2, dw, dh
}

// Map converts an image-pixel coordinate to widget pixels. ok is false
// when the point lies outside the visible source crop.
func (v View) Map(ix, iy float64) (x, y float64, ok bool) {
	if v.SrcW <= 0 || v.SrcH <= 0 || v.ImgW <= 0 || v.ImgH <= 0 {
		return 0, 0, false
	}
	if ix < float64(v.SrcX) || iy < float64(v.SrcY) ||
		ix > float64(v.SrcX+v.SrcW) || iy > float64(v.SrcY+v.SrcH) {
		return 0, 0, false
	}
	x = float64(v.DstX) + (ix-float64(v.SrcX))*float64(v.DstW)/float64(v.SrcW)
	y = float64(v.DstY) + (iy-float64(v.SrcY))*float64(v.DstH)/float64(v.SrcH)
	return x, y, true
}

// Centre is the exact optical centre of a sensor image of size imgW x imgH.
func Centre(imgW, imgH int) (float64, float64) {
	return float64(imgW) / 2, float64(imgH) / 2
}

// Kind is the shape drawn at a mark.
type Kind int

const (
	KindCentre Kind = iota
	KindCorner
	KindEdge
)

// Mark is one overlay point in image pixels.
type Mark struct {
	IX, IY   float64
	Kind     Kind
	InX, InY int // for corners: +1/-1 pointing toward the centre
}

// Marks returns the centre, four 10%-inset corners, and four edge midpoints.
func Marks(imgW, imgH int) []Mark {
	if imgW <= 0 || imgH <= 0 {
		return nil
	}
	cx, cy := Centre(imgW, imgH)
	x0 := Inset * float64(imgW)
	x1 := (1 - Inset) * float64(imgW)
	y0 := Inset * float64(imgH)
	y1 := (1 - Inset) * float64(imgH)
	return []Mark{
		{IX: cx, IY: cy, Kind: KindCentre},
		{IX: x0, IY: y0, Kind: KindCorner, InX: 1, InY: 1},
		{IX: x1, IY: y0, Kind: KindCorner, InX: -1, InY: 1},
		{IX: x0, IY: y1, Kind: KindCorner, InX: 1, InY: -1},
		{IX: x1, IY: y1, Kind: KindCorner, InX: -1, InY: -1},
		{IX: cx, IY: y0, Kind: KindEdge, InX: 0, InY: 1},
		{IX: x1, IY: cy, Kind: KindEdge, InX: -1, InY: 0},
		{IX: cx, IY: y1, Kind: KindEdge, InX: 0, InY: -1},
		{IX: x0, IY: cy, Kind: KindEdge, InX: 1, InY: 0},
	}
}

// Draw paints the overlay onto dst. Lines are clipped to the destination
// image rectangle (the letterboxed or zoomed picture), never to the
// surrounding widget background.
func Draw(dst *image.RGBA, v View) {
	if dst == nil {
		return
	}
	clip := image.Rect(v.DstX, v.DstY, v.DstX+v.DstW, v.DstY+v.DstH).Intersect(dst.Bounds())
	if clip.Empty() {
		return
	}
	for _, m := range Marks(v.ImgW, v.ImgH) {
		x, y, ok := v.Map(m.IX, m.IY)
		if !ok {
			continue
		}
		px, py := int(math.Round(x)), int(math.Round(y))
		switch m.Kind {
		case KindCentre:
			drawCentre(dst, clip, px, py)
		case KindCorner:
			drawCorner(dst, clip, px, py, m.InX, m.InY)
		case KindEdge:
			drawEdge(dst, clip, px, py, m.InX, m.InY)
		}
	}
}

func drawCentre(dst *image.RGBA, clip image.Rectangle, x, y int) {
	// Outlined cross with a gap at the exact centre, plus a small box so
	// the point stays readable next to a short solid meter crosshair.
	g := centerGap
	a := centerArm
	hLine(dst, clip, x-a, x-g, y)
	hLine(dst, clip, x+g, x+a, y)
	vLine(dst, clip, x, y-a, y-g)
	vLine(dst, clip, x, y+g, y+a)
	half := centerBox / 2
	rect(dst, clip, x-half, y-half, x+half, y+half)
}

func drawCorner(dst *image.RGBA, clip image.Rectangle, x, y, inX, inY int) {
	hLine(dst, clip, x, x+inX*cornerArm, y)
	vLine(dst, clip, x, y, y+inY*cornerArm)
}

func drawEdge(dst *image.RGBA, clip image.Rectangle, x, y, inX, inY int) {
	if inX == 0 {
		hLine(dst, clip, x-edgeArm, x+edgeArm, y)
		vLine(dst, clip, x, y, y+inY*edgeArm)
	} else {
		vLine(dst, clip, x, y-edgeArm, y+edgeArm)
		hLine(dst, clip, x, x+inX*edgeArm, y)
	}
}

func hLine(dst *image.RGBA, clip image.Rectangle, x0, x1, y int) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	// Dark outline above and below, light 1 px stroke.
	for x := x0; x <= x1; x++ {
		setClip(dst, clip, x, y-1, Outer)
		setClip(dst, clip, x, y+1, Outer)
	}
	for x := x0; x <= x1; x++ {
		setClip(dst, clip, x, y, Inner)
	}
}

func vLine(dst *image.RGBA, clip image.Rectangle, x, y0, y1 int) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		setClip(dst, clip, x-1, y, Outer)
		setClip(dst, clip, x+1, y, Outer)
	}
	for y := y0; y <= y1; y++ {
		setClip(dst, clip, x, y, Inner)
	}
}

func rect(dst *image.RGBA, clip image.Rectangle, x0, y0, x1, y1 int) {
	hLine(dst, clip, x0, x1, y0)
	hLine(dst, clip, x0, x1, y1)
	vLine(dst, clip, x0, y0, y1)
	vLine(dst, clip, x1, y0, y1)
}

func setClip(dst *image.RGBA, clip image.Rectangle, x, y int, c color.RGBA) {
	if !image.Pt(x, y).In(clip) {
		return
	}
	dst.SetRGBA(x, y, c)
}

// GrayToRGBA copies an 8-bit grey image into a new RGBA buffer.
func GrayToRGBA(src *image.Gray) *image.RGBA {
	if src == nil {
		return nil
	}
	b := src.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := src.GrayAt(x, y).Y
			dst.SetRGBA(x, y, color.RGBA{R: g, G: g, B: g, A: 255})
		}
	}
	return dst
}

// Compose letterboxes src into a widget-sized RGBA and draws the overlay.
func Compose(src *image.Gray, widgetW, widgetH int) *image.RGBA {
	if src == nil || widgetW <= 0 || widgetH <= 0 {
		return nil
	}
	b := src.Bounds()
	iw, ih := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, widgetW, widgetH))
	// Dark widget background, same idea as the live-view paint.
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i+0] = 22
		dst.Pix[i+1] = 22
		dst.Pix[i+2] = 25
		dst.Pix[i+3] = 255
	}
	v := Fit(0, 0, widgetW, widgetH, iw, ih)
	// Nearest-neighbour scale of the grey frame into the letterbox.
	for y := 0; y < v.DstH; y++ {
		sy := b.Min.Y + y*ih/v.DstH
		if sy >= b.Max.Y {
			sy = b.Max.Y - 1
		}
		for x := 0; x < v.DstW; x++ {
			sx := b.Min.X + x*iw/v.DstW
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			g := src.GrayAt(sx, sy).Y
			dst.SetRGBA(v.DstX+x, v.DstY+y, color.RGBA{R: g, G: g, B: g, A: 255})
		}
	}
	Draw(dst, v)
	return dst
}

// ComposeCrop scales the source crop srcRect of src into dst bounds and
// draws the overlay, mapped to the full sensor image. Used to preview a
// zoomed / panned live view.
func ComposeCrop(src *image.Gray, imgW, imgH int, srcRect image.Rectangle, dstW, dstH int) *image.RGBA {
	if src == nil || dstW <= 0 || dstH <= 0 || srcRect.Empty() {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i+0] = 16
		dst.Pix[i+1] = 16
		dst.Pix[i+2] = 18
		dst.Pix[i+3] = 255
	}
	sw, sh := srcRect.Dx(), srcRect.Dy()
	v := View{
		ImgW: imgW, ImgH: imgH,
		SrcX: srcRect.Min.X, SrcY: srcRect.Min.Y, SrcW: sw, SrcH: sh,
		DstX: 0, DstY: 0, DstW: dstW, DstH: dstH,
	}
	for y := 0; y < dstH; y++ {
		sy := srcRect.Min.Y + y*sh/dstH
		if sy >= srcRect.Max.Y {
			sy = srcRect.Max.Y - 1
		}
		for x := 0; x < dstW; x++ {
			sx := srcRect.Min.X + x*sw/dstW
			if sx >= srcRect.Max.X {
				sx = srcRect.Max.X - 1
			}
			g := src.GrayAt(sx, sy).Y
			dst.SetRGBA(x, y, color.RGBA{R: g, G: g, B: g, A: 255})
		}
	}
	Draw(dst, v)
	return dst
}
