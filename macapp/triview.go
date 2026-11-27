//go:build darwin || (linux && gio)

package main

// HDR Scan > Trichrome: the layout of TriUI.Page (../tripage.go), bound to
// hdrctl.Tri. Labels, tooltips and control order are the Windows ones.

import (
	"image"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"basetool/macapp/hdrctl"
)

type triPage struct {
	u    *UI
	view liveView
	log  LogBox
	list widget.List

	card            Card
	modeCB, brCB    Combo
	capBtn, stopBtn Button
	meterBtn        Button
	levelCK         Check
	res             LogBox
	tips            [6]Tip
	resSeq          int
}

func newTriPage(u *UI) *triPage {
	p := &triPage{u: u}
	p.view.k = 2
	p.list.Axis = layout.Vertical
	return p
}

// statusLine is a Label with its full text as the tooltip (setMonoStatusTip).
func statusLine(gtx layout.Context, th *Theme, tip *Tip, text, full string, bold bool, minH unit.Dp) layout.Dimensions {
	if full == text {
		full = ""
	}
	return layout.Inset{Bottom: 2}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return tip.Layout(gtx, th, full, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			d := th.label(gtx, textBody, cText, text, bold, false)
			if h := gtx.Dp(minH); d.Size.Y < h {
				d.Size.Y = h
			}
			return d
		})
	})
}

func (p *triPage) layout(gtx layout.Context) layout.Dimensions {
	th, hp := p.u.th, p.u.hp
	t := p.u.hdr.Tri
	return layout.Inset{Left: 8, Top: 6, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rw := rightWidth(gtx, 380, 470)
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Flexed(3, func(gtx layout.Context) layout.Dimensions { return hp.liveView(gtx, &p.view) }),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.hint(gtx, "Drag on the image to mark the clear film base (orange). Exposure and base come from Camera & Light.")
					}),
					layout.Rigid(layout.Spacer{Height: 6}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min = gtx.Constraints.Max
						gtx.Constraints.Min.Y = max(gtx.Constraints.Min.Y, gtx.Dp(120))
						return p.log.Layout(gtx, th, t.LogText(), t.LogSeq, 120)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(rw, gtx.Constraints.Max.Y))
				return material.List(th.Theme, &p.list).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
					return p.scanCard(gtx)
				})
			}),
		)
	})
}

func (p *triPage) scanCard(gtx layout.Context) layout.Dimensions {
	th, c := p.u.th, p.u.hdr
	t := c.Tri
	b := t.Buttons()
	hp := p.u.hp
	const lw = 130
	return p.card.Layout(gtx, th, "Trichrome scan", "tri", nil, "", func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: 8, Top: 4, Right: 8, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return vbox(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[0], t.CamText, t.CamText, false, 0)
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[1], t.LockText, t.LockTip, true, 0)
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[2], t.LightText, t.LightText, false, 0)
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[3], t.ValText, t.ValTip, false, 0)
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[4], t.OutText, t.OutTip, false, 30)
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Mode", lw, hp.combo(&p.modeCB, hdrctl.ModeItems(), &c.Cfg.ScanMode, !b.Mode, hdrctl.ModeTip(), t.ModeChanged))
				},
				func(gtx layout.Context) layout.Dimensions {
					return gridRow(gtx, th, "Bracket per channel", lw, hp.combo(&p.brCB, hdrctl.BracketItems(), &c.Cfg.TriBracket, !b.Bracket,
						"HDR mode only. Read once when Capture is pressed and used for all three channels.", t.BracketChanged))
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 2, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 6,
							layout.Flexed(3, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								return hp.button(&p.capBtn, b.CapText, btnOpts{primary: true, h: 34, disabled: !b.Capture}, t.Capture)(gtx)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								return hp.button(&p.stopBtn, "Stop", btnOpts{h: 34, disabled: !b.Stop,
									tip: "Stop the scan in progress (capture or merge): the light is restored to its pre-scan state and the interrupted frame's partial RAFs/TIFFs are deleted. Earlier frames are kept."}, t.Stop)(gtx)
							}),
						)
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hbox(gtx, 6,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								return hp.button(&p.meterBtn, "Meter base", btnOpts{h: 28, disabled: !b.Meter,
									tip: "Optional: meter frame 0 by hand (as on Camera and Light > Camera). Not needed after leveling or preset load."}, t.Meter)(gtx)
							}),
							// Windows' "Send base to Develop" is not ported: its only
							// consumer is main.go's Develop / Convert hook
							layout.Flexed(1, layout.Spacer{}.Layout),
						)
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return statusLine(gtx, th, &p.tips[5], t.WhyText, t.WhyTip, true, 30)
				},
				func(gtx layout.Context) layout.Dimensions {
					on := c.UseLastLeveling()
					g := gtx
					if !b.LevelMeta {
						g = gtx.Disabled()
					}
					d, ch := p.levelCK.Layout(g, th, "Film base metadata: Last leveling", &on, hdrctl.LevelMetadataTip())
					if ch && b.LevelMeta {
						t.SetUseLastLeveling(on)
					}
					return d
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 2}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						d := th.text(gtx, t.BaseText)
						d.Size.Y = max(d.Size.Y, gtx.Dp(32))
						return d
					})
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return progressBar(gtx, t.Progress) })
				},
				func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					gtx.Constraints.Max.Y = gtx.Dp(130)
					return p.res.LayoutTip(gtx, th, t.ResText, t.ResSeq, 90, triResTip)
				},
			)
		})
	})
}
