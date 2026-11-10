// rafcompare decodes a scene RAF the same way Convert does (LibRaw X-Trans,
// camera RGB, no embedded JPEG), runs the film model with and without
// as-shot WB, inverts with OFX-equivalent math, and writes preview PNGs.
package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"basetool/convert"
	"basetool/develop"
	"basetool/hdrscan"
	"basetool/rawconv"
	"basetool/scanmeta"

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

func pluginInvert(negR, negG, negB float32, gam [3]float64, expo, balR, balG, balB float64, filmWB, outMode int) (float32, float32, float32) {
	ch := develop.DefaultChain()
	d := &ch.Nodes[0]
	d.V["base_r"], d.V["base_g"], d.V["base_b"] = convert.BaseFS, convert.BaseFS, convert.BaseFS
	d.V["preset"], d.V["cast_on"], d.V["out_trans"], d.V["in_di"] = 0, 0, 0, 0
	ch.Nodes[1].Bypass = true
	p := &ch.Nodes[2]
	p.V["gam_r"], p.V["gam_g"], p.V["gam_b"] = gam[0], gam[1], gam[2]
	p.V["expo"] = expo
	p.V["bal_r"], p.V["bal_g"], p.V["bal_b"] = balR, balG, balB
	p.V["film_wb"], p.V["gray_on"], p.V["contrast"], p.V["out_mode"] = float64(filmWB), 0, 0, float64(outMode)
	for i := range ch.Nodes {
		if i >= 3 {
			ch.Nodes[i].Bypass = true
		}
	}
	return ch.Compile(&develop.Context{}, -1).Pixel(negR, negG, negB)
}

