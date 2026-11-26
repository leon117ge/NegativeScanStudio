//go:build darwin || (linux && gio)

package main

// HDR Scan > Mono trichrome: the layout of MonoUI.Page (../monopage.go),
// bound to hdrctl.Mono. Labels, tooltips and control order are the Windows
// ones; the NumberEdits are fields with their unit after them.

import (
	"image"
	"math"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"basetool/focusmark"
	"basetool/hikcam"
	"basetool/macapp/appstate"
)

// cBlack is the dark outline of the centre marks (focusmark.Outer).
var cBlack = rgb(0, 0, 0)

type monoPage struct {
	u    *UI
	log  LogBox
	list widget.List

	img paint.ImageOp
	seq int

	camCard, capCard                       Card
	devCB, presetCB, fmtCB, trigCB         Combo
	modeCB, brCB                           Combo
	refreshBtn, connBtn, applyBtn          Button
	capBtn, stopBtn                        Button
	expEd, gainEd, blackEd                 Field
	wEd, hEd, xEd, yEd, fpeEd              Field
	gammaCK, fullCK, caCK, meterCK, markCK Check
	rep                                    LogBox
	tips                                   [10]Tip
	fseq                                   int
}

func newMonoPage(u *UI) *monoPage {
	p := &monoPage{u: u, seq: -1, fseq: -1}
	p.list.Axis = layout.Vertical
	return p
}

var monoPixItems = []string{hikcam.PixelAuto.String(), hikcam.PixelMono12.String(), hikcam.PixelMono10.String(), hikcam.PixelMono8.String()}

// syncFields pushes controller text into the editors when the controller
// wrote it (FieldSeq), otherwise reads the editors back.
func (p *monoPage) syncFields() {
	m := p.u.hdr.Mono
	type pair struct {
		ed *Field
		s  *string
	}
	ps := []pair{{&p.expEd, &m.ExpText}, {&p.gainEd, &m.GainText}, {&p.blackEd, &m.BlackText},
		{&p.wEd, &m.WText}, {&p.hEd, &m.HText}, {&p.xEd, &m.XText}, {&p.yEd, &m.YText}, {&p.fpeEd, &m.FPEText}}
	if p.fseq != m.FieldSeq {
		p.fseq = m.FieldSeq
		for _, x := range ps {
			if x.ed.Text() != *x.s {
				x.ed.SetText(*x.s)
			}
		}
		return
	}
	for _, x := range ps {
		*x.s = x.ed.Text()
	}
}

func (p *monoPage) layout(gtx layout.Context) layout.Dimensions {
	p.syncFields()
	defer p.syncFields()
	th := p.u.th
	m := p.u.hdr.Mono
	return layout.Inset{Left: 8, Top: 6, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rw := rightWidth(gtx, 390, 490)
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Flexed(3, p.view),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						d, ch := p.markCK.Layout(gtx, th, "Center mark", &m.Mark,
							"High-contrast crosshair at the sensor centre, plus corner and edge marks at a 10% inset, to help level the film plane. Display only: never written into captured TIFFs.")
						if ch {
							m.MarkChanged()
						}
						return d
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.hint(gtx, "Hikrobot monochrome live view.\nCapture shoots R, G, and B with the RGB light and merges them into one linear 16-bit RGB TIFF.")
					}),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min = gtx.Constraints.Max
						gtx.Constraints.Min.Y = max(gtx.Constraints.Min.Y, gtx.Dp(110))
						return p.log.Layout(gtx, th, m.LogText(), m.LogSeq, 110)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(rw, gtx.Constraints.Max.Y))
				ws := []layout.Widget{p.cameraCard, layout.Spacer{Height: 3}.Layout, p.captureCard}
				return material.List(th.Theme, &p.list).Layout(gtx, len(ws), func(gtx layout.Context, i int) layout.Dimensions {
					return ws[i](gtx)
				})
			}),
		)
	})
}

