//go:build darwin || (linux && gio)

package main

// Camera (Camera & Light) and White light scan (HDR Scan) pages: the layout
// of HDRUI.Pages in ../hdrpage.go, bound to hdrctl.

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

	"basetool/hdrscan"
	"basetool/macapp/appstate"
	"basetool/macapp/hdrctl"
)

// liveView is one of the shared live views (HDRUI.liveView(k)).
type liveView struct {
	k        int
	lvBtn    Button
	resetBtn Button
	tag      int
	img      paint.ImageOp
	seq      int
	ir       image.Rectangle
	dragging bool
	ax, ay   float64
	drag     hdrscan.Rect
}

type hdrPages struct {
	u     *UI
	views [2]liveView
	logs  [2]LogBox
	lists [2]widget.List

	// Camera card
	devCB                            Combo
	refresh, calib, conn, diag, test Button
	// Meter card
	targetCB, areaCB, slowCB, f0CB, isoCB, shutCB, stockCB, tolCB Combo
	rec, saveDB, saveF0, manage, meter                            Button
	levelMeta                                                     Check
	tips                                                          [8]Tip
	// Capture and merge card
	outEd, rollEd                                         Field
	outBtn                                                Button
	brCB, tiffCB, rawCB, alignCB, mergeCB, compCB, dustCB Combo
	capBtn, stopBtn                                       Button
	res                                                   LogBox
	resSeq                                                int
	resText                                               string
	cards                                                 [3]Card
	edInit                                                bool
}

func newHDRPages(u *UI) *hdrPages {
	p := &hdrPages{u: u}
	p.views[0].k, p.views[1].k = 0, 1
	for i := range p.lists {
		p.lists[i].Axis = layout.Vertical
	}
	return p
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// rightWidth is the width of a page's right card column: 2/5 of the page
// between lo and hi dp (the Windows MinSize / MaxSize), but always leaving
// the left preview column at least minLeftDp.
func rightWidth(gtx layout.Context, lo, hi unit.Dp) int {
	total := gtx.Constraints.Max.X
	rw := min(max(total*2/5, gtx.Dp(lo)), gtx.Dp(hi))
	return max(min(rw, total-gtx.Dp(minLeftDp)), 0)
}

// minLeftDp is the narrowest left (preview) column.
const minLeftDp = 360

// twoCol is the Camera / White page frame: left column (live view, hint,
// log) and the 450-480 px right column of cards.
func (p *hdrPages) twoCol(gtx layout.Context, k int, hint string, cards ...layout.Widget) layout.Dimensions {
	return layout.Inset{Left: 8, Top: 6, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rw := rightWidth(gtx, 450, 480)
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Flexed(3, func(gtx layout.Context) layout.Dimensions { return p.liveView(gtx, &p.views[k]) }),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return p.u.th.hint(gtx, hint) }),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min = gtx.Constraints.Max
						gtx.Constraints.Min.Y = max(gtx.Constraints.Min.Y, gtx.Dp(90))
						return p.logs[k].Layout(gtx, p.u.th, p.u.hdr.LogText(), p.u.hdr.LogSeq, 90)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(rw, gtx.Constraints.Max.Y))
				var ws []layout.Widget
				for i, c := range cards {
					if i > 0 {
						ws = append(ws, layout.Spacer{Height: 3}.Layout)
					}
					ws = append(ws, c)
				}
				return material.List(p.u.th.Theme, &p.lists[k]).Layout(gtx, len(ws), func(gtx layout.Context, i int) layout.Dimensions {
					return ws[i](gtx)
				})
			}),
		)
	})
}

func (p *hdrPages) camera(gtx layout.Context) layout.Dimensions {
	return p.twoCol(gtx, 0, "Drag on the image to mark the clear film base (orange). Green box = base region used by the last metering.\nCamera: CONNECTION MODE = USB TETHER SHOOTING FIXED, close X Acquire.",
		p.cameraCard, p.meterCard)
}

