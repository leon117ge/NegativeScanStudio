//go:build darwin || (linux && gio)

package main

// Camera & Light > Light: the layout of LightUI.Page (../lightpage.go),
// presetGroup (../configpage.go) and the 500T card (../castpage.go), bound
// to hdrctl.Light. Labels and tooltips are the Windows ones.

import (
	"image"
	"image/color"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"basetool/lightlevel"
	"basetool/macapp/appstate"
	"basetool/macapp/hdrctl"
	"basetool/rgblight"
)

const (
	lightLabelW  = 96 // Windows MinSize{Width: 96} of the leveling grid labels
	presetLabelW = 62
)

var lightAreaItems = []string{"Auto: find the base (rebate, gaps, sprocket area)", "Rectangle drawn on the image"}

// chCol are the brightness colours of R, G and B (walk.RGB(220,80,80) etc.).
var chCol = [3]color.NRGBA{rgb(220, 80, 80), rgb(80, 200, 100), rgb(90, 130, 235)}

type lightPage struct {
	u    *UI
	view liveView
	log  LogBox
	list widget.List
	left widget.List

	// RGB Light Air
	portCB           Combo
	refresh, connBtn Button
	// Channels
	enCK              [3]Check
	valEd             [3]Field
	applyBtn, readBtn Button
	offBtn            Button
	// Auto film-base leveling
	camSrcCB, isoCB, shutCB, targetCB, areaCB Combo
	hikEd, tolEd, iterEd, settleEd            Field
	camCK                                     Check
	startBtn, stopBtn, copyBtn                Button
	res                                       LogBox
	resSeq                                    int
	resText                                   string
	noteTip                                   Tip
	// presets
	presetCB                               Combo
	loadBtn, delBtn, saveBtn, fsave, fload Button
	nameEd, stockEd, notesEd               Field
	autoCK                                 Check
	// faults
	faults    LogBox
	faultSeq  int
	faultText string
	// last leveling captures
	capView  int
	capImgs  [4]paint.ImageOp
	imgSeq   int
	comp     image.Rectangle // fitted composite (px, page-local)
	dragging bool
	ax, ay   float64
	drag     lightlevel.Rect
	// 500T
	castCB   Combo
	kelvinEd Field
	castCK   Check
	calcBtn  Button

	cards [8]Card
	fseq  int
}

func newLightPage(u *UI) *lightPage {
	p := &lightPage{u: u}
	p.view.k = 3
	p.list.Axis, p.left.Axis = layout.Vertical, layout.Vertical
	p.cards[3].collapsed = true // Last leveling captures (not live)
	p.cards[7].collapsed = true // Faults reported by the light
	p.fseq = -1
	return p
}

// syncFields pushes controller text into the editors when the controller
// wrote it (FieldSeq), otherwise reads the editors back.
func (p *lightPage) syncFields() {
	l := p.u.hdr.Light
	type pair struct {
		ed *Field
		s  *string
	}
	ps := []pair{{&p.valEd[0], &l.Val[0]}, {&p.valEd[1], &l.Val[1]}, {&p.valEd[2], &l.Val[2]},
		{&p.tolEd, &l.TolText}, {&p.iterEd, &l.IterText}, {&p.settleEd, &l.SettleText}, {&p.hikEd, &l.HikExpText},
		{&p.nameEd, &l.NameText}, {&p.stockEd, &l.StockText}, {&p.notesEd, &l.NotesText}, {&p.kelvinEd, &l.KelvinText}}
	if p.fseq != l.FieldSeq {
		p.fseq = l.FieldSeq
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

func (p *lightPage) layout(gtx layout.Context) layout.Dimensions {
	p.syncFields()
	defer p.syncFields() // edits typed this frame reach the controller
	th, hp := p.u.th, p.u.hp
	return layout.Inset{Left: 8, Top: 6, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rw := rightWidth(gtx, 480, 520)
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				// Windows stacks live view, captures, 500T card and log in one
				// column; here the live view keeps 55 % of the height and the
				// rest scrolls, so an expanded card never squeezes the preview
				// out of the window (1280x800 and up).
				lower := []layout.Widget{p.capturesCard, p.castCard, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					gtx.Constraints.Max.Y = gtx.Dp(160)
					gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
					l := p.u.hdr.Light
					return p.log.Layout(gtx, th, l.LogText(), l.LogSeq, 90)
				}}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.text(gtx, "Live view: drag an orange box over clear film base.")
					}),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Flexed(0.55, func(gtx layout.Context) layout.Dimensions { return hp.liveView(gtx, &p.view) }),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Flexed(0.45, func(gtx layout.Context) layout.Dimensions {
						return material.List(th.Theme, &p.left).Layout(gtx, len(lower), func(gtx layout.Context, i int) layout.Dimensions {
							return layout.Inset{Bottom: 6}.Layout(gtx, lower[i])
						})
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(rw, gtx.Constraints.Max.Y))
				ws := []layout.Widget{p.lightCard, p.channelsCard, p.levelCard, p.presetCard, p.faultCard}
				return material.List(th.Theme, &p.list).Layout(gtx, len(ws), func(gtx layout.Context, i int) layout.Dimensions {
					return layout.Inset{Bottom: 3}.Layout(gtx, ws[i])
				})
			}),
		)
	})
}

