//go:build darwin || (linux && gio)

package main

import (
	"image/color"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/layout"
)

func f32pt(x, y int) f32.Point    { return f32.Pt(float32(x), float32(y)) }
func f32p(x, y float32) f32.Point { return f32.Pt(x, y) }
func scaleAff(sx, sy float32) f32.Affine2D {
	return f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sx, sy))
}
func eventOp(gtx layout.Context, tag any) { event.Op(gtx.Ops, tag) }

func colorA(r, g, b, a uint8) color.NRGBA { return color.NRGBA{r, g, b, a} }
