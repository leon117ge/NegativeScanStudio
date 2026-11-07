// Frozen legacy Film base measurement reference. The GUI page was removed in app v0.91.
package main

import (
	"basetool/basestat"
	"fmt"
	"golang.org/x/image/tiff"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var _ = sort.Float64s

type Row struct {
	Path      string
	Name      string
	R, G, B   float64
	OK        bool
	Flag      bool
	Status    string
	Edges     [4][3]float64
	Rejected  float64
	EdgeValid [4]bool
	// Region is set for a base sent from the HDR scan module: the row is
	// measured over that base region instead of the edge bands.
	Region *basestat.Region
}

func median(v []float64) float64 { return basestat.Median(v) }

func getter(img image.Image) (func(x, y int) (float64, float64, float64), error) {
	switch m := img.(type) {
	case *image.RGBA64:
		return func(x, y int) (float64, float64, float64) {
			i := m.PixOffset(x, y)
			p := m.Pix
			return float64(uint16(p[i])<<8|uint16(p[i+1])) / 65535, float64(uint16(p[i+2])<<8|uint16(p[i+3])) / 65535, float64(uint16(p[i+4])<<8|uint16(p[i+5])) / 65535
		}, nil
	case *image.NRGBA64:
		return func(x, y int) (float64, float64, float64) {
			i := m.PixOffset(x, y)
			p := m.Pix
			return float64(uint16(p[i])<<8|uint16(p[i+1])) / 65535, float64(uint16(p[i+2])<<8|uint16(p[i+3])) / 65535, float64(uint16(p[i+4])<<8|uint16(p[i+5])) / 65535
		}, nil
	case *image.RGBA:
		return func(x, y int) (float64, float64, float64) {
			i := m.PixOffset(x, y)
			p := m.Pix
			return float64(p[i]) / 255, float64(p[i+1]) / 255, float64(p[i+2]) / 255
		}, nil
	case *image.NRGBA:
		return func(x, y int) (float64, float64, float64) {
			i := m.PixOffset(x, y)
			p := m.Pix
			return float64(p[i]) / 255, float64(p[i+1]) / 255, float64(p[i+2]) / 255
		}, nil
	}
	b := img.Bounds()
	_ = b
	return func(x, y int) (float64, float64, float64) {
		r, g, bb, _ := img.At(x, y).RGBA()
		return float64(r) / 65535, float64(g) / 65535, float64(bb) / 65535
	}, nil
}

func measureRow(r *Row, startPct, endPct, leakPct float64) (*Row, error) {
	if r.Region == nil {
		return measure(r.Path, startPct, endPct, leakPct)
	}
	return measureRegion(r.Path, r.Name, r.Region, leakPct)
}

func measureRegion(path, name string, reg *basestat.Region, leakPct float64) (*Row, error) {
	img, err := decodeImage(path)
	if err != nil {
		return nil, err
	}
	get, _ := getter(img)
	b := img.Bounds()
	rgb, rej, n, err := basestat.Measure(func(x, y int) (float64, float64, float64) { return get(b.Min.X+x, b.Min.Y+y) },
		b.Dx(), b.Dy(), *reg, leakPct)
	if err != nil {
		return nil, err
	}
	row := &Row{Path: path, Name: name, Region: reg, OK: true, Rejected: rej}
	row.R, row.G, row.B = rgb[0], rgb[1], rgb[2]
	row.Status = fmt.Sprintf("OK (base region from HDR scan, %d samples, %.0f%% leak/specks removed)", n, rej*100)
	return row, nil
}

func measure(path string, startPct, endPct, leakPct float64) (*Row, error) {
	img, err := decodeImage(path)
	if err != nil {
		return nil, err
	}
	get, _ := getter(img)
	b := img.Bounds()
	W, H := b.Dx(), b.Dy()
	x0, y0 := b.Min.X, b.Min.Y
	bandA := func(total int) int { return int(float64(total) * startPct / 100.0) }
	bandB := func(total int) int {
		v := int(float64(total) * endPct / 100.0)
		return v
	}
	cx := int(float64(W) * 0.03) // keep clear of corners
	cy := int(float64(H) * 0.03)
	row := &Row{Path: path, Name: filepath.Base(path)}
	type rect struct{ xa, xb, ya, yb int } // [xa,xb) x [ya,yb)
	rects := [4]rect{
		{cx, W - cx, bandA(H), bandB(H)},         // top
		{cx, W - cx, H - bandB(H), H - bandA(H)}, // bottom
		{bandA(W), bandB(W), cy, H - cy},         // left
		{W - bandB(W), W - bandA(W), cy, H - cy}, // right
	}
	totalRej, nEdges := 0.0, 0
	for e, rc := range rects {
		if rc.xb-rc.xa < 1 || rc.yb-rc.ya < 1 {
			continue
		}
		var vr, vg, vb []float64
		stepX, stepY := 1, 1
		n := (rc.xb - rc.xa) * (rc.yb - rc.ya)
		if n > 400000 {
			s := n / 400000
			if (rc.xb - rc.xa) > (rc.yb - rc.ya) {
				stepX = s + 1
			} else {
				stepY = s + 1
			}
		}
		for y := rc.ya; y < rc.yb; y += stepY {
			for x := rc.xa; x < rc.xb; x += stepX {
				r, g, bl := get(x0+x, y0+y)
				vr = append(vr, r)
				vg = append(vg, g)
				vb = append(vb, bl)
			}
		}
		// Reject light leak (sprocket holes, bright specks) and dark specks
		// (shared with the HDR scan module's "Send base to Film base").
		med, frac, ok := basestat.CleanMedian(vr, vg, vb, leakPct)
		totalRej += frac
		nEdges++
		if !ok {
			continue // too little clean base on this edge: ignore the edge
		}
		row.Edges[e] = med
		row.EdgeValid[e] = true
	}
	var er, eg, eb []float64
	for e := 0; e < 4; e++ {
		if row.EdgeValid[e] {
			er = append(er, row.Edges[e][0])
			eg = append(eg, row.Edges[e][1])
			eb = append(eb, row.Edges[e][2])
		}
	}
	if len(er) == 0 {
		return nil, fmt.Errorf("no clean base found on any edge")
	}
	if nEdges > 0 {
		row.Rejected = totalRej / float64(nEdges)
	}
	row.R, row.G, row.B = median(er), median(eg), median(eb)
	row.OK = true
	row.Status = fmt.Sprintf("OK (%.0f%% leak/specks removed, %d/4 edges)", row.Rejected*100, len(er))
	return row, nil
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".tif" || ext == ".tiff" {
		return tiff.Decode(f)
	}
	img, _, err := image.Decode(f)
	return img, err
}

func medianOld(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

func measureOld(path string, startPct, endPct, leakPct float64) (*Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var img image.Image
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".tif" || ext == ".tiff" {
		img, err = tiff.Decode(f)
	} else {
		img, _, err = image.Decode(f)
	}
	if err != nil {
		return nil, err
	}
	get, _ := getter(img)
	b := img.Bounds()
	W, H := b.Dx(), b.Dy()
	x0, y0 := b.Min.X, b.Min.Y
	bandA := func(total int) int { return int(float64(total) * startPct / 100.0) }
	bandB := func(total int) int {
		v := int(float64(total) * endPct / 100.0)
		return v
	}
	cx := int(float64(W) * 0.03) // keep clear of corners
	cy := int(float64(H) * 0.03)
	row := &Row{Path: path, Name: filepath.Base(path)}
	type rect struct{ xa, xb, ya, yb int } // [xa,xb) x [ya,yb)
	rects := [4]rect{
		{cx, W - cx, bandA(H), bandB(H)},         // top
		{cx, W - cx, H - bandB(H), H - bandA(H)}, // bottom
		{bandA(W), bandB(W), cy, H - cy},         // left
		{W - bandB(W), W - bandA(W), cy, H - cy}, // right
	}
	totalRej, nEdges := 0.0, 0
	for e, rc := range rects {
		if rc.xb-rc.xa < 1 || rc.yb-rc.ya < 1 {
			continue
		}
		var vr, vg, vb []float64
		stepX, stepY := 1, 1
		n := (rc.xb - rc.xa) * (rc.yb - rc.ya)
		if n > 400000 {
			s := n / 400000
			if (rc.xb - rc.xa) > (rc.yb - rc.ya) {
				stepX = s + 1
			} else {
				stepY = s + 1
			}
		}
		for y := rc.ya; y < rc.yb; y += stepY {
			for x := rc.xa; x < rc.xb; x += stepX {
				r, g, bl := get(x0+x, y0+y)
				vr = append(vr, r)
				vg = append(vg, g)
				vb = append(vb, bl)
			}
		}
		// Reject light leak (sprocket holes, bright specks) and dark specks.
		// Reference = 30th percentile of brightness, assuming most of the strip is clean base.
		lum := make([]float64, len(vr))
		for i := range vr {
			lum[i] = vr[i] + vg[i] + vb[i]
		}
		sl := append([]float64{}, lum...)
		sort.Float64s(sl)
		ref := sl[len(sl)*3/10]
		hi := ref * (1 + leakPct/100.0)
		lo := ref * 0.80
		for it := 0; it < 4; it++ {
			var acc []float64
			for _, l := range lum {
				if l <= hi && l >= lo {
					acc = append(acc, l)
				}
			}
			if len(acc) < 50 {
				break
			}
			ref = medianOld(acc)
			hi = ref * (1 + leakPct/100.0)
			lo = ref * 0.80
		}
		var kr, kg, kb []float64
		for i := range vr {
			if lum[i] <= hi && lum[i] >= lo {
				kr = append(kr, vr[i])
				kg = append(kg, vg[i])
				kb = append(kb, vb[i])
			}
		}
		frac := 1 - float64(len(kr))/float64(len(vr))
		totalRej += frac
		nEdges++
		if len(kr) < len(vr)*2/5 || len(kr) < 50 {
			continue // too little clean base on this edge: ignore the edge
		}
		row.Edges[e] = [3]float64{medianOld(kr), medianOld(kg), medianOld(kb)}
		row.EdgeValid[e] = true
	}
	var er, eg, eb []float64
	for e := 0; e < 4; e++ {
		if row.EdgeValid[e] {
			er = append(er, row.Edges[e][0])
			eg = append(eg, row.Edges[e][1])
			eb = append(eb, row.Edges[e][2])
		}
	}
	if len(er) == 0 {
		return nil, fmt.Errorf("no clean base found on any edge")
	}
	if nEdges > 0 {
		row.Rejected = totalRej / float64(nEdges)
	}
	row.R, row.G, row.B = medianOld(er), medianOld(eg), medianOld(eb)
	row.OK = true
	row.Status = fmt.Sprintf("OK (%.0f%% leak/specks removed, %d/4 edges)", row.Rejected*100, len(er))
	return row, nil
}
