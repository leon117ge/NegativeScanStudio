package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"

	"basetool/convert"

	"golang.org/x/image/tiff"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func loadT(path string) (t []float64, w, h int) {
	f := must(os.Open(path))
	defer f.Close()
	img := must(tiff.Decode(f))
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	t = make([]float64, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*w + x) * 3
			t[i] = float64(r) / 65535
			t[i+1] = float64(g) / 65535
			t[i+2] = float64(bl) / 65535
		}
	}
	return t, w, h
}

func cropT(src []float64, sw, sh, x0, y0, cw, ch int) []float64 {
	out := make([]float64, cw*ch*3)
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			si := ((y0+y)*sw + (x0 + x)) * 3
			di := (y*cw + x) * 3
			out[di], out[di+1], out[di+2] = src[si], src[si+1], src[si+2]
		}
	}
	return out
}

func srgb8(v float64) uint8 {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	var s float64
	if v <= 0.0031308 {
		s = 12.92 * v
	} else {
		s = 1.055*math.Pow(v, 1.0/2.4) - 0.055
	}
	if s < 0 {
		s = 0
	}
	if s > 1 {
		s = 1
	}
	return uint8(s*255 + 0.5)
}

func visNeg(t float64) uint8 { return srgb8(t * 1.6) }

func visPos(t, norm float64) uint8 {
	p := (0.5 / math.Max(t, 1e-6)) / norm * 0.8
	return srgb8(p)
}

func posNorm(t []float64) float64 {
	// 90th percentile of 0.5/T
	vals := make([]float64, len(t)/3)
	for i := range vals {
		g := t[i*3+1]
		vals[i] = 0.5 / math.Max(g, 1e-6)
	}
	// cheap percentile via nth
	return percentile(vals, 0.90)
}

func percentile(a []float64, p float64) float64 {
	b := append([]float64(nil), a...)
	// insertion sort is too slow for 512^2; use a simple quantile via sort
	// (stdlib sort)
	sortFloats(b)
	i := int(p * float64(len(b)-1))
	if i < 0 {
		i = 0
	}
	if i >= len(b) {
		i = len(b) - 1
	}
	return b[i]
}

func sortFloats(a []float64) { sort.Float64s(a) }

func sideBySide(realT, simT []float64, w, h, scale int, invert bool) *image.RGBA {
	gap := 8
	ww, hh := w*scale, h*scale
	img := image.NewRGBA(image.Rect(0, 0, ww*2+gap, hh))
	nR := 1.0
	nS := 1.0
	if invert {
		nR = posNorm(realT)
		nS = posNorm(simT)
	}
	pix := func(t []float64, x, y int) color.RGBA {
		i := (y*w + x) * 3
		var r, g, b uint8
		if invert {
			r, g, b = visPos(t[i], nR), visPos(t[i+1], nR), visPos(t[i+2], nR)
			// use each image's own norm — wait, we passed nR for all; fix below
		} else {
			r, g, b = visNeg(t[i]), visNeg(t[i+1]), visNeg(t[i+2])
		}
		return color.RGBA{r, g, b, 255}
	}
	// redraw with correct per-image norm
	_ = nS
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			var rr, rg, rb, sr, sg, sb uint8
			if invert {
				rr, rg, rb = visPos(realT[i], nR), visPos(realT[i+1], nR), visPos(realT[i+2], nR)
				sr, sg, sb = visPos(simT[i], nS), visPos(simT[i+1], nS), visPos(simT[i+2], nS)
			} else {
				rr, rg, rb = visNeg(realT[i]), visNeg(realT[i+1]), visNeg(realT[i+2])
				sr, sg, sb = visNeg(simT[i]), visNeg(simT[i+1]), visNeg(simT[i+2])
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetRGBA(x*scale+dx, y*scale+dy, color.RGBA{rr, rg, rb, 255})
					img.SetRGBA(ww+gap+x*scale+dx, y*scale+dy, color.RGBA{sr, sg, sb, 255})
				}
			}
		}
	}
	for y := 0; y < hh; y++ {
		for x := 0; x < gap; x++ {
			img.SetRGBA(ww+x, y, color.RGBA{20, 20, 20, 255})
		}
	}
	_ = pix
	return img
}

