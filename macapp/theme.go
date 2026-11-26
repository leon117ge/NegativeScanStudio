//go:build darwin || (linux && gio)

package main

// Dark theme and the hand-drawn controls of the Windows UI (theme.go,
// card.go, pages.go there), redrawn with Gio. Colours are the Windows
// values; sizes are the Windows 96-dpi pixel sizes as Gio dp.

import (
	"image"
	"image/color"
	"time"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"basetool/macapp/icons"
)

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{r, g, b, 255} }

var (
	cBg         = rgb(31, 31, 34)
	cPanel      = rgb(42, 42, 46)
	cBorder     = rgb(58, 58, 62)
	cHeaderText = rgb(154, 154, 160)
	cText       = rgb(216, 216, 220)
	cDim        = rgb(138, 138, 146)
	cGrey       = rgb(110, 110, 110) // walk.RGB(110,110,110) hint text, themed like Windows
	cField      = rgb(24, 24, 26)
	cFieldLine  = rgb(66, 66, 72)
	cAccent     = rgb(232, 124, 44)
	cAccentHot  = rgb(246, 146, 66)
	cAccentDown = rgb(196, 100, 32)
	cBtn        = rgb(58, 58, 63)
	cBtnHot     = rgb(72, 72, 78)
	cBtnDown    = rgb(44, 44, 48)
	cBtnLine    = rgb(78, 78, 84)
	cBtnOff     = rgb(46, 46, 50)
	cTextOff    = rgb(104, 104, 110)
	cBar        = rgb(24, 24, 27)
	cCardHead   = rgb(36, 36, 40)
	cCardHot    = rgb(44, 44, 49)
	cComboFill  = rgb(48, 48, 53)
	cListSel    = rgb(70, 58, 48)
	cWhite      = rgb(255, 255, 255)
	cViewBg     = rgb(22, 22, 25)
	cBaseBox    = rgb(60, 220, 120)
	cDragBox    = rgb(255, 170, 40)
	// Windows text colours remapped by theme.go's mapColor for the dark theme
	cGreenText = rgb(96, 204, 128)  // walk.RGB(30, 110, 60)
	cRedText   = rgb(240, 96, 86)   // walk.RGB(200, 50, 50)
	cGreenCh   = rgb(96, 204, 116)  // walk.RGB(40, 150, 60)
	cBlueCh    = rgb(100, 150, 245) // walk.RGB(50, 90, 200)
)

// Theme is the material theme plus the app's text sizes.
type Theme struct {
	*material.Theme
}

const (
	textBody  = unit.Sp(12) // Segoe UI 9 pt at 96 dpi
	textSmall = unit.Sp(11) // 8 pt
	textBig   = unit.Sp(16) // Consolas 12 pt bold readouts
	textTitle = unit.Sp(16) // header title, 12 pt bold
)

func newTheme() *Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	th.Palette = material.Palette{Bg: cBg, Fg: cText, ContrastBg: cAccent, ContrastFg: cWhite}
	th.TextSize = textBody
	return &Theme{th}
}

func fillRect(gtx layout.Context, c color.NRGBA, r image.Rectangle) {
	paint.FillShape(gtx.Ops, c, clip.Rect(r).Op())
}

func roundBox(gtx layout.Context, face, edge color.NRGBA, r image.Rectangle, rad int) {
	paint.FillShape(gtx.Ops, face, clip.UniformRRect(r, rad).Op(gtx.Ops))
	if edge != face {
		paint.FillShape(gtx.Ops, edge, clip.Stroke{Path: clip.UniformRRect(r, rad).Path(gtx.Ops), Width: float32(gtx.Dp(1))}.Op())
	}
}

func strokeRect(gtx layout.Context, c color.NRGBA, r image.Rectangle, w int) {
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: clip.Rect(r).Path(), Width: float32(w)}.Op())
}

