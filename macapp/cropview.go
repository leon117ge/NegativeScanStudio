//go:build darwin || (linux && gio)

package main

// Crop & Merge > Crop: the CropUI page of ../croppage.go. The controller
// (cropctl) holds the state; this file draws the preview with the crop
// frame, the file table and the Crop / Rotation / Export cards.

import (
	"image"
	"image/color"
	"math"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"basetool/cropper"
	"basetool/macapp/cropctl"
)

type cropPage struct {
	u *UI
	c *cropctl.CropUI

	viewTag, sliderTag int
	img                paint.ImageOp
	imgKey             string
	cursor             pointer.Cursor

	addFiles, addFolder, remove, clear  Button
	resetAuto, full, redetect, applyAll Button
	nudge                               [5]Button
	expSel, expAll, choose              Button
	aspectCB, insetCB, gridCB, onImpCB  Combo
	outCB, compCB, jpegCB, interpCB     Combo
	edges                               Check
	angleEd, outEd                      Field
	angleSeq                            int
	outInit                             bool
	table                               Table
	cards                               [3]Card
	list                                widget.List
	log                                 LogBox
	sliderX0, sliderX1                  int
}

func newCropPage(u *UI) *cropPage {
	p := &cropPage{u: u, c: u.crop, angleSeq: -1}
	p.list.Axis = layout.Vertical
	return p
}

var cropCols = []tableCol{{"File", 220}, {"Angle", 70}, {"Crop", 100}, {"Mode", 90}, {"Status", 240}}

func cellColor(k int) color.NRGBA {
	switch k {
	case cropctl.CellError:
		return rgb(240, 100, 86)
	case cropctl.CellDim:
		return rgb(118, 118, 126)
	case cropctl.CellBusy:
		return rgb(240, 150, 70)
	case cropctl.CellDone:
		return rgb(96, 204, 116)
	}
	return cText
}

func (p *cropPage) layout(gtx layout.Context) layout.Dimensions {
	th, c := p.u.th, p.c
	if !p.outInit {
		p.outInit = true
		p.outEd.SetText(c.Cfg().OutDir)
	}
	if p.angleSeq != c.AngleSeq && !p.angleEd.focused {
		p.angleSeq = c.AngleSeq
		p.angleEd.SetText(c.AngleText)
	}
	c.AngleFocused = p.angleEd.focused
	return layout.Inset{Left: 8, Top: 6, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rw := rightWidth(gtx, 350, 350)
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				tableH := gtx.Dp(140)
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Flexed(1, p.preview),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(th.Theme, textBody, c.StatusLbl)
						l.Color, l.MaxLines = cGrey, 2
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						busy := c.Exporting()
						return hbox(gtx, 4,
							layout.Rigid(p.u.hp.button(&p.addFiles, "Add files...", btnOpts{}, c.AddFilesDialog)),
							layout.Rigid(p.u.hp.button(&p.addFolder, "Add folder...", btnOpts{}, c.AddFolderDialog)),
							layout.Rigid(p.u.hp.button(&p.remove, "Remove selected", btnOpts{disabled: busy}, c.RemoveSelected)),
							layout.Rigid(p.u.hp.button(&p.clear, "Clear", btnOpts{disabled: busy}, c.Clear)),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return th.hint(gtx, c.CountLbl) }),
							layout.Flexed(1, layout.Spacer{}.Layout),
						)
					}),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, tableH))
						return p.table.Layout(gtx, th, cropCols, tableSource{
							rows:     c.Rows(),
							cell:     c.Cell,
							color:    func(r, k int) color.NRGBA { return cellColor(c.CellStyle(r, k)) },
							selected: c.Selected,
							click:    c.ClickRow,
						})
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(rw, gtx.Constraints.Max.Y))
				ws := []layout.Widget{p.cropCard, p.rotationCard, p.exportCard}
				return material.List(th.Theme, &p.list).Layout(gtx, len(ws), func(gtx layout.Context, i int) layout.Dimensions {
					return layout.Inset{Bottom: 4}.Layout(gtx, ws[i])
				})
			}),
		)
	})
}

// ---------- preview ----------

