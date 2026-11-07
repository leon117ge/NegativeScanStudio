// Package develop (v0.70) is the Develop module: the three DaVinci DCTLs of
// this program (FilmScan_Density_In -> FilmScan_MaskCrosstalk ->
// FilmScan_Positive_Out) ported 1:1 to Go and run as a Resolve-style serial
// node chain, followed by optional user nodes (exposure, white balance,
// curves, saturation).
//
// The DCTL math is evaluated in float32 like Resolve does; every formula
// below is a literal transcription of the .dctl sources (same order of
// operations, same constants), so the default chain reproduces the DCTL
// output. Pixel values are 0..1 (16-bit TIFF / 65535) as Resolve reads them.
package develop

import (
	"fmt"
	"math"
	"strings"
)

// Node types, in the fixed chain order (Resolve-style serial nodes 1..9).
const (
	TDensity  = "density_in"
	TMask     = "mask_crosstalk"
	TPositive = "positive_out"
	TContrast = "contrast"
	TExposure = "exposure"
	TWB       = "white_balance"
	TCurves   = "curves"
	TPrint    = "print_lut"
	TCSOut    = "cs_out"
)

// Order is the fixed node order. Nodes can be bypassed and edited, never
// removed or reordered.
var Order = []string{TDensity, TMask, TPositive, TContrast, TExposure, TWB, TCurves, TPrint, TCSOut}

// Fixed is the number of DCTL nodes at the head of every chain.
const Fixed = 3

// Kind of a parameter.
type Kind int

const (
	Slider Kind = iota
	Check
	Combo
	Path // a file path (string value)
)

// Param describes one node parameter (mirrors a DCTL DEFINE_UI_PARAMS).
type Param struct {
	Key, Label     string
	Kind           Kind
	Def            float64
	Min, Max, Step float64
	// Items: fixed combo items (nil = list supplied by the Context, see ComboItems).
	Items []string
}

// Type describes a node type.
type Type struct {
	ID, Name, Short string
	Params          []Param
	Note            string
	// Bypassed: the node starts bypassed (optional stages that change the
	// look); every other non-DCTL node starts at neutral values.
	Bypassed bool
}

func sl(key, label string, def, min, max, step float64) Param {
	return Param{Key: key, Label: label, Kind: Slider, Def: def, Min: min, Max: max, Step: step}
}
func cb(key, label string, def float64) Param {
	return Param{Key: key, Label: label, Kind: Check, Def: def}
}
func co(key, label string, def float64, items ...string) Param {
	return Param{Key: key, Label: label, Kind: Combo, Def: def, Items: items}
}

// LUT sources of the print node.
var LUTSources = []string{"Embedded in program", "Resolve Film Looks folder (auto-detect)", "Custom .cube file"}

// Whites of the print node (file name suffix of the 2383 LUTs).
var LUTWhites = []string{"D55", "D60", "D65"}

