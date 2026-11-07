// offhash: merge with a fixed Software tag into -out (Off-mode hash compare).
package main

import (
	"flag"
	"fmt"

	"basetool/colorsci"
	"basetool/hdrscan"
)

func main() {
	out := flag.String("out", "", "output dir")
	lzw := flag.Bool("lzw", true, "LZW")
	cin := flag.Bool("cineon", false, "v0.66 colour file: Cineon log")
	mask := flag.Bool("mask", false, "v0.66 colour file: mask compensation")
	xt := flag.Float64("xt", 0, "v0.66 colour file: crosstalk strength (default matrix)")
	xtmat := flag.String("xtmat", "vision3", "v0.66 crosstalk matrix: vision3 | identity | a stock name (Ektar 100, Portra 400, UltraMax 400, Gold 200)")
	stock := flag.String("stockbase", "", "v0.66: use this base R,G,B (fraction of full scale) instead of frame 0")
	dust := flag.String("dust", "off", "off|light|strong (v0.73 flag)")
	merge := flag.String("merge", "", "classic|weighted (v0.74 flag; empty = hdrscan.DefaultOptions, Weighted since v0.74)")
	flag.Parse()
	mo := hdrscan.DefaultOptions()
	switch *merge {
	case "classic":
		mo.Method = hdrscan.MergeClassic
	case "weighted":
		mo.Method = hdrscan.MergeWeighted
	}
	dl, _ := hdrscan.ParseDust(*dust)
	co := hdrscan.ColorOptions{Cineon: *cin, MaskComp: *mask, Strength: *xt, Matrix: colorsci.DefaultCrosstalk(), MatrixName: "Vision3 5219 (spectral, mask-aware)"}
	if *xtmat == "identity" {
		co.Matrix, co.MatrixName = colorsci.Identity, "identity"
	} else if d, ok := colorsci.MatchStock(*xtmat); ok {
		co.Matrix, co.MatrixName = d.Crosstalk(), d.Name+" (spectral, mask-aware)"
	}
	if *stock != "" {
		fmt.Sscanf(*stock, "%g,%g,%g", &co.StockBase[0], &co.StockBase[1], &co.StockBase[2])
		co.UseStock, co.StockName = true, "cli"
	}
	res, err := hdrscan.ProcessN(flag.Args(), hdrscan.ProcessOptions{Merge: mo, OutDir: *out, LZW: *lzw, Software: "Film Base Meter v0.65", LeakPct: 20, Dust: dl, Color: co})
	if err != nil {
		fmt.Println("ERR", err)
		return
	}
	fmt.Println(res.HDR)
	if res.OrderNote != "" {
		fmt.Println("ORDER", res.OrderNote)
	}
	for _, st := range res.StatsN {
		fmt.Println("  ", st.String())
	}
	if res.ColorNote != "" || res.ColorErr != "" {
		fmt.Println("COLOR", res.ColorErr)
		fmt.Println("  ", res.ColorNote)
	}
	fmt.Printf("BASE1 %.4f/%.4f/%.4f\n", res.Base1[0], res.Base1[1], res.Base1[2])
}
