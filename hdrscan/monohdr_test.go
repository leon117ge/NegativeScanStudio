package hdrscan

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"basetool/scanmeta"
)

// synthetic Mono12 sensor: pedestal 32 DN (512 in 16-bit units), white
// 4095 DN, full well 9000 e-, read noise 2 e-.
const (
	synBlack = 512.0
	synWhite = 65520.0
	synFW    = 9000.0
	synRead  = 2.0
)

func synFrame(L []float64, w, h, ev int, rng *rand.Rand) MonoFrame {
	head := synWhite - synBlack
	pix := make([]uint16, w*h)
	g := math.Pow(2, float64(ev))
	for i, l := range L {
		e := l * g * synFW
		if e < 0 {
			e = 0
		}
		e += rng.NormFloat64()*math.Sqrt(e) + rng.NormFloat64()*synRead
		v := e/synFW*head + synBlack
		if v > synWhite {
			v = synWhite
		}
		if v < 0 {
			v = 0
		}
		pix[i] = uint16(math.Round(v/16)) * 16
	}
	return MonoFrame{W: w, H: h, Pix: pix, Black16: synBlack, White16: synWhite, ExposureUS: 1000 * g, EV: ev, Frames: 1}
}

// patch scene: 16 x 6 constant patches, log-spaced from 0.002 to 0.95 of
// frame-0 headroom.
func patchScene(w, h int) ([]float64, []float64) {
	levels := make([]float64, 96)
	for i := range levels {
		levels[i] = 0.002 * math.Pow(0.95/0.002, float64(i)/95)
	}
	L := make([]float64, w*h)
	pw, ph := w/16, h/6
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			L[y*w+x] = levels[min(5, y/ph)*16+min(15, x/pw)]
		}
	}
	return L, levels
}

type patchStat struct{ level, mean, std float64 }

func patchStats(p []uint16, w, h int, levels []float64, offset float64) []patchStat {
	pw, ph := w/16, h/6
	var out []patchStat
	for py := 0; py < 6; py++ {
		for px := 0; px < 16; px++ {
			var s, s2, n float64
			for y := py*ph + 4; y < (py+1)*ph-4; y++ {
				for x := px*pw + 4; x < (px+1)*pw-4; x++ {
					v := float64(p[y*w+x]) - offset
					s += v
					s2 += v * v
					n++
				}
			}
			m := s / n
			out = append(out, patchStat{levels[py*16+px] * (synWhite - synBlack), m, math.Sqrt(math.Max(0, s2/n-m*m))})
		}
	}
	return out
}

func TestMergeMonoNativeSingleFrameSubtractsBlack(t *testing.T) {
	f := MonoFrame{W: 4, H: 1, Pix: []uint16{512, 1024, 300, 65520}, Black16: 512}
	got, st, err := MergeMonoNative([]MonoFrame{f}, DefaultMonoNoise())
	if err != nil {
		t.Fatal(err)
	}
	want := []uint16{0, 512, 0, 65008}
	for i := range want {
		if got.Pix[i] != want[i] {
			t.Fatalf("pixel %d = %d, want %d", i, got.Pix[i], want[i])
		}
	}
	if !strings.Contains(st.String(), "single exposure") {
		t.Fatal(st.String())
	}
}

func TestMergeMonoNativeRejectsBadBrackets(t *testing.T) {
	a := MonoFrame{W: 2, H: 2, Pix: make([]uint16, 4), EV: 0}
	b := MonoFrame{W: 2, H: 2, Pix: make([]uint16, 4), EV: 0}
	if _, _, err := MergeMonoNative([]MonoFrame{a, b}, DefaultMonoNoise()); err == nil {
		t.Fatal("equal EVs accepted")
	}
	c := MonoFrame{W: 3, H: 2, Pix: make([]uint16, 6), EV: 2}
	if _, _, err := MergeMonoNative([]MonoFrame{a, c}, DefaultMonoNoise()); err == nil {
		t.Fatal("size mismatch accepted")
	}
}