func (p *lightPage) btn(b *Button, txt string, o btnOpts, f func()) layout.FlexChild {
	return layout.Rigid(p.u.hp.button(b, txt, o, f))
}

// ---------- RGB Light Air ----------

func (p *lightPage) lightCard(gtx layout.Context) layout.Dimensions {
	th, l := p.u.th, p.u.hdr.Light
	b := l.Buttons()
	return p.cards[0].Layout(gtx, th, "RGB Light Air", "light", nil, "", func(gtx layout.Context) layout.Dimensions {
		return vbox(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 6,
						layout.Flexed(1, p.u.hp.combo(&p.portCB, l.PortItems, &l.PortIdx, !b.Ports,
							"Serial ports. The light (USB 0483:5740) is found automatically and listed first; any COM port can be chosen by hand.", nil)),
						p.btn(&p.refresh, "Refresh", btnOpts{minW: 70}, l.RefreshPorts),
						p.btn(&p.connBtn, b.ConnText, btnOpts{minW: 90, disabled: !b.Conn,
							tip: "Connect reads the device info (21) and the state (20).\nDisconnect waits until the light is idle, switches R, G and B off and confirms it."}, l.ToggleConnect),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(48)
				return th.bold(gtx, l.InfoText, cText)
			},
			func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(18)
				return th.bold(gtx, l.TelText, cGreenText)
			},
		)
	})
}

// ---------- Channels ----------

func (p *lightPage) channelsCard(gtx layout.Context) layout.Dimensions {
	th, l := p.u.th, p.u.hdr.Light
	b := l.Buttons()
	return p.cards[1].Layout(gtx, th, "Channels", "wb", nil, "", func(gtx layout.Context) layout.Dimensions {
		return vbox(gtx,
			func(gtx layout.Context) layout.Dimensions {
				var ch []layout.FlexChild
				for c := 0; c < 3; c++ {
					c := c
					ch = append(ch,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							d, _ := p.enCK[c].Layout(gtx, th, rgblight.ChannelNames[c]+" on", &l.En[c], "")
							return d
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(56), gtx.Dp(56)
							return p.brightness(gtx, c)
						}))
				}
				return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return hbox(gtx, 6, ch...) })
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 4,
						p.btn(&p.applyBtn, "Apply", btnOpts{disabled: !b.Apply, tip: "Send the three channels (command 03) and confirm with a state read (20)."}, l.Apply),
						p.btn(&p.readBtn, "Read state", btnOpts{disabled: !b.Read}, l.ReadState),
						p.btn(&p.offBtn, "All off", btnOpts{disabled: !b.Off}, l.AllOff),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(44)
				return th.text(gtx, l.StateText)
			},
		)
	})
}

// brightness is a channel's LineEdit (Consolas 12 bold in the channel colour).
func (p *lightPage) brightness(gtx layout.Context, c int) layout.Dimensions {
	f := &p.valEd[c]
	f.SingleLine, f.MaxLen = true, 3
	for {
		if _, ok := f.Update(gtx); !ok {
			break
		}
	}
	return f.tip.Layout(gtx, p.u.th, "Brightness 1..100. Switch a channel off with its checkbox (brightness 0 is not allowed).", func(gtx layout.Context) layout.Dimensions {
		return fieldBox(gtx, gtx.Focused(&f.Editor), func(gtx layout.Context) layout.Dimensions {
			e := material.Editor(p.u.th.Theme, &f.Editor, "")
			e.Color, e.TextSize, e.SelectionColor = chCol[c], textBig, rgb(120, 70, 30)
			e.Font.Typeface = "Go Mono"
			return e.Layout(gtx)
		})
	})
}

