package develop

import (
	"runtime"
	"sync"
)

// PixFunc transforms one pixel.
type PixFunc func(r, g, b float32) (float32, float32, float32)

func (n Node) f(k string) float32 { return f32(n.Get(k)) }
func (n Node) i(k string) int     { return int(n.Get(k)) }

// Compile turns a DCTL node into its pixel function (nil = identity).
// Grading nodes (4-9) are compiled by Chain.Compile, which knows the
// signal domain.
func (n Node) Compile(cx *Context) PixFunc {
	if n.Bypass {
		return nil
	}
	switch n.Type {
	case TDensity:
		return compileDensity(n, cx)
	case TMask:
		return compileMask(n, cx)
	case TPositive:
		return compilePositive(n, cx)
	}
	return nil
}

// ---- FilmScan_Density_In.dctl transform()

func compileDensity(n Node, cx *Context) PixFunc {
	baseR, baseG, baseB := n.f("base_r"), n.f("base_g"), n.f("base_b")
	if id := n.i("preset"); id >= 1 && id <= len(cx.BasePresets) { // preset_base()
		v := cx.BasePresets[id-1].V
		baseR, baseG, baseB = f32(v[0]), f32(v[1]), f32(v[2])
	}
	inDI, outTrans, cl := n.i("in_di") != 0, n.i("out_trans") != 0, n.i("clamp01") != 0
	dmin, step, full := n.f("dmin_code"), n.f("dens_step"), n.f("full_code")
	blkR, blkG, blkB := n.f("blk_r"), n.f("blk_g"), n.f("blk_b")
	gain := exp2f(n.f("ev_stops"))
	k := -1.0 / (2.302585093 * step * full)
	castOn := n.i("cast_on") != 0
	castR, castG, castB := n.f("cast_r")/(step*full), n.f("cast_g")/(step*full), n.f("cast_b")/(step*full)
	dm := dmin / full
	ur, ug, ub := fmaxf(baseR, 0.000001), fmaxf(baseG, 0.000001), fmaxf(baseB, 0.000001)
	di := func(p float32) float32 {
		if p > 0.02740668 {
			return exp2f(p/0.07329248-7.0) - 0.0075
		}
		return p / 10.44426855
	}
	return func(pR, pG, pB float32) (float32, float32, float32) {
		if inDI {
			pR, pG, pB = di(pR), di(pG), di(pB)
		}
		tr := fmaxf((pR-blkR)*gain/ur, 0.000001)
		tg := fmaxf((pG-blkG)*gain/ug, 0.000001)
		tb := fmaxf((pB-blkB)*gain/ub, 0.000001)
		r := dm + logf(tr)*k
		g := dm + logf(tg)*k
		b := dm + logf(tb)*k
		if castOn {
			r += castR
			g += castG
			b += castB
		}
		if outTrans {
			r, g, b = tr, tg, tb
		}
		if cl {
			r, g, b = clamp01(r), clamp01(g), clamp01(b)
		}
		return r, g, b
	}
}

// ---- FilmScan_MaskCrosstalk.dctl transform()

// MatrixOf returns the matrix the mask node uses (mat_row()).
func MatrixOf(n Node, cx *Context) [9]float32 {
	m := [9]float32{n.f("m00"), n.f("m01"), n.f("m02"), n.f("m10"), n.f("m11"), n.f("m12"), n.f("m20"), n.f("m21"), n.f("m22")}
	if id := n.i("mat_preset"); id >= 1 && id <= len(cx.Matrices) {
		for j, v := range cx.Matrices[id-1].M {
			m[j] = f32(v)
		}
	}
	return m
}

func compileMask(n Node, cx *Context) PixFunc {
	maskOn := n.i("mask_on") != 0
	s := n.f("strength")
	if !maskOn && s == 0 {
		return nil // the DCTL returns the input unchanged
	}
	bdR, bdG, bdB := n.f("bd_r"), n.f("bd_g"), n.f("bd_b")
	dmin, step, full := n.f("dmin_code"), n.f("dens_step"), n.f("full_code")
	cl := n.i("clamp01") != 0
	m := MatrixOf(n, cx)
	k := full * step
	off := dmin * step
	return func(pR, pG, pB float32) (float32, float32, float32) {
		dr := pR*k - off
		dg := pG*k - off
		db := pB*k - off
		if maskOn {
			dr = dr - bdR
			dg = dg - bdG
			db = db - bdB
		}
		nr := (1.0-s)*dr + s*(m[0]*dr+m[1]*dg+m[2]*db)
		ng := (1.0-s)*dg + s*(m[3]*dr+m[4]*dg+m[5]*db)
		nb := (1.0-s)*db + s*(m[6]*dr+m[7]*dg+m[8]*db)
		r := (dmin + nr/step) / full
		g := (dmin + ng/step) / full
		b := (dmin + nb/step) / full
		if cl {
			r, g, b = clamp01(r), clamp01(g), clamp01(b)
		}
		return r, g, b
	}
}

