package hdrctl

// "Chart calibration" (chartcal.go on Windows, v0.84): camera + light input
// profile from a 24-patch transmission chart. The Windows dialog is a modal
// walk.Dialog; on macOS it is an in-window panel drawn by chartcalview.go.
// The logic below follows the Windows methods one by one; loading, grid
// detection, patch sampling and the fit run on goroutines and come back
// through Host.Post, so the window keeps drawing.

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"basetool/colorsci"
	"basetool/hdrscan"
)

// calNow is time.Now (tests pin it for the profile info line).
var calNow = time.Now

// ChartCal is the Chart calibration panel. All fields belong to the UI
// goroutine.
type ChartCal struct {
	c *Ctl

	Shown    bool   // the panel is open
	FileText string // fileLbl
	FileOK   bool   // a chart is loaded (label not grey)
	RefText  string // refLbl
	RefOK    bool
	ResText  string // resTE (Windows line ends kept; the view drops \r)
	ResSeq   int
	Name     string // nameLE
	NameSeq  int    // bumped when Name is set by the page
	SaveOK   bool   // saveBtn enabled
	Busy     string // non-empty while a background job runs (what it does)

	Profiles []string // profCB model: "(none)", then the profile names
	ProfIdx  int
	Enabled  bool
	Strength float64
	StrSeq   int // bumped when Strength is set by the page

	Preview    *image.NRGBA // display copy of the chart (nil = none)
	PreviewSeq int
	Corners    colorsci.ChartCorners
	Patches    *[24]colorsci.CamPatch // clip status of the overlay

	path       string
	pix        []uint16
	w, h       int
	drag       int
	ref        *colorsci.CamRef
	refPath    string
	fit        *colorsci.CamFit
	sample     int // generation of the latest patch sampling
	sampledGen int // generation Patches belongs to
	job        int // generation of the latest load / fit
}

func newChartCal(c *Ctl) *ChartCal { return &ChartCal{c: c, drag: -1} }

func (k *ChartCal) host() Host { return k.c.Host }

func (k *ChartCal) setRes(s string) { k.ResText = s; k.ResSeq++ }

// Open is calibrateChart: a fresh dialog, the profile settings and the last
// reference.
func (k *ChartCal) Open() {
	*k = ChartCal{c: k.c, drag: -1, Shown: true, ResSeq: k.ResSeq + 1, NameSeq: k.NameSeq + 1, StrSeq: k.StrSeq,
		sample: k.sample + 1, job: k.job + 1, PreviewSeq: k.PreviewSeq + 1}
	k.FileText = "No chart loaded."
	k.RefText = "No reference loaded."
	k.ResText = "Open a chart TIFF and load its reference, then press Fit."
	k.Name = "XT3 trichrome " + calNow().Format("2006-01-02")
	k.loadSettingsUI()
	if s := camSettings(); s.LastRef != "" {
		k.loadRef(s.LastRef, true)
	}
}

// Close is the Close button (Cancel). A running job's result is dropped.
func (k *ChartCal) Close() {
	k.Shown = false
	k.job++
	k.sample++
	k.pix, k.Preview, k.Patches = nil, nil, nil
}

// ---------- profile settings ----------

func (k *ChartCal) loadSettingsUI() {
	s := camSettings()
	names := []string{"(none)"}
	cur := 0
	for i, p := range s.Profiles {
		names = append(names, p.Name)
		if p.Name == s.Active {
			cur = i + 1
		}
	}
	k.Profiles, k.ProfIdx = names, cur
	k.Enabled = s.Enabled
	k.Strength = s.Strength
	k.StrSeq++
}

// SettingsChanged is settingsChanged: the profile combo, Enable or Strength
// changed.
func (k *ChartCal) SettingsChanged() {
	s := camSettings()
	s.Active = ""
	if i := k.ProfIdx; i > 0 && i <= len(s.Profiles) {
		s.Active = s.Profiles[i-1].Name
	}
	s.Enabled = k.Enabled
	s.Strength = math.Min(math.Max(k.Strength, 0), 1)
	saveCamSettings(s)
	if p, st := activeCam(); p != nil {
		k.c.say(fmt.Sprintf("Camera profile \"%s\" enabled at strength %.2f: applied to the trichrome film-base measurement.", p.Name, st))
	} else {
		k.c.say("Camera profile: off.")
	}
}

// SetStrength is the Strength NumberEdit (Decimals 2, 0..1) after editing.
func (k *ChartCal) SetStrength(text string) {
	v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		k.StrSeq++ // show the stored value again
		return
	}
	v = math.Round(math.Min(math.Max(v, 0), 1)*100) / 100
	k.Strength = v
	k.StrSeq++
	k.SettingsChanged()
}

