package hdrctl

// Declarations copied verbatim from the Windows Light page and its helpers
// (lightpage.go, levelcam.go, camprofile.go, levelingrecord.go, levelhik.go,
// configpage.go, castpage.go). copied_test.go fails when one of them drifts
// from its original, so the leveling steps, thresholds, sequencing, CSV
// formats and settings files stay the same as on Windows.

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"basetool/camera"
	"basetool/colorsci"
	"basetool/hdrscan"
	"basetool/hikcam"
	"basetool/lightlevel"
	"basetool/rgblight"
	"basetool/scanconfig"
	"basetool/scanmeta"
)

// ---------- lightpage.go ----------

const simLightItem = "Simulated light (test, no hardware)"

// simNoAckItem simulates the field light: protocol 1.4 / FW V1.0.5 executes
// 03 but sends no 81 for it, only the new state as event 83 01 (v0.82,
// confirmed from the v0.81 field log). The pure no-reply case is tested in
// rgblight.
const simNoAckItem = "Simulated light - FW V1.0.5 (03: state event, no 81)"

type lightConfig struct {
	UseLastLeveling bool               `json:"use_last_leveling"`
	Leveling        *scanmeta.Leveling `json:"leveling,omitempty"`
	Port            string             `json:"port"`
	Values          [3]int             `json:"values"`
	ISO             int                `json:"iso"`    // index into isoVals
	Target          int                `json:"target"` // index into targetVals
	Tol             float64            `json:"tol_pct"`
	SolverVersion   int                `json:"solver_version,omitempty"`
	MaxIter         int                `json:"max_iter"`
	SettleMS        int                `json:"settle_ms"`
	Area            lightlevel.Rect    `json:"area"`
	AreaPicked      bool               `json:"area_picked,omitempty"` // only true after an actual drag
	AreaMode        int                `json:"area_mode,omitempty"`   // v0.87: 0 auto, 1 rectangle (same as Camera)
	Stock           string             `json:"stock"`
	AutoLoad        bool               `json:"apply_on_select"`
	NoCam           bool               `json:"no_cam_profile,omitempty"`  // v0.85 "Apply camera profile" unticked
	CamSrc          int                `json:"leveling_camera,omitempty"` // 0 existing, 1 Hikrobot, 2 Hikrobot sim
}

func lightConfigFile() string { return filepath.Join(configDir(), "rgb_light.json") }

func loadLightConfig(isoIdx int) lightConfig {
	c := lightConfig{UseLastLeveling: true, Values: [3]int{50, 50, 50}, ISO: isoIdx, Target: 0, Tol: 2, MaxIter: 10, SettleMS: 300, Area: lightlevel.Centre, AutoLoad: true}
	if b, err := os.ReadFile(lightConfigFile()); err == nil {
		json.Unmarshal(b, &c)
	}
	for i := range c.Values {
		if c.Values[i] < 1 || c.Values[i] > 100 {
			c.Values[i] = 50
		}
	}
	if c.ISO < 0 || c.ISO >= len(isoVals) {
		c.ISO = isoIdx
	}
	if c.Target < 0 || c.Target >= len(targetVals) {
		c.Target = 0
	}
	if c.Tol <= 0 || c.Tol > 20 {
		c.Tol = 2
	}
	if c.SolverVersion == 0 && c.MaxIter == 4 {
		c.MaxIter = 10
	}
	c.SolverVersion = 92
	if c.MaxIter < 1 || c.MaxIter > 10 {
		c.MaxIter = 10
	}
	if c.SettleMS < 0 || c.SettleMS > 5000 {
		c.SettleMS = 300
	}
	if c.Area.Empty() {
		c.Area = lightlevel.Centre
	}
	if c.AreaMode != 1 {
		c.AreaMode = 0
	}
	return c
}

// toScanRect converts a Light-page measure box to the Camera page's
// normalized rectangle (same 0..1 coordinates).
func toScanRect(r lightlevel.Rect) hdrscan.Rect {
	return hdrscan.Rect{X0: r.X0, Y0: r.Y0, X1: r.X1, Y1: r.Y1}
}

func toLightRect(r hdrscan.Rect) lightlevel.Rect {
	return lightlevel.Rect{X0: r.X0, Y0: r.Y0, X1: r.X1, Y1: r.Y1}
}