// ---------- Auto film-base leveling ----------

func (p *lightPage) field(f *Field, w unit.Dp, tip string, done func()) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(w), gtx.Dp(w)
		d, fin := f.Layout(gtx, p.u.th, "", tip)
		if fin && done != nil {
			p.syncFields()
			done()
		}
		return d
	})
}

func (p *lightPage) levelCard(gtx layout.Context) layout.Dimensions {
	th, c := p.u.th, p.u.hdr
	l := c.Light
	b := l.Buttons()
	cfg := l.Settings()
	hik := l.UsesMonoCam()
	txt := func(s string) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions { return th.text(gtx, s) })
	}
	rows := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions {
			return gridRow(gtx, th, "Leveling camera", lightLabelW, p.u.hp.combo(&p.camSrcCB, hdrctl.CamSrcItems(), &cfg.CamSrc, l.Leveling(),
				"Which camera measures the film base while the RGB Light Air is adjusted.\nExisting camera is the Fujifilm X-T3 on the Camera page.\nHikrobot monochrome uses the MV-CS240-90UM (shared with Mono trichrome).\nThe simulator needs no MVS SDK.", l.CamSourceChanged))
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(34)
				return p.noteTip.Layout(gtx, th, hdrctl.CamProfileTip(), func(gtx layout.Context) layout.Dimensions { return th.text(gtx, l.CamNote) })
			})
		},
	}
	if hik {
		rows = append(rows, func(gtx layout.Context) layout.Dimensions {
			return gridRow(gtx, th, "Exposure", lightLabelW, func(gtx layout.Context) layout.Dimensions {
				return hbox(gtx, 4,
					p.field(&p.hikEd, 120, "Shared Hikrobot ExposureTime in microseconds (gain 0 dB). This is the same value as Mono Trichrome. Leveling uses it for R, G and B (Mono12, full frame), places the coarsest channel on target at an integer light code inside the feasible window while keeping the other channels within headroom, and saves the result back to both fields.", l.HikExposureEdited),
					txt("us"),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					txt("Gain 0 dB (fixed)"),
				)
			})
		})
	} else {
		rows = append(rows, func(gtx layout.Context) layout.Dimensions {
			return gridRow(gtx, th, "ISO", lightLabelW, func(gtx layout.Context) layout.Dimensions {
				return hbox(gtx, 4,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Max.X = gtx.Dp(80)
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return p.u.hp.combo(&p.isoCB, appstate.ISOItems, &cfg.ISO, l.Leveling(), "Leveling ISO - linked with the Camera page fixed frame-0 ISO (v0.82).", l.ISOChanged)(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(50)
						return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return th.text(gtx, "Shutter") })
					}),
					layout.Flexed(1, p.u.hp.combo(&p.shutCB, appstate.FixedShutterItems, &c.Cfg.Shutter, l.Leveling(),
						appstate.FixedShutterTip+"\nThe same setting as HDR scan's Base shutter (fixed frame 0).", c.ShutterChanged)),
				)
			})
		})
	}
	rows = append(rows,
		func(gtx layout.Context) layout.Dimensions {
			return gridRow(gtx, th, "Base level", lightLabelW, p.u.hp.combo(&p.targetCB, appstate.TargetItems, &cfg.Target, l.Leveling(),
				"Level of the clear base in each channel (fraction of raw full scale) - the same choices as HDR scan's Base level.", nil))
		},
		func(gtx layout.Context) layout.Dimensions {
			return gridRow(gtx, th, "Tolerance", lightLabelW, func(gtx layout.Context) layout.Dimensions {
				return hbox(gtx, 4,
					txt("±"), p.field(&p.tolEd, 44, "", nil), txt("%"),
					layout.Rigid(layout.Spacer{Width: 8}.Layout), txt("Max rounds"), p.field(&p.iterEd, 36, "", nil),
					layout.Rigid(layout.Spacer{Width: 8}.Layout), txt("Settle"), p.field(&p.settleEd, 50, "", nil), txt("ms"),
				)
			})
		},
		func(gtx layout.Context) layout.Dimensions {
			return gridRow(gtx, th, "Film base", lightLabelW, p.u.hp.combo(&p.areaCB, lightAreaItems, &cfg.AreaMode, l.Leveling(),
				"Auto detects a base region once from the initial RGB captures, then locks it.\nRectangle: drag a small patch of clear film base on the preview or Camera live view.\nControl and the final TIFF check use the same sampled pixels and median.", l.AreaModeChanged))
		},
		func(gtx layout.Context) layout.Dimensions {
			return gridRow(gtx, th, "Camera profile", lightLabelW, func(gtx layout.Context) layout.Dimensions {
				g := gtx
				if !l.CamProfileOK || l.Leveling() {
					g = gtx.Disabled()
				}
				d, _ := p.camCK.Layout(g, th, "Apply camera profile", &l.CamProfile,
					"v0.85: with a camera profile enabled (Chart calibration), leveling aims at the film base AFTER the CameraCal matrix,\nso the film-base reference can stay at 0.5/0.5/0.5. Measured slopes and an integer neighbour search use that matrix.\nFresh R/G/B captures and a TIFF check must both pass; failed runs never replace the saved base.")
				return d
			})
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 2, Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return hbox(gtx, 4,
					p.btn(&p.startBtn, "Start leveling", btnOpts{primary: true, h: 26, disabled: !b.Start,
						tip: "Fixed exposure. Measure each channel response, use bounded secant / bisection steps, then test neighbouring integer light codes through the camera matrix.\nFresh R/G/B captures and a calibration TIFF must pass the tolerance before a new base is accepted.\nThe round limit covers coarse solving; neighbour refinement and final verification are separate bounded phases."}, l.StartLevel),
					p.btn(&p.stopBtn, "Stop", btnOpts{h: 26, disabled: !b.Stop}, l.StopLevel),
				)
			})
		},
		func(gtx layout.Context) layout.Dimensions {
			if p.resText != l.ResText {
				p.resText = l.ResText
				p.resSeq++
			}
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			gtx.Constraints.Max.Y = gtx.Dp(110)
			return p.res.Layout(gtx, th, l.ResText, p.resSeq, 90)
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return hbox(gtx, 4, p.btn(&p.copyBtn, "Copy", btnOpts{disabled: !b.Copy}, l.Copy))
			})
		},
	)
	return p.cards[2].Layout(gtx, th, "Auto film-base leveling", "meter", nil, "", func(gtx layout.Context) layout.Dimensions {
		return vbox(gtx, rows...)
	})
}

