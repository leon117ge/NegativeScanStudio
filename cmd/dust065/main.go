// dust065: run the v0.65 dust detector on an HDR TIFF and dump every
// candidate spot (CSV) and the cleaned pixels (raw little-endian uint16).
package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"basetool/hdrscan"
)

func main() {
	lv, _ := hdrscan.ParseDust(os.Args[1])
	pix, w, h, st, spots, err := hdrscan.DespeckleFileDebug(os.Args[2], lv)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%dx%d %+v\n", w, h, st)
	f, _ := os.Create(os.Args[3] + ".csv")
	fmt.Fprintln(f, "x0,y0,x1,y1,px,z,peak,dR,dG,dB,kR,kB,cd,out,len,thick,acc,hair,why,bright")
	for _, s := range spots {
		fmt.Fprintf(f, "%d,%d,%d,%d,%d,%.2f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.1f,%.2f,%v,%v,%s,%.3f\n",
			s.X0, s.Y0, s.X1, s.Y1, s.Px, s.Z, s.Peak, s.Drop[0], s.Drop[1], s.Drop[2], s.KR, s.KB, s.CD, s.Out, s.Len, s.Thick, s.Accepted, s.Hair, s.Why, s.Bright)
	}
	f.Close()
	g, _ := os.Create(os.Args[3] + ".u16")
	binary.Write(g, binary.LittleEndian, pix)
	g.Close()
}
