package colorsci

// v0.78: Kodak Vision3 500T 5219 / 7219 as its own film-stock entry.
//
// Dyes: the H-1-5219 Y/M/C curves (same as Vision3Dyes, spectral.go).
// D-min: the dashed "Minimum Density" curve of the same chart (Kodak
// H-1-5219 "KODAK VISION3 500T Color Negative Film 5219 / 7219", revised
// 3-22, p.4 "Spectral Dye Density Curves", ECN-2), digitised BY EYE from a
// 400 dpi render of the PDF chart (v0.78, about +-0.03 D). ESTIMATE, not a
// measurement; it only weights the spectrum the camera sees through the
// orange mask (KodakDmin / baseT), like the four still-film D-min curves.
//
// Vision3Dyes ("Vision3 5219") is unchanged and stays the legacy/other-ECN
// set (250D, 200T, 50D map to it, no D-min).

// Vision3500TDyes is Kodak Vision3 500T (5219 / 7219) with its D-min.
var Vision3500TDyes = DyeSet{Name: "Vision3 500T", Source: "Kodak H-1-5219 rev. 3-22, p.4 (dye curves + D-min digitised v0.78)",
	Y: DyeYellow, M: DyeMagenta, C: DyeCyan}

// vision3500TDmin: H-1-5219 Minimum Density curve, ESTIMATE (eye-read).
var vision3500TDmin = SpectralCurve{WL: []float64{400, 410, 425, 435, 445, 460, 475, 490, 500, 512, 525, 540, 550, 560, 575, 580, 590, 600, 610, 620, 630, 640, 650, 675, 700, 725, 750, 775, 790, 800},
	V: []float64{0.60, 0.68, 0.79, 0.84, 0.87, 0.85, 0.82, 0.78, 0.72, 0.73, 0.65, 0.56, 0.53, 0.52, 0.53, 0.53, 0.45, 0.33, 0.27, 0.25, 0.24, 0.18, 0.16, 0.15, 0.14, 0.13, 0.11, 0.09, 0.07, 0.07}}

func init() { KodakDmin[Vision3500TDyes.Name] = vision3500TDmin }
