package hdrctl

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/tiff"

	"basetool/colorsci"
	"basetool/hdrscan"
)

// chartColours are the reference patches of the synthetic chart in linear
// Rec709 (row A the neutral ramp, A1 darkest).
var chartColours = func() [24][3]float64 {
	var c [24][3]float64
	ramp := []float64{0.03, 0.06, 0.12, 0.22, 0.38, 0.62}
	for i, v := range ramp {
		c[i] = [3]float64{v, v, v}
	}
	cols := [][3]float64{
		{0.40, 0.08, 0.06}, {0.08, 0.35, 0.08}, {0.06, 0.08, 0.40}, {0.45, 0.40, 0.06}, {0.40, 0.08, 0.38}, {0.06, 0.36, 0.40},
		{0.30, 0.15, 0.08}, {0.12, 0.22, 0.30}, {0.25, 0.30, 0.10}, {0.18, 0.10, 0.25}, {0.50, 0.25, 0.10}, {0.10, 0.25, 0.20},
		{0.35, 0.20, 0.20}, {0.15, 0.15, 0.30}, {0.20, 0.35, 0.25}, {0.28, 0.12, 0.15}, {0.12, 0.30, 0.35}, {0.40, 0.35, 0.25},
	}
	copy(c[6:], cols)
	return c
}()

// writeChart writes a 16-bit TIFF of the chart as a camera with crosstalk
// sees it (scale: exposure; clip: patches pushed over full scale) and its
// CGATS reference (XYZ D50, 0..100).
func writeChart(t *testing.T, dir string, scale float64, rotate bool) (tif, ref string) {
	t.Helper()
	cross := colorsci.Mat3{{0.82, 0.14, 0.04}, {0.10, 0.80, 0.10}, {0.03, 0.15, 0.82}}
	const W, H = 1200, 800
	img := image.NewRGBA64(image.Rect(0, 0, W, H))
	put := func(x, y int, v [3]float64) {
		i := y*img.Stride + x*8
		for c := 0; c < 3; c++ {
			u := uint16(math.Round(math.Min(math.Max(v[c], 0), 1) * 65535))
			img.Pix[i+2*c], img.Pix[i+2*c+1] = uint8(u>>8), uint8(u)
		}
		img.Pix[i+6], img.Pix[i+7] = 0xff, 0xff
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			put(x, y, [3]float64{0.01, 0.01, 0.01})
		}
	}
	const ps, gap, x0, y0 = 140, 40, 80, 60
	for i := 0; i < 24; i++ {
		r, c := i/6, i%6
		cam := cross.Apply(chartColours[i])
		for k := range cam {
			cam[k] *= scale
		}
		px, py := x0+c*(ps+gap), y0+r*(ps+gap)
		if rotate {
			px, py = W-px-ps, H-py-ps
		}
		for y := py; y < py+ps; y++ {
			for x := px; x < px+ps; x++ {
				put(x, y, cam)
			}
		}
	}
	tif = filepath.Join(dir, "chart.tif")
	f, err := os.Create(tif)
	if err != nil {
		t.Fatal(err)
	}
	if err := tiff.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	var sb strings.Builder
	sb.WriteString("IT8.7/1\nDESCRIPTOR \"synthetic test chart\"\nILLUMINATION_NAME \"D50\"\nNUMBER_OF_FIELDS 4\nBEGIN_DATA_FORMAT\nSAMPLE_ID XYZ_X XYZ_Y XYZ_Z\nEND_DATA_FORMAT\nNUMBER_OF_SETS 24\nBEGIN_DATA\n")
	for i, c := range chartColours {
		x := colorsci.Rec709ToXYZD50.Apply(c)
		fmt.Fprintf(&sb, "%s %.4f %.4f %.4f\n", colorsci.CamPatchNames[i], x[0]*100, x[1]*100, x[2]*100)
	}
	sb.WriteString("END_DATA\n")
	ref = filepath.Join(dir, "chart_ref.txt")
	if err := os.WriteFile(ref, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return tif, ref
}

func newCal(t *testing.T) (*testHost, *Ctl, *ChartCal) {
	t.Helper()
	isolate(t)
	camMu.Lock()
	camSet = nil // camera_profiles.json of this test's HOME
	camMu.Unlock()
	h := newHost()
	c := New(h)
	return h, c, c.Cal
}

