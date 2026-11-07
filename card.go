package main

// card.go (v0.83): NegPy-style collapsible titled cards. A Card is a drop-in
// for a GroupBox: a painted header (chevron, icon, bold title, optional
// "Reset" on the right) over a body composite that the header click shows
// or hides. Collapsed state is kept per title for the session.

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/image/vector"
)

var (
	cCardHead     = walk.RGB(36, 36, 40)
	cardCollapsed = map[string]bool{}
	cardFont      *walk.Font
	cardFontDPI   int
)

// Card is a collapsible titled section.
type Card struct {
	Title         string
	Icon          iconFunc
	Layout        Layout
	Children      []Widget
	StretchFactor int
	MinSize       Size
	MaxSize       Size
	Collapsed     bool
	OnReset       func() // shows "Reset" in the header when set
	ResetTip      string
}

type cardState struct {
	Card
	head *walk.CustomWidget
	body *walk.Composite
	hot  int // 0 none, 1 header, 2 reset
	bmp  *walk.Bitmap
	bmpK int
}

func (c Card) Create(b *Builder) error {
	s := &cardState{Card: c}
	if v, ok := cardCollapsed[c.Title]; ok {
		s.Collapsed = v
	}
	tip := c.ResetTip
	if c.OnReset != nil && tip == "" {
		tip = "Reset the settings of this card to their defaults."
	}
	// v0.83: keep the content at the top (extra height goes below it)
	switch l := c.Layout.(type) {
	case VBox:
		if l.Alignment == 0 {
			l.Alignment = AlignHNearVNear
		}
		c.Layout = l
	case Grid:
		if l.Alignment == 0 {
			l.Alignment = AlignHNearVNear
		}
		c.Layout = l
	}
	err := Composite{
		StretchFactor: c.StretchFactor,
		MinSize:       c.MinSize,
		MaxSize:       c.MaxSize,
		Background:    SolidColorBrush{Color: cPanel},
		Layout:        VBox{MarginsZero: true, Spacing: 0, Alignment: AlignHNearVNear},
		Children: []Widget{
			CustomWidget{
				AssignTo:            &s.head,
				MinSize:             Size{Height: 28},
				MaxSize:             Size{Height: 28},
				PaintMode:           PaintBuffered,
				InvalidatesOnResize: true,
				PaintPixels:         s.paint,
				ToolTipText:         tip,
				OnMouseDown: func(x, y int, btn walk.MouseButton) {
					if btn != walk.LeftButton {
						return
					}
					if s.OnReset != nil && x >= s.resetX() {
						s.OnReset()
						return
					}
					s.setCollapsed(!s.Collapsed)
				},
				OnMouseMove: func(x, y int, _ walk.MouseButton) {
					h := 1
					if s.OnReset != nil && x >= s.resetX() {
						h = 2
					}
					if h != s.hot {
						s.hot = h
						s.head.Invalidate()
					}
					hoverLeave[s.head.Handle()] = func() {
						if s.hot != 0 {
							s.hot = 0
							s.head.Invalidate()
						}
					}
					subclass(s.head.Handle(), roleHover, s.head)
					trackLeave(s.head.Handle())
				},
			},
			Composite{AssignTo: &s.body, Visible: !s.Collapsed, Layout: c.Layout, Children: c.Children},
		},
	}.Create(b)
	if s.head != nil {
		s.head.FixedHeight = 28
	}
	return err
}

func (s *cardState) setCollapsed(v bool) {
	s.Collapsed = v
	cardCollapsed[s.Title] = v
	s.body.SetVisible(!v)
	if p := s.body.Parent(); p != nil {
		p.RequestLayout()
		if w, ok := p.(walk.Widget); ok && w.Parent() != nil {
			w.Parent().RequestLayout()
		}
	}
	s.head.Invalidate()
}

func (s *cardState) resetX() int {
	cb := s.head.ClientBoundsPixels()
	return cb.Width - px(64, dpiOf(s.head))
}

func headFont(dpi int) *walk.Font {
	if cardFont == nil || cardFontDPI != dpi {
		if f, err := walk.NewFont("Segoe UI", 9, walk.FontBold); err == nil {
			cardFont, cardFontDPI = f, dpi
		}
	}
	return cardFont
}