// label draws text in colour c; bold and mono pick the face.
func (th *Theme) label(gtx layout.Context, size unit.Sp, c color.NRGBA, s string, bold, mono bool) layout.Dimensions {
	l := material.Label(th.Theme, size, s)
	l.Color = c
	if bold {
		l.Font.Weight = font.Bold
	}
	if mono {
		l.Font.Typeface = "Go Mono"
	}
	return l.Layout(gtx)
}

func (th *Theme) text(gtx layout.Context, s string) layout.Dimensions {
	return th.label(gtx, textBody, cText, s, false, false)
}

func (th *Theme) hint(gtx layout.Context, s string) layout.Dimensions {
	return th.label(gtx, textBody, cGrey, s, false, false)
}

func (th *Theme) bold(gtx layout.Context, s string, c color.NRGBA) layout.Dimensions {
	return th.label(gtx, textBody, c, s, true, false)
}

// ---------- tooltips ----------

// Tip shows text under a widget after the pointer rests on it.
type Tip struct {
	h     gesture.Hover
	since time.Time
}

func (t *Tip) Layout(gtx layout.Context, th *Theme, tip string, w layout.Widget) layout.Dimensions {
	dims := w(gtx)
	if tip == "" {
		return dims
	}
	defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
	hovered := t.h.Update(gtx.Source)
	t.h.Add(gtx.Ops)
	if !hovered {
		t.since = time.Time{}
		return dims
	}
	if t.since.IsZero() {
		t.since = gtx.Now
	}
	if wait := 600*time.Millisecond - gtx.Now.Sub(t.since); wait > 0 {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(wait)})
		return dims
	}
	m := op.Record(gtx.Ops)
	op.Offset(image.Pt(0, dims.Size.Y+gtx.Dp(2))).Add(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(gtx.Dp(460), gtx.Dp(400))}
	layout.Stack{}.Layout(g,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			roundBox(gtx, rgb(50, 50, 55), cBtnLine, image.Rectangle{Max: gtx.Constraints.Min}, gtx.Dp(3))
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(6).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return th.label(gtx, textSmall, cText, tip, false, false)
			})
		}))
	op.Defer(gtx.Ops, m.Stop())
	return dims
}

// ---------- push button ----------

// Button is a PushButton; primary is the bold orange kind (Meter base,
// Capture HDR, Merge to HDR TIFF).
type Button struct {
	widget.Clickable
	tip Tip
}

type btnOpts struct {
	primary  bool
	minW, h  unit.Dp
	tip      string
	disabled bool
}

// Layout draws the button and reports a click.
func (b *Button) Layout(gtx layout.Context, th *Theme, txt string, o btnOpts) (layout.Dimensions, bool) {
	clicked := false
	if o.disabled {
		gtx = gtx.Disabled()
		for b.Clicked(gtx) {
		}
	} else {
		clicked = b.Clicked(gtx)
	}
	h := o.h
	if h == 0 {
		h = 24
	}
	dims := b.tip.Layout(gtx, th, o.tip, func(gtx layout.Context) layout.Dimensions {
		return b.Clickable.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = gtx.Dp(h)
			if o.minW > 0 && gtx.Constraints.Min.X < gtx.Dp(o.minW) {
				gtx.Constraints.Min.X = min(gtx.Dp(o.minW), gtx.Constraints.Max.X)
			}
			face, edge, tc := cBtn, cBtnLine, cText
			switch {
			case o.disabled && o.primary:
				face, edge, tc = rgb(70, 56, 46), rgb(80, 62, 50), rgb(140, 120, 104)
			case o.disabled:
				face, edge, tc = cBtnOff, rgb(54, 54, 58), cTextOff
			case o.primary && b.Pressed():
				face, edge, tc = cAccentDown, cAccentDown, cWhite
			case o.primary && b.Hovered():
				face, edge, tc = cAccentHot, cAccentHot, cWhite
			case o.primary:
				face, edge, tc = cAccent, cAccent, cWhite
			case b.Pressed():
				face = cBtnDown
			case b.Hovered():
				face = cBtnHot
			}
			m := op.Record(gtx.Ops)
			lg := gtx
			lg.Constraints.Min = image.Point{}
			ld := layout.Inset{Left: 10, Right: 10}.Layout(lg, func(gtx layout.Context) layout.Dimensions {
				return th.label(gtx, textBody, tc, txt, o.primary, false)
			})
			call := m.Stop()
			sz := image.Pt(max(gtx.Constraints.Min.X, ld.Size.X), max(gtx.Constraints.Min.Y, ld.Size.Y+gtx.Dp(6)))
			r := image.Rectangle{Max: sz}.Inset(1)
			roundBox(gtx, face, edge, r, gtx.Dp(5))
			off := op.Offset(image.Pt((sz.X-ld.Size.X)/2, (sz.Y-ld.Size.Y)/2)).Push(gtx.Ops)
			call.Add(gtx.Ops)
			off.Pop()
			return layout.Dimensions{Size: sz}
		})
	})
	return dims, clicked
}

