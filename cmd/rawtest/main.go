// rawtest is a Linux harness for basetool/rawconv (same code as the GUI).
//
//	rawtest convert [-lzw] [-sub] [-icc KEY] [-workers N] files...
//	rawtest compare a.tiff b.tiff
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"math"
	"os"
	"sync"

	"basetool/rawconv"

	"golang.org/x/image/tiff"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: rawtest convert|compare ...")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "convert":
		convert(os.Args[2:])
	case "compare":
		compare(os.Args[2], os.Args[3])
	case "base":
		base(os.Args[2])
	}
}

func convert(args []string) {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	lzw := fs.Bool("lzw", false, "")
	sub := fs.Bool("sub", false, "")
	icc := fs.String("icc", "", "")
	workers := fs.Int("workers", 1, "")
	fs.Parse(args)
	fmt.Println("LibRaw", rawconv.LibRawVersion())
	opts := rawconv.Options{Subfolder: *sub, LZW: *lzw, ICC: *icc, Software: "Film Base Meter v0.62"}
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				r, err := rawconv.Convert(context.Background(), f, opts)
				if err != nil {
					fmt.Printf("%s: ERROR %v\n", f, err)
					continue
				}
				fmt.Printf("%s -> %s %dx%d %s %.1fs cam_mul=%v %s\n", f, r.Dst, r.Width, r.Height, r.Path, r.Elapsed.Seconds(), r.CamMul, r.Note)
			}
		}()
	}
	for _, f := range fs.Args() {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
}

func load(p string) *image.RGBA64 {
	f, err := os.Open(p)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	img, err := tiff.Decode(f)
	if err != nil {
		panic(fmt.Errorf("%s: %w", p, err))
	}
	r, ok := img.(*image.RGBA64)
	if !ok {
		panic(fmt.Sprintf("%s: %T", p, img))
	}
	return r
}

func compare(a, b string) {
	A, B := load(a), load(b)
	if A.Bounds() != B.Bounds() {
		fmt.Printf("SIZE MISMATCH %v vs %v\n", A.Bounds(), B.Bounds())
		return
	}
	var sum [3]float64
	var max [3]int
	var diffPix int
	n := 0
	w, h := A.Bounds().Dx(), A.Bounds().Dy()
	for y := 0; y < h; y++ {
		ra := A.Pix[y*A.Stride : y*A.Stride+w*8]
		rb := B.Pix[y*B.Stride : y*B.Stride+w*8]
		for x := 0; x < w; x++ {
			any := false
			for c := 0; c < 3; c++ {
				va := int(ra[x*8+c*2])<<8 | int(ra[x*8+c*2+1])
				vb := int(rb[x*8+c*2])<<8 | int(rb[x*8+c*2+1])
				d := va - vb
				if d < 0 {
					d = -d
				}
				sum[c] += float64(d)
				if d > max[c] {
					max[c] = d
				}
				if d != 0 {
					any = true
				}
			}
			if any {
				diffPix++
			}
			n++
		}
	}
	fmt.Printf("%dx%d meanAbsDiff R=%.4f G=%.4f B=%.4f maxAbsDiff R=%d G=%d B=%d differingPixels=%d\n",
		w, h, sum[0]/float64(n), sum[1]/float64(n), sum[2]/float64(n), max[0], max[1], max[2], diffPix)
}

// base prints the mean RGB of the brightest unclipped 64x64 block (film base on a negative).
func base(p string) {
	A := load(p)
	w, h := A.Bounds().Dx(), A.Bounds().Dy()
	const bs = 64
	best := -1.0
	var bestRGB [3]float64
	bx, by := 0, 0
	for y0 := 0; y0+bs <= h; y0 += bs / 2 {
		for x0 := 0; x0+bs <= w; x0 += bs / 2 {
			var s [3]float64
			clipped := false
			for y := y0; y < y0+bs; y++ {
				row := A.Pix[y*A.Stride:]
				for x := x0; x < x0+bs; x++ {
					for c := 0; c < 3; c++ {
						v := int(row[x*8+c*2])<<8 | int(row[x*8+c*2+1])
						if v >= 65000 {
							clipped = true
						}
						s[c] += float64(v)
					}
				}
			}
			if clipped {
				continue
			}
			g := s[1]
			if g > best {
				best = g
				for c := 0; c < 3; c++ {
					bestRGB[c] = s[c] / (bs * bs)
				}
				bx, by = x0, y0
			}
		}
	}
	fmt.Printf("%s: brightest block @(%d,%d) RGB = %.0f %.0f %.0f  (log10 ratios R/G=%.3f B/G=%.3f)\n", p, bx, by,
		bestRGB[0], bestRGB[1], bestRGB[2], math.Log10(bestRGB[0]/bestRGB[1]), math.Log10(bestRGB[2]/bestRGB[1]))
}