func (s *cardState) paint(c *walk.Canvas, _ walk.Rectangle) error {
	cb := s.head.ClientBoundsPixels()
	d := dpiOf(s.head)
	bg := cCardHead
	if s.hot == 1 {
		bg = walk.RGB(44, 44, 49)
	}
	if br, err := walk.NewSolidColorBrush(bg); err == nil {
		c.FillRectanglePixels(br, cb)
		br.Dispose()
	}
	// accent strip on the left
	if br, err := walk.NewSolidColorBrush(cAccent); err == nil {
		c.FillRectanglePixels(br, walk.Rectangle{X: 0, Y: 0, Width: px(3, d), Height: cb.Height})
		br.Dispose()
	}
	f := headFont(d)
	x := px(10, d)
	cs := px(12, d)
	if bm := chevron(s.Collapsed, cs, bg, d); bm != nil {
		c.DrawImageStretchedPixels(bm, walk.Rectangle{X: x, Y: (cb.Height - cs) / 2, Width: cs, Height: cs})
	}
	x += px(16, d)
	if s.Icon != nil {
		size := px(16, d)
		if s.bmp == nil || s.bmpK != size*1000+int(uint32(bg)&0xff) {
			if bm, err := walk.NewBitmapFromImageForDPI(renderIcon(s.Icon, size, cAccent, bg), d); err == nil {
				if s.bmp != nil {
					s.bmp.Dispose()
				}
				s.bmp, s.bmpK = bm, size*1000+int(uint32(bg)&0xff)
			}
		}
		if s.bmp != nil {
			c.DrawImageStretchedPixels(s.bmp, walk.Rectangle{X: x, Y: (cb.Height - size) / 2, Width: size, Height: size})
		}
		x += size + px(8, d)
	}
	w := cb.Width - x
	if s.OnReset != nil {
		w = s.resetX() - x
		col := cDim
		if s.hot == 2 {
			col = cAccentHot
		}
		c.DrawTextPixels("Reset", f, col, walk.Rectangle{X: s.resetX(), Y: 0, Width: cb.Width - s.resetX() - px(8, d), Height: cb.Height}, walk.TextVCenter|walk.TextRight|walk.TextSingleLine)
	}
	c.DrawTextPixels(s.Title, f, cText, walk.Rectangle{X: x, Y: 0, Width: w, Height: cb.Height}, walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis)
	return nil
}

// card icons (24-unit grid like the nav icons)
func iconCamera(z *vector.Rasterizer, s float32) {
	vRect(z, s, 3, 7, 21, 19, false)
	vRect(z, s, 4.5, 8.5, 19.5, 17.5, true)
	vArc(z, s, 12, 13, 3.4, 0, 6.2832, false, false)
	vRect(z, s, 8, 4.5, 14, 7, false)
}

func iconMeter(z *vector.Rasterizer, s float32) {
	vArc(z, s, 12, 15, 9, 3.1416, 6.2832, false, true)
	vArc(z, s, 12, 15, 7, 3.1416, 6.2832, true, true)
	vRect(z, s, 11, 8, 13, 15, false)
	vRect(z, s, 3, 16, 21, 17.5, false)
}

func iconPreset(z *vector.Rasterizer, s float32) {
	vRect(z, s, 4, 4, 20, 20, false)
	vRect(z, s, 5.5, 5.5, 18.5, 18.5, true)
	vRect(z, s, 8, 4, 16, 10, false)
	vRect(z, s, 8, 13, 16, 15, false)
}

var chevCache = map[[3]int]*walk.Bitmap{}

// chevron is the painted expand/collapse arrow (no font glyph needed).
func chevron(collapsed bool, size int, bg walk.Color, dpi int) *walk.Bitmap {
	k := [3]int{map[bool]int{true: 1}[collapsed], size, int(uint32(bg))}
	if bm, ok := chevCache[k]; ok {
		return bm
	}
	f := func(z *vector.Rasterizer, s float32) {
		if collapsed {
			z.MoveTo(8*s, 5*s)
			z.LineTo(17*s, 12*s)
			z.LineTo(8*s, 19*s)
		} else {
			z.MoveTo(5*s, 8*s)
			z.LineTo(19*s, 8*s)
			z.LineTo(12*s, 17*s)
		}
		z.ClosePath()
	}
	bm, err := walk.NewBitmapFromImageForDPI(renderIcon(f, size, cHeaderText, bg), dpi)
	if err != nil {
		return nil
	}
	chevCache[k] = bm
	return bm
}
