package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/tiff"
)

func main() {
	src := `D:\neg\HDR\500T_2026_11_2\500T_2026_11_2_20261107_230111_0212_RGB_HDR.tiff`
	outDir := filepath.Join(os.Getenv("USERPROFILE"), "nss_grain_crops")
	os.MkdirAll(outDir, 0755)
	f, err := os.Open(src)
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer f.Close()
	img, err := tiff.Decode(f)
	if err != nil {
		fmt.Println("decode:", err)
		os.Exit(1)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	fmt.Printf("size %dx%d type %T\n", w, h, img)

	// Convert to float linear 0..1 RGB
	pix := make([]float64, w*h*3)
	switch m := img.(type) {
	case *image.RGBA64:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				o := m.RGBA64At(b.Min.X+x, b.Min.Y+y)
				i := (y*w + x) * 3
				pix[i] = float64(o.R) / 65535
				pix[i+1] = float64(o.G) / 65535
				pix[i+2] = float64(o.B) / 65535
			}
		}
	case *image.NRGBA64:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				o := m.NRGBA64At(b.Min.X+x, b.Min.Y+y)
				i := (y*w + x) * 3
				pix[i] = float64(o.R) / 65535
				pix[i+1] = float64(o.G) / 65535
				pix[i+2] = float64(o.B) / 65535
			}
		}
	default:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, b16, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				i := (y*w + x) * 3
				pix[i] = float64(r) / 65535
				pix[i+1] = float64(g) / 65535
				pix[i+2] = float64(b16) / 65535
			}
		}
	}

	// Downsample 16x for flatness map (mean + variance of luminance)
	const ds = 16
	const patch = 512
	gw, gh := w/ds, h/ds
	mean := make([]float64, gw*gh)
	varm := make([]float64, gw*gh)
	for gy := 0; gy < gh; gy++ {
		for gx := 0; gx < gw; gx++ {
			var s, s2 float64
			n := 0.0
			for y := gy * ds; y < (gy+1)*ds && y < h; y++ {
				for x := gx * ds; x < (gx+1)*ds && x < w; x++ {
					i := (y*w + x) * 3
					L := 0.2126*pix[i] + 0.7152*pix[i+1] + 0.0722*pix[i+2]
					s += L
					s2 += L * L
					n++
				}
			}
			m := s / n
			mean[gy*gw+gx] = m
			varm[gy*gw+gx] = s2/n - m*m
		}
	}

	// Target densities relative to base 0.5: D=-log10(T/0.5)
	targets := []struct {
		name string
		D    float64
	}{
		{"base", 0.02},
		{"thin", 0.25},
		{"mid", 0.55},
		{"dense", 0.90},
		{"mid2", 0.70},
		{"thin2", 0.35},
	}
	margin := patch / (2 * ds)
	for _, t := range targets {
		bestX, bestY := margin, margin
		bestScore := 1e99
		for gy := margin; gy < gh-margin; gy++ {
			for gx := margin; gx < gw-margin; gx++ {
				m := mean[gy*gw+gx]
				if m < 1e-6 {
					continue
				}
				D := -math.Log10(m / 0.5)
				score := math.Abs(D-t.D)*10 + varm[gy*gw+gx]*5000
				// neighborhood flatness
				var vv float64
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						vv += varm[(gy+dy)*gw+(gx+dx)]
					}
				}
				score += vv * 200
				if score < bestScore {
					bestScore = score
					bestX, bestY = gx, gy
				}
			}
		}
		cx := bestX*ds + ds/2
		cy := bestY*ds + ds/2
		x0 := cx - patch/2
		y0 := cy - patch/2
		if x0 < 0 {
			x0 = 0
		}
		if y0 < 0 {
			y0 = 0
		}
		if x0+patch > w {
			x0 = w - patch
		}
		if y0+patch > h {
			y0 = h - patch
		}
		out := image.NewRGBA64(image.Rect(0, 0, patch, patch))
		var sum [3]float64
		for y := 0; y < patch; y++ {
			for x := 0; x < patch; x++ {
				i := ((y0+y)*w + (x0 + x)) * 3
				r := uint16(pix[i]*65535 + 0.5)
				g := uint16(pix[i+1]*65535 + 0.5)
				b := uint16(pix[i+2]*65535 + 0.5)
				out.SetRGBA64(x, y, color.RGBA64{R: r, G: g, B: b, A: 65535})
				sum[0] += pix[i]
				sum[1] += pix[i+1]
				sum[2] += pix[i+2]
			}
		}
		n := float64(patch * patch)
		path := filepath.Join(outDir, fmt.Sprintf("crop_%s_%d_%d.tiff", t.name, x0, y0))
		wf, err := os.Create(path)
		if err != nil {
			fmt.Println(err)
			continue
		}
		tiff.Encode(wf, out, &tiff.Options{Compression: tiff.Uncompressed, Predictor: true})
		wf.Close()
		fmt.Printf("%s x0=%d y0=%d mean=%.4f/%.4f/%.4f Dapprox=%.3f score=%.4f -> %s\n",
			t.name, x0, y0, sum[0]/n, sum[1]/n, sum[2]/n,
			-math.Log10(((sum[0]+sum[1]+sum[2])/3/n)/0.5), bestScore, path)
	}
	// write meta
	meta := filepath.Join(outDir, "meta.txt")
	os.WriteFile(meta, []byte(fmt.Sprintf("w=%d h=%d px_per_mm=%.4f (assume 36mm long edge)\n", w, h, float64(w)/36.0)), 0644)
	fmt.Println("done", outDir)
}

func init() { _ = binary.LittleEndian }