func (p *cropPage) preview(gtx layout.Context) layout.Dimensions {
	th, c := p.u.th, p.c
	sz := gtx.Constraints.Max
	if sz.X <= 0 || sz.Y <= 0 {
		return layout.Dimensions{Size: gtx.Constraints.Min}
	}
	m := float64(gtx.Dp(14))
	tol := float64(gtx.Dp(9))
	g := c.Geom(0, 0, float64(sz.X), float64(sz.Y), m)
	p.viewInput(gtx, g, tol)
	fillRect(gtx, rgb(18, 18, 20), image.Rectangle{Max: sz})
	v := c.Preview()
	if v.Msg != "" {
		gg := gtx
		gg.Constraints = layout.Exact(sz)
		layout.Center.Layout(gg, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			l := material.Label(th.Theme, textBody, v.Msg)
			l.Color, l.Alignment = rgb(128, 128, 136), text.Middle
			return l.Layout(gtx)
		})
	} else if g.OK() {
		if p.imgKey != v.Key {
			p.imgKey = v.Key
			rgba := cropper.RenderProxy(v.Proxy, v.Angle)
			p.img = paint.NewImageOp(&image.RGBA{Pix: rgba, Stride: 4 * v.Proxy.W, Rect: image.Rect(0, 0, v.Proxy.W, v.Proxy.H)})
		}
		tp := func(x, y float64) image.Point {
			vx, vy := g.ToView(x, y)
			return image.Pt(int(math.Round(vx)), int(math.Round(vy)))
		}
		ir := image.Rectangle{Min: tp(0, 0), Max: tp(float64(v.W), float64(v.H))}
		drawImage(gtx, p.img, ir)
		cr := image.Rectangle{Min: tp(v.Rect.X0, v.Rect.Y0), Max: tp(v.Rect.X1, v.Rect.Y1)}
		// dim outside the crop
		shade := func(x0, y0, x1, y1 int) {
			if x1 > x0 && y1 > y0 {
				paint.FillShape(gtx.Ops, colorA(0, 0, 0, 150), clip.Rect(image.Rect(x0, y0, x1, y1)).Op())
			}
		}
		shade(ir.Min.X, ir.Min.Y, ir.Max.X, cr.Min.Y)
		shade(ir.Min.X, cr.Max.Y, ir.Max.X, ir.Max.Y)
		shade(ir.Min.X, cr.Min.Y, cr.Min.X, cr.Max.Y)
		shade(cr.Max.X, cr.Min.Y, ir.Max.X, cr.Max.Y)
		// detected edges
		if v.Edges && v.Det.OK {
			for _, e := range v.Det.Edges {
				x0, y0 := cropper.RotFwd(v.Angle, float64(v.W), float64(v.H), e.P0[0], e.P0[1])
				x1, y1 := cropper.RotFwd(v.Angle, float64(v.W), float64(v.H), e.P1[0], e.P1[1])
				line(gtx, tp(x0, y0), tp(x1, y1), rgb(80, 220, 120), float32(gtx.Dp(2)))
			}
		}
		// grid
		if v.Grid > 0 {
			n := 3
			if v.Grid == 2 {
				n = 8
			}
			for i := 1; i < n; i++ {
				x := cr.Min.X + cr.Dx()*i/n
				y := cr.Min.Y + cr.Dy()*i/n
				fillRect(gtx, rgb(150, 150, 156), image.Rect(x, cr.Min.Y, x+1, cr.Max.Y))
				fillRect(gtx, rgb(150, 150, 156), image.Rect(cr.Min.X, y, cr.Max.X, y+1))
			}
		}
		// crop rectangle and handles
		strokeRect(gtx, cAccent, cr, gtx.Dp(2))
		hs, ht := gtx.Dp(18), gtx.Dp(4)
		for _, cn := range []struct{ x, y, sx, sy int }{{cr.Min.X, cr.Min.Y, 1, 1}, {cr.Max.X, cr.Min.Y, -1, 1}, {cr.Max.X, cr.Max.Y, -1, -1}, {cr.Min.X, cr.Max.Y, 1, -1}} {
			x0 := cn.x - ht/2
			if cn.sx < 0 {
				x0 = cn.x - hs + ht/2
			}
			y0 := cn.y - ht/2
			fillRect(gtx, cAccent, image.Rect(x0, y0, x0+hs, y0+ht))
			y0 = cn.y - ht/2
			if cn.sy < 0 {
				y0 = cn.y - hs + ht/2
			}
			x0 = cn.x - ht/2
			fillRect(gtx, cAccent, image.Rect(x0, y0, x0+ht, y0+hs))
		}
		for _, mp := range []image.Point{{cr.Min.X + cr.Dx()/2, cr.Min.Y}, {cr.Min.X + cr.Dx()/2, cr.Max.Y}, {cr.Min.X, cr.Min.Y + cr.Dy()/2}, {cr.Max.X, cr.Min.Y + cr.Dy()/2}} {
			s := gtx.Dp(8)
			fillRect(gtx, cAccent, image.Rect(mp.X-s/2, mp.Y-s/2, mp.X-s/2+s, mp.Y-s/2+s))
		}
		// readout
		col := rgb(96, 204, 116)
		if v.ReadoutManual {
			col = cAccent
		}
		tr := image.Rect(gtx.Dp(8), gtx.Dp(4), sz.X-gtx.Dp(8), gtx.Dp(22))
		paint.FillShape(gtx.Ops, colorA(0, 0, 0, 120), clip.Rect(image.Rect(tr.Min.X-gtx.Dp(4), tr.Min.Y, min(tr.Min.X-gtx.Dp(4)+gtx.Dp(420), sz.X), tr.Max.Y)).Op())
		off := op.Offset(image.Pt(tr.Min.X, tr.Min.Y+gtx.Dp(1))).Push(gtx.Ops)
		gg := gtx
		gg.Constraints = layout.Constraints{Max: tr.Size()}
		th.label(gg, textBody, col, v.Readout, true, false)
		off.Pop()
	}
	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	p.cursor.Add(gtx.Ops)
	eventOp(gtx, &p.viewTag)
	st.Pop()
	return layout.Dimensions{Size: sz}
}