// Types lists every node type: DCTL params with the DCTL defaults and ranges.
var Types = []Type{
	{ID: TDensity, Name: "Density In", Short: "FilmScan_Density_In", Note: "Linear scan / film base -> Cineon-style density code (DCTL node 1).", Params: []Param{
		{Key: "preset", Label: "Film base preset", Kind: Combo, Def: 0},
		cb("in_di", "Input is DaVinci Intermediate timeline", 0),
		cb("out_trans", "Output transmittance instead of density", 0),
		sl("base_r", "Film base R", 0.1933, 0.0001, 16.0, 0.00001),
		sl("base_g", "Film base G", 0.1883, 0.0001, 16.0, 0.00001),
		sl("base_b", "Film base B", 0.0593, 0.0001, 16.0, 0.00001),
		cb("cast_on", "Apply colour cast correction", 0),
		sl("cast_r", "Colour cast density R", 0, -1, 1, 0.0001),
		sl("cast_g", "Colour cast density G", 0, -1, 1, 0.0001),
		sl("cast_b", "Colour cast density B", 0, -1, 1, 0.0001),
		sl("dmin_code", "Film base code", 95.0, 0.0, 300.0, 0.5),
		sl("dens_step", "Density per code", 0.002, 0.0005, 0.005, 0.0001),
		sl("full_code", "Full scale code", 1023.0, 255.0, 4095.0, 1.0),
		sl("ev_stops", "Backlight exposure offset", 0.0, -5.0, 5.0, 0.01),
		sl("blk_r", "Black subtract R", 0.0, -0.05, 0.2, 0.00001),
		sl("blk_g", "Black subtract G", 0.0, -0.05, 0.2, 0.00001),
		sl("blk_b", "Black subtract B", 0.0, -0.05, 0.2, 0.00001),
		cb("clamp01", "Clamp output to 0 to 1", 1),
	}},
	{ID: TMask, Name: "Mask / Crosstalk", Short: "FilmScan_MaskCrosstalk", Note: "Orange-mask compensation and 3x3 dye-crosstalk matrix in density space (DCTL). Defaults = no change.", Params: []Param{
		{Key: "mat_preset", Label: "Crosstalk matrix preset", Kind: Combo, Def: 0},
		cb("mask_on", "Orange mask compensation", 0),
		sl("bd_r", "Base density R", 0.0, -0.5, 3.0, 0.0001),
		sl("bd_g", "Base density G", 0.0, -0.5, 3.0, 0.0001),
		sl("bd_b", "Base density B", 0.0, -0.5, 3.0, 0.0001),
		sl("m00", "Matrix R from R", 1.0, -3.0, 3.0, 0.0001),
		sl("m01", "Matrix R from G", 0.0, -3.0, 3.0, 0.0001),
		sl("m02", "Matrix R from B", 0.0, -3.0, 3.0, 0.0001),
		sl("m10", "Matrix G from R", 0.0, -3.0, 3.0, 0.0001),
		sl("m11", "Matrix G from G", 1.0, -3.0, 3.0, 0.0001),
		sl("m12", "Matrix G from B", 0.0, -3.0, 3.0, 0.0001),
		sl("m20", "Matrix B from R", 0.0, -3.0, 3.0, 0.0001),
		sl("m21", "Matrix B from G", 0.0, -3.0, 3.0, 0.0001),
		sl("m22", "Matrix B from B", 1.0, -3.0, 3.0, 0.0001),
		sl("strength", "Crosstalk strength", 0.0, 0.0, 1.0, 0.01),
		sl("dmin_code", "Film base code", 95.0, 0.0, 300.0, 0.5),
		sl("dens_step", "Density per code", 0.002, 0.0005, 0.005, 0.0001),
		sl("full_code", "Full scale code", 1023.0, 255.0, 4095.0, 1.0),
		cb("clamp01", "Clamp output to 0 to 1", 0),
	}},
	{ID: TPositive, Name: "Positive Out", Short: "FilmScan_Positive_Out", Note: "Density code -> positive (print-like inversion) and output encoding (DCTL). Nodes 4-7 grade in this encoding.", Params: []Param{
		co("out_mode", "Output", 2, "Rec709", "DaVinci Intermediate", "Cineon", "ACEScct", "Linear"),
		co("contrast", "Contrast", 0, "Off", "Low", "Medium", "High"),
		sl("white_code", "White point code", 696.0, 200.0, 1023.0, 1.0),
		sl("gam_r", "Gamma R", 0.55, 0.2, 2.0, 0.01),
		sl("gam_g", "Gamma G", 0.635, 0.2, 2.0, 0.01),
		sl("gam_b", "Gamma B", 0.62, 0.2, 2.0, 0.01),
		sl("expo", "Exposure", 1.0, -6.0, 6.0, 0.01),
		sl("bal_r", "Red balance", 0.0, -5.0, 5.0, 0.01),
		sl("bal_g", "Green balance", 0.0, -5.0, 5.0, 0.01),
		sl("bal_b", "Blue balance", 0.0, -5.0, 5.0, 0.01),
		{Key: "film_wb", Label: "Film white balance", Kind: Combo, Def: 1},
		// film_wb_auto (v0.71) is a UI setting only (not a DCTL parameter, not
		// read by the transform): when on, the UI sets film_wb from the known
		// film stock (AutoFilmWB). The chosen item is stored in film_wb, so
		// the output equals the DCTL with that same item.
		cb("film_wb_auto", "Film WB: Auto (follow film stock)", 1),
		cb("gray_on", "Use gray point", 0),
		sl("gray_r", "Gray point R code", 470.0, 100.0, 1023.0, 0.5),
		sl("gray_g", "Gray point G code", 470.0, 100.0, 1023.0, 0.5),
		sl("gray_b", "Gray point B code", 470.0, 100.0, 1023.0, 0.5),
	}},
	{ID: TContrast, Name: "Contrast", Short: "Contrast", Note: "Contrast around a pivot and saturation on the signal (Resolve Primaries style; pivot 0.435 = log mid grey).", Params: []Param{
		sl("contrast", "Contrast", 1, 0, 3, 0.01),
		sl("pivot", "Pivot", 0.435, 0, 1, 0.001),
		sl("sat", "Saturation", 1, 0, 3, 0.01),
	}},
	{ID: TExposure, Name: "Exposure", Short: "Exposure + HDR zones", Note: "Exposure in stops (scene linear), plus HDR zones: extra stops for pixels whose luminance is in that zone (stops from 18% grey, soft edges).", Params: []Param{
		sl("stops", "Exposure (st)", 0, -4, 4, 0.01),
		sl("z_shadow", "Shadows < -2.5 st", 0, -3, 3, 0.01),
		sl("z_mid", "Mids -1.5..+1.5 st", 0, -3, 3, 0.01),
		sl("z_light", "Highlights > +2 st", 0, -3, 3, 0.01),
		sl("z_spec", "Speculars > +4 st", 0, -3, 3, 0.01),
		sl("falloff", "Zone softness (st)", 1, 0.25, 3, 0.01),
	}},
	{ID: TWB, Name: "White Balance", Short: "WB", Note: "Temperature (+ warmer) and tint (+ magenta) in stops on scene linear. Pick neutral: click a grey/white point in the preview.", Params: []Param{
		sl("temp", "Temperature", 0, -3, 3, 0.001),
		sl("tint", "Tint", 0, -3, 3, 0.001),
	}},
	{ID: TCurves, Name: "Curves", Short: "Curves", Note: "Master curve on the signal through 6 points (x = 0, 0.2 ... 1): y = x + offset, smooth monotone spline.", Params: []Param{
		sl("c_black", "Black (x 0.0)", 0, -0.2, 0.2, 0.001),
		sl("c_shadow", "Shadows (x 0.2)", 0, -0.2, 0.2, 0.001),
		sl("c_dark", "Darks (x 0.4)", 0, -0.2, 0.2, 0.001),
		sl("c_light", "Lights (x 0.6)", 0, -0.2, 0.2, 0.001),
		sl("c_high", "Highlights (x 0.8)", 0, -0.2, 0.2, 0.001),
		sl("c_white", "White (x 1.0)", 0, -0.2, 0.2, 0.001),
	}},
	{ID: TPrint, Name: "Cineon -> 2383", Short: "Print LUT", Note: "Kodak 2383 print film LUT (Cineon Film Log in, Rec.709 gamma 2.4 out). The signal is converted to Cineon first when Positive Out uses another encoding.", Params: []Param{
		co("white", "Projection white", 2, LUTWhites...),
		co("source", "LUT source", 0, LUTSources...),
		{Key: "path", Label: "Custom .cube file", Kind: Path},
		co("interp", "Interpolation", 0, "Trilinear", "Tetrahedral"),
	}},
	{ID: TCSOut, Name: "Color space out", Short: "CS out", Note: "Output transform. After the 2383 node the signal is already Rec.709 gamma 2.4 display; without it the scene signal is decoded to linear first.", Params: []Param{
		co("space", "Output color space", 0, "Rec.709 (gamma 2.4)", "sRGB"),
	}},
}

