// Command colorsci is a prototype CLI around package colorsci: it reads a
// Film Base Meter linear HDR TIFF and writes the optional film-log
// variants plus comparison previews.
//
//	colorsci -in x_HDR.tiff -base 29769,33936,10160 -out dir [-matrix default] [-strength 1]
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"basetool/colorsci"

	"golang.org/x/image/tiff"
)

func main() {
	in := flag.String("in", "", "linear 16-bit RGB HDR TIFF")
	baseS := flag.String("base", "", "film base R,G,B in the TIFF's scale; 'region:x0,y0,x1,y1' to measure a rectangle; empty = brightest robust region")
	out := flag.String("out", ".", "output directory")
	mat := flag.String("matrix", "default", "crosstalk matrix: default | unmasked | identity | 9 comma-separated values")
	strength := flag.Float64("strength", 1, "crosstalk strength for variant c (0..1)")
	scale := flag.Int("scale", 4, "preview downscale factor")
	crop := flag.String("crop", "", "1:1 detail crop x,y,w,h (full-res pixels)")
	writeTIFF := flag.Bool("tiff", true, "write full-resolution Cineon TIFFs (b and c)")
	flag.Parse()
	if *in == "" {
		flag.Usage()
		os.Exit(2)
	}
	pix, w, h, err := readTIFF16(*in)
	if err != nil {
		log.Fatal(err)
	}
	base, how := parseBase(*baseS, pix, w, h)
	log.Printf("%dx%d, base %.0f/%.0f/%.0f (%s)", w, h, base[0], base[1], base[2], how)
	m := parseMatrix(*mat)
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}

	optB := colorsci.Options{Base: base, Encoding: colorsci.EncCineon, MaskComp: true} // v0.72: mask comp is no longer implied
	optC := colorsci.Options{Base: base, Encoding: colorsci.EncCineon, MaskComp: true, Crosstalk: m, Strength: *strength}
	cinB, err := colorsci.Apply(pix, optB)
	if err != nil {
		log.Fatal(err)
	}
	cinC, _ := colorsci.Apply(pix, optC)
	if *writeTIFF {
		must(writeTIFF16(filepath.Join(*out, "b_cineon.tiff"), w, h, cinB))
		must(writeTIFF16(filepath.Join(*out, "c_cineon_mask_crosstalk.tiff"), w, h, cinC))
	}

	// Levels from the image itself (0.5% / 99.7%), identical policy for all variants.
	lvA := levels(pix, w, h, func(v uint16, c int) float64 { return base[c] / math.Max(float64(v), 0.5) })
	code := func(v uint16, _ int) float64 { return colorsci.CineonFrom16(v) }
	lvB := levels(cinB, w, h, code)
	lvC := levels(cinC, w, h, code)

	rA := func(src []uint16, i int) [3]float64 {
		return colorsci.SimpleInversion([3]float64{float64(src[i]), float64(src[i+1]), float64(src[i+2])}, base, lvA)
	}
	offB, offC := autoExposure(cinB, w, h, lvB), autoExposure(cinC, w, h, lvC)
	mkC := func(lv colorsci.Levels, off float64) func([]uint16, int) [3]float64 {
		return func(src []uint16, i int) [3]float64 {
			return colorsci.CineonPrintPreview([3]float64{colorsci.CineonFrom16(src[i]), colorsci.CineonFrom16(src[i+1]), colorsci.CineonFrom16(src[i+2])}, lv, off)
		}
	}
	a := render(pix, w, h, *scale, rA)
	b := render(cinB, w, h, *scale, mkC(lvB, offB))
	c := render(cinC, w, h, *scale, mkC(lvC, offC))
	must(savePNG(filepath.Join(*out, "a_linear_inversion.png"), a))
	must(savePNG(filepath.Join(*out, "b_cineon_print.png"), b))
	must(savePNG(filepath.Join(*out, "c_cineon_mask_crosstalk.png"), c))
	must(savePNG(filepath.Join(*out, "side_by_side.png"), hcat(a, b, c)))
	if *crop != "" {
		x0, y0, cw, ch := parse4(*crop)
		ca := renderCrop(pix, w, x0, y0, cw, ch, rA)
		cb := renderCrop(cinB, w, x0, y0, cw, ch, mkC(lvB, offB))
		cc := renderCrop(cinC, w, x0, y0, cw, ch, mkC(lvC, offC))
		must(savePNG(filepath.Join(*out, "crop_side_by_side.png"), hcat(ca, cb, cc)))
	}
	info := map[string]any{
		"input": *in, "width": w, "height": h, "base": base, "base_source": how,
		"matrix": m, "strength": *strength,
		"levels_a": lvA, "levels_b_codes": lvB, "levels_c_codes": lvC, "contrast_gain_b": offB, "contrast_gain_c": offC,
		"preview": "a: base/v per channel, levels 0.5%/99.7%, sRGB. b,c: Cineon codes, per-channel levels 0.5%/99.7% -> 180/700, global gain about code 180 putting the median at 18% grey code 470 but capped so 99.7% white <= code 900, Cineon log2lin 95/685/0.6, Narkowicz ACES filmic, sRGB",
	}
	jb, _ := json.MarshalIndent(info, "", "  ")
	must(os.WriteFile(filepath.Join(*out, "run.json"), jb, 0o644))
	log.Printf("wrote %s", *out)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func parse4(s string) (int, int, int, int) {
	f := strings.Split(s, ",")
	if len(f) != 4 {
		log.Fatalf("need 4 values: %q", s)
	}
	var v [4]int
	for i := range f {
		v[i], _ = strconv.Atoi(strings.TrimSpace(f[i]))
	}
	return v[0], v[1], v[2], v[3]
}

