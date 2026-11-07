package colorsci

import (
	"fmt"
	"math"
	"strings"
)

// ApplyCastDCTL changes only the four generated Density_In defaults.
// Offsets are signed ADDITIVE density units, not Cineon codes or RGB gains.
func ApplyCastDCTL(body string, on bool, offset [3]float64) (string, error) {
	values := []float64{0, offset[0], offset[1], offset[2]}
	if on {
		values[0] = 1
	}
	keys := []string{"cast_on", "cast_r", "cast_g", "cast_b"}
	for i, key := range keys {
		if math.IsNaN(values[i]) || math.IsInf(values[i], 0) || math.Abs(values[i]) > 1 {
			return "", fmt.Errorf("invalid %s", key)
		}
		prefix := "DEFINE_UI_PARAMS(" + key + ","
		if strings.Count(body, prefix) != 1 {
			return "", fmt.Errorf("Density_In missing unique %s", key)
		}
		start := strings.Index(body, prefix)
		end := strings.Index(body[start:], ")") + start
		if end < start {
			return "", fmt.Errorf("invalid parameter %s", key)
		}
		fields := strings.Split(body[start:end], ",")
		if len(fields) < 4 {
			return "", fmt.Errorf("invalid parameter %s", key)
		}
		fields[3] = fmt.Sprintf(" %.6f", values[i])
		if i == 0 {
			fields[3] = fmt.Sprintf(" %d", int(values[i]))
		}
		body = body[:start] + strings.Join(fields, ",") + body[end:]
	}
	return body, nil
}
