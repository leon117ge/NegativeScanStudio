package cropctl

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"basetool/cropper"
)

// testHost runs posted functions when the test pumps them, like the Gio
// window goroutine does between frames.
type testHost struct {
	q        chan func()
	mu       sync.Mutex
	messages []string
	files    []string
	folder   string
}

func newHost() *testHost { return &testHost{q: make(chan func(), 4096)} }

func (h *testHost) Post(f func()) { h.q <- f }
func (h *testHost) Message(title, text string) {
	h.mu.Lock()
	h.messages = append(h.messages, title+": "+text)
	h.mu.Unlock()
}
func (h *testHost) PickFiles(title, start string, exts []string, done func([]string, bool)) {
	done(h.files, len(h.files) > 0)
}
func (h *testHost) PickFolder(title, start string, done func(string, bool)) {
	done(h.folder, h.folder != "")
}

func (h *testHost) pump(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for !ok() {
		select {
		case f := <-h.q:
			f()
		case <-time.After(10 * time.Millisecond):
		}
		if time.Now().After(end) {
			t.Fatalf("timeout waiting for %s", what)
		}
	}
}

// drain runs whatever is queued now.
func (h *testHost) drain() {
	for {
		select {
		case f := <-h.q:
			f()
		default:
			return
		}
	}
}

func isolate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

var (
	white = [3]float64{0.94, 0.92, 0.88}
	black = [3]float64{0.03, 0.03, 0.035}
)

// scan writes a synthetic positive scan (cropper's self-test generator).
func scan(t *testing.T, dir, name string, s cropper.SynthSpec) string {
	t.Helper()
	im, _ := cropper.Synth(s)
	im.Format = cropper.FormatOf(name)
	p := filepath.Join(dir, name)
	if err := cropper.Save(p, im, cropper.SaveOptions{LZW: true, JPEGQuality: 95}); err != nil {
		t.Fatal(err)
	}
	return p
}

func land(seed int64, tilt float64, bits int) cropper.SynthSpec {
	return cropper.SynthSpec{W: 900, H: 600, Tilt: tilt, FrameW: 792, FrameH: 528, Rebate: white, RebateW: 20, Holder: black, Bits: bits, Seed: seed}
}

func (u *CropUI) allReady() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, it := range u.items {
		if !it.analysed() && it.State != ciError {
			return false
		}
	}
	return len(u.items) > 0
}

func TestCropAddFilesFilterAndDetect(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	a := scan(t, d, "a.tif", land(1, 0.7, 16))
	b := scan(t, d, "b.png", land(2, -1.8, 8))
	cropped := filepath.Join(d, "a_crop.tif")
	os.WriteFile(cropped, []byte("x"), 0644)
	txt := filepath.Join(d, "notes.txt")
	os.WriteFile(txt, []byte("x"), 0644)
	h := newHost()
	u := NewCrop(h)
	if n := u.AddFiles([]string{a, b, cropped, txt, a}); n != 2 {
		t.Fatalf("added %d, want 2 (no _crop outputs, no unsupported, no duplicates)", n)
	}
	if u.CountLbl != "2 file(s)" || u.Current() != 0 {
		t.Fatalf("count %q current %d", u.CountLbl, u.Current())
	}
	h.pump(t, "detection", 30*time.Second, u.allReady)
	h.drain()
	for i, want := range []float64{0.7, -1.8} {
		u.mu.Lock()
		it := u.items[i]
		ang, ok := it.Angle, it.Det.OK
		u.mu.Unlock()
		if !ok || math.Abs(ang-want) > 0.06 {
			t.Fatalf("item %d angle %.3f ok %v, want %.2f", i, ang, ok, want)
		}
		if u.Cell(i, 4) != "Ready" || !strings.HasPrefix(u.Cell(i, 3), "Auto ") || !strings.HasSuffix(u.Cell(i, 1), "°") {
			t.Fatalf("row %d: %q %q %q", i, u.Cell(i, 1), u.Cell(i, 3), u.Cell(i, 4))
		}
	}
	if !strings.Contains(u.DetLbl, "Detected") && u.DetLbl == "No image selected." {
		t.Fatalf("details %q", u.DetLbl)
	}
	// AddFilesDialog goes through the native panel
	c := scan(t, d, "c.jpg", land(3, 0.3, 8))
	h.files = []string{c}
	u.AddFilesDialog()
	h.drain()
	if u.Rows() != 3 {
		t.Fatalf("rows %d after Add files", u.Rows())
	}
	// Add folder picks up the folder's images, skipping the ones already there
	h.folder = d
	u.AddFolderDialog()
	h.drain()
	if u.Rows() != 3 {
		t.Fatalf("rows %d after Add folder", u.Rows())
	}
	u.ClickRow(1, false, false)
	u.RemoveSelected()
	if u.Rows() != 2 || u.Cell(1, 0) != "c.jpg" {
		t.Fatalf("after remove: %d rows, row 1 %q", u.Rows(), u.Cell(1, 0))
	}
	u.Clear()
	if u.Rows() != 0 || u.Current() != -1 {
		t.Fatal("clear")
	}
}

