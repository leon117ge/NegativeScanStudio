package develop

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestDefaultChainIsDCTLOnly(t *testing.T) {
	c := DefaultChain()
	if len(c.Nodes) != 9 {
		t.Fatalf("nodes %d", len(c.Nodes))
	}
	for i, n := range c.Nodes {
		if n.Type != Order[i] {
			t.Fatalf("order %d %s", i, n.Type)
		}
	}
	// v0.71: 2383 (D65, embedded) and Rec.709 out are on by default.
	p, o := c.Nodes[7], c.Nodes[8]
	if p.Bypass || o.Bypass || p.Get("white") != 2 || p.Get("source") != 0 || o.Get("space") != 0 {
		t.Fatalf("nodes 8/9 defaults: %+v %+v", p, o)
	}
	// With 8/9 bypassed the chain is exactly the three DCTLs.
	d := c.Clone()
	d.Nodes[7].Bypass, d.Nodes[8].Bypass = true, true
	cx := &Context{}
	full := d.Compile(cx, -1)
	head := Chain{Nodes: c.Nodes[:3]}.Compile(cx, -1)
	if len(full.fs) != len(head.fs) || len(full.fs) != 2 { // mask is identity
		t.Fatalf("fs %d %d", len(full.fs), len(head.fs))
	}
	if len(full.Notes) != 0 {
		t.Fatal(full.Notes)
	}
	if def := c.Compile(cx, -1); len(def.fs) <= len(head.fs) {
		t.Fatalf("default chain lacks the print / output stages: %d", len(def.fs))
	}
}

// TestSavedBypassKept: presets keep their saved node state (v0.70 files:
// "bypass": true for a bypassed node, no field for an enabled one).
func TestSavedBypassKept(t *testing.T) {
	js := `{"nodes":[{"type":"print_lut","bypass":true,"values":{"white":0}},{"type":"cs_out","values":{"space":1}}]}`
	var c Chain
	if err := json.Unmarshal([]byte(js), &c); err != nil {
		t.Fatal(err)
	}
	c.Normalize()
	if !c.Nodes[7].Bypass || c.Nodes[7].Get("white") != 0 || c.Nodes[8].Bypass || c.Nodes[8].Get("space") != 1 {
		t.Fatalf("%+v %+v", c.Nodes[7], c.Nodes[8])
	}
	b, _ := json.Marshal(c)
	var c2 Chain
	json.Unmarshal(b, &c2)
	c2.Normalize()
	if !c2.Nodes[7].Bypass || c2.Nodes[8].Bypass {
		t.Fatal("round trip changed bypass")
	}
}

func TestEstimateBase(t *testing.T) {
	// 10000 pixels: base at (0.5, 0.6, 0.2) for the clearest 1 %, image
	// below, plus clipped leak pixels that must be ignored.
	var pix []uint16
	for i := 0; i < 10000; i++ {
		r, g, b := 0.2, 0.25, 0.05
		if i%100 == 0 {
			r, g, b = 0.5, 0.6, 0.2
		}
		pix = append(pix, uint16(r*65535+0.5), uint16(g*65535+0.5), uint16(b*65535+0.5))
	}
	for i := 0; i < 500; i++ {
		pix = append(pix, 65535, 65535, 30000)
	}
	base, used, ok := EstimateBaseU16(pix)
	if !ok || used != 10000 || math.Abs(base[0]-0.5) > 1e-4 || math.Abs(base[1]-0.6) > 1e-4 || math.Abs(base[2]-0.2) > 1e-4 {
		t.Fatal(base, used, ok)
	}
	if _, _, ok := EstimateBaseU16(make([]uint16, 30)); ok {
		t.Fatal("too few pixels must not estimate")
	}
}