// ---------- Scan config presets ----------

func (p *lightPage) presetCard(gtx layout.Context) layout.Dimensions {
	th, l := p.u.th, p.u.hdr.Light
	b := l.Buttons()
	cfg := l.Settings()
	row := func(label string, f *Field, tip string) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return gridRow(gtx, th, label, presetLabelW, func(gtx layout.Context) layout.Dimensions {
				d, _ := f.Layout(gtx, th, "", tip)
				return d
			})
		}
	}
	return p.cards[4].Layout(gtx, th, "Scan config presets", "preset", nil, "", func(gtx layout.Context) layout.Dimensions {
		return vbox(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 4,
						layout.Flexed(1, p.u.hp.combo(&p.presetCB, l.PresetItems, &l.PresetIdx, l.Leveling(), "", l.PresetChanged)),
						p.btn(&p.loadBtn, "Load preset", btnOpts{disabled: !b.PresetLoad,
							tip: "Apply the preset: light values (03, confirmed), camera ISO and base shutter, frame-0 handoff."}, l.LoadPreset),
						p.btn(&p.delBtn, "Delete", btnOpts{disabled: !b.PresetDel}, l.DeletePreset),
					)
				})
			},
			row("Name", &p.nameEd, "Preset name (Save preset replaces a preset with the same name)."),
			row("Film stock", &p.stockEd, "Film stock; its base values come from the film-base DB entry of the same name when present."),
			row("Notes", &p.notesEd, ""),
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 4,
						p.btn(&p.saveBtn, "Save preset", btnOpts{disabled: !b.PresetSave,
							tip: "Store the current light values and on/off, ISO, base shutter and film info under this name."}, l.SavePreset),
						p.btn(&p.fsave, "Save as file...", btnOpts{disabled: !b.PresetFile}, l.SavePresetFile),
						p.btn(&p.fload, "Load from file...", btnOpts{disabled: !b.PresetFile}, l.LoadPresetFile),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							d, ch := p.autoCK.Layout(gtx, th, "Apply when chosen", &cfg.AutoLoad, "")
							if ch {
								l.AutoLoadChanged()
							}
							return d
						}),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(48)
				return th.text(gtx, l.PresetText)
			},
		)
	})
}