// setAllBusyOnce sends 03 and confirms with 20. A BUSY answer means the
// light rejected the command, so after waiting for idle it is sent once
// more; any other failure is reported, never resent.
func setAllBusyOnce(c *rgblight.Client, on [3]bool, v [3]int) (rgblight.State, error) {
	st, err := c.SetAllConfirmed(on, v)
	if errors.Is(err, rgblight.ErrBusy) {
		if werr := c.WaitIdle(60 * time.Second); werr != nil {
			return st, fmt.Errorf("light busy: %w", werr)
		}
		st, err = c.SetAllConfirmed(on, v)
	}
	return st, err
}

// busyOnceLight adapts the client for the leveling (same BUSY rule).
type busyOnceLight struct{ c *rgblight.Client }

func (l busyOnceLight) SetAll(on [3]bool, v [3]int) error {
	err := l.c.SetAll(on, v)
	if errors.Is(err, rgblight.ErrBusy) {
		if werr := l.c.WaitIdle(60 * time.Second); werr != nil {
			return fmt.Errorf("light busy: %w", werr)
		}
		err = l.c.SetAll(on, v)
	}
	return err
}

// xt3Camera shoots the leveling frames with the tethered X-T3 at the HDR
// Fixed frame-0 exposure (chosen ISO and base shutter, 1/60 s or 1/125 s).
type xt3Camera struct {
	sess *hdrscan.Session
	iso  int
	dir  string
	exp  camera.Exposure
	last [3]string
}

func (x *xt3Camera) Name() string {
	return fmt.Sprintf("Fujifilm X-T3 (USB tether), ISO %d, %s", x.iso, camera.ShutterLabel(x.sess.FixedExposure(x.iso).Shutter))
}

func (x *xt3Camera) Mono() bool { return false }

func (x *xt3Camera) Prepare() (string, error) {
	e, err := x.sess.SetFixed(x.iso, "RGB light leveling: fixed exposure (same as HDR Fixed frame 0)")
	if err != nil {
		return "", err
	}
	x.exp = e
	return e.String(), nil
}

func (x *xt3Camera) Shoot(ctx context.Context, ch int, prefix string) (*lightlevel.Image, error) {
	p, err := x.sess.ShootFixed(x.exp, x.dir, prefix)
	if err != nil {
		return nil, err
	}
	f, err := hdrscan.Load(p)
	if err != nil {
		return nil, fmt.Errorf("%s is not a readable linear RAW: %w", filepath.Base(p), err)
	}
	defer f.Free()
	im := hdrscan.LevelingImage(f, ch)
	im.Path = p
	if x.last[ch] != "" && x.last[ch] != p {
		os.Remove(x.last[ch]) // keep only the latest frame per channel
	}
	x.last[ch] = p
	return im, nil
}

// valueShot is the brightness of the channel's last capture.
func valueShot(r lightlevel.Result, ch int) int { return r.Values[ch] }