func TestAutoFilmWB(t *testing.T) {
	cx := &Context{WBGrays: []NamedRGB{{Name: "QA WB"}, {Name: "Portra 400 gray card", V: [3]float64{440, 470, 420}}}}
	for _, c := range []struct {
		stock string
		want  int
	}{{"", -1}, {"Vision3 500T", 1}, {"Kodak 5219 roll 3", 1}, {"Ektar 100", AutoGray}, {"Gold 200", AutoGray}, {"Portra 400", 3}} {
		if got, why := AutoFilmWB(c.stock, cx); got != c.want {
			t.Errorf("%q: %d (%s), want %d", c.stock, got, why, c.want)
		}
	}
	if FindStock("Ektar_100_0001_HDR.tiff") != "Ektar 100" || FindStock("V3_5219_A.tif") != "Vision3 5219" || FindStock("scan_0001.tif") != "" {
		t.Fatal(FindStock("Ektar_100_0001_HDR.tiff"), FindStock("V3_5219_A.tif"))
	}
}

func TestNormalize(t *testing.T) {
	c := Chain{Nodes: []Node{NewNode(TCurves), NewNode(TDensity), {Type: "bogus"}, NewNode(TDensity)}}
	c.Nodes[0].V["c_dark"] = 0.1
	c.Normalize()
	if len(c.Nodes) != 9 || c.Nodes[6].Get("c_dark") != 0.1 {
		t.Fatal(c.Nodes)
	}
}

func TestEncodeDecode(t *testing.T) {
	for d := DomRec709; d <= DomSRGB; d++ {
		for _, l := range []float32{0.001, 0.01, 0.18, 0.5, 0.9} {
			got := Decode(d, Encode(d, l))
			if math.Abs(float64(got-l)) > 1e-4*float64(l)+1e-6 {
				t.Errorf("%s %v -> %v", DomainNames[d], l, got)
			}
		}
	}
}

func TestWBPicker(t *testing.T) {
	r, g, b := float32(0.3), float32(0.2), float32(0.12)
	tt, ti := WBFromNeutral(r, g, b)
	gr, gg, gb := WBGains(tt, ti)
	r, g, b = r*gr, g*gg, b*gb
	if math.Abs(float64(r-g)) > 1e-5 || math.Abs(float64(b-g)) > 1e-5 {
		t.Fatal(r, g, b)
	}
}

func identityCube(n int) string {
	var s strings.Builder
	fmt.Fprintf(&s, "TITLE \"id\"\nLUT_3D_SIZE %d\n", n)
	for b := 0; b < n; b++ {
		for g := 0; g < n; g++ {
			for r := 0; r < n; r++ {
				f := float64(n - 1)
				fmt.Fprintf(&s, "%f %f %f\n", float64(r)/f, float64(g)/f, float64(b)/f)
			}
		}
	}
	return s.String()
}

func TestCubeIdentity(t *testing.T) {
	l, err := ParseCube(strings.NewReader(identityCube(17)), "id")
	if err != nil {
		t.Fatal(err)
	}
	for _, tetra := range []bool{false, true} {
		for _, v := range [][3]float32{{0.1, 0.5, 0.9}, {0.77, 0.03, 0.4}, {1, 0, 0.5}} {
			r, g, b := l.Apply(v[0], v[1], v[2], tetra)
			if math.Abs(float64(r-v[0]))+math.Abs(float64(g-v[1]))+math.Abs(float64(b-v[2])) > 1e-5 {
				t.Fatal(tetra, v, r, g, b)
			}
		}
	}
	if _, err := ParseCube(strings.NewReader("LUT_3D_SIZE 2\n0 0 0\n"), "bad"); err == nil {
		t.Fatal("short cube accepted")
	}
}

func TestEmbedded(t *testing.T) {
	for _, w := range LUTWhites {
		l, err := LoadEmbedded(w)
		if err != nil || l.N3 != 33 || len(l.L3) != 35937*3 || l.Placeholder {
			t.Fatal(w, err)
		}
		if l.Min != [3]float32{0, 0, 0} || l.Max != [3]float32{1, 1, 1} {
			t.Fatal(w, l.Min, l.Max)
		}
		t.Logf("%s placeholder=%v %q", w, l.Placeholder, l.Title)
	}
	n := NewNode(TPrint)
	n.Bypass = false
	n.V["source"] = 1 // Resolve not installed here -> embedded fallback
	_, note, err := LoadForNode(n)
	if err != nil || !strings.Contains(note, "embedded") {
		t.Fatal(note, err)
	}
	t.Log(note)
}