func (p *hdrPages) white(gtx layout.Context) layout.Dimensions {
	return p.twoCol(gtx, 1, "Drag on the image to mark the clear film base (orange). Frame 0 exposure and the base come from Camera & Light.",
		p.captureCard)
}

// ---------- live view ----------

func (p *hdrPages) liveView(gtx layout.Context, v *liveView) layout.Dimensions {
	c := p.u.hdr
	th := p.u.th
	b := c.Buttons()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			// The view takes exactly the space the column gives it (never
			// more: a forced minimum clipped the controls below it at
			// 1280x800); the frame is fitted inside with its aspect kept.
			sz := gtx.Constraints.Max
			if sz.X <= 0 || sz.Y <= 0 {
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}
			p.viewInput(gtx, v)
			fillRect(gtx, cViewBg, image.Rectangle{Max: sz})
			if c.Frame == nil {
				g := gtx
				g.Constraints = layout.Exact(sz)
				layout.Center.Layout(g, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Point{}
					l := material.Label(th.Theme, unit.Sp(13), "No image yet.\nConnect the camera and start live view, or press Meter base.\nDrag a rectangle here to choose the clear film base yourself.")
					l.Color, l.Alignment = rgb(128, 128, 136), text.Middle
					return l.Layout(gtx)
				})
			} else {
				if v.seq != c.FrameSeq {
					v.seq = c.FrameSeq
					v.img = paint.NewImageOp(c.Frame) // uploaded once as a GPU texture
				}
				v.ir = appstate.FitRect(image.Rectangle{Max: sz}, c.Frame.Bounds().Size())
				drawImage(gtx, v.img, v.ir)
				box := func(r hdrscan.Rect, col color.NRGBA) image.Rectangle {
					pr := appstate.NormToPx(v.ir, r)
					strokeRect(gtx, col, pr, gtx.Dp(2))
					return pr
				}
				if !c.BaseNorm.Empty() {
					r := box(c.BaseNorm, cBaseBox)
					y := r.Min.Y - gtx.Dp(16)
					if y < v.ir.Min.Y {
						y = r.Max.Y + gtx.Dp(2)
					}
					off := op.Offset(image.Pt(r.Min.X, y)).Push(gtx.Ops)
					th.label(gtx, textSmall, cBaseBox, "film base", true, false)
					off.Pop()
				}
				if v.dragging {
					box(v.drag, cDragBox)
				} else if c.Cfg.AreaMode == 1 && !c.ROI.Empty() {
					box(c.ROI, cDragBox)
				}
			}
			// input area over the view
			st := clip.Rect{Max: sz}.Push(gtx.Ops)
			eventOp(gtx, &v.tag)
			st.Pop()
			return layout.Dimensions{Size: sz}
		}),
		layout.Rigid(layout.Spacer{Height: 6}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					d, clk := v.lvBtn.Layout(gtx, th, b.LiveText, btnOpts{disabled: !b.Live})
					if clk {
						c.ToggleLiveView()
					}
					return d
				}),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					d, clk := v.resetBtn.Layout(gtx, th, "Reset to Auto", btnOpts{})
					if clk {
						c.ClearROI()
					}
					return d
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if v.k != 0 {
						return layout.Dimensions{}
					}
					return th.hint(gtx, c.ViewWhat)
				}),
			)
		}),
	)
}

// viewInput: drag a base rectangle (mouseDown / mouseMove / mouseUp).
func (p *hdrPages) viewInput(gtx layout.Context, v *liveView) {
	c := p.u.hdr
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
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
			if e.Buttons != pointer.ButtonPrimary || c.Frame == nil || c.Busy {
				continue
			}
			v.ax, v.ay = appstate.PxToNorm(v.ir, pt)
			v.dragging = true
			v.drag = hdrscan.Rect{X0: v.ax, Y0: v.ay, X1: v.ax, Y1: v.ay}
		case pointer.Drag:
			if v.dragging {
				x, y := appstate.PxToNorm(v.ir, pt)
				v.drag = appstate.DragRect(v.ax, v.ay, x, y)
			}
		case pointer.Release, pointer.Cancel:
			if v.dragging {
				v.dragging = false
				if e.Kind == pointer.Release && !v.drag.Empty() {
					c.SetROI(v.drag)
				}
			}
		}
	}
}

