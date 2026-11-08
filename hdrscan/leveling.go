package hdrscan

import (
	"basetool/lightlevel"
	"fmt"
)

// LevelingImage samples actual pixels on the same fixed lattice for both the
// control captures and the calibration TIFF. No cell is marked clipped just
// because one of its many photosites was clipped. The median is shared with
// lightlevel.Run; linear LibRaw conversion is shared with ProcessTrichrome.
func LevelingImage(p *Plane, ch int) *lightlevel.Image {
	step := max(1, min(16, min(p.W, p.H)/8))
	w, h := p.W/step, p.H/step
	im := &lightlevel.Image{W: w, H: h, V: make([]float32, w*h), Clip: make([]bool, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := p.Pix[3*((y*step+step/2)*p.W+x*step+step/2)+ch]
			im.V[y*w+x] = float32(v) / 65535
			im.Clip[y*w+x] = v >= 65207
		}
	}
	return im
}

// FindLevelingArea selects once from the initial demosaiced channel samples.
// The returned rectangle is then frozen for every solver and TIFF reading.
func FindLevelingArea(images [3]*lightlevel.Image) (lightlevel.Rect, error) {
	if images[0] == nil {
		return lightlevel.Rect{}, fmt.Errorf("no initial R capture")
	}
	w, h := images[0].W, images[0].H
	rgb := make([]float32, w*h*3)
	clip := make([]bool, w*h)
	for c, im := range images {
		if im == nil || im.W != w || im.H != h {
			return lightlevel.Rect{}, fmt.Errorf("channel image dimensions differ")
		}
		for i, v := range im.V {
			rgb[3*i+c] = v
			if im.Clip != nil && im.Clip[i] {
				clip[i] = true
			}
		}
	}
	if f, ok := findBase(w, h, rgb, clip); ok {
		return lightlevel.Rect{X0: float64(f.X0) / float64(w), Y0: float64(f.Y0) / float64(h), X1: float64(f.X1) / float64(w), Y1: float64(f.Y1) / float64(h)}, nil
	}
	x0, y0, x1, y1, ok := autoWindow(w, h, rgb, clip)
	if !ok {
		return lightlevel.Rect{}, fmt.Errorf("no clean base region; choose Rectangle")
	}
	return lightlevel.Rect{X0: float64(x0) / float64(w), Y0: float64(y0) / float64(h), X1: float64(x1) / float64(w), Y1: float64(y1) / float64(h)}, nil
}