func idle(t *testing.T, h *testHost, k *ChartCal, what string) {
	t.Helper()
	h.pump(t, what, 60*time.Second, func() bool { return k.Busy == "" && k.Patches != nil && k.sample == k.sampled() })
}

func waitLog(t *testing.T, h *testHost, c *Ctl, line string) {
	t.Helper()
	h.pump(t, "log line "+line, 5*time.Second, func() bool { return strings.Contains(c.LogText(), line) })
}

// sampled is the generation the overlay patches belong to (tests only).
func (k *ChartCal) sampled() int { return k.sampledGen }

// windowsSolveText is chartCal.solve after k.changed(), verbatim from the
// Windows dialog except that k.ps is ps and the text is returned instead
// of written to resTE.
func windowsSolveText(ps [24]colorsci.CamPatch, ref colorsci.CamRef) (*colorsci.CamFit, string) {
	f, err := colorsci.FitCamChart(ps, ref)
	var sb strings.Builder
	if ev := colorsci.ClipStops(ps, [3]float64{1, 1, 1}); len(f.Clipped) > 0 || ev > 0 {
		cl := f.Clipped
		if err != nil {
			cl = nil
			for i, p := range ps {
				if p.Clipped {
					cl = append(cl, colorsci.CamPatchNames[i])
				}
			}
		}
		stops := f.ClipEV
		if err != nil || stops == 0 {
			stops = math.Max(ev, 1.0/3)
		}
		fmt.Fprintf(&sb, "WARNING: %d patch(es) clipped (>%.0f%% of pixels above %.0f%% FS), left out: %s. Reduce exposure by about %.1f stop(s) and rescan to use them.\r\n",
			len(cl), colorsci.CamClipFrac*100, colorsci.CamClipLevel*100, strings.Join(cl, " "), stops)
	}
	if err != nil {
		sb.WriteString("Could not fit: " + err.Error())
		return nil, sb.String()
	}
	fmt.Fprintf(&sb, "Neutral-preserving 3x3 on %d clean patches (white balance factored out, camera grey unchanged).\r\n", f.Used)
	fmt.Fprintf(&sb, "ΔE2000  before (per-channel gain on %s): mean %.2f  max %.2f    after: mean %.2f  max %.2f\r\n", f.GainPatch, f.MeanB, f.MaxB, f.MeanA, f.MaxA)
	fmt.Fprintf(&sb, "Matrix  R' = %8.5f R %+8.5f G %+8.5f B\r\n", f.Mx[0][0], f.Mx[0][1], f.Mx[0][2])
	fmt.Fprintf(&sb, "        G' = %8.5f R %+8.5f G %+8.5f B\r\n", f.Mx[1][0], f.Mx[1][1], f.Mx[1][2])
	fmt.Fprintf(&sb, "        B' = %8.5f R %+8.5f G %+8.5f B\r\n", f.Mx[2][0], f.Mx[2][1], f.Mx[2][2])
	fmt.Fprintf(&sb, "Film-base multipliers (row sums; a measured base R/G/B is scaled by these when the profile is on): R %.4f  G %.4f  B %.4f\r\n", f.BaseMul[0], f.BaseMul[1], f.BaseMul[2])
	fmt.Fprintf(&sb, "Camera grey R:G:B = %.4f : %.4f : %.4f\r\n\r\n", f.Grey[0], f.Grey[1], f.Grey[2])
	sb.WriteString("Patch  R        G        B        ΔE before  ΔE after\r\n")
	for i, p := range ps {
		st := ""
		if !f.Use[i] {
			st = "  (clipped, not fitted)"
		}
		fmt.Fprintf(&sb, "%-5s  %.5f  %.5f  %.5f  %8.2f  %8.2f%s\r\n", colorsci.CamPatchNames[i], p.Med[0], p.Med[1], p.Med[2], f.DEBefore[i], f.DEAfter[i], st)
	}
	return &f, sb.String()
}

