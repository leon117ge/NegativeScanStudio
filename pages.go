package main

// Resolve-style page switching: a stack of page composites, a bottom page bar
// with icon + label buttons, and a small segmented selector for sub-pages.

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"golang.org/x/image/vector"
)

type pager struct {
	pages    []*walk.Composite
	cur      int
	bars     []*navBar
	onChange func(int)
}

func newPager(n int) *pager { return &pager{pages: make([]*walk.Composite, n)} }

func (p *pager) CurrentIndex() int { return p.cur }

func (p *pager) SetCurrentIndex(i int) {
	if i < 0 || i >= len(p.pages) {
		return
	}
	p.cur = i
	for k, c := range p.pages {
		if c != nil && k != i {
			c.SetVisible(false)
		}
	}
	if c := p.pages[i]; c != nil {
		c.SetVisible(true)
		if par := c.Parent(); par != nil {
			par.RequestLayout()
		}
	}
	for _, b := range p.bars {
		if b.w != nil {
			b.w.Invalidate()
		}
	}
	if p.onChange != nil {
		p.onChange(i)
	}
}

// Stack turns tab pages into the page composites of p (only the first visible).
func (p *pager) Stack(tps ...TabPage) []Widget {
	var ws []Widget
	for i, tp := range tps {
		ws = append(ws, Composite{AssignTo: &p.pages[i], Visible: i == p.cur, StretchFactor: 100, Layout: tp.Layout, Children: tp.Children})
	}
	return ws
}

// ---------- nav bar ----------

type iconFunc func(z *vector.Rasterizer, s float32)

type navItem struct {
	label string
	icon  iconFunc
}

type navBar struct {
	pg      *pager
	items   []navItem
	segment bool // small segmented selector instead of the bottom page bar
	right   string
	w       *walk.CustomWidget
	hot     int
	cache   map[string]*walk.Bitmap
	font    *walk.Font
	fontDPI int
}

func newNavBar(pg *pager, segment bool, items ...navItem) *navBar {
	b := &navBar{pg: pg, items: items, segment: segment, hot: -1, cache: map[string]*walk.Bitmap{}}
	pg.bars = append(pg.bars, b)
	return b
}

func (b *navBar) Widget() Widget {
	h := 60
	if b.segment {
		h = 34
	}
	return CustomWidget{
		AssignTo:            &b.w,
		MinSize:             Size{Height: h},
		MaxSize:             Size{Height: h},
		PaintMode:           PaintBuffered,
		InvalidatesOnResize: true,
		PaintPixels:         b.paint,
		OnMouseDown: func(x, y int, btn walk.MouseButton) {
			if btn == walk.LeftButton {
				if i := b.hit(x, y); i >= 0 && i != b.pg.CurrentIndex() {
					b.pg.SetCurrentIndex(i)
				}
			}
		},
		OnMouseMove: func(x, y int, _ walk.MouseButton) {
			if i := b.hit(x, y); i != b.hot {
				b.hot = i
				b.w.Invalidate()
			}
			if b.w != nil {
				hoverLeave[b.w.Handle()] = func() {
					if b.hot != -1 {
						b.hot = -1
						b.w.Invalidate()
					}
				}
				subclass(b.w.Handle(), roleHover, b.w)
				trackLeave(b.w.Handle())
			}
		},
	}
}

func (b *navBar) dpi() int { return dpiOf(b.w) }

// rects returns the button rectangles in pixels.
func (b *navBar) rects() []walk.Rectangle {
	cb := b.w.ClientBoundsPixels()
	d := b.dpi()
	n := len(b.items)
	rs := make([]walk.Rectangle, n)
	if b.segment {
		sw, sh := px(150, d), px(26, d)
		x0 := px(6, d)
		y := (cb.Height - sh) / 2
		for i := range rs {
			rs[i] = walk.Rectangle{X: x0 + px(3, d) + i*sw, Y: y + px(3, d), Width: sw, Height: sh - px(6, d)}
		}
		return rs
	}
	bw := px(124, d)
	x0 := (cb.Width - n*bw) / 2
	for i := range rs {
		rs[i] = walk.Rectangle{X: x0 + i*bw, Y: px(5, d), Width: bw, Height: cb.Height - px(9, d)}
	}
	return rs
}

func (b *navBar) hit(x, y int) int {
	if b.w == nil {
		return -1
	}
	for i, r := range b.rects() {
		if x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
			return i
		}
	}
	return -1
}

