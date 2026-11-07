// dusttest: synthetic dust / hair / star test of hdrscan.Despeckle and
// before/after images (v0.63).
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"

	"basetool/hdrscan"
)

type obj struct {
	kind   string // dust, hair, star
	px     []int  // core pixels (strong effect)
	all    []int  // all affected pixels
	cx, cy int
}

func main() {
	in := flag.String("in", "./testdata/hdrtest/mdup/Portra_400_#3_20261030_190012_0001_HDR.tiff", "negative HDR TIFF")
	out := flag.String("out", "./testdata/dust063", "output dir")
	nd := flag.Int("dust", 250, "dust specks")
	nh := flag.Int("hairs", 4, "hairs")
	ns := flag.Int("stars", 150, "stars (dense points with real signal)")
	flag.Parse()
	os.MkdirAll(*out, 0o755)
	p, err := hdrscan.Load(*in)
	if err != nil {
		panic(err)
	}
	w, h := p.W, p.H
	gw = w
	orig := p.Pix
	base, _ := hdrscan.FindBasePlane(p)
	fmt.Printf("image %dx%d, base rect %v\n", w, h, base)
	protRect := base.Inset(-8)
	protect := func(x, y int) bool { return (image.Point{x, y}).In(protRect) }
	bm := hdrscan.RegionMean(orig, w, base)
	baseSum := bm[0] + bm[1] + bm[2]

	// density floor via a Light run on the original (also the "real image" run)
	rng := rand.New(rand.NewSource(63))
	dusty := append([]uint16(nil), orig...)
	floor := hdrscan.DensityFloor(orig, w, h)
	fmt.Printf("genuine density floor R %.0f G %.0f B %.0f (16-bit)\n", floor[0], floor[1], floor[2])
	sumAt := func(x, y int) float64 {
		i := 3 * (y*w + x)
		return float64(orig[i]) + float64(orig[i+1]) + float64(orig[i+2])
	}
	fs := floor[0] + floor[1] + floor[2]
	used := make([]bool, w*h)
	pick := func(margin int, minBg float64) (int, int, bool) {
		for t := 0; t < 2000; t++ {
			x, y := margin+rng.Intn(w-2*margin), margin+rng.Intn(h-2*margin)
			if (image.Point{x, y}).In(base.Inset(-60)) || used[y*w+x] {
				continue
			}
			// background must be film (not holder) and not near base
			if s := sumAt(x, y); s > minBg*fs && s < 0.7*baseSum {
				return x, y, true
			}
		}
		return 0, 0, false
	}
	var objs []obj
	mark := func(x0, y0, r int) {
		for y := max(0, y0-r); y < min(h, y0+r+1); y++ {
			for x := max(0, x0-r); x < min(w, x0+r+1); x++ {
				used[y*w+x] = true
			}
		}
	}
	// dust: soft opaque disks, transmission 0.01-0.08 at the core
	for k := 0; k < *nd; k++ {
		r := 1.5 + rng.Float64()*6.5
		x0, y0, ok := pick(40, 4)
		if !ok {
			continue
		}
		mark(x0, y0, int(r)+30)
		tr := 0.01 + rng.Float64()*0.07
		o := obj{kind: "dust", cx: x0, cy: y0}
		R := int(r + 2)
		for y := y0 - R; y <= y0+R; y++ {
			for x := x0 - R; x <= x0+R; x++ {
				d := math.Hypot(float64(x-x0), float64(y-y0))
				a := 1 - smooth(r-0.8, r+0.8, d) // 1 inside, 0 outside
				if a <= 0.02 {
					continue
				}
				f := 1 - a*(1-tr)
				i := y*w + x
				for c := 0; c < 3; c++ {
					dusty[3*i+c] = uint16(float64(dusty[3*i+c]) * f)
				}
				o.all = append(o.all, i)
				if a > 0.5 {
					o.px = append(o.px, i)
				}
			}
		}
		objs = append(objs, o)
	}
	// hairs: opaque curved lines 1.5-2.5 px wide, 200-700 px long
	for k := 0; k < *nh; k++ {
		x0, y0, ok := pick(800, 4)
		if !ok {
			continue
		}
		L := 200 + rng.Float64()*500
		wd := 1.5 + rng.Float64()
		ang := rng.Float64() * 2 * math.Pi
		curv := (rng.Float64() - 0.5) * 0.006
		o := obj{kind: "hair", cx: x0, cy: y0}
		seen := map[int]bool{}
		x, y := float64(x0), float64(y0)
		for t := 0.0; t < L; t += 0.5 {
			ang += curv
			x += 0.5 * math.Cos(ang)
			y += 0.5 * math.Sin(ang)
			R := int(wd + 2)
			for yy := int(y) - R; yy <= int(y)+R; yy++ {
				for xx := int(x) - R; xx <= int(x)+R; xx++ {
					if xx < 0 || yy < 0 || xx >= w || yy >= h {
						continue
					}
					d := math.Hypot(float64(xx)-x, float64(yy)-y)
					a := 1 - smooth(wd/2-0.6, wd/2+0.6, d)
					i := yy*w + xx
					if a <= 0.02 || seen[i] {
						continue
					}
					seen[i] = true
					f := 1 - a*(1-0.05)
					for c := 0; c < 3; c++ {
						dusty[3*i+c] = uint16(float64(dusty[3*i+c]) * f)
					}
					o.all = append(o.all, i)
					if a > 0.5 {
						o.px = append(o.px, i)
						used[i] = true
					}
				}
			}
		}
		objs = append(objs, o)
	}
	// stars / specular points: dense spots that keep real signal: the core
	// goes to 1.3-3 x the genuine floor (per channel, tinted), on a
	// background at least 6 x the floor.
	for k := 0; k < *ns; k++ {
		r := 1 + rng.Float64()*3
		x0, y0, ok := pick(40, 3)
		if !ok {
			continue
		}
		mark(x0, y0, int(r)+30)
		o := obj{kind: "star", cx: x0, cy: y0}
		var lv [3]float64
		for c := 0; c < 3; c++ {
			lv[c] = floor[c] * (1.3 + rng.Float64()*1.7)
		}
		R := int(r + 2)
		for y := y0 - R; y <= y0+R; y++ {
			for x := x0 - R; x <= x0+R; x++ {
				d := math.Hypot(float64(x-x0), float64(y-y0))
				a := 1 - smooth(r-0.8, r+0.8, d)
				if a <= 0.02 {
					continue
				}
				i := y*w + x
				for c := 0; c < 3; c++ {
					v := float64(dusty[3*i+c])
					// log blend toward the dense level
					nv := math.Exp2((1-a)*math.Log2(v+1) + a*math.Log2(math.Min(v, lv[c])+1))
					dusty[3*i+c] = uint16(nv)
				}
				o.all = append(o.all, i)
				if a > 0.5 {
					o.px = append(o.px, i)
				}
			}
		}
		objs = append(objs, o)
	}
	cnt := map[string]int{}
	for _, o := range objs {
		cnt[o.kind]++
	}
	fmt.Printf("synthetic: %d dust, %d hairs, %d stars\n", cnt["dust"], cnt["hair"], cnt["star"])

	// "near" mask of synthetic objects (for false positives)
	near := make([]bool, w*h)
	for _, o := range objs {
		for _, i := range o.all {
			x0, y0 := i%w, i/w
			for y := max(0, y0-5); y < min(h, y0+6); y++ {
				for x := max(0, x0-5); x < min(w, x0+6); x++ {
					near[y*w+x] = true
				}
			}
		}
	}

	results := map[int][]uint16{}
	for _, lvl := range []int{hdrscan.DustLight, hdrscan.DustStrong} {
		name := hdrscan.DustName(lvl)
		// real image only
		cr, sr := hdrscan.Despeckle(orig, w, h, lvl, protect, baseSum)
		chReal := 0
		for i := 0; i < w*h; i++ {
			if cr[3*i] != orig[3*i] || cr[3*i+1] != orig[3*i+1] || cr[3*i+2] != orig[3*i+2] {
				chReal++
			}
		}
		fmt.Printf("\n[%s] original image (no synthetic dust): %d spots filled (%d opaque, %d hairs), %d px changed, %d rejected (%d on base), %.2f s\n",
			name, sr.Spots, sr.Opaque, sr.Hairs, chReal, sr.Rejected, sr.Protected, sr.Elapsed.Seconds())
		savePos(filepath.Join(*out, "real_"+name+"_changed_mask.png"), nil, w, h, nil, cr, orig)
		results[-lvl] = cr

		c, s := hdrscan.Despeckle(dusty, w, h, lvl, protect, baseSum)
		var dbgSpots []hdrscan.DustSpot
		if os.Getenv("DBG") != "" {
			_, _, dbgSpots = hdrscan.DespeckleDebug(dusty, w, h, lvl, protect, baseSum)
		}
		results[lvl] = c
		got := map[string]int{}
		var pxGT, pxFixed int
		var depthSum, residSum float64
		var hairFix []string
		starHit := 0
		for _, o := range objs {
			bad := 0.0
			for _, i := range o.px {
				for ch := 0; ch < 3; ch++ {
					switch o.kind {
					case "star": // changed away from the star value?
						bad += math.Abs(math.Log2((float64(c[3*i+ch]) + 1) / (float64(dusty[3*i+ch]) + 1)))
					default: // residual vs the clean original
						bad += math.Abs(math.Log2((float64(c[3*i+ch]) + 1) / (float64(orig[3*i+ch]) + 1)))
					}
				}
			}
			bad /= float64(3 * max(1, len(o.px)))
			if o.kind == "star" {
				if bad > 0.25 {
					starHit++
				}
				continue
			}
			for _, i := range o.px {
				for ch := 0; ch < 3; ch++ {
					depthSum += math.Abs(math.Log2((float64(dusty[3*i+ch]) + 1) / (float64(orig[3*i+ch]) + 1)))
					residSum += math.Abs(math.Log2((float64(c[3*i+ch]) + 1) / (float64(orig[3*i+ch]) + 1)))
				}
			}
			fixed := 0
			for _, i := range o.px {
				e := 0.0
				for ch := 0; ch < 3; ch++ {
					e += math.Abs(math.Log2((float64(c[3*i+ch]) + 1) / (float64(orig[3*i+ch]) + 1)))
				}
				if e/3 < 0.35 {
					fixed++
				}
			}
			pxGT += len(o.px)
			pxFixed += fixed
			if bad < 0.5 {
				got[o.kind]++
			} else if os.Getenv("DBG") != "" && o.kind == "dust" {
				ch := 0
				for _, i := range o.all {
					if c[3*i] != dusty[3*i] {
						ch++
					}
				}
				i := o.cy*w + o.cx
				fmt.Printf("  miss %s at %d,%d core %d all %d changed %d resid %.2f bg %.0f/%.0f/%.0f dusty %d/%d/%d\n", name, o.cx, o.cy, len(o.px), len(o.all), ch, bad,
					float64(orig[3*i]), float64(orig[3*i+1]), float64(orig[3*i+2]), dusty[3*i], dusty[3*i+1], dusty[3*i+2])
				for _, d := range dbgSpots {
					if d.X0-3 <= o.cx && o.cx <= d.X1+3 && d.Y0-3 <= o.cy && o.cy <= d.Y1+3 {
						fmt.Printf("     spot %d,%d-%d,%d px %d peak %.2f out %.2f cd %.2f drop %.2f/%.2f/%.2f len %.1f thick %.1f large %v acc %v why %q\n",
							d.X0, d.Y0, d.X1, d.Y1, d.Px, d.Peak, d.Out, d.CD, d.Drop[0], d.Drop[1], d.Drop[2], d.Len, d.Thick, d.Large, d.Accepted, d.Why)
					}
				}
			}
			if o.kind == "hair" {
				hairFix = append(hairFix, fmt.Sprintf("%d%%@%d,%d", 100*fixed/max(1, len(o.px)), o.px[len(o.px)/2]%w, o.px[len(o.px)/2]/w))
			}
		}
		fp := 0
		for i := 0; i < w*h; i++ {
			if !near[i] && (c[3*i] != dusty[3*i] || c[3*i+1] != dusty[3*i+1] || c[3*i+2] != dusty[3*i+2]) {
				fp++
			}
		}
		// base
		bdiff := 0
		for y := base.Min.Y; y < base.Max.Y; y++ {
			for x := base.Min.X; x < base.Max.X; x++ {
				i := 3 * (y*w + x)
				if c[i] != dusty[i] || c[i+1] != dusty[i+1] || c[i+2] != dusty[i+2] {
					bdiff++
				}
			}
		}
		m0, m1 := hdrscan.RegionMean(dusty, w, base), hdrscan.RegionMean(c, w, base)
		fmt.Printf("[%s] synthetic: dust removed %d/%d (%.1f%%), hairs removed %d/%d (core px fixed per hair %v), dust+hair core px fixed %.1f%%\n",
			name, got["dust"], cnt["dust"], 100*float64(got["dust"])/float64(max(1, cnt["dust"])), got["hair"], cnt["hair"], hairFix, 100*float64(pxFixed)/float64(max(1, pxGT)))
		fmt.Printf("[%s] dust+hair effect removed: %.1f%% (mean core darkening %.2f EV -> residual %.2f EV)\n", name, 100*(1-residSum/depthSum), depthSum/float64(3*max(1, pxGT)), residSum/float64(3*max(1, pxGT)))
		fmt.Printf("[%s] stars altered (>0.25 EV at the core): %d/%d (%.1f%%)\n", name, starHit, cnt["star"], 100*float64(starHit)/float64(max(1, cnt["star"])))
		fmt.Printf("[%s] changed px away from synthetic objects: %d (%.4f%% of the frame; includes real dust on the film)\n", name, fp, 100*float64(fp)/float64(w*h))
		fmt.Printf("[%s] base rect: %d px changed; mean before R %.2f G %.2f B %.2f after R %.2f G %.2f B %.2f\n", name, bdiff, m0[0], m0[1], m0[2], m1[0], m1[1], m1[2])
		fmt.Printf("[%s] spots filled %d (opaque %d, hairs %d), rejected %d (on base %d), px changed %d, time %.2f s\n", name, s.Spots, s.Opaque, s.Hairs, s.Rejected, s.Protected, s.Pixels, s.Elapsed.Seconds())
	}

	// images
	inv := newInverter(orig, w, h, base)
	full := func(pix []uint16, fn string) {
		img := inv.positive(pix, image.Rect(0, 0, w, h), 4)
		writePNG(filepath.Join(*out, fn), img)
	}
	full(dusty, "full_synthetic_before.png")
	full(results[hdrscan.DustLight], "full_synthetic_light.png")
	full(results[hdrscan.DustStrong], "full_synthetic_strong.png")
	// crops: around the first hair, and dense clusters of dust and stars
	var crops []image.Rectangle
	add := func(cx, cy int) {
		r := image.Rect(cx-200, cy-200, cx+200, cy+200)
		if r.Min.X < 0 {
			r = r.Add(image.Pt(-r.Min.X, 0))
		}
		if r.Min.Y < 0 {
			r = r.Add(image.Pt(0, -r.Min.Y))
		}
		if r.Max.X > w {
			r = r.Add(image.Pt(w-r.Max.X, 0))
		}
		if r.Max.Y > h {
			r = r.Add(image.Pt(0, h-r.Max.Y))
		}
		crops = append(crops, r)
	}
	// pick crop centres with the most objects (mixed kinds)
	type cand struct {
		x, y, n int
		kinds   map[string]bool
	}
	var cs []cand
	for _, o := range objs {
		cc := cand{x: o.cx, y: o.cy, kinds: map[string]bool{}}
		for _, q := range objs {
			if abs(q.cx-o.cx) < 180 && abs(q.cy-o.cy) < 180 {
				cc.n++
				cc.kinds[q.kind] = true
			}
		}
		cc.n += 3 * len(cc.kinds)
		cs = append(cs, cc)
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].n > cs[j].n })
	for _, o := range objs {
		if o.kind == "hair" {
			add(o.cx+100, o.cy)
			break
		}
	}
	for _, c := range cs {
		if len(crops) >= 4 {
			break
		}
		dup := false
		for _, r := range crops {
			if (image.Point{c.x, c.y}).In(r.Inset(-150)) {
				dup = true
			}
		}
		if !dup {
			add(c.x, c.y)
		}
	}
	for k, r := range crops {
		a := inv.positive(dusty, r, 1)
		b := inv.positive(results[hdrscan.DustLight], r, 1)
		c := inv.positive(results[hdrscan.DustStrong], r, 1)
		writePNG(filepath.Join(*out, fmt.Sprintf("crop%d_100pct_before_light_strong.png", k+1)), sideBySide(a, b, c))
	}
	// real image: crops where Strong changed most
	real := results[-hdrscan.DustStrong]
	type blk struct{ x, y, n int }
	var bs []blk
	for by := 0; by+400 <= h; by += 200 {
		for bx := 0; bx+400 <= w; bx += 200 {
			n := 0
			for y := by; y < by+400; y += 2 {
				for x := bx; x < bx+400; x += 2 {
					i := 3 * (y*w + x)
					if real[i] != orig[i] {
						n++
					}
				}
			}
			bs = append(bs, blk{bx, by, n})
		}
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].n > bs[j].n })
	var rr []image.Rectangle
	for _, b := range bs {
		if len(rr) >= 3 || b.n == 0 {
			break
		}
		r := image.Rect(b.x, b.y, b.x+400, b.y+400)
		ok := true
		for _, q := range rr {
			if r.Overlaps(q) {
				ok = false
			}
		}
		if ok {
			rr = append(rr, r)
			fmt.Printf("real-image crop %v: %d changed px (Strong, sampled)\n", r, b.n)
		}
	}
	for k, r := range rr {
		a := inv.positive(orig, r, 1)
		b := inv.positive(results[-hdrscan.DustLight], r, 1)
		c := inv.positive(real, r, 1)
		writePNG(filepath.Join(*out, fmt.Sprintf("real%d_100pct_before_light_strong.png", k+1)), sideBySide(a, b, c))
	}
	full(orig, "full_real_before.png")
	full(results[-hdrscan.DustStrong], "full_real_strong.png")
	fmt.Println("crops:", crops)
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func smooth(a, b, x float64) float64 {
	if x <= a {
		return 0
	}
	if x >= b {
		return 1
	}
	t := (x - a) / (b - a)
	return t * t * (3 - 2*t)
}