// ---------- combo box (dropdown) ----------

// Combo is a ComboBox: a field showing the selection and, when open, the
// item list drawn over everything below it.
type Combo struct {
	head  widget.Clickable
	items []widget.Clickable
	open  bool
	catch int
	tip   Tip
	list  widget.List
}

// Layout draws the combo; it returns true when the selection changed.
func (c *Combo) Layout(gtx layout.Context, th *Theme, items []string, sel *int, disabled bool, tip string) (layout.Dimensions, bool) {
	changed := false
	if len(c.items) != len(items) {
		c.items = make([]widget.Clickable, len(items))
	}
	if disabled {
		gtx = gtx.Disabled()
		c.open = false
	}
	if c.head.Clicked(gtx) {
		c.open = !c.open
	}
	for i := range c.items {
		if c.items[i].Clicked(gtx) {
			if *sel != i {
				*sel = i
				changed = true
			}
			c.open = false
		}
	}
	// a press outside the open list closes it
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &c.catch, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press {
			c.open = false
		}
	}
	cur := ""
	if *sel >= 0 && *sel < len(items) {
		cur = items[*sel]
	}
	dims := c.tip.Layout(gtx, th, tip, func(gtx layout.Context) layout.Dimensions {
		return c.head.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			h := gtx.Dp(24)
			w := gtx.Constraints.Max.X
			if gtx.Constraints.Max.X > gtx.Dp(2000) {
				w = gtx.Dp(200)
			}
			sz := image.Pt(max(w, gtx.Constraints.Min.X), h)
			face, edge, tc := cComboFill, cFieldLine, cText
			if disabled {
				face, edge, tc = cBtnOff, rgb(54, 54, 58), cTextOff
			} else if c.open || c.head.Hovered() {
				face, edge = rgb(58, 58, 64), cAccent
			}
			roundBox(gtx, face, edge, image.Rectangle{Max: sz}.Inset(1), gtx.Dp(4))
			// arrow
			ax, ay := sz.X-gtx.Dp(14), sz.Y/2
			var p clip.Path
			p.Begin(gtx.Ops)
			p.MoveTo(f32pt(ax-gtx.Dp(4), ay-gtx.Dp(2)))
			p.LineTo(f32pt(ax+gtx.Dp(4), ay-gtx.Dp(2)))
			p.LineTo(f32pt(ax, ay+gtx.Dp(3)))
			p.Close()
			paint.FillShape(gtx.Ops, cHeaderText, clip.Outline{Path: p.End()}.Op())
			lg := gtx
			lg.Constraints = layout.Exact(image.Pt(sz.X-gtx.Dp(30), sz.Y))
			off := op.Offset(image.Pt(gtx.Dp(8), 0)).Push(gtx.Ops)
			layout.W.Layout(lg, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(th.Theme, textBody, cur)
				l.Color, l.MaxLines = tc, 1
				gtx.Constraints.Min = image.Point{}
				return l.Layout(gtx)
			})
			off.Pop()
			return layout.Dimensions{Size: sz}
		})
	})
	if c.open {
		m := op.Record(gtx.Ops)
		// full-window catcher behind the list
		big := image.Rect(-10000, -10000, 10000, 10000)
		cs := clip.Rect(big).Push(gtx.Ops)
		pointerEvent(gtx, &c.catch)
		cs.Pop()
		op.Offset(image.Pt(0, dims.Size.Y)).Add(gtx.Ops)
		lg := gtx
		rowH := gtx.Dp(22)
		hgt := min(len(items)*rowH+gtx.Dp(4), gtx.Dp(320))
		lg.Constraints = layout.Exact(image.Pt(max(dims.Size.X, gtx.Dp(120)), hgt))
		roundBox(lg, rgb(30, 30, 33), cAccent, image.Rectangle{Max: lg.Constraints.Max}, gtx.Dp(3))
		c.list.Axis = layout.Vertical
		layout.UniformInset(2).Layout(lg, func(gtx layout.Context) layout.Dimensions {
			return material.List(th.Theme, &c.list).Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
				return c.items[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					sz := image.Pt(gtx.Constraints.Max.X, rowH)
					if i == *sel {
						fillRect(gtx, cListSel, image.Rectangle{Max: sz})
					} else if c.items[i].Hovered() {
						fillRect(gtx, rgb(52, 52, 58), image.Rectangle{Max: sz})
					}
					g := gtx
					g.Constraints = layout.Exact(sz)
					layout.W.Layout(g, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(th.Theme, textBody, items[i])
							l.Color, l.MaxLines = cText, 1
							return l.Layout(gtx)
						})
					})
					return layout.Dimensions{Size: sz}
				})
			})
		})
		op.Defer(gtx.Ops, m.Stop())
	}
	return dims, changed
}