// ---------- cards ----------

func gridRow(gtx layout.Context, th *Theme, label string, lw unit.Dp, w layout.Widget) layout.Dimensions {
	return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(lw)
				gtx.Constraints.Max.X = gtx.Dp(lw)
				return th.text(gtx, label)
			}),
			layout.Flexed(1, w),
		)
	})
}

func vbox(gtx layout.Context, ws ...layout.Widget) layout.Dimensions {
	var ch []layout.FlexChild
	for _, w := range ws {
		ch = append(ch, layout.Rigid(w))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
}

func hbox(gtx layout.Context, gap unit.Dp, ch ...layout.FlexChild) layout.Dimensions {
	var out []layout.FlexChild
	for i, c := range ch {
		if i > 0 {
			out = append(out, layout.Rigid(layout.Spacer{Width: gap}.Layout))
		}
		out = append(out, c)
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, out...)
}

func (p *hdrPages) combo(cb *Combo, items []string, sel *int, disabled bool, tip string, onChange func()) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		d, ch := cb.Layout(gtx, p.u.th, items, sel, disabled, tip)
		if ch && onChange != nil {
			onChange()
		}
		return d
	}
}

func (p *hdrPages) button(b *Button, txt string, o btnOpts, f func()) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		d, clk := b.Layout(gtx, p.u.th, txt, o)
		if clk && f != nil {
			f()
		}
		return d
	}
}

func (p *hdrPages) cameraCard(gtx layout.Context) layout.Dimensions {
	c, th := p.u.hdr, p.u.th
	b := c.Buttons()
	return p.cards[0].Layout(gtx, th, "Camera", "camera", nil, "", func(gtx layout.Context) layout.Dimensions {
		return vbox(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 6,
						layout.Flexed(1, p.combo(&p.devCB, c.DeviceItems(), &c.DevIdx, !b.DevList, "", nil)),
						layout.Rigid(p.button(&p.refresh, "Refresh", btnOpts{minW: 70}, c.RefreshDevices)),
						layout.Rigid(p.button(&p.calib, "Chart calibration...", btnOpts{tip: hdrctl.CalTip()}, c.Cal.Open)),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 6,
						layout.Rigid(p.button(&p.conn, b.ConnText, btnOpts{minW: 100, disabled: !b.Conn}, c.Connect)),
						layout.Rigid(p.button(&p.diag, "Camera diagnostics", btnOpts{disabled: !b.Diag}, c.Diagnostics)),
						layout.Rigid(p.button(&p.test, "Test shot", btnOpts{disabled: !b.Test,
							tip: "One capture with the current settings: download to the Save-to folder (testshot_*), delete from the camera, measure the base. Every step goes to the log."}, c.TestShot)),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions { return th.bold(gtx, c.CamLbl, cText) },
		)
	})
}