// The v1.10 merge must be linear (black-subtracted, frame-0 units) across
// the whole bracket and less noisy than the v1.09 grey->RGB->mergeRGB path.
func TestMergeMonoNativeLinearAndLowerNoiseThanLegacy(t *testing.T) {
	const w, h = 768, 384
	L, levels := patchScene(w, h)
	rng := rand.New(rand.NewSource(7))
	evs := []int{0, 2, 3}
	var frames []MonoFrame
	for _, ev := range evs {
		frames = append(frames, synFrame(L, w, h, ev, rng))
	}
	got, st, err := MergeMonoNative(frames, MonoNoise{FullWellE: synFW, ReadE: synRead})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(st.String())
	for k := 1; k < len(evs); k++ {
		if st.RatioFrom[k] != "measured" || math.Abs(st.Ratio[k]/st.Nominal[k]-1) > 0.01 {
			t.Fatalf("frame %d ratio %.4f (%s), nominal %.4f", k, st.Ratio[k], st.RatioFrom[k], st.Nominal[k])
		}
	}
	nat := patchStats(got.Pix, w, h, levels, 0)
	single := patchStats(SubtractBlack(frames[0]).Pix, w, h, levels, 0)

	var legPlanes []MonoPlane
	for _, f := range frames {
		legPlanes = append(legPlanes, MonoPlane{W: w, H: h, Pix: f.Pix})
	}
	o := ProcessOptions{Merge: DefaultOptions(), Software: "test", Metadata: scanmeta.Metadata{Mode: "test", Encoding: "linear"}}
	o.Merge.Align = AlignOff
	leg, err := mergeMonoBracketRGB(legPlanes, evs, o)
	if err != nil {
		t.Fatal(err)
	}
	old := patchStats(leg.Pix, w, h, levels, synBlack)
	// The same legacy path fed black-subtracted frames isolates the
	// weighting itself from the v1.09 pedestal error.
	var subPlanes []MonoPlane
	for _, f := range frames {
		subPlanes = append(subPlanes, SubtractBlack(f))
	}
	leg0, err := mergeMonoBracketRGB(subPlanes, evs, o)
	if err != nil {
		t.Fatal(err)
	}
	old0 := patchStats(leg0.Pix, w, h, levels, 0)
	var r0, l0 float64
	var n0 float64
	for i, s := range nat {
		if s.level < 0.004*(synWhite-synBlack) {
			continue
		}
		l0 = math.Max(l0, math.Abs(old0[i].mean/s.level-1))
		if s.level < 0.2*(synWhite-synBlack) {
			r0 += old0[i].std / s.std
			n0++
		}
	}
	t.Logf("legacy weighting on black-subtracted frames: worst linearity %.3f%%, noise %.2fx native", 100*l0, r0/n0)

	var worstLin, worstOldLin float64
	var sumRatio, nRatio float64
	t.Logf("%9s %9s %8s %8s %8s %8s %8s", "truth", "native", "nat std", "old", "old std", "f0 std", "old/nat")
	for i, s := range nat {
		if s.level < 0.004*(synWhite-synBlack) {
			continue
		}
		lin := math.Abs(s.mean/s.level - 1)
		ol := math.Abs(old[i].mean/s.level - 1)
		worstLin = math.Max(worstLin, lin)
		worstOldLin = math.Max(worstOldLin, ol)
		if s.level < 0.2*(synWhite-synBlack) {
			sumRatio += old[i].std / s.std
			nRatio++
		}
		if i%8 == 0 {
			t.Logf("%9.1f %9.1f %8.2f %8.1f %8.2f %8.2f %8.2f", s.level, s.mean, s.std, old[i].mean, old[i].std, single[i].std, old[i].std/s.std)
		}
		if s.std > single[i].std*1.02+0.5 {
			t.Fatalf("level %.0f: merged std %.2f above single frame %.2f", s.level, s.std, single[i].std)
		}
	}
	t.Logf("worst linearity error: native %.3f%%, legacy (pedestal removed after merge) %.3f%%; mean legacy/native noise below 20%% FS: %.2fx",
		100*worstLin, 100*worstOldLin, sumRatio/nRatio)
	if worstLin > 0.004 {
		t.Fatalf("native merge not linear: worst %.3f%%", 100*worstLin)
	}
	if sumRatio/nRatio < 1.05 {
		t.Fatalf("native merge not less noisy than legacy: %.2fx", sumRatio/nRatio)
	}
}

