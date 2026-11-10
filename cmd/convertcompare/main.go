// convertcompare measures Convert stock differences after OFX-style inversion
// and writes contact sheets. Used to quantify before/after of the film-color fix.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"basetool/convert"
	"basetool/develop"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type patch struct {
	Name string
	RGB  [3]float64 // scene-linear
}

func srgbToLin(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func linToSrgb(c float64) float64 {
	if c <= 0 {
		return 0
	}
	if c >= 1 {
		return 1
	}
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

// ColorChecker-ish and memory colors in scene-linear (approx sRGB primaries).
func patches() []patch {
	s := func(name string, r, g, b uint8) patch {
		return patch{name, [3]float64{srgbToLin(float64(r) / 255), srgbToLin(float64(g) / 255), srgbToLin(float64(b) / 255)}}
	}
	return []patch{
		{"gray18", [3]float64{0.18, 0.18, 0.18}},
		{"gray90", [3]float64{0.90, 0.90, 0.90}},
		{"gray03", [3]float64{0.03, 0.03, 0.03}},
		s("dark_skin", 115, 82, 68),
		s("light_skin", 194, 150, 130),
		s("sky", 98, 122, 157),
		s("foliage", 87, 108, 67),
		s("blue_flower", 133, 128, 177),
		s("bluish_green", 103, 189, 170),
		s("orange", 214, 126, 44),
		s("purplish_blue", 80, 91, 166),
		s("moderate_red", 193, 90, 99),
		s("purple", 94, 60, 108),
		s("yellow_green", 157, 188, 64),
		s("orange_yellow", 224, 163, 46),
		s("blue", 56, 61, 150),
		s("green", 70, 148, 73),
		s("red", 175, 54, 60),
		s("yellow", 231, 199, 31),
		s("magenta", 187, 86, 149),
		s("cyan", 8, 133, 161),
		s("white", 243, 243, 242),
		s("neutral_5", 122, 122, 121),
		s("black", 52, 52, 52),
	}
}

func pluginInvert(negR, negG, negB float32, gam [3]float64, expo float32) (float32, float32, float32) {
	ch := develop.DefaultChain()
	d := &ch.Nodes[0]
	d.V["base_r"], d.V["base_g"], d.V["base_b"] = convert.BaseFS, convert.BaseFS, convert.BaseFS
	d.V["preset"], d.V["cast_on"], d.V["out_trans"], d.V["in_di"] = 0, 0, 0, 0
	ch.Nodes[1].Bypass = true
	p := &ch.Nodes[2]
	p.V["gam_r"], p.V["gam_g"], p.V["gam_b"] = gam[0], gam[1], gam[2]
	p.V["expo"] = float64(expo)
	p.V["bal_r"], p.V["bal_g"], p.V["bal_b"] = 0, 0, 0
	p.V["film_wb"], p.V["gray_on"], p.V["contrast"], p.V["out_mode"] = 0, 0, 0, 4
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
		lr, lg, lb := pluginInvert(float32(neg[0]), float32(neg[1]), float32(neg[2]), s.Gamma, e)
		err := float32(math.Abs(float64((lr+lg+lb)/3) - convert.MidGrey))
		if err < bestErr {
			bestErr, bestE = err, e
		}
	}
	return bestE
}

// linear sRGB -> CIE Lab (D65)
func linToLab(r, g, b float64) (L, a, bb float64) {
	if r < 0 {
		r = 0
	}
	if g < 0 {
		g = 0
	}
	if b < 0 {
		b = 0
	}
	X := 0.4124564*r + 0.3575761*g + 0.1804375*b
	Y := 0.2126729*r + 0.7151522*g + 0.0721750*b
	Z := 0.0193339*r + 0.1191920*g + 0.9503041*b
	// D65 white
	f := func(t float64) float64 {
		const e = 216.0 / 24389.0
		const k = 24389.0 / 27.0
		if t > e {
			return math.Cbrt(t)
		}
		return (k*t + 16) / 116
	}
	fx, fy, fz := f(X/0.95047), f(Y/1.00000), f(Z/1.08883)
	L = 116*fy - 16
	a = 500 * (fx - fy)
	bb = 200 * (fy - fz)
	return
}

func deltaE76(r1, g1, b1, r2, g2, b2 float64) float64 {
	L1, a1, b1l := linToLab(r1, g1, b1)
	L2, a2, b2l := linToLab(r2, g2, b2)
	dL, da, db := L1-L2, a1-a2, b1l-b2l
	return math.Sqrt(dL*dL + da*da + db*db)
}

func chromaHue(r, g, b float64) (C, h float64) {
	_, a, bb := linToLab(r, g, b)
	C = math.Hypot(a, bb)
	h = math.Atan2(bb, a) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return
}

func label(img *image.RGBA, x, y int, s string) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.RGBA{255, 255, 255, 255}),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	// shadow
	d.Dot = fixed.P(x+1, y+1)
	d.Src = image.NewUniform(color.RGBA{0, 0, 0, 255})
	d.DrawString(s)
	d.Dot = fixed.P(x, y)
	d.Src = image.NewUniform(color.RGBA{255, 255, 255, 255})
	d.DrawString(s)
}

