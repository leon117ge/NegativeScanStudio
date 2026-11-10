// looksheet renders preview4 Convert positives of a RAF through the
// plugin path the user views real scans with: CameraCal matrix,
// Density In, stock Positive Out (Cineon), Kodak 2383 LUT, displayed
// as sRGB with no further transform.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"basetool/colorsci"
	"basetool/convert"
	"basetool/develop"
	"basetool/rawconv"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func srgbEnc(c float64) uint8 {
	if c <= 0 {
		return 0
	}
	if c >= 1 {
		return 255
	}
	var s float64
	if c <= 0.0031308 {
		s = 12.92 * c
	} else {
		s = 1.055*math.Pow(c, 1.0/2.4) - 0.055
	}
	return uint8(s*255 + 0.5)
}

func dispByte(c float64) uint8 {
	if c <= 0 {
		return 0
	}
	if c >= 1 {
		return 255
	}
	return uint8(c*255 + 0.5)
}

func downscale(pix []uint16, w, h, maxEdge int) ([]uint16, int, int) {
	if maxEdge <= 0 || (w <= maxEdge && h <= maxEdge) {
		return pix, w, h
	}
	scale := float64(maxEdge) / float64(w)
	if h > w {
		scale = float64(maxEdge) / float64(h)
	}
	nw := int(float64(w)*scale + 0.5)
	nh := int(float64(h)*scale + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	out := make([]uint16, nw*nh*3)
	for y := 0; y < nh; y++ {
		sy := y * h / nh
		for x := 0; x < nw; x++ {
			sx := x * w / nw
			si, di := (sy*w+sx)*3, (y*nw+x)*3
			out[di], out[di+1], out[di+2] = pix[si], pix[si+1], pix[si+2]
		}
	}
	return out, nw, nh
}

func toFloat(pix []uint16) []float64 {
	out := make([]float64, len(pix))
	for i, v := range pix {
		out[i] = float64(v) / 65535
	}
	return out
}

func stats(rgb []float64, skipBorder float64, w, h int) (mean [3]float64, p1, p50, p99, rb, sat float64) {
	x0, x1 := int(float64(w)*skipBorder), int(float64(w)*(1-skipBorder))
	y0, y1 := int(float64(h)*skipBorder), int(float64(h)*(1-skipBorder))
	if x1 <= x0 || y1 <= y0 {
		x0, y0, x1, y1 = 0, 0, w, h
	}
	n := (x1 - x0) * (y1 - y0)
	var sum [3]float64
	var satSum float64
	hist := make([]int, 1000)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := (y*w + x) * 3
			r, g, b := rgb[i], rgb[i+1], rgb[i+2]
			sum[0] += r
			sum[1] += g
			sum[2] += b
			mx := math.Max(r, math.Max(g, b))
			mn := math.Min(r, math.Min(g, b))
			if mx > 1e-8 {
				satSum += (mx - mn) / mx
			}
			Y := 0.2126*r + 0.7152*g + 0.0722*b
			k := int(Y * 999)
			if k < 0 {
				k = 0
			}
			if k > 999 {
				k = 999
			}
			hist[k]++
		}
	}
	nf := float64(n)
	mean = [3]float64{sum[0] / nf, sum[1] / nf, sum[2] / nf}
	rb = mean[0] / math.Max(mean[2], 1e-8)
	sat = satSum / nf
	pick := func(p float64) float64 {
		want := int(p * nf)
		acc := 0
		for i, c := range hist {
			acc += c
			if acc >= want {
				return float64(i) / 999
			}
		}
		return 1
	}
	return mean, pick(0.01), pick(0.50), pick(0.99), rb, sat
}

