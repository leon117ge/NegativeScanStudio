//go:build darwin || (linux && gio)

package main

// HDR Scan > Focus check: the layout of FocusUI.Page (../focuspage.go),
// bound to hdrctl.Focus. Labels, tooltips and control order are the
// Windows ones.

import (
	"image"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"basetool/focusmark"
	"basetool/macapp/appstate"
	"basetool/macapp/hdrctl"
)

type focusPage struct {
	u    *UI
	list widget.List
	log  LogBox

	img paint.ImageOp
	seq int
	tag int // pointer target of the view

	card               Card
	zoomCB             Combo
	greenBtn, whiteBtn Button
	resetBtn           Button
	markCK             Check
	tips               [5]Tip
}

func newFocusPage(u *UI) *focusPage {
	p := &focusPage{u: u, seq: -1}
	p.list.Axis = layout.Vertical
	return p
}

var (
	cFocusBg    = rgb(16, 16, 18)
	cFocusCross = rgb(80, 220, 120)
)

func (p *focusPage) layout(gtx layout.Context) layout.Dimensions {
	th := p.u.th
	f := p.u.hdr.Focus
	f.Refresh()
	m := p.u.hdr.Mono
	return layout.Inset{Left: 8, Top: 6, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rw := rightWidth(gtx, 340, 420)
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Flexed(1, p.view),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						d, ch := p.markCK.Layout(gtx, th, "Center mark", &m.Mark,
							"High-contrast crosshair at the sensor centre, plus corner and edge marks at a 10% inset, to help level the film plane. Stays on the true image centre when you zoom or pan; hidden if that point is off-screen. Display only.")
						if ch {
							m.MarkChanged()
						}
						return d
					}),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.hint(gtx, "Click to zoom to 200%; drag to pan when zoomed.\nWhite marks = sensor centre and 10% inset; green crosshair = exposure & sharpness sample.")
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(rw, gtx.Constraints.Max.Y))
				return material.List(th.Theme, &p.list).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
					return p.focusCard(gtx)
				})
			}),
		)
	})
}

func (p *focusPage) focusCard(gtx layout.Context) layout.Dimensions {
	th, hp := p.u.th, p.u.hp
	f := p.u.hdr.Focus
	return p.card.Layout(gtx, th, "Focus check", "hdr", nil, "", func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: 8, Top: 4, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return vbox(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[0], f.SrcText, f.SrcTip, true, 16)
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[1], f.LightText, f.LightText, false, 16)
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[2], f.ExpText, f.ExpTip, false, 34)
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Zoom", 80, hp.combo(&p.zoomCB, hdrctl.ZoomItems(), &f.ZoomIdx, false, "", f.ZoomChanged))
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 2, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 6,
							layout.Rigid(hp.button(&p.greenBtn, "Green only", btnOpts{h: 28, disabled: !f.LightOK,
								tip: "Trichrome focus baseline: only the green LED at the scan brightness."}, func() { f.SetLight(0) })),
							layout.Rigid(hp.button(&p.whiteBtn, "White", btnOpts{h: 28, disabled: !f.LightOK,
								tip: "All three LEDs on, for focusing a white-light scan."}, func() { f.SetLight(1) })),
						)
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 2}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return th.label(gtx, textBody, cText, "Sharpness (zoomed region)", true, false)
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return meterBar(gtx, f.Meter, 1000)
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[3], f.SharpText, f.SharpTip, false, 34)
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 6,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								return th.text(gtx, f.PeakText)
							}),
							layout.Rigid(hp.button(&p.resetBtn, "Reset peak", btnOpts{h: 28}, f.ResetPeak)),
						)
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return p.tips[4].Layout(gtx, th, "While this page is open the camera exposure is adjusted automatically so the visible (or zoomed) region is neither clipped nor too dark, and film grain can be judged. The capture exposure used on the scan pages is restored when you leave.",
							func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								d := th.hint(gtx, "Auto exposure keeps the visible region readable.\nCapture exposure is restored when you leave.")
								d.Size.Y = max(d.Size.Y, gtx.Dp(30))
								return d
							})
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					gtx.Constraints.Min.Y = gtx.Dp(120)
					gtx.Constraints.Max.Y = max(gtx.Dp(120), min(gtx.Constraints.Max.Y, gtx.Dp(260)))
					return p.log.Layout(gtx, th, f.LogText(), f.LogSeq, 120)
				},
			)
		})
	})
}