func (p *hdrPages) meterCard(gtx layout.Context) layout.Dimensions {
	c, th := p.u.hdr, p.u.th
	b := c.Buttons()
	save := c.Save
	const lw = 92
	stock := c.StockIndex()
	return p.cards[1].Layout(gtx, th, "Meter the clear film base", "meter", nil, "", func(gtx layout.Context) layout.Dimensions {
		return vbox(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Base level", lw, p.combo(&p.targetCB, appstate.TargetItems, &c.Cfg.Target, false,
					"Median of the brightest channel over the clear base in frame 0. Locks from 1/3 EV below the target up to 1/6 EV above it, never above 0.87 (and never with the brightest part of the base near clipping). 0.50-0.60 leave more headroom.", save))
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Base area", lw, p.combo(&p.areaCB, appstate.AreaItems, &c.Cfg.AreaMode, false, "", save))
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Slowest shutter", lw, p.combo(&p.slowCB, appstate.SlowestItems, &c.Cfg.Slowest, false,
					"Auto frame 0: ISO is fixed at 160. If the base needs a slower frame-0 shutter than this, nothing is locked: open the aperture or brighten the light.", save))
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Frame 0", lw, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 4,
						layout.Flexed(3, p.combo(&p.f0CB, appstate.Frame0Items, &c.Cfg.Frame0, false,
							"Auto: meter the shutter at ISO 160 (as before).\nFixed: no metering search - one probe at the chosen ISO and base shutter (1/60 s or 1/125 s) locks frame 0; the brighter frames change the shutter only.\nWarns when the base clips (> 0.95) or is very low; compares with the film stock.", save)),
						layout.Flexed(1, p.combo(&p.isoCB, appstate.ISOItems, &c.Cfg.ISO, !b.ISO, "ISO of a fixed frame 0 (Fixed only; Auto always uses ISO 160).", c.ISOChanged)),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Base shutter", lw, p.combo(&p.shutCB, appstate.FixedShutterItems, &c.Cfg.Shutter, !b.Shutter,
					"Base shutter of a Fixed frame 0, Record base, the RGB light's auto film-base leveling and trichrome scans.\n1/125 s is one stop faster than 1/60 s (the X-T3 has no 1/120 s; 1/125 s is its nearest step).", c.ShutterChanged))
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Film stock", lw, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 4,
						layout.Flexed(3, p.combo(&p.stockCB, c.StockItems(), &stock, false,
							"Film-base DB (film_base_db.json next to the settings). With Fixed frame 0, frame 0's base is compared with this stock\n(per channel, in EV) and an ISO or shutter mismatch is flagged.", func() { c.SetStock(stock) })),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return th.text(gtx, "Tol.") }),
						layout.Flexed(1, p.combo(&p.tolCB, appstate.StockTolItems, &c.Cfg.StockTol, false, "Allowed deviation from the stored base per channel.", save)),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 4,
						layout.Rigid(p.button(&p.rec, "Record base", btnOpts{disabled: !b.Rec,
							tip: "Shoot a standalone fixed frame 0 at ISO 250 and the base shutter (set f/4 on the lens) of the clear base,\nkeep it as <roll>_..._BASE.RAF and save it as a new film stock or update one."}, c.RecordBase)),
						layout.Rigid(p.button(&p.saveDB, "Save base to DB", btnOpts{disabled: !b.SaveDB,
							tip: "Save the locked frame 0's base (metering probe) as a new film stock or update one."}, c.SaveBaseToDB)),
						layout.Rigid(p.button(&p.saveF0, "Save last frame 0 to DB", btnOpts{disabled: !b.SaveF0,
							tip: "Save the base of the last captured frame 0 without reshooting (only when that frame 0 was a clean, unclipped base)."}, c.SaveLastFrame0)),
						layout.Rigid(p.button(&p.manage, "Manage", btnOpts{tip: "Rename or delete film stocks."}, c.ManageDB)),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return p.button(&p.meter, b.MeterText, btnOpts{primary: true, h: 26, disabled: !b.Meter}, c.Meter)(gtx)
				})
			},
			p.readouts,
			func(gtx layout.Context) layout.Dimensions {
				lv := c.RefreshLevelingUI()
				on, g := lv.On, gtx
				if !lv.Enabled {
					g = gtx.Disabled()
				}
				d, ch := p.levelMeta.Layout(g, th, "Film base for metadata: Last leveling (after profile)", &on, hdrctl.LevelMetadataTip())
				if ch && lv.Enabled {
					c.SetUseLastLeveling(on)
				}
				return d
			},
			func(gtx layout.Context) layout.Dimensions {
				return th.text(gtx, c.RefreshLevelingUI().Text)
			},
			func(gtx layout.Context) layout.Dimensions { return th.bold(gtx, c.ExpLbl, cGreenText) },
			func(gtx layout.Context) layout.Dimensions {
				t := c.BaseLbl
				if t == "" && !c.Connected() {
					t = "Frame 0 Auto: ISO 160, 1/60 s or faster. Fixed: chosen ISO and base shutter.\nAperture: the f-number the camera reports is stored with base records."
				}
				gtx.Constraints.Min.Y = gtx.Dp(30)
				return th.text(gtx, t)
			},
		)
	})
}