// ---------- line edit ----------

// Field is a LineEdit (single line, Return or focus loss = editing finished).
type Field struct {
	widget.Editor
	tip     Tip
	focused bool
}

// Layout draws the field; done reports OnEditingFinished.
func (f *Field) Layout(gtx layout.Context, th *Theme, cue, tip string) (layout.Dimensions, bool) {
	f.SingleLine, f.Submit = true, true
	done := false
	for {
		ev, ok := f.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			done = true
		}
	}
	foc := gtx.Focused(&f.Editor)
	if f.focused && !foc {
		done = true
	}
	f.focused = foc
	dims := f.tip.Layout(gtx, th, tip, func(gtx layout.Context) layout.Dimensions {
		return fieldBox(gtx, foc, func(gtx layout.Context) layout.Dimensions {
			e := material.Editor(th.Theme, &f.Editor, cue)
			e.Color, e.HintColor, e.SelectionColor = cText, cTextOff, rgb(120, 70, 30)
			e.TextSize = textBody
			return e.Layout(gtx)
		})
	})
	return dims, done
}

func fieldBox(gtx layout.Context, focused bool, w layout.Widget) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			edge := cFieldLine
			if focused {
				edge = cAccent
			}
			roundBox(gtx, cField, edge, image.Rectangle{Max: gtx.Constraints.Min}, gtx.Dp(3))
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = gtx.Dp(24)
			return layout.Inset{Left: 6, Right: 6, Top: 4, Bottom: 4}.Layout(gtx, w)
		}))
}

// readout is a read-only LineEdit (the big exposure readouts).
func (th *Theme) readout(gtx layout.Context, s string, c color.NRGBA, big bool, tip *Tip, tipText string) layout.Dimensions {
	w := func(gtx layout.Context) layout.Dimensions {
		return fieldBox(gtx, false, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			if big {
				l := material.Label(th.Theme, textBig, s)
				l.Color, l.MaxLines, l.Font.Weight, l.Font.Typeface = c, 1, font.Bold, "Go Mono"
				return l.Layout(gtx)
			}
			l := material.Label(th.Theme, textBody, s)
			l.Color, l.MaxLines = c, 1
			return l.Layout(gtx)
		})
	}
	if tip != nil {
		return tip.Layout(gtx, th, tipText, w)
	}
	return w(gtx)
}

