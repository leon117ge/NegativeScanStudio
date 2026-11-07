// hairtest runs the v0.77 guided hair removal (the same hdrscan code the
// Remove hairs tab calls) on a TIFF and writes the result to a new file.
package main

import (
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"time"

	"basetool/hdrscan"
)

func main() {
	in := flag.String("in", "", "input 16-bit RGB TIFF")
	out := flag.String("out", "", "output TIFF (must not exist)")
	x0 := flag.Int("x0", 5300, "")
	y0 := flag.Int("y0", 980, "")
	x1 := flag.Int("x1", 5820, "")
	y1 := flag.Int("y1", 1290, "")
	flag.Parse()
	if _, err := os.Stat(*out); err == nil {
		log.Fatal("output exists, not overwriting: ", *out)
	}
	p, err := hdrscan.Load(*in)
	if err != nil {
		log.Fatal(err)
	}
	ti, _ := hdrscan.ReadTIFFInfo(*in)
	t := time.Now()
	e, err := hdrscan.RemoveHair(p.Pix, p.W, p.H, image.Rect(*x0, *y0, *x1, *y1), hdrscan.DefaultHairOptions())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s\nridge %d half-res px, band %d px, elapsed %.2fs\n", e, e.Ridge, e.Band, time.Since(t).Seconds())
	before := append([]uint16(nil), p.Pix...)
	e.Apply(p.Pix)
	e.Undo(p.Pix)
	same := true
	for i := range before {
		if before[i] != p.Pix[i] {
			same = false
			break
		}
	}
	fmt.Println("undo restores every pixel:", same)
	e.Apply(p.Pix)
	ti.Software = hdrscan.HairSoftware(ti.Software, "hairtest", 1)
	if *out != "" {
		if err := hdrscan.SaveHairTIFF(*out, p.W, p.H, p.Pix, ti); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", *out, "software:", ti.Software, "lzw:", ti.LZW)
	}
}