type inverter struct {
	base   [3]float64
	lo, hi [3]float64
}

func newInverter(pix []uint16, w, h int, base image.Rectangle) *inverter {
	in := &inverter{base: hdrscan.RegionMean(pix, w, base)}
	for c := 0; c < 3; c++ {
		var d []float64
		for y := 0; y < h; y += 7 {
			for x := 0; x < w; x += 7 {
				v := float64(pix[3*(y*w+x)+c])
				s := float64(pix[3*(y*w+x)]) + float64(pix[3*(y*w+x)+1]) + float64(pix[3*(y*w+x)+2])
				if s < 3*1000 { // holder
					continue
				}
				d = append(d, math.Log2(in.base[c]/(v+1)))
			}
		}
		sort.Float64s(d)
		in.lo[c], in.hi[c] = d[len(d)/500], d[len(d)-1-len(d)/500]
	}
	return in
}

// positive renders a crop as a simple inverted positive (scale = downscale).
func (in *inverter) positive(pix []uint16, r image.Rectangle, scale int) *image.RGBA {
	w := 0
	// width is implied by the caller's image: pass via closure-free trick
	w = gw
	ow, oh := r.Dx()/scale, r.Dy()/scale
	img := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < oh; y++ {
		for x := 0; x < ow; x++ {
			var acc [3]float64
			for yy := 0; yy < scale; yy++ {
				for xx := 0; xx < scale; xx++ {
					i := 3 * ((r.Min.Y+y*scale+yy)*w + r.Min.X + x*scale + xx)
					for c := 0; c < 3; c++ {
						acc[c] += float64(pix[i+c])
					}
				}
			}
			var o [3]uint8
			for c := 0; c < 3; c++ {
				v := acc[c] / float64(scale*scale)
				d := (math.Log2(in.base[c]/(v+1)) - in.lo[c]) / (in.hi[c] - in.lo[c])
				d = math.Max(0, math.Min(1, d))
				d = math.Pow(d, 1.15)
				o[c] = uint8(255*d + 0.5)
			}
			img.Set(x, y, color.RGBA{o[0], o[1], o[2], 255})
		}
	}
	return img
}

