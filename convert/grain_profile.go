package convert

// GrainProfile is calibrated from a real Vision3 500T trichrome scan
// (500T_2026_11_2_..._0212_RGB_HDR.tiff). Other stocks are scaled from it.
type GrainProfile struct {
	Corr      float64   // shared fluctuation weight (0..1)
	ChanScale [3]float64 // R,G,B RMS multipliers (G≈1)
	Amp       float64   // overall quantum scale vs Portra ISO100 baseline
	Size      float64   // cloud size multiplier
	Note      string
}

// profile500T is from measured HF density stats on flat patches:
// RMS_HF ≈ 0.03/0.05/0.075 (R/G/B), corr RG≈0.75 GB≈0.55 RB≈0.50.
var profile500T = GrainProfile{
	Corr: 0.72, ChanScale: [3]float64{0.62, 1.00, 1.55},
	Amp: 2.4, Size: 1.15,
	Note: "calibrated from 500T_2026_11_2 scan crops (v0.96.3)",
}

func grainProfile(stock string, iso GrainISO) GrainProfile {
	p := profile500T
	switch stock {
	case "Vision3 500T 5219":
		// as measured
	case "Vision3 250D 5207":
		p.Amp *= 0.85
		p.Size *= 0.9
		p.Note = "scaled from 500T measurement (finer daylight stock)"
	case "Ektar 100":
		p.Amp *= 0.45
		p.Size *= 0.7
		p.ChanScale = [3]float64{0.7, 1.0, 1.35}
		p.Note = "scaled from 500T (extremely fine)"
	case "Portra 400":
		p.Amp *= 0.75
		p.Size *= 0.9
		p.Note = "scaled from 500T measurement"
	case "Gold 200":
		p.Amp *= 0.8
		p.Size *= 0.95
		p.Note = "scaled from 500T measurement"
	case "UltraMax 400":
		p.Amp *= 0.95
		p.Size *= 1.0
		p.Note = "scaled from 500T measurement"
	default:
		p.Amp *= 0.8
		p.Note = "scaled from 500T measurement (default)"
	}
	if iso == GrainISO100 {
		p.Amp *= 0.55
		p.Size *= 0.65
	}
	// ISO400 = profile as-is for 400-ish; for 500T ISO400 option is the native look
	return p
}