func line(gtx layout.Context, a, b image.Point, col color.NRGBA, w float32) {
	var pa clip.Path
	pa.Begin(gtx.Ops)
	pa.MoveTo(f32pt(a.X, a.Y))
	pa.LineTo(f32pt(b.X, b.Y))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: pa.End(), Width: w}.Op())
}

var cropCursors = map[int]pointer.Cursor{cropctl.CursorArrow: pointer.CursorDefault, cropctl.CursorMove: pointer.CursorAllScroll,
	cropctl.CursorNWSE: pointer.CursorNorthWestSouthEastResize, cropctl.CursorNESW: pointer.CursorNorthEastSouthWestResize,
	cropctl.CursorWE: pointer.CursorColResize, cropctl.CursorNS: pointer.CursorRowResize}

func (p *cropPage) viewInput(gtx layout.Context, g cropctl.Geom, tol float64) {
	c := p.c
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.viewTag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Move})
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
			if e.Buttons == pointer.ButtonPrimary {
				c.MouseDown(g, x, y, tol)
			}
		case pointer.Drag:
			c.MouseMove(g, x, y)
		case pointer.Release, pointer.Cancel:
			c.MouseUp()
		}
		p.cursor = cropCursors[c.HitCursor(g, x, y, tol)]
	}
}

// ---------- cards ----------

const cropLW = 90

