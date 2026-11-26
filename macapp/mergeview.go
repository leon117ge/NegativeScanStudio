//go:build darwin || (linux && gio)

package main

// Crop & Merge > Merge files (the merge TabPage of ../hdrpage.go). The
// merge runs on a worker goroutine with a progress bar and a Stop button
// (macOS additions; Windows shows the progress on the Camera page).

import (
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"basetool/macapp/appstate"
)

type mergePage struct {
	u                                *UI
	eds                              [3]Field
	browse                           [3]Button
	seq                              int
	alignCB, mergeCB, compCB, dustCB Combo
	mergeBtn, openBtn, stopBtn       Button
	res                              LogBox
	list                             widget.List
}

func newMergePage(u *UI) *mergePage {
	p := &mergePage{u: u, seq: -1}
	p.list.Axis = layout.Vertical
	return p
}

// sync pushes Paths into the fields when the page set them, else reads
// the fields back (typed paths).
func (p *mergePage) sync() {
	m := &p.u.hdr.MF
	if p.seq != m.PathSeq {
		p.seq = m.PathSeq
		for i := range p.eds {
			if p.eds[i].Text() != m.Paths[i] {
				p.eds[i].SetText(m.Paths[i])
			}
		}
		return
	}
	for i := range p.eds {
		m.Paths[i] = p.eds[i].Text()
	}
}

const mergeLW = 120

func (p *mergePage) layout(gtx layout.Context) layout.Dimensions {
	p.sync()
	defer p.sync()
	th, c, hp := p.u.th, p.u.hdr, p.u.hp
	m := &c.MF
	labels := []string{"Frame 0", "Brighter frame\n(+2 or +3 EV)", "Frame +3 or +4 EV\n(optional)"}
	tips := []string{"", "", "Leave empty to merge two files. The EV step (+3 or +4) is detected from the RAW exposure times or the measured ratio."}
	combo := func(label string, cb *Combo, items []string, sel *int, tip string) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			return gridRow(gtx, th, label, mergeLW, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(260))
				d, _ := cb.Layout(gtx, th, items, sel, false, tip)
				return d
			})
		}
	}
	rows := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions {
			return th.hint(gtx, "Merge a bracket you already have: frame 0 (base metered), the frame shot 2 EV brighter and optionally one 3 or 4 EV brighter.\nRAF files are converted with the same linear LibRaw settings as the batch converter; 16-bit TIFFs from this program\ncan be used directly. You can also drop 2 or 3 files here.")
		},
	}
	for i := range labels {
		i := i
		rows = append(rows, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: 5}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return gridRow(gtx, th, labels[i], mergeLW, func(gtx layout.Context) layout.Dimensions {
					return hbox(gtx, 8,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X // full width like the walk LineEdit
							d, _ := p.eds[i].Layout(gtx, th, "", tips[i])
							return d
						}),
						layout.Rigid(hp.button(&p.browse[i], "Browse...", btnOpts{minW: 80}, func() { p.sync(); c.BrowseMerge(i) })))
				})
			})
		})
	}
	rows = append(rows,
		combo("Alignment", &p.alignCB, appstate.AlignItems, &m.Align, ""),
		combo("Merge", &p.mergeCB, appstate.MergeItems, &m.Merge, appstate.MergeTip),
		combo("Compression", &p.compCB, appstate.CompItems, &m.Comp, ""),
		combo("Dust removal", &p.dustCB, appstate.DustItems, &m.Dust, "Off / Light / Strong. Dust removal is applied into the one output <name>_HDR.tiff (no uncleaned copy is kept)."),
		layout.Spacer{Height: 4}.Layout,
		func(gtx layout.Context) layout.Dimensions {
			busy := c.MergeBusy()
			return hbox(gtx, 6,
				layout.Rigid(hp.button(&p.mergeBtn, "Merge to HDR TIFF", btnOpts{primary: true, minW: 200, h: 32, disabled: busy}, func() { p.sync(); c.RunMerge() })),
				layout.Rigid(hp.button(&p.openBtn, "Open in Develop", btnOpts{h: 32, disabled: !c.CanOpenMerge(), tip: "Open the merged HDR TIFF in Develop for preview."}, c.OpenMergeInDevelop)),
				layout.Rigid(hp.button(&p.stopBtn, "Stop", btnOpts{h: 32, disabled: !c.MergeStoppable(),
					tip: "Stop the merge at its next stage. Partial output is removed; the source files are never changed."}, c.StopMerge)),
				layout.Flexed(1, layout.Spacer{}.Layout))
		},
		layout.Spacer{Height: 6}.Layout,
		func(gtx layout.Context) layout.Dimensions { return progressBar(gtx, c.MergeStoppable()) },
		layout.Spacer{Height: 6}.Layout,
	)
	foot := "Merge: each frame's ratio is measured against the next darker frame on pixels unclipped in the brighter one and chained\n(+4/0 = +4/+2 x +2/0; ~4, ~8, ~16). If a frame is too clipped to measure, the RAW exposure times are used instead.\nFrame 0 gives the highlights (clear base), +2 the mid-dense areas, +3/+4 the densest; where a channel reaches 0.85 of\nfull scale (mask grown by 4 px) the darker data is kept, and the clear base always keeps frame 0's exact values."
	return layout.UniformInset(12).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				var ch []layout.FlexChild
				for _, r := range rows {
					ch = append(ch, layout.Rigid(r))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = gtx.Constraints.Max
				gtx.Constraints.Min.Y = max(gtx.Constraints.Min.Y, gtx.Dp(80))
				return p.res.Layout(gtx, th, c.MRes, c.MResSeq, 80)
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(th.Theme, textBody, foot)
				l.Color = cGrey
				return l.Layout(gtx)
			}),
		)
	})
}