// windowsChart replays the Windows dialog on the same files: load, detect,
// changed (sample), solve.
func windowsChart(t *testing.T, tif, refPath string) (colorsci.ChartCorners, string, *colorsci.CamFit, string) {
	t.Helper()
	pl, err := hdrscan.Load(tif)
	if err != nil {
		t.Fatal(err)
	}
	pix, w, h := append([]uint16(nil), pl.Pix...), pl.W, pl.H
	pl.Free()
	c, orient, err := colorsci.DetectCamChart(pix, w, h)
	if err != nil {
		t.Fatalf("Windows detect: %v", err)
	}
	f, _ := os.Open(refPath)
	ref, err := colorsci.ParseCGATS(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	ps := colorsci.SampleCamChart(pix, w, h, c, 0.25)
	fit, text := windowsSolveText(ps, ref)
	return c, orient, fit, text
}

func TestChartCalMatchesWindows(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scale  float64
		rotate bool
	}{{"clean", 1.0, false}, {"rotated", 1.0, true}, {"clipped", 1.9, false}} {
		t.Run(tc.name, func(t *testing.T) {
			h, c, k := newCal(t)
			calNow = func() time.Time { return time.Date(2026, 11, 27, 10, 30, 0, 0, time.UTC) }
			defer func() { calNow = time.Now }()
			tif, ref := writeChart(t, t.TempDir(), tc.scale, tc.rotate)
			wc, orient, wfit, wtext := windowsChart(t, tif, ref)
			t.Logf("chart %s, fit %v, report starts %q", orient, wfit != nil, strings.SplitN(wtext, "\r\n", 2)[0])

			k.Open()
			if k.Name != "XT3 trichrome 2026-11-27" || k.FileText != "No chart loaded." || k.RefText != "No reference loaded." {
				t.Fatalf("fresh dialog: %q %q %q", k.Name, k.FileText, k.RefText)
			}
			h.file = ref
			k.OpenRef()
			if !strings.HasPrefix(k.RefText, "chart_ref.txt  (24 patches, D50)") {
				t.Fatalf("ref label %q", k.RefText)
			}
			k.Load(tif)
			idle(t, h, k, "chart loaded")
			if k.FileText != "chart.tif  (1200 x 800)" {
				t.Errorf("file label %q", k.FileText)
			}
			if k.Corners != wc {
				t.Errorf("corners %v, Windows %v", k.Corners, wc)
			}
			if want := "Grid found automatically (chart " + orient + "). Check the labels, then press Fit."; k.ResText != want {
				t.Errorf("detect text %q, want %q", k.ResText, want)
			}
			k.Fit()
			idle(t, h, k, "fit")
			if k.ResText != wtext {
				t.Errorf("fit report differs from Windows:\nmacOS:\n%s\nWindows:\n%s", k.ResText, wtext)
			}
			if (wfit != nil) != k.SaveOK {
				t.Fatalf("Save enabled %v, Windows fit %v", k.SaveOK, wfit != nil)
			}
			if wfit == nil {
				return
			}
			if tc.name == "clipped" && !strings.HasPrefix(wtext, "WARNING: ") {
				t.Errorf("clipped chart without warning:\n%s", wtext)
			}
			k.Save()
			b, err := os.ReadFile(camPath())
			if err != nil {
				t.Fatal(err)
			}
			// Windows save: CamProfile{Name, M: Mx, Neutral, Info} appended,
			// made active, written with MarshalIndent.
			info := fmt.Sprintf("chart %s, ref %s, %s, %d patches, ΔE2000 mean %.2f max %.2f (before %.2f / %.2f)",
				"chart.tif", "chart_ref.txt", "2026-11-27 10:30", wfit.Used, wfit.MeanA, wfit.MaxA, wfit.MeanB, wfit.MaxB)
			ws := CamSettings{Profiles: []CamProfile{{Name: "XT3 trichrome 2026-11-27", M: wfit.Mx, Neutral: wfit.Neutral, Info: info}},
				Active: "XT3 trichrome 2026-11-27", Strength: 1, LastRef: ref}
			wb, _ := json.MarshalIndent(ws, "", "  ")
			if string(b) != string(wb) {
				t.Errorf("camera_profiles.json differs from Windows:\nmacOS:\n%s\nWindows:\n%s", b, wb)
			}
			if !strings.HasPrefix(k.ResText, "Saved camera profile \"XT3 trichrome 2026-11-27\" and made it the active profile (Enable is off).\r\n") {
				t.Errorf("save text %q", k.ResText[:min(len(k.ResText), 120)])
			}
			if len(k.Profiles) != 2 || k.ProfIdx != 1 || k.Enabled {
				t.Errorf("profile list %v %d %v", k.Profiles, k.ProfIdx, k.Enabled)
			}
			waitLog(t, h, c, "Camera profile \"XT3 trichrome 2026-11-27\" saved (")
		})
	}
}