func (p *cropPage) cropCard(gtx layout.Context) layout.Dimensions {
	th, c, hp := p.u.th, p.c, p.u.hp
	cfg := c.Cfg()
	return p.cards[0].Layout(gtx, th, "Crop", "crop", nil, "", func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: 8, Top: 2, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			asp, ins, grid, imp := cfg.Aspect, cfg.Inset, cfg.Grid, cfg.OnImport
			edges := cfg.Edges
			return vbox(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Aspect", cropLW, hp.combo(&p.aspectCB, cropctl.AspectNames, &asp, false,
						"Auto: 3:2 for landscape frames, 2:3 for portrait frames. Free: any shape, drag the sides.", func() { c.SetAspect(asp) }))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Inset", cropLW, hp.combo(&p.insetCB, cropctl.InsetNames, &ins, false,
						"Extra margin inside the detected picture edge on every side, so no film base or soft edge is left.", func() { c.SetInset(ins) }))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Overlay", cropLW, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 6,
							layout.Flexed(1, hp.combo(&p.gridCB, cropctl.GridNames, &grid, false, "", func() { c.SetGrid(grid) })),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								d, ch := p.edges.Layout(gtx, th, "Edges", &edges, "Show the detected picture edges in green.")
								if ch {
									c.SetEdges(edges)
								}
								return d
							}))
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "On import", cropLW, hp.combo(&p.onImpCB, []string{"Auto straighten + crop", "Manual (full image)"}, &imp, false, "", func() { c.SetOnImport(imp) }))
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 4,
							layout.Flexed(1, hp.button(&p.resetAuto, "Reset to auto", btnOpts{tip: "Back to the detected angle and the automatic crop."}, c.ResetToAuto)),
							layout.Flexed(1, hp.button(&p.full, "Full image", btnOpts{tip: "Manual mode: angle 0, crop = whole image (in the chosen aspect)."}, c.FullImage)),
							layout.Flexed(1, hp.button(&p.redetect, "Re-detect", btnOpts{}, c.Redetect)))
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 4, layout.Flexed(1, hp.button(&p.applyAll, "Apply crop settings to all",
						btnOpts{tip: "Recompute the automatic crop of every image that was not adjusted by hand."}, c.ApplyAll)))
				},
			)
		})
	})
}

func (p *cropPage) rotationCard(gtx layout.Context) layout.Dimensions {
	th, c, hp := p.u.th, p.c, p.u.hp
	return p.cards[1].Layout(gtx, th, "Rotation", "", func() { c.SetAngle(0) }, "Set the rotation back to 0°.", func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: 8, Top: 2, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return vbox(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 6,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return th.label(gtx, unit.Sp(18), rgb(232, 124, 44), c.AngleLbl, true, true)
						}),
						layout.Flexed(1, layout.Spacer{}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return th.text(gtx, "Angle") }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(64), gtx.Dp(64)
							d, fin := p.angleEd.Layout(gtx, th, "", "Degrees, positive = counter-clockwise. Steps of 0.05°, at most ±5°.")
							if fin {
								c.AngleEdited(p.angleEd.Text())
							}
							return d
						}))
				},
				p.slider,
				func(gtx layout.Context) layout.Dimensions {
					labels := []string{"-0.5°", "-0.05°", "0", "+0.05°", "+0.5°"}
					dv := []float64{-0.5, -0.05, 0, 0.05, 0.5}
					var ch []layout.FlexChild
					for i := range labels {
						i := i
						f := func() { c.Nudge(dv[i]) }
						if i == 2 {
							f = func() { c.SetAngle(0) }
						}
						ch = append(ch, layout.Flexed(1, hp.button(&p.nudge[i], labels[i], btnOpts{}, f)))
					}
					return hbox(gtx, 3, ch...)
				},
				layout.Spacer{Height: 4}.Layout,
				func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.Y = gtx.Dp(30)
					return th.hint(gtx, c.DetLbl)
				},
			)
		})
	})
}

