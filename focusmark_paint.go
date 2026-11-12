package main

// Walk-canvas drawing of the display-only centre / flatness overlay.
// Geometry lives in package focusmark so it can be tested on any OS.

import (
	"math"

	"basetool/focusmark"

	"github.com/lxn/walk"
)

const (
	markCenterArm = 28
	markCenterGap = 4
	markCenterBox = 7
	markCornerArm = 14
	markEdgeArm   = 10
)

func paintCenterMarks(c *walk.Canvas, src, dst walk.Rectangle, imgW, imgH int) {
	if c == nil || imgW <= 0 || imgH <= 0 || dst.Width <= 0 || dst.Height <= 0 {
		return
	}
	v := focusmark.View{
		ImgW: imgW, ImgH: imgH,
		SrcX: src.X, SrcY: src.Y, SrcW: src.Width, SrcH: src.Height,
		DstX: dst.X, DstY: dst.Y, DstW: dst.Width, DstH: dst.Height,
	}
	ink, _ := walk.NewSolidColorBrush(walk.RGB(255, 255, 255))
	edge, _ := walk.NewSolidColorBrush(walk.RGB(0, 0, 0))
	if ink == nil || edge == nil {
		if ink != nil {
			ink.Dispose()
		}
		if edge != nil {
			edge.Dispose()
		}
		return
	}
	defer ink.Dispose()
	defer edge.Dispose()
	inner, err1 := walk.NewGeometricPen(walk.PenSolid, 1, ink)
	outer, err2 := walk.NewGeometricPen(walk.PenSolid, 1, edge)
	if err1 != nil || err2 != nil || inner == nil || outer == nil {
		if inner != nil {
			inner.Dispose()
		}
		if outer != nil {
			outer.Dispose()
		}
		return
	}
	defer inner.Dispose()
	defer outer.Dispose()

	clip := dst
	for _, m := range focusmark.Marks(imgW, imgH) {
		x, y, ok := v.Map(m.IX, m.IY)
		if !ok {
			continue
		}
		px, py := int(math.Round(x)), int(math.Round(y))
		switch m.Kind {
		case focusmark.KindCentre:
			g, a := markCenterGap, markCenterArm
			outlinedH(c, inner, outer, clip, px-a, px-g, py)
			outlinedH(c, inner, outer, clip, px+g, px+a, py)
			outlinedV(c, inner, outer, clip, px, py-a, py-g)
			outlinedV(c, inner, outer, clip, px, py+g, py+a)
			half := markCenterBox / 2
			outlinedH(c, inner, outer, clip, px-half, px+half, py-half)
			outlinedH(c, inner, outer, clip, px-half, px+half, py+half)
			outlinedV(c, inner, outer, clip, px-half, py-half, py+half)
			outlinedV(c, inner, outer, clip, px+half, py-half, py+half)
		case focusmark.KindCorner:
			outlinedH(c, inner, outer, clip, px, px+m.InX*markCornerArm, py)
			outlinedV(c, inner, outer, clip, px, py, py+m.InY*markCornerArm)
		case focusmark.KindEdge:
			if m.InX == 0 {
				outlinedH(c, inner, outer, clip, px-markEdgeArm, px+markEdgeArm, py)
				outlinedV(c, inner, outer, clip, px, py, py+m.InY*markEdgeArm)
			} else {
				outlinedV(c, inner, outer, clip, px, py-markEdgeArm, py+markEdgeArm)
				outlinedH(c, inner, outer, clip, px, px+m.InX*markEdgeArm, py)
			}
		}
	}
}

func outlinedH(c *walk.Canvas, inner, outer *walk.GeometricPen, clip walk.Rectangle, x0, x1, y int) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if !clipH(&x0, &x1, y, clip) {
		return
	}
	_ = c.DrawLinePixels(outer, walk.Point{X: x0, Y: y - 1}, walk.Point{X: x1, Y: y - 1})
	_ = c.DrawLinePixels(outer, walk.Point{X: x0, Y: y + 1}, walk.Point{X: x1, Y: y + 1})
	_ = c.DrawLinePixels(inner, walk.Point{X: x0, Y: y}, walk.Point{X: x1, Y: y})
}

func outlinedV(c *walk.Canvas, inner, outer *walk.GeometricPen, clip walk.Rectangle, x, y0, y1 int) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	if !clipV(x, &y0, &y1, clip) {
		return
	}
	_ = c.DrawLinePixels(outer, walk.Point{X: x - 1, Y: y0}, walk.Point{X: x - 1, Y: y1})
	_ = c.DrawLinePixels(outer, walk.Point{X: x + 1, Y: y0}, walk.Point{X: x + 1, Y: y1})
	_ = c.DrawLinePixels(inner, walk.Point{X: x, Y: y0}, walk.Point{X: x, Y: y1})
}

func clipH(x0, x1 *int, y int, r walk.Rectangle) bool {
	if y < r.Y || y >= r.Y+r.Height {
		return false
	}
	if *x0 < r.X {
		*x0 = r.X
	}
	if *x1 > r.X+r.Width-1 {
		*x1 = r.X + r.Width - 1
	}
	return *x0 <= *x1
}

func clipV(x int, y0, y1 *int, r walk.Rectangle) bool {
	if x < r.X || x >= r.X+r.Width {
		return false
	}
	if *y0 < r.Y {
		*y0 = r.Y
	}
	if *y1 > r.Y+r.Height-1 {
		*y1 = r.Y + r.Height - 1
	}
	return *y0 <= *y1
}
