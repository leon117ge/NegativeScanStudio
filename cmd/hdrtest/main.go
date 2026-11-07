// hdrtest exercises meter -> lock -> capture pair -> merge with the simulated camera.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"basetool/camera"
	"basetool/hdrscan"
)

func main() {
	scene := flag.String("scene", "./testdata/negsamples2/_DSF9229.RAF", "scene RAF")
	out := flag.String("out", "./testdata/hdrtest/out", "output dir")
	nativeShutter := flag.Uint("native", 7812, "native shutter (scene 1:1)")
	startISO := flag.Int("iso", 160, "start ISO")
	startSh := flag.Uint("sh", 1953, "start shutter")
	target := flag.Float64("target", 0.80, "target")
	dials := flag.Bool("dials", false, "dial-only body")
	mode := flag.String("mode", "M", "simulated exposure mode (M, A, P, S)")
	nfr := flag.Int("n", 2, "bracket frames (2 = 0/+2, 3 = 0/+2/+4)")
	evsF := flag.String("evs", "", "EV steps, e.g. 0,2,3 (overrides -n)")
	roll := flag.String("roll", "", "roll name")
	shots := flag.Int("shots", 1, "brackets per connection")
	conns := flag.Int("conns", 1, "connections (each reconnect restarts the camera's IMG numbers; the config counter is reset to 0 from the 2nd on)")
	flag.Parse()
	evs := []int{0, 2, 4}[:*nfr]
	if *evsF != "" {
		evs = nil
		for _, f := range strings.Split(*evsF, ",") {
			v, _ := strconv.Atoi(f)
			evs = append(evs, v)
		}
	}
	*nfr = len(evs)
	counter := 0
	for c := 0; c < *conns; c++ {
		if c > 0 {
			counter = 0
			fmt.Println("=== reconnect", c, "(config counter lost) ===")
		}
		for k := 0; k < *shots; k++ {
			counter = runOne(*scene, *out, uint32(*nativeShutter), *startISO, uint32(*startSh), *target, *dials, *mode, evs, *roll, counter, c == 0 && k == 0)
		}
	}
	ents, _ := os.ReadDir(*out)
	seen := 0
	for _, e := range ents {
		if !e.IsDir() {
			fmt.Println("FILE:", e.Name())
			seen++
		}
	}
	fmt.Println("files in output folder:", seen)
}

func runOne(scene, out string, nativeShutter uint32, startISO int, startSh uint32, target float64, dials bool, mode string, evs []int, roll string, counter int, verbose bool) int {
	nfr := &[]int{len(evs)}[0]
	os.MkdirAll(out, 0o755)
	lg := camera.NewLogger(filepath.Dir(out))
	lg.OnLine = func(s string) { fmt.Println(s) }
	sim, err := camera.OpenSim(scene, lg)
	if err != nil {
		log.Fatal(err)
	}
	sim.Native = camera.Exposure{ISO: 160, Shutter: uint32(nativeShutter)}
	sim.SetDials(camera.Exposure{ISO: startISO, Shutter: startSh})
	sim.DialsOnly = dials
	sim.Mode = mode
	sim.Diagnostics()
	s := &hdrscan.Session{Dev: sim, Log: lg, Target: target, ProbeDir: filepath.Join(out, "probe")}
	s.Dials = func(e camera.Exposure, why string) bool {
		fmt.Printf("USER: set dials to %s (%s)\n", e, why)
		sim.SetDials(e)
		return true
	}
	e, r, err := s.Meter()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("LOCKED %s reading %s area %v\n", e, r, r.Area)
	shot, err := s.CaptureBracketEV(out, evs, func(t time.Time) string {
		b, n := hdrscan.NextShot(out, roll, t, counter)
		counter = n
		return b
	})
	if err != nil {
		log.Fatal(err)
	}
	raws := shot.Raws
	fmt.Println("bracket:", raws, "exposures:", shot.Exposures)
	mo := hdrscan.DefaultOptions()
	mo.Exposure = shot.Exposures
	reg := r.Region
	res, err := hdrscan.ProcessN(raws, hdrscan.ProcessOptions{Merge: mo, KeepTIFF: true, LZW: true, MeteredBase: r.RGB,
		Software: "hdrtest", BaseArea: r.Area, BaseRegion: &reg, Progress: func(m string) { fmt.Println(m) }})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("HDR:", res.HDR, res.Elapsed)
	for _, st := range res.StatsN {
		fmt.Println("STATS:", st)
	}
	fmt.Println("BASE:", res.BaseCheck())
	fmt.Printf("BASE1 HDR %.2f/%.2f/%.2f F0 %.2f/%.2f/%.2f\n", res.Base1[0], res.Base1[1], res.Base1[2], res.Base1F0[0], res.Base1F0[1], res.Base1F0[2])
	fmt.Println("EXPOSURE SOURCE:", res.ExposureSrc, "| LIGHT:", res.LightNote)
	if verbose && len(res.TIFFs) == *nfr {
		os.MkdirAll(filepath.Join(out, "manual"), 0o755)
		m, err := hdrscan.ProcessN(res.TIFFs, hdrscan.ProcessOptions{Merge: mo, OutDir: filepath.Join(out, "manual"), LZW: true, Software: "hdrtest"})
		if err != nil {
			log.Fatal(err)
		}
		for _, st := range m.StatsN {
			fmt.Println("MANUAL (TIFF) STATS:", st)
		}
		fmt.Println("MANUAL BASE:", m.BaseCheck())
		a, _ := hdrscan.Load(res.HDR)
		b, _ := hdrscan.Load(m.HDR)
		same := len(a.Pix) == len(b.Pix)
		for i := range a.Pix {
			if !same || a.Pix[i] != b.Pix[i] {
				same = false
				break
			}
		}
		fmt.Println("RAW-path HDR == TIFF-path HDR:", same)
	}
	return counter
}