var gw int

func sideBySide(ims ...*image.RGBA) *image.RGBA {
	gap := 8
	b := ims[0].Bounds()
	out := image.NewRGBA(image.Rect(0, 0, len(ims)*b.Dx()+(len(ims)-1)*gap, b.Dy()))
	draw.Draw(out, out.Bounds(), &image.Uniform{color.RGBA{40, 40, 44, 255}}, image.Point{}, draw.Src)
	for k, im := range ims {
		draw.Draw(out, b.Add(image.Pt(k*(b.Dx()+gap), 0)), im, image.Point{}, draw.Src)
	}
	return out
}

func writePNG(fn string, img image.Image) {
	f, err := os.Create(fn)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	png.Encode(f, img)
}

// savePos writes a mask of changed pixels (white on black, downscaled 4x).
func savePos(fn string, _ []uint16, w, h int, _ any, a, b []uint16) {
	ow, oh := w/4, h/4
	img := image.NewGray(image.Rect(0, 0, ow, oh))
	for i := 0; i < w*h; i++ {
		if a[3*i] != b[3*i] || a[3*i+1] != b[3*i+1] || a[3*i+2] != b[3*i+2] {
			x, y := (i%w)/4, (i/w)/4
			if x < ow && y < oh {
				img.SetGray(x, y, color.Gray{255})
			}
		}
	}
	writePNG(fn, img)
}

func init() { gw = 0 }
