// looksheet renders preview5 Convert positives of a RAF (camera-native
// linear RGB, as-shot WB, no sRGB/Rec.709) through the
// plugin path the user views real scans with: CameraCal matrix,
// Density In, stock Positive Out (Cineon), then either the still-paper
// LUT or Kodak 2383, displayed as Rec.709 gamma 2.4.
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
	"strings"

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

func cineonProgram(s convert.Stock) develop.Program {
	ch := develop.DefaultChain()
	d := &ch.Nodes[0]
	d.V["base_r"], d.V["base_g"], d.V["base_b"] = convert.BaseFS, convert.BaseFS, convert.BaseFS
	d.V["preset"], d.V["cast_on"], d.V["out_trans"], d.V["in_di"] = 0, 0, 0, 0
	ch.Nodes[1].Bypass = true
	p := &ch.Nodes[2]
	p.V["gam_r"], p.V["gam_g"], p.V["gam_b"] = s.Gamma[0], s.Gamma[1], s.Gamma[2]
	p.V["expo"] = 0
	p.V["bal_r"], p.V["bal_g"], p.V["bal_b"] = 0, 0, 0
	p.V["film_wb"], p.V["gray_on"], p.V["contrast"], p.V["out_mode"] = 0, 0, 0, 2
	for i := 3; i < len(ch.Nodes); i++ {
		ch.Nodes[i].Bypass = true
	}
	return ch.Compile(&develop.Context{}, -1)
}

func invertCineon(neg []float64, w, h int, s convert.Stock, m colorsci.Mat3) []float64 {
	prog := cineonProgram(s)
	out := make([]float64, len(neg))
	for i := 0; i < len(neg); i += 3 {
		p := colorsci.ApplyCam(m, 1, [3]float64{neg[i], neg[i+1], neg[i+2]})
		r, g, b := prog.Pixel(float32(p[0]), float32(p[1]), float32(p[2]))
		out[i], out[i+1], out[i+2] = float64(r), float64(g), float64(b)
	}
	return out
}

func apply2383(cin []float64, lut *develop.LUT) []float64 {
	out := make([]float64, len(cin))
	for i := 0; i < len(cin); i += 3 {
		r, g, b := lut.Apply(float32(cin[i]), float32(cin[i+1]), float32(cin[i+2]), true)
		out[i], out[i+1], out[i+2] = float64(r), float64(g), float64(b)
	}
	return out
}

func applyPaper(cin []float64) []float64 {
	out := make([]float64, len(cin))
	for i := 0; i < len(cin); i += 3 {
		r, g, b := convert.ApplyStillPaper(cin[i], cin[i+1], cin[i+2])
		out[i], out[i+1], out[i+2] = r, g, b
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
	fmt.Printf("%-28s mean=%.3f/%.3f/%.3f  R/B=%.3f sat=%.3f  Y p1/p50/p99=%.3f/%.3f/%.3f\n",
		tag, mean[0], mean[1], mean[2], rb, sat, p1, p50, p99)
}

func isStill(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "portra") || strings.Contains(n, "ektar") ||
		strings.Contains(n, "gold") || strings.Contains(n, "ultramax")
}

// greenPatch averages linear-sRGB pixels that look like foliage/frog green.
func greenPatch(rgb []float64, w, h int) (avg [3]float64, n int) {
	x0, x1 := int(float64(w)*0.08), int(float64(w)*0.92)
	y0, y1 := int(float64(h)*0.08), int(float64(h)*0.92)
	var sum [3]float64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := (y*w + x) * 3
			r, g, b := rgb[i], rgb[i+1], rgb[i+2]
			if g > 0.06 && g > r*1.02 && g > b*1.8 && r > b {
				sum[0] += r
				sum[1] += g
				sum[2] += b
				n++
			}
		}
	}
	if n == 0 {
		return avg, 0
	}
	return [3]float64{sum[0] / float64(n), sum[1] / float64(n), sum[2] / float64(n)}, n
}

func dispRG(v [3]float64, linear bool) float64 {
	var r, g float64
	if linear {
		r, g = float64(srgbEnc(v[0])), float64(srgbEnc(v[1]))
	} else {
		r, g = float64(dispByte(v[0])), float64(dispByte(v[1]))
	}
	if g < 1 {
		return 0
	}
	return r / g
}

