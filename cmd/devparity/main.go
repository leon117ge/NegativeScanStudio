// devparity runs the Develop node chain on a 16-bit TIFF and writes the
// float32 result (raw little-endian RGB) for the DCTL parity test.
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"

	"basetool/cropper"
	"basetool/develop"
)

func main() {
	in := flag.String("in", "", "input 16-bit TIFF")
	out := flag.String("out", "", "output .f32 (raw RGB float32)")
	set := flag.String("set", "{}", `JSON {"node_type": {"key": value}, "bypass": ["node_type"]}`)
	stride := flag.Int("stride", 1, "use every n-th pixel")
	tif := flag.String("tiff", "", "also write the full-res 16-bit result here (must not exist)")
	autobase := flag.Bool("autobase", false, "estimate the film base from the image (as Develop does for a roll without preset / DB match)")
	stock := flag.String("stock", "", "film stock for Auto film WB (film_wb_auto=1): \"\" = leave film_wb")
	dump := flag.String("chain", "", "write the final chain JSON here")
	orient := flag.Int("orient", 0, "v0.71: orientation of the -tiff output (develop.Orient 0..7, as in the roll preset)")
	flag.Parse()
	im, err := cropper.Load(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	c := develop.DefaultChain()
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*set), &cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for k, raw := range cfg {
		if k == "bypass" || k == "enable" {
			var l []string
			json.Unmarshal(raw, &l)
			for _, t := range l {
				for i := range c.Nodes {
					if c.Nodes[i].Type == t {
						c.Nodes[i].Bypass = k == "bypass"
					}
				}
			}
			continue
		}
		var m map[string]float64
		json.Unmarshal(raw, &m)
		for i := range c.Nodes {
			if c.Nodes[i].Type == k {
				for kk, v := range m {
					c.Nodes[i].V[kk] = v
				}
			}
		}
	}
	cx := &develop.Context{Matrices: develop.BuiltinMatrices()}
	if *autobase {
		b, used, ok := develop.EstimateBaseU16(im.Pix)
		if !ok {
			fmt.Fprintln(os.Stderr, "base estimate: too few usable pixels", used)
			os.Exit(1)
		}
		n := &c.Nodes[0]
		n.V["preset"], n.V["base_r"], n.V["base_g"], n.V["base_b"] = 0, b[0], b[1], b[2]
		fmt.Fprintf(os.Stderr, "base estimate %.5f %.5f %.5f (%d px)\n", b[0], b[1], b[2], used)
	}
	if msg := develop.ApplyAutoFilmWB(&c, cx, *stock, im.Pix); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}
	if *dump != "" {
		b, _ := json.MarshalIndent(c, "", " ")
		os.WriteFile(*dump, b, 0o644)
	}
	p := c.Compile(cx, -1)
	for _, n := range p.Notes {
		fmt.Fprintln(os.Stderr, n)
	}
	if *tif != "" {
		if _, err := os.Stat(*tif); err == nil {
			fmt.Fprintln(os.Stderr, "exists, not overwritten:", *tif)
			os.Exit(1)
		}
		dst := &cropper.Image{W: im.W, H: im.H, Bits: 16, Format: "tiff", Pix: make([]uint16, im.W*im.H*3)}
		p.RunU16(im.Pix, dst.Pix, nil)
		dst.Pix, dst.W, dst.H = develop.OrientU16(dst.Pix, dst.W, dst.H, develop.Orient(*orient))
		if err := cropper.Save(*tif, dst, cropper.SaveOptions{LZW: true, Software: "NegativeScan Studio v0.71"}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "wrote", *tif, develop.DomainNames[p.Domain])
		if *out == "" {
			return
		}
	}
	f, _ := os.Create(*out)
	defer f.Close()
	ch := len(im.Pix) / (im.W * im.H)
	buf := make([]byte, 12)
	cnt := 0
	for i := 0; i < im.W*im.H; i += *stride {
		r, g, b := p.Pixel(float32(im.Pix[i*ch])/65535, float32(im.Pix[i*ch+1])/65535, float32(im.Pix[i*ch+2])/65535)
		binary.LittleEndian.PutUint32(buf[0:], math.Float32bits(r))
		binary.LittleEndian.PutUint32(buf[4:], math.Float32bits(g))
		binary.LittleEndian.PutUint32(buf[8:], math.Float32bits(b))
		f.Write(buf)
		cnt++
	}
	fmt.Println(im.W, im.H, ch, cnt)
}
