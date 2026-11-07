// basetest runs film base auto-detection on RAW files and writes a preview
// PNG with the detected region outlined (for checking the detector).
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"basetool/hdrscan"
	"basetool/rawconv"
)

func main() {
	out := os.Args[1]
	for _, p := range os.Args[2:] {
		f, err := rawconv.ReadRawFrame(p)
		if err != nil {
			fmt.Println(p, err)
			continue
		}
		r, err := hdrscan.MeasureBase(f, hdrscan.Rect{})
		fmt.Printf("%s: %v\n  clipped=%v warn=%q\n", filepath.Base(p), r, r.Clipped, r.Warn)
		if err != nil {
			fmt.Println("  error:", err)
		}
		pv := hdrscan.Preview(f)
		img := image.NewRGBA(pv.Bounds())
		draw.Draw(img, img.Bounds(), pv, image.Point{}, draw.Src)
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		x0, y0 := int(r.Norm.X0*float64(w)), int(r.Norm.Y0*float64(h))
		x1, y1 := int(r.Norm.X1*float64(w)), int(r.Norm.Y1*float64(h))
		c := color.RGBA{0, 255, 0, 255}
		for t := 0; t < 2; t++ {
			for x := x0 - t; x <= x1+t; x++ {
				img.Set(x, y0-t, c)
				img.Set(x, y1+t, c)
			}
			for y := y0 - t; y <= y1+t; y++ {
				img.Set(x0-t, y, c)
				img.Set(x1+t, y, c)
			}
		}
		fo, _ := os.Create(filepath.Join(out, strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))+"_base.png"))
		png.Encode(fo, img)
		fo.Close()
	}
}