func greyRamp(s convert.Stock, lut string, lut2383 *develop.LUT) {
	fmt.Printf("  grey ramp %s [%s]\n", s.FileTag, lut)
	maxDev := 0.0
	for _, stops := range []float64{-2, -1, 0, 1, 2} {
		L := convert.MidGrey * math.Pow(2, stops)
		neg := convert.SceneToNeg([3]float64{L, L, L}, convert.Options{Stock: s})
		cin := invertCineon([]float64{neg[0], neg[1], neg[2]}, 1, 1, s, convert.DefaultXT3Trichrome)
		var pos []float64
		if lut == "2383" {
			pos = apply2383(cin, lut2383)
		} else {
			pos = applyPaper(cin)
		}
		dev := math.Abs(pos[0]-pos[1]) + math.Abs(pos[1]-pos[2])
		if dev > maxDev {
			maxDev = dev
		}
		fmt.Printf("    %+.0f stop  Cineon=%.4f/%.4f/%.4f  disp=%.4f/%.4f/%.4f  dev=%.4f\n",
			stops, cin[0], cin[1], cin[2], pos[0], pos[1], pos[2], dev)
	}
	fmt.Printf("    max channel-dev %.4f\n", maxDev)
}

func syntheticFrog(s convert.Stock, lut string, lut2383 *develop.LUT) (disp [3]float64, rg float64) {
	src := [3]float64{0.143, 0.159, 0.045}
	neg := convert.SceneToNeg(src, convert.Options{Stock: s})
	cin := invertCineon([]float64{neg[0], neg[1], neg[2]}, 1, 1, s, convert.DefaultXT3Trichrome)
	var pos []float64
	if lut == "2383" {
		pos = apply2383(cin, lut2383)
	} else {
		pos = applyPaper(cin)
	}
	disp = [3]float64{pos[0], pos[1], pos[2]}
	return disp, dispRG(disp, false)
}

func convertAsShot(mul [4]float32) (rm, gm, bm float64, ok bool) {
	g := float64(mul[1])
	if g < 1e-6 {
		return 1, 1, 1, false
	}
	rm = float64(mul[0]) / g
	bm = float64(mul[2]) / g
	if rm < 0.05 || bm < 0.05 || rm > 16 || bm > 16 {
		return 1, 1, 1, false
	}
	return rm, 1, bm, true
}

func displayFromCamera(rgb []float64, w, h int, m colorsci.Mat3) []float64 {
	out := make([]float64, len(rgb))
	for i := 0; i < len(rgb); i += 3 {
		v := colorsci.ApplyCam(m, 1, [3]float64{rgb[i], rgb[i+1], rgb[i+2]})
		// Display encode only: Rec.709 cannot show out-of-gamut camera colours.
		for c := 0; c < 3; c++ {
			if v[c] < 0 {
				v[c] = 0
			}
		}
		out[i], out[i+1], out[i+2] = v[0], v[1], v[2]
	}
	return out
}

func satRGB(v [3]float64) float64 {
	mx := math.Max(v[0], math.Max(v[1], v[2]))
	mn := math.Min(v[0], math.Min(v[1], v[2]))
	if mx < 1e-8 {
		return 0
	}
	return (mx - mn) / mx
}

