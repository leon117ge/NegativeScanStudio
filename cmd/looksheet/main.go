// looksheet renders reference / before / after / defaults positives of a RAF.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"

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

func stats(rgb []float64, skipBorder float64, w, h int) (mean [3]float64, p1, p50, p99 float64, rb, sat float64) {
	x0, x1 := int(float64(w)*skipBorder), int(float64(w)*(1-skipBorder))
	y0, y1 := int(float64(h)*skipBorder), int(float64(h)*(1-skipBorder))
	if x1 <= x0 || y1 <= y0 {
		x0, y0, x1, y1 = 0, 0, w, h
	}
	n := (x1 - x0) * (y1 - y0)
	ys := make([]float64, 0, n)
	var sum [3]float64
	var satSum float64
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
			ys = append(ys, 0.2126*r+0.7152*g+0.0722*b)
		}
	}
	nf := float64(n)
	mean = [3]float64{sum[0] / nf, sum[1] / nf, sum[2] / nf}
	rb = mean[0] / math.Max(mean[2], 1e-8)
	sat = satSum / nf
	// percentiles of luma
	// cheap nth via sort-less histogram
	hist := make([]int, 1000)
	for _, y := range ys {
		k := int(y * 999)
		if k < 0 {
			k = 0
		}
		if k > 999 {
			k = 999
		}
		hist[k]++
	}
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

func densStats(neg []float64, w, h int) {
	x0, x1 := int(float64(w)*0.08), int(float64(w)*0.92)
	y0, y1 := int(float64(h)*0.08), int(float64(h)*0.92)
	var sum, p99 [3]float64
	n := 0
	hist := [3][]int{make([]int, 400), make([]int, 400), make([]int, 400)}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := (y*w + x) * 3
			for c := 0; c < 3; c++ {
				t := math.Max(neg[i+c], 1e-6)
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
	fmt.Printf("  Convert D-Dmin mean=%.3f/%.3f/%.3f", sum[0]/float64(n), sum[1]/float64(n), sum[2]/float64(n))
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

func invertLin(neg []float64, gam [3]float64, expo, balR, balG, balB float64) []float64 {
	ch := develop.DefaultChain()
	d := &ch.Nodes[0]
	d.V["base_r"], d.V["base_g"], d.V["base_b"] = convert.BaseFS, convert.BaseFS, convert.BaseFS
	d.V["preset"], d.V["cast_on"], d.V["out_trans"], d.V["in_di"] = 0, 0, 0, 0
	ch.Nodes[1].Bypass = true
	p := &ch.Nodes[2]
	p.V["gam_r"], p.V["gam_g"], p.V["gam_b"] = gam[0], gam[1], gam[2]
	p.V["expo"] = expo
	p.V["bal_r"], p.V["bal_g"], p.V["bal_b"] = balR, balG, balB
	p.V["film_wb"], p.V["gray_on"], p.V["contrast"], p.V["out_mode"] = 0, 0, 0, 4
	for i := range ch.Nodes {
		if i >= 3 {
			ch.Nodes[i].Bypass = true
		}
	}
	pix := ch.Compile(&develop.Context{}, -1)
	out := make([]float64, len(neg))
	for i := 0; i < len(neg); i += 3 {
		r, g, b := pix.Pixel(float32(neg[i]), float32(neg[i+1]), float32(neg[i+2]))
		out[i], out[i+1], out[i+2] = float64(r), float64(g), float64(b)
	}
	return out
}

func findExpo(s convert.Stock) float64 {
	neg := convert.SceneToNeg([3]float64{convert.MidGrey, convert.MidGrey, convert.MidGrey}, convert.Options{Stock: s})
	bestE, bestErr := 1.0, 1e9
	for e := -1.0; e <= 4; e += 0.05 {
		v := invertLin([]float64{neg[0], neg[1], neg[2]}, s.Gamma, e, 0, 0, 0)
		err := math.Abs((v[0]+v[1]+v[2])/3 - convert.MidGrey)
		if err < bestErr {
			bestErr, bestE = err, e
		}
	}
	return bestE
}

func writePNG(path string, rgb []float64, w, h int) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			img.SetRGBA(x, y, color.RGBA{srgbEnc(rgb[i]), srgbEnc(rgb[i+1]), srgbEnc(rgb[i+2]), 255})
		}
	}
	return savePNG(path, img)
}