func TestChartCalProfilesAndGrid(t *testing.T) {
	h, c, k := newCal(t)
	k.Open()
	// UseLast without a trichrome scan
	saved := lastTriOut
	lastTriOut = ""
	k.UseLast()
	lastTriOut = saved
	if len(h.messages) != 1 || h.messages[0] != "Use last scan: No trichrome scan has been written in this session yet." {
		t.Fatalf("messages %q", h.messages)
	}
	k.Fit()
	if h.messages[1] != "Fit: Open a chart TIFF and load its reference first." {
		t.Fatalf("messages %q", h.messages)
	}
	tif, ref := writeChart(t, t.TempDir(), 1, false)
	k.loadRef(ref, false)
	k.Load(tif)
	idle(t, h, k, "loaded")
	// Rotate 180 swaps the A1 and D6 ends; Reset grid is the default frame.
	c0 := k.Corners
	k.Rotate()
	if k.Corners != (colorsci.ChartCorners{c0[2], c0[3], c0[0], c0[1]}) {
		t.Errorf("rotate %v", k.Corners)
	}
	idle(t, h, k, "resampled")
	k.ResetGrid()
	w, hh := 1200.0, 800.0
	cw := math.Min(w*0.8, hh*0.8*1.5)
	ch := cw / 1.5
	want := colorsci.ChartCorners{{(w-cw)/2 + cw/12, (hh-ch)/2 + ch/8}, {(w+cw)/2 - cw/12, (hh-ch)/2 + ch/8}, {(w+cw)/2 - cw/12, (hh+ch)/2 - ch/8}, {(w-cw)/2 + cw/12, (hh+ch)/2 - ch/8}}
	if k.Corners != want {
		t.Errorf("reset %v want %v", k.Corners, want)
	}
	idle(t, h, k, "resampled")
	// Drag A1 onto a new spot: the fit is dropped, sampling follows the drop.
	ir := k.ImageRect(600, 400)
	p := k.ToView(ir, k.Corners[0][0], k.Corners[0][1])
	k.MouseDown(ir, p.X+5, p.Y+5, 14)
	if !k.Dragging() {
		t.Fatal("corner not picked")
	}
	k.MouseMove(ir, 60, 70)
	k.MouseUp()
	if k.Corners[0] != [2]float64{120, 140} || k.SaveOK {
		t.Errorf("dragged corner %v save %v", k.Corners[0], k.SaveOK)
	}
	idle(t, h, k, "resampled after drag")
	k.MouseDown(ir, 1, 399, 14) // far from every marker
	if k.Dragging() {
		t.Error("picked a marker far away")
	}
	k.Detect()
	idle(t, h, k, "detected")
	k.Fit()
	idle(t, h, k, "fit")
	if !k.SaveOK {
		t.Fatalf("no fit: %s", k.ResText)
	}
	// Bad name, then save twice (the second asks to replace).
	k.SetName("bad/name")
	k.Save()
	if h.messages[len(h.messages)-1] != "Save: Enter a profile name." {
		t.Errorf("messages %q", h.messages)
	}
	k.SetName("Roll A")
	k.Save()
	h.confirm = false
	k.Save()
	if s := camSettings(); len(s.Profiles) != 1 {
		t.Errorf("profiles %d", len(s.Profiles))
	}
	h.confirm = true
	k.SetName("Roll B")
	k.Save()
	if s := camSettings(); len(s.Profiles) != 2 || s.Active != "Roll B" {
		t.Errorf("profiles %+v", s)
	}
	// Enable and Strength go to camera_profiles.json; activeCam follows.
	k.ProfIdx = 1
	k.Enabled = true
	k.SettingsChanged()
	k.SetStrength("1.7")
	if p, st := activeCam(); p == nil || p.Name != "Roll A" || st != 1 || k.Strength != 1 {
		t.Errorf("active %v %v strength %v", p, st, k.Strength)
	}
	k.SetStrength("0.456")
	if _, st := activeCam(); st != 0.46 {
		t.Errorf("strength %v", st)
	}
	waitLog(t, h, c, "Camera profile \"Roll A\" enabled at strength 0.46: applied to the trichrome film-base measurement.")
	k.SetStrength("x")
	if k.Strength != 0.46 {
		t.Errorf("bad strength accepted: %v", k.Strength)
	}
	// Delete the active profile: Active and Enabled are cleared.
	k.DeleteProfile()
	if s := camSettings(); len(s.Profiles) != 1 || s.Active != "" || s.Enabled || k.ProfIdx != 0 {
		t.Errorf("after delete %+v idx %d", s, k.ProfIdx)
	}
	k.ProfIdx = 0
	k.Enabled = false
	k.SettingsChanged()
	waitLog(t, h, c, "Camera profile: off.")
	// Reopen: the last reference loads quietly; a missing one says why.
	k.Close()
	k.Open()
	if !k.RefOK || !strings.HasPrefix(k.RefText, "chart_ref.txt") || k.Preview != nil {
		t.Errorf("reopen ref %q preview %v", k.RefText, k.Preview != nil)
	}
	os.Remove(ref)
	k.Open()
	if !strings.HasPrefix(k.RefText, "Last reference chart_ref.txt not loaded: ") {
		t.Errorf("missing ref %q", k.RefText)
	}
}

