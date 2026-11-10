package colorsci

import (
	_ "embed"
	"encoding/csv"
	"math"
	"strconv"
	"strings"
)

//go:embed data/5207_log_sensitivity.csv
var vision3250DSensitivityCSV string

// LogSensitivity returns wavelength (nm) and per-channel log10 spectral
// sensitivity samples digitised from a public Kodak datasheet.
// 500T / 5219: H-1-5219. 250D / 5207: H-1-5207. ok is false for other names.
func LogSensitivity(stock string) (wl []float64, logS [3][]float64, ok bool) {
	n := strings.ToLower(stock)
	src := ""
	switch {
	case strings.Contains(n, "500t") || strings.Contains(n, "5219"):
		src = vision3SensitivityCSV
	case strings.Contains(n, "250d") || strings.Contains(n, "5207"):
		src = vision3250DSensitivityCSV
	default:
		return nil, logS, false
	}
	rows, err := csv.NewReader(strings.NewReader(src)).ReadAll()
	if err != nil || len(rows) < 2 {
		return nil, logS, false
	}
	for _, row := range rows[1:] {
		if len(row) < 4 || row[0] == "" {
			continue
		}
		nm, err := strconv.ParseFloat(row[0], 64)
		if err != nil {
			continue
		}
		var ch [3]float64
		var has [3]bool
		any := false
		for c := 0; c < 3; c++ {
			if row[c+1] == "" {
				continue
			}
			v, e := strconv.ParseFloat(row[c+1], 64)
			if e != nil {
				continue
			}
			ch[c] = v
			has[c] = true
			any = true
		}
		if !any {
			continue
		}
		wl = append(wl, nm)
		for c := 0; c < 3; c++ {
			if has[c] {
				logS[c] = append(logS[c], ch[c])
			} else {
				logS[c] = append(logS[c], math.NaN())
			}
		}
	}
	return wl, logS, len(wl) > 4
}

// SensLinear interpolates 10^logS at nm. Missing samples are treated as zero.
func SensLinear(wl []float64, logS []float64, nm float64) float64 {
	if len(wl) == 0 || len(wl) != len(logS) {
		return 0
	}
	if nm <= wl[0] {
		if math.IsNaN(logS[0]) {
			return 0
		}
		return math.Pow(10, logS[0])
	}
	n := len(wl)
	if nm >= wl[n-1] {
		if math.IsNaN(logS[n-1]) {
			return 0
		}
		return math.Pow(10, logS[n-1])
	}
	for i := 1; i < n; i++ {
		if nm <= wl[i] {
			a, b := logS[i-1], logS[i]
			if math.IsNaN(a) && math.IsNaN(b) {
				return 0
			}
			if math.IsNaN(a) {
				return math.Pow(10, b)
			}
			if math.IsNaN(b) {
				return math.Pow(10, a)
			}
			t := (nm - wl[i-1]) / (wl[i] - wl[i-1])
			return math.Pow(10, a+(b-a)*t)
		}
	}
	return 0
}