// TypeByID returns the type description (nil = unknown).
func TypeByID(id string) *Type {
	for i := range Types {
		if Types[i].ID == id {
			return &Types[i]
		}
	}
	return nil
}

// Node is one node of the chain.
type Node struct {
	Type   string             `json:"type"`
	Bypass bool               `json:"bypass,omitempty"`
	Label  string             `json:"label,omitempty"`
	V      map[string]float64 `json:"values"`
	// Items records the chosen item text of combo parameters whose list
	// comes from the user's presets / DB, so a preset still finds it after
	// the list changed.
	Items map[string]string `json:"items,omitempty"`
	// S holds string parameters (file paths).
	S map[string]string `json:"strings,omitempty"`
}

// NewNode returns a node with the defaults of its type.
func NewNode(t string) Node {
	n := Node{Type: t, V: map[string]float64{}}
	if ty := TypeByID(t); ty != nil {
		n.Bypass = ty.Bypassed
		for _, p := range ty.Params {
			n.V[p.Key] = p.Def
		}
	}
	return n
}

// Get returns a parameter value (default when missing).
func (n Node) Get(key string) float64 {
	if v, ok := n.V[key]; ok {
		return v
	}
	if ty := TypeByID(n.Type); ty != nil {
		for _, p := range ty.Params {
			if p.Key == key {
				return p.Def
			}
		}
	}
	return 0
}