func TestMergeMonoNativeClipAndGhost(t *testing.T) {
	const w, h = 256, 128
	L := make([]float64, w*h)
	for i := range L {
		L[i] = 0.05
		if i%w >= w/2 {
			L[i] = 0.6 // clips at +2 EV
		}
	}
	rng := rand.New(rand.NewSource(3))
	f0 := synFrame(L, w, h, 0, rng)
	// dust that is only in the +2 EV frame (it moved between exposures)
	L2 := append([]float64(nil), L...)
	for y := 40; y < 60; y++ {
		for x := 40; x < 60; x++ {
			L2[y*w+x] = 0.01
		}
	}
	f2 := synFrame(L2, w, h, 2, rng)
	got, st, err := MergeMonoNative([]MonoFrame{f0, f2}, MonoNoise{FullWellE: synFW, ReadE: synRead})
	if err != nil {
		t.Fatal(err)
	}
	head := synWhite - synBlack
	mean := func(x0, x1, y0, y1 int) float64 {
		var s float64
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				s += float64(got.Pix[y*w+x])
			}
		}
		return s / float64((x1-x0)*(y1-y0))
	}
	if m := mean(44, 56, 44, 56); math.Abs(m/(0.05*head)-1) > 0.03 {
		t.Fatalf("ghost not rejected: %.1f, want %.1f (%s)", m, 0.05*head, st.String())
	}
	if m := mean(w/2+8, w-8, 8, h-8); math.Abs(m/(0.6*head)-1) > 0.005 {
		t.Fatalf("clipped half not from frame 0: %.1f, want %.1f", m, 0.6*head)
	}
	// right next to the clipped half, the +2 EV frame is still used (no dilation)
	if m := mean(w/2-4, w/2-1, 8, h-8); math.Abs(m/(0.05*head)-1) > 0.03 {
		t.Fatalf("edge of clip: %.1f", m)
	}
	if st.Ghost[1] <= 0 {
		t.Fatal("no ghost pixels reported")
	}
}

func TestMergeMonoNativeUsesDarkFrame(t *testing.T) {
	const w, h = 64, 32
	dark := make([]uint16, w*h)
	pix := make([]uint16, w*h)
	for i := range pix {
		dark[i] = uint16(512 + 16*(i%5)) // column fixed pattern
		pix[i] = dark[i] + 8000
	}
	f := MonoFrame{W: w, H: h, Pix: pix, Black16: 512, Dark: dark}
	p := SubtractBlack(f)
	for i, v := range p.Pix {
		if v != 8000 {
			t.Fatalf("pixel %d = %d", i, v)
		}
	}
}

func TestAverageFrames(t *testing.T) {
	a := []uint16{100, 200, 65535}
	b := []uint16{103, 200, 65535}
	got := AverageFrames([][]uint16{a, b})
	if got[0] != 102 || got[1] != 200 || got[2] != 65535 {
		t.Fatalf("%v", got)
	}
}

func TestFlatNormalizeAndApply(t *testing.T) {
	const w, h = 64, 48
	fl := make([]uint16, w*h)
	img := make([]uint16, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := 1.0 - 0.3*math.Abs(float64(x)-w/2)/(w/2) // vignetting
			fl[y*w+x] = uint16(30000 * v)
			img[y*w+x] = uint16(20000 * v)
		}
	}
	g, med, err := NormalizeFlat(MonoPlane{W: w, H: h, Pix: fl})
	if err != nil || med < 25000 {
		t.Fatal(err, med)
	}
	out, err := ApplyFlat(MonoPlane{W: w, H: h, Pix: img}, g)
	if err != nil {
		t.Fatal(err)
	}
	c := float64(out.Pix[h/2*w+w/2])
	e := float64(out.Pix[h/2*w+2])
	if math.Abs(e/c-1) > 0.01 {
		t.Fatalf("flat not removed: centre %.0f edge %.0f", c, e)
	}
	if _, _, err := NormalizeFlat(MonoPlane{W: w, H: h, Pix: make([]uint16, w*h)}); err == nil {
		t.Fatal("dark flat accepted")
	}
}

func TestLevelingImageMonoBlackSubtractsPedestalKeepsRawClip(t *testing.T) {
	const w, h = 64, 48
	pix := make([]uint16, w*h)
	for i := range pix {
		pix[i] = 32768 + 512
	}
	pix[(h/8/2)*w+0] = 65520
	im := LevelingImageMonoBlack(pix, w, h, 512)
	if math.Abs(float64(im.V[1])-0.5) > 1e-4 {
		t.Fatalf("V %v, want 0.5", im.V[1])
	}
	clip := false
	for i, c := range im.Clip {
		if c {
			clip = true
			if im.V[i] >= 1 {
				t.Fatal("clipped value should still be pedestal-subtracted")
			}
		}
	}
	_ = clip
	white := make([]uint16, w*h)
	for i := range white {
		white[i] = 65520
	}
	im = LevelingImageMonoBlack(white, w, h, 512)
	if !im.Clip[0] {
		t.Fatal("raw white code must count as clipped after black subtraction")
	}
}

func moveTexture(w, h int, seed int64) []float64 {
	rng := rand.New(rand.NewSource(seed))
	p := make([]float64, w*h)
	for i := range p {
		p[i] = rng.Float64()
	}
	p = blurSeparable(p, w, h, 2.5)
	for i := range p {
		p[i] = 8000 + 40000*p[i]
	}
	return p
}