// ---- FilmScan_Positive_Out.dctl transform()

func clampf(v, lo, hi float32) float32 { return fminf(fmaxf(v, lo), hi) }

func compilePositive(n Node, cx *Context) PixFunc {
	outMode, contrast, filmWB := n.i("out_mode"), n.i("contrast"), n.i("film_wb")
	white := n.f("white_code")
	gamR, gamG, gamB := n.f("gam_r"), n.f("gam_g"), n.f("gam_b")
	expo := n.f("expo")
	balR, balG, balB := n.f("bal_r"), n.f("bal_g"), n.f("bal_b")
	dw := (white - 95.0) * 0.002
	var ar, ag, ab float32
	if filmWB == 1 {
		ag = 0.0
		ar = 1.56 + (white-696.0)*0.0013
		ab = -0.20
	}
	grC, ggC, gbC := n.f("gray_r"), n.f("gray_g"), n.f("gray_b")
	useGray := n.i("gray_on") != 0
	if !useGray && filmWB >= 2 {
		gp := [3]float32{470, 470, 470} // wb_gray() default
		if k := filmWB - 2; k < len(cx.WBGrays) {
			v := cx.WBGrays[k].V
			gp = [3]float32{f32(v[0]), f32(v[1]), f32(v[2])}
		}
		grC, ggC, gbC = gp[0], gp[1], gp[2]
		useGray = true
	}
	cR := expf(-2.302585 * dw / gamR)
	cG := expf(-2.302585 * dw / gamG)
	cB := expf(-2.302585 * dw / gamB)
	if useGray {
		sr := (expf(clampf(2.302585*((grC-95.0)*0.002-dw)/gamR, -30.0, 20.0)) - cR) / (1.0 - cR)
		sg := (expf(clampf(2.302585*((ggC-95.0)*0.002-dw)/gamG, -30.0, 20.0)) - cG) / (1.0 - cG)
		sb := (expf(clampf(2.302585*((gbC-95.0)*0.002-dw)/gamB, -30.0, 20.0)) - cB) / (1.0 - cB)
		sr = fmaxf(sr, 0.0001)
		sg = fmaxf(sg, 0.0001)
		sb = fmaxf(sb, 0.0001)
		ar = log2f(sg / sr)
		ab = log2f(sg / sb)
	}
	gR := exp2f(expo + balR + ar)
	gG := exp2f(expo + balG + ag)
	gB := exp2f(expo + balB + ab)
	var cc, kk float32
	if contrast != 0 {
		switch contrast {
		case 1:
			cc, kk = 1.10, 0.70
		case 2:
			cc, kk = 1.25, 0.55
		default:
			cc, kk = 1.40, 0.45
		}
	}
	shoulder := func(y float32) float32 {
		if y > kk {
			return kk + (1.0-kk)*tanhf((y-kk)/(1.0-kk))
		}
		return y
	}
	enc := func(l, r float32) float32 {
		switch outMode {
		case 0:
			if l < 0.018 {
				return 4.5 * l
			}
			return r
		case 1:
			if l > 0.00262409 {
				return (log2f(l+0.0075) + 7.0) * 0.07329248
			}
			return l * 10.44426855
		case 2:
			return fmaxf((685.0+c300*c4343*logf(l*0.98921+0.01079))/1023.0, 0.0)
		case 3:
			if l > 0.0078125 {
				return (log2f(l) + 9.72) / 17.52
			}
			return 10.5402377417*l + 0.0729055342
		}
		return l
	}
	return func(pR, pG, pB float32) (float32, float32, float32) {
		dr := (pR*1023.0 - 95.0) * 0.002
		dg := (pG*1023.0 - 95.0) * 0.002
		db := (pB*1023.0 - 95.0) * 0.002
		lr := gR * (expf(clampf(2.302585*(dr-dw)/gamR, -30.0, 20.0)) - cR) / (1.0 - cR)
		lg := gG * (expf(clampf(2.302585*(dg-dw)/gamG, -30.0, 20.0)) - cG) / (1.0 - cG)
		lb := gB * (expf(clampf(2.302585*(db-dw)/gamB, -30.0, 20.0)) - cB) / (1.0 - cB)
		lr = clampf(lr, 0.0, 64.0)
		lg = clampf(lg, 0.0, 64.0)
		lb = clampf(lb, 0.0, 64.0)
		if contrast != 0 {
			lr = shoulder(0.18 * powf(lr/0.18, cc))
			lg = shoulder(0.18 * powf(lg/0.18, cc))
			lb = shoulder(0.18 * powf(lb/0.18, cc))
		}
		r := 1.099*powf(lr, 0.45) - 0.099
		g := 1.099*powf(lg, 0.45) - 0.099
		b := 1.099*powf(lb, 0.45) - 0.099
		return enc(lr, r), enc(lg, g), enc(lb, b)
	}
}

