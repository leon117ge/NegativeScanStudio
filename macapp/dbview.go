//go:build darwin || (linux && gio)

package main

// Film-base DB dialogs (hdrdb.go saveToDB / manageDB on Windows): the two
// modal dialogs become in-window panels over the page with the same rows,
// labels, cue banners and button order. The logic is hdrctl/filmdb.go.

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

type dbPanel struct {
	u                        *UI
	actCB, updCB, avgCB, mCB Combo
	nameEd, lightEd, mNameEd Field
	saveBtn, cancelBtn       Button
	renBtn, delBtn, closeBtn Button
	saveSeq, manSeq          int
}

func newDBPanel(u *UI) *dbPanel { return &dbPanel{u: u, saveSeq: -1, manSeq: -1} }

// layout draws whichever DB panel is open, centred over the window.
func (p *dbPanel) layout(gtx layout.Context) {
	c := p.u.hdr
	var w layout.Widget
	switch {
	case c.DBSave.Shown:
		w = p.save
	case c.DBMan.Shown:
		w = p.manage
	default:
		return
	}
	full := image.Rectangle{Max: gtx.Constraints.Max}
	paint.FillShape(gtx.Ops, colorA(0, 0, 0, 150), clip.Rect(full).Op())
	st := clip.Rect(full).Push(gtx.Ops)
	eventOp(gtx, p) // swallow input behind the panel
	st.Pop()
	pw := min(gtx.Dp(560), full.Dx()-gtx.Dp(40))
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(pw, 0), Max: image.Pt(pw, full.Dy()-gtx.Dp(40))}
	m := op.Record(gtx.Ops)
	d := layout.Inset{Left: 12, Top: 10, Right: 12, Bottom: 10}.Layout(g, w)
	call := m.Stop()
	at := image.Pt((full.Dx()-pw)/2, max(gtx.Dp(20), (full.Dy()-d.Size.Y)/2))
	roundBox(gtx, cPanel, cAccent, image.Rectangle{Min: at, Max: at.Add(image.Pt(pw, d.Size.Y))}, gtx.Dp(6))
	off := op.Offset(at).Push(gtx.Ops)
	call.Add(gtx.Ops)
	off.Pop()
}

const dbLW unit.Dp = 120 // label column (Grid{Columns: 2})

func (p *dbPanel) row(gtx layout.Context, label string, w layout.Widget) layout.Dimensions {
	return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return gridRow(gtx, p.u.th, label, dbLW, w)
	})
}

func (p *dbPanel) btn(b *Button, txt string, f func()) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		d, clk := b.Layout(gtx, p.u.th, txt, btnOpts{minW: 90})
		if clk {
			f()
		}
		return d
	})
}

func (p *dbPanel) text(s string) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return p.u.th.text(gtx, s)
		})
	})
}

func (p *dbPanel) gap() layout.FlexChild {
	return layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
	})
}

// save is the saveToDB dialog.
func (p *dbPanel) save(gtx layout.Context) layout.Dimensions {
	c, th := p.u.hdr, p.u.th
	d := c.DBSave
	if d.Seq != p.saveSeq {
		p.saveSeq = d.Seq
		p.nameEd.SetText(d.Name)
		p.lightEd.SetText(d.Light)
	}
	combo := func(cb *Combo, items []string, sel *int, off bool, f func()) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			dm, ch := cb.Layout(gtx, th, items, sel, off, "")
			if ch && f != nil {
				f()
			}
			return dm
		}
	}
	field := func(f *Field, cue string, off bool, v *string) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			if off {
				gtx = gtx.Disabled()
			}
			dm, _ := f.Layout(gtx, th, cue, "")
			*v = f.Text()
			if off { // greyed like a disabled LineEdit
				dim := cPanel
				dim.A = 150
				paint.FillShape(gtx.Ops, dim, clip.Rect{Max: dm.Size}.Op())
			}
			return dm
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return th.bold(gtx, d.Title, cText) })
		}),
		p.text(d.Info),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.row(gtx, "Action", combo(&p.actCB, d.ActItems, &d.Act, false, c.DBSaveChanged))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.row(gtx, "New stock name", field(&p.nameEd, "e.g. Portra 400", !d.NameOn, &d.Name))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.row(gtx, "Existing stock", combo(&p.updCB, d.Names, &d.Sel, !d.UpdOn, c.DBSaveChanged))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.row(gtx, "Update mode", combo(&p.avgCB, d.AvgItems, &d.Avg, !d.AvgOn, nil))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.row(gtx, "", func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(30)
				dm := th.text(gtx, d.Cur)
				dm.Size.Y = max(dm.Size.Y, gtx.Dp(30))
				return dm
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.row(gtx, "Light source", field(&p.lightEd, "optional, e.g. CS-LITE 5000K", false, &d.Light))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return hbox(gtx, 6, p.gap(), p.btn(&p.saveBtn, "Save", c.DBSaveSave), p.btn(&p.cancelBtn, "Cancel", c.DBSaveCancel))
		}),
	)
}

// manage is the manageDB dialog ("Film-base DB").
func (p *dbPanel) manage(gtx layout.Context) layout.Dimensions {
	c, th := p.u.hdr, p.u.th
	m := c.DBMan
	if m.Seq != p.manSeq {
		p.manSeq = m.Seq
		p.mNameEd.SetText(m.Name)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return th.bold(gtx, "Film-base DB", cText) })
		}),
		p.text(m.Path),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.row(gtx, "Film stock", func(gtx layout.Context) layout.Dimensions {
				dm, ch := p.mCB.Layout(gtx, th, m.Names, &m.Sel, false, "")
				if ch {
					c.ManageSelect()
				}
				return dm
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.row(gtx, "", func(gtx layout.Context) layout.Dimensions {
				dm := th.text(gtx, m.Info)
				dm.Size.Y = max(dm.Size.Y, gtx.Dp(44))
				return dm
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.row(gtx, "Name", func(gtx layout.Context) layout.Dimensions {
				dm, _ := p.mNameEd.Layout(gtx, th, "", "")
				m.Name = p.mNameEd.Text()
				return dm
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return hbox(gtx, 6, p.btn(&p.renBtn, "Rename", c.ManageRename), p.btn(&p.delBtn, "Delete", c.ManageDelete),
				p.gap(), p.btn(&p.closeBtn, "Close", c.ManageClose))
		}),
	)
}
