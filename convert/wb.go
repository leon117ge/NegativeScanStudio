package convert

// asShotScales returns green-normalized white-balance multipliers from
// LibRaw cam_mul (as-shot). ok is false when the multipliers are missing
// or outside a sane range, so a scan or TIFF without cam_mul is left alone.
func asShotScales(mul [4]float32) (rm, gm, bm float64, ok bool) {
	g := float64(mul[1])
	if g < 1e-6 {
		return 1, 1, 1, false
	}
	rm = float64(mul[0]) / g
	bm = float64(mul[2]) / g
	if rm < 0.05 || bm < 0.05 || rm > 16 || bm > 16 {
		return 1, 1, 1, false
	}
	return rm, 1, bm, true
}

func applyAsShot(v [3]float64, mul [4]float32) [3]float64 {
	rm, gm, bm, ok := asShotScales(mul)
	if !ok {
		return v
	}
	return [3]float64{v[0] * rm, v[1] * gm, v[2] * bm}
}
