package colorsci

import (
	"errors"
	"math"
	"runtime"
	"sync"
)

// Encoding of the output TIFF.
type Encoding int

const (
	// EncLinear writes linear transmittance in the input scale (today's
	// HDR TIFF). With every other option off the output is bit-identical
	// to the input.
	EncLinear Encoding = iota
	// EncCineon writes Cineon log codes of the (not inverted) negative:
	// 95 + D/0.002 on the 10-bit scale, stored as code/1023 in 16 bits.
	// v0.72: MaskComp is independent of the encoding.
	//   MaskComp off: a pure log negative. D = -log10(v / ref) with one
	//     reference for all channels, ref = the brightest base channel
	//     (CineonRef): that channel's base lands at 95, the others keep the
	//     orange mask above it. Linear = ref * 10^(-(code-95)*0.002).
	//   MaskComp on (what v0.66-v0.71 always did): D is each channel's
	//     density above its own base, so the base is 95 in every channel.
	EncCineon
)

// Options for Apply. The zero value (plus Base) changes nothing.
type Options struct {
	Base     [3]float64 // film base reading, same scale as the input (required)
	Encoding Encoding
	// MaskComp makes the base neutral: in EncLinear every channel is
	// scaled so the base reads max(Base) in all three; in EncCineon the
	// per-channel base density is subtracted (base at code 95 in R, G, B).
	MaskComp bool
	// Crosstalk is the correction matrix applied to density above base;
	// Strength 0 (default) = off, 0.5, 1 = full. Zero Crosstalk = identity.
	Crosstalk Mat3
	Strength  float64
}

// Enabled reports whether Apply would change anything.
func (o Options) Enabled() bool {
	return o.Encoding != EncLinear || o.MaskComp || o.Strength != 0
}

func (o Options) matrix() Mat3 {
	m := o.Crosstalk
	if m == (Mat3{}) {
		m = Identity
	}
	return m.Lerp(o.Strength)
}

// ErrBadBase is returned for a non-positive base.
var ErrBadBase = errors.New("colorsci: film base must be > 0 in all channels")

// Pixel converts one linear RGB reading. For EncLinear the result is a
// linear value in the input scale, for EncCineon a 10-bit code (float).
func (o Options) Pixel(v [3]float64) [3]float64 {
	d := DensityAboveBase(v, o.Base)
	if o.Strength != 0 {
		d = o.matrix().Apply(d)
	}
	switch o.Encoding {
	case EncCineon:
		off := o.cineonOffsets()
		return [3]float64{CineonCode(d[0] + off[0]), CineonCode(d[1] + off[1]), CineonCode(d[2] + off[2])}
	default:
		ref := o.Base
		if o.MaskComp {
			m := math.Max(o.Base[0], math.Max(o.Base[1], o.Base[2]))
			ref = [3]float64{m, m, m}
		}
		return TransmittanceFromDensity(d, ref)
	}
}

// Apply converts an interleaved 16-bit RGB plane (len = 3·w·h) and returns
// a new plane. With !o.Enabled() it returns an exact copy.
func Apply(pix []uint16, o Options) ([]uint16, error) {
	if o.Base[0] <= 0 || o.Base[1] <= 0 || o.Base[2] <= 0 {
		return nil, ErrBadBase
	}
	out := make([]uint16, len(pix))
	if !o.Enabled() {
		copy(out, pix)
		return out, nil
	}
	m := o.matrix()
	useM := o.Strength != 0
	ref := o.Base
	if o.MaskComp {
		mx := math.Max(o.Base[0], math.Max(o.Base[1], o.Base[2]))
		ref = [3]float64{mx, mx, mx}
	}
	off := o.cineonOffsets()
	// Precompute -log10(v/base) for every 16-bit value per channel.
	var lut [3][]float64
	for c := 0; c < 3; c++ {
		lut[c] = make([]float64, 65536)
		for v := 0; v < 65536; v++ {
			t := math.Max(float64(v), 0.5) / o.Base[c] // 0 -> half an LSB
			lut[c][v] = -math.Log10(t)
		}
	}
	n := len(pix) / 3
	workers := runtime.GOMAXPROCS(0)
	var wg sync.WaitGroup
	chunk := (n + workers - 1) / workers
	for w := 0; w < workers; w++ {
		a, b := w*chunk, min((w+1)*chunk, n)
		if a >= b {
			break
		}
		wg.Add(1)
		go func(a, b int) {
			defer wg.Done()
			for i := a; i < b; i++ {
				j := 3 * i
				d := [3]float64{lut[0][pix[j]], lut[1][pix[j+1]], lut[2][pix[j+2]]}
				if useM {
					d = m.Apply(d)
				}
				for c := 0; c < 3; c++ {
					if o.Encoding == EncCineon {
						out[j+c] = CineonTo16(CineonCode(d[c] + off[c]))
					} else if !useM && !o.MaskComp {
						out[j+c] = pix[j+c]
					} else {
						v := math.Round(ref[c] * math.Pow(10, -d[c]))
						out[j+c] = uint16(math.Min(math.Max(v, 0), 65535))
					}
				}
			}
		}(a, b)
	}
	wg.Wait()
	return out, nil
}

// CineonRef is the linear reference (input scale) of a Cineon file without
// mask compensation: the brightest base channel, which is written at code
// 95. Linear = CineonRef * 10^(-(code-95)*0.002) in every channel.
func CineonRef(base [3]float64) float64 {
	return math.Max(base[0], math.Max(base[1], base[2]))
}

// cineonOffsets is the density added to each channel's density above its
// own base before the Cineon code: zero with MaskComp (base at 95 in every
// channel), else log10(ref/base) so the code is the density relative to
// one common reference and the orange mask stays in the file.
func (o Options) cineonOffsets() [3]float64 {
	var off [3]float64
	if o.MaskComp {
		return off
	}
	ref := CineonRef(o.Base)
	for c := 0; c < 3; c++ {
		off[c] = math.Log10(ref / o.Base[c])
	}
	return off
}

// DecodeCineonNegative turns a 16-bit value of a Cineon file written
// without mask compensation back into the linear negative (input scale):
// ref * 10^(-(code-95)*0.002). Codes clipped at 0 or 1023 do not round-trip.
func DecodeCineonNegative(v uint16, ref float64) float64 {
	return ref * math.Pow(10, -CineonDensity(CineonFrom16(v)))
}