// meterBar is the determinate ProgressBar (range 0..max).
func meterBar(gtx layout.Context, v, maxV int) layout.Dimensions {
	sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(14))
	roundBox(gtx, cField, cFieldLine, image.Rectangle{Max: sz}, gtx.Dp(3))
	if maxV > 0 && v > 0 {
		w := int(int64(sz.X-2) * int64(min(v, maxV)) / int64(maxV))
		if r := image.Rect(1, 1, 1+w, sz.Y-1); !r.Empty() {
			paint.FillShape(gtx.Ops, cAccent, clip.UniformRRect(r, gtx.Dp(2)).Op(gtx.Ops))
		}
	}
	return layout.Dimensions{Size: sz}
}

// view is FocusUI.paint plus the mouse handlers (click, move, up).
func (p *focusPage) view(gtx layout.Context) layout.Dimensions {
	th := p.u.th
	f := p.u.hdr.Focus
	sz := gtx.Constraints.Max
	if sz.X <= 0 || sz.Y <= 0 {
		return layout.Dimensions{Size: gtx.Constraints.Min}
	}
	cb := image.Rectangle{Max: sz}
	f.SetViewSize(sz.X, sz.Y)
	p.input(gtx, cb)

	defer clip.Rect(cb).Push(gtx.Ops).Pop()
	pointerEvent(gtx, &p.tag)
	fillRect(gtx, cFocusBg, cb)
	if f.Frame == nil {
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			l := material.Label(th.Theme, unit.Sp(13), "No live image.\nConnect the Hikrobot camera on Mono trichrome, or the scan camera on Camera, then open this page.")
			l.Color, l.Alignment = rgb(128, 128, 136), text.Middle
			return l.Layout(gtx)
		})
		return layout.Dimensions{Size: sz}
	}
	if p.seq != f.FrameSeq {
		p.seq = f.FrameSeq
		p.img = paint.NewImageOp(f.Frame)
	}
	bs := f.Frame.Bounds().Size()
	z := f.ZoomFactor()
	markW, markH := f.SrcW, f.SrcH
	if markW <= 0 {
		markW = bs.X
	}
	if markH <= 0 {
		markH = bs.Y
	}
	var src, dst image.Rectangle
	if z == 0 || f.BmpFit {
		dst = appstate.FitRect(cb, bs)
		src = image.Rectangle{Max: bs}
		markW, markH = bs.X, bs.Y
		drawImage(gtx, p.img, dst)
	} else {
		src = image.Rect(f.BmpX, f.BmpY, f.BmpX+bs.X, f.BmpY+bs.Y)
		dw, dh := bs.X*z, bs.Y*z
		dst = image.Rect((sz.X-dw)/2, (sz.Y-dh)/2, (sz.X-dw)/2+dw, (sz.Y-dh)/2+dh)
		drawImageFilter(gtx, p.img, dst, paint.FilterNearest)
	}
	if p.u.hdr.Mono.Mark {
		v := focusmark.View{ImgW: markW, ImgH: markH,
			SrcX: src.Min.X, SrcY: src.Min.Y, SrcW: src.Dx(), SrcH: src.Dy(),
			DstX: dst.Min.X, DstY: dst.Min.Y, DstW: dst.Dx(), DstH: dst.Dy()}
		centerMarksView(gtx, v, dst.Intersect(cb), markW, markH)
	}
	cx, cy := sz.X/2, sz.Y/2
	fillRect(gtx, cFocusCross, image.Rect(cx-18, cy, cx+19, cy+1))
	fillRect(gtx, cFocusCross, image.Rect(cx, cy-18, cx+1, cy+19))
	return layout.Dimensions{Size: sz}
}

func (p *focusPage) input(gtx layout.Context, cb image.Rectangle) {
	f := p.u.hdr.Focus
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		x, y := int(e.Position.X), int(e.Position.Y)
		switch e.Kind {
		case pointer.Press:
			if e.Buttons != pointer.ButtonPrimary {
				continue
			}
			f.Click(x, y, cb.Dx(), cb.Dy(), appstate.FitRect(cb, image.Pt(f.SrcW, f.SrcH)))
		case pointer.Drag:
			f.Move(x, y)
		case pointer.Release, pointer.Cancel:
			f.Up()
		}
	}
}