// ---------- Faults ----------

func (p *lightPage) faultCard(gtx layout.Context) layout.Dimensions {
	th, l := p.u.th, p.u.hdr.Light
	return p.cards[7].Layout(gtx, th, "Faults reported by the light", "", nil, "", func(gtx layout.Context) layout.Dimensions {
		t := l.FaultText()
		if t != p.faultText {
			p.faultText = t
			p.faultSeq++
		}
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Max.Y = gtx.Dp(90)
		return p.faults.Layout(gtx, th, t, p.faultSeq, 50)
	})
}

// ---------- Last leveling captures (not live) ----------

func (p *lightPage) capturesCard(gtx layout.Context) layout.Dimensions {
	th, l := p.u.th, p.u.hdr.Light
	return p.cards[3].Layout(gtx, th, "Last leveling captures (not live)", "", nil, "", func(gtx layout.Context) layout.Dimensions {
		return vbox(gtx,
			func(gtx layout.Context) layout.Dimensions {
				w := gtx.Constraints.Max.X
				h := max(gtx.Dp(180), min(w*9/16, gtx.Dp(300)))
				return p.captures(gtx, image.Pt(w, h))
			},
			func(gtx layout.Context) layout.Dimensions {
				return th.label(gtx, textBody, cGrey, l.ViewLbl, false, false)
			},
		)
	})
}

func fitIn(box image.Rectangle, img image.Image) image.Rectangle {
	if img == nil {
		return box
	}
	return appstate.FitRect(box, img.Bounds().Size())
}

// captures is LightUI.paint: composite on top, the three channel captures
// below, the film-base rectangle in orange; a drag on the composite moves
// the measure area.
func (p *lightPage) captures(gtx layout.Context, sz image.Point) layout.Dimensions {
	th, l := p.u.th, p.u.hdr.Light
	p.capturesInput(gtx)
	if p.imgSeq != l.ImgSeq {
		p.imgSeq = l.ImgSeq
		for k, im := range []image.Image{l.Comp, l.Thumbs[0], l.Thumbs[1], l.Thumbs[2]} {
			if im != nil {
				p.capImgs[k] = paint.NewImageOp(im)
			}
		}
	}
	fillRect(gtx, cViewBg, image.Rectangle{Max: sz})
	gap, lab := gtx.Dp(6), gtx.Dp(16)
	topH := (sz.Y - gap) * 64 / 100
	cr := image.Rect(0, 0, sz.X, topH)
	tw := (sz.X - 2*gap) / 3
	var tr [3]image.Rectangle
	for k := 0; k < 3; k++ {
		tr[k] = image.Rect(k*(tw+gap), topH+gap, k*(tw+gap)+tw, sz.Y-lab)
	}
	area, show := l.Settings().Area, false
	if p.dragging {
		area, show = p.drag, true
	} else if box, ok := l.ManualBase(); ok {
		area, show = box, true
	}
	drawArea := func(ir image.Rectangle) {
		if !show || area.Empty() {
			return
		}
		r := image.Rect(ir.Min.X+int(area.X0*float64(ir.Dx())), ir.Min.Y+int(area.Y0*float64(ir.Dy())),
			ir.Min.X+int(area.X1*float64(ir.Dx())), ir.Min.Y+int(area.Y1*float64(ir.Dy())))
		strokeRect(gtx, cDragBox, r, gtx.Dp(2))
	}
	if l.Comp == nil {
		g := gtx
		g.Constraints = layout.Exact(cr.Size())
		layout.Center.Layout(g, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			lb := material.Label(th.Theme, textBody, "No leveling capture yet.\nConnect the light (and the X-T3 on Camera and Light > Camera), lay a piece of clear film base (unexposed, developed) on the light, then press Start leveling.\nAuto finds a large rebate/gap cluster in the TIFF. Choose Rectangle and drag a small orange box over clear base (same as white-light on Camera).")
			lb.Color, lb.Alignment = rgb(128, 128, 136), text.Middle
			return lb.Layout(gtx)
		})
		p.comp = image.Rect(cr.Dx()/6, cr.Dy()/8, cr.Dx()/6+cr.Dx()*2/3, cr.Dy()/8+cr.Dy()*3/4)
		drawArea(p.comp)
	} else {
		p.comp = fitIn(cr, l.Comp)
		drawImage(gtx, p.capImgs[0], p.comp)
		drawArea(p.comp)
	}
	for k := 0; k < 3; k++ {
		if l.Thumbs[k] != nil {
			ir := fitIn(tr[k], l.Thumbs[k])
			drawImage(gtx, p.capImgs[k+1], ir)
			drawArea(ir)
		} else {
			strokeRect(gtx, rgb(60, 60, 66), tr[k], gtx.Dp(2))
		}
		t := l.ThumbTxt[k]
		if t == "" {
			t = rgblight.ChannelNames[k] + " only"
		}
		off := op.Offset(image.Pt(tr[k].Min.X, tr[k].Max.Y+1)).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(tr[k].Dx(), lab))
		layout.N.Layout(g, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			lb := material.Label(th.Theme, textSmall, t)
			lb.Color, lb.MaxLines = chCol[k], 1
			return lb.Layout(gtx)
		})
		off.Pop()
	}
	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	eventOp(gtx, &p.capView)
	st.Pop()
	return layout.Dimensions{Size: sz}
}