// view is the CustomWidget paint of MonoUI: the letterboxed live frame,
// the centre / flatness marks and the frame description.
func (p *monoPage) view(gtx layout.Context) layout.Dimensions {
	th := p.u.th
	m := p.u.hdr.Mono
	sz := gtx.Constraints.Max
	if sz.X <= 0 || sz.Y <= 0 {
		return layout.Dimensions{Size: gtx.Constraints.Min}
	}
	fillRect(gtx, cViewBg, image.Rectangle{Max: sz})
	if m.Frame == nil {
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			l := material.Label(th.Theme, unit.Sp(13), "No Hikrobot image yet.\nConnect the monochrome camera (or the simulator) to preview.")
			l.Color, l.Alignment = rgb(128, 128, 136), text.Middle
			return l.Layout(gtx)
		})
		return layout.Dimensions{Size: sz}
	}
	if p.seq != m.FrameSeq {
		p.seq = m.FrameSeq
		p.img = paint.NewImageOp(m.Frame)
	}
	fs := m.Frame.Bounds().Size()
	ir := appstate.FitRect(image.Rectangle{Max: sz}, fs)
	drawImage(gtx, p.img, ir)
	if m.Mark {
		centerMarks(gtx, ir, fs.X, fs.Y)
	}
	if m.ViewWhat != "" {
		off := op.Offset(image.Pt(ir.Min.X+gtx.Dp(6), ir.Min.Y+gtx.Dp(4))).Push(gtx.Ops)
		th.label(gtx, textSmall, rgb(180, 180, 186), m.ViewWhat, false, false)
		off.Pop()
	}
	return layout.Dimensions{Size: sz}
}

// centerMarks is paintCenterMarks (../focusmark_paint.go): 1-px white
// lines with a black outline at the focusmark positions; arms in dp.
func centerMarks(gtx layout.Context, ir image.Rectangle, imgW, imgH int) {
	v := focusmark.View{ImgW: imgW, ImgH: imgH, SrcW: imgW, SrcH: imgH,
		DstX: ir.Min.X, DstY: ir.Min.Y, DstW: ir.Dx(), DstH: ir.Dy()}
	d := func(n int) int { return gtx.Dp(unit.Dp(n)) }
	line := func(x0, y0, x1, y1 int) {
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		if y0 > y1 {
			y0, y1 = y1, y0
		}
		r := image.Rect(x0, y0, x1+1, y1+1).Intersect(ir)
		if r.Empty() {
			return
		}
		fillRect(gtx, cBlack, r.Inset(-1).Intersect(ir))
		fillRect(gtx, cWhite, r)
	}
	for _, mk := range focusmark.Marks(imgW, imgH) {
		x, y, ok := v.Map(mk.IX, mk.IY)
		if !ok {
			continue
		}
		px, py := int(math.Round(x)), int(math.Round(y))
		switch mk.Kind {
		case focusmark.KindCentre:
			g, a, half := d(4), d(28), d(7)/2
			line(px-a, py, px-g, py)
			line(px+g, py, px+a, py)
			line(px, py-a, px, py-g)
			line(px, py+g, px, py+a)
			line(px-half, py-half, px+half, py-half)
			line(px-half, py+half, px+half, py+half)
			line(px-half, py-half, px-half, py+half)
			line(px+half, py-half, px+half, py+half)
		case focusmark.KindCorner:
			line(px, py, px+mk.InX*d(14), py)
			line(px, py, px, py+mk.InY*d(14))
		case focusmark.KindEdge:
			if mk.InX == 0 {
				line(px-d(10), py, px+d(10), py)
				line(px, py, px, py+mk.InY*d(10))
			} else {
				line(px, py-d(10), px, py+d(10))
				line(px, py, px+mk.InX*d(10), py)
			}
		}
	}
}

func (p *monoPage) field(f *Field, w unit.Dp, unitText, tip string, done func()) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(w), gtx.Dp(w)
				d, fin := f.Layout(gtx, p.u.th, "", tip)
				if fin && done != nil {
					p.syncFields()
					done()
				}
				return d
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if unitText == "" {
					return layout.Dimensions{}
				}
				return layout.Inset{Left: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return p.u.th.text(gtx, unitText) })
			}),
		)
	}
}

