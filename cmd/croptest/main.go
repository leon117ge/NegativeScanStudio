// croptest runs the auto-straighten/crop detector on synthetic scans and
// (optionally) real files, and reports angle error and leftover border.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"time"

	"basetool/cropper"
)

func main() {
	out := flag.String("out", "./testdata/croptest", "output folder")
	write := flag.Bool("write", false, "write synthetic sources and crops")
	flag.Parse()
	os.MkdirAll(*out, 0o755)
	white := [3]float64{0.94, 0.92, 0.88}
	black := [3]float64{0.03, 0.03, 0.035}
	orange := [3]float64{0.80, 0.52, 0.30}
	cases := []struct {
		name string
		s    cropper.SynthSpec
	}{
		{"land_white_+0.7_16b", cropper.SynthSpec{W: 3000, H: 2000, Tilt: 0.7, FrameW: 2640, FrameH: 1760, OffX: 12, OffY: -9, Rebate: white, RebateW: 60, Holder: black, Bits: 16, Seed: 1}},
		{"land_black_-1.8_16b", cropper.SynthSpec{W: 3000, H: 2000, Tilt: -1.8, FrameW: 2700, FrameH: 1800, Rebate: black, RebateW: 0, Holder: black, Bits: 16, Seed: 2}},
		{"port_white_+0.7_16b", cropper.SynthSpec{W: 2000, H: 3000, Tilt: 0.7, FrameW: 1760, FrameH: 2640, OffX: -15, Rebate: white, RebateW: 0, Holder: white, Bits: 16, Seed: 3}},
		{"port_black_-1.8_16b", cropper.SynthSpec{W: 2000, H: 3000, Tilt: -1.8, FrameW: 1780, FrameH: 2670, OffY: 20, Rebate: black, RebateW: 0, Holder: black, Bits: 16, Seed: 4}},
		{"land_sprocket_leak_+1.2", cropper.SynthSpec{W: 3200, H: 2200, Tilt: 1.2, FrameW: 2700, FrameH: 1800, Rebate: orange, RebateW: 150, Holder: black, Sprockets: true, LightLeak: true, Bits: 16, Seed: 5}},
		{"land_horizon_-0.4_8b", cropper.SynthSpec{W: 3000, H: 2000, Tilt: -0.4, FrameW: 2600, FrameH: 1733, OffY: 25, Rebate: white, RebateW: 40, Holder: [3]float64{0.98, 0.98, 0.98}, Horizon: true, Bits: 8, Seed: 6}},
		{"land_partial_0.0", cropper.SynthSpec{W: 3000, H: 2000, Tilt: 0.0, FrameW: 2900, FrameH: 2300, OffX: 0, OffY: 0, Rebate: black, Holder: black, Bits: 16, Seed: 7}},
		{"land_tinted_+3.5", cropper.SynthSpec{W: 3000, H: 2000, Tilt: 3.5, FrameW: 2500, FrameH: 1667, Rebate: orange, RebateW: 0, Holder: orange, Bits: 16, Seed: 8}},
	}
	fmt.Printf("%-26s %7s %8s %7s %6s %-28s %9s %8s %7s\n", "case", "true°", "found°", "err°", "conf", "edges", "border%", "keep%", "ms")
	for _, c := range cases {
		im, inside := cropper.Synth(c.s)
		t0 := time.Now()
		p := cropper.MakeProxy(im, cropper.ProxySide)
		det := cropper.Detect(p)
		r, _ := cropper.AutoRect(im.W, im.H, det.Angle, &det, p.Scale, cropper.AspectAuto, 0.01)
		ms := time.Since(t0).Milliseconds()
		x0, y0, w, h := cropper.IntRect(r, im.W, im.H)
		// leftover border: output pixels whose source lies outside the picture
		bad := 0
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				sx, sy := cropper.RotInv(det.Angle, float64(im.W), float64(im.H), float64(x0+x)+0.5, float64(y0+y)+0.5)
				if inside(sx, sy) < 0 {
					bad++
				}
			}
		}
		keep := float64(w*h) / (c.s.FrameW * c.s.FrameH) * 100
		fmt.Printf("%-26s %7.2f %8.3f %7.3f %6.2f %-28s %9.4f %8.1f %7d\n", c.name, c.s.Tilt, det.Angle, det.Angle-c.s.Tilt, det.Confidence, det.Message, 100*float64(bad)/float64(w*h), keep, ms)
		if *write {
			src := filepath.Join(*out, c.name+".tif")
			cropper.Save(src, im, cropper.SaveOptions{LZW: true})
			o := cropper.Apply(im, det.Angle, r, cropper.InterpBicubic)
			cropper.Save(filepath.Join(*out, c.name+"_crop.tif"), o, cropper.SaveOptions{LZW: true})
		}
	}
	for _, f := range flag.Args() {
		im, err := cropper.Load(f)
		if err != nil {
			fmt.Println(f, err)
			continue
		}
		t0 := time.Now()
		p := cropper.MakeProxy(im, cropper.ProxySide)
		det := cropper.Detect(p)
		r, _ := cropper.AutoRect(im.W, im.H, det.Angle, &det, p.Scale, cropper.AspectAuto, 0.01)
		x0, y0, w, h := cropper.IntRect(r, im.W, im.H)
		fmt.Printf("%s %dx%d %d-bit: angle %.3f conf %.2f %s crop %d,%d %dx%d (%.0f ms)\n", filepath.Base(f), im.W, im.H, im.Bits, det.Angle, det.Confidence, det.Message, x0, y0, w, h, float64(time.Since(t0).Milliseconds()))
		for _, e := range det.Edges {
			fmt.Printf("   %d tilt %.3f sup %.2f uni %.2f con %.2f  P0 %.0f,%.0f P1 %.0f,%.0f\n", e.Side, e.Tilt, e.Support, e.Uniform, e.Contrast, e.P0[0], e.P0[1], e.P1[0], e.P1[1])
		}
		dbg(filepath.Join(*out, "dbg_"+filepath.Base(f)+".png"), p, det, r, im.W, im.H)
		if *write {
			o := cropper.Apply(im, det.Angle, r, cropper.InterpBicubic)
			base := filepath.Base(f)
			ext := filepath.Ext(base)
			cropper.Save(filepath.Join(*out, base[:len(base)-len(ext)]+"_crop"+ext), o, cropper.SaveOptions{LZW: true, JPEGQuality: 92})
		}
		_ = math.Abs
	}
}

