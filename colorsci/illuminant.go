package colorsci

import (
	_ "embed"
	"encoding/csv"
	"fmt"
	"math"
	"strconv"
	"strings"
)

//go:embed data/5219_log_sensitivity.csv
var vision3SensitivityCSV string

// IlluminantBalance is a nominal ECN-2 layer model, NOT scanner calibration.
// Density is the extra density caused by scene illumination; subtract it.
type IlluminantBalance struct {
	Kelvin      float64    `json:"kelvin"`
	LogExposure [3]float64 `json:"log_exposure"`
	Gamma       [3]float64 `json:"gamma"`
	Density     [3]float64 `json:"density"`
	Mired       float64    `json:"mired_to_3200"`
}

// PlanckRelative is energy spectral radiance normalised at 550 nm.
// Kodak's sensitivity is reciprocal energy exposure, not photon response.
func PlanckRelative(nm, kelvin float64) float64 {
	const c2 = 1.438776877e7 // nm K
	return math.Pow(550/nm, 5) * math.Expm1(c2/(550*kelvin)) / math.Expm1(c2/(nm*kelvin))
}

// Vision3Illuminant integrates the published 10 nm visible sensitivity samples.
// 5500 K here means a BLACKBODY approximation, not CIE D55 or an LED SPD.
func Vision3Illuminant(kelvin float64) (IlluminantBalance, error) {
	r := IlluminantBalance{Kelvin: kelvin, Gamma: [3]float64{0.4482, 0.5266, 0.5096}}
	if math.IsNaN(kelvin) || math.IsInf(kelvin, 0) || kelvin < 2500 || kelvin > 10000 {
		return r, fmt.Errorf("scene illuminant must be 2500..10000 K")
	}
	rows, err := csv.NewReader(strings.NewReader(vision3SensitivityCSV)).ReadAll()
	if err != nil {
		return r, err
	}
	var target, tungsten [3]float64
	for _, row := range rows[1:] {
		nm, _ := strconv.ParseFloat(row[0], 64)
		for c := 0; c < 3; c++ {
			if row[c+1] == "" {
				continue
			}
			logS, e := strconv.ParseFloat(row[c+1], 64)
			if e != nil {
				return r, e
			}
			s := math.Pow(10, logS) * 10 // equal 10 nm quadrature; missing tails = zero
			target[c] += s * PlanckRelative(nm, kelvin)
			tungsten[c] += s * PlanckRelative(nm, 3200)
		}
	}
	green := math.Log10(target[1] / tungsten[1])
	for c := 0; c < 3; c++ {
		r.LogExposure[c] = math.Log10(target[c]/tungsten[c]) - green
		r.Density[c] = r.Gamma[c] * r.LogExposure[c]
	}
	r.Mired = 1e6/3200 - 1e6/kelvin
	return r, nil
}