func reportSatRetention(s convert.Stock, lut string, lut2383 *develop.LUT, camGreen, camRed [3]float64) {
	run := func(src [3]float64, tag string) {
		neg := convert.SceneToNeg(src, convert.Options{Stock: s})
		cin := invertCineon([]float64{neg[0], neg[1], neg[2]}, 1, 1, s, convert.DefaultXT3Trichrome)
		var pos []float64
		if lut == "2383" {
			pos = apply2383(cin, lut2383)
		} else {
			pos = applyPaper(cin)
		}
		srcSat := satRGB(src)
		out := [3]float64{pos[0], pos[1], pos[2]}
		outSat := satRGB(out)
		ret := 0.0
		if srcSat > 1e-8 {
			ret = outSat / srcSat
		}
		fmt.Printf("  sat retain %s [%s] src=%.3f out=%.3f ratio=%.3f  srcRGB=%.3f/%.3f/%.3f out=%.3f/%.3f/%.3f\n",
			tag, lut, srcSat, outSat, ret, src[0], src[1], src[2], out[0], out[1], out[2])
	}
	run(camGreen, "cam-green")
	run(camRed, "cam-red")
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

	cube := filepath.Join(outDir, "NSS_StillPaper_v1.cube")
	if err := convert.WriteStillPaperCube(cube, 33); err != nil {
		fmt.Fprintln(os.Stderr, "paper cube", err)
		os.Exit(1)
	}
	if err := convert.WriteStillPaperCube("/workspace/NSS_StillPaper_v1.cube", 33); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if err := convert.WriteStillPaperCube("/workspace/basetool/convert/data/NSS_StillPaper_v1.cube", 33); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	fmt.Println("wrote", cube)

	lut2383, err := develop.LoadEmbedded("D65")
	if err != nil {
		fmt.Fprintln(os.Stderr, "2383", err)
		os.Exit(1)
	}

	fmt.Println("decoding camera-native", src)
	refImg, err := rawconv.Decode(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Decode", err)
		os.Exit(1)
	}
	fmt.Printf("camera=%s %s  %dx%d  cam_mul=%.3f,%.3f,%.3f\n",
		refImg.Make, refImg.Model, refImg.Width, refImg.Height, refImg.CamMul[0], refImg.CamMul[1], refImg.CamMul[2])
	camMul := refImg.CamMul
	rp := make([]uint16, refImg.Width*refImg.Height*3)
	copy(rp, refImg.Pix[:len(rp)])
	rp, rw, rh := downscale(rp, refImg.Width, refImg.Height, 1600)
	refImg.Free()
	refLin := toFloat(rp)
	// White-balance in camera-native RGB (no colour-space matrix).
	if rm, gm, bm, ok := convertAsShot(camMul); ok {
		for i := 0; i < len(refLin); i += 3 {
			refLin[i] *= rm
			refLin[i+1] *= gm
			refLin[i+2] *= bm
		}
	}
	sc := convert.AutoEVScale(refLin, rw, rh)
	fmt.Printf("AutoEV scale=%.3f (%.2f EV)\n", sc, math.Log2(sc))
	ref := append([]float64(nil), refLin...)
	for i := range ref {
		ref[i] *= sc
	}
	report("reference cam-native +AutoEV", ref, rw, rh)
	// Display-only: XT3 chart matrix so the contact sheet is viewable.
	// This matrix is not used by Convert itself.
	refDisp := displayFromCamera(ref, rw, rh, m)
	if err := writePNGDisp(filepath.Join(outDir, "p5_reference.png"), refDisp, rw, rh, true); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	gRef, gn := greenPatch(ref, rw, rh)
	fmt.Printf("reference green patch n=%d camera-native=%.3f/%.3f/%.3f  R/G=%.3f sat=%.3f  disp R/G=%.3f\n",
		gn, gRef[0], gRef[1], gRef[2], gRef[0]/math.Max(gRef[1], 1e-8), satRGB(gRef), dispRG(gRef, true))
	// Saturated camera-native patches (no Rec.709 encoding).
	camGreen := [3]float64{0.08, 0.28, 0.04}
	camRed := [3]float64{0.30, 0.06, 0.04}
	fmt.Printf("synth camera green sat=%.3f R/G=%.3f   camera red sat=%.3f R/B=%.3f\n",
		satRGB(camGreen), camGreen[0]/camGreen[1], satRGB(camRed), camRed[0]/math.Max(camRed[2], 1e-8))

	type cell struct {
		name string
		rgb  []float64
		lin  bool
	}
	cells := []cell{{"reference (camera-native WB + AutoEV, display matrix)", refDisp, true}}
	negByStock := map[string][]float64{}

	for _, s := range convert.Stocks() {
		neg := append([]float64(nil), refLin...)
		convert.ProcessPixelsFast(neg, rw, rh, convert.Options{
			Stock: s, SceneRGB: true, AsShotWB: true, AutoEV: true,
			ScanMatrix: m, ScanStrength: 1, ScanProfile: convert.DefaultXT3Profile,
			BorderPct: 0.04, SoftBorder: true,
		})
		negByStock[s.Name] = neg
		fmt.Print(s.FileTag)
		densStats(neg, rw, rh, m)
		cin := invertCineon(neg, rw, rh, s, m)

		rec := "paper"
		var pos []float64
		if isStill(s.Name) {
			pos = applyPaper(cin)
		} else {
			pos = apply2383(cin, lut2383)
			rec = "2383"
		}
		report("p5 "+s.FileTag+" "+rec, pos, rw, rh)
		name := "p5_" + s.FileTag + ".png"
		if err := writePNGDisp(filepath.Join(outDir, name), pos, rw, rh, false); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		cells = append(cells, cell{s.Name + " (" + rec + ")", pos, false})

		if isStill(s.Name) {
			pos2383 := apply2383(cin, lut2383)
			report("p5 "+s.FileTag+" 2383", pos2383, rw, rh)
			if err := writePNGDisp(filepath.Join(outDir, "p5_"+s.FileTag+"_2383.png"), pos2383, rw, rh, false); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			cells = append(cells, cell{s.Name + " (2383 cmp)", pos2383, false})
		}

		greyRamp(s, rec, lut2383)
		if isStill(s.Name) {
			greyRamp(s, "2383", lut2383)
		}
		frog, rg := syntheticFrog(s, rec, lut2383)
		fmt.Printf("  synth frog [%s] disp=%.3f/%.3f/%.3f R/G=%.3f (src lin R/G=0.899)\n", rec, frog[0], frog[1], frog[2], rg)
		reportSatRetention(s, rec, lut2383, camGreen, camRed)
		if isStill(s.Name) {
			f2, rg2 := syntheticFrog(s, "2383", lut2383)
			fmt.Printf("  synth frog [2383] disp=%.3f/%.3f/%.3f R/G=%.3f\n", f2[0], f2[1], f2[2], rg2)
		}
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
	f, err := os.Create(filepath.Join(outDir, "p5_sheet.png"))
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
	fmt.Println("wrote", filepath.Join(outDir, "p5_sheet.png"), "cells", len(cells))
}
