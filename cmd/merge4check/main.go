// merge4check (v0.75 QA): merges a bracket exactly like HDR scan's Capture
// (hdrscan.DefaultOptions = Weighted, nominal exposures from the bracket EVs
// as CaptureBracketEV records them) and prints the per-frame stats, incl.
// the per-channel sensor ratios of each brighter frame.
//
//	merge4check -evs 0,1,2,3 -out DIR f0.RAF f1.RAF f2.RAF f3.RAF
package main

import (
	"flag"
	"fmt"
	"math"
	"strconv"
	"strings"

	"basetool/hdrscan"
)

func main() {
	out := flag.String("out", "", "output dir")
	evs := flag.String("evs", "0,1,2,3", "EV step of each file")
	flag.Parse()
	mo := hdrscan.DefaultOptions()
	for _, e := range strings.Split(*evs, ",") {
		v, _ := strconv.Atoi(e)
		mo.Exposure = append(mo.Exposure, math.Pow(2, float64(v)))
	}
	res, err := hdrscan.ProcessN(flag.Args(), hdrscan.ProcessOptions{Merge: mo, OutDir: *out, LZW: false, Software: "merge4check"})
	if err != nil {
		fmt.Println("ERR", err)
		return
	}
	fmt.Println(res.HDR, "method", hdrscan.MethodName(mo.Method))
	for _, st := range res.StatsN {
		fmt.Println("  ", st.String())
	}
}
