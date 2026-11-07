// mkbase writes a synthetic "pure clear film base" RAF from an uncompressed
// source RAF: every visible sensor pixel is replaced by an orange-mask base
// level with radial light falloff, photon-like noise and a few dust specks.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"log"
	"math"
	"math/rand"
	"os"

	"basetool/rawconv"
)

func main() {
	src := flag.String("src", "./testdata/negsamples2/_DSF9229.RAF", "uncompressed source RAF")
	dst := flag.String("o", "./testdata/hdrtest/purebase.RAF", "output RAF")
	r := flag.Float64("r", 0.236, "base R (fraction of full scale, centre)")
	g := flag.Float64("g", 0.165, "base G")
	b := flag.Float64("b", 0.049, "base B")
	fall := flag.Float64("fall", 0.18, "relative falloff at the corners")
	dust := flag.Int("dust", 40, "dust specks")
	flag.Parse()
	data, err := os.ReadFile(*src)
	if err != nil {
		log.Fatal(err)
	}
	f, err := rawconv.ReadRawFrame(*src)
	if err != nil {
		log.Fatal(err)
	}
	pat := make([]byte, 128)
	for i := 0; i < 64; i++ {
		binary.LittleEndian.PutUint16(pat[2*i:], f.Raw[i])
	}
	off := bytes.Index(data, pat)
	if off < 0 {
		log.Fatal("sensor data not found")
	}
	rng := rand.New(rand.NewSource(1))
	type speck struct{ x, y, rad, k float64 }
	var sp []speck
	for i := 0; i < *dust; i++ {
		sp = append(sp, speck{rng.Float64() * float64(f.Width), rng.Float64() * float64(f.Height), 4 + rng.Float64()*16, 0.4 + 0.4*rng.Float64()})
	}
	lv := [4]float64{*r, *g, *b, *g}
	span := float64(f.Maximum - f.Black)
	cx, cy := float64(f.Width)/2, float64(f.Height)/2
	rmax := math.Hypot(cx, cy)
	for y := 0; y < f.Height; y++ {
		for x := 0; x < f.Width; x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy) / rmax
			m := 1 - *fall*d*d
			for _, s := range sp {
				if math.Hypot(float64(x)-s.x, float64(y)-s.y) < s.rad {
					m *= s.k
				}
			}
			c := f.Color[y*f.Width+x]
			sig := lv[c] * m * span
			v := sig + rng.NormFloat64()*(math.Sqrt(sig*0.4)+1.5)
			bl := float64(f.BlackAt[y*f.Width+x])
			o := bl + v
			if o < 0 {
				o = 0
			}
			if o > float64(f.Maximum) {
				o = float64(f.Maximum)
			}
			i := (y+f.Top)*f.RawWidth + x + f.Left
			binary.LittleEndian.PutUint16(data[off+2*i:], uint16(o+0.5))
		}
	}
	if err := os.WriteFile(*dst, data, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%dx%d)", *dst, f.Width, f.Height)
}
