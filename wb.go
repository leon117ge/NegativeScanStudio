package main

import (
	_ "embed"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/tiff"
)

//go:embed template_core.dctl
var coreTemplate string

type WBPreset struct {
	Name    string
	R, G, B float64 // neutral gray point, density codes
}

const maxWB = 60

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

type WBParams struct {
	Base  [3]float64
	White float64
	Gam   [3]float64
	Mode  int // 0 mid-tone gray world, 1 all pixels
}

// positive linear value from a density code, same model as the core DCTL
func linFromCode(code, white, gam float64) float64 {
	dw := (white - 95) * 0.002
	d := (code - 95) * 0.002
	e := math.Exp(-2.302585 * dw / gam)
	x := 2.302585 * (d - dw) / gam
	x = math.Min(math.Max(x, -30), 20)
	l := (math.Exp(x) - e) / (1 - e)
	return math.Min(math.Max(l, 0), 64)
}

func codeFromLin(l, white, gam float64) float64 {
	dw := (white - 95) * 0.002
	e := math.Exp(-2.302585 * dw / gam)
	d := dw + gam*math.Log(l*(1-e)+e)/2.302585
	return d/0.002 + 95
}

// returns stops to add to R and B (G is the anchor), and how many pixels were used
func analyzeWB(path string, p WBParams) (stopsR, stopsB float64, n int, err error) {
	img, err := decodeImage(path)
	if err != nil {
		return 0, 0, 0, err
	}
	get, _ := getter(img)
	b := img.Bounds()
	W, H := b.Dx(), b.Dy()
	mx, my := int(float64(W)*0.04), int(float64(H)*0.04)
	cw, ch := W-2*mx, H-2*my
	if cw < 16 || ch < 16 {
		return 0, 0, 0, fmt.Errorf("image too small")
	}
	step := 1
	for (cw/step)*(ch/step) > 700000 {
		step++
	}
	// lookup table of linear value per channel for 16-bit input would be big; compute directly with a code LUT
	const N = 4096
	var lut [3][N + 1]float64
	for c := 0; c < 3; c++ {
		for i := 0; i <= N; i++ {
			t := math.Max(float64(i)/N/p.Base[c], 1e-6)
			code := 95 + (-math.Log10(t))/0.002
			code = math.Min(math.Max(code, 0), 1023)
			lut[c][i] = linFromCode(code, p.White, p.Gam[c])
		}
	}
	var sl [3]float64
	cnt := 0
	for y := my; y < H-my; y += step {
		for x := mx; x < W-mx; x += step {
			r, g, bl := get(b.Min.X+x, b.Min.Y+y)
			lr := lut[0][int(r*N+0.5)]
			lg := lut[1][int(g*N+0.5)]
			lb := lut[2][int(bl*N+0.5)]
			if p.Mode == 0 {
				if !(lg > 0.04 && lg < 0.6) {
					continue
				}
			} else if !(lg > 0) {
				continue
			}
			sl[0] += math.Log(math.Max(lr, 1e-4))
			sl[1] += math.Log(math.Max(lg, 1e-4))
			sl[2] += math.Log(math.Max(lb, 1e-4))
			cnt++
		}
	}
	if cnt < 2000 {
		return 0, 0, cnt, fmt.Errorf("too few usable pixels (%d)", cnt)
	}
	f := float64(cnt)
	stopsR = (sl[1]/f - sl[0]/f) / math.Ln2
	stopsB = (sl[1]/f - sl[2]/f) / math.Ln2
	return stopsR, stopsB, cnt, nil
}

// neutral gray point codes that reproduce the given stops in the core DCTL
func grayFromStops(stopsR, stopsB float64, p WBParams) (gr, gg, gb float64) {
	gg = 470
	sg := linFromCode(gg, p.White, p.Gam[1])
	gr = codeFromLin(sg*math.Pow(2, -stopsR), p.White, p.Gam[0])
	gb = codeFromLin(sg*math.Pow(2, -stopsB), p.White, p.Gam[2])
	return
}