func findExpo(s convert.Stock) float32 {
	neg := convert.SceneToNeg([3]float64{convert.MidGrey, convert.MidGrey, convert.MidGrey}, convert.Options{Stock: s})
	bestE, bestErr := float32(1), float32(1e9)
	for e := float32(-1); e <= 4; e += 0.05 {
		lr, lg, lb := pluginInvert(float32(neg[0]), float32(neg[1]), float32(neg[2]), s.Gamma, float64(e), 0, 0, 0, 0, 4)
		err := float32(math.Abs(float64((lr+lg+lb)/3) - convert.MidGrey))
		if err < bestErr {
			bestErr, bestE = err, e
		}
	}
	return bestE
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

func stats(rgb []float64) (mean [3]float64, rb, sat float64) {
	n := len(rgb) / 3
	if n == 0 {
		return
	}
	var sum [3]float64
	var satSum float64
	for i := 0; i < n; i++ {
		r, g, b := rgb[i*3], rgb[i*3+1], rgb[i*3+2]
		sum[0] += r
		sum[1] += g
		sum[2] += b
		mx := math.Max(r, math.Max(g, b))
		mn := math.Min(r, math.Min(g, b))
		if mx > 1e-8 {
			satSum += (mx - mn) / mx
		}
	}
	mean = [3]float64{sum[0] / float64(n), sum[1] / float64(n), sum[2] / float64(n)}
	rb = mean[0] / math.Max(mean[2], 1e-8)
	sat = satSum / float64(n)
	return
}

func invertBuf(neg []float64, gam [3]float64, expo, balR, balG, balB float64, filmWB, outMode int) []float64 {
	fn := develop.DefaultChain()
	d := &fn.Nodes[0]
	d.V["base_r"], d.V["base_g"], d.V["base_b"] = convert.BaseFS, convert.BaseFS, convert.BaseFS
	d.V["preset"], d.V["cast_on"], d.V["out_trans"], d.V["in_di"] = 0, 0, 0, 0
	fn.Nodes[1].Bypass = true
	p := &fn.Nodes[2]
	p.V["gam_r"], p.V["gam_g"], p.V["gam_b"] = gam[0], gam[1], gam[2]
	p.V["expo"] = expo
	p.V["bal_r"], p.V["bal_g"], p.V["bal_b"] = balR, balG, balB
	p.V["film_wb"], p.V["gray_on"], p.V["contrast"], p.V["out_mode"] = float64(filmWB), 0, 0, float64(outMode)
	for i := range fn.Nodes {
		if i >= 3 {
			fn.Nodes[i].Bypass = true
		}
	}
	pix := fn.Compile(&develop.Context{}, -1)
	out := make([]float64, len(neg))
	for i := 0; i < len(neg); i += 3 {
		r, g, b := pix.Pixel(float32(neg[i]), float32(neg[i+1]), float32(neg[i+2]))
		out[i], out[i+1], out[i+2] = float64(r), float64(g), float64(b)
	}
	return out
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
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

func runConvert(pix []uint16, w, h int, s convert.Stock, wb bool, mul [4]float32) []float64 {
	rgb := toFloat(pix)
	convert.ProcessPixelsFast(rgb, w, h, convert.Options{
		Stock: s, AsShotWB: wb, CamMul: mul, BorderPct: 0,
	})
	return rgb
}

func dumpTIFFMeta(path string) {
	ti, err := hdrscan.ReadTIFFInfo(path)
	if err != nil {
		fmt.Printf("TIFF read failed: %v\n", path)
		return
	}
	fmt.Printf("TIFF Software: %q\n", ti.Software)
	fmt.Printf("TIFF Make/Model: %q / %q\n", ti.Make, ti.Model)
	fmt.Printf("TIFF Description:\n%s\n", ti.Description)
	var m scanmeta.Metadata
	if err := json.Unmarshal([]byte(ti.Description), &m); err != nil {
		fmt.Printf("nss.scan JSON parse failed: %v\n", err)
		return
	}
	fmt.Printf("schema=%s software=%s mode=%s encoding=%s\n", m.Schema, m.Software, m.Mode, m.Encoding)
	fmt.Printf("processing=%s\n", m.Processing)
	fmt.Printf("roll=%s base_source=%s film_base_space=%s\n", m.Roll, m.BaseSource, m.FilmBaseSpace)
	if m.FilmBase != nil {
		fmt.Printf("film_base=%.6f,%.6f,%.6f\n", m.FilmBase[0], m.FilmBase[1], m.FilmBase[2])
	}
	fmt.Printf("camera_applied=%v camera_strength=%.4f camera_profile=%q\n", m.CameraApplied, m.CameraStrength, m.CameraProfile)
	if m.CameraMatrix != nil {
		fmt.Printf("camera_matrix=%v\n", *m.CameraMatrix)
	}
	ver := "unknown"
	sw := m.Software + " " + ti.Software
	switch {
	case strings.Contains(sw, "v1.02-preview2"):
		ver = "v1.02-preview2"
	case strings.Contains(sw, "v1.02-preview"):
		ver = "v1.02-preview"
	case strings.Contains(sw, "v1.01"):
		ver = "v1.01"
	case strings.Contains(sw, "v1.02"):
		ver = "v1.02 (unspecified preview)"
	}
	if strings.Contains(m.Processing, "color=spectral+DIR+dye") {
		ver += " (has v1.02 color pipeline marker)"
	} else if m.Mode == "convert" {
		ver += " (no v1.02 color pipeline marker — likely v1.01 or earlier Convert)"
	}
	fmt.Printf("DETECTED_VERSION=%s\n", ver)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: rafcompare <file.RAF|file.TIFF> [outdir] [stockName]\n")
		os.Exit(2)
	}
	src := os.Args[1]
	outDir := "/workspace/convert_compare"
	if len(os.Args) >= 3 {
		outDir = os.Args[2]
	}
	stockName := "Portra 400"
	if len(os.Args) >= 4 {
		stockName = os.Args[3]
	}

	ext := strings.ToLower(filepath.Ext(src))
	if ext == ".tif" || ext == ".tiff" {
		dumpTIFFMeta(src)
		return
	}

	fmt.Printf("decoding %s with LibRaw (no camera WB, no camera matrix)...\n", src)
	img, err := rawconv.Decode(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "decode: %v\n", err)
		os.Exit(1)
	}
	defer img.Free()
	fmt.Printf("camera=%s %s  size=%dx%d  cam_mul=%.4f,%.4f,%.4f,%.4f  libraw=%s\n",
		img.Make, img.Model, img.Width, img.Height,
		img.CamMul[0], img.CamMul[1], img.CamMul[2], img.CamMul[3],
		rawconv.LibRawVersion())

	n := img.Width * img.Height * 3
	full := make([]uint16, n)
	copy(full, img.Pix[:n])
	pix, w, h := downscale(full, img.Width, img.Height, 1600)
	fmt.Printf("preview %dx%d\n", w, h)

	rawF := toFloat(pix)
	scales := [3]float64{1, 1, 1}
	ok := false
	if img.CamMul[1] > 0 {
		g := float64(img.CamMul[1])
		scales = [3]float64{float64(img.CamMul[0]) / g, 1, float64(img.CamMul[2]) / g}
		ok = true
	}
	meanRaw, rbRaw, satRaw := stats(rawF)
	fmt.Printf("raw camera-RGB mean=%.4f/%.4f/%.4f  R/B=%.3f  sat=%.3f  as_shot_scales ok=%v  %.3f/%.3f/%.3f\n",
		meanRaw[0], meanRaw[1], meanRaw[2], rbRaw, satRaw, ok, scales[0], scales[1], scales[2])

	s, err := convert.StockByName(stockName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	expo := findExpo(s)
	fmt.Printf("stock=%s matching invert expo=%.2f (bal=0 film_wb=None out=linear)\n", s.Name, expo)

	type job struct {
		name             string
		wb               bool
		balR, balG, balB float64
		filmWB, outMode  int
		expo             float64
		path             string
	}
	jobs := []job{
		{"before_noWB_match", false, 0, 0, 0, 0, 4, float64(expo), filepath.Join(outDir, "user_raf_before.png")},
		{"after_WB_match", true, 0, 0, 0, 0, 4, float64(expo), filepath.Join(outDir, "user_raf_after.png")},
		{"before_noWB_pluginDefaults", false, 1.07, 0, 0.67, 1, 4, 0.9, filepath.Join(outDir, "user_raf_before_plugin_defaults.png")},
		{"after_WB_pluginDefaults", true, 1.07, 0, 0.67, 1, 4, 0.9, filepath.Join(outDir, "user_raf_after_plugin_defaults.png")},
	}

	var afterRGB []float64
	for _, j := range jobs {
		neg := runConvert(pix, w, h, s, j.wb, img.CamMul)
		pos := invertBuf(neg, s.Gamma, j.expo, j.balR, j.balG, j.balB, j.filmWB, j.outMode)
		m, rb, sat := stats(pos)
		fmt.Printf("%s  mean=%.4f/%.4f/%.4f  R/B=%.3f  sat=%.3f  -> %s\n",
			j.name, m[0], m[1], m[2], rb, sat, j.path)
		if err := writePNG(j.path, pos, w, h); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", j.path, err)
			os.Exit(1)
		}
		if j.name == "after_WB_match" {
			afterRGB = pos
		}
	}

	// Stock-by-stock sheet (as-shot WB, matching invert).
	stocks := convert.Stocks()
	cellW, cellH := 480, 320
	sheet := image.NewRGBA(image.Rect(0, 0, cellW*len(stocks), cellH+24))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{20, 20, 20, 255}), image.Point{}, draw.Src)
	for i, st := range stocks {
		ex := findExpo(st)
		neg := runConvert(pix, w, h, st, true, img.CamMul)
		pos := invertBuf(neg, st.Gamma, float64(ex), 0, 0, 0, 0, 4)
		m, rb, sat := stats(pos)
		fmt.Printf("stock %s expo=%.2f mean=%.4f/%.4f/%.4f R/B=%.3f sat=%.3f\n",
			st.FileTag, ex, m[0], m[1], m[2], rb, sat)
		thumb := scaleTo(pos, w, h, cellW, cellH)
		for y := 0; y < cellH; y++ {
			for x := 0; x < cellW; x++ {
				k := (y*cellW + x) * 3
				sheet.SetRGBA(i*cellW+x, y, color.RGBA{srgbEnc(thumb[k]), srgbEnc(thumb[k+1]), srgbEnc(thumb[k+2]), 255})
			}
		}
		label(sheet, i*cellW+6, cellH+16, fmt.Sprintf("%s R/B=%.2f sat=%.2f", st.FileTag, rb, sat))
		_ = afterRGB
	}
	sheetPath := filepath.Join(outDir, "user_raf_stocks.png")
	if err := savePNG(sheetPath, sheet); err != nil {
		fmt.Fprintf(os.Stderr, "sheet: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", sheetPath)
}