func savePNG(path string, img image.Image) {
	f := must(os.Create(path))
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

func fmtStats(st convert.GrainStats) string {
	return fmt.Sprintf("meanD=%.4f/%.4f/%.4f  RMS=%.5f/%.5f/%.5f  corr=%.3f/%.3f/%.3f  ac50=%.1f/%.1f/%.1f",
		st.MeanD[0], st.MeanD[1], st.MeanD[2],
		st.RMS[0], st.RMS[1], st.RMS[2],
		st.CorrRG, st.CorrGB, st.CorrRB,
		st.AC50[0], st.AC50[1], st.AC50[2])
}

func rel(got, want float64) float64 {
	if want == 0 {
		return 0
	}
	return (got - want) / want * 100
}

func main() {
	outDir := "/workspace/grain_compare"
	realDir := "/workspace/grain_real"
	os.MkdirAll(outDir, 0755)
	s := must(convert.StockByName("Vision3 500T 5219"))

	type region struct {
		file     string
		name     string
		x, y, wh int
	}
	regions := []region{
		{"crop_base.tiff", "low", 320, 112, 192},
		{"crop_mid.tiff", "mid", 304, 0, 192},
		{"crop_mid2.tiff", "high", 64, 272, 192},
	}

	var report string
	report += "NegativeScan Studio v0.96.3 grain calibration\n"
	report += "Metric: density high-pass (box radius 15 px), identical code for real and sim.\n"
	report += "Scale: 6246 px / 36 mm (~173.5 px/mm), matching the 0212 Vision3 500T scan.\n"
	report += "ISO400 target is visually flat patches (not whole 512 crops: those include scene edges).\n"
	report += "Left = real scan crop, right = simulated ISO400 grain at the same mean density.\n\n"

	for _, r := range regions {
		T, w, h := loadT(filepath.Join(realDir, r.file))
		realC := cropT(T, w, h, r.x, r.y, r.wh, r.wh)
		realD := convert.DensityFromLinearT(realC)
		rst := convert.MeasureDensityGrain(realD, r.wh, r.wh)

		simT, sst := convert.SimulateFlatGrain(rst.MeanD, r.wh, r.wh, convert.ScanFramePx, s.Name, convert.GrainISO400, 42)
		_, s100 := convert.SimulateFlatGrain(rst.MeanD, r.wh, r.wh, convert.ScanFramePx, s.Name, convert.GrainISO100, 42)

		report += fmt.Sprintf("=== %s  (%s  %dx%d @(%d,%d)) ===\n", r.name, r.file, r.wh, r.wh, r.x, r.y)
		report += "real  " + fmtStats(rst) + "\n"
		report += "sim4  " + fmtStats(sst) + "\n"
		report += fmt.Sprintf("      RMS rel %% %+5.1f/%+5.1f/%+5.1f   corr Δ %+.3f/%+.3f/%+.3f   ac50 Δ %+.1f/%+.1f/%+.1f\n",
			rel(sst.RMS[0], rst.RMS[0]), rel(sst.RMS[1], rst.RMS[1]), rel(sst.RMS[2], rst.RMS[2]),
			sst.CorrRG-rst.CorrRG, sst.CorrGB-rst.CorrGB, sst.CorrRB-rst.CorrRB,
			sst.AC50[0]-rst.AC50[0], sst.AC50[1]-rst.AC50[1], sst.AC50[2]-rst.AC50[2])
		report += "sim1  " + fmtStats(s100) + "\n"
		report += fmt.Sprintf("      ISO100/ISO400 G RMS = %.2f (target 0.74 from Kodak PGI)\n\n", s100.RMS[1]/sst.RMS[1])

		for _, inv := range []bool{false, true} {
			tag := "neg"
			if inv {
				tag = "pos"
			}
			for _, sc := range []int{1, 2} {
				img := sideBySide(realC, simT, r.wh, r.wh, sc, inv)
				name := fmt.Sprintf("%s_real_vs_sim_%s_%d00.png", r.name, tag, sc)
				savePNG(filepath.Join(outDir, name), img)
				report += "  wrote " + name + "\n"
			}
		}
		report += "\n"
	}

	// Full 512 mid crop stats (scene structure included) for the record.
	T, w, h := loadT(filepath.Join(realDir, "crop_mid.tiff"))
	rd := convert.DensityFromLinearT(T)
	full := convert.MeasureDensityGrain(rd, w, h)
	report += "=== full 512 crop_mid (includes shoe / bar edges; NOT the calibration target) ===\n"
	report += "real  " + fmtStats(full) + "\n"
	_, simFull := convert.SimulateFlatGrain(full.MeanD, 512, 512, convert.ScanFramePx, s.Name, convert.GrainISO400, 42)
	report += "sim4  " + fmtStats(simFull) + "\n"
	report += "Note: full-crop corr ~0.72 is shared edge structure, not dye-cloud colour.\n"

	fmt.Print(report)
	os.WriteFile(filepath.Join(outDir, "table.txt"), []byte(report), 0644)
}