// Clone deep-copies n.
func (n Node) Clone() Node {
	c := n
	c.V = map[string]float64{}
	for k, v := range n.V {
		c.V[k] = v
	}
	if n.S != nil {
		c.S = map[string]string{}
		for k, v := range n.S {
			c.S[k] = v
		}
	}
	if n.Items != nil {
		c.Items = map[string]string{}
		for k, v := range n.Items {
			c.Items[k] = v
		}
	}
	return c
}

// Chain is the whole node graph (serial).
type Chain struct {
	Nodes []Node `json:"nodes"`
}

// DefaultChain is the fixed 9-node chain: the three DCTLs with the DCTL
// defaults, nodes 4-7 at neutral values, node 8 (Cineon -> 2383, D65) and
// node 9 (Rec.709) enabled (v0.71), so the default output is a display
// image: the output of the three DCTLs through the 2383 print LUT.
func DefaultChain() Chain {
	c := Chain{}
	for _, t := range Order {
		c.Nodes = append(c.Nodes, NewNode(t))
	}
	return c
}

// Clone deep-copies c.
func (c Chain) Clone() Chain {
	out := Chain{Nodes: make([]Node, len(c.Nodes))}
	for i, n := range c.Nodes {
		out.Nodes[i] = n.Clone()
	}
	return out
}

// Normalize enforces the fixed structure: every node type exactly once in
// Order (missing ones get defaults, unknown or duplicate ones are dropped).
func (c *Chain) Normalize() {
	have := map[string]Node{}
	for _, n := range c.Nodes {
		if TypeByID(n.Type) == nil {
			continue
		}
		if _, dup := have[n.Type]; dup {
			continue
		}
		if n.V == nil {
			n.V = map[string]float64{}
		}
		have[n.Type] = n
	}
	out := make([]Node, 0, len(Order))
	for _, t := range Order {
		if n, ok := have[t]; ok {
			out = append(out, n)
		} else {
			out = append(out, NewNode(t))
		}
	}
	c.Nodes = out
}

// Str returns a string parameter.
func (n Node) Str(key string) string { return n.S[key] }

// SetStr sets a string parameter.
func (n *Node) SetStr(key, v string) {
	if n.S == nil {
		n.S = map[string]string{}
	}
	n.S[key] = v
}

// NamedRGB is a named triple (film base preset, WB gray point).
type NamedRGB struct {
	Name string
	V    [3]float64
}

// NamedMat is a named crosstalk matrix (row-major).
type NamedMat struct {
	Name string
	M    [9]float64
}

// Context supplies the lists behind the DCTL preset dropdowns, in DCTL
// order: film base presets (item k = BasePresets[k-1], 0 = Custom),
// crosstalk matrix presets (item k = Matrices[k-1], 0 = Custom) and film
// white balance presets (0 Off, 1 Kodak Vision3 500T, k = WBGrays[k-2]).
type Context struct {
	BasePresets []NamedRGB
	Matrices    []NamedMat
	WBGrays     []NamedRGB
}

// ComboItems returns the dropdown items of a combo parameter.
func (cx *Context) ComboItems(p Param) []string {
	if p.Items != nil {
		return p.Items
	}
	switch p.Key {
	case "preset":
		out := []string{"Custom - use sliders below"}
		for _, b := range cx.BasePresets {
			out = append(out, b.Name)
		}
		return out
	case "mat_preset":
		out := []string{"Custom - use sliders below"}
		for _, m := range cx.Matrices {
			out = append(out, m.Name)
		}
		return out
	case "film_wb":
		out := []string{"Off", "Kodak Vision3 500T"}
		for _, g := range cx.WBGrays {
			out = append(out, g.Name)
		}
		return out
	}
	return nil
}