// float32 literals multiplied at float32 precision, as the DCTL does
var c300, c4343 float32 = 300.0, 0.4342944819

// Program is a compiled chain (up to a node).
type Program struct {
	fs []PixFunc
	// Domain is the encoding of the program output.
	Domain Domain
	// Notes: what the print node loaded, warnings.
	Notes []string
}

// Compile compiles nodes 0..upto (upto < 0 or past the end = whole chain).
func (c Chain) Compile(cx *Context, upto int) Program {
	p := Program{Domain: DomCineon}
	for i, n := range c.Nodes {
		if upto >= 0 && i > upto {
			break
		}
		var f PixFunc
		switch n.Type {
		case TDensity, TMask, TPositive:
			f = n.Compile(cx)
			if n.Type == TPositive && !n.Bypass {
				p.Domain = Domain(clampIdx(n.Get("out_mode"), 5))
			}
		default:
			var note string
			f, p.Domain, note = compileGrade(n, p.Domain)
			if note != "" {
				p.Notes = append(p.Notes, note)
			}
		}
		if f != nil {
			p.fs = append(p.fs, f)
		}
	}
	return p
}

// Identity reports whether the program changes nothing.
func (p Program) Identity() bool { return len(p.fs) == 0 }

// Pixel runs the program on one pixel.
func (p Program) Pixel(r, g, b float32) (float32, float32, float32) {
	for _, f := range p.fs {
		r, g, b = f(r, g, b)
	}
	return r, g, b
}

// Run transforms interleaved RGB in place, in parallel.
func (p Program) Run(px []float32) {
	n := len(px) / 3
	if len(p.fs) == 0 || n == 0 {
		return
	}
	workers := runtime.NumCPU()
	if workers > 16 {
		workers = 16
	}
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		a, b := w*chunk, (w+1)*chunk
		if b > n {
			b = n
		}
		if a >= b {
			break
		}
		wg.Add(1)
		go func(a, b int) {
			defer wg.Done()
			for i := a; i < b; i++ {
				o := px[i*3 : i*3+3 : i*3+3]
				o[0], o[1], o[2] = p.Pixel(o[0], o[1], o[2])
			}
		}(a, b)
	}
	wg.Wait()
}

// To16 converts 0..1 to 16-bit (clamped, rounded).
func To16(v float32) uint16 {
	if !(v > 0) {
		return 0
	}
	if v >= 1 {
		return 65535
	}
	return uint16(v*65535 + 0.5)
}

// RunU16 transforms 16-bit RGB (src) into dst (both 3 samples per pixel),
// in bands so a full frame never needs a float copy of the whole image.
// progress (may be nil) gets the fraction done.
func (p Program) RunU16(src, dst []uint16, progress func(float64)) {
	n := len(src) / 3
	const band = 1 << 18
	buf := make([]float32, band*3)
	for a := 0; a < n; a += band {
		b := a + band
		if b > n {
			b = n
		}
		f := buf[:(b-a)*3]
		for i, v := range src[a*3 : b*3] {
			f[i] = float32(v) / 65535
		}
		p.Run(f)
		for i, v := range f {
			dst[a*3+i] = To16(v)
		}
		if progress != nil {
			progress(float64(b) / float64(n))
		}
	}
}
