package focusmark

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"basetool/hikcam"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func TestCentreIsOpticalMidpoint(t *testing.T) {
	cx, cy := Centre(6016, 4096)
	if cx != 3008 || cy != 2048 {
		t.Fatalf("centre = %v,%v", cx, cy)
	}
	cx, cy = Centre(960, 654)
	if cx != 480 || cy != 327 {
		t.Fatalf("sim centre = %v,%v", cx, cy)
	}
}

func TestFitMapsCentreToWidgetCentre(t *testing.T) {
	// 200x100 image letterboxed into a 400x400 widget: dest is 400x200 at y=100.
	v := Fit(0, 0, 400, 400, 200, 100)
	if v.DstX != 0 || v.DstY != 100 || v.DstW != 400 || v.DstH != 200 {
		t.Fatalf("letterbox %+v", v)
	}
	x, y, ok := v.Map(100, 50)
	if !ok || x != 200 || y != 200 {
		t.Fatalf("centre mapped to %v,%v ok=%v", x, y, ok)
	}
	// Top-left 10% inset: (20, 10) -> (40, 120)
	x, y, ok = v.Map(20, 10)
	if !ok || x != 40 || y != 120 {
		t.Fatalf("corner mapped to %v,%v ok=%v", x, y, ok)
	}
}

func TestZoomedCentreStaysAtImageCentre(t *testing.T) {
	// 200x100 image, 2x crop of the right half: src (100,0)-(200,100)
	// drawn into 200x200. Optical centre (100, 50) sits on the left edge.
	v := View{ImgW: 200, ImgH: 100, SrcX: 100, SrcY: 0, SrcW: 100, SrcH: 100, DstX: 0, DstY: 0, DstW: 200, DstH: 200}
	x, y, ok := v.Map(100, 50)
	if !ok || x != 0 || y != 100 {
		t.Fatalf("centre on crop edge: %v,%v ok=%v", x, y, ok)
	}
	// Left-edge marks at x=20 are outside the crop.
	if _, _, ok := v.Map(20, 50); ok {
		t.Fatal("left edge mark should be hidden when panned right")
	}
	// A crop that excludes the centre entirely.
	v = View{ImgW: 200, ImgH: 100, SrcX: 120, SrcY: 0, SrcW: 80, SrcH: 100, DstX: 0, DstY: 0, DstW: 160, DstH: 200}
	if _, _, ok := v.Map(100, 50); ok {
		t.Fatal("centre should be hidden when the crop is off-centre")
	}
}

func TestDrawLeavesCapturePixelsUntouched(t *testing.T) {
	// A mid-grey frame: overlay must change the preview, not a copy used
	// as a stand-in for a saved capture (the capture path never calls Draw).
	src := image.NewGray(image.Rect(0, 0, 200, 100))
	for i := range src.Pix {
		src.Pix[i] = 120
	}
	preview := image.NewRGBA(src.Bounds())
	draw.Draw(preview, preview.Bounds(), src, image.Point{}, draw.Src)
	capture := image.NewRGBA(src.Bounds())
	draw.Draw(capture, capture.Bounds(), src, image.Point{}, draw.Src)

	Draw(preview, Fit(0, 0, 200, 100, 200, 100))

	cx, cy := 100, 50
	if sameColor(preview.RGBAAt(cx, cy-6), color.RGBA{R: 120, G: 120, B: 120, A: 255}) {
		t.Fatal("expected a centre-mark stroke on the preview")
	}
	// The arm above the gap should be white with a dark outline nearby.
	if c := preview.RGBAAt(cx, cy-6); c != Inner {
		t.Fatalf("centre arm %v, want white", c)
	}
	if c := preview.RGBAAt(cx-1, cy-6); c != Outer {
		t.Fatalf("centre outline %v, want black", c)
	}
	// Capture copy must stay mid-grey at every mark location.
	for _, m := range Marks(200, 100) {
		x, y := int(m.IX), int(m.IY)
		if c := capture.RGBAAt(x, y); c.R != 120 || c.G != 120 || c.B != 120 {
			t.Fatalf("capture polluted at %d,%d: %v", x, y, c)
		}
	}
}

func sameColor(a, b color.RGBA) bool { return a == b }