// readouts is the 4-column grid of big read-only fields.
func (p *hdrPages) readouts(gtx layout.Context) layout.Dimensions {
	c, th := p.u.hdr, p.u.th
	c.Buttons() // as before: Buttons also runs a pending light handoff's lock
	cell := func(s string, col color.NRGBA) layout.FlexChild {
		return layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return th.readout(gtx, s, col, true, nil, "") })
	}
	lbl := func(s string, col color.NRGBA, tip int, tipText string) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(100), gtx.Dp(100)
			if tip >= 0 {
				return p.tips[tip].Layout(gtx, th, tipText, func(gtx layout.Context) layout.Dimensions { return th.bold(gtx, s, col) })
			}
			return th.bold(gtx, s, col)
		})
	}
	row := func(ch ...layout.FlexChild) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return hbox(gtx, 3, ch...) })
		}
	}
	brTip := "Shutters of the brighter frames: shutter only, ISO 160, aperture unchanged."
	return vbox(gtx,
		row(lbl("Camera now", cText, -1, ""), cell(c.CamISO, cText), cell(c.CamSh, cText), cell(c.CamF, cText)),
		row(lbl("Frame 0", cGreenText, -1, ""), cell(c.F0ISO, cGreenText), cell(c.F0Sh, cGreenText),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return th.hint(gtx, "locked") })),
		row(lbl("Brighter", cText, 0, brTip), layout.Flexed(3, func(gtx layout.Context) layout.Dimensions {
			return th.readout(gtx, c.P2Sh, cText, true, &p.tips[1], brTip)
		})),
		// Windows' "Send base to Develop" sits here; its only consumer is the
		// Develop / Convert hook in main.go, which the macOS app does not have
		row(lbl("Base level", cText, -1, ""), cell(c.BaseLv, cText), layout.Flexed(2, layout.Spacer{}.Layout)),
		row(lbl("Base R / G / B", cText, -1, ""), cell(c.BaseR, cRedText), cell(c.BaseG, cGreenCh), cell(c.BaseB, cBlueCh)),
	)
}

