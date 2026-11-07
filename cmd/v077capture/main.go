// v077capture: offline check of the two v0.77 bracket presets (0/+2 and
// 0/+2/+3) with the simulated camera: tethered bracket, file-name suffixes,
// darkest-first order, ratio measurement and base protection.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"basetool/camera"
	"basetool/hdrscan"
)

func main() {
	scene := flag.String("scene", "./testdata/negsamples2/_DSF9229.RAF", "scene RAF")
	save := flag.String("save", "./testdata/v077/capture", "Save-to folder (wiped)")
	native := flag.Uint("native", 31250, "native shutter at ISO 160")
	flag.Parse()
	os.RemoveAll(*save)
	lg := camera.NewLogger(*save)
	sim, err := camera.OpenSim(*scene, lg)
	must(err)
	sim.Native = camera.Exposure{ISO: 160, Shutter: uint32(*native)}
	sim.SetDials(camera.Exposure{ISO: 160, Shutter: 4000})
	for _, evs := range [][]int{{0, 2}, {0, 2, 3}} {
		dir, err := hdrscan.RollDir(*save, fmt.Sprintf("preset %v", evs), time.Now())
		must(err)
		s := &hdrscan.Session{Dev: sim, Log: lg, Target: 0.8, ProbeDir: filepath.Join(dir, "probe")}
		s.Dials = func(e camera.Exposure, why string) bool { sim.SetDials(e); return true }
		e, r, _, err := s.LockFixed(250)
		must(err)
		counter := 0
		shot, err := s.CaptureBracketEV(dir, evs, func(t time.Time) string {
			b, n := hdrscan.NextShot(dir, "roll", t, counter)
			counter = n
			return b
		})
		must(err)
		fmt.Printf("PRESET %v frame0 %s\n", evs, e)
		for i, p := range shot.Raws {
			fmt.Printf("  RAW %d %s exposure %.6g (x%.4f)\n", i, filepath.Base(p), shot.Exposures[i], shot.Exposures[i]/shot.Exposures[0])
		}
		mo := hdrscan.DefaultOptions()
		mo.Exposure = shot.Exposures
		reg := r.Region
		res, err := hdrscan.ProcessN(shot.Raws, hdrscan.ProcessOptions{Merge: mo, LZW: true, MeteredBase: r.RGB, Software: "v077capture", BaseArea: r.Area, BaseRegion: &reg, LeakPct: 20})
		must(err)
		fmt.Printf("  HDR %s frames %d ratio-src %q order-note %q\n", filepath.Base(res.HDR), res.Frames, res.ExposureSrc, res.OrderNote)
		for _, st := range res.StatsN {
			fmt.Printf("  %s: ratio %.4f (EV %.3f, nominal %.4f, step %.4f, method %s) protect %.2f%% dropped %v warn %q\n",
				st.Label, st.Ratio, st.EV, st.Nominal, st.Step, st.Method, st.ProtectPct, st.Dropped, st.Warning)
		}
		fmt.Println("  ", res.BaseCheck())
	}
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
