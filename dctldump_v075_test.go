package main

import "fmt"

type dumpVariant struct {
	tag string
	s   maskSettings
}

// dumpVariants: every Sensor / Light / Form combination (tag _S_L_F).
// Compare: crosstalk Off -> every variant equals v0.74; legacy sensor
// (_1_*_*) -> equals v0.74 for every setting.
func dumpVariants(s maskSettings) []dumpVariant {
	var v []dumpVariant
	for se := 0; se < 2; se++ {
		for l := 0; l < 2; l++ {
			for f := 0; f < 2; f++ {
				x := s
				x.Sensor, x.Light, x.Form = se, l, f
				v = append(v, dumpVariant{fmt.Sprintf("_%d_%d_%d", se, l, f), x})
			}
		}
	}
	return v
}
