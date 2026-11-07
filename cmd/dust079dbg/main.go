// dust079dbg: fibre-stage debug (v0.79). Usage:
//
//	dust079dbg in.tiff light|strong [out.tiff]
//
// prints the stats and every fibre candidate that reached the profile
// checks; writes the cleaned pixels to out.tiff (never overwrites).
package main

import (
	"fmt"
	"os"

	"basetool/hdrscan"
	"basetool/rawconv"
)

func main() {
	path := os.Args[1]
	lv, ok := hdrscan.ParseDust(os.Args[2])
	if !ok {
		panic("level")
	}
	pix, w, h, ds, spots, err := hdrscan.DespeckleFileDebug(path, lv)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%dx%d spots %d hairs %d fibres %d px %d rejected %d time %.1fs\n", w, h, ds.Spots, ds.Hairs, ds.Fibres, ds.Pixels, ds.Rejected, ds.Elapsed.Seconds())
	for _, s := range spots {
		if !s.Fibre {
			continue
		}
		switch s.Why {
		case "short", "thick", "shape":
			continue
		}
		pol := "dark "
		if s.Light {
			pol = "light"
		}
		fmt.Printf("fibre %s box %d,%d-%d,%d px %d len %.0f th %.1f straight %.3f depth %.3f fw %.1f ratio %.1f z %.1f iso %.2f cg %.1f acc %v why %q\n",
			pol, s.X0, s.Y0, s.X1, s.Y1, s.Px, s.Len, s.Thick, s.Straight, s.Peak, s.FW, s.Ratio, s.Z, s.Out, s.CD, s.Accepted, s.Why)
	}
	if len(os.Args) > 3 {
		out := os.Args[3]
		if _, err := os.Stat(out); err == nil {
			panic("exists: " + out)
		}
		if err := rawconv.WriteTIFF16(out, w, h, pix, true, "", "", "dust079dbg "+os.Args[2]); err != nil {
			panic(err)
		}
	}
}