// ---------- text area (read-only log) ----------

// LogBox is a read-only, scrolling multi-line TextEdit.
type LogBox struct {
	ed  widget.Editor
	seq int
}

func (l *LogBox) Layout(gtx layout.Context, th *Theme, text string, seq int, minH unit.Dp) layout.Dimensions {
	l.ed.ReadOnly = true
	if seq != l.seq {
		l.seq = seq
		l.ed.SetText(text)
		n := l.ed.Len()
		l.ed.SetCaret(n, n) // keep the newest line in view
	}
	if gtx.Constraints.Min.Y < gtx.Dp(minH) {
		gtx.Constraints.Min.Y = min(gtx.Dp(minH), gtx.Constraints.Max.Y)
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			roundBox(gtx, cField, cFieldLine, image.Rectangle{Max: gtx.Constraints.Min}, gtx.Dp(3))
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = gtx.Constraints.Max
			if gtx.Constraints.Min.Y > gtx.Dp(2000) {
				gtx.Constraints.Min.Y = gtx.Dp(minH)
			}
			return layout.UniformInset(5).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				e := material.Editor(th.Theme, &l.ed, "")
				e.Color, e.TextSize, e.SelectionColor = cText, textBody, rgb(120, 70, 30)
				return e.Layout(gtx)
			})
		}))
}

// ---------- check box ----------

type Check struct {
	widget.Clickable
	tip Tip
}

func (c *Check) Layout(gtx layout.Context, th *Theme, txt string, on *bool, tip string) (layout.Dimensions, bool) {
	changed := false
	if c.Clicked(gtx) {
		*on = !*on
		changed = true
	}
	dims := c.tip.Layout(gtx, th, tip, func(gtx layout.Context) layout.Dimensions {
		return c.Clickable.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					s := gtx.Dp(13)
					face, edge := cField, rgb(96, 96, 104)
					if c.Hovered() {
						edge = cAccent
					}
					if *on {
						face, edge = cAccent, cAccent
					}
					roundBox(gtx, face, edge, image.Rect(0, 0, s, s), gtx.Dp(3))
					if *on {
						var p clip.Path
						p.Begin(gtx.Ops)
						k := float32(s) / 13
						p.MoveTo(f32p(3.2*k, 7.2*k))
						p.LineTo(f32p(6*k, 10*k))
						p.LineTo(f32p(10.8*k, 4.2*k))
						paint.FillShape(gtx.Ops, cWhite, clip.Stroke{Path: p.End(), Width: float32(gtx.Dp(2))}.Op())
					}
					return layout.Dimensions{Size: image.Pt(s, gtx.Dp(22))}
				}),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return th.text(gtx, txt) }),
			)
		})
	})
	return dims, changed
}

// ---------- progress bar (marquee while a merge runs) ----------

func progressBar(gtx layout.Context, running bool) layout.Dimensions {
	sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(8))
	roundBox(gtx, cField, cFieldLine, image.Rectangle{Max: sz}, gtx.Dp(3))
	if running {
		period := 1600 * time.Millisecond
		t := float64(gtx.Now.UnixNano()%int64(period)) / float64(period)
		w := sz.X / 4
		x := int(t*float64(sz.X+w)) - w
		r := image.Rect(max(0, x), 1, min(sz.X, x+w), sz.Y-1)
		if !r.Empty() {
			paint.FillShape(gtx.Ops, cAccent, clip.UniformRRect(r, gtx.Dp(3)).Op(gtx.Ops))
		}
		gtx.Execute(op.InvalidateCmd{})
	}
	return layout.Dimensions{Size: sz}
}

// ---------- card ----------

// Card is the collapsible titled section of card.go.
type Card struct {
	head      widget.Clickable
	reset     widget.Clickable
	collapsed bool
	icon      paint.ImageOp
	iconOK    bool
	tip       Tip
}

