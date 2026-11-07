// m1check verifies the v0.51 link between "3. HDR scan" and "1. Film base"
// on Linux, using module 1's measuring code extracted verbatim (m1_gen.go):
//  1. measure() is unchanged against the v0.5 code on existing TIFFs;
//  2. meter -> lock -> ProbeBase values (what "Send base" fills after a lock);
//  3. capture -> Process: Result.Base1 (what "Send base" fills after a merge)
//     equals module 1's measureRegion on the written HDR TIFF (LZW and not);
//  4. module 1 decodes the HDR TIFF and measures it on the edge bands.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"

	"basetool/camera"
	"basetool/hdrscan"
)

func main() {
	scene := flag.String("scene", "./testdata/negsamples2/_DSF9229.RAF", "scene RAF")
	out := flag.String("out", "./testdata/m1check_out", "output dir")
	flag.Parse()
	fail := false
	check := func(ok bool, f string, a ...any) {
		s := "PASS"
		if !ok {
			s, fail = "FAIL", true
		}
		fmt.Printf("%s  %s\n", s, fmt.Sprintf(f, a...))
	}

	// 1. measure unchanged
	for _, t := range flag.Args() {
		a, ea := measure(t, 0.5, 1.0, 20)
		b, eb := measureOld(t, 0.5, 1.0, 20)
		if ea != nil || eb != nil {
			check((ea == nil) == (eb == nil), "%s errors new=%v old=%v", filepath.Base(t), ea, eb)
			continue
		}
		check(a.R == b.R && a.G == b.G && a.B == b.B && a.Edges == b.Edges && a.Rejected == b.Rejected && a.Status == b.Status,
			"measure unchanged on %s: %.6f %.6f %.6f (old %.6f %.6f %.6f)", filepath.Base(t), a.R, a.G, a.B, b.R, b.G, b.B)
	}

	os.RemoveAll(*out)
	os.MkdirAll(*out, 0o755)
	lg := camera.NewLogger(*out)
	sim, err := camera.OpenSim(*scene, lg)
	if err != nil {
		log.Fatal(err)
	}
	sim.Native = camera.Exposure{ISO: 160, Shutter: 7812}
	sim.SetDials(camera.Exposure{ISO: 160, Shutter: 1953})
	s := &hdrscan.Session{Dev: sim, Log: lg, Target: 0.80, ProbeDir: filepath.Join(*out, "probe")}
	e, r, err := s.Meter()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("LOCKED %s  reading %s  region %v bin %d cells-mask %v\n", e, r, r.Region.Rect, r.Region.Bin, r.Region.Mask != nil)
	pb, pn, err := s.ProbeBase(20)
	check(err == nil && pn >= 50, "Send base after lock (probe RAW): R %.4f G %.4f B %.4f, %d samples, err %v", pb[0], pb[1], pb[2], pn, err)

	for _, cs := range []struct {
		n   int
		lzw bool
	}{{2, true}, {2, false}, {3, true}, {3, false}} {
		lzw := cs.lzw
		dir := filepath.Join(*out, fmt.Sprintf("n%d_lzw_%v", cs.n, lzw))
		os.MkdirAll(dir, 0o755)
		raws, err := s.CaptureBracket(dir, cs.n)
		if err != nil {
			log.Fatal(err)
		}
		r0 := raws[0]
		reg := r.Region
		res, err := hdrscan.ProcessN(raws, hdrscan.ProcessOptions{Merge: hdrscan.DefaultOptions(), KeepTIFF: false, LZW: lzw, DeleteRAW: cs.n == 3,
			Software: "m1check", BaseArea: r.Area, BaseRegion: &reg, LeakPct: 20})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("n=%d HDR %s  %s\n", cs.n, res.HDR, res.BaseCheck())
		check(res.BaseDiff == 0 && res.BaseTotal > 0, "n=%d base area identical to frame 0", cs.n)
		for k, st := range res.StatsN {
			want := math.Pow(4, float64(k+1))
			check(math.Abs(st.Ratio/want-1) < 0.05, "n=%d %s ratio %.4f ~ %.0f", cs.n, st.Label, st.Ratio, want)
		}
		if cs.n == 3 {
			gone := 0
			for _, f := range raws {
				if _, e := os.Stat(f); os.IsNotExist(e) {
					gone++
				}
			}
			check(gone == 3, "DeleteRAW removed all %d RAFs (%d gone)", len(raws), gone)
		}
		_, statErr := os.Stat(r0 + ".tiff")
		fmt.Printf("frame TIFFs deleted: %v (values kept in Result)\n", os.IsNotExist(statErr) || len(res.TIFFs) == 0)
		row, err := measureRegion(res.HDR, "x", &res.Base1Region, 20)
		if err != nil {
			check(false, "module 1 measureRegion on HDR: %v", err)
			continue
		}
		check(row.R == res.Base1[0] && row.G == res.Base1[1] && row.B == res.Base1[2],
			"LZW=%v sent %.6f %.6f %.6f == module 1 re-measure of the TIFF %.6f %.6f %.6f", lzw, res.Base1[0], res.Base1[1], res.Base1[2], row.R, row.G, row.B)
		check(res.Base1 == res.Base1F0, "HDR base == frame 0 base (%.6f %.6f %.6f)", res.Base1F0[0], res.Base1F0[1], res.Base1F0[2])
		d := func(a, b float64) float64 { return (a/b - 1) * 100 }
		fmt.Printf("probe vs capture: R %+.2f%% G %+.2f%% B %+.2f%%\n", d(pb[0], res.Base1[0]), d(pb[1], res.Base1[1]), d(pb[2], res.Base1[2]))
		m, err := measure(res.HDR, 0.5, 1.0, 20)
		check(err == nil, "Open in Film base: module 1 reads + measures HDR edge bands: %v %s", err, func() string {
			if m == nil {
				return ""
			}
			return fmt.Sprintf("%.4f %.4f %.4f %s", m.R, m.G, m.B, m.Status)
		}())
	}
	if fail {
		os.Exit(1)
	}
}