func parseBase(s string, pix []uint16, w, h int) ([3]float64, string) {
	switch {
	case s == "":
		return colorsci.BrightestRegionBase(pix, w, h, 64), "brightest robust region (lower bound)"
	case strings.HasPrefix(s, "region:"):
		x0, y0, x1, y1 := parse4(strings.TrimPrefix(s, "region:"))
		return colorsci.RegionMean(pix, w, x0, y0, x1, y1), "mean of " + s
	}
	f := strings.Split(s, ",")
	if len(f) != 3 {
		log.Fatalf("base: need R,G,B")
	}
	var b [3]float64
	for i := range f {
		b[i], _ = strconv.ParseFloat(strings.TrimSpace(f[i]), 64)
	}
	return b, "given"
}

func parseMatrix(s string) colorsci.Mat3 {
	switch s {
	case "default":
		return colorsci.DefaultCrosstalk()
	case "unmasked":
		return colorsci.UnmaskedCrosstalk()
	case "identity":
		return colorsci.Identity
	}
	f := strings.Split(s, ",")
	if len(f) != 9 {
		log.Fatalf("matrix: need 9 values")
	}
	var m colorsci.Mat3
	for i := range f {
		m[i/3][i%3], _ = strconv.ParseFloat(strings.TrimSpace(f[i]), 64)
	}
	return m
}

func levels(pix []uint16, w, h int, f func(uint16, int) float64) colorsci.Levels {
	var lv colorsci.Levels
	mx, my := w/50, h/50 // ignore the frame edge / rebate
	for c := 0; c < 3; c++ {
		var v []float64
		for y := my; y < h-my; y += 7 {
			for x := mx; x < w-mx; x += 7 {
				v = append(v, f(pix[3*(y*w+x)+c], c))
			}
		}
		lv.Lo[c] = colorsci.Percentile(v, 0.005)
		lv.Hi[c] = colorsci.Percentile(v, 0.997)
	}
	return lv
}

func to8(v [3]float64) color.NRGBA {
	q := func(x float64) uint8 { return uint8(math.Round(math.Min(math.Max(x, 0), 1) * 255)) }
	return color.NRGBA{q(v[0]), q(v[1]), q(v[2]), 255}
}

// render box-averages scale×scale blocks of the source (in source units)
// before the display transform.
func render(pix []uint16, w, h, s int, f func([]uint16, int) [3]float64) *image.NRGBA {
	ow, oh := w/s, h/s
	img := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	tmp := make([]uint16, 3)
	for y := 0; y < oh; y++ {
		for x := 0; x < ow; x++ {
			var acc [3]float64
			for yy := 0; yy < s; yy++ {
				for xx := 0; xx < s; xx++ {
					i := 3 * ((y*s+yy)*w + x*s + xx)
					for c := 0; c < 3; c++ {
						acc[c] += float64(pix[i+c])
					}
				}
			}
			for c := 0; c < 3; c++ {
				tmp[c] = uint16(acc[c] / float64(s*s))
			}
			img.SetNRGBA(x, y, to8(f(tmp, 0)))
		}
	}
	return img
}

func renderCrop(pix []uint16, w, x0, y0, cw, ch int, f func([]uint16, int) [3]float64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, cw, ch))
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			img.SetNRGBA(x, y, to8(f(pix, 3*((y0+y)*w+x0+x))))
		}
	}
	return img
}

func hcat(ims ...*image.NRGBA) *image.NRGBA {
	gap := 12
	W, H := 0, 0
	for _, im := range ims {
		W += im.Bounds().Dx() + gap
		H = max(H, im.Bounds().Dy())
	}
	out := image.NewNRGBA(image.Rect(0, 0, W-gap, H))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.NRGBA{40, 40, 40, 255}), image.Point{}, draw.Src)
	x := 0
	for _, im := range ims {
		r := im.Bounds().Add(image.Pt(x, 0))
		draw.Draw(out, r, im, image.Point{}, draw.Src)
		x += im.Bounds().Dx() + gap
	}
	return out
}