func (p *monoPage) cameraCard(gtx layout.Context) layout.Dimensions {
	th, hp := p.u.th, p.u.hp
	m := p.u.hdr.Mono
	b := m.Buttons()
	const lw = 110
	grey := cGrey
	return p.camCard.Layout(gtx, th, "Hikrobot monochrome camera", "camera", nil, "", func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: 8, Top: 4, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return vbox(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						d := th.label(gtx, textBody, grey, m.SDKText, false, false)
						d.Size.Y = max(d.Size.Y, gtx.Dp(30))
						return d
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 6,
							layout.Flexed(1, hp.combo(&p.devCB, m.DevItems, &m.DevIdx, !b.DevList, "", nil)),
							layout.Rigid(hp.button(&p.refreshBtn, "Refresh", btnOpts{minW: 70, disabled: !b.Refresh}, m.RefreshDevices)),
						)
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 6, layout.Rigid(hp.button(&p.connBtn, b.ConnText, btnOpts{minW: 100, disabled: !b.Conn}, m.ToggleConnect)))
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[0], m.CamText, m.CamText, true, 16)
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Preset", lw, hp.combo(&p.presetCB, []string{"Custom", "Best scan quality (MV-CS240-90UM)"}, &m.PresetIdx, false, hikcam.BestScanExplain(), m.PresetChanged))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Pixel format", lw, hp.combo(&p.fmtCB, monoPixItems, &m.FmtIdx, false,
						"Auto prefers Mono12, then Mono10, then Mono8 (native bit depth, unpacked to 16-bit linear). Best scan quality selects Mono12.", nil))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Exposure", lw, p.field(&p.expEd, 160, "us",
						"Shared Hikrobot ExposureTime in microseconds (MV-CS240-90UM: 32 us to 10 s, gain 0 dB). This is the same value as Camera and Light leveling. R, G and B capture all use this exposure unless Auto-meter each channel is checked.", m.ExposureEdited))
				},
				func(gtx layout.Context) layout.Dimensions {
					col := cRedText
					if m.BaseExpOK {
						col = cGreenText
					}
					return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						d := th.label(gtx, textBody, col, m.BaseExpText, false, false)
						d.Size.Y = max(d.Size.Y, gtx.Dp(16))
						return d
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Gain", lw, p.field(&p.gainEd, 160, "dB", "Analog gain in dB (MV-CS240-90UM: 0 to 24).", m.FieldEdited))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Black level", lw, p.field(&p.blackEd, 160, "",
						"BlackLevel pedestal in DN at the current bit depth. Best scan quality uses 32 (~0.8% of 12-bit full scale) so readout noise is not clipped at 0.", m.FieldEdited))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Trigger", lw, hp.combo(&p.trigCB, []string{"Free run", "Software trigger"}, &m.TrigIdx, false,
						"Software trigger grabs after the RGB light has settled. Best scan quality uses this so the frame is not a free-running rolling exposure.", nil))
				},
				func(gtx layout.Context) layout.Dimensions {
					d, _ := p.gammaCK.Layout(gtx, th, "Enable camera gamma", &m.Gamma, "Off (default): linear readout, recommended for film scanning.")
					return d
				},
				func(gtx layout.Context) layout.Dimensions {
					d, _ := p.fullCK.Layout(gtx, th, "Full frame", &m.Full, "Ignore ROI and read all 6016 x 4096 photosites.")
					return d
				},
				func(gtx layout.Context) layout.Dimensions {
					cell := func(label string, f *Field) []layout.FlexChild {
						return []layout.FlexChild{
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Dp(54)
								return th.text(gtx, label)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								d, fin := f.Layout(gtx, th, "", "")
								if fin {
									p.syncFields()
									m.FieldEdited()
								}
								return d
							}),
						}
					}
					row := func(a, b string, fa, fb *Field) layout.Widget {
						return func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								ch := append(cell(a, fa), layout.Rigid(layout.Spacer{Width: 6}.Layout))
								ch = append(ch, cell(b, fb)...)
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx, ch...)
							})
						}
					}
					return vbox(gtx, row("Width", "Height", &p.wEd, &p.hEd), row("Offset X", "Offset Y", &p.xEd, &p.yEd))
				},
				func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return hp.button(&p.applyBtn, "Apply camera settings", btnOpts{disabled: !b.Apply}, m.ApplySettings)(gtx)
				},
			)
		})
	})
}

