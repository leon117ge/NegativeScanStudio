package develop

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// ---- v0.71: film base estimate and Auto film white balance ----

// Base estimate parameters: pixels whose brightest channel is at or above
// EstClip (light leaks, sprocket holes, clipped scan) are excluded; the
// film base is the EstPct percentile of each channel of the rest (the
// clearest film in the frame). Same method as the v0.70 Ektar
// "base_est" render.
const (
	EstClip = 0.9
	EstPct  = 99.9
	// EstMinPix: fewer usable pixels than this gives no estimate.
	EstMinPix = 1000
)

// EstimateBaseU16 estimates the film base (0..1, rounded like the DCTL
// %.5f constants) from interleaved 16-bit RGB. ok=false when too few
// pixels are usable. The percentile is exact on the 16-bit codes
// (lowest code with at least EstPct % of the pixels at or below it).
func EstimateBaseU16(pix []uint16) (base [3]float64, used int, ok bool) {
	clip := uint16(math.Ceil(EstClip * 65535)) // first code with v/65535 >= EstClip
	var hist [3][]uint32
	for c := range hist {
		hist[c] = make([]uint32, 65536)
	}
	for i := 0; i+2 < len(pix); i += 3 {
		r, g, b := pix[i], pix[i+1], pix[i+2]
		if r >= clip || g >= clip || b >= clip {
			continue
		}
		hist[0][r]++
		hist[1][g]++
		hist[2][b]++
		used++
	}
	if used < EstMinPix {
		return base, used, false
	}
	need := uint64(float64(used)*EstPct/100.0 + 0.5)
	for c := 0; c < 3; c++ {
		var acc uint64
		for v, n := range hist[c] {
			acc += uint64(n)
			if acc >= need {
				base[c] = Round(float64(v)/65535.0, 5)
				break
			}
		}
	}
	return base, used, true
}

// KnownStocks are film stock names recognised in file names and preset
// names when the film base DB has no entry (Auto film WB).
var KnownStocks = []string{"Vision3 500T", "Vision3 5219", "Vision3 250D", "Vision3 5207", "Vision3 50D", "Vision3 5203", "Vision3 200T", "Vision3 5213",
	"Ektar 100", "Portra 160", "Portra 400", "Portra 800", "UltraMax 400", "Gold 200", "ColorPlus 200", "Pro Image 100"}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// NormName lower-cases s and drops everything but letters and digits.