// DeleteProfile is deleteProfile.
func (k *ChartCal) DeleteProfile() {
	s := camSettings()
	i := k.ProfIdx
	if i <= 0 || i > len(s.Profiles) {
		return
	}
	name := s.Profiles[i-1].Name
	k.host().Confirm("Delete profile", "Delete camera profile \""+name+"\"?", func(ok bool) {
		if !ok {
			return
		}
		s := camSettings()
		i := -1
		for j, p := range s.Profiles {
			if p.Name == name {
				i = j + 1
			}
		}
		if i < 0 {
			return
		}
		s.Profiles = append(s.Profiles[:i-1], s.Profiles[i:]...)
		if s.Active == name {
			s.Active, s.Enabled = "", false
		}
		saveCamSettings(s)
		k.loadSettingsUI()
	})
}

// ---------- inputs ----------

// OpenChart is open: the native open panel for the chart TIFF.
func (k *ChartCal) OpenChart() {
	k.host().PickFile("16-bit TIFF of the 24-patch transmission chart", false, "", []string{"tif", "tiff"}, func(p string, ok bool) {
		if ok && k.Shown {
			k.Load(p)
		}
	})
}

// UseLast is useLast.
func (k *ChartCal) UseLast() {
	if lastTriOut == "" {
		k.host().Message("Use last scan", "No trichrome scan has been written in this session yet.")
		return
	}
	k.Load(lastTriOut)
}

// OpenRef is openRef: the native open panel for the CGATS reference.
func (k *ChartCal) OpenRef() {
	start := ""
	if s := camSettings(); s.LastRef != "" {
		start = filepath.Dir(s.LastRef)
	}
	k.host().PickFile("Chart reference data (IT8.7/1 CGATS, XYZ D50)", false, start, []string{"txt", "it8", "cie", "cgats"}, func(p string, ok bool) {
		if ok && k.Shown {
			k.loadRef(p, false)
		}
	})
}

// loadRef parses a small text file; it runs on the UI goroutine as on
// Windows.
func (k *ChartCal) loadRef(path string, quiet bool) {
	f, err := os.Open(path)
	if err == nil {
		var r colorsci.CamRef
		r, err = colorsci.ParseCGATS(f)
		f.Close()
		if err == nil {
			k.ref, k.refPath = &r, path
			txt := filepath.Base(path) + "  (24 patches"
			if r.Illum != "" {
				txt += ", " + r.Illum
				if !strings.EqualFold(r.Illum, "D50") {
					txt += " - WARNING: treated as D50"
				}
			}
			k.RefText, k.RefOK = txt+")", true
			s := camSettings()
			s.LastRef = path
			saveCamSettings(s)
			k.changed()
			return
		}
	}
	if quiet {
		k.RefText, k.RefOK = "Last reference "+filepath.Base(path)+" not loaded: "+err.Error(), false
		return
	}
	k.host().Message("Reference", err.Error())
}

// Load is load: read the TIFF, build the preview, detect the grid and
// sample the patches, on a goroutine.
func (k *ChartCal) Load(path string) {
	k.job++
	gen := k.job
	k.Busy = "Loading " + filepath.Base(path) + " ..."
	go func() {
		pl, err := hdrscan.Load(path)
		if err != nil {
			k.host().Post(func() {
				if gen != k.job {
					return
				}
				k.Busy = ""
				k.host().Message("Open chart", err.Error())
			})
			return
		}
		pix, w, h := append([]uint16(nil), pl.Pix...), pl.W, pl.H
		pl.Free()
		prev := chartPreview(pix, w, h)
		c, orient, derr := colorsci.DetectCamChart(pix, w, h)
		k.host().Post(func() {
			if gen != k.job {
				return
			}
			k.Busy = ""
			k.path, k.pix, k.w, k.h = path, pix, w, h
			k.Preview = prev
			k.PreviewSeq++
			k.FileText, k.FileOK = fmt.Sprintf("%s  (%d x %d)", filepath.Base(path), k.w, k.h), true
			k.applyDetect(c, orient, derr)
			k.changed()
		})
	}()
}