// Resolve re-maps context-list combo values of every node by their stored
// item text (after the lists changed or a preset was loaded). Unknown text
// falls back to item 0 (Custom / Off) except film_wb 1 which is built in.
func (c *Chain) Resolve(cx *Context) {
	for i := range c.Nodes {
		n := &c.Nodes[i]
		ty := TypeByID(n.Type)
		if ty == nil {
			continue
		}
		for _, p := range ty.Params {
			if p.Kind != Combo || p.Items != nil {
				continue
			}
			items := cx.ComboItems(p)
			want, ok := n.Items[p.Key]
			if !ok {
				if int(n.Get(p.Key)) >= len(items) {
					n.V[p.Key] = 0
				}
				continue
			}
			found := -1
			for k, s := range items {
				if s == want {
					found = k
					break
				}
			}
			if found < 0 {
				found = 0
			}
			n.V[p.Key] = float64(found)
		}
	}
}

// SetCombo sets a combo value and remembers its item text.
func (n *Node) SetCombo(cx *Context, p Param, idx int) {
	n.V[p.Key] = float64(idx)
	if p.Items == nil {
		items := cx.ComboItems(p)
		if n.Items == nil {
			n.Items = map[string]string{}
		}
		if idx >= 0 && idx < len(items) {
			n.Items[p.Key] = items[idx]
		}
	}
}

// Summary is a one-line description of a node for the node strip.
func (n Node) Summary(cx *Context) string {
	ty := TypeByID(n.Type)
	if ty == nil {
		return "?"
	}
	item := func(key string) string {
		for _, p := range ty.Params {
			if p.Key == key {
				it := cx.ComboItems(p)
				k := int(n.Get(key))
				if k >= 0 && k < len(it) {
					return it[k]
				}
			}
		}
		return ""
	}
	switch n.Type {
	case TDensity:
		if int(n.Get("preset")) > 0 {
			return "Base: " + item("preset")
		}
		return fmt.Sprintf("Base %.3f %.3f %.3f", n.Get("base_r"), n.Get("base_g"), n.Get("base_b"))
	case TMask:
		if n.Get("mask_on") == 0 && n.Get("strength") == 0 {
			return "No change (defaults)"
		}
		s := fmt.Sprintf("Strength %.0f%%", n.Get("strength")*100)
		if n.Get("mask_on") != 0 {
			s = "Mask on, " + s
		}
		return s
	case TPositive:
		s := item("out_mode")
		if c := item("contrast"); c != "Off" {
			s += ", contrast " + strings.ToLower(c)
		}
		return s
	case TContrast:
		return fmt.Sprintf("Contrast %.2f  Sat %.2f", n.Get("contrast"), n.Get("sat"))
	case TExposure:
		s := fmt.Sprintf("%+.2f stops", n.Get("stops"))
		for _, k := range []string{"z_shadow", "z_mid", "z_light", "z_spec"} {
			if n.Get(k) != 0 {
				return s + " + zones"
			}
		}
		return s
	case TWB:
		return fmt.Sprintf("Temp %+.2f  Tint %+.2f", n.Get("temp"), n.Get("tint"))
	case TCurves:
		for _, k := range []string{"c_black", "c_shadow", "c_dark", "c_light", "c_high", "c_white"} {
			if n.Get(k) != 0 {
				return "Curve edited"
			}
		}
		return "Straight"
	case TPrint:
		return "2383 " + item("white") + ", " + []string{"embedded", "Resolve folder", "custom file"}[clampIdx(n.Get("source"), 3)]
	case TCSOut:
		return item("space")
	}
	return ""
}

func clampIdx(v float64, n int) int {
	i := int(v)
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// ---- float32 helpers mirroring the DCTL intrinsics

func f32(v float64) float32   { return float32(v) }
func expf(x float32) float32  { return float32(math.Exp(float64(x))) }
func exp2f(x float32) float32 { return float32(math.Exp2(float64(x))) }
func logf(x float32) float32  { return float32(math.Log(float64(x))) }
func log2f(x float32) float32 { return float32(math.Log2(float64(x))) }
func powf(x, y float32) float32 {
	return float32(math.Pow(float64(x), float64(y)))
}
func sqrtf(x float32) float32 { return float32(math.Sqrt(float64(x))) }
func tanhf(x float32) float32 { return float32(math.Tanh(float64(x))) }
func fmaxf(a, b float32) float32 {
	if a > b {
		return a
	}
	if b != b { // NaN: CUDA fmaxf returns the other operand
		return a
	}
	return b
}
func fminf(a, b float32) float32 {
	if a < b {
		return a
	}
	if b != b {
		return a
	}
	return b
}
func clamp01(v float32) float32 { return fminf(fmaxf(v, 0), 1) }