// slider is the dark angle slider (paintSlider).
func (p *cropPage) slider(gtx layout.Context) layout.Dimensions {
	c := p.c
	sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(30))
	m := gtx.Dp(10)
	x0, x1, cy := m, sz.X-m, sz.Y/2
	p.sliderX0, p.sliderX1 = x0, x1
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.sliderTag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		x := int(e.Position.X)
		switch e.Kind {
		case pointer.Press:
			if e.Buttons == pointer.ButtonPrimary {
				c.SliderDown(x, x0, x1)
			}
		case pointer.Drag:
			c.SliderMove(x, x0, x1)
		case pointer.Release, pointer.Cancel:
			c.SliderUp()
		}
	}
	thk := gtx.Dp(4)
	roundBox(gtx, cField, cFieldLine, image.Rect(x0, cy-thk/2, x1, cy-thk/2+thk), thk/2)
	pos := func(v float64) int {
		return x0 + int(math.Round((v+cropctl.SliderMax)/(2*cropctl.SliderMax)*float64(x1-x0)))
	}
	for v := -5; v <= 5; v++ {
		h, col := gtx.Dp(4), cFieldLine
		if v == 0 {
			h, col = gtx.Dp(8), cDim
		}
		x := pos(float64(v))
		fillRect(gtx, col, image.Rect(x, cy+thk/2+gtx.Dp(3), x+1, cy+thk/2+gtx.Dp(3)+h))
	}
	if ang, det, ok, detOK := c.SliderState(); ok {
		zx, vx := pos(0), pos(ang)
		if vx != zx {
			fillRect(gtx, cAccent, image.Rect(min(zx, vx), cy-thk/2, max(zx, vx), cy-thk/2+thk))
		}
		if detOK {
			dx := pos(det)
			fillRect(gtx, rgb(80, 220, 120), image.Rect(dx-gtx.Dp(1), cy-gtx.Dp(10), dx+gtx.Dp(1), cy-gtx.Dp(4)))
		}
		r := gtx.Dp(7)
		col := cAccent
		if c.SliderDrag {
			col = cAccentHot
		}
		roundBox(gtx, col, rgb(255, 196, 140), image.Rect(vx-r, cy-r, vx+r, cy+r), r)
	}
	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	eventOp(gtx, &p.sliderTag)
	st.Pop()
	return layout.Dimensions{Size: sz}
}

func (p *cropPage) exportCard(gtx layout.Context) layout.Dimensions {
	th, c, hp := p.u.th, p.c, p.u.hp
	cfg := c.Cfg()
	return p.cards[2].Layout(gtx, th, "Export", "develop", nil, "", func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: 8, Top: 2, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			out, comp, jq, ip := cfg.OutMode, b2i(cfg.LZW), cfg.JPEGQ, cfg.Interp
			busy := c.Exporting()
			return vbox(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Save to", cropLW, hp.combo(&p.outCB, []string{"\"cropped\" subfolder next to each file", "Custom folder"}, &out, false, "", func() { c.SetOutMode(out) }))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Folder", cropLW, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 4,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								g := gtx
								if cfg.OutMode != 1 {
									g = gtx.Disabled()
								}
								g.Constraints.Min.X = g.Constraints.Max.X // full width like the walk LineEdit
								d, fin := p.outEd.Layout(g, th, "", "")
								if fin {
									c.SetOutDir(p.outEd.Text())
								}
								return d
							}),
							layout.Rigid(hp.button(&p.choose, "Choose...", btnOpts{minW: 70, disabled: cfg.OutMode != 1}, func() {
								c.ChooseOutDir(func(s string) { p.outEd.SetText(s) })
							})))
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "TIFF", cropLW, hp.combo(&p.compCB, []string{"None (fastest)", "LZW + predictor (lossless)"}, &comp, false, "", func() { c.SetLZW(comp) }))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "JPEG quality", cropLW, hp.combo(&p.jpegCB, cropctl.JPEGQNames, &jq, false, "", func() { c.SetJPEGQ(jq) }))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Resampling", cropLW, hp.combo(&p.interpCB, []string{"Bicubic (Catmull-Rom)", "Lanczos-3 (sharpest)"}, &ip, false,
						"Used only when the angle is not 0. At 0° the crop is a lossless pixel copy.", func() { c.SetInterp(ip) }))
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 2, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 4,
							layout.Flexed(1, hp.button(&p.expSel, "Export selected", btnOpts{h: 32, disabled: busy}, func() { c.Export(false) })),
							layout.Flexed(1, hp.button(&p.expAll, "Export all", btnOpts{h: 32, primary: true, disabled: busy}, func() { c.Export(true) })))
					})
				},
				func(gtx layout.Context) layout.Dimensions { return progressValue(gtx, c.PBVal, c.PBMax) },
				layout.Spacer{Height: 4}.Layout,
				func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.Y = gtx.Dp(110)
					gtx.Constraints.Min = gtx.Constraints.Max
					return p.log.LayoutTip(gtx, th, c.LogText, c.LogSeq, 50, cropLogTip)
				},
			)
		})
	})
}
