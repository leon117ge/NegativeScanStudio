package cropper

import (
	"math"
	"math/rand"
)

// SynthSpec describes a synthetic positive scan used by the self-tests.
type SynthSpec struct {
	W, H           int
	Tilt           float64    // degrees, visual clockwise (= expected correction angle)
	FrameW, FrameH float64    // picture size in pixels (before tilt)
	OffX, OffY     float64    // picture centre offset from image centre
	Rebate         [3]float64 // film base colour around the picture
	RebateW        float64    // rebate width beyond the picture (0 = none)
	Holder         [3]float64 // colour outside the rebate
	Sprockets      bool       // holes in the rebate along the long sides
	LightLeak      bool       // orange gradient over the left rebate
	Horizon        bool       // strong straight horizon near the top of the picture
	Bits           int
	Seed           int64
}

// Synth renders s; it also returns a function telling whether a source
// pixel centre lies inside the picture area.
func Synth(s SynthSpec) (*Image, func(x, y float64) float64) {
	rng := rand.New(rand.NewSource(s.Seed))
	type blob struct{ x, y, r, cr, cg, cb float64 }
	var blobs []blob
	for i := 0; i < 40; i++ {
		blobs = append(blobs, blob{rng.Float64()*s.FrameW - s.FrameW/2, rng.Float64()*s.FrameH - s.FrameH/2,
			s.FrameH * (0.03 + 0.12*rng.Float64()), 0.15 + 0.6*rng.Float64(), 0.15 + 0.6*rng.Float64(), 0.15 + 0.6*rng.Float64()})
	}
	phi := s.Tilt * math.Pi / 180
	cs, sn := math.Cos(phi), math.Sin(phi)
	cx, cy := float64(s.W)/2+s.OffX, float64(s.H)/2+s.OffY
	hw, hh := s.FrameW/2, s.FrameH/2
	// frame-local coords of an image point
	local := func(x, y float64) (float64, float64) {
		dx, dy := x-cx, y-cy
		return cs*dx + sn*dy, -sn*dx + cs*dy
	}
	// signed distance inside picture (positive inside)
	inside := func(x, y float64) float64 {
		u, v := local(x, y)
		return math.Min(hw-math.Abs(u), hh-math.Abs(v))
	}
	picture := func(u, v float64) [3]float64 {
		r := 0.35 + 0.1*math.Sin(u/37) + 0.08*math.Cos(v/23)
		g := 0.38 + 0.1*math.Sin((u+v)/51)
		b := 0.33 + 0.1*math.Cos(u/29-v/41)
		for _, bl := range blobs {
			d := math.Hypot(u-bl.x, v-bl.y)
			if d < bl.r {
				r, g, b = bl.cr, bl.cg, bl.cb
			}
		}
		if s.Horizon {
			hz := -hh + 0.17*s.FrameH
			if v < hz {
				t := (v + hh) / (hz + hh)
				r, g, b = 0.55+0.15*t, 0.68+0.1*t, 0.88
			}
		}
		// fine texture
		n := 0.03 * math.Sin(u*1.7+v*0.9) * math.Cos(v*1.3-u*0.4)
		return [3]float64{r + n, g + n, b + n}
	}
	im := &Image{W: s.W, H: s.H, Bits: s.Bits, Format: "tiff", Pix: make([]uint16, s.W*s.H*3)}
	parallelRows(s.H, func(ya, yb int) {
		for y := ya; y < yb; y++ {
			for x := 0; x < s.W; x++ {
				px, py := float64(x)+0.5, float64(y)+0.5
				u, v := local(px, py)
				var col [3]float64
				// layered: holder / rebate / picture, with 1px antialiasing
				dPic := math.Min(hw-math.Abs(u), hh-math.Abs(v))
				dReb := math.Min(hw+s.RebateW-math.Abs(u), hh+s.RebateW-math.Abs(v))
				reb := s.Rebate
				if s.LightLeak && u < -hw {
					t := math.Max(0, 1-math.Abs(v)/(hh*1.2))
					reb = [3]float64{reb[0]*(1-0.5*t) + 1.0*0.5*t, reb[1]*(1-0.5*t) + 0.55*0.5*t, reb[2] * (1 - 0.5*t)}
				}
				if s.Sprockets && s.RebateW > 0 && math.Abs(v) > hh+s.RebateW*0.3 && math.Abs(v) < hh+s.RebateW*0.8 {
					pitch := s.FrameW / 8
					m := math.Mod(u+10*pitch, pitch)
					if m > pitch*0.3 && m < pitch*0.7 {
						reb = s.Holder
					}
				}
				hold := s.Holder
				aR := math.Max(0, math.Min(1, dReb+0.5))
				aP := math.Max(0, math.Min(1, dPic+0.5))
				var pc [3]float64
				if aP > 0 {
					pc = picture(u, v)
				}
				for c := 0; c < 3; c++ {
					base := hold[c]*(1-aR) + reb[c]*aR
					col[c] = base*(1-aP) + pc[c]*aP
				}
				for c := 0; c < 3; c++ {
					n := (rng2(x, y, c) - 0.5) * 0.01
					v := math.Max(0, math.Min(1, col[c]+n))
					q := uint16(v*65535 + 0.5)
					if s.Bits == 8 {
						q = uint16(uint8(v*255+0.5)) * 257
					}
					im.Pix[(y*s.W+x)*3+c] = q
				}
			}
		}
	})
	return im, inside
}

func rng2(x, y, c int) float64 {
	h := uint32(x*73856093) ^ uint32(y*19349663) ^ uint32(c*83492791)
	h ^= h >> 13
	h *= 0x5bd1e995
	h ^= h >> 15
	return float64(h&0xffff) / 65535
}