func densStats(neg []float64, w, h int, m colorsci.Mat3) {
	x0, x1 := int(float64(w)*0.08), int(float64(w)*0.92)
	y0, y1 := int(float64(h)*0.08), int(float64(h)*0.92)
	var sum, p99 [3]float64
	n := 0
	hist := [3][]int{make([]int, 400), make([]int, 400), make([]int, 400)}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := (y*w + x) * 3
			p := colorsci.ApplyCam(m, 1, [3]float64{neg[i], neg[i+1], neg[i+2]})
			for c := 0; c < 3; c++ {
				t := math.Max(p[c], 1e-6)
				d := -math.Log10(t / convert.BaseFS)
				if d < 0 {
					d = 0
				}
				sum[c] += d
				k := int(d * 100)
				if k < 0 {
					k = 0
				}
				if k > 399 {
					k = 399
				}
				hist[c][k]++
			}
			n++
		}
	}
	fmt.Printf("  after-profile D-Dmin mean=%.3f/%.3f/%.3f", sum[0]/float64(n), sum[1]/float64(n), sum[2]/float64(n))
	for c := 0; c < 3; c++ {
		want := int(0.99 * float64(n))
		acc := 0
		for i, v := range hist[c] {
			acc += v
			if acc >= want {
				p99[c] = float64(i) / 100
				break
			}
		}
	}
	fmt.Printf("  p99=%.3f/%.3f/%.3f\n", p99[0], p99[1], p99[2])
}

func viewProgram(s convert.Stock) develop.Program {
	ch := develop.DefaultChain()
	d := &ch.Nodes[0]
	d.V["base_r"], d.V["base_g"], d.V["base_b"] = convert.BaseFS, convert.BaseFS, convert.BaseFS
	d.V["preset"], d.V["cast_on"], d.V["out_trans"], d.V["in_di"] = 0, 0, 0, 0
	ch.Nodes[1].Bypass = true
	p := &ch.Nodes[2]
	p.V["gam_r"] = s.Gamma[0] * 1.15
	p.V["gam_g"] = s.Gamma[1] * 1.15
	p.V["gam_b"] = s.Gamma[2] * 1.15
	p.V["expo"] = 0.9
	p.V["bal_r"], p.V["bal_g"], p.V["bal_b"] = 0, 0, 0
	p.V["film_wb"], p.V["gray_on"], p.V["contrast"], p.V["out_mode"] = 0, 0, 0, 2
	for i := 3; i <= 6; i++ {
		ch.Nodes[i].Bypass = true
	}
	ch.Nodes[7].Bypass = false
	ch.Nodes[7].V["white"] = 2
	ch.Nodes[7].V["source"] = 0
	ch.Nodes[8].Bypass = true
	prog := ch.Compile(&develop.Context{}, -1)
	for _, n := range prog.Notes {
		fmt.Println("  chain:", n)
	}
	return prog
}

func invertView(neg []float64, w, h int, s convert.Stock, m colorsci.Mat3) []float64 {
	prog := viewProgram(s)
	out := make([]float64, len(neg))
	for i := 0; i < len(neg); i += 3 {
		p := colorsci.ApplyCam(m, 1, [3]float64{neg[i], neg[i+1], neg[i+2]})
		r, g, b := prog.Pixel(float32(p[0]), float32(p[1]), float32(p[2]))
		out[i], out[i+1], out[i+2] = float64(r), float64(g), float64(b)
	}
	return out
}

func writePNGDisp(path string, rgb []float64, w, h int, linear bool) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			var pix color.RGBA
			if linear {
				pix = color.RGBA{srgbEnc(rgb[i]), srgbEnc(rgb[i+1]), srgbEnc(rgb[i+2]), 255}
			} else {
				pix = color.RGBA{dispByte(rgb[i]), dispByte(rgb[i+1]), dispByte(rgb[i+2]), 255}
			}
			img.SetRGBA(x, y, pix)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func label(img *image.RGBA, x, y int, s string) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.RGBA{0, 0, 0, 255}), Face: basicfont.Face7x13, Dot: fixed.P(x+1, y+1)}
	d.DrawString(s)
	d.Dot = fixed.P(x, y)
	d.Src = image.NewUniform(color.RGBA{255, 255, 255, 255})
	d.DrawString(s)
}

func scaleTo(rgb []float64, w, h, nw, nh int) []float64 {
	out := make([]float64, nw*nh*3)
	for y := 0; y < nh; y++ {
		sy := y * h / nh
		for x := 0; x < nw; x++ {
			sx := x * w / nw
			si, di := (sy*w+sx)*3, (y*nw+x)*3
			out[di], out[di+1], out[di+2] = rgb[si], rgb[si+1], rgb[si+2]
		}
	}
	return out
}

