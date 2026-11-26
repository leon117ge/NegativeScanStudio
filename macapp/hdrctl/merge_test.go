package hdrctl

import (
	"bytes"
	"context"
	"image"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"basetool/hdrscan"
	"basetool/rawconv"

	"basetool/macapp/appstate"
)

// bracket writes synthetic linear 16-bit frames of one negative: frame 0
// and frames 2 and 4 EV brighter (clipped), with a clear-base strip on the
// left, dark dust specks and a thin fibre. It returns the frame paths.
func bracket(t *testing.T, dir, prefix string, w, h, frames int) []string {
	t.Helper()
	rng := rand.New(rand.NewSource(17))
	type speck struct{ x, y, r float64 }
	var specks []speck
	for i := 0; i < 14; i++ {
		specks = append(specks, speck{60 + rng.Float64()*float64(w-80), 10 + rng.Float64()*float64(h-20), 1.2 + rng.Float64()*1.8})
	}
	rad := make([]float64, w*h*3)
	mask := [3]float64{1, 0.72, 0.38}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			l := 0.015 + 0.25*(0.5+0.5*math.Sin(float64(x)/47)*math.Cos(float64(y)/39))
			if x < w/12 {
				l = 0.62 // clear film base
			}
			att := 1.0
			for _, s := range specks {
				if d := math.Hypot(float64(x)-s.x, float64(y)-s.y); d < s.r+1 {
					att = math.Min(att, 0.25+0.75*math.Max(0, d-s.r+1)*0+0.75*math.Min(1, math.Max(0, d-s.r)))
				}
			}
			if fy := float64(h)/2 + 30*math.Sin(float64(x)/70); x > w/4 && x < w/2 && math.Abs(float64(y)-fy) < 1.2 {
				att = math.Min(att, 0.5)
			}
			for c := 0; c < 3; c++ {
				rad[3*(y*w+x)+c] = l * mask[c] * att * (1 + 0.01*rng.NormFloat64())
			}
		}
	}
	var out []string
	for f := 0; f < frames; f++ {
		k := math.Exp2(float64(2 * f))
		pix := make([]uint16, len(rad))
		for i, v := range rad {
			pix[i] = uint16(math.Min(65535, math.Max(0, math.Round(v*k*65535))))
		}
		p := filepath.Join(dir, prefix+[]string{"_0EV", "_2EV", "_4EV"}[f]+".tif")
		if err := rawconv.WriteTIFF16(p, w, h, pix, true, "Synthetic", "Bracket", "test"); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// windowsMergeOptions is the literal mergeFiles expression of hdrpage.go
// for the combo indexes (align, merge, comp, dust).
func windowsMergeOptions(align, merge, comp, dust int) hdrscan.ProcessOptions {
	mo := hdrscan.DefaultOptions()
	mo.Align = appstate.AlignModes[max(0, align)]
	mo.Method = appstate.MergeModes[max(0, merge)]
	leak := 20.0 // u.leak()
	return hdrscan.ProcessOptions{Merge: mo, LZW: comp == 1, Software: appstate.AppVersion, LeakPct: leak, Dust: max(0, dust)}
}

func copyFiles(t *testing.T, srcs []string, dir string) []string {
	t.Helper()
	var out []string
	for _, s := range srcs {
		b, err := os.ReadFile(s)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, filepath.Base(s))
		if err := os.WriteFile(p, b, 0644); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// TestMergeGoldenMatchesWindows merges with the macOS page and with the
// Windows options on copies of the same frames, with dust removal off,
// Light and Strong, and compares the HDR TIFFs byte for byte.
func TestMergeGoldenMatchesWindows(t *testing.T) {
	isolate(t)
	cases := []struct {
		name                     string
		frames                   int
		align, merge, comp, dust int
	}{
		{"two_frames_default", 2, 0, appstate.MergeDefault, 1, 0},
		{"three_frames_plain", 3, 2, 0, 0, 0},
		{"dust_light", 2, 0, appstate.MergeDefault, 1, 1},
		{"dust_strong_three", 3, 1, appstate.MergeDefault, 1, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := bracket(t, t.TempDir(), "Roll_0001", 640, 420, tc.frames)
			mac := copyFiles(t, src, t.TempDir())
			win := copyFiles(t, src, t.TempDir())
			h := newHost()
			c := New(h)
			c.MF = MergeFiles{Align: tc.align, Merge: tc.merge, Comp: tc.comp, Dust: tc.dust}
			c.DropMergeFiles(mac)
			if c.MF.Paths[0] != mac[0] || c.MF.Paths[1] != mac[1] || (tc.frames == 3) != (c.MF.Paths[2] != "") {
				t.Fatalf("fields %q", c.MF.Paths)
			}
			if !equalOptions(MergeFilesOptions(c.MF), windowsMergeOptions(tc.align, tc.merge, tc.comp, tc.dust)) {
				t.Fatal("options differ from mergeFiles")
			}
			c.RunMerge()
			if !c.MergeBusy() || !c.MergeStoppable() {
				t.Fatal("merge not running")
			}
			h.pump(t, "merge", 120*time.Second, func() bool { return !c.MergeBusy() })
			if c.LastMerge == nil {
				t.Fatalf("merge failed: %s", c.MRes)
			}
			if !strings.HasPrefix(c.MRes, "Saved "+filepath.Join(filepath.Dir(mac[0]), "Roll_0001_HDR.tiff")) {
				t.Fatalf("result %q", c.MRes)
			}
			if tc.dust > 0 && !strings.Contains(c.MRes, "Dust removal ("+hdrscan.DustName(tc.dust)+") applied in Roll_0001_HDR.tiff") {
				t.Fatalf("no dust line: %q", c.MRes)
			}
			res, err := hdrscan.ProcessN(win, windowsMergeOptions(tc.align, tc.merge, tc.comp, tc.dust))
			if err != nil {
				t.Fatal(err)
			}
			gm, err := os.ReadFile(c.LastMerge.HDR)
			if err != nil {
				t.Fatal(err)
			}
			gw, _ := os.ReadFile(res.HDR)
			// the embedded scan metadata carries its creation time (to the
			// second); everything else must be the same bytes
			nm, nw := maskCreated(t, gm), maskCreated(t, gw)
			if len(gm) < 100000 || !bytes.Equal(nm, nw) {
				t.Fatalf("macOS HDR (%d bytes) differs from the Windows path (%d bytes)", len(gm), len(gw))
			}
			pm, err := hdrscan.Load(c.LastMerge.HDR)
			if err != nil {
				t.Fatal(err)
			}
			pw, err := hdrscan.Load(res.HDR)
			if err != nil {
				t.Fatal(err)
			}
			if pm.W != pw.W || pm.H != pw.H || !bytes.Equal(u16b(pm.Pix), u16b(pw.Pix)) {
				t.Fatal("pixel data differs")
			}
			ds := c.LastMerge.DustStats
			t.Logf("%s: %d bytes identical apart from the creation time, pixels equal; dust %s: %d spots, %d px", tc.name, len(gm), hdrscan.DustName(ds.Level), ds.Spots, ds.Pixels)
			for _, s := range mac {
				if _, err := os.Stat(s); err != nil {
					t.Fatalf("source removed: %v", err)
				}
			}
		})
	}
}

var createdRE = regexp.MustCompile(`"created":"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d[+-]\d\d:\d\d"`)

// maskCreated blanks the metadata creation time (same length); there must
// be exactly one.
func maskCreated(t *testing.T, b []byte) []byte {
	t.Helper()
	locs := createdRE.FindAllIndex(b, -1)
	if len(locs) != 1 {
		t.Fatalf("%d creation times in the HDR", len(locs))
	}
	out := append([]byte(nil), b...)
	for i := locs[0][0] + 11; i < locs[0][1]-1; i++ {
		out[i] = '0'
	}
	return out
}

func equalOptions(a, b hdrscan.ProcessOptions) bool {
	return a.Merge.Align == b.Merge.Align && a.Merge.Method == b.Merge.Method && a.Merge.ClipT == b.Merge.ClipT &&
		a.Merge.Ramp == b.Merge.Ramp && a.Merge.Dilate == b.Merge.Dilate && a.Merge.BaseLevel == b.Merge.BaseLevel &&
		a.LZW == b.LZW && a.Software == b.Software && a.LeakPct == b.LeakPct && a.Dust == b.Dust &&
		a.OutDir == b.OutDir && a.KeepTIFF == b.KeepTIFF && a.DeleteRAW == b.DeleteRAW && a.Ctx == b.Ctx
}

func TestMergeValidationAndDrop(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	h := newHost()
	c := New(h)
	if c.MF.Comp != 1 || c.MF.Merge != appstate.MergeDefault || c.MRes != MergeResText || c.CanOpenMerge() {
		t.Fatalf("defaults %+v", c.MF)
	}
	c.RunMerge()
	if len(h.messages) != 1 || h.messages[0] != "Merge files: Choose frame 0 and a brighter frame (+2 or +3 EV) first; the third file is optional." {
		t.Fatalf("messages %q", h.messages)
	}
	a, b := filepath.Join(d, "a.tif"), filepath.Join(d, "b.tif")
	os.WriteFile(a, []byte("x"), 0644)
	os.WriteFile(b, []byte("x"), 0644)
	c.MF.Paths = [3]string{a, strings.ToUpper(a[:len(a)-5]) + a[len(a)-5:], ""}
	c.MF.Paths[1] = a
	c.RunMerge()
	if h.messages[1] != "Merge files: The same file is chosen twice: a.tif" {
		t.Fatalf("messages %q", h.messages)
	}
	c.MF.Paths = [3]string{a, b, filepath.Join(d, "gone.tif")}
	c.RunMerge()
	if !strings.HasPrefix(h.messages[2], "Merge files: The third file does not exist: ") {
		t.Fatalf("messages %q", h.messages)
	}
	// drop: one file fills the first empty field, two or more fill in name order
	c.MF.Paths = [3]string{}
	c.DropMergeFiles([]string{filepath.Join(d, "z.txt"), b})
	if c.MF.Paths != [3]string{b, "", ""} {
		t.Fatalf("one file: %q", c.MF.Paths)
	}
	c.DropMergeFiles([]string{b, a, filepath.Join(d, "n.NEF")})
	if c.MF.Paths != [3]string{a, b, filepath.Join(d, "n.NEF")} {
		t.Fatalf("three files: %q", c.MF.Paths)
	}
	h.file = filepath.Join(d, "picked.raf")
	c.BrowseMerge(2)
	if c.MF.Paths[2] != h.file {
		t.Fatalf("browse %q", c.MF.Paths[2])
	}
	// a failed merge shows the error in the result box
	c.MF.Paths = [3]string{a, b, ""}
	c.RunMerge()
	h.pump(t, "merge", 30*time.Second, func() bool { return !c.MergeBusy() })
	if !strings.HasPrefix(c.MRes, "Merge failed: ") || c.CanOpenMerge() {
		t.Fatalf("result %q", c.MRes)
	}
}

// TestMergeStop cancels a running merge: the partial output is removed and
// the sources are kept.
func TestMergeStop(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	src := bracket(t, d, "Big", 2400, 1600, 3)
	h := newHost()
	c := New(h)
	c.MF.Dust = 2
	c.DropMergeFiles(src)
	c.RunMerge()
	c.StopMerge()
	if c.MRes != "Stopping the merge ..." {
		t.Fatalf("result %q", c.MRes)
	}
	h.pump(t, "stop", 120*time.Second, func() bool { return !c.MergeBusy() })
	if c.MRes != "Merge stopped. Partial output removed; the source files are not changed." || c.MergeStoppable() || c.LastMerge != nil {
		t.Fatalf("result %q", c.MRes)
	}
	ents, _ := os.ReadDir(d)
	if len(ents) != 3 {
		var n []string
		for _, e := range ents {
			n = append(n, e.Name())
		}
		t.Fatalf("folder after Stop: %v", n)
	}
}

func TestOpenMergeInDevelop(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	h := newHost()
	c := New(h)
	var got string
	c.OpenInDevelop = func(p string) { got = p }
	hdr := filepath.Join(d, "x_HDR.tiff")
	c.LastMerge = &hdrscan.Result{HDR: hdr}
	if !c.CanOpenMerge() {
		t.Fatal("button disabled")
	}
	c.OpenMergeInDevelop()
	if got != "" || len(h.messages) != 1 || h.messages[0] != "Open in Develop: The HDR TIFF is gone: "+hdr {
		t.Fatalf("got %q messages %q", got, h.messages)
	}
	os.WriteFile(hdr, []byte("x"), 0644)
	c.OpenMergeInDevelop()
	if got != hdr {
		t.Fatalf("handed %q", got)
	}
}

// hairTIFF writes a flat grainy frame with a thin dark curl.
func hairTIFF(t *testing.T, path, software string) (w, h int) {
	t.Helper()
	w, h = 480, 320
	rng := rand.New(rand.NewSource(5))
	pix := make([]uint16, w*h*3)
	base := [3]float64{30000, 22000, 9000}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ev := 0.03 * rng.NormFloat64()
			best := 1e9
			for fx := 100.0; fx <= 300; fx += 1 {
				fy := 160 + 20*math.Sin(fx/50)
				best = math.Min(best, math.Hypot(float64(x)-fx, float64(y)-fy))
			}
			if best < 6 {
				ev -= 0.35 * math.Exp(-best*best/8)
			}
			for c := 0; c < 3; c++ {
				pix[3*(y*w+x)+c] = uint16(math.Min(65535, base[c]*math.Exp2(ev)))
			}
		}
	}
	if err := rawconv.WriteTIFF16(path, w, h, pix, true, "Synthetic", "Hair", software); err != nil {
		t.Fatal(err)
	}
	return
}