func (b *navBar) getFont() *walk.Font {
	d := b.dpi()
	if b.font == nil || b.fontDPI != d {
		if b.segment {
			b.font, _ = walk.NewFont("Segoe UI", 9, 0)
		} else {
			b.font, _ = walk.NewFont("Segoe UI", 8, walk.FontBold)
		}
		b.fontDPI = d
	}
	return b.font
}

func (b *navBar) paint(c *walk.Canvas, _ walk.Rectangle) error {
	cb := b.w.ClientBoundsPixels()
	d := b.dpi()
	font := b.getFont()
	cur := b.pg.CurrentIndex()
	rs := b.rects()
	if b.segment {
		fill(c, bgAt(win.GetParent(b.w.Handle())), cb)
		all := walk.Rectangle{X: rs[0].X - px(3, d), Y: rs[0].Y - px(3, d), Width: len(rs)*rs[0].Width + px(6, d), Height: rs[0].Height + px(6, d)}
		roundBox(c, cField, cBorder, all, px(6, d))
		for i, it := range b.items {
			r := rs[i]
			txt := cDim
			switch {
			case i == cur:
				roundBox(c, walk.RGB(60, 60, 66), walk.RGB(74, 74, 80), r, px(4, d))
				txt = cAccent
			case i == b.hot:
				roundBox(c, walk.RGB(40, 40, 44), walk.RGB(40, 40, 44), r, px(4, d))
				txt = cText
			}
			c.DrawTextPixels(strings.ReplaceAll(it.label, "&", "&&"), font, txt, r, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis)
		}
		return nil
	}
	fill(c, cBar, cb)
	fill(c, cBorder, walk.Rectangle{Width: cb.Width, Height: 1})
	for i, it := range b.items {
		r := rs[i]
		col := walk.RGB(150, 150, 158)
		if i == b.hot {
			col = walk.RGB(220, 220, 226)
		}
		if i == cur {
			col = cAccent
			roundBox(c, walk.RGB(36, 36, 40), walk.RGB(36, 36, 40), r, px(6, d))
			fill(c, cAccent, walk.Rectangle{X: r.X + r.Width/2 - px(16, d), Y: 1, Width: px(32, d), Height: px(2, d)})
		}
		is := px(22, d)
		if bmp := b.icon(i, col, is, i == cur); bmp != nil {
			c.DrawImagePixels(bmp, walk.Point{X: r.X + (r.Width-is)/2, Y: r.Y + px(5, d)})
		}
		tr := walk.Rectangle{X: r.X, Y: r.Y + px(30, d), Width: r.Width, Height: px(16, d)}
		c.DrawTextPixels(strings.ReplaceAll(it.label, "&", "&&"), font, col, tr, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine)
	}
	if b.right != "" {
		f, _ := walk.NewFont("Segoe UI", 8, 0)
		if f != nil {
			c.DrawTextPixels(b.right, f, walk.RGB(100, 100, 108), walk.Rectangle{X: cb.Width - px(220, d), Y: 0, Width: px(206, d), Height: cb.Height}, walk.TextRight|walk.TextVCenter|walk.TextSingleLine)
			f.Dispose()
		}
	}
	return nil
}

func (b *navBar) icon(i int, col walk.Color, size int, active bool) *walk.Bitmap {
	bg := cBar
	if active {
		bg = walk.RGB(36, 36, 40)
	}
	key := fmt.Sprintf("%d/%x/%x/%d", i, uint32(col), uint32(bg), size)
	if bm, ok := b.cache[key]; ok {
		return bm
	}
	img := renderIcon(b.items[i].icon, size, col, bg)
	bm, err := walk.NewBitmapFromImageForDPI(img, b.dpi())
	if err != nil {
		return nil
	}
	b.cache[key] = bm
	return bm
}

func renderIcon(f iconFunc, size int, fg, bg walk.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{bg.R(), bg.G(), bg.B(), 255}), image.Point{}, draw.Src)
	z := vector.NewRasterizer(size, size)
	f(z, float32(size)/24)
	z.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{fg.R(), fg.G(), fg.B(), 255}), image.Point{})
	return img
}

// path helpers on a 24-unit grid; ccw cuts a hole.
func vRect(z *vector.Rasterizer, s, x0, y0, x1, y1 float32, ccw bool) {
	if ccw {
		x0, x1 = x1, x0
	}
	z.MoveTo(x0*s, y0*s)
	z.LineTo(x1*s, y0*s)
	z.LineTo(x1*s, y1*s)
	z.LineTo(x0*s, y1*s)
	z.ClosePath()
}