func (p *monoPage) captureCard(gtx layout.Context) layout.Dimensions {
	th, hp := p.u.th, p.u.hp
	m := p.u.hdr.Mono
	b := m.Buttons()
	const lw = 130
	return p.capCard.Layout(gtx, th, "Mono trichrome capture", "tri", nil, "", func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: 8, Top: 4, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return vbox(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[1], m.LightText, m.LightText, false, 16)
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[2], m.ValText, m.ValTip, false, 16)
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[3], m.OutText, m.OutTip, false, 30)
				},
				func(gtx layout.Context) layout.Dimensions {
					d, _ := p.caCK.Layout(gtx, th, "Correct lateral chromatic aberration", &m.CA,
						"Measure where R and B sit against green across the frame, fit a radial lateral-CA model and move R and B onto green with a windowed-sinc resampler. Areas with less than about 0.12 px of CA (the frame centre) are left untouched. No sharpening is applied; a channel whose corners or centre would get worse is left as captured.")
					return d
				},
				func(gtx layout.Context) layout.Dimensions {
					d, ch := p.meterCK.Layout(gtx, th, "Auto-meter each channel", &m.MeterEach,
						"Off (default): R, G and B all use the shared exposure from film-base leveling (gain 0 dB). HDR bracket frames are EV offsets from that base. On: each channel auto-meters separately, as in earlier versions.")
					if ch {
						m.MeterEachChanged()
					}
					return d
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Mode", lw, hp.combo(&p.modeCB, []string{"Single frame per channel", "HDR bracket per channel (recommended)"}, &m.CapMode, false,
						"HDR (default): each R, G and B channel captures the bracket on the mono camera and merges it natively on the mono plane (black-subtracted, weighted by the MV-CS240-90UM noise model, ghost rejection), then CA correction and the trichrome merge run as usual.", m.OptionsChanged))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "HDR bracket", lw, hp.combo(&p.brCB, appstate.MonoBracketItems, &m.BrIdx, !b.Bracket,
						"0 / +2 / +3 / +5 EV (recommended): the +5 EV frame puts 32x the light on the dense parts of the negative (the positive's highlights) and roughly doubles their signal-to-noise ratio versus 0 / +2 / +3, which it contains, so no density gets worse. Costs one more grab per channel. 0 / +2 is the fastest. Active only when Mode is HDR.", m.OptionsChanged))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Frames per exposure", lw, p.field(&p.fpeEd, 70, "",
						"Same-exposure grabs averaged for every bracket step (1 = off). N frames cut temporal noise by the square root of N; exposure and readout time grow N times (light settle, merge, CA and TIFF write stay fixed).", m.OptionsChanged))
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 2, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 6,
							layout.Flexed(3, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								return hp.button(&p.capBtn, m.CapText, btnOpts{primary: true, h: 34, disabled: !b.Capture}, m.Capture)(gtx)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								return hp.button(&p.stopBtn, "Stop", btnOpts{h: 34, disabled: !b.Stop,
									tip: "Stop the capture safely between steps and restore the RGB light."}, m.Stop)(gtx)
							}),
						)
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[4], m.WhyText, m.WhyTip, true, 32)
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return progressBar(gtx, m.Progress) })
				},
				func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					gtx.Constraints.Max.Y = gtx.Dp(140)
					return p.rep.Layout(gtx, th, m.RepText, m.RepSeq, 80)
				},
			)
		})
	})
}