func TestCropAngleAndManualEdits(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	a := scan(t, d, "a.tif", land(4, 1.2, 16))
	h := newHost()
	u := NewCrop(h)
	u.AddFiles([]string{a})
	h.pump(t, "detection", 30*time.Second, u.allReady)
	h.drain()
	u.SetAngle(7)
	if ang, _, ok, _ := u.SliderState(); !ok || ang != 5 {
		t.Fatalf("angle clamp: %v", ang)
	}
	u.AngleEdited("-0.333")
	if ang, _, _, _ := u.SliderState(); math.Abs(ang+0.35) > 1e-9 {
		t.Fatalf("angle step 0.05: %v", ang)
	}
	if u.Cell(0, 3) != "Manual" {
		t.Fatalf("method %q after an edit", u.Cell(0, 3))
	}
	u.Nudge(0.05)
	if ang, _, _, _ := u.SliderState(); math.Abs(ang+0.30) > 1e-9 {
		t.Fatalf("nudge: %v", ang)
	}
	u.ResetToAuto()
	if ang, det, _, _ := u.SliderState(); ang != det || u.Cell(0, 3) == "Manual" {
		t.Fatalf("reset: %v vs %v %q", ang, det, u.Cell(0, 3))
	}
	u.FullImage()
	if ang, _, _, _ := u.SliderState(); ang != 0 || u.Cell(0, 2) != "900×600" {
		t.Fatalf("full image: %v %q", ang, u.Cell(0, 2))
	}
	// aspect 4:5 (portrait of a landscape frame keeps the frame orientation)
	u.ResetToAuto()
	for i, n := range AspectNames {
		if n == "1:1" {
			u.SetAspect(i)
		}
	}
	var w, hh int
	if _, err := fmtScan(u.Cell(0, 2), &w, &hh); err != nil || w != hh {
		t.Fatalf("1:1 gives %q", u.Cell(0, 2))
	}
	// settings persist
	u.SetInterp(1)
	u.SetLZW(0)
	if c := loadCropConfig(); c.Interp != 1 || c.LZW || cropAspectNames[c.Aspect] != "1:1" {
		t.Fatalf("saved %+v", c)
	}
}

// TestCropDrag moves and resizes the frame with the mouse; it must stay
// inside the rotated image.
func TestCropDrag(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	a := scan(t, d, "a.tif", land(5, 1.0, 16))
	h := newHost()
	u := NewCrop(h)
	u.AddFiles([]string{a})
	h.pump(t, "detection", 30*time.Second, u.allReady)
	h.pump(t, "proxy", 5*time.Second, func() bool { return u.Preview().Proxy != nil })
	g := u.Geom(0, 0, 800, 560, 8)
	if !g.OK() {
		t.Fatal("geometry")
	}
	r0 := u.Preview().Rect
	cx, cy := g.ToView((r0.X0+r0.X1)/2, (r0.Y0+r0.Y1)/2)
	if c := u.HitCursor(g, int(cx), int(cy), 7); c != CursorMove {
		t.Fatalf("cursor %d in the middle", c)
	}
	u.MouseDown(g, int(cx), int(cy), 7)
	u.MouseMove(g, int(cx)-40, int(cy))
	u.MouseUp()
	r1 := u.Preview().Rect
	if r1.X0 >= r0.X0 || math.Abs(r1.W()-r0.W()) > 1e-6 {
		t.Fatalf("move: %+v -> %+v", r0, r1)
	}
	hp := cropper.Constraints(900, 600, u.Preview().Angle, nil, 0)
	if !cropper.Fits(hp, r1) {
		t.Fatal("moved frame left the image")
	}
	// drag the bottom-right corner far outside: clamps inside the image
	bx, by := g.ToView(r1.X1, r1.Y1)
	u.MouseDown(g, int(bx), int(by), 7)
	u.MouseMove(g, int(bx)+400, int(by)+400)
	u.MouseUp()
	r2 := u.Preview().Rect
	if !cropper.Fits(hp, r2) || r2.W() < r1.W() {
		t.Fatalf("corner: %+v -> %+v", r1, r2)
	}
	if u.Cell(0, 3) != "Manual" {
		t.Fatalf("method %q after a drag", u.Cell(0, 3))
	}
}