// countFiles counts the regular files directly in dir (v0.83 temp cleanup log).
func countFiles(dir string) int {
	es, _ := os.ReadDir(dir)
	n := 0
	for _, e := range es {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

// ---------- levelcam.go ----------

type camPlan struct {
	on   bool
	name string
	k    float64
	m    colorsci.Mat3
	want float64    // base wanted in the final (post-matrix) TIFF
	pre  [3]float64 // pre-matrix base that gives want after the matrix
}

// camPlanFor builds the plan for target t; apply=false or no enabled
// profile gives a flat plan (pre = t).
func camPlanFor(t float64, apply bool) (camPlan, error) {
	pl := camPlan{want: t, pre: [3]float64{t, t, t}}
	if !apply {
		return pl, nil
	}
	p, k := activeCam()
	if p == nil {
		return pl, nil
	}
	a := p.M.Lerp(k) // ApplyCam(M, k, v) == a·v
	inv, err := a.Inverse()
	if err != nil {
		return pl, fmt.Errorf("camera profile %q cannot be inverted: %w", p.Name, err)
	}
	pre := inv.Apply([3]float64{t, t, t})
	for c, v := range pre {
		if v < 0.02 || v > 0.95 {
			return pl, fmt.Errorf("camera profile %q needs a %c base of %.3f before the matrix - out of range", p.Name, "RGB"[c], v)
		}
	}
	pl.on, pl.name, pl.k, pl.m, pl.pre = true, p.Name, k, p.M, pre
	return pl, nil
}

func (pl camPlan) post(v [3]float64) [3]float64 {
	if !pl.on {
		return v
	}
	return colorsci.ApplyCam(pl.m, pl.k, v)
}

func (pl camPlan) label() string {
	if !pl.on {
		return "no camera profile"
	}
	return fmt.Sprintf("camera profile \"%s\" x%.2f", pl.name, pl.k)
}

// worstDev is the largest relative deviation of v from want.
func (pl camPlan) worstDev(v [3]float64) float64 {
	w := 0.0
	for _, x := range v {
		w = math.Max(w, math.Abs(x/pl.want-1))
	}
	return w
}

// tiffBase runs one RAW per channel through hdrscan.ProcessTrichrome (the
// scan's own converter, merge and clear-base measurement) and returns the
// pre-matrix base. The TIFF is written to dir and removed.
func tiffBase(ctx context.Context, raws [3]string, dir string, box hdrscan.Rect) ([3]float64, int, error) {
	var rs [3][]string
	for c, r := range raws {
		if r == "" {
			return [3]float64{}, 0, errors.New("no leveling RAW for " + "RGB"[c:c+1])
		}
		rs[c] = []string{r}
	}
	res, err := hdrscan.ProcessTrichrome(rs, nil, hdrscan.ProcessOptions{MeasureLevelingBase: true, Ctx: ctx, OutDir: dir, Software: appVersion + " leveling check", BaseNorm: box})
	if err != nil {
		return [3]float64{}, 0, err
	}
	if res.Out != "" {
		os.Remove(res.Out)
	}
	if res.BaseErr != "" {
		return [3]float64{}, 0, errors.New(res.BaseErr)
	}
	return res.Base, res.BaseN, nil
}

func f3(v [3]float64) string { return fmt.Sprintf("R %.4f G %.4f B %.4f", v[0], v[1], v[2]) }

// ---------- camprofile.go ----------

type CamProfile struct {
	Name    string        `json:"name"`
	M       colorsci.Mat3 `json:"matrix"`         // grey-preserving camera -> camera
	Neutral colorsci.Mat3 `json:"neutral_matrix"` // grey -> equal RGB variant (neutral variant)
	Info    string        `json:"info"`
}

type CamSettings struct {
	Profiles []CamProfile `json:"profiles"`
	Active   string       `json:"active"`
	Enabled  bool         `json:"enabled"`
	Strength float64      `json:"strength"`
	LastRef  string       `json:"last_reference"`
}

var (
	camMu  sync.Mutex
	camSet *CamSettings
	// lastTriOut is the newest trichrome TIFF written in this session ("Use last scan").
	lastTriOut string
)

func camPath() string { return filepath.Join(configDir(), "camera_profiles.json") }

// camSettings returns a copy of the stored settings.
func camSettings() CamSettings {
	camMu.Lock()
	defer camMu.Unlock()
	if camSet == nil {
		s := &CamSettings{Strength: 1}
		if b, err := os.ReadFile(camPath()); err == nil {
			json.Unmarshal(b, s)
		}
		if s.Strength < 0 || s.Strength > 1 {
			s.Strength = 1
		}
		camSet = s
	}
	c := *camSet
	c.Profiles = append([]CamProfile(nil), camSet.Profiles...)
	return c
}

func saveCamSettings(s CamSettings) error {
	camMu.Lock()
	defer camMu.Unlock()
	cp := s
	camSet = &cp
	os.MkdirAll(configDir(), 0755)
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(camPath(), b, 0644)
}

func (s CamSettings) find(name string) *CamProfile {
	for i := range s.Profiles {
		if s.Profiles[i].Name == name {
			return &s.Profiles[i]
		}
	}
	return nil
}

// activeCam returns the enabled profile and strength, or nil.
func activeCam() (*CamProfile, float64) {
	s := camSettings()
	if !s.Enabled {
		return nil, 0
	}
	p := s.find(s.Active)
	if p == nil {
		return nil, 0
	}
	return p, s.Strength
}

// ---------- levelingrecord.go ----------

// Base uses linear full-scale units (not preview/JPEG values or light percentages).
// With a camera profile it is AFTER that exact matrix/strength, before Density_In.
type levelingRecord struct {
	Time            time.Time
	Version, Mode   string
	Box             hdrscan.Rect
	Values          [3]int
	Base            [3]float64
	Profile, Source string
	Measured        bool
}

func (r levelingRecord) text() string {
	return fmt.Sprintf("%s\r\nFilm base (linear FS): R %.5f G %.5f B %.5f\r\nRGB Light Air: R %d G %d B %d\r\n%s; %s; %s; box %.6f,%.6f,%.6f,%.6f",
		r.Time.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format(time.RFC3339), r.Base[0], r.Base[1], r.Base[2],
		r.Values[0], r.Values[1], r.Values[2], r.Mode, r.Profile, r.Source, r.Box.X0, r.Box.Y0, r.Box.X1, r.Box.Y1)
}

func appendLevelingLog(path string, r levelingRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.UseCRLF = true
	if st.Size() == 0 {
		if err = w.Write([]string{"time_Asia_Shanghai", "version", "mode", "box_x0", "box_y0", "box_x1", "box_y1", "light_R", "light_G", "light_B", "base_R", "base_G", "base_B", "camera_profile", "source"}); err != nil {
			return err
		}
	}
	row := []string{r.Time.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format(time.RFC3339), r.Version, r.Mode}
	for _, v := range []float64{r.Box.X0, r.Box.Y0, r.Box.X1, r.Box.Y1} {
		row = append(row, strconv.FormatFloat(v, 'f', 6, 64))
	}
	for _, v := range r.Values {
		row = append(row, strconv.Itoa(v))
	}
	for _, v := range r.Base {
		row = append(row, strconv.FormatFloat(v, 'f', 8, 64))
	}
	row = append(row, r.Profile, r.Source)
	if err = w.Write(row); err != nil {
		return err
	}
	w.Flush()
	if err = w.Error(); err != nil {
		return err
	}
	return f.Close()
}

// Failed attempts have a separate schema; existing successful CSVs remain valid.
// Blank fields explicitly mean no frame was captured, never a measured zero.
func appendLevelingFailure(path string, r levelingRecord, result lightlevel.Result, target, tol float64, reason string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.UseCRLF = true
	if st.Size() == 0 {
		if err = w.Write([]string{"time_Asia_Shanghai", "version", "mode", "box_x0", "box_y0", "box_x1", "box_y1", "light_R", "light_G", "light_B", "base_R", "base_G", "base_B", "camera_profile", "reason", "rounds", "target_FS", "relative_tolerance", "final_RGB_captured"}); err != nil {
			return err
		}
	}
	row := []string{r.Time.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format(time.RFC3339), r.Version, r.Mode}
	for _, v := range []float64{r.Box.X0, r.Box.Y0, r.Box.X1, r.Box.Y1} {
		row = append(row, strconv.FormatFloat(v, 'f', 6, 64))
	}
	for c, v := range r.Values {
		value := ""
		if result.Captured[c] {
			value = strconv.Itoa(v)
		}
		row = append(row, value)
	}
	for _, v := range r.Base {
		value := ""
		if result.Captured == [3]bool{true, true, true} {
			value = strconv.FormatFloat(v, 'f', 8, 64)
		}
		row = append(row, value)
	}
	row = append(row, r.Profile, reason, strconv.Itoa(result.Rounds), strconv.FormatFloat(target, 'f', 5, 64), strconv.FormatFloat(tol, 'f', 5, 64), strconv.FormatBool(result.Verified))
	if err = w.Write(row); err != nil {
		return err
	}
	w.Flush()
	if err = w.Error(); err != nil {
		return err
	}
	return f.Close()
}

// Only an accepted measured record can replace the last metadata reference.
func acceptedLeveling(previous *scanmeta.Leveling, r *levelingRecord) *scanmeta.Leveling {
	if r == nil || !r.Measured {
		return previous
	}
	return &scanmeta.Leveling{Base: r.Base, Values: r.Values, At: r.Time.Format(time.RFC3339)}
}

// ---------- levelhik.go ----------

const (
	camSrcExisting = 0
	camSrcHik      = 1
	camSrcHikSim   = 2
)

var camSrcItems = []string{
	"Existing camera (Fujifilm)",
	"Hikrobot monochrome",
	"Simulated Hikrobot MV-CS240-90UM",
}

const camProfileMonoNote = "Apply camera profile: for a Bayer camera this aims at the film base AFTER the CameraCal matrix, so the saved reference can stay at 0.5/0.5/0.5. A monochrome camera has no CFA and no CameraCal matrix, so the option is disabled and each raw film-base channel is leveled independently to the 0.5 target."

const camProfileBayerShort = "Bayer camera: aims at the film base after CameraCal matrix (when Apply camera profile is checked)."

const camProfileMonoShort = "Monochrome camera: no Bayer CFA or CameraCal matrix; each raw channel levels directly to 0.5."

// hikLevelCamera captures leveling frames with a Hikrobot monochrome
// body (or its simulator) at a fixed exposure. No Bayer matrix applies.
// Leveling always uses BestScanQuality (Mono12, gain 0 dB, software
// trigger, full frame) and a single common ExposureUS for R, G and B.
type hikLevelCamera struct {
	dev                    hikcam.Device
	lights                 func() (on [3]bool, v [3]int) // used only when the device is simulated
	last                   [3]hdrscan.MonoPlane
	wantUS                 float64
	appliedUS, appliedGain float64
	simGain                *[3]float64 // tests only; nil uses the default orange-mask gains
	simSize                int         // tests only; 0 = full frame
}

func (h *hikLevelCamera) Name() string {
	if h.dev == nil {
		return "Hikrobot monochrome"
	}
	info := h.dev.Info()
	us, gain := h.GetExposure()
	st := h.dev.Settings()
	kind := "monochrome"
	if info.Simulated {
		kind = "simulated monochrome"
	}
	if us <= 0 {
		us, gain = st.ExposureUS, st.GainDB
	}
	return fmt.Sprintf("%s (%s), %.0f us, %.2f dB, black %.0f, %s (fixed exposure)",
		info.Label(), kind, us, gain, st.BlackLevel, st.PixelFormat)
}

func (h *hikLevelCamera) Mono() bool { return true }

func (h *hikLevelCamera) GetExposure() (float64, float64) {
	if h.appliedUS > 0 {
		return h.appliedUS, h.appliedGain
	}
	if h.wantUS > 0 {
		return h.wantUS, 0
	}
	st := h.dev.Settings()
	return st.ExposureUS, st.GainDB
}

func (h *hikLevelCamera) scanSettings(us float64) hikcam.Settings {
	st := hikcam.BestScanQuality()
	if us > 0 {
		st.ExposureUS = us
	} else if h.wantUS > 0 {
		st.ExposureUS = h.wantUS
	}
	st.GainDB = 0
	if h.simSize > 0 {
		st.FullFrame = false
		st.Width, st.Height = h.simSize, h.simSize
		st.OffsetX, st.OffsetY = 0, 0
	}
	return st.Clamp()
}

func (h *hikLevelCamera) applyScan(us float64) error {
	if sim, ok := h.dev.(*hikcam.Sim); ok {
		sim.FlatField = true
	}
	st := h.scanSettings(us)
	if err := h.dev.Apply(st); err != nil {
		return err
	}
	if err := h.dev.StartGrab(); err != nil {
		return err
	}
	got := h.dev.Settings()
	h.appliedUS, h.appliedGain = got.ExposureUS, got.GainDB
	h.wantUS = h.appliedUS
	return nil
}

func (h *hikLevelCamera) SetExposure(us, _ float64) error {
	return h.applyScan(us)
}

func (h *hikLevelCamera) Prepare() (string, error) {
	// Always start from the intended scan exposure (the UI / Mono Trichrome
	// field), never leftover live-view Mono8 settings.
	if err := h.applyScan(h.wantUS); err != nil {
		return "", err
	}
	got := h.dev.Settings()
	return fmt.Sprintf("%.0f us, %.2f dB, black %.0f, %s", got.ExposureUS, got.GainDB, got.BlackLevel, got.PixelFormat), nil
}

func (h *hikLevelCamera) Shoot(ctx context.Context, ch int, prefix string) (*lightlevel.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timeout := hikcam.GrabTimeout(h.appliedUS)
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}
	fr, err := h.dev.Grab(timeout)
	if err != nil {
		return nil, err
	}
	pix := append([]uint16(nil), fr.Pix...)
	if h.dev.Info().Simulated && h.lights != nil {
		on, v := h.lights()
		gain := [3]float64{1.35, 0.85, 0.55}
		if h.simGain != nil {
			gain = *h.simGain
		}
		pix = hdrscan.ScaleMonoForSimLightBlack(pix, fr.Black16, ch, on, v, gain)
	}
	// v1.10: the BlackLevel pedestal is subtracted here exactly as in the
	// mono-trichrome scan, so the leveled base matches the scan TIFF.
	h.last[ch] = hdrscan.SubtractBlack(hdrscan.MonoFrame{W: fr.W, H: fr.H, Pix: pix, Black16: fr.Black16})
	im := hdrscan.LevelingImageMonoBlack(pix, fr.W, fr.H, fr.Black16)
	im.Path = prefix
	return im, nil
}

func hikTiffBase(ctx context.Context, hk *hikLevelCamera, dir string, box hdrscan.Rect) ([3]float64, int, error) {
	if err := ctx.Err(); err != nil {
		return [3]float64{}, 0, err
	}
	for c, p := range hk.last {
		if p.Pix == nil {
			return [3]float64{}, 0, fmt.Errorf("no last %c mono frame", "RGB"[c])
		}
	}
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("", "nss-hik-tiff-*")
		if err != nil {
			return [3]float64{}, 0, err
		}
		defer os.RemoveAll(dir)
	}
	path := filepath.Join(dir, "leveling_mono_check.tiff")
	r, err := hdrscan.ProcessMonoTrichrome(hk.last[0], hk.last[1], hk.last[2], hdrscan.MonoTriOptions{
		OutPath:             path,
		CorrectCA:           false,
		Software:            appVersion + " leveling check",
		MeasureLevelingBase: true,
		BaseNorm:            box,
		Metadata:            scanmeta.Metadata{Mode: "mono-trichrome", Encoding: "linear"},
	})
	if path != "" {
		_ = os.Remove(path)
	}
	if err != nil {
		return [3]float64{}, 0, err
	}
	if r.BaseErr != "" {
		return r.Base, r.BaseN, errors.New(r.BaseErr)
	}
	return r.Base, r.BaseN, nil
}