func extractRAFPreview2(path string) (image.Image, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var best image.Image
	bestN := 0
	for i := 0; i < len(b)-3; i++ {
		if b[i] != 0xFF || b[i+1] != 0xD8 || b[i+2] != 0xFF {
			continue
		}
		img, err := jpeg.Decode(bytes.NewReader(b[i:]))
		if err != nil {
			continue
		}
		r := img.Bounds()
		n := r.Dx() * r.Dy()
		if n > bestN {
			best, bestN = img, n
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no JPEG preview")
	}
	return best, nil
}

func imgToLinear(img image.Image, maxW int) ([]float64, int, int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if maxW > 0 && w > maxW {
		nh := h * maxW / w
		dst := image.NewRGBA(image.Rect(0, 0, maxW, nh))
		// nearest for speed
		for y := 0; y < nh; y++ {
			sy := b.Min.Y + y*h/nh
			for x := 0; x < maxW; x++ {
				sx := b.Min.X + x*w/maxW
				dst.Set(x, y, img.At(sx, sy))
			}
		}
		img, w, h = dst, maxW, nh
		b = img.Bounds()
	}
	rgb := make([]float64, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*w + x) * 3
			rgb[i] = srgbToLin(float64(r) / 65535)
			rgb[i+1] = srgbToLin(float64(g) / 65535)
			rgb[i+2] = srgbToLin(float64(bl) / 65535)
		}
	}
	return rgb, w, h
}

func processAndInvert(rgb []float64, w, h int, s convert.Stock, expo float32) []float64 {
	cp := append([]float64(nil), rgb...)
	convert.ProcessPixels(cp, w, h, convert.Options{Stock: s, Grain: convert.GrainOff, BorderPct: 0})
	out := make([]float64, len(cp))
	ch := develop.DefaultChain()
	d := &ch.Nodes[0]
	d.V["base_r"], d.V["base_g"], d.V["base_b"] = convert.BaseFS, convert.BaseFS, convert.BaseFS
	d.V["preset"], d.V["cast_on"], d.V["out_trans"], d.V["in_di"] = 0, 0, 0, 0
	ch.Nodes[1].Bypass = true
	p := &ch.Nodes[2]
	p.V["gam_r"], p.V["gam_g"], p.V["gam_b"] = s.Gamma[0], s.Gamma[1], s.Gamma[2]
	p.V["expo"] = float64(expo)
	p.V["bal_r"], p.V["bal_g"], p.V["bal_b"] = 0, 0, 0
	p.V["film_wb"], p.V["gray_on"], p.V["contrast"], p.V["out_mode"] = 0, 0, 0, 4
	for i := range ch.Nodes {
		if i >= 3 {
			ch.Nodes[i].Bypass = true
		}
	}
	fn := ch.Compile(&develop.Context{}, -1)
	for i := 0; i < w*h; i++ {
		r, g, b := fn.Pixel(float32(cp[i*3]), float32(cp[i*3+1]), float32(cp[i*3+2]))
		out[i*3], out[i*3+1], out[i*3+2] = float64(r), float64(g), float64(b)
	}
	return out
}