func (p *hdrPages) captureCard(gtx layout.Context) layout.Dimensions {
	c, th := p.u.hdr, p.u.th
	b := c.Buttons()
	save := c.Save
	if !p.edInit {
		p.outEd.SetText(c.Cfg.OutDir)
		p.rollEd.SetText(c.Cfg.Roll)
		p.edInit = true
	}
	keep, del, lzw := b2i(c.Cfg.KeepTIFF), b2i(c.Cfg.DeleteRAW), b2i(c.Cfg.LZW)
	const lw = 96
	reset := func() {
		c.Cfg.Bracket, c.Cfg.Merge, c.Cfg.LZW, c.Cfg.Dust = appstate.BracketDefault, appstate.MergeDefault, true, 0
		c.BracketChanged()
		c.Log.Printf("%s", "Capture and merge settings reset to defaults (bracket, merge, LZW, dust off).")
	}
	return p.cards[2].Layout(gtx, th, "Capture and merge", "hdr", reset, "Reset bracket, merge, compression and dust removal to the defaults (Save to and roll are kept).", func(gtx layout.Context) layout.Dimensions {
		return vbox(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Save to", lw, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 4,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							d, done := p.outEd.Layout(gtx, th, "", "")
							if done {
								c.Cfg.OutDir = p.outEd.Text()
								save()
							}
							return d
						}),
						layout.Rigid(p.button(&p.outBtn, "...", btnOpts{minW: 30}, func() {
							p.u.pick("Folder for the HDR scans", pickFolder, p.outEd.Text(), nil, func(path string, ok bool) {
								if ok {
									p.outEd.SetText(path)
									c.Cfg.OutDir = path
									save()
								}
							})
						})),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Roll name", lw, func(gtx layout.Context) layout.Dimensions {
					d, done := p.rollEd.Layout(gtx, th, "roll / folder name, e.g. Portra400_Kyoto",
						"Captures go into <Save to>/<roll name>/ (created if missing; an existing folder is continued, nothing is overwritten).\nThe roll name is also the file-name prefix: <roll>_<date>_<time>_<NNNN>_HDR.tiff and _0EV/_+3EV/... .RAF.\nEmpty: folder <YYYYMMDD>_roll (date of the first capture of this session), no prefix. Characters not allowed in file names become _.")
					if done {
						r := hdrscan.SanitizeRoll(p.rollEd.Text())
						if r != p.rollEd.Text() {
							p.rollEd.SetText(r)
						}
						c.Cfg.Roll = r
						save()
					}
					return d
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Bracket", lw, p.combo(&p.brCB, appstate.BracketItems, &c.Cfg.Bracket, false,
					"0 / +2 EV (default): frame 0 + one frame 2 EV brighter (4x frame 0's shutter).\n0 / +2 / +3 EV: adds a third frame 3 EV brighter (8x) for dense areas.\nBoth are merged with the Merge method below (Weighted by default). Shutter only, frame 0's ISO is kept.", c.BracketChanged))
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Frame TIFFs", lw, p.combo(&p.tiffCB, appstate.TIFFItems, &keep, false, "", func() { c.Cfg.KeepTIFF = keep == 1; save() }))
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "RAF files", lw, p.combo(&p.rawCB, appstate.RAFItems, &del, false, "", func() { c.Cfg.DeleteRAW = del == 1; save() }))
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Alignment", lw, p.combo(&p.alignCB, appstate.AlignItems, &c.Cfg.Align, false, "", save))
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Merge", lw, p.combo(&p.mergeCB, appstate.MergeItems, &c.Cfg.Merge, false, appstate.MergeTip, save))
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Compression", lw, p.combo(&p.compCB, appstate.CompItems, &lzw, false, "", func() { c.Cfg.LZW = lzw == 1; save() }))
			},
			func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, "Dust removal", lw, p.combo(&p.dustCB, appstate.DustItems, &c.Cfg.Dust, false,
					"Removes dust specks and hairs (also semi-transparent ones) from the merged HDR, judged against the local grain and image colour. Light: only clear outliers, leaves fine detail alone. Strong: also fainter dust, may touch some texture. The film base and rebate are left alone.\nApplied into the one output <name>_HDR.tiff (no uncleaned copy is kept). Off: the HDR is written exactly as before.", save))
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 2, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 6,
						layout.Flexed(2, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return p.button(&p.capBtn, b.CapText, btnOpts{primary: true, h: 30, disabled: !b.Capture}, c.Capture)(gtx)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return p.button(&p.stopBtn, "Stop", btnOpts{h: 30, disabled: !b.Stop,
								tip: "Stop the scan in progress (capture or merge): the interrupted frame's partial RAFs/TIFFs are deleted, the light is restored to its pre-scan state. Earlier frames are kept."}, c.Stop)(gtx)
						}),
					)
				})
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return progressBar(gtx, c.Progress) })
			},
			func(gtx layout.Context) layout.Dimensions {
				if p.resText != c.Res {
					p.resText = c.Res
					p.resSeq++
				}
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				gtx.Constraints.Max.Y = gtx.Dp(64)
				return p.res.Layout(gtx, th, c.Res, p.resSeq, 44)
			},
		)
	})
}