// Detect is the Auto-detect button (detect, then changed).
func (k *ChartCal) Detect() {
	if k.pix == nil {
		k.changed()
		return
	}
	k.job++
	gen := k.job
	pix, w, h := k.pix, k.w, k.h
	k.Busy = "Detecting the grid ..."
	go func() {
		c, orient, err := colorsci.DetectCamChart(pix, w, h)
		k.host().Post(func() {
			if gen != k.job {
				return
			}
			k.Busy = ""
			k.applyDetect(c, orient, err)
			k.changed()
		})
	}()
}

func (k *ChartCal) applyDetect(c colorsci.ChartCorners, orient string, err error) {
	if err != nil {
		k.resetCorners()
		k.setRes("Auto-detect failed (" + err.Error() + "). Drag the four corner markers onto A1, A6, D6, D1.")
		return
	}
	k.Corners = c
	k.setRes("Grid found automatically (chart " + orient + "). Check the labels, then press Fit.")
}

// Rotate is "Rotate 180°": swap the A1 and D6 ends.
func (k *ChartCal) Rotate() {
	c := k.Corners
	k.Corners = colorsci.ChartCorners{c[2], c[3], c[0], c[1]}
	k.changed()
}

// ResetGrid is "Reset grid".
func (k *ChartCal) ResetGrid() { k.resetCorners(); k.changed() }

// chartPreview is makePreview: the linear scan shown with a display gamma,
// levelled to the 99.5th percentile (the image part of the Windows code;
// Windows then makes a walk.Bitmap of it).
func chartPreview(pix []uint16, w, h int) *image.NRGBA {
	s := max(1, int(math.Ceil(math.Max(float64(w)/1100, float64(h)/700))))
	pw, ph := w/s, h/s
	var hist []float64
	for y := 0; y < ph; y += 3 {
		for x := 0; x < pw; x += 3 {
			i := ((y*s+s/2)*w + x*s + s/2) * 3
			hist = append(hist, math.Max(float64(pix[i]), math.Max(float64(pix[i+1]), float64(pix[i+2]))))
		}
	}
	sort.Float64s(hist)
	hi := math.Max(hist[len(hist)*995/1000], 1)
	img := image.NewNRGBA(image.Rect(0, 0, pw, ph))
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			i := ((y*s+s/2)*w + x*s + s/2) * 3
			var px [3]uint8
			for c := 0; c < 3; c++ {
				t := math.Min(float64(pix[i+c])/hi, 1)
				px[c] = uint8(255*math.Pow(t, 1/2.2) + 0.5)
			}
			img.SetNRGBA(x, y, color.NRGBA{px[0], px[1], px[2], 255})
		}
	}
	return img
}

