//go:build darwin || (linux && gio)

package main

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"golang.org/x/image/tiff"

	"basetool/colorsci"
	"basetool/macapp/appstate"
	"basetool/scanmeta"
)

// uiChart writes a synthetic 24-patch chart TIFF (1500 x 1000, A6 pushed to
// clipping when clip is set) and its CGATS reference.
func uiChart(t *testing.T, dir string, clip bool) (string, string) {
	t.Helper()
	ramp := []float64{0.03, 0.06, 0.12, 0.22, 0.38, 0.62}
	var cols [24][3]float64
	for i, v := range ramp {
		cols[i] = [3]float64{v, v, v}
	}
	for i := 6; i < 24; i++ {
		f := float64(i)
		cols[i] = [3]float64{0.08 + 0.4*math.Abs(math.Sin(f)), 0.08 + 0.35*math.Abs(math.Sin(f*1.7)), 0.08 + 0.35*math.Abs(math.Cos(f*0.9))}
	}
	cross := colorsci.Mat3{{0.82, 0.14, 0.04}, {0.10, 0.80, 0.10}, {0.03, 0.15, 0.82}}
	const W, H, ps, gap, x0, y0 = 1500, 1000, 170, 50, 110, 90
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
			put(x, y, [3]float64{0.012, 0.011, 0.010})
		}
	}
	for i := 0; i < 24; i++ {
		v := cross.Apply(cols[i])
		if clip && i == 5 {
			v = [3]float64{1, 1, 1}
		}
		px, py := x0+(i%6)*(ps+gap), y0+(i/6)*(ps+gap)
		for y := py; y < py+ps; y++ {
			for x := px; x < px+ps; x++ {
				put(x, y, v)
			}
		}
	}
	tif := filepath.Join(dir, "IT8_chart_RGB.tif")
	f, err := os.Create(tif)
	if err != nil {
		t.Fatal(err)
	}
	tiff.Encode(f, img, nil)
	f.Close()
	var sb strings.Builder
	sb.WriteString("IT8.7/1\nILLUMINATION_NAME \"D50\"\nBEGIN_DATA_FORMAT\nSAMPLE_ID XYZ_X XYZ_Y XYZ_Z\nEND_DATA_FORMAT\nBEGIN_DATA\n")
	for i, c := range cols {
		x := colorsci.Rec709ToXYZD50.Apply(c)
		fmt.Fprintf(&sb, "%s %.4f %.4f %.4f\n", colorsci.CamPatchNames[i], x[0]*100, x[1]*100, x[2]*100)
	}
	sb.WriteString("END_DATA\n")
	ref := filepath.Join(dir, "R231215.txt")
	os.WriteFile(ref, []byte(sb.String()), 0o644)
	return tif, ref
}

// TestChartCalibrationPanel opens Chart calibration from the Camera card,
// loads a chart and its reference through the (stubbed) native panels,
// fits, drags a corner, saves a profile and closes; with NSS_SHOTS=<dir>
// the frames are saved. It also shows the Camera card's leveling switch.
func TestChartCalibrationPanel(t *testing.T) {
	isolate(t)
	w, err := headless.NewWindow(1280, 800)
	if err != nil {
		t.Skip("no headless GPU:", err)
	}
	defer w.Release()
	// an accepted leveling that matches the stored light values
	lev := scanmeta.Leveling{At: time.Date(2026, 11, 26, 13, 5, 0, 0, time.UTC).Format(time.RFC3339),
		Base: [3]float64{0.41234, 0.38765, 0.21111}, Values: [3]int{44, 67, 99}}
	cfg, _ := json.Marshal(map[string]any{"use_last_leveling": true, "leveling": lev, "values": lev.Values, "tol_pct": 2, "max_iter": 10, "settle_ms": 300, "apply_on_select": true})
	os.MkdirAll(appstate.ConfigDir(), 0o755)
	if err := os.WriteFile(filepath.Join(appstate.ConfigDir(), "rgb_light.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	u := newUI(newTheme(), nil)
	s := &shooter{t: t, w: w, u: u, sz: image.Pt(1280, 800), dir: os.Getenv("NSS_SHOTS")}
	if s.dir != "" {
		os.MkdirAll(s.dir, 0o755)
	}
	u.setNav()
	startHDR(u)
	s.pump("start", func() bool { return u.hdr.LogSeq > 0 })

	// Camera card: the leveling switch on (readouts show the leveling
	// base), then off.
	s.save("01-camera-leveling-on.png")
	if u.hdr.BaseR != "0.41234" || !strings.HasPrefix(u.hdr.RefreshLevelingUI().Text, "Last leveling: R 0.41234 G 0.38765 B 0.21111 (after profile, 2026-11-26 21:05:00 CST (UTC+8))") {
		t.Fatalf("leveling readouts %q %q", u.hdr.BaseR, u.hdr.RefreshLevelingUI().Text)
	}
	u.hdr.SetUseLastLeveling(false)
	s.save("02-camera-leveling-off.png")
	if !strings.HasSuffix(u.hdr.RefreshLevelingUI().Text, ". Metadata writing is off.") {
		t.Errorf("off label %q", u.hdr.RefreshLevelingUI().Text)
	}

	k := u.hdr.Cal
	dir := t.TempDir()
	tif, ref := uiChart(t, dir, true)
	k.Open()
	s.save("03-chartcal-empty.png")
	picks := []string{ref, tif}
	u.pick = func(title string, _ pickKind, _ string, exts []string, done func(string, bool)) {
		p := picks[0]
		picks = picks[1:]
		done(p, true)
	}
	k.OpenRef()
	k.OpenChart()
	s.pump("chart loaded", func() bool { return k.Busy == "" && k.Patches != nil })
	s.save("04-chartcal-loaded.png")
	if u.cal.img.Size().X == 0 {
		t.Fatal("no preview image")
	}
	k.Fit()
	s.pump("fit", func() bool { return k.Busy == "" && k.SaveOK })
	s.save("05-chartcal-fit-clipped.png")
	if !strings.HasPrefix(k.ResText, "WARNING: 1 patch(es) clipped") {
		t.Errorf("report %q", k.ResText[:min(80, len(k.ResText))])
	}

	// drag D6 a little: the fit is dropped until Fit is pressed again
	s.frame()
	ir := u.cal.ir
	p := k.ToView(ir, k.Corners[2][0], k.Corners[2][1])
	k.MouseDown(ir, p.X, p.Y, 14)
	k.MouseMove(ir, p.X+12, p.Y+9)
	s.save("06-chartcal-dragging.png")
	k.MouseUp()
	if k.SaveOK {
		t.Error("Save still enabled after a drag")
	}
	k.Detect()
	s.pump("detect", func() bool { return k.Busy == "" })
	k.Fit()
	s.pump("refit", func() bool { return k.Busy == "" && k.SaveOK })
	k.Save()
	s.save("07-chartcal-saved.png")
	if len(k.Profiles) < 2 || k.Profiles[k.ProfIdx] != k.Name {
		t.Errorf("profiles %v %d", k.Profiles, k.ProfIdx)
	}
	u.cal.profCB.open = true
	s.save("08-chartcal-profiles-open.png")
	u.cal.profCB.open = false
	k.Close()
	s.save("09-camera-after-close.png")
	if k.Shown {
		t.Fatal("panel still open")
	}
}
