package main

import (
	"fmt"
	"os"

	"basetool/hdrscan"
	"basetool/rawconv"
)

func main() {
	p := os.Args[1]
	pl, err := hdrscan.Load(p)
	if err != nil {
		panic(err)
	}
	rf, _ := rawconv.ReadRawFrame(p)
	fmt.Println("plane", pl.W, pl.H, "raw", rf.Width, rf.Height, rf.Maximum, rf.Black)
	var h [3][64]int
	for i := 0; i < len(pl.Pix); i++ {
		h[i%3][pl.Pix[i]>>10]++
	}
	for k := 0; k < 64; k++ {
		fmt.Printf("%5d: %9d %9d %9d\n", k<<10, h[0][k], h[1][k], h[2][k])
	}
	// raw histogram
	var rh [64]int
	for y := 0; y < rf.Height; y++ {
		for x := 0; x < rf.Width; x++ {
			v := int(rf.Raw[(y+rf.Top)*rf.RawWidth+x+rf.Left])
			rh[v>>8]++
		}
	}
	for k := 0; k < 64; k++ {
		if rh[k] > 0 {
			fmt.Printf("raw %5d: %d\n", k<<8, rh[k])
		}
	}
}