func previewGray(fr *hikcam.Frame, maxEdge int) *image.Gray {
	return hikcam.PreviewGray(fr.Pix, fr.W, fr.H, maxEdge)
}

// ---------- configpage.go ----------

const presetNone = "(choose a preset)"

func presetSummary(c *scanconfig.Config) string {
	on := ""
	for i, b := range c.Light.On {
		if b {
			on += string("RGB"[i])
		}
	}
	if on == "" {
		on = "none"
	}
	sh := "keep current"
	if c.ShutterUS != 0 {
		sh = camera.ShutterLabel(c.ShutterUS)
	}
	t := fmt.Sprintf("%s: light R %d G %d B %d (on %s), ISO %d, shutter %s", c.Name, c.Light.Values[0], c.Light.Values[1], c.Light.Values[2], on, c.ISO, sh)
	if c.Film.Stock != "" {
		t += ", film " + c.Film.Stock
	}
	if b := c.Film.Base; b != [3]float64{} {
		t += fmt.Sprintf(", base %.3f / %.3f / %.3f", b[0], b[1], b[2])
	}
	if c.Film.CamProfile != "" {
		t += fmt.Sprintf(", levelled for camera profile \"%s\" x%.2f", c.Film.CamProfile, c.Film.CamStrength)
	}
	if b := c.Film.TIFFBase; b != [3]float64{} {
		t += fmt.Sprintf(", TIFF base %.3f / %.3f / %.3f", b[0], b[1], b[2])
	}
	if c.Film.Notes != "" {
		t += " - " + c.Film.Notes
	}
	if c.Date != "" {
		t += " (" + c.Date + ")"
	}
	return t
}

// ---------- castpage.go ----------

const castModel = "5219 H-1-5219 rev3-26 / Planck visible 10nm / nominal ECN-2 (approximate)"

// ---------- tripage.go ----------

// baseHandoff (v0.82) is a leveling or Apply stored result handed to the HDR
// page: frame 0 is locked at its exposure automatically (no Meter base).
type baseHandoff struct {
	ISO     int
	Shutter uint32
	Values  [3]int // the light values it was made with
	Source  string // "RGB light leveling 12:59:39", "stored Ektar100_250"
	pending bool   // the lock probe has not run yet
}

// ---------- focuspage.go ----------

func liveGrabMessage(err error, misses int) (string, bool) {
	if hikcam.IsNoData(err) {
		if misses == 1 || misses%20 == 0 {
			return fmt.Sprintf("Live: waiting for a frame (GetImageBuffer timeout, x%d)", misses), true
		}
		return "", false
	}
	if misses == 1 || misses%10 == 0 {
		return "Live: " + err.Error(), true
	}
	return "", false
}
