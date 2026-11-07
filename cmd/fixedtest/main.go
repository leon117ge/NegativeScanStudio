// fixedtest exercises v0.63's fixed frame 0, Record base, the film-base DB
// and the per-roll folder offline with the simulated camera.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"basetool/camera"
	"basetool/filmdb"
	"basetool/hdrscan"
)

func main() {
	scene := flag.String("scene", "./testdata/negsamples2/_DSF9229.RAF", "scene RAF")
	save := flag.String("save", "./testdata/dust063/fixedtest", "Save-to folder")
	native := flag.Uint("native", 31250, "native shutter at ISO 160 (scene 1:1)")
	roll := flag.String("roll", "Portra400 Kyoto", "roll name")
	flag.Parse()
	os.RemoveAll(*save)
	first := time.Now()
	dir, err := hdrscan.RollDir(*save, *roll, first)
	must(err)
	fmt.Println("ROLL DIR:", dir)
	lg := camera.NewLogger(*save)
	sim, err := camera.OpenSim(*scene, lg)
	must(err)
	sim.Native = camera.Exposure{ISO: 160, Shutter: uint32(*native)}
	sim.SetDials(camera.Exposure{ISO: 160, Shutter: 4000})
	s := &hdrscan.Session{Dev: sim, Log: lg, Target: 0.8, ProbeDir: filepath.Join(dir, "probe")}
	s.Dials = func(e camera.Exposure, why string) bool { sim.SetDials(e); return true }

	// 1. fixed frame 0 at ISO 250
	e, r, warns, err := s.LockFixed(250)
	must(err)
	fmt.Printf("LOCKFIXED %s  max %.3f peak %.3f pure %v warnings %q\n", e, r.Max, r.Peak, hdrscan.PureBase(r), warns)
	pb, pn, err := s.ProbeBase(20)
	must(err)
	fmt.Printf("PROBE BASE (module 1 scale) R %.4f G %.4f B %.4f n %d\n", pb[0], pb[1], pb[2], pn)

	// 2. capture 0/+3 into the roll folder
	counter := 0
	shot, err := s.CaptureBracketEV(dir, []int{0, 3}, func(t time.Time) string {
		b, n := hdrscan.NextShot(dir, *roll, t, counter)
		counter = n
		return b
	})
	must(err)
	fmt.Println("BRACKET:", names(shot.Raws), "exposure values:", shot.Exposures)
	fmt.Println("FRAME0", hdrscan.EXIFCheck(shot.Raws[0], e), "(sim: EXIF is the scene file's)")
	mo := hdrscan.DefaultOptions()
	mo.Exposure = shot.Exposures
	reg := r.Region
	res, err := hdrscan.ProcessN(shot.Raws, hdrscan.ProcessOptions{Merge: mo, LZW: true, MeteredBase: r.RGB, Software: "fixedtest", BaseArea: r.Area, BaseRegion: &reg, LeakPct: 20})
	must(err)
	fmt.Println("HDR:", filepath.Base(res.HDR), "|", res.BaseCheck())
	fmt.Printf("FRAME0 BASE1 R %.4f G %.4f B %.4f (n %d) vs probe: %s\n", res.Base1F0[0], res.Base1F0[1], res.Base1F0[2], res.Base1N, evDiff(res.Base1F0, pb))

	// 3. Record base twice (ISO 250, 1/60), same roll folder
	var recs []*hdrscan.BaseRecord
	for i := 0; i < 2; i++ {
		rec, n, err := s.RecordBase(dir, *roll, hdrscan.RefISO, counter, 20)
		must(err)
		counter = n
		recs = append(recs, rec)
		fmt.Printf("RECORD %d: %s  %s  base1 R %.4f G %.4f B %.4f n %d warnings %q | %s\n", i+1, filepath.Base(rec.Raw), rec.Exposure, rec.Base1[0], rec.Base1[1], rec.Base1[2], rec.Base1N, rec.Warnings, rec.EXIF)
	}
	fmt.Println("RECORD vs frame 0 of the capture:", evDiff(recs[0].Base1, res.Base1F0))

	// 4. DB: new, average, compare
	db, _ := filmdb.Load(filepath.Join(*save, filmdb.FileName))
	db.Import([]filmdb.Preset{{Name: "Ektar 100", R: 0.55, G: 0.40, B: 0.20}})
	ent := filmdb.Entry{Name: "Portra 400", R: recs[0].Base1[0], G: recs[0].Base1[1], B: recs[0].Base1[2], ISO: 250, Shutter: camera.Shutter1_60, Aperture: 4, Samples: 1, Source: "record"}
	_, err = db.Put(ent, false)
	must(err)
	ent2 := ent
	ent2.R, ent2.G, ent2.B = recs[1].Base1[0], recs[1].Base1[1], recs[1].Base1[2]
	got, err := db.Put(ent2, true)
	must(err)
	must(db.Save())
	fmt.Printf("DB after average: %s R %.4f G %.4f B %.4f samples %d (%s)\n", got.Name, got.R, got.G, got.B, got.Samples, filmdb.Cond(*got))
	fmt.Println("COMPARE frame 0 (ISO 250):", filmdb.Compare(*got, res.Base1F0, e.ISO, e.Shutter, 0.10))
	e2, r2, w2, err := s.LockFixed(160)
	must(err)
	b2, _, _ := s.ProbeBase(20)
	fmt.Printf("LOCKFIXED %s max %.3f warnings %q\n", e2, r2.Max, w2)
	fmt.Println("COMPARE frame 0 (ISO 160):", filmdb.Compare(*got, b2, e2.ISO, e2.Shutter, 0.10))
	e3, r3, w3, err := s.LockFixed(320)
	must(err)
	fmt.Printf("LOCKFIXED %s max %.3f peak %.3f warnings %q\n", e3, r3.Max, r3.Peak, w3)
	sim.Native = camera.Exposure{ISO: 160, Shutter: 250000}
	e4, r4, w4, err := s.LockFixed(160)
	must(err)
	fmt.Printf("LOCKFIXED (dim light) %s max %.3f warnings %q\n", e4, r4.Max, w4)

	// 5. existing roll folder: continue, never overwrite
	sim.Native = camera.Exposure{ISO: 160, Shutter: uint32(*native)}
	dir2, err := hdrscan.RollDir(*save, *roll, first)
	must(err)
	_, _, _, err = s.LockFixed(250)
	must(err)
	shot2, err := s.CaptureBracketEV(dir2, []int{0, 3}, func(t time.Time) string {
		b, n := hdrscan.NextShot(dir2, *roll, t, 0) // counter lost (e.g. new install)
		counter = n
		return b
	})
	must(err)
	fmt.Println("SECOND SESSION same folder (counter 0):", names(shot2.Raws))

	// 6. empty roll name
	dir3, err := hdrscan.RollDir(*save, "", first)
	must(err)
	shot3, err := s.CaptureBracketEV(dir3, []int{0, 3}, func(t time.Time) string {
		b, n := hdrscan.NextShot(dir3, "", t, counter)
		counter = n
		return b
	})
	must(err)
	fmt.Println("EMPTY ROLL dir:", filepath.Base(dir3), names(shot3.Raws))
	listTree(*save)
}

func evDiff(a, b [3]float64) string {
	return fmt.Sprintf("R %+.3f G %+.3f B %+.3f EV", math.Log2(a[0]/b[0]), math.Log2(a[1]/b[1]), math.Log2(a[2]/b[2]))
}

func names(p []string) []string {
	var o []string
	for _, x := range p {
		o = append(o, filepath.Base(x))
	}
	return o
}

func listTree(root string) {
	var all []string
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			rel, _ := filepath.Rel(root, p)
			all = append(all, fmt.Sprintf("%s (%d KB)", rel, fi.Size()/1024))
		}
		return nil
	})
	sort.Strings(all)
	for _, a := range all {
		fmt.Println("FILE:", a)
	}
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
