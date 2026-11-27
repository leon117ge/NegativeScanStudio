//go:build darwin || (linux && gio)

package main

// Chart calibration (chartcal.go calibrateChart on Windows): the modal
// dialog becomes an in-window panel over the page, with the same rows,
// labels, tooltips and control order. The logic is hdrctl/chartcal.go.

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"basetool/colorsci"
)

var calGrey = rgb(128, 128, 136) // walk.RGB(128, 128, 136)

type calPanel struct {
	u                                       *UI
	openBtn, lastBtn, refBtn                Button
	detBtn, rotBtn, resetBtn, fitBtn        Button
	saveBtn, delBtn, closeBtn               Button
	nameEd, strEd                           Field
	profCB                                  Combo
	enCK                                    Check
	res                                     widget.Editor
	resSeq, nameSeq, strSeq, imgSeq, viewID int
	img                                     paint.ImageOp
	ir                                      image.Rectangle
}

func newCalPanel(u *UI) *calPanel {
	return &calPanel{u: u, resSeq: -1, nameSeq: -1, strSeq: -1, imgSeq: -1}
}

// layout draws the panel over the whole window when it is open.
func (p *calPanel) layout(gtx layout.Context) {
	k, th := p.u.hdr.Cal, p.u.th
	if !k.Shown {
		return
	}
	full := image.Rectangle{Max: gtx.Constraints.Max}
	paint.FillShape(gtx.Ops, colorA(0, 0, 0, 150), clip.Rect(full).Op())
	st := clip.Rect(full).Push(gtx.Ops)
	eventOp(gtx, p) // swallow input behind the panel
	st.Pop()
	m := gtx.Dp(20)
	r := full.Inset(m)
	if r.Dx() > gtx.Dp(1240) {
		r.Min.X = (full.Dx() - gtx.Dp(1240)) / 2
		r.Max.X = r.Min.X + gtx.Dp(1240)
	}
	roundBox(gtx, cPanel, cAccent, r, gtx.Dp(6))
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(r.Size())
	idle := k.Busy == ""
	if k.NameSeq != p.nameSeq {
		p.nameSeq = k.NameSeq
		p.nameEd.SetText(k.Name)
	}
	if k.StrSeq != p.strSeq {
		p.strSeq = k.StrSeq
		p.strEd.SetText(fmtF2(k.Strength))
	}
	btn := func(b *Button, txt string, o btnOpts, f func()) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			d, clk := b.Layout(gtx, th, txt, o)
			if clk && f != nil {
				f()
			}
			return d
		})
	}
	lbl := func(s string, c color.NRGBA) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions { return th.label(gtx, textBody, c, s, false, false) })
	}
	spacer := layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} })
	row := func(ch ...layout.FlexChild) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return hbox(gtx, 6, ch...) })
		})
	}
	layout.Inset{Left: 10, Top: 8, Right: 10, Bottom: 8}.Layout(g, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return th.bold(gtx, "Chart calibration - camera + light input profile", cText)
				})
			}),
			row(
				btn(&p.openBtn, "Open chart TIFF...", btnOpts{disabled: !idle, tip: "16-bit linear RGB TIFF of the chart (a trichrome scan from this program)."}, k.OpenChart),
				btn(&p.lastBtn, "Use last scan", btnOpts{disabled: !idle, tip: "The last trichrome TIFF written on the Trichrome page in this session."}, k.UseLast),
				lbl(k.FileText, calGrey), spacer),
			row(
				btn(&p.refBtn, "Load reference (CGATS)...", btnOpts{tip: "IT8.7/1 reference data: SAMPLE_ID and XYZ_X/Y/Z (or LAB_L/A/B), D50."}, k.OpenRef),
				lbl(k.RefText, calGrey), spacer),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return th.label(gtx, textBody, calGrey, "The 4x6 grid is found automatically, row A = neutral ramp, A1 darkest. Drag the corner markers A1 / A6 / D6 / D1 to correct it. Green = clean, red = clipped (left out).", false, false)
				})
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 6}.Layout(gtx, p.view)
			}),
			row(
				btn(&p.detBtn, "Auto-detect", btnOpts{disabled: !idle}, k.Detect),
				btn(&p.rotBtn, "Rotate 180°", btnOpts{disabled: !idle, tip: "Swap A1 and D6 ends (chart upside down)."}, k.Rotate),
				btn(&p.resetBtn, "Reset grid", btnOpts{disabled: !idle}, k.ResetGrid),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if idle {
						return layout.Dimensions{}
					}
					return th.label(gtx, textBody, cAccent, k.Busy, false, false)
				}),
				btn(&p.fitBtn, "Fit", btnOpts{minW: 100, disabled: !idle}, k.Fit)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: 6}.Layout(gtx, p.result)
			}),
			row(
				lbl("Profile name", cText),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(260), gtx.Dp(260)
					d, _ := p.nameEd.Layout(gtx, th, "", "")
					k.SetName(p.nameEd.Text())
					return d
				}),
				btn(&p.saveBtn, "Save as camera profile", btnOpts{disabled: !k.SaveOK || !idle}, k.Save),
				spacer),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return hbox(gtx, 6,
					lbl("Active camera profile", cText),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(220), gtx.Dp(220)
						d, ch := p.profCB.Layout(gtx, th, k.Profiles, &k.ProfIdx, false, "")
						if ch {
							k.SettingsChanged()
						}
						return d
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						d, ch := p.enCK.Layout(gtx, th, "Enable", &k.Enabled, "Apply the active camera profile to trichrome film-base measurement.")
						if ch {
							k.SettingsChanged()
						}
						return d
					}),
					lbl("Strength", cText),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(70), gtx.Dp(70)
						d, done := p.strEd.Layout(gtx, th, "", "")
						if done {
							k.SetStrength(p.strEd.Text())
						}
						return d
					}),
					btn(&p.delBtn, "Delete profile", btnOpts{}, k.DeleteProfile),
					spacer,
					btn(&p.closeBtn, "Close", btnOpts{minW: 80}, k.Close))
			}),
		)
	})
}

