package develop

import "sort"

// Domain is the encoding of the signal between nodes.
type Domain int

const (
	DomRec709  Domain = iota // Positive Out "Rec709" (BT.709 OETF of scene linear)
	DomDI                    // DaVinci Intermediate
	DomCineon                // Cineon film log (also the density code before Positive Out)
	DomACEScct               // ACEScct
	DomLinear                // scene linear
	DomDisp709               // display Rec.709 gamma 2.4 (after the 2383 LUT or CS out)
	DomSRGB                  // display sRGB
)

// DomainNames for the UI.
var DomainNames = []string{"Rec709 (scene)", "DaVinci Intermediate", "Cineon log", "ACEScct", "Linear", "Rec.709 gamma 2.4 display", "sRGB display"}

// Display reports whether d is display-referred.
func (d Domain) Display() bool { return d == DomDisp709 || d == DomSRGB }

// Decode converts a value in d to linear (display linear for display domains).
func Decode(d Domain, v float32) float32 {
	switch d {
	case DomRec709:
		if v < 0.081 {
			return v / 4.5
		}
		return powf((v+0.099)/1.099, 1/0.45)
	case DomDI:
		if v > 0.02740668 {
			return exp2f(v/0.07329248-7.0) - 0.0075
		}
		return v / 10.44426855
	case DomCineon:
		return (expf((v*1023-685)/300*2.302585093) - 0.01079) / 0.98921
	case DomACEScct:
		if v <= 0.155251141552511 {
			return (v - 0.0729055342) / 10.5402377417
		}
		return exp2f(v*17.52 - 9.72)
	case DomDisp709:
		return powf(fmaxf(v, 0), 2.4)
	case DomSRGB:
		if v <= 0.04045 {
			return v / 12.92
		}
		return powf((v+0.055)/1.055, 2.4)
	}
	return v
}

// Encode converts linear to d (the Positive Out encodings for scene domains).
func Encode(d Domain, l float32) float32 {
	switch d {
	case DomRec709:
		if l < 0.018 {
			return 4.5 * l
		}
		return 1.099*powf(l, 0.45) - 0.099
	case DomDI:
		if l > 0.00262409 {
			return (log2f(l+0.0075) + 7.0) * 0.07329248
		}
		return l * 10.44426855
	case DomCineon:
		return fmaxf((685.0+c300*c4343*logf(fmaxf(l*0.98921+0.01079, 1e-10)))/1023.0, 0.0)
	case DomACEScct:
		if l > 0.0078125 {
			return (log2f(l) + 9.72) / 17.52
		}
		return 10.5402377417*l + 0.0729055342
	case DomDisp709:
		return powf(clamp01(l), 1/2.4)
	case DomSRGB:
		l = clamp01(l)
		if l <= 0.0031308 {
			return 12.92 * l
		}
		return 1.055*powf(l, 1/2.4) - 0.055
	}
	return l
}

// ViewFunc maps a signal in d to a Rec.709 gamma 2.4 view (for previews of
// log signals). Display domains pass through.
func ViewFunc(d Domain) PixFunc {
	if d.Display() || d == DomRec709 {
		return nil
	}
	return func(r, g, b float32) (float32, float32, float32) {
		return Encode(DomRec709, Decode(d, r)), Encode(DomRec709, Decode(d, g)), Encode(DomRec709, Decode(d, b))
	}
}

func smoothstep(a, b, x float32) float32 {
	t := clamp01((x - a) / (b - a))
	return t * t * (3 - 2*t)
}

func luma(r, g, b float32) float32 { return 0.2126*r + 0.7152*g + 0.0722*b }

// ZoneWeights returns the weights of the shadows, midtones, highlights and
// specular zones at x stops from 18% grey with edge softness s.
func ZoneWeights(x, s float32) (sh, mid, hi, sp float32) {
	h := s / 2
	sh = 1 - smoothstep(-2.5-h, -2.5+h, x)
	mid = smoothstep(-1.5-h, -1.5+h, x) * (1 - smoothstep(1.5-h, 1.5+h, x))
	hi = smoothstep(2-h, 2+h, x)
	sp = smoothstep(4-h, 4+h, x)
	return
}

// WBGains returns the linear gains of temperature/tint (stops).
func WBGains(t, ti float32) (float32, float32, float32) {
	return exp2f(t/2 + ti/3), exp2f(-2 * ti / 3), exp2f(-t/2 + ti/3)
}

// WBFromNeutral returns temperature and tint that make linear (r,g,b) grey.
func WBFromNeutral(r, g, b float32) (t, ti float32) {
	r, g, b = fmaxf(r, 1e-6), fmaxf(g, 1e-6), fmaxf(b, 1e-6)
	lr, lb := log2f(g/r), log2f(g/b)
	return lr - lb, (lr + lb) / 2
}

// monotone cubic (Fritsch-Carlson) through xs/ys, linear outside.
type spline struct{ x, y, m []float32 }

func newSpline(x, y []float32) spline {
	n := len(x)
	d := make([]float32, n-1)
	for i := range d {
		d[i] = (y[i+1] - y[i]) / (x[i+1] - x[i])
	}
	m := make([]float32, n)
	m[0], m[n-1] = d[0], d[n-2]
	for i := 1; i < n-1; i++ {
		if d[i-1]*d[i] <= 0 {
			m[i] = 0
		} else {
			m[i] = (d[i-1] + d[i]) / 2
		}
	}
	for i := 0; i < n-1; i++ {
		if d[i] == 0 {
			m[i], m[i+1] = 0, 0
			continue
		}
		a, b := m[i]/d[i], m[i+1]/d[i]
		if s := a*a + b*b; s > 9 {
			t := 3 / sqrtf(s)
			m[i], m[i+1] = t*a*d[i], t*b*d[i]
		}
	}
	return spline{x, y, m}
}

