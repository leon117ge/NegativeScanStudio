package convert

import (
	"bytes"
	"embed"
	"encoding/csv"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed data/*_characteristic.csv
var curveFS embed.FS

// CurveSet is per-channel Status M D(logH) above Dmin, with PCHIP interpolants.
type CurveSet struct {
	Stock     string
	Source    string
	LogHMid   float64   // scene MidGrey maps to this datasheet logH (G @ ~0.75 above Dmin)
	Dmin      [3]float64
	LogH      [3][]float64
	DAbove    [3][]float64 // density minus Dmin
	pchip     [3]pchip
	HasFull   bool
}

type pchip struct {
	x, y, m []float64
}

var (
	curveOnce sync.Once
	curveMap  map[string]*CurveSet
)

func loadCurves() {
	curveMap = map[string]*CurveSet{}
	files := map[string]string{
		"Ektar 100":    "data/ektar_characteristic.csv",
		"Portra 400":   "data/portra_characteristic.csv",
		"Gold 200":     "data/gold_characteristic.csv",
		"UltraMax 400": "data/ultramax_characteristic.csv",
	}
	srcNote := map[string]string{
		"Ektar 100":    "Kodak E-4046 rev. 1/25 p.4 daylight Status M (vector polyline)",
		"Portra 400":   "Kodak E-4050 rev. 1/25 p.4 daylight Status M (vector polyline)",
		"Gold 200":     "Kodak E-7022 rev. 06-23 p.4 daylight Status M (Bezier expanded)",
		"UltraMax 400": "Kodak E-7023 rev. 2/16 p.4 daylight Status M (vector polyline)",
	}
	chIdx := map[string]int{"R": 0, "G": 1, "B": 2}
	for stock, path := range files {
		b, err := curveFS.ReadFile(path)
		if err != nil {
			continue
		}
		cs := &CurveSet{Stock: stock, Source: srcNote[stock], HasFull: true}
		type pt struct{ x, y float64 }
		raw := [3][]pt{}
		r := csv.NewReader(bytes.NewReader(b))
		rows, err := r.ReadAll()
		if err != nil || len(rows) < 2 {
			continue
		}
		for _, row := range rows[1:] {
			if len(row) < 6 {
				continue
			}
			ci, ok := chIdx[strings.TrimSpace(row[1])]
			if !ok {
				continue
			}
			lh, e1 := strconv.ParseFloat(row[4], 64)
			d, e2 := strconv.ParseFloat(row[5], 64)
			if e1 != nil || e2 != nil {
				continue
			}
			raw[ci] = append(raw[ci], pt{lh, d})
		}
		ok := true
		for c := 0; c < 3; c++ {
			pts := raw[c]
			if len(pts) < 4 {
				ok = false
				break
			}
			sort.Slice(pts, func(i, j int) bool { return pts[i].x < pts[j].x })
			// Average duplicate logH.
			xs, ys := []float64{}, []float64{}
			for i := 0; i < len(pts); {
				j := i + 1
				sum := pts[i].y
				for j < len(pts) && math.Abs(pts[j].x-pts[i].x) < 1e-9 {
					sum += pts[j].y
					j++
				}
				xs = append(xs, pts[i].x)
				ys = append(ys, sum/float64(j-i))
				i = j
			}
			// Dmin: median of the lowest ~5% of samples (toe / base+mask).
			nToe := len(ys) / 20
			if nToe < 3 {
				nToe = 3
			}
			if nToe > len(ys) {
				nToe = len(ys)
			}
			toe := append([]float64(nil), ys[:nToe]...)
			sort.Float64s(toe)
			cs.Dmin[c] = toe[len(toe)/2]
			dab := make([]float64, len(ys))
			for i := range ys {
				dab[i] = math.Max(ys[i]-cs.Dmin[c], 0)
			}
			cs.LogH[c], cs.DAbove[c] = xs, dab
			cs.pchip[c] = newPCHIP(xs, dab)
		}
		if !ok {
			continue
		}
		// Place MidGrey at G logH where D-Dmin ~= 0.75 (Kodak mid-scale region).
		cs.LogHMid = invertDensity(cs.pchip[1], 0.75)
		curveMap[stock] = cs
	}
}

func curves() map[string]*CurveSet {
	curveOnce.Do(loadCurves)
	return curveMap
}

// CurveFor returns the datasheet curve set when available.
func CurveFor(stockName string) *CurveSet {
	return curves()[stockName]
}

func invertDensity(p pchip, d float64) float64 {
	if len(p.x) == 0 {
		return -0.75
	}
	// Binary search on monotone curve.
	lo, hi := p.x[0], p.x[len(p.x)-1]
	for k := 0; k < 40; k++ {
		mid := 0.5 * (lo + hi)
		if p.eval(mid) < d {
			lo = mid
		} else {
			hi = mid
		}
	}
	return 0.5 * (lo + hi)
}

func (cs *CurveSet) densityAbove(logH float64, c int) float64 {
	if cs == nil || !cs.HasFull {
		return -1
	}
	return math.Max(cs.pchip[c].eval(logH), 0)
}

func newPCHIP(x, y []float64) pchip {
	n := len(x)
	m := make([]float64, n)
	if n == 1 {
		return pchip{x, y, m}
	}
	h := make([]float64, n-1)
	delta := make([]float64, n-1)
	for i := 0; i < n-1; i++ {
		h[i] = x[i+1] - x[i]
		if h[i] == 0 {
			h[i] = 1e-12
		}
		delta[i] = (y[i+1] - y[i]) / h[i]
	}
	m[0], m[n-1] = delta[0], delta[n-2]
	for i := 1; i < n-1; i++ {
		if delta[i-1]*delta[i] <= 0 {
			m[i] = 0
		} else {
			w1 := 2*h[i] + h[i-1]
			w2 := h[i] + 2*h[i-1]
			m[i] = (w1 + w2) / (w1/delta[i-1] + w2/delta[i])
		}
	}
	// Endpoint slopes (Fritsch-Carlson).
	if n > 2 {
		m[0] = ((2*h[0]+h[1])*delta[0] - h[0]*delta[1]) / (h[0] + h[1])
		if m[0]*delta[0] <= 0 {
			m[0] = 0
		} else if delta[0]*delta[1] < 0 && math.Abs(m[0]) > math.Abs(3*delta[0]) {
			m[0] = 3 * delta[0]
		}
		m[n-1] = ((2*h[n-2]+h[n-3])*delta[n-2] - h[n-2]*delta[n-3]) / (h[n-2] + h[n-3])
		if m[n-1]*delta[n-2] <= 0 {
			m[n-1] = 0
		} else if delta[n-2]*delta[n-3] < 0 && math.Abs(m[n-1]) > math.Abs(3*delta[n-2]) {
			m[n-1] = 3 * delta[n-2]
		}
	}
	return pchip{x, y, m}
}

func (p pchip) eval(xv float64) float64 {
	n := len(p.x)
	if n == 0 {
		return 0
	}
	if xv <= p.x[0] {
		return p.y[0]
	}
	if xv >= p.x[n-1] {
		return p.y[n-1]
	}
	i := sort.Search(n, func(j int) bool { return p.x[j] >= xv }) - 1
	if i < 0 {
		i = 0
	}
	if i > n-2 {
		i = n - 2
	}
	h := p.x[i+1] - p.x[i]
	t := (xv - p.x[i]) / h
	t2, t3 := t*t, t*t*t
	h00 := 2*t3 - 3*t2 + 1
	h10 := t3 - 2*t2 + t
	h01 := -2*t3 + 3*t2
	h11 := t3 - t2
	return h00*p.y[i] + h10*h*p.m[i] + h01*p.y[i+1] + h11*h*p.m[i+1]
}

func curveStatus() string {
	var b strings.Builder
	for _, s := range Stocks() {
		if cs := CurveFor(s.Name); cs != nil {
			fmt.Fprintf(&b, "%s: full Status M CSV, LogHMid=%.3f, Dmin=%.3f/%.3f/%.3f (%s)\n",
				s.Name, cs.LogHMid, cs.Dmin[0], cs.Dmin[1], cs.Dmin[2], cs.Source)
		} else {
			fmt.Fprintf(&b, "%s: logistic mid-slope fallback (no full D-logH digitisation)\n", s.Name)
		}
	}
	return b.String()
}