func savePNG(p string, im image.Image) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, im)
}

func readTIFF16(p string) ([]uint16, int, int, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()
	im, err := tiff.Decode(f)
	if err != nil {
		return nil, 0, 0, err
	}
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	pix := make([]uint16, 3*w*h)
	switch t := im.(type) {
	case *image.RGBA64:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				o := t.PixOffset(x+b.Min.X, y+b.Min.Y)
				i := 3 * (y*w + x)
				pix[i] = uint16(t.Pix[o])<<8 | uint16(t.Pix[o+1])
				pix[i+1] = uint16(t.Pix[o+2])<<8 | uint16(t.Pix[o+3])
				pix[i+2] = uint16(t.Pix[o+4])<<8 | uint16(t.Pix[o+5])
			}
		}
	case *image.NRGBA64:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				o := t.PixOffset(x+b.Min.X, y+b.Min.Y)
				i := 3 * (y*w + x)
				pix[i] = uint16(t.Pix[o])<<8 | uint16(t.Pix[o+1])
				pix[i+1] = uint16(t.Pix[o+2])<<8 | uint16(t.Pix[o+3])
				pix[i+2] = uint16(t.Pix[o+4])<<8 | uint16(t.Pix[o+5])
			}
		}
	default:
		return nil, 0, 0, fmt.Errorf("%s: need a 16-bit RGB TIFF, got %T", p, im)
	}
	return pix, w, h, nil
}

// writeTIFF16 writes an uncompressed little-endian 16-bit RGB TIFF.
func writeTIFF16(p string, w, h int, pix []uint16) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	const nTags = 11
	ifd := 8
	extra := ifd + 2 + nTags*12 + 4 // BitsPerSample array follows the IFD
	data := extra + 6
	le := binary.LittleEndian
	hdr := make([]byte, data)
	copy(hdr, []byte{'I', 'I', 42, 0})
	le.PutUint32(hdr[4:], uint32(ifd))
	le.PutUint16(hdr[ifd:], nTags)
	tag := func(k int, id, typ uint16, cnt, val uint32) {
		o := ifd + 2 + k*12
		le.PutUint16(hdr[o:], id)
		le.PutUint16(hdr[o+2:], typ)
		le.PutUint32(hdr[o+4:], cnt)
		if typ == 3 && cnt == 1 {
			le.PutUint16(hdr[o+8:], uint16(val))
		} else {
			le.PutUint32(hdr[o+8:], val)
		}
	}
	tag(0, 256, 4, 1, uint32(w))
	tag(1, 257, 4, 1, uint32(h))
	tag(2, 258, 3, 3, uint32(extra))
	tag(3, 259, 3, 1, 1)
	tag(4, 262, 3, 1, 2)
	tag(5, 273, 4, 1, uint32(data))
	tag(6, 277, 3, 1, 3)
	tag(7, 278, 4, 1, uint32(h))
	tag(8, 279, 4, 1, uint32(len(pix)*2))
	tag(9, 284, 3, 1, 1)
	tag(10, 339, 3, 1, 1)
	le.PutUint16(hdr[extra:], 16)
	le.PutUint16(hdr[extra+2:], 16)
	le.PutUint16(hdr[extra+4:], 16)
	if _, err := f.Write(hdr); err != nil {
		return err
	}
	buf := make([]byte, 2*len(pix))
	for i, v := range pix {
		le.PutUint16(buf[2*i:], v)
	}
	_, err = f.Write(buf)
	return err
}

// autoExposure returns the global gain about the black point (code 180)
// that puts the frame's median (mean of the three normalised channels) on
// the 18% grey card, code 470; highlights then roll off in the S-curve.
func autoExposure(pix []uint16, w, h int, lv colorsci.Levels) float64 {
	var v []float64
	mx, my := w/50, h/50
	for y := my; y < h-my; y += 7 {
		for x := mx; x < w-mx; x += 7 {
			i := 3 * (y*w + x)
			s := 0.0
			for c := 0; c < 3; c++ {
				s += colorsci.PrintNormalise(colorsci.CineonFrom16(pix[i+c]), c, lv)
			}
			v = append(v, s/3)
		}
	}
	g := (colorsci.CineonGrey - colorsci.PreviewBlackCode) / (colorsci.Percentile(v, 0.5) - colorsci.PreviewBlackCode)
	// Cap so the 99.7% white lands no higher than code 900 (≈ 0.93 after
	// the S-curve): keeps cloud detail instead of chasing the median.
	return math.Min(math.Max(g, 1), (900-colorsci.PreviewBlackCode)/(colorsci.PreviewWhiteCode-colorsci.PreviewBlackCode))
}