func vArc(z *vector.Rasterizer, s, cx, cy, r, a0, a1 float32, ccw bool, closeToCentre bool) {
	n := 48
	for k := 0; k <= n; k++ {
		t := float64(k) / float64(n)
		if ccw {
			t = 1 - t
		}
		a := float64(a0) + (float64(a1)-float64(a0))*t
		x := (cx + r*float32(math.Cos(a))) * s
		y := (cy + r*float32(math.Sin(a))) * s
		if k == 0 {
			if closeToCentre {
				z.MoveTo(cx*s, cy*s)
				z.LineTo(x, y)
			} else {
				z.MoveTo(x, y)
			}
		} else {
			z.LineTo(x, y)
		}
	}
	z.ClosePath()
}

// film strip with sprocket holes
func iconFilm(z *vector.Rasterizer, s float32) {
	vRect(z, s, 2.5, 4.5, 21.5, 19.5, false)
	vRect(z, s, 5, 8.5, 19, 15.5, true)
	for _, x := range []float32{4.2, 8.1, 12, 15.9, 19.8} {
		vRect(z, s, x-1, 5.8, x+1, 7.3, true)
		vRect(z, s, x-1, 16.7, x+1, 18.2, true)
	}
}

// balance: ring with the left half filled
func iconWB(z *vector.Rasterizer, s float32) {
	vArc(z, s, 12, 12, 9.5, 0, 2*math.Pi, false, false)
	vArc(z, s, 12, 12, 7.8, 0, 2*math.Pi, true, false)
	vArc(z, s, 12, 12, 6.2, math.Pi/2, 3*math.Pi/2, false, true)
	// small drop above (eyedropper tip hint): a dot at the right
	vArc(z, s, 15.2, 12, 1.4, 0, 2*math.Pi, false, false)
}

// stacked bracketed frames
func iconHDR(z *vector.Rasterizer, s float32) {
	vRect(z, s, 2.5, 9.5, 15.5, 21.5, false)
	vRect(z, s, 4.3, 11.3, 13.7, 19.7, true)
	vRect(z, s, 6, 6, 19, 7.6, false)
	vRect(z, s, 17.4, 7.6, 19, 18, false)
	vRect(z, s, 9.5, 2.5, 21.5, 4.1, false)
	vRect(z, s, 19.9, 4.1, 21.5, 14.5, false)
	// sun inside the front frame
	vArc(z, s, 9, 15.5, 2.2, 0, 2*math.Pi, false, false)
}

// crop corners: two interlocking L brackets
func iconCrop(z *vector.Rasterizer, s float32) {
	vRect(z, s, 5.5, 2, 7.6, 16.4, false)
	vRect(z, s, 5.5, 16.4, 22, 18.5, false)
	vRect(z, s, 2, 5.5, 16.4, 7.6, false)
	vRect(z, s, 16.4, 7.6, 18.5, 22, false)
	// small level mark through the middle (rotate / straighten)
	vRect(z, s, 9.6, 11.2, 14.4, 12.8, false)
}

// develop: a negative frame with an arrow to a filled positive frame
func iconConvert(z *vector.Rasterizer, s float32) { iconDevelop(z, s) }

func iconDevelop(z *vector.Rasterizer, s float32) {
	vRect(z, s, 2, 6, 10, 18, false)
	vRect(z, s, 3.6, 7.6, 8.4, 16.4, true)
	vRect(z, s, 10.5, 11.2, 13.5, 12.8, false)
	vArc(z, s, 13.5, 12, 1.6, 0, 2*math.Pi, false, false)
	vRect(z, s, 14, 6, 22, 18, false)
}

// light: a lamp panel with three LEDs and rays (v0.80, RGB Light page)
func iconLight(z *vector.Rasterizer, s float32) {
	vRect(z, s, 3, 13, 21, 21, false)
	vRect(z, s, 4.6, 14.6, 19.4, 19.4, true)
	for _, x := range []float32{7.5, 12, 16.5} {
		vArc(z, s, x, 17, 1.5, 0, 2*math.Pi, false, false)
	}
	vRect(z, s, 11.2, 2.5, 12.8, 9.5, false)
	vRect(z, s, 4.4, 6.2, 6, 10.8, false)
	vRect(z, s, 18, 6.2, 19.6, 10.8, false)
}

// trichrome (v0.82): three overlapping rings (R, G, B separation)
func iconTri(z *vector.Rasterizer, s float32) {
	for _, c := range [][2]float32{{12, 7.5}, {7.8, 15}, {16.2, 15}} {
		vArc(z, s, c[0], c[1], 5.6, 0, 2*math.Pi, false, false)
		vArc(z, s, c[0], c[1], 4.0, 0, 2*math.Pi, true, false)
	}
}
