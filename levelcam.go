package main

// v0.85: light leveling for the trichrome TIFF + camera profile.
//
// The leveling loop measures the RAW own-channel plane (median of the
// Light-page measure box, centre 50 % by default). FilmScan_Density_In's
// film base is measured differently: LibRaw-demosaiced trichrome TIFF,
// clear-base box found in the image (light-leak rejection, median), and
// since v0.84 the camera-profile matrix on top. levelCam closes that gap:
// per-channel targets for the matrix, then a check of the last leveling
// RAWs through the real trichrome pipeline and one corrective pass.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"

	"basetool/colorsci"
	"basetool/hdrscan"
)

type camPlan struct {
	on   bool
	name string
	k    float64
	m    colorsci.Mat3
	want float64    // base wanted in the final (post-matrix) TIFF
	pre  [3]float64 // pre-matrix base that gives want after the matrix
}

// camPlanFor builds the plan for target t; apply=false or no enabled
// profile gives a flat plan (pre = t).
func camPlanFor(t float64, apply bool) (camPlan, error) {
	pl := camPlan{want: t, pre: [3]float64{t, t, t}}
	if !apply {
		return pl, nil
	}
	p, k := activeCam()
	if p == nil {
		return pl, nil
	}
	a := p.M.Lerp(k) // ApplyCam(M, k, v) == a·v
	inv, err := a.Inverse()
	if err != nil {
		return pl, fmt.Errorf("camera profile %q cannot be inverted: %w", p.Name, err)
	}
	pre := inv.Apply([3]float64{t, t, t})
	for c, v := range pre {
		if v < 0.02 || v > 0.95 {
			return pl, fmt.Errorf("camera profile %q needs a %c base of %.3f before the matrix - out of range", p.Name, "RGB"[c], v)
		}
	}
	pl.on, pl.name, pl.k, pl.m, pl.pre = true, p.Name, k, p.M, pre
	return pl, nil
}

func (pl camPlan) post(v [3]float64) [3]float64 {
	if !pl.on {
		return v
	}
	return colorsci.ApplyCam(pl.m, pl.k, v)
}

func (pl camPlan) label() string {
	if !pl.on {
		return "no camera profile"
	}
	return fmt.Sprintf("camera profile \"%s\" x%.2f", pl.name, pl.k)
}

// worstDev is the largest relative deviation of v from want.
func (pl camPlan) worstDev(v [3]float64) float64 {
	w := 0.0
	for _, x := range v {
		w = math.Max(w, math.Abs(x/pl.want-1))
	}
	return w
}

// tiffBase runs one RAW per channel through hdrscan.ProcessTrichrome (the
// scan's own converter, merge and clear-base measurement) and returns the
// pre-matrix base. The TIFF is written to dir and removed.
func tiffBase(ctx context.Context, raws [3]string, dir string, leak float64, box hdrscan.Rect) ([3]float64, int, error) {
	var rs [3][]string
	for c, r := range raws {
		if r == "" {
			return [3]float64{}, 0, errors.New("no leveling RAW for " + "RGB"[c:c+1])
		}
		rs[c] = []string{r}
	}
	res, err := hdrscan.ProcessTrichrome(rs, nil, hdrscan.ProcessOptions{MeasureLevelingBase: true, Ctx: ctx, OutDir: dir, Software: appVersion + " leveling check", LeakPct: leak, BaseNorm: box})
	if err != nil {
		return [3]float64{}, 0, err
	}
	if res.Out != "" {
		os.Remove(res.Out)
	}
	if res.BaseErr != "" {
		return [3]float64{}, 0, errors.New(res.BaseErr)
	}
	return res.Base, res.BaseN, nil
}

func f3(v [3]float64) string { return fmt.Sprintf("R %.4f G %.4f B %.4f", v[0], v[1], v[2]) }