// Layout draws the header and, unless collapsed, the body. onReset != nil
// shows "Reset" on the right.
func (c *Card) Layout(gtx layout.Context, th *Theme, title, icon string, onReset func(), resetTip string, body layout.Widget) layout.Dimensions {
	if c.head.Clicked(gtx) {
		c.collapsed = !c.collapsed
	}
	if onReset != nil && c.reset.Clicked(gtx) {
		onReset()
	}
	if !c.iconOK && icon != "" {
		c.icon = paint.NewImageOp(icons.Render(icon, 32, cAccent))
		c.iconOK = true
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			fillRect(gtx, cPanel, image.Rectangle{Max: gtx.Constraints.Min})
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					h := gtx.Dp(28)
					w := gtx.Constraints.Max.X
					resetW := 0
					if onReset != nil {
						resetW = gtx.Dp(64)
					}
					d := c.head.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						bg := cCardHead
						if c.head.Hovered() {
							bg = cCardHot
						}
						fillRect(gtx, bg, image.Rect(0, 0, w, h))
						fillRect(gtx, cAccent, image.Rect(0, 0, gtx.Dp(3), h))
						// chevron
						cs := float32(gtx.Dp(12))
						x0, y0 := float32(gtx.Dp(10)), float32(h)/2-cs/2
						var p clip.Path
						p.Begin(gtx.Ops)
						k := cs / 24
						if c.collapsed {
							p.MoveTo(f32p(x0+8*k, y0+5*k))
							p.LineTo(f32p(x0+17*k, y0+12*k))
							p.LineTo(f32p(x0+8*k, y0+19*k))
						} else {
							p.MoveTo(f32p(x0+5*k, y0+8*k))
							p.LineTo(f32p(x0+19*k, y0+8*k))
							p.LineTo(f32p(x0+12*k, y0+17*k))
						}
						p.Close()
						paint.FillShape(gtx.Ops, cHeaderText, clip.Outline{Path: p.End()}.Op())
						x := gtx.Dp(26)
						if c.iconOK {
							s := gtx.Dp(16)
							drawImage(gtx, c.icon, image.Rect(x, (h-s)/2, x+s, (h-s)/2+s))
							x += s + gtx.Dp(8)
						}
						g := gtx
						g.Constraints = layout.Exact(image.Pt(w-x-resetW, h))
						off := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
						layout.W.Layout(g, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min = image.Point{}
							l := material.Label(th.Theme, textBody, title)
							l.Color, l.MaxLines, l.Font.Weight = cText, 1, font.Bold
							return l.Layout(gtx)
						})
						off.Pop()
						return layout.Dimensions{Size: image.Pt(w, h)}
					})
					if onReset != nil {
						off := op.Offset(image.Pt(w-resetW, 0)).Push(gtx.Ops)
						g := gtx
						g.Constraints = layout.Exact(image.Pt(resetW, h))
						c.tip.Layout(g, th, resetTip, func(gtx layout.Context) layout.Dimensions {
							return c.reset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								col := cDim
								if c.reset.Hovered() {
									col = cAccentHot
								}
								return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Right: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return th.bold(gtx, "Reset", col)
									})
								})
							})
						})
						off.Pop()
					}
					return d
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if c.collapsed {
						return layout.Dimensions{}
					}
					return layout.Inset{Left: 8, Top: 4, Right: 8, Bottom: 6}.Layout(gtx, body)
				}),
			)
		}))
}

// drawImage paints img scaled into r (GPU texture on Metal).
func drawImage(gtx layout.Context, img paint.ImageOp, r image.Rectangle) {
	sz := img.Size()
	if sz.X == 0 || sz.Y == 0 || r.Empty() {
		return
	}
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	defer clip.Rect{Max: r.Size()}.Push(gtx.Ops).Pop()
	sc := op.Affine(scaleAff(float32(r.Dx())/float32(sz.X), float32(r.Dy())/float32(sz.Y))).Push(gtx.Ops)
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	sc.Pop()
}

func pointerEvent(gtx layout.Context, tag any) {
	eventOp(gtx, tag)
}
