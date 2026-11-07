package colorsci

import "strings"

// StockDyeSets lists the built-in dye sets (see stocks.go for sources).
var StockDyeSets = []DyeSet{Ektar100Dyes, Portra400Dyes, UltraMax400Dyes, Gold200Dyes, Vision3Dyes}

// MatchStock picks a built-in dye set from a film stock name. Portra 160 /
// 800 map to the Portra 400 set, Vision3 500T (5219 / 7219) to its own set
// with D-min (v0.78) and every other Vision3 / ECN stock to 5219 (the
// closest published curves). ok is false when nothing matches.
func MatchStock(name string) (DyeSet, bool) {
	n := strings.ToLower(name)
	n = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(n)
	has := func(s ...string) bool {
		for _, x := range s {
			if strings.Contains(n, x) {
				return true
			}
		}
		return false
	}
	switch {
	case has("ektar"):
		return Ektar100Dyes, true
	case has("portra"):
		return Portra400Dyes, true
	case has("ultramax", "umax", "max400"):
		return UltraMax400Dyes, true
	case has("gold"):
		return Gold200Dyes, true
	case has("500t", "5219", "7219"):
		return Vision3500TDyes, true
	case has("vision", "5219", "5207", "5213", "5203", "7219", "7207", "7213", "7203", "500t", "250d", "200t", "50d", "ecn"):
		return Vision3Dyes, true
	}
	return DyeSet{}, false
}