func fmtF2(v float64) string { return fmt.Sprintf("%.2f", v) }

// result is the read-only, monospaced result TextEdit (MinSize height 170).
func (p *calPanel) result(gtx layout.Context) layout.Dimensions {
	k, th := p.u.hdr.Cal, p.u.th
	p.res.ReadOnly = true
	if k.ResSeq != p.resSeq {
		p.resSeq = k.ResSeq
		p.res.SetText(strings.ReplaceAll(k.ResText, "\r\n", "\n"))
	}
	h := gtx.Dp(170)
	gtx.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, h))
	roundBox(gtx, cField, cFieldLine, image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(3))
	layout.UniformInset(5).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		e := material.Editor(th.Theme, &p.res, "")
		e.Color, e.TextSize, e.SelectionColor = cText, textBody, rgb(120, 70, 30)
		e.Font = font.Font{Typeface: "Go Mono"}
		return e.Layout(gtx)
	})
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// view is the chart preview with the 24 patch boxes and the corner markers
// (paint, mouseDown / mouseMove / mouseUp).
func (p *calPanel) view(gtx layout.Context) layout.Dimensions {
	k, th := p.u.hdr.Cal, p.u.th
	sz := gtx.Constraints.Max
	if sz.Y < gtx.Dp(240) {
		sz.Y = gtx.Constraints.Max.Y
	}
	ir := k.ImageRect(sz.X, sz.Y)
	p.ir = ir
	tol := float64(gtx.Dp(14))
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.viewID, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
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
			if e.Buttons == pointer.ButtonPrimary && k.Busy == "" {
				k.MouseDown(ir, x, y, tol)
			}
		case pointer.Drag:
			k.MouseMove(ir, x, y)
		case pointer.Release, pointer.Cancel:
			k.MouseUp()
		}
	}
	fillRect(gtx, cViewBg, image.Rectangle{Max: sz})
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	if k.Preview == nil {
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			l := material.Label(th.Theme, textBody, "Open a 16-bit TIFF of the 24-patch transmission chart (or Use last scan).\nScan it with the same light levels as your film; no patch may clip.")
			l.Color, l.Alignment, l.Font.Weight = calGrey, text.Middle, font.Bold
			return l.Layout(gtx)
		})
	} else {
		if p.imgSeq != k.PreviewSeq {
			p.imgSeq = k.PreviewSeq
			p.img = paint.NewImageOp(k.Preview)
		}
		drawImage(gtx, p.img, ir)
		okC, badC := rgb(60, 210, 90), rgb(235, 60, 60)
		w := float64(max(1, ir.Dx()))
		cw, _ := k.ImageSize()
		hv := int(k.Corners.PatchHalf(0.25) / float64(max(1, cw)) * w)
		pw := gtx.Dp(2)
		for i := 0; i < 24; i++ {
			x, y := k.Corners.PatchCentre(i)
			c := k.ToView(ir, x, y)
			col, l := okC, colorsci.CamPatchNames[i]
			if k.Patches != nil && k.Patches[i].Clipped {
				col, l = badC, l+" CLIP"
			}
			strokeRect(gtx, col, image.Rect(c.X-hv, c.Y-hv, c.X+hv+1, c.Y+hv+1), pw)
			p.small(gtx, l, col, image.Pt(c.X-hv+3, c.Y-hv+2))
		}
		labels := [4]string{"A1", "A6", "D6", "D1"}
		for i, cn := range k.Corners {
			c := k.ToView(ir, cn[0], cn[1])
			ring(gtx, c, gtx.Dp(7), pw, cWhite)
			p.small(gtx, labels[i], cWhite, image.Pt(c.X+gtx.Dp(9), c.Y+gtx.Dp(4)))
		}
	}
	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	eventOp(gtx, &p.viewID)
	st.Pop()
	return layout.Dimensions{Size: sz}
}

func (p *calPanel) small(gtx layout.Context, s string, col color.NRGBA, at image.Point) {
	off := op.Offset(at).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(gtx.Dp(80), gtx.Dp(16))}
	p.u.th.label(g, textSmall, col, s, true, false)
	off.Pop()
}

// ring is a circle outline of radius r (DrawEllipsePixels).
func ring(gtx layout.Context, c image.Point, r, w int, col color.NRGBA) {
	e := clip.Ellipse{Min: image.Pt(c.X-r, c.Y-r), Max: image.Pt(c.X+r+1, c.Y+r+1)}
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: e.Path(gtx.Ops), Width: float32(w)}.Op())
}