func NormName(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// stockAliases: short names in file names (no "Vision3").
var stockAliases = [][2]string{{"5219", "Vision3 5219"}, {"500t", "Vision3 500T"}, {"5207", "Vision3 5207"}, {"250d", "Vision3 250D"},
	{"5213", "Vision3 5213"}, {"200t", "Vision3 200T"}, {"5203", "Vision3 5203"}, {"ektar", "Ektar 100"}, {"ultramax", "UltraMax 400"}}

// FindStock returns the known stock named in s ("" = none; longest wins).
func FindStock(s string) string {
	f := NormName(s)
	best, bl := "", 0
	try := func(key, name string) {
		if n := NormName(key); n != "" && strings.Contains(f, n) && len(n) > bl {
			best, bl = name, len(n)
		}
	}
	for _, k := range KnownStocks {
		try(k, k)
	}
	for _, a := range stockAliases {
		try(a[0], a[1])
	}
	return best
}

// isVision3_500T: the DCTL's built-in "Kodak Vision3 500T" film WB is a
// fixed log2 offset for 5219 (tungsten balanced, orange mask); it is only
// right for that stock.
func isVision3_500T(stock string) bool {
	n := NormName(stock)
	return strings.Contains(n, "500t") || strings.Contains(n, "5219")
}

// AutoGray: AutoFilmWB result "estimate a gray point from the frame".
const AutoGray = -2

// AutoFilmWB returns the film WB choice for a film stock: a film WB gray
// preset (module WB) whose name names the stock -> that film_wb item;
// Vision3 500T / 5219 -> item 1 "Kodak Vision3 500T" (the DCTL default and
// its only built-in film balance); any other known stock -> AutoGray (the
// 500T offset would be wrong, Off leaves the red/blue dye-gamma imbalance:
// the Ektar test frame comes out cyan, mean R 0.05 G 0.17 B 0.22). stock ""
// gives -1: unknown, leave film_wb as it is (the DCTL default).
func AutoFilmWB(stock string, cx *Context) (item int, why string) {
	s := NormName(stock)
	if s == "" {
		return -1, "film stock unknown - film WB left at its current item"
	}
	if cx != nil {
		for k, g := range cx.WBGrays {
			if n := NormName(g.Name); n != "" && (strings.Contains(n, s) || strings.Contains(s, n)) {
				return k + 2, "WB gray preset \"" + g.Name + "\" for " + stock
			}
		}
	}
	if isVision3_500T(stock) {
		return 1, stock + " -> Kodak Vision3 500T"
	}
	return AutoGray, stock + ": no WB preset for this stock -> gray point estimated from the frame"
}

// ApplyAutoFilmWB sets Positive Out's film WB for stock when its
// film_wb_auto is on (pix: the frame, 16-bit RGB, for AutoGray). It only
// writes DCTL parameters (film_wb, gray_on, gray_r/g/b), so the result is
// the DCTL's output with those settings. Returns a log line ("" = nothing
// changed).
func ApplyAutoFilmWB(c *Chain, cx *Context, stock string, pix []uint16) string {
	pi := -1
	for i, n := range c.Nodes {
		if n.Type == TPositive {
			pi = i
		}
	}
	if pi < 0 || c.Nodes[pi].Get("film_wb_auto") == 0 {
		return ""
	}
	n := &c.Nodes[pi]
	k, why := AutoFilmWB(stock, cx)
	switch {
	case k == -1:
		return ""
	case k == AutoGray:
		g, used, ok := EstimateGrayCodes(*c, cx, pix, 7)
		if !ok {
			return fmt.Sprintf("Film WB Auto: %s, but too few usable pixels (%d) - film WB left unchanged", stock, used)
		}
		n.SetCombo(cx, Param{Key: "film_wb", Kind: Combo}, 0)
		n.V["gray_on"] = 1
		n.V["gray_r"], n.V["gray_g"], n.V["gray_b"] = g[0], g[1], g[2]
		return fmt.Sprintf("Film WB Auto: %s: gray point %.1f %.1f %.1f (Use gray point on). Pick neutral for an exact balance.", why, g[0], g[1], g[2])
	default:
		n.SetCombo(cx, Param{Key: "film_wb", Kind: Combo}, k)
		n.V["gray_on"] = 0
		return "Film WB Auto: " + why
	}
}

// EstimateGrayCodes estimates a neutral for the Positive Out gray point
// from the frame: the per-channel median of the density codes (node 2
// output x 1023) of every step-th pixel, ignoring clipped input pixels
// (brightest channel >= EstClip: leaks, holes) and film base (codes
// within 0.05 D of D-min). Gray world: right for an ordinary mixed scene,
// wrong for a frame dominated by one colour (then Pick neutral).
// Codes are rounded to 0.5 like the DCTL slider step.
func EstimateGrayCodes(c Chain, cx *Context, pix []uint16, step int) (gray [3]float64, used int, ok bool) {
	if step < 1 {
		step = 1
	}
	head := c.Clone()
	head.Nodes = head.Nodes[:2]
	p := head.Compile(cx, -1)
	clip := uint16(math.Ceil(EstClip * 65535))
	var vs [3][]float32
	for i := 0; i+2 < len(pix); i += 3 * step {
		if pix[i] >= clip || pix[i+1] >= clip || pix[i+2] >= clip {
			continue
		}
		r, g, b := p.Pixel(float32(pix[i])/65535, float32(pix[i+1])/65535, float32(pix[i+2])/65535)
		cr, cg, cb := r*1023, g*1023, b*1023
		if cr < 120 && cg < 120 && cb < 120 {
			continue
		}
		vs[0], vs[1], vs[2] = append(vs[0], cr), append(vs[1], cg), append(vs[2], cb)
	}
	used = len(vs[0])
	if used < EstMinPix {
		return gray, used, false
	}
	for ch := 0; ch < 3; ch++ {
		sort.Slice(vs[ch], func(a, b int) bool { return vs[ch][a] < vs[ch][b] })
		m := float64(vs[ch][used/2])
		gray[ch] = math.Min(math.Max(math.Round(m*2)/2, 100), 1023)
	}
	return gray, used, true
}