func savePNG(path string, img image.Image) error {
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

func rgbaFromLin(rgb []float64, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			img.SetRGBA(x, y, color.RGBA{srgbEnc(rgb[i]), srgbEnc(rgb[i+1]), srgbEnc(rgb[i+2]), 255})
		}
	}
	return img
}

func drawCell(sheet *image.RGBA, src image.Image, col, row, cellW, cellH int, title string) {
	b := src.Bounds()
	iw, ih := b.Dx(), b.Dy()
	ox, oy := col*cellW, row*(cellH+22)
	for y := 0; y < cellH; y++ {
		sy := b.Min.Y + y*ih/cellH
		for x := 0; x < cellW; x++ {
			sx := b.Min.X + x*iw/cellW
			r, g, bl, a := src.At(sx, sy).RGBA()
			sheet.SetRGBA(ox+x, oy+y, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), uint8(a >> 8)})
		}
	}
	label(sheet, ox+8, oy+cellH+16, title)
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

	s, err := convert.StockByName("Portra 400")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	expo := findExpo(s)
	fmt.Printf("Portra matching expo=%.2f\n", expo)

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
	fmt.Printf("preview %dx%d\n", rw, rh)

	refLin := toFloat(rp)
	sc := convert.AutoEVScale(refLin, rw, rh)
	fmt.Printf("AutoEV scale=%.3f (%.2f EV)\n", sc, math.Log2(sc))

	ref := append([]float64(nil), refLin...)
	for i := range ref {
		ref[i] *= sc
	}
	report("reference +AutoEV", ref, rw, rh)
	if err := writePNG(outDir+"/user_DSCF5795_reference.png", ref, rw, rh); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	negAfter := append([]float64(nil), refLin...)
	convert.ProcessPixelsFast(negAfter, rw, rh, convert.Options{
		Stock: s, SceneRGB: true, AsShotWB: true, AutoEV: true,
		BorderPct: 0.04, SoftBorder: true,
	})
	fmt.Print("after")
	densStats(negAfter, rw, rh)
	posAfter := invertLin(negAfter, s.Gamma, expo, 0, 0, 0)
	report("after (scene+AutoEV)", posAfter, rw, rh)
	if err := writePNG(outDir+"/user_DSCF5795_after.png", posAfter, rw, rh); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	posDef := invertLin(negAfter, [3]float64{s.Gamma[0] * 1.15, s.Gamma[1] * 1.15, s.Gamma[2] * 1.15}, 0.9, 1.07, 0, 0.67)
	report("defaults on after", posDef, rw, rh)
	if err := writePNG(outDir+"/user_DSCF5795_defaults.png", posDef, rw, rh); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	beforePath := outDir + "/user_DSCF5795_before.png"
	var beforeImg image.Image
	if f, err := os.Open(beforePath); err == nil {
		beforeImg, err = png.Decode(f)
		f.Close()
		if err != nil {
			fmt.Println("before png decode:", err)
			beforeImg = nil
		} else {
			fmt.Println("loaded existing before", beforePath)
		}
	}
	if beforeImg == nil {
		fmt.Println("WARN: no preview2 before.png; sheet will reuse after")
		beforeImg = rgbaFromLin(posAfter, rw, rh)
	}

	cellW, cellH := 640, 428
	sheet := image.NewRGBA(image.Rect(0, 0, cellW*2, (cellH+22)*2))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{16, 16, 16, 255}), image.Point{}, draw.Src)
	drawCell(sheet, rgbaFromLin(ref, rw, rh), 0, 0, cellW, cellH, "reference (DecodeScene + AutoEV)")
	drawCell(sheet, beforeImg, 1, 0, cellW, cellH, "before (preview2 recommended invert)")
	drawCell(sheet, rgbaFromLin(posAfter, rw, rh), 0, 1, cellW, cellH, "after (scene-sRGB + AutoEV + film)")
	drawCell(sheet, rgbaFromLin(posDef, rw, rh), 1, 1, cellW, cellH, "defaults on after (bal 1.07/0/0.67)")
	if err := savePNG(outDir+"/user_DSCF5795_sheet.png", sheet); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote sheet")
}