func (k *ChartCal) resetCorners() {
	if k.w == 0 {
		return
	}
	w, h := float64(k.w), float64(k.h)
	cw := math.Min(w*0.8, h*0.8*1.5)
	ch := cw / 1.5
	x0, y0 := (w-cw)/2+cw/12, (h-ch)/2+ch/8
	x1, y1 := (w+cw)/2-cw/12, (h+ch)/2-ch/8
	k.Corners = colorsci.ChartCorners{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}

// changed re-samples the patches (clip status for the overlay, on a
// goroutine) and drops the fit.
func (k *ChartCal) changed() {
	k.fit = nil
	k.SaveOK = false
	k.sample++
	if k.pix == nil {
		return
	}
	gen, pix, w, h, c := k.sample, k.pix, k.w, k.h, k.Corners
	go func() {
		ps := colorsci.SampleCamChart(pix, w, h, c, 0.25)
		k.host().Post(func() {
			if gen == k.sample {
				k.Patches, k.sampledGen = &ps, gen
			}
		})
	}()
}

// ---------- corner markers (mouse) ----------

// ImageRect is imageRect: where the chart is drawn inside a view of size
// vw x vh (aspect kept, centred).
func (k *ChartCal) ImageRect(vw, vh int) image.Rectangle {
	if k.Preview == nil || k.w == 0 {
		return image.Rect(0, 0, vw, vh)
	}
	sc := math.Min(float64(vw)/float64(k.w), float64(vh)/float64(k.h))
	iw, ih := int(float64(k.w)*sc), int(float64(k.h)*sc)
	x, y := (vw-iw)/2, (vh-ih)/2
	return image.Rect(x, y, x+iw, y+ih)
}

// ToView maps image coordinates into the view (toView).
func (k *ChartCal) ToView(ir image.Rectangle, x, y float64) image.Point {
	return image.Pt(ir.Min.X+int(x/float64(k.w)*float64(ir.Dx())), ir.Min.Y+int(y/float64(k.h)*float64(ir.Dy())))
}

func (k *ChartCal) toImage(ir image.Rectangle, px, py int) (float64, float64) {
	x := float64(px-ir.Min.X) / float64(max(1, ir.Dx())) * float64(k.w)
	y := float64(py-ir.Min.Y) / float64(max(1, ir.Dy())) * float64(k.h)
	return math.Min(math.Max(x, 0), float64(k.w-1)), math.Min(math.Max(y, 0), float64(k.h-1))
}

// MouseDown picks the corner marker within tol view pixels (14 on Windows).
func (k *ChartCal) MouseDown(ir image.Rectangle, x, y int, tol float64) {
	if k.Preview == nil {
		return
	}
	best, bd := -1, tol*tol
	for i, cn := range k.Corners {
		p := k.ToView(ir, cn[0], cn[1])
		if d := float64((p.X-x)*(p.X-x) + (p.Y-y)*(p.Y-y)); d < bd {
			best, bd = i, d
		}
	}
	k.drag = best
}

// MouseMove drags the picked marker.
func (k *ChartCal) MouseMove(ir image.Rectangle, x, y int) {
	if k.drag < 0 {
		return
	}
	ix, iy := k.toImage(ir, x, y)
	k.Corners[k.drag] = [2]float64{ix, iy}
	k.fit = nil
}

// MouseUp ends a drag and re-samples.
func (k *ChartCal) MouseUp() {
	if k.drag >= 0 {
		k.drag = -1
		k.changed()
	}
}

// ImageSize is the loaded chart's size in pixels (0, 0 = none).
func (k *ChartCal) ImageSize() (int, int) { return k.w, k.h }

// Dragging reports a corner drag in progress.
func (k *ChartCal) Dragging() bool { return k.drag >= 0 }

// ---------- fit and outputs ----------

// Fit is solve: sample and fit on a goroutine, then the report.
func (k *ChartCal) Fit() {
	if k.pix == nil || k.ref == nil {
		k.host().Message("Fit", "Open a chart TIFF and load its reference first.")
		return
	}
	k.changed()
	k.job++
	gen, pix, w, h, c, ref := k.job, k.pix, k.w, k.h, k.Corners, *k.ref
	k.Busy = "Fitting ..."
	go func() {
		ps := colorsci.SampleCamChart(pix, w, h, c, 0.25)
		f, text := calReport(ps, ref)
		k.host().Post(func() {
			if gen != k.job {
				return
			}
			k.Busy = ""
			k.sample++ // this sampling is the current one
			k.Patches, k.sampledGen = &ps, k.sample
			k.setRes(text)
			if f == nil {
				return
			}
			k.fit = f
			k.SaveOK = true
		})
	}()
}

// calReport is the fit and report text of solve (nil fit = could not fit).
func calReport(ps [24]colorsci.CamPatch, ref colorsci.CamRef) (*colorsci.CamFit, string) {
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

func (k *ChartCal) info() string {
	f := k.fit
	return fmt.Sprintf("chart %s, ref %s, %s, %d patches, ΔE2000 mean %.2f max %.2f (before %.2f / %.2f)",
		filepath.Base(k.path), filepath.Base(k.refPath), calNow().Format("2006-01-02 15:04"), f.Used, f.MeanA, f.MaxA, f.MeanB, f.MaxB)
}

// SetName is the Profile name LineEdit.
func (k *ChartCal) SetName(s string) { k.Name = s }

// Save is save: "Save as camera profile".
func (k *ChartCal) Save() {
	if k.fit == nil {
		return
	}
	name, ok := cleanName(k.Name)
	if !ok {
		k.host().Message("Save", "Enter a profile name.")
		return
	}
	p := CamProfile{Name: name, M: k.fit.Mx, Neutral: k.fit.Neutral, Info: k.info()}
	store := func() {
		s := camSettings()
		if e := s.find(name); e != nil {
			*e = p
		} else {
			s.Profiles = append(s.Profiles, p)
		}
		s.Active = name
		if err := saveCamSettings(s); err != nil {
			k.host().Message("Save", err.Error())
			return
		}
		k.loadSettingsUI()
		k.c.say("Camera profile \"" + name + "\" saved (" + p.Info + "). Tick Enable to apply it to the trichrome film-base measurement.")
		k.setRes("Saved camera profile \"" + name + "\" and made it the active profile (Enable is " + map[bool]string{true: "on", false: "off"}[s.Enabled] + ").\r\n" + k.ResText)
	}
	if camSettings().find(name) != nil {
		k.host().Confirm("Save", "Replace camera profile \""+name+"\"?", func(ok bool) {
			if ok {
				store()
			}
		})
		return
	}
	store()
}