func TestSimPreviewAndCapture(t *testing.T) {
	sim := hikcam.OpenSim()
	defer sim.Close()
	if err := sim.Apply(hikcam.DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := sim.StartGrab(); err != nil {
		t.Fatal(err)
	}
	fr, err := sim.Grab(0)
	if err != nil {
		t.Fatal(err)
	}
	src := image.NewGray(image.Rect(0, 0, fr.W, fr.H))
	for i, v := range fr.Pix {
		src.Pix[i] = byte(v >> 8)
	}

	// Capture stand-in: the 8-bit preview of the grabbed frame, no overlay.
	capture := image.NewRGBA(src.Bounds())
	draw.Draw(capture, capture.Bounds(), src, image.Point{}, draw.Src)
	cx, cy := int(float64(fr.W)/2), int(float64(fr.H)/2)

	fit := Compose(src, 960, 720)
	if fit == nil {
		t.Fatal("compose fit")
	}
	v := Fit(0, 0, 960, 720, fr.W, fr.H)
	mx, my, ok := v.Map(float64(fr.W)/2, float64(fr.H)/2)
	if !ok {
		t.Fatal("centre not visible in fit view")
	}
	px, py := int(mx+0.5), int(my+0.5)
	if fit.RGBAAt(px, py-6) != Inner {
		t.Fatalf("fit centre mark missing at %d,%d: %v", px, py-6, fit.RGBAAt(px, py-6))
	}

	// Zoomed 200% crop of the upper-left 40%, so the optical centre is
	// clearly off-screen and must not be drawn; the top-left corner mark should be.
	cropW, cropH := fr.W*2/5, fr.H*2/5
	crop := image.Rect(0, 0, cropW, cropH)
	zoomed := ComposeCrop(src, fr.W, fr.H, crop, cropW*2, cropH*2)
	zv := View{ImgW: fr.W, ImgH: fr.H, SrcX: 0, SrcY: 0, SrcW: cropW, SrcH: cropH, DstX: 0, DstY: 0, DstW: cropW * 2, DstH: cropH * 2}
	if _, _, ok := zv.Map(float64(fr.W)/2, float64(fr.H)/2); ok {
		t.Fatal("centre should be outside the upper-left crop")
	}
	cornerX, cornerY, ok := zv.Map(Inset*float64(fr.W), Inset*float64(fr.H))
	if !ok {
		t.Fatal("top-left corner mark should be visible in the upper-left crop")
	}
	cpx, cpy := int(cornerX+0.5), int(cornerY+0.5)
	if zoomed.RGBAAt(cpx+3, cpy) != Inner && zoomed.RGBAAt(cpx, cpy+3) != Inner {
		t.Fatalf("corner bracket missing near %d,%d: h=%v v=%v", cpx, cpy, zoomed.RGBAAt(cpx+3, cpy), zoomed.RGBAAt(cpx, cpy+3))
	}

	// The captured frame at the optical centre equals the source grey,
	// not the overlay white/black.
	got := capture.RGBAAt(cx, cy)
	wantG := src.GrayAt(cx, cy).Y
	if got.R != wantG || got.G != wantG || got.B != wantG {
		t.Fatalf("capture centre %v, source grey %d", got, wantG)
	}

	// Two-panel proof image: fit (left) and a 200% crop around the
	// optical centre (right), so the centre mark stays on the true centre
	// after zoom.
	panelW, panelH := 800, 540
	left := Compose(src, panelW, panelH)
	zw, zh := fr.W/2, fr.H/2
	cx0 := fr.W/2 - zw/2
	cy0 := fr.H/2 - zh/2
	right := ComposeCrop(src, fr.W, fr.H, image.Rect(cx0, cy0, cx0+zw, cy0+zh), panelW, panelH)
	// Caption bars.
	sheet := image.NewRGBA(image.Rect(0, 0, panelW*2, panelH+28))
	for i := 0; i < len(sheet.Pix); i += 4 {
		sheet.Pix[i+0], sheet.Pix[i+1], sheet.Pix[i+2], sheet.Pix[i+3] = 18, 18, 20, 255
	}
	draw.Draw(sheet, image.Rect(0, 28, panelW, 28+panelH), left, image.Point{}, draw.Src)
	draw.Draw(sheet, image.Rect(panelW, 28, panelW*2, 28+panelH), right, image.Point{}, draw.Src)
	label := func(s string, x, y int) {
		d := &font.Drawer{Dst: sheet, Src: image.NewUniform(color.RGBA{R: 220, G: 220, B: 226, A: 255}), Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
		d.DrawString(s)
	}
	label("Fit  -  centre, corners and edge midpoints (10% inset)", 10, 18)
	label("200 % at the optical centre  -  mark stays on the true centre", panelW+10, 18)

	out := filepath.Join(t.TempDir(), "mono_center_mark.png")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, sheet); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Also write the raw capture (no overlay) next to it for comparison.
	cf, err := os.Create("/tmp/mono_capture_no_overlay.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(cf, capture); err != nil {
		cf.Close()
		t.Fatal(err)
	}
	cf.Close()
}

func TestMarksCount(t *testing.T) {
	ms := Marks(960, 654)
	if len(ms) != 9 {
		t.Fatalf("got %d marks, want 9 (1 centre + 4 corners + 4 edges)", len(ms))
	}
	if ms[0].Kind != KindCentre {
		t.Fatal("first mark should be the centre")
	}
}