func toDisplayPNG(rgb []float64, w, h int, path string) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			img.SetRGBA(x, y, color.RGBA{
				uint8(linToSrgb(rgb[i])*255 + 0.5),
				uint8(linToSrgb(rgb[i+1])*255 + 0.5),
				uint8(linToSrgb(rgb[i+2])*255 + 0.5),
				255,
			})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func contactSheet(cols []namedImg, title, path string) error {
	if len(cols) == 0 {
		return fmt.Errorf("no images")
	}
	w, h := cols[0].w, cols[0].h
	labelH := 22
	dw := w
	dh := h + labelH
	out := image.NewRGBA(image.Rect(0, 0, dw*len(cols), dh+18))
	draw.Draw(out, out.Bounds(), &image.Uniform{color.RGBA{20, 20, 20, 255}}, image.Point{}, draw.Src)
	label(out, 8, 14, title)
	for i, c := range cols {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				j := (y*w + x) * 3
				img.SetRGBA(x, y, color.RGBA{
					uint8(linToSrgb(c.rgb[j])*255 + 0.5),
					uint8(linToSrgb(c.rgb[j+1])*255 + 0.5),
					uint8(linToSrgb(c.rgb[j+2])*255 + 0.5),
					255,
				})
			}
		}
		ox := i * dw
		draw.Draw(out, image.Rect(ox, 18, ox+w, 18+h), img, image.Point{}, draw.Src)
		label(out, ox+6, 18+h+14, c.name)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}

type namedImg struct {
	name string
	rgb  []float64
	w, h int
}

func makeChart(ps []patch, pw, ph, pad int) ([]float64, int, int) {
	n := len(ps)
	cols := 8
	rows := (n + cols - 1) / cols
	w := cols*pw + (cols+1)*pad
	h := rows*ph + (rows+1)*pad
	rgb := make([]float64, w*h*3)
	// mid-gray background
	for i := 0; i < w*h; i++ {
		rgb[i*3], rgb[i*3+1], rgb[i*3+2] = 0.18, 0.18, 0.18
	}
	for i, p := range ps {
		c, r := i%cols, i/cols
		x0 := pad + c*(pw+pad)
		y0 := pad + r*(ph+pad)
		for y := 0; y < ph; y++ {
			for x := 0; x < pw; x++ {
				j := ((y0+y)*w + (x0 + x)) * 3
				rgb[j], rgb[j+1], rgb[j+2] = p.RGB[0], p.RGB[1], p.RGB[2]
			}
		}
	}
	return rgb, w, h
}

func histStats(rgb []float64) (ymin, ymax, ystd, meanC float64) {
	n := len(rgb) / 3
	if n == 0 {
		return
	}
	ys := make([]float64, n)
	var sum, sum2, sumC float64
	ymin = 1e9
	for i := 0; i < n; i++ {
		r, g, b := rgb[i*3], rgb[i*3+1], rgb[i*3+2]
		y := 0.2126*r + 0.7152*g + 0.0722*b
		ys[i] = y
		sum += y
		sum2 += y * y
		if y < ymin {
			ymin = y
		}
		if y > ymax {
			ymax = y
		}
		mx := r
		if g > mx {
			mx = g
		}
		if b > mx {
			mx = b
		}
		mn := r
		if g < mn {
			mn = g
		}
		if b < mn {
			mn = b
		}
		if mx > 1e-9 {
			sumC += (mx - mn) / mx
		}
	}
	mean := sum / float64(n)
	ystd = math.Sqrt(math.Max(sum2/float64(n)-mean*mean, 0))
	meanC = sumC / float64(n)
	return
}