func parseWBLine(line string) (WBPreset, bool) {
	f := strings.Split(strings.TrimSpace(line), "|")
	if len(f) != 5 || f[0] != "// FBM_WB" {
		return WBPreset{}, false
	}
	name, ok := cleanName(f[1])
	if !ok {
		return WBPreset{}, false
	}
	r, e1 := strconv.ParseFloat(f[2], 64)
	g, e2 := strconv.ParseFloat(f[3], 64)
	b, e3 := strconv.ParseFloat(f[4], 64)
	if e1 != nil || e2 != nil || e3 != nil || r < 50 || g < 50 || b < 50 || r > 1100 || g > 1100 || b > 1100 {
		return WBPreset{}, false
	}
	return WBPreset{name, r, g, b}, true
}

func parseWBPresets(text string) []WBPreset {
	var out []WBPreset
	for _, ln := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		if p, ok := parseWBLine(ln); ok && len(out) < maxWB {
			out = append(out, p)
		}
	}
	return out
}

type WBConfig struct {
	Presets  []WBPreset
	DCTLPath string
	AutoSync bool
}

func wbConfigFile() string { return filepath.Join(configDir(), "wb_config.txt") }

func loadWBConfig() WBConfig {
	c := WBConfig{}
	b, err := os.ReadFile(wbConfigFile())
	if err != nil {
		return c
	}
	txt := strings.ReplaceAll(string(b), "\r", "")
	c.Presets = parseWBPresets(txt)
	for _, ln := range strings.Split(txt, "\n") {
		if strings.HasPrefix(ln, "DCTL_PATH=") {
			c.DCTLPath = strings.TrimPrefix(ln, "DCTL_PATH=")
		}
		if ln == "AUTOSYNC=1" {
			c.AutoSync = true
		}
	}
	return c
}

func saveWBConfig(c WBConfig) {
	os.MkdirAll(configDir(), 0755)
	var sb strings.Builder
	for _, p := range c.Presets {
		sb.WriteString(fmt.Sprintf("// FBM_WB|%s|%.2f|%.2f|%.2f\n", p.Name, p.R, p.G, p.B))
	}
	sb.WriteString("DCTL_PATH=" + c.DCTLPath + "\n")
	if c.AutoSync {
		sb.WriteString("AUTOSYNC=1\n")
	}
	os.WriteFile(wbConfigFile(), []byte(sb.String()), 0644)
}

func buildCoreDCTL(ps []WBPreset) string {
	var h strings.Builder
	h.WriteString("// ===== FILM WHITE BALANCE PRESETS =====\n")
	h.WriteString("// Written by Film Base Meter (module 2). Each preset is the density code of a neutral gray in R, G, B.\n")
	h.WriteString("// Options 0 and 1 are built in and unchanged. The manual Use gray point checkbox overrides any preset.\n")
	for _, p := range ps {
		h.WriteString(fmt.Sprintf("// FBM_WB|%s|%.2f|%.2f|%.2f\n", p.Name, p.R, p.G, p.B))
	}
	ids := "WB_OFF, WB_500T"
	labels := "Off, Kodak Vision3 500T"
	for i, p := range ps {
		ids += fmt.Sprintf(", WB_%d", i+1)
		labels += ", " + p.Name
	}
	h.WriteString(fmt.Sprintf("DEFINE_UI_PARAMS(film_wb, Film white balance, DCTLUI_COMBO_BOX, 1, {%s}, {%s})", ids, labels))

	var f strings.Builder
	f.WriteString("__DEVICE__ float3 wb_gray(int id)\n{\n    switch (id)\n    {\n")
	for i, p := range ps {
		f.WriteString(fmt.Sprintf("        case %d: return make_float3(%.2ff, %.2ff, %.2ff);\n", i+2, p.R, p.G, p.B))
	}
	f.WriteString("        default: return make_float3(470.0f, 470.0f, 470.0f);\n    }\n}")
	out := strings.Replace(coreTemplate, "@@WB_HEADER@@", h.String(), 1)
	out = strings.Replace(out, "@@WB_FUNC@@", f.String(), 1)
	return out
}
