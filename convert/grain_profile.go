package convert

// GrainProfile is calibrated from a real Vision3 500T trichrome scan
// (500T_2026_11_2_..._0212_RGB_HDR.tiff, 6246x4170, ~173.5 px/mm).
// Other ISO presets are scaled from that ISO 400 / 500T-class measurement.
type GrainProfile struct {
	// ChanLoad is the per-channel weight on a shared fluctuation.
	// Pairwise correlation is approximately load[i]*load[j]
	// (not the load itself — a common factor model).
	ChanLoad [3]float64
	// ChanScale multiplies each channel's zero-mean grain plane (G ≈ 1).
	ChanScale [3]float64
	// ChanSize multiplies each channel's dye-cloud sigma (G ≈ 1).
	ChanSize [3]float64
	// DensRef is the per-channel density at which ChanScale was
	// calibrated. The grain plane is multiplied by sqrt(DensRef[c]/D)
	// so RMS follows the scan's nearly flat granularity-vs-density
	// instead of raw Poisson (which grows as sqrt(D)).
	DensRef [3]float64
	Amp     float64 // overall density-quantum scale
	Size    float64 // overall cloud-size scale
	Note    string
}

// ScanFramePx is the long-edge pixel count of the calibration scan,
// treated as a 36 mm frame. Full-resolution X-T3 RAF files are ~6240 px
// wide, so export-time grain matches this physical scale.
const ScanFramePx = 6246

// profile500T matches HF density grain on visually flat mid-density
// patches of the 0212 scan (box high-pass radius 15 px, same metric as
// MeasureDensityGrain). Whole-crop figures (RMS 0.028/0.044/0.073,
// corr 0.72/0.38/0.25) were inflated by scene edges (shoe, bars);
// flat peach/wall patches give RMS ≈ 0.015/0.022/0.055, corr ≈ 0.20/0.05/0.06,
// ac50 ≈ 2.0 px, with B much stronger than G — that is the grain colour.
var profile500T = GrainProfile{
	ChanLoad:  [3]float64{0.80, 0.90, 0.42},
	ChanScale: [3]float64{3.13, 3.16, 8.80},
	ChanSize:  [3]float64{1.00, 0.95, 1.45},
	DensRef:   [3]float64{0.189, 0.383, 0.442},
	Amp:       3.1,
	Size:      0.78,
	Note:      "ISO400 matched to grain_real crop_mid 500T stats (v1.02-preview5)",
}

// iso100FromISO400 documents how the ISO 100 preset is derived from the
// measured ISO 400 / 500T-class profile.
//
// RMS amplitude: Kodak still-colour Print Grain Index at 8x10 from 35 mm
// is 38 for Ektar 100 (E-4046) and 59 for Portra 400 (E-4050). Kodak
// defines 4 PGI units as one just-noticeable difference; ~6 % RMS per
// JND is the usual conversion, so
//
//	RMS_100 / RMS_400 = 1.06^((38-59)/4) ≈ 0.736
//
// Amp is a density quantum (unblurred RMS ∝ sqrt(Amp)), so Amp is
// multiplied by 0.736² ≈ 0.54.
//
// Linear dye-cloud size follows the area-speed model (ISO ∝ grain
// area, size ∝ sqrt(ISO)): sqrt(100/400) = 0.50.
//
// Modern T-grain B&W (T-Max 100 RMS 8 vs T-Max 400 RMS 10) is a milder
// 0.80 ratio; we follow the colour-negative PGI pair because Convert's
// ISO 400 target is itself a colour-negative (Vision3 500T-class).
const (
	iso100AmpScale  = 0.54 // 0.736² from PGI-derived RMS ratio
	iso100SizeScale = 0.50 // sqrt(100/400)
)

func grainProfile(stock string, iso GrainISO) GrainProfile {
	p := profile500T
	// ISO 400 is the measured 500T-class look for every stock. Stock only
	// trims the known-finer emulsions slightly; it does not redo the
	// calibration (so Portra 400 + ISO 400 still matches the 0212 scan).
	switch stock {
	case "Ektar 100":
		p.Amp *= 0.85
		p.Size *= 0.85
		p.Note = "ISO400 500T-class, slightly finer for Ektar"
	case "Vision3 250D 5207":
		p.Amp *= 0.90
		p.Size *= 0.90
		p.Note = "ISO400 500T-class, slightly finer for 250D"
	default:
		p.Note = "ISO400 matched to measured Vision3 500T 0212 scan"
	}
	if iso == GrainISO100 {
		// Size follows sqrt(ISO). RMS is applied through ChanScale so it
		// stays linear after blur: PGI ratio 0.74, times ~0.50 to cancel
		// the HF boost of the finer clouds (RMS of blurred noise ~ 1/sigma).
		p.Size *= iso100SizeScale
		// Finer clouds raise HF RMS only ~1.33x (not 2x) because of the
		// sigma floor and the measurement high-pass. 0.736/1.33 ≈ 0.55.
		adj := 0.55
		p.ChanScale[0] *= adj
		p.ChanScale[1] *= adj
		p.ChanScale[2] *= adj
		p.Note = "ISO100 from measured ISO400: RMS x0.74 (Kodak PGI Ektar100/Portra400), size x0.50 (sqrt ISO)"
	}
	return p
}

func chanSizeMul(p GrainProfile, c int) float64 {
	if c < 0 || c > 2 {
		return 1
	}
	if p.ChanSize[0] == 0 && p.ChanSize[1] == 0 && p.ChanSize[2] == 0 {
		return 1
	}
	s := p.ChanSize[c]
	if s <= 0 {
		return 1
	}
	return s
}