// windowsHair is the Windows code path: load, RemoveHair + Apply per box,
// and saveFile's HairSoftware + SaveHairTIFF.
func windowsHair(t *testing.T, src, to string, boxes []image.Rectangle) {
	t.Helper()
	p, err := hdrscan.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	ti, err := hdrscan.ReadTIFFInfo(src)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	if m := hairNoteRE.FindStringSubmatch(ti.Software); m != nil {
		n = atoiT(m[1])
	}
	for _, b := range boxes {
		e, err := hdrscan.RemoveHair(p.Pix, p.W, p.H, b, hdrscan.DefaultHairOptions())
		if err != nil {
			t.Fatal(err)
		}
		e.Apply(p.Pix)
	}
	ti.Software = hdrscan.HairSoftware(ti.Software, appVersion, n+len(boxes))
	if err := hdrscan.SaveHairTIFF(to, p.W, p.H, p.Pix, ti); err != nil {
		t.Fatal(err)
	}
}

func atoiT(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestHairGoldenMatchesWindows(t *testing.T) {
	isolate(t)
	box := image.Rect(80, 120, 320, 200)
	for _, name := range []string{"Roll_0002_HDR.tiff", "scan.tif"} {
		t.Run(name, func(t *testing.T) {
			mac, win := t.TempDir(), t.TempDir()
			pm := filepath.Join(mac, name)
			hairTIFF(t, pm, "NegativeScan Studio test")
			pw := copyFiles(t, []string{pm}, win)[0]
			h := newHost()
			c := New(h)
			hu := c.Hair
			hu.Open(pm)
			h.pump(t, "load", 20*time.Second, func() bool { return hu.Loaded() && !hu.Busy() })
			if !strings.Contains(hu.Status, "480x320") {
				t.Fatalf("status %q", hu.Status)
			}
			// a tiny box does nothing; zoom mode zooms
			hu.Box(image.Rect(10, 10, 12, 12), 800, 500)
			hu.Mode = 1
			hu.Box(image.Rect(100, 100, 140, 120), 800, 500)
			if vp := hu.Viewport(); vp.Dx() >= 480 || math.Abs(float64(vp.Dx())/float64(vp.Dy())-1.6) > 0.05 {
				t.Fatalf("zoom viewport %v", vp)
			}
			hu.Fit()
			hu.Mode = 0
			hu.Box(image.Rect(box.Max.X, box.Max.Y, box.Min.X, box.Min.Y), 800, 500) // dragged up-left
			if !hu.Busy() {
				t.Fatal("removal not on a worker")
			}
			h.pump(t, "removal", 30*time.Second, func() bool { return !hu.Busy() })
			if len(hu.Edits()) != 1 {
				t.Fatalf("edits %d: %q", len(hu.Edits()), hu.Status)
			}
			if img := hu.Preview(400, 300); img == nil || img.Bounds().Dx() > 400 {
				t.Fatal("preview")
			}
			hu.Save()
			h.pump(t, "save", 30*time.Second, func() bool { return !hu.Busy() && strings.HasPrefix(hu.Status, "Saved ") })
			to, inPlace := hdrscan.HairSavePath(pw)
			if inPlace != strings.HasSuffix(name, "_HDR.tiff") {
				t.Fatalf("in place %v", inPlace)
			}
			windowsHair(t, pw, to, []image.Rectangle{box})
			gm, err := os.ReadFile(hu.src)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(hu.src) != filepath.Base(to) {
				t.Fatalf("saved %s, Windows %s", hu.src, to)
			}
			gw, _ := os.ReadFile(to)
			if !bytes.Equal(gm, gw) {
				t.Fatalf("macOS file (%d bytes) differs from the Windows path (%d bytes)", len(gm), len(gw))
			}
			if !inPlace {
				if _, err := os.Stat(pm); err != nil {
					t.Fatal("source changed")
				}
			}
		})
	}
}

func TestHairUndoUnsavedAndLastHDR(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	p := filepath.Join(d, "a_HDR.tiff")
	hairTIFF(t, p, "x")
	h := newHost()
	c := New(h)
	hu := c.Hair
	hu.LastHDR()
	if len(h.messages) != 1 || !strings.Contains(h.messages[0], "No HDR made in this session yet.") {
		t.Fatalf("messages %q", h.messages)
	}
	hu.Open(filepath.Join(d, "x.jpg"))
	if !strings.Contains(h.messages[1], "Open a 16-bit TIFF") {
		t.Fatalf("messages %q", h.messages)
	}
	c.LastMerge = &hdrscan.Result{HDR: p}
	hu.LastHDR()
	h.pump(t, "load", 20*time.Second, func() bool { return hu.Loaded() && !hu.Busy() })
	orig := append([]uint16(nil), hu.pix...)
	hu.Box(image.Rect(80, 120, 320, 200), 800, 500)
	h.pump(t, "removal", 30*time.Second, func() bool { return !hu.Busy() })
	undo, save, _, _ := hu.Buttons()
	if !undo || !save {
		t.Fatal("buttons after a removal")
	}
	// reopening asks; No keeps the edit
	h.confirm = false
	hu.Open(p)
	if hu.Busy() || len(hu.Edits()) != 1 {
		t.Fatal("unsaved edit dropped without asking")
	}
	hu.Undo()
	if !bytes.Equal(u16b(hu.pix), u16b(orig)) || len(hu.Edits()) != 0 || !strings.HasPrefix(hu.Status, "Undone: ") {
		t.Fatalf("undo: %q", hu.Status)
	}
	if _, save, _, _ := hu.Buttons(); save {
		t.Fatal("save enabled with nothing to save")
	}
}

func u16b(p []uint16) []byte {
	b := make([]byte, 2*len(p))
	for i, v := range p {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	return b
}

var _ = context.Background
