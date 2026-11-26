// decodegate compares the darwin/arm64 decode of the sample raw with the
// Linux and Windows decodes (16-bit TIFFs from rawtest convert) and fails
// when the Mac output differs by more than the tolerance.
//
//	go run ./ci/macos/decodegate darwin.tiff linux.tiff windows.tiff
package main

import (
	"fmt"
	"image"
	"os"

	"golang.org/x/image/tiff"
)

// Tolerance in 16-bit code values. Integer paths are expected to match
// exactly; float rounding in the libm may move a value by a code or two.
const (
	maxAbsTol  = 4
	meanAbsTol = 0.01
)

type stats struct {
	max     int
	mean    float64
	differ  int
	samples int
}

func load(p string) *image.RGBA64 {
	f, err := os.Open(p)
	if err != nil {
		fail("%v", err)
	}
	defer f.Close()
	img, err := tiff.Decode(f)
	if err != nil {
		fail("%s: %v", p, err)
	}
	r, ok := img.(*image.RGBA64)
	if !ok {
		fail("%s: %T, want 16-bit RGB", p, img)
	}
	return r
}

func compare(a, b *image.RGBA64) stats {
	if a.Bounds() != b.Bounds() {
		fail("size %v vs %v", a.Bounds(), b.Bounds())
	}
	var s stats
	var sum float64
	w, h := a.Bounds().Dx(), a.Bounds().Dy()
	for y := 0; y < h; y++ {
		ra := a.Pix[y*a.Stride : y*a.Stride+w*8]
		rb := b.Pix[y*b.Stride : y*b.Stride+w*8]
		for i := 0; i < w*8; i += 2 {
			if i%8 == 6 { // alpha
				continue
			}
			d := (int(ra[i])<<8 | int(ra[i+1])) - (int(rb[i])<<8 | int(rb[i+1]))
			if d < 0 {
				d = -d
			}
			if d > s.max {
				s.max = d
			}
			if d != 0 {
				s.differ++
			}
			sum += float64(d)
			s.samples++
		}
	}
	s.mean = sum / float64(s.samples)
	return s
}

func fail(f string, a ...any) {
	fmt.Printf("FAIL: "+f+"\n", a...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: decodegate darwin.tiff ref.tiff [ref2.tiff ...]")
		os.Exit(2)
	}
	mac := load(os.Args[1])
	bad := false
	for _, p := range os.Args[2:] {
		s := compare(mac, load(p))
		verdict := "identical"
		if s.differ > 0 {
			verdict = "within tolerance"
			if s.max > maxAbsTol || s.mean > meanAbsTol {
				verdict = "OUT OF TOLERANCE"
				bad = true
			}
		}
		fmt.Printf("darwin vs %s: %s (max abs %d, mean abs %.6f, %d of %d samples differ)\n",
			p, verdict, s.max, s.mean, s.differ, s.samples)
	}
	if bad {
		fail("darwin/arm64 decode differs beyond max %d / mean %.3f code values", maxAbsTol, meanAbsTol)
	}
	fmt.Println("decode gate: PASS")
}