func dbg(path string, p *cropper.Proxy, det cropper.Detection, r cropper.Rect, W, H int) {
	buf := cropper.RenderProxy(p, 0)
	img := &image.RGBA{Pix: buf, Stride: p.W * 4, Rect: image.Rect(0, 0, p.W, p.H)}
	line := func(x0, y0, x1, y1 float64, c color.RGBA) {
		n := int(math.Hypot(x1-x0, y1-y0)/p.Scale) + 1
		for i := 0; i <= n; i++ {
			t := float64(i) / float64(n)
			x := (x0 + t*(x1-x0)) / p.Scale
			y := (y0 + t*(y1-y0)) / p.Scale
			img.Set(int(x), int(y), c)
			img.Set(int(x)+1, int(y), c)
		}
	}
	for _, e := range det.Edges {
		line(e.P0[0], e.P0[1], e.P1[0], e.P1[1], color.RGBA{0, 255, 0, 255})
	}
	fw, fh := float64(W), float64(H)
	var cs [4][2]float64
	for i, c := range [4][2]float64{{r.X0, r.Y0}, {r.X1, r.Y0}, {r.X1, r.Y1}, {r.X0, r.Y1}} {
		x, y := cropper.RotInv(det.Angle, fw, fh, c[0], c[1])
		cs[i] = [2]float64{x, y}
	}
	for i := 0; i < 4; i++ {
		a, b := cs[i], cs[(i+1)%4]
		line(a[0], a[1], b[0], b[1], color.RGBA{255, 128, 0, 255})
	}
	f, _ := os.Create(path)
	png.Encode(f, img)
	f.Close()
}