func report(tag string, rgb []float64, w, h int) {
	mean, p1, p50, p99, rb, sat := stats(rgb, 0.06, w, h)
	fmt.Printf("%-22s mean=%.3f/%.3f/%.3f  R/B=%.3f sat=%.3f  Y p1/p50/p99=%.3f/%.3f/%.3f\n",
		tag, mean[0], mean[1], mean[2], rb, sat, p1, p50, p99)
}

func main() {
	src := "/workspace/user_raf/DSCF5795.RAF"
	outDir := "/workspace/convert_compare"
	if len(os.Args) > 1 {
		src = os.Args[1]
	}
	if len(os.Args) > 2 {
		outDir = os.Args[2]
	}
	os.MkdirAll(outDir, 0755)
	m := convert.DefaultXT3Trichrome

	fmt.Println("decoding scene", src)
	refImg, err := rawconv.DecodeScene(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, "DecodeScene", err)
		os.Exit(1)
	}
	fmt.Printf("camera=%s %s  %dx%d  cam_mul=%.3f,%.3f,%.3f\n",
		refImg.Make, refImg.Model, refImg.Width, refImg.Height, refImg.CamMul[0], refImg.CamMul[1], refImg.CamMul[2])
	rp := make([]uint16, refImg.Width*refImg.Height*3)
	copy(rp, refImg.Pix[:len(rp)])
	rp, rw, rh := downscale(rp, refImg.Width, refImg.Height, 1600)
	refImg.Free()
	refLin := toFloat(rp)
	sc := convert.AutoEVScale(refLin, rw, rh)
	fmt.Printf("AutoEV scale=%.3f (%.2f EV)\n", sc, math.Log2(sc))
	ref := append([]float64(nil), refLin...)
	for i := range ref {
		ref[i] *= sc
	}
	report("reference +AutoEV", ref, rw, rh)
	if err := writePNGDisp(filepath.Join(outDir, "p4_reference.png"), ref, rw, rh, true); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	type cell struct {
		name string
		rgb  []float64
		lin  bool
	}
	cells := []cell{{"reference (DecodeScene + AutoEV)", ref, true}}

	for _, s := range convert.Stocks() {
		neg := append([]float64(nil), refLin...)
		convert.ProcessPixelsFast(neg, rw, rh, convert.Options{
			Stock: s, SceneRGB: true, AsShotWB: true, AutoEV: true,
			ScanMatrix: m, ScanStrength: 1, ScanProfile: convert.DefaultXT3Profile,
			BorderPct: 0.04, SoftBorder: true,
		})
		fmt.Print(s.FileTag)
		densStats(neg, rw, rh, m)
		pos := invertView(neg, rw, rh, s, m)
		report("p4 "+s.FileTag, pos, rw, rh)
		name := "p4_" + s.FileTag + ".png"
		if err := writePNGDisp(filepath.Join(outDir, name), pos, rw, rh, false); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		cells = append(cells, cell{s.Name + " (Cineon+2383)", pos, false})
	}

	cellW, cellH := 480, 321
	cols := 4
	rows := (len(cells) + cols - 1) / cols
	sheet := image.NewRGBA(image.Rect(0, 0, cellW*cols, (cellH+22)*rows))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{16, 16, 16, 255}), image.Point{}, draw.Src)
	for i, c := range cells {
		col, row := i%cols, i/cols
		th := scaleTo(c.rgb, rw, rh, cellW, cellH)
		ox, oy := col*cellW, row*(cellH+22)
		for y := 0; y < cellH; y++ {
			for x := 0; x < cellW; x++ {
				k := (y*cellW + x) * 3
				var pix color.RGBA
				if c.lin {
					pix = color.RGBA{srgbEnc(th[k]), srgbEnc(th[k+1]), srgbEnc(th[k+2]), 255}
				} else {
					pix = color.RGBA{dispByte(th[k]), dispByte(th[k+1]), dispByte(th[k+2]), 255}
				}
				sheet.SetRGBA(ox+x, oy+y, pix)
			}
		}
		label(sheet, ox+8, oy+cellH+16, c.name)
	}
	f, err := os.Create(filepath.Join(outDir, "p4_sheet.png"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := png.Encode(f, sheet); err != nil {
		f.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	f.Close()
	fmt.Println("wrote", filepath.Join(outDir, "p4_sheet.png"), "cells", len(cells))
}
