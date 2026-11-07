// puretest reproduces a pure-film-base bracket with the simulated camera.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"basetool/camera"
	"basetool/hdrscan"
	"basetool/rawconv"
)

func main() {
	scene := flag.String("scene", "./testdata/hdrtest/purebase.RAF", "scene")
	out := flag.String("out", "./testdata/hdrtest/pure", "out")
	evsF := flag.String("evs", "0,2", "EV steps")
	noexp := flag.Bool("noexp", false, "do not pass the exposure times (EXIF of sim files is constant)")
	probe := flag.String("probe", "", "meter the base on this RAF instead of frame 0 (light changed after metering)")
	flag.Parse()
	os.RemoveAll(*out)
	os.MkdirAll(*out, 0o755)
	lg := camera.NewLogger(*out)
	sim, err := camera.OpenSim(*scene, lg)
	if err != nil {
		log.Fatal(err)
	}
	sim.Native = camera.Exposure{ISO: 160, Shutter: 12401}
	e0 := camera.Exposure{ISO: 160, Shutter: 12401}
	var evs []int
	for _, f := range strings.Split(*evsF, ",") {
		v, _ := strconv.Atoi(f)
		evs = append(evs, v)
	}
	es, err := hdrscan.BracketEV(e0, evs)
	if err != nil {
		log.Fatal(err)
	}
	var raws []string
	var ex []float64
	for _, e := range es {
		fmt.Printf("shoot %s\n", e)
		ex = append(ex, hdrscan.ExposureValue(e))
		sim.SetExposure(e)
		r, err := sim.Capture(*out, "")
		if err != nil {
			log.Fatal(err)
		}
		raws = append(raws, r)
	}
	pf := raws[0]
	if *probe != "" {
		pf = *probe
	}
	f, _ := rawconv.ReadRawFrame(pf)
	rd, err := hdrscan.MeasureBase(f, hdrscan.Rect{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("READING %s area %v region %v px %d\n", rd.Where, rd.Area, rd.Region.Rect, rd.Region.Pixels())
	reg := rd.Region
	mo := hdrscan.DefaultOptions()
	if !*noexp {
		mo.Exposure = ex
	}
	mb := rd.RGB
	res, err := hdrscan.ProcessN(raws, hdrscan.ProcessOptions{Merge: mo, BaseArea: rd.Area, BaseRegion: &reg, Software: "puretest", MeteredBase: mb})
	if err != nil {
		fmt.Println("MERGE FAILED:", err)
		return
	}
	for _, st := range res.StatsN {
		fmt.Println("STATS:", st)
	}
	fmt.Println("BASE:", res.BaseCheck())
	fmt.Println("EXPOSURE SOURCE:", res.ExposureSrc, "| frames merged:", res.Frames, "|", res.HDR)
	fmt.Println("LIGHT:", res.LightNote)
	fmt.Println("BASE1:", res.Base1, res.Base1F0, res.Base1N, res.Base1Err)
	_ = filepath.Base
}