func shiftPlane(p []float64, w, h int, dx, dy float64) []float64 {
	out := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v, _ := sampleBilinear(p, w, h, float64(x)-dx, float64(y)-dy)
			out[y*w+x] = v
		}
	}
	return out
}

func TestMeasureMovement(t *testing.T) {
	const w, h = 1200, 800
	g := moveTexture(w, h, 1)
	still := MeasureMovement(shiftPlane(g, w, h, 0.2, -0.1), g, shiftPlane(g, w, h, -0.1, 0.15), w, h)
	if !still.Valid || still.Warn {
		t.Fatalf("still: %s", still.String())
	}
	moved := MeasureMovement(shiftPlane(g, w, h, 3, -2), g, g, w, h)
	if !moved.Valid || !moved.Warn || math.Abs(moved.R[0]-3) > 0.5 || math.Abs(moved.R[1]+2) > 0.5 {
		t.Fatalf("moved: %s", moved.String())
	}
	if !strings.HasPrefix(moved.String(), "WARNING") {
		t.Fatal(moved.String())
	}
}

// ofxAccepts mirrors ofx/src/metadata.hpp parseMetadata (plugin 0.90.6).
func ofxAccepts(desc string) string {
	var j map[string]any
	if err := json.Unmarshal([]byte(desc), &j); err != nil {
		return "Bad NSS JSON"
	}
	str := func(k string) string { s, _ := j[k].(string); return s }
	if str("schema") != "nss.scan/1" {
		return "No NSS info (old scan)"
	}
	if str("encoding") != "linear" || str("processing") != "" {
		return "Not a linear NSS scan"
	}
	if s := str("film_base_space"); s != "" && s != "after_profile" {
		return "Unknown film base coordinate space"
	}
	b, ok := j["film_base"].([]any)
	if !ok || len(b) != 3 {
		return "No film base in file"
	}
	return ""
}

func TestMonoTrichromeMetadataAcceptedByOFX(t *testing.T) {
	const w, h = 640, 448
	g := moveTexture(w, h, 5)
	r := shiftPlane(g, w, h, 0.4, 0)
	b := shiftPlane(g, w, h, -0.4, 0)
	toU := func(p []float64) MonoPlane {
		o := make([]uint16, len(p))
		for i, v := range p {
			o[i] = f64ToU16(v)
		}
		return MonoPlane{W: w, H: h, Pix: o}
	}
	base := [3]float64{0.5, 0.5, 0.5}
	rgb := [3]int{20, 80, 30}
	meta := scanmeta.Metadata{Encoding: "linear", LightRGB: &rgb, FilmBase: &base, FilmBaseSpace: scanmeta.AfterProfile, BaseSource: scanmeta.LevelingSource,
		Mono: &scanmeta.MonoCapture{PixelFormat: "Mono12", BlackLevelDN: 32, BlackSubtracted16: 512, FramesPerExposure: 1, Dark: "off", Flat: "off"}}
	res, err := ProcessMonoTrichrome(toU(r), toU(g), toU(b), MonoTriOptions{
		OutPath: filepath.Join(t.TempDir(), "x_RGB.tiff"), LZW: true, CorrectCA: true, CheckMovement: true,
		Software: "NegativeScan Studio test", Metadata: meta,
	})
	if err != nil {
		t.Fatal(err)
	}
	ti, err := ReadTIFFInfo(res.Out)
	if err != nil {
		t.Fatal(err)
	}
	if why := ofxAccepts(ti.Description); why != "" {
		t.Fatalf("OFX would reject: %s\n%s", why, ti.Description)
	}
	var j map[string]any
	_ = json.Unmarshal([]byte(ti.Description), &j)
	mc, _ := j["mono_capture"].(map[string]any)
	if mc == nil || !strings.Contains(mc["ca_correction"].(string), "CA correction on") || !strings.Contains(mc["movement_check"].(string), "movement check") {
		t.Fatalf("mono_capture missing CA / movement: %s", ti.Description)
	}
	if mc["black_subtracted_16bit"].(float64) != 512 || mc["pixel_format"] != "Mono12" {
		t.Fatalf("black / format: %v", mc)
	}
	for _, r := range ti.Description {
		if r >= 0x2E80 {
			t.Fatalf("non-ASCII CJK in metadata: %q", ti.Description)
		}
	}
	// Optional: the real plugin parser (g++ -std=c++17 ofx/tests/math_runner.cpp).
	if runner := os.Getenv("NSS_OFX_RUNNER"); runner != "" {
		out, err := exec.Command(runner, res.Out).CombinedOutput()
		if err != nil {
			t.Fatalf("OFX math_runner rejected the scan: %v %s", err, out)
		}
		t.Logf("OFX math_runner: %s", strings.TrimSpace(string(out)))
	}
}