// A job that finishes after Close must not touch the closed panel.
func TestChartCalCloseDropsJob(t *testing.T) {
	h, _, k := newCal(t)
	tif, _ := writeChart(t, t.TempDir(), 1, false)
	k.Open()
	k.Load(tif)
	k.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case f := <-h.q:
			f()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if k.Preview != nil || k.FileText == "chart.tif  (1200 x 800)" {
		t.Error("closed panel was updated by a late load")
	}
}

// Every user-visible string of the Windows dialog is in the macOS port
// (labels, tooltips, messages, report lines), so the two stay in step.
func TestChartCalStringsMatchWindows(t *testing.T) {
	lits := func(files ...string) map[string]bool {
		out := map[string]bool{}
		for _, f := range files {
			fs := token.NewFileSet()
			af, err := parser.ParseFile(fs, f, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(af, func(n ast.Node) bool {
				if b, ok := n.(*ast.BasicLit); ok && b.Kind == token.STRING {
					if s, err := strconv.Unquote(b.Value); err == nil {
						out[strings.ReplaceAll(s, "\r\n", "\n")] = true
					}
				}
				return true
			})
		}
		return out
	}
	win := lits("../../chartcal.go")
	mac := lits("chartcal.go", "copied_cal.go", "../chartcalview.go")
	walkOnly := map[string]bool{
		"basetool/colorsci": true, "basetool/hdrscan": true, "github.com/lxn/walk": true, "github.com/lxn/walk/declarative": true,
		"fmt": true, "image": true, "image/color": true, "math": true, "os": true, "path/filepath": true, "sort": true, "strings": true, "time": true,
		"[%.3f %.3f %.3f | %.3f %.3f %.3f | %.3f %.3f %.3f]": true, // matText, not called on Windows
		"Consolas": true, "Segoe UI": true, "Chart calibration": true, // dialog creation error title
		"16-bit TIFF (*.tif;*.tiff)|*.tif;*.tiff|All files (*.*)|*.*":                           true, // native panels take extensions
		"CGATS / IT8 (*.txt;*.it8;*.cie;*.cgats)|*.txt;*.it8;*.cie;*.cgats|All files (*.*)|*.*": true,
	}
	n := 0
	for s := range win {
		if walkOnly[s] {
			continue
		}
		n++
		if !mac[s] {
			t.Errorf("Windows string missing on macOS: %q", s)
		}
	}
	if n < 50 {
		t.Fatalf("only %d strings checked", n)
	}
}