func main() {
	outDir := "/workspace/convert_compare"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	os.MkdirAll(outDir, 0755)

	stocks := convert.Stocks()
	ps := patches()
	expos := map[string]float32{}
	fmt.Println("=== stock expo (mid-gray -> 0.18) ===")
	for _, s := range stocks {
		e := findExpo(s)
		expos[s.Name] = e
		neg := convert.SceneToNeg([3]float64{0.18, 0.18, 0.18}, convert.Options{Stock: s})
		ir, ig, ib := pluginInvert(float32(neg[0]), float32(neg[1]), float32(neg[2]), s.Gamma, e)
		fmt.Printf("  %s  expo=%.2f  neg=%.4f/%.4f/%.4f  inv=%.4f/%.4f/%.4f\n",
			s.FileTag, e, neg[0], neg[1], neg[2], ir, ig, ib)
	}

	// Per-patch inverted linear RGB
	type key struct{ stock, patch string }
	inv := map[key][3]float64{}
	negN := map[key][3]float64{}

	fmt.Println("\n=== inverted positives (linear) ===")
	fmt.Printf("%-16s %-16s %8s %8s %8s %8s %8s\n", "stock", "patch", "R", "G", "B", "C*", "hue")
	for _, s := range stocks {
		e := expos[s.Name]
		for _, p := range ps {
			neg := convert.SceneToNeg(p.RGB, convert.Options{Stock: s})
			ir, ig, ib := pluginInvert(float32(neg[0]), float32(neg[1]), float32(neg[2]), s.Gamma, e)
			v := [3]float64{float64(ir), float64(ig), float64(ib)}
			inv[key{s.Name, p.Name}] = v
			negN[key{s.Name, p.Name}] = neg
			C, h := chromaHue(v[0], v[1], v[2])
			if p.Name == "gray18" || p.Name == "light_skin" || p.Name == "sky" || p.Name == "foliage" || p.Name == "red" || p.Name == "cyan" || p.Name == "yellow" {
				fmt.Printf("%-16s %-16s %8.4f %8.4f %8.4f %8.2f %8.1f\n", s.FileTag, p.Name, v[0], v[1], v[2], C, h)
			}
		}
	}

	// Pairwise mean ΔE over chromatic patches
	fmt.Println("\n=== pairwise mean ΔE76 (chromatic patches) ===")
	var names []string
	for _, s := range stocks {
		names = append(names, s.Name)
	}
	chromatic := []string{"dark_skin", "light_skin", "sky", "foliage", "orange", "blue", "green", "red", "yellow", "magenta", "cyan", "purplish_blue", "moderate_red"}
	meanDE := func(a, b string) float64 {
		var sum float64
		n := 0
		for _, pn := range chromatic {
			va, vb := inv[key{a, pn}], inv[key{b, pn}]
			sum += deltaE76(va[0], va[1], va[2], vb[0], vb[1], vb[2])
			n++
		}
		return sum / float64(n)
	}
	fmt.Printf("%-16s", "")
	for _, s := range stocks {
		fmt.Printf(" %10s", s.FileTag)
	}
	fmt.Println()
	var allDE []float64
	for i, a := range names {
		fmt.Printf("%-16s", stocks[i].FileTag)
		for j, b := range names {
			if i == j {
				fmt.Printf(" %10s", "-")
				continue
			}
			de := meanDE(a, b)
			allDE = append(allDE, de)
			fmt.Printf(" %10.2f", de)
		}
		fmt.Println()
	}
	if len(allDE) > 0 {
		var sum, mx float64
		for _, v := range allDE {
			sum += v
			if v > mx {
				mx = v
			}
		}
		fmt.Printf("mean pairwise ΔE=%.2f  max=%.2f\n", sum/float64(len(allDE)), mx)
	}

	fmt.Println("\n=== memory-color chroma / hue ===")
	for _, pn := range []string{"light_skin", "sky", "foliage", "red", "yellow"} {
		fmt.Printf("-- %s --\n", pn)
		for _, s := range stocks {
			v := inv[key{s.Name, pn}]
			C, h := chromaHue(v[0], v[1], v[2])
			fmt.Printf("  %-16s C*=%6.2f  h=%6.1f  RGB=%.3f/%.3f/%.3f\n", s.FileTag, C, h, v[0], v[1], v[2])
		}
	}

	// Gray balance check across a ramp
	fmt.Println("\n=== gray ramp max |R-G|+|G-B| after invert ===")
	for _, s := range stocks {
		e := expos[s.Name]
		maxDev := 0.0
		for _, stops := range []float64{-3, -2, -1, 0, 1, 2} {
			L := 0.18 * math.Pow(2, stops)
			neg := convert.SceneToNeg([3]float64{L, L, L}, convert.Options{Stock: s})
			ir, ig, ib := pluginInvert(float32(neg[0]), float32(neg[1]), float32(neg[2]), s.Gamma, e)
			dev := math.Abs(float64(ir-ig)) + math.Abs(float64(ig-ib))
			if dev > maxDev {
				maxDev = dev
			}
		}
		fmt.Printf("  %-16s max gray dev=%.5f\n", s.FileTag, maxDev)
	}

	// Contact sheet of color chart
	chart, cw, ch := makeChart(ps, 48, 36, 6)
	var cols []namedImg
	for _, s := range stocks {
		invRGB := processAndInvert(chart, cw, ch, s, expos[s.Name])
		cols = append(cols, namedImg{s.FileTag, invRGB, cw, ch})
		_ = toDisplayPNG(invRGB, cw, ch, filepath.Join(outDir, "chart_"+s.FileTag+".png"))
	}
	if err := contactSheet(cols, "inverted positives: color patches", filepath.Join(outDir, "contact_patches.png")); err != nil {
		fmt.Println("contact patches:", err)
	}

	fmt.Println("\n=== histogram vs straight camera render (color chart, linear) ===")
	cy0, cy1, cstd, csat := histStats(chart)
	fmt.Printf("  camera   Y[%.3f..%.3f] std=%.3f  mean_sat=%.3f\n", cy0, cy1, cstd, csat)
	for i, st := range stocks {
		hy0, hy1, hstd, hsat := histStats(cols[i].rgb)
		fmt.Printf("  %-16s Y[%.3f..%.3f] std=%.3f  mean_sat=%.3f  (std ratio %.2f, sat ratio %.2f)\n",
			st.FileTag, hy0, hy1, hstd, hsat, hstd/math.Max(cstd, 1e-9), hsat/math.Max(csat, 1e-9))
	}

	// RAF previews if present
	rafs := []string{
		"/workspace/raf_samples/XT3_AFXT2720.RAF",
		"/workspace/raf_samples/XT3_AFXT2721.RAF",
		"/workspace/raf_samples/X100V_DSCF0733.RAF",
	}
	for _, rp := range rafs {
		if _, err := os.Stat(rp); err != nil {
			continue
		}
		img, err := extractRAFPreview2(rp)
		if err != nil {
			fmt.Println("preview", rp, err)
			continue
		}
		lin, w, h := imgToLinear(img, 480)
		var rcols []namedImg
		for _, s := range stocks {
			invRGB := processAndInvert(lin, w, h, s, expos[s.Name])
			rcols = append(rcols, namedImg{s.FileTag, invRGB, w, h})
		}
		stem := strings.TrimSuffix(filepath.Base(rp), filepath.Ext(rp))
		outp := filepath.Join(outDir, "contact_"+stem+".png")
		if err := contactSheet(rcols, "inverted positives: "+stem+" (JPEG preview, approx linear)", outp); err != nil {
			fmt.Println("contact raf:", err)
		} else {
			fmt.Println("wrote", outp)
		}
	}
	fmt.Println("done ->", outDir)
}