func fmtScan(s string, w, h *int) (int, error) {
	parts := strings.Split(s, "×")
	if len(parts) != 2 {
		return 0, os.ErrInvalid
	}
	var err error
	*w, err = atoi(parts[0])
	if err != nil {
		return 0, err
	}
	*h, err = atoi(parts[1])
	return 2, err
}

func atoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// windowsCrop is the Windows code path for one file: analyse (Load,
// MakeProxy, Detect, resetLocked with the drift-checked Windows methods)
// and export (Load, Apply, Save with the Windows options).
func windowsCrop(t *testing.T, cfg CropConfig, src, out string, edit func(w *CropUI, it *cropItem)) {
	t.Helper()
	w := &CropUI{cfg: cfg}
	im, err := cropper.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	p := cropper.MakeProxy(im, cropper.ProxySide)
	det := cropper.Detect(p)
	it := &cropItem{Path: src, W: im.W, H: im.H, Bits: im.Bits, Scale: p.Scale, Det: det}
	w.resetLocked(it, cfg.OnImport == 1)
	it.State = ciReady
	if edit != nil {
		w.items, w.cur = []*cropItem{it}, 0
		edit(w, it)
	}
	im, err = cropper.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	res := cropper.Apply(im, it.Angle, it.Rect, cfg.Interp)
	if err := cropper.Save(out, res, cropper.SaveOptions{LZW: cfg.LZW, JPEGQuality: cropJPEGQ[cfg.JPEGQ], Software: appVersion}); err != nil {
		t.Fatal(err)
	}
}

// TestCropGoldenMatchesWindows exports with the macOS page and with the
// Windows code path and compares the files byte for byte.
func TestCropGoldenMatchesWindows(t *testing.T) {
	isolate(t)
	type tc struct {
		name   string
		spec   cropper.SynthSpec
		cfg    func(c *CropConfig)
		angle  float64 // 0 = keep detection
		manual bool
	}
	cases := []tc{
		{"tilt.tif", land(11, 0.7, 16), nil, 0, false},
		{"tilt_lanczos_plain.tif", land(12, -1.8, 16), func(c *CropConfig) { c.Interp, c.LZW = 1, false }, 0, false},
		{"port.png", cropper.SynthSpec{W: 600, H: 900, Tilt: 0.9, FrameW: 528, FrameH: 792, Rebate: black, Holder: black, Bits: 8, Seed: 13}, nil, 0, false},
		{"q.jpg", land(14, 0.4, 8), func(c *CropConfig) { c.JPEGQ = 2 }, 0, false},
		{"edited.tif", land(15, 1.1, 16), func(c *CropConfig) { c.Aspect, c.Inset = 3, 2 }, -0.65, false},
		{"manual.tif", land(16, 0.5, 16), func(c *CropConfig) { c.OnImport = 1 }, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mac, win := t.TempDir(), t.TempDir()
			srcM := scan(t, mac, c.name, c.spec)
			srcW := filepath.Join(win, c.name)
			b, _ := os.ReadFile(srcM)
			os.WriteFile(srcW, b, 0644)
			cfg := CropConfig{LZW: true, Edges: true}
			if c.cfg != nil {
				c.cfg(&cfg)
			}
			saveCropConfig(cfg)
			h := newHost()
			u := NewCrop(h)
			if u.Cfg() != cfg {
				t.Fatalf("config %+v != %+v", u.Cfg(), cfg)
			}
			u.AddFiles([]string{srcM})
			h.pump(t, "detection", 30*time.Second, u.allReady)
			h.drain()
			if c.angle != 0 {
				u.AngleEdited(fmtF(c.angle))
			}
			u.Export(false)
			h.pump(t, "export", 60*time.Second, func() bool { return !u.Exporting() })
			if u.Cell(0, 4) != "Exported "+cropName(c.name) {
				t.Fatalf("status %q", u.Cell(0, 4))
			}
			outW := filepath.Join(win, cropName(c.name))
			windowsCrop(t, cfg, srcW, outW, func(w *CropUI, it *cropItem) {
				if c.angle != 0 {
					w.setAngleNoUI(c.angle)
				}
			})
			gm, err := os.ReadFile(filepath.Join(mac, "cropped", cropName(c.name)))
			if err != nil {
				t.Fatal(err)
			}
			gw, _ := os.ReadFile(outW)
			if len(gm) < 1000 || !bytes.Equal(gm, gw) {
				t.Fatalf("%s: macOS output (%d bytes) differs from the Windows path (%d bytes)", c.name, len(gm), len(gw))
			}
		})
	}
}