func (s spline) at(v float32) float32 {
	n := len(s.x)
	if v <= s.x[0] {
		return s.y[0] + (v-s.x[0])*s.m[0]
	}
	if v >= s.x[n-1] {
		return s.y[n-1] + (v-s.x[n-1])*s.m[n-1]
	}
	i := sort.Search(n, func(k int) bool { return s.x[k] > v }) - 1
	h := s.x[i+1] - s.x[i]
	t := (v - s.x[i]) / h
	t2, t3 := t*t, t*t*t
	return (2*t3-3*t2+1)*s.y[i] + (t3-2*t2+t)*h*s.m[i] + (-2*t3+3*t2)*s.y[i+1] + (t3-t2)*h*s.m[i+1]
}

// CurveKeys are the curve node offsets, at x = 0, 0.2 ... 1.
var CurveKeys = []string{"c_black", "c_shadow", "c_dark", "c_light", "c_high", "c_white"}

// CurveOf returns the curve function of a curves node (nil = straight).
func CurveOf(n Node) func(float32) float32 {
	xs := []float32{0, 0.2, 0.4, 0.6, 0.8, 1}
	ys := make([]float32, 6)
	flat := true
	for i, k := range CurveKeys {
		o := n.f(k)
		if o != 0 {
			flat = false
		}
		ys[i] = xs[i] + o
	}
	if flat {
		return nil
	}
	// keep it monotone non-decreasing
	for i := 1; i < 6; i++ {
		if ys[i] < ys[i-1] {
			ys[i] = ys[i-1]
		}
	}
	return newSpline(xs, ys).at
}

// compileGrade compiles nodes 4-9 for input domain d.
func compileGrade(n Node, d Domain) (PixFunc, Domain, string) {
	if n.Bypass {
		return nil, d, ""
	}
	lin := func(f func(r, g, b float32) (float32, float32, float32)) PixFunc {
		return func(r, g, b float32) (float32, float32, float32) {
			r, g, b = f(Decode(d, r), Decode(d, g), Decode(d, b))
			return Encode(d, r), Encode(d, g), Encode(d, b)
		}
	}
	switch n.Type {
	case TContrast:
		c, p, s := n.f("contrast"), n.f("pivot"), n.f("sat")
		if c == 1 && s == 1 {
			return nil, d, ""
		}
		return func(r, g, b float32) (float32, float32, float32) {
			r, g, b = p+(r-p)*c, p+(g-p)*c, p+(b-p)*c
			if s != 1 {
				l := luma(r, g, b)
				r, g, b = l+(r-l)*s, l+(g-l)*s, l+(b-l)*s
			}
			return r, g, b
		}, d, ""
	case TExposure:
		st := n.f("stops")
		zs, zm, zh, zp, soft := n.f("z_shadow"), n.f("z_mid"), n.f("z_light"), n.f("z_spec"), n.f("falloff")
		zones := zs != 0 || zm != 0 || zh != 0 || zp != 0
		if st == 0 && !zones {
			return nil, d, ""
		}
		g0 := exp2f(st)
		return lin(func(r, g, b float32) (float32, float32, float32) {
			k := g0
			if zones {
				x := log2f(fmaxf(luma(r, g, b), 1e-6) / 0.18)
				a, m, h, p := ZoneWeights(x, soft)
				k = exp2f(st + a*zs + m*zm + h*zh + p*zp)
			}
			return r * k, g * k, b * k
		}), d, ""
	case TWB:
		t, ti := n.f("temp"), n.f("tint")
		if t == 0 && ti == 0 {
			return nil, d, ""
		}
		gr, gg, gb := WBGains(t, ti)
		return lin(func(r, g, b float32) (float32, float32, float32) { return r * gr, g * gg, b * gb }), d, ""
	case TCurves:
		cv := CurveOf(n)
		if cv == nil {
			return nil, d, ""
		}
		return func(r, g, b float32) (float32, float32, float32) { return cv(r), cv(g), cv(b) }, d, ""
	case TPrint:
		l, note, err := LoadForNode(n)
		if err != nil {
			return nil, d, "2383 LUT: " + note + ": " + err.Error()
		}
		tetra := n.i("interp") == 1
		if d.Display() {
			note += " (input was already display-referred)"
		}
		toCin := d != DomCineon
		return func(r, g, b float32) (float32, float32, float32) {
			if toCin {
				r, g, b = Encode(DomCineon, Decode(d, r)), Encode(DomCineon, Decode(d, g)), Encode(DomCineon, Decode(d, b))
			}
			return l.Apply(r, g, b, tetra)
		}, DomDisp709, "2383 LUT: " + note
	case TCSOut:
		out := DomDisp709
		if n.i("space") == 1 {
			out = DomSRGB
		}
		if d == out {
			return nil, d, ""
		}
		return func(r, g, b float32) (float32, float32, float32) {
			return Encode(out, Decode(d, r)), Encode(out, Decode(d, g)), Encode(out, Decode(d, b))
		}, out, ""
	}
	return nil, d, ""
}