func (p *lightPage) norm(pt image.Point) (float64, float64) {
	ir := p.comp
	cl := func(v float64) float64 { return min(max(v, 0), 1) }
	return cl(float64(pt.X-ir.Min.X) / float64(max(ir.Dx(), 1))), cl(float64(pt.Y-ir.Min.Y) / float64(max(ir.Dy(), 1)))
}

func (p *lightPage) capturesInput(gtx layout.Context) {
	l := p.u.hdr.Light
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.capView, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		pt := image.Pt(int(e.Position.X), int(e.Position.Y))
		switch e.Kind {
		case pointer.Press:
			if e.Buttons != pointer.ButtonPrimary || !l.CanDrag() {
				continue
			}
			p.dragging = true
			p.ax, p.ay = p.norm(pt)
			p.drag = lightlevel.Rect{X0: p.ax, Y0: p.ay, X1: p.ax, Y1: p.ay}
		case pointer.Drag:
			if p.dragging {
				fx, fy := p.norm(pt)
				p.drag = lightlevel.Rect{X0: min(p.ax, fx), Y0: min(p.ay, fy), X1: max(p.ax, fx), Y1: max(p.ay, fy)}
			}
		case pointer.Release, pointer.Cancel:
			if p.dragging {
				p.dragging = false
				l.PickArea(p.drag)
			}
		}
	}
}

// ---------- 500T illuminant ----------

func (p *lightPage) castCard(gtx layout.Context) layout.Dimensions {
	th, l := p.u.th, p.u.hdr.Light
	return p.cards[5].Layout(gtx, th, "500T illuminant", "", nil, "", func(gtx layout.Context) layout.Dimensions {
		return vbox(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return th.text(gtx, "500T is balanced for 3200K. Set the ORIGINAL scene light, not the scanning light.\nCamera WB and leveled light stay unchanged.")
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 3, Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 4,
						layout.Flexed(1, p.u.hp.combo(&p.castCB, hdrctl.CastItems, &l.CastIdx, false, "", l.CastChoiceChanged)),
						p.field(&p.kelvinEd, 65, "", nil),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return th.text(gtx, "K") }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							d, _ := p.castCK.Layout(gtx, th, "Enable", &l.CastOn, "")
							return d
						}),
						p.btn(&p.calcBtn, "Calculate", btnOpts{}, l.CastPreview),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(72)
				return th.text(gtx, l.CastText)
			},
		)
	})
}
