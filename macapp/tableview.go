//go:build darwin || (linux && gio)

package main

// Table is the walk TableView of the Crop and Batch RAW to TIFF pages:
// a header row, alternating row backgrounds, multi selection (Cmd-click
// toggles, Shift-click selects a range) and per-cell text colours.

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type tableCol struct {
	title string
	w     unit.Dp // width; the last column takes the rest
}

type tableSource struct {
	rows     int
	cell     func(row, col int) string
	color    func(row, col int) color.NRGBA
	selected func(row int) bool
	click    func(row int, extend, span bool)
}

type Table struct {
	list widget.List
	rows []widget.Clickable
}

const tableRowDp = 20

func (t *Table) Layout(gtx layout.Context, th *Theme, cols []tableCol, src tableSource) layout.Dimensions {
	t.list.Axis = layout.Vertical
	if len(t.rows) < src.rows {
		t.rows = append(t.rows, make([]widget.Clickable, src.rows-len(t.rows))...)
	}
	for i := 0; i < src.rows; i++ {
		for {
			c, ok := t.rows[i].Update(gtx)
			if !ok {
				break
			}
			if src.click != nil {
				src.click(i, c.Modifiers.Contain(key.ModShortcut), c.Modifiers.Contain(key.ModShift))
			}
		}
	}
	sz := gtx.Constraints.Max
	roundBox(gtx, cField, cFieldLine, image.Rectangle{Max: sz}, gtx.Dp(3))
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	rh := gtx.Dp(tableRowDp)
	widths := func(w int) []int {
		ws := make([]int, len(cols))
		used := 0
		for i, c := range cols {
			if i == len(cols)-1 {
				ws[i] = max(w-used, gtx.Dp(40))
			} else {
				ws[i] = gtx.Dp(c.w)
				used += ws[i]
			}
		}
		return ws
	}(sz.X - gtx.Dp(2))
	cell := func(gtx layout.Context, x, w int, s string, col color.NRGBA, bold bool) {
		g := gtx
		g.Constraints = layout.Exact(image.Pt(max(w-gtx.Dp(8), 0), rh))
		st := clip.Rect{Min: image.Pt(x, 0), Max: image.Pt(x+w-gtx.Dp(4), rh)}.Push(gtx.Ops)
		defer st.Pop()
		off := image.Pt(x+gtx.Dp(5), 0)
		stack := opOffset(gtx, off)
		layout.W.Layout(g, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			l := material.Label(th.Theme, textBody, s)
			l.Color, l.MaxLines = col, 1
			if bold {
				l.Font.Weight = font.SemiBold
			}
			return l.Layout(gtx)
		})
		stack.Pop()
	}
	// header
	fillRect(gtx, cCardHead, image.Rect(1, 1, sz.X-1, rh+1))
	x := 1
	for i, c := range cols {
		cell(gtx, x, widths[i], c.title, cHeaderText, true)
		x += widths[i]
		if i < len(cols)-1 {
			fillRect(gtx, cBorder, image.Rect(x-1, 3, x, rh-2))
		}
	}
	body := image.Rect(1, rh+1, sz.X-1, sz.Y-1)
	off := opOffset(gtx, body.Min)
	g := gtx
	g.Constraints = layout.Exact(body.Size())
	material.List(th.Theme, &t.list).Layout(g, src.rows, func(gtx layout.Context, i int) layout.Dimensions {
		return t.rows[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			w := gtx.Constraints.Max.X
			bg := cField
			if i%2 == 1 {
				bg = rgb(30, 30, 33)
			}
			if src.selected != nil && src.selected(i) {
				bg = cListSel
			} else if t.rows[i].Hovered() {
				bg = rgb(40, 40, 44)
			}
			fillRect(gtx, bg, image.Rect(0, 0, w, rh))
			x := 0
			for c := range cols {
				col := cText
				if src.color != nil {
					col = src.color(i, c)
				}
				cell(gtx, x, widths[c], src.cell(i, c), col, false)
				x += widths[c]
			}
			return layout.Dimensions{Size: image.Pt(w, rh)}
		})
	})
	off.Pop()
	return layout.Dimensions{Size: sz}
}

// progressValue is a determinate ProgressBar (value of n).
func progressValue(gtx layout.Context, v, n int) layout.Dimensions {
	sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(8))
	roundBox(gtx, cField, cFieldLine, image.Rectangle{Max: sz}, gtx.Dp(3))
	if n > 0 && v > 0 {
		w := min(sz.X, sz.X*v/n)
		fillRect(gtx, cAccent, image.Rect(1, 1, max(w-1, 1), sz.Y-1))
	}
	return layout.Dimensions{Size: sz}
}