func fmtF(v float64) string { return strings.TrimRight(strings.TrimRight(strconvF(v), "0"), ".") }

func cropName(n string) string {
	ext := filepath.Ext(n)
	return strings.TrimSuffix(n, ext) + "_crop" + ext
}

// setAngleNoUI is the state part of the Windows setAngle (the copied method
// also refreshes the table and labels, which need no window here).
func (u *CropUI) setAngleNoUI(v float64) {
	it := u.current()
	v = math.Round(v/0.05) * 0.05
	it.Angle, it.Manual = v, true
	if !it.RectEdited {
		it.Rect = u.autoRect(it)
	}
}

func TestCropExportNamingAndOutFolder(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	a := scan(t, d, "a.tif", land(21, 0.6, 16))
	h := newHost()
	u := NewCrop(h)
	u.AddFiles([]string{a})
	h.pump(t, "detection", 30*time.Second, u.allReady)
	for _, want := range []string{"a_crop.tif", "a_crop2.tif"} {
		u.Export(true)
		h.pump(t, "export", 60*time.Second, func() bool { return !u.Exporting() })
		if _, err := os.Stat(filepath.Join(d, "cropped", want)); err != nil {
			t.Fatalf("%s: %v (existing files are never overwritten)", want, err)
		}
	}
	// custom folder without a folder: message, nothing exported
	u.SetOutMode(1)
	u.Export(true)
	if u.Exporting() || len(h.messages) != 1 || !strings.Contains(h.messages[0], "Choose the output folder first") {
		t.Fatalf("messages %q", h.messages)
	}
	out := filepath.Join(d, "out")
	h.folder = out
	u.ChooseOutDir(func(string) {})
	h.drain()
	if u.Cfg().OutDir != out {
		t.Fatalf("out dir %q", u.Cfg().OutDir)
	}
	u.Export(true)
	h.pump(t, "export", 60*time.Second, func() bool { return !u.Exporting() })
	if _, err := os.Stat(filepath.Join(out, "a_crop.tif")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u.StatusLbl, "Export finished: 1 written, 0 failed.") || u.PBVal != 1 || u.PBMax != 1 {
		t.Fatalf("status %q %d/%d", u.StatusLbl, u.PBVal, u.PBMax)
	}
	if !strings.Contains(u.LogText, "Export 1 image(s): TIFF LZW, bicubic.") {
		t.Fatalf("log %q", u.LogText)
	}
}

// TestCropBadFile shows the error in the table and skips it on export.
func TestCropBadFile(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	bad := filepath.Join(d, "bad.tif")
	os.WriteFile(bad, []byte("not a tiff"), 0644)
	h := newHost()
	u := NewCrop(h)
	u.AddFiles([]string{bad})
	h.pump(t, "error", 10*time.Second, u.allReady)
	h.drain()
	if !strings.HasPrefix(u.Cell(0, 4), "ERROR: ") || u.CellStyle(0, 4) != CellError {
		t.Fatalf("status %q style %d", u.Cell(0, 4), u.CellStyle(0, 4))
	}
	u.Export(true)
	if u.Exporting() || u.StatusLbl != "Nothing to export. Add images first." {
		t.Fatalf("status %q", u.StatusLbl)
	}
}

func strconvF(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
