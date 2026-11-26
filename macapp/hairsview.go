//go:build darwin || (linux && gio)

package main

// Crop & Merge > Remove hairs (hairUI.page in ../hairpage.go).

import (
	"image"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"basetool/macapp/hdrctl"
)

type hairPage struct {
	u                             *UI
	pathEd                        Field
	seq                           int
	browse, last, fit, undo, save Button
	modeCB                        Combo
	mask                          Check
	status                        LogBox
	statusSeq                     int
	statusText                    string
	tag                           int
	img                           paint.ImageOp
	imgSeq                        int
	imgSize                       image.Point
	ir                            image.Rectangle
	dragging                      bool
	dragA, dragB                  image.Point
}

func newHairPage(u *UI) *hairPage { return &hairPage{u: u, seq: -1, imgSeq: -1} }

func (p *hairPage) layout(gtx layout.Context) layout.Dimensions {
	th, hp := p.u.th, p.u.hp
	hu := p.u.hdr.Hair
	if p.seq != hu.PathSeq {
		p.seq = hu.PathSeq
		p.pathEd.SetText(hu.Path)
	}
	if p.statusText != hu.Status {
		p.statusText = hu.Status
		p.statusSeq++
	}
	undo, save, fit, load := hu.Buttons()
	return layout.UniformInset(12).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return th.hint(gtx, "Remove a hair or fibre by hand: draw a box around it. Only that box is searched and only the line found in it is healed\nwith film texture from beside it - shadows and image detail outside the line stay as they are. Undo takes the last box back.")
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return hbox(gtx, 6,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return th.text(gtx, "TIFF") }),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X // full width like the walk LineEdit
						d, fin := p.pathEd.Layout(gtx, th, "", "")
						if fin {
							hu.PathEdited(p.pathEd.Text())
						}
						return d
					}),
					layout.Rigid(hp.button(&p.browse, "Browse...", btnOpts{disabled: !load}, hu.Browse)),
					layout.Rigid(hp.button(&p.last, "Last HDR", btnOpts{tip: "Open the HDR TIFF made last in this session (Camera capture or Merge files)."}, hu.LastHDR)))
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				mask := hu.Mask
				return hbox(gtx, 6,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(260), gtx.Dp(260)
						d, _ := p.modeCB.Layout(gtx, th, hdrctl.HairModes, &hu.Mode, false, "")
						return d
					}),
					layout.Rigid(hp.button(&p.fit, "Fit", btnOpts{disabled: !fit, tip: "Show the whole image again."}, hu.Fit)),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						d, ch := p.mask.Layout(gtx, th, "Show mask", &mask, "Changed pixels in red, the boxes used in green.")
						if ch {
							hu.SetMask(mask)
						}
						return d
					}),
					layout.Rigid(hp.button(&p.undo, "Undo", btnOpts{disabled: !undo}, hu.Undo)),
					layout.Flexed(1, layout.Spacer{}.Layout),
					layout.Rigid(hp.button(&p.save, "Save into _HDR.tiff", btnOpts{primary: true, disabled: !save,
						tip: "An <name>_HDR.tiff from this program is updated in place (asks first). Any other TIFF is never changed: a new <name>_HDR.tiff is written next to it."}, hu.Save)))
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Flexed(1, p.view),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				gtx.Constraints.Max.Y = gtx.Dp(72)
				gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
				return p.status.Layout(gtx, th, hu.Status, p.statusSeq, 56)
			}),
		)
	})
}

// view is the preview (paint, mouseDown / mouseMove / mouseUp).
func (p *hairPage) view(gtx layout.Context) layout.Dimensions {
	th := p.u.th
	hu := p.u.hdr.Hair
	sz := gtx.Constraints.Max
	if sz.X <= 0 || sz.Y <= 0 {
		return layout.Dimensions{Size: gtx.Constraints.Min}
	}
	p.input(gtx, sz)
	fillRect(gtx, rgb(22, 22, 25), image.Rectangle{Max: sz})
	if !hu.Loaded() {
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			l := material.Label(th.Theme, unit.Sp(13), "No image.\nOpen an HDR TIFF (Browse..., Last HDR, or drop it here),\nthen drag a box around a hair.")
			l.Color, l.Alignment = rgb(128, 128, 136), text.Middle
			return l.Layout(gtx)
		})
	} else {
		vp := hu.Viewport()
		p.ir = hdrctl.FitRect(vp.Dx(), vp.Dy(), sz.X, sz.Y)
		if p.imgSeq != hu.Seq || p.imgSize != p.ir.Size() {
			p.imgSeq, p.imgSize = hu.Seq, p.ir.Size()
			if img := hu.Preview(p.ir.Dx(), p.ir.Dy()); img != nil {
				p.img = paint.NewImageOp(img)
			}
		}
		drawImage(gtx, p.img, p.ir)
		if hu.Mask {
			for _, r := range hu.Edits() {
				strokeRect(gtx, rgb(60, 220, 120), image.Rectangle{Min: p.toView(r.Min), Max: p.toView(r.Max)}, gtx.Dp(2))
			}
		}
		if p.dragging {
			col := rgb(255, 170, 40)
			if hu.Mode == 1 {
				col = rgb(90, 170, 255)
			}
			strokeRect(gtx, col, image.Rectangle{Min: p.dragA, Max: p.dragB}.Canon(), gtx.Dp(2))
		}
		if hu.Busy() {
			off := opOffset(gtx, image.Pt(p.ir.Min.X+8, p.ir.Min.Y+6))
			th.label(gtx, unit.Sp(13), rgb(255, 170, 40), "Working...", true, false)
			off.Pop()
		}
	}
	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	pointer.CursorCrosshair.Add(gtx.Ops)
	eventOp(gtx, &p.tag)
	st.Pop()
	return layout.Dimensions{Size: sz}
}

func (p *hairPage) toImage(pt image.Point) image.Point {
	vp := p.u.hdr.Hair.Viewport()
	ir := p.ir
	if ir.Dx() <= 0 || ir.Dy() <= 0 {
		return vp.Min
	}
	x := vp.Min.X + int(float64(pt.X-ir.Min.X)*float64(vp.Dx())/float64(ir.Dx()))
	y := vp.Min.Y + int(float64(pt.Y-ir.Min.Y)*float64(vp.Dy())/float64(ir.Dy()))
	return image.Pt(max(vp.Min.X, min(vp.Max.X, x)), max(vp.Min.Y, min(vp.Max.Y, y)))
}

func (p *hairPage) toView(pt image.Point) image.Point {
	vp := p.u.hdr.Hair.Viewport()
	ir := p.ir
	return image.Pt(ir.Min.X+int(float64(pt.X-vp.Min.X)*float64(ir.Dx())/float64(vp.Dx())),
		ir.Min.Y+int(float64(pt.Y-vp.Min.Y)*float64(ir.Dy())/float64(vp.Dy())))
}

func (p *hairPage) input(gtx layout.Context, sz image.Point) {
	hu := p.u.hdr.Hair
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
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
			if e.Buttons != pointer.ButtonPrimary || !hu.Loaded() || hu.Busy() {
				continue
			}
			p.dragging = true
			p.dragA, p.dragB = pt, pt
		case pointer.Drag:
			if p.dragging {
				p.dragB = pt
			}
		case pointer.Release, pointer.Cancel:
			if !p.dragging {
				continue
			}
			p.dragging = false
			if e.Kind == pointer.Cancel {
				continue
			}
			p.dragB = pt
			a, b := p.toImage(p.dragA), p.toImage(p.dragB)
			hu.Box(image.Rectangle{Min: a, Max: b}, sz.X, sz.Y)
		}
	}
}
